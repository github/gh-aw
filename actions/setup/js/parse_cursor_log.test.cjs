import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { parseCursorLog } from "./parse_cursor_log.cjs";
import { loadEngineLogParser, parseBehaviorLog } from "./engine_log_parser.cjs";
import { parseEngineSession, collectUnifiedSession } from "./unified_session.cjs";
import { parseCustomLog } from "./parse_custom_log.cjs";
import { normalizeAgentSession, selectSessionResult } from "./agent_session.cjs";
import { runLogParser } from "./log_parser_bootstrap.cjs";
import { writeSessionArtifact } from "./session_artifact.cjs";
import textRuns from "./test_data/cursor_text_runs.json";

const jsonl = records => records.map(record => JSON.stringify(record)).join("\n") + "\n";
const assistant = (text, extra = {}) => ({ type: "assistant", session_id: "session", message: { role: "assistant", content: [{ type: "text", text }] }, ...extra });
const tool = (subtype, call_id, body, extra = {}) => ({ type: "tool_call", subtype, call_id, session_id: "session", tool_call: body, ...extra });
const roots = [];
const temp = () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "cursor-session-test-"));
  roots.push(root);
  return root;
};

afterEach(() => {
  delete global.core;
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
  for (const root of roots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
});

describe("Cursor historical Actions text evidence (sanitized excerpts)", () => {
  it.each(textRuns)("preserves the final answer from $runUrl without inventing tools or initialization", fixture => {
    const content = `[INFO] Executing agent command...\n[entrypoint] Chroot mode enabled\n\n[entrypoint] Unsetting sensitive tokens...\n2026.07.20-8cc9c0b\n${fixture.answer}[INFO] Stopping containers...\n Container awf-agent  Stopped\nProcess exiting with code: ${fixture.processExitCode}\n{"type":"result","num_turns":1}\n`;
    const events = parseCursorLog(content).logEntries;
    expect(events.filter(event => event.type === "assistant.message")).toEqual([{ type: "assistant.message", data: { content: fixture.answer } }]);
    expect(events.some(event => event.type === "session.init" || event.type.startsWith("tool."))).toBe(false);
    expect(selectSessionResult(events)).toMatchObject({ numTurns: 1 });
    expect(selectSessionResult(events).usage).toBeUndefined();
    // The failed workflow failed telemetry checking, not Cursor process execution.
    expect(selectSessionResult(events).status).toBeUndefined();
    expect(parseCursorLog(fixture.answer).logEntries).toEqual([{ type: "assistant.message", data: { content: fixture.answer } }]);
  });

  it("does not interpret ordinary tool/error/limit prose as observed execution", () => {
    const content = "\nRunning tests\nTool readToolCall\nMCP server example failed earlier; max turns are configurable.\n\n  preserved tail \n";
    expect(parseCursorLog(content)).toMatchObject({ logEntries: [{ type: "assistant.message", data: { content } }], mcpFailures: [], maxTurnsHit: false });
  });

  it("preserves plaintext around opaque canonical extensions and execution diagnostics", () => {
    const extension = { type: "vendor.progress", id: "native", data: { ready: false, count: 0 } };
    const execution = { type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [], exitCode: 0 } };
    expect(parseCursorLog(` first\n\n${JSON.stringify(extension)}\n second \n${JSON.stringify(execution)}\n`).logEntries).toEqual([
      { type: "assistant.message", data: { content: " first\n\n" } },
      extension,
      { type: "assistant.message", data: { content: " second \n" } },
      execution,
    ]);
  });

  it("retains JSON-shaped final-answer text in observed older text-format framing", () => {
    const answer = '  {"items":[],"ready":false,"count":0}\n42\nnull\n\n';
    expect(parseCursorLog(`[entrypoint] execution\n2026.07.20-8cc9c0b\n${answer}[INFO] Stopping containers...\n`).logEntries).toEqual([{ type: "assistant.message", data: { content: answer } }]);
  });

  it("returns empty observations for empty, infrastructure-only and unsupported JSON input", () => {
    for (const content of ["", "[INFO] started\n[entrypoint] setup\n2026.07.20-8cc9c0b\n", '{"unrecognized":true}\n', "null\n", "[]\n"]) {
      expect(parseCursorLog(content).logEntries).toEqual([]);
    }
  });
});

