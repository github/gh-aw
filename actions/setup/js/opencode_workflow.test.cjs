import { describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import vm from "node:vm";

const source = fs.readFileSync(new URL("../../../.github/workflows/shared/opencode.md", import.meta.url), "utf8");
const req = require;
const reflect = req("./awf_reflect.cjs");
const gateway = req("./convert_gateway_config_shared.cjs");

function engineScript(key) {
  const block = source.split(`    ${key}: |\n`)[1];
  if (!block) throw new Error(`Missing OpenCode ${key}`);
  return block.split(/\n(?= {0,5}\S)/)[0].replace(/^ {6}/gm, "");
}

async function harness({ env = {}, endpoints, processResult = {}, events = [], mcp, stderrLines = [] } = {}) {
  const stdout = [];
  const stderr = [];
  const process = {
    argv: ["node", "harness", "opencode", "run", "--format", "json", "--agent", "build"],
    env: { OPENCODE_MODEL: "copilot/auto", GH_AW_LLM_PROVIDER: "github", AWF_REFLECT_ENABLED: "1", GH_AW_PROMPT: "/prompt.txt", GITHUB_WORKSPACE: "/workspace", ...env },
    stdout: { write: value => stdout.push(value) },
    stderr: { write: value => stderr.push(value) },
    exitCode: undefined,
  };
  const runProcess = vi.fn(async options => {
    for (const event of events) options.onStdoutLine(JSON.stringify(event));
    for (const line of stderrLines) options.onStderrLine(line);
    return { exitCode: 0, stderr: "", ...processResult };
  });
  const readFileSync = vi.fn(file => {
    if (file === "/prompt.txt") return "Secret prompt; $(do-not-execute) --flag\n";
    if (file === "/mcp.json") return JSON.stringify(mcp ?? { mcp: {} });
    throw new Error(`Unexpected file ${file}`);
  });
  await vm.runInNewContext(engineScript("harness-script"), {
    process,
    require: name => {
      if (name === "fs") return { readFileSync };
      if (name === "./process_runner.cjs") return { runProcess };
      if (name === "./parse_opencode_log.cjs") return req(name);
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
  return { process, options, config: options ? JSON.parse(options.env.OPENCODE_CONFIG_CONTENT) : undefined, stdout: stdout.join(""), stderr: stderr.join(""), readFileSync };
}

describe("OpenCode shared workflow harness", () => {
  it.each([
    ["copilot/auto", "github", "@ai-sdk/openai-compatible"],
    ["anthropic/claude-sonnet-4-5", "anthropic", "@ai-sdk/anthropic"],
    ["openai/gpt-5", "openai", "@ai-sdk/openai-compatible"],
    ["codex/gpt-5", "openai", "@ai-sdk/openai-compatible"],
  ])("routes %s through its selected provider with an explicit model", async (model, provider, sdk) => {
    const result = await harness({ env: { OPENCODE_MODEL: model, GH_AW_LLM_PROVIDER: provider } });
    expect(result.process.exitCode).toBeUndefined();
    expect(result.config.provider["awf-proxy"]).toMatchObject({ npm: sdk, options: { baseURL: "http://127.0.0.1:10002/v1", apiKey: "awf-proxy" } });
    expect(result.config.enabled_providers).toEqual(["awf-proxy"]);
    expect(result.options.args.slice(-2)).toEqual(["--model", `awf-proxy/${model.slice(model.indexOf("/") + 1)}`]);
    expect(result.options.stdin).toContain("Secret prompt");
    expect(result.options.args.join(" ")).not.toContain("Secret prompt");
    expect(result.options.logArgs.join(" ")).not.toContain("Secret prompt");
    expect(result.stderr).not.toContain("Secret prompt");
    expect(result.readFileSync.mock.calls.map(call => call[0])).toEqual(["/prompt.txt"]);
  });

  it("does not select a different configured provider when the requested one is missing", async () => {
    const result = await harness({ endpoints: [{ configured: true, provider: "anthropic", models_url: "http://127.0.0.1:10001/v1/models" }] });
    expect(result.process.exitCode).toBe(1);
    expect(result.options).toBeUndefined();
    expect(result.stderr).toContain("No configured /reflect models endpoint");
  });

  it("preserves remote MCP credentials in the runtime overlay without modifying repository config", async () => {
    const mcp = { github: { type: "remote", url: "http://gateway/mcp/github", headers: { Authorization: "Bearer fixture" }, oauth: false } };
    const result = await harness({ env: { GH_AW_MCP_CONFIG: "/mcp.json" }, mcp: { mcp } });
    expect(result.config.mcp).toEqual(mcp);
    expect(result.readFileSync.mock.calls.map(call => call[0])).toEqual(["/mcp.json", "/prompt.txt"]);
    expect(result.stderr).not.toContain("Bearer fixture");
  });

  it("fails explicitly on malformed MCP configuration", async () => {
    const result = await harness({ env: { GH_AW_MCP_CONFIG: "/mcp.json" }, mcp: { mcpServers: {} } });
    expect(result.process.exitCode).toBe(1);
    expect(result.stderr).toContain("must contain an mcp object");
  });

  it("converts configured MCP startup and tool timeout units", async () => {
    const result = await harness({
      env: { GH_AW_MCP_CONFIG: "/mcp.json", GH_AW_STARTUP_TIMEOUT: "240", GH_AW_TOOL_TIMEOUT: "90" },
      mcp: { mcp: { github: { type: "remote", url: "http://gateway/mcp/github", oauth: false } } },
    });
    expect(result.config.mcp.github.timeout).toBe(240000);
    expect(result.config.experimental.mcp_timeout).toBe(90000);
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
    expect(result.config.agent.build.steps).toBe(1);
    expect(result.options.runtimeGuard.shouldTerminate()).toMatchObject({ terminate: true });
    expect(result.process.exitCode).toBe(1);
    expect(JSON.parse(result.stdout)).toMatchObject({ type: "opencode.max_turns", data: { maxTurns: 1 } });
    const completed = await harness({ env: { GH_AW_MAX_TURNS: "1" }, events: [{ ...event, part: { ...event.part, reason: "stop" } }] });
    expect(completed.options.runtimeGuard.shouldTerminate()).toMatchObject({ terminate: false });
    expect(completed.process.exitCode).toBeUndefined();
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
    expect(result.config.provider["awf-proxy"].options).toEqual({ baseURL: "https://api.openai.com/v1", apiKey: "secret-fixture" });
    expect(result.stderr).not.toContain("secret-fixture");
    const github = await harness({ env: { AWF_REFLECT_ENABLED: "0" } });
    expect(github.process.exitCode).toBe(1);
    expect(github.stderr).toContain("Copilot routing requires the AWF sandbox");
  });
});

describe("OpenCode MCP adapter", () => {
  it("rewrites URLs, preserves headers, excludes CLI servers, and emits only native fields", () => {
    const writeSecureOutput = vi.fn();
    vm.runInNewContext(engineScript("config-adapter"), {
      require: () => ({
        ...gateway,
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
    expect(writeSecureOutput.mock.calls[0][0]).toBe("/tmp/gh-aw/opencode-mcp.json");
    expect(JSON.parse(writeSecureOutput.mock.calls[0][1])).toEqual({ mcp: { github: { type: "remote", url: "http://host.docker.internal:80/mcp/github", headers: { Authorization: "Bearer fixture" }, enabled: true, oauth: false } } });
  });

  it("does not silently accept a malformed gateway server", () => {
    expect(() =>
      vm.runInNewContext(engineScript("config-adapter"), {
        require: () => ({
          ...gateway,
          loadGatewayContext: () => ({ urlPrefix: "http://gateway", cliServers: new Set(), servers: { bad: {} } }),
        }),
      })
    ).toThrow("requires a gateway HTTP URL");
  });
});
