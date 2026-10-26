// @ts-check

const MAX_HARNESS_ATTEMPTS = 101;
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
]);

/** @param {unknown} value @returns {value is Record<string, any>} */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @param {Record<string, any>} record @param {string} snakeCase @param {string} camelCase */
function readField(record, snakeCase, camelCase) {
  return Object.hasOwn(record, snakeCase) ? record[snakeCase] : record[camelCase];
}

/**
 * Validate and project a harness retry record without retaining arbitrary fields.
 * @param {unknown} value
 * @returns {{attempts: number, exitCode: number, attemptResults: {attempt: number, exitCode: number, failureClasses: string[]}[]} | undefined}
 */
function normalizeHarnessRetryRecord(value) {
  if (!isRecord(value)) return undefined;
  const attempts = value.attempts;
  const exitCode = readField(value, "exit_code", "exitCode");
  const rawResults = readField(value, "attempt_results", "attemptResults");
  if (!Number.isSafeInteger(attempts) || attempts < 0 || attempts > MAX_HARNESS_ATTEMPTS || !Number.isSafeInteger(exitCode) || !Array.isArray(rawResults) || rawResults.length !== attempts) {
    return undefined;
  }
  const attemptResults = [];
  for (const [index, result] of rawResults.entries()) {
    if (!isRecord(result)) return undefined;
    const resultExitCode = readField(result, "exit_code", "exitCode");
    const failureClasses = readField(result, "failure_classes", "failureClasses");
    if (
      result.attempt !== index + 1 ||
      !Number.isSafeInteger(resultExitCode) ||
      !Array.isArray(failureClasses) ||
      failureClasses.length > MAX_HARNESS_FAILURE_CLASSES ||
      !failureClasses.every(value => typeof value === "string" && FAILURE_CLASS_PATTERN.test(value) && HARNESS_FAILURE_CLASSES.has(value))
    ) {
      return undefined;
    }
    attemptResults.push({ attempt: result.attempt, exitCode: resultExitCode, failureClasses: [...failureClasses] });
  }
  return { attempts, exitCode, attemptResults };
}

module.exports = { normalizeHarnessRetryRecord, MAX_HARNESS_ATTEMPTS, MAX_HARNESS_FAILURE_CLASSES };
