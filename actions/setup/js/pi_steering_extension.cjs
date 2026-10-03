// @ts-check

/**
 * Pi Steering Extension for gh-aw
 *
 * Monitors elapsed time and injects steering messages into a Pi agent session
 * when remaining time falls below configured thresholds.
 *
 * This extension is automatically added to every Pi agent invocation by the
 * gh-aw compiler. No workflow frontmatter configuration is required.
 *
 * Configuration (read from environment variables):
 *   GH_AW_TIMEOUT_MINUTES               Total allowed runtime in minutes (default: 30)
 *   GH_AW_STEERING_TIME_WARNING_MINUTES  Minutes-remaining threshold for warning message (default: 5)
 *   GH_AW_STEERING_TIME_CRITICAL_MINUTES Minutes-remaining threshold for critical message (default: 2)
 */

"use strict";

/** Default total session timeout in minutes. */
const DEFAULT_TIMEOUT_MINUTES = 30;

/** Default minutes-remaining threshold for the warning steering message. */
const DEFAULT_TIME_WARNING_MINUTES = 5;

/** Default minutes-remaining threshold for the critical steering message. */
const DEFAULT_TIME_CRITICAL_MINUTES = 2;

/**
 * Loads steering configuration from environment variables.
 * @returns {{ timeoutMinutes: number, timeWarningMinutes: number, timeCriticalMinutes: number }}
 */
function loadSteeringConfig() {
  const timeoutMinutes = parseFloat(process.env.GH_AW_TIMEOUT_MINUTES || "") || DEFAULT_TIMEOUT_MINUTES;
  const timeWarningMinutes = parseFloat(process.env.GH_AW_STEERING_TIME_WARNING_MINUTES || "") || DEFAULT_TIME_WARNING_MINUTES;
  const timeCriticalMinutes = parseFloat(process.env.GH_AW_STEERING_TIME_CRITICAL_MINUTES || "") || DEFAULT_TIME_CRITICAL_MINUTES;
  return { timeoutMinutes, timeWarningMinutes, timeCriticalMinutes };
}

/**
 * Pi steering extension for gh-aw.
 *
 * Subscribes to `agent_start` and `turn_end` Pi SDK events and injects time-pressure
 * steering messages when the remaining session time falls below configured thresholds.
 * Each threshold fires at most once per session to avoid message flooding.
 *
 * @param {any} pi - Pi ExtensionAPI instance
 * @returns {void}
 */
function piSteeringExtension(pi) {
  const config = loadSteeringConfig();

  /** @type {number | undefined} */
  let startTime;
  let warningInjected = false;
  let criticalInjected = false;

  pi.on("agent_start", async () => {
    startTime ??= Date.now();
    process.stderr.write(`[gh-aw/steering] Session started. timeout=${config.timeoutMinutes}min, warn<${config.timeWarningMinutes}min, critical<${config.timeCriticalMinutes}min\n`);
  });

  pi.on("turn_end", async (/** @type {any} */ _event, /** @type {any} */ ctx) => {
    if (startTime === undefined) {
      return;
    }

    const elapsedMinutes = (Date.now() - startTime) / 60000;
    const remainingMinutes = config.timeoutMinutes - elapsedMinutes;

    if (remainingMinutes <= config.timeCriticalMinutes && !criticalInjected) {
      // Mark warning as injected too — critical supersedes it.
      warningInjected = true;
      criticalInjected = true;
      process.stderr.write(`[gh-aw/steering] CRITICAL: ${remainingMinutes.toFixed(1)}min remaining — injecting critical message\n`);
      pi.sendUserMessage(`⚠️ CRITICAL: Only ${remainingMinutes.toFixed(0)} minute(s) remaining before the workflow times out. Stop all new research and produce your final output immediately.`, { deliverAs: "steer" });
    } else if (remainingMinutes <= config.timeWarningMinutes && !warningInjected) {
      warningInjected = true;
      process.stderr.write(`[gh-aw/steering] WARNING: ${remainingMinutes.toFixed(1)}min remaining — injecting warning message\n`);
      pi.sendUserMessage(`⚠️ ${remainingMinutes.toFixed(0)} minute(s) remaining. Please wrap up your current task and start writing your final output.`, { deliverAs: "steer" });
    }
  });
}

module.exports = piSteeringExtension;
/** @type {any} */
const _steeringExports = module.exports;
_steeringExports.loadSteeringConfig = loadSteeringConfig;
