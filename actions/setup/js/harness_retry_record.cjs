// @ts-check

const MAX_HARNESS_ATTEMPTS = 101;
const MAX_HARNESS_DECISIONS = 1024;
const MAX_HARNESS_FAILURE_CLASSES = 16;
const FAILURE_CLASS_PATTERN = /^[a-z][a-z0-9_]{0,63}$/;
const HARNESS_FAILURE_CLASSES = new Set([
  "cancelled",
  "runtime_guard",
  "watchdog",
  "unknown_error",
  "no_output",
  "content_filter",
  "model_endpoint_mismatch",
  "model_routing_failed",
  "invocation_cap_exceeded",
  "ai_credits_exhausted",
  "api_proxy_guard_rejected",
  "capi_quota_exceeded",
  "capi_server_error",
  "mcp_policy_blocked",
  "model_not_supported",
  "tool_call_id_schema_error",
  "incomplete_tool_call_stream",
  "http_400_response_error",
  "null_type_tool_call",
  "no_auth_info",
  "authentication_failed",
  "sdk_session_idle_timeout",
  "mcp_gateway_shutdown",
  "permission_denied",
  "capi_error_400",
  "long_run_exit",
  "partial_execution",
  "copilot_model_verification",
  "overloaded_error",
  "rate_limit_error",
  "rate_limit_wait_exceeded",
  "max_turns",
  "no_deferred_marker",
  "invalid_model",
  "invalid_json_body",
  "connection_refused",
  "sandbox_runtime_crash",
  "cancelled_or_timed_out",
  "token_per_minute_rate_limit",
  "missing_api_key",
  "server_error",
  "unsupported_model_tools",
  "invalid_request",
  "startup_error",
]);

/** @param {unknown} value @returns {value is Record<string, any>} */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {Record<string, any>} record @param {string} snakeCase @param {string} camelCase */
function readField(record, snakeCase, camelCase) {
  return Object.hasOwn(record, snakeCase) ? record[snakeCase] : record[camelCase];
}

/** @param {unknown} value */
function isFailureClass(value) {
  return typeof value === "string" && FAILURE_CLASS_PATTERN.test(value) && HARNESS_FAILURE_CLASSES.has(value);
}

/**
 * Validate and project only bounded, observed attempt metadata.
 * @param {unknown} value
 * @param {"harness" | "startup"} [layer]
 * @returns {import("./types/unified_session").HarnessRetryAttemptData | undefined}
 */
function normalizeHarnessAttemptData(value, layer = "harness") {
  if (!isRecord(value)) return undefined;
  const exitCode = readField(value, "exit_code", "exitCode");
  const failureClasses = readField(value, "failure_classes", "failureClasses");
  if (
    !Number.isSafeInteger(value.attempt) ||
    value.attempt < 1 ||
    value.attempt > MAX_HARNESS_ATTEMPTS ||
    !(exitCode === null || Number.isSafeInteger(exitCode)) ||
    !Array.isArray(failureClasses) ||
    failureClasses.length > MAX_HARNESS_FAILURE_CLASSES ||
    !failureClasses.every(isFailureClass)
  )
    return undefined;
  /** @type {import("./types/unified_session").HarnessRetryAttemptData} */
  const result = { attempt: value.attempt, exitCode, failureClasses: [...failureClasses] };
  for (const [snake, camel] of [
    ["attempt_id", "attemptId"],
    ["startup_attempt_id", "startupAttemptId"],
    ["started_at", "startedAt"],
    ["finished_at", "finishedAt"],
    ["backoff_ms", "backoffMs"],
    ["strategy", "strategy"],
    ["outcome", "outcome"],
    ["reason", "reason"],
  ]) {
    if (!Object.hasOwn(value, snake) && !Object.hasOwn(value, camel)) continue;
    const field = readField(value, snake, camel);
    if (camel === "attemptId") {
      const scoped = typeof field === "string" ? /^startup:([1-9][0-9]{0,2}):harness:([1-9][0-9]{0,2})$/.exec(field) : null;
      if (field !== `${layer}:${value.attempt}` && !(layer === "harness" && scoped && Number(scoped[1]) <= MAX_HARNESS_ATTEMPTS && Number(scoped[2]) === value.attempt)) return undefined;
    }
    if (camel === "startupAttemptId" && !(layer === "harness" && typeof field === "string" && /^startup:[1-9][0-9]{0,2}$/.test(field) && Number(field.slice(8)) <= MAX_HARNESS_ATTEMPTS)) return undefined;
    if (["startedAt", "finishedAt", "backoffMs"].includes(camel) && !(camel === "finishedAt" && field === null) && !(Number.isSafeInteger(field) && field >= 0)) return undefined;
    if (camel === "strategy" && !["restart", "resume"].includes(field)) return undefined;
    if (camel === "outcome" && !["running", "success", "failure", "cancelled", "interrupted"].includes(field)) return undefined;
    if (camel === "reason" && !isFailureClass(field)) return undefined;
    Object.assign(result, { [camel]: field });
  }
  if (result.startedAt !== undefined && result.finishedAt != null && result.finishedAt < result.startedAt) return undefined;
  if (result.attemptId?.includes(":harness:") && result.startupAttemptId !== undefined && !result.attemptId.startsWith(`${result.startupAttemptId}:`)) return undefined;
  return result;
}

/**
 * @param {unknown} value
 * @returns {(import("./types/unified_session").HarnessAttemptData & Record<string, unknown>) | undefined}
 */
