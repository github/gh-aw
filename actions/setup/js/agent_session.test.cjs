import { describe, it, expect, vi } from "vitest";
import {
  normalizeAgentSession,
  createSessionEvent,
  selectSessionResult,
  projectSessionResult,
  normalizeSessionUsage,
  accumulateSessionUsage,
  reconcileSessionUsage,
  sessionOutputText,
  sessionToolSuccess,
  sessionTokenTotal,
  sessionContext,
  isNestedSessionEvent,
  isSingleNestedSession,
  projectSessionInitialization,
  observedSessionModel,
} from "./agent_session.cjs";
import { scopedAgentSessions } from "./unified_session_render.cjs";
import { normalizeUnifiedSessionEvent } from "./unified_session_payload.cjs";
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
  it("recognizes native OpenCode sessionID without overriding existing context precedence", () => {
    const native = { type: "tool.execution_complete", sessionID: "opencode-session", data: {} };
    expect(sessionContext(native)).toEqual({ sessionId: "opencode-session" });
    expect(sessionContext({ ...native, data: { sessionId: null } })).toEqual({ sessionId: null });
    expect(sessionContext({ ...native, sessionId: "canonical" })).toEqual({ sessionId: "canonical" });
    expect(sessionContext({ ...native, session_id: "legacy" })).toEqual({ sessionId: "legacy" });
    expect(sessionContext({ ...native, data: { session_id: "legacy-data" } })).toEqual({ sessionId: "legacy-data" });
    expect(sessionContext({ ...native, sessionID: 123 })).toEqual({});
  });

  it("retains observed assistant reasoning alongside provider refusals", () => {
    const source = freeze({
      object: "chat.completion",
      id: "response",
      choices: [{ message: { role: "assistant", reasoning_text: "observed reasoning\n", refusal: "declined\n" } }, { message: { role: "user", reasoning_text: "not assistant reasoning", content: "prompt" } }],
    });
    const events = normalizeAgentSession([source]);
    expect(events.map(event => event.type)).toEqual(["assistant.reasoning", "assistant.refusal"]);
    expect(events[0].data.content).toBe("observed reasoning\n");
    expect(events[1].data).toMatchObject({ reason: "refusal", content: "declined\n" });
    expect(events.every(event => event.id === "response")).toBe(true);
    expect(normalizeAgentSession(events)).toEqual(events);
  });

  it("separates agentId-only child evidence without inventing a parent tool ID", () => {
    const child = [
      { type: "session.init", data: { sourceEngine: "pi", agentId: "legacy:pi:worker:1", model: "child-model" } },
      { type: "assistant.message", agentId: "legacy:pi:worker:1", data: { content: "child answer" } },
      { type: "session.result", data: { agentId: "legacy:pi:worker:1", usage: { input_tokens: 99 } } },
    ];
    const root = [
      { type: "session.init", data: { sourceEngine: "pi", model: "root-model" } },
      { type: "assistant.message", data: { content: "root answer" } },
      { type: "session.result", data: { usage: { input_tokens: 2 } } },
    ];
    const events = [...root, ...child];
    expect(isSingleNestedSession(child)).toBe(true);
    expect(isSingleNestedSession(events)).toBe(false);
    expect(isNestedSessionEvent({ type: "subagent.started", agentId: "target", data: {} })).toBe(false);
    expect(sessionContext(child[1])).toEqual({ agentId: "legacy:pi:worker:1" });
    expect(projectSessionResult(events).usage.input_tokens).toBe(2);
    expect(projectSessionInitialization(events).model).toBe("root-model");
    expect(observedSessionModel(events)).toBe("root-model");
    expect(observedSessionModel(child, { includeNested: true })).toBe("child-model");
    const groups = scopedAgentSessions(events);
    expect(groups).toHaveLength(2);
    expect(groups[0].events).toEqual(root);
    expect(groups[1].events).toEqual(child);
    expect(groups[1].label).toContain("agentId=legacy:pi:worker:1");
    expect(groups[1].label).not.toContain("parentToolUseId");
    for (const render of [generatePlainTextSummary, generateCopilotCliStyleSummary]) {
      const output = render(events);
      expect(output).toContain("agentId=legacy:pi:worker:1");
      expect(output.indexOf("root answer")).toBeLessThan(output.indexOf("agentId=legacy:pi:worker:1"));
      expect(output.indexOf("child answer")).toBeGreaterThan(output.indexOf("agentId=legacy:pi:worker:1"));
      expect(render(child)).toContain("child answer");
      expect(render(child)).toContain("99");
    }
  });

  it("does not aggregate agentId-only Claude child snapshots into root sessions", () => {
    const events = [
      { type: "session.result", data: { sourceEngine: "claude", sessionId: "first", usage: { input_tokens: 2 } } },
      { type: "session.result", data: { sourceEngine: "claude", sessionId: "second", usage: { input_tokens: 3 } } },
      { type: "session.result", data: { sourceEngine: "claude", sessionId: "child", agentId: "worker", usage: { input_tokens: 99 }, errors: ["child-only"] } },
    ];
    expect(projectSessionResult(events).usage.input_tokens).toBe(5);
    expect(projectSessionResult(events).errors).toBeUndefined();
  });

  it("keeps Pi parent tool-call relationships in the root session group", () => {
    const events = [
      { type: "session.init", data: { sourceEngine: "pi", sessionId: "pi-root" } },
      { type: "tool.execution_start", data: { toolCallId: "nested-tool", parentToolCallId: "root-tool", toolName: "bash", input: false } },
      { type: "tool.execution_complete", data: { toolCallId: "nested-tool", parentToolCallId: "root-tool", output: 0, success: true } },
      { type: "session.result", data: { sourceEngine: "pi", parentToolCallId: "root-tool", usage: { input_tokens: 0 } } },
    ];
    expect(sessionContext(events[1])).toEqual({});
    expect(sessionContext({ ...events[1], parentToolCallId: "envelope-tool" })).toEqual({});
    const grouped = scopedAgentSessions(events);
    expect(grouped).toHaveLength(1);
    expect(grouped[0].events).toHaveLength(4);
    expect(projectSessionResult(events).usage.input_tokens).toBe(0);
  });

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

  it("projects text-block arrays without treating ordinary structured arrays as MCP content", () => {
    const blocks = freeze([
      { type: "text", text: "line one" },
      { type: "text", text: "line two" },
    ]);
    expect(sessionOutputText(blocks)).toBe("line one\nline two");
    for (const value of [[], [false, 0, null], [{ text: "ordinary data" }], [{ type: "text", text: "partial" }, { value: 0 }]]) {
      expect(sessionOutputText(value)).toBe(JSON.stringify(value, null, 2));
    }
    expect(blocks).toEqual([
      { type: "text", text: "line one" },
      { type: "text", text: "line two" },
    ]);
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
    expect(usage.cacheCreationInputTokens).toBeUndefined();
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

  it("T-UAS-030: canonical usage fields win over camelCase aliases by presence", () => {
    expect(sessionTokenTotal({ total_tokens: 7, totalTokens: 99 })).toBe(7);
    expect(sessionTokenTotal({ total_tokens: -1, totalTokens: 99, input_tokens: 2, output_tokens: 3 })).toBe(5);
    expect(sessionTokenTotal({ input_tokens: 7, inputTokens: 99, output_tokens: 2 })).toBe(9);
    expect(sessionTokenTotal({ input_tokens: 10, output_tokens: 2, cache_read_input_tokens: 7, cacheReadInputTokens: 99, input_tokens_include_cache: true, inputTokensIncludeCache: false })).toBe(12);
    expect(sessionTokenTotal({ input_tokens: 10, output_tokens: 2, cache_read_input_tokens: 7, input_tokens_include_cache: false, inputTokensIncludeCache: true })).toBe(19);
  });

  it("prefers the canonical overflow marker by property presence", () => {
    expect(normalizeSessionUsage({ input_tokens: 10, overflowed_tokens: [], overflowedTokens: ["inputTokens"] })).toMatchObject({ input_tokens: 10, overflowed_tokens: [] });
    expect(normalizeSessionUsage({ input_tokens: 10, overflowed_tokens: ["input_tokens"], overflowedTokens: [] })).toMatchObject({ overflowed_tokens: ["input_tokens"] });
  });

  it.each([
    ["cache_read_input_tokens", "cacheReadTokens", "cacheReadInputTokens"],
    ["cache_creation_input_tokens", "cacheWriteTokens", "cacheCreationInputTokens"],
    ["cache_read_input_tokens", "cache_read_tokens", "cacheReadInputTokens"],
    ["cache_creation_input_tokens", "cache_write_tokens", "cacheCreationInputTokens"],
  ])("normalizes native cache alias %s / %s without overriding canonical evidence", (key, alias, primaryAlias) => {
    expect(normalizeSessionUsage({ [alias]: 0 })[key]).toBe(0);
    expect(normalizeSessionUsage({ [alias]: 95360 })[key]).toBe(95360);
    expect(normalizeSessionUsage({ [key]: 0, [alias]: 42 })[key]).toBe(0);
    expect(normalizeSessionUsage({ [primaryAlias]: 3, [alias]: 42 })[key]).toBe(3);
    const invalid = normalizeSessionUsage({ [key]: -1, [alias]: 42 });
    expect(invalid[key]).toBeUndefined();
    expect(invalid[alias]).toBeUndefined();
    expect(normalizeSessionUsage({ [alias]: "42" })[key]).toBeUndefined();
    const overflow = normalizeSessionUsage({ [alias]: 42, overflowed_tokens: [alias] });
    expect(overflow.overflowed_tokens).toEqual([key]);
    expect(overflow[key]).toBeUndefined();
    expect(overflow[alias]).toBeUndefined();
    const usage = {};
    accumulateSessionUsage(usage, { [alias]: Number.MAX_SAFE_INTEGER });
    accumulateSessionUsage(usage, { [alias]: 1 });
    accumulateSessionUsage(usage, { [alias]: 1 });
    expect(normalizeSessionUsage(usage)[key]).toBeUndefined();
    expect(usage.overflowed_tokens).toEqual([key]);
  });

  it.each([
    ["cache_read_input_tokens", "cacheReadInputTokens", "cacheReadTokens", "cache_read_tokens"],
    ["cache_creation_input_tokens", "cacheCreationInputTokens", "cacheWriteTokens", "cache_write_tokens"],
  ])("never resurrects rejected %s aliases through unified projection or totals", (key, primaryAlias, secondaryAlias, nativeAlias) => {
    for (const rejected of [
      { [key]: -1, [primaryAlias]: 42, [secondaryAlias]: 42, [nativeAlias]: 42 },
      { [primaryAlias]: "x", [secondaryAlias]: 42, [nativeAlias]: 42 },
      { [secondaryAlias]: -1, [nativeAlias]: 42 },
    ]) {
      const source = freeze({ inputTokens: 10, outputTokens: 2, inputTokensIncludeCache: false, ...rejected });
      const normalized = normalizeSessionUsage(source);
      for (const name of [key, primaryAlias, secondaryAlias, nativeAlias]) expect(Object.hasOwn(normalized, name)).toBe(false);
      expect(normalizeSessionUsage(normalized)).toEqual(normalized);
      const event = normalizeUnifiedSessionEvent({ type: "session.result", data: { usage: normalized } });
      for (const name of [key, primaryAlias, secondaryAlias, nativeAlias]) expect(Object.hasOwn(event.data.usage, name)).toBe(false);
      expect(sessionTokenTotal(event.data.usage)).toBe(12);
      expect(sessionTokenTotal(projectSessionResult([event]).usage)).toBe(12);
      const accumulated = {};
      accumulateSessionUsage(accumulated, source);
      expect(sessionTokenTotal(accumulated)).toBe(12);
    }
    for (const selected of [key, primaryAlias, secondaryAlias]) {
      const normalized = normalizeSessionUsage({ [selected]: 0, [nativeAlias]: 42 });
      expect(normalized[key]).toBe(0);
      expect(normalizeUnifiedSessionEvent({ type: "session.result", data: { usage: normalized } }).data.usage[primaryAlias]).toBe(0);
    }
    const overflow = normalizeSessionUsage({ [secondaryAlias]: 42, [nativeAlias]: 42, overflowed_tokens: [secondaryAlias] });
    expect(overflow.overflowed_tokens).toEqual([key]);
    const projected = normalizeUnifiedSessionEvent({ type: "session.result", data: { usage: overflow } });
    expect(projected.data.usage[primaryAlias]).toBeUndefined();
    expect(sessionTokenTotal(projected.data.usage)).toBeUndefined();
  });

  it.each(["bogus", "__proto__", "constructor", "prototype", "toString", "hasOwnProperty", "valueOf"])("ignores hostile overflow key %s in normalization, reconciliation and accumulation", key => {
    const source = freeze({ input_tokens: 4, [key]: { kept: true }, overflowed_tokens: [key, null, 17, ["input_tokens"], {}] });
    const normalized = normalizeSessionUsage(source);
    expect(normalized).toMatchObject({ input_tokens: 4, overflowed_tokens: [] });
    expect(Object.hasOwn(normalized, key)).toBe(true);
    expect(normalized[key]).toEqual({ kept: true });
    const reconciled = reconcileSessionUsage(source, { output_tokens: 2, overflowed_tokens: [key] });
    expect(reconciled).toMatchObject({ input_tokens: 4, output_tokens: 2 });
    expect(reconciled.overflowed_tokens).toBeUndefined();
    expect(reconciled[key]).toEqual({ kept: true });
    const accumulated = { ...source };
    accumulateSessionUsage(accumulated, { inputTokens: 3, outputTokens: 0, overflowed_tokens: [key] });
    expect(accumulated).toMatchObject({ input_tokens: 7, output_tokens: 0 });
    expect(accumulated.overflowed_tokens).toBeUndefined();
    expect(accumulated[key]).toEqual({ kept: true });
  });

  it("retains recognized overflow evidence while discarding hostile prior markers", () => {
    const previous = { output_tokens: 2, overflowed_tokens: ["inputTokens", "constructor", "__proto__"] };
    expect(reconcileSessionUsage(previous, { outputTokens: 0 })).toMatchObject({ output_tokens: 0, overflowed_tokens: ["input_tokens"] });
    expect(reconcileSessionUsage(previous, { inputTokens: 0 }).overflowed_tokens).toBeUndefined();
    const accumulated = { ...previous };
    accumulateSessionUsage(accumulated, { inputTokens: 42, outputTokens: 0 });
    expect(accumulated).toEqual({ output_tokens: 2, overflowed_tokens: ["input_tokens"] });
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

  it("T-UAS-031/032: overflowing contributions stay unavailable, including after serialization", () => {
    let usage = {};
    accumulateSessionUsage(usage, { input_tokens: Number.MAX_SAFE_INTEGER, output_tokens: 1 });
    const first = { type: "session.result", data: { usage: { ...usage } } };
    accumulateSessionUsage(usage, { input_tokens: 1, output_tokens: 2 });
    expect(usage.input_tokens).toBeUndefined();
    usage = JSON.parse(JSON.stringify(usage));
    accumulateSessionUsage(usage, { input_tokens: 0, output_tokens: 3 });
    accumulateSessionUsage(usage, { input_tokens: 4 });
    expect(usage.input_tokens).toBeUndefined();
    expect(usage.output_tokens).toBe(6);
    const events = [first, { type: "session.result", data: { usage } }];
    expect(projectSessionResult(events).usage.input_tokens).toBeUndefined();
    expect(projectSessionResult(JSON.parse(JSON.stringify(events))).usage.output_tokens).toBe(6);
    expect(projectSessionResult([...events, { type: "session.result", data: { usage: { input_tokens: 8 } } }]).usage.input_tokens).toBe(8);
    expect(sessionTokenTotal({ input_tokens: Number.MAX_SAFE_INTEGER, output_tokens: 1 })).toBeUndefined();
    expect(sessionTokenTotal({ input_tokens: Number.MAX_SAFE_INTEGER, output_tokens: 0 })).toBe(Number.MAX_SAFE_INTEGER);
  });

  it.each([
    ["input_tokens", "inputTokens"],
    ["output_tokens", "outputTokens"],
    ["reasoning_output_tokens", "reasoningOutputTokens"],
    ["cache_creation_input_tokens", "cacheCreationInputTokens"],
    ["cache_read_input_tokens", "cacheReadInputTokens"],
  ])("keeps overflowed %s unavailable after further %s contributions", (key, alias) => {
    const usage = {};
    accumulateSessionUsage(usage, { [alias]: Number.MAX_SAFE_INTEGER });
    accumulateSessionUsage(usage, { [key]: 1 });
    accumulateSessionUsage(usage, { [alias]: 5 });
    expect(normalizeSessionUsage(usage)[key]).toBeUndefined();
    expect(usage.overflowed_tokens).toEqual([key]);
    const result = selectSessionResult([
      { type: "session.result", data: { usage } },
      { type: "session.result", data: { usage: { [alias]: 0 } } },
    ]);
    expect(result.usage[key]).toBe(0);
    expect(result.usage.overflowed_tokens).toBeUndefined();
  });

  it("ignores invalid overflow metadata without discarding valid counts", () => {
    const result = selectSessionResult([
      { type: "session.result", data: { usage: { input_tokens: 4, overflowed_tokens: 17 } } },
      { type: "session.result", data: { usage: { output_tokens: 2, overflowed_tokens: [null, ["input_tokens"], "unknown"] } } },
    ]);
    expect(result.usage).toMatchObject({ input_tokens: 4, output_tokens: 2 });
    expect(result.usage.overflowed_tokens).toBeUndefined();
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
      const expected = _engine === "claude" || _engine === "custom" ? canonical.map(event => ({ ...event, type: `claude.${event.type}`, data: { ...event.data, sourceEngine: "claude" } })) : canonical;
      // An adapter may recover an absent tool name from its exact matching start.
      expect(result.logEntries).toMatchObject(expected);
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
      expect(result.logEntries.map(e => e.type)).toEqual([_engine === "claude" || _engine === "custom" ? "claude.vendor.progress" : "vendor.progress", "assistant.message", "session.result"]);
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
