import { describe, expect, it } from "vitest";
import { parseClaudeLog } from "./parse_claude_log.cjs";
import { normalizeClaudeSession, normalizeClaudeObservations } from "./claude_session.cjs";
import { projectSessionResult } from "./agent_session.cjs";
import { mergeSessionSources } from "./unified_session.cjs";
import { createSessionValidator } from "./scripts/validate_session.cjs";
import { success, failure } from "./fixtures/claude_ci_sessions.cjs";
import { normalization } from "./fixtures/claude_ci_normalization.cjs";

const parse = records => parseClaudeLog(records.map(record => JSON.stringify(record)).join("\n")).logEntries;
const project = events => mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events }]);
const stream = event => ({ type: "stream_event", session_id: "audit-session", parent_tool_use_id: null, event });

describe("Claude native background Bash lifecycle", () => {
  // Verified against run 36812703027, agent artifact 11139794162, Claude Code 2.1.285.
  const tasks = success.filter(record => record.type === "system" && record.subtype.startsWith("task_"));

  it("uses canonical tool updates, not dynamic workflow events or duplicate completions", () => {
    const events = parse(success);
    const updates = events.filter(event => event.type === "tool.execution_update");
    expect(updates.map(event => event.data.sourceType)).toEqual(["task_started", "task_notification"]);
    expect(updates.every(event => event.data.taskId === "task-example" && event.data.taskType === "local_bash")).toBe(true);
    expect(updates.every(event => event.data.toolCallId === "tool-example" && event.data.toolName === "Bash")).toBe(true);
    expect(updates[1].data).toMatchObject({ status: "completed", output: "Example task completed", output_file: "/workspace/example/task.output" });
    expect(events.filter(event => event.type === "tool.execution_complete")).toHaveLength(2);
    expect(events.some(event => event.type.startsWith("dynamicWorkflows."))).toBe(false);
    const projected = project(events).filter(event => event.type === "tool.execution_update");
    expect(projected[1].data).toMatchObject({ taskId: "task-example", taskType: "local_bash", toolCallId: "tool-example", status: "completed", output: "Example task completed" });
    const validate = createSessionValidator("unified").event;
    for (const event of projected) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
  });

  it("migrates saved task wrappers with provenance intact and without replaying tool results", () => {
    const saved = tasks.map((record, index) => ({
      ...record,
      type: `dynamicWorkflows.${record.subtype}`,
      data: { ...record, toolName: "Bash" },
      provenance: { path: "agent-session.jsonl", index },
    }));
    const original = structuredClone(saved);
    const migrated = normalizeClaudeObservations(saved, event => ({ ...event, mapped: true }));
    expect(migrated.map(event => event.type)).toEqual(["tool.execution_update", "tool.execution_update"]);
    expect(migrated.map(event => event.provenance)).toEqual(saved.map(event => event.provenance));
    expect(migrated.every(event => event.mapped)).toBe(true);
    expect(migrated.every(event => event.data.toolName === "Bash")).toBe(true);
    expect(migrated[1].data).toMatchObject({ taskType: "local_bash", taskId: "task-example", toolName: "Bash", output: "Example task completed" });
    expect(normalizeClaudeObservations(migrated)).toEqual(migrated);
    expect(normalizeClaudeSession(migrated)).toEqual(migrated);
    expect(saved).toEqual(original);
  });

  it.each(["failed", "stopped", "future-status"])("retains %s as an observed update without fabricating a result", status => {
    const events = parse([tasks[0], { ...tasks[1], status, error: "Observed background diagnostic." }]);
    expect(events.map(event => event.type)).toEqual(["tool.execution_update", "tool.execution_update"]);
    expect(events[1].data).toMatchObject({ status, error: "Observed background diagnostic.", partial: true });
    expect(project(events)[1].data.error).toBe("Observed background diagnostic.");
    expect(events[1].data.success).toBeUndefined();
    expect(projectSessionResult(events)).toBeUndefined();
  });

  it("does not infer task types across session/child boundaries or override explicit workflow types", () => {
    const events = parse([tasks[0], { ...tasks[1], session_id: "other-session" }, { ...tasks[1], parent_tool_use_id: "child-tool" }, { ...tasks[1], task_type: "local_workflow" }]);
    expect(events.map(event => event.type)).toEqual(["tool.execution_update", "dynamicWorkflows.task_notification", "dynamicWorkflows.task_notification", "dynamicWorkflows.task_notification"]);
  });

  it("keeps orphan notifications opaque but maps an explicitly identified orphan Bash task", () => {
    expect(parse([tasks[1]])[0].type).toBe("dynamicWorkflows.task_notification");
    expect(parse([{ ...tasks[1], task_type: "local_bash" }])[0]).toMatchObject({ type: "tool.execution_update", data: { taskId: "task-example", toolCallId: "tool-example" } });
  });
});

