import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { normalizeCodexSession } from "./codex_session.cjs";
import { parseCodexLog, isCodexJsonlFormat } from "./parse_codex_log.cjs";
import { projectSessionInitialization, projectSessionResult, selectSessionResult, sessionTokenTotal } from "./agent_session.cjs";
import { convertCopilotEventsToLegacyLogEntries } from "./log_parser_shared.cjs";

const fixture = name => readFileSync(new URL(`./test_data/${name}.jsonl`, import.meta.url), "utf8");
const ofType = (events, type) => events.filter(event => event.type === type);
const parse = records => parseCodexLog(records.map(record => (typeof record === "string" ? record : JSON.stringify(record))).join("\n")).logEntries;

describe("Codex real CI trace regression", () => {
  // Sanitized excerpts: github/gh-aw/actions/runs/36909965579 (Smoke Codex) and
  // /36850958249 (Daily Documentation Updater), plus /37096302208 (Codex 0.159.3
  // tool-free Smoke Codex turn), artifact `agent`/agent-stdio.log.
  // IDs, commands, arguments, outputs and messages replaced; shape and usage retained.
  it("preserves the live command lifecycle, model, exact text and failed exit", () => {
    const events = parseCodexLog(fixture("codex_ci_smoke")).logEntries;
    expect(events.map(event => event.type)).toEqual(["session.init", "session.result", "turn.started", "assistant.message", "tool.execution_start", "tool.execution_complete", "assistant.message", "assistant.message", "session.result"]);
    expect(events[0].data).toMatchObject({ sourceEngine: "codex", model: "gpt-5.3-codex", sessionId: "sanitized-smoke-thread" });
    expect(events[3].data.content).toBe("  Inspecting the example.\n");
    expect(events[4].data).toMatchObject({ toolCallId: "item_2", toolName: "bash", command: "example-check --version", exit_code: null });
    expect(events[5].data).toMatchObject({ toolCallId: "item_2", success: false, exitCode: 127, output: "example-check: command not found\n" });
    const result = selectSessionResult(events);
    expect(result).toMatchObject({
      numTurns: 1,
      errors: ["Model metadata unavailable; using fallback metadata."],
      usage: { input_tokens: 35078, output_tokens: 1638, cache_read_input_tokens: 18176, cache_creation_input_tokens: 0, reasoning_output_tokens: 0 },
    });
    expect(sessionTokenTotal(result.usage)).toBe(36716);
    expect(result.totalCostUsd).toBeUndefined();
    expect(result.durationMs).toBeUndefined();
  });

  it("retains structured MCP results, null metadata and separate equal diagnostics", () => {
    const events = parseCodexLog(fixture("codex_ci_mcp")).logEntries;
    const start = ofType(events, "tool.execution_start").find(event => event.data.toolName === "search_pull_requests");
    const complete = ofType(events, "tool.execution_complete").find(event => event.data.toolName === "search_pull_requests");
    expect(start.data).toMatchObject({ toolCallId: "item_6", mcpServerName: "github", result: null, error: null });
    expect(complete.data).toMatchObject({
      toolCallId: "item_6",
      success: true,
      error: null,
      result: { content: [{ type: "text", text: '{"items":[],"totalCount":0}' }], structured_content: null },
    });
    expect(complete.data.output).toEqual(complete.data.result);
    expect(selectSessionResult(events).errors).toEqual(["An example configuration setting is ignored.", "An example configuration setting is ignored.", "Model metadata unavailable; using fallback metadata."]);
    expect(selectSessionResult(events).usage).toMatchObject({ input_tokens: 117247, output_tokens: 1000, cache_read_input_tokens: 90112, cache_creation_input_tokens: 0 });
  });

  it("preserves the current tool-free turn without treating CLI completion as completed smoke checks", () => {
    const content = fixture("codex_ci_no_tools");
    const events = parseCodexLog(content).logEntries;
    expect(events.map(event => event.type)).toEqual(["session.init", "session.result", "turn.started", "assistant.message", "assistant.message", "session.result"]);
    expect(events[0].data).toMatchObject({ sourceEngine: "codex", model: "gpt-5.3-codex", sessionId: "sanitized-no-tools-thread" });
    expect(events[1].data.status).toBeUndefined();
    expect(ofType(events, "assistant.message").map(event => event.data.content)).toEqual(["I will run the required checks now.", "I cannot execute the required checks because the tools are unavailable.\n\nNo outputs were written."]);
    expect(ofType(events, "tool.execution_start")).toEqual([]);
    expect(ofType(events, "tool.execution_complete")).toEqual([]);
    const result = selectSessionResult(events);
    expect(result).toMatchObject({
      status: "completed",
      numTurns: 1,
      errors: ["Model metadata unavailable; using fallback metadata."],
      usage: { input_tokens: 16847, output_tokens: 167, cache_read_input_tokens: 8576, cache_creation_input_tokens: 0, cached_input_tokens: 8576, cache_write_input_tokens: 0, reasoning_output_tokens: 0 },
    });
    expect(sessionTokenTotal(result.usage)).toBe(17014);
    expect(result.totalCostUsd).toBeUndefined();
    expect(result.durationMs).toBeUndefined();
    const compatibility = { type: "result", num_turns: 1, usage: { input_tokens: 16847, output_tokens: 167, cache_read_input_tokens: 8576, cache_creation_input_tokens: 0, reasoning_output_tokens: 0 } };
    expect(selectSessionResult(parseCodexLog(`${content}${JSON.stringify(compatibility)}\n`).logEntries)).toEqual(result);
    expect(normalizeCodexSession(events)).toEqual(events);
  });

  it("keeps a real-shape truncated command dangling even with a session failure", () => {
    const lines = fixture("codex_ci_smoke").split("\n");
    const stop = lines.findIndex(line => line.includes('"type":"item.started"'));
    const events = parseCodexLog(lines.slice(0, stop + 1).join("\n")).logEntries;
    expect(ofType(events, "tool.execution_start")).toHaveLength(1);
    expect(ofType(events, "tool.execution_complete")).toEqual([]);
    expect(selectSessionResult(events).numTurns).toBeUndefined();
    expect(selectSessionResult(events).usage).toBeUndefined();
  });
});

