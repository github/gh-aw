// @ts-check
/// <reference types="@actions/github-script" />

/**
 * github_rate_limit_logger.cjs
 *
 * Helpers for capturing GitHub API rate-limit information to a JSONL file
 * for observability.  Each entry is a single JSON line written to
 * GITHUB_RATE_LIMITS_JSONL_PATH so the file can be included in the job
 * artifact and inspected after a workflow run.
 *
 * Three usage patterns are supported:
 *
 * 1. **After a single REST call** – pass the response object to
 *    `logRateLimitFromResponse(response, operation)` to record the
 *    x-ratelimit-* headers returned by that call.
 *
 * 2. **On-demand snapshot** – call `fetchAndLogRateLimit(github, operation)`
 *    to query the GitHub rate-limit API and record the current limits for
 *    all resource categories.
 *
 * 3. **Automatic wrapping** – call `createRateLimitAwareGithub(github)` to
 *    get a Proxy around the github REST client.  Every `github.rest.*.*(...)`
 *    call will automatically log rate-limit headers from the response without
 *    any further changes to call sites.
 */

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { GITHUB_RATE_LIMITS_JSONL_PATH } = require("./constants.cjs");

const CREDENTIAL_SOURCES = new Set(["github_actions", "pat", "app", "unknown"]);

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

/**
 * @param {unknown} source
 * @returns {string | undefined}
 */
function normalizeCredentialSource(source) {
  return typeof source === "string" && CREDENTIAL_SOURCES.has(source) ? source : undefined;
}

/**
 * Ensure the directory containing the log file exists.
 * @param {string} filePath
 */
function ensureDir(filePath) {
  const dir = path.dirname(filePath);
  if (!fs.existsSync(dir)) {
    try {
      fs.mkdirSync(dir, { recursive: true });
    } catch (err) {
      throw new Error(`Failed to create directory ${dir}: ${getErrorMessage(err)}`, { cause: err });
    }
  }
}

/**
 * Append a single rate-limit entry to the JSONL log file.
 * Errors are non-fatal – a warning is emitted to the Actions log.
 *
 * @param {Record<string, unknown>} entry
 */
function appendEntry(entry) {
  try {
    ensureDir(GITHUB_RATE_LIMITS_JSONL_PATH);
    const line = JSON.stringify(entry) + "\n";
    fs.appendFileSync(GITHUB_RATE_LIMITS_JSONL_PATH, line);
  } catch (err) {
    core.warning(`github_rate_limit_logger: failed to write entry: ${getErrorMessage(err)}`);
  }
}

/**
 * Parse an x-ratelimit-reset Unix timestamp (seconds) into an ISO 8601 string.
 * Returns null when the header is absent or unparseable.
 *
 * @param {string | undefined} resetHeader
 * @returns {string | null}
 */
