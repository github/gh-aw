// @ts-check

const { RESERVE_STOPS, getGitHubAPIQuota, getGitHubAPIDiagnostic, getSanitizedGitHubAPICause } = require("./github_api_diagnostics.cjs");

const SAFE_HEADERS = ["x-ratelimit-limit", "x-ratelimit-remaining", "x-ratelimit-used", "x-ratelimit-reset", "x-ratelimit-resource", "retry-after"];
const API_READ_OPERATIONS = Object.freeze({
  CURRENT_RUN: "current_run",
  WORKFLOW_HISTORY: "workflow_history",
  REPOSITORY_HISTORY: "repository_history",
  RUN_JOBS: "run_jobs",
  ARTIFACT_LIST: "artifact_list",
  ARTIFACT_REDIRECT: "artifact_redirect",
  ARTIFACT_DOWNLOAD: "artifact_download",
});
const READ_OPERATIONS = new Set(Object.values(API_READ_OPERATIONS));
const READ_FAILURES = new WeakMap();

function getAPIReadFailure(error) {
  // Artifact transport wrappers retain the original failure as their cause.
  for (let depth = 0; depth < 5 && error && typeof error === "object"; depth++, error = error.cause) {
    const failure = READ_FAILURES.get(error);
    if (failure) return failure;
  }
  return null;
}

function safeResponseHeaders(headers) {
  return Object.fromEntries(
    SAFE_HEADERS.flatMap(name => {
      const value = typeof headers?.get === "function" ? headers.get(name) : headers?.[name];
      return value == null ? [] : [[name, String(value)]];
    })
  );
}

function apiError(status, headers, message) {
  const error = new Error(message);
  return Object.assign(error, { status, response: { status, headers: safeResponseHeaders(headers) } });
}

function retryNotBefore(headers, now = Date.now(), quotaStopped = false) {
  return getGitHubAPIQuota(headers, now, quotaStopped).retryNotBefore;
}

function getErrorStatus(error) {
  return Number(error?.status || error?.response?.status || 0);
}

function isQuotaError(error) {
  const headers = safeResponseHeaders(error?.response?.headers);
  const status = getErrorStatus(error);
  return error?.code === "DAILY_AIC_QUOTA_STOP" || status === 429 || (status === 403 && (headers["x-ratelimit-remaining"] === "0" || headers["retry-after"] != null)) || getGitHubAPIDiagnostic(error)?.quota === true;
}

function isTransientError(error) {
  if (isQuotaError(error) || safeResponseHeaders(error?.response?.headers)["retry-after"] != null) return false;
  const status = getErrorStatus(error);
  if (status >= 400) return status >= 500 && status < 600;
  const codes = ["ECONNRESET", "ECONNREFUSED", "ETIMEDOUT", "EAI_AGAIN", "UND_ERR_CONNECT_TIMEOUT", "UND_ERR_HEADERS_TIMEOUT", "UND_ERR_BODY_TIMEOUT", "UND_ERR_SOCKET"];
  return ["TimeoutError", "AbortError"].includes(error?.name) || codes.includes(error?.code) || (!!error?.cause && isTransientError(error.cause));
}

/**
 * Restart a complete read-only accounting phase, never just the failed page.
 * Quota deferral is separate from transient read retries and grants no startup
 * authorization. Allow at most one minute of timer scheduling tolerance.
 */
async function withQuotaResetRecovery(operation, { now = Date.now, sleep = delay => new Promise(resolve => setTimeout(resolve, delay)), runAttempt = process.env.GITHUB_RUN_ATTEMPT, maxWaitMs = 10 * 60 * 1000 } = {}) {
  const startedAt = now();
  try {
    return await operation();
  } catch (error) {
    const diagnostic = getGitHubAPIDiagnostic(error, now());
    if (!isQuotaError(error) || ["authentication_failed", "permission_denied"].includes(diagnostic?.classification || "")) throw error;
    const retryAt = diagnostic?.retryNotBefore || "";
    const retryAtMs = Date.parse(retryAt);
    const delayMs = retryAtMs + 1000 - now();
    const remainingMs = maxWaitMs - (now() - startedAt);
    const reason = String(runAttempt) !== "1" ? "not_original_attempt" : !Number.isFinite(retryAtMs) || delayMs <= 0 ? "unknown_reset" : delayMs > remainingMs ? "reset_exceeds_budget" : "";
    if (reason) {
      core.info(`[daily-workflow-aic] Quota reset recovery unavailable: ${JSON.stringify({ reason, retryNotBefore: retryAt || null, maxWaitMs })}. Accounting remains unverified; agent startup is blocked.`);
      throw error;
    }
    core.info(`[daily-workflow-aic] Deferring complete accounting phase until quota reset: ${JSON.stringify({ retryNotBefore: retryAt, delayMs, maxWaitMs })}`);
    await sleep(delayMs);
    if (!Number.isFinite(retryAtMs) || now() < retryAtMs || now() - startedAt > maxWaitMs + 60000) {
      core.info("[daily-workflow-aic] Quota reset recovery missed its time budget. Accounting remains unverified; agent startup is blocked.");
      throw error;
    }
    // Deliberately outside the catch: renewed exhaustion cannot defer again.
    return operation();
  }
}

