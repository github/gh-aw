// @ts-check

/** @typedef {import("./types/agent_session").SessionEvent} SessionEvent */
/** @typedef {Record<string, string[]>} Fields */

/** @type {Fields} */
const USAGE_FIELDS = {
  total_tokens: ["total_tokens", "totalTokens"],
  input_tokens: ["input_tokens", "inputTokens"],
  output_tokens: ["output_tokens", "outputTokens"],
  reasoning_output_tokens: ["reasoning_output_tokens"],
  cache_read_input_tokens: ["cache_read_input_tokens", "cacheReadInputTokens", "cache_read_tokens"],
  cache_creation_input_tokens: ["cache_creation_input_tokens", "cacheCreationInputTokens", "cache_write_tokens"],
  input_tokens_include_cache: ["input_tokens_include_cache"],
  overflowed_tokens: ["overflowed_tokens"],
};

/** @type {Fields} */
const TOOL_FIELDS = {
  toolCallId: ["toolCallId", "tool_call_id", "tool_use_id"],
  toolName: ["toolName", "tool_name"],
  mcpServerName: ["mcpServerName"],
};
/** @type {Fields} */
const RUNTIME_FIELDS = {
  event: ["event", "type", "event_name", "eventName"],
  level: ["level"],
  status: ["status"],
  message: ["message"],
  reason: ["reason"],
  requestId: ["requestId", "request_id", "rid"],
};
/** @type {Fields} */
const MCP_FIELDS = {
  serverName: ["serverName", "server_name", "server_id"],
  direction: ["direction"],
  rpcId: ["rpcId"],
  method: ["method"],
  toolName: ["toolName", "tool_name"],
  toolCallId: ["toolCallId", "tool_call_id"],
  requestId: ["requestId", "request_id"],
  durationMs: ["durationMs", "duration_ms", "duration"],
  inputSize: ["inputSize", "input_size"],
  outputSize: ["outputSize", "output_size"],
  status: ["status"],
  reason: ["reason"],
  error: ["error"],
};
/** @type {Fields} */
const GRADER_FIELDS = {
  id: ["id"],
  name: ["name"],
  status: ["status"],
  value: ["value"],
  score: ["score"],
  unit: ["unit"],
  passed: ["passed"],
  direction: ["direction"],
  threshold: ["threshold"],
  error: ["error"],
};
/** @type {Fields} */
const SAFE_OUTPUT_FIELDS = {
  type: ["type"],
  repo: ["repo", "repository"],
  number: ["number"],
  provider: ["provider"],
  identifier: ["identifier"],
  url: ["url"],
  status: ["status"],
  message: ["message"],
  error: ["error"],
  errorCode: ["errorCode", "error_code"],
};

/** @type {Record<string, Fields>} */
const EVENT_FIELDS = {
  "session.format": { version: ["version"] },
  "session.init": { sourceEngine: ["sourceEngine"], model: ["model"], sessionId: ["sessionId", "session_id"], cwd: ["cwd"] },
  "user.message": { content: ["content"] },
  "assistant.message": { content: ["content"] },
  "assistant.reasoning": { content: ["content"] },
  "tool.execution_start": { ...TOOL_FIELDS, input: ["input", "parameters"], command: ["command"] },
  "tool.execution_complete": {
    ...TOOL_FIELDS,
    success: ["success"],
    output: ["output", "result"],
    error: ["error"],
    durationMs: ["durationMs", "duration_ms"],
    exitCode: ["exitCode", "exit_code"],
    status: ["status"],
    isError: ["isError", "is_error"],
  },
  "session.result": {
    numTurns: ["numTurns", "num_turns"],
    durationMs: ["durationMs", "duration_ms"],
    totalCostUsd: ["totalCostUsd", "total_cost_usd"],
    status: ["status"],
    sourceType: ["sourceType"],
    errors: ["errors"],
    permissionDenials: ["permissionDenials", "permission_denials"],
  },
  "mcp.rpc.request": MCP_FIELDS,
  "mcp.rpc.response": MCP_FIELDS,
  "mcp.difc.filtered": MCP_FIELDS,
  "mcp.guard.blocked": MCP_FIELDS,
  "mcp.tool_call": MCP_FIELDS,
  "mcp.event": RUNTIME_FIELDS,
  "firewall.http_access": {
    host: ["host", "domain"],
    method: ["method"],
    status: ["status", "http_status"],
    decision: ["decision", "squid_request_status"],
    bytes: ["bytes"],
    durationMs: ["durationMs", "duration_ms"],
  },
  "firewall.steering": RUNTIME_FIELDS,
  "firewall.event": RUNTIME_FIELDS,
  "firewall.token_usage": {
    provider: ["provider"],
    model: ["model"],
    requestId: ["requestId", "request_id"],
    status: ["status"],
    aic: ["aic", "ai_credits_this_response"],
    totalAic: ["totalAic", "ai_credits_total", "ai_credits"],
    premiumRequests: ["premiumRequests", "premium_requests"],
    durationMs: ["durationMs", "duration_ms"],
  },
  "safe_output.request": SAFE_OUTPUT_FIELDS,
  "safe_output.result": SAFE_OUTPUT_FIELDS,
  "safe_output.error": SAFE_OUTPUT_FIELDS,
  "experiment.state": { runId: ["runId", "run_id"], assignments: ["assignments"], counts: ["counts"] },
  "grader.manifest": {},
  "grader.result": GRADER_FIELDS,
  "eval.result": { id: ["id"], answer: ["answer"], model: ["model"], error: ["error"] },
  "execution.result": { outcome: ["outcome"], conclusion: ["conclusion"], exitCode: ["exitCode", "exit_code"], durationMs: ["durationMs", "duration_ms"], startedAt: ["startedAt", "started_at"], finishedAt: ["finishedAt", "finished_at"] },
  "detection.result": {
    jobResult: ["jobResult", "job_result"],
    conclusion: ["conclusion"],
    reason: ["reason"],
    promptInjection: ["promptInjection", "prompt_injection"],
    secretLeak: ["secretLeak", "secret_leak"],
    maliciousPatch: ["maliciousPatch", "malicious_patch"],
  },
  "workflow.info": { engine: ["engine", "engine_id"], model: ["model"], workflow: ["workflow", "workflow_name"], repository: ["repository"], runId: ["runId", "run_id"] },
};
EVENT_FIELDS["session.start"] = EVENT_FIELDS["session.init"];
EVENT_FIELDS["usage.report"] = EVENT_FIELDS["firewall.token_usage"];