describe("Cursor documented stream-json (synthetic, not Actions-native evidence)", () => {
  it("maps messages, reasoning, tools and terminal metadata in source order without duplicate aggregate text", () => {
    const input = [
      { type: "system", subtype: "init", id: "init", session_id: "session", cwd: "/workspace", model: "Cursor model", apiKeySource: "env", permissionMode: "default" },
      { type: "user", id: "user", parentId: null, timestamp: 0, session_id: "session", message: { role: "user", content: [{ type: "text", text: " prompt\n " }] } },
      assistant(" first\n ", { id: "answer", parentId: "user", metadata: false }),
      { type: "assistant", id: "thought", message: { content: [{ type: "thinking", thinking: " private reasoning\n " }] }, session_id: "session" },
      tool("started", "native-call", { readToolCall: { args: false } }, { id: "start", parentId: "answer", timestamp_ms: 0 }),
      tool("completed", "native-call", { readToolCall: { args: false, result: { success: { content: "", isEmpty: false, totalLines: 0 } } } }, { id: "complete", duration_ms: 0 }),
      assistant(" done\n "),
      {
        type: "result",
        subtype: "success",
        id: "terminal",
        session_id: "session",
        request_id: "request",
        duration_ms: 0,
        duration_api_ms: 0,
        is_error: false,
        result: " first\n  done\n ",
        usage: { input_tokens: 0, output_tokens: 3, reasoning_output_tokens: 0, input_tokens_include_cache: false, nativeCounter: 0 },
        total_cost_usd: 0,
      },
    ];
    const events = parseCursorLog(jsonl(input)).logEntries;
    expect(events.map(event => event.type)).toEqual(["session.init", "user.message", "assistant.message", "assistant.reasoning", "tool.execution_start", "tool.execution_complete", "assistant.message", "session.result"]);
    expect(events[0]).toMatchObject({ id: "init", apiKeySource: "env", data: { sourceEngine: "cursor", sessionId: "session", cwd: "/workspace", model: "Cursor model" } });
    expect(events[1]).toMatchObject({ id: "user", parentId: null, timestamp: 0, data: { content: " prompt\n " } });
    expect(events[2]).toMatchObject({ id: "answer", parentId: "user", metadata: false, data: { content: " first\n " } });
    expect(events[3].data.content).toBe(" private reasoning\n ");
    expect(events[4]).toMatchObject({ id: "start", parentId: "answer", timestamp: 0, timestamp_ms: 0, data: { toolCallId: "native-call", toolName: "readToolCall", input: false } });
    expect(events[5]).toMatchObject({ id: "complete", data: { toolCallId: "native-call", toolName: "readToolCall", durationMs: 0, success: true, output: input[5].tool_call.readToolCall.result } });
    expect(events.at(-1)).toMatchObject({ id: "terminal", request_id: "request", duration_api_ms: 0, data: { status: "completed", durationMs: 0, totalCostUsd: 0, usage: input.at(-1).usage } });
    expect(events.at(-1).data.numTurns).toBeUndefined();
    expect(normalizeAgentSession(events)).toEqual(events);
    expect(JSON.parse(JSON.stringify(events))).toEqual(JSON.parse(JSON.stringify(parseCursorLog(JSON.stringify(input, null, 2)).logEntries)));
  });

  it("preserves deltas, duplicate flush metadata and interrupted partial text", () => {
    const records = [
      assistant(" A", { id: "d1", timestamp_ms: 0 }),
      assistant("\n ", { id: "d2", timestamp_ms: 1 }),
      assistant(" A\n ", { id: "snapshot", timestamp_ms: 2, model_call_id: "model-call" }),
      assistant(" A\n ", { id: "final-flush" }),
      tool("started", "pending", { shellToolCall: { args: { command: " pwd\n " } } }),
      assistant(" unfinished ", { id: "d3", timestamp_ms: 3 }),
    ];
    const events = parseCursorLog(jsonl(records)).logEntries;
    expect(events.map(event => event.type)).toEqual(["assistant.message", "assistant.message", "tool.execution_start", "assistant.message"]);
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual([" A", "\n ", " unfinished "]);
    expect(events[1].data.nativeSnapshots).toEqual([records[2], records[3]]);
    expect(events.at(-1)).toMatchObject({ id: "d3", data: { partial: true } });
    expect(events.some(event => event.type === "session.result" || event.type === "tool.execution_complete")).toBe(false);
  });

  it("retains only the unobserved snapshot suffix after a partial message", () => {
    const snapshot = assistant(" observed suffix\n ", { timestamp_ms: 1, model_call_id: "model", id: "snapshot" });
    const finalFlush = assistant(" observed suffix\n ", { id: "final-flush" });
    const events = parseCursorLog(jsonl([assistant(" observed", { timestamp_ms: 0, id: "delta" }), snapshot, finalFlush])).logEntries;
    expect(events.map(event => event.data.content)).toEqual([" observed", " suffix\n "]);
    expect(events[1]).toMatchObject({ id: "snapshot", data: { nativeSnapshots: [snapshot, finalFlush] } });
  });

  it.each([false, 0, "", null, [], {}])("preserves structured/falsy function input and completion output %j", payload => {
    const events = parseCursorLog(
      jsonl([tool("started", "function-id", { function: { name: "native_tool", arguments: JSON.stringify(payload) } }), tool("completed", "function-id", { function: { name: "native_tool", result: payload } })])
    ).logEntries;
    expect(events[0].data).toMatchObject({ toolCallId: "function-id", toolName: "native_tool", input: payload });
    expect(events[1].data).toMatchObject({ toolCallId: "function-id", output: payload });
    expect(events[1].data.success).toBeUndefined();
  });

  it("retains orphan completions, exact command arguments, conflicting failures and dangling calls", () => {
    const records = [
      tool("completed", "orphan", { shellToolCall: { args: { command: "printf ' hi\\n' " }, result: { success: { exitCode: 7 }, error: { message: " failed\n " }, isError: true } } }, { success: true, id: "native-orphan", duration_ms: 0 }),
      tool("started", "dangling", { function: { name: "native_tool", arguments: "{broken " } }),
      tool("completed", "different-id", { function: { name: "native_tool", result: 0 } }),
    ];
    const events = parseCursorLog(jsonl(records)).logEntries;
    expect(events.map(event => event.type)).toEqual(["tool.execution_complete", "tool.execution_start", "tool.execution_complete"]);
    expect(events[0]).toMatchObject({ id: "native-orphan", success: true, data: { success: false, exitCode: 7, durationMs: 0, error: { message: " failed\n " }, command: "printf ' hi\\n' " } });
    expect(events[1].data.input).toBe("{broken ");
    expect(events[2].data.toolCallId).toBe("different-id");
  });

  it("retains canonical unknown extensions and adjacent records after malformed JSON", () => {
    const extension = { type: "vendor.progress", id: "native", parentId: null, timestamp: 0, version: "native-version", data: { falseValue: false, zero: 0, empty: "", payload: [null, {}] } };
    const content = `${JSON.stringify(assistant(" before "))}\n{broken\nnull\n${JSON.stringify(extension)}\n${JSON.stringify(tool("started", "pending", { readToolCall: { args: {} } }))}\n{"unrecognized":true}\n`;
    const events = parseCursorLog(content).logEntries;
    expect(events.map(event => event.type)).toEqual(["assistant.message", "vendor.progress", "tool.execution_start"]);
    expect(events[1]).toEqual(extension);
    expect(parseBehaviorLog(jsonl([extension]), "cursor").logEntries).toEqual([extension]);
  });

  it("maps structured refusals and diagnostics without successful assistant messages", () => {
    const records = [
      assistant("", { message: { role: "assistant", refusal: " cannot comply\n ", content: [] } }),
      { type: "error", id: "err", session_id: "session", error: { code: 0, message: " authentication failed\n " }, retry: false },
      { type: "result", subtype: "error", session_id: "session", is_error: true, result: "not a successful answer", errors: [{ code: 0, message: "failure" }], permission_denials: [], num_turns: 0 },
    ];
    const events = parseCursorLog(jsonl(records)).logEntries;
    expect(events.map(event => event.type)).toEqual(["assistant.refusal", "session.result", "session.result"]);
    expect(events[0].data).toMatchObject({ reason: "refusal", content: " cannot comply\n " });
    expect(events[1]).toMatchObject({ id: "err", retry: false, data: { status: "error", errors: [records[1].error] } });
    expect(events[2].data).toMatchObject({ status: "error", errors: records[2].errors, permissionDenials: [], numTurns: 0 });
  });

  it("keeps stderr errors in order without requiring a terminal result", () => {
    const events = parseCursorLog(`${JSON.stringify(assistant(" partial "))}\nError: authentication failed\n${JSON.stringify(tool("started", "pending", { readToolCall: { args: {} } }))}\n`).logEntries;
    expect(events.map(event => event.type)).toEqual(["assistant.message", "session.result", "tool.execution_start"]);
    expect(events[1].data.errors).toEqual(["Error: authentication failed"]);
    expect(parseCursorLog("[cursor-harness] Cursor Agent execution failed with exit code 1\n").logEntries).toMatchObject([{ type: "session.result", data: { status: "error" } }]);
  });

  it("does not count terminal usage snapshots twice or infer turns from messages", () => {
    const records = [
      assistant(" answer "),
      { type: "result", subtype: "success", session_id: "session", result: " answer ", usage: { input_tokens: 4, output_tokens: 0 } },
      { type: "result", subtype: "success", session_id: "session", usage: { input_tokens: 4, output_tokens: 0 } },
    ];
    const events = parseCursorLog(jsonl(records)).logEntries;
    expect(selectSessionResult(events).usage).toEqual({ input_tokens: 4, output_tokens: 0 });
    expect(selectSessionResult(events).numTurns).toBeUndefined();
    expect(events.filter(event => event.type === "assistant.message")).toHaveLength(1);
  });

  it("uses result-only text exactly once and preserves source metadata", () => {
    const record = { type: "result", subtype: "success", id: "terminal", session_id: "session", is_error: false, result: " answer\n " };
    expect(parseCursorLog(jsonl([record])).logEntries).toMatchObject([
      { type: "assistant.message", id: "terminal", data: { content: record.result } },
      { type: "session.result", id: "terminal", data: { status: "completed" } },
    ]);
  });
});

