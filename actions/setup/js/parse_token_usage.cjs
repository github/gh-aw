// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_PARSE } = require("./error_codes.cjs");
const { parseTokenUsageJsonl, generateTokenUsageSummary, formatAICForOutput } = require("./parse_mcp_gateway_log.cjs");
const { calculateWorkingSetFromJSONL } = require("./working_set_metrics.cjs");

/**
 * Parses the firewall proxy token-usage.jsonl and appends a collapsible markdown
 * table to $GITHUB_STEP_SUMMARY via core.summary.addDetails.
 *
 * Also writes aggregated token totals to /tmp/gh-aw/agent_usage.json so the data
 * is bundled in the agent artifact and accessible to third-party tools.
 */

const TOKEN_USAGE_AUDIT_PATH = "/tmp/gh-aw/sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl";
const TOKEN_USAGE_PATH = "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl";
// AWF v0.27.7+ may write token-usage.jsonl under --audit-dir as well as --proxy-logs-dir.
// Include this path so the agent job captures token data regardless of which dir AWF chose.
const TOKEN_USAGE_AWF_AUDIT_PATH = "/tmp/gh-aw/sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl";
const TOKEN_USAGE_PATHS = [TOKEN_USAGE_AUDIT_PATH, TOKEN_USAGE_AWF_AUDIT_PATH, TOKEN_USAGE_PATH];
const AGENT_USAGE_PATH = "/tmp/gh-aw/agent_usage.json";
const DEFAULT_SUMMARY_TITLE = "Token Usage";

/**
 * Returns readable, non-empty token usage files, skipping paths that error.
 * @param {string[]} paths
 * @returns {string[]}
 */
function getReadableTokenUsagePaths(paths) {
  const readablePaths = [];
  for (const path of paths) {
    try {
      if (!fs.existsSync(path)) continue;
      const stat = fs.statSync(path);
      if (!stat || stat.size <= 0) continue;
      readablePaths.push(path);
    } catch (error) {
      core.warning(`Skipping token usage path ${path}: ${getErrorMessage(error)}`);
    }
  }
  return readablePaths;
}

/**
 * Extracts request_id with lightweight matching (no full JSON parse).
 * @param {string} line
 * @returns {string}
 */
function extractRequestId(line) {
  const requestMatch = line.match(/"request_id"\s*:\s*"((?:\\.|[^"\\])*)"/);
  return requestMatch ? requestMatch[1] : "";
}

/**
 * Extracts a cross-file dedupe key with lightweight matching (no full JSON parse).
 * @param {string} line
 * @returns {string}
 */
function extractTokenUsageDedupeKey(line) {
  const requestId = extractRequestId(line);
  if (!requestId) return "";
  const eventMatch = line.match(/"event"\s*:\s*"((?:\\.|[^"\\])*)"/);
  return `${eventMatch ? eventMatch[1] : "token_usage"}:${requestId}`;
}

/**
 * Reads token usage files and deduplicates overlapping lines by event and request_id.
 * Falls back to raw line dedupe when request_id is absent.
 * @param {string[]} paths
 * @returns {string}
 */
function readDedupedTokenUsage(paths) {
  const uniqueLineKeys = new Set();
  const dedupedLines = [];

  for (const path of paths) {
    let fileContent = "";
    try {
      fileContent = fs.readFileSync(path, "utf8");
    } catch (error) {
      core.warning(`Skipping unreadable token usage file ${path}: ${getErrorMessage(error)}`);
      continue;
    }

    for (const line of fileContent.split("\n")) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      const dedupeKey = extractTokenUsageDedupeKey(trimmed) || trimmed;
      if (uniqueLineKeys.has(dedupeKey)) continue;
      uniqueLineKeys.add(dedupeKey);
      dedupedLines.push(trimmed);
    }
  }

  return dedupedLines.join("\n");
}

/**
 * Returns the token usage summary title for the current job.
 * @returns {string}
 */
function getSummaryTitle() {
  const title = process.env.GH_AW_TOKEN_USAGE_SUMMARY_TITLE;
  return title && title.trim() ? title.trim() : DEFAULT_SUMMARY_TITLE;
}

