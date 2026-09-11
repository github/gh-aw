// @ts-check

const SAFE_HEADERS = ["x-ratelimit-limit", "x-ratelimit-remaining", "x-ratelimit-used", "x-ratelimit-reset", "retry-after"];

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

function retryNotBefore(headers, now = Date.now()) {
  const safe = safeResponseHeaders(headers);
  const reset = Number(safe["x-ratelimit-reset"]) * 1000;
  const retry = safe["retry-after"];
  const retryAt = retry == null ? NaN : /^\d+$/.test(retry) ? now + Number(retry) * 1000 : Date.parse(retry);
  const times = [reset, retryAt].filter(value => Number.isFinite(value) && value > now);
  return times.length ? new Date(Math.max(...times)).toISOString() : "";
}

/**
 * Observe business responses, including native-fetch artifact requests. Do not use
 * /rate_limit as the authority for a different request's quota or retry deadline.
 * The caller stops on errors; this helper never retries or sleeps on shared quota.
 */
function createAPIBudget(reserve = 100) {
  let requests = 0;
  let latest = {};
  return {
    observe(response) {
      requests++;
      latest = safeResponseHeaders(response.headers);
      const remaining = latest["x-ratelimit-remaining"];
      const limited = response.status === 429 || (response.status === 403 && (remaining === "0" || latest["retry-after"] != null));
      if (limited || (remaining != null && Number(remaining) <= reserve)) {
        throw apiError(response.status, latest, "Daily AIC inspection stopped to preserve GitHub API quota");
      }
    },
    snapshot() {
      return {
        requests,
        remaining: Number(latest["x-ratelimit-remaining"] || 0),
        limit: Number(latest["x-ratelimit-limit"] || 0),
        used: Number(latest["x-ratelimit-used"] || 0),
        reset: latest["x-ratelimit-reset"] ? new Date(Number(latest["x-ratelimit-reset"]) * 1000).toISOString() : "",
      };
    },
  };
}

module.exports = { safeResponseHeaders, apiError, retryNotBefore, createAPIBudget };
