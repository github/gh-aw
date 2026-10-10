import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createRequire } from "module";
import * as fs from "fs";
import * as path from "path";
import os from "node:os";

const require = createRequire(import.meta.url);
const { runWithCopilotSDK } = require("./copilot_sdk_session.cjs");
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
    expect(createSession).toHaveBeenCalledWith(expect.objectContaining({ model: "gpt-5.4", customAgents: [{ name: "researcher", prompt: "Research carefully.", model: "claude-sonnet-4.6" }] }));
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