describe("Cursor behavior, Actions bootstrap and unified artifact integration", () => {
  it("uses the same mapping in generated behavior, custom routing and reconstruction", () => {
    const content = jsonl([
      assistant(" answer\n "),
      tool("started", "call", { readToolCall: { args: false } }),
      tool("completed", "call", { readToolCall: { result: { success: { content: "" } } } }),
      { type: "result", subtype: "success", session_id: "session", result: " answer\n ", duration_ms: 0 },
    ]);
    const events = parseCursorLog(content).logEntries;
    expect(loadEngineLogParser("cursor")(content).logEntries).toEqual(events);
    expect(parseBehaviorLog(content, "cursor").logEntries).toEqual(events);
    expect(parseCustomLog(content, "cursor").logEntries).toEqual(events);
    expect(parseEngineSession(content, "cursor")).toEqual(events);
    const root = temp();
    writeSessionArtifact(path.join(root, "agent-session.jsonl"), events);
    fs.writeFileSync(path.join(root, "agent-stdio.log"), "not authoritative");
    const result = collectUnifiedSession({ rootDir: root, engine: "cursor", warn: () => {} });
    const agent = result.events.filter(event => event.provenance?.component === "agent");
    expect(agent.map(event => event.type)).toEqual(events.map(event => event.type));
    expect(agent[0].data.content).toBe(" answer\n ");
    expect(agent[1].data.input).toBe(false);
    expect(agent[2].data.output).toEqual({ success: { content: "" } });
    expect(agent.every(event => event.provenance.path === "agent-session.jsonl")).toBe(true);
  });

  it("persists canonical native observations from Actions bootstrap without invented results", async () => {
    const root = temp();
    const content = jsonl([assistant(" answer\n "), tool("started", "pending", { readToolCall: { args: {} } })]);
    fs.writeFileSync(path.join(root, "agent-stdio.log"), content);
    vi.stubEnv("GH_AW_AGENT_OUTPUT", path.join(root, "agent-stdio.log"));
    global.core = { info: vi.fn(), debug: vi.fn(), warning: vi.fn(), error: vi.fn(), setFailed: vi.fn(), setOutput: vi.fn(), summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue() } };
    await runLogParser({ rootDir: root, parserName: "Cursor", parseLog: parseCursorLog });
    const events = fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8").trimEnd().split("\n").map(JSON.parse);
    expect(events.filter(event => event.type === "assistant.message")[0].data.content).toBe(" answer\n ");
    expect(events.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(events.some(event => event.type === "tool.execution_complete" || event.type === "result" || event.type === "session.result")).toBe(false);
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });
});
