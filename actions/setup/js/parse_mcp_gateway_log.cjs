// @ts-check
/// <reference types="@actions/github-script" />

const { iterateJsonl, iterateJsonlEvents } = require("./jsonl_helpers.cjs");
const fs = require("fs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { displayDirectories } = require("./display_file_helpers.cjs");
const { ERR_PARSE, ERR_SYSTEM } = require("./error_codes.cjs");
const { formatModelEmojiAlias } = require("./model_aliases.cjs");
const { computeInferenceAIC, findModelPricing, formatAIC } = require("./model_costs.cjs");
const { parseUnknownModelAICreditsFromAuditLog } = require("./ai_credits_context.cjs");
const { getMcpRateLimitHint, getRpcRateLimitHint } = require("./mcp_rate_limit.cjs");
const { MAX_STEP_SUMMARY_SIZE } = require("./log_parser_shared.cjs");
const { boundSummaryLines } = require("./agent_session_render.cjs");

/**
 * Parses MCP gateway logs and creates a step summary
 * Log file locations:
 *  - /tmp/gh-aw/mcp-logs/gateway.jsonl (structured JSONL log, parsed for DIFC_FILTERED events)
 *  - /tmp/gh-aw/mcp-logs/gateway.md (markdown summary from gateway, preferred for general content)
 *  - /tmp/gh-aw/mcp-logs/gateway.log (main gateway log, fallback)
 *  - /tmp/gh-aw/mcp-logs/stderr.log (stderr output, fallback)
 *  - /tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl (token usage from firewall proxy)
 */

const TOKEN_USAGE_PATH = "/tmp/gh-aw/sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl";
const MAX_RPC_SUMMARY_DETAILS_LENGTH = 120;
const MAX_RPC_SUMMARY_GENERIC_LENGTH = 160;
const MAX_RPC_MESSAGE_LABEL_LENGTH = 80;
const TOP_LEVEL_RPC_IGNORED_KEYS = new Set(["timestamp", "direction", "type", "event", "_schema", "server_id", "payload"]);
// Maps the "event" field values used by the schema "rpc-message/v2" format (written by
// real Copilot CLI MCP gateway telemetry) to the legacy "type" values ("REQUEST",
// "RESPONSE", "DIFC_FILTERED") this file was originally written against. Real-world
// rpc-messages.jsonl files do not populate a top-level "type" field at all; they use
// "event" instead (e.g. "rpc_request"/"rpc_response").
const RPC_EVENT_TO_TYPE = { rpc_request: "REQUEST", rpc_response: "RESPONSE", difc_filtered: "DIFC_FILTERED" };

/**
 * Returns the normalized rpc-messages.jsonl entry type ("REQUEST", "RESPONSE", or
 * "DIFC_FILTERED"), accepting either the legacy top-level "type" field or the
 * "event" field used by schema "rpc-message/v2".
 * @param {Object} entry
 * @returns {string}
 */
function getRpcMessageType(entry) {
  if (typeof entry?.type === "string" && entry.type) return entry.type;
  if (typeof entry?.event === "string") return RPC_EVENT_TO_TYPE[entry.event] || entry.event;
  return "";
}
const AI_CREDITS_RATE_LIMIT_PATTERNS = [
  /ai[\s_-]*credits?.{0,80}(?:rate[\s-]*limit|limit exceeded|budget exceeded|exceeded)/i,
  /(?:rate[\s-]*limit|too many requests).{0,80}(?:ai[\s_-]*credits?)/i,
  /\b429\b.{0,80}(?:rate[\s-]*limit|too many requests|ai[\s_-]*credits?)/i,
];
// Detects the AWF API proxy HTTP 400 error emitted when maxAiCredits is active and
// the requested model is not in the built-in pricing table.
const UNKNOWN_MODEL_AI_CREDITS_PATTERNS = [/\bunknown_model_ai_credits\b/i];

/**
 * Formats milliseconds as a human-readable duration string.
 * @param {number} ms - Duration in milliseconds
 * @returns {string} Formatted duration (e.g. "500ms", "2.5s", "1m30s")
 */
function formatDurationMs(ms) {
  if (ms < 1000) return `${ms}ms`;
  const seconds = ms / 1000;
  if (seconds < 60) return `${seconds.toFixed(1)}s`;
  const minutes = Math.floor(seconds / 60);
  const secs = Math.round(seconds % 60);
  return `${minutes}m${secs}s`;
}

/**
 * AWF-reported AIC fields are numeric JSON fields. Invalid or missing values
 * are identified so the caller can report that legacy pricing was used.
 *
 * @param {unknown} value
 * @returns {number | null}
 */
function parseNonNegativeFiniteNumber(value) {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : null;
}

/**
 * Preserve AWF's reported six-decimal precision while retaining the
 * historical three-decimal export for recomputed legacy records.
 *
 * @param {number} value
 * @param {"awf_reported"|"recomputed"} source
 * @returns {string}
 */
function formatAICForOutput(value, source) {
  if (!Number.isFinite(value) || value < 0) return "";
  if (source !== "awf_reported") return value.toFixed(3);
  const rounded = Number(value.toFixed(6));
  return String(rounded);
}

/**
 * Keep the human-readable table compact for large totals while preserving
 * exact AWF precision for the normal per-run range.
 *
 * @param {number} value
 * @param {"awf_reported"|"recomputed"} source
 * @returns {string}
 */
function formatAICForTable(value, source) {
  return source === "awf_reported" && value < 1000 ? formatAICForOutput(value, source) : formatAIC(value);
}

/**
 * Parses token-usage.jsonl content and returns an aggregated summary.
 *
 * token-usage.jsonl is agent-visible runtime telemetry. This parser uses its
 * AWF-computed AIC fields only for diagnostics and public reporting; budget
 * aborts, retries, authentication, and safe outputs use separate paths.
 *
 * @param {string} jsonlContent - The token-usage.jsonl file content
 * @returns {{totalInputTokens: number, totalOutputTokens: number, totalCacheReadTokens: number, totalCacheWriteTokens: number, tokenUsage: Partial<Record<"input_tokens"|"output_tokens"|"cache_read_tokens"|"cache_write_tokens", number>>, totalRequests: number, totalDurationMs: number, totalAIC: number, aiCreditsAvailable: boolean, aiCreditsSource: "awf_reported"|"recomputed", aiCreditsWarnings: string[], ambientContextTokens: number|undefined, byModel: Record<string, any>, entries: Array} | null}
 * ambientContextTokens records first-request context size as:
 * input_tokens + ((cache_read_tokens + cache_write_tokens) / 10).
 */
function parseTokenUsageJsonl(jsonlContent) {
  const seenRequestIds = new Set();
  let duplicateRecordCount = 0;
  const summary = {
    totalInputTokens: 0,
    totalOutputTokens: 0,
    totalCacheReadTokens: 0,
    totalCacheWriteTokens: 0,
    /** @type {Partial<Record<"input_tokens"|"output_tokens"|"cache_read_tokens"|"cache_write_tokens", number>>} */
    tokenUsage: { input_tokens: 0, output_tokens: 0, cache_read_tokens: 0, cache_write_tokens: 0 },
    totalRequests: 0,
    totalDurationMs: 0,
    totalAIC: 0,
    aiCreditsAvailable: true,
    /** @type {"awf_reported"|"recomputed"} */
    aiCreditsSource: "recomputed",
    /** @type {string[]} */
    aiCreditsWarnings: [],
    /** @type {number | undefined} */
    ambientContextTokens: undefined,
    /** @type {Record<string, any>} */
    byModel: Object.create(null),
    /** @type {{ model: string, provider: string, inputTokens: number, outputTokens: number, cacheReadTokens: number, cacheWriteTokens: number, reasoningTokens: number, durationMs: number, timestampMs: number|null, originalIndex: number, inputTokensIncludeCache: boolean|undefined, hasInputTokensIncludeCacheField: boolean, reportedDeltaAIC: number|null, reportedTotalAIC: number|null, hasReportedDeltaField: boolean, hasReportedTotalField: boolean, deltaAIC: number, runningAIC: number }[]} */
    entries: [],
  };

  for (const event of iterateJsonlEvents(jsonlContent)) {
    if (event.type !== "json") {
      if (event.reason !== "blank") summary.tokenUsage = {};
      continue;
    }
    const entry = event.value;
    try {
      if (!entry || typeof entry !== "object") continue;
      const requestId = typeof entry.request_id === "string" ? entry.request_id.trim() : "";
      const eventName = typeof entry.event === "string" && entry.event ? entry.event : "token_usage";
      const dedupeKey = requestId ? `${eventName}:${requestId}` : "";
      if (dedupeKey && seenRequestIds.has(dedupeKey)) {
        duplicateRecordCount++;
        continue;
      }
      if (dedupeKey) seenRequestIds.add(dedupeKey);

      // Public counters require complete evidence across requests. Keep the
      // historical display/pricing totals separate from unavailable counters.
      for (const key of /** @type {Array<keyof typeof summary.tokenUsage>} */ Object.keys(summary.tokenUsage)) {
        const value = parseNonNegativeFiniteNumber(entry[key]);
        const total = summary.tokenUsage[key] + (value ?? 0);
        if (value === null || !Number.isFinite(total)) {
          delete summary.tokenUsage[key];
        } else {
          summary.tokenUsage[key] = total;
        }
      }
      const inputTokens = parseNonNegativeFiniteNumber(entry.input_tokens) ?? 0;
      const outputTokens = parseNonNegativeFiniteNumber(entry.output_tokens) ?? 0;
      const cacheReadTokens = parseNonNegativeFiniteNumber(entry.cache_read_tokens) ?? 0;
      const cacheWriteTokens = parseNonNegativeFiniteNumber(entry.cache_write_tokens) ?? 0;
      const reasoningTokens = entry.reasoning_tokens || 0;
      const durationMs = entry.duration_ms || 0;
      const parsedTimestamp = typeof entry.timestamp === "string" ? Date.parse(entry.timestamp) : Number.NaN;
      const hasInputTokensIncludeCacheField = Object.prototype.hasOwnProperty.call(entry, "input_tokens_include_cache") && entry.input_tokens_include_cache !== null;
      const inputTokensIncludeCache = typeof entry.input_tokens_include_cache === "boolean" ? entry.input_tokens_include_cache : undefined;
      const hasReportedDelta = Object.prototype.hasOwnProperty.call(entry, "ai_credits_this_response");
      const hasReportedTotal = Object.prototype.hasOwnProperty.call(entry, "ai_credits_total");
      const reportedDeltaAIC = parseNonNegativeFiniteNumber(entry.ai_credits_this_response);
      const reportedTotalAIC = parseNonNegativeFiniteNumber(entry.ai_credits_total);

      summary.totalInputTokens += inputTokens;
      summary.totalOutputTokens += outputTokens;
      summary.totalCacheReadTokens += cacheReadTokens;
      summary.totalCacheWriteTokens += cacheWriteTokens;
      summary.totalRequests++;
      summary.totalDurationMs += durationMs;
      if (summary.ambientContextTokens === undefined) {
        const cacheTokens = cacheReadTokens + cacheWriteTokens;
        summary.ambientContextTokens = inputTokens + cacheTokens / 10;
      }

      const model = entry.model || "unknown";
      summary.byModel[model] ??= {
        provider: entry.provider || "",
        inputTokens: 0,
        outputTokens: 0,
        cacheReadTokens: 0,
        cacheWriteTokens: 0,
        reasoningTokens: 0,
        requests: 0,
        durationMs: 0,
        aic: 0,
      };
      const m = summary.byModel[model];
      m.inputTokens += inputTokens;
      m.outputTokens += outputTokens;
      m.cacheReadTokens += cacheReadTokens;
      m.cacheWriteTokens += cacheWriteTokens;
      m.reasoningTokens += reasoningTokens;
      m.requests++;
      m.durationMs += durationMs;

      summary.entries.push({
        model,
        provider: m.provider,
        inputTokens,
        outputTokens,
        cacheReadTokens,
        cacheWriteTokens,
        reasoningTokens,
        durationMs,
        timestampMs: Number.isFinite(parsedTimestamp) ? parsedTimestamp : null,
        originalIndex: summary.entries.length,
        inputTokensIncludeCache,
        hasInputTokensIncludeCacheField,
        reportedDeltaAIC,
        reportedTotalAIC,
        hasReportedDeltaField: hasReportedDelta,
        hasReportedTotalField: hasReportedTotal,
        deltaAIC: 0,
        runningAIC: 0,
      });
    } catch {
      // Malformed line — ignored.
    }
  }

  if (summary.totalRequests === 0) return null;
  if (duplicateRecordCount > 0) {
    summary.aiCreditsWarnings.push(`${duplicateRecordCount} duplicate token usage record(s) were ignored by event and request_id.`);
  }

  const hasReportedAIC = summary.entries.some(entry => entry.reportedDeltaAIC !== null || entry.reportedTotalAIC !== null);
  const hasAnyReportedAICFields = summary.entries.some(entry => entry.hasReportedDeltaField || entry.hasReportedTotalField);
  const hasExplicitCacheSemantics = summary.entries.some(entry => typeof entry.inputTokensIncludeCache === "boolean");
  let invalidCacheSemanticsCount = 0;

  if (!hasAnyReportedAICFields && !hasExplicitCacheSemantics) {
    invalidCacheSemanticsCount = summary.entries.filter(entry => entry.hasInputTokensIncludeCacheField && typeof entry.inputTokensIncludeCache !== "boolean").length;
    // Preserve the legacy aggregation contract exactly for records emitted before
    // AWF added reported AIC and explicit cache-semantics fields.
    let totalAIC = 0;
    for (const [model, usage] of Object.entries(summary.byModel)) {
      if (!findModelPricing(usage.provider || "", model)) summary.aiCreditsAvailable = false;
      const aic = computeInferenceAIC({
        provider: usage.provider || "",
        model,
        inputTokens: usage.inputTokens,
        outputTokens: usage.outputTokens,
        cacheReadTokens: usage.cacheReadTokens,
        cacheWriteTokens: usage.cacheWriteTokens,
        reasoningTokens: usage.reasoningTokens || 0,
      });
      usage.aic = aic;
      totalAIC += aic;
    }
    summary.totalAIC = totalAIC;

    for (const entry of summary.entries) {
      entry.deltaAIC = computeInferenceAIC({
        provider: entry.provider || "",
        model: entry.model,
        inputTokens: entry.inputTokens,
        outputTokens: entry.outputTokens,
        cacheReadTokens: entry.cacheReadTokens,
        cacheWriteTokens: entry.cacheWriteTokens,
        reasoningTokens: entry.reasoningTokens || 0,
      });
    }
  } else {
    summary.entries.sort((left, right) => {
      if (left.timestampMs !== null && right.timestampMs !== null) {
        return left.timestampMs - right.timestampMs || left.originalIndex - right.originalIndex;
      }
      if (left.timestampMs !== null) return -1;
      if (right.timestampMs !== null) return 1;
      return left.originalIndex - right.originalIndex;
    });
    const firstEntry = summary.entries[0];
    if (firstEntry) {
      summary.ambientContextTokens = firstEntry.inputTokens + (firstEntry.cacheReadTokens + firstEntry.cacheWriteTokens) / 10;
    }
    let runningAIC = 0;
    let fallbackRecordCount = 0;
    for (const usage of Object.values(summary.byModel)) {
      usage.aic = 0;
    }

    for (let index = 0; index < summary.entries.length; index++) {
      const entry = summary.entries[index];
      const reportedFieldsMissingOrInvalid = hasAnyReportedAICFields && (!entry.hasReportedDeltaField || entry.reportedDeltaAIC === null || !entry.hasReportedTotalField || entry.reportedTotalAIC === null);
      if (reportedFieldsMissingOrInvalid) fallbackRecordCount++;

      if (entry.reportedDeltaAIC !== null) {
        entry.deltaAIC = entry.reportedDeltaAIC;
      } else {
        if (!findModelPricing(entry.provider || "", entry.model)) summary.aiCreditsAvailable = false;
        if (entry.hasInputTokensIncludeCacheField && typeof entry.inputTokensIncludeCache !== "boolean") {
          invalidCacheSemanticsCount++;
        }
        entry.deltaAIC = computeInferenceAIC({
          provider: entry.provider || "",
          model: entry.model,
          inputTokens: entry.inputTokens,
          outputTokens: entry.outputTokens,
          cacheReadTokens: entry.cacheReadTokens,
          cacheWriteTokens: entry.cacheWriteTokens,
          reasoningTokens: entry.reasoningTokens || 0,
          inputTokensIncludeCache: entry.inputTokensIncludeCache,
        });
      }
      summary.byModel[entry.model].aic += entry.deltaAIC;
      runningAIC = entry.reportedTotalAIC ?? runningAIC + entry.deltaAIC;
      // A reported cumulative total covers earlier requests with unknown pricing.
      if (entry.reportedTotalAIC !== null) summary.aiCreditsAvailable = true;
      entry.runningAIC = runningAIC;
    }

    summary.totalAIC = runningAIC;
    summary.aiCreditsSource = hasReportedAIC ? "awf_reported" : "recomputed";
    if (fallbackRecordCount > 0) {
      summary.aiCreditsWarnings.push(`${fallbackRecordCount} token usage record(s) had missing or invalid AWF-reported AI Credits fields; fallback accounting was used for the missing values.`);
    }
    const summedDeltaAIC = Object.values(summary.byModel).reduce((total, usage) => total + (usage.aic || 0), 0);
    if (summary.aiCreditsSource === "awf_reported" && Math.abs(summedDeltaAIC - summary.totalAIC) > 1e-6 * Math.max(1, Math.abs(summary.totalAIC))) {
      summary.aiCreditsWarnings.push("The AWF-reported cumulative AI Credits total differs from the sum of per-request credits; the cumulative total was preserved for reporting.");
    }
  }
  if (invalidCacheSemanticsCount > 0) {
    summary.aiCreditsWarnings.push(`${invalidCacheSemanticsCount} token usage record(s) had invalid input_tokens_include_cache values; legacy provider cache semantics were used.`);
  }

  return summary;
}

/**
 * Generates a markdown summary section for token usage data.
 * Renders one row per request in chronological order with per-request AI credits,
 * a running AI credits total, followed by an aggregate totals row and legend.
 * @param {ReturnType<typeof parseTokenUsageJsonl>} summary
 * @returns {string} Markdown section, or empty string if no data
 */
function generateTokenUsageSummary(summary) {
  if (!summary || summary.totalRequests === 0) return "";

  const lines = [];
  lines.push("| # | Alias | Input | Output | Cache Read | Cache Write | ΔAI Credits | AI Credits | Duration |");
  lines.push("|--:|-------|------:|-------:|-----------:|------------:|-------------:|-----------:|---------:|");

  const entries = summary.entries || [];
  let compoundedAIC = 0;
  const formatSummaryAIC = value => formatAICForTable(value, summary.aiCreditsSource);
  for (let i = 0; i < entries.length; i++) {
    const entry = entries[i];
    const deltaAIC = entry.deltaAIC || 0;
    compoundedAIC += deltaAIC;
    const runningAIC = summary.aiCreditsSource === "awf_reported" ? entry.runningAIC : compoundedAIC;
    lines.push(
      `| ${i + 1} | ${formatModelEmojiAlias(entry.model) || entry.model} | ${entry.inputTokens.toLocaleString()} | ${entry.outputTokens.toLocaleString()} | ${entry.cacheReadTokens.toLocaleString()} | ${entry.cacheWriteTokens.toLocaleString()} | ${formatSummaryAIC(deltaAIC)} | ${formatSummaryAIC(runningAIC)} | ${formatDurationMs(entry.durationMs)} |`
    );
  }

  const totalAIC = formatSummaryAIC(summary.totalAIC || 0);
  lines.push(
    `| **Total** | | **${summary.totalInputTokens.toLocaleString()}** | **${summary.totalOutputTokens.toLocaleString()}** | **${summary.totalCacheReadTokens.toLocaleString()}** | **${summary.totalCacheWriteTokens.toLocaleString()}** | | **${totalAIC}** | **${formatDurationMs(summary.totalDurationMs)}** |`
  );

  lines.push("");
  const accountingDescription =
    summary.aiCreditsSource === "awf_reported"
      ? summary.aiCreditsWarnings.length > 0
        ? "mirrored from AWF fields where available, with warned fallback accounting"
        : "mirrored from AWF fields for reporting"
      : "recomputed with the current AI credits pricing model for legacy records";
  lines.push(`Legend: \`Alias\` shows the model shorthand used in the table. \`ΔAI Credits\` is the per-request cost, and \`AI Credits\` is the running total ${accountingDescription}.`);
  for (const warning of summary.aiCreditsWarnings) {
    lines.push(`Warning: ${warning}`);
  }
  lines.push("");

  return lines.join("\n") + "\n";
}

/**
 * Bound oversized sections without leaving partial Markdown/HTML structures open.
 * The complete redacted logs remain in the existing agent artifact.
 * @param {string} content
 * @param {number} maxBytes
 * @returns {string}
 */
function boundGatewaySummary(content, maxBytes) {
  if (Buffer.byteLength(content, "utf8") <= maxBytes) return content;
  const notice = `MCP gateway summary truncated (${Buffer.byteLength(content, "utf8")} bytes). Full redacted logs: \`mcp-logs/\` in the agent artifact.\n\n`;
  const header = `${notice}<details>\n<summary>MCP gateway diagnostic preview</summary>\n<pre>\n`;
  const footer = "\n</pre>\n</details>\n";
  const budget = maxBytes - Buffer.byteLength(header + footer, "utf8");
  if (budget < 128) return Buffer.byteLength(notice, "utf8") <= maxBytes ? notice : "";
  const lines = content.split("\n").map(line => escapeHtml(line));
  return header + boundSummaryLines(lines, budget, 4096).join("\n") + footer;
}

/**
 * @param {typeof import('@actions/core')} coreObj
 * @param {string[]} sections
 */
function appendGatewaySummary(coreObj, sections) {
  const summaryPath = process.env.GITHUB_STEP_SUMMARY;
  let remaining = MAX_STEP_SUMMARY_SIZE;
  try {
    if (summaryPath && fs.existsSync(summaryPath)) remaining -= fs.statSync(summaryPath).size;
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to measure existing MCP gateway step summary: ${getErrorMessage(error)}`, { cause: error });
  }
  for (const [index, section] of sections.entries()) {
    // Share the remaining capacity so a verbose gateway log cannot crowd out
    // later rate-limit, steering, or DIFC diagnostics.
    const budget = Math.max(0, Math.floor(remaining / (sections.length - index)));
    const bounded = boundGatewaySummary(section, budget);
    if (bounded !== section) coreObj.warning("MCP gateway step summary byte limit reached; full redacted logs are in the agent artifact.");
    if (bounded) coreObj.summary.addRaw(bounded);
    remaining -= Buffer.byteLength(bounded, "utf8");
  }
}

/**
 * Writes bounded gateway diagnostics and exports AI credit metadata.
 * Token Usage rendering is handled by parse_token_usage.cjs to avoid duplicate sections.
 * @param {typeof import('@actions/core')} coreObj - The GitHub Actions core object
 * @param {string[]} [sections] - Gateway sections sharing this step's byte budget
 */
async function writeStepSummaryWithTokenUsage(coreObj, sections = []) {
  appendGatewaySummary(coreObj, sections);
  if (!fs.existsSync(TOKEN_USAGE_PATH)) {
    coreObj.debug(`No token-usage.jsonl found at: ${TOKEN_USAGE_PATH}`);
  } else {
    let content;
    try {
      content = fs.readFileSync(TOKEN_USAGE_PATH, "utf8");
    } catch (err) {
      throw new Error(`${ERR_SYSTEM}: Failed to read file ${TOKEN_USAGE_PATH}: ${getErrorMessage(err)}`, { cause: err });
    }
    if (content?.trim()) {
      coreObj.info(`Found token-usage.jsonl (${content.length} bytes)`);
      const parsedSummary = parseTokenUsageJsonl(content);
      for (const warning of parsedSummary?.aiCreditsWarnings || []) {
        coreObj.warning?.(`[ai-credits] ${warning}`);
      }
      if (parsedSummary?.aiCreditsAvailable && (parsedSummary.aiCreditsSource === "awf_reported" || parsedSummary.totalAIC > 0)) {
        const aic = formatAICForOutput(parsedSummary.totalAIC, parsedSummary.aiCreditsSource);
        coreObj.exportVariable("GH_AW_AIC", aic);
        coreObj.setOutput("aic", aic);
        coreObj.info(`AI Credits: ${aic}`);
      }
      if (parsedSummary && typeof parsedSummary.ambientContextTokens === "number" && parsedSummary.ambientContextTokens > 0) {
        const roundedAmbientContext = String(Math.round(parsedSummary.ambientContextTokens));
        coreObj.exportVariable("GH_AW_AMBIENT_CONTEXT", roundedAmbientContext);
        coreObj.setOutput("ambient_context", roundedAmbientContext);
        coreObj.info(`Ambient context: ${roundedAmbientContext}`);
      }
    }
  }

  await coreObj.summary.write();
}

/**
 * Detects AI credit budget/rate-limit failures from gateway-related logs.
 * @param {string[]} contents
 * @returns {boolean}
 */
function hasAICreditsRateLimitError(contents) {
  return contents.filter(Boolean).some(content => content.split(/\r?\n/).some(line => !/\btool[_\s-]*result\b/i.test(line) && AI_CREDITS_RATE_LIMIT_PATTERNS.some(pattern => pattern.test(line))));
}

/**
 * Exports ai_credits_rate_limit_error output.
 * @param {typeof import('@actions/core')} coreObj
 * @param {boolean} value
 */
function setAICreditsRateLimitOutput(coreObj, value) {
  const strValue = value ? "true" : "false";
  coreObj.setOutput("ai_credits_rate_limit_error", strValue);
}

/**
 * Detects `unknown_model_ai_credits` errors from gateway log text content.
 * Also checks the firewall audit log via ai_credits_context.
 * @param {string[]} contents
 * @returns {boolean}
 */
function hasUnknownModelAICreditsError(contents) {
  const joined = contents.filter(Boolean).join("\n");
  if (joined && UNKNOWN_MODEL_AI_CREDITS_PATTERNS.some(pattern => pattern.test(joined))) return true;
  return parseUnknownModelAICreditsFromAuditLog();
}

/**
 * Exports unknown_model_ai_credits output.
 * @param {typeof import('@actions/core')} coreObj
 * @param {boolean} value
 */
function setUnknownModelAICreditsOutput(coreObj, value) {
  coreObj.setOutput("unknown_model_ai_credits", value ? "true" : "false");
}

/**
 * Prints all gateway-related files to core.info for debugging
 */
function printAllGatewayFiles() {
  const gatewayDirs = ["/tmp/gh-aw/mcp-logs"];
  displayDirectories(gatewayDirs, 64 * 1024);
}

/**
 * Parses gateway.jsonl content and extracts DIFC_FILTERED events
 * @param {string} jsonlContent - The gateway.jsonl file content
 * @returns {Array<Object>} Array of DIFC_FILTERED event objects
 */
function parseGatewayJsonlForDifcFiltered(jsonlContent) {
  const filteredEvents = [];
  for (const entry of iterateJsonl(jsonlContent, { lineFilter: line => /difc_filtered/i.test(line) })) {
    try {
      if (getRpcMessageType(entry) === "DIFC_FILTERED") {
        filteredEvents.push(entry);
      }
    } catch {
      // Malformed line — ignored.
    }
  }
  return filteredEvents;
}

/**
 * Parses gateway.jsonl content and extracts token steering events emitted by
 * the AWF API proxy.
 * @param {string} jsonlContent
 * @returns {Array<Object>}
 */
function parseGatewayJsonlForTokenSteering(jsonlContent) {
  const steeringEvents = [];
  for (const entry of iterateJsonl(jsonlContent, { lineFilter: line => line.includes("token_steering") })) {
    try {
      const eventName = getGatewayEventName(entry);
      if (eventName === "token_steering") {
        steeringEvents.push(entry);
      }
    } catch {
      // Malformed line — ignored.
    }
  }
  return steeringEvents;
}

/**
 * Resolve a normalized event/type name from a gateway JSONL entry.
 * @param {Object} entry
 * @returns {string}
 */
function getGatewayEventName(entry) {
  return typeof entry?.event === "string" ? entry.event : typeof entry?.type === "string" ? entry.type : "";
}

const MODEL_ALIAS_EVENT_NAMES = new Set(["model_alias_resolution", "model_rewrite", "MODEL_ALIAS_REWRITE"]);

/**
 * Parses gateway.jsonl content and extracts model alias resolution events emitted by
 * the AWF API proxy.
 * @param {string} jsonlContent
 * @returns {Array<Object>}
 */
function parseGatewayJsonlForModelAliasResolution(jsonlContent) {
  const aliasResolutionEvents = [];
  for (const entry of iterateJsonl(jsonlContent, { lineFilter: line => line.includes("model_alias") || line.includes("model_rewrite") || line.includes("MODEL_ALIAS") })) {
    try {
      const eventName = getGatewayEventName(entry);
      if (MODEL_ALIAS_EVENT_NAMES.has(eventName)) {
        aliasResolutionEvents.push(entry);
      }
    } catch {
      // Malformed line — ignored.
    }
  }
  return aliasResolutionEvents;
}

/**
 * Generates a markdown summary section for gateway token steering events.
 * @param {Array<Object>} steeringEvents
 * @returns {string}
 */
function generateTokenSteeringSummary(steeringEvents) {
  if (!steeringEvents || steeringEvents.length === 0) return "";

  const lines = [];
  lines.push("<details>");
  lines.push(`<summary>⚠️ Token Steering Events (${steeringEvents.length})</summary>\n`);
  lines.push("");
  lines.push("The firewall API proxy injected AI credit budget warnings into upstream requests.\n");
  lines.push("");
  lines.push("| Time | Provider | Request ID | Message |");
  lines.push("|------|----------|------------|---------|");

  for (const event of steeringEvents) {
    lines.push(buildRpcSummaryRow([formatRpcMessageTime(event.timestamp), event.provider || "-", event.request_id || "-", event.message || "-"]));
  }

  lines.push("");
  lines.push("</details>\n");
  return lines.join("\n");
}

/**
 * Generates a markdown summary section for model alias resolution events.
 * Includes a compact table plus a full raw JSON payload for complete inspection.
 * @param {Array<Object>} aliasResolutionEvents
 * @returns {string}
 */
function generateModelAliasResolutionSummary(aliasResolutionEvents) {
  if (!aliasResolutionEvents || aliasResolutionEvents.length === 0) return "";

  const lines = [];
  lines.push("<details>");
  lines.push(`<summary>🧭 Model Alias Resolution Events (${aliasResolutionEvents.length})</summary>\n`);
  lines.push("");
  lines.push("Model alias requests captured by the firewall API proxy.");
  lines.push("");
  lines.push("| Time | Provider | Request ID | Alias | Resolved model |");
  lines.push("|------|----------|------------|-------|----------------|");
  for (const event of aliasResolutionEvents) {
    // AWF has evolved the model alias event schema over time; support the known
    // snake_case/camelCase and token-diag data payload variants emitted by gateway/rpc JSONL streams.
    const data = event.data && typeof event.data === "object" && !Array.isArray(event.data) ? event.data : null;
    const provider = event.provider || data?.provider || event.resolved_provider || event.target_provider || "-";
    const requestId = event.request_id || data?.request_id || event.requestId || data?.requestId || "-";
    const alias = event.alias || event.model_alias || data?.original_model || event.requested_alias || event.requested_model || event.requestedModel || "-";
    const resolvedModel = event.resolved_model || data?.resolved_model || event.resolvedModel || data?.resolvedModel || event.model || event.selected_model || event.selectedModel || "-";
    lines.push(buildRpcSummaryRow([formatRpcMessageTime(event.timestamp), provider, requestId, alias, resolvedModel]));
  }
  lines.push("");
  lines.push("Raw events");
  lines.push("```json");
  lines.push(JSON.stringify(aliasResolutionEvents, null, 2));
  lines.push("```");
  lines.push("");
  lines.push("</details>\n");
  return lines.join("\n");
}

/**
 * Generates a markdown summary section for DIFC_FILTERED events
 * @param {Array<Object>} filteredEvents - Array of DIFC_FILTERED event objects
 * @returns {string} Markdown section, or empty string if no events
 */
function generateDifcFilteredSummary(filteredEvents) {
  if (!filteredEvents || filteredEvents.length === 0) return "";

  const lines = [];
  lines.push("<details>");
  lines.push(`<summary>🔒 DIFC Filtered Events (${filteredEvents.length})</summary>\n`);
  lines.push("");
  lines.push("The following tool calls were blocked by DIFC integrity or secrecy checks:\n");
  lines.push("");
  lines.push("| Time | Server | Tool | Reason | User | Resource |");
  lines.push("|------|--------|------|--------|------|----------|");

  for (const event of filteredEvents) {
    const time = event.timestamp ? event.timestamp.replace("T", " ").replace(/\.\d+Z$/, "Z") : "-";
    const server = event.server_id || "-";
    const tool = event.tool_name ? `\`${event.tool_name}\`` : "-";
    const reason = (event.reason || "-").replace(/\n/g, " ").replace(/\|/g, "\\|");
    const user = event.author_login ? `${event.author_login} (${event.author_association || "NONE"})` : "-";
    let resource;
    if (event.html_url) {
      const lastSegment = event.html_url.split("/").filter(Boolean).pop();
      const label = event.number ? `#${event.number}` : lastSegment || event.html_url;
      resource = `[${label}](${event.html_url})`;
    } else {
      const rawDesc = event.description ? event.description.replace(/^[a-z-]+:(?!\/\/)/i, "") : null;
      resource = rawDesc && rawDesc !== "#unknown" ? event.description : "-";
    }
    lines.push(`| ${time} | ${server} | ${tool} | ${reason} | ${user} | ${resource} |`);
  }

  lines.push("");
  lines.push("</details>\n");
  return lines.join("\n");
}

/**
 * Parses rpc-messages.jsonl content and returns entries categorized by type.
 * DIFC_FILTERED entries are excluded here because they are handled separately
 * by parseGatewayJsonlForDifcFiltered.
 * @param {string} jsonlContent - The rpc-messages.jsonl file content
 * @returns {{requests: Array<Object>, responses: Array<Object>, other: Array<Object>}}
 */
function parseRpcMessagesJsonl(jsonlContent) {
  const requests = [];
  const responses = [];
  const other = [];

  for (const entry of iterateJsonl(jsonlContent)) {
    try {
      if (!entry || typeof entry !== "object") continue;
      const messageType = getRpcMessageType(entry);
      if (!messageType) continue;
      // Normalize entry.type so downstream consumers that read entry.type directly
      // (e.g. grouping "other" entries by type) keep working regardless of whether
      // the source used the legacy "type" field or the "event" field. Only set it
      // when absent so we never overwrite an entry's own explicit "type" value.
      if (!entry.type) entry.type = messageType;

      if (messageType === "REQUEST") {
        requests.push(entry);
      } else if (messageType === "RESPONSE") {
        responses.push(entry);
      } else if (messageType !== "DIFC_FILTERED") {
        other.push(entry);
      }
    } catch {
      // Malformed line — ignored.
    }
  }

  return { requests, responses, other };
}

/**
 * Extracts a human-readable label for an MCP REQUEST entry.
 * For tools/call requests, returns the tool name; for other methods, returns the method name.
 * @param {Object} entry - REQUEST entry from rpc-messages.jsonl
 * @returns {string} Display label for the request
 */
function getRpcRequestLabel(entry) {
  const payload = entry.payload;
  if (!payload) return "unknown";
  const method = payload.method;
  if (method === "tools/call") {
    const toolName = payload.params && payload.params.name;
    return toolName || method;
  }
  return method || "unknown";
}

/**
 * Formats an rpc-messages timestamp for display in the step summary.
 * @param {string|undefined} timestamp
 * @returns {string}
 */
function formatRpcMessageTime(timestamp) {
  return timestamp ? timestamp.replace("T", " ").replace(/\.\d+Z$/, "Z") : "-";
}

/**
 * Escapes text for safe display inside a markdown table cell.
 * @param {unknown} value
 * @returns {string}
 */
function escapeMarkdownTableCell(value) {
  return String(value ?? "-")
    .replace(/\n/g, " ")
    .replace(/\|/g, "\\|")
    .trim();
}

/**
 * Escapes text for safe use in HTML fragments embedded in markdown.
 * @param {unknown} value
 * @returns {string}
 */
function escapeHtml(value) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/**
 * Truncates a string to a maximum length, appending an ellipsis when needed.
 * @param {unknown} value
 * @param {number} maxLength
 * @returns {string}
 */
function truncateSummaryValue(value, maxLength) {
  const text = String(value);
  if (maxLength <= 0) return "";
  if (text.length <= maxLength) return text;
  if (maxLength < 4) return text.slice(0, maxLength);
  return `${text.slice(0, maxLength - 3)}...`;
}

/**
 * Normalizes an RPC summary label sourced from logs.
 * @param {unknown} value
 * @param {number} maxLength
 * @returns {string}
 */
function normalizeRpcSummaryLabel(value, maxLength = MAX_RPC_MESSAGE_LABEL_LENGTH) {
  return truncateSummaryValue(
    String(value ?? "-")
      .replace(/\s+/g, " ")
      .trim() || "-",
    maxLength
  );
}

/**
 * Formats an RPC label as HTML code for safe use inside markdown tables.
 * @param {unknown} value
 * @returns {string}
 */
function formatRpcInlineCodeLabel(value) {
  return `<code>${escapeHtml(normalizeRpcSummaryLabel(value))}</code>`;
}

/**
 * Surface failed MCP calls separately from successful content that quotes 429s.
 * @param {string} jsonlContent
 * @returns {string}
 */
function generateMcpRateLimitSummary(jsonlContent) {
  const { requests, responses, other } = parseRpcMessagesJsonl(jsonlContent);
  /** @param {Object} entry */
  const requestKey = entry => JSON.stringify([entry.server_id, entry.payload?.id]);
  const tools = new Map(requests.filter(entry => entry.payload?.method === "tools/call" && entry.payload?.id != null).map(entry => [requestKey(entry), getRpcRequestLabel(entry)]));
  const rows = [];
  for (const response of responses) {
    const hint = getRpcRateLimitHint(response);
    if (hint !== null) {
      rows.push(buildRpcSummaryRow([formatRpcInlineCodeLabel(response.server_id), formatRpcInlineCodeLabel(tools.get(requestKey(response)) || "unknown"), hint]));
    }
  }
  for (const entry of other) {
    if (entry.type !== "tool_call" || typeof entry.error !== "string") continue;
    const hint = getMcpRateLimitHint(entry.error);
    if (hint !== null) rows.push(buildRpcSummaryRow([formatRpcInlineCodeLabel(entry.server_name), formatRpcInlineCodeLabel(entry.tool_name), hint]));
  }
  if (rows.length === 0) return "";
  return [
    "<details>",
    "<summary>MCP rate limits (HTTP 429)</summary>",
    "",
    "| Server | Tool | Retry hint |",
    "|--------|------|------------|",
    ...new Set(rows),
    "",
    "Retry hints are reported by the server, not a guarantee that the next request will succeed.",
    "",
    "</details>",
    "",
  ].join("\n");
}

/**
 * Summarizes an MCP RESPONSE entry for table rendering.
 * @param {Object} entry
 * @returns {{status: string, details: string}}
 */
function summarizeRpcResponseEntry(entry) {
  const rateLimitHint = getRpcRateLimitHint(entry);
  if (rateLimitHint !== null) return { status: "error", details: `HTTP 429: ${rateLimitHint}` };
  const payload = entry.payload && typeof entry.payload === "object" ? entry.payload : {};
  const error = payload.error && typeof payload.error === "object" ? payload.error : null;
  if (error) {
    const code = error.code !== null && error.code !== undefined ? ` ${error.code}` : "";
    const message = truncateSummaryValue(String(error.message || "Unknown error"), MAX_RPC_SUMMARY_DETAILS_LENGTH);
    return {
      status: "error",
      details: `error${code}: ${message}`,
    };
  }

  const result = payload.result;
  if (result && typeof result === "object") {
    if (result.isError === true) return { status: "error", details: "Tool returned an error" };
    if (Array.isArray(result.tools)) {
      return {
        status: "ok",
        details: `${result.tools.length} tool${result.tools.length !== 1 ? "s" : ""}`,
      };
    }

    const keys = Object.keys(result);
    if (keys.length > 0) {
      const shownKeys = keys.slice(0, 3);
      const moreCount = keys.length - shownKeys.length;
      return {
        status: "ok",
        details: `result keys: ${shownKeys.join(", ")}${moreCount > 0 ? ` +${moreCount} more` : ""}`,
      };
    }
  }

  if (result !== undefined) {
    return {
      status: "ok",
      details: truncateSummaryValue(JSON.stringify(result), MAX_RPC_SUMMARY_DETAILS_LENGTH),
    };
  }

  return {
    status: "ok",
    details: "response received",
  };
}

/**
 * Summarizes a non-REQUEST rpc-messages entry for table rendering.
 * @param {Object} entry
 * @returns {string}
 */
function summarizeGenericRpcEntry(entry) {
  const parts = [];
  const pushPart = (key, value) => {
    parts.push(`${key}=${truncateSummaryValue(String(value), MAX_RPC_SUMMARY_GENERIC_LENGTH)}`);
  };

  for (const [key, value] of Object.entries(entry)) {
    if (TOP_LEVEL_RPC_IGNORED_KEYS.has(key) || value === null || value === undefined || typeof value === "object") continue;
    pushPart(key, value);
  }

  const payload = entry.payload && typeof entry.payload === "object" ? entry.payload : null;
  if (payload) {
    if (payload.method) {
      pushPart("method", payload.method);
    }
    if (payload.params && typeof payload.params === "object" && payload.params.name) {
      pushPart("tool", payload.params.name);
    }
    if (payload.id !== null && payload.id !== undefined) {
      pushPart("id", payload.id);
    }
    if (payload.error && typeof payload.error === "object" && payload.error.message) {
      pushPart("error", payload.error.message);
    }
    if (parts.length === 0) {
      const payloadKeys = Object.keys(payload);
      if (payloadKeys.length > 0) {
        pushPart("payload keys", payloadKeys.join(", "));
      }
    }
  }

  if (parts.length === 0) {
    return "-";
  }

  return truncateSummaryValue(parts.join(" · "), MAX_RPC_SUMMARY_GENERIC_LENGTH);
}

/**
 * Builds a markdown table row for the RPC message summary.
 * @param {Array<unknown>} cells
 * @returns {string}
 */
function buildRpcSummaryRow(cells) {
  return `| ${cells.map(cell => escapeMarkdownTableCell(cell)).join(" | ")} |`;
}

/**
 * Generates a markdown step summary for rpc-messages.jsonl entries (mcpg v0.2.0+ format).
 * Shows a table of REQUEST entries (tool calls), a count of RESPONSE entries, any other
 * message types, and the DIFC_FILTERED section if there are blocked events.
 * @param {{requests: Array<Object>, responses: Array<Object>, other: Array<Object>}} entries
 * @param {Array<Object>} difcFilteredEvents - DIFC_FILTERED events parsed separately
 * @returns {string} Markdown summary, or empty string if nothing to show
 */
function generateRpcMessagesSummary(entries, difcFilteredEvents) {
  const { requests, responses, other } = entries;
  const blockedCount = difcFilteredEvents ? difcFilteredEvents.length : 0;
  const totalMessages = requests.length + responses.length + other.length + blockedCount;

  if (totalMessages === 0) return "";

  const parts = [];
  /** @type {Map<string, Array<Object>>} */
  const otherByType = new Map();
  for (const entry of other) {
    const entriesForType = otherByType.get(entry.type) || [];
    entriesForType.push(entry);
    otherByType.set(entry.type, entriesForType);
  }
  const renderedOtherTypes = Array.from(otherByType.keys());

  if (requests.length === 0 && responses.length === 0 && other.length === 0 && blockedCount > 0) {
    // No requests, but there are DIFC_FILTERED events — add a minimal header
    parts.push(`<details>\n<summary>MCP Gateway Activity (${blockedCount} blocked)</summary>\n\n*All tool calls were blocked by the integrity filter.*\n\n</details>\n`);
  } else {
    const summaryParts = [];
    if (requests.length > 0) {
      summaryParts.push(`${requests.length} request${requests.length !== 1 ? "s" : ""}`);
    }
    if (responses.length > 0) {
      summaryParts.push(`${responses.length} response${responses.length !== 1 ? "s" : ""}`);
    }
    for (const type of renderedOtherTypes) {
      const count = otherByType.get(type)?.length || 0;
      summaryParts.push(`${count} ${escapeHtml(normalizeRpcSummaryLabel(type))}`);
    }
    if (blockedCount > 0) {
      summaryParts.push(`${blockedCount} blocked`);
    }

    const callLines = [];
    callLines.push("<details>");
    callLines.push(`<summary>MCP Gateway Activity (${summaryParts.join(", ")})</summary>\n`);
    callLines.push("");

    if (requests.length > 0) {
      callLines.push("#### REQUEST");
      callLines.push("");
      callLines.push("| Time | Server | Tool / Method |");
      callLines.push("|------|--------|---------------|");

      for (let i = 0; i < requests.length; i++) {
        const req = requests[i];
        const time = formatRpcMessageTime(req.timestamp);
        const server = escapeMarkdownTableCell(req.server_id || "-");
        const label = formatRpcInlineCodeLabel(getRpcRequestLabel(req));
        callLines.push(`| ${time} | ${server} | ${label} |`);
      }

      callLines.push("");
    }

    if (responses.length > 0) {
      callLines.push("#### RESPONSE");
      callLines.push("");
      callLines.push("| Time | Server | Direction | Status | Details |");
      callLines.push("|------|--------|-----------|--------|---------|");

      for (const response of responses) {
        const { status, details } = summarizeRpcResponseEntry(response);
        callLines.push(buildRpcSummaryRow([formatRpcMessageTime(response.timestamp), response.server_id || "-", response.direction || "-", status, details]));
      }

      callLines.push("");
    }

    for (const type of renderedOtherTypes) {
      callLines.push(`#### ${escapeHtml(normalizeRpcSummaryLabel(type))}`);
      callLines.push("");
      callLines.push("| Time | Server | Direction | Details |");
      callLines.push("|------|--------|-----------|---------|");

      for (const entry of otherByType.get(type) || []) {
        callLines.push(buildRpcSummaryRow([formatRpcMessageTime(entry.timestamp), entry.server_id || "-", entry.direction || "-", summarizeGenericRpcEntry(entry)]));
      }

      callLines.push("");
    }

    callLines.push("</details>\n");
    parts.push(callLines.join("\n"));
  }

  // DIFC_FILTERED section (re-uses existing table renderer)
  if (blockedCount > 0) {
    parts.push(generateDifcFilteredSummary(difcFilteredEvents));
  }

  return parts.join("\n");
}

/**
 * Main function to parse and display MCP gateway logs
 */
async function main() {
  try {
    const summarySections = [];
    // First, print all gateway-related files for debugging
    printAllGatewayFiles();

    const gatewayJsonlPath = "/tmp/gh-aw/mcp-logs/gateway.jsonl";
    const rpcMessagesPath = "/tmp/gh-aw/mcp-logs/rpc-messages.jsonl";
    const gatewayMdPath = "/tmp/gh-aw/mcp-logs/gateway.md";
    const gatewayLogPath = "/tmp/gh-aw/mcp-logs/gateway.log";
    const stderrLogPath = "/tmp/gh-aw/mcp-logs/stderr.log";
    let aiCreditsRateLimitError = false;
    let unknownModelAICredits = false;

    // Parse DIFC_FILTERED events from gateway.jsonl (preferred) or rpc-messages.jsonl (fallback).
    // Both files use the same JSONL format with DIFC_FILTERED entries interleaved.
    let difcFilteredEvents = [];
    let tokenSteeringEvents = [];
    let modelAliasResolutionEvents = [];
    /** @type {any} */
    let rpcMessagesContent = null;
    let gatewayJsonlContent = "";
    if (fs.existsSync(gatewayJsonlPath)) {
      const jsonlContent = fs.readFileSync(gatewayJsonlPath, "utf8");
      gatewayJsonlContent = jsonlContent;
      core.info(`Found gateway.jsonl (${jsonlContent.length} bytes)`);
      difcFilteredEvents = parseGatewayJsonlForDifcFiltered(jsonlContent);
      tokenSteeringEvents = parseGatewayJsonlForTokenSteering(jsonlContent);
      modelAliasResolutionEvents = parseGatewayJsonlForModelAliasResolution(jsonlContent);
      // Do NOT scan gateway.jsonl / rpc-messages.jsonl for AI credits rate limit errors.
      // These files contain full MCP tool call request/response payloads including arbitrary
      // repository data (branch names, commit messages, file contents) that can false-positively
      // match the rate-limit patterns. Real AI credits rate limit errors from the inference API
      // appear in gateway.log / stderr.log / gateway.md, not in MCP RPC message logs.
      if (difcFilteredEvents.length > 0) {
        core.info(`Found ${difcFilteredEvents.length} DIFC_FILTERED event(s) in gateway.jsonl`);
      }
      if (tokenSteeringEvents.length > 0) {
        core.info(`Found ${tokenSteeringEvents.length} token_steering event(s) in gateway.jsonl`);
      }
      if (modelAliasResolutionEvents.length > 0) {
        core.info(`Found ${modelAliasResolutionEvents.length} model alias event(s) in gateway.jsonl`);
      }
    } else if (fs.existsSync(rpcMessagesPath)) {
      rpcMessagesContent = fs.readFileSync(rpcMessagesPath, "utf8");
      core.info(`Found rpc-messages.jsonl (${rpcMessagesContent.length} bytes)`);
      if (rpcMessagesContent.length === 0) {
        core.warning("rpc-messages.jsonl is present but zero bytes; continuing without RPC summary");
      }
      difcFilteredEvents = parseGatewayJsonlForDifcFiltered(rpcMessagesContent);
      tokenSteeringEvents = parseGatewayJsonlForTokenSteering(rpcMessagesContent);
      modelAliasResolutionEvents = parseGatewayJsonlForModelAliasResolution(rpcMessagesContent);
      // Do NOT scan rpc-messages.jsonl for AI credits signals (same reason as gateway.jsonl above).
      if (difcFilteredEvents.length > 0) {
        core.info(`Found ${difcFilteredEvents.length} DIFC_FILTERED event(s) in rpc-messages.jsonl`);
      }
      if (tokenSteeringEvents.length > 0) {
        core.info(`Found ${tokenSteeringEvents.length} token_steering event(s) in rpc-messages.jsonl`);
      }
      if (modelAliasResolutionEvents.length > 0) {
        core.info(`Found ${modelAliasResolutionEvents.length} model alias event(s) in rpc-messages.jsonl`);
      }
    } else {
      core.info(`No gateway.jsonl or rpc-messages.jsonl found for steering or DIFC_FILTERED scanning`);
    }

    // RPC responses remain useful even when gateway.md or gateway.jsonl is present.
    if (rpcMessagesContent === null && fs.existsSync(rpcMessagesPath)) {
      rpcMessagesContent = fs.readFileSync(rpcMessagesPath, "utf8");
    }
    const rateLimitSummary = generateMcpRateLimitSummary(`${gatewayJsonlContent}\n${rpcMessagesContent || ""}`);
    if (rateLimitSummary) {
      core.warning("MCP tools returned HTTP 429; see the MCP rate limits summary for retry hints.");
      summarySections.push(rateLimitSummary);
    }

    // Always scan authoritative text logs for AI credits signals before selecting
    // which format to render in the step summary.
    let gatewayLogContent = "";
    let stderrLogContent = "";

    if (fs.existsSync(gatewayLogPath)) {
      gatewayLogContent = fs.readFileSync(gatewayLogPath, "utf8");
      core.info(`Found gateway.log (${gatewayLogContent.length} bytes)`);
      aiCreditsRateLimitError ||= hasAICreditsRateLimitError([gatewayLogContent]);
      unknownModelAICredits ||= hasUnknownModelAICreditsError([gatewayLogContent]);
    } else {
      core.info(`No gateway.log found at: ${gatewayLogPath}`);
    }

    if (fs.existsSync(stderrLogPath)) {
      stderrLogContent = fs.readFileSync(stderrLogPath, "utf8");
      core.info(`Found stderr.log (${stderrLogContent.length} bytes)`);
      aiCreditsRateLimitError ||= hasAICreditsRateLimitError([stderrLogContent]);
      unknownModelAICredits ||= hasUnknownModelAICreditsError([stderrLogContent]);
    } else {
      core.info(`No stderr.log found at: ${stderrLogPath}`);
    }

    // Try to read gateway.md if it exists (preferred for general gateway summary)
    if (fs.existsSync(gatewayMdPath)) {
      // MCPG pre-allocates a fixed-size header region in gateway.md that is never
      // populated, leaving leading null bytes (U+0000). GitHub renders U+0000 as
      // U+FFFD (replacement character), producing hundreds of garbled characters at
      // the top of the step summary. Strip all null bytes before using the content.
      const gatewayMdContent = fs.readFileSync(gatewayMdPath, "utf8").replace(/\x00/g, "");
      if (gatewayMdContent && gatewayMdContent.trim().length > 0) {
        core.info(`Found gateway.md (${gatewayMdContent.length} bytes)`);
        aiCreditsRateLimitError ||= hasAICreditsRateLimitError([gatewayMdContent]);
        unknownModelAICredits ||= hasUnknownModelAICreditsError([gatewayMdContent]);

        // Write the markdown directly to the step summary
        summarySections.push(gatewayMdContent.endsWith("\n") ? gatewayMdContent : gatewayMdContent + "\n");

        // Append any proxy-side steering or DIFC_FILTERED sections after the gateway summary
        if (tokenSteeringEvents.length > 0) {
          const steeringSummary = generateTokenSteeringSummary(tokenSteeringEvents);
          summarySections.push(steeringSummary);
        }

        if (modelAliasResolutionEvents.length > 0) {
          const modelAliasResolutionSummary = generateModelAliasResolutionSummary(modelAliasResolutionEvents);
          summarySections.push(modelAliasResolutionSummary);
        }

        if (difcFilteredEvents.length > 0) {
          const difcSummary = generateDifcFilteredSummary(difcFilteredEvents);
          summarySections.push(difcSummary);
        }

        setAICreditsRateLimitOutput(core, aiCreditsRateLimitError);
        setUnknownModelAICreditsOutput(core, unknownModelAICredits);
        await writeStepSummaryWithTokenUsage(core, summarySections);
        return;
      }
    } else {
      core.info(`No gateway.md found at: ${gatewayMdPath}, falling back to log files`);
    }

    // When no gateway.md exists, check if rpc-messages.jsonl is available (mcpg v0.2.0+ unified format).
    // In this format, all message types (REQUEST, RESPONSE, DIFC_FILTERED, etc.) are written to a
    // single rpc-messages.jsonl file instead of separate gateway.md / gateway.log streams.
    if (rpcMessagesContent !== null) {
      const rpcEntries = parseRpcMessagesJsonl(rpcMessagesContent);
      const totalMessages = rpcEntries.requests.length + rpcEntries.responses.length + rpcEntries.other.length;
      core.info(`rpc-messages.jsonl: ${rpcEntries.requests.length} request(s), ${rpcEntries.responses.length} response(s), ${rpcEntries.other.length} other, ${difcFilteredEvents.length} DIFC_FILTERED`);

      if (totalMessages > 0 || difcFilteredEvents.length > 0) {
        const rpcSummary = generateRpcMessagesSummary(rpcEntries, difcFilteredEvents);
        if (rpcSummary.length > 0) {
          summarySections.push(rpcSummary);
        }
        if (tokenSteeringEvents.length > 0) {
          summarySections.push(generateTokenSteeringSummary(tokenSteeringEvents));
        }
        if (modelAliasResolutionEvents.length > 0) {
          summarySections.push(generateModelAliasResolutionSummary(modelAliasResolutionEvents));
        }
      } else {
        core.info("rpc-messages.jsonl is present but contains no renderable messages");
      }
      setAICreditsRateLimitOutput(core, aiCreditsRateLimitError);
      setUnknownModelAICreditsOutput(core, unknownModelAICredits);
      await writeStepSummaryWithTokenUsage(core, summarySections);
      return;
    }

    // Fallback to legacy log files for summary rendering.
    // If no legacy log content and no DIFC events, check if token usage is available
    if (
      (!gatewayLogContent || gatewayLogContent.trim().length === 0) &&
      (!stderrLogContent || stderrLogContent.trim().length === 0) &&
      difcFilteredEvents.length === 0 &&
      tokenSteeringEvents.length === 0 &&
      modelAliasResolutionEvents.length === 0
    ) {
      core.info("MCP gateway log files are empty or missing");
      setAICreditsRateLimitOutput(core, aiCreditsRateLimitError);
      setUnknownModelAICreditsOutput(core, unknownModelAICredits);
      await writeStepSummaryWithTokenUsage(core, summarySections);
      return;
    }

    // Generate plain text summary for core.info
    if ((gatewayLogContent && gatewayLogContent.trim().length > 0) || (stderrLogContent && stderrLogContent.trim().length > 0)) {
      const plainTextSummary = generatePlainTextLegacySummary(gatewayLogContent, stderrLogContent);
      core.info(plainTextSummary);
    }

    // Keep supplemental diagnostics separate from potentially verbose legacy logs.
    const legacySummary = generateGatewayLogSummary(gatewayLogContent, stderrLogContent);
    const steeringSummary = generateTokenSteeringSummary(tokenSteeringEvents);
    const modelAliasResolutionSummary = generateModelAliasResolutionSummary(modelAliasResolutionEvents);
    const difcSummary = generateDifcFilteredSummary(difcFilteredEvents);
    summarySections.push(...[legacySummary, steeringSummary, modelAliasResolutionSummary, difcSummary].filter(s => s.length > 0));
    setAICreditsRateLimitOutput(core, aiCreditsRateLimitError);
    setUnknownModelAICreditsOutput(core, unknownModelAICredits);
    await writeStepSummaryWithTokenUsage(core, summarySections);
  } catch (error) {
    core.setFailed(`${ERR_PARSE}: ${getErrorMessage(error)}`);
  }
}

/**
 * Generates a plain text summary from gateway.md content for console output
 * @param {string} gatewayMdContent - The gateway.md markdown content
 * @returns {string} Plain text summary for console output
 */
function generatePlainTextGatewaySummary(gatewayMdContent) {
  const lines = [];

  // Header
  lines.push("=== MCP Gateway Logs ===");
  lines.push("");

  // Strip markdown formatting for plain text display
  const plainText = gatewayMdContent
    .replace(/<details>/g, "")
    .replace(/<\/details>/g, "")
    .replace(/<summary>(.*?)<\/summary>/g, "$1")
    .replace(/```[\s\S]*?```/g, match => {
      // Extract content from code blocks
      return match.replace(/```[a-z]*\n?/g, "").replace(/```$/g, "");
    })
    .replace(/\*\*(.*?)\*\*/g, "$1") // Remove bold
    .replace(/\*(.*?)\*/g, "$1") // Remove italic
    .replace(/`(.*?)`/g, "$1") // Remove inline code
    .replace(/\[([^\]]+)\]\([^)]+\)/g, "$1") // Remove links, keep text
    .replace(/^#+\s+/gm, "") // Remove heading markers
    .replace(/^\|-+.*-+\|$/gm, "") // Remove table separator lines
    .replace(/^\|/gm, "") // Remove leading pipe from table rows
    .replace(/\|$/gm, "") // Remove trailing pipe from table rows
    .replace(/\s*\|\s*/g, " ") // Replace remaining pipes with spaces
    .trim();

  lines.push(plainText);
  lines.push("");

  return lines.join("\n");
}

/**
 * Generates a plain text summary from legacy log files for console output
 * @param {string} gatewayLogContent - The gateway.log content
 * @param {string} stderrLogContent - The stderr.log content
 * @returns {string} Plain text summary for console output
 */
function generatePlainTextLegacySummary(gatewayLogContent, stderrLogContent) {
  const lines = [];

  // Header
  lines.push("=== MCP Gateway Logs ===");
  lines.push("");

  // Add gateway.log if it has content
  if (gatewayLogContent && gatewayLogContent.trim().length > 0) {
    lines.push("Gateway Log (gateway.log):");
    lines.push("");
    lines.push(gatewayLogContent.trim());
    lines.push("");
  }

  // Add stderr.log if it has content
  if (stderrLogContent && stderrLogContent.trim().length > 0) {
    lines.push("Gateway Log (stderr.log):");
    lines.push("");
    lines.push(stderrLogContent.trim());
    lines.push("");
  }

  return lines.join("\n");
}

/**
 * Generates a markdown summary of MCP gateway logs
 * @param {string} gatewayLogContent - The gateway.log content
 * @param {string} stderrLogContent - The stderr.log content
 * @returns {string} Markdown summary
 */
function generateGatewayLogSummary(gatewayLogContent, stderrLogContent) {
  const summary = [];

  // Add gateway.log if it has content
  if (gatewayLogContent && gatewayLogContent.trim().length > 0) {
    summary.push("<details>");
    summary.push("<summary>MCP Gateway Log (gateway.log)</summary>\n");
    summary.push("```");
    summary.push(gatewayLogContent.trim());
    summary.push("```");
    summary.push("\n</details>\n");
  }

  // Add stderr.log if it has content
  if (stderrLogContent && stderrLogContent.trim().length > 0) {
    summary.push("<details>");
    summary.push("<summary>MCP Gateway Log (stderr.log)</summary>\n");
    summary.push("```");
    summary.push(stderrLogContent.trim());
    summary.push("```");
    summary.push("\n</details>");
  }

  return summary.join("\n");
}

// Export for testing
if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    main,
    generateGatewayLogSummary,
    generatePlainTextGatewaySummary,
    generatePlainTextLegacySummary,
    parseGatewayJsonlForDifcFiltered,
    parseGatewayJsonlForTokenSteering,
    parseGatewayJsonlForModelAliasResolution,
    generateDifcFilteredSummary,
    generateTokenSteeringSummary,
    generateModelAliasResolutionSummary,
    parseRpcMessagesJsonl,
    getRpcMessageType,
    getRpcRequestLabel,
    getMcpRateLimitHint,
    getRpcRateLimitHint,
    generateRpcMessagesSummary,
    generateMcpRateLimitSummary,
    printAllGatewayFiles,
    parseTokenUsageJsonl,
    generateTokenUsageSummary,
    formatDurationMs,
    formatAICForOutput,
    writeStepSummaryWithTokenUsage,
    boundGatewaySummary,
    hasAICreditsRateLimitError,
    hasUnknownModelAICreditsError,
    setUnknownModelAICreditsOutput,
  };
}

// Run main if called directly
if (require.main === module) {
  main().catch(err => {
    console.error(err && err.stack ? err.stack : getErrorMessage(err));
    process.exitCode = 1;
  });
}
