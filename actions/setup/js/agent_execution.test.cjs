import { describe, expect, it } from "vitest";
import { collectAgentExecution, parseAgentExitCode, validateAgentExecution } from "./agent_execution.cjs";
import { success, failure } from "./fixtures/claude_ci_sessions.cjs";
import { normalizeClaudeSession } from "./claude_session.cjs";

describe("unique agent execution observation", () => {
  it("mines the existing Claude API-error/retry run without counting tool failures", () => {
    const events = normalizeClaudeSession(failure);
    const execution = collectAgentExecution({ content: failure.map(JSON.stringify).join("\n"), events });
    expect(execution.data.errorCodes).toEqual([502]);
    expect(execution.data.errorTypes).toEqual(["UnknownError", "api_error", "server_error"]);
    expect(execution.data).not.toHaveProperty("exitCode");
    expect(collectAgentExecution({ content: success.map(JSON.stringify).join("\n"), events: normalizeClaudeSession(success) })).toBeUndefined();
  });

  it("retains distinct native codes and types from nested Codex provider errors", () => {
    const record = { type: "turn.failed", error: { message: JSON.stringify({ error: { type: "invalid_request_error", code: 400, message: "The requested model is not supported" } }) } };
    expect(collectAgentExecution({ content: JSON.stringify(record), exitCode: 1 }).data).toEqual({
      categories: ["model_not_supported_error"],
      errorCodes: [400],
      errorTypes: ["invalid_request_error"],
      exitCode: 1,
    });
  });

  it("deduplicates persisted and inferred diagnostics but uses the recorded final exit", () => {
    const observation = { categories: ["agentic_engine_timeout"], errorCodes: [0, "0"], errorTypes: ["provider_error"], exitCode: 1 };
    const content = ["[claude-harness] attempt 1: failed exitCode=1 (failure_reason=harness_retry_path_invalid)", "[claude-harness] done: exitCode=1", "[claude-harness] done: exitCode=0"].join("\n");
    expect(collectAgentExecution({ content, observations: [observation, observation], exitCode: 0 }).data).toEqual({ ...observation, categories: ["agentic_engine_timeout", "harness_retry_path_invalid"], exitCode: 0 });
    expect(collectAgentExecution({ content }).data.exitCode).toBe(0);
    expect(collectAgentExecution({ content, observations: [observation] }).data.exitCode).toBe(1);
  });

  it.each([
    ["Access denied by policy settings", "inference_access_error"],
    ["MCP servers were blocked by policy: 'github'", "mcp_policy_error"],
    ["Timeout after 100ms waiting for session.idle", "agentic_engine_timeout"],
    ["unknown model example", "model_not_supported_error"],
    ["400 Bad Request", "http_400_response_error"],
    ["CAPIError: 429 Too Many Requests", "capi_quota_exceeded_error"],
    ["Failed to get response from the AI model; retried 5 times. Last error: 503", "capi_server_error"],
    ["CAPIError: 429 Maximum LLM invocations exceeded (10/10)", "invocation_cap_exceeded"],
    ["Maximum consecutive cache misses exceeded (6/5)", "max_cache_misses_exceeded"],
    ['Model "example" has no AI credits pricing', "missing_model_pricing_error"],
    ["could enable arbitrary code execution. Please rewrite the command without these expansion patterns.", "shell_expansion_guard_rejected"],
    ["Authentication failed (Request ID: example)", "authentication_failed"],
    ["max_ai_credits_exceeded=true", "max_ai_credits_exceeded"],
    ["effective_tokens_limit_exceeded", "effective_tokens_limit_exceeded"],
    ["permission_denied_limit_exceeded", "permission_denied_limit_exceeded"],
    ["model_policy_violation", "model_policy_violation"],
    ["AWF API proxy blocking requests", "awf_api_proxy_blocking_requests"],
    ["This thread already has a goal. Use update_goal", "goal_already_active"],
  ])("retains the harness/compiler category for %s", (content, category) => {
    expect(collectAgentExecution({ content }).data.categories).toContain(category);
  });

  it("does not turn a post-result watchdog kill into a timeout", () => {
    const content = "[copilot-harness] process closed exitCode=0 signal=SIGTERM watchdogFired=true\n[copilot-harness] done: exitCode=0\n";
    expect(collectAgentExecution({ content }).data).toEqual({ categories: [], errorCodes: [], errorTypes: [], exitCode: 0 });
  });

  it("preserves invocation-cap precedence and runtime crashes", () => {
    expect(collectAgentExecution({ content: "CAPIError: 429 Too Many Requests\nmax_runs_exceeded" }).data.categories).toEqual(["invocation_cap_exceeded"]);
    expect(collectAgentExecution({ exitCode: 159 }).data).toEqual({ categories: ["sandbox_runtime_crash"], errorCodes: [], errorTypes: ["SIGSYS"], exitCode: 159 });
  });

  it("retains the observed HTTP code from legacy engine error messages", () => {
    expect(collectAgentExecution({ content: "400 Bad Request" }).data.errorCodes).toEqual([400]);
    expect(collectAgentExecution({ content: "CAPIError: 429 Too Many Requests" }).data.errorCodes).toEqual([429]);
  });

  it("does not reinterpret a canonical failed result status as a provider error code", () => {
    const event = { type: "session.result", data: { sourceType: "turn.failed", status: "failed", errors: [{ code: "model_not_supported", type: "invalid_request_error" }] } };
    expect(collectAgentExecution({ events: [event] }).data.errorCodes).toEqual(["model_not_supported"]);
  });

  it("ignores errors quoted in conversation and tool output", () => {
    const records = [
      { type: "assistant.message", data: { content: "CAPIError: 429 Too Many Requests" } },
      { type: "tool.execution_complete", data: { success: false, error: { code: 400, type: "tool_error" }, output: "max_runs_exceeded" } },
      { type: "user.message", data: { content: "Access denied by policy settings" } },
    ];
    expect(collectAgentExecution({ content: records.map(JSON.stringify).join("\n"), events: records })).toBeUndefined();
  });

  it("does not classify Codex fallback model metadata warnings as unsupported models", () => {
    const warning = "2026-10-03T04:25:37Z WARN codex_models_manager::model_info: Unknown model gpt-5.3-codex is used. This will use fallback model metadata.";
    expect(collectAgentExecution({ content: warning })).toBeUndefined();
    expect(collectAgentExecution({ content: warning + "\nunknown model unavailable" }).data.categories).toEqual(["model_not_supported_error"]);
  });

  it.each(["", "-1", "256", "1.5", "1 trailing", "NaN"])("rejects invalid recorded exit code %s", value => {
    expect(() => parseAgentExitCode(value)).toThrow("Invalid agent execution exit code");
  });
  it("preserves a recorded zero rather than inferring failure from a prior attempt", () => {
    expect(parseAgentExitCode(" 0\n")).toBe(0);
    expect(collectAgentExecution({ exitCode: 0 }).data.exitCode).toBe(0);
  });

  it.each([
    {},
    { categories: null, errorCodes: [], errorTypes: [] },
    { categories: [], errorCodes: [false], errorTypes: [] },
    { categories: [], errorCodes: [1, 1], errorTypes: [] },
    { categories: [], errorCodes: [], errorTypes: [null] },
    { categories: [], errorCodes: [], errorTypes: [], exitCode: null },
  ])("rejects malformed execution payloads", data => {
    expect(() => validateAgentExecution(data)).toThrow("agent.execution");
  });
});
