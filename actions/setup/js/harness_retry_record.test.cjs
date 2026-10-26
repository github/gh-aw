// @ts-check

import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { normalizeHarnessRetryRecord, MAX_HARNESS_ATTEMPTS, MAX_HARNESS_FAILURE_CLASSES } = require("./harness_retry_record.cjs");

function retryRecord(attempts = 1, failureClasses = ["rate_limit_error"]) {
  return {
    attempts,
    exit_code: 1,
    attempt_results: Array.from({ length: attempts }, (_, index) => ({ attempt: index + 1, exit_code: 1, failure_classes: failureClasses })),
  };
}

describe("normalizeHarnessRetryRecord", () => {
  it.each([
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
  ])("accepts producer failure class %s", reason => {
    expect(normalizeHarnessRetryRecord(retryRecord(1, [reason]))?.attemptResults[0].failureClasses).toEqual([reason]);
  });

  it.each([0, MAX_HARNESS_ATTEMPTS])("accepts %i attempts", attempts => {
    expect(normalizeHarnessRetryRecord(retryRecord(attempts))?.attempts).toBe(attempts);
  });

  it("accepts the maximum bounded number of failure classes", () => {
    expect(normalizeHarnessRetryRecord(retryRecord(MAX_HARNESS_ATTEMPTS, Array(MAX_HARNESS_FAILURE_CLASSES).fill("rate_limit_error")))?.attemptResults).toHaveLength(MAX_HARNESS_ATTEMPTS);
  });

  it("accepts normalized session records without retaining extra fields", () => {
    const record = { attempts: 1, exitCode: 1, attemptResults: [{ attempt: 1, exitCode: 1, failureClasses: ["runtime_guard"] }] };
    expect(normalizeHarnessRetryRecord({ ...record, output: "private output" })).toEqual(record);
  });

  it.each([
    retryRecord(MAX_HARNESS_ATTEMPTS + 1),
    retryRecord(1, Array(MAX_HARNESS_FAILURE_CLASSES + 1).fill("rate_limit_error")),
    retryRecord(1, ["secret_736563726574"]),
    retryRecord(1, ["unknown_failure"]),
    retryRecord(1, ["constructor"]),
    { ...retryRecord(), exit_code: null },
    { ...retryRecord(), attempt_results: [] },
    { ...retryRecord(), attempt_results: [{ attempt: 2, exit_code: 1, failure_classes: [] }] },
    { ...retryRecord(), attempt_results: [{ attempt: 1, exit_code: null, failure_classes: [] }] },
    { ...retryRecord(), attempt_results: [{ attempt: 1, failure_classes: [] }] },
  ])("rejects incomplete, oversized or unrecognized records %#", record => {
    expect(normalizeHarnessRetryRecord(record)).toBeUndefined();
  });
});