/**
 * Prefer an explicitly supplied canonical field, including false, zero, and null.
 * @param {any} value
 * @param {Fields} fields
 * @returns {Record<string, any>}
 */
function selectFields(value, fields) {
  const result = {};
  for (const [key, aliases] of Object.entries(fields)) {
    const alias = aliases.find(name => value != null && Object.hasOwn(value, name) && value[name] !== undefined);
    if (alias !== undefined) result[key] = structuredClone(value[alias]);
  }
  return result;
}

/**
 * Compact known payloads without truncating text or changing native parser traces.
 * Unknown extension data remains opaque for forward compatibility.
 * @param {SessionEvent} event
 * @returns {SessionEvent}
 */
function normalizeUnifiedSessionEvent(event) {
  /** @type {any} */
  const source = event.data;
  const known = Object.hasOwn(EVENT_FIELDS, event.type);
  const data = known ? selectFields(source, EVENT_FIELDS[event.type]) : structuredClone(source);
  if (known && event.type.startsWith("mcp.") && event.type !== "mcp.event") {
    const rpc = source.payload;
    for (const [key, value] of Object.entries({ rpcId: rpc?.id, method: rpc?.method, toolName: rpc?.params?.name, error: rpc?.error })) {
      if (!Object.hasOwn(data, key) && value !== undefined) data[key] = structuredClone(value);
    }
    if (data.error && typeof data.error === "object") data.error = selectFields(data.error, { code: ["code"], message: ["message"] });
  }
  if (["session.result", "firewall.token_usage", "usage.report"].includes(event.type)) {
    const usage = selectFields(event.type === "session.result" ? source.usage : (source.usage ?? source), USAGE_FIELDS);
    if (Object.keys(usage).length || (source.usage && typeof source.usage === "object")) data.usage = usage;
    else if (source.usage === null) data.usage = null;
  }
  if (event.type === "tool.execution_complete" && (source.is_error === true || source.result?.isError === true || source.result?.is_error === true)) data.isError = true;
  if (event.type === "experiment.assignment") {
    for (const key of Object.keys(data)) delete data[key];
    data.assignments = structuredClone(source.assignments ?? source);
  }
  if (event.type === "grader.manifest" && Array.isArray(source.graders)) data.graders = source.graders.map(grader => selectFields(grader, GRADER_FIELDS));
  if (event.type === "grader.result" && Array.isArray(source.results)) data.results = source.results.map(result => selectFields(result, GRADER_FIELDS));
  if (event.type === "safe_output.error") {
    const errors = source.errors ?? source.failures;
    if (Array.isArray(errors)) data.errors = errors.map(error => (error && typeof error === "object" && !Array.isArray(error) ? selectFields(error, SAFE_OUTPUT_FIELDS) : structuredClone(error)));
  }
  const metadata = selectFields(event, { id: ["id"], parentId: ["parentId"], timestamp: ["timestamp", "ts", "time", "created_at"] });
  /** @type {any} */
  const nativeMessage = event.message;
  if (!Object.hasOwn(metadata, "timestamp") && nativeMessage?.timestamp !== undefined) metadata.timestamp = structuredClone(nativeMessage.timestamp);
  return {
    type: event.type,
    ...metadata,
    data,
  };
}

module.exports = { normalizeUnifiedSessionEvent };
