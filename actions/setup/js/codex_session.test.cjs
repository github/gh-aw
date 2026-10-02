import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { normalizeCodexSession } from "./codex_session.cjs";
import { parseCodexLog, isCodexJsonlFormat } from "./parse_codex_log.cjs";
import { selectSessionResult, sessionTokenTotal } from "./agent_session.cjs";

const fixture = name => readFileSync(new URL(`./test_data/${name}.jsonl`, import.meta.url), "utf8");
const ofType = (events, type) => events.filter(event => event.type === type);
const parse = records => parseCodexLog(records.map(record => (typeof record === "string" ? record : JSON.stringify(record))).join("\n")).logEntries;

describe("Codex real CI trace regression", () => {
  // Sanitized excerpts: github/gh-aw/actions/runs/36909965579 (Smoke Codex) and
  // /36850958249 (Daily Documentation Updater), artifact `agent`/agent-stdio.log.
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
