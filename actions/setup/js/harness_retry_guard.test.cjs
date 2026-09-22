// @ts-check

import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { detectNonRetryableHarnessGuard, buildSoftTimeoutGuard, isMaxRunsExceededError, isAuthenticationFailedError, parseAICreditsExceededProxyRejection, parseAPIProxyGuardRejection } = require("./harness_retry_guard.cjs");

describe("harness_retry_guard.cjs", () => {
  it("detects the AWF API proxy cache-miss guardrail in text form", () => {
    const output = "API Error: 403 Maximum consecutive cache misses exceeded (5 / 5).";
    expect(parseAPIProxyGuardRejection(output)).toEqual({
      guard: "max_cache_misses_exceeded",
      counters: { consecutive_cache_misses: 5, max_cache_misses: 5 },
    });
    expect(detectNonRetryableHarnessGuard(output).apiProxyGuardRejection?.guard).toBe("max_cache_misses_exceeded");
  });

  it("detects the structured api-proxy guardrail error types", () => {
    expect(parseAPIProxyGuardRejection('{"type":"max_cache_misses_exceeded"}')?.guard).toBe("max_cache_misses_exceeded");
    expect(parseAPIProxyGuardRejection('{"type":"effective_tokens_limit_exceeded"}')?.guard).toBe("effective_tokens_limit_exceeded");
    expect(parseAPIProxyGuardRejection('{"type":"permission_denied_limit_exceeded"}')?.guard).toBe("permission_denied_limit_exceeded");
    expect(parseAPIProxyGuardRejection('{"type":"model_policy_violation"}')?.guard).toBe("model_policy_violation");
  });

  it("does not report a proxy guardrail for a genuine credential failure", () => {
    const output = "Authentication failed with provider at http://172.30.0.30:10002 (HTTP 403).";
    expect(parseAPIProxyGuardRejection(output)).toBeNull();
    expect(detectNonRetryableHarnessGuard(output).apiProxyGuardRejection).toBeNull();
    expect(isAuthenticationFailedError(output)).toBe(true);
  });

  it("detects AI credits exceeded markers", () => {
    const result = detectNonRetryableHarnessGuard("error: max_ai_credits_exceeded=true");
    expect(result.aiCreditsExceeded).toBe(true);
    expect(result.awfAPIProxyBlockingRequests).toBe(false);
  });

  it("detects AI credits rate-limit markers", () => {
    const result = detectNonRetryableHarnessGuard("error: ai_credits_rate_limit_error=true");
    expect(result.aiCreditsExceeded).toBe(true);
    expect(result.awfAPIProxyBlockingRequests).toBe(false);
  });

  it("detects AI credits budget markers", () => {
    const result = detectNonRetryableHarnessGuard("error: ai credits budget exceeded");
    expect(result.aiCreditsExceeded).toBe(true);
    expect(result.awfAPIProxyBlockingRequests).toBe(false);
  });

  it("parses the AWF API proxy HTTP 403 AI credits rejection surfaced as an auth failure", () => {
    const output = JSON.stringify({
      type: "assistant",
      message: { content: [{ type: "text", text: "Failed to authenticate. API Error: 403 Maximum AI credits exceeded (302.111025 / 300)." }] },
      error: "authentication_failed",
      is_api_error_message: true,
    });
    expect(detectNonRetryableHarnessGuard(output).aiCreditsExceeded).toBe(true);
    expect(isAuthenticationFailedError(output)).toBe(true);
    expect(parseAICreditsExceededProxyRejection(output)).toEqual({ aiCredits: 302.111025, maxAICredits: 300 });
  });

  it("ignores AI credits markers that lack the proxy 403 usage pair", () => {
    expect(parseAICreditsExceededProxyRejection("error: max_ai_credits_exceeded=true")).toBeNull();
    expect(parseAICreditsExceededProxyRejection('{"error":"authentication_failed","message":"API Error: 403 Maximum AI credits exceeded"}')).toBeNull();
    expect(parseAICreditsExceededProxyRejection(undefined)).toBeNull();
  });

  it("ignores a proxy 403 usage pair that has not reached the budget", () => {
    expect(parseAICreditsExceededProxyRejection('{"error":"authentication_failed","message":"API Error: 403 Maximum AI credits exceeded (12 / 300)."}')).toBeNull();
  });

  it("accepts a proxy 403 usage pair that exactly reaches the budget", () => {
    expect(parseAICreditsExceededProxyRejection('{"error":"authentication_failed","message":"API Error: 403 Maximum AI credits exceeded (300 / 300)."}')).toEqual({
      aiCredits: 300,
      maxAICredits: 300,
    });
  });

  it("ignores the proxy 403 usage pair when it only appears in plain assistant text without an engine error marker", () => {
    // Simulates an assistant response that merely discusses/quotes the phrase — this must
    // NOT be trusted as proxy evidence since there is no engine-authenticated error marker.
    const output = JSON.stringify({
      type: "assistant",
      message: { content: [{ type: "text", text: "Earlier the run hit: API Error: 403 Maximum AI credits exceeded (302.111025 / 300)." }] },
    });
    expect(parseAICreditsExceededProxyRejection(output)).toBeNull();
  });

  it("ignores JSON lines without an engine error marker even when the usage pair is present", () => {
    expect(parseAICreditsExceededProxyRejection('{"type":"assistant","text":"API Error: 403 Maximum AI credits exceeded (302.111025 / 300)."}')).toBeNull();
  });

  it("detects AWF API proxy blocking request markers", () => {
    const result = detectNonRetryableHarnessGuard("awf api proxy is blocking requests for this run");
    expect(result.aiCreditsExceeded).toBe(false);
    expect(result.awfAPIProxyBlockingRequests).toBe(true);
  });

  it("detects API proxy blocking request markers without AWF prefix", () => {
    const result = detectNonRetryableHarnessGuard("api-proxy is blocking requests");
    expect(result.aiCreditsExceeded).toBe(false);
    expect(result.awfAPIProxyBlockingRequests).toBe(true);
  });

  it("detects API proxy blocked request markers", () => {
    const result = detectNonRetryableHarnessGuard("api proxy blocked request");
    expect(result.aiCreditsExceeded).toBe(false);
    expect(result.awfAPIProxyBlockingRequests).toBe(true);
  });

  it("detects DIFC filtered proxy block markers", () => {
    const result = detectNonRetryableHarnessGuard('{"type":"DIFC_FILTERED","reason":"blocked"}');
    expect(result.aiCreditsExceeded).toBe(false);
    expect(result.awfAPIProxyBlockingRequests).toBe(true);
  });

  it("returns false for non-string input", () => {
    const result = detectNonRetryableHarnessGuard(null);
    expect(result.aiCreditsExceeded).toBe(false);
    expect(result.awfAPIProxyBlockingRequests).toBe(false);
    expect(result.goalAlreadyActive).toBe(false);
  });

  it("detects both flags when output contains both signals", () => {
    const result = detectNonRetryableHarnessGuard("max_ai_credits_exceeded=true DIFC_FILTERED");
    expect(result.aiCreditsExceeded).toBe(true);
    expect(result.awfAPIProxyBlockingRequests).toBe(true);
    expect(result.goalAlreadyActive).toBe(false);
  });

  it("returns false when output has no guard markers", () => {
    const result = detectNonRetryableHarnessGuard("transient network timeout");
    expect(result.aiCreditsExceeded).toBe(false);
    expect(result.awfAPIProxyBlockingRequests).toBe(false);
    expect(result.goalAlreadyActive).toBe(false);
  });

  it("detects goal already active markers", () => {
    const result = detectNonRetryableHarnessGuard("cannot create a new goal because this thread already has a goal; use update_goal only when the existing goal is complete");
    expect(result.aiCreditsExceeded).toBe(false);
    expect(result.awfAPIProxyBlockingRequests).toBe(false);
    expect(result.goalAlreadyActive).toBe(true);
  });

  it("detects goal already active markers across newlines", () => {
    const result = detectNonRetryableHarnessGuard("this thread already has a goal\nuse update_goal to update it");
    expect(result.goalAlreadyActive).toBe(true);
  });

  it("does not detect goal active from first phrase alone", () => {
    const result = detectNonRetryableHarnessGuard("this thread already has a goal");
    expect(result.goalAlreadyActive).toBe(false);
  });

  it("detects goal already active when embedded in longer output", () => {
    const result = detectNonRetryableHarnessGuard("[codex] cannot create a new goal because this thread already has a goal; use update_goal only when the existing goal is complete\nExit code: 1");
    expect(result.goalAlreadyActive).toBe(true);
  });

  it("detects goal already active for unfinished-goal wording", () => {
    const result = detectNonRetryableHarnessGuard("cannot create a new goal because this thread has an unfinished goal; complete the existing goal first");
    expect(result.goalAlreadyActive).toBe(true);
  });

  it("detects goal already active for unfinished-goal wording (JSON-wrapped)", () => {
    const result = detectNonRetryableHarnessGuard('{"type":"error","message":"cannot create a new goal because this thread has an unfinished goal; complete the existing goal first"}');
    expect(result.goalAlreadyActive).toBe(true);
  });

  it("detects raw ai_credits_limit_exceeded API error type", () => {
    const result = detectNonRetryableHarnessGuard("ai_credits_limit_exceeded");
    expect(result.aiCreditsExceeded).toBe(true);
    expect(result.awfAPIProxyBlockingRequests).toBe(false);
  });

  it("detects ai_credits_limit_exceeded embedded in JSON stream-json result", () => {
    const result = detectNonRetryableHarnessGuard(
      '{"type":"result","subtype":"error_api_error","is_error":true,"result":"Error 403: {\\"type\\":\\"ai_credits_limit_exceeded\\",\\"message\\":\\"You have exceeded your AI credits budget.\\"}"}'
    );
    expect(result.aiCreditsExceeded).toBe(true);
  });

  it("detects ai_credits_limit_exceeded in Anthropic error JSON", () => {
    const result = detectNonRetryableHarnessGuard('{"type":"error","error":{"type":"ai_credits_limit_exceeded","message":"AI credits budget exceeded"}}');
    expect(result.aiCreditsExceeded).toBe(true);
  });

  it("detects max_runs_exceeded by JSON error type", () => {
    const result = detectNonRetryableHarnessGuard('{"error":{"type":"max_runs_exceeded","message":"Maximum LLM invocations exceeded (20 / 20).","invocation_count":20,"max_runs":20}}');
    expect(result.maxRunsExceeded).toBe(true);
    expect(result.aiCreditsExceeded).toBe(false);
  });

  it("detects max_runs_exceeded by human-readable message", () => {
    const result = detectNonRetryableHarnessGuard("Failed to authenticate. API Error: 403 Maximum LLM invocations exceeded (20 / 20).");
    expect(result.maxRunsExceeded).toBe(true);
  });

  it("does not falsely detect max_runs_exceeded for unrelated output", () => {
    const result = detectNonRetryableHarnessGuard("transient network timeout");
    expect(result.maxRunsExceeded).toBe(false);
  });

  it("isMaxRunsExceededError matches max_runs_exceeded JSON signatures", () => {
    expect(isMaxRunsExceededError('{"error":{"type":"max_runs_exceeded"}}')).toBe(true);
  });

  it("isMaxRunsExceededError matches human-readable invocation-cap signatures", () => {
    expect(isMaxRunsExceededError("CAPIError: 429 Maximum LLM invocations exceeded (25/25)")).toBe(true);
  });

  it("isMaxRunsExceededError ignores unrelated output", () => {
    expect(isMaxRunsExceededError("transient network timeout")).toBe(false);
  });

  it("isAuthenticationFailedError returns true for Anthropic-direct auth failure with request ID", () => {
    expect(isAuthenticationFailedError("Authentication failed (Request ID: C818:3ED713:19D401B:1C446B7:69D653CA)")).toBe(true);
  });

  it("isAuthenticationFailedError returns true for bare Authentication failed", () => {
    expect(isAuthenticationFailedError("Authentication failed")).toBe(true);
  });

  it('isAuthenticationFailedError returns true for Claude Code stream-JSON "error":"authentication_failed" field', () => {
    const jsonLine = JSON.stringify({ type: "result", error: "authentication_failed" });
    expect(isAuthenticationFailedError(jsonLine)).toBe(true);
  });

  it('isAuthenticationFailedError returns true for Claude Code "not logged in" message (case-insensitive)', () => {
    expect(isAuthenticationFailedError("Not logged in · Please run /login")).toBe(true);
    expect(isAuthenticationFailedError("NOT LOGGED IN")).toBe(true);
  });

  it("isAuthenticationFailedError returns false for unrelated output", () => {
    expect(isAuthenticationFailedError("No authentication information found")).toBe(false);
    expect(isAuthenticationFailedError("rate_limit_error")).toBe(false);
    expect(isAuthenticationFailedError("")).toBe(false);
  });

  it("isAuthenticationFailedError returns false for non-string input", () => {
    expect(isAuthenticationFailedError(null)).toBe(false);
    expect(isAuthenticationFailedError(undefined)).toBe(false);
  });
});

