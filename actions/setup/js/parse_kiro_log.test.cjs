import { describe, expect, it } from "vitest";
import { parseKiroLog } from "./parse_kiro_log.cjs";
import { isSessionEvent } from "./agent_session.cjs";
import kiroCI from "./fixtures/kiro_ci_sessions.cjs";

const header = "kiro-cli 2.16.1\n";
const byType = (result, type) => result.logEntries.filter(event => event.type === type);

describe("Kiro headless plaintext conversation parser", () => {
  it("preserves observed multiline assistant turns, shell arguments, output, and build exit 2", () => {
    const result = parseKiroLog(
      header +
        `\u001b[m> \u001b[0mI'll run the smoke test.\u001b[0m

I will run the following command: \u001b[mGOCACHE=/cache make build 2>&1 | tail -20; echo "EXIT:\${PIPESTATUS[0]}"\u001b[0m (using tool: shell)
Purpose: Build gh-aw project

go: download go1.26.8: tls: failed to verify certificate
make: *** [Makefile:42: build] Error 1
EXIT:2
 - Completed in 0.669s

> Build failed due to a network/toolchain download issue.
- File Writing: PASS
- Build: FAIL
 ▸ Credits: 1.17 • Time: 41s
[entrypoint] secret infrastructure noise
Process exiting with code: 0`
    );
    expect(result.logEntries.every(isSessionEvent)).toBe(true);
    expect(byType(result, "assistant.message").map(event => event.data.content)).toEqual(["I'll run the smoke test.", "Build failed due to a network/toolchain download issue.\n- File Writing: PASS\n- Build: FAIL"]);
    expect(byType(result, "tool.execution_start")[0].data).toMatchObject({
      toolName: "shell",
      input: { command: 'GOCACHE=/cache make build 2>&1 | tail -20; echo "EXIT:${PIPESTATUS[0]}"' },
      description: "Build gh-aw project",
    });
    expect(byType(result, "tool.execution_complete")[0].data).toMatchObject({ toolName: "shell", durationMs: 669, exitCode: 2, success: false, error: { code: 2, message: expect.stringContaining("tls: failed to verify certificate") } });
    expect(result.markdown).toContain("Build failed due to a network/toolchain download issue.");
    expect(result.markdown).not.toContain("secret infrastructure noise");
    expect(result.markdown).not.toContain("\u001b");
    expect(byType(result, "session.result")).toEqual([]);
    for (const event of result.logEntries) {
      expect(event).not.toHaveProperty("timestamp");
      for (const key of ["toolCallId", "sessionId", "numTurns", "usage"]) expect(event.data).not.toHaveProperty(key);
    }
  });

  it("preserves assistant paragraph whitespace apart from framing and ANSI", () => {
    const result = parseKiroLog(header + ">   Indented first paragraph.  \n\n  Second paragraph.\n\n> Next paragraph.\n\n");
    expect(byType(result, "assistant.message").map(event => event.data.content)).toEqual(["  Indented first paragraph.  \n\n  Second paragraph.", "Next paragraph."]);
  });

  it("does not turn elapsed completion markers or textual success claims into successful outcomes", () => {
    const result = parseKiroLog(header + "> Checking.\nI will run the following command: echo ok (using tool: shell)\nok\n - Completed in 0.5s\n> Passed.");
    expect(byType(result, "tool.execution_complete")[0].data).toMatchObject({ toolName: "shell", output: "ok", durationMs: 500 });
    expect(byType(result, "tool.execution_complete")[0].data).not.toHaveProperty("success");
    expect(byType(result, "tool.execution_complete")[0].data).not.toHaveProperty("exitCode");
  });

  it("retains orphan completion observations without invalid durations or invented owners", () => {
    const result = parseKiroLog(header + `> Checking.\n - Completed in 1s\nI will run the following command: run (using tool: shell)\n - Completed in ${"9".repeat(400)}s`);
    expect(byType(result, "tool.execution_complete")).toHaveLength(2);
    expect(byType(result, "tool.execution_complete")[0].data).toEqual({ durationMs: 1000 });
    expect(byType(result, "tool.execution_complete")[1].data).not.toHaveProperty("durationMs");
    expect(byType(result, "tool.execution_complete")[0].data).not.toHaveProperty("success");
  });

  it("accepts only unambiguous explicitly observed exit codes", () => {
    const command = 'work; echo "EXIT:${PIPESTATUS[0]}"';
    const result = parseKiroLog(header + `> Checking.\nI will run the following command: ${command} (using tool: shell)\nEXIT:0\n - Completed in 0s`);
    expect(byType(result, "tool.execution_complete")[0].data).toMatchObject({ success: true, exitCode: 0, durationMs: 0 });
    for (const output of ["EXIT:999", "EXIT:-1", "EXIT:0\nEXIT:2"]) {
      const ambiguous = parseKiroLog(header + `> Checking.\nI will run the following command: ${command} (using tool: shell)\n${output}\n - Completed in 1s`);
      expect(byType(ambiguous, "tool.execution_complete")[0].data).not.toHaveProperty("success");
    }
    const literal = parseKiroLog(header + "> Checking.\nI will run the following command: echo EXIT:0 (using tool: shell)\nEXIT:0\n - Completed in 1s");
    expect(byType(literal, "tool.execution_complete")[0].data).not.toHaveProperty("success");
  });

  it("retains multiline shell announcements without treating their heredoc as assistant messages", () => {
    const result = parseKiroLog(
      header +
        '> Creating an issue.\nI will run the following command: cat <<\'EOF\' > issue.md\n> quoted content\n## Results\nEOF\nsafeoutputs create_issue . (using tool: shell)\nPurpose: Create issue\n{"result":"success"}\n - Completed in 0.250s'
    );
    expect(byType(result, "tool.execution_start")[0].data.input.command).toBe("cat <<'EOF' > issue.md\n> quoted content\n## Results\nEOF\nsafeoutputs create_issue .");
    expect(byType(result, "assistant.message")).toHaveLength(1);
    expect(byType(result, "tool.execution_complete")[0].data.output).toBe('{"result":"success"}');
    expect(byType(result, "tool.execution_complete")[0].data).not.toHaveProperty("success");
  });

  it("preserves interleaved observed completions without guessing their tool owners", () => {
    const result = parseKiroLog(
      header +
        `> Running in parallel.
I will run the following command: cat smoke.txt (using tool: shell)
Purpose: Read smoke test
Searching for symbols matching: "list_pull_requests" (using tool: code)Smoke test passed
 - Completed in 0.5s
  1. Function listOpenPullRequests at file.cjs:17:1
 - Completed in 6.89s
> Done.
Querying available agents for task delegation (using tool: subagent) - Completed in 0.0s`
    );
    expect(byType(result, "tool.execution_start").map(event => event.data.toolName)).toEqual(["shell", "code", "subagent"]);
    expect(byType(result, "tool.execution_start")[1].data.input).toEqual({ query: '"list_pull_requests"' });
    const completions = byType(result, "tool.execution_complete");
    expect(completions).toHaveLength(3);
    expect(completions[0].data).toMatchObject({ output: "Smoke test passed", durationMs: 500 });
    expect(completions[1].data.output).toContain("Function listOpenPullRequests");
    for (const event of completions.slice(0, 2)) {
      expect(event.data).not.toHaveProperty("toolName");
      expect(event.data).not.toHaveProperty("toolCallId");
    }
    expect(completions[2].data).toMatchObject({ toolName: "subagent", durationMs: 0 });
    expect(byType(result, "tool.execution_start")[2].data).not.toHaveProperty("input");
  });

  it("does not invent completions, arguments, or terminal results for partial logs", () => {
    const partial = parseKiroLog(header + "> Checking.\nI will run the following command: echo run (using tool: shell)\npartial output");
    expect(byType(partial, "tool.execution_start")).toHaveLength(1);
    expect(byType(partial, "tool.execution_complete")).toEqual([]);
    expect(byType(partial, "session.result")).toEqual([]);
    expect(byType(partial, "tool.output")[0].data).toEqual({ toolName: "shell", output: "partial output", partial: true });
    const incomplete = parseKiroLog(header + "> Checking.\nI will run the following command: cat <<'EOF'\nincomplete PRIVATE command");
    expect(byType(incomplete, "tool.execution_start")).toEqual([]);
    expect(incomplete.markdown).not.toContain("PRIVATE");
  });

  it("preserves legacy tool-output indentation and trailing spaces", () => {
    const result = parseKiroLog(header + "> Checking.\nI will run the following command: printf example (using tool: shell)\n\n  example  \n[INFO] observed tool output\n - Completed in 0s");
    expect(byType(result, "tool.execution_complete")[0].data.output).toBe("  example  \n[INFO] observed tool output");
  });

  it("retains partial output before another invocation rather than discarding or completing it", () => {
    const result = parseKiroLog(header + "> Checking.\nI will run the following command: first (using tool: shell)\npartial first output\nI will run the following command: second (using tool: shell)\npartial second output");
    expect(result.logEntries.map(event => event.type)).toEqual(["session.init", "assistant.message", "tool.execution_start", "tool.output", "tool.execution_start", "tool.output"]);
    expect(byType(result, "tool.output").map(event => event.data.output)).toEqual(["partial first output", "partial second output"]);
    expect(byType(result, "tool.output")[1].data).not.toHaveProperty("toolName");
  });

  it("keeps partial tool output without consuming unframed shutdown diagnostics", () => {
    const result = parseKiroLog(header + "> Checking.\nI will run the following command: first (using tool: shell)\npartial output\n[entrypoint] Cleanup\nPRIVATE_SHUTDOWN\n> PRIVATE_SHUTDOWN_ANSWER");
    expect(byType(result, "tool.output")[0].data.output).toBe("partial output");
    expect(JSON.stringify(result.logEntries)).not.toContain("PRIVATE");
  });

  it("does not switch legacy layout for compact-looking text inside a command", () => {
    const result = parseKiroLog(header + "> Checking.\nI will run the following command: cat <<'EOF'\n[tool] Running: quoted command\n[INFO] quoted information\nEOF (using tool: shell)\n - Completed in 0s");
    expect(byType(result, "tool.execution_start")[0].data.input.command).toBe("cat <<'EOF'\n[tool] Running: quoted command\n[INFO] quoted information\nEOF");
  });

  it("accepts signed legacy tool-only and completion-only partial traces", () => {
    expect(byType(parseKiroLog(header + "I will run the following command: printf example (using tool: shell)"), "tool.execution_start")).toHaveLength(1);
    expect(byType(parseKiroLog(header + " - Completed in 0s"), "tool.execution_complete")[0].data).toEqual({ durationMs: 0 });
  });

  it("omits prompt echoes, masks registered secrets, and excludes startup and shutdown diagnostics", () => {
    const result = parseKiroLog(
      `PRIVATE_SYSTEM_PROMPT\n::add-mask::PRIVATE_SECRET\n[INFO] infrastructure\n${header}` +
        "Prompt: PRIVATE_USER_PROMPT\ncontinued private prompt\n> Observed response.\nUser: PRIVATE_FOLLOWUP\ncontinued private followup\n> Next response.\nI will run the following command: echo PRIVATE_SECRET (using tool: shell)\n[info] [bridge] diagnostic PRIVATE_DIAGNOSTIC\nPRIVATE_SECRET\n - Completed in 1s\n> Done.\n ▸ Credits: 1\n[entrypoint] PRIVATE_SHUTDOWN\nContainer private Creating"
    );
    expect(result.markdown).toContain("Observed response.");
    expect(result.markdown).toContain("***");
    expect(result.markdown).not.toContain("PRIVATE");
    expect(JSON.stringify(result.logEntries)).not.toContain("PRIVATE");
    expect(result.mcpFailures).toEqual([]);
    expect(result.maxTurnsHit).toBe(false);
  });

  describe("Kiro 2.27 compact headless conversation parser", () => {
    it("maps the sanitized successful CI excerpt to canonical tools and buffered answers", () => {
      const result = parseKiroLog(kiroCI.success);
      expect(result.logEntries.every(isSessionEvent)).toBe(true);
      expect(byType(result, "session.init")[0].data).toEqual({ sourceEngine: "kiro", agentVersion: "2.27.1" });
      expect(byType(result, "tool.execution_start").map(event => event.data.toolName)).toEqual(["shell", "shell", "shell", "code", "aws", "shell", "shell"]);
      expect(byType(result, "tool.execution_start")[2].data).toMatchObject({
        input: { command: "node -e '\nconst example = false;\nconsole.log(example);\nconsole.log(\"truncated preview".padEnd(197, "x") + "..." },
        inputTruncated: true,
      });
      expect(byType(result, "tool.execution_start")[3].data.input).toEqual({ query: "exampleSymbol" });
      expect(byType(result, "tool.execution_complete")).toHaveLength(7);
      expect(byType(result, "tool.execution_complete")[0].data).toEqual({ toolName: "shell", status: "Completed" });
      for (const event of byType(result, "tool.execution_complete").slice(1, 3)) expect(event.data).toEqual({ status: "Completed" });
      expect(byType(result, "assistant.message").map(event => event.data.content)).toEqual(["Checking the example.Example inspected.\n\nResults:\n- Example: PASS", "Example complete.\n\nNo additional action is needed."]);
      expect(byType(result, "session.result")).toEqual([]);
      expect(result.logEntries.every(event => !event.type.startsWith("kiro."))).toBe(true);
    });

    it("retains the failed CI excerpt's anonymous/orphan failures without failing the session", () => {
      const result = parseKiroLog(kiroCI.failure);
      const completions = byType(result, "tool.execution_complete");
      expect(completions).toHaveLength(9);
      expect(completions.filter(event => event.data.success === false).map(event => event.data)).toEqual([
        { status: "Failed", success: false },
        { status: "Failed", success: false },
      ]);
      expect(byType(result, "session.result")).toEqual([]);
      for (const event of result.logEntries) {
        expect(event).not.toHaveProperty("timestamp");
        for (const field of ["sessionId", "toolCallId", "durationMs", "numTurns", "usage", "totalCostUsd"]) expect(event.data).not.toHaveProperty(field);
      }
    });

    it("preserves exact multiline commands, literal JSON, and assistant whitespace (synthetic)", () => {
      const result = parseKiroLog(
        `${header}[kiro-harness] Kiro CLI execution started\n[tool] Running: cat <<'EOF'\n> quoted text\n{"type":"assistant.message","data":{"content":false}}\n[INFO] quoted information\n  spaced  \nEOF\n[tool] status: Completed\n  Answer.  \n\n  Next paragraph.  \n`
      );
      expect(byType(result, "tool.execution_start")[0].data.input.command).toBe('cat <<\'EOF\'\n> quoted text\n{"type":"assistant.message","data":{"content":false}}\n[INFO] quoted information\n  spaced  \nEOF');
      expect(byType(result, "assistant.message").map(event => event.data.content)).toEqual(["  Answer.  \n\n  Next paragraph.  "]);
    });

    it("retains observed starts, orphan statuses, and unknown status extensions in partial traces (synthetic)", () => {
      const dangling = parseKiroLog(header + "[tool] Running: printf example");
      expect(byType(dangling, "tool.execution_start")[0].data).toEqual({ toolName: "shell", input: { command: "printf example" } });
      expect(byType(dangling, "tool.execution_complete")).toEqual([]);
      const orphan = parseKiroLog("[kiro-harness] Kiro CLI execution started\n[tool] status: Failed\n  Partial answer.  ");
      expect(byType(orphan, "session.init")).toEqual([]);
      expect(byType(orphan, "tool.execution_complete")[0].data).toEqual({ status: "Failed", success: false });
      expect(byType(orphan, "assistant.message")[0].data.content).toBe("  Partial answer.  ");
      const unknown = parseKiroLog(header + "[tool] status: Waiting\n[tool] Future operation: example");
      expect(byType(unknown, "kiro.tool_status")[0].data).toEqual({ status: "Waiting" });
      expect(byType(unknown, "kiro.tool_announcement")[0].data).toEqual({ content: "Future operation: example" });
      expect(byType(unknown, "tool.execution_start")).toEqual([]);
      expect(byType(unknown, "tool.execution_complete")).toEqual([]);
      const ambiguous = parseKiroLog(header + "[tool] Running: known\n[tool] Future operation: unknown\n[tool] status: Completed");
      expect(byType(ambiguous, "tool.execution_complete")[0].data).not.toHaveProperty("toolName");
    });

    it("does not classify a short literal ellipsis as truncated input (synthetic)", () => {
      const result = parseKiroLog(header + "[tool] Running: echo ...\n[tool] status: Completed");
      expect(byType(result, "tool.execution_start")[0].data).toEqual({ toolName: "shell", input: { command: "echo ..." } });
    });

    it("accepts an assistant-only signed execution without fabricating initialization (synthetic)", () => {
      expect(parseKiroLog("[kiro-harness] Kiro CLI execution started\n  Observed answer.  ").logEntries).toEqual([{ line: 2, type: "assistant.message", data: { content: "  Observed answer.  " } }]);
      expect(parseKiroLog("[kiro-harness] Kiro CLI execution started\n").logEntries).toEqual([]);
    });

    it("omits prompt echoes and infrastructure and applies registered masks (synthetic)", () => {
      const result = parseKiroLog(
        "PRIVATE_PROMPT\n::add-mask::PRIVATE_SECRET\n" +
          header +
          "[kiro-harness] Kiro CLI execution started\nPrompt: PRIVATE_ECHO\ncontinued private prompt\n[tool] Running: echo PRIVATE_SECRET\n[tool] status: Completed\nObserved answer.\n[entrypoint] PRIVATE_INFRA\nPRIVATE_SHUTDOWN\nProcess exiting with code: 0"
      );
      expect(JSON.stringify(result.logEntries)).not.toContain("PRIVATE");
      expect(byType(result, "tool.execution_start")[0].data.input.command).toBe("echo ***");
      expect(byType(result, "assistant.message")[0].data.content).toBe("Observed answer.");
    });

    it("maps declared harness execution errors to session diagnostics, not assistant answers (synthetic)", () => {
      const message = "Kiro CLI execution failed with exit code 3; required MCP server startup failed; check .kiro/settings/mcp.json and MCP gateway logs";
      const result = parseKiroLog(`[kiro-harness] ${message}\n[kiro-harness] Cleaned up Kiro CLI installation; total duration=0ms; exit code=3\nProcess exiting with code: 3`);
      expect(result.logEntries).toEqual([{ line: 1, type: "session.result", data: { sourceEngine: "kiro", sourceType: "kiro-harness", status: "failed", errors: [message] } }]);
      expect(byType(result, "assistant.message")).toEqual([]);
    });

    it("does not reinterpret quoted harness errors inside a command as session diagnostics (synthetic)", () => {
      const message = "[kiro-harness] Kiro CLI execution failed with exit code 3";
      const result = parseKiroLog(header + "[tool] Running: cat <<'EOF'\n" + message + "\nEOF\n[tool] status: Completed\nObserved answer.");
      expect(byType(result, "tool.execution_start")[0].data.input.command).toBe("cat <<'EOF'\n" + message + "\nEOF");
      expect(byType(result, "session.result")).toEqual([]);
    });
  });

  it.each([
    "",
    "PRIVATE_PROMPT mentioning kiro-cli and tools",
    "> PRIVATE_PROMPT\nI will run the following command: echo secret (using tool: shell)",
    header + "Prompt: PRIVATE_PROMPT",
    header + "> User: PRIVATE_PROMPT\nraw user input",
    header + "> Prompt: PRIVATE_PROMPT",
    '{"type":"assistant","content":"PRIVATE_PROMPT"}',
  ])("does not publish unsupported or unsigned content %j", content => {
    const result = parseKiroLog(content);
    expect(result.logEntries).toEqual([]);
    expect(result.markdown).not.toContain("PRIVATE");
  });
});
