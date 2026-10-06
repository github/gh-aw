// @ts-check

import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { parseDeepSeekLog, isDeepSeekLog } from "./parse_deepseek_log.cjs";

const fixture = readFileSync(new URL("./test_data/deepseek_headless_stdout.log", import.meta.url), "utf8");
const answer = fixture.slice(fixture.indexOf("Perfect!"), fixture.indexOf("[INFO] Stopping containers...")).trim();

describe("parseDeepSeekLog", () => {
  it("preserves the actual 577-character final answer and its build failure explanation", () => {
    const result = parseDeepSeekLog(fixture);

    expect(isDeepSeekLog(fixture)).toBe(true);
    expect(answer).toHaveLength(577);
    expect(result.logEntries).toEqual([
      { type: "session.init", data: { sourceEngine: "deepseek-harness", model: "claude-sonnet-4.5" } },
      { type: "assistant.message", data: { content: answer } },
    ]);
    expect(result.markdown).toContain("Perfect! All smoke tests completed.");
    expect(result.markdown).toContain("Go toolchain download restriction from the proxy.");
    expect(result.markdown).toContain("## Smoke Test Results: ❌ FAIL");
    expect(result.mcpFailures).toEqual([]);
    expect(result.maxTurnsHit).toBe(false);
  });

  it("omits the user prompt, credentials, and infrastructure outside the answer", () => {
    const raw = fixture.replace("fixture user prompt; do not publish", "User prompt: private-input; credential=fixture-secret-value");
    const result = parseDeepSeekLog(raw);
    const published = JSON.stringify(result);

    expect(published).not.toContain("private-input");
    expect(published).not.toContain("fixture-secret-value");
    expect(published).not.toContain("COPILOT_GITHUB_TOKEN");
    expect(published).not.toContain("models fetch returned 401");
    expect(published).not.toContain("Stopping containers");
    expect(result.logEntries[1].data.content).toBe(answer);
  });

  it("preserves multiline answers while normalizing CRLF", () => {
    expect(parseDeepSeekLog(fixture.replace(/\n/g, "\r\n")).logEntries).toEqual(parseDeepSeekLog(fixture).logEntries);
  });

  it("preserves paragraph indentation and trailing spaces except for ANSI framing", () => {
    const text = "  First paragraph with preserved spaces.  \n\n\tSecond paragraph.\n\nFinal paragraph.  ";
    const raw = fixture.replace(answer, `\u001b[32m${text}\u001b[0m`);
    const result = parseDeepSeekLog(raw);

    expect(result.logEntries[1].data.content).toBe(text);
  });

  it("accepts ANSI-colored framing without retaining escape sequences", () => {
    const raw = fixture.replace("[deepseek-harness]", "\u001b[32m[deepseek-harness]\u001b[0m").replace("[INFO] Stopping containers...", "\u001b[32m[INFO] Stopping containers...\u001b[0m");

    expect(parseDeepSeekLog(raw).logEntries).toEqual(parseDeepSeekLog(fixture).logEntries);
  });

  it("does not fill missing source fields with generated canonical identifiers or metrics", () => {
    for (const event of parseDeepSeekLog(fixture).logEntries) {
      for (const key of ["id", "sessionId", "toolCallId", "timestamp", "turnId", "numTurns", "turns", "usage", "inputTokens", "outputTokens", "totalTokens", "success", "status"]) {
        expect(event).not.toHaveProperty(key);
        expect(event.data).not.toHaveProperty(key);
      }
    }
  });

  it("does not invent tool calls, errors, success, usage, or reasoning from the smoke summary", () => {
    const result = parseDeepSeekLog(fixture);

    expect(result.logEntries.map(event => event.type)).toEqual(["session.init", "assistant.message"]);
    expect(result.logEntries.some(event => event.type.startsWith("tool.") || event.type === "session.error" || event.type === "session.result" || event.type === "assistant.reasoning")).toBe(false);
  });

  it("does not interpret tool-looking answer text as a native tool transcript", () => {
    const text = 'Tool call: bash {"command":"make build"}\nOutcome: build download forbidden.';
    const result = parseDeepSeekLog(fixture.replace(answer, text));

    expect(result.logEntries[1]).toEqual({ type: "assistant.message", data: { content: text } });
    expect(result.logEntries).toHaveLength(2);
  });

  it.each(["", "Some unrelated stdout", answer, '{"type":"assistant","message":{"content":"unrelated"}}'])("withholds unrecognized input %s", raw => {
    const result = parseDeepSeekLog(raw);

    expect(isDeepSeekLog(raw)).toBe(false);
    expect(result.logEntries).toEqual([]);
    expect(result.markdown).toContain("Raw content is omitted");
    if (raw) expect(result.markdown).not.toContain(raw);
  });

  it.each([
    fixture.replace("[deepseek-harness] configured provider=github model=claude-sonnet-4.5\n", ""),
    fixture.slice(0, fixture.indexOf("[INFO] Stopping containers...")),
    fixture.replace("[SUCCESS] Command completed successfully", "[WARN] Command failed"),
    fixture.replace("Process exiting with code: 0", "Process exiting with code: 1"),
    fixture.replace(answer, ""),
    fixture.replace(answer, "User prompt: private-input\nPossible answer"),
    fixture.replace(answer, "  User prompt: private-input\nPossible answer"),
    fixture.replace(answer, "System message: private-input"),
    fixture.replace(answer, "Prompt: private-input"),
    fixture.replace(answer, "[entrypoint] Unexpected diagnostic\nPossible answer"),
    `${fixture}\n${fixture}`,
  ])("withholds incomplete or ambiguous headless envelopes", raw => {
    expect(isDeepSeekLog(raw)).toBe(false);
    expect(parseDeepSeekLog(raw).logEntries).toEqual([]);
    expect(parseDeepSeekLog(raw).markdown).not.toContain("private-input");
  });

  it("ignores post-shutdown content rather than publishing another conversation", () => {
    const result = parseDeepSeekLog(`${fixture}\nUser prompt: private-input\nOther assistant text`);

    expect(result.logEntries[1].data.content).toBe(answer);
    expect(JSON.stringify(result)).not.toContain("private-input");
    expect(JSON.stringify(result)).not.toContain("Other assistant text");
  });
});
