// @ts-check

"use strict";

const { emitInfrastructureIncomplete } = require("./safeoutputs_cli.cjs");
const { fetchAWFReflect } = require("./awf_reflect.cjs");

// Stop retrying this long before the step hard timeout so the harness can emit
// structured safe-output diagnostics instead of being terminated by Actions.
const SOFT_TIMEOUT_BUFFER_MS = 90 * 1000;

const errorPatterns = require("./harness_error_patterns.cjs");

/**
 * Validate a fresh AWF reflection before treating a native terminal 429 as an
 * exhausted shared invocation budget. Missing or malformed evidence preserves
 * the engine's existing retry policy.
 * @param {{
 *   nativeTerminalRateLimitError: boolean,
 *   env?: NodeJS.ProcessEnv,
 *   fetchReflect?: typeof fetchAWFReflect,
 *   logger?: (message: string) => void,
 * }} options
 * @returns {Promise<{ invocationCount: number, maxRuns: number } | null>}
 */
async function detectExhaustedAWFInvocationBudget({ nativeTerminalRateLimitError, env = process.env, fetchReflect = fetchAWFReflect, logger = () => {} }) {
  if (!nativeTerminalRateLimitError || env.AWF_REFLECT_ENABLED !== "1") return null;
  try {
    const refreshed = await fetchReflect({ logger, timeoutMs: 3000, modelsTimeoutMs: 1000 });
    if (!refreshed.ok) return null;
    const runs = refreshed.reflectData?.runs;
    if (!runs || typeof runs !== "object" || Array.isArray(runs) || runs.enabled !== true) return null;
    const invocationCount = runs.invocation_count;
    const maxRuns = runs.max_runs;
    if (typeof maxRuns !== "number" || !Number.isSafeInteger(maxRuns) || maxRuns <= 0 || typeof invocationCount !== "number" || !Number.isSafeInteger(invocationCount) || invocationCount < maxRuns || runs.remaining_runs !== 0) return null;
    return { invocationCount, maxRuns };
  } catch {
    return null;
  }
}

/**
 * Add fresh budget evidence to an engine's already-classified guard, without changing trust policy.
 * Engines that classify afterwards derive their failure class from the updated guard.
 * @param {{
 *   guard: { maxRunsExceeded: boolean }, attempt: number,
 *   nativeTerminalRateLimitError: boolean, log: (message: string) => void,
 *   result?: import("./harness_retry_runner.cjs").HarnessAttemptResult,
 *   env?: NodeJS.ProcessEnv, fetchReflect?: typeof fetchAWFReflect,
 * }} options
 */
async function observeInvocationBudget({ guard, attempt, nativeTerminalRateLimitError, log, result, env, fetchReflect }) {
  if (guard.maxRunsExceeded) return;
  const budget = await detectExhaustedAWFInvocationBudget({ nativeTerminalRateLimitError, logger: log, env, fetchReflect });
  if (!budget) return;
  guard.maxRunsExceeded = true;
  if (result) result.failureClasses = [...new Set([...(result.failureClasses || []), "invocation_cap_exceeded"])];
  log(`attempt ${attempt + 1}: AWF shared invocation budget exhausted (${budget.invocationCount}/${budget.maxRuns}) — confirmed by fresh reflection`);
}

/**
 * Enforce provider waits once, after engine-local retry vetoes have been applied.
 * @param {{
 *   attempt: number, result: import("./harness_retry_runner.cjs").HarnessAttemptResult,
 *   maxDelayMs: number, softTimeoutGuard: { softDeadlineMs: number } | null,
 *   harnessName: string, log: (message: string) => void,
 * }} options
 * @returns {import("./harness_retry_runner.cjs").HarnessFailureDecision | null}
 */
function resolveRetryAfterDecision({ attempt, result, maxDelayMs, softTimeoutGuard, harnessName, log }) {
  const wait = result.retryAfterMs;
  if (wait == null) return null;
  const invalidWait = !Number.isSafeInteger(wait) || wait < 0;
  const exceedsDelayCap = wait > Math.min(maxDelayMs, 2147483647);
  const exceedsTimeout = softTimeoutGuard && Date.now() + wait >= softTimeoutGuard.softDeadlineMs;
  if (invalidWait || exceedsDelayCap || exceedsTimeout) {
    const reason = invalidWait ? "a valid nonnegative timer delay" : exceedsDelayCap ? `max-delay-ms=${maxDelayMs}` : "remaining job timeout budget";
    result.failureClasses = ["rate_limit_wait_exceeded"];
    log(`attempt ${attempt + 1}: failureClass=rate_limit_wait_exceeded — HTTP 429 Retry-After=${wait}ms exceeds ${reason}; not retrying`);
    emitInfrastructureIncomplete(`${harnessName} HTTP 429 Retry-After wait exceeds ${reason}. Stopping rather than retrying inside the rate-limit window.`, { logger: log });
    return { action: "stop" };
  }
  result.failureClasses = ["rate_limit_error"];
  log(`attempt ${attempt + 1}: failureClass=rate_limit_error — HTTP 429 Retry-After requires ${wait}ms before next attempt`);
  return { action: "retry", nextDelayMs: wait };
}

/**
 * Compute a soft timeout deadline for the harness based on GH_AW_TIMEOUT_MINUTES.
 * Returns null when timeout is unset/invalid.
 * @param {number} driverStartTime
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {{ timeoutMinutes: number, softDeadlineMs: number } | null}
 */
function buildSoftTimeoutGuard(driverStartTime, env = process.env) {
  const timeoutMinutes = Number(env.GH_AW_TIMEOUT_MINUTES);
  if (!Number.isFinite(timeoutMinutes) || timeoutMinutes <= 0) {
    return null;
  }
  const hardTimeoutMs = Math.floor(timeoutMinutes * 60 * 1000);
  const softDeadlineMs = driverStartTime + Math.max(hardTimeoutMs - SOFT_TIMEOUT_BUFFER_MS, 1000);
  return { timeoutMinutes, softDeadlineMs };
}

/**
 * Emit infrastructure incomplete signal and log when the soft timeout guard fires.
 * @param {{ timeoutMinutes: number, softDeadlineMs: number }} guard
 * @param {string} context - Short label for where the check fired (e.g. "before attempt 2")
 * @param {string} harnessName - Human-readable name of the harness (e.g. "Copilot harness")
 * @param {(message: string) => void} logFn - Harness-specific log function
 */
function emitSoftTimeoutSignal(guard, context, harnessName, logFn) {
  emitInfrastructureIncomplete(`${harnessName} reached soft retry budget before the ${guard.timeoutMinutes}-minute step timeout. ` + "Stopping retries early to preserve structured failure output.");
  logFn(`soft-timeout guard reached ${context}: timeoutMinutes=${guard.timeoutMinutes} bufferMs=${SOFT_TIMEOUT_BUFFER_MS}`);
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    ...errorPatterns,
    SOFT_TIMEOUT_BUFFER_MS,
    buildSoftTimeoutGuard,
    detectExhaustedAWFInvocationBudget,
    observeInvocationBudget,
    resolveRetryAfterDecision,
    emitSoftTimeoutSignal,
  };
}
