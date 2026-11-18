import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import vm from "node:vm";

const source = fs.readFileSync(new URL("../../../.github/workflows/shared/opencode.md", import.meta.url), "utf8");
const req = require;
const reflect = req("./awf_reflect.cjs");
const gateway = req("./convert_gateway_config_shared.cjs");
const opencodeParser = req("./parse_opencode_log.cjs");

function engineScript(key) {
  const block = source.split(`    ${key}: |\n`)[1];
  if (!block) throw new Error(`Missing OpenCode ${key}`);
  return block.split(/\n(?= {0,5}\S)/)[0].replace(/^ {6}/gm, "");
}

async function harness({
  env = {},
  endpoints,
  processResult = {},
  runs,
  events = [{ type: "text", sessionID: "ses", part: { text: "Finished." } }],
  mcp,
  stderrLines = [],
  safeOutputsExists = true,
  safeOutputsContents = "",
  sessionExport = { info: { id: "ses" }, messages: [{ type: "assistant", id: "default", finish: "stop", time: { completed: 1000 } }] },
  exportError,
  exportOutput,
} = {}) {
  const stdout = [];
  const stderr = [];
  const errors = [];
  class HarnessError extends Error {
    constructor(...args) {
      super(...args);
      errors.push(this);
    }
  }
  const process = {
    argv: ["node", "harness", "opencode", "run", "--standalone", "--format", "json", "--agent", "build", "--log-level", "info"],
    env: { OPENCODE_MODEL: "copilot/auto", GH_AW_LLM_PROVIDER: "github", AWF_REFLECT_ENABLED: "1", GH_AW_PROMPT: "/prompt.txt", GITHUB_WORKSPACE: "/workspace", ...env },
    stdout: { write: value => stdout.push(value) },
    stderr: { write: value => stderr.push(value) },
    exitCode: undefined,
  };
  const runProcess = vi.fn(async options => {
    const execution = runs?.[runProcess.mock.calls.length - 1] ?? { events, processResult, stderrLines };
    for (const event of execution.events ?? []) options.onStdoutLine(JSON.stringify(event));
    for (const line of execution.stderrLines ?? stderrLines) options.onStderrLine(line);
    execution.afterRun?.();
    return { exitCode: 0, stderr: "", ...(execution.processResult ?? processResult) };
  });
  const readFileSync = vi.fn(file => {
    if (file === "/prompt.txt") return "Secret prompt; $(do-not-execute) --flag\n";
    if (file === "/mcp.json") return JSON.stringify(mcp ?? { mcp: { servers: {} } });
    if (file === "/outputs.jsonl") return typeof safeOutputsContents === "function" ? safeOutputsContents() : safeOutputsContents;
    throw new Error(`Unexpected file ${file}`);
  });
  const execFile = vi.fn((command, args, options, callback) => callback(exportError, exportOutput ?? JSON.stringify(sessionExport)));
  await vm.runInNewContext(engineScript("harness-script"), {
    Error: HarnessError,
    process,
    require: name => {
      if (name === "fs") return { readFileSync, existsSync: vi.fn(() => safeOutputsExists) };
      if (name === "child_process") return { execFile };
      if (name === "./process_runner.cjs") return { runProcess };
      if (name === "./parse_opencode_log.cjs") return opencodeParser;
      if (name === "./awf_reflect.cjs")
        return {
          ...reflect,
          fetchAWFReflect: async () => ({
            ok: true,
            reflectData: { endpoints: endpoints ?? [{ configured: true, provider: process.env.GH_AW_LLM_PROVIDER, models_url: "http://127.0.0.1:10002/v1/models" }] },
          }),
        };
      throw new Error(`Unexpected dependency ${name}`);
    },
  });
  const options = runProcess.mock.calls[0]?.[0];
  return {
    process,
    options,
    config: options ? JSON.parse(options.env.OPENCODE_CONFIG_CONTENT) : undefined,
    stdout: stdout.join(""),
    stderr: stderr.join(""),
    logContent: [...events.map(event => JSON.stringify(event)), ...stdout].join("\n"),
    errors,
    readFileSync,
    execFile,
    runProcess,
  };
}

