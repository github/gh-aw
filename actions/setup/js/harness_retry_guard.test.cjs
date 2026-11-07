// @ts-check

import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const {
  detectNonRetryableHarnessGuard,
  detectExhaustedAWFInvocationBudget,
  buildSoftTimeoutGuard,
  isMaxRunsExceededError,
  isAuthenticationFailedError,
  parseAICreditsExceededProxyRejection,
  parseAPIProxyGuardRejection,
} = require("./harness_retry_guard.cjs");
const { parseModelEndpointMismatch, MODEL_ENDPOINT_MISMATCH_PATTERN, shouldSuppressGuardCategory } = require("./harness_error_patterns.cjs");

describe("harness_retry_guard.cjs", () => {
  describe("detectExhaustedAWFInvocationBudget", () => {
    const options = { nativeTerminalRateLimitError: true, env: { AWF_REFLECT_ENABLED: "1" } };
    const exhaustedReflect = {
      ok: true,
      reflectData: { runs: { enabled: true, max_runs: 60, invocation_count: 60, remaining_runs: 0 } },
    };

    it("confirms exhaustion only from a fresh, complete reflection", async () => {
      const fetchReflect = async () => exhaustedReflect;
      await expect(detectExhaustedAWFInvocationBudget({ ...options, fetchReflect })).resolves.toEqual({ invocationCount: 60, maxRuns: 60 });
    });

    it.each([
      { ok: false, reflectData: exhaustedReflect.reflectData },
      { ok: true, reflectData: { runs: { enabled: false, max_runs: 60, invocation_count: 60, remaining_runs: 0 } } },
      { ok: true, reflectData: { runs: { enabled: true, max_runs: 60, invocation_count: 59, remaining_runs: 1 } } },
      { ok: true, reflectData: { runs: { enabled: true, max_runs: 60, invocation_count: 60 } } },
      { ok: true, reflectData: { runs: { enabled: true, max_runs: "60", invocation_count: 60, remaining_runs: 0 } } },
    ])("preserves fallback behavior when reflection is incomplete or does not show exhaustion", async reflection => {
      const fetchReflect = async () => reflection;
      await expect(detectExhaustedAWFInvocationBudget({ ...options, fetchReflect })).resolves.toBeNull();
    });

    it("does not fetch reflection without a native terminal 429 or enabled AWF reflection", async () => {
      const fetchReflect = async () => {
        throw new Error("must not fetch");
      };
      await expect(detectExhaustedAWFInvocationBudget({ ...options, nativeTerminalRateLimitError: false, fetchReflect })).resolves.toBeNull();
      await expect(detectExhaustedAWFInvocationBudget({ ...options, env: { AWF_REFLECT_ENABLED: "0" }, fetchReflect })).resolves.toBeNull();
    });

    it("preserves fallback behavior when fresh reflection throws", async () => {
      const fetchReflect = async () => {
        throw new Error("reflection unavailable");
      };
      await expect(detectExhaustedAWFInvocationBudget({ ...options, fetchReflect })).resolves.toBeNull();
    });
  });

  it.each([
    "400 Cannot translate Copilot request feature: tool_search",
    "400 Unsupported Responses custom tool: web_search",
    '{"error":{"type":"model_endpoint_incompatible","model":"gpt-5","endpoint":"/responses"}}',
    "Model 'gpt-5' is not accessible via the /v1/responses endpoint.",
  ])("detects model endpoint mismatch: %s", message => {
    const parsed = parseModelEndpointMismatch(`unrelated first line\n${message}\nunrelated last line`);
    expect(parsed?.detail).toBe(message);
    expect(detectNonRetryableHarnessGuard(message).modelEndpointMismatch).toEqual(parsed);
  });

  it("extracts the model and endpoint from matching text and JSON", () => {
    expect(parseModelEndpointMismatch("Model 'gpt-5' is not accessible via the /v1/responses endpoint.")).toEqual({
      model: "gpt-5",
      endpoint: "/v1/responses",
      detail: "Model 'gpt-5' is not accessible via the /v1/responses endpoint.",
    });
    const message = '{"type":"model_endpoint_incompatible","model":"gpt-5","endpoint":"/responses"}';
    expect(parseModelEndpointMismatch(message)).toEqual({ model: "gpt-5", endpoint: "/responses", detail: message });
    expect(parseModelEndpointMismatch("Unsupported Responses custom tool: web_search")?.endpoint).toBe("responses");
    expect(parseModelEndpointMismatch("Model gpt-5 is not accessible via the Chat Completions API endpoint.")?.endpoint).toBe("Chat Completions API");
    expect(parseModelEndpointMismatch("Cannot translate Copilot request feature: tool_search")).toEqual({ detail: "Cannot translate Copilot request feature: tool_search" });
  });

  it("extracts model and endpoint from an escaped structured provider error", () => {
    const message = JSON.stringify({ type: "error", message: JSON.stringify({ type: "model_endpoint_incompatible", model: "gpt-5", endpoint: "/responses" }) });
    expect(parseModelEndpointMismatch(message)).toEqual({ model: "gpt-5", endpoint: "/responses", detail: message });
    expect(parseModelEndpointMismatch("cannot translate copilot request feature: tool_search")).not.toBeNull();
  });

  it("captures the actual routing model and endpoint", () => {
    const detail = 'Routing model "gpt-5.6-luna" to /responses is incompatible';
    expect(parseModelEndpointMismatch(detail)).toEqual({ model: "gpt-5.6-luna", endpoint: "/responses", detail });
    expect(parseModelEndpointMismatch(`model_endpoint_incompatible: ${detail}`)?.endpoint).toBe("/responses");
  });

  it("redacts credentials and registered masks before bounding matching details", () => {
    const token = "ghp_" + "A".repeat(36);
    const output = `::add-mask::private-secret-value\nAuthorization: ${"Bearer"} ${"arbitrary-" + "credential"} ${token} private-secret-value Cannot translate Copilot request feature: tool_search`;
    const detail = parseModelEndpointMismatch(output)?.detail;
    expect(detail).toContain("Cannot translate Copilot request feature");
    expect(detail).not.toContain(token);
    expect(detail).not.toContain("arbitrary-credential");
    expect(detail).not.toContain("private-secret-value");
    expect(detail).toContain("***");
  });

  it("bounds details around the actual mismatch rather than the first log line", () => {
    const message = "Cannot translate Copilot request feature";
    const parsed = parseModelEndpointMismatch(`unrelated\n${"x".repeat(2000)} ${message}: ${"y".repeat(2000)}\ntrailing`);
    expect(parsed?.detail.length).toBeLessThanOrEqual(1000);
    expect(parsed?.detail).toContain(message);
    expect(parsed?.detail).not.toContain("unrelated");
    expect(parsed?.detail).not.toContain("trailing");
  });

  it("does not mask endpoint mismatch with a generic model policy rejection", () => {
    const message = '{"type":"model_policy_violation","message":"model_endpoint_incompatible: model gpt-5 is not accessible via the responses endpoint"}';
    expect(detectNonRetryableHarnessGuard(message).modelEndpointMismatch).not.toBeNull();
    expect(parseAPIProxyGuardRejection(message)).toBeNull();
    expect(parseAPIProxyGuardRejection('{"type":"model_policy_violation"}')?.guard).toBe("model_policy_violation");
  });

  it("shares diagnostic-scoped model policy precedence without suppressing other guards", () => {
    expect(shouldSuppressGuardCategory("model_policy_violation", "model_endpoint_incompatible")).toBe(true);
    expect(shouldSuppressGuardCategory("model_policy_violation", "request rejected")).toBe(false);
    expect(shouldSuppressGuardCategory("effective_tokens_limit_exceeded", "model_endpoint_incompatible")).toBe(false);
  });

  it.each(["error", "turn.failed", "session.error", "model_policy_violation"])("retains independent model policy rejection across %s transport records", type => {
    const messages = ["model_policy_violation", "model_policy_violation: model_endpoint_incompatible"];
    const records = messages.map(message => (type === "session.error" ? { type, data: { message } } : type === "turn.failed" ? { type, error: { message } } : { type, message }));
    for (const ordered of [records, [...records].reverse()]) {
      expect(parseAPIProxyGuardRejection(ordered.map(JSON.stringify).join("\n"))).toEqual({ guard: "model_policy_violation", counters: {} });
    }
    expect(parseAPIProxyGuardRejection(JSON.stringify(records[1]))).toBeNull();
  });

  it.each(["400 Bad Request", "400 invalid input", "normal output", "", null, undefined])("does not detect unrelated output: %s", output => {
    expect(parseModelEndpointMismatch(output)).toBeNull();
    expect(detectNonRetryableHarnessGuard(output).modelEndpointMismatch).toBeNull();
  });

  it("does not match empty strings or join endpoint phrases across lines", () => {
    expect(MODEL_ENDPOINT_MISMATCH_PATTERN.test("")).toBe(false);
    expect(parseModelEndpointMismatch("not accessible via the responses\nendpoint")).toBeNull();
  });

  it.each([
    { type: "turn.failed", error: { message: "unexpected status 403 Forbidden: Maximum AI credits exceeded (301 / 300)." } },
    { type: "error", message: "unexpected status 403 Forbidden: Maximum AI credits exceeded (301 / 300)." },
    { type: "turn.failed", error: { message: JSON.stringify({ error: { message: "403 Maximum AI credits exceeded (301 / 300)." } }) } },
  ])("trusts the proxy rejection in a native Codex error envelope", event => {
    expect(parseAICreditsExceededProxyRejection(JSON.stringify(event))).toEqual({ aiCredits: 301, maxAICredits: 300 });
  });

  it("does not trust the same wording in an ordinary Codex assistant item", () => {
    expect(parseAICreditsExceededProxyRejection(JSON.stringify({ type: "item.completed", item: { type: "agent_message", text: "403 Maximum AI credits exceeded (301 / 300)." } }))).toBeNull();
  });

  it("detects the AWF API proxy cache-miss guardrail in text form", () => {
    const output = "API Error: 403 Maximum consecutive cache misses exceeded (5 / 5).";
    expect(parseAPIProxyGuardRejection(output)).toBeNull();
    expect(detectNonRetryableHarnessGuard(output).apiProxyGuardRejection).toBeNull();
  });

  it("detects the structured api-proxy guardrail error types", () => {
    expect(parseAPIProxyGuardRejection('{"type":"max_cache_misses_exceeded"}')?.guard).toBe("max_cache_misses_exceeded");
    expect(parseAPIProxyGuardRejection('{"type":"effective_tokens_limit_exceeded"}')?.guard).toBe("effective_tokens_limit_exceeded");
    expect(parseAPIProxyGuardRejection('{"type":"permission_denied_limit_exceeded"}')?.guard).toBe("permission_denied_limit_exceeded");
    expect(parseAPIProxyGuardRejection('{"type":"model_policy_violation"}')?.guard).toBe("model_policy_violation");
  });

  it.each([
    { type: "turn.failed", error: { message: "403 Maximum consecutive cache misses exceeded (5 / 5)." } },
    { type: "session.error", data: { message: "403 Maximum consecutive cache misses exceeded (5 / 5)." } },
    { type: "assistant", is_api_error_message: true, message: { content: [{ type: "text", text: "403 Maximum consecutive cache misses exceeded (5 / 5)." }] } },
    { type: "result", is_error: true, result: "API Error: 403 Maximum consecutive cache misses exceeded (5 / 5)." },
    { type: "result", is_error: true, errors: ["API Error: 403 Maximum consecutive cache misses exceeded (5 / 5)."] },
  ])("preserves transport guard diagnostics in native engine envelopes: %j", record => {
    expect(parseAPIProxyGuardRejection(JSON.stringify(record))).toEqual({ guard: "max_cache_misses_exceeded", counters: { consecutive_cache_misses: 5, max_cache_misses: 5 } });
  });

  it("ignores successful Claude result diagnostics", () => {
    expect(parseAPIProxyGuardRejection(JSON.stringify({ type: "result", is_error: false, result: "Maximum consecutive cache misses exceeded (5 / 5)." }))).toBeNull();
  });

  it.each(["max_cache_misses_exceeded", "effective_tokens_limit_exceeded", "permission_denied_limit_exceeded", "model_policy_violation"])("ignores quoted %s guard names in model and tool content", guard => {
    const records = [
      { type: "item.completed", item: { type: "command_execution", aggregated_output: `GH_AW_FAILURE_CATEGORIES: ${guard}` } },
      { type: "item.completed", item: { type: "agent_message", text: `403 ${guard}` } },
      { type: "assistant", message: { content: [{ type: "text", text: `403 ${guard}` }] } },
      { type: "tool_result", content: `403 ${guard}` },
      { type: "error", message: "connection reset", metadata: { quoted: guard } },
      { type: "turn.failed", error: { message: "connection reset", metadata: { quoted: guard } } },
      { type: "error", message: JSON.stringify({ message: "connection reset", metadata: { quoted: guard } }) },
      { type: "result", is_error: true, result: "connection reset", permission_denials: [{ tool_input: { command: guard } }] },
      { type: "assistant", is_api_error_message: true, error: "api_error", message: { content: [{ type: "tool_use", input: { command: guard } }] } },
      { type: "turn_end", message: { errorMessage: "connection reset", content: [{ type: "text", text: guard }] } },
    ];
    for (const record of records) {
      expect(parseAPIProxyGuardRejection(JSON.stringify(record))).toBeNull();
    }
    const error = { type: "turn.failed", error: { message: "unexpected status 403 Forbidden: Maximum AI credits exceeded (1.028578 / 1)." } };
    const output = [...records, error].map(JSON.stringify).join("\n");
    expect(parseAPIProxyGuardRejection(output)).toBeNull();
    expect(parseAICreditsExceededProxyRejection(output)).toEqual({ aiCredits: 1.028578, maxAICredits: 1 });
  });

  it("ignores plain-text and untyped JSON proxy guard claims", () => {
    const message = "403 Maximum consecutive cache misses exceeded (5 / 5).";
    expect(parseAPIProxyGuardRejection(message)).toBeNull();
    expect(parseAPIProxyGuardRejection(JSON.stringify({ error: message }))).toBeNull();
    expect(parseAPIProxyGuardRejection(JSON.stringify({ error: { message } }))).toBeNull();
    expect(parseAPIProxyGuardRejection(JSON.stringify({ code: "max_cache_misses_exceeded" }))).toBeNull();
    expect(parseAPIProxyGuardRejection(JSON.stringify({ is_api_error_message: true, message }))).toBeNull();
  });

  it("ignores a guard claim in a byte-truncated tool record beyond the 4 MiB collection limit", () => {
    const outputLimit = 4 * 1024 * 1024;
    const completeRecord = JSON.stringify({
      type: "item.completed",
      item: { type: "command_execution", aggregated_output: `${"x".repeat(outputLimit)}Maximum consecutive cache misses exceeded (5 / 5).` },
    });
    const collectedTail = Buffer.from(completeRecord).subarray(-outputLimit).toString("utf8");
    expect(collectedTail.startsWith("{")).toBe(false);
    expect(parseAPIProxyGuardRejection(collectedTail)).toBeNull();
  });

  it("does not report a proxy guardrail for a genuine credential failure", () => {
    const output = "Authentication failed with provider at http://172.30.0.30:10002 (HTTP 403).";
    expect(parseAPIProxyGuardRejection(output)).toBeNull();
    expect(detectNonRetryableHarnessGuard(output).apiProxyGuardRejection).toBeNull();
    expect(isAuthenticationFailedError(output)).toBe(true);
  });

  it.each([
    { type: "unknown_model_ai_credits", model: "gpt-6.1-sol" },
    { type: "error", message: JSON.stringify({ type: "unknown_model_ai_credits", model: "gpt-6.1-sol" }) },
    { type: "turn.failed", error: { message: JSON.stringify({ type: "unknown_model_ai_credits", model: "gpt-6.1-sol" }) } },
    { type: "turn.failed", error: { message: JSON.stringify({ error: { code: "unknown_model_ai_credits" } }) } },
  ])("treats missing model pricing as a terminal proxy rejection: %j", event => {
    const output = JSON.stringify(event);
    expect(parseAPIProxyGuardRejection(output)).toEqual({ guard: "unknown_model_ai_credits", counters: {} });
    expect(detectNonRetryableHarnessGuard(output).apiProxyGuardRejection?.guard).toBe("unknown_model_ai_credits");
  });

  it.each([
    "",
    "unknown_model_ai_credits",
    '{"type":"error","message":"unknown_model_ai_credits"}',
    '{"type":"error","message":"{broken"}',
    JSON.stringify({ type: "item.completed", item: { type: "agent_message", text: '{"type":"unknown_model_ai_credits"}' } }),
    JSON.stringify({ type: "item.completed", item: { type: "mcp_tool_call", result: { type: "unknown_model_ai_credits" } } }),
    JSON.stringify({ type: "assistant", message: { type: "unknown_model_ai_credits" } }),
    JSON.stringify({ type: "turn.failed", error: { message: "Model has no pricing; unknown_model_ai_credits" } }),
  ])("does not classify quoted or malformed pricing errors as transport rejections: %s", output => {
    expect(parseAPIProxyGuardRejection(output)).toBeNull();
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
    expect(parseAICreditsExceededProxyRejection('{"type":"error","message":"API Error: 403 Maximum AI credits exceeded (12 / 300)."}')).toBeNull();
  });

  it("accepts a proxy 403 usage pair that exactly reaches the budget", () => {
    expect(parseAICreditsExceededProxyRejection('{"type":"error","message":"API Error: 403 Maximum AI credits exceeded (300 / 300)."}')).toEqual({
      aiCredits: 300,
      maxAICredits: 300,
    });
  });

  it("ignores untyped JSON credit rejection claims", () => {
    expect(parseAICreditsExceededProxyRejection('{"error":"authentication_failed","message":"API Error: 403 Maximum AI credits exceeded (1.02 / 1)."}')).toBeNull();
    expect(parseAICreditsExceededProxyRejection('{"is_api_error_message":true,"message":"API Error: 403 Maximum AI credits exceeded (1.02 / 1)."}')).toBeNull();
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

  it.each([
    { type: "tool_result", error: "403 Maximum AI credits exceeded (1.02 / 1)." },
    { type: "assistant", error: "403 Maximum AI credits exceeded (1.02 / 1)." },
    { type: "error", message: "403 Forbidden", metadata: "403 Maximum AI credits exceeded (1.02 / 1)." },
    { type: "turn.failed", error: { message: "403 Forbidden", metadata: "403 Maximum AI credits exceeded (1.02 / 1)." } },
  ])("does not trust a credit rejection quoted outside the transport diagnostic: %j", record => {
    expect(parseAICreditsExceededProxyRejection(JSON.stringify(record))).toBeNull();
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
