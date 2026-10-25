import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { buildEmptyOutputOutcome, buildCompletionEvidence, hasFailedFinalResult } = require("./empty_output_outcome.cjs");

describe("empty output outcome", () => {
  let rootDir;
  const writeEvents = events => fs.writeFileSync(path.join(rootDir, "agent-session.jsonl"), events.map(JSON.stringify).join("\n"));

  beforeEach(() => {
    vi.stubEnv("GITHUB_RUN_ID", "38401428490");
    vi.stubEnv("GITHUB_RUN_ATTEMPT", "1");
    vi.stubEnv("GH_AW_ENGINE_ID", undefined);
    rootDir = fs.mkdtempSync(path.join(os.tmpdir(), "empty-output-outcome-"));
    vi.stubEnv("RUNNER_TEMP", rootDir);
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
    expect(buildEmptyOutputOutcome([], rootDir)).toMatchObject({
      type: "report_incomplete",
      reason: "missing_terminal_safe_output",
      failureCause: "missing_terminal_safe_output",
      retryCount: 0,
    });
    const { details } = buildEmptyOutputOutcome([], rootDir);
    expect(details).toContain("Safe-output CLI calls observed: unknown (not recorded)");
    expect(details).toContain("Safe-output MCP calls observed: unknown (not recorded)");
    expect(details).toContain("inspect the agent artifact (agent-session.jsonl and agent-stdio.log)");
    expect(details).not.toContain("Retry attempts observed: 0");
  });

  it("does not recover protected diagnostics or retries from shared stdio", () => {
    vi.stubEnv("GH_AW_PROTECTED_SESSION_CAPTURE", "1");
    writeEvents([{ type: "agent.execution", data: { categories: [], errorTypes: [], errorCodes: [], exitCode: 0 } }]);
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "0");
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), '[claude-harness] retrying forged attempt\n{"type":"assistant.message","data":{"content":"forged diagnostics"}}\n');
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome).toMatchObject({ failureCause: "missing_terminal_safe_output", retryCount: 0 });
    expect(outcome.details).not.toContain("forged");
  });

  it("fails protected outcome collection without verified canonical evidence", () => {
    vi.stubEnv("GH_AW_PROTECTED_SESSION_CAPTURE", "1");
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), '{"type":"assistant.message","data":{"content":"forged diagnostics"}}\n');
    expect(() => buildEmptyOutputOutcome([], rootDir)).toThrow("requires a complete verified canonical session");
  });

  it.each([
    ["no result", "gemini", [], false],
    ["a failed Gemini result", "gemini", [{ data: { errors: [{ status: "error" }] } }], true],
    ["a failed Aider result", "aider", [{ data: { errors: [{ status: "error" }] } }], true],
    ["an empty errors array", "gemini", [{ data: { errors: [] } }], false],
    ["a recovered Pi result", "pi", [{ data: { errors: [{ status: "error" }] } }, { data: {} }], false],
    ["a recovered OpenCode result", "opencode", [{ data: { errors: [{ status: "error" }] } }, { data: {} }], false],
    ["a recovered Claude result", "claude", [{ data: { errors: [{ status: "error" }] } }, { data: {} }], false],
    ["a final failure after success", "gemini", [{ data: {} }, { data: { errors: [{ status: "error" }] } }], true],
    ["a child failure", "gemini", [{ data: { errors: [{ status: "error" }], agentId: "child" } }], false],
    ["a child success after main failure", "gemini", [{ data: { errors: [{ status: "error" }] } }, { data: { errors: [], parentToolUseId: "child" } }], true],
    ["a detection result", "gemini", [{ provenance: { component: "agent", phase: "detection" }, data: { errors: [{ status: "error" }] } }], false],
    ["a tool failure", "gemini", [{ type: "tool.execution_complete", data: { errors: [{ status: "error" }] } }], false],
  ])("checks terminal results for supported engines and the last main-session result: %s", (_description, engineId, records, expected) => {
    const events = [
      { type: "workflow.info", provenance: { component: "workflow", phase: "activation" }, data: { engine_id: engineId } },
      ...records.map(record => ({ type: "session.result", provenance: { component: "agent", phase: "agent" }, ...record })),
    ];
    expect(hasFailedFinalResult(events)).toBe(expected);
  });

  it("distinguishes a failed final Gemini result from missing terminal output despite exit 0", () => {
    fs.writeFileSync(path.join(rootDir, "aw_info.json"), JSON.stringify({ engine_id: "gemini" }));
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "0");
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), JSON.stringify({ type: "result", status: "error" }));

    const outcome = buildEmptyOutputOutcome([], rootDir);

    expect(outcome).toMatchObject({ reason: "engine_driver_failure", failureCause: "unknown", driverExitCode: 0 });
    expect(outcome.details).toContain("The engine's final result reported an error");
    expect(outcome.details).not.toContain("No terminal safe output was recorded");
    expect(outcome.details).not.toContain("without emitting a terminal safe output");
  });

  it("preserves a failed final Gemini result over a recovered safeoutputs CLI error", () => {
    fs.writeFileSync(path.join(rootDir, "aw_info.json"), JSON.stringify({ engine_id: "gemini" }));
    writeEvents([{ type: "session.result", data: { errors: [{ status: "error" }] } }]);
    fs.writeFileSync(path.join(rootDir, "mcp-cli-audit.safeoutputs.audit.jsonl"), `${JSON.stringify({ event: "parse_args_error" })}\n`);

    const outcome = buildEmptyOutputOutcome([], rootDir);

    expect(outcome).toMatchObject({ reason: "engine_driver_failure", failureCause: "unknown" });
    expect(outcome.details).toContain("The safeoutputs CLI failed.");
    expect(outcome.details).toContain("The engine's final result reported an error");
  });

  it.each([0, 1])("classifies the observed SDK runtime content filter as request rejection despite exit %s", exitCode => {
    fs.writeFileSync(path.join(rootDir, "aw_info.json"), JSON.stringify({ engine_id: "copilot" }));
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), String(exitCode));
    writeEvents([
      { type: "session.start", data: { sessionId: "observed-sdk-session" } },
      { type: "assistant.message", data: { content: "The model returned no content because the response was blocked by content filtering.", rte: true, toolRequests: [] } },
    ]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome).toMatchObject({ type: "report_incomplete", failureCause: "request_rejection", driverExitCode: exitCode, retryCount: 0 });
    expect(outcome.details).toContain("The model response was blocked by content filtering");
    expect(outcome.details).not.toContain("ensure the workflow emits");
    expect(outcome.details).not.toContain("Token budget state: exceeded");
  });

  it.each([
    { data: { content: "The model returned no content because the response was blocked by content filtering." } },
    { agentId: "child", data: { content: "The model returned no content because the response was blocked by content filtering.", rte: true } },
    { data: { content: "The model returned no content because the response was blocked by content filtering.", rte: true, parentToolCallId: "child" } },
  ])("does not attribute quoted or nested refusals to a main-session rejection %j", fields => {
    fs.writeFileSync(path.join(rootDir, "aw_info.json"), JSON.stringify({ engine_id: "copilot" }));
    writeEvents([
      { type: "session.start", data: { sessionId: "observed-sdk-session" } },
      { type: "assistant.message", ...fields },
    ]);
    expect(buildEmptyOutputOutcome([], rootDir).failureCause).toBe("missing_terminal_safe_output");
  });

  it("does not treat an empty persistence file as evidence of zero safe-output calls", () => {
    fs.writeFileSync(path.join(rootDir, "safeoutputs.jsonl"), "");
    const { details } = buildEmptyOutputOutcome([], rootDir);
    expect(details).toContain("Safe-output CLI calls observed: unknown (not recorded)");
    expect(details).toContain("Safe-output MCP calls observed: unknown (not recorded)");
    expect(details).toContain("Safe-output persistence: empty (0 records, 0 malformed; not validation or publication evidence)");
  });

  it.each(["agent_execution.json", "usage/agent/execution.json"])("classifies recorded pre-agent startup failure from %s", relativePath => {
    const file = path.join(rootDir, relativePath);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, JSON.stringify({ version: 1, component: "agent", run_id: 38401428490, run_attempt: 1, state: "not_started" }));
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), "Authentication failed\n");
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome).toMatchObject({ reason: "agent_startup_failure", failureCause: "agent_startup_failure" });
    expect(outcome).not.toHaveProperty("engineErrorType");
    expect(outcome.details).toContain("Agent execution state: not_started");
    expect(outcome.details).toContain("Safe-output CLI calls observed: unknown (not recorded)");
    expect(outcome.details).toContain("Safe-output MCP calls observed: unknown (not recorded)");
    expect(outcome.details).toContain("Inspect the failed startup step and gateway/setup logs");
    expect(outcome.details).not.toContain("inspect the agent artifact");
    expect(outcome.details).not.toContain("Agent finished");
    expect(outcome.details).not.toContain("Driver exit code");
    expect(outcome.details).not.toContain("Token budget");
    expect(outcome.details).not.toContain("Engine error:");
    expect(outcome.details).not.toContain("Last engine error type:");
    expect(outcome.details).not.toContain("Authentication failed");
  });

  it("preserves engine error extraction when agent execution invalidates a not_started marker", () => {
    fs.writeFileSync(path.join(rootDir, "agent_execution.json"), JSON.stringify({ version: 1, component: "agent", run_id: 38401428490, run_attempt: 1, state: "not_started" }));
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "1");
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), "Authentication failed\n");
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome).toMatchObject({ reason: "engine_driver_failure", failureCause: "request_rejection", driverExitCode: 1, engineErrorType: "authentication_failed" });
    expect(outcome.details).toContain("Engine error: Authentication failed");
  });

  it.each([{ version: 2 }, { component: "detection" }, { state: "started" }, { state: "completed" }, { run_id: "38401428490" }, { run_attempt: 0 }])(
    "does not infer a startup failure from invalid or started execution evidence %j",
    override => {
      fs.writeFileSync(path.join(rootDir, "agent_execution.json"), JSON.stringify({ version: 1, component: "agent", run_id: 38401428490, run_attempt: 1, state: "not_started", ...override }));
      expect(buildEmptyOutputOutcome([], rootDir).reason).toBe("missing_terminal_safe_output");
    }
  );

  it.each([
    ["GITHUB_RUN_ID", "38386214820"],
    ["GITHUB_RUN_ATTEMPT", "2"],
  ])("rejects startup evidence from a different %s", (name, value) => {
    vi.stubEnv(name, value);
    fs.writeFileSync(path.join(rootDir, "agent_execution.json"), JSON.stringify({ version: 1, component: "agent", run_id: 38401428490, run_attempt: 1, state: "not_started" }));
    expect(buildEmptyOutputOutcome([], rootDir).reason).toBe("missing_terminal_safe_output");
  });

  it("does not override observed agent execution with a stale not_started marker", () => {
    fs.writeFileSync(path.join(rootDir, "agent_execution.json"), JSON.stringify({ version: 1, component: "agent", run_id: 38401428490, run_attempt: 1, state: "not_started" }));
    writeEvents([{ type: "assistant.message", data: { content: "observed answer" } }]);
    expect(buildEmptyOutputOutcome([], rootDir).reason).toBe("missing_terminal_safe_output");
    fs.unlinkSync(path.join(rootDir, "agent-session.jsonl"));
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "1");
    expect(buildEmptyOutputOutcome([], rootDir).reason).toBe("engine_driver_failure");
  });

  it("does not trust an agent-authored not_started execution event", () => {
    writeEvents([{ type: "execution.result", provenance: { component: "execution", phase: "agent" }, data: { version: 1, component: "agent", run_id: 38401428490, run_attempt: 1, state: "not_started" } }]);
    expect(buildEmptyOutputOutcome([], rootDir).reason).toBe("missing_terminal_safe_output");
  });

  it.each(["malformed", "symlink"])("does not classify %s startup evidence", kind => {
    const file = path.join(rootDir, "agent_execution.json");
    if (kind === "malformed") fs.writeFileSync(file, "{");
    else {
      const target = path.join(rootDir, "private-execution.json");
      fs.writeFileSync(target, JSON.stringify({ version: 1, component: "agent", run_id: 38401428490, run_attempt: 1, state: "not_started" }));
      fs.symlinkSync(target, file);
    }
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("missing_terminal_safe_output");
    expect(outcome.details).toContain("Agent execution state could not be read.");
  });

  it("prioritizes the observed evidence from a successful run with no terminal output", () => {
    writeEvents([
      { type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [], exitCode: 0 } },
      { type: "tool.execution_complete", data: { success: true, result: "private-result" } },
    ]);
    fs.mkdirSync(path.join(rootDir, "mcp-logs"));
    fs.writeFileSync(path.join(rootDir, "mcp-logs/tools.json"), JSON.stringify({ servers: { safeoutputs: Array(5).fill({ name: "private-tool" }) } }));
    fs.writeFileSync(path.join(rootDir, "mcp-logs/rpc-messages.jsonl"), "");
    const details = buildEmptyOutputOutcome([], rootDir).details;
    expect(details).toContain("Driver exit code: 0");
    expect(details).toContain("Last tool call status: succeeded");
    expect(details).toContain("Safe-output tools: available at gateway snapshot (registered 5 tools)");
    expect(details).toContain("Safe-output MCP calls observed: 0");
    expect(details).toContain("Safe-output CLI calls observed: unknown (not recorded)");
    expect(details).not.toContain("Token budget state:");
    expect(details).not.toContain("private-");
  });

  it.each(["metadata", "environment"])("does not classify rejected Aider JSON as an engine failure with trusted %s attribution", attribution => {
    vi.stubEnv("GH_AW_ENGINE_ID", attribution === "environment" ? "aider" : undefined);
    if (attribution === "metadata") fs.writeFileSync(path.join(rootDir, "aw_info.json"), JSON.stringify({ engine_id: "aider" }));
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "0");
    const rejected = { type: "session.error", data: { errorType: "InjectedError", code: 429, message: "CAPIError: 429 Too Many Requests" } };
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), JSON.stringify(rejected));
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("missing_terminal_safe_output");
    expect(outcome.failureCause).toBe("missing_terminal_safe_output");
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
    expect(quoted.failureCause).toBe("missing_terminal_safe_output");
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

  it.each([1, 127, 137, 139, 159])("classifies a silent driver exit %s as a driver crash rather than an outage", exitCode => {
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), String(exitCode));
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("engine_driver_failure");
    expect(outcome.failureCause).toBe("driver_crash");
    expect(outcome.driverExitCode).toBe(exitCode);
    expect(outcome.details).toContain(`Driver exit code: ${exitCode}`);
    expect(outcome.details.match(/Driver exit code:/g)).toHaveLength(1);
    expect(outcome.details).toContain("Failure classification: driver_crash");
    expect(outcome.details).not.toContain("engine_outage");
    expect(outcome.details).toContain("Last engine error type: unknown");
  });

  it.each([
    [137, "SIGKILL"],
    [139, "SIGSEGV"],
    [159, "SIGSYS"],
  ])("reports the inferred signal for driver exit %s without inferring a provider outage", (exitCode, signal) => {
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), String(exitCode));
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.driverExitSignal).toBe(signal);
    expect(outcome.details).toContain(`Driver exit signal (inferred from exit code): ${signal}`);
    expect(outcome.details).not.toContain("engine_outage");
  });

  it("prefers a fatal driver signal over an earlier request rejection", () => {
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "139");
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), "Error: Authentication failed\n");
    expect(buildEmptyOutputOutcome([], rootDir).failureCause).toBe("driver_crash");
  });

  it("summarizes the last attributed driver stderr diagnostic without publishing its payload", () => {
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "1");
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), "Error: Authentication failed\n[copilot-sdk-driver] unhandled error: private-payload\n");
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.failureCause).toBe("driver_crash");
    expect(outcome.details).toContain("Last driver stderr summary: SDK driver reported an error");
    expect(outcome.details).not.toContain("private-payload");
  });

  it("keeps the last classifiable stderr diagnostic despite later noise and unclassifiable diagnostics", () => {
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "1");
    fs.writeFileSync(
      path.join(rootDir, "agent-stdio.log"),
      ["Error: Authentication failed", "Maximum LLM invocations exceeded", "[copilot-harness] process completed with no additional diagnostic", "[ERROR] private-unclassifiable-diagnostic", "private-trailing-noise", ""].join("\n")
    );
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("Last driver stderr summary: Engine error: Maximum LLM invocations exceeded (invocation_cap_exceeded)");
    expect(outcome.details).not.toContain("Last driver stderr summary: Engine error: Authentication failed");
    expect(outcome.details).not.toContain("private-");
  });

  it.each([
    ["Copilot model response blocked by content filtering; task completion could not be confirmed.", "request_rejection", "content_filter"],
    ["CAPIError: 503 Service Unavailable", "engine_outage"],
    ["Response status code does not indicate success: 401 (Unauthorized)", "request_rejection"],
    ["Maximum effective tokens exceeded", "prompt_exhaustion"],
  ])("preserves a handled SDK session failure: %s", (message, failureCause, code) => {
    fs.writeFileSync(path.join(rootDir, "aw_info.json"), JSON.stringify({ engine_id: "copilot" }));
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "1");
    writeEvents([
      { type: "session.start", data: { sessionId: "observed-sdk-session" } },
      ...(code === "content_filter" ? [{ type: "assistant.message", data: { content: "The model returned no content because the response was blocked by content filtering.", rte: true, toolRequests: [] } }] : []),
      { type: "session.error", data: { errorType: "sdk_driver", message, ...(code ? { code } : {}) } },
    ]);
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), `[copilot-sdk-driver] [sdk-driver] error: ${message}\n`);
    expect(buildEmptyOutputOutcome([], rootDir)).toMatchObject({ failureCause, driverExitCode: 1 });
  });

  it("does not let an unclassified handled SDK error override a confirmed request rejection", () => {
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "1");
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), "Error: Authentication failed\n[copilot-sdk-driver] [sdk-driver] error: private-session-failure\n");
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.failureCause).toBe("request_rejection");
    expect(outcome.details).toContain("Last driver stderr summary: SDK driver reported an error");
    expect(outcome.details).not.toContain("private-session-failure");
  });

  it.each([
    [0, "", "missing_terminal_safe_output"],
    [1, "Error: Authentication failed\n", "request_rejection"],
  ])("does not let a recovered crash override final exit %s", (exitCode, finalDiagnostic, failureCause) => {
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), String(exitCode));
    fs.writeFileSync(
      path.join(rootDir, "agent-stdio.log"),
      `[claude-harness] attempt 1: fatal-signal crash exitCode=139 (signal=SIGSEGV, failure_reason=sandbox_runtime_crash) — will retry with fresh run (--continue disabled permanently) (attempt 2/4)\n${finalDiagnostic}[claude-harness] done: exitCode=${exitCode} totalDuration=1s\n`
    );
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome).toMatchObject({ failureCause, driverExitCode: exitCode });
  });

  it("preserves category-only crash evidence when no final exit was recorded", () => {
    fs.writeFileSync(
      path.join(rootDir, "agent-stdio.log"),
      "[claude-harness] attempt 1: fatal-signal crash exitCode=139 (signal=SIGSEGV, failure_reason=sandbox_runtime_crash) — will retry with fresh run (--continue disabled permanently) (attempt 2/4)\n"
    );
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.failureCause).toBe("driver_crash");
    expect(outcome).not.toHaveProperty("driverExitCode");
  });

  it("does not publish quoted driver stderr diagnostics", () => {
    const content = "[copilot-sdk-driver] unhandled error: private-payload";
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "1");
    writeEvents([{ type: "assistant.message", data: { content } }]);
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), content);
    expect(buildEmptyOutputOutcome([], rootDir).details).toContain("Last driver stderr summary: not recorded");
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
    expect(outcome.details.match(/Driver exit code:/g)).toHaveLength(1);
    expect(outcome.failureCause).toBe("missing_terminal_safe_output");
  });

  it("reports only the last tool status without identifiers, arguments, or successful results", () => {
    writeEvents([
      { type: "tool.execution_start", data: { toolCallId: "first", toolName: "bash", input: { command: "private-command" } } },
      { type: "tool.execution_start", data: { toolCallId: "last", toolName: "get_file_contents", mcpServerName: "github", arguments: { token: "private-token" } } },
      { type: "tool.execution_complete", data: { toolCallId: "last", success: true, result: "private-result" } },
    ]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("Last tool call status: succeeded");
    expect(outcome.details).not.toContain("get_file_contents");
    expect(outcome.details).not.toContain("private-");
  });

  it("does not copy malformed tool identifiers into the last-call summary", () => {
    writeEvents([{ type: "tool.execution_start", data: { toolName: "bash\nprivate-tool-payload", input: "private-input" } }]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("Last tool call status: started (completion not recorded)");
    expect(outcome.details).not.toContain("private-");
  });

  it.each(["private-exit-code", -1, 256, 0.5])("does not publish invalid tool exit codes: %s", exitCode => {
    writeEvents([{ type: "tool.execution_complete", data: { success: false, exitCode, toolName: "private-tool", error: "private-error" } }]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("Last tool call status: failed");
    expect(outcome.details).not.toContain("Last tool exit code:");
    expect(outcome.details).not.toContain("private-");
  });

  it("reports token exhaustion only when the execution collector records it", () => {
    writeEvents([{ type: "agent.execution", data: { categories: ["effective_tokens_limit_exceeded"], errorCodes: [], errorTypes: [], exitCode: 0 } }]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.failureCause).toBe("prompt_exhaustion");
    expect(outcome.details).toContain("Token budget state: exceeded");
    expect(outcome.details).toContain("Step budget state (LLM invocations): unknown");
  });

  it.each([
    [[], "unavailable at setup"],
    [[{ name: "noop", inputSchema: { private: "private-schema" } }], "available at setup"],
    [{ tools: [] }, "unknown (invalid registration evidence)"],
  ])("reports observed safe-output registration without copying schemas: %j", (tools, expected) => {
    const toolsDir = path.join(rootDir, "gh-aw/mcp-cli/tools");
    fs.mkdirSync(toolsDir, { recursive: true });
    fs.writeFileSync(path.join(toolsDir, "safeoutputs.json"), JSON.stringify(tools));
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain(`Safe-output tools: ${expected}`);
    expect(outcome.details).not.toContain("private-schema");
  });

  it("reports the existing zero-tools flag without inventing a gateway root cause", () => {
    const flagDir = path.join(rootDir, "gh-aw/safeoutputs");
    fs.mkdirSync(flagDir, { recursive: true });
    fs.writeFileSync(path.join(flagDir, "gateway_empty.flag"), "");
    expect(buildEmptyOutputOutcome([], rootDir).details).toContain("Safe-output tools: unavailable (gateway registered zero tools)");
  });

  it.each([
    [0, "tools"],
    [1, "tool"],
    [2, "tools"],
  ])("pluralizes registration metadata for %i tools", (count, noun) => {
    const tools = Array.from({ length: count }, (_, index) => ({ name: `tool-${index}` }));
    fs.mkdirSync(path.join(rootDir, "mcp-logs"));
    fs.writeFileSync(path.join(rootDir, "mcp-logs/tools.json"), JSON.stringify({ servers: { safeoutputs: tools } }));
    const availability = count ? "available" : "unavailable";
    expect(buildCompletionEvidence(rootDir)[0]).toBe(`Safe-output tools: ${availability} at gateway snapshot (registered ${count} ${noun})`);
    fs.mkdirSync(path.join(rootDir, "gh-aw/mcp-cli/tools"), { recursive: true });
    fs.writeFileSync(path.join(rootDir, "gh-aw/mcp-cli/tools/safeoutputs.json"), JSON.stringify(tools));
    expect(buildCompletionEvidence(rootDir)[0]).toBe(`Safe-output tools: ${availability} at setup (registered ${count} ${noun})`);
  });

  it("surfaces unreadable registration evidence without copying its contents or masking the outcome", () => {
    const toolsDir = path.join(rootDir, "gh-aw/mcp-cli/tools");
    fs.mkdirSync(toolsDir, { recursive: true });
    fs.writeFileSync(path.join(toolsDir, "safeoutputs.json"), "private-malformed-json");
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.failureCause).toBe("missing_terminal_safe_output");
    expect(outcome.details).toContain("Safe-output tools: unknown (registration evidence could not be read)");
    expect(outcome.details).not.toContain("private-malformed-json");
  });

  it("uses archived gateway registration when the setup cache is no longer present", () => {
    fs.mkdirSync(path.join(rootDir, "mcp-logs"));
    fs.writeFileSync(path.join(rootDir, "mcp-logs/tools.json"), JSON.stringify({ servers: { safeoutputs: [{ name: "private-tool", description: "private-description", inputSchema: { private: "private-schema" } }] } }));
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.failureCause).toBe("missing_terminal_safe_output");
    expect(outcome.details).toContain("available at gateway snapshot (registered 1 tool)");
    expect(outcome.details).not.toContain("private-");
  });

  it("prefers setup registration over a conflicting gateway snapshot", () => {
    fs.mkdirSync(path.join(rootDir, "gh-aw/mcp-cli/tools"), { recursive: true });
    fs.mkdirSync(path.join(rootDir, "mcp-logs"));
    fs.writeFileSync(path.join(rootDir, "gh-aw/mcp-cli/tools/safeoutputs.json"), "[]");
    fs.writeFileSync(path.join(rootDir, "mcp-logs/tools.json"), JSON.stringify({ servers: { safeoutputs: [{ name: "noop" }] } }));
    expect(buildCompletionEvidence(rootDir).join("\n")).toContain("unavailable at setup (registered 0 tools)");
  });

  it.each(["directory", "corrupted JSON", "invalid tools"])("falls back to gateway registration when the setup cache has %s", failure => {
    const cachePath = path.join(rootDir, "gh-aw/mcp-cli/tools/safeoutputs.json");
    fs.mkdirSync(path.dirname(cachePath), { recursive: true });
    if (failure === "directory") fs.mkdirSync(cachePath);
    else fs.writeFileSync(cachePath, failure === "corrupted JSON" ? "private-corrupted-json" : JSON.stringify({ private: "private-invalid-tools" }));
    fs.mkdirSync(path.join(rootDir, "mcp-logs"));
    const gatewayPath = path.join(rootDir, "mcp-logs/tools.json");
    fs.writeFileSync(gatewayPath, JSON.stringify({ servers: { safeoutputs: [{ name: "private-tool", inputSchema: { private: "private-schema" } }] } }));
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.failureCause).toBe("missing_terminal_safe_output");
    expect(outcome.details).toContain("available at gateway snapshot (registered 1 tool)");
    expect(outcome.details).not.toContain("private-");
    fs.writeFileSync(gatewayPath, JSON.stringify({ servers: { safeoutputs: [] } }));
    expect(buildCompletionEvidence(rootDir)[0]).toBe("Safe-output tools: unavailable at gateway snapshot (registered 0 tools)");
  });

  it.each(["directory", "symlinked parent"])("reports unknown for an unreadable zero-tools flag with %s despite valid registration", failure => {
    const flagDir = path.join(rootDir, "gh-aw/safeoutputs");
    fs.mkdirSync(path.dirname(flagDir), { recursive: true });
    if (failure === "directory") fs.mkdirSync(path.join(flagDir, "gateway_empty.flag"), { recursive: true });
    else fs.symlinkSync(rootDir, flagDir);
    fs.mkdirSync(path.join(rootDir, "mcp-logs"));
    fs.writeFileSync(path.join(rootDir, "mcp-logs/tools.json"), JSON.stringify({ servers: { safeoutputs: [{ name: "private-tool" }] } }));
    expect(buildCompletionEvidence(rootDir)[0]).toBe("Safe-output tools: unknown (registration evidence could not be read)");
    fs.mkdirSync(path.join(rootDir, "gh-aw/mcp-cli/tools"), { recursive: true });
    fs.writeFileSync(path.join(rootDir, "gh-aw/mcp-cli/tools/safeoutputs.json"), JSON.stringify([{ name: "private-tool", inputSchema: { private: "private-schema" } }]));
    const summary = buildCompletionEvidence(rootDir).join("\n");
    expect(summary).toContain("Safe-output tools: unknown (registration evidence could not be read)");
    expect(summary).not.toContain("private-");
  });

  it("lets a recorded zero-tools flag override valid setup and gateway registration", () => {
    fs.mkdirSync(path.join(rootDir, "gh-aw/safeoutputs"), { recursive: true });
    fs.writeFileSync(path.join(rootDir, "gh-aw/safeoutputs/gateway_empty.flag"), "private-flag-content");
    fs.mkdirSync(path.join(rootDir, "gh-aw/mcp-cli/tools"), { recursive: true });
    fs.writeFileSync(path.join(rootDir, "gh-aw/mcp-cli/tools/safeoutputs.json"), JSON.stringify([{ name: "private-tool" }]));
    fs.mkdirSync(path.join(rootDir, "mcp-logs"));
    fs.writeFileSync(path.join(rootDir, "mcp-logs/tools.json"), JSON.stringify({ servers: { safeoutputs: [{ name: "private-tool" }] } }));
    const summary = buildCompletionEvidence(rootDir).join("\n");
    expect(summary).toContain("Safe-output tools: unavailable (gateway registered zero tools)");
    expect(summary).not.toContain("private-");
  });

  it.each(["rpc_request", "REQUEST"])("counts %s calls separately from CLI attempts without publishing arguments or responses", event => {
    fs.mkdirSync(path.join(rootDir, "mcp-logs"));
    fs.writeFileSync(
      path.join(rootDir, "mcp-logs/rpc-messages.jsonl"),
      [
        { event, server_id: "safeoutputs", payload: { method: "tools/call", params: { name: "private-tool", arguments: { body: "private-arguments" } } } },
        { event, server_id: "github", method: "tools/call" },
        { event, server_id: "safeoutputs", method: "tools/list" },
        { event: "rpc_response", server_id: "safeoutputs", payload: { result: { content: "private-result" } } },
      ]
        .map(JSON.stringify)
        .join("\n") + "\nprivate-malformed-record"
    );
    fs.writeFileSync(path.join(rootDir, "mcp-cli-audit.safeoutputs.audit.jsonl"), [{ event: "call_start", tool: "private-tool" }, { event: "tools_call_start" }, { event: "call_complete" }].map(JSON.stringify).join("\n"));
    const summary = buildCompletionEvidence(rootDir).join("\n");
    expect(summary).toContain("Safe-output CLI calls observed: 1");
    expect(summary).toContain("Safe-output MCP calls observed: 1 (partial evidence: 1 malformed record)");
    expect(summary).not.toContain("private-");
  });

  it.each([
    [0, ""],
    [1, " (partial evidence: 1 malformed record)"],
    [2, " (partial evidence: 2 malformed records)"],
  ])("pluralizes partial call evidence for %i malformed records", (count, suffix) => {
    fs.mkdirSync(path.join(rootDir, "mcp-logs"));
    const content = Array(count).fill("invalid-json").join("\n");
    fs.writeFileSync(path.join(rootDir, "mcp-cli-audit.safeoutputs.audit.jsonl"), content);
    fs.writeFileSync(path.join(rootDir, "mcp-logs/rpc-messages.jsonl"), content);
    const summary = buildCompletionEvidence(rootDir);
    expect(summary).toContain(`Safe-output CLI calls observed: 0${suffix}`);
    expect(summary).toContain(`Safe-output MCP calls observed: 0${suffix}`);
  });

  it.each([
    [0, "records"],
    [1, "record"],
    [2, "records"],
  ])("pluralizes persistence metadata for %i records", (count, noun) => {
    fs.writeFileSync(path.join(rootDir, "safeoutputs.jsonl"), Array(count).fill("invalid-json").join("\n"));
    expect(buildCompletionEvidence(rootDir)).toContain(`Safe-output persistence: ${count ? "nonempty" : "empty"} (${count} ${noun}, ${count} malformed; not validation or publication evidence)`);
  });

  it("distinguishes missing call evidence from an observed tools-list-only gateway log", () => {
    expect(buildCompletionEvidence(rootDir).join("\n")).toContain("Safe-output MCP calls observed: unknown (not recorded)");
    fs.mkdirSync(path.join(rootDir, "mcp-logs"));
    fs.writeFileSync(path.join(rootDir, "mcp-logs/rpc-messages.jsonl"), JSON.stringify({ event: "rpc_request", server_id: "safeoutputs", method: "tools/list" }));
    expect(buildCompletionEvidence(rootDir).join("\n")).toContain("Safe-output MCP calls observed: 0");
  });

  it("reports output persistence without treating records as validated or published outcomes", () => {
    expect(buildCompletionEvidence(rootDir).join("\n")).toContain("Safe-output persistence: not recorded");
    fs.writeFileSync(path.join(rootDir, "safeoutputs.jsonl"), " \n");
    expect(buildCompletionEvidence(rootDir).join("\n")).toContain("Safe-output persistence: empty (0 records, 0 malformed");
    fs.writeFileSync(path.join(rootDir, "safeoutputs.jsonl"), `${JSON.stringify({ type: "report_incomplete", details: "private-details" })}\nprivate-broken-json\n[]\n`);
    const summary = buildCompletionEvidence(rootDir).join("\n");
    expect(summary).toContain("Safe-output persistence: nonempty (3 records, 2 malformed; not validation or publication evidence)");
    expect(summary).not.toContain("private-");
  });

  it.each(["mcp-logs", "safeoutputs.jsonl"])("refuses symlinked %s evidence", relativePath => {
    const outside = fs.mkdtempSync(path.join(os.tmpdir(), "completion-symlink-"));
    try {
      fs.writeFileSync(path.join(outside, "tools.json"), JSON.stringify({ servers: { safeoutputs: [{ name: "private-tool" }] } }));
      fs.symlinkSync(relativePath === "mcp-logs" ? outside : path.join(outside, "tools.json"), path.join(rootDir, relativePath));
      const summary = buildCompletionEvidence(rootDir).join("\n");
      expect(summary).toContain(relativePath === "mcp-logs" ? "Safe-output tools: unknown (registration evidence could not be read)" : "Safe-output persistence: unreadable");
      expect(summary).not.toContain("private-tool");
    } finally {
      fs.rmSync(outside, { recursive: true, force: true });
    }
  });

  it("reports oversized and non-file evidence as unreadable rather than empty", () => {
    const outputPath = path.join(rootDir, "safeoutputs.jsonl");
    fs.writeFileSync(outputPath, "");
    fs.truncateSync(outputPath, 10 * 1024 * 1024 + 1);
    expect(buildCompletionEvidence(rootDir).join("\n")).toContain("Safe-output persistence: unreadable");
    fs.unlinkSync(outputPath);
    fs.mkdirSync(outputPath);
    expect(buildCompletionEvidence(rootDir).join("\n")).toContain("Safe-output persistence: unreadable");
  });

  it("keeps core diagnostics even when validation errors fill the detail limit", () => {
    const outcome = buildEmptyOutputOutcome(
      Array.from({ length: 30 }, (_, i) => `Invalid output ${i}`),
      rootDir
    );
    expect(outcome.details).toContain("Failure classification: request_rejection");
    expect(outcome.details).toContain("Last tool call status: not recorded");
    expect(outcome.details).toContain("Safe-output validation status: failed");
    expect(outcome.details).not.toContain("Invalid output");
  });

  it("preserves CLI parse classification when the bridge exits non-zero without copying payloads", () => {
    fs.writeFileSync(path.join(rootDir, "mcp-cli-audit.safeoutputs.audit.jsonl"), 'not JSON\n{"event":"parse_args_error","tool":"noop","error":"private payload"}\n');
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "1");
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("safeoutputs_cli_error");
    expect(outcome.details).toContain("safeoutputs <tool> --help");
    expect(outcome.details).not.toContain("private payload");
  });

  it.each(["tool_error", "call_error"])("does not classify downstream %s audit events as CLI errors", event => {
    fs.writeFileSync(path.join(rootDir, "mcp-cli-audit.safeoutputs.audit.jsonl"), `${JSON.stringify({ event, tool: "create_issue" })}\n`);
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

  it("counts failed tools without copying denied commands, names, or error text", () => {
    writeEvents([
      { type: "tool.execution_start", data: { toolCallId: "shell", toolName: "bash", input: { command: "git status" } } },
      { type: "tool.execution_complete", data: { toolCallId: "shell", success: false, error: "Permission denied by workflow tool permissions" } },
      { type: "tool.execution_complete", data: { toolName: "github.get_file_contents", success: false, output: "Resource not accessible by integration" } },
      { type: "tool.execution_complete", data: { toolName: "safeoutputs.create_issue", success: false, error: { message: "Unknown tool" } } },
    ]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("Failed tool calls observed: 3");
    expect(outcome.details).toContain("Last tool call status: failed");
    for (const text of ["git status", "Permission denied", "get_file_contents", "create_issue", "Unknown tool", "Resource not accessible"]) expect(outcome.details).not.toContain(text);
  });

  it("includes canonical tool failures even when success is absent", () => {
    writeEvents([
      { type: "tool.execution_start", data: { toolCallId: "shell", toolName: "bash", input: { command: "cat secret" } } },
      { type: "tool.execution_complete", data: { toolCallId: "shell", isError: true, error: "Permission denied" } },
      { type: "tool.execution_complete", data: { toolName: "read", status: "failed", error: "Read unavailable" } },
      { type: "tool.execution_complete", data: { toolName: "write", exitCode: 1, error: "Write failed" } },
    ]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("Failed tool calls observed: 3");
    expect(outcome.details).toContain("Last tool exit code: 1");
    for (const text of ["cat secret", "Permission denied", "Read unavailable", "Write failed"]) expect(outcome.details).not.toContain(text);
  });

  it("infers guard status without copying guard reasons or quoted assistant narratives", () => {
    writeEvents([
      { type: "assistant.message", data: { content: "Permission denied: private quoted command" } },
      { type: "guard.tool_denials_exceeded", data: { reason: "Permission denied: bash(rm denied-file)" } },
    ]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("Tool denial limit status: exceeded");
    expect(outcome.details).not.toContain("bash(rm denied-file)");
    expect(outcome.details).not.toContain("private quoted command");
  });

  it("counts Claude permission denials without copying their arguments or harness text", () => {
    writeEvents([{ type: "session.result", data: { permissionDenials: [{ tool_name: "Bash", tool_input: { command: "ls private" } }] } }]);
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), "not JSON\n[copilot-harness] permission denied by workflow tool permissions: bash(cat blocked)\n");
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.details).toContain("Permission denials observed: 1");
    for (const text of ["Bash", "ls private", "bash(cat blocked)"]) expect(outcome.details).not.toContain(text);
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

  it("retains outage classification when a nonzero exit has an independent provider HTTP error", () => {
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "1");
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), "CAPIError: 502 Bad Gateway private-provider");
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("engine_driver_failure");
    expect(outcome.failureCause).toBe("engine_outage");
    expect(outcome.details).toContain("Engine HTTP error codes: 502");
    expect(outcome.details).not.toContain("private-provider");
  });

  it.each([
    ["capi_server_error", [502], "engine_outage"],
    ["agentic_engine_timeout", [], "engine_outage"],
    ["sandbox_runtime_crash", [], "driver_crash"],
    ["", [503], "engine_outage"],
    ["", [], "driver_crash"],
    ["http_400_response_error", [400], "request_rejection"],
    ["invocation_cap_exceeded", [], "prompt_exhaustion"],
  ])("classifies %s using collector execution metadata", (category, errorCodes, failureCause) => {
    writeEvents([
      {
        type: "agent.execution",
        data: { categories: category ? [category] : [], errorCodes, errorTypes: [], exitCode: 1 },
      },
    ]);
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.failureCause).toBe(failureCause);
    expect(outcome.driverExitCode).toBe(1);
    expect(outcome.details).toContain(`Failure classification: ${failureCause}`);
    if (errorCodes.length) expect(outcome.details).toContain(`Engine HTTP error codes: ${errorCodes.join(", ")}`);
    expect(outcome.details).toContain(`Step budget state (LLM invocations): ${category === "invocation_cap_exceeded" ? "exceeded" : "unknown"}`);
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

  it("omits validation text completely, including overlapping runtime masks", () => {
    const secret = `sk-proj-${"a".repeat(64)}${"b".repeat(16)}`;
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), `::add-mask::${secret}\n`);
    const outcome = buildEmptyOutputOutcome([`Invalid output: ${secret}`], rootDir);
    expect(outcome.details).not.toContain("b".repeat(16));
    expect(outcome.details).toContain("Safe-output validation status: failed");
    expect(outcome.details).not.toContain("Invalid output");
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
    expect(outcome.details).toContain("Failed tool calls observed: 2");
    expect(outcome.details).toContain("Last tool call status: failed");
    for (const text of ["cat restricted-file", "create_issue", "Tool not available"]) expect(outcome.details).not.toContain(text);
  });

  it("excludes arbitrary validation and tool text, regardless of configured secrets or masks", () => {
    process.env.GH_AW_SECRET_NAMES = "OUTCOME_TEST";
    process.env.SECRET_OUTCOME_TEST = 'sensitive"value\\with\nnewline';
    writeEvents([{ type: "tool.execution_complete", data: { toolName: "read", success: false, error: { message: process.env.SECRET_OUTCOME_TEST } } }]);
    fs.writeFileSync(path.join(rootDir, "agent-stdio.log"), "::add-mask::runtime-private-value\n");
    const outcome = buildEmptyOutputOutcome([`Permission denied: ${process.env.SECRET_OUTCOME_TEST}`, "Unknown tool: runtime-private-value", "x".repeat(9000)], rootDir);
    expect(outcome.details).not.toContain("sensitive");
    expect(outcome.details).not.toContain("runtime-private-value");
    expect(outcome.details).toContain("Safe-output validation status: failed");
    expect(outcome.details).not.toContain("Permission denied");
    expect(outcome.details).not.toContain("x".repeat(100));
    expect(outcome.details.length).toBeLessThanOrEqual(8000 + "\n[Content truncated due to length]".length);
  });
});
