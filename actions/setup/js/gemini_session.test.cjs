import { describe, it, expect } from "vitest";
import { normalizeGeminiSession } from "./gemini_session.cjs";
import { parseGeminiLog, transformGeminiEntries } from "./parse_gemini_log.cjs";
import { selectSessionResult } from "./agent_session.cjs";
import { generatePlainTextSummary, generateCopilotCliStyleSummary } from "./log_parser_shared.cjs";
const { spendingCapSeptember29, spendingCapSeptember27 } = require("./fixtures/gemini_ci_sessions.cjs");

const jsonl = records => records.map(record => JSON.stringify(record)).join("\n");
const json = value => JSON.parse(JSON.stringify(value));
function freeze(value) {
  if (value && typeof value === "object") {
    Object.freeze(value);
    for (const nested of Object.values(value)) freeze(nested);
  }
  return value;
}

describe("Gemini CI-backed provider failures", () => {
  it.each([spendingCapSeptember29, spendingCapSeptember27])("retains the three supported observations without inventing activity", (...records) => {
    const source = freeze(records);
    const parsed = parseGeminiLog(jsonl(source));
    expect(parsed.logEntries.map(event => event.type)).toEqual(["session.init", "user.message", "session.result"]);
    expect(parsed.logEntries[0]).toMatchObject({ timestamp: source[0].timestamp, data: { sourceEngine: "gemini", model: "auto", sessionId: source[0].session_id } });
    expect(parsed.logEntries[1].data.content).toBe(source[1].content);
    expect(parsed.logEntries[2]).toMatchObject({
      timestamp: source[2].timestamp,
      data: {
        status: "error",
        errors: [source[2].error],
        durationMs: 0,
        usage: { total_tokens: 0, input_tokens: 0, output_tokens: 0, cache_read_input_tokens: 0, input_tokens_include_cache: true },
        stats: source[2].stats,
      },
    });
    expect(parsed.logEntries[2].data.numTurns).toBeUndefined();
    expect(parsed.logEntries[2].data.totalCostUsd).toBeUndefined();
    expect(parsed.markdown).toContain("monthly spending cap");
    for (const summary of [parsed.markdown, generatePlainTextSummary(parsed.logEntries), generateCopilotCliStyleSummary(parsed.logEntries)]) {
      expect(summary).not.toContain("SANITIZED_CI_PROMPT");
      expect(summary).not.toContain("Turns:");
    }
  });
});

