// @ts-check

"use strict";

const { formatDuration, sleep } = require("./process_runner.cjs");
const { emitSoftTimeoutSignal } = require("./harness_retry_guard.cjs");

/**
 * `nextDelayMs` overrides the delay before the immediately next attempt; following retries
 * resume exponential backoff from that delay.
 * @typedef {{ exitCode: number, output: string, hasOutput: boolean, durationMs?: number, watchdogFired?: boolean, runtimeGuardFired?: boolean, runtimeGuardReason?: string, safeOutputsByteOffset?: number, cancelled?: boolean }} HarnessAttemptResult
 * @typedef {{ action: "retry" | "stop", exitCode?: number, nextDelayMs?: number }} HarnessFailureDecision
 */

/**
 * Return true when the harness should skip spawning an agent because a prior step
 * already wrote a noop safe-output.
 * @param {{ safeOutputsPath: string, hasNoopInSafeOutputs: (path: string, options: { logger: (message: string) => void }) => boolean, log: (message: string) => void }} options
 * @returns {boolean}
 */
function shouldSkipForNoopSafeOutputs({ safeOutputsPath, hasNoopInSafeOutputs, log }) {
  if (!safeOutputsPath || !hasNoopInSafeOutputs(safeOutputsPath, { logger: log })) {
    return false;
  }
  log("pre-flight: noop message found in safe-outputs — skipping agent (work is already complete or no work needed)");
  return true;
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
    const result = await options.runAttempt(attempt);
    lastResult = result;
    lastExitCode = result.exitCode;

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
    if (typeof decision.exitCode === "number") {
      lastExitCode = decision.exitCode;
    }
    if (typeof decision.nextDelayMs === "number") {
      delay = decision.nextDelayMs;
    }
    if (decision.action === "retry") {
      continue;
    }
    break;
  }

  return { exitCode: lastExitCode, attempts, lastResult };
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    runHarnessRetryLoop,
    shouldSkipForNoopSafeOutputs,
    shouldStopForNoopSafeOutputs,
  };
}