describe("Codex normalization contract", () => {
  it.each(["future_item"])("retains native %s observations without losing payloads", type => {
    const record = { type: "item.completed", timestamp: 0, item: { id: "item_0", type, status: "failed", extension: { retained: true } } };
    const events = normalizeCodexSession([record]);
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({ type: "codex.item_snapshot", timestamp: 0, data: { item: record.item } });
    expect(normalizeCodexSession(events)).toEqual(events);
  });

  it("reports the successful final attempt while retaining historical retry errors and usage", () => {
    const events = normalizeCodexSession([
      { type: "thread.started", thread_id: "first" },
      { type: "turn.failed", error: { message: "transient" } },
      { type: "thread.started", thread_id: "retry" },
      { type: "turn.completed", usage: { input_tokens: 10, output_tokens: 1 } },
    ]);
    expect(selectSessionResult(events)).toMatchObject({ status: "completed", sourceType: "turn.completed", errors: [{ message: "transient" }], usage: { input_tokens: 10 } });
  });

  it("recognizes JSON arrays and recovers adjacent supported records after malformed JSONL", () => {
    const records = [
      null,
      42,
      {},
      { type: "thread.started", thread_id: "thread" },
      { type: "assistant", message: { content: [{ type: "text", text: " \n" }] } },
      { type: "vendor.progress", id: "opaque", data: { extra: false } },
      { type: "item.completed", item: { id: "message", type: "agent_message", text: "Unicode 🐙\r\n" } },
    ];
    const expected = normalizeCodexSession(records);
    expect(parseCodexLog(JSON.stringify(records, null, 2)).logEntries).toEqual(expected);
    expect(isCodexJsonlFormat([JSON.stringify(records)])).toBe(true);
    expect(parse(["DEBUG ignored", "{broken", ...records.filter(record => record !== null), '{"type":"item.completed"'])).toEqual(expected);
    expect(parseCodexLog('{"type":"unrecognized","text":"not a session"}').logEntries).toEqual([]);
  });

  it("preserves metadata, native additions and original record IDs on expansions", () => {
    const records = [
      { type: "thread.started", id: "event-init", timestamp: 0, thread_id: "thread", tools: [], mcp_servers: [], slash_commands: [], model_info: { vendor: "example" }, extra: null },
      {
        type: "item.completed",
        id: "event-tool",
        parentId: "event-init",
        timestamp: "native",
        item: { id: "call", type: "mcp_tool_call", server: "", tool: "lookup", arguments: false, result: 0, duration_ms: 0, status: "completed", extension: [] },
      },
    ];
    const original = structuredClone(records);
    const events = normalizeCodexSession(records);
    expect(events[0]).toMatchObject({ id: "event-init", timestamp: 0, extra: null, data: { tools: [], mcpServers: [], slashCommands: [], modelInfo: { vendor: "example" } } });
    expect(events[1]).toMatchObject({ id: "event-tool", parentId: "event-init", timestamp: "native", data: { toolCallId: "call", toolName: "lookup", mcpServerName: "", input: false, extension: [] } });
    expect(events[2]).toMatchObject({ id: "event-tool", data: { toolCallId: "call", success: true, output: 0, result: 0, durationMs: 0 } });
    expect(records).toEqual(original);
    expect(JSON.parse(JSON.stringify(events))).toEqual(JSON.parse(JSON.stringify(normalizeCodexSession(events))));
    events[1].item.extension.push("mutated output");
    expect(records).toEqual(original);
  });

  it.each([false, 0, "", null, [], {}])("retains falsy and structured outputs: %j", value => {
    const events = normalizeCodexSession([{ type: "item.completed", item: { id: "call", type: "mcp_tool_call", tool: "lookup", arguments: value, result: value, status: "completed" } }]);
    expect(events[0].data.input).toEqual(value);
    expect(events[1].data.output).toEqual(value);
    expect(events[1].data.result).toEqual(value);
  });

  it.each([{ status: "failed" }, { status: "error" }, { status: "completed", isError: true }, { status: "completed", result: { isError: true } }, { status: "completed", error: { message: "bad" } }, { status: "completed", exit_code: 2 }])(
    "explicit failure overrides otherwise successful completion: %j",
    outcome => {
      const events = normalizeCodexSession([{ type: "item.completed", item: { id: "call", type: "command_execution", command: "example", ...outcome } }]);
      expect(events.at(-1).data.success).toBe(false);
      expect(ofType(events, "session.result")).toEqual([]);
    }
  );

  it("does not fabricate success or invocation arguments for orphan completions", () => {
    const events = normalizeCodexSession([
      { type: "item.completed", item: { id: "orphan", type: "command_execution", aggregated_output: "", exit_code: 1 } },
      { type: "item.completed", item: { id: "unknown", type: "mcp_tool_call", result: false } },
      { type: "item.completed", item: { id: "named-orphan", type: "mcp_tool_call", tool: "lookup", result: null, status: "completed" } },
    ]);
    expect(events.map(event => event.type)).toEqual(["tool.execution_complete", "tool.execution_complete", "tool.execution_complete"]);
    expect(events[0].data).toMatchObject({ toolCallId: "orphan", output: "", success: false });
    expect(events[0].data.input).toBeUndefined();
    expect(events[1].data.success).toBeUndefined();
    expect(events[1].data.toolName).toBeUndefined();
    expect(events[2].data).toMatchObject({ toolName: "lookup", success: true, output: null });
    expect(events[2].data.input).toBeUndefined();
  });

  it("preserves observed starts even when arguments or a command are absent", () => {
    const events = normalizeCodexSession([
      { type: "item.started", item: { id: "missing-command", type: "command_execution", status: "in_progress" } },
      { type: "item.started", item: { id: "missing-arguments", type: "mcp_tool_call", tool: "lookup", status: "in_progress" } },
    ]);
    expect(events.map(event => event.type)).toEqual(["tool.execution_start", "tool.execution_start"]);
    expect(events[0].data.input).toBeUndefined();
    expect(events[1].data.input).toBeUndefined();
  });

  it("does not synthesize completion from unfinished items or terminal failures", () => {
    const events = normalizeCodexSession([
      { type: "item.started", item: { id: "pending", type: "command_execution", command: "example", status: "in_progress" } },
      { type: "item.completed", item: { id: "pending", type: "command_execution", status: "pending" } },
      { type: "turn.failed", id: "failed-turn" },
    ]);
    expect(ofType(events, "tool.execution_complete")).toEqual([]);
    expect(events.at(-1)).toMatchObject({ id: "failed-turn", type: "session.result", data: { status: "failed", sourceType: "turn.failed" } });
    expect(events.at(-1).data.numTurns).toBeUndefined();
  });

  it("reconciles message snapshots without duplicating final answers and keeps incomplete text", () => {
    const events = normalizeCodexSession([
      { type: "item.started", timestamp: 1, item: { id: "message", type: "agent_message", text: " " } },
      { type: "item.updated", timestamp: 2, item: { id: "message", type: "agent_message", text: " partial" } },
      { type: "item.started", item: { id: "tool", type: "command_execution", command: "example" } },
      { type: "item.completed", timestamp: 3, item: { id: "message", type: "agent_message", text: " final\n" } },
      { type: "item.completed", timestamp: 3, item: { id: "message", type: "agent_message", text: " final\n" } },
      { type: "item.started", item: { id: "partial", type: "reasoning", text: "" } },
      { type: "item.updated", timestamp: 4, item: { id: "partial", type: "reasoning", summary: " \r\n" } },
    ]);
    expect(ofType(events, "assistant.message").map(event => event.data.content)).toEqual([" final\n"]);
    expect(ofType(events, "assistant.reasoning").map(event => event.data.content)).toEqual([" \r\n"]);
    expect(ofType(events, "codex.item_snapshot")).toHaveLength(4);
    expect(ofType(events, "codex.item_snapshot")[0]).toMatchObject({ timestamp: 1, data: { item: { text: " " } } });
    expect(events.findIndex(event => event.type === "tool.execution_start")).toBeLessThan(events.findIndex(event => event.type === "assistant.message"));
  });

  it("retains partial text when a later completion does not expose text", () => {
    const events = normalizeCodexSession([
      { type: "item.updated", item: { id: "partial", type: "agent_message", text: " observed " } },
      { type: "item.completed", item: { id: "partial", type: "agent_message" } },
    ]);
    expect(events[0]).toMatchObject({ type: "assistant.message", data: { content: " observed " } });
    expect(events[1].type).toBe("codex.item_snapshot");
  });

  it("counts distinct per-turn reports once and accumulates all reported native cache fields", () => {
    const first = {
      type: "turn.completed",
      id: "event-turn-1",
      turn_id: "turn-1",
      timestamp: 0,
      usage: { input_tokens: 10, cached_input_tokens: 4, cache_write_input_tokens: 0, output_tokens: 2, reasoning_output_tokens: 1, native: false },
    };
    const events = normalizeCodexSession([
      first,
      structuredClone(first),
      { type: "turn.completed", id: "event-turn-2", turn_id: "turn-2", usage: { input_tokens: 3, output_tokens: 0, reasoning_output_tokens: 0 } },
      { type: "vendor.after_result", data: { done: true } },
    ]);
    expect(ofType(events, "session.result")).toHaveLength(2);
    expect(events[0]).toMatchObject({ id: "event-turn-1", timestamp: 0, usage: first.usage, data: { numTurns: 1 } });
    expect(selectSessionResult(events)).toMatchObject({
      numTurns: 2,
      usage: { input_tokens: 13, output_tokens: 2, cache_read_input_tokens: 4, cache_creation_input_tokens: 0, cached_input_tokens: 4, reasoning_output_tokens: 1, native: false },
    });
    expect(sessionTokenTotal(selectSessionResult(events).usage)).toBe(15);
  });

  it("preserves usage aliases and prefers a supplied canonical zero over its alias", () => {
    const events = normalizeCodexSession([
      { type: "turn.completed", turn_id: "first", usage: { input_tokens: 0, inputTokens: 8, outputTokens: 2, cacheReadInputTokens: 0 } },
      { type: "turn.completed", turn_id: "second", usage: { inputTokens: 3, output_tokens: 1 } },
    ]);
    expect(selectSessionResult(events).usage).toMatchObject({ input_tokens: 3, inputTokens: 3, output_tokens: 3, outputTokens: 3, cache_read_input_tokens: 0, cacheReadInputTokens: 0 });
    expect(events[0].usage.inputTokens).toBe(8);
  });

  it("uses exact call IDs rather than pairing concurrent tools by name", () => {
    const events = normalizeCodexSession([
      { type: "item.started", item: { id: "first", type: "mcp_tool_call", tool: "lookup", arguments: false } },
      { type: "item.started", item: { id: "second", type: "mcp_tool_call", tool: "lookup", arguments: 0 } },
      { type: "item.completed", item: { id: "second", type: "mcp_tool_call", result: false, status: "completed" } },
      { type: "item.completed", item: { id: "orphan", type: "mcp_tool_call", result: 0, status: "failed" } },
      { type: "turn.failed", error: { message: "stopped" } },
    ]);
    const completions = ofType(events, "tool.execution_complete");
    expect(completions[0].data).toMatchObject({ toolCallId: "second", toolName: "lookup", success: true, output: false });
    expect(completions[1].data).toMatchObject({ toolCallId: "orphan", success: false, output: 0 });
    expect(completions[1].data.toolName).toBeUndefined();
    expect(completions.some(event => event.data.toolCallId === "first")).toBe(false);
  });

  it("keeps duplicate tool snapshots without reporting another execution", () => {
    const completion = { type: "item.completed", item: { id: "call", type: "mcp_tool_call", tool: "lookup", arguments: {}, result: false, status: "completed" } };
    const events = normalizeCodexSession([completion, structuredClone(completion)]);
    expect(ofType(events, "tool.execution_start")).toHaveLength(1);
    expect(ofType(events, "tool.execution_complete")).toHaveLength(1);
    expect(events.at(-1)).toMatchObject({ type: "codex.item_snapshot", data: { item: completion.item } });
  });

  it("correlates mixed legacy starts and canonical completions without dropping extensions", () => {
    const events = normalizeCodexSession([
      { type: "assistant", message: { content: [{ type: "tool_use", id: "legacy", name: "lookup", input: false }] } },
      { type: "vendor.progress", data: { position: 0 } },
      { type: "user", message: { content: [{ type: "tool_result", tool_use_id: "legacy", content: 0, is_error: false }] } },
      { type: "result", usage: { output_tokens: 0 }, errors: [] },
    ]);
    expect(events.map(event => event.type)).toEqual(["tool.execution_start", "vendor.progress", "tool.execution_complete", "session.result"]);
    expect(events[2].data).toMatchObject({ toolCallId: "legacy", toolName: "lookup", success: true, output: 0 });
    expect(selectSessionResult(events)).toMatchObject({ usage: { output_tokens: 0 }, errors: [] });
  });

  it("applies an authoritative native accounting snapshot before a later raw turn contribution", () => {
    const events = normalizeCodexSession([
      { type: "session.result", data: { numTurns: 2, usage: { inputTokens: 10, outputTokens: 1 } } },
      { type: "turn.completed", usage: { input_tokens: 3, output_tokens: 1 } },
    ]);
    expect(selectSessionResult(events)).toMatchObject({ numTurns: 3, usage: { input_tokens: 13, output_tokens: 2 } });
  });

  it("separates item IDs reused after a new native thread and does not invent missing metrics", () => {
    const events = normalizeCodexSession([
      { type: "thread.started", thread_id: "first" },
      { type: "item.completed", item: { id: "item_0", type: "agent_message", text: "first" } },
      { type: "turn.completed", id: "turn", usage: { input_tokens: -1, output_tokens: "2", cached_input_tokens: null } },
      { type: "thread.started", thread_id: "retry" },
      { type: "item.completed", item: { id: "item_0", type: "agent_message", text: "retry" } },
      { type: "turn.completed", id: "turn" },
    ]);
    expect(ofType(events, "assistant.message").map(event => event.data.content)).toEqual(["first", "retry"]);
    expect(selectSessionResult(events)).toMatchObject({ numTurns: 2 });
    expect(selectSessionResult(events).usage).toBeUndefined();
  });

  it("keeps authoritative native results without summing snapshots", () => {
    const events = normalizeCodexSession([
      { type: "turn.completed", usage: { input_tokens: 8, output_tokens: 2, cached_input_tokens: 3 } },
      { type: "session.result", id: "snapshot", data: { usage: { input_tokens: 9 }, totalCostUsd: 0, durationMs: 0, errors: [], permissionDenials: [] } },
      { type: "vendor.after", data: {} },
    ]);
    expect(selectSessionResult(events)).toMatchObject({ numTurns: 1, usage: { input_tokens: 9, output_tokens: 2, cache_read_input_tokens: 3 }, totalCostUsd: 0, durationMs: 0, errors: [], permissionDenials: [] });
    expect(normalizeCodexSession(events)).toEqual(events);
  });

  it("retains terminal errors and retry observations without treating them as answers", () => {
    const records = [
      { type: "error", id: "retry-1", message: "" },
      { type: "error", id: "retry-2", message: "" },
      { type: "error", id: "retry-2", message: "" },
      { type: "turn.failed", turn_id: "failed", error: { message: "provider error", details: { retryable: false } } },
      { type: "item.completed", item: { id: "error-item", type: "error", message: null } },
    ];
    const events = normalizeCodexSession(records);
    expect(ofType(events, "assistant.message")).toEqual([]);
    expect(selectSessionResult(events).errors).toEqual(["", "", { message: "provider error", details: { retryable: false } }, null]);
    expect(events[2]).toMatchObject({ turn_id: "failed", data: { status: "failed" } });
  });
});