describe("buildSoftTimeoutGuard", () => {
  it("returns null when GH_AW_TIMEOUT_MINUTES is missing", () => {
    expect(buildSoftTimeoutGuard(1_000, {})).toBeNull();
  });

  it("returns null for zero timeout", () => {
    expect(buildSoftTimeoutGuard(1_000, { GH_AW_TIMEOUT_MINUTES: "0" })).toBeNull();
  });

  it("returns null for negative timeout", () => {
    expect(buildSoftTimeoutGuard(1_000, { GH_AW_TIMEOUT_MINUTES: "-5" })).toBeNull();
  });

  it("returns null for NaN timeout", () => {
    expect(buildSoftTimeoutGuard(1_000, { GH_AW_TIMEOUT_MINUTES: "NaN" })).toBeNull();
  });

  it("returns null for non-numeric timeout", () => {
    expect(buildSoftTimeoutGuard(1_000, { GH_AW_TIMEOUT_MINUTES: "abc" })).toBeNull();
  });

  it("computes a deadline before hard timeout", () => {
    const guard = buildSoftTimeoutGuard(10_000, { GH_AW_TIMEOUT_MINUTES: "15" });
    expect(guard).toEqual({
      timeoutMinutes: 15,
      softDeadlineMs: 820000,
    });
  });

  it("clamps deadline to start+1000ms when timeout is shorter than the buffer", () => {
    // 1 minute (60_000ms) < SOFT_TIMEOUT_BUFFER_MS (90_000ms): deadline should be clamped to start + 1000ms
    const guard = buildSoftTimeoutGuard(10_000, { GH_AW_TIMEOUT_MINUTES: "1" });
    expect(guard).not.toBeNull();
    expect(guard).toEqual({
      timeoutMinutes: 1,
      softDeadlineMs: 11_000,
    });
  });
});
