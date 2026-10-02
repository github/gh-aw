import { describe, it, expect, vi } from "vitest";
import { normalizeAgentSession, createSessionEvent, selectSessionResult, projectSessionResult, normalizeSessionUsage, accumulateSessionUsage, sessionOutputText, sessionToolSuccess, sessionTokenTotal } from "./agent_session.cjs";
import {
  convertLegacyLogEntriesToCopilotEvents,
  convertCopilotEventsToLegacyLogEntries,
  generateConversationMarkdown,
  generatePlainTextSummary,
  generateCopilotCliStyleSummary,
  generateInformationSection,
  formatInitializationSummary,
  formatToolUse,
} from "./log_parser_shared.cjs";
import { parseClaudeLog } from "./parse_claude_log.cjs";
import { parseCopilotLog, parseDebugLogFormat } from "./parse_copilot_log.cjs";
import { parseCodexLog } from "./parse_codex_log.cjs";
import { parseGeminiLog } from "./parse_gemini_log.cjs";
import { parsePiLog } from "./parse_pi_log.cjs";
import { parseCustomLog } from "./parse_custom_log.cjs";

function freeze(value) {
  if (value && typeof value === "object") {
    Object.freeze(value);
    for (const nested of Object.values(value)) freeze(nested);
  }
  return value;
}

function json(value) {
  return JSON.parse(JSON.stringify(value));
}

const parsers = [
  ["claude", parseClaudeLog],
  ["copilot", parseCopilotLog],
  ["codex", parseCodexLog],
  ["gemini", parseGeminiLog],
  ["pi", parsePiLog],
  ["custom", parseCustomLog],
];

const canonical = freeze([
  { type: "session.init", id: "init", data: { model: "fixture-model", sessionId: "session", tools: [], mcpServers: [] } },
  { type: "user.message", data: { content: "PRIVATE_USER_PROMPT" } },
  { type: "assistant.reasoning", parentId: null, timestamp: "2026-10-02T00:00:00Z", data: { content: "  think\n" } },
  { type: "tool.execution_start", data: { toolCallId: "call-1", toolName: "bash", command: "echo fixture", input: { cwd: "/fixture" } } },
  { type: "tool.execution_complete", data: { toolCallId: "call-1", success: true, output: false, durationMs: 0 } },
  { type: "assistant.message", data: { content: "Done.\n" } },
  { type: "session.result", data: { numTurns: 0, totalCostUsd: 0, usage: { input_tokens: 0, outputTokens: 2 }, errors: [] } },
  { type: "vendor.progress", id: "extension", data: { nested: { zero: 0, no: false, absent: null } } },
]);

