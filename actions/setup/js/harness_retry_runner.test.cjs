// @ts-check

import { afterEach, describe, expect, it } from "vitest";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const { captureExecutionEvidenceConfiguration, resolveHarnessRetryTelemetryPath, runHarnessRetryLoop, shouldSkipForNoopSafeOutputs, shouldStopForNoopSafeOutputs, stripExecutionEvidenceEnv } = require("./harness_retry_runner.cjs");

describe("harness_retry_runner.cjs", () => {
  let directory;
  afterEach(() => {
    if (directory) fs.rmSync(directory, { recursive: true, force: true });
    directory = undefined;
  });

  function executionEnv(overrides = {}) {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "noop-execution-"));
    return {
      GH_AW_AWF_EXECUTION_EVIDENCE_FILE: path.join(directory, "execution.json"),
      GH_AW_AWF_EXECUTION_COMPONENT: "agent",
      GITHUB_RUN_ID: "123",
      GITHUB_RUN_ATTEMPT: "2",
      ...overrides,
    };
  }

  function telemetryOptions(exitCodes = [0]) {
    directory = fs.mkdtempSync(path.join(os.tmpdir(), "harness-retry-"));
    return {
      telemetryPath: path.join(directory, "harness_retry.json"),
      maxRetries: 2,
      initialDelayMs: 1,
      backoffMultiplier: 2,
      maxDelayMs: 10,
      driverStartTime: Date.now(),
      harnessName: "Test harness",
      log: () => {},
      softTimeoutGuard: null,
      runAttempt: async attempt => ({ exitCode: exitCodes[attempt], output: "private output", hasOutput: true }),
      handleFailure: ({ result }) => {
        result.failureClasses = ["rate_limit_error"];
        return { action: "retry" };
      },
      sleepFn: async () => {},
    };
  }

  it.each([
    [{}, "/tmp/gh-aw/agent/harness_retry.json"],
    [{ GH_AW_PHASE: "detection" }, "/tmp/gh-aw/threat-detection/detection_harness_retry.json"],
    [{ GH_AW_TMP_DIR: "/custom" }, "/custom/agent/harness_retry.json"],
    [{ GH_AW_HARNESS_TELEMETRY_DIR: "/shared/sandbox/agent" }, "/shared/sandbox/agent/harness_retry.json"],
    [{ GH_AW_HARNESS_TELEMETRY_DIR: "/shared/sandbox/agent", GH_AW_PHASE: "detection" }, "/shared/sandbox/agent/detection_harness_retry.json"],
  ])("resolves the phase-isolated telemetry path for %j", (env, expected) => {
    expect(resolveHarnessRetryTelemetryPath(env)).toBe(expected);
  });

  it.each([
    { exitCodes: [0], attempts: 1, exitCode: 0 },
    { exitCodes: [1, 1, 0], attempts: 3, exitCode: 0 },
    { exitCodes: [1, 1, 1], attempts: 3, exitCode: 1 },
  ])("persists attempt telemetry for $exitCodes", async ({ exitCodes, attempts, exitCode }) => {
    const options = telemetryOptions(exitCodes);
    fs.writeFileSync(options.telemetryPath, '{"attempts":99}');
    const result = await runHarnessRetryLoop(options);
    const content = fs.readFileSync(options.telemetryPath, "utf8");
    expect(result).toMatchObject({ attempts, exitCode });
    expect(JSON.parse(content)).toEqual({
      attempts,
      exit_code: exitCode,
      attempt_results: exitCodes.map((code, index) => ({ attempt: index + 1, exit_code: code, failure_classes: code === 0 ? [] : ["rate_limit_error"] })),
    });
    expect(content).not.toContain("private output");
    expect(fs.existsSync(`${options.telemetryPath}.tmp`)).toBe(false);
  });

  it("records cancellation without invoking the failure handler", async () => {
    const options = telemetryOptions();
    options.runAttempt = async () => ({ exitCode: 143, output: "", hasOutput: false, cancelled: true });
    await runHarnessRetryLoop(options);
    expect(JSON.parse(fs.readFileSync(options.telemetryPath, "utf8"))).toMatchObject({
      attempts: 1,
      exit_code: 143,
      attempt_results: [{ attempt: 1, exit_code: 143, failure_classes: ["cancelled"] }],
    });
  });

  it("does not count attempts prevented by the soft deadline", async () => {
    const options = telemetryOptions();
    options.softTimeoutGuard = { timeoutMinutes: 1, softDeadlineMs: Date.now() - 1 };
    await runHarnessRetryLoop(options);
    expect(JSON.parse(fs.readFileSync(options.telemetryPath, "utf8"))).toEqual({ attempts: 0, exit_code: 1, attempt_results: [] });
  });

  it("checkpoints started attempts even if execution throws", async () => {
    const options = telemetryOptions();
    options.runAttempt = async () => {
      expect(JSON.parse(fs.readFileSync(options.telemetryPath, "utf8")).attempts).toBe(1);
      throw new Error("spawn failed");
    };
    await expect(runHarnessRetryLoop(options)).rejects.toThrow("spawn failed");
    expect(JSON.parse(fs.readFileSync(options.telemetryPath, "utf8"))).toEqual({
      attempts: 1,
      exit_code: 1,
      attempt_results: [{ attempt: 1, exit_code: null, failure_classes: [] }],
    });
  });

  it("does not change the harness outcome when telemetry cannot be written", async () => {
    const options = telemetryOptions();
    fs.mkdirSync(options.telemetryPath);
    const logs = [];
    options.log = message => logs.push(message);
    await expect(runHarnessRetryLoop(options)).resolves.toMatchObject({ exitCode: 0, attempts: 1 });
    expect(logs.some(message => message.includes("could not record harness retry telemetry"))).toBe(true);
  });

  it("does not propagate telemetry diagnostic failures", async () => {
    const options = telemetryOptions();
    fs.mkdirSync(options.telemetryPath);
    options.log = message => {
      if (message.includes("could not record harness retry telemetry")) throw new Error("logging failed");
    };
    await expect(runHarnessRetryLoop(options)).resolves.toMatchObject({ exitCode: 0, attempts: 1 });
  });

  it.each(["agent", "detection", "evals"])("records run-scoped zero-use evidence for a pre-flight %s noop", component => {
    const env = executionEnv({ GH_AW_AWF_EXECUTION_COMPONENT: component });
    fs.writeFileSync(env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE, '{"state":"started"}');
    expect(shouldSkipForNoopSafeOutputs({ safeOutputsPath: "/tmp/safe-outputs.jsonl", hasNoopInSafeOutputs: () => true, log: () => {}, executionEvidence: captureExecutionEvidenceConfiguration(env) })).toBe(true);
    expect(JSON.parse(fs.readFileSync(env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE, "utf8"))).toEqual({
      version: 1,
      component,
      run_id: 123,
      run_attempt: 2,
      state: "not_started",
    });
    expect(fs.existsSync(`${env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE}.tmp`)).toBe(false);
  });

  it("does not use a fixed component allow-list", () => {
    const env = executionEnv({ GH_AW_AWF_EXECUTION_COMPONENT: "future_component" });
    expect(shouldSkipForNoopSafeOutputs({ safeOutputsPath: "/tmp/safe-outputs.jsonl", hasNoopInSafeOutputs: () => true, log: () => {}, executionEvidence: captureExecutionEvidenceConfiguration(env) })).toBe(true);
    expect(JSON.parse(fs.readFileSync(env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE, "utf8"))).toMatchObject({ component: "future_component", state: "not_started" });
  });

  it.each([
    { GH_AW_AWF_EXECUTION_EVIDENCE_FILE: "relative.json" },
    { GITHUB_RUN_ID: undefined },
    { GITHUB_RUN_ID: "invalid" },
    { GITHUB_RUN_ID: "9007199254740992" },
    { GITHUB_RUN_ATTEMPT: "0" },
    { GITHUB_RUN_ATTEMPT: "1.5" },
    { GH_AW_AWF_EXECUTION_COMPONENT: "invalid/component" },
  ])("does not manufacture zero-use evidence with invalid metadata %j", overrides => {
    const env = executionEnv(overrides);
    expect(shouldSkipForNoopSafeOutputs({ safeOutputsPath: "/tmp/safe-outputs.jsonl", hasNoopInSafeOutputs: () => true, log: () => {}, executionEvidence: captureExecutionEvidenceConfiguration(env) })).toBe(true);
    expect(fs.existsSync(env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE)).toBe(false);
  });

  it("preserves started evidence when no pre-flight noop exists or a noop is written after an attempt", () => {
    const env = executionEnv();
    const started = '{"state":"started"}';
    fs.writeFileSync(env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE, started);
    expect(shouldSkipForNoopSafeOutputs({ safeOutputsPath: "/tmp/safe-outputs.jsonl", hasNoopInSafeOutputs: () => false, log: () => {}, executionEvidence: captureExecutionEvidenceConfiguration(env) })).toBe(false);
    expect(shouldStopForNoopSafeOutputs({ attempt: 0, safeOutputsPath: "/tmp/safe-outputs.jsonl", hasNoopInSafeOutputs: () => true, log: () => {} })).toBe(true);
    expect(fs.readFileSync(env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE, "utf8")).toBe(started);
  });

  it("keeps the noop exit when evidence cannot be written without replacing started evidence", () => {
    const env = executionEnv();
    const started = '{"state":"started"}';
    fs.writeFileSync(env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE, started);
    fs.mkdirSync(`${env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE}.tmp`);
    const logs = [];
    expect(shouldSkipForNoopSafeOutputs({ safeOutputsPath: "/tmp/safe-outputs.jsonl", hasNoopInSafeOutputs: () => true, log: message => logs.push(message), executionEvidence: captureExecutionEvidenceConfiguration(env) })).toBe(true);
    expect(fs.readFileSync(env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE, "utf8")).toBe(started);
    expect(logs.some(message => message.includes("could not record zero-use execution evidence"))).toBe(true);
  });

  it("records pre-flight noop evidence on the host runner", () => {
    const env = executionEnv();
    const safeOutputsPath = path.join(directory, "safe-outputs.jsonl");
    fs.writeFileSync(safeOutputsPath, '{"type":"noop","message":"already done"}\n');
    const result = spawnSync(process.execPath, [require.resolve("./harness_retry_runner.cjs"), "--preflight-noop"], {
      encoding: "utf8",
      env: { ...process.env, ...env, GH_AW_SAFE_OUTPUTS: safeOutputsPath },
    });

    expect(result.status).toBe(0);
    expect(JSON.parse(fs.readFileSync(env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE, "utf8"))).toMatchObject({
      component: "agent",
      run_id: 123,
      run_attempt: 2,
      state: "not_started",
    });
  });

  it("strips evidence configuration from engine child environments", () => {
    const env = executionEnv();
    const childEnv = stripExecutionEvidenceEnv({ ...env });

    expect(childEnv).not.toHaveProperty("GH_AW_AWF_EXECUTION_EVIDENCE_FILE");
    expect(childEnv).not.toHaveProperty("GH_AW_AWF_EXECUTION_COMPONENT");
    expect(env).toHaveProperty("GH_AW_AWF_EXECUTION_EVIDENCE_FILE");
    expect(env).toHaveProperty("GH_AW_AWF_EXECUTION_COMPONENT");
  });

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
