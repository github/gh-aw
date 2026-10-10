// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { ERR_PARSE } = require("./error_codes.cjs");
const { parseTokenUsageJsonl, generateTokenUsageSummary, formatAICForOutput } = require("./parse_mcp_gateway_log.cjs");
const { calculateWorkingSetFromJSONL } = require("./working_set_metrics.cjs");
const { mapAWFRoutingEffort, resolveAWFModelRoutingSelection, getAWFModelRoutingPolicy } = require("./awf_model_routing.cjs");
const { recordModelRouting, resolveEffectiveModel, validateModelIdentifier } = require("./model_attribution.cjs");

const DEFAULT_GH_AW_DIR = "/tmp/gh-aw";
/**
 * Parses the firewall proxy token-usage.jsonl and appends a collapsible markdown
 * table to $GITHUB_STEP_SUMMARY via core.summary.addDetails.
 *
 * Also writes aggregated token totals to the gh-aw directory so the data is
 * bundled in the agent artifact and accessible to third-party tools.
 */

const TOKEN_USAGE_AUDIT_PATH = path.join(DEFAULT_GH_AW_DIR, "sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl");
const TOKEN_USAGE_PATH = path.join(DEFAULT_GH_AW_DIR, "sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl");
// AWF v0.27.7+ may write token-usage.jsonl under --audit-dir as well as --proxy-logs-dir.
// Include this path so the agent job captures token data regardless of which dir AWF chose.
const TOKEN_USAGE_AWF_AUDIT_PATH = path.join(DEFAULT_GH_AW_DIR, "sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl");
const TOKEN_USAGE_PATHS = [TOKEN_USAGE_AUDIT_PATH, TOKEN_USAGE_AWF_AUDIT_PATH, TOKEN_USAGE_PATH];
const AGENT_USAGE_PATH = path.join(DEFAULT_GH_AW_DIR, "agent_usage.json");
const AGENT_USAGE_JSONL_PATH = path.join(DEFAULT_GH_AW_DIR, "agent_usage.jsonl");
const COPILOT_SESSION_STATE_DIR = path.join(DEFAULT_GH_AW_DIR, "sandbox/agent/logs/copilot-session-state");
const DEFAULT_SUMMARY_TITLE = "Token Usage";
function getGhAwPath(relativePath) {
  const root = process.env.GH_AW_TMP_DIR;
  return path.join(root && root.trim() ? root.trim() : DEFAULT_GH_AW_DIR, relativePath);
}

function getTokenUsagePaths() {
  return [getGhAwPath("sandbox/firewall-audit-logs/api-proxy-logs/token-usage.jsonl"), getGhAwPath("sandbox/firewall/audit/api-proxy-logs/token-usage.jsonl"), getGhAwPath("sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl")];
}

function getUsageOutputPath(envName, defaultPath) {
  const configured = process.env[envName];
  return configured && configured.trim() ? configured.trim() : defaultPath;
}

/** @returns {any|null} */
function readJSONIfExists(filePath) {
  try {
    return JSON.parse(fs.readFileSync(filePath, "utf8"));
  } catch {
    return null;
  }
}

/** @returns {any|null} */
function readModelRoutingRecord(ghAwDir) {
  const paths = ["sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl", "sandbox/firewall/audit/api-proxy-logs/model-routing.jsonl", "sandbox/firewall-audit-logs/api-proxy-logs/model-routing.jsonl"];
  /** @type {any|null} */
  let result = null;
  const requestRecords = [];
  for (const relativePath of paths) {
    let lines;
    try {
      lines = fs.readFileSync(path.join(ghAwDir, relativePath), "utf8").split("\n");
    } catch {
      continue;
    }
    for (const line of lines) {
      try {
        const record = JSON.parse(line);
        if (typeof record?._schema !== "string" || !record._schema.startsWith("model-routing/")) continue;
        if (record.stage === "selection" || record.stage === "failure") result = record;
        else if (record.stage === "request") requestRecords.push(record);
      } catch {
        // Skip incomplete proxy records.
      }
    }
  }
  return result || requestRecords.length ? { record: result, requestRecords } : null;
}

function getModelRoutingRequestModel(record) {
  for (const key of ["requested_model", "wire_model", "selected_model", "model"]) {
    const model = validateModelIdentifier(record?.[key]);
    if (model) return model;
  }
  return "";
}

