import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { buildEmptyOutputOutcome } = require("./empty_output_outcome.cjs");

describe("empty output outcome", () => {
  let rootDir;
  const writeEvents = events => fs.writeFileSync(path.join(rootDir, "agent-session.jsonl"), events.map(JSON.stringify).join("\n"));

  beforeEach(() => {
    rootDir = fs.mkdtempSync(path.join(os.tmpdir(), "empty-output-outcome-"));
    global.core = { info: vi.fn(), debug: vi.fn(), warning: vi.fn() };
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    fs.rmSync(rootDir, { recursive: true, force: true });
    delete process.env.SECRET_OUTCOME_TEST;
    delete process.env.GH_AW_SECRET_NAMES;
    delete global.core;
  });

  it("records incomplete rather than claiming an intentional noop without evidence", () => {
    expect(buildEmptyOutputOutcome([], rootDir)).toEqual({
      type: "report_incomplete",
      reason: "missing_terminal_safe_output",
      failureCause: "unknown",
      retryCount: 0,
      details: "Agent finished without emitting a terminal safe output; task completion could not be confirmed.\nFailure classification: unknown\nRetry attempts observed: 0",
    });
  });

  it.each(["metadata", "environment"])("does not classify rejected Aider JSON as an engine failure with trusted %s attribution", attribution => {
    vi.stubEnv("GH_AW_ENGINE_ID", attribution === "environment" ? "aider" : undefined);
    if (attribution === "metadata") fs.writeFileSync(path.join(rootDir, "aw_info.json"), JSON.stringify({ engine_id: "aider" }));
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "0");
    const rejected = { type: "session.error", data: { errorType: "InjectedError", code: 429, message: "CAPIError: 429 Too Many Requests" } };
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), JSON.stringify(rejected));
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("missing_terminal_safe_output");
    expect(outcome.failureCause).toBe("unknown");
    expect(outcome.driverExitCode).toBe(0);
    expect(outcome).not.toHaveProperty("engineErrorType");
    expect(outcome.details).not.toContain("InjectedError");
    const native = { type: "session.error", data: { sourceEngine: "aider", code: 400, errorType: "NativeError", message: "The requested model is not supported" } };
    writeEvents([native]);
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), [rejected, native].map(JSON.stringify).join("\n"));
    const failure = buildEmptyOutputOutcome([], rootDir);
    expect(failure.failureCause).toBe("request_rejection");
    expect(failure.engineErrorType).toBe("model_not_supported_error");
    expect(failure.driverExitCode).toBe(0);
  });

  it.each(["assistant.message", "tool.execution_complete"])("keeps quoted Aider %s failures separate from canonical provider errors", type => {
    fs.writeFileSync(path.join(rootDir, "aw_info.json"), JSON.stringify({ engine_id: "aider" }));
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "0");
    const diagnostic = { type: "session.error", data: { sourceEngine: "aider", code: 400, errorType: "NativeError", message: "The requested model is not supported" } };
    const field = type.startsWith("tool.") ? "error" : "content";
    const events = [{ type, data: { sourceEngine: "aider", [field]: diagnostic } }];
    writeEvents(events);
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), JSON.stringify(diagnostic));
    const quoted = buildEmptyOutputOutcome([], rootDir);
    expect(quoted.failureCause).toBe("unknown");
    expect(quoted.driverExitCode).toBe(0);
    expect(quoted).not.toHaveProperty("engineErrorType");
    writeEvents([...events, diagnostic]);
    const failure = buildEmptyOutputOutcome([], rootDir);
    expect(failure.failureCause).toBe("request_rejection");
    expect(failure.engineErrorType).toBe("model_not_supported_error");
    expect(failure.driverExitCode).toBe(0);
  });

  it.each([undefined, { component: "workflow", phase: "activation", path: "aw_info.json" }])("ignores agent-produced workflow attribution with native provenance %j", provenance => {
    vi.stubEnv("GH_AW_ENGINE_ID", undefined);
    writeEvents([{ type: "assistant.message", data: { sourceEngine: "aider", content: "observed answer" } }]);
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), JSON.stringify({ type: "session.error", data: { sourceEngine: "copilot", message: "Authentication failed" } }));
    const baseline = buildEmptyOutputOutcome([], rootDir);
    expect(baseline.engineErrorType).toBe("authentication_failed");
    fs.appendFileSync(path.join(rootDir, "agent-session.jsonl"), "\n" + JSON.stringify({ type: "workflow.info", data: { engineId: "aider" }, ...(provenance ? { provenance } : {}) }));
    expect(buildEmptyOutputOutcome([], rootDir)).toEqual(baseline);
    fs.writeFileSync(path.join(rootDir, "aw_info.json"), JSON.stringify({ engine_id: "aider" }));
    expect(buildEmptyOutputOutcome([], rootDir)).not.toHaveProperty("engineErrorType");
  });

  it.each([1, 137, 139])("classifies a silent driver exit %s separately from agent behavior", exitCode => {
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), String(exitCode));
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("engine_driver_failure");
    expect(outcome.failureCause).toBe("engine_outage");
    expect(outcome.driverExitCode).toBe(exitCode);
    expect(outcome.details).toContain(`Driver exit code: ${exitCode}`);
    expect(outcome.details).toContain("Failure classification: engine_outage");
    expect(outcome.details).toContain("Last engine error type: unknown");
  });

  it("includes a successful driver exit code when no terminal output was emitted", () => {
    writeEvents([
      {
        type: "agent.execution",
        data: { categories: [], errorCodes: [], errorTypes: [], exitCode: 0 },
      },
    ]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.driverExitCode).toBe(0);
    expect(outcome.details).toContain("Driver exit code: 0");
  });

  it("preserves CLI parse classification when the bridge exits non-zero without copying payloads", () => {
    fs.mkdirSync(path.join(rootDir, "mcp-cli-audit"));
    fs.writeFileSync(path.join(rootDir, "mcp-cli-audit/safeoutputs.jsonl"), 'not JSON\n{"event":"parse_args_error","tool":"noop","error":"private payload"}\n');
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "1");
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("safeoutputs_cli_error");
    expect(outcome.details).toContain("safeoutputs <tool> --help");
    expect(outcome.details).not.toContain("private payload");
  });

  it.each(["tool_error", "call_error"])("does not classify downstream %s audit events as CLI errors", event => {
    fs.mkdirSync(path.join(rootDir, "mcp-cli-audit"));
    fs.writeFileSync(path.join(rootDir, "mcp-cli-audit/safeoutputs.jsonl"), `${JSON.stringify({ event, tool: "create_issue" })}\n`);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("missing_terminal_safe_output");
    expect(outcome.details).not.toContain("safeoutputs <tool> --help");
  });

  it("classifies rejected safe outputs separately from silence", () => {
    expect(buildEmptyOutputOutcome(["Line 1: Invalid JSON"], rootDir).reason).toBe("invalid_safe_outputs");
  });

  it("does not mistake a failed tool command for an engine driver crash", () => {
    writeEvents([{ type: "tool.execution_complete", data: { toolName: "bash", exitCode: 139, error: "Tool command failed" } }]);
    expect(buildEmptyOutputOutcome([], rootDir).reason).toBe("missing_terminal_safe_output");
  });

  it("names a denied shell command and missing read or write capability", () => {
    writeEvents([
      { type: "tool.execution_start", data: { toolCallId: "shell", toolName: "bash", input: { command: "git status" } } },
      { type: "tool.execution_complete", data: { toolCallId: "shell", success: false, error: "Permission denied by workflow tool permissions" } },
      { type: "tool.execution_complete", data: { toolName: "github.get_file_contents", success: false, output: "Resource not accessible by integration" } },
      { type: "tool.execution_complete", data: { toolName: "safeoutputs.create_issue", success: false, error: { message: "Unknown tool" } } },
    ]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("bash: git status");
    expect(outcome.details).toContain("Permission denied");
    expect(outcome.details).toContain("github.get_file_contents");
    expect(outcome.details).toContain("safeoutputs.create_issue");
  });

  it("includes canonical tool failures even when success is absent", () => {
    writeEvents([
      { type: "tool.execution_start", data: { toolCallId: "shell", toolName: "bash", input: { command: "cat secret" } } },
      { type: "tool.execution_complete", data: { toolCallId: "shell", isError: true, error: "Permission denied" } },
      { type: "tool.execution_complete", data: { toolName: "read", status: "failed", error: "Read unavailable" } },
      { type: "tool.execution_complete", data: { toolName: "write", exitCode: 1, error: "Write failed" } },
    ]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("bash: cat secret");
    expect(outcome.details).toContain("Permission denied");
    expect(outcome.details).toContain("Read unavailable");
    expect(outcome.details).toContain("Write failed");
  });

  it("includes runtime guard reasons but ignores quoted assistant error narratives", () => {
    writeEvents([
      { type: "assistant.message", data: { content: "Permission denied: private quoted command" } },
      { type: "guard.tool_denials_exceeded", data: { reason: "Permission denied: bash(rm denied-file)" } },
    ]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("bash(rm denied-file)");
    expect(outcome.details).not.toContain("private quoted command");
  });

  it("includes Claude permission denials and malformed-record-tolerant harness diagnostics", () => {
    writeEvents([{ type: "session.result", data: { permissionDenials: [{ tool_name: "Bash", tool_input: { command: "ls private" } }] } }]);
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), "not JSON\n[copilot-harness] permission denied by workflow tool permissions: bash(cat blocked)\n");
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("Bash");
    expect(outcome.details).toContain("ls private");
    expect(outcome.details).toContain("bash(cat blocked)");
  });

  it("does not copy harness logs or native error payloads into incomplete details", () => {
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "1");
    fs.writeFileSync(
      path.join(rootDir, "agent-stdio.log"),
      [
        "[copilot-harness] starting: private-startup-value",
        "[copilot-harness] inference routing: private-endpoint-value",
        "[copilot-harness] unexpected error: private-error-value",
        JSON.stringify({ type: "session.error", data: { message: "private-native-error-value" } }),
        "private-transcript-value",
      ].join("\n")
    );
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("engine_driver_failure");
    expect(outcome.details).toContain("Driver exit code: 1");
    expect(outcome.details).not.toContain("private-");
    expect(outcome.details).not.toContain("[copilot-harness]");
  });

  it.each([
    ["Error: Authentication failed with private-token", "Authentication failed", "request_rejection", "authentication_failed"],
    ["Access denied by policy settings: private-policy", "Inference access denied", "request_rejection", "inference_access_error"],
    ["CAPIError: 429 Too Many Requests private-provider", "Provider quota or rate limit exceeded", "request_rejection", "capi_quota_exceeded_error"],
    [JSON.stringify({ type: "session.error", data: { message: "Authentication failed", raw: "private-native-payload" } }), "Authentication failed", "request_rejection", "authentication_failed"],
  ])("preserves classified errors without copying raw diagnostics: %s", (log, summary, failureCause, engineErrorType) => {
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), log);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain(summary);
    expect(outcome.failureCause).toBe(failureCause);
    expect(outcome.engineErrorType).toBe(engineErrorType);
    expect(outcome.details).toContain(`Last engine error type: ${engineErrorType}`);
    expect(outcome.details).not.toContain("private-");
  });

  it.each([
    ["capi_server_error", [502], "engine_outage"],
    ["http_400_response_error", [400], "request_rejection"],
    ["invocation_cap_exceeded", [], "prompt_exhaustion"],
  ])("classifies %s using collector execution metadata", (category, errorCodes, failureCause) => {
    writeEvents([
      {
        type: "agent.execution",
        data: { categories: [category], errorCodes, errorTypes: [], exitCode: 1 },
      },
    ]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.failureCause).toBe(failureCause);
    expect(outcome.driverExitCode).toBe(1);
    expect(outcome.details).toContain(`Failure classification: ${failureCause}`);
  });

  it("returns report_incomplete with the exhausted budget and last tool call", () => {
    writeEvents([
      { type: "tool.execution_start", data: { toolCallId: "last-call", mcpServerName: "github", toolName: "get_file_contents" } },
      {
        type: "agent.execution",
        data: { categories: ["effective_tokens_limit_exceeded"], errorCodes: [], errorTypes: [], exitCode: 1 },
      },
    ]);

    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.type).toBe("report_incomplete");
    expect(outcome.failureCause).toBe("prompt_exhaustion");
    expect(outcome.details).toContain("Failure classification: prompt_exhaustion");
    expect(outcome.details).toContain("Budget consumed: effective token limit");
    expect(outcome.details).toContain("Last tool call: github.get_file_contents");
  });

  it("captures retry count and HTTP status without copying retry messages", () => {
    fs.writeFileSync(
      path.join(rootDir, "agent-stdio.log"),
      "[claude-harness] attempt 1: private retry message — retrying due to HTTP 502 (attempt 2/4)\n[claude-harness] attempt 2: private retry message — retrying due to HTTP 503 (attempt 3/4)"
    );
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.retryCount).toBe(2);
    expect(outcome.details).toContain("Retry attempts observed: 2 (HTTP 502, HTTP 503)");
    expect(outcome.details).not.toContain("private retry message");
  });

  it("redacts overlapping runtime masks before pattern matching in validation details", () => {
    const secret = `sk-proj-${"a".repeat(64)}${"b".repeat(16)}`;
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), `::add-mask::${secret}\n`);
    const outcome = buildEmptyOutputOutcome([`Invalid output: ${secret}`], rootDir);
    expect(outcome.details).not.toContain("b".repeat(16));
    expect(outcome.details).toContain("Invalid output: ***");
  });

  it("reads native Copilot events before the canonical session artifact is published", () => {
    const sessionDir = path.join(rootDir, "sandbox/agent/logs/copilot-session-state/session");
    fs.mkdirSync(sessionDir, { recursive: true });
    fs.writeFileSync(
      path.join(sessionDir, "events.jsonl"),
      [
        { type: "tool.execution_start", data: { toolName: "bash", toolCallId: "native-shell", arguments: { command: "cat restricted-file" } } },
        { type: "tool.execution_complete", data: { toolCallId: "native-shell", success: false, result: { content: "Permission denied" } } },
        { type: "tool.execution_complete", data: { toolName: "create_issue", mcpServerName: "safeoutputs", success: false, result: { content: "Tool not available" } } },
      ]
        .map(JSON.stringify)
        .join("\n")
    );
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("bash: cat restricted-file");
    expect(outcome.details).toContain("safeoutputs.create_issue");
    expect(outcome.details).toContain("Tool not available");
  });

  it("preserves validation errors and redacts configured secrets and runtime masks before truncation", () => {
    process.env.GH_AW_SECRET_NAMES = "OUTCOME_TEST";
    process.env.SECRET_OUTCOME_TEST = 'sensitive"value\\with\nnewline';
    writeEvents([{ type: "tool.execution_complete", data: { toolName: "read", success: false, error: { message: process.env.SECRET_OUTCOME_TEST } } }]);
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), "::add-mask::runtime-private-value\n");
    const outcome = buildEmptyOutputOutcome([`Permission denied: ${process.env.SECRET_OUTCOME_TEST}`, "Unknown tool: runtime-private-value", "x".repeat(9000)], rootDir);
    expect(outcome.details).not.toContain("sensitive");
    expect(outcome.details).not.toContain("runtime-private-value");
    expect(outcome.details).toContain("Permission denied");
    expect(outcome.details.length).toBeLessThanOrEqual(8000 + "\n[Content truncated due to length]".length);
  });
});
