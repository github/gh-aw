import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import { createRequire } from "node:module";
import { atomicWorkflowStream } from "./fixtures/atomic_ci_stream.cjs";
import { loadEngineLogParser, parseBehaviorLog } from "./engine_log_parser.cjs";
import { runLogParser } from "./log_parser_bootstrap.cjs";
import { sessionCLI } from "./session_cli.cjs";
import { parseEngineSession, writeUnifiedSession } from "./unified_session.cjs";
import { validateSession } from "./scripts/validate_session.cjs";

const req = createRequire(import.meta.url);
const atomicOutputRedaction = req("./atomic_output_redaction.cjs");
const awfReflect = req("./awf_reflect.cjs");
function extractAtomicHarnessScript() {
  const currentSource = fs.readFileSync(new URL("../../../.github/workflows/shared/atomic.md", import.meta.url), "utf8");
  const block = currentSource.split("\n    harness-script: |\n")[1];
  if (!block) throw new Error("Missing harness-script in shared/atomic.md");
  return block.split(/\n(?= {0,4}\S)/)[0].replace(/^ {6}/gm, "");
}

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

  it("redacts multi-line raw secrets and JSON-escaped secrets on stdout while keeping JSONL valid", async () => {
    const multilineSecret = "multiline-secret-first-line\nmultiline-secret-second-line";
    const singleLineSecret = "single-line-bearer-secret";
    const promptPath = path.join(root, "prompt.txt");
    const mcpPath = path.join(root, "mcp.json");
    const agentParentDir = path.join(root, "agent");
    fs.writeFileSync(promptPath, "List the available workflows.\n", { mode: 0o600 });
    fs.writeFileSync(
      mcpPath,
      JSON.stringify({
        autoEnableCodemode: false,
        mcpServers: {
          fixture: {
            type: "http",
            url: "http://127.0.0.1:8080/mcp",
            headers: {
              Authorization: `Bearer ${singleLineSecret}`,
              "X-Multiline": multilineSecret,
            },
          },
        },
      }),
      { mode: 0o600 }
    );

    const streamWithSecrets = atomicWorkflowStream.map(record => {
      if (record.type === "message_end" && record.message?.role === "assistant" && record.message?.stopReason === "stop") {
        return {
          ...record,
          message: {
            ...record.message,
            content: [{ type: "text", text: `listed (${singleLineSecret}) [${multilineSecret}]` }],
          },
        };
      }
      if (record.type === "turn_end" && record.message?.stopReason === "stop") {
        return {
          ...record,
          message: {
            ...record.message,
            content: [{ type: "text", text: `listed (${singleLineSecret}) [${multilineSecret}]` }],
          },
        };
      }
      if (record.type === "agent_end") {
        return {
          ...record,
          messages: record.messages.map(msg => (msg?.role === "assistant" && msg?.stopReason === "stop" ? { ...msg, content: [{ type: "text", text: `listed (${singleLineSecret}) [${multilineSecret}]` }] } : msg)),
        };
      }
      return record;
    });

    const childStdout = Buffer.from(`[raw-diagnostic] before ${multilineSecret} after\n` + `${streamWithSecrets.map(record => JSON.stringify(record)).join("\n")}\n`, "utf8");

    const script = extractAtomicHarnessScript().replaceAll("/tmp/gh-aw/agent", agentParentDir);
    const stdoutChunks = [];
    const stderrChunks = [];
    const fakeProcess = {
      argv: ["node", "atomic_harness.cjs", "atomic", "--print", "--mode", "json"],
      env: {
        ATOMIC_MODEL: "copilot/mock-model",
        GH_AW_LLM_PROVIDER: "github",
        AWF_REFLECT_ENABLED: "1",
        GH_AW_PROMPT: promptPath,
        GH_AW_MCP_CONFIG: mcpPath,
        GITHUB_WORKSPACE: root,
      },
      stdout: { write: chunk => stdoutChunks.push(String(chunk)) },
      stderr: { write: chunk => stderrChunks.push(String(chunk)) },
      exitCode: 0,
      kill: () => {},
    };

    await vm.runInNewContext(script, {
      Buffer,
      URL,
      process: fakeProcess,
      require: specifier => {
        if (specifier === "fs") return fs;
        if (specifier === "path") return path;
        if (specifier === "child_process") {
          return {
            spawnSync: () => ({ status: 0, signal: null, stdout: childStdout, stderr: Buffer.alloc(0) }),
          };
        }
        if (specifier === "./atomic_output_redaction.cjs") return atomicOutputRedaction;
        if (specifier === "./awf_reflect.cjs") {
          return {
            ...awfReflect,
            fetchAWFReflect: async () => ({
              ok: true,
              reflectData: {
                endpoints: [{ configured: true, provider: "github", models_url: "http://127.0.0.1:18998/v1/models" }],
              },
            }),
          };
        }
        throw new Error(`Unexpected module ${specifier}`);
      },
    });
    // Allow main() async promise settlement.
    await new Promise(resolve => setImmediate(resolve));

    expect(fakeProcess.exitCode).toBe(0);
    expect(stderrChunks.join("")).not.toContain("multiline-secret");
    expect(stderrChunks.join("")).not.toContain(singleLineSecret);
    const sanitizedStdout = stdoutChunks.join("");
    expect(sanitizedStdout).not.toContain("multiline-secret-first-line");
    expect(sanitizedStdout).not.toContain("multiline-secret-second-line");
    expect(sanitizedStdout).not.toContain(singleLineSecret);
    expect(sanitizedStdout).toContain("[raw-diagnostic] before [REDACTED] after\n");

    const jsonLines = sanitizedStdout
      .trimEnd()
      .split("\n")
      .filter(line => line.startsWith("{"));
    expect(jsonLines).toHaveLength(streamWithSecrets.length);
    for (const line of jsonLines) {
      expect(() => JSON.parse(line)).not.toThrow();
    }

    fs.writeFileSync(process.env.GH_AW_AGENT_OUTPUT, sanitizedStdout);
    const declaredParser = loadEngineLogParser("atomic");
    await runLogParser({ parserName: "Atomic", parseLog: declaredParser, rootDir: root });
    expect(global.core.setFailed).not.toHaveBeenCalled();

    const unified = writeUnifiedSession({ rootDir: root, engine: "atomic", warn: vi.fn() });
    const serialized = fs.readFileSync(path.join(root, "usage/aw_session.jsonl"), "utf8");
    expect(validateSession(serialized)).toBe(unified.length);
    expect(serialized).not.toContain("multiline-secret");
    expect(serialized).not.toContain(singleLineSecret);
    expect(unified.filter(event => event.type === "assistant.message").map(event => event.data.content)).toEqual(["listed ([REDACTED]) [[REDACTED]]"]);
  });
});
