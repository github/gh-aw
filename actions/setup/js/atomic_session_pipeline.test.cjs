import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { atomicWorkflowStream } from "./fixtures/atomic_ci_stream.cjs";
import { loadEngineLogParser, parseBehaviorLog } from "./engine_log_parser.cjs";
import { runLogParser } from "./log_parser_bootstrap.cjs";
import { sessionCLI } from "./session_cli.cjs";
import { parseEngineSession, writeUnifiedSession } from "./unified_session.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

describe("Atomic native-to-unified session pipeline", () => {
  let root;
  let originalEnv;
  let originalCore;
  const readEvents = file => fs.readFileSync(file, "utf8").trimEnd().split("\n").map(JSON.parse);

  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-atomic-session-"));
    originalEnv = { ...process.env };
    originalCore = global.core;
    process.env.GH_AW_AGENT_OUTPUT = path.join(root, "agent-stdio.log");
    delete process.env.GH_AW_SAFE_OUTPUTS;
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
    fs.rmSync(root, { recursive: true, force: true });
  });

  it("converts captured Atomic JSONL into unified session events and Markdown with sourceEngine atomic and a single final answer", async () => {
    const raw = `[INFO] Starting firewall...\n${atomicWorkflowStream.map(record => JSON.stringify(record)).join("\n")}\n`;
    fs.writeFileSync(process.env.GH_AW_AGENT_OUTPUT, raw);

    const declaredParser = loadEngineLogParser("atomic");
    expect(declaredParser).toBeTypeOf("function");

    const direct = declaredParser(raw);
    expect(direct.markdown).toContain("**Engine:** atomic");
    expect(direct.markdown).toContain("workflow");
    expect(direct.markdown.match(/\blisted\b/g)).toEqual(["listed"]);
    expect(direct.markdown).not.toContain("You are a coding assistant running inside Atomic");

    await runLogParser({ parserName: "Atomic", parseLog: declaredParser, rootDir: root });
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(global.core.summary.addRaw).toHaveBeenCalled();
    const stepSummary = global.core.summary.addRaw.mock.calls.flat().join("\n");
    expect(stepSummary).toContain("listed");
    expect(stepSummary).toContain("workflow");

    const unified = writeUnifiedSession({ rootDir: root, engine: "atomic", warn: vi.fn() });
    const sessionPath = path.join(root, "usage/aw_session.jsonl");
    const serialized = fs.readFileSync(sessionPath, "utf8");
    expect(validateSession(serialized)).toBe(unified.length);

    const init = unified.find(event => event.type === "session.init");
    expect(init).toMatchObject({
      timestamp: "2026-11-12T02:45:30.124Z",
      data: {
        sourceEngine: "atomic",
        sessionId: "01a1cda5-618c-773e-a218-2b71aeb9777d",
        cwd: "/workspace/repo",
      },
    });

    const toolStarts = unified.filter(event => event.type === "tool.execution_start");
    expect(toolStarts).toHaveLength(1);
    expect(toolStarts[0].data).toMatchObject({
      toolCallId: "call_1",
      toolName: "workflow",
      input: { action: "list" },
      model: "mock-model",
    });

    const toolCompletions = unified.filter(event => event.type === "tool.execution_complete");
    expect(toolCompletions).toHaveLength(1);
    expect(toolCompletions[0].data).toMatchObject({
      toolCallId: "call_1",
      toolName: "workflow",
      success: true,
      durationMs: 63,
    });

    const assistantMessages = unified.filter(event => event.type === "assistant.message");
    expect(assistantMessages).toHaveLength(1);
    expect(assistantMessages[0].data).toMatchObject({
      content: "listed",
      model: "mock-model",
      messageId: "1",
    });

    const result = unified.find(event => event.type === "session.result");
    expect(result?.data).toMatchObject({
      sourceEngine: "atomic",
      numTurns: 2,
      totalCostUsd: 0,
      usage: {
        inputTokens: 10,
        outputTokens: 4,
        totalTokens: 14,
        inputTokensIncludeCache: false,
      },
    });

    // Reconstruct directly from the enriched agent-stdio.log when agent-session.jsonl is absent.
    const enrichedStdio = fs.readFileSync(process.env.GH_AW_AGENT_OUTPUT, "utf8");
    expect(parseBehaviorLog(enrichedStdio, "atomic").logEntries.filter(event => event.type === "session.result")).toHaveLength(1);
    fs.unlinkSync(path.join(root, "agent-session.jsonl"));
    const reconstructedText = sessionCLI(["reconstruct", root, "atomic"]);
    expect(validateSession(reconstructedText)).toBeGreaterThan(0);
    const reconstructed = reconstructedText.trimEnd().split("\n").map(JSON.parse);
    expect(reconstructed.find(event => event.type === "session.init")?.data.sourceEngine).toBe("atomic");
    expect(reconstructed.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["listed"]);
    expect(reconstructed.filter(event => event.type === "session.result")).toHaveLength(1);
    expect(reconstructed.find(event => event.type === "session.result")?.data).toMatchObject({
      sourceEngine: "atomic",
      numTurns: 2,
    });

    expect(parseEngineSession(raw, "atomic").find(event => event.type === "session.init")?.data.sourceEngine).toBe("atomic");
    expect(readEvents(sessionPath)).toEqual(unified);
  });
});
