import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { parseCopilotLog, parseDebugLogFormat, parsePrettyPrintFormat } = require("./parse_copilot_log.cjs");
const { normalizeCopilotSession } = require("./copilot_session.cjs");
const { projectSessionResult, sessionTokenTotal, sessionToolSuccess } = require("./agent_session.cjs");

// Sanitized real shapes, not raw traces:
// Smoke Copilot failure: https://github.com/github/gh-aw/actions/runs/36946387975
// agent artifact 11202477124:
// sandbox/agent/logs/copilot-session-state/4a35a5d9-6c1a-44e2-acbd-641fa3a17edd/events.jsonl
// and sandbox/agent/logs/process-1790901389861-401.log.
// Smoke Copilot success: https://github.com/github/gh-aw/actions/runs/36798242962
// agent artifact 11134847308: sandbox/agent/logs/process-1790816195144-401.log
// and agent-stdio.log (no persisted native session artifact in this success).
// IDs, commands, context, text, timestamps and token values below are sanitized.
// CI used Copilot 1.0.87; Rust debug data blocks include [rust:model_wire].
// Live assistant.usage/delta and error variants follow the installed SDK's
// dist/generated/session-events.d.ts; these are not persisted in sampled logs.
const native = [
  {
    type: "session.start",
    id: "native-start",
    parentId: null,
    timestamp: "2026-10-02T00:00:00.000Z",
    data: {
      sessionId: "sanitized-session",
      version: 1,
      producer: "copilot-agent",
      copilotVersion: "1.0.87",
      startTime: "2026-10-02T00:00:00.000Z",
      selectedModel: "claude-sonnet-5.5",
      reasoningEffort: "medium",
      contextTier: null,
      alreadyInUse: false,
      remoteSteerable: false,
      context: { cwd: "/workspace/repo", gitRoot: "/workspace/repo", repository: "example/repo", hostType: "github", branch: "main" },
    },
  },
  { type: "session.info", id: "native-info", parentId: "native-start", data: { infoType: "mcp", message: "Sanitized transport information." } },
  { type: "user.message", id: "native-user", data: { content: "PRIVATE_USER_PROMPT", messageId: "user-1", turnId: "0", transformedContent: "PRIVATE_TRANSFORMED_PROMPT", delivery: "immediate", agentMode: "autopilot" } },
  { type: "system.message", id: "native-system", data: { content: "PRIVATE_SYSTEM_INSTRUCTIONS", role: "system" } },
  { type: "assistant.turn_start", id: "native-turn-start", data: { turnId: "0", interactionId: "interaction-1" } },
  {
    type: "assistant.message",
    id: "native-message",
    parentId: "native-turn-start",
    data: { content: "  Observed response.\n", reasoningText: "  Observed reasoning.\n", reasoningOpaque: "opaque-state", apiCallId: "api-1", messageId: "message-1", model: "claude-sonnet-5.5", turnId: "0", toolRequests: [], rte: false },
  },
  {
    type: "tool.execution_start",
    id: "native-tool-start",
    data: { toolCallId: "tool-1", toolName: "bash", arguments: { command: "  echo sanitized\n" }, model: "claude-sonnet-5.5", shellToolInfo: { possiblePaths: [], hasWriteFileRedirection: false }, turnId: "0" },
  },
  { type: "permission.requested", id: "native-permission", data: { requestId: "permission-1", agentMode: "autopilot", permissionMode: "default", permissionRequest: { kind: "shell" } } },
  { type: "permission.completed", id: "native-permission-complete", data: { requestId: "permission-1", decisionSource: "policy", result: { kind: "denied" }, toolCallId: "tool-1" } },
  {
    type: "tool.execution_complete",
    id: "native-tool-failure",
    data: { toolCallId: "tool-1", success: false, error: { message: "Sanitized permission denial.", code: "PERMISSION_DENIED" }, model: "claude-sonnet-5.5", toolTelemetry: { properties: {} }, turnId: "0" },
  },
  { type: "assistant.turn_end", id: "native-turn-end", data: { turnId: "0" } },
  { type: "assistant.turn_end", id: "native-turn-end-2", data: { turnId: "1" } },
  { type: "session.task_complete", id: "native-task-complete", data: { success: true, summary: "Sanitized task summary." } },
  {
    type: "session.shutdown",
    id: "native-shutdown",
    timestamp: "2026-10-02T00:00:05.000Z",
    data: {
      shutdownType: "routine",
      sessionStartTime: 1790899200000,
      totalApiDurationMs: 3000,
      totalNanoAiu: 123456,
      tokenDetails: { input: { tokenCount: 10 }, cache_read: { tokenCount: 20 }, cache_write: { tokenCount: 30 }, output: { tokenCount: 4 } },
      modelMetrics: { "claude-sonnet-5.5": { requests: { count: 2, cost: 0 }, usage: { inputTokens: 60, outputTokens: 4, cacheReadTokens: 20, cacheWriteTokens: 0, reasoningTokens: 0 }, totalNanoAiu: 123456 } },
      codeChanges: { linesAdded: 0, linesRemoved: 0, filesModified: [] },
      currentModel: "claude-sonnet-5.5",
      currentTokens: 25,
      systemTokens: 8,
      conversationTokens: 9,
      toolDefinitionsTokens: 8,
    },
  },
];

