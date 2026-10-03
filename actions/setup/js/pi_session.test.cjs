import { describe, it, expect } from "vitest";
import { parsePiLog } from "./parse_pi_log.cjs";
import { transformPiV3Entries, computePiV3Stats } from "./pi_session.cjs";
import { success, failure } from "./fixtures/pi_ci_stream.cjs";
import { normalizeAgentSession, selectSessionResult, sessionTokenTotal } from "./agent_session.cjs";

const parse = records => parsePiLog(records.map(record => JSON.stringify(record)).join("\n"));
const byType = (events, type) => events.filter(event => event.type === type);

describe("Pi CI stream regressions", () => {
  // https://github.com/github/gh-aw/actions/runs/36884805242 (success)
  // https://github.com/github/gh-aw/actions/runs/36447274044 (provider failure)
  // Both agent artifacts expose pi-streaming.jsonl; fixture strings are replacements.
  it("normalizes real lifecycle shapes once, without flattening results or exposing prompts", () => {
    const original = structuredClone(success);
    const { logEntries, markdown } = parse(success);
    expect(byType(logEntries, "tool.execution_start")).toHaveLength(1);
    expect(byType(logEntries, "tool.execution_complete")).toHaveLength(1);
    expect(byType(logEntries, "tool.execution_start")[0].data).toMatchObject({ toolCallId: "sanitized-call", toolName: "bash", input: { command: "printf sanitized" } });
    const completion = byType(logEntries, "tool.execution_complete")[0];
    expect(completion.data.output).toEqual(success[15].result);
    expect(completion.data.result).toEqual(success[15].result);
    expect(completion.data.durationMs).toBeUndefined();
    expect(completion.data.success).toBe(true);
    expect(
      byType(logEntries, "assistant.message")
        .map(e => e.data.content)
        .join("")
    ).toBe("Sanitized complete.\n");
    expect(byType(logEntries, "user.message").map(e => e.data.content)).toEqual(["SANITIZED PRIVATE PROMPT"]);
    expect(markdown).not.toContain("SANITIZED PRIVATE PROMPT");
    expect(markdown).not.toContain("SANITIZED SYSTEM PROMPT");
    expect(byType(logEntries, "pi.tool_execution_update")[0].data.partialResult).toEqual(success[14].partialResult);
    expect(selectSessionResult(logEntries)).toMatchObject({ numTurns: 2, totalCostUsd: 0, usage: { input_tokens: 6427, output_tokens: 145, cache_read_input_tokens: 1540, cache_creation_input_tokens: 1 } });
    expect(selectSessionResult(logEntries).usage.input_tokens_include_cache).toBe(false);
    expect(sessionTokenTotal(selectSessionResult(logEntries).usage)).toBe(8113);
    expect(logEntries.some(e => e.type === "result")).toBe(false);
    expect(normalizeAgentSession(logEntries)).toEqual(logEntries);
    expect(success).toEqual(original);
  });

  it("counts the failed finalized assistant once and preserves reported zero cost and tokens", () => {
    const { logEntries, markdown } = parse(failure);
    expect(selectSessionResult(logEntries)).toMatchObject({
      numTurns: 1,
      totalCostUsd: 0,
      usage: { input_tokens: 0, output_tokens: 0, cache_read_input_tokens: 0, cache_creation_input_tokens: 0 },
      errors: [expect.stringContaining("model_not_supported")],
    });
    expect(markdown).toContain("model_not_supported");
    expect(byType(logEntries, "assistant.message")).toEqual([]);
    expect(byType(logEntries, "tool.execution_complete")).toEqual([]);
  });

  it("keeps deltas before interleaved tools and emits only a missing snapshot suffix", () => {
    const message = { role: "assistant", timestamp: 10, content: [{ type: "text", text: " before after!\n" }], usage: { input: 4, output: 2 } };
    const events = transformPiV3Entries([
      { type: "turn_start" },
      { type: "message_update", id: "delta-1", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: " before" } },
      { type: "tool_execution_start", toolCallId: "t", toolName: "bash", args: false },
      { type: "tool_execution_end", toolCallId: "t", result: false, isError: false, durationMs: 0 },
      { type: "message_update", id: "delta-2", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: " after" } },
      { type: "message_end", message },
      { type: "turn_end", message },
      { type: "agent_end", messages: [message] },
    ]);
    expect(events.filter(e => !e.type.startsWith("pi.")).map(e => e.type)).toEqual(["assistant.message", "tool.execution_start", "tool.execution_complete", "assistant.message", "assistant.message"]);
    expect(byType(events, "assistant.message").map(e => e.data.content)).toEqual([" before", " after", "!\n"]);
    expect(events[0].id).toBe("delta-1");
    expect(events[1].data.input).toBe(false);
    expect(events[2].data).toMatchObject({ output: false, durationMs: 0, success: true });
  });

  it("retains interrupted text/thinking, partial arguments, dangling starts and orphan completions", () => {
    const result = parse([
      { type: "message_update", assistantMessageEvent: { type: "thinking_delta", contentIndex: 0, delta: " \n" } },
      { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 1, delta: "" } },
      { type: "message_update", assistantMessageEvent: { type: "toolcall_start", contentIndex: 2, id: "dangling", toolName: "bash" } },
      { type: "message_update", assistantMessageEvent: { type: "toolcall_delta", contentIndex: 2, delta: '{"command":' } },
      { type: "tool_execution_end", toolCallId: "orphan", toolName: "bash", result: { content: [], details: { exitCode: 2 } }, isError: false },
      { type: "error", error: { code: "interrupted" } },
    ]);
    expect(byType(result.logEntries, "assistant.reasoning")[0].data.content).toBe(" \n");
    expect(byType(result.logEntries, "assistant.message")[0].data.content).toBe("");
    expect(byType(result.logEntries, "tool.execution_start")[0].data).toMatchObject({ toolCallId: "dangling", toolName: "bash" });
    expect(byType(result.logEntries, "tool.execution_complete")).toHaveLength(1);
    expect(byType(result.logEntries, "tool.execution_complete")[0].data).toMatchObject({ toolCallId: "orphan", success: false });
    expect(byType(result.logEntries, "pi.message_update").at(-1).data.delta).toBe('{"command":');
    expect(selectSessionResult(result.logEntries).errors).toContainEqual({ code: "interrupted" });
  });

  it("emits toolcall starts in source order and reconciles owned partial and final arguments", () => {
    const records = [
      { type: "message_update", id: "start", assistantMessageEvent: { type: "toolcall_start", contentIndex: 0, id: "call", toolName: "bash" } },
      { type: "message_update", id: "delta", assistantMessageEvent: { type: "toolcall_delta", contentIndex: 0, delta: '{"command":' } },
      { type: "vendor.interleaved", data: { observed: true } },
      { type: "message_update", id: "end", assistantMessageEvent: { type: "toolcall_end", contentIndex: 0, toolCall: { id: "call", name: "bash", arguments: { command: "echo exact" } } } },
      { type: "tool_execution_start", toolCallId: "call", toolName: "bash", args: { command: "echo exact" } },
    ];
    const original = structuredClone(records);
    const events = transformPiV3Entries(records);
    expect(events.map(event => event.type)).toEqual(["pi.message_update", "tool.execution_start", "pi.message_update", "vendor.interleaved", "pi.message_update"]);
    const start = byType(events, "tool.execution_start")[0];
    expect(start.id).toBe("start");
    expect(start.data).toMatchObject({ toolCallId: "call", toolName: "bash", argumentText: '{"command":', input: { command: "echo exact" } });
    expect(byType(transformPiV3Entries(records.slice(0, 2)), "tool.execution_start")[0].data.argumentText).toBe('{"command":');
    expect(transformPiV3Entries(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
    expect(records).toEqual(original);
  });

  it("reconciles a later message snapshot into the original toolcall start", () => {
    const records = [
      { type: "message_update", id: "first", assistantMessageEvent: { type: "toolcall_start", contentIndex: 1, id: "call", toolName: "lookup" } },
      { type: "message_update", assistantMessageEvent: { type: "toolcall_delta", contentIndex: 1, delta: "false" } },
      {
        type: "message_end",
        message: {
          role: "assistant",
          content: [
            { type: "text", text: "after" },
            { type: "toolCall", id: "call", name: "lookup", arguments: false },
          ],
        },
      },
    ];
    const starts = byType(transformPiV3Entries(records), "tool.execution_start");
    expect(starts).toHaveLength(1);
    expect(starts[0]).toMatchObject({ id: "first", data: { input: false, argumentText: "false" } });
  });

  it("reconciles newly exposed tool IDs without conflating anonymous executions", () => {
    const events = transformPiV3Entries([
      { type: "message_update", id: "first", assistantMessageEvent: { type: "toolcall_start", contentIndex: 0 } },
      { type: "message_update", assistantMessageEvent: { type: "toolcall_end", contentIndex: 0, toolCall: { id: "known", name: "lookup", arguments: 0, nativeFlag: true } } },
      { type: "tool_execution_start", toolName: "bash", args: { command: "first" } },
      { type: "tool_execution_start", toolName: "bash", args: { command: "second" } },
    ]);
    const starts = byType(events, "tool.execution_start");
    expect(starts).toHaveLength(3);
    expect(starts[0]).toMatchObject({ id: "first", data: { toolCallId: "known", toolName: "lookup", input: 0, nativeFlag: true } });
    expect(starts.slice(1).map(event => event.data.input.command)).toEqual(["first", "second"]);
  });

  it("recovers malformed neighbors and handles mixed native, flat and v3 records", () => {
    const lines = [
      JSON.stringify({ type: "assistant", content: " flat " }),
      "debug: sanitized",
      '{"type":"message_end","message":',
      "null",
      JSON.stringify({ type: "vendor.progress", id: "native", data: { count: 0 } }),
      JSON.stringify({ type: "message_end", message: { role: "assistant", content: [{ type: "text", text: " v3 " }] } }),
    ];
    const { logEntries } = parsePiLog(lines.join("\n"));
    expect(logEntries.map(e => e.type)).toEqual(["assistant.message", "vendor.progress", "assistant.message", "session.result"]);
    expect(byType(logEntries, "assistant.message").map(e => e.data.content)).toEqual([" flat ", " v3 "]);
    expect(logEntries[1]).toMatchObject({ id: "native", data: { count: 0 } });
    expect(parsePiLog('{"type":"unknown","value":1}').logEntries).toEqual([]);
  });

  it("accounts for truncated message_end and terminal-only transcript snapshots", () => {
    const message = { role: "assistant", id: "m", content: [], usage: { input: 3, output: 2, cacheRead: 1, cost: { total: 0.1 } }, errorMessage: { code: "aborted" } };
    expect(computePiV3Stats([{ type: "message_end", message }])).toMatchObject({ turns: 1, input_tokens: 3, output_tokens: 2, total_cost_usd: 0.1, errors: [{ code: "aborted" }] });
    expect(computePiV3Stats([{ type: "agent_end", messages: [message] }])).toMatchObject({ turns: 1, input_tokens: 3, output_tokens: 2 });
    const snapshot = computePiV3Stats([
      { type: "turn_end", message },
      { type: "agent_end", messages: [message], usage: { input: 9, output: 5 }, totalCostUsd: 0, durationMs: 0 },
    ]);
    expect(snapshot).toMatchObject({ turns: 1, usage: { input_tokens: 9, output_tokens: 5, cache_read_input_tokens: 1 }, total_cost_usd: 0, duration_ms: 0 });
  });

  it("does not duplicate partial deltas when only agent_end supplies the final message", () => {
    const records = [
      { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "partial " } },
      { type: "agent_end", messages: [{ role: "assistant", content: [{ type: "text", text: "partial complete" }], usage: { input: 2 } }] },
    ];
    const events = transformPiV3Entries(records);
    expect(byType(events, "assistant.message").map(e => e.data.content)).toEqual(["partial ", "complete"]);
    expect(computePiV3Stats(records)).toMatchObject({ turns: 1, usage: { input_tokens: 2 } });
  });

  it("reconciles the same finalized report's later fields and duplicate source IDs", () => {
    const base = { role: "assistant", id: "assistant-id", content: [{ type: "text", text: "same" }] };
    const final = { ...base, usage: { input: 7, output: 2, cacheRead: 3, totalTokens: 12, cost: { total: 0.2 } } };
    const records = [
      { type: "message_end", message: { ...base, usage: { input: 7 } } },
      { type: "turn_end", id: "turn-id", message: final },
      { type: "turn_end", id: "turn-id", message: final },
      { type: "agent_end", messages: [final] },
    ];
    expect(computePiV3Stats(records)).toMatchObject({ turns: 1, usage: { input_tokens: 7, output_tokens: 2, cache_read_input_tokens: 3, total_tokens: 12 }, total_cost_usd: 0.2 });
    const noIdentity = { role: "assistant", content: [] };
    expect(
      computePiV3Stats([
        { type: "turn_end", message: noIdentity },
        { type: "turn_end", message: noIdentity },
      ]).turns
    ).toBe(2);
  });

  it("uses an authoritative terminal result instead of adding it to derived reports", () => {
    const records = [
      { type: "turn_end", message: { role: "assistant", content: [], usage: { input: 4, output: 1, cacheRead: 3 } } },
      { type: "result", stats: { input_tokens: 6, output_tokens: 2, turns: 1, duration_ms: 0, total_cost_usd: 0 } },
    ];
    expect(selectSessionResult(parse(records).logEntries)).toMatchObject({ numTurns: 1, durationMs: 0, totalCostUsd: 0, usage: { input_tokens: 6, output_tokens: 2, cache_read_input_tokens: 3 } });
    expect(computePiV3Stats([{ type: "result", stats: { turns: 0 } }]).turns).toBe(0);
  });

  it.each(["result", "session.result"])("does not synthesize a duplicate terminal result in a hybrid v3/%s stream", type => {
    const terminal =
      type === "result"
        ? { type, id: "terminal", stats: { input_tokens: 6, turns: 0, duration_ms: 0, total_cost_usd: 0 }, errors: ["terminal"], permission_denials: [] }
        : { type, id: "terminal", data: { usage: { input_tokens: 6 }, numTurns: 0, durationMs: 0, totalCostUsd: 0, errors: ["terminal"], permissionDenials: [] } };
    const records = [{ type: "message_end", message: { role: "assistant", id: "m", content: [], usage: { input: 4, output: 2, cacheRead: 3 } } }, terminal, { type: "vendor.trailing", data: { observed: true } }];
    const original = structuredClone(records);
    const events = parse(records).logEntries;
    expect(byType(events, "session.result")).toHaveLength(1);
    expect(byType(events, "session.result")[0]).toMatchObject({
      id: "terminal",
      data: { numTurns: 0, durationMs: 0, totalCostUsd: 0, usage: { input_tokens: 6, output_tokens: 2, cache_read_input_tokens: 3 }, errors: ["terminal"], permissionDenials: [] },
    });
    expect(events.at(-1).type).toBe("vendor.trailing");
    expect(parse(events).logEntries).toEqual(events);
    expect(records).toEqual(original);
  });

  it("keeps hybrid flat errors once while retaining separate v3 message diagnostics", () => {
    const events = parse([
      { type: "turn_start" },
      { type: "message_end", message: { role: "assistant", content: [], errorMessage: "same diagnostic" } },
      { type: "error", error: "same diagnostic" },
      { type: "result", errors: ["terminal diagnostic"] },
    ]).logEntries;
    expect(byType(events, "session.result")).toHaveLength(1);
    expect(byType(events, "pi.error")).toHaveLength(1);
    expect(selectSessionResult(events).errors).toEqual(["terminal diagnostic", "same diagnostic", "same diagnostic"]);
  });

  it("reconciles hybrid snapshots in source order, including a later agent_end", () => {
    const events = parse([
      { type: "message_end", message: { role: "assistant", id: "m", content: [], usage: { input: 4, output: 2, cacheRead: 3 } } },
      { type: "result", stats: { input_tokens: 6, output_tokens: 5, duration_ms: 10, turns: 2, total_cost_usd: 1 }, usage: { nativeFlag: true } },
      { type: "agent_end", messages: [], usage: { input: 0 }, durationMs: 0, totalCostUsd: 0, numTurns: 0 },
    ]).logEntries;
    expect(byType(events, "session.result")).toHaveLength(1);
    expect(selectSessionResult(events)).toMatchObject({ numTurns: 0, durationMs: 0, totalCostUsd: 0, usage: { input_tokens: 0, output_tokens: 5, cache_read_input_tokens: 3, nativeFlag: true } });
  });

  it("retains anonymous calls without inventing IDs or repeating message snapshots", () => {
    const message = { role: "assistant", timestamp: 8, content: [{ type: "toolCall", name: "bash", arguments: null }] };
    const events = transformPiV3Entries([null, [], { type: "message_end", message }, { type: "turn_end", message }, { type: "agent_end", messages: [message] }]);
    const starts = byType(events, "tool.execution_start");
    expect(starts).toHaveLength(1);
    expect(starts[0].data.input).toBeNull();
    expect(starts[0].data.toolCallId).toBeUndefined();
    expect(starts[0].id).toBeUndefined();
    expect(byType(events, "tool.execution_complete")).toEqual([]);
  });

  it("preserves explicit empty diagnostics and zero terminal accounting", () => {
    const records = [{ type: "agent_end", messages: [], errors: [], stats: { turns: 0, input_tokens: 0, output_tokens: 0, duration_ms: 0, total_cost_usd: 0 } }];
    expect(selectSessionResult(parse(records).logEntries)).toMatchObject({ numTurns: 0, durationMs: 0, totalCostUsd: 0, usage: { input_tokens: 0, output_tokens: 0 }, errors: [] });
  });

  it("uses Pi's separate-cache semantics when a partial usage report omits totalTokens", () => {
    const records = [{ type: "turn_end", message: { role: "assistant", content: [], usage: { input: 10, output: 3, cacheRead: 2, cacheWrite: 0 } } }];
    const usage = computePiV3Stats(records).usage;
    expect(usage).toMatchObject({ input_tokens: 10, output_tokens: 3, cache_read_input_tokens: 2, cache_creation_input_tokens: 0, input_tokens_include_cache: false });
    expect(sessionTokenTotal(usage)).toBe(15);
    expect(sessionTokenTotal({ ...usage, total_tokens: 14 })).toBe(14);
  });

  it("includes classifier/image, compaction, and cache-warming usage once", () => {
    const assistant = { role: "assistant", timestamp: 1, content: [], usage: { input: 100, output: 10, cost: { total: 0.1 } } };
    const tool = { role: "toolResult", toolCallId: "image-1", content: [], usage: { input: 50, output: 2, cost: { total: 0.2 } } };
    const stats = computePiV3Stats([
      { type: "message_end", message: assistant },
      { type: "message_end", message: tool },
      { type: "turn_end", message: assistant, toolResults: [tool] },
      { type: "compaction_end", result: { firstKeptEntryId: "entry-1", usage: { input: 200, output: 20, cost: { total: 0.3 } } } },
      { type: "entry_appended", entry: { id: "warming-1", type: "usage", usage: { input: 5, output: 0, cost: { total: 0.01 } } } },
    ]);
    expect(stats).toMatchObject({ turns: 1, input_tokens: 355, output_tokens: 32 });
    expect(stats.total_cost_usd).toBeCloseTo(0.61);
  });

  it("preserves nested tool relationships and settled/retry events", () => {
    const events = transformPiV3Entries([
      { type: "tool_execution_start", toolCallId: "outer/1", parentToolCallId: "outer", toolName: "bash", args: { command: "echo ok" } },
      { type: "tool_execution_end", toolCallId: "outer/1", parentToolCallId: "outer", toolName: "bash", result: {}, isError: false },
      { type: "auto_retry_start", attempt: 1 },
      { type: "agent_settled" },
    ]);
    expect(events[0].data.parentToolCallId).toBe("outer");
    expect(events[1].data.parentToolCallId).toBe("outer");
    expect(events.map(event => event.type)).toContain("pi.agent_settled");
    expect(events.map(event => event.type)).toContain("pi.auto_retry_start");
  });

  it("deduplicates snapshots and diagnostics independently of object key insertion order", () => {
    const message = {
      role: "assistant",
      content: [{ type: "text", text: "unchanged" }],
      usage: { input: 2, output: 1 },
      errorMessage: { code: "interrupted", details: { first: 1, second: 2 } },
    };
    const reordered = {
      errorMessage: { details: { second: 2, first: 1 }, code: "interrupted" },
      usage: { output: 1, input: 2 },
      content: [{ text: "unchanged", type: "text" }],
      role: "assistant",
    };
    const records = [
      { type: "message_end", message },
      { type: "turn_end", message: reordered },
      { type: "agent_end", messages: [reordered] },
    ];
    const events = transformPiV3Entries(records);
    expect(byType(events, "assistant.message").map(e => e.data.content)).toEqual(["unchanged"]);
    expect(computePiV3Stats(records)).toMatchObject({ turns: 1, usage: { input_tokens: 2, output_tokens: 1 }, errors: [message.errorMessage] });
  });
});
