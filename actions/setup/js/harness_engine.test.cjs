// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import os from "os";
import path from "path";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { createHarnessRuntime, startEngineHarness, recordFailureClasses, formatAttemptFailure, resolveNonRetryableGuardDecision, stopAfterUnrecoverableFailure } = require("./harness_engine.cjs");

const FIXED_RETRY_CONFIG = () => ({ maxRetries: 2, initialDelayMs: 1, backoffMultiplier: 1, maxDelayMs: 1 });
const PROXY_AI_CREDITS_REJECTION = JSON.stringify({
  type: "assistant",
  is_api_error_message: true,
  error: "authentication_failed",
  message: { content: [{ type: "text", text: "API Error: 403 Maximum AI credits exceeded (302.1 / 300)." }] },
});

describe("harness_engine", () => {
  /** @type {string} */
  let tmpDir;
  /** @type {string[]} */
  let logs;
  const log = (/** @type {string} */ message) => logs.push(message);

  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "harness-engine-"));
    logs = [];
    vi.stubEnv("GH_AW_SAFE_OUTPUTS", "");
    vi.stubEnv("GH_AW_HARNESS_TELEMETRY_DIR", tmpDir);
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    fs.rmSync(tmpDir, { recursive: true, force: true });
  });

  /** @param {object[]} entries */
  const writeSafeOutputs = entries => {
    const file = path.join(tmpDir, "safe-outputs.jsonl");
    fs.writeFileSync(file, entries.map(entry => JSON.stringify(entry) + "\n").join(""));
    return file;
  };

  describe("formatAttemptFailure", () => {
    it("omits undefined fields", () => {
      expect(formatAttemptFailure(0, { exitCode: 1, crashSignal: undefined, hasOutput: false })).toBe("attempt 1 failed: exitCode=1 hasOutput=false");
    });
  });

  describe("recordFailureClasses", () => {
    it("records only detected classes on the result", () => {
      const result = { exitCode: 1, output: "", hasOutput: false };
      expect(recordFailureClasses(result, { auth: true, rate_limit: false, crash: true })).toEqual(["auth", "crash"]);
      expect(result.failureClasses).toEqual(["auth", "crash"]);
    });
  });

  describe("stopAfterUnrecoverableFailure", () => {
    it("distinguishes exhausted retries from a silent no-output failure", () => {
      expect(stopAfterUnrecoverableFailure({ attempt: 2, maxRetries: 2, exitCode: 3, log })).toEqual({ action: "stop" });
      expect(stopAfterUnrecoverableFailure({ attempt: 0, maxRetries: 2, exitCode: 3, log })).toEqual({ action: "stop" });
      expect(logs[0]).toBe("all 2 retries exhausted — giving up (exitCode=3)");
      expect(logs[1]).toContain("attempt 1: no output produced — not retrying");
    });
  });

  describe("createHarnessRuntime.run", () => {
    it("passes the pre-attempt safe-outputs offset to each attempt and records it on the result", async () => {
      const safeOutputsPath = writeSafeOutputs([{ type: "noop" }]);
      const env = { ...process.env, GH_AW_SAFE_OUTPUTS: safeOutputsPath };
      const runtime = createHarnessRuntime({ harnessName: "Test harness", log, env, resolveRetryConfig: FIXED_RETRY_CONFIG });
      const seenOffsets = [];
      /** @type {any[]} */
      const failures = [];

      const outcome = await runtime.run({
        sleepFn: async () => {},
        runAttempt: async (_attempt, _observe, { safeOutputsByteOffset }) => {
          seenOffsets.push(safeOutputsByteOffset);
          fs.appendFileSync(safeOutputsPath, JSON.stringify({ type: "create_issue" }) + "\n");
          return { exitCode: 1, output: "boom", hasOutput: true };
        },
        handleFailure: ({ attempt, result }) => {
          failures.push(result);
          return attempt === 0 ? { action: "retry" } : { action: "stop" };
        },
      });

      const initialSize = Buffer.byteLength(JSON.stringify({ type: "noop" }) + "\n");
      const appendedSize = Buffer.byteLength(JSON.stringify({ type: "create_issue" }) + "\n");
      expect(seenOffsets).toEqual([initialSize, initialSize + appendedSize]);
      expect(failures.map(result => result.safeOutputsByteOffset)).toEqual(seenOffsets);
      expect(outcome.exitCode).toBe(1);
    });
  });

  describe("startEngineHarness", () => {
    it("exits with status 1 and returns null when no command is supplied", () => {
      const exit = vi.fn();
      const stderr = vi.spyOn(process.stderr, "write").mockImplementation(() => true);
      try {
        expect(startEngineHarness({ engine: "demo", harnessName: "Demo harness", log, argv: ["node", "demo_harness.cjs"], exit })).toBeNull();
        expect(stderr).toHaveBeenCalledWith("demo-harness: Usage: node demo_harness.cjs <command> [args...]\n");
      } finally {
        stderr.mockRestore();
      }
      expect(exit).toHaveBeenCalledWith(1);
    });

    it("logs the startup configuration and the final duration", () => {
      const harness = startEngineHarness({ engine: "demo", harnessName: "Demo harness", log, argv: ["node", "demo_harness.cjs", "agent", "--flag"], resolveRetryConfig: FIXED_RETRY_CONFIG });
      expect(harness?.command).toBe("agent");
      expect(harness?.args).toEqual(["--flag"]);
      expect(logs[0]).toMatch(/^starting: command=agent maxRetries=2 initialDelayMs=1 backoffMultiplier=1 maxDelayMs=1 nodeVersion=/);
      expect(harness?.finish(7)).toBe(7);
      expect(logs.at(-1)).toMatch(/^done: exitCode=7 totalDuration=/);
    });
  });

  describe("resolveNonRetryableGuardDecision", () => {
    /** @param {string} output @param {object} [extra] */
    const decide = (output, extra = {}) => resolveNonRetryableGuardDecision({ attempt: 0, result: { exitCode: 1, output, hasOutput: true }, log, safeOutputsPath: "", ...extra });

    it("returns null when no guard applies", async () => {
      expect(await decide("ordinary failure")).toBeNull();
    });

    it("exits 0 for trusted AI-credit exhaustion reported by the proxy", async () => {
      expect(await decide(PROXY_AI_CREDITS_REJECTION, { isAuthenticationFailed: true })).toEqual({ action: "stop", exitCode: 0 });
      expect(logs.join("\n")).toContain("trusted budget-abort evidence");
    });

    it("keeps normal failure handling for an untrusted AI-credits marker", async () => {
      expect(await decide("max ai credits exceeded")).toBeNull();
      expect(logs.join("\n")).toContain("without trusted firewall audit confirmation");
    });

    it("lets an engine veto the AI-credits success exit", async () => {
      expect(await decide(PROXY_AI_CREDITS_REJECTION, { vetoAICreditsSuccess: true })).toBeNull();
    });

    it("stops on an already-active goal only when the engine opts in", async () => {
      const output = "This thread already has a goal. use update_goal instead";
      expect(await decide(output)).toBeNull();
      expect(await decide(output, { stopOnGoalAlreadyActive: true })).toEqual({ action: "stop" });
    });

    it("suppresses a false-red invocation cap once expected outputs were staged", async () => {
      const safeOutputsPath = writeSafeOutputs([{ type: "create_issue" }]);
      expect(await decide("Maximum LLM invocations exceeded", { safeOutputsPath })).toEqual({ action: "stop", exitCode: 0 });
      expect(logs.join("\n")).toContain("suppressing terminal verdict (false-red: core work succeeded)");
    });

    it("requires a terminal output for the invocation cap when requested and reports incompleteness", async () => {
      const safeOutputsPath = writeSafeOutputs([{ type: "create_pull_request_review_comment" }]);
      const decision = await decide("Maximum LLM invocations exceeded", { safeOutputsPath, requireTerminalSafeOutputForInvocationCap: true, invocationCapIncompleteReason: "cap reached" });
      expect(decision).toEqual({ action: "stop" });
      expect(logs.join("\n")).not.toContain("suppressing terminal verdict");
      expect(logs).toContain("report_incomplete skipped: GH_AW_SAFE_OUTPUTS is not set");
    });
  });
});