function parseResetTimestamp(resetHeader) {
  if (!resetHeader) return null;
  const seconds = parseInt(resetHeader, 10);
  if (Number.isNaN(seconds)) return null;
  return new Date(seconds * 1000).toISOString();
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Log GitHub API rate-limit information extracted from a REST response's
 * headers.  Call this immediately after any `github.rest.*.*()` call to
 * record the current rate-limit state without an extra API round-trip.
 *
 * @param {{ headers?: Record<string, string | undefined> }} response - The github.rest response object
 * @param {string} operation - Human-readable description of the operation (e.g. "issues.listComments")
 * @param {unknown} [credentialSource] - Known credential category, never a token
 */
function logRateLimitFromResponse(response, operation, credentialSource) {
  const headers = response?.headers;
  if (!headers) return;

  const limit = headers["x-ratelimit-limit"];
  const remaining = headers["x-ratelimit-remaining"];
  const reset = headers["x-ratelimit-reset"];
  const used = headers["x-ratelimit-used"];
  const resource = headers["x-ratelimit-resource"];

  // Skip if no rate-limit headers are present (e.g. GraphQL responses)
  if (!limit && !remaining && !reset) return;

  /** @type {Record<string, unknown>} */
  const entry = {
    timestamp: new Date().toISOString(),
    source: "response_headers",
    operation,
  };
  const normalizedCredentialSource = normalizeCredentialSource(credentialSource);
  if (normalizedCredentialSource) entry.credentialSource = normalizedCredentialSource;

  if (resource) entry.resource = resource;
  if (limit !== undefined) entry.limit = parseInt(limit, 10);
  if (remaining !== undefined) entry.remaining = parseInt(remaining, 10);
  if (used !== undefined) entry.used = parseInt(used, 10);
  if (reset) entry.reset = parseResetTimestamp(reset);

  appendEntry(entry);
}

/**
 * Fetch the current GitHub API rate-limit information via the rate-limit API
 * and write a JSONL entry for each resource category.
 *
 * Use this for a point-in-time snapshot at the start or end of a script,
 * rather than after every individual API call.
 *
 * Returns the core rate-limit snapshot so callers can use a single API call
 * for both logging and in-memory rate-limit tracking.
 *
 * @param {any} github - The github object injected by actions/github-script
 * @param {string} [operation="fetch"] - Label recorded in each log entry
 * @returns {Promise<{remaining:number,limit:number,used:number,reset:string}|null>}
 *   Core rate-limit data, or null if the call fails or the core resource is absent.
 */
async function fetchAndLogRateLimit(github, operation = "fetch") {
  try {
    const response = await github.rest.rateLimit.get();
    const resources = response?.data?.resources;
    if (!resources) return null;

    const timestamp = new Date().toISOString();
    for (const [resource, data] of Object.entries(resources)) {
      if (!data || typeof data !== "object") continue;
      /** @type {Record<string, unknown>} */
      const entry = {
        timestamp,
        source: "rate_limit_api",
        operation,
        resource,
        limit: data.limit,
        remaining: data.remaining,
        used: data.used,
        reset: data.reset ? new Date(data.reset * 1000).toISOString() : null,
      };
      appendEntry(entry);
    }

    const coreData = resources.core;
    if (!coreData || typeof coreData !== "object") return null;
    const remaining = Number(coreData.remaining);
    const limit = Number(coreData.limit);
    const used = Number(coreData.used);
    const resetSeconds = Number(coreData.reset);
    if (!Number.isFinite(remaining) || !Number.isFinite(limit) || !Number.isFinite(used) || !Number.isFinite(resetSeconds)) {
      return null;
    }
    return {
      remaining,
      limit,
      used,
      reset: new Date(resetSeconds * 1000).toISOString(),
    };
  } catch (err) {
    core.warning(`github_rate_limit_logger: fetchAndLogRateLimit failed: ${getErrorMessage(err)}`);
    return null;
  }
}

/**
 * Log a retry attempt to the JSONL log file, capturing any rate-limit headers
 * present in the error response so that retry storms can be correlated with
 * quota exhaustion in post-run analysis.
 *
 * Call this from {@link withRetry} immediately before sleeping on a retry attempt.
 *
 * @param {any} error - The error that triggered the retry
 * @param {string} operation - Human-readable name of the failing operation
 * @param {number} attempt - Retry attempt number (1-based; 1 = first retry)
 * @param {number} delayMs - How long the caller will sleep before the next try
 */
function logRetryEvent(error, operation, attempt, delayMs) {
  const headers = error?.response?.headers ?? error?.headers ?? {};
  const status = error?.response?.status ?? error?.status ?? null;

  /** @type {Record<string, unknown>} */
  const entry = {
    timestamp: new Date().toISOString(),
    source: "retry",
    operation,
    attempt,
    delay_ms: delayMs,
  };

  if (status != null) entry.status = status;

  const remaining = headers["x-ratelimit-remaining"];
  const limit = headers["x-ratelimit-limit"];
  const reset = headers["x-ratelimit-reset"];
  const resource = headers["x-ratelimit-resource"];

  if (remaining !== undefined) entry.remaining = parseInt(remaining, 10);
  if (limit !== undefined) entry.limit = parseInt(limit, 10);
  if (reset) entry.reset = parseResetTimestamp(reset);
  if (resource) entry.resource = resource;

  appendEntry(entry);
}

/**
 * Wrap a github object (as provided by actions/github-script) so that every
 * `github.rest.*.*()` call automatically logs rate-limit headers from the
 * response.
 *
 * Usage:
 * ```js
 * const { createRateLimitAwareGithub } = require('./github_rate_limit_logger.cjs');
 * const gh = createRateLimitAwareGithub(github);
 * // All calls via gh.rest.* will now log rate limits automatically.
 * const { data } = await gh.rest.issues.get({ owner, repo, issue_number: 1 });
 * ```
 *
 * @param {any} github - The github object injected by actions/github-script
 * @param {unknown} [credentialSource] - Known credential category, never a token
 * @returns {any} A proxied github object with automatic rate-limit logging
 */
function createRateLimitAwareGithub(github, credentialSource) {
  const normalizedCredentialSource = normalizeCredentialSource(credentialSource) ?? "unknown";

  /**
   * Wrap a single REST namespace (e.g. github.rest.issues) so each method
   * call intercepts the response and logs rate-limit headers.
   *
   * @param {any} namespace - The REST namespace object
   * @param {string} namespaceName - Name used for logging (e.g. "issues")
   * @returns {any}
   */
  function wrapNamespace(namespace, namespaceName) {
    return new Proxy(namespace, {
      get(target, method) {
        const fn = target[method];
        if (typeof fn !== "function") return fn;
        const wrapper = async (/** @type {any[]} */ ...args) => {
          const response = await fn.apply(target, args);
          logRateLimitFromResponse(response, `${namespaceName}.${String(method)}`, normalizedCredentialSource);
          return response;
        };
        // Wrap the wrapper in a Proxy so that Octokit-specific property accesses
        // (e.g. .endpoint, used by github.paginate()) fall back to the original fn.
        // Without this, github.paginate(github.rest.checks.listForRef, ...) throws
        // "route.endpoint is not a function" because the wrapper does not have
        // the .endpoint decorator that Octokit endpoint methods carry.
        return new Proxy(wrapper, {
          get(wrapperTarget, prop) {
            const own = Reflect.get(wrapperTarget, prop);
            if (own !== undefined) return own;
            return Reflect.get(fn, prop);
          },
        });
      },
    });
  }

  // Wrap github.rest so every namespace access returns an instrumented proxy.
  const restProxy = new Proxy(github.rest, {
    get(target, namespaceName) {
      const ns = target[namespaceName];
      if (!ns || typeof ns !== "object") return ns;
      return wrapNamespace(ns, String(namespaceName));
    },
  });

  // Return a shallow proxy over github that replaces the `rest` property.
  return new Proxy(github, {
    get(target, prop) {
      if (prop === "rest") return restProxy;
      return target[prop];
    },
  });
}

module.exports = {
  logRateLimitFromResponse,
  fetchAndLogRateLimit,
  logRetryEvent,
  createRateLimitAwareGithub,
  GITHUB_RATE_LIMITS_JSONL_PATH,
};
