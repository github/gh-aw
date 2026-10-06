import { describe, expect, it } from "vitest";
const { getMessageRefusal, getProviderRefusals } = require("./provider_refusal.cjs");
const { normalizeAgentSession, projectSessionResult } = require("./agent_session.cjs");
const { normalizeClaudeSession } = require("./claude_session.cjs");
const { parseCopilotLog } = require("./parse_copilot_log.cjs");
const { parseClaudeLog } = require("./parse_claude_log.cjs");
const { parseEngineSession, mergeSessionSources } = require("./unified_session.cjs");
const { generatePlainTextSummary, generateCopilotCliStyleSummary } = require("./log_parser_shared.cjs");
const { normalizeUnifiedSessionEvent } = require("./unified_session_payload.cjs");
const { contentFiltered, openaiRefusal, responsesRefusal, anthropicRefusal } = require("./fixtures/provider_refusals.cjs");

const jsonl = records => records.map(record => JSON.stringify(record)).join("\n");
const refusals = events => events.filter(event => event.type === "assistant.refusal");
const debugBlock = value => {
  const prefix = "2026-10-06T01:12:45.797Z [DEBUG] [rust:model_wire] ";
  return `${prefix}response (Request-ID request-filtered):\n${prefix}data:\n${prefix}${JSON.stringify(value, null, 2)}\n`;
};

