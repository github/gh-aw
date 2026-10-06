import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { randomUUID } from "node:crypto";
import { fileURLToPath } from "node:url";

const { readCopilotSessions, runLogParser } = require("./log_parser_bootstrap.cjs");
const { parseCopilotLog } = require("./parse_copilot_log.cjs");
const { projectSessionResult } = require("./agent_session.cjs");
const { sessionCLI } = require("./session_cli.cjs");
const { generateCopilotCliStyleSummary } = require("./log_parser_shared.cjs");

describe("Copilot retry session bootstrap", () => {
  let root, logs, previousEnvironment, core;
  const serialize = events => events.map(event => JSON.stringify(event)).join("\n") + "\n";
  const readEvents = file =>
    fs
      .readFileSync(file, "utf8")
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
  const writeSession = (directory, events) => {
    const file = path.join(logs, "copilot-session-state", directory, "events.jsonl");
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, serialize(events));
    return file;
  };
  const session = (sessionId, startTime, { inputTokens = 10, tool = false } = {}) => [
    { type: "session.start", timestamp: startTime, data: { sessionId, startTime, selectedModel: "gpt-5" } },
    { type: "user.message", data: { content: "private prompt" } },
    { type: "assistant.turn_start", data: { turnId: "0" } },
    ...(tool
      ? [
          { type: "assistant.message", data: { content: "Earlier attempt\ncompleted a tool." } },
          { type: "tool.execution_start", data: { toolName: "bash", toolCallId: "call", arguments: { command: "echo retained" } } },
          { type: "tool.execution_complete", data: { toolCallId: "call", success: true, result: { content: "retained result" } } },
        ]
      : []),
    { type: "assistant.turn_end", data: { turnId: "0" } },
    {
      type: "session.shutdown",
      data: { modelMetrics: inputTokens === undefined ? {} : { "gpt-5": { usage: { inputTokens, outputTokens: 2 } } } },
    },
  ];

  beforeEach(() => {
    root = path.join(path.dirname(fileURLToPath(import.meta.url)), `test-copilot-bootstrap-${randomUUID()}`);
    logs = path.join(root, "sandbox", "agent", "logs");
    fs.mkdirSync(logs, { recursive: true });
    previousEnvironment = { ...process.env };
    process.env.GH_AW_AGENT_OUTPUT = logs;
    delete process.env.GH_AW_SAFE_OUTPUTS;
    core = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) },
    };
    global.core = core;
  });

  afterEach(() => {
    vi.restoreAllMocks();
    process.env = previousEnvironment;
    delete global.core;
    fs.rmSync(root, { recursive: true, force: true });
  });

  const run = () => runLogParser({ parseLog: parseCopilotLog, parserName: "Copilot", supportsDirectories: true, artifactDir: root });

  it("persists every retry conversation with source provenance but accounts only the chronological final attempt", async () => {
    writeSession("z-first", session("first", "2026-10-03T18:23:15Z", { inputTokens: 100, tool: true }));
    writeSession("a-last", session("last", "2026-10-03T18:23:23Z"));

    await run();

    expect(core.setFailed).not.toHaveBeenCalled();
    const canonical = readEvents(path.join(root, "agent-session.jsonl"));
    expect(canonical.filter(event => event.type === "session.start").map(event => event.data.sessionId)).toEqual(["first", "last"]);
    const tool = canonical.find(event => event.type === "tool.execution_start");
    expect(tool.data.input.command).toBe("echo retained");
    expect(tool.provenance).toMatchObject({ component: "agent", phase: "agent", path: "sandbox/agent/logs/copilot-session-state/z-first/events.jsonl" });
    expect(canonical.filter(event => event.type === "tool.execution_complete")).toHaveLength(1);
    expect(core.summary.addRaw.mock.calls.flat().join("\n")).toContain("retained result");
    expect(core.summary.addRaw.mock.calls.flat().join("\n")).not.toContain("private prompt");
    const telemetry = readEvents(path.join(root, "agent-stdio.log")).find(event => event.type === "result");
    expect(telemetry).toMatchObject({ num_turns: 1, usage: { input_tokens: 10, output_tokens: 2 } });
    expect(projectSessionResult(canonical).usage.input_tokens).toBe(10);
  });

  it("does not infer final-attempt token usage from an earlier session", async () => {
    writeSession("first", session("first", "2026-10-03T18:23:15Z", { inputTokens: 100, tool: true }));
    const final = session("last", "2026-10-03T18:23:23Z");
    final.at(-1).data.modelMetrics = {};
    writeSession("last", final);

    await run();

    const telemetry = readEvents(path.join(root, "agent-stdio.log")).find(event => event.type === "result");
    expect(telemetry.num_turns).toBe(1);
    expect(telemetry.usage).toBeUndefined();
    expect(readEvents(path.join(root, "agent-session.jsonl")).filter(event => event.type === "tool.execution_start")).toHaveLength(1);
  });

  it("deduplicates replicated session snapshots without duplicating tools or result accounting", async () => {
    const complete = session("first", "2026-10-03T18:23:15Z", { inputTokens: 100, tool: true });
    writeSession("a-snapshot", complete.slice(0, 3));
    writeSession("b-complete", complete);
    writeSession("c-copy", complete);
    writeSession("d-final", session("last", "2026-10-03T18:23:23Z"));

    const sessions = readCopilotSessions(logs, parseCopilotLog);
    expect(sessions).toHaveLength(2);
    expect(sessions[0].source).toContain("b-complete");
    await run();
    const canonical = readEvents(path.join(root, "agent-session.jsonl"));
    expect(canonical.filter(event => event.type === "session.start")).toHaveLength(2);
    expect(canonical.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(canonical.filter(event => event.type === "tool.execution_complete")).toHaveLength(1);
    expect(canonical.filter(event => event.type === "agent.execution").length).toBeLessThanOrEqual(1);
    expect(readEvents(path.join(root, "agent-stdio.log")).filter(event => event.type === "result")).toHaveLength(1);
  });

  it("keeps identical tool call IDs scoped to their respective retry sources", async () => {
    const first = session("first", "2026-10-03T18:23:15Z", { tool: true });
    const last = session("last", "2026-10-03T18:23:23Z", { tool: true });
    first.find(event => event.type === "tool.execution_complete").data.result.content = "first attempt result";
    last.find(event => event.type === "tool.execution_complete").data.result.content = "final attempt result";
    writeSession("first", first);
    writeSession("last", last);

    await run();

    const canonical = readEvents(path.join(root, "agent-session.jsonl"));
    expect(canonical.filter(event => event.type === "tool.execution_start")).toHaveLength(2);
    const summary = core.summary.addRaw.mock.calls.flat().join("\n");
    expect(summary).toContain("first attempt result");
    expect(summary).toContain("final attempt result");
    expect(summary).toContain("Agent conversation: agent/sandbox/agent/logs/copilot-session-state/first/events.jsonl");
    expect(summary).toContain("Agent conversation: agent/sandbox/agent/logs/copilot-session-state/last/events.jsonl");

    const reconstructed = sessionCLI(["reconstruct", root, "copilot"])
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    const starts = reconstructed.filter(event => event.type === "tool.execution_start");
    expect(starts).toHaveLength(2);
    expect(new Set(starts.map(event => event.provenance.path)).size).toBe(2);
    expect(reconstructed.filter(event => event.type === "tool.execution_complete")).toHaveLength(2);
    const unified = generateCopilotCliStyleSummary(reconstructed);
    const firstGroup = unified.indexOf("Agent conversation: agent/sandbox/agent/logs/copilot-session-state/first/events.jsonl");
    const lastGroup = unified.indexOf("Agent conversation: agent/sandbox/agent/logs/copilot-session-state/last/events.jsonl");
    expect(firstGroup).toBeGreaterThanOrEqual(0);
    expect(lastGroup).toBeGreaterThan(firstGroup);
    expect(unified.slice(firstGroup, lastGroup)).toContain("first attempt result");
    expect(unified.slice(firstGroup, lastGroup)).not.toContain("final attempt result");
    const finalConversation = unified.slice(lastGroup, unified.indexOf("Chronological trace"));
    expect(finalConversation).toContain("final attempt result");
    expect(finalConversation).not.toContain("first attempt result");
  });

  it("applies masks from earlier sessions to every published retry conversation", async () => {
    const first = session("first", "2026-10-03T18:23:15Z", { tool: true });
    first.find(event => event.type === "tool.execution_complete").data.result.content = "earlier-secret";
    const last = session("last", "2026-10-03T18:23:23Z", { tool: true });
    last.find(event => event.type === "assistant.message").data.content = "earlier-secret";
    writeSession("first", first);
    writeSession("last", last);
    fs.writeFileSync(path.join(root, "agent-stdio.log"), "::add-mask::earlier-secret\n");

    await run();

    expect(core.setFailed).not.toHaveBeenCalled();
    expect(fs.readFileSync(path.join(root, "agent-session.jsonl"), "utf8")).not.toContain("earlier-secret");
    expect(core.summary.addRaw.mock.calls.flat().join("\n")).not.toContain("earlier-secret");
  });

  it("never follows symlinked session directories or files", () => {
    const outside = path.join(root, "outside");
    fs.mkdirSync(outside);
    fs.writeFileSync(path.join(outside, "events.jsonl"), serialize(session("outside", "2026-10-03T18:23:25Z")));
    fs.symlinkSync(outside, path.join(logs, "linked-directory"));
    fs.symlinkSync(path.join(outside, "events.jsonl"), path.join(logs, "events.jsonl"));
    writeSession("real", session("real", "2026-10-03T18:23:23Z"));

    expect(readCopilotSessions(logs, parseCopilotLog).map(entry => entry.events[0].data.sessionId)).toEqual(["real"]);
  });

  it("preserves the existing debug-log fallback when no native sessions are available", async () => {
    fs.writeFileSync(path.join(logs, "process.log"), "● view file.json\n  └ result\n");
    const parseLog = vi.fn(() => ({ markdown: "Fallback report", logEntries: [] }));
    await runLogParser({ parseLog, parserName: "Copilot", supportsDirectories: true, artifactDir: root });
    expect(parseLog).toHaveBeenCalledExactlyOnceWith("● view file.json\n  └ result\n");
    expect(core.setFailed).not.toHaveBeenCalled();
  });

  it("surfaces directory enumeration failures with context and the original cause", () => {
    const failure = new Error("Permission denied");
    const readdir = fs.readdirSync;
    vi.spyOn(fs, "readdirSync").mockImplementation((directory, options) => {
      if (directory === logs) throw failure;
      return readdir(directory, options);
    });

    let observed;
    try {
      readCopilotSessions(logs, parseCopilotLog);
    } catch (error) {
      observed = error;
    }
    expect(observed.message).toContain(`Failed to enumerate Copilot session directory ${logs}: Permission denied`);
    expect(observed.cause).toBe(failure);
  });

  it("surfaces native event read failures without falling back or replacing existing logs", async () => {
    const file = writeSession("unreadable", session("unreadable", "2026-10-03T18:23:23Z"));
    const canonical = path.join(root, "agent-session.jsonl");
    const stdio = path.join(root, "agent-stdio.log");
    fs.writeFileSync(canonical, "preserved canonical log");
    fs.writeFileSync(stdio, "preserved stdio log");
    const failure = new Error("Permission denied");
    const read = fs.readFileSync;
    vi.spyOn(fs, "readFileSync").mockImplementation((input, options) => {
      if (input === file) throw failure;
      return read(input, options);
    });

    expect(() => readCopilotSessions(logs, parseCopilotLog)).toThrow(`Failed to read Copilot session events ${file}: Permission denied`);
    await run();

    expect(core.setFailed).toHaveBeenCalledExactlyOnceWith(expect.stringContaining(`Failed to read Copilot session events ${file}: Permission denied`));
    expect(core.summary.addRaw).not.toHaveBeenCalled();
    expect(read(canonical, "utf8")).toBe("preserved canonical log");
    expect(read(stdio, "utf8")).toBe("preserved stdio log");
  });
});
