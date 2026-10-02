import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "node:fs";
import { runLogParser } from "./log_parser_bootstrap.cjs";

const STDIO = "/tmp/gh-aw/agent-stdio.log";
const SOURCE = "/fixture/session.jsonl";

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
});
