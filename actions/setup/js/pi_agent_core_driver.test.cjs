import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { main, jsonEvent } = await import("./pi_agent_core_driver.cjs");
const { stageFiles, restoreFiles } = await import("./durable_files.cjs");
let dir;

beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-sdk-"));
  vi.stubEnv("PI_CODING_AGENT_DIR", path.join(dir, "agent"));
  vi.stubEnv("RUNNER_TEMP", dir);
  vi.stubEnv("GH_AW_PROMPT", path.join(dir, "prompt.txt"));
  vi.stubEnv("GH_AW_PI_MODEL", "openai/gpt-5.4");
  vi.stubEnv("GH_AW_PI_CONFIG", "{}");
  fs.writeFileSync(path.join(dir, "prompt.txt"), "Complete the task");
});
afterEach(() => {
  vi.unstubAllEnvs();
  fs.rmSync(dir, { recursive: true, force: true });
});

describe("Pi coding-agent SDK driver", () => {
  it("opens the exact restored native session with current models, policy and workflow instructions", async () => {
    const root = path.join(dir, "state");
    const sessionDirectory = path.join(root, "durable-sessions/pi");
    const sessionFile = path.join(sessionDirectory, "session.jsonl");
    const id = "00000000-0000-4000-8000-000000000001";
    vi.stubEnv("GH_AW_DURABLE_STORAGE", "artifact");
    vi.stubEnv("GH_AW_DURABLE_ENGINE", "pi");
    vi.stubEnv("GH_AW_DURABLE_SESSION_DIR", sessionDirectory);
    vi.stubEnv("GH_AW_DURABLE_RESUMED", "false");
    vi.stubEnv("GH_AW_DURABLE_FILES", JSON.stringify([sessionFile]));
    const model = { id: "gpt-5.4" };
    const session = {
      sessionId: id,
      subscribe: vi.fn(),
      bindExtensions: vi.fn(),
      prompt: vi.fn(async prompt => {
        const timestamp = new Date().toISOString();
        if (!fs.existsSync(sessionFile)) fs.writeFileSync(sessionFile, JSON.stringify({ type: "session", version: 3, id, timestamp, cwd: process.cwd() }) + "\n");
        const count = session.prompt.mock.calls.length;
        fs.appendFileSync(
          sessionFile,
          JSON.stringify({ type: "message", id: `message-${count}`, parentId: count > 1 ? `message-${count - 1}` : null, timestamp, message: { role: "user", content: [{ type: "text", text: prompt }] } }) + "\n"
        );
      }),
      dispose: vi.fn(),
    };
    const sdk = {
      ModelRuntime: { create: vi.fn(async () => ({ getModel: () => model })) },
      SettingsManager: { create: () => ({ applyOverrides: vi.fn(), getDefaultThinkingLevel: () => undefined, getModelThinkingLevel: () => undefined }) },
      DefaultResourceLoader: vi.fn(function () {
        return { reload: vi.fn() };
      }),
      createCodemodeExtension: vi.fn(),
      createToolSearchExtension: vi.fn(),
      createMcpExtension: vi.fn(),
      SessionManager: { open: vi.fn(() => "persistent"), inMemory: vi.fn() },
      createAgentSession: vi.fn(async () => ({ session })),
    };
    await main({ sdk, emit: vi.fn() });
    expect(sdk.createAgentSession.mock.calls[0][0]).not.toHaveProperty("thinkingLevel");
    const core = { info() {}, setOutput() {} };
    stageFiles({ root, core });
    fs.rmSync(sessionDirectory, { recursive: true });
    restoreFiles({ root, core });
    vi.stubEnv("GH_AW_DURABLE_RESUMED", "true");
    fs.writeFileSync(path.join(dir, "prompt.txt"), "Raw combined prompt is not submitted");
    const userPrompt = path.join(dir, "user.txt");
    const systemPrompt = path.join(dir, "system.txt");
    fs.writeFileSync(userPrompt, "Current workflow instructions");
    fs.writeFileSync(systemPrompt, "Current system instructions");
    vi.stubEnv("GH_AW_PI_USER_PROMPT", userPrompt);
    vi.stubEnv("GH_AW_PI_SYSTEM_PROMPT", systemPrompt);
    await main({ sdk, emit: vi.fn() });
    expect(sdk.SessionManager.open).toHaveBeenNthCalledWith(2, sessionFile, sessionDirectory, process.env.GH_AW_ENGINE_CWD || process.env.GITHUB_WORKSPACE || process.cwd());
    expect(sdk.SessionManager.inMemory).not.toHaveBeenCalled();
    expect(sdk.createAgentSession).toHaveBeenLastCalledWith(expect.objectContaining({ model, sessionManager: "persistent", thinkingLevel: "medium" }));
    expect(sdk.DefaultResourceLoader).toHaveBeenLastCalledWith(expect.objectContaining({ appendSystemPrompt: ["Current system instructions"] }));
    expect(session.prompt).toHaveBeenLastCalledWith("Current workflow instructions");
    expect(session.bindExtensions).toHaveBeenCalledTimes(2);
    session.sessionId = "00000000-0000-4000-8000-000000000002";
    await expect(main({ sdk, emit: vi.fn() })).rejects.toThrow("exact durable session");
    expect(session.prompt).toHaveBeenCalledTimes(2);
  });

  it.each([
    { name: "current workflow effort off", settings: { defaultThinkingLevel: "off" }, expected: "off" },
    { name: "current model-specific effort", settings: { defaultThinkingLevel: "high", modelThinkingLevels: { "openai/gpt-5.4": "low" } }, expected: "low" },
    { name: "model-specific effort off", settings: { defaultThinkingLevel: "high", modelThinkingLevels: { "openai/gpt-5.4": "off" } }, expected: "off" },
    { name: "native first-run default with no current effort", settings: {}, expected: "medium" },
    { name: "routed effort off over fixed and model-specific effort", settings: { defaultThinkingLevel: "high", modelThinkingLevels: { "aw-gateway/gpt-5.4": "low" } }, routedEffort: "off", expected: "off" },
    { name: "routed effort low", settings: { defaultThinkingLevel: "high" }, routedEffort: "low", expected: "low" },
  ])("overrides saved high effort with $name on durable SDK resume", async ({ settings, routedEffort, expected }) => {
    const sessionDirectory = path.join(dir, "state/durable-sessions/pi");
    const sessionFile = path.join(sessionDirectory, "session.jsonl");
    const id = "00000000-0000-4000-8000-000000000001";
    const timestamp = new Date().toISOString();
    fs.mkdirSync(sessionDirectory, { recursive: true });
    fs.writeFileSync(
      sessionFile,
      [
        { type: "session", version: 3, id, timestamp, cwd: process.cwd() },
        { type: "thinking_level_change", id: "thinking", parentId: null, timestamp, thinkingLevel: "high" },
        { type: "message", id: "message", parentId: "thinking", timestamp, message: { role: "user", content: [{ type: "text", text: "Previous task" }] } },
      ]
        .map(record => JSON.stringify(record))
        .join("\n") + "\n"
    );
    vi.stubEnv("GH_AW_DURABLE_STORAGE", "artifact");
    vi.stubEnv("GH_AW_DURABLE_ENGINE", "pi");
    vi.stubEnv("GH_AW_DURABLE_SESSION_DIR", sessionDirectory);
    vi.stubEnv("GH_AW_DURABLE_RESUMED", "true");
    vi.stubEnv("GH_AW_PI_CONFIG", JSON.stringify({ settings }));
    fs.mkdirSync(path.join(dir, "agent"), { recursive: true });
    if (routedEffort) {
      vi.stubEnv("GH_AW_MODEL_ROUTING", "1");
      fs.writeFileSync(path.join(dir, "agent/model-routing-selection.json"), JSON.stringify({ wire_model: "gpt-5.4", mapped_effort: routedEffort }));
    }
    const model = { provider: routedEffort ? "aw-gateway" : "openai", id: "gpt-5.4", reasoning: true };
    let currentSettings = {};
    const settingsManager = {
      applyOverrides: overrides => {
        currentSettings = { ...currentSettings, ...overrides };
      },
      getDefaultThinkingLevel: () => currentSettings.defaultThinkingLevel,
      getModelThinkingLevel: (provider, modelId) => currentSettings.modelThinkingLevels?.[`${provider}/${modelId}`],
    };
    const session = { sessionId: id, thinkingLevel: "high", subscribe: vi.fn(), bindExtensions: vi.fn(), prompt: vi.fn(), dispose: vi.fn() };
    const sdk = {
      ModelRuntime: { create: vi.fn(async () => ({ getModel: () => model })) },
      SettingsManager: { create: () => settingsManager },
      DefaultResourceLoader: vi.fn(function () {
        return { reload: vi.fn() };
      }),
      createCodemodeExtension: vi.fn(),
      createToolSearchExtension: vi.fn(),
      createMcpExtension: vi.fn(),
      SessionManager: { open: vi.fn(() => "persistent") },
      createAgentSession: vi.fn(async options => {
        // Pi 1.0.0 through 1.5.0 prefer saved effort when the option is omitted.
        session.thinkingLevel = options.thinkingLevel ?? "high";
        return { session };
      }),
    };
    await main({ sdk, emit: vi.fn() });
    expect(sdk.createAgentSession).toHaveBeenCalledWith(expect.objectContaining({ thinkingLevel: expected, model }));
    expect(session.thinkingLevel).toBe(expected);
    expect(session.prompt).toHaveBeenCalledWith("Complete the task");
  });

  it("runs the full session layer with MCP, codemode, and policy extensions", async () => {
    const model = { id: "gpt-5.4", reasoning: true, input: ["text", "image"] };
    const session = { sessionId: "session-id", subscribe: vi.fn(), bindExtensions: vi.fn(), prompt: vi.fn(), dispose: vi.fn() };
    const resourceLoader = { reload: vi.fn() };
    const sdk = {
      ModelRuntime: { create: vi.fn(async () => ({ getModel: () => model })) },
      SettingsManager: { create: () => ({ applyOverrides: vi.fn() }) },
      DefaultResourceLoader: vi.fn(function () {
        return resourceLoader;
      }),
      createCodemodeExtension: vi.fn(() => "codemode"),
      createToolSearchExtension: vi.fn(() => "search"),
      createMcpExtension: vi.fn(() => "mcp"),
      SessionManager: { inMemory: vi.fn(() => "in-memory") },
      createAgentSession: vi.fn(async () => ({ session })),
    };
    const emit = vi.fn();
    await main({ sdk, emit });
    expect(sdk.DefaultResourceLoader).toHaveBeenCalledWith(
      expect.objectContaining({
        extensionFactories: ["codemode", "search", "mcp"],
        additionalExtensionPaths: expect.arrayContaining([path.join(dir, "gh-aw/actions/pi_tool_policy.cjs"), path.join(dir, "gh-aw/actions/pi_subagent_extension.cjs")]),
      })
    );
    expect(sdk.createAgentSession).toHaveBeenCalledWith(expect.objectContaining({ model, resourceLoader, sessionManager: "in-memory" }));
    expect(sdk.createAgentSession.mock.calls[0][0]).not.toHaveProperty("thinkingLevel");
    expect(session.bindExtensions).toHaveBeenCalledOnce();
    expect(session.prompt).toHaveBeenCalledWith("Complete the task");
    expect(session.dispose).toHaveBeenCalledOnce();
    expect(emit).toHaveBeenCalledWith(expect.objectContaining({ type: "session", version: 3 }));
  });

  it("keeps ambient skill discovery off in bare mode but loads the staged workflow skills", async () => {
    vi.stubEnv("GH_AW_PI_BARE", "true");
    vi.stubEnv("GH_AW_PI_STAGING_DIR", path.join(dir, "staging"));
    fs.mkdirSync(path.join(dir, "staging/skills/reporting"), { recursive: true });
    fs.mkdirSync(path.join(dir, "staging/skills/ambient-helper"), { recursive: true });
    fs.writeFileSync(path.join(dir, "staging/skills/reporting/SKILL.md"), "Report.");
    fs.writeFileSync(path.join(dir, "staging/skills/ambient-helper/SKILL.md"), "Ambient.");
    fs.writeFileSync(path.join(dir, "staging/skills/.gh-aw-inline-skills.json"), '["reporting"]');
    const session = { sessionId: "session-id", subscribe: vi.fn(), bindExtensions: vi.fn(), prompt: vi.fn(), dispose: vi.fn() };
    const sdk = {
      ModelRuntime: { create: vi.fn(async () => ({ getModel: () => ({ id: "gpt-5.4" }) })) },
      SettingsManager: { create: () => ({ applyOverrides: vi.fn() }) },
      DefaultResourceLoader: vi.fn(function () {
        return { reload: vi.fn() };
      }),
      createCodemodeExtension: vi.fn(),
      createToolSearchExtension: vi.fn(),
      createMcpExtension: vi.fn(),
      SessionManager: { inMemory: vi.fn() },
      createAgentSession: vi.fn(async () => ({ session })),
    };
    await main({ sdk, emit: vi.fn() });
    expect(sdk.DefaultResourceLoader).toHaveBeenCalledWith(
      expect.objectContaining({ noSkills: true, noContextFiles: true, noPromptTemplates: true, noExtensions: true, additionalSkillPaths: [path.join(dir, "agent/skills/reporting/SKILL.md")] })
    );
  });

  it("removes cumulative streaming snapshots while preserving usage and tool identity", () => {
    const event = jsonEvent({
      type: "message_update",
      message: { usage: { input: 1 } },
      assistantMessageEvent: { type: "toolcall_start", contentIndex: 0, partial: { content: [{ type: "toolCall", id: "call-id", name: "bash" }] } },
    });
    expect(event).toEqual({ type: "message_update", usage: { input: 1 }, assistantMessageEvent: { type: "toolcall_start", contentIndex: 0, id: "call-id", toolName: "bash" } });
  });

  it("redacts secrets from provider errors in emitted session events", () => {
    vi.stubEnv("OPENAI_API_KEY", "provider-secret-value");
    const event = jsonEvent({
      type: "message_end",
      message: { role: "assistant", errorMessage: '429: {"api_key":"provider-secret-value"}' },
    });
    expect(event.message.errorMessage).toContain("[REDACTED]");
    expect(event.message.errorMessage).not.toContain("provider-secret-value");
  });

  it("redacts secrets from assistant errors in agent_end message snapshots", () => {
    vi.stubEnv("OPENAI_API_KEY", "provider-secret-value");
    const event = jsonEvent({
      type: "agent_end",
      messages: [
        { role: "assistant", errorMessage: "429: provider-secret-value" },
        { role: "user", content: "Complete the task" },
      ],
    });
    expect(event.messages[0].errorMessage).toContain("[REDACTED]");
    expect(event.messages[0].errorMessage).not.toContain("provider-secret-value");
    expect(event.messages[1]).toEqual({ role: "user", content: "Complete the task" });
  });
});