describe("Unified Agent Session 1.0.0 conformance", () => {
  it("T-UAS-003/006/025/026/029: native events are deterministic, idempotent and JSON-preserving", () => {
    const original = json(canonical);
    const normalized = normalizeAgentSession(canonical);
    expect(normalized).toEqual(canonical);
    expect(normalizeAgentSession(normalized)).toEqual(normalized);
    expect(normalizeAgentSession(canonical)).toEqual(normalized);
    expect(json(normalized)).toEqual(original);
    normalized[0].data.tools.push("display-copy");
    expect(canonical).toEqual(original);
  });

  it("T-UAS-009: initialization maps every supported field and keeps source additions", () => {
    const source = freeze({
      type: "system",
      subtype: "init",
      model: "fixture-model",
      session_id: "source-session",
      cwd: "/fixture",
      tools: ["lookup"],
      mcp_servers: [{ name: "catalog", status: "failed", error: { code: "offline" } }],
      slash_commands: ["/help"],
      model_info: { billing: { is_premium: false } },
      nativeVersion: 3,
    });
    const result = normalizeAgentSession([source], { sourceEngine: "claude" })[0];
    expect(result.data).toMatchObject({
      sourceEngine: "claude",
      model: "fixture-model",
      sessionId: "source-session",
      cwd: "/fixture",
      tools: ["lookup"],
      mcpServers: [{ name: "catalog", status: "failed", error: { code: "offline" } }],
      slashCommands: ["/help"],
      modelInfo: { billing: { is_premium: false } },
    });
    expect(result.nativeVersion).toBe(3);
    expect(json(result.data)).toEqual(json(normalizeAgentSession([source], { sourceEngine: "claude" })[0].data));
  });

  it("T-UAS-007/010/011/018/019/020/028: mixed records expand in content order without dropping metadata", () => {
    const records = freeze([
      { type: "vendor.progress", data: { sequence: 1 } },
      {
        type: "assistant",
        id: "response",
        parentId: "parent",
        timestamp: 123,
        nativeMetadata: { retained: true },
        message: {
          content: [
            { type: "text", text: "" },
            { type: "thinking", thinking: " \n" },
            { type: "tool_use", id: "tool", name: "lookup", input: false },
          ],
        },
      },
      {
        type: "user",
        message: {
          content: [
            { type: "text", text: "prompt\n" },
            { type: "tool_result", tool_use_id: "tool", content: { count: 0 }, is_error: false },
          ],
        },
      },
      { type: "result", num_turns: 0, usage: { input_tokens: 0 }, errors: [], permission_denials: [{ permission: "read" }] },
      null,
      123,
      { type: "unknown" },
    ]);
    const events = convertLegacyLogEntriesToCopilotEvents(records);
    expect(events.map(e => e.type)).toEqual(["vendor.progress", "assistant.message", "assistant.reasoning", "tool.execution_start", "user.message", "tool.execution_complete", "session.result"]);
    for (const event of events.slice(1, 4)) {
      expect(event).toMatchObject({ id: "response", parentId: "parent", timestamp: 123, nativeMetadata: { retained: true } });
    }
    expect(events[1].data.content).toBe("");
    expect(events[2].data.content).toBe(" \n");
    expect(events[3].data).toMatchObject({ toolCallId: "tool", toolName: "lookup", input: false });
    expect(events[4].data.content).toBe("prompt\n");
    expect(events[5].data).toMatchObject({ output: { count: 0 }, success: true });
    expect(events[6].data).toMatchObject({ numTurns: 0, permissionDenials: [{ permission: "read" }] });
    expect(normalizeAgentSession(events)).toEqual(events);
  });

  it.each([false, 0, "", null, [], { result: false }])("T-UAS-005/012/013/027: preserves argument and output %j", value => {
    const events = normalizeAgentSession([
      { type: "assistant", message: { content: [{ type: "tool_use", id: "call", name: "lookup", input: value }] } },
      { type: "user", message: { content: [{ type: "tool_result", tool_use_id: "call", content: value, is_error: false }] } },
    ]);
    expect(events[0].data.input).toEqual(value);
    expect(events[1].data.output).toEqual(value);
    expect(convertCopilotEventsToLegacyLogEntries(events)[1].message.content[0].content).toBe(sessionOutputText(value));
  });

  it("T-UAS-022/024: supplied mismatched IDs never pair by name, and orphan IDs remain intact", () => {
    const events = [
      { type: "tool.execution_start", data: { toolCallId: "start", toolName: "lookup", input: { secret: "do-not-invent" } } },
      { type: "tool.execution_complete", data: { toolCallId: "orphan", toolName: "lookup", output: 0, success: false } },
    ];
    const legacy = convertCopilotEventsToLegacyLogEntries(freeze(events));
    expect(legacy[0].message.content[0].id).toBe("start");
    expect(legacy[1].message.content[0]).toMatchObject({ id: "orphan", input: {} });
    expect(legacy[2].message.content[0]).toMatchObject({ tool_use_id: "orphan", content: "0", is_error: true });
  });

  it("T-UAS-023/034/044: pending calls are not successful and do not invent turns", () => {
    const events = [{ type: "tool.execution_start", data: { toolCallId: "pending", toolName: "lookup", input: {} } }];
    const text = generatePlainTextSummary(events);
    expect(text).toContain("? lookup");
    expect(text).toContain("Tools: 0/1 succeeded");
    expect(text).not.toContain("Turns:");
    expect(normalizeAgentSession(events)).toHaveLength(1);
  });

  it("T-UAS-022: concurrent same-name tools pair only by their supplied IDs", () => {
    const source = freeze([
      { type: "tool.execution_start", data: { toolCallId: "a", toolName: "lookup", input: { name: "a" } } },
      { type: "tool.execution_start", data: { toolCallId: "b", toolName: "lookup", input: { name: "b" } } },
      { type: "tool.execution_complete", data: { toolCallId: "b", toolName: "lookup", success: false, output: "b failed" } },
      { type: "tool.execution_complete", data: { toolCallId: "a", success: true, output: "a passed" } },
    ]);
    const text = generatePlainTextSummary(source);
    expect(text).toContain("✓ lookup(name: a)");
    expect(text).toContain("✗ lookup(name: b)");
    expect(text).toContain("Tools: 1/2 succeeded");
    expect(normalizeAgentSession(source)).toEqual(source);
  });

  it("T-UAS-014: conflicting error signals control display outcome without changing evidence", () => {
    const data = { success: true, output: false, error: { message: "failed" } };
    expect(sessionToolSuccess(data)).toBe(false);
    expect(data.success).toBe(true);
    expect(sessionToolSuccess({})).toBeUndefined();
    expect(sessionToolSuccess({ success: true, result: { isError: true } })).toBe(false);
    expect(sessionToolSuccess({ success: true, exitCode: 2 })).toBe(false);
  });

  it("T-UAS-030/031: aliases preserve zeros and invalid metrics are unavailable", () => {
    const usage = normalizeSessionUsage({ input_tokens: 0, inputTokens: 42, outputTokens: 2, cacheReadInputTokens: 3, extra: { kept: true }, cache_creation_input_tokens: -1 });
    expect(usage).toMatchObject({ input_tokens: 0, inputTokens: 42, output_tokens: 2, cache_read_input_tokens: 3, extra: { kept: true } });
    expect(usage.cache_creation_input_tokens).toBeUndefined();
    const result = projectSessionResult([{ type: "session.result", data: { numTurns: NaN, durationMs: -2, totalCostUsd: Infinity, usage: { input_tokens: -1 } } }]);
    expect(result.num_turns).toBeUndefined();
    expect(result.duration_ms).toBeUndefined();
    expect(result.total_cost_usd).toBeUndefined();
    expect(result.usage.input_tokens).toBeUndefined();
  });

  it("T-UAS-031: cache subtotals are not blindly added to input", () => {
    expect(sessionTokenTotal({ input_tokens: 10, output_tokens: 2, cache_read_input_tokens: 7 })).toBe(12);
    expect(sessionTokenTotal({ input_tokens: 10, output_tokens: 2, cache_read_input_tokens: 7, input_tokens_include_cache: false })).toBe(19);
    expect(sessionTokenTotal({ total_tokens: 30, input_tokens: 10, output_tokens: 2 })).toBe(30);
    expect(sessionTokenTotal({ cache_read_input_tokens: 7 })).toBeUndefined();
  });

  it("T-UAS-032/033: sum contributions but select snapshots field by field", () => {
    const usage = {};
    accumulateSessionUsage(usage, { input_tokens: 5, outputTokens: 2 });
    accumulateSessionUsage(usage, { input_tokens: 7, output_tokens: 0 });
    expect(usage).toEqual({ input_tokens: 12, output_tokens: 2 });
    const result = selectSessionResult(
      freeze([
        { type: "session.result", data: { numTurns: 2, durationMs: 0, usage, errors: ["first"], permissionDenials: [] } },
        { type: "session.result", data: { usage: { inputTokens: 15 }, errors: ["second"] } },
        { type: "vendor.progress", data: {} },
      ])
    );
    expect(result).toMatchObject({ numTurns: 2, durationMs: 0, usage: { input_tokens: 15, output_tokens: 2 }, errors: ["first", "second"], permissionDenials: [] });
  });

  it("T-UAS-027/049: extension keys cannot inject inherited accounting", () => {
    const events = JSON.parse('[{"type":"session.result","data":{"__proto__":{"numTurns":99},"constructor":{"native":true},"errors":[]}}]');
    const result = selectSessionResult(events);
    expect(Object.hasOwn(result, "__proto__")).toBe(true);
    expect(result.numTurns).toBeUndefined();
    expect(projectSessionResult(events).num_turns).toBeUndefined();
    expect({}.numTurns).toBeUndefined();
  });

  it("T-UAS-043: command projection preserves an existing command and never mutates native input", () => {
    const event = freeze({ type: "tool.execution_start", data: { toolName: "bash", toolCallId: "command", input: { command: "" }, command: "not-this-command" } });
    const projected = convertCopilotEventsToLegacyLogEntries([event]);
    expect(projected[0].message.content[0].input.command).toBe("");
    expect(event.data.input).toEqual({ command: "" });
  });

  it("T-UAS-009/043: native tool descriptors and falsy parameters remain readable", () => {
    const info = formatInitializationSummary({ tools: [{ name: "bash" }, { type: "function", function: { name: "lookup" } }, null] });
    expect(info.markdown).toContain("bash");
    expect(info.markdown).toContain("lookup");
    const text = generatePlainTextSummary([{ type: "tool.execution_start", data: { toolCallId: "falsy", toolName: "lookup", input: { count: 0, enabled: false, nothing: null } } }]);
    expect(text).toContain("count: 0");
    expect(text).toContain("enabled: false");
    expect(text).toContain("nothing: null");
  });

  it("T-UAS-031/034: partial accounting displays unavailable input rather than measured zero", () => {
    const text = generatePlainTextSummary([{ type: "session.result", data: { usage: { output_tokens: 2 } } }]);
    expect(text).toContain("unknown in / 2 out");
    expect(text).toContain("Tokens: 2 observed");
    expect(generateInformationSection({ usage: { cache_read_input_tokens: 0 } })).toContain("Cache Read: 0");
    const claude = normalizeAgentSession([{ type: "result", usage: { input_tokens: 2, output_tokens: 1, cache_read_input_tokens: 3 } }], { sourceEngine: "claude" });
    expect(sessionTokenTotal(claude[0].data.usage)).toBe(6);
    const zeroDuration = [{ type: "session.result", data: { durationMs: 0 } }];
    expect(generatePlainTextSummary(zeroDuration)).toContain("Duration: 0s");
    expect(generateInformationSection(projectSessionResult(zeroDuration))).toContain("Duration:** 0m 0s");
  });

  it("T-UAS-042/045: all default summary views omit retained user prompts and read nonterminal-position results", () => {
    const conversation = generateConversationMarkdown(canonical, { formatToolCallback: formatToolUse, formatInitCallback: formatInitializationSummary }).markdown;
    const plain = generatePlainTextSummary(canonical);
    const cli = generateCopilotCliStyleSummary(canonical);
    for (const text of [conversation, plain, cli]) {
      expect(text).not.toContain("PRIVATE_USER_PROMPT");
      expect(text).toContain("Done.");
      expect(text).toContain("false");
    }
    expect(plain).toContain("Turns: 0");
    expect(plain).toContain("Cost: $0.0000");
    expect(generateInformationSection(projectSessionResult(canonical))).toContain("Input: 0");
  });

  it("T-UAS-040/042: Pi's later observed model is displayed without backfilling source initialization", () => {
    const events = freeze([
      { type: "session.init", data: { sourceEngine: "pi", sessionId: "pi-session" } },
      { type: "pi.message_snapshot", data: { model: "observed-pi-model" } },
    ]);
    expect(generatePlainTextSummary(events)).toContain("Model: observed-pi-model");
    expect(generateCopilotCliStyleSummary(events)).toContain("Model: observed-pi-model");
    expect(generateConversationMarkdown(events, { formatToolCallback: formatToolUse, formatInitCallback: formatInitializationSummary }).markdown).toContain("Observed Model:** observed-pi-model");
    expect(events[0].data.model).toBeUndefined();
  });

  it("T-UAS-007/026: creating an event preserves and isolates arbitrary metadata", () => {
    const source = freeze({ id: "native", timestamp: 0, nativeExtra: { value: 0 } });
    const event = createSessionEvent(source, "assistant.message", { content: " \r\n" });
    expect(event).toMatchObject(source);
    event.nativeExtra.value = 1;
    expect(source.nativeExtra.value).toBe(0);
  });

  it("T-UAS-016/017: malformed-record recovery does not conceal normalization defects", () => {
    const clone = vi.spyOn(globalThis, "structuredClone").mockImplementation(() => {
      throw new Error("fixture normalization failure");
    });
    try {
      expect(() => parseDebugLogFormat('[DEBUG] data:\n{"choices":[{"message":{"content":"fixture"}}]}')).toThrow("fixture normalization failure");
    } finally {
      clone.mockRestore();
    }
  });

  describe.each(parsers)("%s engine canonical interoperability", (_engine, parser) => {
    it("T-UAS-004/006/008/025/029/036-041: accepts canonical arrays without losing extensions", () => {
      const result = parser(JSON.stringify(canonical));
      // An adapter may recover an absent tool name from its exact matching start.
      expect(result.logEntries).toMatchObject(canonical);
      expect(parser(JSON.stringify(result.logEntries)).logEntries).toEqual(result.logEntries);
      expect(result.logEntries.every(e => typeof e.type === "string" && e.data && typeof e.data === "object")).toBe(true);
    });

    it("T-UAS-016/018/020: recovers mixed adjacent JSONL records and preserves whitespace", () => {
      const input = [
        "debug noise",
        '{"broken":',
        JSON.stringify({ type: "vendor.progress", data: { index: 1 } }),
        JSON.stringify({ type: "assistant", message: { content: [{ type: "text", text: " \n" }] } }),
        JSON.stringify({ type: "result", num_turns: 0, usage: { input_tokens: 0 } }),
      ].join("\n");
      const result = parser(input);
      expect(result.logEntries.map(e => e.type)).toEqual(["vendor.progress", "assistant.message", "session.result"]);
      expect(result.logEntries[1].data.content).toBe(" \n");
      expect(result.logEntries[2].data.numTurns).toBe(0);
    });
  });

  it.each(parsers)("T-UAS-017/041/045: %s rejects unsupported records without publishing raw prompts", (_engine, parser) => {
    const result = parser('{"unknown":"PRIVATE_USER_PROMPT"}');
    expect(result.logEntries).toEqual([]);
    expect(result.markdown).not.toContain("PRIVATE_USER_PROMPT");
  });
});
