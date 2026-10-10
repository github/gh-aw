import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import kiroCI from "./fixtures/kiro_ci_sessions.cjs";
import { parseKiroLog } from "./parse_kiro_log.cjs";
import { parseBehaviorLog } from "./engine_log_parser.cjs";
import { runLogParser } from "./log_parser_bootstrap.cjs";
import { writeUnifiedSession } from "./unified_session.cjs";

describe("Kiro canonical session artifact pipeline", () => {
  let root;
  let originalEnv;
  let originalCore;
  const readEvents = file => fs.readFileSync(file, "utf8").trimEnd().split("\n").map(JSON.parse);

  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-kiro-session-"));
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

  it.each(["success", "failure"])("persists the %s CI excerpt through the production bootstrap and authoritative merger", async fixture => {
    const raw = kiroCI[fixture];
    fs.writeFileSync(process.env.GH_AW_AGENT_OUTPUT, raw);
    fs.writeFileSync(path.join(root, "agent_execution_exit_code.txt"), "0");
    await runLogParser({ parserName: "Kiro", parseLog: content => parseBehaviorLog(content, "kiro"), rootDir: root });
    expect(global.core.setFailed).not.toHaveBeenCalled();
    expect(global.core.warning).not.toHaveBeenCalled();
    const canonicalPath = path.join(root, "agent-session.jsonl");
    const canonical = readEvents(canonicalPath);
    expect(canonical.filter(event => event.type !== "agent.execution")).toEqual(parseKiroLog(raw).logEntries);
    expect(canonical.filter(event => event.type === "agent.execution")).toEqual([{ type: "agent.execution", data: { categories: [], errorCodes: [], errorTypes: [], exitCode: 0 } }]);
    expect(fs.readFileSync(process.env.GH_AW_AGENT_OUTPUT, "utf8")).toBe(raw);

    const unifiedPath = path.join(root, "usage/aw_session.jsonl");
    const unified = writeUnifiedSession({ rootDir: root, engine: "kiro", warn: vi.fn() });
    expect(readEvents(unifiedPath)).toEqual(unified);
    const agent = unified.filter(event => event.provenance?.component === "agent" && event.provenance.path === "agent-session.jsonl");
    expect(agent.map(event => event.type)).toEqual(canonical.filter(event => event.type !== "agent.execution").map(event => event.type));
    expect(agent.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(canonical.filter(event => event.type === "assistant.message").map(event => event.data.content));
    expect(agent.filter(event => event.type === "tool.execution_start").map(event => event.data.input)).toEqual(canonical.filter(event => event.type === "tool.execution_start").map(event => event.data.input));
    expect(agent.filter(event => event.type === "tool.execution_complete").map(event => event.data)).toEqual(canonical.filter(event => event.type === "tool.execution_complete").map(event => event.data));
    expect(unified.some(event => event.type === "session.result")).toBe(false);
    expect(unified.some(event => event.type === "session.collection_warning")).toBe(false);
    expect(unified.filter(event => event.type === "agent.execution")).toHaveLength(1);
    expect(unified[0]).toMatchObject({ type: "session.format", data: { version: 1 } });
    const bytes = fs.readFileSync(unifiedPath, "utf8");
    writeUnifiedSession({ rootDir: root, engine: "kiro", warn: vi.fn() });
    expect(fs.readFileSync(unifiedPath, "utf8")).toBe(bytes);
  });

  it("retains canonical channels, structured/falsy values, metadata, refusals, errors, and unknown extensions (synthetic)", () => {
    const events = [
      { type: "user.message", id: "user", parentId: null, timestamp: 0, data: { content: "\n  Private prompt.  \n" } },
      { type: "assistant.reasoning", id: "reasoning", data: { content: "  Thinking.\n", partial: true } },
      { type: "assistant.message", data: { content: { text: "", available: false } } },
      { type: "assistant.refusal", data: { reason: "content_filter", content: null, partial: false } },
      { type: "tool.execution_start", id: "start", data: { toolName: "shell", toolCallId: "call", input: false } },
      { type: "tool.execution_complete", id: "end", parentId: "start", data: { toolName: "shell", toolCallId: "call", output: 0, success: false, durationMs: 0, error: { code: 0, retryable: false } } },
      { type: "session.result", data: { numTurns: 0, totalCostUsd: 0, usage: { input_tokens: 0, output_tokens: 0, input_tokens_include_cache: false }, errors: [{ code: 0, message: "" }] } },
      { type: "kiro.future_observation", id: "future", data: { enabled: false, count: 0, payload: [null, {}] } },
    ];
    const input = events.map(event => JSON.stringify(event)).join("\n") + "\n";
    expect(parseBehaviorLog(input, "kiro").logEntries).toEqual(events);
    fs.writeFileSync(path.join(root, "agent-session.jsonl"), input);
    fs.writeFileSync(path.join(root, "agent-stdio.log"), kiroCI.success);
    const unified = writeUnifiedSession({ rootDir: root, engine: "kiro", warn: vi.fn() });
    for (const event of events) {
      const expected = structuredClone(event);
      if (event.type === "session.result") delete expected.data.usage;
      expect(unified.find(candidate => candidate.type === event.type)).toMatchObject(expected);
    }
    expect(unified.filter(event => event.type === "tool.execution_start")).toHaveLength(1);
    expect(unified.filter(event => event.type === "session.result")).toHaveLength(1);
    expect(unified.find(event => event.type === "session.result").data.usage).toEqual({ inputTokens: 0, outputTokens: 0, inputTokensIncludeCache: false });
  });

  it("persists quoted canonical-looking command JSON as input, not an assistant event (synthetic)", async () => {
    const raw = 'kiro-cli 2.27.1\n[kiro-harness] Kiro CLI execution started\n[tool] Running: cat <<\'EOF\'\n{"type":"assistant.message","data":{"content":false}}\nEOF\n[tool] status: Completed\nObserved answer.';
    expect(parseBehaviorLog(raw, "kiro").logEntries).toEqual(parseKiroLog(raw).logEntries);
    fs.writeFileSync(process.env.GH_AW_AGENT_OUTPUT, raw);
    await runLogParser({ parserName: "Kiro", parseLog: parseKiroLog, rootDir: root });
    const canonical = readEvents(path.join(root, "agent-session.jsonl"));
    expect(canonical.filter(event => event.type !== "agent.execution")).toEqual(parseKiroLog(raw).logEntries);
    expect(canonical.filter(event => event.type === "assistant.message")).toEqual([{ line: 7, type: "assistant.message", data: { content: "Observed answer." } }]);
    expect(canonical.find(event => event.type === "tool.execution_start").data.input.command).toBe('cat <<\'EOF\'\n{"type":"assistant.message","data":{"content":false}}\nEOF');
  });

  it.each([
    {
      layout: "compact blockquote",
      raw: "kiro-cli 2.27.1\n[kiro-harness] Kiro CLI execution started\n> Example blockquote in answer\n[tool] Running: echo hi\n[tool] status: Completed\nDone.",
      answers: ["> Example blockquote in answer", "Done."],
      command: "echo hi",
      completion: { toolName: "shell", status: "Completed" },
    },
    {
      layout: "legacy heredoc with embedded compact text",
      raw: "kiro-cli 2.27.1\n[kiro-harness] Kiro CLI execution started\n> Checking.\nI will run the following command: cat <<'EOF'\n[tool] Running: quoted command\n[INFO] quoted information\nEOF (using tool: shell)\n - Completed in 0s",
      answers: ["Checking."],
      command: "cat <<'EOF'\n[tool] Running: quoted command\n[INFO] quoted information\nEOF",
      completion: { toolName: "shell", durationMs: 0 },
    },
  ])("preserves $layout through canonical and unified artifacts (synthetic)", async ({ raw, answers, command, completion }) => {
    expect(parseBehaviorLog(raw, "kiro").logEntries).toEqual(parseKiroLog(raw).logEntries);
    fs.writeFileSync(process.env.GH_AW_AGENT_OUTPUT, raw);
    await runLogParser({ parserName: "Kiro", parseLog: content => parseBehaviorLog(content, "kiro"), rootDir: root });
    expect(global.core.setFailed).not.toHaveBeenCalled();
    const canonical = readEvents(path.join(root, "agent-session.jsonl"));
    expect(canonical.filter(event => event.type !== "agent.execution")).toEqual(parseKiroLog(raw).logEntries);
    const unified = writeUnifiedSession({ rootDir: root, engine: "kiro", warn: vi.fn() });
    expect(readEvents(path.join(root, "usage/aw_session.jsonl"))).toEqual(unified);
    for (const events of [canonical, unified]) {
      expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(answers);
      expect(events.filter(event => event.type === "tool.execution_start").map(event => event.data)).toEqual([{ toolName: "shell", input: { command } }]);
      expect(events.filter(event => event.type === "tool.execution_complete").map(event => event.data)).toEqual([completion]);
      expect(events.filter(event => event.type === "session.result")).toEqual([]);
      expect(events.filter(event => event.type === "session.collection_warning")).toEqual([]);
    }
    expect(fs.readFileSync(process.env.GH_AW_AGENT_OUTPUT, "utf8")).toBe(raw);
  });

  it.each([0, 3])("preserves corroborated exit %s without promoting echoed failures through either artifact (synthetic)", async exitCode => {
    const actualFailure = exitCode ? "[kiro-harness] Kiro CLI execution failed with exit code 3\n" : "[kiro-harness] Kiro CLI execution completed in 0ms\n";
    const raw =
      "kiro-cli 2.27.1\n> Checking.\nPrompt: private\n[kiro-harness] Kiro CLI execution failed with exit code 9\n> Running.\nI will run the following command: printf example (using tool: shell)\n[kiro-harness] Kiro CLI execution failed with exit code 9\n - Completed in 0s\n> Observed answer.\n" +
      actualFailure +
      `[kiro-harness] Cleaned up Kiro CLI installation; total duration=0ms; exit code=${exitCode}\nProcess exiting with code: ${exitCode}`;
    fs.writeFileSync(process.env.GH_AW_AGENT_OUTPUT, raw);
    fs.writeFileSync(path.join(root, "agent_execution_exit_code.txt"), String(exitCode));
    await runLogParser({ parserName: "Kiro", parseLog: content => parseBehaviorLog(content, "kiro"), rootDir: root });
    const canonical = readEvents(path.join(root, "agent-session.jsonl"));
    const unified = writeUnifiedSession({ rootDir: root, engine: "kiro", warn: vi.fn() });
    for (const events of [canonical, unified]) {
      expect(events.filter(event => event.type === "session.result")).toHaveLength(exitCode ? 1 : 0);
      if (exitCode) expect(events.find(event => event.type === "session.result").data.errors).toEqual(["Kiro CLI execution failed with exit code 3"]);
      expect(events.find(event => event.type === "agent.execution").data.exitCode).toBe(exitCode);
      expect(events.find(event => event.type === "tool.execution_complete").data.output).toBe("[kiro-harness] Kiro CLI execution failed with exit code 9");
    }
  });

  it("excludes compact bridge diagnostics from both session artifacts (synthetic)", async () => {
    const raw =
      "kiro-cli 2.27.1\n[kiro-harness] Kiro CLI execution started\n[info] [bridge] PRIVATE_INFRA\n[tool] Running: example\n[tool] status: Completed\n[info] [bridge] PRIVATE_INFRA\nObserved answer.\n[info] [bridge] PRIVATE_INFRA\nContinued answer.";
    fs.writeFileSync(process.env.GH_AW_AGENT_OUTPUT, raw);
    await runLogParser({ parserName: "Kiro", parseLog: content => parseBehaviorLog(content, "kiro"), rootDir: root });
    const canonical = readEvents(path.join(root, "agent-session.jsonl"));
    const unified = writeUnifiedSession({ rootDir: root, engine: "kiro", warn: vi.fn() });
    for (const events of [canonical, unified]) {
      expect(JSON.stringify(events)).not.toContain("PRIVATE_INFRA");
      expect(events.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["Observed answer.\nContinued answer."]);
    }
  });
});