describe("Structured policy refusals", () => {
  it("recovers the real CI content_filter signal despite null content and refusal fields", () => {
    const parsed = parseCopilotLog(debugBlock(contentFiltered));
    expect(refusals(parsed.logEntries)).toEqual([
      expect.objectContaining({
        type: "assistant.refusal",
        id: "response-filtered",
        requestId: "request-filtered",
        timestamp: "2026-10-06T01:12:45.797Z",
        model: "claude-sonnet-5",
        data: { reason: "content_filter" },
      }),
    ]);
    expect(parsed.logEntries.filter(event => event.type === "assistant.message")).toEqual([]);
    expect(projectSessionResult(parsed.logEntries).usage).toMatchObject({ input_tokens: 19929, output_tokens: 9, cache_read_input_tokens: 19251 });
    expect(parsed.markdown).toContain("Policy refusal: content_filter");
    expect(parsed.markdown).not.toContain("PRIVATE_USER_PROMPT");
  });

  it.each([contentFiltered, openaiRefusal, responsesRefusal])("accepts raw refusal envelopes in JSON arrays, JSONL, and debug blocks: $object", record => {
    for (const input of [JSON.stringify([record]), jsonl([record]), debugBlock(record)]) {
      const events = parseCopilotLog(input).logEntries;
      expect(refusals(events)).toHaveLength(1);
      expect(projectSessionResult(events).usage.input_tokens).toBe(record.usage.prompt_tokens ?? record.usage.input_tokens);
      expect(events.filter(event => event.type === "assistant.message")).toHaveLength(0);
      expect(normalizeAgentSession(events)).toEqual(events);
    }
  });

  it("preserves exact OpenAI refusal text, empty strings, and filtered partial output", () => {
    expect(getProviderRefusals(openaiRefusal)).toEqual([{ reason: "refusal", content: "  Cannot provide that answer.\r\n" }]);
    expect(getMessageRefusal({ role: "assistant", refusal: "", content: null })).toEqual({ reason: "refusal", content: "" });
    expect(getMessageRefusal({ role: "assistant", content: "  partial\n" }, "content_filter")).toEqual({ reason: "content_filter", content: "  partial\n" });
    expect(getProviderRefusals({ ...contentFiltered, choices: [{ finish_reason: "content_filter" }] })).toEqual([{ reason: "content_filter" }]);
    expect(getMessageRefusal({ content: [{ type: "refusal", refusal: "exact\n" }] })).toEqual({ reason: "refusal", content: "exact\n" });
  });

  it.each([
    ["response.refusal.delta", { delta: " part " }, { reason: "refusal", content: " part ", partial: true }],
    ["response.refusal.done", { refusal: " full\n" }, { reason: "refusal", content: " full\n" }],
    ["response.content_part.done", { part: { type: "refusal", refusal: "" } }, { reason: "refusal", content: "" }],
    ["response.content_part.added", { part: { type: "refusal", refusal: "" } }, { reason: "refusal", content: "", partial: true }],
    ["response.output_item.done", { item: responsesRefusal.output[0] }, { reason: "refusal", content: "  Cannot provide that answer.\r\n" }],
    ["response.completed", { response: responsesRefusal }, { reason: "refusal", content: "  Cannot provide that answer.\r\n" }],
    ["response.incomplete", { response: { object: "response", incomplete_details: { reason: "content_filter" }, output: [] } }, { reason: "content_filter" }],
  ])("maps the documented OpenAI %s observation", (type, fields, expected) => {
    const record = { type, ...fields, item_id: "item", output_index: 0, content_index: 0, sequence_number: 4 };
    const events = parseCopilotLog(jsonl([record])).logEntries;
    expect(refusals(events)).toMatchObject([{ type: "assistant.refusal", sequence_number: 4, item_id: "item", data: expected }]);
    expect(normalizeAgentSession(events)).toEqual(events);
  });

  it("distinguishes Chat streaming fragments and Responses filtering from token truncation", () => {
    expect(getProviderRefusals({ object: "chat.completion.chunk", choices: [{ delta: { refusal: " part " }, finish_reason: null }] })).toEqual([{ reason: "refusal", content: " part ", partial: true }]);
    expect(getProviderRefusals({ object: "chat.completion.chunk", choices: [{ delta: {}, finish_reason: "content_filter" }] })).toEqual([{ reason: "content_filter" }]);
    const response = {
      object: "response",
      status: "incomplete",
      incomplete_details: { reason: "content_filter" },
      output: [{ type: "message", role: "assistant", content: [{ type: "output_text", text: " partial\n" }] }],
    };
    expect(getProviderRefusals(response)).toEqual([{ reason: "content_filter", content: " partial\n" }]);
    expect(getProviderRefusals({ ...response, incomplete_details: { reason: "max_output_tokens" } })).toEqual([]);
  });

  it("maps Anthropic Messages API and Claude SDK refusals without inventing empty content", () => {
    for (const record of [anthropicRefusal, { type: "assistant", uuid: "sdk-refusal", session_id: "session", message: anthropicRefusal }]) {
      const parsed = parseClaudeLog(jsonl([record]));
      expect(refusals(parsed.logEntries)).toMatchObject([{ data: { reason: "refusal", policyCategory: "general_harms", explanation: "  This request was declined.\r\n" } }]);
      expect(refusals(parsed.logEntries)[0].data.content).toBeUndefined();
      expect(parsed.logEntries.filter(event => event.type === "assistant.message")).toEqual([]);
      expect(parsed.markdown).toContain("Policy refusal: refusal");
      expect(normalizeClaudeSession(parsed.logEntries)).toEqual(parsed.logEntries);
    }
    const api = normalizeClaudeSession([anthropicRefusal]);
    expect(projectSessionResult(api).usage).toMatchObject({ input_tokens: 10, output_tokens: 0, input_tokens_include_cache: false });
    expect(getMessageRefusal({ ...anthropicRefusal, stop_details: { category: null, explanation: null } })).toEqual({ reason: "refusal", policyCategory: null, explanation: null });
  });

  it("recovers a streamed Claude refusal once even with a subsequent assistant snapshot", () => {
    const stream = event => ({ type: "stream_event", session_id: "session", parent_tool_use_id: null, event });
    const records = [
      stream({ type: "message_start", message: { id: "message-refused", usage: { input_tokens: 10 } } }),
      stream({ type: "content_block_start", index: 0, content_block: { type: "text", text: "" } }),
      stream({ type: "content_block_delta", index: 0, delta: { type: "text_delta", text: " partial\n" } }),
      stream({ type: "message_delta", delta: { stop_reason: "refusal", stop_details: { category: null, explanation: null } }, usage: { output_tokens: 2 } }),
      { type: "assistant", session_id: "session", parent_tool_use_id: null, message: { ...anthropicRefusal, content: [{ type: "text", text: " partial\n" }] } },
    ];
    const original = structuredClone(records);
    const events = normalizeClaudeSession(records);
    expect(refusals(events)).toMatchObject([{ data: { reason: "refusal", content: " partial\n", policyCategory: "general_harms" } }]);
    expect(events.filter(event => event.type === "claude.assistant_snapshot")).toHaveLength(1);
    expect(projectSessionResult(events).usage.output_tokens).toBe(0);
    expect(records).toEqual(original);
    expect(normalizeClaudeSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it("accepts raw Anthropic Messages streaming records as well as SDK envelopes", () => {
    const records = [
      { type: "message_start", message: { id: "raw-stream", role: "assistant", content: [], usage: { input_tokens: 10 } } },
      { type: "content_block_start", index: 0, content_block: { type: "text", text: "" } },
      { type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "  partial\r\n" } },
      { type: "content_block_stop", index: 0 },
      { type: "message_delta", delta: { stop_reason: "refusal", stop_details: { category: null, explanation: null } }, usage: { output_tokens: 2 } },
      { type: "message_stop" },
    ];
    const parsed = parseClaudeLog(jsonl(records));
    expect(refusals(parsed.logEntries)).toMatchObject([{ data: { reason: "refusal", content: "  partial\r\n", policyCategory: null, explanation: null } }]);
    expect(projectSessionResult(parsed.logEntries).usage).toMatchObject({ input_tokens: 10, output_tokens: 2, input_tokens_include_cache: false });
    expect(parsed.markdown).toContain("Policy refusal: refusal");
  });

  it("preserves separately exposed Copilot reasoning on a refused assistant response", () => {
    const record = { type: "assistant.message", id: "native-refusal", data: { refusal: "Cannot provide that answer.", reasoningText: "  exposed reasoning\n" } };
    const events = parseCopilotLog(jsonl([record])).logEntries;
    expect(refusals(events)).toMatchObject([{ id: "native-refusal", data: { reason: "refusal", content: "Cannot provide that answer." } }]);
    expect(events.find(event => event.type === "assistant.reasoning").data.content).toBe("  exposed reasoning\n");
    expect(parseCopilotLog(JSON.stringify(events)).logEntries).toEqual(events);
  });

  it.each([
    { role: "assistant", refusal: null, content: "I cannot help with that request." },
    { role: "assistant", refusal: false, finish_reason: "stop", content: "Quoted refusal." },
    { role: "assistant", stop_reason: "max_tokens", content: "Refusal policy documentation." },
    { role: "user", refusal: "A refusal example", stop_reason: "refusal" },
    { role: "system", finish_reason: "content_filter" },
  ])("does not guess refusals from prose or non-assistant input: %j", message => {
    expect(getMessageRefusal(message)).toBeUndefined();
  });

  it("does not promote tool/user refusal examples or authentication failures to policy refusals", () => {
    const records = [
      { type: "user.message", data: { content: "PRIVATE_USER_PROMPT", stop_reason: "refusal", refusal: "quoted" } },
      { type: "tool.execution_complete", data: { success: false, error: { code: "PERMISSION_DENIED" }, output: anthropicRefusal } },
      { type: "session.error", data: { statusCode: 403, message: "Authentication failed with provider." } },
      { type: "session.result", data: { errors: ["I cannot help with that request."] } },
      { type: "assistant.reasoning", data: { content: "Discussing refusal signals.", stop_reason: "refusal" } },
    ];
    expect(refusals(normalizeAgentSession(records))).toEqual([]);
    expect(getProviderRefusals({ messages: [anthropicRefusal] })).toEqual([]);
  });

  it.each([null, [], { nativeValue: false }])("preserves supplied native structured content %j on a filtered canonical message", content => {
    const record = { type: "assistant.message", id: "filtered-native", data: { content, finish_reason: "content_filter" } };
    const events = normalizeAgentSession([record]);
    expect(events).toEqual([{ ...record, type: "assistant.refusal", data: { ...record.data, reason: "content_filter" } }]);
    expect(normalizeAgentSession(events)).toEqual(events);
    expect(normalizeUnifiedSessionEvent(events[0]).data).toEqual({ reason: "content_filter", content });
  });

  it.each(["copilot", "claude", "codex", "gemini", "pi", "custom", "opencode", "goose"])("retains canonical refusal events through the %s parser", engine => {
    const event = { type: "assistant.refusal", id: "native", parentId: null, timestamp: 0, data: { reason: "refusal", content: " text\n", partial: false, nativeExtra: true } };
    const events = parseEngineSession(jsonl([event]), engine);
    expect(refusals(events)).toEqual([event]);
    expect(normalizeAgentSession(JSON.parse(JSON.stringify(events)))).toEqual(events);
  });

  it("compacts refusal payloads, keeps provenance, and labels all publication views", () => {
    const event = { type: "assistant.refusal", id: "refusal", timestamp: 0, data: { reason: "refusal", content: " text\n", policyCategory: null, explanation: null, partial: false, envelope: "PRIVATE_USER_PROMPT" } };
    expect(normalizeUnifiedSessionEvent(event).data).toEqual({ reason: "refusal", content: " text\n", policyCategory: null, explanation: null, partial: false });
    const merged = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events: [event] }]);
    expect(merged[0].provenance).toMatchObject({ path: "agent-session.jsonl", index: 0, timestampMs: 0 });
    for (const render of [generatePlainTextSummary, generateCopilotCliStyleSummary]) {
      const summary = render([event]);
      expect(summary).toContain("Policy refusal: refusal");
      expect(summary).toContain("text");
      expect(summary).not.toContain("PRIVATE_USER_PROMPT");
    }
  });
});