describe("OpenCode shared workflow harness", () => {
  it.each([
    ["copilot/auto", "github", "@opencode/ai/providers/openai-compatible"],
    ["anthropic/claude-sonnet-4-5", "anthropic", "@opencode/ai/providers/anthropic"],
    ["openai/gpt-5", "openai", "@opencode/ai/providers/openai-compatible"],
    ["codex/gpt-5", "openai", "@opencode/ai/providers/openai-compatible"],
  ])("routes %s through its selected provider with an explicit model", async (model, provider, sdk) => {
    const result = await harness({ env: { OPENCODE_MODEL: model, GH_AW_LLM_PROVIDER: provider } });
    expect(result.process.exitCode).toBeUndefined();
    expect(result.config.providers["awf-proxy"]).toMatchObject({ package: sdk, settings: { baseURL: "http://127.0.0.1:10002/v1", apiKey: "awf-proxy" } });
    expect(result.config.experimental.policies).toEqual([
      { action: "provider.use", resource: "*", effect: "deny" },
      { action: "provider.use", resource: "awf-proxy", effect: "allow" },
    ]);
    expect(result.config.update).toBe("disable");
    expect(result.config.permissions).toEqual([{ action: "*", resource: "*", effect: "allow" }]);
    for (const legacy of ["provider", "agent", "permission", "enabled_providers", "disabled_providers", "autoupdate"]) expect(result.config).not.toHaveProperty(legacy);
    expect(result.options.args).toContain("--standalone");
    expect(result.options.args.slice(-2)).toEqual(["--model", `awf-proxy/${model.slice(model.indexOf("/") + 1)}`]);
    expect(result.options.stdin).toContain("Secret prompt");
    expect(result.options.args.join(" ")).not.toContain("Secret prompt");
    expect(result.options.logArgs.join(" ")).not.toContain("Secret prompt");
    expect(result.stderr).not.toContain("Secret prompt");
    expect(result.readFileSync.mock.calls.map(call => call[0])).toEqual(["/prompt.txt"]);
  });

  it.each([
    ["unset", undefined],
    ["empty", ""],
  ])("uses only the lead model when the model roster is %s", async (_label, roster) => {
    const result = await harness({ env: { GH_AW_OPENCODE_MODELS: roster } });
    expect(result.config.providers["awf-proxy"].models).toEqual({ auto: { name: "auto" } });
  });

  it("trims and deduplicates model roster entries while retaining the lead model once", async () => {
    const result = await harness({ env: { GH_AW_OPENCODE_MODELS: " fast , , auto, fast " } });
    expect(result.config.providers["awf-proxy"].models).toEqual({ fast: { name: "fast" }, auto: { name: "auto" } });
  });

  it("continues an empty successful run in the same session until safe output appears", async () => {
    let outputWritten = false;
    const result = await harness({
      env: { GH_AW_SAFE_OUTPUTS: "/outputs.jsonl" },
      safeOutputsContents: () => (outputWritten ? '{"type":"noop"}\n' : ""),
      runs: [
        { events: [{ type: "text", sessionID: "ses", part: { text: "I will do that." } }] },
        {
          events: [{ type: "text", sessionID: "ses", part: { text: "Done." } }],
          afterRun: () => {
            outputWritten = true;
          },
        },
      ],
    });
    const [, continuation] = result.runProcess.mock.calls.map(([options]) => options);
    expect(result.runProcess).toHaveBeenCalledTimes(2);
    expect(continuation.args).toContain("--session");
    expect(continuation.args).toContain("ses");
    expect(continuation.stdin).toContain("Do not explain or plan");
    expect(continuation.onStdoutLine).toBe(result.options.onStdoutLine);
  });

  it("limits empty-session continuation to two nudges", async () => {
    const textEvent = { type: "text", sessionID: "ses", part: { text: "Still working." } };
    const result = await harness({
      env: { GH_AW_SAFE_OUTPUTS: "/outputs.jsonl" },
      runs: [{ events: [textEvent] }, { events: [textEvent] }, { events: [textEvent] }],
    });
    expect(result.runProcess).toHaveBeenCalledTimes(3);
    expect(result.runProcess.mock.calls.slice(1).map(([options]) => options.stdin)).toEqual([expect.stringContaining("Do not explain or plan"), expect.stringContaining("Do not explain or plan")]);
  });

  it.each([
    ["live completion", [{ type: "step_finish", sessionID: "ses", part: { id: "finish", reason: "stop" } }]],
    ["completion missing from live events", [{ type: "text", sessionID: "ses", part: { text: "Finished." } }]],
  ])("does not continue when the reconciled session has exhausted max-turns (%s)", async (_case, events) => {
    const result = await harness({
      env: { GH_AW_SAFE_OUTPUTS: "/outputs.jsonl", GH_AW_MAX_TURNS: "1" },
      events,
      sessionExport: { info: { id: "ses" }, messages: [{ type: "assistant", id: "first", finish: "stop", time: { completed: 1000 } }] },
    });
    expect(result.runProcess).toHaveBeenCalledOnce();
    expect(result.process.exitCode).toBe(1);
    expect(
      result.stdout
        .trim()
        .split("\n")
        .map(line => JSON.parse(line).type)
    ).toEqual(["step_finish", "opencode.max_turns"]);
  });

  it("limits a continuation to the remaining cumulative max-turns budget", async () => {
    let outputWritten = false;
    const result = await harness({
      env: { GH_AW_SAFE_OUTPUTS: "/outputs.jsonl", GH_AW_MAX_TURNS: "3" },
      safeOutputsContents: () => (outputWritten ? '{"type":"noop"}\n' : ""),
      events: [{ type: "text", sessionID: "ses", part: { text: "Continuing." } }],
      runs: [
        { events: [{ type: "text", sessionID: "ses", part: { text: "Working." } }] },
        {
          events: [{ type: "text", sessionID: "ses", part: { text: "Done." } }],
          afterRun: () => {
            outputWritten = true;
          },
        },
      ],
      sessionExport: {
        info: { id: "ses" },
        messages: [
          { type: "assistant", id: "first", finish: "stop", time: { completed: 1000 } },
          { type: "assistant", id: "second", finish: "stop", time: { completed: 2000 } },
        ],
      },
    });
    const [initial, continuation] = result.runProcess.mock.calls.map(([options]) => options);
    expect(JSON.parse(initial.env.OPENCODE_CONFIG_CONTENT).agents.build.steps).toBe(3);
    expect(JSON.parse(continuation.env.OPENCODE_CONFIG_CONTENT).agents.build.steps).toBe(1);
  });

  it.each([
    ["a nonzero exit", { events: [{ type: "text", sessionID: "ses", part: { text: "Finished." } }], processResult: { exitCode: 3 } }, {}],
    ["a max-turns hit", { events: [{ type: "step_finish", sessionID: "ses", part: { id: "finish", reason: "tool-calls" } }] }, { GH_AW_MAX_TURNS: "1" }],
    ["existing safe output", { events: [{ type: "text", sessionID: "ses", part: { text: "Finished." } }] }, {}],
    ["a missing session ID", { events: [] }, {}],
  ])("does not continue after %s", async (_case, execution, extraEnv) => {
    const result = await harness({
      env: { GH_AW_SAFE_OUTPUTS: "/outputs.jsonl", ...extraEnv },
      safeOutputsContents: _case === "existing safe output" ? '{"type":"noop"}\n' : "",
      runs: [execution],
    });
    expect(result.runProcess).toHaveBeenCalledOnce();
  });

  it("does not select a different configured provider when the requested one is missing", async () => {
    const result = await harness({ endpoints: [{ configured: true, provider: "anthropic", models_url: "http://127.0.0.1:10001/v1/models" }] });
    expect(result.process.exitCode).toBe(1);
    expect(result.options).toBeUndefined();
    expect(result.stderr).toContain("No configured /reflect models endpoint");
  });

  it("preserves remote MCP credentials in the runtime overlay without modifying repository config", async () => {
    const mcp = { github: { type: "remote", url: "http://gateway/mcp/github", headers: { Authorization: "Bearer fixture" }, oauth: false } };
    const result = await harness({ env: { GH_AW_MCP_CONFIG: "/mcp.json" }, mcp: { mcp: { servers: mcp } } });
    expect(result.config.mcp.servers).toEqual(mcp);
    expect(result.readFileSync.mock.calls.map(call => call[0])).toEqual(["/mcp.json", "/prompt.txt"]);
    expect(result.stderr).not.toContain("Bearer fixture");
  });

  it.each([{ mcpServers: {} }, { mcp: {} }, { mcp: { servers: [] } }, { mcp: { servers: null } }, { mcp: null }])("fails explicitly on malformed MCP configuration: %j", async mcp => {
    const result = await harness({ env: { GH_AW_MCP_CONFIG: "/mcp.json" }, mcp });
    expect(result.process.exitCode).toBe(1);
    expect(result.stderr).toContain("must contain an mcp.servers object");
  });

  it("converts configured MCP startup and tool timeout units", async () => {
    const result = await harness({
      env: { GH_AW_MCP_CONFIG: "/mcp.json", GH_AW_STARTUP_TIMEOUT: "240", GH_AW_TOOL_TIMEOUT: "90" },
      mcp: { mcp: { servers: { github: { type: "remote", url: "http://gateway/mcp/github", oauth: false } } } },
    });
    expect(result.config.mcp.timeout).toEqual({ startup: 240000, catalog: 240000, execution: 90000 });
    expect(result.config.mcp.servers.github).not.toHaveProperty("timeout");
    expect(result.config.experimental).not.toHaveProperty("mcp_timeout");
    const nativeOnly = await harness({ env: { GH_AW_STARTUP_TIMEOUT: "240", GH_AW_TOOL_TIMEOUT: "90" } });
    expect(nativeOnly.config.mcp.timeout).toEqual(result.config.mcp.timeout);
    const invalid = await harness({ env: { GH_AW_TOOL_TIMEOUT: "not-a-number" } });
    expect(invalid.process.exitCode).toBe(1);
    expect(invalid.stderr).toContain("positive timeout in seconds");
  });

  it.each(["", "copilot", "/auto", "copilot/"])("rejects malformed model %s", async model => {
    const result = await harness({ env: { OPENCODE_MODEL: model } });
    expect(result.process.exitCode).toBe(1);
    expect(result.options).toBeUndefined();
    expect(result.stderr).toContain("provider/model format");
  });

  it.each(["0", "-1", "bad", "1.5", "Infinity"])("rejects invalid max-turns %s", async maxTurns => {
    const result = await harness({ env: { GH_AW_MAX_TURNS: maxTurns } });
    expect(result.process.exitCode).toBe(1);
    expect(result.stderr).toContain("positive integer");
  });

  it("enforces max-turns only when the limit requires another tool-calling turn", async () => {
    const event = { type: "step_finish", sessionID: "ses", part: { id: "finish", reason: "tool-calls" } };
    const result = await harness({ env: { GH_AW_MAX_TURNS: "1" }, events: [event, event] });
    expect(result.config.agents.build.steps).toBe(1);
    expect(result.options.runtimeGuard.shouldTerminate()).toMatchObject({ terminate: true });
    expect(result.process.exitCode).toBe(1);
    expect(JSON.parse(result.stdout)).toMatchObject({ type: "opencode.max_turns", data: { maxTurns: 1 } });
    const completed = await harness({ env: { GH_AW_MAX_TURNS: "1" }, events: [{ ...event, part: { ...event.part, reason: "stop" } }] });
    expect(completed.options.runtimeGuard.shouldTerminate()).toMatchObject({ terminate: false });
    expect(completed.process.exitCode).toBeUndefined();
  });

  it.each([false, true])("counts replayed completions and accounting once despite fresh projected part IDs (live usage: %s)", async liveUsage => {
    const first = { type: "step_finish", sessionID: "ses", part: { id: "original", messageID: "first", reason: "tool-calls", ...(liveUsage ? { tokens: { input: 10, output: 3 }, cost: 0.01 } : {}) } };
    const result = await harness({
      env: { GH_AW_MAX_TURNS: "2" },
      events: [first, { ...first, part: { ...first.part, id: "reprojected" } }],
      sessionExport: {
        info: { id: "ses" },
        messages: [
          { type: "assistant", id: "first", finish: "tool-calls", time: { completed: 1000 }, tokens: { input: 10, output: 2, reasoning: 1 }, cost: 0.01 },
          { type: "assistant", id: "second", finish: "stop", time: { completed: 2000 }, tokens: { input: 20, output: 5, reasoning: 2 }, cost: 0.02 },
        ],
      },
    });
    expect(result.process.exitCode).toBeUndefined();
    expect(result.options.runtimeGuard.shouldTerminate()).toMatchObject({ terminate: false });
    const records = result.stdout
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(records[0].part).toMatchObject({ id: "original", messageID: "first" });
    const summary = opencodeParser.parseOpenCodeLog(result.logContent).logEntries.at(-1).data;
    expect(summary.numTurns).toBe(2);
    expect(summary.usage.input_tokens).toBe(30);
    expect(summary.usage.output_tokens).toBe(10);
    expect(summary.totalCostUsd).toBeCloseTo(0.03);
  });

  it("falls back to part IDs without merging completions from different sessions", async () => {
    const event = { type: "step_finish", sessionID: "ses", part: { id: "same-part", reason: "tool-calls" } };
    const repeated = await harness({ env: { GH_AW_MAX_TURNS: "2" }, events: [event, event] });
    expect(repeated.process.exitCode).toBeUndefined();
    expect(repeated.options.runtimeGuard.shouldTerminate()).toMatchObject({ terminate: false });
    const differentSession = await harness({ env: { GH_AW_MAX_TURNS: "2" }, events: [event, { ...event, sessionID: "other" }] });
    expect(differentSession.process.exitCode).toBe(1);
    expect(differentSession.stderr).toContain("max-turns limit reached");
  });

  it("counts different messages separately even when their projected part ID is reused", async () => {
    const event = { type: "step_finish", sessionID: "ses", part: { id: "reused", messageID: "first", reason: "tool-calls" } };
    const result = await harness({
      env: { GH_AW_MAX_TURNS: "2" },
      events: [event, { ...event, part: { ...event.part, messageID: "second" } }],
    });
    expect(result.process.exitCode).toBe(1);
    expect(result.options.runtimeGuard.shouldTerminate()).toMatchObject({ terminate: true });
    expect(result.execFile).not.toHaveBeenCalled();
  });

  it.each([undefined, {}, { id: "tool", tool: "shell" }, { id: "tool", tool: "shell", state: null }])("rejects malformed tool parts before inspecting maximum-step errors: %j", async part => {
    const malformed = { type: "tool_use", sessionID: "ses", part };
    expect(opencodeParser.isOpenCodeEvent(malformed)).toBe(false);
    const result = await harness({ env: { GH_AW_MAX_TURNS: "1" }, events: [malformed, { type: "text", sessionID: "ses", part: { text: "Finished." } }] });
    expect(result.process.exitCode).toBeUndefined();
    expect(result.options.runtimeGuard.shouldTerminate()).toMatchObject({ terminate: false });
    expect(result.stderr).not.toContain("TypeError");
  });

  it("preserves a nonzero exit code and rejects JSON errors with zero exit status", async () => {
    expect((await harness({ processResult: { exitCode: 7 } })).process.exitCode).toBe(7);
    const failure = await harness({ events: [{ type: "error", sessionID: "ses", error: { name: "APIError" } }] });
    expect(failure.process.exitCode).toBe(1);
    expect(failure.stderr).toContain("reported an error despite exiting with code 0");
  });

  it("does not accept a successful process status when an MCP server failed to start", async () => {
    const result = await harness({ processResult: { stderr: 'timestamp=2026-10-03T17:28:22.606Z level=WARN run=fixture message="server unavailable" key=github type=remote status=failed\n' } });
    expect(result.process.exitCode).toBe(1);
    expect(result.stderr).toContain("MCP server(s) failed to start: github");
    expect(JSON.parse(result.stdout)).toMatchObject({ type: "opencode.mcp_failure", data: { serverName: "github" } });
  });

  it("retains MCP startup failures even after they leave the bounded classifier tail", async () => {
    const result = await harness({
      stderrLines: ['timestamp=2026-10-03T17:28:22.606Z level=WARN run=fixture message="server unavailable" key=github type=remote status=failed'],
      processResult: { stderr: "only recent unrelated diagnostics remain" },
    });
    expect(result.process.exitCode).toBe(1);
    expect(result.stderr).toContain("MCP server(s) failed to start: github");
  });

  it("supports direct BYOK without pretending Copilot can use a GitHub token as an API key", async () => {
    const result = await harness({ env: { AWF_REFLECT_ENABLED: "0", GH_AW_LLM_PROVIDER: "openai", OPENCODE_MODEL: "openai/gpt-5", OPENAI_API_KEY: "secret-fixture" } });
    expect(result.process.exitCode).toBeUndefined();
    expect(result.config.providers["awf-proxy"].settings).toEqual({ baseURL: "https://api.openai.com/v1", apiKey: "secret-fixture" });
    expect(result.stderr).not.toContain("secret-fixture");
    const github = await harness({ env: { AWF_REFLECT_ENABLED: "0" } });
    expect(github.process.exitCode).toBe(1);
    expect(github.stderr).toContain("Copilot routing requires the AWF sandbox");
  });

  it.each([
    ["nested", 'timestamp=2026-10-18T15:00:00.000Z level=WARN fiber=#1 message="mcp connect failed" role=server server=github status="{\\"status\\":\\"failed\\",\\"error\\":\\"Connection refused\\"}"', "github"],
    [
      "flattened",
      String.raw`timestamp=2026-10-18T17:22:46.585Z level=WARN run=fixture message="mcp connect failed" server=fixture status.status=failed status.error="Error POSTing to endpoint: {\"error\":\"fixture denial\"} (HTTP 403)"`,
      "fixture",
    ],
  ])("rejects v2 %s MCP startup failures without relying on a nonzero CLI exit", async (_format, line, serverName) => {
    const result = await harness({
      stderrLines: [line],
    });
    expect(result.process.exitCode).toBe(1);
    expect(result.stderr).toContain(`MCP server(s) failed to start: ${serverName}`);
    expect(JSON.parse(result.stdout)).toMatchObject({ type: "opencode.mcp_failure", data: { serverName } });
  });

  it("allows successful work after a transient v2 MCP connect failure without terminal failed status", async () => {
    const transient = 'timestamp=2026-10-18T15:00:00.000Z level=WARN fiber=#1 message="mcp connect failed" role=server server=github error="Temporary connection reset"';
    const result = await harness({
      stderrLines: [transient, 'timestamp=2026-10-18T15:00:01.000Z level=INFO fiber=#1 message="mcp connected" role=server server=github'],
      processResult: { stderr: transient },
      events: [
        { type: "tool_use", sessionID: "ses", part: { id: "call", tool: "shell", state: { status: "completed", output: "Finished." } } },
        { type: "step_finish", sessionID: "ses", part: { id: "live", messageID: "first", reason: "stop", tokens: { input: 10 }, cost: 0.01 } },
      ],
      sessionExport: { info: { id: "ses" }, messages: [{ type: "assistant", id: "first", finish: "stop", time: { completed: 1000 }, tokens: { input: 10 }, cost: 0.01 }] },
    });
    expect(result.process.exitCode).toBeUndefined();
    expect(result.execFile).toHaveBeenCalledOnce();
    expect(result.stdout).not.toContain("opencode.mcp_failure");
    const summary = opencodeParser.parseOpenCodeLog(result.logContent).logEntries.at(-1).data;
    expect(summary.numTurns).toBe(1);
    expect(summary.usage.input_tokens).toBe(10);
    expect(summary.totalCostUsd).toBe(0.01);
  });

  it("reconciles missing steps and observed usage without publishing the native export's prompt", async () => {
    const result = await harness({
      events: [{ type: "step_finish", sessionID: "ses", part: { id: "live", messageID: "first", reason: "tool-calls" } }],
      sessionExport: {
        info: { id: "ses" },
        messages: [
          { type: "user", text: "Secret exported prompt." },
          { type: "assistant", id: "first", finish: "tool-calls", time: { completed: 1000 }, tokens: { input: 10 }, cost: 0.01 },
          { type: "assistant", id: "second", finish: "stop", time: { completed: 2000 }, tokens: { input: 20 }, cost: 0.02, providerState: { secret: "private provider state" } },
        ],
      },
    });
    expect(result.process.exitCode).toBeUndefined();
    expect(result.execFile.mock.calls[0].slice(0, 2)).toEqual(["opencode", ["session", "export", "--standalone", "ses"]]);
    expect(result.execFile.mock.calls[0][2]).toMatchObject({ timeout: 60000, maxBuffer: 64 * 1024 * 1024, env: result.options.env });
    const records = result.stdout
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(records).toMatchObject([
      { type: "step_finish", timestamp: 1000, part: { id: "live", messageID: "first", reason: "tool-calls", tokens: { input: 10 }, cost: 0.01 } },
      { type: "step_finish", timestamp: 2000, part: { messageID: "second", reason: "stop", tokens: { input: 20 }, cost: 0.02 } },
    ]);
    const summary = opencodeParser.parseOpenCodeLog(result.logContent).logEntries.at(-1).data;
    expect(summary.numTurns).toBe(2);
    expect(summary.usage.input_tokens).toBe(30);
    expect(summary.totalCostUsd).toBeCloseTo(0.03);
    for (const secret of ["Secret exported prompt.", "private provider state"]) {
      expect(result.stdout).not.toContain(secret);
      expect(result.stderr).not.toContain(secret);
    }
  });

  it("does not double-count accounting already present on an observed completion", async () => {
    const result = await harness({
      events: [{ type: "step_finish", sessionID: "ses", part: { id: "live", messageID: "first", reason: "stop", tokens: { input: 10 }, cost: 0.01 } }],
      sessionExport: { info: { id: "ses" }, messages: [{ type: "assistant", id: "first", finish: "stop", time: { completed: 1000 }, tokens: { input: 10 }, cost: 0.01 }] },
    });
    const summary = opencodeParser.parseOpenCodeLog(result.logContent).logEntries.at(-1).data;
    expect(summary.numTurns).toBe(1);
    expect(summary.usage.input_tokens).toBe(10);
    expect(summary.totalCostUsd).toBe(0.01);
  });

  it.each(["completed", "failed"])("emits safe %s compaction accounting without counting it as a model step", async status => {
    const result = await harness({
      env: { GH_AW_MAX_TURNS: "2" },
      sessionExport: {
        info: { id: "ses" },
        messages: [
          {
            type: "compaction",
            id: "compaction",
            status,
            time: { created: 500 },
            tokens: { input: 100, output: 3, reasoning: 4, cache: { read: 5, write: 6 } },
            cost: 0.1,
            summary: "private compaction summary",
            recent: "private recent messages",
            providerState: { secret: "private provider state" },
            providerContext: { secret: "private provider context" },
          },
          { type: "assistant", id: "first", finish: "tool-calls", time: { completed: 1000 }, tokens: { input: 10 }, cost: 0.01 },
          { type: "assistant", id: "second", finish: "stop", time: { completed: 2000 }, tokens: { input: 20 }, cost: 0.02 },
        ],
      },
    });
    expect(result.process.exitCode).toBeUndefined();
    const records = result.stdout
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(records.filter(entry => entry.type === "step_finish")).toHaveLength(2);
    expect(records.find(entry => entry.type === "opencode.compaction")).toMatchObject({
      sessionID: "ses",
      timestamp: 500,
      part: { messageID: "compaction", tokens: { input: 100, output: 7, cache: { read: 5, write: 6 } }, cost: 0.1 },
    });
    const summary = opencodeParser.parseOpenCodeLog(result.logContent).logEntries.at(-1).data;
    expect(summary.numTurns).toBe(2);
    expect(summary.usage.input_tokens).toBe(130);
    expect(summary.usage.output_tokens).toBe(7);
    expect(summary.totalCostUsd).toBeCloseTo(0.13);
    for (const secret of ["private compaction summary", "private recent messages", "private provider state", "private provider context"]) {
      expect(result.stdout).not.toContain(secret);
      expect(result.stderr).not.toContain(secret);
    }
  });

  it("does not mistake compaction-only accounting for a completed assistant step", async () => {
    const result = await harness({
      sessionExport: {
        info: { id: "ses" },
        messages: [{ type: "compaction", id: "compaction", status: "completed", time: { created: 500 }, tokens: { input: 100 }, cost: 0.1 }],
      },
    });
    expect(result.process.exitCode).toBe(1);
    expect(result.stderr).toContain("contains no completed model steps");
    expect(result.stdout).toBe("");
  });

  it("removes inherited PWD from both run and export so configured cwd controls discovery", async () => {
    const result = await harness({ env: { PWD: "/inherited", GH_AW_ENGINE_CWD: "/workspace/subdir" } });
    expect(result.process.exitCode).toBeUndefined();
    expect(result.process.env.PWD).toBe("/inherited");
    expect(result.options.env).not.toHaveProperty("PWD");
    expect(result.execFile.mock.calls[0][2]).toMatchObject({ cwd: "/workspace/subdir" });
    expect(result.execFile.mock.calls[0][2].env).not.toHaveProperty("PWD");
  });

  it("retains export failure causes but logs only safe exit and signal metadata", async () => {
    const exportError = Object.assign(new Error("Secret captured stderr and prompt."), {
      code: 1,
      signal: "SIGTERM",
      killed: true,
      stdout: "Secret provider state.",
      stderr: "Secret captured stderr and prompt.",
    });
    const result = await harness({ exportError, exportOutput: exportError.stdout });
    expect(result.process.exitCode).toBe(1);
    expect(result.errors.at(-1).cause).toBe(exportError);
    expect(result.stderr).toContain("(code=1, signal=SIGTERM, killed=true)");
    for (const content of [exportError.message, exportError.stdout, exportError.stderr]) {
      expect(result.stdout).not.toContain(content);
      expect(result.stderr).not.toContain(content);
    }
    const missingBinary = await harness({ exportError: Object.assign(new Error("Secret executable path."), { code: "ENOENT" }) });
    expect(missingBinary.stderr).toContain("(code=ENOENT)");
    const unsafeMetadata = await harness({ exportError: { code: "Secret provider state.", signal: "Secret captured stderr." } });
    expect(unsafeMetadata.stderr).not.toContain("Secret");
  });

  it("fails explicitly on session export errors or malformed exports without leaking their content", async () => {
    for (const options of [
      { exportError: new Error("Secret exported prompt.") },
      { exportOutput: "Secret exported prompt." },
      { sessionExport: { info: { id: "wrong" }, messages: [] } },
      { sessionExport: { info: { id: "ses" }, messages: [] } },
    ]) {
      const result = await harness(options);
      expect(result.process.exitCode).toBe(1);
      expect(result.stdout).toBe("");
      expect(result.stderr).toContain("OpenCode session");
      expect(result.stderr).not.toContain("Secret exported prompt.");
    }
    const empty = await harness({ events: [] });
    expect(empty.process.exitCode).toBe(1);
    expect(empty.stderr).toContain("produced no session events");
    expect(empty.execFile).not.toHaveBeenCalled();
  });

  it("rejects the native v2 maximum-step tool error even without step_finish events", async () => {
    const result = await harness({
      env: { GH_AW_MAX_TURNS: "1" },
      events: [{ type: "tool_use", sessionID: "ses", part: { partID: "part", id: "call", tool: "shell", state: { status: "error", error: "Tools are disabled after the maximum agent steps" } } }],
    });
    expect(result.process.exitCode).toBe(1);
    expect(JSON.parse(result.stdout)).toMatchObject({ type: "opencode.max_turns", data: { maxTurns: 1 } });
    expect(result.execFile).not.toHaveBeenCalled();
  });

  it("enforces max-turns from reconciled steps when the CLI omitted live step events", async () => {
    const result = await harness({
      env: { GH_AW_MAX_TURNS: "1" },
      sessionExport: { info: { id: "ses" }, messages: [{ type: "assistant", id: "first", finish: "tool-calls", time: { completed: 1000 } }] },
    });
    expect(result.process.exitCode).toBe(1);
    expect(
      result.stdout
        .trim()
        .split("\n")
        .map(line => JSON.parse(line).type)
    ).toEqual(["step_finish", "opencode.max_turns"]);
  });

  it.each([
    ["tool-calls", "unknown", "tool-calls", "stop"],
    ["tool-calls", "stop", "tool-calls", "stop"],
  ])("checks every recovered step at or after max-turns: %j", async (...reasons) => {
    const result = await harness({
      env: { GH_AW_MAX_TURNS: "2" },
      sessionExport: {
        info: { id: "ses" },
        messages: reasons.map((finish, index) => ({ type: "assistant", id: `step-${index}`, finish, time: { completed: 1000 + index } })),
      },
    });
    expect(result.process.exitCode).toBe(1);
    expect(result.stderr).toContain("max-turns limit reached");
    expect(JSON.parse(result.stdout.trim().split("\n").at(-1))).toMatchObject({ type: "opencode.max_turns", data: { maxTurns: 2 } });
  });
});