/**
 * Observe business responses, including native-fetch artifact requests. Do not use
 * /rate_limit as the authority for a different request's quota or retry deadline.
 * Retry transient reads only; shared quota and server retry restrictions stop immediately.
 */
function createAPIBudget(reserve = 100) {
  let requests = 0;
  let latest = {};
  const budget = {
    observe(response, cause = undefined) {
      requests++;
      latest = safeResponseHeaders(response.headers);
      const remaining = latest["x-ratelimit-remaining"];
      const quotaRemaining = getGitHubAPIQuota(latest).remaining;
      const reserveStopped = quotaRemaining !== null && quotaRemaining <= reserve;
      const limited = response.status === 429 || (response.status === 403 && (remaining === "0" || latest["retry-after"] != null));
      if (limited || reserveStopped) {
        const sanitizedCause = getSanitizedGitHubAPICause(cause);
        const error = Object.assign(apiError(response.status, latest, "Daily AIC inspection stopped to preserve GitHub API quota"), { code: "DAILY_AIC_QUOTA_STOP", ...(sanitizedCause ? { cause: sanitizedCause } : {}) });
        if (reserveStopped) RESERVE_STOPS.add(error);
        const diagnostic = getGitHubAPIDiagnostic(error);
        if (diagnostic) error.message += `: ${diagnostic.message}`;
        throw error;
      }
    },
    async request(operation, observe = true, readOperation) {
      if (readOperation !== undefined && !READ_OPERATIONS.has(readOperation)) {
        throw new TypeError("Unknown daily AIC read operation");
      }
      const operationName = readOperation ?? "unknown";
      for (let attempt = 0; ; attempt++) {
        try {
          let response;
          try {
            response = await operation();
          } catch (error) {
            if (observe && error?.response) budget.observe(error.response, error);
            throw error;
          }
          if (observe) budget.observe(response);
          if (response.status >= 500 && response.status < 600) {
            throw apiError(response.status, response.headers, "Daily AIC read failed with a server error");
          }
          if (attempt > 0) {
            core.info(`[daily-workflow-aic] Transient read recovered: ${JSON.stringify({ attempts: attempt + 1, status: response.status || null, requests, operation: operationName })}`);
          }
          return response;
        } catch (error) {
          const transient = isTransientError(error);
          const status = getErrorStatus(error) || null;
          if (attempt >= 2 || !transient) {
            const reason = isQuotaError(error) ? "quota_stop" : safeResponseHeaders(error?.response?.headers)["retry-after"] != null ? "retry_restricted" : transient ? "retry_exhausted" : "non_transient";
            const failure = Object.freeze({ attempts: attempt + 1, status: status !== null && Number.isInteger(status) && status >= 100 && status <= 599 ? status : null, reason, requests, operation: operationName });
            if (error && typeof error === "object") READ_FAILURES.set(error, failure);
            core.info(`[daily-workflow-aic] Stopped daily AIC read: ${JSON.stringify(failure)}`);
            const diagnostic = getGitHubAPIDiagnostic(error);
            if (diagnostic) core.info(`[daily-workflow-aic] ${operationName}: ${diagnostic.message}`);
            throw error;
          }
          const delayMs = 1000 * 2 ** attempt;
          core.info(`[daily-workflow-aic] Retrying transient read: ${JSON.stringify({ attempt: attempt + 1, maxAttempts: 3, status, delayMs, operation: operationName })}`);
          await new Promise(resolve => setTimeout(resolve, delayMs));
        }
      }
    },
    snapshot() {
      const metadata = getGitHubAPIQuota(latest);
      return {
        requests,
        remaining: metadata.remaining ?? 0,
        limit: metadata.limit ?? 0,
        used: metadata.used ?? 0,
        reset: metadata.reset,
      };
    },
  };
  return budget;
}

module.exports = { API_READ_OPERATIONS, safeResponseHeaders, apiError, retryNotBefore, createAPIBudget, getErrorStatus, isQuotaError, isTransientError, getAPIReadFailure, withQuotaResetRecovery };
