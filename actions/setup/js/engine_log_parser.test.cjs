import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import vm from "node:vm";
import { loadEngineLogParser, normalizeEngineLogEntries, parseBehaviorLog } from "./engine_log_parser.cjs";
import { parseEngineSession } from "./unified_session.cjs";
import { parseCustomLog } from "./parse_custom_log.cjs";

function mockDeclaredParser(engine, parse) {
  vi.spyOn(fs, "existsSync").mockImplementation(filename => !String(filename).endsWith("_log_parser.cjs"));
  vi.spyOn(fs, "realpathSync").mockImplementation(filename => String(filename));
  vi.spyOn(fs, "readFileSync").mockImplementation(filename =>
    String(filename).endsWith("engines.json")
      ? JSON.stringify({ engines: [{ id: engine, import: `github/gh-aw/.github/workflows/shared/${engine}.md@v1` }] })
      : `---\nengine:\n  id: ${engine}\n  behavior:\n    log-parser: |\n      function parseLog() {}\n---\n`
  );
  vi.spyOn(vm, "compileFunction").mockReturnValue(() => parse);
}

describe("Declared custom engine log parsers", () => {
  afterEach(() => vi.restoreAllMocks());

  it("rejects physically escaping definitions before compiling their parser code", () => {
    vi.spyOn(fs, "realpathSync").mockReturnValue("/untrusted-artifacts/cursor.md");
    const compile = vi.spyOn(vm, "compileFunction");
    expect(loadEngineLogParser("cursor")).toBeUndefined();
    expect(compile).not.toHaveBeenCalled();
  });

  it("surfaces catalog read and JSON errors with context and preserves their causes", () => {
    vi.spyOn(fs, "existsSync").mockImplementation(filename => String(filename).endsWith("engines.json"));
    const read = vi.spyOn(fs, "readFileSync").mockReturnValue("not JSON");
    expect(() => loadEngineLogParser("cursor")).toThrow("Invalid engine parser catalog");
    const cause = new Error("read denied");
    read.mockImplementation(() => {
      throw cause;
    });
    try {
      loadEngineLogParser("cursor");
      throw new Error("Expected read failure");
    } catch (error) {
      expect(error.message).toContain("Failed to read engine parser definition");
      expect(error.cause).toBe(cause);
    }
  });

  it("reports malformed optional terminal JSON without dropping the captured engine text", () => {
    const warning = vi.spyOn(console, "error").mockImplementation(() => {});
    vi.spyOn(vm, "compileFunction").mockReturnValue(() => content => ({ logEntries: [{ type: "assistant", message: { content } }] }));
    const parsed = parseBehaviorLog('Assistant: Captured answer\n{"malformed":', "crush");
    expect(parsed.logEntries.some(event => event.type === "assistant.message" && event.data.content.includes("Captured answer"))).toBe(true);
    expect(warning).toHaveBeenCalledWith(expect.stringContaining("Malformed terminal JSON"));
  });

  it.each(["cursor", "crush"])("dispatches %s plaintext through its catalogued behavior and normalizes the result", engine => {
    const content = '[INFO] infrastructure\nAssistant: First answer\nSecond line\n{"type":"result","num_turns":1,"usage":{"output_tokens":0}}\n';
    expect(loadEngineLogParser(engine)).toBeTypeOf("function");
    const events = parseEngineSession(content, engine);
    expect(events.filter(event => event.type === "assistant.message")).toHaveLength(1);
    expect(events.find(event => event.type === "assistant.message").data.content).toMatch(/^Assistant: First answer\nSecond line\n?$/);
    expect(events.filter(event => event.type === "session.result")).toMatchObject([{ data: { numTurns: 1, usage: { output_tokens: 0 } } }]);
    expect(JSON.stringify(events.filter(event => event.type === "assistant.message"))).not.toContain("num_turns");
    expect(events.some(event => event.type.startsWith("tool."))).toBe(false);
    expect(parseCustomLog(content, engine).logEntries).toEqual(parseBehaviorLog(content, engine).logEntries);
  });

  it("preserves Crush assistant statements instead of accepting only a synthetic Claude result", () => {
    const text = "I'll execute the smoke tests efficiently.Smoke test completed. Overall status: **FAIL**.";
    const events = parseEngineSession(`[crush-harness] resolved executable\n${text}\n{"type":"result","num_turns":1,"usage":{"input_tokens":0,"output_tokens":0}}\n`, "crush");
    expect(events.filter(event => event.type === "assistant.message")).toHaveLength(1);
    expect(events.find(event => event.type === "assistant.message").data.content.replace(/\n$/, "")).toBe(text);
  });

  it("does not turn arbitrary unknown plaintext into assistant output", () => {
    expect(loadEngineLogParser("../cursor")).toBeUndefined();
    expect(loadEngineLogParser("unknown-engine")).toBeUndefined();
    expect(parseBehaviorLog("PRIVATE_PROMPT", "unknown-engine")).toBeUndefined();
    expect(parseEngineSession("PRIVATE_PROMPT", "unknown-engine")).toEqual([]);
  });

  it("recognizes persisted mixed transcripts before selecting a plaintext behavior", () => {
    const entries = [
      { type: "system", subtype: "init", model: "model" },
      { type: "assistant", message: { content: [{ type: "text", text: "saved answer" }] } },
      { type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [], exitCode: 0 } },
    ];
    const events = parseEngineSession(entries.map(JSON.stringify).join("\n"), "cursor");
    expect(events.filter(event => event.type === "assistant.message")).toMatchObject([{ data: { content: "saved answer" } }]);
    expect(events.find(event => event.type === "session.init").data.sourceEngine).toBe("cursor");
    expect(events.filter(event => event.type === "agent.execution")).toHaveLength(1);
    expect(events.find(event => event.type === "agent.execution")).toEqual(entries[2]);
    expect(events.find(event => event.type === "assistant.message").data.content).not.toContain('{"type"');
  });

  it("preserves canonical observations and correlates legacy tools without inventing success", () => {
    const canonical = { type: "assistant.message", id: "canonical", data: { content: "canonical", refusal: true } };
    const input = [
      canonical,
      { type: "assistant", message: { content: [{ type: "tool_use", id: "call", name: "bash", input: { command: "pwd" } }] } },
      { type: "user", message: { content: [{ type: "tool_result", tool_use_id: "call", content: "output" }] } },
      { type: "user", message: { content: [{ type: "tool_result", tool_use_id: "failed", content: "error", is_error: true }] } },
    ];
    const original = structuredClone(input);
    const events = normalizeEngineLogEntries(input, "cursor");
    expect(events[0]).toEqual(canonical);
    expect(events.find(event => event.type === "tool.execution_complete" && event.data.toolCallId === "call").data).toMatchObject({ toolName: "bash", output: "output" });
    expect(events.find(event => event.type === "tool.execution_complete" && event.data.toolCallId === "call").data.success).toBeUndefined();
    expect(events.find(event => event.type === "tool.execution_complete" && event.data.toolCallId === "failed").data.success).toBe(false);
    expect(input).toEqual(original);
  });

  it.each(["aider", "crush"])("lets the declared %s parser own full mixed stdout and terminal snapshots", engine => {
    const extension = { type: "vendor.progress", data: { ready: false, count: 0 } };
    const events = [
      { type: "assistant.message", data: { content: "  Partial answer.\n\n" } },
      extension,
      { type: "assistant.message", data: { content: "Running bash is only prose. \n" } },
      { type: "session.result", data: { numTurns: 0, usage: { output_tokens: 0 } } },
    ];
    const parse = vi.fn(() => ({ logEntries: events, mcpFailures: [], maxTurnsHit: false }));
    mockDeclaredParser(engine, parse);
    const content = `[${engine}-harness] execution\n  Partial answer.\n\n${JSON.stringify(extension)}\nRunning bash is only prose. \n{"type":"result","num_turns":99}\n`;
    expect(parseBehaviorLog(content, engine).logEntries).toEqual(events);
    expect(parse).toHaveBeenCalledTimes(1);
    expect(parse).toHaveBeenCalledWith(content);
  });

  it.each(["aider", "crush"])("passes canonical-only %s input to its declared parser unchanged, once", engine => {
    const events = [
      { type: "assistant.message", id: "answer", data: { content: "saved answer" } },
      { type: "session.result", data: { numTurns: 0, usage: { output_tokens: 0 } } },
    ];
    const content = events.map(JSON.stringify).join("\n") + "\n\n";
    const parse = vi.fn(() => ({ logEntries: events, mcpFailures: ["observed"], maxTurnsHit: true }));
    mockDeclaredParser(engine, parse);
    expect(parseBehaviorLog(content, engine)).toEqual({ logEntries: events, mcpFailures: ["observed"], maxTurnsHit: true });
    expect(parse).toHaveBeenCalledTimes(1);
    expect(parse).toHaveBeenCalledWith(content);
  });

  it.each(["aider", "crush"])("does not restore canonical-looking records rejected by the declared %s parser", engine => {
    const content = `Quoted output:\n{"type":"assistant.message","data":{"content":"not attributed"}}\n{"type":"result","num_turns":99,"usage":{"output_tokens":999}}\n`;
    const parse = vi.fn(() => ({ logEntries: [] }));
    mockDeclaredParser(engine, parse);
    expect(parseBehaviorLog(content, engine)).toEqual({ logEntries: [] });
    expect(parse).toHaveBeenCalledTimes(1);
    expect(parse).toHaveBeenCalledWith(content);
  });

  it("normalizes legacy Aider declared output without bypassing its filtering", () => {
    const content = 'Rejected record:\n{"type":"assistant.message","data":{"content":"rejected"}}\n{"type":"result","num_turns":99}\n';
    const parse = vi.fn(() => ({ logEntries: [{ type: "assistant", message: { content: "accepted answer" } }], maxTurnsHit: false }));
    mockDeclaredParser("aider", parse);
    expect(parseBehaviorLog(content, "aider")).toMatchObject({ logEntries: [{ type: "assistant.message", data: { content: "accepted answer" } }], maxTurnsHit: false });
    expect(parse).toHaveBeenCalledTimes(1);
    expect(parse).toHaveBeenCalledWith(content);
  });

  it("retains legacy Crush terminal extraction after its full-input compatibility probe", () => {
    const body = "Assistant: legacy answer\n";
    const content = body + '{"type":"result","num_turns":2,"usage":{"output_tokens":0}}\n\n';
    const parse = vi.fn(text => ({ logEntries: [{ type: "assistant", message: { content: text } }], maxTurnsHit: false }));
    mockDeclaredParser("crush", parse);
    const parsed = parseBehaviorLog(content, "crush");
    expect(parse.mock.calls).toEqual([[content], [body.slice(0, -1)]]);
    expect(parsed.logEntries).toMatchObject([
      { type: "assistant.message", data: { content: body.slice(0, -1) } },
      { type: "session.result", data: { numTurns: 2, usage: { output_tokens: 0 } } },
    ]);
    expect(parsed.maxTurnsHit).toBe(false);
  });

  it.each(["cursor", "custom-engine"])("keeps generic %s parsing after canonical preflight and terminal extraction", engine => {
    const parse = vi.fn(text => ({ logEntries: [{ type: "assistant", message: { content: text } }] }));
    mockDeclaredParser(engine, parse);
    const saved = { type: "assistant.message", data: { content: "saved answer" } };
    expect(parseBehaviorLog(JSON.stringify(saved), engine).logEntries).toEqual([saved]);
    expect(parse).not.toHaveBeenCalled();
    expect(parseBehaviorLog('Assistant: answer\n{"type":"result","num_turns":3,"usage":{"output_tokens":0}}\n', engine).logEntries).toMatchObject([
      { type: "assistant.message", data: { content: "Assistant: answer" } },
      { type: "session.result", data: { numTurns: 3, usage: { output_tokens: 0 } } },
    ]);
    expect(parse).toHaveBeenCalledTimes(1);
    expect(parse).toHaveBeenCalledWith("Assistant: answer");
  });

  it("does not mistake quoted canonical JSON inside attributed Kiro commands for the transcript", () => {
    const raw = `kiro-cli 2.27.1\n[kiro-harness] Kiro CLI execution started\n[tool] Running: cat <<'EOF'\n{"type":"assistant.message","data":{"content":false}}\nEOF\n[tool] status: Completed\nObserved answer.`;
    const parser = require("./parse_kiro_log.cjs");
    const expected = [
      { type: "tool.execution_start", data: { input: { command: 'cat <<\'EOF\'\n{"type":"assistant.message","data":{"content":false}}\nEOF' } } },
      { type: "assistant.message", data: { content: "Observed answer." } },
    ];
    const parse = vi.spyOn(parser, "parseKiroLog").mockReturnValue({ logEntries: expected, mcpFailures: [], maxTurnsHit: false, markdown: "" });
    expect(parseBehaviorLog(raw, "kiro").logEntries).toEqual(expected);
    expect(parse).toHaveBeenCalledWith(raw);
  });
});