// Supplemental cases below are synthetic protocol/compatibility fixtures, not CI observations.
describe("Gemini synthetic unified-session regressions", () => {
  it("T-UAS-007/009: maps complete initialization and preserves empty values and native additions", () => {
    const source = freeze({
      type: "init",
      id: "init-record",
      parentId: null,
      timestamp: 0,
      session_id: "session",
      model: "fixture-model",
      cwd: "",
      tools: [],
      mcp_servers: [{ name: "catalog", status: "failed", diagnostic: { code: "offline" } }],
      slash_commands: [],
      model_info: { billing: false },
      native: { zero: 0, no: false, nil: null },
    });
    const event = transformGeminiEntries([source])[0];
    expect(event).toMatchObject({
      type: "session.init",
      id: "init-record",
      parentId: null,
      timestamp: 0,
      native: source.native,
      data: { sourceEngine: "gemini", sessionId: "session", model: "fixture-model", cwd: "", tools: [], mcpServers: source.mcp_servers, slashCommands: [], modelInfo: source.model_info, native: source.native },
    });
  });

  it("T-UAS-016/018/019: recovers malformed-adjacent records and expands mixed legacy blocks in order", () => {
    const legacy = {
      type: "user",
      id: "legacy-record",
      parentId: null,
      timestamp: 2,
      extra: { retained: true },
      message: {
        content: [
          { type: "text", text: "  private\n" },
          { type: "tool_result", tool_use_id: "native-call", content: false, is_error: false },
        ],
      },
    };
    const content = [
      "[DEBUG] framing",
      '{"type":"message","content":',
      jsonl([{ type: "tool.execution_start", id: "native-start", data: { toolCallId: "native-call", toolName: "lookup", input: null } }, legacy]),
      "[null,42,[],{}]",
      '{"type":"vendor.progress","data":{"nested":{"zero":0,"no":false,"nil":null}}}',
      '{"type":"assistant","message":{"content":[{"type":"thinking","thinking":" \\r\\n"},{"type":"text","text":""}]}}',
      '{"type":"result","stats":{"input_tokens":3}}',
      '{"truncated":',
    ].join("\n");
    const parsed = parseGeminiLog(content);
    expect(parsed.logEntries.map(event => event.type)).toEqual(["tool.execution_start", "user.message", "tool.execution_complete", "vendor.progress", "assistant.reasoning", "assistant.message", "session.result"]);
    expect(parsed.logEntries[1].data.content).toBe("  private\n");
    for (const event of parsed.logEntries.slice(1, 3)) expect(event).toMatchObject({ id: "legacy-record", parentId: null, timestamp: 2, extra: legacy.extra });
    expect(parsed.logEntries[2].data).toMatchObject({ toolCallId: "native-call", toolName: "lookup", output: false, success: true });
    expect(parsed.logEntries[3].data).toEqual({ nested: { zero: 0, no: false, nil: null } });
    expect(parsed.logEntries[4].data.content).toBe(" \r\n");
    expect(parsed.logEntries[5].data.content).toBe("");
  });

  it.each([
    "{}",
    '{"type":"vendor.progress"}',
    '{"type":"message","role":"system","content":"text"}',
    '{"type":"message","role":"system","error":"not a Gemini message"}',
    '{"type":"message","role":"system","content":[{"type":"text","text":"not a Gemini message"}]}',
    "[null,42,[]]",
    '{"type":"tool.unknown","data":[]}',
  ])("T-UAS-017: unrelated JSON is not Gemini evidence: %s", input => {
    const parsed = parseGeminiLog(input);
    expect(parsed.logEntries).toEqual([]);
    expect(parsed.markdown).toContain("Log format not recognized");
  });

  it("T-UAS-020/021: preserves streaming whitespace and each distinct source envelope", () => {
    const records = ["", " ", "\r\n", "λ", "\\n"].map((content, index) => ({
      type: "message",
      role: "assistant",
      content,
      delta: true,
      message_id: "message",
      id: `native-record-${index}`,
      parentId: null,
      timestamp: index,
      native: { sequence: index },
    }));
    const events = normalizeGeminiSession(records);
    expect(events).toHaveLength(records.length);
    expect(events.map(event => event.data.content).join("")).toBe(" \r\nλ\\n");
    for (const [index, event] of events.entries())
      expect(event).toMatchObject({ type: "assistant.message", id: records[index].id, timestamp: index, parentId: null, native: records[index].native, data: { delta: true, content: records[index].content } });
  });

  it("does not coalesce separate message IDs, reasoning channels, native additions, or intervening events", () => {
    const events = normalizeGeminiSession([
      { type: "message", role: "assistant", message_id: "a", delta: true, content: "A" },
      { type: "message", role: "assistant", message_id: "b", delta: true, content: "B" },
      { type: "reasoning", message_id: "b", delta: true, content: "  think\n" },
      { type: "reasoning", message_id: "b", delta: true, content: "", native: false },
      { type: "vendor.progress", data: {} },
      { type: "reasoning", message_id: "b", delta: true, content: "next" },
      { type: "message", role: "user", content: "" },
    ]);
    expect(events.map(event => event.type)).toEqual(["assistant.message", "assistant.message", "assistant.reasoning", "assistant.reasoning", "vendor.progress", "assistant.reasoning", "user.message"]);
    expect(events.map(event => event.data.content)).toEqual(["A", "B", "  think\n", "", undefined, "next", ""]);
  });

  it("retains identified final snapshots without duplicating streamed content, even across tools", () => {
    const events = normalizeGeminiSession([
      { type: "message", role: "assistant", message_id: "a", delta: true, content: "Hello " },
      { type: "message", role: "assistant", message_id: "a", delta: true, content: "world" },
      { type: "tool_use", tool_id: "call", tool_name: "lookup", parameters: {} },
      { type: "message", role: "assistant", message_id: "a", content: "Hello world!\n", timestamp: "final" },
      { type: "message", role: "assistant", message_id: "a", content: "Hello world!\n", timestamp: "repeat" },
      { type: "message", role: "assistant", message_id: "a", content: "corrected text", timestamp: "correction" },
      { type: "message", role: "assistant", message_id: "b", content: "Hello world!\n" },
    ]);
    expect(events.map(event => event.type)).toEqual(["assistant.message", "tool.execution_start", "gemini.message_snapshot", "assistant.message", "gemini.message_snapshot", "gemini.message_snapshot", "assistant.message"]);
    expect(events[0].data.content).toBe("Hello world");
    expect(events[2].data.content).toBe("Hello world!\n");
    expect(events[3]).toMatchObject({ timestamp: "final", data: { content: "!\n" } });
    expect(events[5].data.content).toBe("corrected text");
    expect(events[6].data.content).toBe("Hello world!\n");
  });

  it("does not assume an event ID or anonymous full message is a final snapshot", () => {
    const events = normalizeGeminiSession([
      { type: "message", role: "assistant", id: "event", delta: true, content: "same" },
      { type: "message", role: "assistant", id: "event", content: "same" },
      { type: "message", role: "assistant", content: "same" },
    ]);
    expect(events.map(event => event.type)).toEqual(["assistant.message", "assistant.message", "assistant.message"]);
  });

  it("reconciles legacy snapshots in content-block order, including a flat-to-legacy message", () => {
    const events = normalizeGeminiSession([
      { type: "message", role: "assistant", message_id: "mixed", delta: true, content: "Hello" },
      { type: "assistant", message: { id: "mixed", content: [{ type: "text", text: "Hello\n" }] } },
      {
        type: "assistant",
        message: {
          id: "blocks",
          content: [
            { type: "thinking", thinking: " think" },
            { type: "text", text: "one" },
            { type: "text", text: "two" },
          ],
        },
      },
      {
        type: "assistant",
        message: {
          id: "blocks",
          content: [
            { type: "thinking", thinking: " think\n" },
            { type: "text", text: "one!" },
            { type: "text", text: "two" },
          ],
        },
      },
    ]);
    expect(events.map(event => event.type)).toEqual([
      "assistant.message",
      "gemini.message_snapshot",
      "assistant.message",
      "assistant.reasoning",
      "assistant.message",
      "assistant.message",
      "gemini.message_snapshot",
      "assistant.reasoning",
      "assistant.message",
    ]);
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["Hello", "\n", "one", "two", "!"]);
    expect(events.filter(event => event.type === "assistant.reasoning").map(event => event.data.content)).toEqual([" think", "\n"]);
  });

  it.each([false, 0, "", null, [], {}, { content: [{ type: "json", json: { count: 0 } }] }])("T-UAS-005/012/013: preserves structured/falsy arguments and output %j", value => {
    const records = freeze([
      { type: "tool_use", id: "record-start", tool_id: "native-call", tool_name: "lookup", input: value, parameters: "do not select alias", command: "", mcpServerName: "", native: { retained: true } },
      { type: "tool_result", id: "record-end", tool_id: "native-call", status: "success", output: value, result: "do not select alias", duration_ms: 0 },
    ]);
    const events = normalizeGeminiSession(records);
    expect(events[0]).toMatchObject({ id: "record-start", data: { toolCallId: "native-call", input: value, parameters: "do not select alias", command: "", mcpServerName: "" } });
    expect(events[1]).toMatchObject({ id: "record-end", data: { toolCallId: "native-call", toolName: "lookup", output: value, result: "do not select alias", durationMs: 0, success: true } });
    expect(json(events[0].data.input)).toEqual(value);
    expect(json(events[1].data.output)).toEqual(value);
  });

  it.each([{ error: "" }, { error: { code: "permission" } }, { is_error: true }, { isError: true }, { success: false }, { exit_code: 1 }, { exitCode: 2 }, { result: { isError: true } }, { output: { is_error: true } }])(
    "T-UAS-014: explicit failure overrides a claimed successful tool result %j",
    signal => {
      const event = normalizeGeminiSession([{ type: "tool_result", status: "success", tool_id: "failed", ...signal }])[0];
      expect(event.type).toBe("tool.execution_complete");
      expect(event.data.success).toBe(false);
      expect(event.data.status).toBe("success");
      expect(event).toMatchObject(signal);
    }
  );

  it("T-UAS-022/023/024: keeps independent concurrent calls, orphans, missing IDs and dangling starts", () => {
    const events = normalizeGeminiSession([
      { type: "tool_use", tool_id: "a", tool_name: "first", parameters: null },
      { type: "tool_use", tool_id: "b", tool_name: "second", parameters: false },
      { type: "tool_result", tool_id: "b", status: "success", output: "" },
      { type: "tool_result", tool_id: "other", output: 0 },
      { type: "tool_result", output: null },
      { type: "tool_result", tool_id: "a", status: "error", error: { type: "permission_denied", message: "Denied" } },
      { type: "tool_use", tool_id: "dangling", tool_name: "unfinished" },
    ]);
    expect(events.map(event => event.type)).toEqual(["tool.execution_start", "tool.execution_start", "tool.execution_complete", "tool.execution_complete", "tool.execution_complete", "tool.execution_complete", "tool.execution_start"]);
    expect(events[2].data.toolName).toBe("second");
    expect(events[3].data.toolCallId).toBe("other");
    expect(events[3].data.toolName).toBeUndefined();
    expect(events[3].data.success).toBeUndefined();
    expect(events[4].data.toolCallId).toBeUndefined();
    expect(events[4].data.toolName).toBeUndefined();
    expect(events[4].data.success).toBeUndefined();
    expect(events[5].data).toMatchObject({ toolCallId: "a", toolName: "first", success: false, error: { type: "permission_denied", message: "Denied" } });
    expect(events.at(-1).data.input).toBeUndefined();
    expect(events.some(event => event.type === "session.result")).toBe(false);
  });

  it("scopes tool and message identities across observed sessions", () => {
    const events = normalizeGeminiSession([
      { type: "init", session_id: "one" },
      { type: "tool_use", tool_id: "reused", tool_name: "old" },
      { type: "message", role: "assistant", message_id: "reused", delta: true, content: "old" },
      { type: "session.init", data: { sessionId: "two" } },
      { type: "tool_result", tool_id: "reused", output: false },
      { type: "message", role: "assistant", message_id: "reused", content: "new" },
    ]);
    expect(events[4].data.toolName).toBeUndefined();
    expect(events[5]).toMatchObject({ type: "assistant.message", data: { content: "new" } });
  });

  it("T-UAS-015/035: separates tool failures, provider errors, permission denials and warnings", () => {
    const records = [
      { type: "tool_result", tool_id: "denied", status: "error", error: { type: "permission_denied", message: "Tool denied" } },
      { type: "error", severity: "warning", message: "Provider retry warning", timestamp: "warning" },
      { type: "error", severity: "error", message: "Provider unavailable", timestamp: "attempt-1" },
      { type: "error", severity: "error", message: "Provider unavailable", timestamp: "attempt-2" },
      { type: "message", role: "assistant", content: "", error: { code: "provider" }, permission_denials: [{ tool_id: "denied", permission: "write" }] },
      { type: "result", status: "error", errors: [], permission_denials: [] },
      { type: "vendor.after_result", data: { retained: true } },
    ];
    const events = normalizeGeminiSession(records);
    expect(events.map(event => event.type)).toEqual([
      "tool.execution_complete",
      "gemini.error",
      "gemini.error",
      "session.result",
      "gemini.error",
      "session.result",
      "gemini.message_error",
      "session.result",
      "session.result",
      "vendor.after_result",
    ]);
    expect(events.filter(event => event.type === "assistant.message")).toEqual([]);
    expect(events[3]).toMatchObject({ timestamp: "attempt-1", data: { errors: [{ severity: "error", message: "Provider unavailable" }] } });
    expect(events[5]).toMatchObject({ timestamp: "attempt-2", data: { errors: [{ severity: "error", message: "Provider unavailable" }] } });
    expect(events[7].data).toMatchObject({ errors: [{ code: "provider" }], permissionDenials: records[4].permission_denials });
    expect(events[8].data).toMatchObject({ errors: [{ status: "error" }], permissionDenials: [] });
    expect(selectSessionResult(events).errors).toHaveLength(4);
  });

  it("preserves both an explicit error array and a separate terminal error", () => {
    const events = normalizeGeminiSession([{ type: "result", status: "success", errors: [{ code: "retry" }], error: { code: "terminal" } }]);
    expect(events[0].data.errors).toEqual([{ code: "retry" }, { code: "terminal" }]);
    expect(events[0].data.status).toBe("success");
  });

  it("retains user prompt metadata without reclassifying the prompt as a provider diagnostic", () => {
    const parsed = parseGeminiLog(jsonl([{ type: "message", role: "user", content: "PRIVATE_FAILURE_PROMPT", status: "error", error: { code: "native-user-metadata" } }]));
    expect(parsed.logEntries.map(event => event.type)).toEqual(["user.message"]);
    expect(parsed.logEntries[0].data).toMatchObject({ content: "PRIVATE_FAILURE_PROMPT", error: { code: "native-user-metadata" } });
    expect(parsed.markdown).not.toContain("PRIVATE_FAILURE_PROMPT");
    expect(generatePlainTextSummary(parsed.logEntries)).not.toContain("PRIVATE_FAILURE_PROMPT");
    expect(generateCopilotCliStyleSummary(parsed.logEntries)).not.toContain("PRIVATE_FAILURE_PROMPT");
  });

  it("T-UAS-030/033/034: selects cumulative result snapshots by field, without summing or inventing metrics", () => {
    const parsed = parseGeminiLog(
      jsonl([
        { type: "result", stats: { input_tokens: 10, output_tokens: 2, cached: 6, total_tokens: 12, tool_calls: 4, duration_ms: 3 }, usage: { extra: { retained: true } } },
        { type: "result", stats: { input_tokens: 4, tool_calls: 8 } },
        { type: "result", stats: { output_tokens: 0, duration_ms: 0, total_cost_usd: 0, turns: 0 } },
        { type: "vendor.progress", data: {} },
      ])
    );
    expect(parsed.logEntries.map(event => event.type)).toEqual(["session.result", "session.result", "session.result", "vendor.progress"]);
    expect(parsed.logEntries[0].data.numTurns).toBeUndefined();
    expect(parsed.logEntries[1].data.totalCostUsd).toBeUndefined();
    expect(selectSessionResult(parsed.logEntries)).toMatchObject({
      usage: { input_tokens: 4, output_tokens: 0, cache_read_input_tokens: 6, total_tokens: 12, extra: { retained: true } },
      durationMs: 0,
      totalCostUsd: 0,
      numTurns: 0,
    });
  });

  it.each([-1, 0.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1, "12", null])("invalid observed token counts are not mapped to zero or another source field: %s", invalid => {
    const event = normalizeGeminiSession([
      {
        type: "result",
        stats: { input_tokens: invalid, output_tokens: invalid, cached: invalid, total_tokens: invalid, turns: invalid, duration_ms: null },
        usage: { input_tokens: 9 },
        num_turns: 7,
        duration_ms: 6,
      },
    ])[0];
    expect(event.data.usage?.input_tokens).toBeUndefined();
    expect(event.data.usage?.output_tokens).toBeUndefined();
    expect(event.data.usage?.cache_read_input_tokens).toBeUndefined();
    expect(event.data.usage?.total_tokens).toBeUndefined();
    expect(event.data.numTurns).toBeUndefined();
    expect(event.data.durationMs).toBeUndefined();
    expect(event.stats.input_tokens).toBe(invalid);
  });

  it("uses canonical usage aliases without replacing zero and never derives cost or turns from a total", () => {
    const events = normalizeGeminiSession([
      { type: "result", usage: { input_tokens: 0, inputTokens: 9, outputTokens: 2, cacheCreationInputTokens: 0, native: false }, num_turns: 0 },
      { type: "result", stats: { total_tokens: 100, models: { model: { total_tokens: 100 } }, tool_calls: 99 } },
    ]);
    expect(events[0].data).toMatchObject({ numTurns: 0, usage: { input_tokens: 0, inputTokens: 9, output_tokens: 2, cache_creation_input_tokens: 0, native: false } });
    expect(events[1].data.usage).toEqual({ total_tokens: 100 });
    expect(events[1].data.numTurns).toBeUndefined();
    expect(events[1].data.durationMs).toBeUndefined();
    expect(events[1].data.totalCostUsd).toBeUndefined();
  });

  it("does not restore invalid canonical usage from an alias and permits a valid snapshot after overflow", () => {
    const events = normalizeGeminiSession([
      { type: "result", usage: { input_tokens: null, inputTokens: 9 } },
      { type: "result", usage: { inputTokens: 8, overflowed_tokens: ["input_tokens"] }, stats: { input_tokens: 0 } },
    ]);
    expect(events[0].data.usage).toBeUndefined();
    expect(events[0].usage).toEqual({ input_tokens: null, inputTokens: 9 });
    expect(events[1].data.usage).toEqual({ input_tokens: 0 });
    expect(selectSessionResult(events).usage.input_tokens).toBe(0);
  });

  it("T-UAS-025/026/029: is deterministic, idempotent, JSON-preserving and independent of frozen input", () => {
    const records = freeze([
      { type: "init", session_id: "fixture", tools: [] },
      { type: "message", role: "assistant", content: "", delta: true },
      { type: "message", role: "assistant", content: "\n ", delta: true },
      { type: "tool_use", id: "record", tool_id: "call", tool_name: "lookup", parameters: { nested: [false, null] } },
      { type: "tool_result", tool_id: "call", status: "success", output: { items: [] } },
      { type: "result", stats: { input_tokens: 0 }, errors: [], permission_denials: [] },
      { type: "vendor.progress", id: "native", data: { nested: { no: false } } },
    ]);
    const original = json(records);
    const events = normalizeGeminiSession(records);
    expect(normalizeGeminiSession(records)).toEqual(events);
    expect(normalizeGeminiSession(events)).toEqual(events);
    expect(normalizeGeminiSession(json(events))).toEqual(json(events));
    expect(transformGeminiEntries(records)).toEqual(events);
    events[0].data.tools.push("output-copy");
    events[2].data.input.nested.push("output-copy");
    events[3].data.output.items.push("output-copy");
    events.at(-1).data.nested.no = true;
    expect(records).toEqual(original);
  });
});