function hasCorroboratedModelRoutingRequests(requestRecords, wireModel, endpoint) {
  const requests = requestRecords.filter(record => getModelRoutingRequestModel(record) === wireModel);
  return requests.length > 0 && requests.every(record => record.routed === "as_selected" && record.upstream_endpoint === endpoint && Array.isArray(record.deviations) && record.deviations.every(deviation => deviation === "endpoint"));
}

function getModelRoutingSelectionFailureCode(error) {
  return /none compatible|not supported by this engine|does not advertise endpoint/.test(error) ? "unsupported_endpoint" : "invalid_selection";
}

/** @returns {any|null} */
function resolveModelRoutingOutcome(env = process.env, ghAwDir = env.GH_AW_TMP_DIR || DEFAULT_GH_AW_DIR) {
  if (env.GH_AW_MODEL_ROUTING_ENABLED !== "true") return null;
  const proxyRecords = readModelRoutingRecord(ghAwDir);
  const record = proxyRecords?.record;
  const requestRecords = proxyRecords?.requestRecords || [];
  const reflectData = readJSONIfExists(path.join(ghAwDir, "agent", "awf-reflect.json"));
  const advisory = readJSONIfExists(path.join(ghAwDir, "agent", "awf-routing-outcome.json"));
  let routing;
  if (record?.stage === "failure") {
    routing = { status: "failed", failure_code: record.code, detail: record.detail };
  } else if (record?.stage === "selection") {
    const wireModel = validateModelIdentifier(record.wire_model || record.selected_model);
    routing = {
      status: wireModel ? "selected" : "rejected",
      provider: record.selected_provider || record.provider,
      model: validateModelIdentifier(record.selected_model) || wireModel,
      wire_model: wireModel,
      effort: record.selected_effort == null || record.selected_effort === "" ? null : record.selected_effort,
      endpoint: record.endpoint,
      mode: record.mode,
      selected_id: record.selected_id,
      router_version: record.router?.version,
      failure_code: wireModel ? "" : "invalid_selection",
    };
    const policy = getAWFModelRoutingPolicy(String(env.GH_AW_ENGINE_ID || ""));
    const selectedAdvisory = advisory?.status === "selected" && (advisory.endpoint !== undefined || advisory.selected_endpoint !== undefined);
    const advisoryEndpointOverride = selectedAdvisory && ((typeof advisory.endpoint === "string" && advisory.endpoint !== routing.endpoint) || advisory.selected_endpoint !== routing.endpoint);
    const endpointOverride = !policy.endpoints.includes(routing.endpoint) || advisoryEndpointOverride;
    if (advisory?.status === "selected" && advisory.wire_model && advisory.wire_model !== wireModel) {
      routing = { ...routing, status: "rejected", failure_code: "harness_selection_mismatch", detail: "Harness routing outcome did not match the proxy selection" };
    } else if (["failed", "rejected", "pending", "unavailable"].includes(advisory?.status)) {
      routing = { ...routing, status: advisory.status, failure_code: advisory.failure_code || routing.failure_code, detail: advisory.detail || routing.detail };
    } else if (endpointOverride) {
      const requestedEndpoint = selectedAdvisory && typeof advisory.endpoint === "string" ? advisory.endpoint : routing.endpoint;
      const checked = resolveAWFModelRoutingSelection(
        { ...reflectData, routing: { status: "selected", selection: { ...routing, endpoint: requestedEndpoint, selected_endpoint: routing.endpoint } } },
        true,
        policy.endpoints,
        policy.allowEndpointOverride
      );
      if (checked.error || !checked.selection) {
        const detail = checked.error || "AWF /reflect did not return a compatible model-routing selection";
        routing = { ...routing, status: "rejected", failure_code: getModelRoutingSelectionFailureCode(detail), detail };
      } else {
        const effectiveEndpoint = selectedAdvisory ? advisory.endpoint : checked.selection.endpoint;
        const selectedModelMatches = !selectedAdvisory || advisory.wire_model === wireModel;
        const selectedEndpointMatches = !selectedAdvisory || advisory.selected_endpoint === routing.endpoint;
        const endpointMatches = policy.endpoints.includes(effectiveEndpoint) && checked.selection.endpoint === effectiveEndpoint;
        const requestsCorroborate = hasCorroboratedModelRoutingRequests(requestRecords, wireModel, effectiveEndpoint);
        if (!selectedModelMatches || !selectedEndpointMatches || !endpointMatches || !requestsCorroborate) {
          routing = {
            ...routing,
            status: "rejected",
            failure_code: "uncorroborated_endpoint",
            detail: "AWF model routing endpoint override was not corroborated by matching proxy request records",
          };
        } else {
          routing = { ...routing, endpoint: effectiveEndpoint, selected_endpoint: routing.endpoint };
        }
      }
    } else if (reflectData?.endpoints) {
      const checked = resolveAWFModelRoutingSelection({ ...reflectData, routing: { status: "selected", selection: routing } }, true, policy.endpoints, policy.allowEndpointOverride);
      if (checked.error || !checked.selection) {
        const detail = checked.error || "AWF /reflect did not return a compatible model-routing selection";
        routing = { ...routing, status: "rejected", failure_code: getModelRoutingSelectionFailureCode(detail), detail };
      } else {
        routing = { ...routing, endpoint: checked.selection.endpoint, selected_endpoint: routing.endpoint };
      }
    }
  } else {
    const reflectRouting = reflectData?.routing;
    if (reflectRouting?.status === "selected") {
      routing = {
        status: "rejected",
        failure_code: "uncorroborated_selection",
        detail: "AWF /reflect selection has no corroborating proxy model-routing record",
      };
    } else {
      routing = reflectRouting
        ? {
            status: reflectRouting.status || "pending",
            failure_code: reflectRouting.failure_code,
            detail: reflectRouting.detail,
          }
        : { status: "unavailable" };
    }
  }

  if (record?.stage !== "selection" && advisory && ["failed", "rejected", "pending", "unavailable"].includes(advisory.status)) {
    routing = { ...routing, status: advisory.status, failure_code: advisory.failure_code || routing.failure_code, detail: advisory.detail || routing.detail };
  } else if (record?.stage !== "selection" && advisory?.status === "selected" && advisory.wire_model && advisory.wire_model !== routing.wire_model) {
    routing = { ...routing, status: "rejected", failure_code: "harness_selection_mismatch", detail: "Harness routing outcome did not match the proxy selection" };
  }

  if (routing.status === "selected") {
    const engine = String(env.GH_AW_ENGINE_ID || "").toLowerCase();
    const mapped = mapAWFRoutingEffort(engine, routing.effort == null ? null : String(routing.effort).toLowerCase());
    if (mapped.error) {
      routing = { ...routing, status: "rejected", failure_code: "unsupported_effort", detail: mapped.error, effort: null };
    } else if (mapped.effort && mapped.effort !== routing.effort) {
      routing.applied_effort = mapped.effort;
    }
  }
  return routing;
}