describe("Codex native tools and nested sessions", () => {
  // collab_tool_call is the upstream codex-rs/exec/src/exec_events.rs shape.
  // The sampled CI runs above did not expose native collaboration items.
  it("normalizes collaboration calls, descendant identity and completed state messages", () => {
    const events = parseCodexLog(fixture("codex_collab")).logEntries;
    expect(ofType(events, "tool.execution_start")).toHaveLength(7);
    expect(ofType(events, "tool.execution_complete")).toHaveLength(7);
    expect(ofType(events, "subagent.started").map(event => [event.agentId, event.data.parentId])).toEqual([
      ["child", "parent"],
      ["grandchild", "child"],
    ]);
    expect(ofType(events, "subagent.completed").map(event => [event.agentId, event.data.toolCallId])).toEqual([
      ["grandchild", "item_0"],
      ["child", "item_0"],
    ]);
    expect(ofType(events, "assistant.message").map(event => [event.agentId, event.data.sessionId, event.data.parentSessionId, event.data.content])).toEqual([
      ["grandchild", "grandchild", "child", "  Detail checked.\n"],
      ["child", "child", "parent", "Inspection complete.\n"],
    ]);
    const spawn = ofType(events, "tool.execution_start")[0];
    expect(spawn.data.input).toEqual({ senderThreadId: "parent", receiverThreadIds: [], prompt: "Inspect the example.\n" });
    const running = ofType(events, "tool.execution_complete").find(event => event.data.toolName === "wait" && event.data.sessionId === "parent");
    expect(running.data).toMatchObject({ success: true, output: { agentsStates: { child: { status: "running", message: null } } } });
    expect(selectSessionResult(events)).toMatchObject({ numTurns: 1, usage: { input_tokens: 10, output_tokens: 2 } });
    expect(normalizeCodexSession(events)).toEqual(events);
  });

  it("does not mistake a failed spawn, running wait or shutdown for an agent completion", () => {
    const events = normalizeCodexSession([
      { type: "item.completed", item: { id: "spawn", type: "collab_tool_call", tool: "spawn_agent", sender_thread_id: "parent", receiver_thread_ids: [], agents_states: {}, status: "failed" } },
      {
        type: "item.completed",
        item: { id: "wait", type: "collab_tool_call", tool: "wait", sender_thread_id: "parent", receiver_thread_ids: ["unknown"], agents_states: { unknown: { status: "errored", message: "Child failed." } }, status: "completed" },
      },
    ]);
    expect(ofType(events, "subagent.started")).toEqual([]);
    expect(ofType(events, "subagent.completed")).toEqual([]);
    expect(ofType(events, "assistant.message")).toEqual([]);
    expect(ofType(events, "subagent.failed")[0]).toMatchObject({ agentId: "unknown", data: { error: "Child failed." } });
    expect(ofType(events, "tool.execution_complete").map(event => event.data.success)).toEqual([false, true]);
    expect(selectSessionResult(events)).toBeUndefined();
  });

  it("does not infer descendant ancestry from started or failed spawns", () => {
    const events = normalizeCodexSession([
      { type: "item.started", item: { id: "spawn", type: "collab_tool_call", tool: "spawn_agent", sender_thread_id: "parent", receiver_thread_ids: ["child"] } },
      { type: "item.completed", item: { id: "spawn", type: "collab_tool_call", tool: "spawn_agent", sender_thread_id: "parent", receiver_thread_ids: ["child"], status: "failed" } },
      { type: "thread.started", thread_id: "child", model: "child-model" },
      { type: "item.completed", thread_id: "child", item: { id: "answer", type: "agent_message", text: "Observed child session." } },
    ]);
    expect(events.find(event => event.type === "session.init" && event.data.sessionId === "child").data.parentSessionId).toBeUndefined();
    expect(events.find(event => event.type === "assistant.message").data).toMatchObject({ sessionId: "child", content: "Observed child session." });
    expect(events.find(event => event.type === "assistant.message").data.parentSessionId).toBeUndefined();
  });

  it("preserves descendant failures without replacing parent accounting or initialization", () => {
    const events = [
      { type: "session.init", data: { sourceEngine: "codex", sessionId: "parent", model: "parent-model" } },
      { type: "session.init", data: { sessionId: "child", parentSessionId: "parent", model: "child-model" } },
      { type: "session.result", data: { sourceEngine: "codex", sessionId: "parent", status: "completed", numTurns: 1, usage: { input_tokens: 10, output_tokens: 2 } } },
      { type: "session.result", data: { sessionId: "child", parentSessionId: "parent", status: "failed", numTurns: 9, usage: { input_tokens: 100, output_tokens: 20 }, errors: ["child failed"] } },
    ];
    expect(selectSessionResult(events)).toMatchObject({ status: "failed", numTurns: 1, usage: { input_tokens: 10, output_tokens: 2 }, errors: ["child failed"] });
    expect(projectSessionResult(events)).toMatchObject({ status: "failed", num_turns: 1, errors: ["child failed"], usage: { input_tokens: 10, output_tokens: 2 } });
    expect(projectSessionInitialization(events)).toMatchObject({ model: "parent-model", session_id: "parent" });
  });

  it("starts observed receivers without requiring an agent-state snapshot and keeps revised final messages", () => {
    const collab = (id, tool, states) => ({ type: "item.completed", item: { id, type: "collab_tool_call", tool, sender_thread_id: "parent", receiver_thread_ids: ["child"], agents_states: states, status: "completed" } });
    const events = normalizeCodexSession([collab("spawn", "spawn_agent", {}), collab("wait", "wait", { child: { status: "completed", message: "" } }), collab("wait", "wait", { child: { status: "completed", message: "Revised.\n" } })]);
    expect(ofType(events, "subagent.started")).toHaveLength(1);
    expect(ofType(events, "subagent.completed")).toHaveLength(1);
    expect(ofType(events, "assistant.message").map(event => event.data.content)).toEqual(["Revised.\n"]);
    expect(ofType(events, "codex.agent_snapshot")[0].data.state.message).toBe("");
    expect(ofType(events, "tool.execution_complete")).toHaveLength(2);
    expect(ofType(events, "codex.item_snapshot")).toHaveLength(1);
  });

  it("isolates interleaved parent, child and grandchild item IDs, invocation metadata and usage", () => {
    const records = [
      { type: "thread.started", thread_id: "parent" },
      { type: "item.updated", thread_id: "parent", item: { id: "item_0", type: "agent_message", text: "parent partial" } },
      { type: "item.started", thread_id: "parent", item: { id: "item_1", type: "command_execution", command: "parent-command" } },
      { type: "thread.started", thread_id: "child", parent_thread_id: "parent" },
      { type: "item.completed", thread_id: "child", item: { id: "item_0", type: "agent_message", text: "child final" } },
      { type: "item.started", thread_id: "child", item: { id: "item_1", type: "command_execution", command: "child-command" } },
      { type: "thread.started", thread_id: "grandchild", parent_thread_id: "child" },
      { type: "item.completed", thread_id: "grandchild", item: { id: "item_0", type: "agent_message", text: "grandchild final" } },
      { type: "turn.completed", thread_id: "grandchild", id: "turn", usage: { input_tokens: 100, output_tokens: 20 } },
      { type: "item.completed", thread_id: "child", item: { id: "item_1", type: "command_execution", aggregated_output: "child output", exit_code: 0 } },
      { type: "turn.completed", thread_id: "child", id: "turn", usage: { input_tokens: 50, output_tokens: 10 } },
      { type: "item.completed", thread_id: "parent", item: { id: "item_1", type: "command_execution", aggregated_output: "parent output", exit_code: 1 } },
      { type: "item.completed", thread_id: "parent", item: { id: "item_0", type: "agent_message", text: "parent final" } },
      { type: "turn.completed", thread_id: "parent", id: "turn", usage: { input_tokens: 10, output_tokens: 2 } },
      { type: "turn.failed", thread_id: "child", error: { message: "child-only error" } },
    ];
    const original = structuredClone(records);
    const events = normalizeCodexSession(records);
    expect(ofType(events, "assistant.message").map(event => [event.data.sessionId, event.data.content])).toEqual([
      ["child", "child final"],
      ["grandchild", "grandchild final"],
      ["parent", "parent final"],
    ]);
    expect(ofType(events, "tool.execution_complete").map(event => [event.data.sessionId, event.data.command, event.data.output, event.data.success])).toEqual([
      ["child", "child-command", "child output", true],
      ["parent", "parent-command", "parent output", false],
    ]);
    const display = convertCopilotEventsToLegacyLogEntries(events);
    const calls = display.flatMap(entry => entry.message?.content ?? []).filter(block => block.type === "tool_use");
    const results = display.flatMap(entry => entry.message?.content ?? []).filter(block => block.type === "tool_result");
    expect(calls).toHaveLength(2);
    expect(calls[0].id).not.toBe(calls[1].id);
    expect(results.map(result => [calls.find(call => call.id === result.tool_use_id)?.input.command, result.content])).toEqual([
      ["child-command", "child output"],
      ["parent-command", "parent output"],
    ]);
    expect(
      ofType(events, "session.result")
        .filter(event => event.data.usage)
        .map(event => [event.data.sessionId, event.data.numTurns, event.data.usage.input_tokens])
    ).toEqual([
      ["grandchild", 1, 100],
      ["child", 1, 50],
      ["parent", 1, 10],
    ]);
    expect(selectSessionResult(events)).toMatchObject({ status: "failed", numTurns: 1, usage: { input_tokens: 10, output_tokens: 2 }, errors: [{ message: "child-only error" }] });
    expect(events.find(event => event.type === "assistant.message" && event.data.sessionId === "grandchild")).toMatchObject({ agentId: "grandchild", data: { parentSessionId: "child", messageId: "item_0" } });
    expect(records).toEqual(original);
    expect(normalizeCodexSession(events)).toEqual(events);
    expect(JSON.parse(JSON.stringify(events))).toEqual(JSON.parse(JSON.stringify(normalizeCodexSession(events))));
  });

  it.each([
    ["file_change", "apply_patch", { changes: [{ path: "example.go", kind: "update" }], status: "failed" }, { changes: [{ path: "example.go", kind: "update" }] }, false],
    ["web_search", "web_search", { query: "example query", results: [], status: "completed" }, { query: "example query" }, true],
    ["todo_list", "update_plan", { items: [{ text: "Check", completed: false }] }, { items: [{ text: "Check", completed: false }] }, undefined],
  ])("normalizes %s without inventing unreported outcomes", (type, toolName, fields, input, success) => {
    const record = { type: "item.completed", timestamp: 0, item: { id: "item_0", type, ...fields, extension: false } };
    const events = normalizeCodexSession([record]);
    expect(events.map(event => event.type)).toEqual(["tool.execution_start", "tool.execution_complete"]);
    expect(events[0].data).toMatchObject({ toolName, toolCallId: "item_0", input, extension: false });
    expect(events[1].data.success).toBe(success);
    expect(events[1].item).toEqual(record.item);
    expect(normalizeCodexSession(events)).toEqual(events);
  });

  it("retains incomplete tool invocations and partial message identity", () => {
    const events = normalizeCodexSession([
      { type: "item.started", item: { id: "call", type: "mcp_tool_call" } },
      { type: "item.updated", item: { id: "call", type: "mcp_tool_call", arguments: false } },
      { type: "item.updated", item: { id: "call", type: "mcp_tool_call", tool: "lookup" } },
      { type: "item.completed", item: { id: "call", type: "mcp_tool_call", result: 0, status: "completed" } },
      { type: "item.updated", item: { id: "message", type: "agent_message", text: " observed " } },
      { type: "item.completed", item: { id: "declined", type: "command_execution", command: "example", status: "declined" } },
    ]);
    expect(ofType(events, "tool.execution_complete")[0].data).toMatchObject({ toolName: "lookup", input: false, output: 0, success: true });
    expect(ofType(events, "assistant.message")[0].data).toMatchObject({ messageId: "message", content: " observed ", partial: true });
    expect(ofType(events, "tool.execution_complete")[1].data.success).toBe(false);
  });

  it("keeps parent initialization and does not assume child models match the harness model", () => {
    const events = normalizeCodexSession(
      [
        { type: "thread.started", thread_id: "parent", model: "parent-model" },
        { type: "thread.started", thread_id: "child", parent_thread_id: "parent", model: "child-model" },
        { type: "thread.started", thread_id: "grandchild", parent_thread_id: "child" },
      ],
      "harness-model"
    );
    expect(projectSessionInitialization(events)).toMatchObject({ model: "parent-model", session_id: "parent" });
    expect(events[1].data.model).toBe("child-model");
    expect(events[2].data.model).toBeUndefined();
  });

  it("deduplicates descendant state snapshots observed by different ancestors", () => {
    const spawn = (parent, child) => ({ type: "item.completed", item: { id: "spawn", type: "collab_tool_call", tool: "spawn_agent", sender_thread_id: parent, receiver_thread_ids: [child], agents_states: {}, status: "completed" } });
    const wait = sender => ({
      type: "item.completed",
      item: { id: "wait", type: "collab_tool_call", tool: "wait", sender_thread_id: sender, receiver_thread_ids: ["grandchild"], agents_states: { grandchild: { status: "completed", message: "One answer." } }, status: "completed" },
    });
    const events = normalizeCodexSession([spawn("parent", "child"), spawn("child", "grandchild"), wait("child"), wait("parent")]);
    expect(ofType(events, "assistant.message").map(event => event.data.content)).toEqual(["One answer."]);
    expect(ofType(events, "subagent.completed")).toHaveLength(1);
    expect(ofType(events, "subagent.completed")[0].data).toMatchObject({ parentSessionId: "child", toolCallId: "spawn" });
    expect(ofType(events, "tool.execution_complete")).toHaveLength(4);
  });
});