describe("Claude canonical diagnostics from live run shapes", () => {
  it.each(["thinking_tokens", "vcs_state_changed", "status", "compact_boundary"])("keeps mapped %s status text out of assistant messages", subtype => {
    for (const payload of [{ message: "Status text" }, { message: { content: "Status text" } }, { content: [{ type: "text", text: "Status text" }] }]) {
      const record = { type: "system", subtype, session_id: "audit-session", ...payload };
      const events = parse([record]);
      expect(events.map(event => event.type)).toEqual(["session.info"]);
      expect(events[0].data).toMatchObject({ sourceType: subtype, ...payload });
      const unified = project(events);
      expect(unified.map(event => event.type)).toEqual(["session.info"]);
      expect(normalizeClaudeSession(events)).toEqual(events);
      expect(createSessionValidator("unified").event(unified[0])).toBe(true);
    }
  });

  // Existing failed run 36762044297 and docs run 37623157868, sanitized in the imported fixtures.
  it("retains API failures and retries once, as canonical errors rather than duplicate opaque envelopes", () => {
    const events = parse(failure);
    expect(events.some(event => event.type === "claude.assistant_error" || (event.type === "claude.system" && event.data.subtype === "api_retry"))).toBe(false);
    const errors = events.filter(event => ["session.result", "engine.retry"].includes(event.type) && event.data.errors);
    expect(errors).toHaveLength(3);
    expect(errors[0]).toMatchObject({ uuid: "provider-error-example", data: { sourceType: "assistant", is_api_error_message: true, error: "server_error" } });
    expect(errors[2]).toMatchObject({ type: "engine.retry", data: { sourceType: "system", attempt: 1, maxRetries: 10, backoffMs: 1000 } });
    expect(projectSessionResult(events).errors).toHaveLength(3);
    expect(project(events).filter(event => ["session.result", "engine.retry"].includes(event.type) && event.data.errors)).toHaveLength(3);
    const unified = project(events);
    expect(unified.filter(event => event.type === "session.result" && event.data.errors)).toHaveLength(2);
    expect(unified.filter(event => event.type === "engine.retry")).toHaveLength(1);
    expect(unified.find(event => event.type === "engine.retry").data).toMatchObject({ attempt: 1, maxRetries: 10, backoffMs: 1000, errors: errors[2].data.errors });
    expect(normalizeClaudeSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it("maps a denial immediately and reconciles the terminal denial snapshot without fabricating a tool result", () => {
    const partial = parse(normalization.slice(0, 3));
    const report = partial.find(event => event.type === "session.result");
    expect(report).toMatchObject({ uuid: "denial-record", data: { sourceEngine: "claude", sourceType: "system", permissionDenials: [{ tool_use_id: "denied-tool" }] } });
    expect(report.data.status).toBeUndefined();
    expect(partial.some(event => event.type === "tool.execution_complete")).toBe(false);
    const events = parse(normalization);
    expect(events.filter(event => event.type === "session.result").map(event => event.data.permissionDenials)).toMatchObject([
      [{ tool_use_id: "denied-tool", tool_input: { file_path: "/workspace/example.txt" }, message: "Permission to use Edit has been denied." }],
      [],
    ]);
    expect(events.at(-1).permission_denials).toEqual(normalization.at(-1).permission_denials);
    expect(projectSessionResult(events).permission_denials).toHaveLength(1);
    expect(project(events).flatMap(event => event.data.permissionDenials ?? [])).toHaveLength(1);
    expect(normalizeClaudeSession(events)).toEqual(events);
  });

  it("preserves distinct denial observations, unmatched snapshot entries, and explicitly empty snapshots", () => {
    const denied = normalization[2];
    const events = parse([
      denied,
      { ...denied, uuid: "distinct-denial" },
      { type: "result", session_id: denied.session_id, permission_denials: [{ tool_use_id: denied.tool_use_id }, { tool_use_id: "unobserved-denial" }] },
      { type: "result", session_id: denied.session_id, permission_denials: [] },
    ]);
    expect(events.map(event => event.data.permissionDenials.length)).toEqual([1, 1, 1, 0]);
    expect(projectSessionResult(events).permission_denials.map(denial => denial.tool_use_id)).toEqual(["denied-tool", "denied-tool", "unobserved-denial"]);
    expect(events.map(event => event.uuid).slice(0, 2)).toEqual(["denial-record", "distinct-denial"]);
  });

  it("does not reconcile denial identities across sessions or ambiguous child scopes", () => {
    const denied = normalization[2];
    const events = parse([
      denied,
      { ...denied, uuid: "child-denial", parent_tool_use_id: "child-tool" },
      { type: "result", session_id: denied.session_id, permission_denials: [{ tool_use_id: denied.tool_use_id }] },
      { type: "result", session_id: "other-session", permission_denials: [{ tool_use_id: denied.tool_use_id }] },
    ]);
    expect(events.map(event => event.data.permissionDenials.length)).toEqual([1, 1, 1, 1]);
  });
});

describe("Claude observed partials and structured payloads", () => {
  it("retains unknown native record types without inventing conversation or terminal success", () => {
    const records = [{ type: "future_observation", uuid: "future-record", session_id: "audit-session", payload: { zero: 0, empty: "", disabled: false, unknown: null } }];
    const original = structuredClone(records);
    const events = parse(records);
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({ type: "claude.future_observation", uuid: "future-record", data: { payload: records[0].payload } });
    expect(project(events)[0].data.payload).toEqual(records[0].payload);
    expect(projectSessionResult(events)).toBeUndefined();
    expect(normalizeClaudeSession(events)).toEqual(events);
    expect(records).toEqual(original);
  });
  it.each(["session.result", "tool.execution_complete"])("keeps unknown dotted type %s opaque", type => {
    const events = parse([{ type, id: "unknown-dotted-record", session_id: "audit-session", data: { success: true, toolCallId: "fabricated-tool" } }]);
    expect(events.map(event => event.type)).toEqual([`claude.${type}`]);
    expect(project(events).map(event => event.type)).toEqual([`claude.${type}`]);
    expect(events.some(event => ["session.result", "tool.execution_complete"].includes(event.type))).toBe(false);
    expect(projectSessionResult(events)).toBeUndefined();
  });
  it("keeps a bare canonical-looking refusal envelope opaque", () => {
    const record = { type: "assistant.refusal", id: "native", parentId: null, timestamp: 0, data: { reason: "refusal", content: " text\n", partial: false, nativeExtra: true } };
    const original = structuredClone(record);
    const events = parse([record]);
    expect(events.map(event => event.type)).toEqual(["claude.assistant.refusal"]);
    expect(events[0].data).toMatchObject(record.data);
    expect(project(events).map(event => event.type)).toEqual(["claude.assistant.refusal"]);
    expect(normalizeClaudeSession(JSON.parse(JSON.stringify(events)))).toEqual(events);
    expect(record).toEqual(original);
  });
  it("preserves a mapped native refusal through saved-session normalization", () => {
    const events = parse([{ type: "assistant", uuid: "native-refusal", message: { role: "assistant", content: [{ type: "text", text: " text\n" }], stop_reason: "refusal" } }]);
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({ type: "assistant.refusal", data: { sourceEngine: "claude", reason: "refusal", content: " text\n" } });
    expect(normalizeClaudeSession(JSON.parse(JSON.stringify(events)))).toEqual(events);
    expect(project(events)[0]).toMatchObject({ type: "assistant.refusal", data: { reason: "refusal", content: " text\n" } });
  });
  // Synthetic Messages API/SDK shapes: the inspected CI artifacts do not enable partial-message streaming.
  it("retains mapped stream metadata and final snapshots without duplicate claude records in any serialization", () => {
    const records = [
      stream({ type: "message_start", message: { id: "streamed", model: "fixture-model" } }),
      { ...stream({ type: "content_block_start", index: 0, content_block: { type: "text", text: "  start" } }), id: "native-start", parentId: null, timestamp: 0 },
      stream({ type: "content_block_delta", index: 0, delta: { type: "text_delta", text: "\r\n" } }),
      { type: "assistant", session_id: "audit-session", parent_tool_use_id: null, uuid: "final-snapshot", message: { id: "streamed", model: "fixture-model", content: [{ type: "text", text: "  start\r\n" }] } },
      stream({ type: "future_transport_event", value: false }),
      { type: "vendor.unknown", id: "unknown-id", data: { content: " \n", zero: 0, value: null } },
    ];
    const original = structuredClone(records);
    const events = parse(records);
    const text = events.filter(event => event.type === "assistant.message");
    expect(text.map(event => event.data.content)).toEqual(["  start", "\r\n"]);
    expect(text[0]).toMatchObject({ id: "native-start", parentId: null, timestamp: 0, data: { snapshots: [{ uuid: "final-snapshot" }] } });
    expect(text[0].event).toEqual(records[1].event);
    expect(events.filter(event => event.type === "claude.stream_event").map(event => event.data.event.type)).toEqual(["future_transport_event"]);
    expect(events.find(event => event.type === "assistant.message_update").data).toMatchObject({ sourceType: "message_start", messageId: "streamed", model: "fixture-model" });
    expect(events.some(event => event.type === "claude.assistant_snapshot")).toBe(false);
    const persisted = JSON.parse(JSON.stringify(events));
    expect(normalizeClaudeSession(persisted)).toEqual(persisted);
    const unified = project(persisted);
    expect(unified.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["  start", "\r\n"]);
    expect(unified.find(event => event.type === "claude.vendor.unknown").data).toMatchObject(records.at(-1).data);
    const validate = createSessionValidator("unified").event;
    for (const event of unified) expect(validate(event), JSON.stringify(validate.errors)).toBe(true);
    expect(records).toEqual(original);
  });

  it.each([false, 0, null, [], { source: { type: "base64", data: "example" } }])("retains an explicit structured/falsy user payload %j", content => {
    const events = parse([{ type: "user", id: "user-record", message: { content } }]);
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({ type: "user.message", id: "user-record" });
    expect(events[0].data.content).toEqual(content);
    expect(project(events)[0].data.content).toEqual(content);
  });

  it("maps images, documents and opaque redacted reasoning while retaining truly unknown blocks", () => {
    const image = { type: "image", source: { type: "base64", media_type: "image/png", data: "sanitized-image" } };
    const document = { type: "document", source: { type: "text", media_type: "text/plain", data: " exact\n" } };
    const redacted = { type: "redacted_thinking", data: "opaque-sanitized-thinking" };
    const events = parse([
      { type: "user", message: { content: [image, document] } },
      { type: "assistant", message: { content: [redacted, { type: "future_block", payload: false }] } },
    ]);
    expect(events.map(event => event.type)).toEqual(["user.message", "user.message", "assistant.reasoning", "claude.content_block"]);
    expect(events.slice(0, 3).map(event => event.data.content)).toEqual([image, document, redacted]);
    expect(
      project(events)
        .slice(0, 3)
        .map(event => event.data.content)
    ).toEqual([image, document, redacted]);
    expect(projectSessionResult(events)).toBeUndefined();
  });

  it.each([
    ["assistant.reasoning", { type: "redacted_thinking", data: "opaque-sanitized-thinking" }],
    ["assistant.message", { type: "image", source: { type: "base64", media_type: "image/png", data: "sanitized-image" } }],
    ["assistant.message", { type: "document", source: { type: "text", media_type: "text/plain", data: " exact\n" } }],
  ])("retains a streamed atomic %s block once with every finalized snapshot", (type, block) => {
    const start = { ...stream({ type: "content_block_start", index: 0, content_block: block }), id: "atomic-start", parentId: null, timestamp: 0 };
    const snapshot = {
      type: "assistant",
      session_id: "audit-session",
      parent_tool_use_id: null,
      uuid: "atomic-snapshot",
      message: { id: "atomic", model: "fixture-model", content: [block] },
    };
    const repeated = { ...snapshot, uuid: "repeated-atomic-snapshot" };
    const records = [stream({ type: "message_start", message: { id: "atomic", model: "fixture-model" } }), start, snapshot, repeated];
    const original = structuredClone(records);
    const events = parse(records);
    const blocks = events.filter(event => event.type === type);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]).toMatchObject({ id: "atomic-start", parentId: null, timestamp: 0, data: { contentIndex: 0, messageId: "atomic", model: "fixture-model", content: block } });
    expect(blocks[0].event).toEqual(start.event);
    expect(blocks[0].data.snapshots).toEqual([snapshot, repeated]);
    const persisted = JSON.parse(JSON.stringify(events));
    expect(normalizeClaudeSession(persisted)).toEqual(persisted);
    const unified = project(persisted).filter(event => event.type === type);
    expect(unified).toHaveLength(1);
    expect(unified[0].data.content).toEqual(block);
    expect(records).toEqual(original);
  });

  it("enriches an atomic block from its final snapshot without discarding the original observation", () => {
    const observed = { type: "redacted_thinking", data: "observed-opaque-thinking" };
    const finalized = { type: "redacted_thinking", data: "final-opaque-thinking", signature: "native-signature" };
    const snapshot = { type: "assistant", session_id: "audit-session", parent_tool_use_id: null, message: { id: "atomic-update", content: [finalized] } };
    const events = parse([stream({ type: "message_start", message: { id: "atomic-update" } }), stream({ type: "content_block_start", index: 0, content_block: observed }), snapshot]);
    const reasoning = events.filter(event => event.type === "assistant.reasoning");
    expect(reasoning).toHaveLength(1);
    expect(reasoning[0].data).toMatchObject({ content: finalized, signature: "native-signature", snapshots: [snapshot] });
    expect(reasoning[0].event.content_block).toEqual(observed);
    expect(project(events).find(event => event.type === "assistant.reasoning").data.content).toEqual(finalized);
  });

  it("keeps an explicit camelCase tool success through normalization and unified projection", () => {
    const events = parse([{ type: "user", id: "tool-result", message: { content: [{ type: "tool_result", tool_use_id: "orphan", content: "", isError: false }] } }]);
    expect(events[0]).toMatchObject({ type: "tool.execution_complete", data: { toolCallId: "orphan", output: "", success: true, isError: false } });
    expect(project(events)[0].data).toMatchObject({ toolCallId: "orphan", output: "", success: true, isError: false });
    expect(normalizeClaudeSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it.each([{ is_error: true }, { isError: true }, { success: false }])("preserves a failure %j over a contradictory success signal", failureSignal => {
    const events = parse([{ type: "user", message: { content: [{ type: "tool_result", tool_use_id: "orphan", content: { exact: false }, is_error: false, success: true, ...failureSignal }] } }]);
    expect(events[0].data.success).toBe(false);
    expect(project(events)[0].data.success).toBe(false);
  });

  it.each([{ is_error: true }, { success: false }, { error: { code: "failed" } }])("keeps failure %j authoritative over isError: false", failureSignal => {
    const events = parse([{ type: "user", message: { content: [{ type: "tool_result", content: false, isError: false, ...failureSignal }] } }]);
    expect(events[0].data.success).toBe(false);
    expect(project(events)[0].data.success).toBe(false);
  });

  it("retains interruption evidence without inventing tool durations or exit codes", () => {
    const events = parse([{ type: "user", message: { content: [{ type: "tool_result", content: "", is_error: false }] }, tool_use_result: { stdout: "", stderr: "", interrupted: true } }]);
    expect(events[0].data.success).toBe(false);
    expect(events[0].data.tool_use_result.interrupted).toBe(true);
    expect(events[0].data.durationMs).toBeUndefined();
    expect(events[0].data.exitCode).toBeUndefined();
  });

  it("maps a raw provider error separately from assistant answers", () => {
    const events = parse([{ type: "error", error: { type: "overloaded_error", message: "Sanitized provider failure." }, request_id: "request", timestamp: 0 }]);
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({ type: "session.result", timestamp: 0, request_id: "request", data: { sourceType: "error", errors: [{ type: "overloaded_error" }] } });
    expect(events[0].data.status).toBeUndefined();
  });

  it("retains a failed authentication diagnostic as a canonical error, not a tool result", () => {
    const events = parse([{ type: "auth_status", isAuthenticating: false, output: [], error: "Sanitized authentication failure." }]);
    expect(events.map(event => event.type)).toEqual(["session.result"]);
    expect(events[0].data).toMatchObject({ sourceType: "auth_status", isAuthenticating: false, output: [], errors: ["Sanitized authentication failure."] });
    expect(parse([{ type: "auth_status", isAuthenticating: true, output: [] }])[0]).toMatchObject({ type: "session.info", data: { sourceType: "auth_status", isAuthenticating: true, output: [] } });
  });

  it("correlates finalized snapshots after missing block starts, without losing reasoning deltas or signatures", () => {
    const snapshot = {
      type: "assistant",
      session_id: "audit-session",
      parent_tool_use_id: null,
      uuid: "recovered-snapshot",
      message: { id: "recovered", model: "fixture-model", content: [{ type: "thinking", thinking: "  one two\n", signature: "exact-signature" }] },
    };
    const events = parse([
      stream({ type: "message_start", message: { id: "recovered" } }),
      stream({ type: "content_block_delta", index: 0, delta: { type: "thinking_delta", thinking: "  one" } }),
      stream({ type: "content_block_delta", index: 0, delta: { type: "thinking_delta", thinking: " two\n" } }),
      snapshot,
      { ...snapshot, uuid: "repeated-snapshot" },
    ]);
    const reasoning = events.filter(event => event.type === "assistant.reasoning");
    expect(reasoning.map(event => event.data.content)).toEqual(["  one", " two\n"]);
    expect(reasoning[0].data).toMatchObject({ model: "fixture-model", signature: "exact-signature", snapshots: [{ uuid: "recovered-snapshot" }, { uuid: "repeated-snapshot" }] });
  });

  it("keeps a finalized snapshot once when only its message start was observed", () => {
    const snapshot = { type: "assistant", session_id: "audit-session", parent_tool_use_id: null, message: { id: "snapshot-only", content: [{ type: "text", text: " exact\n" }] } };
    const events = parse([stream({ type: "message_start", message: { id: "snapshot-only" } }), snapshot, snapshot]);
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual([" exact\n"]);
    expect(events.find(event => event.type === "assistant.message").data.snapshots).toEqual([snapshot, snapshot]);
  });

  it("retains the first and only finalized snapshot after a message start", () => {
    const snapshot = { type: "assistant", session_id: "audit-session", parent_tool_use_id: null, id: "only-snapshot", timestamp: 0, message: { id: "one-snapshot", content: [{ type: "text", text: " exact\n" }] } };
    const events = parse([stream({ type: "message_start", message: { id: "one-snapshot" } }), snapshot]);
    const messages = events.filter(event => event.type === "assistant.message");
    expect(messages).toHaveLength(1);
    expect(messages[0].data.snapshots).toEqual([snapshot]);
    const persisted = JSON.parse(JSON.stringify(events));
    expect(normalizeClaudeSession(persisted)).toEqual(persisted);
    expect(project(persisted).find(event => event.type === "assistant.message")).toMatchObject({ id: "only-snapshot", timestamp: 0, data: { content: " exact\n" } });
  });
});

describe("Claude refusal snapshot boundaries", () => {
  it("merges repeated refusal markers and final policy metadata without duplicating answers or opaque snapshots", () => {
    const records = [
      stream({ type: "message_start", message: { id: "refused", model: "fixture-model" } }),
      stream({ type: "content_block_start", index: 0, content_block: { type: "text", text: "  Partial\n" } }),
      stream({ type: "message_delta", delta: { stop_reason: "refusal" } }),
      stream({ type: "message_delta", delta: { stop_reason: "refusal" } }),
      {
        type: "assistant",
        session_id: "audit-session",
        parent_tool_use_id: null,
        uuid: "policy-snapshot",
        message: { id: "refused", model: "fixture-model", stop_reason: "refusal", stop_details: { category: "example", explanation: null }, content: [{ type: "text", text: "  Final refusal\n" }] },
      },
    ];
    const partial = parse(records.slice(0, 3));
    expect(partial.find(event => event.type === "assistant.refusal").data).toMatchObject({ partial: true, content: "  Partial\n", messageId: "refused", model: "fixture-model" });
    const events = parse(records);
    expect(events.some(event => event.type === "assistant.message" || event.type === "claude.assistant_snapshot")).toBe(false);
    const refusals = events.filter(event => event.type === "assistant.refusal");
    expect(refusals).toHaveLength(1);
    expect(refusals[0].data).toMatchObject({ reason: "refusal", content: "  Final refusal\n", policyCategory: "example", explanation: null, snapshots: [{}, { uuid: "policy-snapshot" }] });
    expect(refusals[0].data.partial).toBeUndefined();
    expect(refusals[0].data.observedTextEvents[0].data.content).toBe("  Partial\n");
    expect(project(events).find(event => event.type === "assistant.refusal").data.content).toBe("  Final refusal\n");
    expect(normalizeClaudeSession(events)).toEqual(events);
  });

  it("reclassifies earlier text when refusal is first supplied by the finalized SDK snapshot", () => {
    const snapshot = {
      type: "assistant",
      session_id: "audit-session",
      parent_tool_use_id: null,
      uuid: "late-refusal-snapshot",
      message: { id: "late-refusal", stop_reason: "refusal", stop_details: { category: "example", explanation: null }, content: null },
    };
    const events = parse([stream({ type: "message_start", message: { id: "late-refusal" } }), stream({ type: "content_block_start", index: 0, content_block: { type: "text", text: "Not an answer." } }), snapshot]);
    expect(events.filter(event => event.type === "assistant.refusal")).toHaveLength(1);
    expect(events.some(event => event.type === "assistant.message")).toBe(false);
    expect(events.find(event => event.type === "assistant.refusal").data.observedTextEvents).toHaveLength(1);
    expect(events.find(event => event.type === "assistant.refusal").data.snapshots).toEqual([snapshot]);
    const persisted = JSON.parse(JSON.stringify(events));
    expect(normalizeClaudeSession(persisted)).toEqual(persisted);
    expect(project(persisted).find(event => event.type === "assistant.refusal").data).toMatchObject({ reason: "refusal", policyCategory: "example", explanation: null });
  });

  it("reclassifies snapshot suffixes and corrected text rather than leaving a duplicate answer", () => {
    const snapshot = content => ({ type: "assistant", session_id: "audit-session", parent_tool_use_id: null, message: { id: "suffix-refusal", content: [{ type: "text", text: content }] } });
    const events = parse([
      stream({ type: "message_start", message: { id: "suffix-refusal" } }),
      stream({ type: "content_block_start", index: 0, content_block: { type: "text", text: "Part" } }),
      snapshot("Partial"),
      snapshot("Corrected"),
      stream({ type: "message_delta", delta: { stop_reason: "refusal" } }),
    ]);
    expect(events.some(event => event.type === "assistant.message")).toBe(false);
    const refusal = events.find(event => event.type === "assistant.refusal");
    expect(refusal.data.content).toBe("Corrected");
    expect(refusal.data.observedTextEvents.map(event => event.data.content)).toEqual(["Part", "ial", "Corrected"]);
  });
});
