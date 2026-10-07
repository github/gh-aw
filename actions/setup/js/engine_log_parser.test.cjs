import { afterEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import vm from "node:vm";
import { loadEngineLogParser, normalizeEngineLogEntries, parseBehaviorLog } from "./engine_log_parser.cjs";
import { parseEngineSession } from "./unified_session.cjs";
import { parseCustomLog } from "./parse_custom_log.cjs";

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
    const parsed = parseBehaviorLog('Assistant: Captured answer\n{"malformed":', "cursor");
    expect(parsed.logEntries.some(event => event.type === "assistant.message" && event.data.content.includes("Captured answer"))).toBe(true);
    expect(warning).toHaveBeenCalledWith(expect.stringContaining("Malformed terminal JSON"));
  });

  it.each(["cursor", "crush"])("dispatches %s plaintext through its catalogued behavior and normalizes the result", engine => {
    const content = '[INFO] infrastructure\nAssistant: First answer\nSecond line\n{"type":"result","num_turns":1,"usage":{"output_tokens":0}}\n';
    expect(loadEngineLogParser(engine)).toBeTypeOf("function");
    const events = parseEngineSession(content, engine);
    expect(events.find(event => event.type === "session.init").data.sourceEngine).toBe(engine);
    expect(events.filter(event => event.type === "assistant.message")).toMatchObject([{ data: { content: "Assistant: First answer\nSecond line" } }]);
    expect(events.filter(event => event.type === "session.result")).toMatchObject([{ data: { numTurns: 1, usage: { output_tokens: 0 } } }]);
    expect(JSON.stringify(events.filter(event => event.type === "assistant.message"))).not.toContain("num_turns");
    expect(events.some(event => event.type.startsWith("tool."))).toBe(false);
    expect(parseCustomLog(content, engine).logEntries).toEqual(parseBehaviorLog(content, engine).logEntries);
  });

  it("preserves Crush assistant statements instead of accepting only a synthetic Claude result", () => {
    const text = "I'll execute the smoke tests efficiently.Smoke test completed. Overall status: **FAIL**.";
    const events = parseEngineSession(`[crush-harness] resolved executable\n${text}\n{"type":"result","num_turns":1,"usage":{"input_tokens":0,"output_tokens":0}}\n`, "crush");
    expect(events.filter(event => event.type === "assistant.message")).toMatchObject([{ data: { content: text } }]);
    expect(events.find(event => event.type === "session.init").data.sourceEngine).toBe("crush");
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
});