describe("OpenCode MCP adapter", () => {
  const runtimeRoot = "/tmp/gh-aw-run.Test123";

  it("rewrites URLs, preserves headers, excludes CLI servers, and emits only native fields", () => {
    const writeSecureOutput = vi.fn();
    vm.runInNewContext(engineScript("config-adapter"), {
      process: { env: { GH_AW_RUNTIME_ROOT: runtimeRoot } },
      require: () => ({
        ...gateway,
        join: (...parts) => require("node:path").join(...parts),
        loadGatewayContext: () => ({
          urlPrefix: "http://host.docker.internal:80",
          cliServers: new Set(["safeoutputs"]),
          servers: {
            github: { type: "http", url: "http://gateway:80/mcp/github", tools: ["*"], headers: { Authorization: "Bearer fixture" } },
            safeoutputs: { url: "http://gateway:80/mcp/safeoutputs" },
          },
        }),
        writeSecureOutput,
      }),
    });
    expect(writeSecureOutput.mock.calls[0][0]).toBe(`${runtimeRoot}/opencode-mcp.json`);
    expect(JSON.parse(writeSecureOutput.mock.calls[0][1])).toEqual({
      mcp: { servers: { github: { type: "remote", url: "http://host.docker.internal:80/mcp/github", headers: { Authorization: "Bearer fixture" }, disabled: false, codemode: false, oauth: false } } },
    });
  });

  it("does not silently accept a malformed gateway server", () => {
    expect(() =>
      vm.runInNewContext(engineScript("config-adapter"), {
        process: { env: { GH_AW_RUNTIME_ROOT: runtimeRoot } },
        require: () => ({
          ...gateway,
          join: (...parts) => require("node:path").join(...parts),
          loadGatewayContext: () => ({ urlPrefix: "http://gateway", cliServers: new Set(), servers: { bad: {} } }),
        }),
      })
    ).toThrow("requires a gateway HTTP URL");
  });
});
