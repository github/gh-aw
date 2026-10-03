// @ts-check

const fs = require("fs");
const { sessionOutputText } = require("./agent_session.cjs");
const { boundSummaryLines, escapeSummaryText, redactSessionForPublication } = require("./agent_session_render.cjs");
const { collectArtifactSecretValues, redactManifestValue } = require("./safe_output_manifest.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { normalizeUnifiedSessionEvent } = require("./unified_session_payload.cjs");

const RUNTIME_TYPES = new Set([
  "session.format",
  "session.sandbox",
  "session.collection",
  "session.collection_warning",
  "mcp.rpc.request",
  "mcp.rpc.response",
  "mcp.difc.filtered",
  "mcp.guard.blocked",
  "mcp.tool_call",
  "mcp.event",
  "firewall.http_access",
  "firewall.token_usage",
  "firewall.steering",
  "firewall.event",
  "safe_output.request",
  "safe_output.result",
  "safe_output.error",
  "experiment.state",
  "experiment.assignment",
  "grader.manifest",
  "grader.result",
  "eval.result",
  "usage.report",
  "execution.result",
  "detection.result",
  "workflow.info",
  "workflow.aw_info",
]);

/** @param {Array<any>} events @returns {boolean} */
function isUnifiedSessionTrace(events) {
  return Array.isArray(events) && events.some(event => event?.provenance?.component || RUNTIME_TYPES.has(event?.type));
}

/** @param {Array<any>} events */
function validateSessionFileHeader(events) {
  const header = events[0];
  if (header?.type !== "session.format" || header.provenance?.component !== "collector") throw new Error("Unified session file is missing its leading session.format header");
  if (header.data?.version !== 1) throw new Error(`Unsupported unified session file-format version: ${sessionOutputText(header.data?.version)}`);
}

/** @param {any} value @returns {string} */
function inline(value) {
  return sessionOutputText(value).replace(/[\u0000-\u001f\u007f]/g, " ");
}

/** @param {any} value @param {string[]} keys @returns {string} */
function fields(value, keys) {
  return keys
    .filter(key => value?.[key] !== undefined)
    .map(key => `${key}=${inline(value[key])}`)
    .join(" ");
}

/** @param {any} event @returns {string | undefined} */
function eventDetail(event) {
  const data = normalizeUnifiedSessionEvent({ ...event, data: event.data ?? {} }).data;
  switch (event.type) {
    case "session.format":
      return `version=${inline(data.version)}`;
    case "session.sandbox":
      return fields(data, ["engine", "engineVersion", "runtime", "firewallEnabled", "firewallType", "firewallVersion", "mcpGatewayVersion", "allowedDomains"]);
    case "session.init":
    case "session.start":
      return fields(data, ["sourceEngine", "model", "sessionId"]);
    case "user.message":
      return undefined;
    case "assistant.message":
    case "assistant.reasoning":
      return inline(data.content);
    case "tool.execution_start":
      return fields(data, ["toolName", "mcpServerName", "toolCallId"]) + " [started]";
    case "tool.execution_complete": {
      const outcome = data.success === false || data.error != null || data.is_error === true || data.isError === true ? "failed" : data.success === true ? "succeeded" : "outcome unknown";
      return `${fields(data, ["toolName", "mcpServerName", "toolCallId", "durationMs"])} [${outcome}]`;
    }
    case "session.result":
      return fields(data, ["numTurns", "durationMs", "totalCostUsd"]);
    case "mcp.rpc.request":
      return fields(data, ["serverName", "direction", "rpcId", "method", "toolName"]);
    case "mcp.rpc.response":
      return fields(data, ["serverName", "direction", "rpcId"]) + (data.error !== undefined ? ` [error] ${fields(data.error, ["code", "message"])}` : " [response observed]");
    case "mcp.difc.filtered":
    case "mcp.guard.blocked":
      return `[blocked/filtered] ${fields(data, ["serverName", "toolName", "reason"])}`;
    case "mcp.tool_call":
      return fields(data, ["serverName", "toolName", "durationMs", "status", "error"]);
    case "firewall.http_access":
      return fields(data, ["host", "method", "status", "decision"]);
    case "firewall.steering":
      return fields(data, ["event", "message", "reason"]);
    case "firewall.token_usage":
    case "usage.report":
      return fields(data, ["provider", "model", "aic", "totalAic", "premiumRequests", "durationMs"]) + " " + fields(data.usage, ["input_tokens", "output_tokens", "cache_read_input_tokens", "cache_creation_input_tokens"]);
    case "mcp.event":
    case "firewall.event":
      return fields(data, ["event", "level", "status"]);
    case "safe_output.request":
      return `${fields(data, ["type", "repo", "number"])} [requested, not executed]`;
    case "safe_output.result":
      return `${fields(data, ["type", "repo", "number", "provider", "identifier", "url"])} [execution recorded]`;
    case "safe_output.error":
      return `${fields(data, ["type", "status", "errorCode", "message", "error"])}${Array.isArray(data.errors) ? ` errors=${data.errors.length} [${data.errors.map(error => fields(error, ["type", "errorCode"])).join("; ")}]` : ""}`;
    case "experiment.assignment":
      return `assignments=${inline(data.assignments ?? data)}`;
    case "experiment.state":
      return fields(data, ["runId", "assignments", "counts"]);
    case "grader.manifest":
      return `graders=${Array.isArray(data.graders) ? data.graders.length : "unavailable"}`;
    case "grader.result":
      return (
        fields(data, ["id", "value", "score", "unit", "passed", "status"]) + (Array.isArray(data.results) ? ` results=[${data.results.map(result => fields(result, ["id", "value", "score", "unit", "passed", "status"])).join("; ")}]` : "")
      );
    case "eval.result":
      return fields(data, ["id", "answer", "model", "error"]);
    case "execution.result":
      return fields(data, ["outcome", "conclusion", "exitCode", "durationMs", "startedAt", "finishedAt"]);
    case "detection.result":
      return fields(data, ["jobResult", "conclusion", "promptInjection", "secretLeak", "maliciousPatch"]);
    case "workflow.info":
      return fields(data, ["engine", "model", "workflow", "repository", "runId"]);
    case "workflow.aw_info":
      return fields(data, ["engine_id", "model", "workflow_name", "repository", "run_id"]);
    case "session.collection_warning":
      return fields(data, ["path", "line", "code"]);
    case "session.collection":
      return `sources=${Array.isArray(data.sources) ? data.sources.length : "unavailable"} ${fields(data, ["warnings", "untimedEvents", "absentComponents"])}`;
    default:
      return "[extension payload omitted]";
  }
}

/**
 * The source-local order, not the merged wall clock, controls agent correlation.
 * @param {Array<any>} events
 * @returns {Array<{label: string, events: Array<any>}>}
 */
function scopedAgentSessions(events) {
  const groups = new Map();
  for (const event of events) {
    const source = event.provenance;
    if (source ? source.component !== "agent" : RUNTIME_TYPES.has(event.type)) continue;
    const label = source ? `${source.phase}/${source.path}` : "agent";
    if (!groups.has(label)) groups.set(label, []);
    groups.get(label).push(event);
  }
  return [...groups].map(([label, entries]) => ({
    label,
    events: entries.sort((left, right) => (left.provenance?.index ?? 0) - (right.provenance?.index ?? 0)).map(({ provenance, ...event }) => event),
  }));
}

/** @param {Array<any>} events @returns {Array<{label: string, events: Array<any>}>} */
function publicationAgentSessions(events) {
  const secrets = collectArtifactSecretValues();
  const privateCopy = redactSessionForPublication(events, value => {
    const redacted = redactManifestValue(value, secrets);
    if (typeof redacted !== "string") throw new Error("Expected redacted session string");
    return redacted;
  });
  return scopedAgentSessions(privateCopy);
}

/**
 * Known runtime fields are display projections, never raw request/response dumps.
 * @param {Array<any>} events
 * @param {{markdown: boolean, maxBytes: number, maxLineBytes: number, agentStatistics: (events: Array<any>) => string[]}} options
 * @returns {string}
 */
function renderUnifiedSession(events, { markdown, maxBytes, maxLineBytes, agentStatistics }) {
  const headers = events.filter(event => event?.type === "session.format" && event.provenance?.component === "collector");
  if (headers.length) validateSessionFileHeader(events);
  if (headers.length > 1) throw new Error("Unified session file contains multiple collector format headers");
  const redacted = redactManifestValue(events, collectArtifactSecretValues());
  if (!Array.isArray(redacted)) throw new Error("Expected unified session events");
  const lines = ["=== Unified session ==="];
  if (headers.length) lines.push(`File format version: ${headers[0].data.version}`);
  const counts = new Map();
  for (const event of redacted) {
    const component = event.provenance?.component ?? "agent";
    counts.set(component, (counts.get(component) ?? 0) + 1);
  }
  lines.push(`Records: ${redacted.length} (${[...counts].map(([name, count]) => `${inline(name)}=${count}`).join(", ")})`);
  for (const group of publicationAgentSessions(events)) {
    lines.push("", `Agent source: ${inline(group.label)}`, ...agentStatistics(group.events));
  }
  lines.push("", "Chronological trace (user prompts omitted; untimed observations follow):");
  for (const [index, event] of redacted.entries()) {
    const detail = eventDetail(event);
    if (detail === undefined) continue;
    const source = event.provenance;
    const component = source?.component ?? "agent";
    const phase = source?.phase ?? "agent";
    const observedTime = source?.timestampMs;
    const time = typeof observedTime === "number" && Number.isFinite(observedTime) && Math.abs(observedTime) <= 8640000000000000 ? new Date(observedTime).toISOString() : "untimed";
    lines.push(`[${index + 1}] ${event.type === "session.format" && component === "collector" ? "file metadata" : time} ${inline(component)}/${inline(phase)} ${inline(event.type)} ${detail.trim()}`);
  }
  const preamble = markdown ? "### Unified session\n\n<details><summary>Unified trace details</summary>\n\n" : "";
  const tail = markdown ? "\n\n</details>" : "";
  const bodyBudget = maxBytes - Buffer.byteLength(preamble + tail, "utf8") - 2 * (maxLineBytes + 1) - 3;
  if (bodyBudget < 128) return markdown ? "Unified session summary omitted: remaining step-summary byte limit reached.\n" : "Unified session summary omitted: byte limit reached.\n";
  const visible = boundSummaryLines(markdown ? lines.map(escapeSummaryText) : lines, bodyBudget, maxLineBytes);
  if (!markdown) return visible.join("\n").replace(/::/g, ": :");
  let longest = 2;
  for (const line of visible) for (const run of line.match(/`+/g) ?? []) longest = Math.max(longest, run.length);
  const fence = "`".repeat(longest + 1);
  return `${preamble}${fence}\n${visible.join("\n")}\n${fence}${tail}`;
}

/** @param {string} filePath @returns {Promise<void>} */
async function publishUnifiedSessionSummary(filePath) {
  let events;
  try {
    events = fs
      .readFileSync(filePath, "utf8")
      .split("\n")
      .filter(line => line.trim())
      .map(line => JSON.parse(line));
  } catch (error) {
    throw new Error(`Failed to read unified session for summary: ${getErrorMessage(error)}`, { cause: error });
  }
  validateSessionFileHeader(events);
  const { generatePlainTextSummary, generateCopilotCliStyleSummary } = require("./log_parser_shared.cjs");
  core.info(generatePlainTextSummary(events));
  if (process.env.GITHUB_STEP_SUMMARY) {
    try {
      const used = fs.existsSync(process.env.GITHUB_STEP_SUMMARY) ? fs.statSync(process.env.GITHUB_STEP_SUMMARY).size : 0;
      const { MAX_STEP_SUMMARY_SIZE } = require("./log_parser_shared.cjs");
      const available = MAX_STEP_SUMMARY_SIZE - used - 2;
      const markdown = generateCopilotCliStyleSummary(events, { maxBytes: available });
      if (Buffer.byteLength(markdown, "utf8") > available) {
        core.warning("Unified session step summary omitted: existing step-summary byte limit reached");
        return;
      }
      fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, `\n${markdown}\n`, "utf8");
    } catch (error) {
      throw new Error(`Failed to publish unified session step summary: ${getErrorMessage(error)}`, { cause: error });
    }
  } else if (core.summary?.addRaw) await core.summary.addRaw(generateCopilotCliStyleSummary(events)).write();
}

module.exports = { isUnifiedSessionTrace, validateSessionFileHeader, scopedAgentSessions, publicationAgentSessions, renderUnifiedSession, publishUnifiedSessionSummary };