const debugResponse = (id, input, output, message = {}) => ({
  usage: { prompt_tokens: input, completion_tokens: output, prompt_tokens_details: { cached_tokens: 0 }, total_tokens: input + output },
  copilot_usage: { total_nano_aiu: 123 },
  id,
  choices: [{ message: { role: "assistant", reasoning_opaque: "opaque", reasoning_text: "  reasoning \n", content: "  response \n", refusal: null, tool_calls: null, ...message }, finish_reason: "stop", index: 0, logprobs: null }],
  created: 1790899200,
  model: "claude-sonnet-5.5",
  object: "chat.completion",
});

const debugBlock = (response, rust = true) => {
  const prefix = `2026-10-02T00:00:01.000Z [DEBUG] ${rust ? "[rust:model_wire] " : ""}`;
  return `${prefix}response (Request-ID request-${response.id}):\n${prefix}data:\n${prefix}${JSON.stringify(response, null, 2)}\n`;
};

describe("Copilot real CI shape projections", () => {
  it("maps native startup, arguments, reasoning and shutdown without dropping native additions", () => {
    const result = parseCopilotLog(native.map(event => JSON.stringify(event)).join("\n"));
    const init = result.logEntries.find(event => event.type === "session.init");
    expect(init.data).toMatchObject({ sourceEngine: "copilot", model: "claude-sonnet-5.5", sessionId: "sanitized-session", cwd: "/workspace/repo", contextTier: null, remoteSteerable: false });
    expect(result.logEntries.find(event => event.type === "session.start").data.context).toEqual(native[0].data.context);
    expect(result.logEntries.find(event => event.type === "tool.execution_start").data.input).toEqual({ command: "  echo sanitized\n" });
    expect(result.logEntries.find(event => event.type === "tool.execution_start").data.arguments).toEqual({ command: "  echo sanitized\n" });
    expect(result.logEntries.find(event => event.type === "assistant.reasoning").data.content).toBe("  Observed reasoning.\n");
    expect(result.logEntries.find(event => event.type === "tool.execution_complete").data).toMatchObject({ toolName: "bash", success: false, error: { code: "PERMISSION_DENIED" } });
    const stats = projectSessionResult(result.logEntries);
    expect(stats).toMatchObject({ num_turns: 2, duration_ms: 5000, usage: { input_tokens: 60, output_tokens: 4, cache_read_input_tokens: 20, cache_creation_input_tokens: 30 } });
    expect(stats.total_cost_usd).toBeUndefined();
    expect(stats.errors).toBeUndefined();
    expect(sessionTokenTotal(stats.usage)).toBe(64);
    expect(result.markdown).toContain("claude-sonnet-5.5");
    expect(result.markdown).not.toContain("PRIVATE_USER_PROMPT");
    expect(result.markdown).not.toContain("PRIVATE_TRANSFORMED_PROMPT");
    expect(result.markdown).not.toContain("PRIVATE_SYSTEM_INSTRUCTIONS");
  });

  it("preserves native observation order, IDs and opaque extension payloads", () => {
    const events = normalizeCopilotSession(native);
    const retained = events.filter(event => !event.copilotProjection);
    expect(retained.map(event => event.id)).toEqual(native.map(event => event.id));
    expect(events.find(event => event.type === "permission.completed")).toEqual(native[8]);
    expect(events.find(event => event.type === "session.task_complete")).toEqual(native[12]);
    expect(events.find(event => event.type === "session.shutdown")).toEqual(native[13]);
    expect(events.every(event => event.type.includes(".") && event.data)).toBe(true);
  });

  it("does not mutate native input and remains idempotent through JSON serialization", () => {
    const original = structuredClone(native);
    const events = normalizeCopilotSession(native);
    expect(native).toEqual(original);
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
    expect(parseCopilotLog(JSON.stringify(events)).logEntries).toEqual(events);
  });

  it("deduplicates identified projections across interleaving without losing distinct evidence", () => {
    const start = { type: "session.start", id: "same", timestamp: 0, data: { sessionId: "s", selectedModel: "first" } };
    const records = [start, { type: "vendor.interleaved", data: {} }, start, { ...start, data: { ...start.data, selectedModel: "second" } }, { ...start, nativeFlag: true }];
    const events = normalizeCopilotSession(records);
    expect(events.filter(event => event.type === "session.start")).toHaveLength(4);
    expect(events.filter(event => event.type === "session.init").map(event => event.data.model)).toEqual(["first", "second", "first"]);
    const interleaved = [...events.filter(event => !event.copilotProjection), ...events.filter(event => event.copilotProjection)];
    expect(normalizeCopilotSession(interleaved)).toEqual(interleaved);
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(interleaved)))).toEqual(JSON.parse(JSON.stringify(interleaved)));
  });

  it.each([undefined, 0])("retains separate unidentified projections with timestamp %j and matches existing ones by occurrence", timestamp => {
    const error = { type: "session.error", timestamp, data: { message: "same text" } };
    const records = [error, { type: "vendor.interleaved", data: {} }, error];
    const events = normalizeCopilotSession(records);
    expect(projectSessionResult(events).errors).toEqual([error.data, error.data]);
    const interleaved = [...events.filter(event => !event.copilotProjection), ...events.filter(event => event.copilotProjection)];
    expect(normalizeCopilotSession(interleaved)).toEqual(interleaved);
  });

  it("leaves overflowed live accounting unavailable rather than reverting to an earlier snapshot", () => {
    const records = [
      { type: "assistant.usage", id: "large", data: { inputTokens: Number.MAX_SAFE_INTEGER, outputTokens: 1 } },
      { type: "assistant.usage", id: "overflow", data: { inputTokens: 1, outputTokens: 2 } },
      { type: "assistant.usage", id: "later", data: { inputTokens: 0, outputTokens: 3 } },
    ];
    const events = normalizeCopilotSession(records);
    expect(projectSessionResult(events).usage.input_tokens).toBeUndefined();
    expect(projectSessionResult(events).usage.output_tokens).toBe(6);
    const roundTrip = JSON.parse(JSON.stringify(events));
    expect(normalizeCopilotSession(roundTrip)).toEqual(roundTrip);
    expect(projectSessionResult(roundTrip).usage.input_tokens).toBeUndefined();
  });

  it("uses explicit finalized-turn identities, not messages or tools", () => {
    const events = [
      { type: "assistant.message", data: { content: "First." } },
      { type: "assistant.message", data: { content: "Second." } },
      { type: "tool.execution_start", data: { toolName: "bash" } },
      { type: "assistant.turn_end", id: "end-1", data: { turnId: "0" } },
      { type: "assistant.turn_end", id: "repeated-end", data: { turnId: "0" } },
    ];
    expect(projectSessionResult(normalizeCopilotSession(events)).num_turns).toBe(1);
    expect(projectSessionResult(normalizeCopilotSession(events.slice(0, 3)))).toBeUndefined();
    expect(normalizeCopilotSession(events.slice(0, 3)).filter(event => event.type === "tool.execution_complete")).toEqual([]);
    expect(projectSessionResult(normalizeCopilotSession([...events, { type: "session.result", data: { numTurns: 0 } }])).num_turns).toBe(0);
  });

  it("accumulates distinct live usage reports and replaces covered totals with shutdown snapshots", () => {
    const reports = [
      { type: "assistant.usage", id: "usage-1", ephemeral: true, data: { apiCallId: "api-1", inputTokens: 10, outputTokens: 0, cacheReadTokens: 3, cost: 2, duration: 100 } },
      { type: "assistant.usage", id: "usage-duplicate", ephemeral: true, data: { apiCallId: "api-1", inputTokens: 10, outputTokens: 0, cacheReadTokens: 3 } },
      { type: "assistant.usage", id: "usage-2", ephemeral: true, data: { apiCallId: "api-2", inputTokens: 20, outputTokens: 2 } },
    ];
    const events = normalizeCopilotSession(reports);
    expect(projectSessionResult(events).usage).toEqual({ input_tokens: 30, output_tokens: 2, cache_read_input_tokens: 3 });
    expect(projectSessionResult(events).total_cost_usd).toBeUndefined();
    expect(projectSessionResult(events).duration_ms).toBeUndefined();
    expect(projectSessionResult(normalizeCopilotSession([...reports, native.at(-1)])).usage).toMatchObject({ input_tokens: 60, output_tokens: 4, cache_read_input_tokens: 20, cache_creation_input_tokens: 30 });
    expect(normalizeCopilotSession(JSON.parse(JSON.stringify(events)))).toEqual(JSON.parse(JSON.stringify(events)));
  });

  it("retains error records independently of usage, with stable nonduplicating projections", () => {
    const source = [
      { type: "session.result", data: { usage: { input_tokens: 0 }, errors: [] } },
      { type: "session.error", id: "error-1", data: { errorType: "provider", message: "Retry failed", statusCode: 503 } },
      { type: "session.error", id: "error-2", data: { errorType: "provider", message: "Retry failed", statusCode: 503 } },
      { type: "tool.execution_start", data: { toolCallId: "pending", toolName: "bash" } },
    ];
    const events = normalizeCopilotSession(source);
    expect(projectSessionResult(events).errors).toEqual([source[1].data, source[2].data]);
    expect(events.filter(event => event.type === "tool.execution_complete")).toEqual([]);
    expect(normalizeCopilotSession(events)).toEqual(events);
  });

  it("preserves structured and falsy native payloads and command-only inputs", () => {
    const records = [
      { type: "tool.execution_start", id: "start", data: { toolCallId: "tool", toolName: "bash", arguments: false, command: "  echo exact \n", mcpServerName: "" } },
      { type: "tool.execution_complete", id: "end", data: { toolCallId: "tool", success: false, output: null, result: { content: [{ type: "json", json: { value: 0 } }] }, error: "", durationMs: 0 } },
    ];
    const events = normalizeCopilotSession(records);
    expect(events[0].data).toMatchObject({ input: false, arguments: false, command: "  echo exact \n", mcpServerName: "" });
    expect(events[1].data).toMatchObject({ success: false, output: null, error: "", durationMs: 0, result: records[1].data.result });
    expect(normalizeCopilotSession(events)).toEqual(events);
  });

  it("maps native shell exit codes without erasing contradictory success or native execution details", () => {
    const records = [
      { type: "tool.execution_complete", id: "failed-shell", data: { toolCallId: "tool-1", toolName: "bash", success: true, shellExecution: { exitCode: 2 }, result: { content: "  observed failure output\n" } } },
      { type: "tool.execution_complete", id: "successful-shell", data: { toolCallId: "tool-2", toolName: "bash", success: true, shellExecution: { exitCode: 0 }, result: { content: "" } } },
      { type: "tool.execution_complete", id: "unknown-shell", data: { toolCallId: "tool-3", toolName: "bash", shellExecution: {} } },
    ];
    const original = structuredClone(records);
    const events = normalizeCopilotSession(records);
    expect(events[0].data).toMatchObject({ exitCode: 2, success: true, shellExecution: { exitCode: 2 } });
    expect(sessionToolSuccess(events[0].data)).toBe(false);
    expect(events[1].data.exitCode).toBe(0);
    expect(sessionToolSuccess(events[1].data)).toBe(true);
    expect(events[2].data.exitCode).toBeUndefined();
    expect(sessionToolSuccess(events[2].data)).toBeUndefined();
    expect(records).toEqual(original);
    expect(normalizeCopilotSession(events)).toEqual(events);
  });

  it("retains partial message deltas without duplicating a final native message snapshot", () => {
    const deltas = [
      { type: "assistant.message_delta", id: "delta-1", data: { messageId: "message", deltaContent: "  partial " } },
      { type: "tool.execution_start", data: { toolCallId: "pending", toolName: "bash" } },
      { type: "assistant.message_delta", id: "delta-2", data: { messageId: "message", deltaContent: "\n" } },
    ];
    const partial = normalizeCopilotSession(deltas);
    expect(partial.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["  partial ", "\n"]);
    expect(normalizeCopilotSession(partial)).toEqual(partial);
    const complete = normalizeCopilotSession([...deltas, { type: "assistant.message", data: { messageId: "message", content: "  partial \n" } }]);
    expect(complete.filter(event => event.type === "assistant.message")).toHaveLength(1);
    expect(complete.filter(event => event.type === "assistant.message_delta")).toHaveLength(2);
    const incomplete = normalizeCopilotSession([...deltas, { type: "assistant.message", data: { messageId: "message", content: "" } }]);
    expect(incomplete.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["  partial ", "\n", ""]);
  });

  it("keeps tokenDetails-only uncached input distinct from cache subtotals", () => {
    const events = normalizeCopilotSession([{ type: "session.shutdown", data: { modelMetrics: {}, tokenDetails: { input: { tokenCount: 10 }, cache_read: { tokenCount: 20 }, cache_write: { tokenCount: 30 }, output: { tokenCount: 0 } } } }]);
    const usage = projectSessionResult(events).usage;
    expect(usage).toEqual({ input_tokens: 10, output_tokens: 0, cache_read_input_tokens: 20, cache_creation_input_tokens: 30, input_tokens_include_cache: false });
    expect(sessionTokenTotal(usage)).toBe(60);
    expect(projectSessionResult(events).duration_ms).toBeUndefined();
  });

  it("retains invalid native metrics without inventing mapped zero totals", () => {
    const records = [
      { type: "assistant.usage", id: "invalid-live", data: { inputTokens: -1, outputTokens: 1.5, cacheReadTokens: null } },
      {
        type: "session.shutdown",
        id: "invalid-shutdown",
        timestamp: "not a timestamp",
        data: { modelMetrics: { model: { usage: { inputTokens: -5, outputTokens: "3" } } }, tokenDetails: { input: { tokenCount: -2 } }, sessionStartTime: -1 },
      },
    ];
    expect(normalizeCopilotSession(records)).toEqual(records);
    expect(projectSessionResult(normalizeCopilotSession(records))).toBeUndefined();
  });
});

