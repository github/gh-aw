// @ts-check

const { iterateJsonlEvents } = require("./jsonl_helpers.cjs");
const fs = require("fs");
const { sessionOutputText, sessionContext, sessionScopeKey, isNestedSessionEvent } = require("./agent_session.cjs");
const { collapseStreamedMessages, boundSummaryLines, escapeSummaryText, redactSessionForPublication } = require("./agent_session_render.cjs");
const { collectArtifactSecretValues, redactManifestValue } = require("./safe_output_manifest.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { normalizeUnifiedSessionEvent } = require("./unified_session_payload.cjs");
const { COPILOT_WORKFLOW_EVENT_FIELDS, COPILOT_WORKFLOW_EVENT_TYPES } = require("./copilot_workflow_events.cjs");
const { ERR_SYSTEM, ERR_VALIDATION } = require("./error_codes.cjs");
const { validateAgentExecution } = require("./agent_execution.cjs");
const { DYNAMIC_WORKFLOW_EVENT_TYPES } = require("./dynamic_workflow_session.cjs");

const RUNTIME_TYPES = new Set([
  "session.format",
  "agent.execution",
  "session.collection",
  "session.collection_warning",
  "prompt.system",
  "prompt.user",
  "mcp.rpc.request",
  "mcp.rpc.response",
  "mcp.difc.filtered",
  "mcp.guard.blocked",
  "mcp.tool_call",
  "mcp.event",
  "mcp.configuration",
  "firewall.http_access",
  "firewall.token_usage",
  "firewall.model_routing",
  "model.inventory",
  "model_routing.outcome",
  "model_endpoint.mismatch",
  "model.resolution",
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
  "workflow.job_result",
  "github_api.rate_limit",
  "guardrail.daily_aic",
  "guardrail.admission",
  "otel.span",
  "otel.span_event",
  "otel.log",
]);

/** @param {Array<any>} events @returns {boolean} */
function isUnifiedSessionTrace(events) {
  return Array.isArray(events) && events.some(event => event?.provenance?.component || RUNTIME_TYPES.has(event?.type));
}

/** @param {Array<any>} events */
function validateSessionFileHeader(events) {
  const header = events[0];
  if (header?.type !== "session.format" || header.provenance?.component !== "collector") throw new Error(`${ERR_VALIDATION}: Unified session file is missing its leading session.format header`);
  if (header.data?.version !== 1) throw new Error(`${ERR_VALIDATION}: Unsupported unified session file-format version: ${sessionOutputText(header.data?.version)}`);
  const executions = events.filter(event => event.type === "agent.execution");
  if (executions.length > 1) throw new Error(`${ERR_VALIDATION}: Unified session contains multiple agent.execution records`);
  for (const execution of executions) validateAgentExecution(execution.data);
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

/** @param {any} value @param {string[]} keys @returns {string} */
function scalarFields(value, keys) {
  return fields(
    value,
    keys.filter(key => ["string", "number", "boolean"].includes(typeof value?.[key]))
  );
}

/** @param {any} event @returns {string | undefined} */
function eventDetail(event) {
  /** @type {Record<string, any>} */
  const data = normalizeUnifiedSessionEvent({ ...event, data: event.data ?? {} }).data;
  if (COPILOT_WORKFLOW_EVENT_TYPES.has(event.type)) {
    return fields({ ...data, agentId: event.agentId }, ["agentId", ...Object.keys(COPILOT_WORKFLOW_EVENT_FIELDS[event.type])]);
  }
  if (Object.values(DYNAMIC_WORKFLOW_EVENT_TYPES).includes(event.type)) {
    if (event.type === DYNAMIC_WORKFLOW_EVENT_TYPES.background_tasks_changed) {
      return Array.isArray(data.tasks) ? `tasks=[${data.tasks.map(task => fields(task, ["taskId", "taskType", "workflowName", "status"])).join("; ")}]` : "tasks unavailable";
    }
    return (
      fields(data, ["taskId", "toolCallId", "taskType", "workflowName", "status"]) +
      (data.usage ? ` ${fields(data.usage, ["totalTokens", "toolUses", "durationMs"])}` : "") +
      (Array.isArray(data.workflowProgress) ? ` progress=[${data.workflowProgress.map(item => fields(item, ["type", "index", "phaseIndex", "agentId", "model", "state", "attempt"])).join("; ")}]` : "")
    );
  }
  switch (event.type) {
    case "workflow.job_result":
      return fields(data, ["jobId", "result", "durationMs"]);
    case "session.format":
      return `version=${inline(data.version)}`;
    case "session.init":
    case "session.start":
      return fields(data, ["sourceEngine", "model", "sessionId", "reasoningEffort", "cwd"]);
    case "session.error": {
      const error = data.error && typeof data.error === "object" ? scalarFields(data.error, ["code", "type", "message"]) : scalarFields(data, ["error"]);
      return `${scalarFields(data, ["severity", "status", "code", "errorType", "statusCode", "model", "message", "exitCode"])}${error ? ` ${error}` : ""}`;
    }
    case "session.info":
      return scalarFields(data, ["status", "model", "message", "sourceType", "kind", "isAuthenticating", "rateLimitInfo"]);
    case "session.shutdown":
      return fields(data, ["shutdownType", "errorReason", "currentModel", "premiumRequests", "totalApiDurationMs", "totalNanoAiu"]);
    case "session.task_complete":
      return fields(data, ["success"]);
    case "session.mode_changed":
      return fields(data, ["previousMode", "newMode"]);
    case "assistant.turn_start":
    case "assistant.turn_end":
    case "turn.started":
    case "turn.completed":
      return fields(data, ["turnId", "interactionId", "status"]);
    case "permission.requested":
      return fields(data, ["requestId", "agentMode", "permissionMode"]) + " " + fields(data.permissionRequest, ["kind", "toolCallId"]);
    case "permission.completed":
      return fields(data, ["requestId", "toolCallId", "decisionSource"]) + " " + fields(data.result, ["kind"]);
    case "skill.invoked":
      return fields(data, ["name", "path", "source", "trigger", "model", "invokedAtTurn"]);
    case "skill.inventory":
      return `installed skills=${Array.isArray(data.skills) ? data.skills.length : "unavailable"}`;
    case "skill.conflict":
      return fields(data, ["name", "kind", "sources"]);
    case "skill.context_delivered_ref":
      return fields(data, ["interactionId", "source", "contentId"]) + " [skill instructions omitted]";
    case "system.message":
      return "[system instructions omitted]";
    case "user.message":
    case "prompt.system":
    case "prompt.user":
      return undefined;
    case "assistant.message":
    case "assistant.reasoning":
      return inline(data.content);
    case "assistant.message_update":
      return (
        fields(data, ["sourceType", "type", "messageId", "contentIndex", "partial", "delta", "channel"]) +
        (data.usageSnapshot ? ` usage=${fields(data.usageSnapshot, ["inputTokens", "outputTokens", "cacheReadInputTokens", "cacheCreationInputTokens"])}` : "")
      );
    case "model.call_final_result":
      return (
        fields(data, ["model", "isByok", "agentId", "parentToolCallId"]) +
        (data.result ? ` ${fields(data.result, ["status", "stopReason"])} ${fields(data.result.usage, ["inputTokens", "outputTokens", "cacheReadInputTokens", "cacheCreationInputTokens"])}` : "")
      );
    case "assistant.refusal":
      return `[Policy refusal: ${inline(data.reason)}] ${fields(data, ["policyCategory", "partial", "content", "explanation"])}`;
    case "tool.execution_start":
      return fields(data, ["toolName", "mcpServerName", "toolCallId", "stepIndex"]) + " [started]";
    case "tool.execution_update":
      return fields(data, ["toolName", "mcpServerName", "toolCallId", "stepIndex", "taskId", "taskType", "status", "error", "partial"]) + " [updated]";
    case "tool.execution_complete": {
      const outcome = data.success === false || data.error != null || data.is_error === true || data.isError === true ? "failed" : data.success === true ? "succeeded" : "outcome unknown";
      if (data.workflowRunId !== undefined && data.status === "async_launched") {
        return `${fields(data, ["toolName", "toolCallId", "taskId", "workflowName", "workflowRunId"])} [launch ${outcome}; workflow outcome pending]`;
      }
      return `${fields(data, ["toolName", "mcpServerName", "toolCallId", "stepIndex", "durationMs"])} [${outcome}]`;
    }
    case "session.result":
      return fields(data, ["numTurns", "durationMs", "totalCostUsd", "status", "sourceType"]);
    case "agent.execution":
      return fields({ ...data, categories: JSON.stringify(data.categories), errorCodes: JSON.stringify(data.errorCodes), errorTypes: JSON.stringify(data.errorTypes) }, ["categories", "errorCodes", "errorTypes", "exitCode"]);
    case "mcp.rpc.request":
      return fields(data, ["serverName", "direction", "rpcId", "method", "toolName"]);
    case "mcp.rpc.response":
      return fields(data, ["serverName", "direction", "rpcId"]) + (data.error !== undefined ? ` [error] ${fields(data.error, ["code", "message"])}` : " [response observed]");
    case "mcp.difc.filtered":
    case "mcp.guard.blocked":
      return `[blocked/filtered] ${fields(data, ["serverName", "toolName", "reason"])}`;
    case "mcp.tool_call":
      return fields(data, ["serverName", "toolName", "durationMs", "status", "error"]);
    case "mcp.configuration":
      return `servers=[${data.servers.map(server => fields(server, ["serverName", "transport", "required", "tools"])).join("; ")}]`;
    case "firewall.http_access":
      return fields(data, ["host", "method", "status", "decision"]);
    case "firewall.steering":
      return fields(data, ["event", "threshold", "message", "reason"]);
    case "firewall.token_usage":
    case "usage.report":
      return (
        fields(data, ["provider", "model", "aic", "totalAic", "premiumRequests", "durationMs"]) +
        " " +
        fields(data.usage, ["inputTokens", "outputTokens", "cacheReadInputTokens", "cacheCreationInputTokens", "totalTokens", "reasoningOutputTokens", "overflowedTokens"])
      );
    case "firewall.model_routing":
      return fields(data, ["stage", "routed", "outcome", "selectedModel", "selectedEffort", "router"]);
    case "model_routing.outcome":
      return fields(data, ["status", "wireModel", "effectiveEndpoint", "selectedEndpoint", "effort", "appliedEffort", "failureCode"]);
    case "model.inventory":
      return fields(data, ["endpoints", "modelsFetchComplete"]);
    case "model_endpoint.mismatch":
      return `${fields(data, ["category", "phase", "configured_model", "resolved_model", "wire_api", "wire_api_source", "supported_endpoints", "model", "endpoint"])} cause=${inline(data.detail)} fix=${inline(data.fix)}`;
    case "model.resolution":
      return fields(data, ["engine", "role", "configured_model", "resolved_model", "provider", "status", "cause"]);
    case "mcp.event":
      return fields(data, ["event", "serverName", "level", "status", "message", "reason"]);
    case "firewall.event":
      return fields(data, ["event", "level", "status", "message", "reason"]);
    case "guardrail.admission":
      return fields(data, ["status"]) + " (allowance values available in session JSONL)";
    case "guardrail.daily_aic":
      return fields(data, ["status", "exceeded", "total", "estimated", "threshold"]);
    case "github_api.rate_limit":
      return fields(data, ["source", "operation", "resource", "status", "remaining", "limit", "attempt", "delayMs"]);
    case "otel.span":
    case "otel.span_event":
    case "otel.log":
      // OTLP bodies and attributes can contain prompts and tool payloads.
      return fields(data, ["name", "traceId", "spanId", "parentSpanId", "severityText", "severityNumber"]) + (data.status ? ` ${fields(data.status, ["code", "message"])}` : "");
    case "safe_output.request":
      return `${fields(data, ["type", "repo", "number"])} [requested, not executed]`;
    case "safe_output.result":
      return `${fields(data, ["type", "status", "repo", "number", "provider", "identifier", "url"])} [execution recorded]`;
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
      return fields(data, ["outcome", "conclusion", "exitCode", "durationMs"]);
    case "detection.result":
      return fields(data, ["jobResult", "conclusion", "reason", "promptInjection", "secretLeak", "maliciousPatch"]);
    case "workflow.info":
      return (
        fields(data, [
          "engineId",
          "model",
          "requestedModel",
          "modelRouting",
          "triggerType",
          "cliVersion",
          "awfVersion",
          "mcpgVersion",
          "agentVersion",
          "workflow",
          "workflowId",
          "workflowCallId",
          "callerWorkflowRef",
          "repository",
          "runId",
          "runAttempt",
          "ref",
          "sha",
          "actor",
          // @safe-outputs-exempt SEC-005: display-only session metadata; this module makes no cross-repo API writes.
          // Remove this exemption and validate the allowlist if this module gains cross-repo writes.
          "targetRepo",
        ]) +
        " " +
        fields(data.episode, ["episodeId", "hopId", "parentHopId", "originEvent", "rootRepo", "rootWorkflowId", "rootRunId", "parentRepo", "parentRunId", "parentRunAttempt", "parentWorkflowId", "parentWorkflowCallId", "parentEventType"]) +
        (data.awContext !== undefined ? ` awContext=${inline(JSON.stringify(data.awContext))}` : "")
      );
    case "session.collection_warning":
      return fields(data, ["path", "line", "code", "message"]);
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
    const context = sessionContext(event);
    const baseLabel = source ? `${source.phase}/${source.path}` : "agent";
    const nested = isNestedSessionEvent(event);
    const label = nested
      ? `${baseLabel} sessionId=${context.sessionId ?? "unavailable"}${context.parentToolUseId ? ` parentToolUseId=${context.parentToolUseId}` : ""}${context.agentId !== undefined ? ` agentId=${context.agentId}` : ""}`
      : baseLabel;
    const key = JSON.stringify([source?.phase, source?.path, nested ? sessionScopeKey(event) : undefined]);
    if (!groups.has(key)) groups.set(key, { label, events: [] });
    groups.get(key).events.push(event);
  }
  return [...groups.values()].map(({ label, events: entries }) => ({
    label,
    events: entries.sort((left, right) => (left.provenance?.index ?? 0) - (right.provenance?.index ?? 0)).map(({ provenance, ...event }) => event),
  }));
}

/** @param {Array<any>} events @returns {Array<{label: string, events: Array<any>}>} */
function publicationAgentSessions(events, secrets = collectArtifactSecretValues()) {
  /** @param {string} value @returns {string} */
  const redact = value => {
    const redacted = redactManifestValue(value, secrets);
    if (typeof redacted !== "string") throw new Error(`${ERR_VALIDATION}: Expected redacted session string`);
    return redacted;
  };
  return scopedAgentSessions(events).map(group => ({
    label: redact(group.label),
    events: redactSessionForPublication(group.events, redact),
  }));
}

/** @param {any} event @returns {{callKey: string | undefined, nameKey: string}} */
function timelineToolKeys(event) {
  const data = normalizeUnifiedSessionEvent({ ...event, data: event.data ?? {} }).data;
  const scope = [event.provenance?.component, event.provenance?.phase, event.provenance?.path, sessionScopeKey(event)];
  const correlation = typeof data.toolCallId === "string" ? ["id", data.toolCallId] : typeof data.stepIndex === "number" && Number.isSafeInteger(data.stepIndex) && data.stepIndex >= 0 ? ["step", data.stepIndex] : undefined;
  let name = typeof data.toolName === "string" && data.toolName.trim() ? data.toolName.trim() : "unknown";
  const server = typeof data.mcpServerName === "string" ? data.mcpServerName.trim() : "";
  if (!name.startsWith("mcp__")) name = server ? `mcp__${server}__${name}` : name.toLowerCase() === "bash" ? "Bash" : name;
  return { callKey: correlation === undefined ? undefined : JSON.stringify([...scope, correlation]), nameKey: JSON.stringify([...scope, name]) };
}

/**
 * Known runtime fields are display projections, never raw request/response dumps.
 * @param {Array<any>} events
 * @param {{markdown: boolean, maxBytes: number, maxLineBytes: number, agentStatistics: (events: Array<any>, secrets: string[]) => string[], agentConversation: (events: Array<any>, secrets: string[]) => string[]}} options
 * @returns {string}
 */
function renderUnifiedSession(events, { markdown, maxBytes, maxLineBytes, agentStatistics, agentConversation }) {
  const headers = events.filter(event => event?.type === "session.format" && event.provenance?.component === "collector");
  if (headers.length) validateSessionFileHeader(events);
  if (headers.length > 1) throw new Error(`${ERR_VALIDATION}: Unified session file contains multiple collector format headers`);
  const secrets = collectArtifactSecretValues();
  const redacted = redactManifestValue(events, secrets);
  if (!Array.isArray(redacted)) throw new Error(`${ERR_VALIDATION}: Expected unified session events`);
  const lines = markdown ? [] : ["=== Unified session ==="];
  if (headers.length) lines.push(`File format version: ${headers[0].data.version}`);
  const counts = new Map();
  for (const event of redacted) {
    const component = event.provenance?.component ?? "agent";
    counts.set(component, (counts.get(component) ?? 0) + 1);
  }
  lines.push(`Records: ${redacted.length} (${[...counts].map(([name, count]) => `${inline(name)}=${count}`).join(", ")})`);
  const agentSessions = publicationAgentSessions(events, secrets);
  for (const group of agentSessions) {
    lines.push("", `Agent source: ${inline(group.label)}`, ...agentStatistics(group.events, secrets));
  }
  const overview = [...lines];
  const traceLines = ["Chronological trace (user prompts omitted; untimed observations follow):"];
  const toolStarts = new Map();
  const toolStartsByName = new Map();
  const pairedStarts = new WeakMap();
  // Pair using private identities and source-local order, never the merged clock.
  for (const event of [...events].sort((left, right) => (left.provenance?.index ?? 0) - (right.provenance?.index ?? 0))) {
    if (!["tool.execution_start", "tool.execution_complete"].includes(event.type)) continue;
    const { callKey, nameKey } = timelineToolKeys(event);
    if (event.type === "tool.execution_start") {
      if (callKey !== undefined) toolStarts.set(callKey, event);
      if (!toolStartsByName.has(nameKey)) toolStartsByName.set(nameKey, new Set());
      toolStartsByName.get(nameKey).add(event);
    } else {
      const names = toolStartsByName.get(nameKey);
      const start = callKey !== undefined ? toolStarts.get(callKey) : names?.size === 1 ? [...names][0] : undefined;
      if (!start) continue;
      pairedStarts.set(event, start);
      const startKeys = timelineToolKeys(start);
      if (startKeys.callKey !== undefined && toolStarts.get(startKeys.callKey) === start) toolStarts.delete(startKeys.callKey);
      toolStartsByName.get(startKeys.nameKey)?.delete(start);
    }
  }
  const timelineIndex = Symbol("timelineIndex");
  const streamScopes = new Map();
  for (const [index, event] of events.entries()) {
    const source = event.provenance;
    const key = JSON.stringify([source?.component, source?.phase, source?.path, sessionScopeKey(event)]);
    if (!streamScopes.has(key)) streamScopes.set(key, []);
    streamScopes.get(key).push({ ...event, [timelineIndex]: index });
  }
  // Reconcile each private conversation before restoring merged timeline order.
  const displayEvents = [...streamScopes.values()]
    .flatMap(entries =>
      collapseStreamedMessages(
        entries.sort((left, right) => (left.provenance?.index ?? 0) - (right.provenance?.index ?? 0)),
        secrets
      )
    )
    .sort((left, right) => left[timelineIndex] - right[timelineIndex]);
  const trace = redactManifestValue(displayEvents, secrets);
  if (!Array.isArray(trace)) throw new Error(`${ERR_VALIDATION}: Expected unified session display events`);
  const displayEventsBySource = new Map(displayEvents.map((entry, index) => [events[entry[timelineIndex]], trace[index]]));
  let previousTime;
  for (const [index, event] of trace.entries()) {
    const detail = eventDetail(event);
    if (detail === undefined) continue;
    const source = event.provenance;
    const component = source?.component ?? "agent";
    const phase = source?.phase ?? "agent";
    const observedTime = source?.timestampMs;
    const metadata = event.type === "session.format" && component === "collector";
    let elapsed = metadata ? " [file metadata]" : " [untimed]";
    if (!metadata && typeof observedTime === "number" && Number.isFinite(observedTime) && Math.abs(observedTime) <= 8640000000000000) {
      const displayTime = Math.max(observedTime, previousTime ?? observedTime);
      const seconds = (displayTime - (previousTime ?? displayTime)) / 1000;
      elapsed = ` +${Number(seconds.toFixed(3))}s`;
      previousTime = displayTime;
    }
    const context = sessionContext(event);
    const identity = fields({ path: source?.path, ...context }, ["path", "agentId", "sessionId", "parentToolUseId"]);
    const conversational = ["assistant.message", "assistant.reasoning", "assistant.refusal", "tool.execution_start", "tool.execution_complete"].includes(event.type) && component === "agent";
    const message = ["assistant.message", "assistant.reasoning", "assistant.refusal"].includes(event.type);
    traceLines.push("", `[${index + 1}] ${inline(component)}/${inline(phase)} ${inline(event.type)}${message && conversational ? "" : ` ${detail.trim()}`}${elapsed}`);
    if (identity) traceLines.push(`  ${identity}`);
    if (conversational) {
      const original = displayEvents[index];
      const start = pairedStarts.get(events[original[timelineIndex]]);
      const entries = [...(start && displayEventsBySource.has(start) ? [displayEventsBySource.get(start)] : []), event].map(({ provenance, ...entry }) => entry);
      const content = agentConversation(entries, secrets).map(line => line.replace(/^\[\d+\] /, ""));
      traceLines.push(...content);
    }
  }
  lines.push("", ...traceLines);
  const preamble = markdown ? "<details><summary>Unified session</summary>\n\n" : "";
  const tail = markdown ? "\n\n</details>" : "";
  if (!markdown) return boundSummaryLines(lines, maxBytes, maxLineBytes).join("\n").replace(/::/g, ": :");
  const sections = [
    { title: "Session overview", collapsed: true, lines: overview },
    {
      title: "Conversation",
      collapsed: false,
      lines: publicationAgentSessions(displayEvents, secrets).flatMap(group => {
        const content = agentConversation(group.events, secrets);
        return content.length ? [`Agent source: ${inline(group.label)}`, "", ...content, ""] : [];
      }),
    },
    { title: "Full timeline", collapsed: true, lines: traceLines },
  ];
  const bounded = sections.map(section => ({
    ...section,
    lines: boundSummaryLines(section.lines.length ? section.lines : ["No agent conversation recorded."], Number.MAX_SAFE_INTEGER, maxLineBytes),
    notice: `... (${section.title} omitted: byte limit reached)`,
  }));
  let longest = 2;
  // Fenced code treats HTML as literal text; escaping here would print entities.
  for (const section of bounded) for (const line of section.lines) for (const run of line.match(/`+/g) ?? []) longest = Math.max(longest, run.length);
  const fence = "`".repeat(longest + 1);
  /** @param {{title: string, collapsed: boolean}} section @param {string[]} content */
  const block = (section, content) => {
    const transcript = `${fence}text\n${content.join("\n")}\n${fence}\n\n`;
    return section.collapsed ? `<details><summary>${section.title}</summary>\n\n${transcript}</details>\n\n` : `### ${section.title}\n\n${transcript}`;
  };
  const shellBytes = Buffer.byteLength(preamble + tail + bounded.map(section => block(section, [])).join(""), "utf8");
  let remaining = maxBytes - shellBytes;
  const fallback = "Unified session summary omitted: remaining step-summary byte limit reached.\n";
  if (remaining < 512) return Buffer.byteLength(fallback, "utf8") <= maxBytes ? fallback : "";
  const contentBySection = new Map();
  const priority = [bounded[1], bounded[0], bounded[2]];
  for (const [index, section] of priority.entries()) {
    // Reserve an explicit omission notice for every later priority section.
    const reserved = priority.slice(index + 1).reduce((bytes, next) => bytes + Buffer.byteLength(next.notice, "utf8") + 1, 0);
    const budget = remaining - reserved;
    const noticeBytes = Buffer.byteLength(section.notice, "utf8");
    const content = budget < 128 ? (noticeBytes <= budget ? [section.notice] : []) : boundSummaryLines(section.lines, budget, maxLineBytes);
    remaining -= Buffer.byteLength(content.join("\n"), "utf8");
    contentBySection.set(section, content);
  }
  const blocks = bounded.map(section => block(section, contentBySection.get(section)));
  return preamble + blocks.join("") + tail;
}

/** @param {string} filePath @returns {Promise<void>} */
async function publishUnifiedSessionSummary(filePath) {
  let events;
  try {
    events = [];
    for (const event of iterateJsonlEvents(fs.readFileSync(filePath, "utf8"))) {
      if (event.type === "json") events.push(event.value);
      else if (event.reason !== "blank") throw event.error;
    }
  } catch (error) {
    throw new Error(`${ERR_SYSTEM}: Failed to read unified session for summary: ${getErrorMessage(error)}`, { cause: error });
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
      if (!markdown || Buffer.byteLength(markdown, "utf8") > available) {
        core.warning("Unified session step summary omitted: existing step-summary byte limit reached");
        return;
      }
      fs.appendFileSync(process.env.GITHUB_STEP_SUMMARY, `\n${markdown}\n`, "utf8");
    } catch (error) {
      throw new Error(`${ERR_SYSTEM}: Failed to publish unified session step summary: ${getErrorMessage(error)}`, { cause: error });
    }
  } else if (core.summary?.addRaw) await core.summary.addRaw(generateCopilotCliStyleSummary(events)).write();
}

module.exports = { isUnifiedSessionTrace, validateSessionFileHeader, scopedAgentSessions, publicationAgentSessions, renderUnifiedSession, publishUnifiedSessionSummary };