/**
 * Builds the token usage section for the GitHub step summary.
 * The Working-Set Rebuild Factor block is emitted as a sibling of the token
 * usage block: nesting a <details> inside another <details> prevents GitHub
 * from rendering the markdown table that follows it.
 * @param {string} title
 * @param {string} markdown
 * @param {ReturnType<typeof calculateWorkingSetFromJSONL>["workingSet"] | null} workingSet
 * @returns {string}
 */
function buildStepSummarySection(title, markdown, workingSet = null) {
  const workingSetSection = buildWorkingSetDetailsSection(workingSet);
  return `<details>\n<summary>${title}</summary>\n\nPer-request AI credits and token totals\n\n${markdown}</details>\n\n${workingSetSection}`;
}

/**
 * Builds a progressive-disclosure block for the Working-Set Rebuild Factor.
 * @param {ReturnType<typeof calculateWorkingSetFromJSONL>["workingSet"] | null} workingSet
 * @returns {string}
 */
function buildWorkingSetDetailsSection(workingSet) {
  if (!workingSet || typeof workingSet !== "object") return "";
  const measurementState = workingSet.measurement_state || "unavailable";
  const rebuildFactor = typeof workingSet.rebuild_factor === "number" && Number.isFinite(workingSet.rebuild_factor) ? workingSet.rebuild_factor : null;
  const displayFactor = rebuildFactor === null ? "unavailable" : `${rebuildFactor.toFixed(2)}×`;
  const displayInvocations = Number.isFinite(workingSet.invocations) ? workingSet.invocations.toLocaleString() : "0";
  const displayCumulative = Number.isFinite(workingSet.cumulative_input_tokens) ? workingSet.cumulative_input_tokens.toLocaleString() : "0";
  const displayPeak = Number.isFinite(workingSet.peak_input_tokens) ? workingSet.peak_input_tokens.toLocaleString() : "0";
  const displayExcess = Number.isFinite(workingSet.rebuild_excess_tokens) ? workingSet.rebuild_excess_tokens.toLocaleString() : "0";

  return [
    "<details>",
    `<summary>Working-Set Rebuild Factor (WSRF): ${displayFactor} (${measurementState})</summary>`,
    "",
    `- State: \`${measurementState}\``,
    `- Invocations: ${displayInvocations}`,
    `- Cumulative input tokens: ${displayCumulative}`,
    `- Peak invocation input tokens: ${displayPeak}`,
    `- Rebuild excess tokens: ${displayExcess}`,
    "",
    "</details>",
    "",
    "",
  ].join("\n");
}

/**
 * Renders the token usage markdown table as plain text for core.info output.
 * Strips markdown table separators, pipes, and bold markers so the table is
 * readable in the raw step log.
 * @param {string} title
 * @param {string} markdown
 * @returns {string}
 */
function renderTokenTableAsPlainText(title, markdown) {
  const plainText = markdown
    .replace(/^\|(?:[-: ]+\|)+$/gm, "") // Remove table separator lines (handles alignment colons)
    .replace(/^\|/gm, "") // Remove leading pipe from table rows
    .replace(/\|$/gm, "") // Remove trailing pipe from table rows
    .replace(/\s*\|\s*/g, " | ") // Normalize remaining pipes to spaced separators
    .replace(/\*\*(.*?)\*\*/g, "$1") // Remove bold markers
    .replace(/\n{3,}/g, "\n\n") // Collapse excess blank lines
    .trim();
  return `${title}\n\n${plainText}`;
}

/**
 * Appends the token usage section to GITHUB_STEP_SUMMARY when available.
 * Falls back to the Actions summary API when the summary path is unavailable.
 * @param {string} title
 * @param {string} markdown
 * @param {ReturnType<typeof calculateWorkingSetFromJSONL>["workingSet"] | null} workingSet
 * @returns {Promise<void>}
 */
async function appendStepSummarySection(title, markdown, workingSet = null) {
  const section = buildStepSummarySection(title, markdown, workingSet);
  const summaryPath = process.env.GITHUB_STEP_SUMMARY;
  if (summaryPath) {
    try {
      fs.appendFileSync(summaryPath, section, "utf8");
    } catch {
      /* ignore */
    }
    return;
  }

  core.summary.addRaw(section, true);
  await core.summary.write();
}

/**
 * Main function to parse token usage and write the step summary.
 */
