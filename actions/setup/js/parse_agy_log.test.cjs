import { describe, expect, it } from "vitest";
import { parseAgyLog } from "./parse_agy_log.cjs";
import { parseEngineSession } from "./unified_session.cjs";
import { parseCustomLog } from "./parse_custom_log.cjs";

const jsonl = events => events.map(event => JSON.stringify(event)).join("\n");
const init = { event: "init", conversation_id: "conversation-1", init: { model: "gemini-3.8-flash-medium" } };
const step = (step_index, data) => ({ event: "step_update", step_update: { step_index, ...data } });
const result = data => ({ event: "result", result: { status: "SUCCESS", ...data } });
const usage = { input_tokens: 100, output_tokens: 20, thinking_tokens: 8, cache_read_tokens: 30, total_tokens: 120 };
const fixture = [
  init,
  step(0, { step_type: "agent_response", state: "ACTIVE", text_delta: "Conformance " }),
  step(0, { step_type: "agent_response", state: "DONE", text_delta: "passed." }),
  step(1, { step_type: "tool", state: "ACTIVE", tool_info: { name: "run_command", parameters: { command: "node probe.cjs" } } }),
  step(1, { step_type: "tool", state: "DONE", tool_info: { name: "run_command", output: "receipt" } }),
  result({ num_turns: 1, usage }),
  result({ num_turns: 2, usage: { ...usage, input_tokens: 110, output_tokens: 25, total_tokens: 135 } }),
];

describe("native Agy session parser", () => {
  it("merges deltas, pairs tools and retains only the latest cumulative usage", () => {
    const parsed = parseAgyLog(jsonl([...fixture, fixture.at(-1)]));
    expect(parsed.logEntries.find(event => event.type === "session.init").data).toEqual({ sourceEngine: "agy", model: init.init.model });
    expect(parsed.logEntries.filter(event => event.type === "assistant.message")).toHaveLength(1);
    expect(parsed.logEntries.find(event => event.type === "assistant.message").data.content).toBe("Conformance passed.");
    const start = parsed.logEntries.find(event => event.type === "tool.execution_start");
    const completion = parsed.logEntries.find(event => event.type === "tool.execution_complete");
    expect(start.data).toMatchObject({ toolCallId: "conversation-1:1", toolName: "run_command", input: { command: "node probe.cjs" } });
    expect(completion.data).toMatchObject({ toolCallId: start.data.toolCallId, success: true, output: "receipt" });
    expect(parsed.logEntries.filter(event => event.type === "session.result")).toHaveLength(1);
    expect(parsed.logEntries.at(-1).data).toMatchObject({ status: "completed", numTurns: 2, usage: { input_tokens: 110, output_tokens: 25, total_tokens: 135, reasoning_output_tokens: 8, cache_read_input_tokens: 30 } });
    expect(parsed.markdown).toContain("Conformance passed.");
    expect(parseEngineSession(jsonl(fixture), "agy")).toEqual(parseAgyLog(jsonl(fixture)).logEntries);
    expect(parseCustomLog(jsonl(fixture)).logEntries).toEqual(parseAgyLog(jsonl(fixture)).logEntries);
    expect(parseAgyLog(jsonl(parsed.logEntries)).logEntries).toEqual(parsed.logEntries);
  });

  it.each(["ERROR", "CANCELED", "INTERRUPTED", "INVALID", "WAITING", "RUNNING"])("does not report %s as successful completion", status => {
    const parsed = parseAgyLog(jsonl([init, result({ status, error: "native diagnostic" })]));
    expect(parsed.logEntries.at(-1).data.status).not.toBe("completed");
    expect(parsed.logEntries.some(event => event.type === "session.error")).toBe(true);
    expect(parsed.logEntries.at(-1).data).not.toHaveProperty("usage");
  });

  it("preserves a bare startup failure and a failed tool", () => {
    const parsed = parseAgyLog(
      jsonl([init, step(1, { step_type: "tool", state: "DONE", tool_info: { name: "mcp__challenge", error: { type: "permission_denied", message: "Denied" } } }), { status: "ERROR", error: "Maximum turns reached" }])
    );
    expect(parsed.logEntries.find(event => event.type === "tool.execution_complete").data.success).toBe(false);
    expect(parsed.logEntries.at(-1).data).toMatchObject({ status: "error", errors: ["Maximum turns reached"] });
    expect(parsed.maxTurnsHit).toBe(true);
  });

  it("keeps partial logs partial, missing metrics absent and malformed raw text private", () => {
    const parsed = parseAgyLog(`{PRIVATE_SECRET\n${jsonl([init, fixture[1]])}`);
    expect(parsed.logEntries.some(event => event.type === "session.result")).toBe(false);
    expect(parsed.logEntries.find(event => event.type === "session.collection_warning").data).toEqual({ code: "malformed_jsonl", line: 1 });
    expect(parsed.markdown).not.toContain("PRIVATE_SECRET");
    expect(parseAgyLog("PRIVATE_PROMPT").markdown).not.toContain("PRIVATE_PROMPT");
    const metrics = parseAgyLog(jsonl([result({ usage: { input_tokens: 0, output_tokens: -1, cache_read_tokens: "30" } })]));
    expect(metrics.logEntries.at(-1).data.usage).toEqual({ input_tokens: 0, input_tokens_include_cache: true });
    expect(metrics.logEntries.at(-1).data).not.toHaveProperty("numTurns");
  });

  it("preserves cumulative inference when the harness corrects a nominal success", () => {
    const parsed = parseAgyLog(jsonl([init, result({ num_turns: 2, usage }), result({ status: "ERROR", num_turns: 2, usage, error: "Agy run was denied, interrupted, or lacked completed inference" })]));
    const terminal = parsed.logEntries.filter(event => event.type === "session.result");
    expect(terminal).toHaveLength(1);
    expect(terminal[0].data).toMatchObject({ status: "error", numTurns: 2, usage: { input_tokens: 100, output_tokens: 20, reasoning_output_tokens: 8 } });
    expect(parsed.logEntries.some(event => event.type === "session.error")).toBe(true);
  });
});
