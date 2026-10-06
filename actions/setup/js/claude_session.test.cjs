import { describe, it, expect } from "vitest";
const { normalizeClaudeSession } = require("./claude_session.cjs");
const { parseClaudeLog } = require("./parse_claude_log.cjs");
const { projectSessionResult, normalizeAgentSession } = require("./agent_session.cjs");
const { success, failure } = require("./fixtures/claude_ci_sessions.cjs");
const { dynamicWorkflow } = require("./fixtures/claude_dynamic_workflow.cjs");

const parse = records => parseClaudeLog(records.map(record => JSON.stringify(record)).join("\n"));
const text = (events, type) =>
  events
    .filter(event => event.type === type)
    .map(event => event.data.content)
    .join("");
const freeze = value => {
  if (value && typeof value === "object") {
    Object.values(value).forEach(freeze);
    Object.freeze(value);
  }
  return value;
};

describe("Claude CI session shapes", () => {
  it("retains initialization additions, source IDs, reasoning, tool errors, and structured native output", () => {
    const events = normalizeClaudeSession(success);
    const init = events[0];
    expect(init.type).toBe("session.init");
    expect(init.data).toMatchObject({
      sourceEngine: "claude",
      sessionId: "session-example",
      permissionMode: "acceptEdits",
      claude_code_version: "2.1.285",
      capabilities: ["thinking_tokens"],
      plugins: [{ name: "example", path: "/workspace/plugins/example" }],
    });
    expect(events.find(event => event.type === "assistant.reasoning").data).toMatchObject({ content: "  Inspect the example.\n", signature: "signature-example" });
    const start = events.find(event => event.type === "tool.execution_start");
    expect(start.data.toolCallId).toBe("tool-example");
    expect(start.uuid).toBe("tool-example");
    expect(start.id).toBeUndefined();
    expect(start.request_id).toBe("request-example");
    const completions = events.filter(event => event.type === "tool.execution_complete");
    expect(completions[0].data).toMatchObject({
      toolCallId: "tool-example",
      toolName: "Bash",
      success: true,
      output: "example\n",
      tool_use_result: { stdout: "example\n", stderr: "", interrupted: false },
    });
    expect(completions[1].data).toMatchObject({ toolCallId: "tool-failed", toolName: "Read", success: false, output: "Example file does not exist." });
    expect(events.filter(event => event.type === "claude.system").map(event => event.data.subtype)).toEqual(["thinking_tokens"]);
    expect(events.filter(event => event.type.startsWith("claude.task_")).map(event => event.data.subtype)).toEqual(["task_started", "task_notification"]);
  });

  it("uses authoritative terminal usage once, including Claude's disjoint cache inputs", () => {
    const parsed = parse(success);
    const result = parsed.logEntries.find(event => event.type === "session.result");
    expect(result.data).toMatchObject({
      numTurns: 47,
      totalCostUsd: 0.6764328,
      durationMs: 237562,
      subtype: "success",
      is_error: false,
      permissionDenials: [],
      modelUsage: { "claude-sonnet-4-6": { cacheReadInputTokens: 1025011, provider: "firstParty" } },
      usage: { input_tokens: 34, output_tokens: 13004, cache_creation_input_tokens: 46338, cache_read_input_tokens: 1025011, input_tokens_include_cache: false, fallback_credit: null },
    });
    expect(parsed.logEntries.filter(event => event.type === "session.result")).toHaveLength(1);
    expect(parsed.markdown).toContain((34 + 46338 + 1025011 + 13004).toLocaleString());
  });

  it("does not mistake subtype success for success when the source reports an API failure", () => {
    const parsed = parse(failure);
    const results = parsed.logEntries.filter(event => event.type === "session.result");
    expect(results[0].data.errors[0]).toMatchObject({ error: "server_error", message: { content: [{ type: "text", text: "Sanitized provider error." }] } });
    expect(text(parsed.logEntries, "assistant.message")).not.toContain("Sanitized provider error.");
    expect(results[1].data).toMatchObject({ subtype: "success", is_error: true, terminal_reason: "api_error", api_error_status: 502 });
    expect(results[1].data.errors).toEqual([{ subtype: "success", is_error: true, terminal_reason: "api_error", api_error_status: 502 }]);
    expect(results[2].data.errors).toEqual([{ error: "UnknownError", error_status: 502, attempt: 1 }]);
    expect(parsed.markdown).toContain("**Errors:**");
    expect(parsed.markdown).toContain("UnknownError");
    expect(projectSessionResult(parsed.logEntries).num_turns).toBe(47);
    expect(parsed.logEntries.filter(event => event.type === "tool.execution_complete")).toEqual([]);
  });

  it("counts repeated response usage snapshots once in a genuinely partial trace", () => {
    const records = success.slice(0, 7);
    const result = projectSessionResult(normalizeClaudeSession(records));
    expect(result.usage).toMatchObject({ input_tokens: 3, output_tokens: 8, cache_creation_input_tokens: 10, cache_read_input_tokens: 20 });
    expect(result.num_turns).toBeUndefined();
    expect(result.duration_ms).toBeUndefined();
    expect(result.total_cost_usd).toBeUndefined();
    const partial = normalizeClaudeSession(records).at(-1);
    expect(partial.data.partial).toBe(true);
    expect(partial.data.subtype).toBeUndefined();
    expect(partial.data.is_error).toBeUndefined();
  });

  it.each([undefined, { output_tokens: 0 }, { input_tokens: 9, output_tokens: -1 }])("reconciles incomplete terminal usage %j field by field", terminalUsage => {
    const records = freeze([
      { type: "assistant", message: { id: "first", content: [], usage: { input_tokens: 2, output_tokens: 3, cache_read_input_tokens: 4 } } },
      { type: "assistant", message: { id: "first", content: [], usage: { output_tokens: 5 } } },
      { type: "assistant", message: { id: "second", content: [], usage: { input_tokens: 6, output_tokens: 7 } } },
      { type: "result", usage: terminalUsage, num_turns: 2, duration_ms: 0, errors: ["terminal"], permission_denials: [] },
    ]);
    const events = normalizeClaudeSession(records);
    const results = events.filter(event => event.type === "session.result");
    expect(results).toHaveLength(1);
    expect(projectSessionResult(events)).toMatchObject({
      num_turns: 2,
      duration_ms: 0,
      errors: ["terminal"],
      permission_denials: [],
      usage: { input_tokens: terminalUsage?.input_tokens ?? 8, output_tokens: terminalUsage?.output_tokens === 0 ? 0 : 12, cache_read_input_tokens: 4, input_tokens_include_cache: false },
    });
    expect(normalizeClaudeSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it("preserves independent response fields with canonical terminal snapshots", () => {
    const events = normalizeClaudeSession([
      { type: "assistant", message: { id: "known", content: [], usage: { input_tokens: 4, output_tokens: 2 } } },
      { type: "session.result", data: { usage: { outputTokens: 0 }, numTurns: 1 } },
      { type: "session.result", data: { durationMs: 0 } },
    ]);
    expect(projectSessionResult(events)).toMatchObject({ num_turns: 1, duration_ms: 0, usage: { input_tokens: 4, output_tokens: 0 } });
  });

  it("is pure, deterministic, idempotent, and JSON-round-trippable", () => {
    const original = structuredClone(success);
    const records = freeze(structuredClone(success));
    const events = normalizeClaudeSession(records);
    expect(records).toEqual(original);
    expect(normalizeClaudeSession(records)).toEqual(events);
    expect(normalizeClaudeSession(events)).toEqual(events);
    expect(normalizeAgentSession(events)).toEqual(events);
    const roundTrip = JSON.parse(JSON.stringify(events));
    expect(normalizeClaudeSession(roundTrip)).toEqual(roundTrip);
  });
});

describe("Claude dynamic workflow transport", () => {
  it("preserves launch metadata, workflow phases, agent progress, task patches and terminal notifications", () => {
    const records = freeze(structuredClone(dynamicWorkflow));
    const events = normalizeClaudeSession(records);
    const completion = events.find(event => event.type === "tool.execution_complete");
    expect(completion.data).toMatchObject({
      toolCallId: "workflow-tool",
      toolName: "Workflow",
      success: true,
      tool_use_result: { status: "async_launched", taskId: "dynamic-task", taskType: "local_workflow", workflowName: "smoke-claude-dynamic", runId: "dynamic-run" },
    });
    expect(events.find(event => event.type === "claude.task_started").data).toMatchObject({
      task_id: "dynamic-task",
      tool_use_id: "workflow-tool",
      task_type: "local_workflow",
      workflow_name: "smoke-claude-dynamic",
      prompt: "PRIVATE_WORKFLOW_SCRIPT",
    });
    const progress = events.filter(event => event.type === "claude.task_progress");
    expect(progress).toHaveLength(2);
    expect(progress[0].data.usage).toEqual({ total_tokens: 0, tool_uses: 0, duration_ms: 0 });
    expect(progress[0].data.workflow_progress[1]).toMatchObject({ agentId: "dynamic-agent", phaseIndex: 1, state: "start", promptPreview: "PRIVATE_AGENT_PROMPT" });
    expect(progress[1].data.workflow_progress[1].state).toBe("done");
    expect(events.find(event => event.type === "claude.task_updated").data.patch).toEqual({ status: "completed", end_time: 1791298671939 });
    expect(events.find(event => event.type === "claude.task_notification")).toMatchObject({
      uuid: "dynamic-completion",
      session_id: "dynamic-session",
      data: { task_id: "dynamic-task", tool_use_id: "workflow-tool", status: "completed", usage: { total_tokens: 250 } },
    });
    expect(events.filter(event => event.type === "claude.background_tasks_changed").map(event => event.data.tasks.length)).toEqual([1, 0]);
    expect(projectSessionResult(events).usage).toMatchObject({ input_tokens: 7, output_tokens: 11 });
    expect(events.filter(event => event.type === "session.result")).toHaveLength(1);
    expect(records).toEqual(dynamicWorkflow);
    expect(normalizeClaudeSession(JSON.parse(JSON.stringify(events)))).toEqual(events);
  });

  it.each(["failed", "stopped", "future-status"])("retains task status %s without turning a launch acknowledgement into task success or a session result", status => {
    const records = dynamicWorkflow.slice(0, -1).filter(record => !["task_progress", "task_updated"].includes(record.subtype));
    const events = normalizeClaudeSession(records.map(record => (record.subtype === "task_notification" ? { ...record, status } : record)));
    expect(events.find(event => event.type === "tool.execution_complete").data.success).toBe(true);
    expect(events.find(event => event.type === "claude.task_notification").data.status).toBe(status);
    expect(events.filter(event => event.type === "session.result")).toEqual([]);
  });

  it("retains partial and orphan task observations without synthesizing a launch, completion, or usage result", () => {
    const event = dynamicWorkflow.find(record => record.subtype === "task_progress");
    const events = normalizeClaudeSession([event]);
    expect(events).toHaveLength(1);
    expect(events[0].type).toBe("claude.task_progress");
    expect(events[0].data.tool_use_id).toBe("workflow-tool");
    expect(projectSessionResult(events)).toBeUndefined();
  });
});

describe("Claude partial streaming transport", () => {
  // SDK stream_event shapes supplement the CI fixtures; sampled CLI runs did not enable partial messages.
  const stream = (event, uuid = "stream-example") => ({
    type: "stream_event",
    event,
    uuid,
    session_id: "stream-session",
    parent_tool_use_id: null,
    timestamp: "2026-10-01T03:56:00.000Z",
  });

  it("retains ordered text/reasoning deltas and exact tool arguments without duplicating a snapshot", () => {
    const events = normalizeClaudeSession([
      stream({ type: "message_start", message: { id: "message-streamed", content: [], usage: { input_tokens: 2, output_tokens: 0, cache_read_input_tokens: 4 } } }),
      stream({ type: "content_block_start", index: 0, content_block: { type: "thinking", thinking: "" } }),
      stream({ type: "content_block_delta", index: 0, delta: { type: "thinking_delta", thinking: "  Think.\n" } }, "reasoning-chunk"),
      stream({ type: "content_block_delta", index: 0, delta: { type: "signature_delta", signature: "signature-streamed" } }),
      stream({ type: "content_block_stop", index: 0 }),
      stream({ type: "content_block_start", index: 1, content_block: { type: "text", text: "" } }),
      stream({ type: "content_block_delta", index: 1, delta: { type: "text_delta", text: " Answer" } }),
      stream({ type: "content_block_delta", index: 1, delta: { type: "text_delta", text: " " } }),
      stream({ type: "content_block_delta", index: 1, delta: { type: "text_delta", text: "😀\r\n" } }),
      stream({ type: "content_block_start", index: 2, content_block: { type: "tool_use", id: "stream-tool", name: "Bash", input: {} } }),
      stream({ type: "content_block_delta", index: 2, delta: { type: "input_json_delta", partial_json: '{"command":"printf ' } }),
      stream({ type: "content_block_delta", index: 2, delta: { type: "input_json_delta", partial_json: "'example\\\\n'\"}" } }),
      stream({ type: "content_block_stop", index: 2 }),
      stream({ type: "message_delta", delta: { stop_reason: "tool_use" }, usage: { output_tokens: 9 } }),
      {
        type: "assistant",
        session_id: "stream-session",
        parent_tool_use_id: null,
        uuid: "snapshot-example",
        message: {
          id: "message-streamed",
          content: [
            { type: "thinking", thinking: "  Think.\n", signature: "signature-streamed" },
            { type: "text", text: " Answer 😀\r\n" },
            { type: "tool_use", id: "stream-tool", name: "Bash", input: { command: "printf 'example\\n'" } },
          ],
          usage: { input_tokens: 2, output_tokens: 9, cache_read_input_tokens: 4 },
        },
      },
    ]);
    expect(text(events, "assistant.message")).toBe(" Answer 😀\r\n");
    expect(text(events, "assistant.reasoning")).toBe("  Think.\n");
    const start = events.find(event => event.type === "tool.execution_start");
    expect(start.data).toMatchObject({ toolCallId: "stream-tool", input: { command: "printf 'example\\n'" }, argumentText: '{"command":"printf \'example\\\\n\'"}' });
    expect(events.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(events.filter(event => event.type === "tool.execution_complete")).toHaveLength(0);
    expect(events.find(event => event.uuid === "reasoning-chunk" && event.type === "assistant.reasoning").timestamp).toBe("2026-10-01T03:56:00.000Z");
    expect(events.find(event => event.type === "claude.assistant_snapshot").data.message.content[1].text).toBe(" Answer 😀\r\n");
    expect(projectSessionResult(events).usage).toMatchObject({ input_tokens: 2, output_tokens: 9, cache_read_input_tokens: 4 });
  });

  it("keeps dangling starts and incomplete JSON arguments without inventing results or IDs", () => {
    const events = normalizeClaudeSession([
      stream({ type: "content_block_start", index: 0, content_block: { type: "tool_use", name: "Bash", input: {} } }),
      stream({ type: "content_block_delta", index: 0, delta: { type: "input_json_delta", partial_json: '{"command":"partial' } }),
      stream({ type: "content_block_stop", index: 0 }),
      stream({ type: "content_block_delta", index: 1, delta: { type: "text_delta", text: "Recovered" } }),
    ]);
    const start = events.find(event => event.type === "tool.execution_start");
    expect(start.data.toolCallId).toBeUndefined();
    expect(start.id).toBeUndefined();
    expect(start.data.argumentText).toBe('{"command":"partial');
    expect(start.data.input).toEqual({});
    expect(text(events, "assistant.message")).toBe("Recovered");
    expect(events.some(event => event.type === "tool.execution_complete" || event.type === "session.result")).toBe(false);
  });

  it("recovers independently observed deltas when the beginning of the stream is missing", () => {
    const events = normalizeClaudeSession([stream({ type: "content_block_delta", index: 1, delta: { type: "text_delta", text: " \n" } }), stream({ type: "content_block_delta", index: 1, delta: { type: "text_delta", text: "" } })]);
    expect(text(events, "assistant.message")).toBe(" \n");
    expect(events.filter(event => event.type === "assistant.message")).toHaveLength(2);
    expect(events.some(event => event.type === "session.result")).toBe(false);
  });

  it("retains unseen snapshot suffixes, stream errors, and provider errors separately from answers", () => {
    const events = normalizeClaudeSession([
      stream({ type: "message_start", message: { id: "msg-partial" } }),
      stream({ type: "content_block_start", index: 0, content_block: { type: "text", text: "Part" } }),
      { type: "assistant", session_id: "stream-session", parent_tool_use_id: null, message: { id: "msg-partial", content: [{ type: "text", text: "Partial answer" }] } },
      stream({ type: "error", error: { type: "overloaded_error", message: "Provider unavailable" } }),
      { type: "assistant", error: "authentication_failed", message: { content: [{ type: "text", text: "Authentication rejected" }] } },
    ]);
    expect(text(events, "assistant.message")).toBe("Partial answer");
    expect(events.filter(event => event.type === "session.result").map(event => event.data.errors)).toEqual([
      [{ type: "overloaded_error", message: "Provider unavailable" }],
      [{ error: "authentication_failed", message: { content: [{ type: "text", text: "Authentication rejected" }] } }],
    ]);
  });
});

describe("Claude preservation and privacy boundaries", () => {
  it("keeps native events and mixed legacy records, and never exposes user prompts by default", () => {
    const native = { type: "example.telemetry", id: "native-id", parentId: null, timestamp: 0, data: { arbitrary: { false: false, zero: 0, null: null } } };
    const parsed = parse([
      native,
      { type: "user", uuid: "user-example", message: { content: [{ type: "text", text: "PRIVATE_USER_PROMPT_SENTINEL" }] } },
      { type: "assistant", message: { content: [{ type: "text", text: "Public answer" }] } },
      { type: "result", usage: { input_tokens: 0, output_tokens: 0 }, total_cost_usd: 0, permission_denials: [] },
      { type: "example.trailing", data: { source: true } },
    ]);
    expect(parsed.logEntries[0]).toEqual(native);
    expect(parsed.logEntries.find(event => event.type === "user.message").data.content).toBe("PRIVATE_USER_PROMPT_SENTINEL");
    expect(parsed.markdown).not.toContain("PRIVATE_USER_PROMPT_SENTINEL");
    expect(parsed.markdown).toContain("Public answer");
    expect(parsed.markdown).toContain("**Total Cost:** $0.0000");
    expect(parsed.logEntries.some(event => event.type === "result")).toBe(false);
  });

  it("retains typed/falsy orphan tool outputs and explicit errors with their supplied IDs", () => {
    for (const output of [false, 0, "", null, [], { nested: [0, false] }]) {
      const events = normalizeClaudeSession([{ type: "user", id: "record-id", message: { content: [{ type: "tool_result", tool_use_id: "orphan-id", content: output, is_error: true, error: { code: "example" }, duration_ms: 0 }] } }]);
      expect(events).toHaveLength(1);
      expect(events[0].id).toBe("record-id");
      expect(events[0].data).toMatchObject({ toolCallId: "orphan-id", success: false, durationMs: 0, error: { code: "example" } });
      expect(events[0].data.output).toEqual(output);
    }
  });

  it("leaves unavailable metrics and outcomes unknown", () => {
    const events = normalizeClaudeSession([
      { type: "user", message: { content: [{ type: "tool_result", content: false }] } },
      { type: "result", usage: { input_tokens: -1, output_tokens: "unknown" } },
    ]);
    expect(events[0].data.success).toBeUndefined();
    expect(events[0].data.toolCallId).toBeUndefined();
    expect(events[1].data.numTurns).toBeUndefined();
    expect(events[1].data.durationMs).toBeUndefined();
    expect(events[1].data.totalCostUsd).toBeUndefined();
    expect(events[1].data.usage.input_tokens).toBeUndefined();
    expect(events[1].data.usage.output_tokens).toBeUndefined();
  });

  it("preserves permission denials and subtype-only max-turn errors without requiring an environment limit", () => {
    const parsed = parse([
      { type: "result", subtype: "error_max_turns", is_error: true, num_turns: 5, permission_denials: [{ tool_name: "Bash", tool_use_id: "denied-id", tool_input: { command: "example" } }] },
      { type: "claude.trailing", data: { status: "partial" } },
    ]);
    expect(parsed.maxTurnsHit).toBe(true);
    expect(parsed.logEntries[0].data.permissionDenials).toEqual([{ tool_name: "Bash", tool_use_id: "denied-id", tool_input: { command: "example" } }]);
    expect(parsed.logEntries[0].data.errors).toEqual([{ subtype: "error_max_turns", is_error: true }]);
  });

  it("recovers after malformed JSONL and refuses unrelated JSON without leaking previews", () => {
    const parsed = parseClaudeLog('{"type":"user","message":{"content":"PRIVATE_PROMPT"}}\n{"type":"assistant",\n{"type":"assistant","message":{"content":"valid neighbor"}}');
    expect(text(parsed.logEntries, "assistant.message")).toBe("valid neighbor");
    expect(parsed.markdown).not.toContain("PRIVATE_PROMPT");
    const unrelated = parseClaudeLog('{"private":"UNRELATED_PROMPT"}');
    expect(unrelated.logEntries).toEqual([]);
    expect(unrelated.markdown).toContain("not recognized");
    expect(unrelated.markdown).not.toContain("UNRELATED_PROMPT");
  });
});
