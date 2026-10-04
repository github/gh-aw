// @ts-check
/// <reference types="@actions/github-script" />

const { fetchAndLogRateLimit, logRateLimitFromResponse } = require("./github_rate_limit_logger.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Minimum rate limit remaining before we skip further operations.
 * This reserves capacity for other workflow jobs and API consumers.
 */
const MIN_RATE_LIMIT_REMAINING = 100;

/**
 * Percentage of remaining quota below which a warning is emitted.
 * E.g. 20 means a warning is logged when < 20 % of the total quota remains.
 */
const LOW_RATE_LIMIT_THRESHOLD_PERCENT = 20;

/**
 * Check the current rate limit and determine if we should continue.
 * Returns the remaining requests count, or -1 if we couldn't check.
 * Also logs the rate limit snapshot for observability.
 *
 * @param {any} github - GitHub REST client
 * @param {string} [operation="rate_limit_check"] - Label for the log entry
 * @returns {Promise<number>} Remaining requests, or -1 on error
 */
async function getRateLimitRemaining(github, operation = "rate_limit_check") {
  try {
    await fetchAndLogRateLimit(github, operation);
    const { data } = await github.rest.rateLimit.get();
    return (data.rate ?? data.resources?.core).remaining;
  } catch {
    return -1;
  }
}

/**
 * Check if the current rate limit is sufficient for operations.
 * Logs a warning and returns false if the rate limit is too low.
 *
 * @param {any} github - GitHub REST client
 * @param {string} [operation="rate_limit_check"] - Label for the log entry
 * @returns {Promise<{ok: boolean, remaining: number}>}
 */
async function checkRateLimit(github, operation = "rate_limit_check") {
  const remaining = await getRateLimitRemaining(github, operation);
  if (remaining !== -1 && remaining < MIN_RATE_LIMIT_REMAINING) {
    return { ok: false, remaining };
  }
  return { ok: true, remaining };
}

/**
 * Check rate-limit headroom and emit a warning when the remaining quota
 * falls below {@link LOW_RATE_LIMIT_THRESHOLD_PERCENT} percent of the total.
 *
 * This is a lightweight observability call – it does not block execution.
 *
 * @param {any} github - GitHub REST client
 * @param {string} [operation="rate_limit_headroom"] - Label used in log messages
 * @returns {Promise<{remaining: number, limit: number, percentRemaining: number}>}
 */
async function checkRateLimitHeadroom(github, operation = "rate_limit_headroom") {
  try {
    const response = await github.rest.rateLimit.get();
    const { data } = response;
    logRateLimitFromResponse(response, operation);
    const quota = data.rate ?? data.resources?.core;
    if (!quota) throw new Error("GitHub rate-limit response has no core quota");
    const { remaining, limit } = quota;
    const percentRemaining = limit > 0 ? Math.floor((remaining / limit) * 100) : 100;

    if (percentRemaining < LOW_RATE_LIMIT_THRESHOLD_PERCENT) {
      core.warning(`⚠️ Rate-limit headroom low: ${remaining}/${limit} requests remaining (${percentRemaining}%) [${operation}]. Safe-output writes may be rate-limited.`);
    } else {
      core.info(`ℹ️ Rate-limit headroom: ${remaining}/${limit} requests remaining (${percentRemaining}%) [${operation}]`);
    }

    return { remaining, limit, percentRemaining };
  } catch (err) {
    core.warning(`Could not check rate-limit headroom for ${operation}: ${getErrorMessage(err)}`);
    return { remaining: -1, limit: -1, percentRemaining: -1 };
  }
}

module.exports = {
  MIN_RATE_LIMIT_REMAINING,
  LOW_RATE_LIMIT_THRESHOLD_PERCENT,
  getRateLimitRemaining,
  checkRateLimit,
  checkRateLimitHeadroom,
};