async function main() {
  try {
    const tokenUsagePaths = getReadableTokenUsagePaths(TOKEN_USAGE_PATHS);
    if (tokenUsagePaths.length === 0) {
      core.info("No token usage data found, skipping summary");
      return;
    }

    const content = readDedupedTokenUsage(tokenUsagePaths);
    core.info(`Parsing token usage from ${tokenUsagePaths.length} file(s): ${tokenUsagePaths.join(", ")} (${content.length} bytes)`);

    const summary = parseTokenUsageJsonl(content);
    if (!summary || summary.totalRequests === 0) {
      core.info("Token usage file contained no valid entries");
      return;
    }
    for (const warning of summary.aiCreditsWarnings) {
      core.warning(`[ai-credits] ${warning}`);
    }
    const markdown = generateTokenUsageSummary(summary);
    const workingSet = calculateWorkingSetFromJSONL(content).workingSet;
    if (markdown.length > 0) {
      core.info(renderTokenTableAsPlainText(getSummaryTitle(), markdown));
      await appendStepSummarySection(getSummaryTitle(), markdown, workingSet);
    }

    core.info("Token usage summary appended to step summary");

    // Write agent_usage.json so the aggregated totals are bundled in the agent
    // artifact and accessible to third-party tools without parsing the step summary.
    // Determine the primary model: the one with the highest AI credits.
    // This is the actual model name from the API call logs, which may differ from
    // GH_AW_ENGINE_MODEL when the user specified a model alias (e.g. "agent").
    let primaryModel = "";
    let primaryModelAIC = -1;
    for (const [model, usage] of Object.entries(summary.byModel || {})) {
      if (model !== "unknown" && usage && typeof usage.aic === "number" && usage.aic > primaryModelAIC) {
        primaryModelAIC = usage.aic;
        primaryModel = model;
      }
    }

    const agentUsage = {
      input_tokens: summary.totalInputTokens,
      output_tokens: summary.totalOutputTokens,
      cache_read_tokens: summary.totalCacheReadTokens,
      cache_write_tokens: summary.totalCacheWriteTokens,
      ambient_context: Math.round(summary.ambientContextTokens || 0),
      ai_credits: summary.aiCreditsSource === "awf_reported" ? Number(summary.totalAIC.toFixed(6)) : Number((summary.totalAIC || 0).toFixed(3)),
      ...(primaryModel ? { primary_model: primaryModel } : {}),
    };
    fs.writeFileSync(AGENT_USAGE_PATH, JSON.stringify(agentUsage) + "\n");

    if (primaryModel) {
      core.exportVariable("GH_AW_PRIMARY_MODEL", primaryModel);
      core.setOutput("primary_model", primaryModel);
      core.info(`Primary model: ${primaryModel}`);
    }
    if (summary.aiCreditsSource === "awf_reported" || summary.totalAIC > 0) {
      const aic = formatAICForOutput(summary.totalAIC, summary.aiCreditsSource);
      core.exportVariable("GH_AW_AIC", aic);
      core.setOutput("aic", aic);
      core.info(`AI Credits: ${aic}`);
    }
    if (typeof summary.ambientContextTokens === "number" && summary.ambientContextTokens > 0) {
      const ambientContext = String(Math.round(summary.ambientContextTokens));
      core.exportVariable("GH_AW_AMBIENT_CONTEXT", ambientContext);
      core.setOutput("ambient_context", ambientContext);
      core.info(`Ambient context: ${ambientContext}`);
    }
  } catch (error) {
    core.setFailed(`${ERR_PARSE}: ${getErrorMessage(error)}`);
  }
}

// Export for testing
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    main,
    getReadableTokenUsagePaths,
    extractRequestId,
    extractTokenUsageDedupeKey,
    readDedupedTokenUsage,
    getSummaryTitle,
    buildStepSummarySection,
    buildWorkingSetDetailsSection,
    appendStepSummarySection,
    renderTokenTableAsPlainText,
    TOKEN_USAGE_AUDIT_PATH,
    TOKEN_USAGE_PATH,
    TOKEN_USAGE_AWF_AUDIT_PATH,
    TOKEN_USAGE_PATHS,
    AGENT_USAGE_PATH,
    DEFAULT_SUMMARY_TITLE,
  };
}

// Run main if called directly
if (require.main === module) {
  main().catch(err => {
    console.error(err instanceof Error && err.stack ? err.stack : getErrorMessage(err));
    process.exitCode = 1;
  });
}