/** @returns {{routing: any, effective: any}|null} */
function recordModelRoutingFromArtifacts(env = process.env, ghAwDir = env.GH_AW_TMP_DIR || DEFAULT_GH_AW_DIR) {
  const routing = resolveModelRoutingOutcome(env, ghAwDir);
  if (!routing) return null;
  const infoPath = path.join(ghAwDir, "aw_info.json");
  const modelRouting = recordModelRouting(routing, env, infoPath);
  const effective = resolveEffectiveModel(infoPath, "agent", env);
  core.setOutput("model", effective.model);
  core.setOutput("model_effort", effective.effort);
  core.setOutput("model_routing_status", routing.status);
  return { routing: modelRouting, effective };
}

function persistAgentAwInfoCopy(ghAwDir = process.env.GH_AW_TMP_DIR || DEFAULT_GH_AW_DIR) {
  const infoPath = path.join(ghAwDir, "aw_info.json");
  const artifactInfoPath = path.join(ghAwDir, "agent", "aw_info.json");
  if (!fs.existsSync(infoPath)) return;
  try {
    fs.mkdirSync(path.dirname(artifactInfoPath), { recursive: true });
    fs.copyFileSync(infoPath, artifactInfoPath);
  } catch (error) {
    core.warning(`Could not add routed aw_info.json to the agent artifact: ${getErrorMessage(error)}`);
  }
}

