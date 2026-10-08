import { describe, expect, it } from "vitest";
import { normalizeCopilotSession } from "./copilot_session.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { mergeSessionSources } from "./unified_session.cjs";
import { collapseStreamedMessages } from "./agent_session_render.cjs";
import { projectSessionResult, sessionTokenTotal } from "./agent_session.cjs";
import { parseCopilotLog } from "./parse_copilot_log.cjs";
import { generatePlainTextSummary, generateCopilotCliStyleSummary } from "./log_parser_shared.cjs";
import { createSessionValidator } from "./scripts/validate_session.cjs";
import { copilotCiMessages } from "./fixtures/copilot_ci_messages.cjs";

const byType = (events, type) => events.filter(event => event.type === type);
const compact = events => mergeSessionSources([{ component: "agent", phase: "agent", path: "events.jsonl", events }]);
const stable = events => {
  const serialized = JSON.parse(JSON.stringify(events));
  expect(normalizeCopilotSession(serialized)).toEqual(serialized);
};

describe("Copilot message normalization from sampled CI shapes", () => {
  it("normalizes native messages and lifecycle without duplicating observed tools or dropping correlation", () => {
    const original = structuredClone(copilotCiMessages);
    const canonical = parseCopilotLog(JSON.stringify(copilotCiMessages)).logEntries;
    expect(copilotCiMessages).toEqual(original);
    stable(canonical);
    const events = compact(canonical);
    expect(byType(events, "tool.execution_start")).toHaveLength(2);
    expect(byType(events, "tool.execution_complete")).toHaveLength(2);
    expect(byType(events, "assistant.message").map(event => event.data.content)).toEqual(["  Checking the repository.\n", "  Completed the task.\n"]);
    expect(byType(events, "assistant.reasoning")[0].data).toMatchObject({ content: "  Inspect the available evidence.\n", messageId: "answer", originatingMessageId: "origin", apiCallId: "api", interactionId: "interaction", turnId: "0" });
    expect(byType(events, "user.message")[0].data).toEqual({ content: "PRIVATE_USER_PROMPT", messageId: "user-message", interactionId: "interaction", turnId: "0", parentAgentTaskId: "task" });
    expect(byType(events, "system.message")[0].data).toEqual({ content: "PRIVATE_SYSTEM_PROMPT", role: "system", interactionId: "interaction" });
    expect(byType(events, "tool.execution_complete")[0].data).toMatchObject({ toolName: "bash", model: "claude-sonnet-5.5", interactionId: "interaction", turnId: "0", exitCode: 0, output: { content: "  sanitized output\n" } });
    expect(byType(events, "assistant.turn_start")[0].data).toEqual({ interactionId: "interaction", turnId: "0" });
    expect(byType(events, "session.task_complete")[0].data).toEqual({ summary: "  Completed the task.\n", success: true });
    const validate = createSessionValidator("unified").event;
    for (const event of events) {
      expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
      expect(normalizeUnifiedSessionEvent(event).data).toEqual(event.data);
    }
    const result = projectSessionResult(canonical);
    expect(result).toMatchObject({ num_turns: 1, usage: { input_tokens: 30, output_tokens: 5, reasoning_output_tokens: 0, cache_creation_input_tokens: 15 } });
    expect(sessionTokenTotal(result.usage)).toBe(35);
    for (const summary of [generatePlainTextSummary(events), generateCopilotCliStyleSummary(events), parseCopilotLog(JSON.stringify(copilotCiMessages)).markdown]) {
      expect(summary).not.toContain("PRIVATE_");
      expect(summary).toContain("Completed the task.");
    }
  });

  it("recovers requested tools in an interrupted message without inventing results or rewriting arguments", () => {
    const message = copilotCiMessages.find(event => event.type === "assistant.message");
    const partial = normalizeCopilotSession([message]);
    expect(byType(partial, "tool.execution_start").map(event => event.data)).toMatchObject([
      { toolCallId: "shell", toolName: "bash", input: { command: "printf sanitized" }, messageId: "answer", turnId: "0" },
      { toolCallId: "complete", toolName: "task_complete", input: { summary: "  Completed the task.\n" } },
    ]);
    expect(byType(partial, "tool.execution_complete")).toEqual([]);
    expect(byType(partial, "tool.execution_start").every(event => event.id === message.id && event.parentId === message.parentId)).toBe(true);
    stable(partial);
    const completed = normalizeCopilotSession([message, { type: "tool.execution_complete", data: { toolCallId: "shell", success: false, error: "Observed rejection" } }]);
    expect(byType(completed, "tool.execution_complete")[0].data.toolName).toBe("bash");
    const resumed = normalizeCopilotSession([...partial, copilotCiMessages.find(event => event.id === "shell-start")]);
    expect(byType(resumed, "tool.execution_start").map(event => event.data.toolCallId)).toEqual(["complete", "shell"]);
    stable(resumed);
  });

  it.each([false, 0, "", null, [], { value: 0 }])("preserves tool request arguments %j and explicit MCP server names", input => {
    const events = normalizeCopilotSession([
      { type: "assistant.message", id: "request", data: { toolRequests: [{ toolCallId: "call", name: "lookup", arguments: input, mcpServerName: "" }] } },
      { type: "tool.execution_complete", data: { toolCallId: "call", output: false, success: true } },
    ]);
    expect(byType(events, "tool.execution_start")[0].data.input).toEqual(input);
    expect(byType(events, "tool.execution_complete")[0].data).toMatchObject({ toolName: "lookup", mcpServerName: "", output: false });
    stable(events);
  });

  it("prefers explicitly present request inputs and retains malformed or unsupported request evidence", () => {
    const source = {
      type: "assistant.message",
      data: { toolRequests: [null, false, { arguments: {} }, { toolCallId: "call", name: "lookup", input: null, arguments: false }] },
    };
    const events = normalizeCopilotSession([source]);
    expect(events[0]).toEqual(source);
    expect(byType(events, "tool.execution_start")).toHaveLength(1);
    expect(byType(events, "tool.execution_start")[0].data).toMatchObject({ input: null, arguments: false });
    stable(events);
  });
});

