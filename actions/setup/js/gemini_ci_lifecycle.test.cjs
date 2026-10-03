import { describe, it, expect } from "vitest";
import { parseGeminiLog } from "./parse_gemini_log.cjs";
import { normalizeGeminiSession } from "./gemini_session.cjs";
import { selectSessionResult } from "./agent_session.cjs";
const source = require("./fixtures/gemini_ci_lifecycle.cjs");

describe("Gemini existing-CI successful lifecycle", () => {
  it("retains native tool identities, empty success output, and a failed tool before successful terminal accounting", () => {
    const result = parseGeminiLog(source.map(record => JSON.stringify(record)).join("\n"));
    const events = result.logEntries;
    expect(events.map(event => event.type)).toEqual([
      "session.init",
      "user.message",
      "tool.execution_start",
      "tool.execution_complete",
      "assistant.message",
      "tool.execution_start",
      "tool.execution_complete",
      "assistant.message",
      "session.result",
    ]);
    for (const [index, event] of events.entries()) expect(event.timestamp).toBe(source[index].timestamp);
    expect(events[2].data).toMatchObject({ toolCallId: "read_file__call_621569", toolName: "read_file", input: source[2].parameters });
    expect(events[3].data).toMatchObject({ toolCallId: "read_file__call_621569", toolName: "read_file", output: "", success: true });
    expect(events[4].data.content).toBe(source[4].content);
    expect(events[6].data).toMatchObject({ toolCallId: "list_directory__call_347288", toolName: "list_directory", output: source[6].output, error: source[6].error, success: false });
    expect(events[7].data.content).toBe(source[7].content);
    expect(events[8].data).toMatchObject({
      status: "success",
      durationMs: 383869,
      usage: { total_tokens: 6413770, input_tokens: 6376298, output_tokens: 6710, cache_read_input_tokens: 4989184, input_tokens_include_cache: true },
      stats: source[8].stats,
    });
    expect(selectSessionResult(events).errors).toBeUndefined();
    expect(selectSessionResult(events).numTurns).toBeUndefined();
    expect(selectSessionResult(events).totalCostUsd).toBeUndefined();
    expect(result.markdown).not.toContain("SANITIZED_CI_LIFECYCLE_PROMPT");
    expect(normalizeGeminiSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });
});
