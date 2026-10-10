import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createRequire } from "module";
import * as fs from "fs";
import * as path from "path";
import os from "node:os";

const require = createRequire(import.meta.url);
const { runWithCopilotSDK, resolveCopilotSDKWorkingDirectory } = require("./copilot_sdk_session.cjs");
const models = [
  { id: "gpt-5.4", provider: "openai" },
  { id: "claude-sonnet-4.6", provider: "anthropic" },
];
const providers = [
  { name: "openai", type: "openai", baseUrl: "http://api-proxy:10002" },
  { name: "anthropic", type: "anthropic", baseUrl: "http://api-proxy:10002" },
];

describe("copilot_sdk_session runtime configuration", () => {
  let base;
  beforeEach(() => {
    base = fs.mkdtempSync(path.join(os.tmpdir(), "sdk-session-test-"));
    vi.stubEnv("GH_AW_SESSION_STATE_BASE_DIR", base);
    vi.stubEnv("GH_AW_ENGINE_CWD", base);
    vi.stubEnv("COPILOT_MODEL", "");
    vi.stubEnv("GH_AW_MODEL_FALLBACK", "");
    vi.stubEnv("GH_AW_MODEL_ROUTING", "");
    vi.stubEnv("GH_AW_COPILOT_SDK_SERVER_ARGS", "[]");
    vi.stubEnv("GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON", JSON.stringify({ model: "claude-sonnet-4.6", models, providers }));
  });
  afterEach(() => {
    vi.unstubAllEnvs();
    fs.rmSync(base, { recursive: true, force: true });
    vi.restoreAllMocks();
  });

  it("resolves engine.cwd against the workspace rather than the driver process directory", async () => {
    vi.stubEnv("GITHUB_WORKSPACE", "/w");
    vi.stubEnv("GH_AW_ENGINE_CWD", "packages/app");
    const cwd = vi.spyOn(process, "cwd").mockReturnValue("/w/packages/app");
    expect(resolveCopilotSDKWorkingDirectory()).toBe("/w/packages/app");
    cwd.mockRestore();

    const app = path.join(base, "packages/app");
    fs.mkdirSync(path.join(app, ".github/agents"), { recursive: true });
    fs.writeFileSync(path.join(app, ".github/agents/local.agent.md"), "---\nmodel: small\n---\nApp agent.");
    vi.stubEnv("GITHUB_WORKSPACE", base);
    vi.spyOn(process, "cwd").mockReturnValue(app);
    const createSession = vi.fn().mockResolvedValue({
      sessionId: "relative-cwd",
      on: () => {},
      sendAndWait: async () => ({ data: { content: "done" } }),
      disconnect: async () => {},
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    expect(createSession).toHaveBeenCalledWith(
      expect.objectContaining({
        workingDirectory: app,
        customAgents: [{ name: "local", displayName: "local", prompt: "App agent.", model: "anthropic/claude-sonnet-4.6" }],
      })
    );
  });

  it.each(["{bad json", '{"args":["--no-custom-instructions"]}'])("warns and starts a session for invalid server args %s", serverArgs => {
    vi.stubEnv("GH_AW_COPILOT_SDK_SERVER_ARGS", serverArgs);
    const createSession = vi.fn().mockResolvedValue({
      sessionId: "invalid-args",
      on: () => {},
      sendAndWait: async () => ({ data: { content: "done" } }),
      disconnect: async () => {},
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const logger = vi.fn();
    return runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger,
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    }).then(result => {
      expect(result.exitCode).toBe(0);
      expect(createSession).toHaveBeenCalledWith(expect.objectContaining({ skipCustomInstructions: false }));
      expect(logger).toHaveBeenCalledWith(expect.stringMatching(/warning: Invalid GH_AW_COPILOT_SDK_SERVER_ARGS.*using sidecar default args/));
    });
  });

  it("preserves explicit agent models without a BYOK catalog or misleading warnings", async () => {
    vi.stubEnv("GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON", "");
    fs.mkdirSync(path.join(base, ".github/agents"), { recursive: true });
    fs.writeFileSync(path.join(base, ".github/agents/researcher.agent.md"), "---\nmodel: claude-sonnet-4.6\n---\nResearch carefully.");
    const createSession = vi.fn().mockResolvedValue({
      sessionId: "no-catalog",
      on: () => {},
      sendAndWait: async () => ({ data: { content: "done" } }),
      disconnect: async () => {},
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const logger = vi.fn();
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      model: "gpt-5.4",
      logger,
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    expect(createSession).toHaveBeenCalledWith(expect.objectContaining({ model: "gpt-5.4", customAgents: [{ name: "researcher", displayName: "researcher", prompt: "Research carefully.", model: "claude-sonnet-4.6" }] }));
    expect(logger.mock.calls.flat().some(message => message.includes("warning: custom agent"))).toBe(false);
  });

  it("fails an unknown session model explicitly rather than warning and continuing", async () => {
    const createSession = vi.fn();
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const logger = vi.fn();
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      model: "unavailable-session-model",
      logger,
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(1);
    expect(result.output).toMatch(/Cannot qualify session model "unavailable-session-model"/);
    expect(logger).toHaveBeenCalledWith(expect.stringMatching(/error: Cannot qualify session model/));
    expect(createSession).not.toHaveBeenCalled();
  });

  it("attributes SDK lifecycle and metrics to declared names instead of task display names", async () => {
    let handler;
    const events = [
      { type: "subagent.started", data: { invocationId: "child", agentName: "file-summarizer", agentDisplayName: "readme-summarizer" } },
      { type: "subagent.completed", data: { invocationId: "child", agentName: "file-summarizer", agentDisplayName: "summarize-readme" } },
      { type: "session.shutdown", data: { agentMetrics: { child: { agentName: "file-summarizer", agentDisplayName: "readme-summarizer" }, main: { totalNanoAiu: 1 } } } },
    ];
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = async () => ({
        sessionId: "declared-names",
        on: callback => {
          handler = callback;
        },
        sendAndWait: async () => {
          events.forEach(event => handler(event));
          return { data: { content: "done" } };
        },
        disconnect: async () => {},
      });
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    const captured = fs
      .readFileSync(path.join(base, "declared-names/events.jsonl"), "utf8")
      .trim()
      .split("\n")
      .map(line => JSON.parse(line));
    expect(captured[0].data.agentDisplayName).toBe("file-summarizer");
    expect(captured[1].data.agentDisplayName).toBe("file-summarizer");
    expect(captured[2].data.agentMetrics.child.agentDisplayName).toBe("file-summarizer");
    expect(captured[2].data.agentMetrics.main).toEqual({ totalNanoAiu: 1 });
    expect(events[0].data.agentDisplayName).toBe("readme-summarizer");
    expect(events[2].data.agentMetrics.child.agentDisplayName).toBe("readme-summarizer");
  });

  it.each(["routing", "environment", "fallback", "reflect"])("qualifies the %s model before createSession", async source => {
    const expected = source === "reflect" ? "anthropic/claude-sonnet-4.6" : "openai/gpt-5.4";
    if (source === "routing" || source === "environment") vi.stubEnv("COPILOT_MODEL", "gpt-5.4");
    if (source === "routing") {
      vi.stubEnv("GH_AW_MODEL_ROUTING", "1");
      vi.stubEnv("GH_AW_MODEL_FALLBACK", "claude-sonnet-4.6");
    }
    if (source === "fallback") vi.stubEnv("GH_AW_MODEL_FALLBACK", "gpt-5.4");
    vi.stubEnv("GH_AW_COPILOT_SDK_SERVER_ARGS", '["--no-custom-instructions"]');
    const createSession = vi.fn().mockResolvedValue({
      sessionId: source,
      on: () => {},
      sendAndWait: async () => ({ data: { content: "done" } }),
      disconnect: async () => {},
    });
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = createSession;
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "test",
      logger: () => {},
      agentsBaseDir: base,
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    expect(result.exitCode).toBe(0);
    expect(createSession).toHaveBeenCalledWith(expect.objectContaining({ model: expected, providers, models, workingDirectory: base, skipCustomInstructions: true, customAgents: [] }));
  });
});