describe("Copilot scoped stream and tool correlation", () => {
  const scopes = [
    [{ provenance: { component: "agent", phase: "agent", path: "first" } }, { provenance: { component: "agent", phase: "agent", path: "last" } }],
    [{ provenance: { component: "agent", phase: "agent", path: "same" } }, { provenance: { component: "agent", phase: "detection", path: "same" } }],
    [{ session_id: "first" }, { session_id: "last" }],
    [{ sessionId: "first" }, { sessionId: "last" }],
    [{ agentId: "first" }, { agentId: "last" }],
    [{ parent_tool_use_id: "first" }, { parent_tool_use_id: "last" }],
  ];

  it.each(scopes)("does not suppress independent message deltas with reused identities (%j, %j)", (first, last) => {
    const events = normalizeCopilotSession([
      { ...first, type: "assistant.message_delta", data: { messageId: "shared", deltaContent: "retained" } },
      { ...last, type: "assistant.message_delta", data: { messageId: "shared", deltaContent: "retained" } },
      { ...last, type: "assistant.message", data: { messageId: "shared", content: "retained" } },
    ]);
    expect(byType(events, "assistant.message")).toHaveLength(2);
    expect(byType(events, "assistant.message").map(event => event.data.content)).toEqual(["retained", "retained"]);
    stable(events);
  });

  it.each(scopes)("does not pair completions or tool requests with another source's start (%j, %j)", (first, last) => {
    const events = normalizeCopilotSession([
      { ...first, type: "tool.execution_start", data: { toolCallId: "shared", toolName: "wrong", mcpServerName: "wrong" } },
      { ...last, type: "tool.execution_complete", data: { toolCallId: "shared", success: true } },
      { ...last, type: "assistant.message", id: "requested", data: { toolRequests: [{ toolCallId: "shared", name: "right", arguments: {} }] } },
    ]);
    expect(byType(events, "tool.execution_complete")[0].data).toMatchObject({ toolCallId: "shared", success: true });
    expect(byType(events, "tool.execution_complete")[0].data.toolName).toBeUndefined();
    expect(byType(events, "tool.execution_start").map(event => event.data.toolName)).toEqual(["wrong", "right"]);
    stable(events);
  });

  it("keeps retry session boundaries even without provenance or top-level session IDs", () => {
    const events = normalizeCopilotSession([
      { type: "session.start", data: { sessionId: "first" } },
      { type: "tool.execution_start", data: { toolCallId: "shared", toolName: "earlier" } },
      { type: "assistant.message_delta", data: { messageId: "shared", deltaContent: "retained" } },
      { type: "session.start", data: { sessionId: "last" } },
      { type: "tool.execution_complete", data: { toolCallId: "shared" } },
      { type: "assistant.message", data: { messageId: "shared", content: "retained" } },
    ]);
    expect(byType(events, "tool.execution_complete")[0].data.toolName).toBeUndefined();
    expect(byType(events, "assistant.message")).toHaveLength(2);
    stable(events);
  });

  it("projects SDK reasoning deltas with independent reasoning IDs and exact whitespace", () => {
    const records = [
      { type: "assistant.reasoning_delta", id: "first", data: { reasoningId: "thought", deltaContent: "  Think " } },
      { type: "assistant.reasoning_delta", id: "last", data: { reasoningId: "thought", deltaContent: "\ncarefully. " } },
      { type: "assistant.message_delta", data: { messageId: "thought", deltaContent: "Answer" } },
    ];
    const partial = normalizeCopilotSession(records);
    stable(partial);
    const events = compact(partial);
    expect(byType(events, "assistant.reasoning").map(event => event.data)).toEqual([
      { content: "  Think ", delta: true, reasoningId: "thought" },
      { content: "\ncarefully. ", delta: true, reasoningId: "thought" },
    ]);
    expect(byType(collapseStreamedMessages(events), "assistant.reasoning").map(event => event.data.content)).toEqual(["  Think \ncarefully. "]);
    const snapshot = { type: "assistant.reasoning", data: { reasoningId: "thought", content: "  Think \ncarefully. " } };
    const complete = normalizeCopilotSession([...records, snapshot]);
    expect(byType(complete, "assistant.reasoning")).toEqual([snapshot]);
    expect(byType(complete, "assistant.message")).toHaveLength(1);
    stable(complete);
    const resumed = normalizeCopilotSession([...partial, snapshot]);
    expect(byType(resumed, "assistant.reasoning")).toEqual([snapshot]);
    stable(resumed);
    const incomplete = normalizeCopilotSession([...records, { ...snapshot, data: { ...snapshot.data, content: "" } }]);
    expect(byType(incomplete, "assistant.reasoning")).toHaveLength(3);
    const validate = createSessionValidator("unified").event;
    for (const event of events) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it("retains refusal correlation and native reasoning without exposing duplicated payloads", () => {
    const source = {
      type: "assistant.refusal",
      session_id: "session",
      agentId: "agent",
      data: { reason: "refusal", content: "Observed refusal", messageId: "message", originatingMessageId: "origin", apiCallId: "api", turnId: "0", reasoningText: "Observed reasoning" },
    };
    const events = compact(normalizeCopilotSession([source]));
    expect(byType(events, "assistant.refusal")[0].data).toEqual({
      reason: "refusal",
      content: "Observed refusal",
      sessionId: "session",
      agentId: "agent",
      messageId: "message",
      originatingMessageId: "origin",
      apiCallId: "api",
      turnId: "0",
    });
    const validate = createSessionValidator("unified").event;
    for (const event of events) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it("retains reasoning usage without adding it to output totals or counting a live report twice", () => {
    const records = [
      { type: "assistant.usage", id: "usage-first", data: { apiCallId: "first", inputTokens: 10, outputTokens: 5, reasoningTokens: 2 } },
      { type: "assistant.usage", id: "usage-repeat", data: { apiCallId: "first", inputTokens: 10, outputTokens: 5, reasoningTokens: 2 } },
      { type: "assistant.usage", id: "usage-last", data: { apiCallId: "last", inputTokens: 20, outputTokens: 10, reasoningTokens: 3 } },
    ];
    const events = normalizeCopilotSession(records);
    const usage = projectSessionResult(events).usage;
    expect(usage).toEqual({ input_tokens: 30, output_tokens: 15, reasoning_output_tokens: 5 });
    expect(sessionTokenTotal(usage)).toBe(45);
    stable(events);
  });
});
