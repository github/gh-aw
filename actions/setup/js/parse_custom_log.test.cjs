// @ts-check

import { describe, it, expect } from "vitest";
import { parseCustomLog } from "./parse_custom_log.cjs";

describe("parseCustomLog", () => {
  it("should detect and parse Claude format logs", () => {
    const claudeLog = JSON.stringify([
      {
        type: "system",
        subtype: "init",
        tools: [],
      },
      {
        type: "user",
        message: { role: "user", content: "Hello" },
      },
    ]);

    const result = parseCustomLog(claudeLog);

    expect(result).toBeDefined();
    expect(result.markdown).toContain("Custom Engine Log");
    expect(result.logEntries.length).toBeGreaterThan(0);
  });

  it("should detect and parse Codex format logs", () => {
    const codexLog = `{"type":"thread.started","thread_id":"fixture-thread"}
{"type":"item.completed","item":{"id":"item-1","type":"agent_message","text":"Hello"}}`;

    const result = parseCustomLog(codexLog);

    expect(result).toBeDefined();
    expect(result.markdown).toContain("Codex format");
    expect(result.logEntries.find(e => e.type === "assistant.message").data.content).toBe("Hello");
  });

  it("uses Codex's supported legacy-compatible detector after malformed neighbors", () => {
    const log = 'noise\n{"type":"assistant",\n{"type":"reasoning","data":{"content":"recovered"}}';
    const result = parseCustomLog(log);
    expect(result.markdown).toContain("Codex format");
    expect(result.logEntries).toMatchObject([{ type: "assistant.reasoning", data: { content: "recovered" } }]);
  });

  it("preserves Claude priority for supported signatures shared with Codex", () => {
    expect(parseCustomLog('{"type":"result","usage":{"output_tokens":0}}').markdown).toContain("Claude format");
  });

  it("should handle unrecognized log format with basic fallback", () => {
    const unknownLog = "Some plain text log\nwith multiple lines\nand no structure";

    const result = parseCustomLog(unknownLog);

    expect(result).toBeDefined();
    expect(result.markdown).toContain("Custom Engine Log");
    expect(result.markdown).toContain("not recognized");
    expect(result.mcpFailures).toBeDefined();
    expect(result.logEntries).toEqual([]);
  });

  it("should not publish unrecognized raw logs in fallback mode", () => {
    const longLog = "a".repeat(2000);

    const result = parseCustomLog(longLog);

    expect(result).toBeDefined();
    expect(result.markdown).toContain("Custom Engine Log");
    expect(result.markdown).not.toContain("a".repeat(20));
    expect(result.logEntries).toEqual([]);
  });

  it("should handle empty log content", () => {
    const result = parseCustomLog("");

    expect(result).toBeDefined();
    expect(result.markdown).toContain("Custom Engine Log");
    expect(result.logEntries).toEqual([]);
  });

  it.each(['{"type":"thread.unknown"}', '{"type":"assistant","message":17}', '{"unrelated":true}', '{"type":"vendor.progress","data":"not-an-event-payload"}'])("rejects syntactically valid unsupported input %s", input => {
    expect(parseCustomLog(input).logEntries).toEqual([]);
  });
});
