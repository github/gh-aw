import { afterEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { parseOpenCodeLog } from "./parse_opencode_log.cjs";
import { parseEngineSession, collectUnifiedSession, writeUnifiedSession } from "./unified_session.cjs";
import { isSessionEvent } from "./agent_session.cjs";

const req = require;
const { logs: startupLogs } = req("./fixtures/opencode_ci_startup.cjs");
const synthetic = fs.readFileSync(path.join(__dirname, "test_data/opencode_synthetic_cli.jsonl"), "utf8");
const temporaryRoots = [];
afterEach(() => {
  for (const root of temporaryRoots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
});

describe("OpenCode log parser and artifact integration", () => {
  it.each(Object.entries(startupLogs))("does not reinterpret startup-only CI run %s as assistant or measured accounting evidence", (id, log) => {
    const parsed = parseOpenCodeLog(log);
    expect(parsed.logEntries).toEqual([]);
    expect(parsed.markdown).toContain("not recognized as OpenCode JSON events");
    expect(parsed.markdown).not.toContain("sanitized prompt");
    expect(parsed.mcpFailures).toEqual([]);
    expect(parsed.maxTurnsHit).toBe(false);
    expect(parseEngineSession(log, "opencode")).toEqual([]);
  });

  it("maps the pinned CLI protocol and renders canonical observations (synthetic fixture)", () => {
    const parsed = parseOpenCodeLog(synthetic);
    expect(parsed.logEntries.every(isSessionEvent)).toBe(true);
    expect(parsed.logEntries.map(event => event.type)).toEqual([
      "opencode.step_start",
      "assistant.reasoning",
      "assistant.message",
      "tool.execution_start",
      "tool.execution_complete",
      "assistant.message",
      "opencode.step_finish",
      "session.result",
    ]);
    expect(parsed.logEntries.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["  before\r\n", "\t after\n"]);
    expect(parsed.logEntries.at(-1).data).toMatchObject({ totalCostUsd: 0, usage: { input_tokens: 12, output_tokens: 4, cache_read_input_tokens: 3, cache_creation_input_tokens: 1, reasoning_output_tokens: 2, total_tokens: 22 } });
    expect(parsed.logEntries.at(-1).data).not.toHaveProperty("numTurns");
    expect(parsed.markdown).toContain("before");
    expect(parsed.markdown).toContain("after");
    expect(parsed.markdown).toContain("printf");
    expect(parsed.markdown).not.toContain("**Turns:**");
    expect(parseEngineSession(synthetic, "opencode")).toEqual(parsed.logEntries);
  });

  it("recovers valid adjacent records without treating arbitrary logs or failed-looking message text as protocol", () => {
    const message = { type: "text", sessionID: "s", part: { type: "text", text: "MCP server x failed; max turns reached\n" } };
    const extension = { type: "vendor.event", data: { flag: false }, id: "native-id" };
    const parsed = parseOpenCodeLog(["debug text", "{broken", "null", "0", '{"type":"tool.call","msg":"unrecognized"}', JSON.stringify(message), "[truncated", JSON.stringify(extension), '{"type":"text","part":'].join("\n"));
    expect(parsed.logEntries).toHaveLength(2);
    expect(parsed.logEntries[0].data.content).toBe(message.part.text);
    expect(parsed.logEntries[1]).toEqual(extension);
    expect(parsed.mcpFailures).toEqual([]);
    expect(parsed.maxTurnsHit).toBe(false);
    expect(parseOpenCodeLog(JSON.stringify([message, extension])).logEntries).toEqual(parsed.logEntries);
    for (const log of ["", "hello", '{"type":"vendor.event"}', '{"type":"tool.call","message":"not a tool"}']) expect(parseOpenCodeLog(log).logEntries).toEqual([]);
  });

  it("preserves user prompts without publishing them in ordinary summaries", () => {
    const records = [
      { type: "message.updated", properties: { info: { id: "u", sessionID: "s", role: "user" } } },
      { type: "message.part.updated", properties: { part: { id: "p", sessionID: "s", messageID: "u", type: "text", text: "private user prompt" } } },
      { type: "vendor.private", data: { prompt: "private extension prompt" } },
    ];
    const parsed = parseOpenCodeLog(JSON.stringify(records));
    expect(parsed.logEntries.some(event => event.type === "user.message" && event.data.content === "private user prompt")).toBe(true);
    expect(parsed.markdown).not.toContain("private user prompt");
    expect(parsed.markdown).not.toContain("private extension prompt");
  });

  it("routes raw OpenCode logs through the focused adapter and persists compact essential canonical events", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-opencode-"));
    temporaryRoots.push(root);
    fs.writeFileSync(path.join(root, "aw_info.json"), JSON.stringify({ engine_id: "opencode" }));
    fs.writeFileSync(path.join(root, "agent-stdio.log"), synthetic);
    const events = writeUnifiedSession({ rootDir: root });
    const agent = events.filter(event => event.provenance.component === "agent");
    expect(agent.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["  before\r\n", "\t after\n"]);
    expect(agent.find(event => event.type === "tool.execution_complete").data).toMatchObject({ toolCallId: "call_synthetic", success: true, output: "ok\n", durationMs: 1, exitCode: 0 });
    expect(agent.at(-1).data.usage.input_tokens).toBe(12);
    expect(agent.every(event => event.provenance.path === "agent-stdio.log")).toBe(true);
    expect(events.at(-1).data.warnings).toBe(0);
    const output = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(output.endsWith("\n")).toBe(true);
    for (const line of output.trimEnd().split("\n")) expect(line).toBe(JSON.stringify(JSON.parse(line)));
    fs.writeFileSync(path.join(root, "agent-session.jsonl"), JSON.stringify({ type: "assistant.message", data: { content: "authoritative" } }) + "\n");
    expect(collectUnifiedSession({ rootDir: root }).events.filter(event => event.provenance.component === "agent")).toEqual([expect.objectContaining({ data: { content: "authoritative" } })]);
  });

  it("retains unknown-engine custom fallback rather than guessing OpenCode from arbitrary JSON", () => {
    const claude = '{"type":"assistant","message":{"content":[{"type":"text","text":"legacy custom"}]}}\n';
    expect(parseEngineSession(claude, "unregistered")).toMatchObject([{ type: "assistant.message", data: { content: "legacy custom" } }]);
    expect(parseEngineSession('{"type":"text","text":"not OpenCode"}', "opencode")).toEqual([]);
  });

  it("executes the shared declarative parser body through the same focused adapter", () => {
    const source = fs.readFileSync(path.join(__dirname, "../../../.github/workflows/shared/opencode.md"), "utf8");
    const body = source
      .split("    log-parser: |\n")[1]
      .split("\n---")[0]
      .replace(/^      /gm, "");
    const parseLog = new Function("require", body + "\nreturn parseLog;")(req);
    expect(parseLog(synthetic)).toEqual(parseOpenCodeLog(synthetic));
  });
});
