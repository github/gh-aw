import { describe, expect, it, vi } from "vitest";
import { getGitHubAPIQuota, getGitHubAPIDiagnostic } from "./github_api_diagnostics.cjs";

const now = Date.parse("2026-10-14T10:04:13Z");
const reset = String(Date.parse("2026-10-14T10:06:00Z") / 1000);
const error = (status, headers = {}, message = "private API payload") => Object.assign(new Error(message), { status, response: { status, headers } });

describe("failed-response GitHub API diagnostics", () => {
  it("identifies exhausted installation quota with validated business-response metadata", () => {
    const diagnostic = getGitHubAPIDiagnostic(
      error(403, { "x-ratelimit-resource": "core", "x-ratelimit-remaining": "0", "x-ratelimit-limit": "15000", "x-ratelimit-used": "15000", "x-ratelimit-reset": reset }, "API rate limit exceeded for installation"),
      now
    );
    expect(diagnostic).toEqual({
      quota: true,
      classification: "quota_exhausted",
      retryNotBefore: "2026-10-14T10:06:00.000Z",
      message: "GitHub API quota_exhausted (HTTP 403; resource=core; remaining=0; limit=15000; used=15000; reset=2026-10-14T10:06:00.000Z; retry_not_before=2026-10-14T10:06:00.000Z)",
    });
  });

  it("distinguishes the protective reserve from actual exhaustion", () => {
    const failure = Object.assign(error(200, { "x-ratelimit-remaining": "100", "x-ratelimit-reset": reset }), { code: "DAILY_AIC_QUOTA_STOP" });
    expect(getGitHubAPIDiagnostic(failure, now)).toMatchObject({ quota: true, classification: "quota_reserve" });
    expect(getGitHubAPIDiagnostic(failure, now).message).toContain("remaining=100");
  });

  it.each([
    [403, {}, "API rate limit exceeded for installation"],
    [403, {}, "You have exceeded a secondary rate limit"],
    [429, {}, "private API payload"],
    [403, { "retry-after": "120", "x-ratelimit-remaining": "4000" }, "private API payload"],
    [403, { "retry-after": "0", "x-ratelimit-remaining": "4000" }, "private API payload"],
  ])("recognizes rate limits without assuming missing quota headers mean zero", (status, headers, message) => {
    const diagnostic = getGitHubAPIDiagnostic(error(status, headers, message), now);
    expect(diagnostic).toMatchObject({ quota: true, classification: "rate_limited" });
    expect(diagnostic.message).not.toContain(message);
    expect(diagnostic.message).not.toContain("quota_exhausted");
  });

  it.each([
    [401, "authentication_failed"],
    [403, "permission_denied"],
  ])("does not equate HTTP %i with quota exhaustion", (status, classification) => {
    expect(getGitHubAPIDiagnostic(error(status), now)).toMatchObject({ quota: false, classification });
    const reserved = Object.assign(error(status, { "x-ratelimit-remaining": "100" }, status === 403 ? "Resource not accessible by integration" : "Bad credentials"), { code: "DAILY_AIC_QUOTA_STOP" });
    expect(getGitHubAPIDiagnostic(reserved, now)).toMatchObject({ quota: true, classification });
    expect(getGitHubAPIDiagnostic(reserved, now).message).toContain("quota reserve guard also stopped further reads");
  });

  it.each(["Resource not accessible by integration", "Resource not accessible by personal access token"])("preserves explicit permission evidence at an empty budget: %s", message => {
    const failure = error(403, { "x-ratelimit-remaining": "0" }, message);
    expect(getGitHubAPIDiagnostic(failure, now)).toMatchObject({ quota: false, classification: "permission_denied" });
    const stopped = Object.assign(error(403, failure.response.headers, "Daily AIC inspection stopped to preserve GitHub API quota"), { code: "DAILY_AIC_QUOTA_STOP", cause: failure });
    expect(getGitHubAPIDiagnostic(stopped, now)).toMatchObject({ quota: true, classification: "permission_denied" });
    expect(getGitHubAPIDiagnostic(stopped, now).message).toContain("quota reserve guard also stopped further reads");
    expect(getGitHubAPIDiagnostic(stopped, now).message).not.toContain(message);
  });

  it("does not treat a zero remaining header alone as a rate-limit response", () => {
    expect(getGitHubAPIDiagnostic(error(403, { "x-ratelimit-remaining": "0" }), now)).toMatchObject({ quota: false, classification: "permission_denied" });
    expect(getGitHubAPIDiagnostic(Object.assign(error(403, { "x-ratelimit-remaining": "50" }), { code: "DAILY_AIC_QUOTA_STOP" }), now)).toMatchObject({ quota: true, classification: "quota_reserve" });
  });

  it.each([
    error(429, { "retry-after": "120" }),
    error(403, { "x-ratelimit-remaining": "0" }, "API rate limit exceeded"),
    error(403, {}, "Resource not accessible by integration"),
    error(401),
    Object.assign(error(200, { "x-ratelimit-remaining": "50" }), { code: "DAILY_AIC_QUOTA_STOP" }),
  ])("retains diagnostics through bounded transport cause wrappers", failure => {
    const expected = getGitHubAPIDiagnostic(failure, now);
    let wrapped = failure;
    for (let depth = 0; depth < 4; depth++) {
      wrapped = new Error("https://example.invalid/?token=secret-test-value", { cause: wrapped });
      expect(getGitHubAPIDiagnostic(wrapped, now)).toEqual(expected);
    }
    expect(getGitHubAPIDiagnostic(new Error("outside traversal bound", { cause: wrapped }), now)).toBeNull();
  });

  it("terminates bounded traversal on cycles and non-object causes", () => {
    const cycle = new Error("wrapper");
    cycle.cause = cycle;
    expect(getGitHubAPIDiagnostic(cycle, now)).toBeNull();
    expect(getGitHubAPIDiagnostic(new Error("wrapper", { cause: "secret-test-value" }), now)).toBeNull();
  });

  it("retains the later reset or Retry-After deadline, without promising automatic recovery", () => {
    const headers = new Headers({ "x-ratelimit-remaining": "0", "x-ratelimit-reset": reset, "retry-after": "120" });
    expect(getGitHubAPIQuota(headers, now).retryNotBefore).toBe("2026-10-14T10:06:13.000Z");
    expect(getGitHubAPIQuota({ "retry-after": "Wed, 14 Oct 2026 10:07:00 GMT", "x-ratelimit-reset": reset }, now).retryNotBefore).toBe("2026-10-14T10:07:00.000Z");
    expect(getGitHubAPIQuota({ "x-ratelimit-reset": reset, "x-ratelimit-remaining": "4000" }, now).retryNotBefore).toBe("");
    expect(getGitHubAPIQuota({ "x-ratelimit-reset": "1", "x-ratelimit-remaining": "0" }, now).retryNotBefore).toBe("");
  });

  it.each(["10/15/2026", "2026-10-15T12:00:00Z", "2027 ", "Wed, 14 Oct 2026 10:07:00 UTC", "Wed, 14 Oct 2026 24:07:00 GMT", "Wed, 14 Oct 2026 10:67:00 GMT"])(
    "rejects non-HTTP-date Retry-After values while retaining the restriction: %s",
    value => {
      expect(getGitHubAPIQuota({ "retry-after": value }, now)).toMatchObject({ retryRestricted: true, retryNotBefore: "" });
    }
  );

  it.each(["Wed, 14 Oct 2026 10:07:00 GMT", "Wednesday, 14-Oct-26 10:07:00 GMT", "Wed Oct 14 10:07:00 2026"])("accepts HTTP-date Retry-After grammar: %s", value => {
    expect(getGitHubAPIQuota({ "retry-after": value }, now).retryNotBefore).toBe("2026-10-14T10:07:00.000Z");
  });

  it.each(["America/Los_Angeles", "Asia/Tokyo"])("interprets asctime HTTP-date in UTC regardless of host timezone %s", timezone => {
    vi.stubEnv("TZ", timezone);
    try {
      expect(getGitHubAPIQuota({ "retry-after": "Wed Oct 14 10:07:00 2026" }, now).retryNotBefore).toBe("2026-10-14T10:07:00.000Z");
      expect(getGitHubAPIQuota({ "retry-after": "2027" }, now).retryNotBefore).toBe(new Date(now + 2027000).toISOString());
    } finally {
      vi.unstubAllEnvs();
    }
  });

  it("interprets numeric Retry-After as seconds, never as a year", () => {
    expect(getGitHubAPIQuota({ "retry-after": "2027" }, now).retryNotBefore).toBe(new Date(now + 2027000).toISOString());
    expect(getGitHubAPIQuota({ "retry-after": "0" }, now)).toMatchObject({ retryRestricted: true, retryNotBefore: "" });
    expect(getGitHubAPIQuota({ "retry-after": "Wed, 14 Oct 2026 10:00:00 GMT" }, now)).toMatchObject({ retryRestricted: true, retryNotBefore: "" });
  });

  it.each(["secret-test-value", "-1", "1.5", "Infinity", "9999999999999999", "1e300", "", " 0 "])("never publishes malformed quota header values: %s", value => {
    const headers = Object.fromEntries(["x-ratelimit-resource", "x-ratelimit-remaining", "x-ratelimit-limit", "x-ratelimit-used", "x-ratelimit-reset", "retry-after", "authorization", "location"].map(name => [name, value]));
    const diagnostic = getGitHubAPIDiagnostic(error(429, headers), now);
    expect(diagnostic.message).toBe("GitHub API rate_limited (HTTP 429; resource=unknown; remaining=unknown; limit=unknown; used=unknown; reset=unknown; retry_not_before=unknown)");
  });

  it("excludes payloads, request headers, URLs and caller-supplied diagnostic fields", () => {
    const failure = error(403, {}, "API rate limit exceeded for installation; secret-test-value");
    failure.request = { headers: { authorization: "secret-test-value" }, url: "https://example.invalid/?token=secret-test-value" };
    failure.diagnostic = "secret-test-value";
    expect(JSON.stringify(getGitHubAPIDiagnostic(failure, now))).not.toContain("secret-test-value");
    expect(getGitHubAPIDiagnostic(error(500), now)).toBeNull();
    expect(getGitHubAPIDiagnostic(null, now)).toBeNull();
  });
});
