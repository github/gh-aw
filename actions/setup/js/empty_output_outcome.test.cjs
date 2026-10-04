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
    fs.rmSync(rootDir, { recursive: true, force: true });
    delete process.env.SECRET_OUTCOME_TEST;
    delete process.env.GH_AW_SECRET_NAMES;
    delete global.core;
  });

  it("records incomplete rather than claiming an intentional noop without evidence", () => {
    expect(buildEmptyOutputOutcome([], rootDir)).toEqual({
      type: "report_incomplete",
      reason: "missing_terminal_safe_output",
      details: "Agent finished without emitting a terminal safe output; task completion could not be confirmed.",
    });
  });

  it.each([1, 137, 139])("classifies a silent driver exit %s separately from agent behavior", exitCode => {
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), String(exitCode));
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("engine_driver_failure");
    expect(outcome.details).toContain(`Driver exit code: ${exitCode}`);
  });

  it("classifies a malformed CLI invocation without copying payloads from the audit log", () => {
    fs.mkdirSync(path.join(rootDir, "mcp-cli-audit"));
    fs.writeFileSync(path.join(rootDir, "mcp-cli-audit/safeoutputs.jsonl"), 'not JSON\n{"event":"parse_args_error","tool":"noop","error":"private payload"}\n');
    fs.writeFileSync(path.join(rootDir, "agent_execution_exit_code.txt"), "0");
    const outcome = buildEmptyOutputOutcome([], rootDir);
    expect(outcome.reason).toBe("safeoutputs_cli_error");
    expect(outcome.details).toContain("safeoutputs <tool> --help");
    expect(outcome.details).not.toContain("private payload");
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
