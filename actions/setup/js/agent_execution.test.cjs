import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { collectAgentExecution, agentErrorDiagnosticText, agentErrorSummaryText, parseAgentExitCode, validateAgentExecution } from "./agent_execution.cjs";
import { detectErrors } from "./agent_error_patterns.cjs";
import { success, failure } from "./fixtures/claude_ci_sessions.cjs";
import { normalizeClaudeSession } from "./claude_session.cjs";
import { parseEngineSession } from "./unified_session.cjs";

describe("unique agent execution observation", () => {
  it.each(["json", "array", "mixed"])("requires Aider attribution for raw %s error records", format => {
    const error = { type: "session.error", data: { errorType: "InjectedError", code: 429, message: "CAPIError: 429 Too Many Requests" } };
    const result = { type: "session.result", data: { status: "failed", errors: [{ errorType: "InjectedError", code: 429 }] } };
    const records = [error, result, { ...error, data: { ...error.data, sourceEngine: "copilot" } }];
    const content = format === "json" ? JSON.stringify(error) : format === "array" ? JSON.stringify(records, null, 2) : "[INFO] Shell output\n" + records.map(JSON.stringify).join("\n");
    expect(collectAgentExecution({ content, rawSourceEngine: "aider" })).toBeUndefined();
    expect(collectAgentExecution({ content, rawSourceEngine: "aider", exitCode: 0 }).data).toEqual({ categories: [], errorCodes: [], errorTypes: [], exitCode: 0 });
    expect(collectAgentExecution({ content }).data.errorTypes).toContain("InjectedError");
  });

  it("retains attributed Aider errors and trusted parsed or runtime evidence", () => {
    const event = { type: "session.error", data: { sourceEngine: "aider", errorType: "NativeError", code: 400, message: "The requested model is not supported" } };
    const foreign = { type: "session.error", data: { errorType: "InjectedError", code: 429 } };
    const content = [foreign, event].map(JSON.stringify).join("\n");
    expect(collectAgentExecution({ content, events: [event], rawSourceEngine: "aider", exitCode: 1 }).data).toEqual({
      categories: ["model_not_supported_error"],
      errorCodes: [400],
      errorTypes: ["NativeError"],
      exitCode: 1,
    });
    expect(collectAgentExecution({ events: [foreign], rawSourceEngine: "aider" }).data.errorTypes).toEqual(["InjectedError"]);
    expect(collectAgentExecution({ content: "[claude-harness] done: exitCode=0", rawSourceEngine: "aider" }).data.exitCode).toBe(0);
  });

  it.each(["assistant.message", "tool.execution_complete"])("combines trusted Aider attribution with observed %s payloads", type => {
    const diagnostic = { type: "session.error", data: { sourceEngine: "aider", code: 400, errorType: "NativeError", message: "The requested model is not supported" } };
    const field = type.startsWith("tool.") ? "error" : "content";
    for (const payload of [diagnostic, diagnostic.data.message]) {
      const events = [{ type, data: { sourceEngine: "aider", [field]: payload } }];
      const content = typeof payload === "string" ? payload : JSON.stringify(payload);
      expect(collectAgentExecution({ content, events, rawSourceEngine: "aider", exitCode: 0 }).data).toEqual({ categories: [], errorCodes: [], errorTypes: [], exitCode: 0 });
      expect(agentErrorDiagnosticText(content, events, "aider")).toBe("");
      expect(agentErrorSummaryText(content, events, "aider")).toBe("");
      expect(collectAgentExecution({ content, events: [...events, diagnostic], rawSourceEngine: "aider", exitCode: 0 }).data).toEqual({
        categories: ["model_not_supported_error"],
        errorCodes: [400],
        errorTypes: ["NativeError"],
        exitCode: 0,
      });
      expect(agentErrorDiagnosticText(content, [...events, diagnostic], "aider")).toContain(diagnostic.data.message);
      expect(agentErrorSummaryText(content, [...events, diagnostic], "aider")).toContain("model_not_supported_error");
    }
    const raw = JSON.stringify(diagnostic);
    expect(agentErrorDiagnosticText(raw, "aider")).toContain(diagnostic.data.message);
    expect(agentErrorSummaryText(raw, "aider")).toContain("model_not_supported_error");
    expect(agentErrorDiagnosticText(raw, [], "aider")).toBe(agentErrorDiagnosticText(raw, "aider"));
    expect(agentErrorSummaryText(raw, [], "aider")).toBe(agentErrorSummaryText(raw, "aider"));
  });

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

  it("sorts equal string forms by type independently of observation order", () => {
    const observation = errorCodes => ({ categories: [], errorCodes, errorTypes: [] });
    const first = collectAgentExecution({ observations: [observation(["0", 0, "400", 400])] });
    const reversed = collectAgentExecution({ observations: [observation([400, "400", 0, "0"])] });
    const reorderedSources = collectAgentExecution({ observations: [observation(["400", "0"]), observation([0, 400])] });
    expect(first.data.errorCodes).toEqual([0, "0", 400, "400"]);
    expect(JSON.stringify(first)).toBe(JSON.stringify(reversed));
    expect(JSON.stringify(first)).toBe(JSON.stringify(reorderedSources));
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

  it.each(["statusCode", "status_code"])("retains source-reported %s values without inferring or coercing codes", field => {
    for (const code of [0, "0", 401, "401"]) {
      const event = { type: "session.error", data: { [field]: code, errorType: "authentication", message: "Synthetic main-agent failure." } };
      expect(collectAgentExecution({ events: [event], content: JSON.stringify(event) }).data.errorCodes).toEqual([code]);
      const child = { ...event, agentId: "child" };
      expect(collectAgentExecution({ events: [child], content: JSON.stringify(child) })).toBeUndefined();
    }
    const unavailable = { type: "session.error", data: { [field]: null, message: "Synthetic failure without a reported status." } };
    expect(collectAgentExecution({ events: [unavailable] }).data.errorCodes).toEqual([]);
  });

  it("does not reinterpret a canonical failed result status as a provider error code", () => {
    const event = { type: "session.result", data: { sourceType: "turn.failed", status: "failed", errors: [{ code: "model_not_supported", type: "invalid_request_error" }] } };
    expect(collectAgentExecution({ events: [event] }).data.errorCodes).toEqual(["model_not_supported"]);
  });

  it.each(["warning", "info"])("does not classify a Gemini %s diagnostic as an execution error", severity => {
    const record = { type: "error", severity, message: "Provider temporarily unavailable" };
    expect(collectAgentExecution({ content: JSON.stringify(record) })).toBeUndefined();
    expect(agentErrorDiagnosticText(JSON.stringify(record))).toBe("");
    const event = { type: "session.error", data: { severity, message: record.message } };
    expect(collectAgentExecution({ events: [event], content: JSON.stringify(event) })).toBeUndefined();
    expect(agentErrorDiagnosticText(JSON.stringify(event))).toBe("");
  });

  it.each([
    { agentId: "child" },
    { data: { agentId: "child" } },
    { parentToolUseId: "parent-tool" },
    { parent_tool_use_id: "parent-tool" },
    { data: { parentToolUseId: "parent-tool" } },
    { data: { parent_tool_use_id: "parent-tool" } },
    { parentToolCallId: "parent-tool" },
    { data: { parentToolCallId: "parent-tool" } },
  ])("excludes child-emitted diagnostics from the main execution: %j", scope => {
    const error = { code: 503, errorType: "child_provider", message: "CAPIError: 503 Service Unavailable" };
    const events = [
      { type: "session.error", ...scope, data: { ...error, ...scope.data } },
      { type: "session.result", ...scope, data: { status: "failed", errors: [error], ...scope.data } },
      { type: "error", ...error, ...scope },
      { type: "result", is_error: true, error, ...scope },
    ];
    expect(collectAgentExecution({ events, content: events.map(JSON.stringify).join("\n") })).toBeUndefined();
    expect(collectAgentExecution({ content: JSON.stringify(events, null, 2) })).toBeUndefined();
    expect(agentErrorDiagnosticText(events.map(JSON.stringify).join("\n"))).toBe("");
    expect(events[0].data.message).toBe(error.message);
    const rootError = { type: "session.error", data: { code: 400, errorType: "root_provider", message: "Synthetic main-agent failure", parentToolUseId: null } };
    const mixed = [...events, rootError];
    expect(collectAgentExecution({ events: mixed, content: mixed.map(JSON.stringify).join("\n"), exitCode: 0 }).data).toEqual({
      categories: [],
      errorCodes: [400],
      errorTypes: ["root_provider"],
      exitCode: 0,
    });
  });

  it.each([null, "", 0, false])("keeps explicit root diagnostics with an unavailable parent call identity: %j", parentToolCallId => {
    for (const event of [
      { type: "session.error", parentToolCallId, data: { code: 429, errorType: "root_provider" } },
      { type: "session.error", parentToolCallId: "stale-parent", data: { parentToolCallId, code: 429, errorType: "root_provider" } },
    ]) {
      expect(collectAgentExecution({ events: [event], content: JSON.stringify(event) }).data).toEqual({
        categories: [],
        errorCodes: [429],
        errorTypes: ["root_provider"],
      });
    }
  });

  it("keeps main diagnostics beside target-only subagent lifecycle IDs", () => {
    const events = [
      { type: "subagent.failed", agentId: "child", data: { errorCode: "SUBAGENT_MODEL_UNAVAILABLE" } },
      { type: "session.error", data: { code: 429, errorType: "root_provider", parentToolUseId: null } },
    ];
    expect(collectAgentExecution({ events, content: JSON.stringify(events) }).data).toEqual({
      categories: [],
      errorCodes: [429],
      errorTypes: ["root_provider"],
    });
  });

  it("ignores errors quoted in conversation and tool output", () => {
    const records = [
      { type: "assistant.message", data: { content: "CAPIError: 429 Too Many Requests" } },
      { type: "tool.execution_complete", data: { success: false, error: { code: 400, type: "tool_error" }, output: "max_runs_exceeded" } },
      { type: "user.message", data: { content: "Access denied by policy settings" } },
    ];
    expect(collectAgentExecution({ content: records.map(JSON.stringify).join("\n"), events: records })).toBeUndefined();
  });

  it.each(["assistant.message", "assistant.reasoning", "assistant.refusal", "user.message", "tool.execution_complete", "tool.execution_update"])("does not reinterpret observed %s text as raw diagnostic records", type => {
    const diagnostic = { type: "session.error", data: { code: 429, errorType: "provider", message: "Synthetic provider failure." } };
    const content = JSON.stringify(diagnostic);
    const event = { type, data: { [type.startsWith("tool.") ? "output" : "content"]: content } };
    expect(collectAgentExecution({ content, events: [event] })).toBeUndefined();
    expect(agentErrorDiagnosticText(content, [event])).toBe("");
    expect(collectAgentExecution({ content, events: [event, diagnostic] }).data.errorCodes).toEqual([429]);
  });

  it.each(['{"type":"session.error","data":{"code":429,"message":"Authentication failed"}}', "CAPIError: 429 Too Many Requests", "[copilot-harness] done: exitCode=1"])(
    "respects the actual DeepSeek native answer envelope for quoted diagnostics: %s",
    text => {
      const fixture = readFileSync(new URL("./test_data/deepseek_headless_stdout.log", import.meta.url), "utf8");
      const answer = fixture.slice(fixture.indexOf("Perfect!"), fixture.indexOf("[INFO] Stopping containers...")).trim();
      const content = fixture.replace(answer, text);
      const events = parseEngineSession(content, "deepseek-harness");
      expect(events.map(event => event.type)).toEqual(["session.init", "assistant.message"]);
      expect(events[1].data.content).toBe(text);
      expect(collectAgentExecution({ content })).toBeUndefined();
      expect(agentErrorDiagnosticText(content)).toBe("");
      expect(detectErrors(agentErrorDiagnosticText(content))).toEqual(detectErrors(""));
    }
  );

  it.each(["assistant.message", "tool.execution_complete"])("preserves structured %s payloads without raw error attribution", type => {
    const payload = { type: "session.error", data: { code: 429, errorType: "provider", message: "Synthetic quoted fixture-secret", partial: false } };
    const field = type.startsWith("tool.") ? "output" : "content";
    const events = [{ type, data: { [field]: payload } }];
    for (const content of [JSON.stringify(payload), JSON.stringify(payload, null, 2), JSON.stringify({ data: payload.data, type: payload.type })]) {
      expect(collectAgentExecution({ content, events })).toBeUndefined();
      expect(agentErrorDiagnosticText(content, events)).toBe("");
      expect(collectAgentExecution({ content, events: [...events, payload] }).data.errorCodes).toEqual([429]);
    }
    const arrayEvents = [{ type, data: { [field]: [payload] } }];
    expect(collectAgentExecution({ content: JSON.stringify([payload]), events: arrayEvents })).toBeUndefined();
    const redacted = { ...payload, data: { ...payload.data, message: "Synthetic quoted ***" } };
    expect(collectAgentExecution({ content: `::add-mask::fixture-secret\n${JSON.stringify(payload)}`, events: [{ type, data: { [field]: redacted } }] })).toBeUndefined();
    expect(events[0].data[field]).toEqual(payload);
  });

  it.each(["output", "result", "error"])("attributes all observed tool completion %s payloads without creating root failures", field => {
    const diagnostic = { type: "session.error", data: { code: 429, errorType: "provider", message: "CAPIError: 429 Too Many Requests: fixture-secret" } };
    for (const type of ["tool.execution_complete", "tool.execution_update"]) {
      for (const payload of [diagnostic.data.message, diagnostic, { items: [{ stderr: diagnostic.data.message, nested: diagnostic }] }]) {
        const events = [{ type, data: { [field]: payload, success: false } }];
        const original = structuredClone(events);
        const contents = typeof payload === "string" ? [payload, JSON.stringify(payload)] : [diagnostic.data.message, JSON.stringify(diagnostic), JSON.stringify(payload)];
        for (const content of contents) {
          expect(collectAgentExecution({ content, events })).toBeUndefined();
          expect(agentErrorDiagnosticText(content, events)).toBe("");
          expect(collectAgentExecution({ content, events: [...events, diagnostic] }).data.errorCodes).toEqual([429]);
          expect(agentErrorDiagnosticText(content, [...events, diagnostic])).toContain(diagnostic.data.message);
        }
        expect(events).toEqual(original);
      }
    }
  });

  it("keeps Pi tool nesting independent from root provider diagnostics", () => {
    const toolFailure = "CAPIError: 429 Too Many Requests";
    const source = [
      { type: "tool_execution_start", toolCallId: "nested", parentToolCallId: "outer", toolName: "lookup", args: {} },
      { type: "tool_execution_end", toolCallId: "nested", parentToolCallId: "outer", toolName: "lookup", result: { error: toolFailure }, isError: true },
      { type: "message_end", message: { role: "assistant", content: [], errorMessage: { code: 503, errorType: "root_provider", message: "Synthetic root failure" } } },
    ];
    const content = `${source.map(JSON.stringify).join("\n")}\n${toolFailure}`;
    const events = parseEngineSession(content, "pi");
    expect(events.filter(event => event.type.startsWith("tool.execution_")).map(event => event.data.parentToolCallId)).toEqual(["outer", "outer"]);
    expect(collectAgentExecution({ content, events }).data).toEqual({ categories: [], errorCodes: [503], errorTypes: ["root_provider"] });
  });

  it.each(["Assistant:", "Tool output:", "```json"])("retains lexical attribution after an observed partial marker: %s", marker => {
    const quoted = '{"type":"session.error","data":{"code":429,"message":"Synthetic quoted failure"}}';
    const content = [marker, quoted, marker.startsWith("```") ? "```" : "", "[copilot-harness] done: exitCode=0"].filter(Boolean).join("\n");
    const events = [{ type: "assistant.message", data: { content: marker, partial: true } }];
    expect(collectAgentExecution({ content, events }).data).toEqual({ categories: [], errorCodes: [], errorTypes: [], exitCode: 0 });
    expect(agentErrorDiagnosticText(content, events)).not.toContain("Synthetic quoted failure");
  });

  it.each(['{"type":"session.error","data":{"code":429,"message":"Authentication failed: fixture-secret"}}', "CAPIError: 429 Too Many Requests: fixture-secret"])(
    "matches redacted conversation evidence against raw masked stdio: %s",
    text => {
      const fixture = readFileSync(new URL("./test_data/deepseek_headless_stdout.log", import.meta.url), "utf8");
      const answer = fixture.slice(fixture.indexOf("Perfect!"), fixture.indexOf("[INFO] Stopping containers...")).trim();
      const content = fixture.replace(answer, text).replace("[deepseek-harness] configured", "::add-mask::fixture-secret\n[deepseek-harness] configured");
      const events = [{ type: "assistant.message", data: { content: text.replace("fixture-secret", "***") } }];
      expect(collectAgentExecution({ content, events })).toBeUndefined();
      expect(agentErrorDiagnosticText(content, events)).toBe("");
      expect(collectAgentExecution({ content })).toBeUndefined();
      expect(agentErrorDiagnosticText(content)).toBe("");
      expect(events[0].data.content).toBe(text.replace("fixture-secret", "***"));
    }
  );

  it.each([
    "Assistant: The log text says Access denied by policy settings",
    "Tool output: CAPIError: 429 Too Many Requests",
    "The example error is CAPIError: 429 Too Many Requests",
    "> Access denied by policy settings",
    "Assistant:\nAccess denied by policy settings\nCAPIError: 429 Too Many Requests",
    "Tool output:\nCAPIError: 429 Too Many Requests\nmax_runs_exceeded",
    'exec\nprintf "CAPIError: 429 Too Many Requests"\nCAPIError: 429 Too Many Requests',
    "```text\nCAPIError: 429 Too Many Requests\n[copilot-harness] done: exitCode=1\n```",
    '[copilot-harness] attempt 1: outputTail="CAPIError: 429 Too Many Requests"',
    '[copilot-harness] attempt 1: spawning: echo "Access denied by policy settings"',
  ])("ignores untrusted plaintext transcript evidence: %s", content => {
    expect(collectAgentExecution({ content })).toBeUndefined();
    expect(agentErrorDiagnosticText(content)).toBe("");
    expect(detectErrors(agentErrorDiagnosticText(content))).toEqual(detectErrors(""));
  });

  it.each([
    ["Error: Access denied by policy settings", "inference_access_error"],
    ["! 2 MCP servers were blocked by policy: 'github', 'safeoutputs'", "mcp_policy_error"],
    ["[copilot-sdk-driver] [sdk-driver] error: Timeout after 100ms waiting for session.idle", "agentic_engine_timeout"],
    ["[claude-harness] unexpected error: Authentication failed", "authentication_failed"],
    ["[ERROR] CAPIError: 429 Too Many Requests", "capi_quota_exceeded_error"],
    ['[gh-aw/pi-provider] 2026-10-03T20:27:10Z provider_error error="400 Bad Request"', "http_400_response_error"],
  ])("preserves attributed startup diagnostics: %s", (content, category) => {
    // Runtime attribution resumes after a plaintext transcript block.
    const transcript = content.startsWith("[") ? `Assistant:\nCAPIError: 429 Too Many Requests\n${content}` : content;
    expect(collectAgentExecution({ content: transcript }).data.categories).toContain(category);
  });

  it("preserves pretty-printed native errors without mining quoted canonical messages", () => {
    const records = [
      { type: "assistant.message", data: { content: "Access denied by policy settings" } },
      { type: "turn.failed", error: { code: 400, message: "The requested model is not supported" } },
    ];
    const content = JSON.stringify(records, null, 2);
    expect(collectAgentExecution({ content }).data).toEqual({ categories: ["model_not_supported_error"], errorCodes: [400], errorTypes: [] });
    expect(detectErrors(agentErrorDiagnosticText(content)).inferenceAccessError).toBe(false);
  });

  it("tolerates malformed provider diagnostic string escaping", () => {
    const content = '[gh-aw/pi-provider] 2026-10-03T20:27:10Z provider_error error="bad\\q escape"';
    expect(() => collectAgentExecution({ content })).not.toThrow();
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

  it.each(["1e3", "1.0", "-0", "9007199254740991", "-9007199254740991"])("accepts safe integral JSON error codes %s", code => {
    expect(() => validateAgentExecution(JSON.parse(`{"categories":[],"errorCodes":[${code}],"errorTypes":[]}`))).not.toThrow();
  });
  it.each(["1.5", "9007199254740992", "-9007199254740992", "null", "true"])("rejects invalid JSON error codes %s", code => {
    expect(() => validateAgentExecution(JSON.parse(`{"categories":[],"errorCodes":[${code}],"errorTypes":[]}`))).toThrow("agent.execution");
  });
});
