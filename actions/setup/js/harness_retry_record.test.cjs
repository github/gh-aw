// @ts-check

import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { normalizeHarnessRetryRecord, normalizeHarnessAttemptData, normalizeHarnessAttemptEvent, MAX_HARNESS_ATTEMPTS, MAX_HARNESS_FAILURE_CLASSES } = require("./harness_retry_record.cjs");

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
    "startup_error",
  ])("accepts producer failure class %s", reason => {
    expect(normalizeHarnessRetryRecord(retryRecord(1, [reason]))?.attemptResults[0].failureClasses).toEqual([reason]);
    expect(normalizeHarnessAttemptData({ attempt: 1, exitCode: 1, failureClasses: [], reason })?.reason).toBe(reason);
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
    { ...retryRecord(), attempt_results: [] },
    { ...retryRecord(), attempt_results: [{ attempt: 2, exit_code: 1, failure_classes: [] }] },
    { ...retryRecord(), attempt_results: [{ attempt: 1, failure_classes: [] }] },
  ])("rejects incomplete, oversized or unrecognized records %#", record => {
    expect(normalizeHarnessRetryRecord(record)).toBeUndefined();
  });

  it("preserves explicitly incomplete attempts without inventing completion metadata", () => {
    const record = { attempts: 1, exitCode: null, attemptResults: [{ attempt: 1, exitCode: null, failureClasses: [], attemptId: "harness:1", startedAt: 123, outcome: "running" }] };
    expect(normalizeHarnessRetryRecord(record)).toEqual(record);
    expect(normalizeHarnessAttemptData(record.attemptResults[0])).not.toHaveProperty("finishedAt");
    expect(normalizeHarnessAttemptData({ ...record.attemptResults[0], finished_at: null })).toEqual({ ...record.attemptResults[0], finishedAt: null });
  });

  it("preserves legacy integer exit codes without narrowing them to process status codes", () => {
    const record = { attempts: 1, exitCode: -1, attemptResults: [{ attempt: 1, exitCode: 256, failureClasses: [] }] };
    expect(normalizeHarnessRetryRecord(record)).toEqual(record);
  });

  it("preserves bounded startup scope for nested harness attempts", () => {
    const raw = { attempt: 1, exit_code: null, failure_classes: [], attempt_id: "harness:1", startup_attempt_id: "startup:2" };
    expect(normalizeHarnessAttemptData(raw)).toEqual({ attempt: 1, exitCode: null, failureClasses: [], attemptId: "harness:1", startupAttemptId: "startup:2" });
    expect(normalizeHarnessAttemptData({ ...raw, attempt_id: "startup:1" }, "startup")).toBeUndefined();
    expect(normalizeHarnessAttemptData({ ...raw, attempt_id: "startup:2:harness:1" })).toEqual({ attempt: 1, exitCode: null, failureClasses: [], attemptId: "startup:2:harness:1", startupAttemptId: "startup:2" });
    expect(normalizeHarnessAttemptData({ ...raw, attempt_id: "startup:3:harness:1" })).toBeUndefined();
  });

  it("normalizes all optional metadata and startup attempts without retaining payloads", () => {
    const raw = {
      attempt: 1,
      exit_code: 1,
      failure_classes: ["rate_limit_error"],
      attempt_id: "harness:1",
      started_at: 1700000000000,
      finished_at: 1700000000100,
      strategy: "resume",
      backoff_ms: 100,
      outcome: "failure",
      reason: "rate_limit_error",
      output: "PRIVATE",
      payload: { secret: "PRIVATE" },
    };
    const attempt = {
      attempt: 1,
      exitCode: 1,
      failureClasses: ["rate_limit_error"],
      attemptId: "harness:1",
      startedAt: 1700000000000,
      finishedAt: 1700000000100,
      strategy: "resume",
      backoffMs: 100,
      outcome: "failure",
      reason: "rate_limit_error",
    };
    const expected = { attempts: 1, exitCode: 1, attemptResults: [attempt], startupAttemptResults: [{ ...attempt, attemptId: "startup:1" }] };
    const record = normalizeHarnessRetryRecord({ ...retryRecord(), attempt_results: [raw], startup_attempt_results: [{ ...raw, attempt_id: "startup:1" }] });
    expect(record).toEqual(expected);
    expect(normalizeHarnessRetryRecord(record)).toEqual(expected);
    expect(normalizeHarnessAttemptEvent({ ...raw, layer: "harness" })).toEqual({ ...attempt, layer: "harness" });
    expect(JSON.stringify(record)).not.toContain("PRIVATE");
  });

  it.each([
    { attemptId: "PRIVATE" },
    { attemptId: "startup:1" },
    { attemptId: "harness:2" },
    { attemptId: "startup:0:harness:1" },
    { attemptId: "startup:102:harness:1" },
    { attemptId: "startup:1:harness:2" },
    { attemptId: "startup:1:harness:01" },
    { startupAttemptId: "PRIVATE" },
    { startupAttemptId: "startup:0" },
    { startupAttemptId: "startup:102" },
    { startupAttemptId: "startup:01" },
    { startedAt: -1 },
    { startedAt: 1.5 },
    { finishedAt: Infinity },
    { backoffMs: null },
    { strategy: "PRIVATE" },
    { outcome: "PRIVATE" },
    { reason: "PRIVATE" },
    { reason: "constructor" },
    { startedAt: 20, finishedAt: 10 },
    { exitCode: undefined },
    { exitCode: "1" },
  ])("rejects malformed or unsafe optional fields %j", fields => {
    expect(normalizeHarnessAttemptData({ attempt: 1, exitCode: null, failureClasses: [], ...fields })).toBeUndefined();
  });

  it.each(["running", "success", "failure", "cancelled", "interrupted"])("preserves observed %s outcome", outcome => {
    expect(normalizeHarnessAttemptData({ attempt: 1, exitCode: null, failureClasses: [], outcome })?.outcome).toBe(outcome);
  });

  it.each([{ layer: "PRIVATE" }, { layer: null }, {}, { layer: "startup", attemptId: "harness:1" }])("rejects malformed attempt event layers %j", fields => {
    expect(normalizeHarnessAttemptEvent({ attempt: 1, exitCode: null, failureClasses: [], ...fields })).toBeUndefined();
  });

  it.each([null, {}, Array(MAX_HARNESS_ATTEMPTS + 1).fill({}), [{ attempt: 2, exitCode: 0, failureClasses: [] }]])("rejects malformed startup arrays %j", startupAttemptResults => {
    expect(normalizeHarnessRetryRecord({ ...retryRecord(), startupAttemptResults })).toBeUndefined();
  });
});
