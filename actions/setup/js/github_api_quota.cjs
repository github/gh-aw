// @ts-check
/// <reference types="@actions/github-script" />

const { getGitHubAPIQuota, getGitHubAPIDiagnostic } = require("./github_api_diagnostics.cjs");

const GITHUB_API_QUOTA_EXHAUSTED = "GitHub API quota exhausted";
const GITHUB_API_QUOTA_EXHAUSTED_CODE = "GH_AW_GITHUB_API_QUOTA_EXHAUSTED";
const DEFAULT_GITHUB_API_QUOTA_MAX_WAIT_MS = 10 * 60 * 1000;

/**
 * @typedef {{status: number | null, resource: string, limit: number | null, remaining: number | null, used: number | null, reset: string, retryNotBefore: string}} GitHubAPIQuotaExhaustion
 */

/**
 * Identify a genuine primary-quota exhaustion or explicit server retry
 * restriction from the failed business response. Authentication, permission,
 * reserve-guard, and other failures are never reclassified as quota exhaustion.
 * @param {any} error
 * @param {number} [now]
 * @returns {GitHubAPIQuotaExhaustion | null}
 */
function getGitHubAPIQuotaExhaustion(error, now = Date.now()) {
  if (error?.code === GITHUB_API_QUOTA_EXHAUSTED_CODE && error.quota) return error.quota;
  const diagnostic = getGitHubAPIDiagnostic(error, now);
  if (!diagnostic?.quota || ["authentication_failed", "permission_denied"].includes(diagnostic.classification)) return null;
  let failure = error;
  for (let depth = 0; depth < 5 && failure && typeof failure === "object"; depth++, failure = failure.cause) {
    if (failure.code === "DAILY_AIC_QUOTA_STOP" || [401, 403, 429].includes(Number(failure.status || failure.response?.status || 0))) break;
  }
  if (!failure || typeof failure !== "object") return null;
  const status = Number(failure.status || failure.response?.status || 0);
  const metadata = getGitHubAPIQuota(failure.response?.headers, now);
  // A reserve-guard stop or a rate limit without server metadata is not
  // established exhaustion; callers keep their existing handling for those.
  if (metadata.remaining !== 0 && !metadata.retryRestricted) return null;
  return {
    status: Number.isInteger(status) && status >= 100 && status <= 599 ? status : null,
    resource: metadata.resource,
    limit: metadata.limit,
    remaining: metadata.remaining,
    used: metadata.used,
    reset: metadata.reset,
    retryNotBefore: metadata.retryNotBefore,
  };
}

/**
 * @param {GitHubAPIQuotaExhaustion} quota
 * @returns {string}
 */
function formatGitHubAPIQuotaDetails(quota) {
  return `resource=${quota.resource}; limit=${quota.limit ?? "unknown"}; remaining=${quota.remaining ?? "unknown"}; used=${quota.used ?? "unknown"}; reset=${quota.reset || "unknown"}; retry_not_before=${quota.retryNotBefore || "unknown"}`;
}

class GitHubAPIQuotaExhaustedError extends Error {
  /**
   * @param {string} operation
   * @param {GitHubAPIQuotaExhaustion} quota
   * @param {string} reason
   */
  constructor(operation, quota, reason) {
    super(`${GITHUB_API_QUOTA_EXHAUSTED} during ${operation} (HTTP ${quota.status ?? "unknown"}; ${formatGitHubAPIQuotaDetails(quota)}; reason=${reason})`);
    this.name = "GitHubAPIQuotaExhaustedError";
    this.code = GITHUB_API_QUOTA_EXHAUSTED_CODE;
    this.operation = operation;
    this.quota = quota;
    this.reason = reason;
  }
}

/**
 * Render a step-summary section with safe quota metadata only.
 * @param {{stage: string, quota: GitHubAPIQuotaExhaustion, reason?: string, impact: string}} options
 * @returns {string}
 */
