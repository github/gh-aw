import { describe, expect, it } from "vitest";
import { parseKiroLog } from "./parse_kiro_log.cjs";
import { isSessionEvent } from "./agent_session.cjs";

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

  it("does not emit invalid durations or orphan completion observations", () => {
    const result = parseKiroLog(header + `> Checking.\n - Completed in 1s\nI will run the following command: run (using tool: shell)\n - Completed in ${"9".repeat(400)}s`);
    expect(byType(result, "tool.execution_complete")).toHaveLength(1);
    expect(byType(result, "tool.execution_complete")[0].data).not.toHaveProperty("durationMs");
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
    const incomplete = parseKiroLog(header + "> Checking.\nI will run the following command: cat <<'EOF'\nincomplete PRIVATE command");
    expect(byType(incomplete, "tool.execution_start")).toEqual([]);
    expect(incomplete.markdown).not.toContain("PRIVATE");
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