function writeEmptyUsageEvidence() {
  if (process.env.GH_AW_WRITE_EMPTY_USAGE !== "true") return;
  const usagePath = getUsageOutputPath("GH_AW_AGENT_USAGE_PATH", getGhAwPath("agent_usage.json"));
  const usageJSONLPath = getUsageOutputPath("GH_AW_AGENT_USAGE_JSONL_PATH", getGhAwPath("agent_usage.jsonl"));
  fs.mkdirSync(path.dirname(usagePath), { recursive: true });
  fs.mkdirSync(path.dirname(usageJSONLPath), { recursive: true });
  fs.writeFileSync(usagePath, '{"input_tokens":0,"output_tokens":0,"ai_credits":0}\n');
  fs.writeFileSync(usageJSONLPath, '{"provider":"unknown","ai_credits":0}\n');
  core.info("Recorded explicit zero token usage evidence");
}

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
 * Finds the latest valid Copilot session usage checkpoint.
 * @param {string} sessionStateDir
 * @returns {{aiCredits: number, premiumRequests: number} | null}
 */
function findCopilotUsageCheckpoint(sessionStateDir = getGhAwPath("sandbox/agent/logs/copilot-session-state")) {
  if (!fs.existsSync(sessionStateDir)) return null;

  /** @type {string[]} */
  const eventPaths = [];
  try {
    for (const entry of fs.readdirSync(sessionStateDir, { withFileTypes: true })) {
      if (entry.isFile() && entry.name === "events.jsonl") {
        eventPaths.push(path.join(sessionStateDir, entry.name));
      } else if (entry.isDirectory()) {
        const eventsPath = path.join(sessionStateDir, entry.name, "events.jsonl");
        if (fs.existsSync(eventsPath)) eventPaths.push(eventsPath);
      }
    }
  } catch {
    return null;
  }

  /** @type {{aiCredits: number, premiumRequests: number} | null} */
  let latest = null;
  let latestTimestamp = Number.NEGATIVE_INFINITY;
  let sequence = 0;
  for (const eventsPath of eventPaths.sort()) {
    let content;
    try {
      content = fs.readFileSync(eventsPath, "utf8");
    } catch {
      continue;
    }
    for (const line of content.split("\n")) {
      sequence++;
      if (!line.trim()) continue;
      try {
        const event = JSON.parse(line);
        if (event?.type !== "session.usage_checkpoint") continue;
        const totalNanoAiu = Number(event?.data?.totalNanoAiu);
        if (!Number.isFinite(totalNanoAiu) || totalNanoAiu < 0) continue;
        const parsedTimestamp = Date.parse(event.timestamp);
        const timestamp = Number.isFinite(parsedTimestamp) ? parsedTimestamp : sequence;
        if (latest && timestamp < latestTimestamp) continue;
        const premiumRequests = Number(event?.data?.totalPremiumRequests);
        latest = {
          aiCredits: totalNanoAiu / 1e9,
          premiumRequests: Number.isFinite(premiumRequests) && premiumRequests >= 0 ? premiumRequests : 0,
        };
        latestTimestamp = timestamp;
      } catch {
        // Ignore malformed session events.
      }
    }
  }
  return latest;
}

/**
 * Writes and reports authoritative usage from a Copilot session checkpoint.
 * @param {{aiCredits: number, premiumRequests: number}} checkpoint
 * @returns {Promise<void>}
 */
