// @ts-check

import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { runHarnessRetryLoop, shouldSkipForNoopSafeOutputs, shouldStopForNoopSafeOutputs } = require("./harness_retry_runner.cjs");

describe("harness_retry_runner.cjs", () => {
  it("detects pre-flight noop safe-outputs", () => {
    const logs = [];
    const skipped = shouldSkipForNoopSafeOutputs({
      safeOutputsPath: "/tmp/safe-outputs.jsonl",
      hasNoopInSafeOutputs: () => true,
      log: message => logs.push(message),
    });

    expect(skipped).toBe(true);
    expect(logs[0]).toContain("pre-flight: noop message found in safe-outputs");
  });

  it("ignores pre-flight noop when no safe-outputs path is configured", () => {
    const skipped = shouldSkipForNoopSafeOutputs({
      safeOutputsPath: "",
      hasNoopInSafeOutputs: () => {
        throw new Error("should not inspect safe-outputs without a path");
      },
      log: () => {},
    });

    expect(skipped).toBe(false);
  });

  it("detects noop safe-outputs after a failed attempt", () => {
    const logs = [];
    const stopped = shouldStopForNoopSafeOutputs({
      attempt: 1,
      safeOutputsPath: "/tmp/safe-outputs.jsonl",
      hasNoopInSafeOutputs: () => true,
      log: message => logs.push(message),
    });

    expect(stopped).toBe(true);
    expect(logs[0]).toContain("attempt 2: noop message found in safe-outputs — not retrying");
  });

  it("stops on first-attempt success", async () => {
    const logs = [];
    let runCalls = 0;
    const result = await runHarnessRetryLoop({
      maxRetries: 3,
      initialDelayMs: 5,
      backoffMultiplier: 2,
      maxDelayMs: 20,
      driverStartTime: Date.now(),
      harnessName: "Test harness",
      log: message => logs.push(message),
      softTimeoutGuard: null,
      runAttempt: async attempt => {
        runCalls++;
        return { exitCode: 0, output: "", hasOutput: false, durationMs: 0, watchdogFired: false };
      },
      handleFailure: () => {
        throw new Error("success should not call handleFailure");
      },
      sleepFn: async () => {},
    });

    expect(runCalls).toBe(1);
    expect(result.exitCode).toBe(0);
    expect(result.attempts).toBe(1);
    expect(logs.some(message => message.includes("success on attempt 1"))).toBe(true);
  });

  it("never retries or suppresses cancellation", async () => {
    const result = await runHarnessRetryLoop({
      maxRetries: 3,
      initialDelayMs: 1,
      backoffMultiplier: 2,
      maxDelayMs: 10,
      driverStartTime: Date.now(),
      harnessName: "Test",
      log: () => {},
      softTimeoutGuard: null,
      runAttempt: async () => ({ exitCode: 143, output: "partial", hasOutput: true, cancelled: true }),
      handleFailure: () => {
        throw new Error("cancellation must bypass failure suppression");
      },
    });
    expect(result.attempts).toBe(1);
    expect(result.exitCode).toBe(143);
  });

  it("applies exponential backoff between retry decisions", async () => {
    const sleeps = [];
    const retryModes = [];
    const result = await runHarnessRetryLoop({
      maxRetries: 2,
      initialDelayMs: 10,
      backoffMultiplier: 3,
      maxDelayMs: 25,
      driverStartTime: Date.now(),
      harnessName: "Test harness",
      log: () => {},
      softTimeoutGuard: null,
      getRetryMode: attempt => {
        retryModes.push(attempt);
        return "fresh run";
      },
      runAttempt: async attempt => ({ exitCode: attempt === 2 ? 0 : 1, output: "partial", hasOutput: true, durationMs: 0, watchdogFired: false }),
      handleFailure: () => ({ action: "retry" }),
      sleepFn: async ms => {
        sleeps.push(ms);
      },
    });

    expect(result.exitCode).toBe(0);
    expect(result.attempts).toBe(3);
    expect(sleeps).toEqual([10, 25]);
    expect(retryModes).toEqual([1, 2]);
  });

  it("resumes exponential backoff from a callback-selected next delay", async () => {
    const sleeps = [];
    const result = await runHarnessRetryLoop({
      maxRetries: 3,
      initialDelayMs: 10,
      backoffMultiplier: 2,
      maxDelayMs: 100,
      driverStartTime: Date.now(),
      harnessName: "Test harness",
      log: () => {},
      softTimeoutGuard: null,
      runAttempt: async attempt => ({ exitCode: attempt === 3 ? 0 : 1, output: "", hasOutput: false, durationMs: 0, watchdogFired: false }),
      handleFailure: ({ attempt }) => ({ action: "retry", nextDelayMs: attempt === 0 ? 7 : undefined }),
      sleepFn: async ms => {
        sleeps.push(ms);
      },
    });

    expect(result.exitCode).toBe(0);
    expect(sleeps).toEqual([7, 14, 28]);
  });

  it("propagates a callback-selected terminal exit code", async () => {
    const result = await runHarnessRetryLoop({
      maxRetries: 3,
      initialDelayMs: 1,
      backoffMultiplier: 2,
      maxDelayMs: 10,
      driverStartTime: Date.now(),
      harnessName: "Test harness",
      log: () => {},
      softTimeoutGuard: null,
      runAttempt: async () => ({ exitCode: 1, output: "noop", hasOutput: true, durationMs: 0, watchdogFired: false }),
      handleFailure: () => ({ action: "stop", exitCode: 0 }),
      sleepFn: async () => {},
    });

    expect(result.exitCode).toBe(0);
    expect(result.attempts).toBe(1);
  });

  it("stops before an expired soft deadline without reporting an unstarted attempt", async () => {
    const logs = [];
    const result = await runHarnessRetryLoop({
      maxRetries: 3,
      initialDelayMs: 1,
      backoffMultiplier: 2,
      maxDelayMs: 10,
      driverStartTime: Date.now(),
      harnessName: "Test harness",
      log: message => logs.push(message),
      softTimeoutGuard: { timeoutMinutes: 1, softDeadlineMs: Date.now() - 1 },
      runAttempt: async () => {
        throw new Error("should not run after soft timeout");
      },
      handleFailure: () => ({ action: "stop" }),
      sleepFn: async () => {},
    });

    expect(result.exitCode).toBe(1);
    expect(result.attempts).toBe(0);
    expect(result.lastResult).toBeNull();
    expect(logs.some(message => message.includes("before attempt 1"))).toBe(true);
  });

  it("caps backoff at the remaining soft deadline", async () => {
    const guard = { timeoutMinutes: 2, softDeadlineMs: Date.now() + 50000 };
    const sleeps = [];
    const result = await runHarnessRetryLoop({
      maxRetries: 3,
      initialDelayMs: 100000,
      backoffMultiplier: 2,
      maxDelayMs: 100000,
      driverStartTime: Date.now(),
      harnessName: "Test",
      log: () => {},
      softTimeoutGuard: guard,
      runAttempt: async () => ({ exitCode: 1, output: "partial", hasOutput: true }),
      handleFailure: () => ({ action: "retry" }),
      sleepFn: async ms => {
        sleeps.push(ms);
        guard.softDeadlineMs = Date.now() - 1;
      },
    });
    expect(sleeps[0]).toBeGreaterThan(0);
    expect(sleeps[0]).toBeLessThanOrEqual(50000);
    expect(result.attempts).toBe(1);
    expect(result.exitCode).toBe(1);
  });

  it("stops after backoff when the soft deadline expires during sleep", async () => {
    const softTimeoutGuard = { timeoutMinutes: 1, softDeadlineMs: Date.now() + 1000 };
    let runCalls = 0;
    const result = await runHarnessRetryLoop({
      maxRetries: 3,
      initialDelayMs: 1,
      backoffMultiplier: 2,
      maxDelayMs: 10,
      driverStartTime: Date.now(),
      harnessName: "Test harness",
      log: () => {},
      softTimeoutGuard,
      runAttempt: async () => {
        runCalls++;
        return { exitCode: 1, output: "", hasOutput: false, durationMs: 0, watchdogFired: false };
      },
      handleFailure: () => ({ action: "retry" }),
      sleepFn: async () => {
        softTimeoutGuard.softDeadlineMs = Date.now() - 1;
      },
    });

    expect(result.exitCode).toBe(1);
    expect(runCalls).toBe(1);
    expect(result.attempts).toBe(1);
  });
});