function normalizeHarnessAttemptEvent(value) {
  if (!isRecord(value) || !["harness", "startup"].includes(value.layer)) return undefined;
  const attempt = normalizeHarnessAttemptData(value, value.layer);
  return attempt ? { ...attempt, layer: value.layer } : undefined;
}

/**
 * Validate and project a harness retry record without retaining arbitrary fields.
 * @param {unknown} value
 * @returns {(import("./types/unified_session").HarnessRetryData & Record<string, unknown>) | undefined}
 */
function normalizeHarnessRetryRecord(value) {
  if (!isRecord(value)) return undefined;
  const attempts = value.attempts;
  const exitCode = readField(value, "exit_code", "exitCode");
  const rawResults = readField(value, "attempt_results", "attemptResults");
  if (!Number.isSafeInteger(attempts) || attempts < 0 || attempts > MAX_HARNESS_ATTEMPTS || !(exitCode === null || Number.isSafeInteger(exitCode)) || !Array.isArray(rawResults) || rawResults.length !== attempts) return undefined;
  const attemptResults = [];
  for (const [index, value] of rawResults.entries()) {
    const result = normalizeHarnessAttemptData(value);
    if (!result || result.attempt !== index + 1) return undefined;
    attemptResults.push(result);
  }
  /** @type {import("./types/unified_session").HarnessRetryData & Record<string, unknown>} */
  const record = { attempts, exitCode, attemptResults };
  if (Object.hasOwn(value, "startup_attempt_results") || Object.hasOwn(value, "startupAttemptResults")) {
    const rawStartup = readField(value, "startup_attempt_results", "startupAttemptResults");
    if (!Array.isArray(rawStartup) || rawStartup.length > MAX_HARNESS_ATTEMPTS) return undefined;
    const startupAttemptResults = [];
    for (const [index, value] of rawStartup.entries()) {
      const result = normalizeHarnessAttemptData(value, "startup");
      if (!result || result.attempt !== index + 1) return undefined;
      startupAttemptResults.push(result);
    }
    record.startupAttemptResults = startupAttemptResults;
  }
  if (Object.hasOwn(value, "decision_results") || Object.hasOwn(value, "decisionResults")) {
    const rawDecisions = readField(value, "decision_results", "decisionResults");
    if (!Array.isArray(rawDecisions) || rawDecisions.length > MAX_HARNESS_DECISIONS) return undefined;
    const decisions = [];
    for (const raw of rawDecisions) {
      const decision = normalizeHarnessDecisionData(raw);
      if (!decision) return undefined;
      decisions.push(decision);
    }
    record.decisionResults = decisions;
  }
  return record;
}

const DECISION_OUTCOMES = new Set(["allow", "deny", "unknown"]);
const DECISION_ERRORS = new Set(["invalid_configuration", "sandbox_unavailable", "proxy_unavailable", "invalid_proxy_endpoint", "model_unavailable", "timeout", "evaluation_failed", "file_reference_failed", "sensitive_content_blocked"]);

/**
 * Project only bounded metadata; never keep decision input or provider diagnostics.
 * @param {unknown} value
 * @returns {import("./types/unified_session").HarnessDecisionData | undefined}
 */
function normalizeHarnessDecisionData(value) {
  if (
    !isRecord(value) ||
    !Number.isSafeInteger(value.proposedAttempt) ||
    value.proposedAttempt < 1 ||
    value.proposedAttempt > MAX_HARNESS_ATTEMPTS ||
    !DECISION_OUTCOMES.has(value.outcome) ||
    !Number.isSafeInteger(value.latencyMs) ||
    value.latencyMs < 0 ||
    !Number.isSafeInteger(value.timestamp) ||
    value.timestamp < 0 ||
    value.avoidedInvocation !== (value.outcome === "deny") ||
    !Array.isArray(value.questions) ||
    value.questions.length > 400 ||
    (value.error !== undefined && !DECISION_ERRORS.has(value.error))
  )
    return undefined;
  if (value.startupAttemptId !== undefined && !(typeof value.startupAttemptId === "string" && /^startup:[1-9][0-9]{0,2}$/.test(value.startupAttemptId) && Number(value.startupAttemptId.slice(8)) <= MAX_HARNESS_ATTEMPTS)) return undefined;
  const questions = [];
  for (const [index, item] of value.questions.entries()) {
    if (!isRecord(item) || item.question !== index + 1 || !DECISION_OUTCOMES.has(item.outcome) || (item.error !== undefined && !DECISION_ERRORS.has(item.error))) return undefined;
    questions.push({ question: item.question, outcome: item.outcome, ...(item.error ? { error: item.error } : {}) });
  }
  const aggregate = questions.some(question => question.outcome === "deny") ? "deny" : questions.some(question => question.outcome === "allow") ? "allow" : "unknown";
  if (value.outcome !== aggregate) return undefined;
  return {
    proposedAttempt: value.proposedAttempt,
    outcome: value.outcome,
    questions,
    latencyMs: value.latencyMs,
    timestamp: value.timestamp,
    avoidedInvocation: value.avoidedInvocation,
    ...(value.error ? { error: value.error } : {}),
    ...(value.startupAttemptId ? { startupAttemptId: value.startupAttemptId } : {}),
  };
}

module.exports = { normalizeHarnessRetryRecord, normalizeHarnessAttemptData, normalizeHarnessAttemptEvent, normalizeHarnessDecisionData, MAX_HARNESS_ATTEMPTS, MAX_HARNESS_DECISIONS, MAX_HARNESS_FAILURE_CLASSES };
