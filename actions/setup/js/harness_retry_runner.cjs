// @ts-check

"use strict";

const fs = require("fs");
const path = require("path");
const { formatDuration, sleep } = require("./process_runner.cjs");
const { emitSoftTimeoutSignal } = require("./harness_retry_guard.cjs");

const executionEvidenceEnvVars = ["GH_AW_AWF_EXECUTION_EVIDENCE_FILE", "GH_AW_AWF_EXECUTION_COMPONENT"];

/** @param {NodeJS.ProcessEnv} [env] */
function resolveHarnessRetryTelemetryPath(env = process.env) {
  const detection = env.GH_AW_PHASE === "detection";
  const directory = env.GH_AW_HARNESS_TELEMETRY_DIR || path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", detection ? "threat-detection" : "");
  return path.join(directory, detection ? "detection_harness_retry.json" : "harness_retry.json");
}

/**
 * @param {Record<string, string | undefined>} env
 * @returns {{ evidenceFile?: string, component?: string, runId?: string, runAttempt?: string }}
 */
function captureExecutionEvidenceConfiguration(env) {
  return {
    evidenceFile: env.GH_AW_AWF_EXECUTION_EVIDENCE_FILE,
    component: env.GH_AW_AWF_EXECUTION_COMPONENT,
    runId: env.GITHUB_RUN_ID,
    runAttempt: env.GITHUB_RUN_ATTEMPT,
  };
}

/**
 * @template {Record<string, string | undefined>} T
 * @param {T} env
 * @returns {T}
 */
function stripExecutionEvidenceEnv(env) {
  for (const name of executionEvidenceEnvVars) {
    delete env[name];
  }
  return env;
}

/**
 * `nextDelayMs` overrides the delay before the immediately next attempt; following retries
 * resume exponential backoff from that delay.
 * @typedef {{ exitCode: number, output: string, hasOutput: boolean, durationMs?: number, watchdogFired?: boolean, runtimeGuardFired?: boolean, runtimeGuardReason?: string, safeOutputsByteOffset?: number, cancelled?: boolean, failureClasses?: string[] }} HarnessAttemptResult
 * @typedef {{ action: "retry" | "stop", exitCode?: number, nextDelayMs?: number }} HarnessFailureDecision
 */

/**
 * Return true when the harness should skip spawning an agent because a prior step
 * already wrote a noop safe-output.
 * @param {{ safeOutputsPath: string, hasNoopInSafeOutputs: (path: string, options: { logger: (message: string) => void }) => boolean, log: (message: string) => void, executionEvidence?: ReturnType<typeof captureExecutionEvidenceConfiguration> }} options
 * @returns {boolean}
 */
function shouldSkipForNoopSafeOutputs({ safeOutputsPath, hasNoopInSafeOutputs, log, executionEvidence }) {
  if (!safeOutputsPath || !hasNoopInSafeOutputs(safeOutputsPath, { logger: log })) {
    return false;
  }
  log("pre-flight: noop message found in safe-outputs — skipping agent (work is already complete or no work needed)");
  const evidenceFile = executionEvidence?.evidenceFile;
  const component = executionEvidence?.component;
  const runId = executionEvidence?.runId;
  const runAttempt = executionEvidence?.runAttempt;
  if (
    evidenceFile &&
    path.isAbsolute(evidenceFile) &&
    typeof component === "string" &&
    /^[a-z][a-z0-9_-]{0,63}$/.test(component) &&
    /^\d+$/.test(runId || "") &&
    /^\d+$/.test(runAttempt || "") &&
    Number.isSafeInteger(Number(runId)) &&
    Number(runId) > 0 &&
    Number.isSafeInteger(Number(runAttempt)) &&
    Number(runAttempt) > 0
  ) {
    try {
      // Only this pre-flight branch proves no model ran; a noop after an attempt does not.
      fs.mkdirSync(path.dirname(evidenceFile), { recursive: true });
      fs.writeFileSync(`${evidenceFile}.tmp`, JSON.stringify({ version: 1, component, run_id: Number(runId), run_attempt: Number(runAttempt), state: "not_started" }) + "\n");
      fs.renameSync(`${evidenceFile}.tmp`, evidenceFile);
      log(`pre-flight: recorded ${component} execution evidence as not_started`);
    } catch (error) {
      log(`pre-flight: could not record zero-use execution evidence: ${error instanceof Error ? error.message : String(error)}`);
    }
  }
  return true;
}

function runPreflightNoopCheck() {
  const { hasNoopInSafeOutputs } = require("./safeoutputs_cli.cjs");
  return shouldSkipForNoopSafeOutputs({
    safeOutputsPath: process.env.GH_AW_SAFE_OUTPUTS || "",
    hasNoopInSafeOutputs,
    log: message => process.stderr.write(`${message}\n`),
    executionEvidence: captureExecutionEvidenceConfiguration(process.env),
  });
}

if (require.main === module && process.argv[2] === "--preflight-noop") {
  process.exitCode = runPreflightNoopCheck() ? 0 : 1;
}

/**
 * Return true when a failed attempt wrote a noop safe-output, making retries wasteful.
 * @param {{ attempt: number, safeOutputsPath: string, hasNoopInSafeOutputs: (path: string, options: { logger: (message: string) => void }) => boolean, log: (message: string) => void }} options
 * @returns {boolean}
 */
function shouldStopForNoopSafeOutputs({ attempt, safeOutputsPath, hasNoopInSafeOutputs, log }) {
  if (!safeOutputsPath || !hasNoopInSafeOutputs(safeOutputsPath, { logger: log })) {
    return false;
  }
  log(`attempt ${attempt + 1}: noop message found in safe-outputs — not retrying (work is already complete or no work needed)`);
  return true;
}