describe("Codex legacy observation fidelity", () => {
  it("preserves short, empty and whitespace-only reasoning and native tool names", () => {
    const events = parseCodexLog("thinking\n \nShort\n\nToolCall: catalog__lookup false\ncatalog.lookup(...) success in 0ms:\nfalse\nthinking\n").logEntries;
    expect(events.map(event => event.type)).toEqual(["assistant.reasoning", "tool.execution_start", "tool.execution_complete", "assistant.reasoning"]);
    expect(events[0].data.content).toBe(" \nShort\n");
    expect(events[1].data).toMatchObject({ toolName: "lookup", mcpServerName: "catalog", input: false });
    expect(events[2].data).toMatchObject({ success: true, durationMs: 0, output: "false" });
    expect(events[3].data.content).toBe("");
  });

  it("preserves interleaved legacy starts and outcomes rather than moving completions", () => {
    const events = parseCodexLog("tool catalog.first({})\nthinking\n x \ntool catalog.second([])\ncatalog.first(...) failed in 2ms:\n failure \ncatalog.second(...) success in 3ms:\n[]").logEntries;
    expect(events.map(event => event.type)).toEqual(["tool.execution_start", "assistant.reasoning", "tool.execution_start", "tool.execution_complete", "tool.execution_complete"]);
    expect(events[1].data.content).toBe(" x ");
    expect(events[3].data.output).toBe(" failure ");
    expect(events[3].data.toolCallId).toBeUndefined();
  });

  it("retains orphan outcomes, source timestamps and reported zero token totals", () => {
    const events = parseCodexLog("[native-timestamp] catalog.lookup(...) failed in 0ms:\n\nERROR:  exact error \ntokens used\n0").logEntries;
    expect(events.map(event => event.type)).toEqual(["tool.execution_complete", "session.result", "session.result"]);
    expect(events[0]).toMatchObject({ timestamp: "native-timestamp", data: { success: false, durationMs: 0, output: "" } });
    expect(events[1].data.errors).toEqual([" exact error "]);
    expect(selectSessionResult(events).usage.total_tokens).toBe(0);
  });
});
