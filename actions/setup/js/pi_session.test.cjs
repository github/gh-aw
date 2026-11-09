import { describe, it, expect } from "vitest";
import { parsePiLog } from "./parse_pi_log.cjs";
import { transformPiV3Entries, computePiV3Stats } from "./pi_session.cjs";
import { success, failure, childObservations } from "./fixtures/pi_ci_stream.cjs";
import { normalizeAgentSession, selectSessionResult, sessionTokenTotal } from "./agent_session.cjs";
import { mergeSessionSources } from "./unified_session.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { createSessionValidator } from "./scripts/validate_session.cjs";

const parse = records => parsePiLog(records.map(record => JSON.stringify(record)).join("\n"));
const byType = (events, type) => events.filter(event => event.type === type);

describe("Pi CI stream regressions", () => {
  // Synthetic content in the shape of Smoke Pi 42539928028 and
  // Smoke Pi Sub Agents 39516073462; no captured payloads are published.
  it.each([0, 1794184043729, "2026-11-09T00:27:23.729Z"])("retains the observed message clock %s through streaming reconciliation and publication", timestamp => {
    const message = {
      role: "assistant",
      responseId: "sanitized-response",
      model: "requested-model",
      responseModel: "served-model",
      timestamp,
      content: [
        { type: "thinking", thinking: "Sanitized reasoning." },
        { type: "toolCall", id: "sanitized-tool", name: "lookup", arguments: { nested: [false, 0, null, "line\n\u00e9"] } },
        { type: "text", text: "Sanitized answer.\n" },
      ],
      usage: { input: 10, output: 3 },
    };
    const records = [
      { type: "turn_start" },
      { type: "message_update", assistantMessageEvent: { type: "thinking_delta", contentIndex: 0, delta: "Sanitized reasoning." } },
      { type: "message_update", assistantMessageEvent: { type: "toolcall_start", contentIndex: 1, id: "sanitized-tool", toolName: "lookup" } },
      { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 2, delta: "Sanitized answer.\n" } },
      { type: "message_end", message },
      { type: "turn_end", message },
      { type: "agent_end", messages: [message] },
    ];
    const original = structuredClone(records);
    const events = parse(records).logEntries;
    const messages = events.filter(event => event.data.messageId === message.responseId);
    expect(messages.map(event => event.type)).toEqual(["assistant.reasoning", "tool.execution_start", "assistant.message"]);
    expect(messages.every(event => event.timestamp === timestamp)).toBe(true);
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events }]);
    for (const event of unified.filter(event => event.type !== "session.result")) {
      expect(event.timestamp).toBe(timestamp);
      expect(event.provenance.timestampMs).toBe(typeof timestamp === "number" ? timestamp : Date.parse(timestamp));
      expect(event.data).toMatchObject({ messageId: message.responseId, model: "served-model" });
      expect(event.data).not.toHaveProperty("messageMetadata");
      expect(event).not.toHaveProperty("assistantMessageEvent");
    }
    expect(unified.find(event => event.type === "tool.execution_start").data.input).toEqual(message.content[1].arguments);
    expect(selectSessionResult(events).usage).toMatchObject({ input_tokens: 10, output_tokens: 3 });
    expect(records).toEqual(original);
  });

  it.each(["timestamp", "ts", "time", "created_at"])("preserves the source %s clock over embedded message clocks and never invents a missing clock", clock => {
    const message = { role: "assistant", timestamp: 10, content: [{ type: "text", text: "Sanitized answer" }] };
    const events = transformPiV3Entries([
      { type: "message_update", [clock]: 5, assistantMessageEvent: { type: "text_delta", delta: "Sanitized answer" } },
      { type: "message_end", timestamp: 20, message },
    ]);
    expect(events).toHaveLength(1);
    expect(events[0][clock]).toBe(5);
    if (clock !== "timestamp") expect(events[0]).not.toHaveProperty("timestamp");
    expect(events[0].data.messageMetadata.timestamp).toBe(10);
    const unified = mergeSessionSources([
      { component: "agent", phase: "agent", path: "pi-streaming.jsonl", events },
      { component: "other", phase: "agent", path: "other.jsonl", events: [{ type: "session.info", timestamp: 7, data: { content: "later" } }] },
    ]);
    expect(unified[0]).toMatchObject({ type: "assistant.message", provenance: { timestampMs: 5 } });
    const [untimed] = transformPiV3Entries([{ type: "message_end", message: { ...message, timestamp: undefined } }]);
    expect(untimed).not.toHaveProperty("timestamp");
  });

  it("recovers the sampled tool-result clock onto its single observed execution completion", () => {
    const original = structuredClone(success);
    const events = parse(success).logEntries;
    expect(byType(events, "tool.execution_complete")).toMatchObject([{ timestamp: 3 }]);
    expect(byType(events, "tool.execution_complete")).toHaveLength(1);
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "pi-streaming.jsonl", events }]);
    expect(byType(unified, "tool.execution_complete")).toMatchObject([{ timestamp: 3, provenance: { timestampMs: 3 } }]);
    expect(success).toEqual(original);
  });

  it.each([false, true])("reconciles repeated tool-result clocks when the execution end arrives first=%s", endFirst => {
    const end = { type: "tool_execution_end", toolCallId: "call", result: { content: "observed execution" }, isError: false };
    const result = { type: "message_end", message: { role: "toolResult", toolCallId: "call", timestamp: 3, content: "snapshot" } };
    const records = endFirst ? [end, result, result] : [result, result, end];
    const [event] = transformPiV3Entries(records);
    expect(transformPiV3Entries(records)).toHaveLength(1);
    expect(event).toMatchObject({ timestamp: 3, data: { output: end.result, success: true } });
  });

  it.each(["timestamp", "ts", "time", "created_at"])("does not replace an execution completion's explicit %s clock with its result clock", clock => {
    const events = transformPiV3Entries([
      { type: "tool_execution_end", [clock]: 0, toolCallId: "call", result: { content: "observed execution" } },
      { type: "message_end", message: { role: "toolResult", toolCallId: "call", timestamp: 3, content: "snapshot" } },
    ]);
    expect(events).toHaveLength(1);
    expect(events[0][clock]).toBe(0);
    if (clock !== "timestamp") expect(events[0]).not.toHaveProperty("timestamp");
  });

  it("retains child, tool-result and provider-error clocks without duplicating snapshots or parent usage", () => {
    const child = { role: "assistant", responseId: "child-response", model: "fixture-model", timestamp: 3, content: [{ type: "text", text: "Child answer" }], usage: { input: 5, output: 2 } };
    const records = [
      { type: "gh_aw_subagent_dispatch", agent: "reader", invocation_id: "child", timestamp: 1 },
      { type: "gh_aw_subagent_event", agent: "reader", invocation_id: "child", timestamp: 4, event: { type: "message_end", message: child } },
      { type: "gh_aw_subagent_event", agent: "reader", invocation_id: "child", timestamp: 5, event: { type: "turn_end", message: child } },
      { type: "message_end", message: { role: "toolResult", toolCallId: "tool", timestamp: 6, content: [{ type: "text", text: "result" }], isError: false } },
      { type: "message_end", message: { role: "assistant", timestamp: 7, content: [], errorMessage: "Sanitized provider failure", usage: { input: 0, output: 0 } } },
    ];
    const events = parse(records).logEntries;
    expect(byType(events, "assistant.message")).toMatchObject([{ agentId: "child", timestamp: 3 }]);
    expect(byType(events, "tool.execution_complete")).toMatchObject([{ timestamp: 6 }]);
    expect(byType(events, "session.error")).toMatchObject([{ timestamp: 7 }]);
    expect(byType(events, "subagent.request")).toHaveLength(1);
    expect(selectSessionResult(events).usage).toMatchObject({ input_tokens: 0, output_tokens: 0 });
  });

  it("retains sampled child text and reasoning in their evidenced dispatch scope through unified projection", () => {
    const original = structuredClone(childObservations);
    const records = [...success, ...childObservations];
    const events = parse(records).logEntries;
    const scope = "legacy:pi:sanitized-reader:1";
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "pi-streaming.jsonl", events }]);
    expect(byType(unified, "assistant.reasoning")).toMatchObject([{ agentId: scope, data: { content: "Sanitized reasoning.", agentId: scope } }]);
    expect(byType(unified, "assistant.message").find(event => event.agentId === scope)).toMatchObject({
      data: { content: "Sanitized child answer.", model: "gpt-5-mini", messageId: "sanitized-child-response", agentId: scope },
    });
    expect(byType(unified, "subagent.request")).toMatchObject([{ agentId: scope, data: { inputTokens: 3085, outputTokens: 136, cacheReadTokens: 0, cacheWriteTokens: 0 } }]);
    expect(selectSessionResult(events)).toMatchObject({ numTurns: 2, usage: { input_tokens: 6427, output_tokens: 145 } });
    expect(byType(unified, "subagent.completed")).toEqual([]);
    expect(byType(unified, "subagent.failed")).toEqual([]);
    const validate = createSessionValidator("unified").event;
    for (const event of unified) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    expect(childObservations).toEqual(original);
  });

  it("reconciles synthetic child streams without conflating parent tool IDs or repeated snapshots", () => {
    const message = { role: "assistant", id: "child-answer", model: "fixture-model", content: [{ type: "text", text: "Child answer" }] };
    const wrap = event => ({ type: "gh_aw_subagent_event", agent: "reader", invocation_id: "child", event, provenance: { path: "pi-streaming.jsonl", index: 7 } });
    const records = [
      { type: "gh_aw_subagent_dispatch", agent: "reader", invocation_id: "child" },
      wrap({ type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "Child " } }),
      { type: "tool_execution_start", toolCallId: "same-id", toolName: "parent-tool", args: {} },
      wrap({ type: "tool_execution_start", toolCallId: "same-id", toolName: "child-tool", args: { child: true } }),
      wrap({ type: "tool_execution_end", toolCallId: "same-id", result: { error: "Child failure" } }),
      wrap({ type: "message_end", message }),
      wrap({ type: "turn_end", message }),
      wrap({ type: "agent_end", messages: [message] }),
      wrap({ type: "error", message: "Child error" }),
    ];
    const events = transformPiV3Entries(records);
    expect(byType(events, "tool.execution_start").map(event => [event.agentId, event.data.toolName])).toEqual([
      [undefined, "parent-tool"],
      ["child", "child-tool"],
    ]);
    expect(byType(events, "tool.execution_complete")).toMatchObject([{ agentId: "child", data: { success: false, error: "Child failure" } }]);
    expect(byType(events, "assistant.message").map(event => event.data.content)).toEqual(["Child ", "answer"]);
    expect(byType(events, "assistant.message").every(event => event.agentId === "child" && event.provenance.path === "pi-streaming.jsonl" && event.provenance.index === 7)).toBe(true);
    expect(byType(events, "session.error")).toMatchObject([{ agentId: "child", data: { error: "Child error" } }]);
    expect(byType(events, "subagent.request")).toHaveLength(1);
    expect(transformPiV3Entries(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it("preserves native unknown observations and deduplicated system snapshots without new custom types", () => {
    const message = { role: "system", content: "SANITIZED SYSTEM PROMPT", sections: ["sanitized"], timestamp: 1, toolsAdded: [] };
    const unknown = { type: "future_native_event", payload: { preserved: false }, timestamp: 2 };
    const update = { type: "future_update", payload: { preserved: 0 }, contentIndex: 1 };
    const records = [{ type: "message_start", message }, { type: "message_end", message }, unknown, { type: "message_update", assistantMessageEvent: update }, { type: "agent_end", messages: [message] }];
    const events = parse(records).logEntries;
    const compact = events.map(event => normalizeUnifiedSessionEvent(event));
    expect(byType(compact, "session.info").map(event => event.data)).toEqual([
      { sourceType: "message_snapshot", role: "system", content: message },
      { sourceType: "future_native_event", content: unknown },
    ]);
    expect(byType(compact, "assistant.message_update")).toMatchObject([{ data: { type: "future_update", content: update } }]);
    expect(parse(records).markdown).not.toContain("SANITIZED SYSTEM PROMPT");
    expect(byType(events, "session.result")).toEqual([]);
  });

  // Official v1.3.0 agent/src/types.ts and coding-agent/src/core/agent-session.ts;
  // synthetic observations, not a workflow run using the upgraded CLI.
  it.each([true, false])("retains Pi 1.3.0 cancellation=%s and measured tool duration without fabricating a terminal result", aborted => {
    const records = [
      { type: "session", version: 3, id: "release-contract" },
      { type: "tool_execution_start", toolCallId: "measured", toolName: "bash", args: { command: "sanitized" } },
      { type: "tool_execution_end", toolCallId: "measured", toolName: "bash", result: { content: [] }, isError: false, durationMs: 0 },
      { type: "agent_settled", aborted },
    ];
    const compact = parse(records).logEntries.map(event => normalizeUnifiedSessionEvent(event));
    expect(byType(compact, "tool.execution_complete")).toMatchObject([{ data: { durationMs: 0, success: true } }]);
    expect(byType(compact, "session.idle")).toEqual([{ type: "session.idle", data: { aborted } }]);
    expect(byType(compact, "session.result")).toEqual([]);
  });

  // Synthetic persisted entries following v1.3.0 core/session-manager.ts,
  // not a downloaded persisted session (the sampled artifacts contain streams).
  it("maps persisted native message envelopes through the same canonical content and tool mapping", () => {
    const records = [
      { type: "session", version: 3, id: "persisted", cwd: "/sanitized" },
      { type: "message", id: "user-entry", parentId: null, timestamp: 0, message: { role: "user", content: "Sanitized prompt" } },
      {
        type: "message",
        id: "call-entry",
        parentId: "user-entry",
        timestamp: 1,
        message: { role: "assistant", id: "call-message", content: [{ type: "toolCall", id: "tool", name: "bash", arguments: false }], usage: { input: 3, output: 1 } },
      },
      { type: "message", id: "result-entry", parentId: "call-entry", timestamp: 2, message: { role: "toolResult", toolCallId: "tool", content: 0, isError: false } },
      { type: "message", id: "answer-entry", parentId: "result-entry", timestamp: 3, message: { role: "assistant", id: "answer-message", content: [{ type: "text", text: "Sanitized answer" }], usage: { input: 2, output: 1 } } },
      { type: "usage", id: "auxiliary", parentId: "answer-entry", timestamp: 4, kind: "cache_warm", model: "fixture", provider: "fixture", usage: { input: 1, output: 0 } },
    ];
    const events = parse(records).logEntries;
    expect(byType(events, "user.message")).toMatchObject([{ id: "user-entry", parentId: null, timestamp: 0, data: { content: "Sanitized prompt" } }]);
    expect(byType(events, "tool.execution_start")).toMatchObject([{ id: "call-entry", data: { toolCallId: "tool", toolName: "bash", input: false } }]);
    expect(byType(events, "tool.execution_complete")).toMatchObject([{ id: "result-entry", data: { output: 0, success: true } }]);
    expect(byType(events, "assistant.message")).toMatchObject([{ id: "answer-entry", data: { content: "Sanitized answer" } }]);
    expect(byType(events, "session.info")).toMatchObject([{ data: { sourceType: "usage", content: records.at(-1) } }]);
    expect(selectSessionResult(events)).toMatchObject({ numTurns: 2, usage: { input_tokens: 6, output_tokens: 2 } });
    expect(parse(records.slice(1, -1)).logEntries).toHaveLength(events.length - 2);
  });

  it.each([
    ["success", success],
    ["provider failure", failure],
  ])("projects the sampled %s stream into standard session events", (_, records) => {
    const events = parse(records).logEntries;
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "pi-streaming.jsonl", events }]);
    expect(unified.some(event => event.type.startsWith("pi."))).toBe(false);
    expect(byType(unified, "assistant.message").map(event => event.data.content)).toEqual(byType(events, "assistant.message").map(event => event.data.content));
    expect(byType(unified, "tool.execution_update").map(event => event.data.output)).toEqual(byType(events, "tool.execution_update").map(event => event.data.output));
    const validate = createSessionValidator("unified").event;
    for (const event of unified) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    expect(normalizeAgentSession(events)).toEqual(events);
  });

  it("preserves interrupted arguments, orphan deltas and partial tool results in the compact format", () => {
    const records = [
      { type: "message_update", id: "start", assistantMessageEvent: { type: "toolcall_start", contentIndex: 1, id: "call", toolName: "lookup" } },
      { type: "message_update", assistantMessageEvent: { type: "toolcall_delta", contentIndex: 1, delta: '{"query":' } },
      { type: "tool_execution_update", id: "progress", parentId: "start", timestamp: 0, toolCallId: "call", parentToolCallId: "parent", toolName: "lookup", args: false, partialResult: 0 },
      { type: "message_update", id: "orphan", assistantMessageEvent: { type: "toolcall_delta", contentIndex: 2, delta: "false" } },
    ];
    const events = transformPiV3Entries(records).map(event => normalizeUnifiedSessionEvent(event));
    expect(events).toEqual([
      { type: "tool.execution_start", id: "start", data: { toolCallId: "call", toolName: "lookup", input: '{"query":' } },
      { type: "tool.execution_update", id: "progress", parentId: "start", timestamp: 0, data: { toolCallId: "call", parentToolCallId: "parent", toolName: "lookup", input: false, output: 0, partial: true } },
      { type: "tool.execution_update", id: "orphan", data: { input: "false", delta: true, partial: true, contentIndex: 2 } },
    ]);
  });

  it.each(["done", "error"])("maps a %s update without duplicating later finalized message observations", type => {
    const message = { role: "assistant", content: [{ type: "text", text: "exact\n" }], usage: { input: 3, output: 1 }, ...(type === "error" ? { errorMessage: { code: "interrupted" } } : {}) };
    const records = [
      { type: "message_update", assistantMessageEvent: { type, [type === "error" ? "error" : "message"]: message } },
      { type: "message_end", message },
      { type: "turn_end", message },
      { type: "agent_end", messages: [message] },
    ];
    const events = parse(records).logEntries;
    expect(byType(events, "assistant.message").map(event => event.data.content)).toEqual(["exact\n"]);
    expect(byType(events, "session.error")).toHaveLength(type === "error" ? 1 : 0);
    expect(selectSessionResult(events)).toMatchObject({ numTurns: 1, usage: { input_tokens: 3, output_tokens: 1 } });
  });

  it("preserves child model evidence without counting child answers as parent messages or usage", () => {
    const parent = { type: "message_end", message: { role: "assistant", content: [{ type: "text", text: "Parent answer" }], usage: { input: 10, output: 3 } } };
    const child = {
      type: "gh_aw_subagent_event",
      invocation_id: "invocation-1",
      agent: "reader",
      event: { type: "message_end", message: { role: "assistant", model: "claude-haiku-4.5", content: [{ type: "text", text: "Child answer" }], usage: { input: 5, output: 2 } } },
    };
    const records = [
      { type: "gh_aw_subagent_dispatch", invocation_id: "invocation-1", agent: "reader", requested_model: "small", resolved_model: "claude-haiku-4.5" },
      child,
      { type: "gh_aw_subagent_result", invocation_id: "invocation-1", agent: "reader", outcome: "completed" },
      parent,
    ];
    const events = transformPiV3Entries(records);
    expect(byType(events, "subagent.started")[0]).toMatchObject({
      agentId: "invocation-1",
      data: { invocationId: "invocation-1", agentName: "reader", model: "small", resolvedModel: "claude-haiku-4.5" },
    });
    expect(byType(events, "subagent.configured")[0]).toMatchObject({
      agentId: "invocation-1",
      data: { invocationId: "invocation-1", model: "claude-haiku-4.5" },
    });
    expect(byType(events, "subagent.request")[0]).toMatchObject({
      agentId: "invocation-1",
      data: { invocationId: "invocation-1", agentName: "reader", model: "claude-haiku-4.5", inputTokens: 5, outputTokens: 2 },
    });
    expect(byType(events, "subagent.completed")[0].data).toMatchObject({ invocationId: "invocation-1", outcome: "completed" });
    expect(byType(events, "pi.subagent_dispatch")).toEqual([]);
    expect(byType(events, "pi.subagent_event")).toEqual([]);
    expect(byType(events, "pi.subagent_result")).toEqual([]);
    expect(
      byType(events, "assistant.message")
        .filter(e => !e.agentId)
        .map(e => e.data.content)
    ).toEqual(["Parent answer"]);
    expect(byType(events, "assistant.message").find(e => e.agentId === "invocation-1")).toMatchObject({ data: { content: "Child answer", agentId: "invocation-1" } });
    expect(computePiV3Stats(records).usage.input_tokens).toBe(10);
  });

  it("normalizes a failed child with its invocation and failure code", () => {
    const events = transformPiV3Entries([
      { type: "gh_aw_subagent_dispatch", invocation_id: "unavailable-1", agent: "reader", requested_model: "small", resolved_model: "gpt-5.6-luna" },
      { type: "gh_aw_subagent_result", invocation_id: "unavailable-1", agent: "reader", outcome: "failed", error_code: "SUBAGENT_MODEL_UNAVAILABLE", error: "No model endpoint is available" },
    ]);
    expect(byType(events, "subagent.failed")[0]).toMatchObject({
      agentId: "unavailable-1",
      data: {
        invocationId: "unavailable-1",
        outcome: "failed",
        errorCode: "SUBAGENT_MODEL_UNAVAILABLE",
        error: "No model endpoint is available",
      },
    });
  });

  it("derives legacy invocation identities by agent and dispatch order", () => {
    const events = transformPiV3Entries([
      { type: "gh_aw_subagent_dispatch", agent: "reader", requested_model: "small" },
      { type: "gh_aw_subagent_event", agent: "reader", event: { message: { role: "assistant", model: "gpt-5.4-mini", usage: { input: 5, output: 2, cacheRead: 0, cacheWrite: 0 } } } },
      { type: "gh_aw_subagent_result", agent: "reader", outcome: "completed" },
    ]);
    expect(byType(events, "subagent.started")[0].agentId).toBe("legacy:pi:reader:1");
    expect(byType(events, "subagent.request")[0].agentId).toBe("legacy:pi:reader:1");
    expect(byType(events, "subagent.completed")[0].agentId).toBe("legacy:pi:reader:1");
  });
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
    expect(byType(logEntries, "tool.execution_update")[0].data).toMatchObject({ output: success[14].partialResult, partial: true });
    expect(logEntries.some(event => event.type.startsWith("pi."))).toBe(false);
    expect(byType(logEntries, "assistant.message")[0].data).toMatchObject({ model: "gpt-5.4", messageId: "sanitized-response-2", messageMetadata: { stopReason: "stop" } });
    expect(byType(logEntries, "tool.execution_start")[0].data).toMatchObject({ model: "gpt-5.4", messageId: "sanitized-response-1", messageMetadata: { stopReason: "toolUse" } });
    expect(selectSessionResult(logEntries)).toMatchObject({ sourceEngine: "pi", numTurns: 2, totalCostUsd: 0, usage: { input_tokens: 6427, output_tokens: 145, cache_read_input_tokens: 1540, cache_creation_input_tokens: 1 } });
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
    expect(byType(logEntries, "session.error")).toHaveLength(1);
    expect(byType(logEntries, "session.error")[0].data.error).toContain("model_not_supported");
    expect(byType(logEntries, "tool.execution_complete")).toEqual([]);
  });

  it("coalesces adjacent deltas without moving later text before interleaved tools", () => {
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
    expect(events.map(e => e.type)).toEqual(["assistant.message", "tool.execution_start", "tool.execution_complete", "assistant.message"]);
    expect(byType(events, "assistant.message").map(e => e.data.content)).toEqual([" before", " after!\n"]);
    expect(
      byType(events, "assistant.message")
        .map(e => e.data.content)
        .join("")
    ).toBe(message.content[0].text);
    expect(events[0].id).toBe("delta-1");
    expect(events[1].data.input).toBe(false);
    expect(events[2].data).toMatchObject({ output: false, durationMs: 0, success: true });
  });

  it("places a recovered snapshot suffix after the interruption without repeating the complete answer", () => {
    const message = { role: "assistant", id: "answer", content: [{ type: "text", text: " before\n\nafter\n" }] };
    const events = transformPiV3Entries([
      { type: "message_update", id: "before", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: " before\n" } },
      { type: "tool_execution_start", toolCallId: "tool", toolName: "bash", args: {} },
      { type: "tool_execution_end", toolCallId: "tool", result: {}, isError: false },
      { type: "message_end", id: "after", message },
      { type: "turn_end", message },
      { type: "agent_end", messages: [message] },
    ]);
    const ordered = events;
    expect(ordered.map(e => e.type)).toEqual(["assistant.message", "tool.execution_start", "tool.execution_complete", "assistant.message"]);
    expect(ordered[0]).toMatchObject({ id: "before", data: { content: " before\n" } });
    expect(ordered[3]).toMatchObject({ id: "after", data: { content: "\nafter\n" } });
    expect(
      byType(events, "assistant.message")
        .map(e => e.data.content)
        .join("")
    ).toBe(message.content[0].text);
  });

  it("reconciles corrected complete content across interrupted segments without relocating events", () => {
    const message = { role: "assistant", id: "corrected", content: [{ type: "text", text: "right\n", signature: "complete" }] };
    const events = transformPiV3Entries([
      { type: "message_update", id: "first", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "wrong" } },
      { type: "vendor.progress", data: { count: 1 } },
      { type: "message_update", id: "second", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: " text\n" } },
      { type: "message_end", message },
      { type: "agent_end", messages: [message] },
    ]);
    expect(events.map(e => e.type)).toEqual(["assistant.message", "vendor.progress", "assistant.message"]);
    const text = byType(events, "assistant.message");
    expect(text.map(e => e.id)).toEqual(["first", "second"]);
    expect(text.map(e => e.data.content).join("")).toBe("right\n");
    expect(text.every(e => e.data.signature === "complete")).toBe(true);
  });

  it("renders the real Pi final-answer lifecycle once with continuous Markdown and exact whitespace", () => {
    const text = " The smoke tests have been executed successfully.\n\n### Results\n- **Overall Status**: PASS\n\n[Run](https://github.com/github/gh-aw/actions/runs/37248388015)\n ";
    const message = { role: "assistant", responseId: "answer", timestamp: 10, content: [{ type: "text", text, signature: "final-metadata" }], usage: { input: 7, output: 3 } };
    const records = [
      { type: "turn_start" },
      { type: "message_start", message: { ...message, responseId: undefined, content: [] } },
      ...Array.from(text, (delta, index) => ({ type: "message_update", id: `delta-${index}`, timestamp: index + 10, assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta } })),
      { type: "message_update", assistantMessageEvent: { type: "text_end", contentIndex: 0, content: text } },
      { type: "message_end", message },
      { type: "turn_end", message },
      { type: "agent_end", messages: [message] },
    ];
    const original = structuredClone(records);
    const { logEntries, markdown } = parse(records);
    expect(byType(logEntries, "assistant.message")).toHaveLength(1);
    expect(byType(logEntries, "assistant.message")[0]).toMatchObject({ id: "delta-0", timestamp: 10, data: { content: text, signature: "final-metadata" } });
    expect(markdown).toContain(text.trim());
    expect(markdown.split("The smoke tests have been executed successfully.")).toHaveLength(2);
    expect(selectSessionResult(logEntries)).toMatchObject({ numTurns: 1, usage: { input_tokens: 7, output_tokens: 3 } });
    expect(records).toEqual(original);
  });

  it("coalesces interrupted text and reasoning separately without moving their first observations", () => {
    const records = [
      { type: "message_update", id: "reasoning", timestamp: 1, assistantMessageEvent: { type: "thinking_delta", contentIndex: 0, delta: " why\n" } },
      { type: "vendor.progress", id: "progress", data: { count: 1 } },
      { type: "message_update", assistantMessageEvent: { type: "thinking_delta", contentIndex: 0, delta: " now " } },
      { type: "message_update", id: "answer", timestamp: 2, assistantMessageEvent: { type: "text_delta", contentIndex: 1, delta: " first\n" } },
      { type: "tool_execution_start", toolCallId: "tool", toolName: "bash", args: { command: "echo ok" } },
      { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 1, delta: "\nsecond " } },
      { type: "error", error: "interrupted" },
    ];
    const events = transformPiV3Entries(records);
    expect(events.map(e => e.type)).toEqual(["assistant.reasoning", "vendor.progress", "assistant.reasoning", "assistant.message", "tool.execution_start", "assistant.message", "session.error"]);
    expect(events[0]).toMatchObject({ id: "reasoning", timestamp: 1, data: { content: " why\n" } });
    expect(events[3]).toMatchObject({ id: "answer", timestamp: 2, data: { content: " first\n" } });
    expect(
      byType(events, "assistant.reasoning")
        .map(e => e.data.content)
        .join("")
    ).toBe(" why\n now ");
    expect(
      byType(events, "assistant.message")
        .map(e => e.data.content)
        .join("")
    ).toBe(" first\n\nsecond ");
  });

  it("keeps equal content in distinct messages and interrupted turns separate", () => {
    const message = id => ({ role: "assistant", id, content: [{ type: "text", text: "same\n" }] });
    const events = transformPiV3Entries([
      { type: "message_end", message: message("first") },
      { type: "message_end", message: message("second") },
      { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "partial\n" } },
      { type: "turn_start" },
      { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "new\n" } },
      { type: "message_start", message: { role: "assistant", id: "last", content: [] } },
      { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "last\n" } },
    ]);
    expect(byType(events, "assistant.message").map(e => e.data.content)).toEqual(["same\n", "same\n", "partial\n", "new\n", "last\n"]);
  });

  it("respects anonymous message boundaries while suppressing repeated transcript snapshots", () => {
    const message = { role: "assistant", content: [{ type: "text", text: "same\n" }] };
    const lifecycle = [
      { type: "message_start", message: { ...message, content: [] } },
      { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "same\n" } },
      { type: "message_end", message },
    ];
    const events = transformPiV3Entries([...lifecycle, ...lifecycle, { type: "agent_end", messages: [message, message] }]);
    expect(byType(events, "assistant.message").map(e => e.data.content)).toEqual(["same\n", "same\n"]);
    expect(
      byType(
        transformPiV3Entries([
          { type: "message_end", message: { ...message, content: [{ type: "text", text: "first\n" }] } },
          { type: "message_end", message: { ...message, content: [{ type: "text", text: "second\n" }] } },
        ]),
        "assistant.message"
      ).map(e => e.data.content)
    ).toEqual(["first\n", "second\n"]);
  });

  it("recovers authoritative snapshots rather than keeping incorrect partial text and reasoning", () => {
    const message = {
      role: "assistant",
      id: "corrected",
      content: [
        { type: "thinking", thinking: "complete reasoning\n", signature: "reasoning-metadata" },
        { type: "text", text: "complete answer\n", signature: "answer-metadata" },
      ],
    };
    const events = transformPiV3Entries([
      { type: "message_update", id: "reasoning", assistantMessageEvent: { type: "thinking_delta", contentIndex: 0, delta: "incorrect reasoning" } },
      { type: "message_update", id: "answer", assistantMessageEvent: { type: "text_delta", contentIndex: 1, delta: "incorrect answer" } },
      { type: "message_end", message },
      { type: "turn_end", message: { ...message, usage: { input: 2 } } },
      { type: "agent_end", messages: [message] },
    ]);
    expect(byType(events, "assistant.reasoning")).toHaveLength(1);
    expect(byType(events, "assistant.reasoning")[0]).toMatchObject({ id: "reasoning", data: { content: "complete reasoning\n", signature: "reasoning-metadata" } });
    expect(byType(events, "assistant.message")).toHaveLength(1);
    expect(byType(events, "assistant.message")[0]).toMatchObject({ id: "answer", data: { content: "complete answer\n", signature: "answer-metadata" } });
  });

  const snapshotParts = [
    { type: "image", data: "image-data", mimeType: "image/png" },
    { type: "text", text: "answer" },
    { type: "thinking", thinking: "reasoning" },
    { type: "toolCall", id: "call", name: "lookup", arguments: { query: "answer" } },
  ];

  it.each(snapshotParts.flatMap(previous => snapshotParts.filter(part => part.type !== previous.type).map(part => [previous.type, part.type, previous, part])))(
    "reconciles index zero from %s to %s across repeated finalized snapshots",
    (_, __, previous, part) => {
      const message = { role: "assistant", id: "same-message", content: [part] };
      const records = [
        { type: "message_start", message: { ...message, content: [previous] } },
        { type: "message_end", message },
        { type: "turn_end", message },
        { type: "agent_end", messages: [message] },
      ];
      const original = structuredClone(records);
      const events = transformPiV3Entries(records);
      expect(events).toHaveLength(1);
      expect(events[0].type).toBe(part.type === "thinking" ? "assistant.reasoning" : part.type === "toolCall" ? "tool.execution_start" : "assistant.message");
      expect(events[0].data.nativeSnapshots).toEqual([{ content: previous.type === "text" ? previous.text : previous.type === "thinking" ? previous.thinking : previous.type === "toolCall" ? previous.arguments : previous }]);
      if (part.type === "toolCall") expect(events[0].data.input).toEqual(part.arguments);
      else expect(events[0].data.content).toEqual(part.type === "text" ? part.text : part.type === "thinking" ? part.thinking : part);
      expect(byType(events, "tool.execution_complete")).toHaveLength(0);
      const saved = JSON.parse(JSON.stringify(events));
      expect(transformPiV3Entries(saved)).toEqual(saved);
      expect(normalizeAgentSession(saved)).toEqual(saved);
      expect(records).toEqual(original);
    }
  );

  it.each(snapshotParts.filter(part => part.type !== "toolCall"))("retains actual tool execution when a later $type snapshot replaces its content index", part => {
    const call = snapshotParts[3];
    const message = { role: "assistant", id: "same-message", content: [part] };
    const events = transformPiV3Entries([
      { type: "message_start", message: { ...message, content: [call] } },
      { type: "tool_execution_start", toolCallId: "call", toolName: "lookup", args: { query: "executed" } },
      { type: "message_end", message },
      { type: "tool_execution_end", toolCallId: "call", toolName: "lookup", result: "found", isError: false },
      { type: "turn_end", message },
      { type: "agent_end", messages: [message] },
    ]);
    expect(events.map(event => event.type)).toEqual(["tool.execution_start", part.type === "thinking" ? "assistant.reasoning" : "assistant.message", "tool.execution_complete"]);
    expect(events[0].data).toMatchObject({ toolCallId: "call", input: { query: "executed" } });
    expect(events[1].data).toMatchObject({ content: part.type === "text" ? part.text : part.type === "thinking" ? part.thinking : part, nativeSnapshots: [{ content: { query: "executed" } }] });
    expect(events[2].data).toMatchObject({ toolCallId: "call", output: "found", success: true });
    const saved = JSON.parse(JSON.stringify(events));
    expect(transformPiV3Entries(saved)).toEqual(saved);
    expect(normalizeAgentSession(saved)).toEqual(saved);
  });

  it("preserves chained native snapshot history without manufacturing a start for an orphan completion", () => {
    const parts = snapshotParts.slice(0, 3);
    const message = { role: "assistant", id: "same-message", content: [snapshotParts[0]] };
    const events = transformPiV3Entries([
      { type: "message_start", message: { ...message, content: [parts[0]] } },
      { type: "message_end", message: { ...message, content: [parts[1]] } },
      { type: "turn_end", message: { ...message, content: [parts[2]] } },
      { type: "tool_execution_end", toolCallId: "orphan", result: "found", isError: false },
      { type: "agent_end", messages: [message] },
      { type: "agent_end", messages: [message] },
    ]);
    expect(events.map(event => event.type)).toEqual(["tool.execution_complete", "assistant.message"]);
    expect(events[1].data).toMatchObject({
      content: parts[0],
      nativeSnapshots: [{ content: parts[0] }, { content: parts[1].text }, { content: parts[2].thinking }],
    });
    const saved = JSON.parse(JSON.stringify(events));
    expect(transformPiV3Entries(saved)).toEqual(saved);
    expect(normalizeAgentSession(saved)).toEqual(saved);
  });

  it("does not merge content block indices or text and reasoning at the same index", () => {
    const events = transformPiV3Entries([
      { type: "message_update", assistantMessageEvent: { type: "thinking_delta", contentIndex: 0, delta: "reason" } },
      { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 0, delta: "first" } },
      { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 2, delta: "second" } },
      { type: "message_update", assistantMessageEvent: { type: "thinking_delta", contentIndex: 0, delta: "ing\n" } },
      { type: "message_update", assistantMessageEvent: { type: "text_delta", contentIndex: 2, delta: "\n" } },
    ]);
    expect(byType(events, "assistant.reasoning").map(e => e.data.content)).toEqual(["reason", "ing\n"]);
    expect(byType(events, "assistant.message").map(e => e.data.content)).toEqual(["first", "second", "\n"]);
    expect(events.map(e => e.type)).toEqual(["assistant.reasoning", "assistant.message", "assistant.message", "assistant.reasoning", "assistant.message"]);
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
    expect(byType(result.logEntries, "tool.execution_start")[0].data.argumentText).toBe('{"command":');
    expect(result.logEntries.some(event => event.type.startsWith("pi."))).toBe(false);
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
    expect(events.map(event => event.type)).toEqual(["tool.execution_start", "vendor.interleaved"]);
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
    expect(parsePiLog('{"type":"unknown","value":1}').logEntries).toMatchObject([{ type: "session.info", data: { sourceType: "unknown", content: { type: "unknown", value: 1 } } }]);
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
    expect(byType(events, "assistant.message").map(e => e.data.content)).toEqual(["partial complete"]);
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
    expect(byType(events, "session.error")).toHaveLength(2);
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
    expect(events.map(event => event.type)).toContain("session.idle");
    expect(events.map(event => event.type)).toContain("session.retry_start");
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
