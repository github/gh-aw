import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

const { getGitHubAPIQuotaExhaustion, renderGitHubAPIQuotaSummary, withGitHubAPIQuotaWait } = require("./github_api_quota.cjs");

const now = Date.parse("2026-11-09T23:58:53Z");
const apiError = (status, headers, message = "API rate limit exceeded for installation") => Object.assign(new Error(message), { status, response: { status, headers } });
const exhausted = (resetSeconds, extra = {}) =>
  apiError(403, { "x-ratelimit-remaining": "0", "x-ratelimit-limit": "15000", "x-ratelimit-used": "15000", "x-ratelimit-resource": "core", ...(resetSeconds ? { "x-ratelimit-reset": String(resetSeconds) } : {}), ...extra });

describe("github_api_quota", () => {
  beforeEach(() => {
    global.core = { info: vi.fn(), warning: vi.fn() };
  });
  afterEach(() => {
    delete global.core;
  });

  it("extracts safe metadata from a genuine quota 403", () => {
    expect(getGitHubAPIQuotaExhaustion(exhausted(now / 1000 + 71), now)).toEqual({
      status: 403,
      resource: "core",
      limit: 15000,
      remaining: 0,
      used: 15000,
      reset: "2026-11-10T00:00:04.000Z",
      retryNotBefore: "2026-11-10T00:00:04.000Z",
    });
    expect(getGitHubAPIQuotaExhaustion(new Error("wrapped", { cause: exhausted(now / 1000 + 71) }), now)).toMatchObject({ remaining: 0 });
  });

  it.each([
    ["401", apiError(401, { "x-ratelimit-remaining": "0" }, "Bad credentials")],
    ["permission 403", apiError(403, { "x-ratelimit-remaining": "0" }, "Resource not accessible by integration")],
    ["403 without quota metadata", apiError(403, {}, "Forbidden")],
    ["secondary rate limit without deadline", apiError(403, {}, "You have exceeded a secondary rate limit")],
    ["server error", apiError(502, { "x-ratelimit-remaining": "0" }, "Bad gateway")],
    ["non-HTTP failure", new Error("checksum mismatch")],
  ])("does not reclassify %s as quota exhaustion", (_case, error) => {
    expect(getGitHubAPIQuotaExhaustion(error, now)).toBeNull();
  });

  it("renders only validated metadata in the summary", () => {
    const summary = renderGitHubAPIQuotaSummary({ stage: "Detection", quota: getGitHubAPIQuotaExhaustion(exhausted(now / 1000 + 71), now), reason: "reset_exceeds_budget", impact: "Blocked." });
    expect(summary).toContain("### GitHub API quota exhausted");
    expect(summary).toContain("| Resource | core |");
    expect(summary).toContain("| Reset | 2026-11-10T00:00:04.000Z |");
    expect(summary).not.toContain("installation");
  });

  it("waits until the server deadline instead of a shorter retry cap, then retries once", async () => {
    let current = now;
    const sleep = vi.fn(async ms => {
      current += ms;
    });
    const operation = vi
      .fn()
      .mockRejectedValueOnce(exhausted(now / 1000 + 300))
      .mockResolvedValue("ok");
    await expect(withGitHubAPIQuotaWait(operation, { label: "read", now: () => current, sleep })).resolves.toBe("ok");
    expect(sleep).toHaveBeenCalledExactlyOnceWith(301000);
    expect(operation).toHaveBeenCalledTimes(2);
  });

  it("passes non-quota failures through unchanged without waiting", async () => {
    const failure = apiError(403, {}, "Forbidden");
    const sleep = vi.fn();
    await expect(withGitHubAPIQuotaWait(vi.fn().mockRejectedValue(failure), { label: "read", now: () => now, sleep })).rejects.toBe(failure);
    expect(sleep).not.toHaveBeenCalled();
  });
});
