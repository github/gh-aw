// @ts-check

"use strict";

const { emitInfrastructureIncomplete } = require("./safeoutputs_cli.cjs");

// Stop retrying this long before the step hard timeout so the harness can emit
// structured safe-output diagnostics instead of being terminated by Actions.
const SOFT_TIMEOUT_BUFFER_MS = 90 * 1000;

const errorPatterns = require("./harness_error_patterns.cjs");

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
    emitSoftTimeoutSignal,
  };
}
