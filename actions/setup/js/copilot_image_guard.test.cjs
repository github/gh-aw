import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";

const require = createRequire(import.meta.url);
const { COPILOT_TOOL_IMAGE_DIAGNOSTIC, COPILOT_TOOL_IMAGE_EXIT_CODE, isCopilotBYOK, hasUnsupportedCopilotToolImage, createCopilotImageObserver } = require("./copilot_image_guard.cjs");
const { runWithCopilotSDK } = require("./copilot_sdk_session.cjs");
const imageEvent = {
  type: "tool.execution_complete",
  data: { toolCallId: "view-call", success: true, result: { content: "Viewed image file successfully.", binaryResultsForLlm: [{ type: "image", mimeType: "image/png", byteLength: 21942, assetId: "sha256:test" }] } },
};

describe("Copilot BYOK tool image guard", () => {
  let root, file;
  beforeEach(() => {
    root = fs.mkdtempSync(path.join(os.tmpdir(), "copilot-image-"));
    file = path.join(root, "session", "events.jsonl");
    fs.mkdirSync(path.dirname(file), { recursive: true });
    vi.stubEnv("GH_AW_COPILOT_CONFIGURED_MODEL", "");
    vi.stubEnv("COPILOT_MODEL", "");
    vi.stubEnv("GH_AW_MODEL_FALLBACK", "");
    vi.stubEnv("GH_AW_COPILOT_SDK_MULTI_PROVIDER_JSON", "");
    vi.stubEnv("COPILOT_PROVIDER_BASE_URL", "");
    vi.stubEnv("COPILOT_API_KEY", "");
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);
  });
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
    fs.rmSync(root, { recursive: true, force: true });
  });

  it("recognizes asset-backed image results without inspecting image bytes or tool names", () => {
    expect(hasUnsupportedCopilotToolImage(imageEvent)).toBe(true);
    expect(hasUnsupportedCopilotToolImage({ ...imageEvent, data: { ...imageEvent.data, parentToolCallId: "subagent" } })).toBe(true);
  });

  it("recognizes both managed AWF and explicit BYOK without treating native auth as BYOK", () => {
    expect(isCopilotBYOK({ COPILOT_API_KEY: "test-sentinel" })).toBe(true);
    expect(isCopilotBYOK({ COPILOT_PROVIDER_BASE_URL: "http://api-proxy:10002" })).toBe(true);
    expect(isCopilotBYOK({ COPILOT_GITHUB_TOKEN: "test-native-auth" })).toBe(false);
  });

  it.each([
    null,
    { type: "assistant.message", data: { content: JSON.stringify(imageEvent) } },
    { ...imageEvent, data: { ...imageEvent.data, success: false } },
    { ...imageEvent, data: { success: true, result: { content: "Viewed image file successfully." } } },
    { ...imageEvent, data: { success: true, result: { binaryResultsForLlm: [{ type: "image", data: "aW1hZ2U=" }] } } },
    { ...imageEvent, data: { success: true, result: { binaryResultsForLlm: [{ type: "audio" }] } } },
  ])("does not mistake text, failed tools, inline images, or non-images for missing image data (%j)", event => {
    expect(hasUnsupportedCopilotToolImage(event)).toBe(false);
  });

  it("tails only new records in the exact invocation, including nested tools and split records", () => {
    fs.writeFileSync(file, JSON.stringify(imageEvent) + "\n");
    const observer = createCopilotImageObserver({ sessionId: "session", sessionDirs: [root] });
    fs.appendFileSync(file, JSON.stringify({ ...imageEvent, sessionId: "unrelated" }) + "\n");
    expect(observer.check()).toBe("");
    const record = JSON.stringify({ ...imageEvent, parentToolCallId: "subagent" });
    fs.appendFileSync(file, record.slice(0, 40));
    expect(observer.check()).toBe("");
    fs.appendFileSync(file, record.slice(40));
    expect(observer.check()).toBe("");
    expect(observer.check(true)).toBe(COPILOT_TOOL_IMAGE_DIAGNOSTIC);
    expect(observer.check()).toBe(COPILOT_TOOL_IMAGE_DIAGNOSTIC);
  });

  it("watches an invocation whose session file does not exist until the process starts", () => {
    const observer = createCopilotImageObserver({ sessionId: "session", sessionDirs: [root] });
    expect(observer.check()).toBe("");
    fs.writeFileSync(file, JSON.stringify(imageEvent) + "\n");
    expect(observer.check()).toBe(COPILOT_TOOL_IMAGE_DIAGNOSTIC);
  });

  it("fails closed if session evidence becomes unreadable", () => {
    const observer = createCopilotImageObserver({ sessionId: "session", sessionDirs: [root] });
    fs.mkdirSync(file);
    expect(observer.check()).toContain("verification failed");
  });

  it("rejects session path traversal", () => {
    expect(() => createCopilotImageObserver({ sessionId: "../other", sessionDirs: [root] })).toThrow("exact invocation session ID");
  });

  it.each([false, true])("fails a CLI image result without retrying or accepting prior output (long-lived=%s)", longLived => {
    const stub = path.join(root, "cli.cjs");
    fs.writeFileSync(
      stub,
      `
      const fs = require("node:fs"), path = require("node:path");
      const id = process.argv[process.argv.indexOf("--session-id") + 1];
      const file = path.join(process.env.COPILOT_HOME, "session-state", id, "events.jsonl");
      fs.mkdirSync(path.dirname(file), { recursive: true });
      fs.appendFileSync(file, JSON.stringify(${JSON.stringify(imageEvent)}) + "\\n");
      console.log("CLI says success");
      ${longLived ? "setInterval(() => {}, 1000);" : ""}
      `
    );
    const run = spawnSync(process.execPath, [require.resolve("./copilot_harness.cjs"), process.execPath, stub], {
      env: {
        ...process.env,
        HOME: root,
        COPILOT_HOME: path.join(root, ".copilot"),
        GH_AW_TMP_DIR: root,
        GH_AW_AWF_CONFIG_PATH: path.join(root, "awf-config.json"),
        COPILOT_API_KEY: "test-sentinel",
        COPILOT_SDK_URI: "",
        COPILOT_PROVIDER_WIRE_API: "",
        GH_AW_PHASE: "agent",
        GH_AW_MODEL_ROUTING: "0",
        GH_AW_SAFE_OUTPUTS: "",
        AWF_REFLECT_ENABLED: "0",
        GH_AW_HARNESS_INITIAL_DELAY_MS: "1",
        GH_AW_HARNESS_MAX_RETRIES: "1",
      },
      encoding: "utf8",
      timeout: 10000,
    });
    expect(run.error, run.stderr).toBeUndefined();
    expect(run.status, run.stderr).toBe(1);
    expect(run.stderr).toContain(COPILOT_TOOL_IMAGE_DIAGNOSTIC);
    expect(run.stderr).not.toContain("attempt 2: spawning");
  });

  it.each(["normal", "partial-output", "idle-timeout", "stalled", "subagent", "native", "inline", "byok-env", "abort-fails", "verification-enabled"])("handles SDK image completion (%s)", async mode => {
    const abort = vi.fn().mockResolvedValue(undefined);
    if (mode === "abort-fails") abort.mockRejectedValue(new Error("abort failed"));
    if (mode === "byok-env") vi.stubEnv("COPILOT_API_KEY", "test-sentinel");
    if (mode === "verification-enabled") vi.stubEnv("GH_AW_COPILOT_CONFIGURED_MODEL", "test");
    const disconnect = vi.fn().mockResolvedValue(undefined);
    const logger = vi.fn();
    let handler;
    class FakeCopilotClient {
      start = async () => {};
      stop = async () => {};
      createSession = async () => ({
        sessionId: "sdk",
        on: callback => {
          handler = callback;
        },
        sendAndWait: async () => {
          if (mode === "partial-output") handler({ type: "assistant.message", data: { content: "Starting." } });
          handler(
            mode === "inline" ? { ...imageEvent, data: { ...imageEvent.data, result: { binaryResultsForLlm: [{ type: "image", data: "aW1hZ2U=" }] } } } : { ...imageEvent, ...(mode === "subagent" ? { parentToolCallId: "task" } : {}) }
          );
          if (mode === "stalled") return new Promise(() => {});
          if (mode === "idle-timeout") throw new Error("Timeout after 100ms waiting for session.idle");
          return { type: "assistant.message", data: { content: "done" } };
        },
        abort,
        disconnect,
      });
    }
    const result = await runWithCopilotSDK({
      sdkUri: "http://127.0.0.1:3002",
      prompt: "Describe the PNG.",
      logger,
      workingDirectory: root,
      agentsBaseDir: root,
      sessionStateBaseDir: root,
      ...(mode === "native" || mode === "byok-env" ? {} : { model: "test", providers: [{ name: "test", type: "openai", baseUrl: "http://api-proxy:10002" }], models: [{ id: "test", provider: "test" }] }),
      sdkModule: { CopilotClient: FakeCopilotClient, RuntimeConnection: { forUri: () => ({}) }, approveAll: () => "allow" },
    });
    const guarded = mode !== "native" && mode !== "inline";
    expect(result.exitCode).toBe(guarded ? COPILOT_TOOL_IMAGE_EXIT_CODE : 0);
    expect(abort).toHaveBeenCalledTimes(guarded ? 1 : 0);
    expect(disconnect).toHaveBeenCalledTimes(1);
    if (guarded) {
      expect(result.output).toContain(COPILOT_TOOL_IMAGE_DIAGNOSTIC);
      expect(logger.mock.calls.flat().some(message => message.includes("treating as completed"))).toBe(false);
      const events = fs.readFileSync(path.join(root, "sdk/events.jsonl"), "utf8");
      expect(events).toContain('"code":"copilot_tool_image_unsupported"');
      expect(events).toContain('"errorType":"unsupported_capability"');
    }
  });
});
