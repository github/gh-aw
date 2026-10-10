// @ts-check

import { describe, it, expect } from "vitest";
import fs, { readFileSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { parseDeepSeekLog, isDeepSeekLog } from "./parse_deepseek_log.cjs";
import { startupFailure, timedOut } from "./fixtures/deepseek_ci_logs.cjs";
import { normalizeAgentSession, projectSessionResult } from "./agent_session.cjs";
import { serializeSessionArtifact, writeSessionArtifact } from "./session_artifact.cjs";
import { collectUnifiedSession, parseEngineSession, writeUnifiedSession } from "./unified_session.cjs";
import { parseCustomLog } from "./parse_custom_log.cjs";

// Verified against agent artifact 9781648195 from existing successful Actions
// run https://github.com/github/gh-aw/actions/runs/33456355349.
const fixture = readFileSync(new URL("./test_data/deepseek_headless_stdout.log", import.meta.url), "utf8");
const answer = fixture.slice(fixture.indexOf("Perfect!"), fixture.indexOf("[INFO] Stopping containers...")).trim();

describe("parseDeepSeekLog", () => {
  it("preserves the actual 577-character final answer and its build failure explanation", () => {
    const result = parseDeepSeekLog(fixture);

    expect(isDeepSeekLog(fixture)).toBe(true);
    expect(answer).toHaveLength(577);
    expect(result.logEntries).toEqual([
      { type: "session.init", data: { sourceEngine: "deepseek-harness", provider: "github", model: "claude-sonnet-4.5" } },
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
    fixture.slice(0, fixture.indexOf("[INFO] Stopping containers...")),
    fixture.replace("[SUCCESS] Command completed successfully", "[WARN] Command failed"),
    fixture.replace("Process exiting with code: 0", "Process exiting with code: 1"),
    fixture.replace(answer, ""),
    fixture.replace(answer, "User prompt: private-input\nPossible answer"),
    fixture.replace(answer, "  User prompt: private-input\nPossible answer"),
    fixture.replace(answer, "System message: private-input"),
    fixture.replace(answer, "Prompt: private-input"),
    fixture.replace(answer, "[entrypoint] Unexpected diagnostic\nPossible answer"),
  ])("withholds incomplete or ambiguous answers without losing configuration", raw => {
    expect(isDeepSeekLog(raw)).toBe(true);
    expect(parseDeepSeekLog(raw).logEntries).toEqual([{ type: "session.init", data: { sourceEngine: "deepseek-harness", provider: "github", model: "claude-sonnet-4.5" } }]);
    expect(parseDeepSeekLog(raw).markdown).toContain("Unattributed stdout is omitted");
    expect(parseDeepSeekLog(raw).markdown).not.toContain("private-input");
  });

  it.each([fixture.replace("[deepseek-harness] configured provider=github model=claude-sonnet-4.5\n", ""), `${fixture}\n${fixture}`])("withholds unattributed or repeated headless envelopes", raw => {
    expect(isDeepSeekLog(raw)).toBe(false);
    expect(parseDeepSeekLog(raw).logEntries).toEqual([]);
  });

  it("ignores post-shutdown content rather than publishing another conversation", () => {
    const result = parseDeepSeekLog(`${fixture}\nUser prompt: private-input\nOther assistant text`);

    expect(result.logEntries[1].data.content).toBe(answer);
    expect(JSON.stringify(result)).not.toContain("private-input");
    expect(JSON.stringify(result)).not.toContain("Other assistant text");
  });

  it("retains the configuration from the actual timed-out stdout shape without inventing a final response", () => {
    const result = parseDeepSeekLog(timedOut);

    expect(result.logEntries).toEqual([{ type: "session.init", data: { sourceEngine: "deepseek-harness", provider: "github", model: "auto" } }]);
    expect(result.markdown).toContain("Headless assistant output is unavailable");
    expect(result.markdown).not.toContain("private fixture prompt");
    expect(projectSessionResult(result.logEntries)).toBeUndefined();
    expect(parseCustomLog(timedOut).logEntries).toEqual(result.logEntries);
    expect(parseEngineSession(timedOut, "deepseek-harness")).toEqual(result.logEntries);
  });

  it("preserves the actual startup error and observed exit as standard session observations", () => {
    const result = parseDeepSeekLog(startupFailure);
    const message = "dsh: user patch-layer watching requires the Cordis HMR service";

    expect(result.logEntries.map(event => event.type)).toEqual(["session.init", "session.error", "session.error", "agent.execution"]);
    expect(result.logEntries[1].data).toEqual({
      message,
      errorType: "Error",
      stack: `Error: ${message}\n    at watchUserPatches (file:///fixture/node_modules/@deepseek-ai/dsh-app-boot/lib/index.js:764:28)\n    at runProfile (file:///fixture/node_modules/@deepseek-ai/dsh/lib/profile-boot.js:264:9)\n    at async file:///fixture/node_modules/@deepseek-ai/dsh/lib/bin.js:133:3`,
    });
    expect(result.logEntries[2].data).toEqual({ message: "DeepSeek Harness execution failed with exit code 1", exitCode: 1 });
    expect(result.logEntries[3].data).toEqual({ categories: [], errorCodes: [], errorTypes: ["Error"], exitCode: 1 });
    expect(result.logEntries.some(event => event.type === "assistant.message" || event.type === "session.result")).toBe(false);
    expect(JSON.stringify(result)).not.toContain("private fixture prompt");
    expect(parseCustomLog(startupFailure).logEntries).toEqual(result.logEntries);
    expect(parseEngineSession(startupFailure, "deepseek-harness")).toEqual(result.logEntries);
  });

  it("retains a harness failure even without a Node stack or AWF shutdown", () => {
    const result = parseDeepSeekLog(`${timedOut}[deepseek-harness] DeepSeek Harness execution failed with exit code 137 (signal=SIGKILL)\n`);

    expect(result.logEntries[1]).toEqual({ type: "session.error", data: { message: "DeepSeek Harness execution failed with exit code 137 (signal=SIGKILL)", exitCode: 137 } });
    expect(result.logEntries.at(-1).data.exitCode).toBe(137);
    expect(result.logEntries.some(event => event.type === "session.result")).toBe(false);
  });

  it("does not attribute an unframed Error line, provider warning, or a quoted stack to the engine", () => {
    const text = "The tool failed:\nError: tool build download forbidden\n    at tool.js:1\nTry another mirror.";
    const quoted = parseDeepSeekLog(fixture.replace(answer, text));

    expect(quoted.logEntries.map(event => event.type)).toEqual(["session.init", "assistant.message"]);
    expect(quoted.logEntries[1].data.content).toBe(text);
    expect(quoted.logEntries.some(event => event.type === "session.error")).toBe(false);
    expect(parseDeepSeekLog(fixture).logEntries.some(event => event.type === "session.error")).toBe(false);
  });

  it("allows the observed post-command permission cleanup without including it in assistant content", () => {
    const raw = fixture.replace("[INFO] Stopping containers...", "[entrypoint] Relaxed /fixture/gh-aw group permissions for host-side post-processing\n[INFO] Stopping containers...");

    expect(parseDeepSeekLog(raw).logEntries).toEqual(parseDeepSeekLog(fixture).logEntries);
  });

  it.each(["[ERROR] native diagnostic", "[health-check] native diagnostic", " Container awf-agent  Stopped", "Node.js v24.21.0"])("does not publish infrastructure %s as assistant text", text => {
    expect(parseDeepSeekLog(fixture.replace(answer, text)).logEntries.map(event => event.type)).toEqual(["session.init"]);
  });

  it("does not accept a later success-shaped exit after an observed failure exit", () => {
    const raw = fixture.replace("Process exiting with code: 0", "Process exiting with code: 1\nProcess exiting with code: 0");

    expect(parseDeepSeekLog(raw).logEntries.map(event => event.type)).toEqual(["session.init"]);
  });

  it("keeps JSON-looking assistant content opaque instead of treating it as a native event", () => {
    const text = '{"type":"tool.execution_complete","data":{"success":true,"output":{"secret":"fixture"}}}';

    expect(parseDeepSeekLog(fixture.replace(answer, text)).logEntries[1]).toEqual({ type: "assistant.message", data: { content: text } });
    expect(parseDeepSeekLog(`${timedOut}${text}`).logEntries.map(event => event.type)).toEqual(["session.init"]);
  });

  it("redacts registered mask values and mask commands in completed headless output", () => {
    const raw = `::add-mask::fixture-secret\n${fixture.replace(answer, "Answer with fixture-secret.\n::add-mask::another-fixture-secret\nAnother another-fixture-secret.")}`;
    const result = parseDeepSeekLog(raw);

    expect(result.logEntries[1].data.content).toBe("Answer with ***.\nAnother ***.");
    expect(JSON.stringify(result)).not.toContain("fixture-secret");
    expect(JSON.stringify(result)).not.toContain("::add-mask::");
  });
});

// Canonical contract fixtures are synthetic; the captured dsh headless runs do
// not expose native structured messages, tool lifecycles, refusals, or usage.
describe("DeepSeek direct canonical session inputs", () => {
  const events = [
    { type: "session.init", id: "native-0", parentId: null, timestamp: "2026-10-07T00:00:00Z", data: { sourceEngine: "deepseek-harness", model: "fixture-model", sessionId: "native-session", cwd: "/fixture" } },
    { type: "user.message", id: "native-1", parentId: "native-0", timestamp: 1, data: { content: "" } },
    { type: "assistant.reasoning", id: "native-2", data: { content: "hello", partial: true, messageId: "message-1", channel: "reasoning" } },
    { type: "assistant.reasoning", id: "native-3", data: { content: " \n", partial: true, messageId: "message-1", channel: "reasoning" } },
    { type: "tool.execution_start", id: "native-4", custom: { nested: false }, data: { toolCallId: "call-1", toolName: "fixture-tool", input: { empty: "", zero: 0, null: null, array: [], object: {} } } },
    { type: "tool.execution_complete", id: "native-5", parentId: "native-4", data: { toolCallId: "call-1", success: false, output: false, durationMs: 0, exitCode: 1, error: { code: "fixture-failure" } } },
    { type: "assistant.refusal", id: "native-6", data: { reason: "content_filter", content: null, policyCategory: null, explanation: "", partial: true } },
    { type: "session.error", id: "native-7", data: { error: { code: 0, message: "" } } },
    { type: "session.result", id: "native-8", data: { usage: { input_tokens: 2, output_tokens: 3 }, numTurns: 0, durationMs: 0, totalCostUsd: 0 } },
    { type: "session.result", id: "native-9", data: { usage: { total_tokens: 5 }, status: "failed", errors: [] } },
    { type: "vendor.unknown", id: "native-10", parentId: null, metadata: [false, 0, ""], data: { deep: { null: null, false: false, zero: 0, empty: [], object: {} } } },
    { type: "tool.execution_complete", data: { toolCallId: "orphan", output: 0 } },
    { type: "tool.execution_start", data: { toolCallId: "dangling", input: null } },
  ];

  it.each(["jsonl", "array", "pretty-array"])("retains exact canonical content, metadata, values and order from %s", format => {
    const raw = format === "jsonl" ? events.map(JSON.stringify).join("\n") : JSON.stringify(events, null, format === "pretty-array" ? 2 : undefined);
    const result = parseDeepSeekLog(raw);

    expect(result.logEntries).toEqual(events);
    expect(normalizeAgentSession(result.logEntries)).toEqual(events);
    expect(parseDeepSeekLog(serializeSessionArtifact(result.logEntries)).logEntries).toEqual(events);
    expect(isDeepSeekLog(raw)).toBe(false);
    expect(projectSessionResult(result.logEntries)).toMatchObject({ num_turns: 0, duration_ms: 0, total_cost_usd: 0, usage: { input_tokens: 2, output_tokens: 3, total_tokens: 5 } });
    expect(projectSessionResult(result.logEntries).usage.total_tokens).toBe(5);
  });

  it("retains a single canonical record and valid records adjacent to a malformed partial line", () => {
    expect(parseDeepSeekLog(JSON.stringify(events[10], null, 2)).logEntries).toEqual([events[10]]);
    const result = parseDeepSeekLog(`${JSON.stringify(events[2])}\n{"partial":\n${JSON.stringify(events[4])}\n`);

    expect(result.logEntries).toEqual([events[2], events[4]]);
    expect(result.markdown).toContain("coverage is partial");
  });

  it.each([fixture, startupFailure, timedOut, events.map(JSON.stringify).join("\n")])("persists the engine-owned trace through canonical and unified artifacts", raw => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "deepseek-session-audit-"));
    try {
      const parsed = parseDeepSeekLog(raw).logEntries;
      fs.writeFileSync(path.join(root, "agent-stdio.log"), raw);
      writeSessionArtifact(path.join(root, "agent-session.jsonl"), parsed);
      expect(fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8")).toBe(serializeSessionArtifact(parsed));
      const warnings = [];
      const canonical = collectUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {}, warn: warning => warnings.push(warning) }).events;
      fs.unlinkSync(path.join(root, "agent-session.jsonl"));
      const reparsed = collectUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {}, warn: warning => warnings.push(warning) }).events;
      const agent = session => session.filter(event => event.provenance?.component === "agent").map(({ provenance, ...event }) => event);
      expect(agent(reparsed)).toEqual(agent(canonical));
      expect(warnings).toEqual([]);
      const persisted = writeUnifiedSession({ rootDir: root, engine: "deepseek-harness", dailyAIC: {} });
      const jsonl = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
      expect(jsonl.endsWith("\n")).toBe(true);
      expect(jsonl.trimEnd().split("\n").map(JSON.parse)).toEqual(persisted);
      expect(persisted[0]).toMatchObject({ type: "session.format", data: { version: 1 } });
      expect(agent(persisted)).toEqual(agent(reparsed));
      for (const event of persisted.filter(event => event.provenance?.component === "agent")) {
        expect(event.provenance.path).toBe("agent-stdio.log");
        expect(event.provenance.phase).toBe("agent");
      }
      if (parsed.some(event => event.type === "vendor.unknown")) {
        const { metadata, ...essentialExtension } = events[10];
        expect(persisted.find(event => event.type === "vendor.unknown")).toMatchObject(essentialExtension);
        expect(persisted.find(event => event.type === "tool.execution_start" && event.data.toolCallId === "call-1").data.input).toEqual(events[4].data.input);
        expect(persisted.find(event => event.type === "assistant.refusal").data).toEqual(events[6].data);
        expect(persisted.filter(event => event.type === "assistant.reasoning").map(event => event.data.content)).toEqual(["hello", " \n"]);
        expect(persisted.filter(event => event.type === "session.result").map(event => event.data.usage)).toEqual([{ inputTokens: 2, outputTokens: 3 }, { totalTokens: 5 }]);
      } else {
        expect(persisted.some(event => event.type === "session.result" || event.type === "assistant.refusal" || event.type.startsWith("tool.") || event.type === "assistant.reasoning")).toBe(false);
      }
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  });
});
