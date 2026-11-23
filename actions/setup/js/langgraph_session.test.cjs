import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import Module from "node:module";
import { normalizeLangGraphSession, parseLangGraphLog } from "./langgraph_session.cjs";
import { loadEngineLogParser, parseBehaviorLog } from "./engine_log_parser.cjs";
import { collectUnifiedSession, writeUnifiedSession } from "./unified_session.cjs";
import { runLogParser } from "./log_parser_bootstrap.cjs";
import { validateSession } from "./scripts/validate_session.cjs";
import { computeInclusiveInputTokens } from "./model_costs.cjs";

// Synthetic messages_to_dict shape observed in LangGraph 1.2.23 trajectories.
const trajectory = {
  exit_status: "Completed",
  messages: [
    { type: "human", data: { id: "human", content: "  Inspect the fixture.\n" } },
    {
      type: "ai",
      data: {
        id: "first-ai",
        content: "",
        tool_calls: [{ id: "call-1", name: "run_shell", args: { command: "printf fixture" } }],
        invalid_tool_calls: [],
        usage_metadata: { input_tokens: 10, output_tokens: 2, total_tokens: 12, input_token_details: { cache_read: 4 } },
        response_metadata: { model_name: "fixture-model" },
      },
    },
    { type: "tool", data: { id: "first-tool", tool_call_id: "call-1", content: "exit code: 1\nstdout:\n\nstderr:\nfixture error", status: "success" } },
    { type: "ai", data: { id: "second-ai", content: "Retry.", tool_calls: [{ id: "call-2", name: "read_file", args: { path: "fixture.txt" } }], usage_metadata: { input_tokens: 20, output_tokens: 3, total_tokens: 23 } } },
    { type: "tool", data: { id: "second-tool", tool_call_id: "call-2", content: "fixture", status: "success" } },
    { type: "ai", data: { id: "last-ai", content: "  Done.\n", tool_calls: [], usage_metadata: { input_tokens: 30, output_tokens: 4, total_tokens: 34, output_token_details: { reasoning: 1 } } } },
  ],
};