async function reportCopilotUsageCheckpoint(checkpoint) {
  const agentUsage = {
    ai_credits: checkpoint.aiCredits,
    premium_requests: checkpoint.premiumRequests,
  };
  try {
    fs.writeFileSync(getUsageOutputPath("GH_AW_AGENT_USAGE_PATH", getGhAwPath("agent_usage.json")), JSON.stringify(agentUsage) + "\n");
    fs.writeFileSync(getUsageOutputPath("GH_AW_AGENT_USAGE_JSONL_PATH", getGhAwPath("agent_usage.jsonl")), JSON.stringify({ provider: "copilot", ai_credits: checkpoint.aiCredits, premium_requests: checkpoint.premiumRequests }) + "\n");
  } catch (error) {
    throw new Error(`${ERR_PARSE}: Failed to write Copilot usage files: ${getErrorMessage(error)}`, { cause: error });
  }

  const aic = formatAICForOutput(checkpoint.aiCredits, "awf_reported");
  core.exportVariable("GH_AW_AIC", aic);
  core.setOutput("aic", aic);
  const markdown = ["| AI Credits | Premium Requests |", "| ---: | ---: |", `| ${aic} | ${checkpoint.premiumRequests.toLocaleString()} |`, ""].join("\n");
  core.info(`Copilot session usage: ${aic} AI Credits, ${checkpoint.premiumRequests} premium request(s)`);
  await appendStepSummarySection(getSummaryTitle(), markdown);
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
async function main(copilotSessionStateDir = getGhAwPath("sandbox/agent/logs/copilot-session-state")) {
  /** @type {{routing: any, effective: any}|null} */
  let routedAttribution = null;
  try {
    routedAttribution = recordModelRoutingFromArtifacts();
    const tokenUsagePaths = getReadableTokenUsagePaths(getTokenUsagePaths());
    if (tokenUsagePaths.length === 0) {
      const checkpoint = findCopilotUsageCheckpoint(copilotSessionStateDir);
      if (checkpoint) {
        await reportCopilotUsageCheckpoint(checkpoint);
        return;
      }
      writeEmptyUsageEvidence();
      core.info("No token usage data found, skipping summary");
      return;
    }

    const content = readDedupedTokenUsage(tokenUsagePaths);
    core.info(`Parsing token usage from ${tokenUsagePaths.length} file(s): ${tokenUsagePaths.join(", ")} (${content.length} bytes)`);

    const summary = parseTokenUsageJsonl(content);
    if (!summary || summary.totalRequests === 0) {
      const checkpoint = findCopilotUsageCheckpoint(copilotSessionStateDir);
      if (checkpoint) {
        await reportCopilotUsageCheckpoint(checkpoint);
        return;
      }
      writeEmptyUsageEvidence();
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
    const { getFallbackModel, recordFallbackModelFromUsage } = require("./model_attribution.cjs");
    const fallbackModel = recordFallbackModelFromUsage(content, process.env, undefined, message => core.warning(message));
    primaryModel = fallbackModel || getFallbackModel(undefined, process.env.GH_AW_PHASE || "agent") || primaryModel;
    const effectiveAttribution = resolveEffectiveModel(undefined, process.env.GH_AW_PHASE || "agent", { ...process.env, GH_AW_PRIMARY_MODEL: primaryModel });
    if (process.env.GH_AW_PHASE !== "detection") primaryModel = effectiveAttribution.model || primaryModel;

    const agentUsage = {
      input_tokens: summary.totalInputTokens,
      output_tokens: summary.totalOutputTokens,
      cache_read_tokens: summary.totalCacheReadTokens,
      cache_write_tokens: summary.totalCacheWriteTokens,
      ambient_context: Math.round(summary.ambientContextTokens || 0),
      ai_credits: summary.aiCreditsSource === "awf_reported" ? Number(summary.totalAIC.toFixed(6)) : Number((summary.totalAIC || 0).toFixed(3)),
      ...(primaryModel ? { primary_model: primaryModel } : {}),
      ...(routedAttribution ? { model_routing: routedAttribution.routing } : {}),
    };
    fs.writeFileSync(getUsageOutputPath("GH_AW_AGENT_USAGE_PATH", getGhAwPath("agent_usage.json")), JSON.stringify(agentUsage) + "\n");

    if (primaryModel) {
      core.exportVariable("GH_AW_PRIMARY_MODEL", primaryModel);
      core.setOutput("primary_model", primaryModel);
      core.info(`Primary model: ${primaryModel}`);
    }
    if (routedAttribution) core.setOutput("model", effectiveAttribution.model || "");
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
  } finally {
    persistAgentAwInfoCopy();
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
    AGENT_USAGE_JSONL_PATH,
    COPILOT_SESSION_STATE_DIR,
    DEFAULT_SUMMARY_TITLE,
    findCopilotUsageCheckpoint,
    reportCopilotUsageCheckpoint,
    getUsageOutputPath,
    getTokenUsagePaths,
    writeEmptyUsageEvidence,
    readModelRoutingRecord,
    resolveModelRoutingOutcome,
    recordModelRoutingFromArtifacts,
    persistAgentAwInfoCopy,
  };
}

// Run main if called directly
if (require.main === module) {
  main().catch(err => {
    console.error(err instanceof Error && err.stack ? err.stack : getErrorMessage(err));
    process.exitCode = 1;
  });
}
