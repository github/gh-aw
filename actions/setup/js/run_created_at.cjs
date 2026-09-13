// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Workflow-run creation time acquisition.
 *
 * The operational-value grader can only bind a run opportunity when it receives an
 * authoritative run creation time, so both the activation job (which publishes the
 * `run_created_at` output) and the graders step (which falls back to Actions run
 * metadata) share the normalization and lookup logic in this module.
 */

const { withRetry, isTransientError } = require("./error_recovery.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { RUN_CREATED_AT_MAX_RETRIES, RUN_CREATED_AT_INITIAL_DELAY_MS, RUN_CREATED_AT_MAX_DELAY_MS } = require("./constants.cjs");

/**
 * Retry configuration for the workflow-run creation time lookup. Only transient
 * failures are retried: a permanent error such as a missing `actions: read`
 * permission fails immediately instead of consuming the retry budget.
 * @type {import('./error_recovery.cjs').RetryConfig}
 */
const RUN_CREATED_AT_RETRY_CONFIG = {
  maxRetries: RUN_CREATED_AT_MAX_RETRIES,
  initialDelayMs: RUN_CREATED_AT_INITIAL_DELAY_MS,
  maxDelayMs: RUN_CREATED_AT_MAX_DELAY_MS,
  backoffMultiplier: 2,
  jitterMs: 0,
  shouldRetry: error => {
    const status = Number(error?.status ?? error?.response?.status);
    return (Number.isInteger(status) && status >= 500) || isTransientError(error);
  },
};

// ISO-8601 date-time with a UTC designator or a numeric offset. Date.parse accepts
// looser formats (for example "2026" or "2026-09-12 20:58:00"), which must not be
// promoted to a valid run creation time.
const ISO_8601_DATE_TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/;

/**
 * Normalize a workflow-run creation time into the strict UTC ISO-8601 form
 * (`YYYY-MM-DDTHH:MM:SSZ`) expected by the operational-value grader contract.
 *
 * GitHub returns `created_at` as an ISO timestamp, but callers may also receive
 * an empty string (unresolved workflow output) or an offset-bearing timestamp.
 * Values that are not ISO-8601 date-times are reported as an empty string so
 * callers can treat the timestamp as unavailable rather than propagating an
 * invalid value. Sub-second precision is truncated (not rounded) because the
 * grader contract records run creation times at second granularity.
 *
 * @param {unknown} value
 * @returns {string} normalized timestamp, or "" when the value is unusable
 */
function normalizeRunCreatedAt(value) {
  if (typeof value !== "string") return "";
  const trimmed = value.trim();
  if (!ISO_8601_DATE_TIME.test(trimmed)) return "";
  const parsed = Date.parse(trimmed);
  if (!Number.isFinite(parsed)) return "";
  return new Date(Math.trunc(parsed / 1000) * 1000).toISOString().replace(/\.\d{3}Z$/, "Z");
}

/**
 * Read the workflow-run creation time from Actions run metadata, retrying transient failures.
 *
 * @param {any} github - Authenticated GitHub client
 * @param {{owner: string, repo: string, runId: number}} target
 * @returns {Promise<{createdAt: string, error?: string}>}
 */
async function fetchRunCreatedAt(github, target) {
  try {
    const response = await withRetry(() => github.rest.actions.getWorkflowRun({ owner: target.owner, repo: target.repo, run_id: target.runId }), RUN_CREATED_AT_RETRY_CONFIG, "workflow run creation time lookup");
    const createdAt = normalizeRunCreatedAt(response?.data?.created_at);
    if (!createdAt) {
      return { createdAt: "", error: `workflow run creation time is not a valid timestamp: ${JSON.stringify(response?.data?.created_at)}` };
    }
    return { createdAt };
  } catch (err) {
    return { createdAt: "", error: getErrorMessage(err) };
  }
}

/**
 * Resolve the workflow-run creation time for the activation job's `run_created_at` output.
 * Reports the acquisition failure as a warning and an empty value so downstream steps can
 * distinguish a missing timestamp from an invalid one.
 *
 * @param {typeof import('@actions/core')} core - GitHub Actions core library
 * @param {any} ctx - GitHub Actions context object
 * @param {any} [githubClient] - Authenticated GitHub client; falls back to global.github
 * @returns {Promise<string>} normalized timestamp, or "" when it cannot be resolved
 */
async function resolveRunCreatedAtForInfo(core, ctx, githubClient) {
  // @ts-ignore - global.github is set by setupGlobals() from the github-script context
  const github = githubClient || global.github;
  if (typeof github?.rest?.actions?.getWorkflowRun !== "function") {
    core.warning("Unable to load workflow-run creation time: no authenticated GitHub client is available");
    return "";
  }
  const { createdAt, error } = await fetchRunCreatedAt(github, { owner: ctx.repo.owner, repo: ctx.repo.repo, runId: ctx.runId });
  if (!createdAt) {
    core.warning(`Unable to load workflow-run creation time: ${error}`);
  }
  return createdAt;
}

/**
 * Resolve the authoritative workflow-run creation time for operational-value grading.
 *
 * The activation job normally publishes it through GH_AW_RUN_CREATED_AT. When that
 * value is missing or unusable (for example the activation lookup failed), fall back
 * to the Actions run metadata so the grader request is still bound to a run opportunity.
 *
 * @param {NodeJS.ProcessEnv} [env]
 * @param {any} [githubClient] - Authenticated GitHub client; falls back to global.github
 * @returns {Promise<{createdAt: string, error?: string}>}
 */
async function resolveRunCreatedAtForGrading(env = process.env, githubClient = undefined) {
  const fromEnv = normalizeRunCreatedAt(env.GH_AW_RUN_CREATED_AT);
  if (fromEnv) return { createdAt: fromEnv };

  // @ts-ignore - global.github is set by setupGlobals() from the github-script context
  const github = githubClient || global.github;
  const repository = String(env.GITHUB_REPOSITORY || "");
  const [owner, repo] = repository.split("/");
  const runId = String(env.GITHUB_RUN_ID || "");
  const missing = [];
  if (typeof github?.rest?.actions?.getWorkflowRun !== "function") missing.push("an authenticated GitHub client");
  if (!owner || !repo) missing.push("GITHUB_REPOSITORY");
  if (!/^\d+$/.test(runId)) missing.push("GITHUB_RUN_ID");
  if (missing.length > 0) {
    return { createdAt: "", error: `workflow run creation time is unavailable and cannot be read from Actions run metadata (missing ${missing.join(", ")})` };
  }
  const { createdAt, error } = await fetchRunCreatedAt(github, { owner, repo, runId: Number(runId) });
  if (!createdAt) {
    return { createdAt: "", error: `failed to read workflow run creation time from Actions run metadata: ${error}` };
  }
  return { createdAt };
}

module.exports = { normalizeRunCreatedAt, fetchRunCreatedAt, resolveRunCreatedAtForInfo, resolveRunCreatedAtForGrading, RUN_CREATED_AT_RETRY_CONFIG };