/**
 * Shared retry loop for engine harnesses. Engine-specific argument selection,
 * execution, and failure classification remain callbacks; this centralizes the
 * attempt/backoff/soft-timeout/success state machine.
 * @param {{
 *   maxRetries: number,
 *   initialDelayMs: number,
 *   backoffMultiplier: number,
 *   maxDelayMs: number,
 *   driverStartTime: number,
 *   harnessName: string,
 *   log: (message: string) => void,
 *   softTimeoutGuard: { timeoutMinutes: number, softDeadlineMs: number } | null,
 *   runAttempt: (attempt: number) => Promise<HarnessAttemptResult>,
 *   handleFailure: (context: { attempt: number, result: HarnessAttemptResult, maxRetries: number }) => Promise<HarnessFailureDecision> | HarnessFailureDecision,
 *   getRetryMode?: (attempt: number) => string,
 *   sleepFn?: (ms: number) => Promise<void>,
 *   telemetryPath?: string,
 * }} options
 * @returns {Promise<{ exitCode: number, attempts: number, lastResult: HarnessAttemptResult | null }>}
 */
async function runHarnessRetryLoop(options) {
  let delay = options.initialDelayMs;
  let lastExitCode = 1;
  let attempts = 0;
  /** @type {HarnessAttemptResult | null} */
  let lastResult = null;
  const sleepFor = options.sleepFn ?? sleep;
  /** @type {{ attempt: number, exit_code: number | null, failure_classes: string[] }[]} */
  const attemptResults = [];
  const writeTelemetry = () => {
    if (!options.telemetryPath) return;
    try {
      fs.mkdirSync(path.dirname(options.telemetryPath), { recursive: true });
      fs.writeFileSync(`${options.telemetryPath}.tmp`, JSON.stringify({ attempts, exit_code: lastExitCode, attempt_results: attemptResults }) + "\n");
      fs.renameSync(`${options.telemetryPath}.tmp`, options.telemetryPath);
    } catch (error) {
      try {
        options.log(`could not record harness retry telemetry: ${error instanceof Error ? error.message : String(error)}`);
      } catch {
        // Telemetry diagnostics must not change the harness outcome.
      }
    }
  };
  writeTelemetry();

  try {
    for (let attempt = 0; attempt <= options.maxRetries; attempt++) {
      if (options.softTimeoutGuard && Date.now() >= options.softTimeoutGuard.softDeadlineMs) {
        emitSoftTimeoutSignal(options.softTimeoutGuard, `before attempt ${attempt + 1}`, options.harnessName, options.log);
        lastExitCode = 1;
        break;
      }

      if (attempt > 0) {
        const retryMode = options.getRetryMode ? options.getRetryMode(attempt) : "fresh run";
        const sleepDelay = options.softTimeoutGuard ? Math.min(delay, Math.max(0, options.softTimeoutGuard.softDeadlineMs - Date.now())) : delay;
        options.log(`retry ${attempt}/${options.maxRetries}: sleeping ${sleepDelay}ms before next attempt (${retryMode})`);
        await sleepFor(sleepDelay);
        delay = Math.min(delay * options.backoffMultiplier, options.maxDelayMs);
        options.log(`retry ${attempt}/${options.maxRetries}: woke up, next delay will be ${delay}ms`);
        if (options.softTimeoutGuard && Date.now() >= options.softTimeoutGuard.softDeadlineMs) {
          emitSoftTimeoutSignal(options.softTimeoutGuard, "after backoff sleep", options.harnessName, options.log);
          lastExitCode = 1;
          break;
        }
      }

      attempts = attempt + 1;
      /** @type {(typeof attemptResults)[number]} */
      const attemptResult = { attempt: attempts, exit_code: null, failure_classes: [] };
      attemptResults.push(attemptResult);
      writeTelemetry();
      const result = await options.runAttempt(attempt);
      lastResult = result;
      lastExitCode = result.exitCode;
      attemptResult.exit_code = result.exitCode;
      if (result.exitCode !== 0) {
        attemptResult.failure_classes = [result.cancelled ? "cancelled" : result.runtimeGuardFired ? "runtime_guard" : result.watchdogFired ? "watchdog" : result.hasOutput ? "unknown_error" : "no_output"];
      }

      if (result.cancelled) {
        options.log(`attempt ${attempt + 1}: cancelled — not retrying`);
        break;
      }

      if (result.exitCode === 0) {
        options.log(`success on attempt ${attempt + 1}: totalDuration=${formatDuration(Date.now() - options.driverStartTime)}`);
        lastExitCode = 0;
        break;
      }

      const decision = await options.handleFailure({ attempt, result, maxRetries: options.maxRetries });
      if (result.failureClasses?.length) {
        attemptResult.failure_classes = result.failureClasses;
      }
      if (typeof decision.exitCode === "number") {
        lastExitCode = decision.exitCode;
      }
      if (typeof decision.nextDelayMs === "number") {
        delay = decision.nextDelayMs;
      }
      writeTelemetry();
      if (decision.action === "retry") {
        continue;
      }
      break;
    }
  } finally {
    writeTelemetry();
  }

  return { exitCode: lastExitCode, attempts, lastResult };
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    captureExecutionEvidenceConfiguration,
    resolveHarnessRetryTelemetryPath,
    runHarnessRetryLoop,
    runPreflightNoopCheck,
    shouldSkipForNoopSafeOutputs,
    shouldStopForNoopSafeOutputs,
    stripExecutionEvidenceEnv,
  };
}
