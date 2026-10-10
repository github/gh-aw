import { describe, expect, it } from "vitest";
import { parseClaudeLog } from "./parse_claude_log.cjs";
import { normalizeClaudeSession } from "./claude_session.cjs";
import { projectSessionResult } from "./agent_session.cjs";
import { mergeSessionSources } from "./unified_session.cjs";
import { createSessionValidator } from "./scripts/validate_session.cjs";
import { failure } from "./fixtures/claude_ci_sessions.cjs";
import { normalization } from "./fixtures/claude_ci_normalization.cjs";

const parse = records => parseClaudeLog(records.map(record => JSON.stringify(record)).join("\n")).logEntries;
const project = events => mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events }]);
const stream = event => ({ type: "stream_event", session_id: "audit-session", parent_tool_use_id: null, event });

describe("Claude canonical diagnostics from live run shapes", () => {
  // Existing failed run 36762044297 and docs run 37623157868, sanitized in the imported fixtures.
  it("retains API failures and retries once, as canonical errors rather than duplicate opaque envelopes", () => {
    const events = parse(failure);
    expect(events.some(event => event.type === "claude.assistant_error" || (event.type === "claude.system" && event.data.subtype === "api_retry"))).toBe(false);
    const errors = events.filter(event => event.type === "session.result" && event.data.errors);
    expect(errors).toHaveLength(3);
    expect(errors[0]).toMatchObject({ uuid: "provider-error-example", data: { sourceType: "assistant", is_api_error_message: true, error: "server_error" } });
    expect(errors[2].data).toMatchObject({ sourceType: "system", subtype: "api_retry", max_retries: 10, retry_delay_ms: 1000 });
    expect(projectSessionResult(events).errors).toHaveLength(3);
    expect(project(events).filter(event => event.type === "session.result" && event.data.errors)).toHaveLength(3);
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
    expect(events.filter(event => event.type === "claude.stream_event").map(event => event.data.event.type)).toEqual(["message_start", "future_transport_event"]);
    expect(events.some(event => event.type === "claude.assistant_snapshot")).toBe(false);
    const persisted = JSON.parse(JSON.stringify(events));
    expect(normalizeClaudeSession(persisted)).toEqual(persisted);
    const unified = project(persisted);
    expect(unified.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["  start", "\r\n"]);
    expect(unified.find(event => event.type === "vendor.unknown").data).toEqual(records.at(-1).data);
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

  it.each([{ is_error: true }, { isError: true }, { success: false }])("preserves a failure %j over a contradictory success signal", failureSignal => {
    const events = parse([{ type: "user", message: { content: [{ type: "tool_result", tool_use_id: "orphan", content: { exact: false }, is_error: false, success: true, ...failureSignal }] } }]);
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
    expect(parse([{ type: "auth_status", isAuthenticating: true, output: [] }])[0].type).toBe("claude.auth_status");
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
    expect(events.find(event => event.type === "assistant.message").data.snapshots).toHaveLength(1);
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
    const events = parse([
      stream({ type: "message_start", message: { id: "late-refusal" } }),
      stream({ type: "content_block_start", index: 0, content_block: { type: "text", text: "Not an answer." } }),
      { type: "assistant", session_id: "audit-session", parent_tool_use_id: null, message: { id: "late-refusal", stop_reason: "refusal", content: null } },
    ]);
    expect(events.filter(event => event.type === "assistant.refusal")).toHaveLength(1);
    expect(events.some(event => event.type === "assistant.message")).toBe(false);
    expect(events.find(event => event.type === "assistant.refusal").data.observedTextEvents).toHaveLength(1);
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
