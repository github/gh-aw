import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { main, jsonEvent } = await import("./pi_agent_core_driver.cjs");
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
        additionalExtensionPaths: expect.arrayContaining([path.join(dir, "gh-aw/actions/pi_tool_policy.cjs")]),
      })
    );
    expect(sdk.createAgentSession).toHaveBeenCalledWith(expect.objectContaining({ model, resourceLoader, sessionManager: "in-memory" }));
    expect(session.bindExtensions).toHaveBeenCalledOnce();
    expect(session.prompt).toHaveBeenCalledWith("Complete the task");
    expect(session.dispose).toHaveBeenCalledOnce();
    expect(emit).toHaveBeenCalledWith(expect.objectContaining({ type: "session", version: 3 }));
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
});