function renderGitHubAPIQuotaSummary({ stage, quota, reason, impact }) {
  const rows = [
    ["Stage", stage],
    ["HTTP status", String(quota.status ?? "unknown")],
    ["Resource", quota.resource],
    ["Limit", String(quota.limit ?? "unknown")],
    ["Remaining", String(quota.remaining ?? "unknown")],
    ["Used", String(quota.used ?? "unknown")],
    ["Reset", quota.reset || "unknown"],
    ["Retry not before", quota.retryNotBefore || "unknown"],
    ...(reason ? [["Reason", reason]] : []),
  ];
  return [`### ${GITHUB_API_QUOTA_EXHAUSTED}`, "", "| Field | Value |", "| --- | --- |", ...rows.map(([name, value]) => `| ${name} | ${value} |`), "", impact, ""].join("\n");
}

/**
 * @param {{stage: string, quota: GitHubAPIQuotaExhaustion, reason?: string, impact: string}} options
 * @returns {Promise<void>}
 */
async function writeGitHubAPIQuotaSummary(options) {
  if (!core.summary) return;
  try {
    await core.summary.addRaw(renderGitHubAPIQuotaSummary(options)).write();
  } catch {
    core.warning(`Failed to write ${GITHUB_API_QUOTA_EXHAUSTED} summary.`);
  }
}

/**
 * Run one business read. On genuine quota exhaustion, wait until the failed
 * response's own reset/Retry-After deadline (never a shorter retry cap) when it
 * fits the bounded budget, then retry once. Otherwise throw a typed quota error.
 * @template T
 * @param {() => Promise<T>} operation
 * @param {{label: string, now?: () => number, sleep?: (ms: number) => Promise<void>, maxWaitMs?: number}} options
 * @returns {Promise<T>}
 */
async function withGitHubAPIQuotaWait(operation, { label, now = Date.now, sleep = ms => new Promise(resolve => setTimeout(resolve, ms)), maxWaitMs = DEFAULT_GITHUB_API_QUOTA_MAX_WAIT_MS }) {
  try {
    return await operation();
  } catch (error) {
    const quota = getGitHubAPIQuotaExhaustion(error, now());
    if (!quota) throw error;
    const retryAtMs = Date.parse(quota.retryNotBefore);
    const delayMs = retryAtMs + 1000 - now();
    const reason = !Number.isFinite(retryAtMs) || delayMs <= 0 ? "unknown_reset" : delayMs > maxWaitMs ? "reset_exceeds_budget" : "";
    if (reason) {
      core.info(`[github-api-quota] ${label}: ${GITHUB_API_QUOTA_EXHAUSTED}; not waiting (${JSON.stringify({ reason, retryNotBefore: quota.retryNotBefore || null, maxWaitMs })})`);
      throw new GitHubAPIQuotaExhaustedError(label, quota, reason);
    }
    core.info(`[github-api-quota] ${label}: ${GITHUB_API_QUOTA_EXHAUSTED}; waiting for the server reset before one retry (${JSON.stringify({ retryNotBefore: quota.retryNotBefore, delayMs, maxWaitMs })})`);
    await sleep(delayMs);
    if (!Number.isFinite(retryAtMs) || now() < retryAtMs) throw new GitHubAPIQuotaExhaustedError(label, quota, "wait_ended_before_reset");
    try {
      return await operation();
    } catch (retryError) {
      const renewed = getGitHubAPIQuotaExhaustion(retryError, now());
      if (renewed) throw new GitHubAPIQuotaExhaustedError(label, renewed, "renewed_exhaustion");
      throw retryError;
    }
  }
}

module.exports = {
  GITHUB_API_QUOTA_EXHAUSTED,
  GITHUB_API_QUOTA_EXHAUSTED_CODE,
  DEFAULT_GITHUB_API_QUOTA_MAX_WAIT_MS,
  GitHubAPIQuotaExhaustedError,
  getGitHubAPIQuotaExhaustion,
  formatGitHubAPIQuotaDetails,
  renderGitHubAPIQuotaSummary,
  writeGitHubAPIQuotaSummary,
  withGitHubAPIQuotaWait,
};
