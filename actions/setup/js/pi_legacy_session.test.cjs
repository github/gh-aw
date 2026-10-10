import { afterEach, describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { normalizePiObservations, transformPiV3Entries } from "./pi_session.cjs";
import { parsePiLog } from "./parse_pi_log.cjs";
import { normalizeEngineLogEntries } from "./engine_log_parser.cjs";
import { observedSessionModel, selectSessionResult } from "./agent_session.cjs";
import { collectUnifiedSession, mergeSessionSources } from "./unified_session.cjs";
import { serializeSessionArtifact } from "./session_artifact.cjs";
import { generateCopilotCliStyleSummary } from "./log_parser_shared.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
import { createSessionValidator } from "./scripts/validate_session.cjs";

const roots = [];
afterEach(() => {
  for (const root of roots.splice(0)) fs.rmSync(root, { recursive: true, force: true });
});
const byType = (events, type) => events.filter(event => event.type === type);
const parse = events => parsePiLog(events.map(JSON.stringify).join("\n")).logEntries;

describe("historical Pi observations", () => {
  it.each(["assistant", "toolResult"])("maps a %s snapshot as metadata, not another message or usage report", role => {
    const usage = { input: 0, output: 2, cacheRead: 0, cost: { total: 0 } };
    const snapshot = {
      type: "pi.message_snapshot",
      id: "snapshot",
      parentId: null,
      timestamp: 0,
      message: { role, id: "native-message", content: [{ type: "text", text: "PRIVATE_REPEATED_CONTENT" }], model: "reported", usage, toolCallId: "call" },
      data: { role, model: "reported", usage },
    };
    const original = structuredClone(snapshot);
    const events = parse([snapshot, snapshot]);
    expect(events.map(event => event.type)).toEqual(["session.info", "session.info"]);
    expect(events[0]).toMatchObject({
      id: "snapshot",
      parentId: null,
      timestamp: 0,
      data: { role, model: "reported", messageId: "native-message", toolCallId: "call", sourceType: "message_snapshot", usageSnapshot: usage },
    });
    expect(selectSessionResult(events)).toBeUndefined();
    expect(byType(events, "assistant.message")).toEqual([]);
    expect(byType(events, "usage.report")).toEqual([]);
    expect(observedSessionModel(events)).toBe("reported");
    expect(generateCopilotCliStyleSummary(events)).not.toContain("PRIVATE_REPEATED_CONTENT");
    expect(snapshot).toEqual(original);
    expect(normalizePiObservations(events)).toEqual(events);
    const compact = events.map(normalizeUnifiedSessionEvent);
    expect(compact[0].data).toMatchObject({ role, model: "reported", usageSnapshot: usage });
    expect(compact[0].data).not.toHaveProperty("content");
  });

  it("retains the conversation, outcomes, argument observations and final accounting exactly once", () => {
    const usage = { input_tokens: 7, output_tokens: 2, cache_read_input_tokens: 0 };
    const records = [
      { type: "session.init", data: { sourceEngine: "pi", model: "initial" } },
      { type: "pi.message_snapshot", data: { role: "assistant", model: "reported", usage: { input: 7, output: 2 } } },
      { type: "pi.message_update", id: "start", data: { type: "toolcall_start", contentIndex: 0, id: "call", toolName: "lookup", usage: { input: 7 } } },
      { type: "tool.execution_start", data: { toolCallId: "call", toolName: "lookup", input: false } },
      { type: "pi.message_update", id: "delta", data: { type: "toolcall_delta", contentIndex: 0, delta: '{"flag":', usage: { input: 7, output: 2 } } },
      { type: "pi.message_update", id: "end", data: { type: "toolcall_end", contentIndex: 0, toolCall: { id: "call", name: "lookup", arguments: false } } },
      { type: "pi.tool_execution_update", id: "progress", parentId: null, timestamp: 0, data: { toolCallId: "call", parentToolCallId: null, input: false, partialResult: 0 } },
      { type: "tool.execution_complete", data: { toolCallId: "call", output: false, success: false } },
      { type: "assistant.reasoning", data: { content: " reasoning\n" } },
      { type: "assistant.message", data: { content: " answer\n\n" } },
      { type: "pi.agent_settled", data: {} },
      { type: "session.result", data: { numTurns: 1, totalCostUsd: 0, usage } },
    ];
    const original = structuredClone(records);
    for (const events of [parse(records), transformPiV3Entries(records), normalizeEngineLogEntries(records, "pi")]) {
      expect(events.some(event => event.type.startsWith("pi."))).toBe(false);
      expect(byType(events, "tool.execution_start")).toHaveLength(1);
      expect(byType(events, "tool.execution_complete")).toHaveLength(1);
      expect(byType(events, "assistant.message").map(event => event.data.content)).toEqual([" answer\n\n"]);
      expect(byType(events, "assistant.reasoning").map(event => event.data.content)).toEqual([" reasoning\n"]);
      expect(selectSessionResult(events)).toMatchObject({ numTurns: 1, totalCostUsd: 0, usage });
      expect(byType(events, "tool.execution_update").map(event => normalizeUnifiedSessionEvent(event))).toEqual([
        { type: "tool.execution_update", id: "start", data: { toolCallId: "call", toolName: "lookup", partial: true, contentIndex: 0, sourceType: "toolcall_start", usageSnapshot: { input: 7 } } },
        { type: "tool.execution_update", id: "delta", data: { toolCallId: "call", toolName: "lookup", input: '{"flag":', delta: true, partial: true, contentIndex: 0, sourceType: "toolcall_delta", usageSnapshot: { input: 7, output: 2 } } },
        { type: "tool.execution_update", id: "end", data: { toolCallId: "call", toolName: "lookup", input: false, partial: true, contentIndex: 0, sourceType: "toolcall_end" } },
        { type: "tool.execution_update", id: "progress", parentId: null, timestamp: 0, data: { toolCallId: "call", parentToolCallId: null, input: false, output: 0, partial: true, sourceType: "tool_execution_update" } },
      ]);
      const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events }]);
      const validate = createSessionValidator("unified").event;
      for (const event of unified) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
      expect(serializeSessionArtifact(unified).trim().split("\n").map(JSON.parse)).toEqual(JSON.parse(JSON.stringify(unified)));
      expect(normalizePiObservations(events)).toEqual(events);
    }
    expect(records).toEqual(original);
  });

  it.each([{ agentId: "child" }, { sessionId: "different" }, { parentToolCallId: "parent" }])("does not borrow a tool identity across observed scopes: %j", scope => {
    const events = normalizePiObservations([
      { type: "pi.message_update", data: { type: "toolcall_start", contentIndex: 0, id: "root-call", toolName: "lookup" } },
      { type: "pi.message_update", data: { type: "toolcall_delta", contentIndex: 0, delta: "false", ...scope } },
    ]);
    expect(events[1].data).toMatchObject({ input: "false", delta: true, contentIndex: 0 });
    expect(events[1].data).not.toHaveProperty("toolCallId");
  });

  it("leaves argument deltas orphaned after a finalized call, a new snapshot or an anonymous start", () => {
    for (const boundary of [
      { type: "pi.message_update", data: { type: "toolcall_end", contentIndex: 0, toolCall: { id: "old", name: "lookup", arguments: {} } } },
      { type: "pi.message_snapshot", data: { role: "assistant" } },
      { type: "pi.message_update", data: { type: "toolcall_start", contentIndex: 0 } },
    ]) {
      const events = normalizePiObservations([
        { type: "pi.message_update", data: { type: "toolcall_start", contentIndex: 0, id: "old", toolName: "lookup" } },
        boundary,
        { type: "pi.message_update", data: { type: "toolcall_delta", contentIndex: 0, delta: "false" } },
      ]);
      expect(events.at(-1).data).not.toHaveProperty("toolCallId");
      expect(events.at(-1).data).not.toHaveProperty("toolName");
    }
  });

  it("keeps boundary-only or unfamiliar message updates without inventing text", () => {
    const events = normalizePiObservations([
      { type: "pi.message_update", data: { type: "text_start", contentIndex: 0, usage: { input: 0 } } },
      { type: "pi.message_update", data: { type: "future_update", payload: { exact: [false, 0, null] } } },
    ]);
    expect(events.map(event => event.type)).toEqual(["assistant.message_update", "assistant.message_update"]);
    expect(events[1].data.payload).toEqual({ exact: [false, 0, null] });
    expect(byType(events, "assistant.message")).toEqual([]);
    expect(selectSessionResult(events)).toBeUndefined();
  });

  it("preserves explicit null identities and inputs over nested or earlier values", () => {
    const events = normalizePiObservations([
      { type: "pi.message_update", data: { type: "toolcall_start", contentIndex: 0, id: "old", toolName: "old-name" } },
      { type: "pi.message_update", data: { type: "toolcall_end", contentIndex: 0, toolCallId: null, toolName: null, input: null, toolCall: { id: "nested", name: "nested-name", arguments: false } } },
      { type: "pi.message_snapshot", message: { role: "assistant", id: null, responseId: "fallback" }, data: {} },
    ]);
    expect(events[1].data).toMatchObject({ toolCallId: null, toolName: null, input: null });
    expect(events[2].data).toMatchObject({ role: "assistant", messageId: null });
  });

  it.each([undefined, null, -1, "0"])("does not correlate tool updates without an observed content index: %j", contentIndex => {
    const events = normalizePiObservations([
      { type: "pi.message_update", data: { type: "toolcall_start", contentIndex, id: "old", toolName: "lookup" } },
      { type: "pi.message_update", data: { type: "toolcall_delta", contentIndex, delta: "false" } },
    ]);
    expect(events[1].data).not.toHaveProperty("toolCallId");
    expect(events[1].data).not.toHaveProperty("toolName");
  });

  it("does not reset another parent call's identity when a sibling snapshot arrives", () => {
    const events = normalizePiObservations([
      { type: "pi.message_update", data: { parentToolCallId: "child-a", type: "toolcall_start", contentIndex: 0, id: "retained", toolName: "lookup" } },
      { type: "pi.message_snapshot", message: { role: "assistant" }, data: { parentToolCallId: "child-b" } },
      { type: "pi.message_update", data: { parentToolCallId: "child-a", type: "toolcall_delta", contentIndex: 0, delta: "false" } },
      { type: "pi.message_snapshot", message: { role: "assistant" }, data: { parentToolCallId: "child-a" } },
      { type: "pi.message_update", data: { parentToolCallId: "child-a", type: "toolcall_delta", contentIndex: 0, delta: "false" } },
    ]);
    expect(events[2].data).toMatchObject({ toolCallId: "retained", toolName: "lookup" });
    expect(events[4].data).not.toHaveProperty("toolCallId");
  });

  it.each([
    ["agent_settled", "idle"],
    ["auto_retry_start", "retry_start"],
    ["auto_retry_end", "retry_end"],
    ["compaction_start", "compaction_start"],
    ["compaction_end", "compaction_end"],
    ["queue_update", "queue_update"],
    ["entry_appended", "entry_appended"],
    ["thinking_level_changed", "reasoning_effort_changed"],
    ["summarization_retry_scheduled", "summarization_retry_scheduled"],
    ["summarization_retry_attempt_start", "summarization_retry_attempt_start"],
    ["summarization_retry_finished", "summarization_retry_finished"],
  ])("maps legacy %s to session.%s with its exact payload", (source, target) => {
    const [event] = normalizePiObservations([{ type: `pi.${source}`, timestamp: 0, data: { exact: [false, 0, null] } }]);
    expect(event).toMatchObject({ type: `session.${target}`, timestamp: 0, data: { exact: [false, 0, null] } });
  });

  it("normalizes legacy subagent wrappers with the same isolated invocation and accounting contract", () => {
    const events = normalizePiObservations([
      { type: "pi.subagent_dispatch", data: { agent: "reader", invocation_id: "child", requested_model: "small", resolved_model: "gpt-5.4-mini" } },
      { type: "pi.subagent_event", data: { agent: "reader", invocation_id: "child", event: { message: { role: "assistant", model: "gpt-5.4-mini", usage: { input: 5, output: 2 } } } } },
      { type: "pi.subagent_result", data: { agent: "reader", invocation_id: "child", outcome: "failed", error_code: "SUBAGENT_MODEL_UNAVAILABLE" } },
    ]);
    expect(events.map(event => event.type)).toEqual(["subagent.started", "subagent.configured", "subagent.request", "subagent.failed"]);
    expect(events.every(event => event.agentId === "child")).toBe(true);
    expect(byType(events, "subagent.request")[0].data).toMatchObject({ inputTokens: 5, outputTokens: 2 });
    expect(byType(events, "subagent.failed")[0].data).toMatchObject({ errorCode: "SUBAGENT_MODEL_UNAVAILABLE" });
    expect(selectSessionResult(events)).toBeUndefined();
  });

  it("converts error-only saved Pi sources during collection, without needing a conversation", () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-legacy-pi-"));
    roots.push(root);
    fs.writeFileSync(path.join(root, "agent-session.jsonl"), JSON.stringify({ type: "pi.error", data: { error: { statusCode: 401, errorType: "provider" } } }));
    const { events } = collectUnifiedSession({ rootDir: root, engine: "pi" });
    expect(byType(events, "session.error")).toHaveLength(1);
    expect(byType(events, "agent.execution")[0].data.errorCodes).toEqual([401]);
    expect(events.some(event => event.type.startsWith("pi."))).toBe(false);
  });

  it("preserves genuinely unknown Pi extensions, foreign attribution and other-engine normalization", () => {
    const opaque = [{ type: "pi.future_extension", data: { exact: [false, 0, null] } }];
    const foreign = [{ type: "pi.message_snapshot", data: { sourceEngine: "copilot", role: "assistant", model: "foreign" } }];
    expect(normalizePiObservations(opaque)).toBe(opaque);
    expect(normalizePiObservations(foreign)).toBe(foreign);
    for (const engine of ["aider", "copilot", "custom"]) {
      const records = [{ type: "pi.message_snapshot", data: { model: "reported" } }, ...opaque];
      expect(normalizeEngineLogEntries(records, engine)).toEqual(records);
    }
  });
});
