import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { loadEngineLogParser } from "./engine_log_parser.cjs";
import { runLogParser } from "./log_parser_bootstrap.cjs";
import { collectUnifiedSession, mergeSessionSources, parseEngineSession } from "./unified_session.cjs";
import { serializeSessionArtifact } from "./session_artifact.cjs";
import { sessionCLI } from "./session_cli.cjs";

const { infrastructureOnly, bootstrapResult } = require("./fixtures/crush_ci_sessions.cjs");
const parse = content => loadEngineLogParser("crush")(content).logEntries;
const jsonl = records => records.map(record => JSON.stringify(record)).join("\n") + "\n";

describe("Crush quiet stdout session preservation", () => {
  let root;
  let originalEnv;
  let originalCore;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-crush-session-"));
    originalEnv = { ...process.env };
    originalCore = global.core;
    global.core = {
      info: vi.fn(),
      warning: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn() },
    };
  });
  afterEach(() => {
    process.env = originalEnv;
    global.core = originalCore;
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });

  it("does not invent a session or accounting from the sampled timed-out run", () => {
    expect(parse(infrastructureOnly)).toEqual([]);
    expect(parseEngineSession(infrastructureOnly, "crush")).toEqual([]);
    const events = parse(infrastructureOnly + jsonl([bootstrapResult]));
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({ type: "session.result", num_turns: 0, data: { sourceEngine: "crush", sourceType: "result", numTurns: 0 } });
    expect(events[0].data.usage).toBeUndefined();
    expect(events[0].data).not.toHaveProperty("model");
  });

  it("persists no fabricated zero-turn result when Actions captures only infrastructure", async () => {
    const log = path.join(root, "agent-stdio.log");
    fs.writeFileSync(log, infrastructureOnly);
    fs.writeFileSync(path.join(root, "agent_execution_exit_code.txt"), "0\n");
    process.env.GH_AW_AGENT_OUTPUT = log;
    await runLogParser({ parseLog: loadEngineLogParser("crush"), parserName: "Crush", rootDir: root });
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(fs.readFileSync(log, "utf8")).toBe(infrastructureOnly);
    const persisted = fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8");
    expect(persisted).toBe(jsonl([{ type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [], exitCode: 0 } }]));
    const events = collectUnifiedSession({ rootDir: root, engine: "crush" }).events;
    expect(events.filter(event => event.provenance.component === "agent")).toEqual([]);
    expect(events.filter(event => event.type === "agent.execution")).toHaveLength(1);
    expect(events.some(event => event.type === "session.init" || event.type === "session.result")).toBe(false);
  });

  it("keeps exact quiet text and does not interpret prose as tools, errors, refusals, or turns", () => {
    const text = "  Assistant: A hard break.  \r\n\r\nRunning bash is the next task.\r\nTool view is unavailable.\r\nI cannot finish before the turn limit; MCP server example failed.\r\n  \r\n";
    const expected = [{ type: "assistant.message", data: { content: text } }];
    expect(parse(text)).toEqual(expected);
    expect(parseEngineSession(text, "crush")).toEqual(expected);
    const result = loadEngineLogParser("crush")(text);
    expect(result.mcpFailures).toEqual([]);
    expect(result.maxTurnsHit).toBe(false);
  });

  it("preserves malformed and ordinary JSON as captured text instead of dropping it", () => {
    const text = '{"answer":false,"count":0}\n{"type":"native.unsupported","message":"still stdout"}\n{"truncated":\n';
    expect(parse(text)).toEqual([{ type: "assistant.message", data: { content: text } }]);
  });

  it("retains all supplied canonical signatures and opaque extensions without duplicating snapshots", () => {
    // Synthetic contract coverage, not a claim that quiet Crush emits this protocol.
    const records = [
      { type: "session.init", id: "init", timestamp: 0, parentId: null, nativeFlag: false, data: { sourceEngine: "crush", model: "", sessionId: null, cwd: "" } },
      { type: "user.message", data: { content: "" } },
      { type: "assistant.reasoning", data: { content: "  reasoning\n", partial: true, delta: false } },
      { type: "assistant.refusal", data: { reason: "content_filter", content: "", policyCategory: null, explanation: null, partial: false } },
      { type: "tool.execution_start", id: "start", timestamp: 0, data: { toolName: "probe", input: false } },
      { type: "tool.execution_complete", parentId: null, data: { toolName: "probe", output: 0, success: false, error: null, durationMs: 0, exitCode: 0 } },
      { type: "tool.execution_start", data: { toolCallId: "dangling", toolName: "probe", input: {} } },
      { type: "session.error", data: { message: " exact error \n", code: 0, retryable: false } },
      { type: "vendor.progress", id: "", timestamp: 0, extra: null, data: { text: "", flag: false, count: 0, object: {}, array: [], absent: null } },
      {
        type: "session.result",
        data: {
          numTurns: 0,
          durationMs: 0,
          totalCostUsd: 0,
          status: "error",
          errors: [],
          permissionDenials: [],
          usage: { input_tokens: 0, output_tokens: 0, reasoning_output_tokens: 0, cache_read_input_tokens: 0, input_tokens_include_cache: false },
        },
      },
    ];
    expect(parse(jsonl(records))).toEqual(records);
    expect(parse(JSON.stringify(records, null, 2))).toEqual(records);
    expect(parseEngineSession(jsonl(records), "crush").filter(event => event.type !== "agent.execution")).toEqual(records);
    expect(parse(serializeSessionArtifact(records))).toEqual(records);
    const unified = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events: records }]);
    expect(unified.find(event => event.type === "assistant.reasoning").data.content).toBe("  reasoning\n");
    expect(unified.find(event => event.type === "assistant.refusal").data).toEqual(records[3].data);
    expect(unified.find(event => event.type === "tool.execution_start").data).toEqual(records[4].data);
    expect(unified.find(event => event.type === "tool.execution_complete").data).toEqual(records[5].data);
    expect(unified.find(event => event.type === "vendor.progress").data).toEqual(records[8].data);
    expect(unified.find(event => event.type === "vendor.progress")).toMatchObject({ id: "", timestamp: 0, provenance: { index: 8, timestampMs: 0 } });
    expect(unified.filter(event => event.type === "session.result")).toHaveLength(1);
    expect(unified.at(-1).data.usage).toEqual({ inputTokens: 0, outputTokens: 0, reasoningOutputTokens: 0, cacheReadInputTokens: 0, inputTokensIncludeCache: false });
  });

  it("normalizes supplied compatibility tools without inventing IDs, inputs, or successful outcomes", () => {
    const records = [
      { type: "assistant", id: "native", timestamp: 0, message: { content: [{ type: "tool_use", id: "call", name: "probe", input: null }] } },
      { type: "user", parentId: null, message: { content: [{ type: "tool_result", tool_use_id: "call", content: false }] } },
    ];
    const events = parse(jsonl(records));
    expect(events.map(event => event.type)).toEqual(["tool.execution_start", "tool.execution_complete"]);
    expect(events[0]).toMatchObject({ id: "native", timestamp: 0, data: { toolCallId: "call", toolName: "probe", input: null } });
    expect(events[1]).toMatchObject({ parentId: null, data: { toolCallId: "call", toolName: "probe", output: false } });
    expect(events[1].data.success).toBeUndefined();
    expect(serializeSessionArtifact(events)).not.toContain('"success"');
  });

  it("maps observed compatibility accounting fields without inventing or summing totals", () => {
    const record = {
      type: "result",
      id: "",
      timestamp: 0,
      status: "error",
      num_turns: 0,
      duration_ms: 0,
      total_cost_usd: 0,
      errors: [],
      permission_denials: [],
      usage: { input_tokens: 0, output_tokens: 0, cache_read_input_tokens: 0, input_tokens_include_cache: false },
    };
    const events = parse(jsonl([record]));
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({ id: "", timestamp: 0, data: { sourceEngine: "crush", sourceType: "result", status: "error", numTurns: 0, durationMs: 0, totalCostUsd: 0, errors: [], permissionDenials: [], usage: record.usage } });
    const [unified] = mergeSessionSources([{ component: "agent", phase: "agent", path: "agent-session.jsonl", events }]);
    expect(unified.data).toEqual({
      sourceEngine: "crush",
      sourceType: "result",
      status: "error",
      numTurns: 0,
      durationMs: 0,
      totalCostUsd: 0,
      errors: [],
      permissionDenials: [],
      usage: { inputTokens: 0, outputTokens: 0, cacheReadInputTokens: 0, inputTokensIncludeCache: false },
    });
    expect(parse(jsonl([{ type: "result", status: "interrupted" }]))[0].data).toMatchObject({ sourceEngine: "crush", sourceType: "result", status: "interrupted" });
    expect(parse(JSON.stringify([record]))).toEqual(events);
  });

  it("does not append or duplicate an observed accounting snapshot during Actions publication", async () => {
    const record = { type: "result", status: "error", num_turns: 0, usage: { input_tokens: 0, output_tokens: 0 } };
    const native = infrastructureOnly + "  Captured answer. \n" + jsonl([record]);
    const log = path.join(root, "agent-stdio.log");
    fs.writeFileSync(log, native);
    process.env.GH_AW_AGENT_OUTPUT = log;
    await runLogParser({ parseLog: loadEngineLogParser("crush"), parserName: "Crush", rootDir: root });
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(fs.readFileSync(log, "utf8")).toBe(native);
    const events = fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8").trim().split("\n").map(JSON.parse);
    expect(events.filter(event => event.type === "session.result")).toHaveLength(1);
    expect(events.find(event => event.type === "session.result").data).toMatchObject({ sourceEngine: "crush", sourceType: "result", status: "error", numTurns: 0, usage: record.usage });
    const unified = collectUnifiedSession({ rootDir: root, engine: "crush" }).events;
    expect(unified.filter(event => event.type === "session.result")).toHaveLength(1);
    expect(unified.find(event => event.type === "session.result").data.usage).toEqual({ inputTokens: 0, outputTokens: 0 });
  });

  it("keeps mixed stdout and canonical observations in order through Actions and CLI artifacts", async () => {
    const text = "  Partial answer.\n\n";
    const extension = { type: "vendor.progress", data: { ready: false, count: 0 } };
    const tail = "Running bash is only prose. \n";
    const native = infrastructureOnly + text + jsonl([extension]) + tail;
    const expected = [{ type: "assistant.message", data: { content: text } }, extension, { type: "assistant.message", data: { content: tail } }];
    const log = path.join(root, "agent-stdio.log");
    fs.writeFileSync(log, native);
    process.env.GH_AW_AGENT_OUTPUT = log;
    expect(parseEngineSession(native, "crush")).toEqual(expected);
    expect(
      collectUnifiedSession({ rootDir: root, engine: "crush" })
        .events.filter(event => event.provenance.component === "agent")
        .map(({ type, data }) => ({ type, data }))
    ).toEqual(expected);
    const fromNative = sessionCLI(["reconstruct", root, "crush"]).trim().split("\n").map(JSON.parse);
    expect(fromNative.filter(event => event.provenance.component === "agent").map(({ type, data }) => ({ type, data }))).toEqual(expected);
    await runLogParser({ parseLog: loadEngineLogParser("crush"), parserName: "Crush", rootDir: root });
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8")).toBe(serializeSessionArtifact(expected));
    const unified = collectUnifiedSession({ rootDir: root, engine: "crush" }).events;
    expect(unified.filter(event => event.provenance.component === "agent").map(({ type, data }) => ({ type, data }))).toEqual(expected);
    const reconstructed = sessionCLI(["reconstruct", root, "crush"]).trim().split("\n").map(JSON.parse);
    expect(reconstructed.filter(event => event.provenance.component === "agent").map(({ type, data }) => ({ type, data }))).toEqual(expected);
    expect(sessionCLI(["agent-markdown", log, "crush"])).toContain("Partial answer.");
  });
});
