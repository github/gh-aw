import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "node:fs";
import { runLogParser } from "./log_parser_bootstrap.cjs";
import { parseCodexLog } from "./parse_codex_log.cjs";

const STDIO = "/tmp/gh-aw/agent-stdio.log";
const SOURCE = "/fixture/session.jsonl";
const codexSmoke = fs.readFileSync(new URL("./test_data/codex_ci_smoke.jsonl", import.meta.url), "utf8");
const codexNoTools = fs.readFileSync(new URL("./test_data/codex_ci_no_tools.jsonl", import.meta.url), "utf8");

describe("Unified session bootstrap telemetry conformance", () => {
  let files;
  let originalEnv;

  beforeEach(() => {
    originalEnv = { ...process.env };
    process.env.GH_AW_AGENT_OUTPUT = SOURCE;
    delete process.env.GH_AW_SAFE_OUTPUTS;
    files = new Map([[SOURCE, "fixture"]]);
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
    vi.spyOn(fs, "existsSync").mockImplementation(file => files.has(String(file)));
    vi.spyOn(fs, "statSync").mockImplementation(() => ({ isDirectory: () => false }));
    vi.spyOn(fs, "readFileSync").mockImplementation(file => {
      if (!files.has(String(file))) throw new Error("Missing virtual file");
      return files.get(String(file));
    });
    vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    vi.spyOn(fs, "appendFileSync").mockImplementation((file, content) => files.set(String(file), (files.get(String(file)) ?? "") + content));
    vi.spyOn(fs, "writeFileSync").mockImplementation((file, content) => files.set(String(file), String(content)));
    vi.spyOn(fs, "renameSync").mockImplementation((from, to) => {
      files.set(String(to), files.get(String(from)));
      files.delete(String(from));
    });
  });

  afterEach(() => {
    process.env = originalEnv;
    vi.restoreAllMocks();
    delete global.core;
  });

  async function parse(events) {
    await runLogParser({ parserName: "Test", parseLog: () => ({ markdown: "fixture", logEntries: events }) });
  }

  it("T-UAS-046/047/053: canonical snapshots select known fields without real disk I/O", async () => {
    const events = [
      { type: "session.result", data: { numTurns: 2, usage: { inputTokens: 10, output_tokens: 5 } } },
      { type: "session.result", data: { usage: { input_tokens: 0 } } },
      { type: "vendor.after", data: {} },
    ];
    const original = structuredClone(events);
    await parse(events);
    expect(JSON.parse(files.get(STDIO))).toEqual({ type: "result", num_turns: 2, usage: { input_tokens: 0, output_tokens: 5 } });
    expect(events).toEqual(original);
  });

  it("T-UAS-047: zero is retained and absent output tokens/turns stay absent", async () => {
    await parse([{ type: "session.result", data: { usage: { input_tokens: 0 } } }]);
    expect(JSON.parse(files.get(STDIO))).toEqual({ type: "result", usage: { input_tokens: 0 } });
  });

  it("preserves the default Codex JSONL cache accounting in runtime telemetry", async () => {
    files.set(SOURCE, codexSmoke);
    await runLogParser({ parserName: "Codex", parseLog: parseCodexLog });
    expect(JSON.parse(files.get(STDIO))).toMatchObject({
      type: "result",
      num_turns: 1,
      usage: { input_tokens: 35078, output_tokens: 1638, cache_read_input_tokens: 18176, cache_creation_input_tokens: 0, reasoning_output_tokens: 0 },
    });
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });

  it("publishes all six current Codex events and appends provider-matched compatibility usage once", async () => {
    files.set(SOURCE, codexNoTools);
    files.set(STDIO, codexNoTools);
    await runLogParser({ parserName: "Codex", parseLog: parseCodexLog });
    const published = files.get("/tmp/gh-aw/agent-session.jsonl").trimEnd().split("\n").map(JSON.parse);
    expect(published).toEqual(JSON.parse(JSON.stringify(parseCodexLog(codexNoTools).logEntries)));
    expect(published).toHaveLength(6);
    expect(published.some(event => event.type.startsWith("tool."))).toBe(false);
    expect(JSON.parse(files.get(STDIO).trimEnd().split("\n").at(-1))).toEqual({
      type: "result",
      num_turns: 1,
      usage: { input_tokens: 16847, output_tokens: 167, cache_read_input_tokens: 8576, cache_creation_input_tokens: 0, reasoning_output_tokens: 0 },
    });
    const appended = files.get(STDIO);
    files.set(SOURCE, appended);
    await runLogParser({ parserName: "Codex", parseLog: parseCodexLog });
    expect(files.get(STDIO)).toBe(appended);
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });

  it.each(['{"type":"result","num_turns":999,"usage":{"input_tokens":999}}', '[\n{"type":"result","num_turns":999,"usage":{"input_tokens":999}}\n]'])(
    "does not let framed tool output suppress the authoritative Codex compatibility result: %s",
    async payload => {
      const content = `tool api.fetch({})\napi.fetch(...) success in 2ms:\n${payload}\n{"type":"thread.started","thread_id":"real"}\n{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}`;
      files.set(SOURCE, content);
      files.set(STDIO, content);
      await runLogParser({ parserName: "Codex", parseLog: parseCodexLog });
      const result = JSON.parse(files.get(STDIO).trimEnd().split("\n").at(-1));
      expect(result).toEqual({ type: "result", num_turns: 1, usage: { input_tokens: 10, output_tokens: 2 } });
      const appended = files.get(STDIO);
      files.set(SOURCE, appended);
      await runLogParser({ parserName: "Codex", parseLog: parseCodexLog });
      expect(files.get(STDIO)).toBe(appended);
    }
  );

  it("retains total-only and cache semantics without fabricating token components", async () => {
    await parse([{ type: "session.result", data: { usage: { total_tokens: 0, cache_read_input_tokens: 0, input_tokens_include_cache: true, private_extension: "omit" } } }]);
    expect(JSON.parse(files.get(STDIO))).toEqual({ type: "result", usage: { total_tokens: 0, cache_read_input_tokens: 0, input_tokens_include_cache: true } });
  });

  it("persists canonical events for conclusion without a legacy telemetry result or input mutation", async () => {
    const events = [
      { type: "assistant.message", timestamp: "2026-10-02T00:00:01Z", data: { content: "complete" } },
      { type: "session.result", data: { numTurns: 0 } },
      { type: "vendor.extension", data: { available: false } },
    ];
    await parse(events);
    expect(files.get("/tmp/gh-aw/agent-session.jsonl").trimEnd().split("\n").map(JSON.parse)).toEqual(events);
    expect(events.some(event => event.type === "result")).toBe(false);
  });

  it("T-UAS-047: a turn-only result does not fabricate an empty usage report", async () => {
    await parse([{ type: "session.result", data: { numTurns: 0 } }]);
    expect(JSON.parse(files.get(STDIO))).toEqual({ type: "result", num_turns: 0 });
  });

  it("T-UAS-047/048: absent or invalid metrics do not cause a success-shaped result", async () => {
    await parse([
      { type: "tool.execution_start", data: { toolName: "lookup" } },
      { type: "session.result", data: { numTurns: -1, usage: { input_tokens: NaN, output_tokens: -1 }, errors: ["provider failed"] } },
    ]);
    expect(files.has(STDIO)).toBe(false);
    expect(fs.appendFileSync).not.toHaveBeenCalled();
  });

  it.each(['{"type":"result","num_turns":1}\n', '[{"type":"result","usage":{"input_tokens":0}}]\n'])("T-UAS-047: enrichment is idempotent with existing usable telemetry %s", async previous => {
    files.set(STDIO, previous);
    await parse([{ type: "session.result", data: { numTurns: 3 } }]);
    expect(files.get(STDIO)).toBe(previous);
    expect(fs.appendFileSync).not.toHaveBeenCalled();
  });

  it("T-UAS-047: safely separates an unterminated source line and ignores an unusable result", async () => {
    files.set(STDIO, '{"type":"result","errors":["early"]}');
    await parse([{ type: "session.result", data: { numTurns: 3 } }]);
    const lines = files.get(STDIO).trimEnd().split("\n");
    expect(lines).toHaveLength(2);
    expect(JSON.parse(lines[1])).toEqual({ type: "result", num_turns: 3 });
  });

  it("T-UAS-047: append failures are visible but non-fatal", async () => {
    fs.appendFileSync.mockImplementation(() => {
      throw new Error("fixture permission denied");
    });
    await parse([{ type: "session.result", data: { numTurns: 1 } }]);
    expect(global.core.warning).toHaveBeenCalledWith(expect.stringContaining("fixture permission denied"));
    expect(global.core.setFailed).not.toHaveBeenCalled();
  });

  it("T-UAS-045/049: Actions and console publication redact before shortening values", async () => {
    const token = "github_pat_" + "a".repeat(82);
    const masked = "opaque-mask-" + "b".repeat(180);
    files.set(STDIO, `::add-mask::${masked}\n`);
    const events = [
      { type: "user.message", data: { content: "PRIVATE_PROMPT" } },
      { type: "tool.execution_start", data: { toolCallId: "lookup", toolName: "lookup", input: { token, masked } } },
      { type: "tool.execution_complete", data: { toolCallId: "lookup", success: true, output: token + "\n" + masked } },
    ];
    const original = structuredClone(events);
    await parse(events);
    const consoleText = global.core.info.mock.calls.map(call => call[0]).find(text => text.includes("Execution Summary"));
    const markdown = global.core.summary.addRaw.mock.calls[0][0];
    for (const output of [consoleText, markdown]) {
      expect(output).not.toContain(token.slice(0, 40));
      expect(output).not.toContain(masked.slice(0, 40));
      expect(output).not.toContain("PRIVATE_PROMPT");
      expect(output).toContain("***REDACTED***");
    }
    expect(events).toEqual(original);
  });
});
