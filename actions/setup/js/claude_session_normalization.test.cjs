import { describe, expect, it } from "vitest";
import { normalizeClaudeSession } from "./claude_session.cjs";
import { projectSessionResult, sessionTokenTotal } from "./agent_session.cjs";
import { mergeSessionSources } from "./unified_session.cjs";
import { collapseStreamedMessages } from "./agent_session_render.cjs";
import { generatePlainTextSummary } from "./log_parser_shared.cjs";
import { createSessionValidator } from "./scripts/validate_session.cjs";
import { normalization } from "./fixtures/claude_ci_normalization.cjs";

const unified = records => mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-stdio.log", events: normalizeClaudeSession(records) }]);
const stream = event => ({ type: "stream_event", session_id: "stream-session", parent_tool_use_id: null, event });

describe("Claude normalization from sampled runs", () => {
  it("exposes message identity, model, terminal status and reasoning usage without native envelopes", () => {
    const events = unified(normalization);
    expect(events.find(event => event.type === "assistant.message").data).toEqual({
      content: "  Inspect the example.\n",
      messageId: "normalization-message",
      contentIndex: 0,
      model: "claude-sonnet-5",
      sessionId: "normalization-session",
      parentToolUseId: null,
    });
    expect(events.find(event => event.type === "session.result" && event.data.sourceType === "result").data).toMatchObject({
      status: "success",
      sourceType: "result",
      usage: { inputTokens: 3, outputTokens: 7, reasoningOutputTokens: 2, cacheReadInputTokens: 11, cacheCreationInputTokens: 5, inputTokensIncludeCache: false },
    });
    expect(sessionTokenTotal(projectSessionResult(normalizeClaudeSession(normalization)).usage)).toBe(26);
    const validate = createSessionValidator("unified").event;
    for (const event of events) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it("keeps permission-denial evidence out of assistant answers without synthesizing a tool completion", () => {
    const events = normalizeClaudeSession(normalization);
    expect(events.filter(event => event.type === "assistant.message")).toHaveLength(1);
    expect(events.find(event => event.type === "session.result" && event.data.permissionDenials?.length).data.permissionDenials[0]).toMatchObject({
      subtype: "permission_denied",
      tool_use_id: "denied-tool",
      message: "Permission to use Edit has been denied.",
    });
    expect(events.filter(event => event.type === "tool.execution_complete" && event.data.toolCallId === "denied-tool")).toHaveLength(1);
    expect(normalizeClaudeSession(normalization.slice(0, 3)).some(event => event.type === "tool.execution_complete")).toBe(false);
    expect(projectSessionResult(events).permission_denials).toHaveLength(1);
  });

  it("normalizes MCP server/tool identity while retaining structured output and an unknown outcome", () => {
    const events = normalizeClaudeSession(normalization);
    const start = events.find(event => event.type === "tool.execution_start" && event.data.toolCallId === "mcp-tool");
    expect(start.data).toMatchObject({ name: "mcp__safeoutputs__noop", toolName: "noop", mcpServerName: "safeoutputs" });
    const complete = events.find(event => event.type === "tool.execution_complete" && event.data.toolCallId === "mcp-tool");
    expect(complete.data).toMatchObject({ toolName: "noop", mcpServerName: "safeoutputs", output: [{ type: "text", text: '{"result":"success"}' }] });
    expect(complete.data.success).toBeUndefined();
    expect(generatePlainTextSummary(events)).toContain("safeoutputs-noop");
    expect(normalizeClaudeSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it.each([false, true])("does not publish contradictory terminal success when is_error=%s", is_error => {
    const result = unified([{ type: "result", subtype: "success", is_error }])[0];
    expect(result.data).toMatchObject({ status: is_error ? "error" : "success", sourceType: "result" });
    if (is_error) expect(result.data.errors).toEqual([{ subtype: "success", is_error: true }]);
  });
});

describe("Claude message and stream boundaries", () => {
  it("keeps multiple text/reasoning blocks distinct after compaction and rendering", () => {
    const content = [
      { type: "text", text: "first\n" },
      { type: "text", text: "second\n" },
      { type: "thinking", thinking: "think one\n" },
      { type: "thinking", thinking: "think two\n" },
    ];
    const events = unified([{ type: "assistant", message: { id: "multi-block", model: "fixture-model", content } }]);
    expect(events.map(event => event.data.contentIndex)).toEqual([0, 1, 2, 3]);
    expect(collapseStreamedMessages(events).map(event => event.data.content)).toEqual(["first\n", "second\n", "think one\n", "think two\n"]);
  });

  it.each(["assistant", "user"])("normalizes a direct Messages API %s envelope", role => {
    const record = { type: "message", role, id: "native-message", model: "fixture-model", content: [{ type: "text", text: " exact\n" }], usage: { input_tokens: 2, output_tokens: 3 } };
    const original = structuredClone(record);
    const events = normalizeClaudeSession([record]);
    expect(events[0]).toMatchObject({ type: `${role}.message`, id: "native-message", data: { content: " exact\n", messageId: "native-message", contentIndex: 0, model: "fixture-model" } });
    if (role === "assistant") expect(projectSessionResult(events).usage).toMatchObject({ input_tokens: 2, output_tokens: 3 });
    else expect(events.some(event => event.type === "session.result")).toBe(false);
    expect(record).toEqual(original);
  });

  it("correlates block-at-a-time SDK snapshots with their streamed positions", () => {
    const events = normalizeClaudeSession([
      stream({ type: "message_start", message: { id: "streamed-message", model: "fixture-model" } }),
      stream({ type: "content_block_start", index: 0, content_block: { type: "thinking", thinking: "Think." } }),
      stream({ type: "content_block_start", index: 1, content_block: { type: "text", text: "Part" } }),
      { type: "assistant", session_id: "stream-session", parent_tool_use_id: null, message: { id: "streamed-message", model: "fixture-model", content: [{ type: "thinking", thinking: "Think.", signature: "snapshot-signature" }] } },
      { type: "assistant", session_id: "stream-session", parent_tool_use_id: null, message: { id: "streamed-message", model: "fixture-model", content: [{ type: "text", text: "Partial answer" }] } },
    ]);
    const messages = events.filter(event => event.type === "assistant.message");
    expect(messages.map(event => event.data.content).join("")).toBe("Partial answer");
    expect(messages.every(event => event.data.contentIndex === 1 && event.data.model === "fixture-model")).toBe(true);
    expect(events.find(event => event.type === "assistant.reasoning").data.signature).toBe("snapshot-signature");
  });

  it("does not attach user-supplied usage to session accounting", () => {
    const events = normalizeClaudeSession([{ type: "user", message: { id: "prompt", usage: { input_tokens: 999 }, content: "PRIVATE_PROMPT" } }]);
    expect(events.map(event => event.type)).toEqual(["user.message"]);
    expect(generatePlainTextSummary(events)).not.toContain("PRIVATE_PROMPT");
  });

  it("preserves user message attribution without exposing prompts in the summary", () => {
    const events = unified([{ type: "user", session_id: "session", parent_tool_use_id: "parent", message: { id: "prompt", content: "PRIVATE_PROMPT" } }]);
    expect(events[0].data).toEqual({ content: "PRIVATE_PROMPT", messageId: "prompt", contentIndex: 0, sessionId: "session", parentToolUseId: "parent" });
    expect(generatePlainTextSummary(events)).not.toContain("PRIVATE_PROMPT");
    expect(createSessionValidator("unified").event(events[0])).toBe(true);
  });

  it("retains a divergent final snapshot and renders it instead of duplicating stream text", () => {
    const records = [
      stream({ type: "message_start", message: { id: "corrected-message" } }),
      stream({ type: "content_block_start", index: 0, content_block: { type: "text", text: "partial observation" } }),
      { type: "assistant", session_id: "stream-session", parent_tool_use_id: null, message: { id: "corrected-message", content: [{ type: "text", text: "Authoritative snapshot" }] } },
    ];
    const events = unified(records);
    const messages = events.filter(event => event.type === "assistant.message");
    expect(messages.map(event => event.data.content)).toEqual(["partial observation", "Authoritative snapshot"]);
    expect(messages[1].data.delta).toBe(false);
    expect(
      collapseStreamedMessages(events)
        .filter(event => event.type === "assistant.message")
        .map(event => event.data.content)
    ).toEqual(["Authoritative snapshot"]);
    expect(normalizeClaudeSession(normalizeClaudeSession(records))).toEqual(normalizeClaudeSession(records));
  });

  it.each(["", "completed"])("retains an explicit native terminal status %j", status => {
    expect(unified([{ type: "result", subtype: "success", status }])[0].data.status).toBe(status);
  });

  it("accumulates reasoning snapshots once and respects authoritative terminal zero", () => {
    const records = [
      { type: "assistant", message: { id: "first", content: [], usage: { output_tokens: 5, output_tokens_details: { thinking_tokens: 2 } } } },
      { type: "assistant", message: { id: "first", content: [], usage: { output_tokens: 7, output_tokens_details: { thinking_tokens: 3 } } } },
      { type: "assistant", message: { id: "second", content: [], usage: { output_tokens: 4, output_tokens_details: { thinking_tokens: 1 } } } },
    ];
    expect(projectSessionResult(normalizeClaudeSession(records)).usage).toMatchObject({ output_tokens: 11, reasoning_output_tokens: 4 });
    expect(projectSessionResult(normalizeClaudeSession([...records, { type: "result", usage: { output_tokens_details: { thinking_tokens: 0 } } }])).usage).toMatchObject({ output_tokens: 11, reasoning_output_tokens: 0 });
  });

  it.each([-1, "2", null, NaN])("does not normalize invalid reasoning counts %j", thinking_tokens => {
    const event = normalizeClaudeSession([{ type: "result", usage: { output_tokens_details: { thinking_tokens } } }])[0];
    expect(event.data.usage.reasoning_output_tokens).toBeUndefined();
  });
});