describe("LangGraph native trajectory pipeline", () => {
  let root;
  let originalEnv;
  let originalCore;
  beforeEach(() => {
    root = fs.mkdtempSync(path.resolve(".gh-aw-langgraph-session-"));
    originalEnv = { ...process.env };
    originalCore = global.core;
    process.env = { PATH: process.env.PATH, HOME: root, RUNNER_TEMP: root, GH_AW_AGENT_OUTPUT: path.join(root, "agent-stdio.log") };
    global.core = {
      info: vi.fn(),
      debug: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
      exportVariable: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) },
    };
  });
  afterEach(() => {
    process.env = originalEnv;
    global.core = originalCore;
    vi.restoreAllMocks();
    fs.rmSync(root, { recursive: true, force: true });
  });
  const write = (name, content) => {
    const file = path.join(root, name);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, content);
    return file;
  };

  it("parses the native file through the trusted declared parser with exact messages, calls, and per-response usage", () => {
    const raw = JSON.stringify(trajectory, null, 2);
    expect(loadEngineLogParser("langgraph")).toBeTypeOf("function");
    const events = parseBehaviorLog(raw, "langgraph").logEntries;
    expect(events).toEqual(normalizeLangGraphSession(trajectory));
    expect(events.filter(event => event.type === "user.message").map(event => event.data.content)).toEqual(["  Inspect the fixture.\n"]);
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["", "Retry.", "  Done.\n"]);
    expect(events.filter(event => event.type === "tool.execution_start").map(event => event.data)).toEqual([
      { sourceEngine: "langgraph", toolCallId: "call-1", toolName: "run_shell", input: { command: "printf fixture" } },
      { sourceEngine: "langgraph", toolCallId: "call-2", toolName: "read_file", input: { path: "fixture.txt" } },
    ]);
    const results = events.filter(event => event.type === "tool.execution_complete");
    expect(results.map(event => event.data.toolCallId)).toEqual(["call-1", "call-2"]);
    expect(results[0].data).toMatchObject({ toolName: "run_shell", status: "success", output: trajectory.messages[2].data.content });
    expect(results.every(event => !Object.hasOwn(event.data, "success"))).toBe(true);
    expect(events.filter(event => event.type === "usage.report").map(event => event.data.usage)).toEqual([
      { input_tokens: 10, output_tokens: 2, total_tokens: 12, cache_read_input_tokens: 4 },
      { input_tokens: 20, output_tokens: 3, total_tokens: 23 },
      { input_tokens: 30, output_tokens: 4, total_tokens: 34, reasoning_output_tokens: 1 },
    ]);
    expect(events.at(-1)).toEqual({ type: "session.result", data: { sourceEngine: "langgraph", status: "Completed", numTurns: 3 } });
    expect(events.every(event => event.timestamp === undefined)).toBe(true);
    expect(events.find(event => event.id === "last-ai").provenance).toEqual({ messageIndex: 5, sourceType: "ai" });
  });

  it("prefers the final stdout snapshot over canonical-looking JSON in the answer", async () => {
    const decoy = '{"type":"assistant.message","data":{"content":"not a separate event"}}';
    const raw = `${decoy}\nLangGraph exit status: Completed\n${JSON.stringify(trajectory)}\n`;
    write("agent-stdio.log", raw);
    write("agent_execution_exit_code.txt", "0");
    await runLogParser({ parserName: "LangGraph", parseLog: content => parseBehaviorLog(content, "langgraph"), rootDir: root });
    const canonical = fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8");
    expect(canonical).not.toContain("not a separate event");
    const events = writeUnifiedSession({ rootDir: root, engine: "langgraph", warn: vi.fn() });
    expect(validateSession(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8"))).toBe(events.length);
    expect(events.filter(event => event.type === "assistant.message")).toHaveLength(3);
    expect(events.filter(event => event.type === "tool.execution_start")).toHaveLength(2);
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });

  it("captures the private trajectory without a public stdout snapshot or false bootstrap failure", async () => {
    const secret = "private-langgraph-fixture-credential";
    process.env.GH_AW_SECRET_NAMES = "LANGGRAPH_FIXTURE";
    process.env.SECRET_LANGGRAPH_FIXTURE = secret;
    const sample = structuredClone(trajectory);
    sample.messages[1].data.tool_calls[0].args.command = secret;
    sample.messages[2].data.content = secret;
    write("agent/langgraph.traj.json", JSON.stringify(sample));
    write("agent-stdio.log", "Done.\nLangGraph exit status: Completed\n");
    await runLogParser({ parserName: "LangGraph", parseLog: loadEngineLogParser("langgraph"), rootDir: root });
    const canonical = fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8");
    expect(canonical).toContain("Inspect the fixture.");
    expect(canonical).not.toContain(secret);
    expect(canonical).toContain("***REDACTED***");
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(JSON.stringify(global.core.info.mock.calls)).not.toContain(secret);
    expect(JSON.stringify(global.core.summary.addRaw.mock.calls)).not.toContain(secret);
    expect(fs.readFileSync(path.join(root, "agent-stdio.log"), "utf8")).not.toContain("Inspect the fixture.");
    expect(fs.readFileSync(path.join(root, "agent-stdio.log"), "utf8")).not.toContain('"messages"');
  });

  it.each([
    ["openai", { token_usage: { prompt_tokens: 100, prompt_tokens_details: { cached_tokens: 40 } } }, 0],
    ["anthropic", { usage: { input_tokens: 50, cache_read_input_tokens: 40, cache_creation_input_tokens: 10 } }, 10],
  ])("verifies %s adapter cache inclusion against raw counters without double accounting", (provider, response_metadata, cacheWrite) => {
    const sample = {
      exit_status: "Completed",
      messages: [
        {
          type: "ai",
          data: {
            usage_metadata: { input_tokens: 100, output_tokens: 20, input_token_details: { cache_read: 40, cache_creation: cacheWrite } },
            response_metadata,
          },
        },
      ],
    };
    const usage = normalizeLangGraphSession(sample).find(event => event.type === "usage.report").data.usage;
    expect(usage.input_tokens_include_cache).toBe(true);
    expect(
      computeInclusiveInputTokens({
        provider,
        inputTokens: usage.input_tokens,
        cacheReadTokens: usage.cache_read_input_tokens,
        cacheWriteTokens: usage.cache_creation_input_tokens,
        inputTokensIncludeCache: usage.input_tokens_include_cache,
      })
    ).toBe(100);
    sample.messages[0].data.usage_metadata.input_tokens = 99;
    expect(normalizeLangGraphSession(sample).find(event => event.type === "usage.report").data.usage).not.toHaveProperty("input_tokens_include_cache");
  });

  it("collects historical native files, then prefers canonical capture without duplicating stdout", () => {
    write("agent/langgraph.traj.json", JSON.stringify(trajectory, null, 2));
    write("agent-stdio.log", `  Done.\n${JSON.stringify(trajectory)}\n`);
    const native = writeUnifiedSession({ rootDir: root, engine: "langgraph", warn: vi.fn() });
    const output = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(validateSession(output)).toBe(native.length);
    expect(native.filter(event => event.provenance.component === "agent").every(event => event.provenance.path === "agent/langgraph.traj.json")).toBe(true);
    expect(native.filter(event => event.type === "assistant.message")).toHaveLength(3);
    expect(native.filter(event => event.type === "tool.execution_complete")).toHaveLength(2);
    expect(native.filter(event => event.type === "usage.report").map(event => event.data.usage.totalTokens)).toEqual([12, 23, 34]);
    expect(native.find(event => event.type === "session.result").data).toEqual({ sourceEngine: "langgraph", status: "Completed", numTurns: 3 });
    expect(native.some(event => event.type === "session.collection_warning")).toBe(false);
    const canonical = [{ type: "assistant.message", data: { content: "canonical wins" } }];
    write("agent-session.jsonl", canonical.map(JSON.stringify).join("\n") + "\n");
    const events = collectUnifiedSession({ rootDir: root, engine: "langgraph", warn: vi.fn() }).events;
    expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["canonical wins"]);
    expect(events.filter(event => event.type === "tool.execution_start")).toHaveLength(0);
  });

  it("retains structured and falsy content, unknown records, invalid calls, and observed failure without invented success", () => {
    const sample = {
      exit_status: "LimitsExceeded",
      messages: [
        { type: "human", data: { content: [{ type: "text", text: "  exact\n" }] } },
        { type: "ai", data: { content: false, tool_calls: [{ id: "zero", name: "fixture", args: 0 }], invalid_tool_calls: [{ id: "invalid", args: "{" }], usage_metadata: { input_tokens: 0, output_tokens: 0, total_tokens: 0 } } },
        { type: "tool", data: { tool_call_id: "zero", content: null, status: "error", artifact: false } },
        { type: "future_message", data: { content: { value: 0, enabled: false } } },
      ],
    };
    const parsed = parseLangGraphLog(JSON.stringify(sample));
    expect(parsed.maxTurnsHit).toBe(true);
    expect(parsed.logEntries.find(event => event.type === "user.message").data.content).toEqual(sample.messages[0].data.content);
    expect(parsed.logEntries.find(event => event.type === "assistant.message").data.content).toBe(false);
    expect(parsed.logEntries.find(event => event.type === "tool.execution_start").data.input).toBe(0);
    expect(parsed.logEntries.find(event => event.type === "tool.execution_complete").data).toMatchObject({ output: null, structuredContent: false, success: false, status: "error" });
    expect(parsed.logEntries.find(event => event.type === "langgraph.invalid_tool_call").data.record).toEqual(sample.messages[1].data.invalid_tool_calls[0]);
    expect(parsed.logEntries.find(event => event.type === "langgraph.message").data.record).toEqual(sample.messages[3]);
    write("agent/langgraph.traj.json", JSON.stringify(sample));
    const events = writeUnifiedSession({ rootDir: root, engine: "langgraph", warn: vi.fn() });
    expect(validateSession(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8"))).toBe(events.length);
  });

  it("redacts native messages, tool inputs/results, and unknown records before UAS publication", () => {
    const secret = "langgraph-synthetic-secret-token";
    process.env.GH_AW_SECRET_NAMES = "LANGGRAPH_FIXTURE";
    process.env.SECRET_LANGGRAPH_FIXTURE = secret;
    const sample = structuredClone(trajectory);
    sample.messages[0].data.content = secret;
    sample.messages[1].data.tool_calls[0].args.command = secret;
    sample.messages[2].data.content = secret;
    sample.messages.push({ type: "future_message", data: { content: secret } });
    write("agent/langgraph.traj.json", JSON.stringify(sample));
    writeUnifiedSession({ rootDir: root, engine: "langgraph", warn: vi.fn() });
    const content = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(content).not.toContain(secret);
    expect(content).toContain("***REDACTED***");
    expect(validateSession(content)).toBeGreaterThan(0);
  });

  it("rejects malformed native structure and accounting without exposing payloads", () => {
    expect(() => parseLangGraphLog('{"messages":["PRIVATE_PAYLOAD"]')).toThrow("Malformed LangGraph trajectory JSON");
    expect(() => parseLangGraphLog(JSON.stringify({ exit_status: "Completed", messages: [{ type: "ai", data: null }] }))).toThrow("Invalid LangGraph message at index 0");
    const invalid = structuredClone(trajectory);
    invalid.messages[1].data.usage_metadata.input_tokens = -1;
    expect(() => parseLangGraphLog(JSON.stringify(invalid))).toThrow("Invalid LangGraph token count at message index 1");
    write("agent/langgraph.traj.json", '{"messages":["PRIVATE_PAYLOAD"]');
    const warnings = vi.fn();
    const events = collectUnifiedSession({ rootDir: root, engine: "langgraph", warn: warnings }).events;
    expect(warnings).toHaveBeenCalledWith(expect.stringContaining("malformed_json"));
    expect(JSON.stringify(events)).not.toContain("PRIVATE_PAYLOAD");
    expect(events.some(event => event.type === "session.result")).toBe(false);
  });

  it.each([
    ["envelope", { exit_status: "Completed", messages: "PRIVATE_PAYLOAD" }],
    ["message", { exit_status: "Completed", messages: [{ type: "ai", data: null, secret: "PRIVATE_PAYLOAD" }] }],
    ["count", { exit_status: "Completed", messages: [{ type: "ai", data: { content: "PRIVATE_PAYLOAD", usage_metadata: { input_tokens: -1 } } }] }],
    ["array envelope", [trajectory]],
  ])("retains independent evidence and grader artifacts for an invalid native %s", (_name, invalid) => {
    write("agent/langgraph.traj.json", JSON.stringify(invalid));
    write("agent-stdio.log", "LangGraph exit status: Failed\n");
    write("mcp-logs/events.jsonl", '{"type":"tool.execution_complete","data":{"toolName":"independent","success":true}}\n');
    const warnings = vi.fn();
    const events = writeUnifiedSession({ rootDir: root, engine: "langgraph", warn: warnings, graders: [] });
    const content = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(validateSession(content)).toBe(events.length);
    expect(events.some(event => event.provenance.component === "mcp")).toBe(true);
    expect(content).not.toContain("PRIVATE_PAYLOAD");
    expect(events.some(event => event.type === "session.collection_warning" && event.data.code === "invalid_langgraph_trajectory")).toBe(true);
    expect(fs.existsSync(path.join(root, "usage/graders/grader_manifest.json"))).toBe(true);
    expect(fs.existsSync(path.join(root, "usage/graders/grader_results.json"))).toBe(true);
    expect(JSON.stringify(warnings.mock.calls)).not.toContain("PRIVATE_PAYLOAD");
  });

  it("retains artifacts for an invalid legacy stdout snapshot with execution evidence", () => {
    write("agent-stdio.log", '{"exit_status":"Completed","messages":[{"type":"ai","data":{"content":"PRIVATE_PAYLOAD","usage_metadata":{"input_tokens":-1}}}]}\n');
    write("agent_execution_exit_code.txt", "1");
    const warnings = vi.fn();
    const events = writeUnifiedSession({ rootDir: root, engine: "langgraph", warn: warnings, graders: [] });
    expect(events.some(event => event.type === "session.collection_warning" && event.data.code === "invalid_langgraph_trajectory" && event.data.path === "agent-stdio.log")).toBe(true);
    const content = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(validateSession(content)).toBe(events.length);
    expect(content).not.toContain("PRIVATE_PAYLOAD");
    expect(content).toContain('"type":"agent.execution"');
  });

  it("retains a valid historical stdout snapshot when the native trajectory is invalid", () => {
    write("agent/langgraph.traj.json", '{"exit_status":"Completed","messages":"PRIVATE_PAYLOAD"}');
    write("agent-stdio.log", JSON.stringify(trajectory));
    const events = writeUnifiedSession({ rootDir: root, engine: "langgraph", warn: vi.fn() });
    expect(events.filter(event => event.type === "assistant.message")).toHaveLength(3);
    expect(events.some(event => event.type === "session.collection_warning" && event.data.path === "agent/langgraph.traj.json")).toBe(true);
    expect(fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8")).not.toContain("PRIVATE_PAYLOAD");
  });

  it.each(["null", "{}"])("marks a JSON-valid invalid stdout envelope %s incomplete", content => {
    write("agent-stdio.log", content);
    const events = writeUnifiedSession({ rootDir: root, engine: "langgraph", warn: vi.fn(), graders: [] });
    expect(events.some(event => event.type === "session.collection_warning" && event.data.code === "invalid_langgraph_trajectory" && event.data.path === "agent-stdio.log")).toBe(true);
    expect(fs.existsSync(path.join(root, "usage/graders/grader_results.json"))).toBe(true);
  });

  it.each(["agent/langgraph.traj.json", "agent-stdio.log"])("propagates filesystem failures from %s without labeling evidence malformed", source => {
    const file = write(source, JSON.stringify(trajectory));
    const readFileSync = fs.readFileSync;
    const failure = Object.assign(new Error("Fixture read failed"), { code: "EACCES" });
    vi.spyOn(fs, "readFileSync").mockImplementation((name, ...args) => {
      if (name === file) throw failure;
      return readFileSync(name, ...args);
    });
    const warnings = vi.fn();
    expect(() => collectUnifiedSession({ rootDir: root, engine: "langgraph", warn: warnings })).toThrow("Fixture read failed");
    expect(warnings).not.toHaveBeenCalled();
  });

  it.each(["agent/langgraph.traj.json", "agent-stdio.log"])("propagates programmer failures from %s without labeling evidence malformed", source => {
    write(source, JSON.stringify(trajectory));
    const failure = new TypeError("Fixture parser bug");
    vi.spyOn(global, "structuredClone").mockImplementation(() => {
      throw failure;
    });
    const warnings = vi.fn();
    expect(() => collectUnifiedSession({ rootDir: root, engine: "langgraph", warn: warnings })).toThrow(failure);
    expect(warnings).not.toHaveBeenCalled();
  });

  it.each(["agent/langgraph.traj.json", "agent-stdio.log"])("propagates module failures from %s without labeling evidence malformed", source => {
    write(source, JSON.stringify(trajectory));
    const originalRequire = Module.prototype.require;
    const failure = Object.assign(new Error("Fixture module missing"), { code: "MODULE_NOT_FOUND" });
    vi.spyOn(Module.prototype, "require").mockImplementation(function (name) {
      if (name === "./langgraph_session.cjs") throw failure;
      return originalRequire.call(this, name);
    });
    const warnings = vi.fn();
    expect(() => collectUnifiedSession({ rootDir: root, engine: "langgraph", warn: warnings })).toThrow(failure);
    expect(warnings).not.toHaveBeenCalled();
  });

  it("reports private runtime validation failures without leaking their payload", async () => {
    const secret = "PRIVATE_RUNTIME_CREDENTIAL";
    write("agent/langgraph.traj.json", JSON.stringify({ exit_status: "Completed", messages: [{ type: "ai", data: { content: secret, usage_metadata: { input_tokens: -1 } } }] }));
    write("agent-stdio.log", "LangGraph exit status: Failed\n");
    await runLogParser({ parserName: "LangGraph", parseLog: loadEngineLogParser("langgraph"), rootDir: root });
    expect(global.core.setFailed).toHaveBeenCalled();
    for (const method of ["info", "warning", "error", "setFailed"]) expect(JSON.stringify(global.core[method].mock.calls)).not.toContain(secret);
    expect(JSON.stringify(global.core.summary.addRaw.mock.calls)).not.toContain(secret);
  });
});
