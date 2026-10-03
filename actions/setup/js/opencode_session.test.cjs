import { describe, expect, it } from "vitest";
import { isOpenCodeRecord, normalizeOpenCodeSession, openCodeUsage } from "./opencode_session.cjs";
import { selectSessionResult } from "./agent_session.cjs";

// Synthetic native protocol cases; the retained CI runs contain no lifecycle events.
function cli(type, part, extra = {}) {
  return { type, sessionID: "session", timestamp: 0, ...extra, part: { sessionID: "session", messageID: "message", ...part } };
}
function native(type, properties, extra = {}) {
  return { type, properties, ...extra };
}
function text(part, extra = {}) {
  return native("message.part.updated", { part: { type: "text", sessionID: "session", messageID: "message", id: "part", ...part } }, extra);
}
function info(data) {
  return native("message.updated", { info: { id: "message", sessionID: "session", role: "assistant", ...data } });
}
function delta(content, extra = {}) {
  return native("message.part.delta", { sessionID: "session", messageID: "message", partID: "part", field: "text", delta: content }, extra);
}
function step(id, tokens, cost, extra = {}) {
  return cli("step_finish", { id, type: "step-finish", tokens, ...(cost !== undefined ? { cost } : {}), ...extra });
}

describe("OpenCode protocol normalization (synthetic)", () => {
  it("does not recognize arbitrary JSON, legacy bootstrap accounting, or namespace matches", () => {
    for (const value of [null, [], "text", {}, { type: "tool_use", tool: "bash", msg: "ok" }, { type: "text", text: "hello" }, { type: "session.status", properties: {} }, { type: "vendor.notice" }, { type: "result", num_turns: 1 }]) {
      expect(isOpenCodeRecord(value)).toBe(false);
    }
    expect(normalizeOpenCodeSession([null, {}, { type: "result", num_turns: 1 }])).toEqual([]);
  });

  it("preserves native extensions, identifiers and metadata without mutation and is idempotent", () => {
    const records = [
      native("session.created", { info: { id: "source-session", directory: "", parentID: null, version: "1.2.14", time: { created: 0 }, native: false } }, { id: "source-event", parentId: "", timestamp: 0 }),
      { type: "vendor.notice", data: { flag: false, nested: [null, "", 0] }, id: "native-id", extra: { enabled: false } },
      cli("text", { type: "text", id: "source-part", text: "" }, { id: "envelope-id", parentId: null, timestamp: "source-timestamp", native: { version: 7 } }),
    ];
    const original = structuredClone(records);
    const events = normalizeOpenCodeSession(records);
    expect(events[0]).toMatchObject({ type: "session.init", id: "source-event", parentId: "", timestamp: 0, data: { sourceEngine: "opencode", sessionId: "source-session", cwd: "", version: "1.2.14", native: false } });
    expect(events[1]).toEqual(records[1]);
    expect(events[2]).toMatchObject({ type: "assistant.message", id: "envelope-id", parentId: null, timestamp: "source-timestamp", native: { version: 7 }, data: { id: "source-part", content: "" } });
    expect(normalizeOpenCodeSession(records)).toEqual(events);
    expect(normalizeOpenCodeSession(events)).toEqual(events);
    events[1].data.nested.push("changed");
    expect(records).toEqual(original);
  });

  it("keeps ordered streaming fragments, whitespace and reasoning across tool boundaries without duplicating snapshots", () => {
    const records = [
      info({ modelID: "synthetic-model", providerID: "synthetic-provider" }),
      text({ text: "" }),
      delta(" \n", { id: "chunk-1", timestamp: 99 }),
      cli("tool_use", { type: "tool", callID: "call", tool: "bash", state: { status: "running", input: false } }),
      delta("A\\n\r\n", { id: "chunk-2", timestamp: 1 }),
      text({ text: " \nA\\n\r\n", time: { start: 0, end: 1 } }),
      cli("reasoning", { type: "reasoning", text: "\t\n" }),
      { type: "vendor.notice", data: {} },
      delta(""),
    ];
    const events = normalizeOpenCodeSession(records);
    expect(events.map(event => event.type)).toEqual([
      "opencode.message_snapshot",
      "assistant.message",
      "assistant.message",
      "tool.execution_start",
      "assistant.message",
      "opencode.part_snapshot",
      "assistant.reasoning",
      "vendor.notice",
      "assistant.message",
    ]);
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["", " \n", "A\\n\r\n", ""]);
    expect(events[2]).toMatchObject({ id: "chunk-1", timestamp: 99 });
    expect(events[4]).toMatchObject({ id: "chunk-2", timestamp: 1 });
    expect(events.find(event => event.type === "assistant.reasoning").data.content).toBe("\t\n");
    expect(events.some(event => event.type === "session.result" || event.type === "session.init")).toBe(false);
  });

  it("retains incomplete text, growing/revised snapshots, unknown-channel deltas and user text", () => {
    const events = normalizeOpenCodeSession([
      info({ role: "user" }),
      text({ text: " private\n" }),
      text({ text: " private\nnext" }),
      text({ text: "revised" }),
      native("message.part.delta", { sessionID: "session", messageID: "message", partID: "unknown", field: "text", delta: "?" }),
    ]);
    expect(events.filter(event => event.type === "user.message").map(event => event.data.content)).toEqual([" private\n", "next"]);
    expect(events.at(-2)).toMatchObject({ type: "opencode.part_snapshot", data: { part: { text: "revised" } } });
    expect(events.at(-1)).toMatchObject({ type: "opencode.part_delta", data: { delta: "?" } });
  });

  it.each([false, 0, "", null, [], {}, { content: [0, false] }])("preserves typed arguments and output (%j)", value => {
    const events = normalizeOpenCodeSession([cli("tool_use", { type: "tool", id: "part-id", callID: "", tool: "native_tool", state: { status: "completed", input: value, output: value, time: { start: 0, end: 0 } } }, { id: "event-id" })]);
    expect(events).toHaveLength(2);
    expect(events[0]).toMatchObject({ id: "event-id", data: { toolCallId: "", toolName: "native_tool", input: value } });
    expect(events[1]).toMatchObject({ id: "event-id", data: { toolCallId: "", output: value, success: true, durationMs: 0 } });
    expect(events[1].id).not.toBe(events[1].data.toolCallId);
  });

  it("keeps dangling and orphan calls, avoids matching conflicting IDs, and does not invent arguments or completions", () => {
    const events = normalizeOpenCodeSession([
      cli("tool_use", { type: "tool", callID: "dangling", tool: "same", state: { status: "running", input: [] } }),
      cli("tool_use", { type: "tool", callID: "orphan", tool: "same", state: { status: "error", error: "" } }),
      cli("tool_use", { type: "tool", tool: "anonymous", state: { status: "running", input: null } }),
      cli("error", undefined, { error: { name: "APIError", data: { message: "provider failed" } } }),
    ]);
    expect(events.filter(event => event.type === "tool.execution_start")).toHaveLength(2);
    expect(events.filter(event => event.type === "tool.execution_complete")).toHaveLength(1);
    expect(events.find(event => event.type === "tool.execution_complete").data).toMatchObject({ toolCallId: "orphan", error: "", success: false });
    expect(events.find(event => event.type === "tool.execution_complete").data.input).toBeUndefined();
    expect(events.filter(event => event.type === "tool.execution_start")[1].data.toolCallId).toBeUndefined();
    expect(events.at(-1)).toMatchObject({ type: "session.result", data: { errors: [{ name: "APIError" }] } });
  });

  it.each([{ isError: true }, { is_error: true }, { exitCode: 1 }, { exit: 2 }, { error: { code: "rejected" } }, { success: false }])("lets explicit failure override completed status (%j)", failure => {
    const events = normalizeOpenCodeSession([cli("tool_use", { type: "tool", callID: "failed", state: { status: "completed", output: { content: false }, metadata: failure } })]);
    expect(events[0]).toMatchObject({ type: "tool.execution_complete", data: { success: false, output: { content: false }, metadata: failure } });
    expect(events.some(event => event.type === "session.result")).toBe(false);
  });

  it("maps provider failures independently from unfinished calls and keeps native error structures", () => {
    const error = { name: "APIError", data: { statusCode: 403, isRetryable: false, message: "no access" } };
    const events = normalizeOpenCodeSession([native("session.error", { sessionID: "session", error }, { id: "error-id" }), info({ error, tokens: { input: 0, output: 0 }, cost: 0 })]);
    expect(events[0]).toMatchObject({ type: "session.result", id: "error-id", data: { errors: [error] } });
    expect(events.filter(event => event.type === "session.result" && event.data.errors).map(event => event.data.errors)).toEqual([[error], [error]]);
    expect(events.at(-1).data).toMatchObject({ totalCostUsd: 0, usage: { input_tokens: 0, output_tokens: 0 } });
    expect(events.filter(event => event.type === "assistant.message")).toEqual([]);
  });

  it("retains repeated tool snapshots without hiding a later explicit failure", () => {
    const completed = cli("tool_use", { type: "tool", id: "part", callID: "call", tool: "bash", state: { status: "completed", input: {}, output: "" } });
    const failed = structuredClone(completed);
    failed.part.state.error = { code: "late_failure" };
    const events = normalizeOpenCodeSession([completed, completed, failed]);
    expect(events.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(events.filter(event => event.type === "tool.execution_complete").map(event => event.data.success)).toEqual([true, false]);
    expect(events.filter(event => event.type === "opencode.part_snapshot")).toHaveLength(1);
  });

  it("accumulates distinct steps, replaces duplicate reports, and does not infer turns, duration or session success", () => {
    const events = normalizeOpenCodeSession([
      step("s1", { input: 2, output: 3, reasoning: 1, total: 8, cache: { read: 2, write: 1 } }, 0),
      step("s1", { input: 2, output: 3, reasoning: 1, total: 8, cache: { read: 2, write: 1 } }, 0),
      step("s2", { input: 4, output: 5, reasoning: 2, total: 14, cache: { read: 3, write: 2 } }, 0.25),
    ]);
    expect(selectSessionResult(events)).toMatchObject({
      totalCostUsd: 0.25,
      usage: { input_tokens: 6, output_tokens: 8, reasoning_output_tokens: 3, total_tokens: 22, cache_read_input_tokens: 5, cache_creation_input_tokens: 3, input_tokens_include_cache: false },
    });
    const result = events.at(-1).data;
    for (const field of ["numTurns", "durationMs", "success"]) expect(result).not.toHaveProperty(field);
  });

  it("reconciles message snapshots with prior steps without double counting repeated snapshots or step IDs", () => {
    const events = normalizeOpenCodeSession([
      step("s1", { input: 10, output: 2 }, 0.1),
      info({ tokens: { input: 10, output: 2 }, cost: 0.1, time: { completed: 10 } }),
      step("s1", { input: 10, output: 2 }, 0.1),
      info({ tokens: { input: 10, output: 2 }, cost: 0.1, time: { completed: 10 } }),
      step("s2", { input: 3, output: 1 }, 0.2),
      info({ tokens: { input: 3, output: 1 }, cost: 0.3, time: { completed: 20 } }),
    ]);
    expect(events.at(-1).data).toMatchObject({ totalCostUsd: 0.3, usage: { input_tokens: 13, output_tokens: 3 } });
  });

  it("keeps accounting scoped to native sessions and messages", () => {
    const records = [
      step("s1", { input: 10 }, 1),
      cli("step_finish", { type: "step-finish", id: "s1", messageID: "other", tokens: { input: 5 } }),
      cli("step_finish", { type: "step-finish", id: "s1", sessionID: "other-session", tokens: { input: 2 } }, { sessionID: "other-session" }),
    ];
    const results = normalizeOpenCodeSession(records).filter(event => event.type === "session.result");
    expect(results.map(event => event.data.usage.input_tokens)).toEqual([15, 2]);
  });

  it("reconciles sparse overlapping snapshots field by field without erasing observed values", () => {
    const events = normalizeOpenCodeSession([
      step("s1", { input: 10, output: 2, cache: { read: 3 } }, 0.1),
      info({ tokens: { input: 10 }, cost: 0.1 }),
      step("s2", { input: 3, output: 1, cache: { read: 2 } }, 0.2),
      info({ tokens: { input: 3 } }),
    ]);
    expect(events.at(-1).data).toMatchObject({ totalCostUsd: 0.1 + 0.2, usage: { input_tokens: 13, output_tokens: 3, cache_read_input_tokens: 5 } });
  });

  it("treats native message tokens as last-step snapshots, not cumulative contributions", () => {
    const events = normalizeOpenCodeSession([info({ tokens: { input: 10, output: 2 }, cost: 0.1 }), info({ tokens: { input: 3, output: 1 }, cost: 0.3 }), info({ tokens: { input: 3, output: 1 }, cost: 0.3 })]);
    expect(events.at(-1).data).toMatchObject({ totalCostUsd: 0.3, usage: { input_tokens: 3, output_tokens: 1 } });
    const stepReports = normalizeOpenCodeSession([
      step("s1", { input: 10, output: 2 }, 0.1),
      info({ tokens: { input: 10, output: 2 }, cost: 0.1 }),
      step("s2", { input: 3, output: 1 }, 0.2),
      info({ tokens: { input: 3, output: 1 }, cost: 0.3 }),
    ]);
    expect(stepReports.at(-1).data).toMatchObject({ totalCostUsd: 0.3, usage: { input_tokens: 13, output_tokens: 3 } });
  });

  it("omits unavailable/invalid accounting rather than introducing zeros and reports unsafe sums", () => {
    expect(openCodeUsage({ input: -1, output: Infinity, cache: { read: 0.5 }, total: NaN })).toBeUndefined();
    expect(normalizeOpenCodeSession([step("empty", undefined, undefined)]).map(event => event.type)).toEqual(["opencode.step_finish"]);
    const events = normalizeOpenCodeSession([step("s1", { input: Number.MAX_SAFE_INTEGER, total: Number.MAX_SAFE_INTEGER }, Number.MAX_VALUE), step("s2", { input: 1, total: 1 }, Number.MAX_VALUE)]);
    expect(events.at(-1).data.usage.input_tokens).toBeUndefined();
    expect(events.at(-1).data.usage.overflowed_tokens).toContain("input_tokens");
    expect(events.at(-1).data.usage.total_tokens).toBeUndefined();
    expect(events.at(-1).data.usage.opencodeOverflowedTokens).toContain("total_tokens");
    expect(events.at(-1).data.totalCostUsd).toBeUndefined();
    expect(events.at(-1).data.costOverflow).toBe(true);
    expect(selectSessionResult(events).totalCostUsd).toBeUndefined();
    expect(selectSessionResult(events).usage.total_tokens).toBeUndefined();
  });
});