describe("Copilot Rust and legacy structured debug decoding", () => {
  it("decodes real Rust debug framing and distinct response accounting at EOF", () => {
    const first = debugResponse("response-1", 10, 0);
    const second = debugResponse("response-2", 20, 2);
    const result = parseCopilotLog(debugBlock(first) + debugBlock(first) + debugBlock(second));
    expect(projectSessionResult(result.logEntries).usage).toEqual({ input_tokens: 30, output_tokens: 2, cache_read_input_tokens: 0 });
    expect(projectSessionResult(result.logEntries).num_turns).toBeUndefined();
    expect(result.logEntries.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["  response \n", "  response \n", "  response \n"]);
    expect(result.logEntries.find(event => event.type === "assistant.reasoning").data.content).toBe("  reasoning \n");
    expect(result.logEntries.find(event => event.type === "assistant.message")).toMatchObject({ id: "response-1", requestId: "request-response-1", copilot_usage: { total_nano_aiu: 123 } });
  });

  it("decodes legacy framing, blank text, primitive arguments, and dangling calls without renamed storage", () => {
    const response = debugResponse("response-tools", 0, 0, {
      content: "",
      reasoning_text: " \n ",
      tool_calls: [
        { id: "native-call", type: "function", function: { name: "github-list_issues", arguments: "false" }, nativeFlag: true },
        { type: "function", function: { name: "bash", arguments: "  not JSON \n" } },
      ],
    });
    const result = parseCopilotLog(debugBlock(response, false) + "2026-10-02T00:00:02.000Z [ERROR] Permission denied\n");
    expect(result.logEntries.filter(event => event.type === "tool.execution_complete")).toEqual([]);
    const starts = result.logEntries.filter(event => event.type === "tool.execution_start");
    expect(starts[0].data).toMatchObject({ toolCallId: "native-call", toolName: "github-list_issues", input: false, nativeFlag: true });
    expect(starts[1].data).toMatchObject({ toolName: "bash", input: "  not JSON \n" });
    expect(starts[1].data.toolCallId).toBeUndefined();
    expect(result.logEntries.find(event => event.type === "assistant.message").data.content).toBe("");
    expect(projectSessionResult(result.logEntries).usage).toMatchObject({ input_tokens: 0, output_tokens: 0 });
    expect(projectSessionResult(result.logEntries).errors).toBeUndefined();
  });

  it("recovers valid adjacent debug blocks and native/legacy records in source order", () => {
    const before = { type: "assistant.message", id: "before", data: { content: " before " } };
    const after = { type: "assistant", id: "after", message: { content: [{ type: "text", text: " after " }] } };
    const input = [
      JSON.stringify(before),
      "[DEBUG] data:",
      '{"choices": [',
      "[DEBUG] data:",
      JSON.stringify(debugResponse("recovered", 5, 1)),
      "[DEBUG] data:",
      '{"choices": [',
      JSON.stringify(after),
      "null",
      JSON.stringify({ type: "vendor.progress", id: "extension", data: { native: [0, false, null] } }),
    ].join("\n");
    const result = parseCopilotLog(input);
    expect(result.logEntries.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual([" before ", "  response \n", " after "]);
    expect(result.logEntries.at(-1)).toMatchObject({ type: "vendor.progress", id: "extension", data: { native: [0, false, null] } });
  });

  it("maps observed provider errors separately without turning them into tool failures", () => {
    const input = debugBlock({ id: "provider-error", error: { message: "Sanitized provider error", code: "unavailable" } });
    const result = parseCopilotLog(input);
    expect(projectSessionResult(result.logEntries).errors).toEqual([{ message: "Sanitized provider error", code: "unavailable" }]);
    expect(result.logEntries.filter(event => event.type === "assistant.message" || event.type === "tool.execution_complete")).toEqual([]);
  });

  it("does not claim unsupported JSON or metadata-free debug noise is a recognized trace", () => {
    for (const input of ['[{"unrelated":true}]', '{"type":"not.supported.raw"}', '[DEBUG] data:\n{"unrelated":true}', "[DEBUG] Starting CLI"]) {
      const result = parseCopilotLog(input);
      expect(result.logEntries).toEqual([]);
      expect(result.markdown).toContain("not recognized");
    }
  });

  it("retains actual tools and model inventory blocks without canonical renaming", () => {
    const tools = [{ type: "function", function: { name: "github-list_issues", native: true } }];
    const entries = parseDebugLogFormat(`[DEBUG] Tools:\n[DEBUG] ${JSON.stringify(tools)}\n[DEBUG] Got model info: {"name":"observed-model","billing":{"is_premium":false}}\n`);
    expect(entries[0].data.tools).toEqual(tools);
    expect(entries[1].data.modelInfo).toEqual({ name: "observed-model", billing: { is_premium: false } });
  });

  it("does not mistake an inline nested function block for an adjacent raw record", () => {
    const response = debugResponse("inline-function", 10, 1, { tool_calls: [{ id: "call", type: "function", function: { name: "bash", arguments: "{}" } }] });
    const source = debugBlock(response).replace(/\{\s+"id": "call",\s+"type": "function",\s+"function": \{\s+"name": "bash",\s+"arguments": "\{\}"\s+\}\s+\}/, '{"id":"call","type":"function","function":{"name":"bash","arguments":"{}"}}');
    expect(source).toContain('{"id":"call","type":"function"');
    const result = parseCopilotLog(source);
    expect(result.logEntries.find(event => event.type === "tool.execution_start").data.toolCallId).toBe("call");
    expect(projectSessionResult(result.logEntries).usage.input_tokens).toBe(10);
  });

  it("does not leak a user warning-shaped prompt through the Firewall Steering section", () => {
    const result = parseCopilotLog(JSON.stringify([{ type: "user.message", data: { content: "[AWF TOKEN WARNING] PRIVATE_USER_PROMPT" } }]));
    expect(result.logEntries[0].data.content).toBe("[AWF TOKEN WARNING] PRIVATE_USER_PROMPT");
    expect(result.markdown).not.toContain("PRIVATE_USER_PROMPT");
    expect(result.markdown).not.toContain("Firewall Steering");
  });
});

describe("Copilot pretty-print observations", () => {
  it("preserves interleaved source text and actual output without fabricated IDs, inputs or success output", () => {
    const input = "  Before. \n\n● Bash\n    └   output  \n  Between. \n✓ Read\n✗ Write\n    └ \n  After.  ";
    const events = parsePrettyPrintFormat(input);
    expect(events.map(event => event.type)).toEqual([
      "assistant.message",
      "tool.execution_start",
      "tool.execution_complete",
      "assistant.message",
      "tool.execution_start",
      "tool.execution_complete",
      "tool.execution_start",
      "tool.execution_complete",
      "assistant.message",
    ]);
    expect(events[0].data.content).toBe("  Before. \n\n");
    expect(events[2].data.output).toBe("  output  ");
    expect(events[3].data.content).toBe("  Between. \n");
    expect(events[5].data).toMatchObject({ success: true });
    expect(Object.hasOwn(events[5].data, "output")).toBe(false);
    expect(events[7].data).toMatchObject({ success: false, output: "" });
    expect(events.at(-1).data.content).toBe("  After.  ");
    expect(events.every(event => event.data.toolCallId === undefined && event.data.input === undefined)).toBe(true);
    expect(projectSessionResult(events)).toBeUndefined();
  });

  it("recognizes the real wrapped shell display instead of naming the tool cd", () => {
    const input = "✗ cd /workspace/repo; echo sanitized\n  wrapped command\n  └ Sanitized permission denial.\n● gh pr list --repo example/repo\n  │ observed output  \n  └ 2 lines…";
    const events = parsePrettyPrintFormat(input);
    expect(events[0].data).toMatchObject({ toolName: "bash", command: "cd /workspace/repo; echo sanitized\nwrapped command" });
    expect(events[1].data).toMatchObject({ success: false, output: "Sanitized permission denial." });
    expect(events[2].data.toolName).toBe("bash");
    expect(events[3].data.output).toBe("observed output  \n2 lines…");
  });

  it.each([
    "go test ./...",
    "sed -n '1,3p' file",
    "awk '{print $1}' file",
    "find . -name '*.cjs'",
    "printf exact",
    "PATH=/custom/bin command --flag",
    "/opt/custom/bin/run --flag",
    "./scripts/check.sh",
    "pwd",
    "PowerShell -Command example",
    "CustomExecutable",
  ])("recognizes a shell display without an executable allowlist: %s", command => {
    const events = parsePrettyPrintFormat(`✓ ${command}\n  └ observed`);
    expect(events[0].data).toMatchObject({ toolName: "bash", command });
    expect(events[1].data).toMatchObject({ toolName: "bash", success: true, output: "observed" });
  });

  it("does not classify named built-ins or MCP display headers as shell commands", () => {
    const events = parsePrettyPrintFormat('✓ Read file.txt\n✓ Bash\n✓ github list_issues · {"state":"open"}');
    expect(events.filter(event => event.type === "tool.execution_start").map(event => event.data)).toEqual([{ toolName: "Read" }, { toolName: "Bash" }, { toolName: "list_issues", mcpServerName: "github" }]);
  });

  it("uses explicit zero turns and duration while reconciling CLI footer with model breakdown", () => {
    const events = parsePrettyPrintFormat("● Bash\n\nBreakdown by AI model:\n  model-1  10 in, 2 out, 3 cached\n  model-2  20 in, 4 out\nTurns: 0\nDuration   1m 2s\nTokens   ↑ 30 (3 cached, 0 written) • ↓ 6");
    expect(projectSessionResult(events)).toMatchObject({ num_turns: 0, duration_ms: 62000, usage: { input_tokens: 30, output_tokens: 6, cache_read_input_tokens: 3, cache_creation_input_tokens: 0 } });
    expect(parseCopilotLog(JSON.stringify(events)).logEntries).toEqual(events);
  });

  it("accepts a footer reporting zero usage without inventing a model or turns", () => {
    const events = parsePrettyPrintFormat("● Bash\nTokens   ↑ 0 • ↓ 0 • 0 (cached)");
    expect(projectSessionResult(events).usage).toEqual({ input_tokens: 0, output_tokens: 0, cache_read_input_tokens: 0 });
    expect(projectSessionResult(events).num_turns).toBeUndefined();
    expect(events.some(event => event.type === "session.init")).toBe(false);
  });

  it("does not swallow indented assistant text after a named tool without output", () => {
    const events = parsePrettyPrintFormat("● Bash\n  Assistant text with no tool continuation.");
    expect(events.at(-1)).toMatchObject({ type: "assistant.message", data: { content: "  Assistant text with no tool continuation." } });
    expect(events[1].data.output).toBeUndefined();
  });

  it("does not fabricate valid metrics from unsafe CLI token or turn counts", () => {
    const events = parsePrettyPrintFormat("● Bash\nTurns: 999999999999999999999999\nTokens   ↑ 999999999999999999999999 • ↓ 999999999999999999999999");
    expect(projectSessionResult(events)).toBeUndefined();
  });
});
