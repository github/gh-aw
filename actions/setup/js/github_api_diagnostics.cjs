// @ts-check

const { isRateLimitError } = require("./error_helpers.cjs");

// A synthetic quota stop can also represent a secondary limit above the reserve.
const RESERVE_STOPS = new WeakSet();

const HTTP_DATE =
  /^(?:(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun), (?:0[1-9]|[12]\d|3[01]) (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) \d{4} (?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d GMT|(?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday), (?:0[1-9]|[12]\d|3[01])-(?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)-\d{2} (?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d GMT|(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) (?:Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec) (?: [1-9]|0[1-9]|[12]\d|3[01]) (?:[01]\d|2[0-3]):[0-5]\d:[0-5]\d \d{4})$/;

function getGitHubAPIQuota(headers, now = Date.now(), quotaStopped = false) {
  const header = name => (typeof headers?.get === "function" ? headers.get(name) : headers?.[name]);
  const integer = value => {
    if (typeof value !== "string" || !/^\d{1,16}$/.test(value)) return null;
    const number = Number(value);
    return Number.isSafeInteger(number) ? number : null;
  };
  const remaining = integer(header("x-ratelimit-remaining"));
  const limit = integer(header("x-ratelimit-limit"));
  const used = integer(header("x-ratelimit-used"));
  const resetSeconds = integer(header("x-ratelimit-reset"));
  const resetMs = resetSeconds === null ? NaN : resetSeconds * 1000;
  const reset = Number.isFinite(resetMs) && resetMs > 0 && resetMs <= 8640000000000000 ? new Date(resetMs).toISOString() : "";
  const retrySeconds = integer(header("retry-after"));
  const retryHeader = header("retry-after");
  const retryMs =
    retrySeconds === null ? (typeof retryHeader === "string" && retryHeader.length <= 64 && HTTP_DATE.test(retryHeader) ? Date.parse(retryHeader.endsWith("GMT") ? retryHeader : `${retryHeader} GMT`) : NaN) : now + retrySeconds * 1000;
  const retryAt = Number.isFinite(retryMs) && retryMs > now && retryMs <= 8640000000000000 ? new Date(retryMs).toISOString() : "";
  const deadlines = [reset && (remaining === 0 || quotaStopped) && resetMs > now ? resetMs : NaN, retryAt ? retryMs : NaN].filter(Number.isFinite);
  const deadline = Math.max(...deadlines);
  const retryNotBefore = Number.isFinite(deadline) && deadline > now && deadline <= 8640000000000000 ? new Date(deadline).toISOString() : "";
  const resourceHeader = header("x-ratelimit-resource");
  const resource = ["core", "search", "graphql", "code_search", "integration_manifest", "actions_runner_registration"].includes(resourceHeader) ? resourceHeader : "unknown";
  return { resource, remaining, limit, used, reset, retryRestricted: retryHeader != null, retryNotBefore };
}

function getErrorChain(error) {
  // Match the artifact read-failure traversal bound, including cyclic causes.
  const chain = [];
  for (let depth = 0; depth < 5 && error && typeof error === "object"; depth++, error = error.cause) {
    chain.push(error);
  }
  return chain;
}

function getAccessFailure(chain) {
  if (chain.some(failure => Number(failure.status || failure.response?.status || 0) === 401)) return "authentication_failed";
  if (
    chain.some(
      failure =>
        Number(failure.status || failure.response?.status || 0) === 403 &&
        typeof failure.message === "string" &&
        /\bresource not accessible by (?:integration|personal access token)\b|\binsufficient permissions\b|\bpermission denied\b/i.test(failure.message)
    )
  ) {
    return "permission_denied";
  }
  return null;
}

function getSanitizedGitHubAPICause(error) {
  const accessFailure = getAccessFailure(getErrorChain(error));
  if (!accessFailure) return undefined;
  // Terminal Error inspection must not retain any original error properties.
  return Object.assign(new Error(accessFailure === "authentication_failed" ? "Bad credentials" : "Permission denied"), { status: accessFailure === "authentication_failed" ? 401 : 403 });
}

/**
 * Derive diagnostics only from the failed business response, never another
 * client's /rate_limit precheck. Publish validated metadata, not API payloads.
 * @param {any} error
 * @param {number} [now]
 */
function getGitHubAPIDiagnostic(error, now = Date.now()) {
  const chain = getErrorChain(error);
  error = chain.find(failure => failure.code === "DAILY_AIC_QUOTA_STOP" || [401, 403, 429].includes(Number(failure.status || failure.response?.status || 0)));
  if (!error) return null;

  const quotaStopped = error?.code === "DAILY_AIC_QUOTA_STOP";
  const metadata = getGitHubAPIQuota(error?.response?.headers, now, RESERVE_STOPS.has(error));
  const { resource, remaining, limit, used, reset, retryRestricted, retryNotBefore } = metadata;
  const status = Number(error?.status || error?.response?.status || 0);
  const secondary = typeof error?.message === "string" && /\bsecondary rate limit\b/i.test(error.message);
  const limited = status === 429 || (status === 403 && (retryRestricted || isRateLimitError(error) || secondary));
  const quota = quotaStopped || limited;
  const classification = getAccessFailure(chain) || (quota ? (remaining === 0 ? "quota_exhausted" : quotaStopped ? "quota_reserve" : "rate_limited") : "permission_denied");
  const accessFailure = classification === "authentication_failed" || classification === "permission_denied";
  const details = quota
    ? `resource=${resource}; remaining=${remaining ?? "unknown"}; limit=${limit ?? "unknown"}; used=${used ?? "unknown"}; reset=${reset || "unknown"}; retry_not_before=${retryNotBefore || "unknown"}${quotaStopped && accessFailure ? "; quota reserve guard also stopped further reads" : ""}`
    : "Check the credential's access to this operation; quota exhaustion was not established.";
  return { quota, classification, retryNotBefore, message: `GitHub API ${classification} (HTTP ${status >= 100 && status <= 599 && Number.isInteger(status) ? status : "unknown"}; ${details})` };
}

module.exports = { RESERVE_STOPS, getGitHubAPIQuota, getGitHubAPIDiagnostic, getSanitizedGitHubAPICause };
