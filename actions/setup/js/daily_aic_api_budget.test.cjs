import { createRequire } from "node:module";
import { inspect } from "node:util";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const require = createRequire(import.meta.url);
const { API_READ_OPERATIONS, apiError, createAPIBudget, isTransientError, retryNotBefore, getAPIReadFailure, withQuotaResetRecovery } = require("./daily_aic_api_budget.cjs");
const { getGitHubAPIDiagnostic } = require("./github_api_diagnostics.cjs");

beforeEach(() => {
  vi.useFakeTimers();
  global.core = { info: vi.fn() };
});
afterEach(() => {
  vi.useRealTimers();
  delete global.core;
});

const response = (status = 200, headers = { "x-ratelimit-remaining": "4000" }) => ({ status, headers });

describe("bounded daily AIC read retries", () => {
  it("classifies a headerless installation rate limit without retrying or publishing its payload", async () => {
    const error = apiError(403, {}, "API rate limit exceeded for installation; secret-test-value");
    const request = vi.fn().mockRejectedValue(error);
    await expect(createAPIBudget().request(request, true, API_READ_OPERATIONS.CURRENT_RUN)).rejects.toBe(error);
    expect(getAPIReadFailure(error)).toMatchObject({ attempts: 1, reason: "quota_stop", operation: "current_run" });
    expect(request).toHaveBeenCalledOnce();
    const logs = global.core.info.mock.calls.flat().join("\n");
    expect(logs).toContain("current_run: GitHub API rate_limited");
    expect(logs).toContain("remaining=unknown");
    expect(logs).not.toContain("secret-test-value");
  });

  it("does not let malformed reset metadata replace the original failure", async () => {
    const request = vi.fn().mockRejectedValue(apiError(403, { "x-ratelimit-remaining": "0", "x-ratelimit-reset": "9999999999999999" }, "private payload"));
    const budget = createAPIBudget();
    await expect(budget.request(request)).rejects.toMatchObject({ code: "DAILY_AIC_QUOTA_STOP" });
    expect(budget.snapshot().reset).toBe("");
    expect(retryNotBefore({ "x-ratelimit-remaining": "0", "x-ratelimit-reset": "9999999999999999" })).toBe("");
    expect(request).toHaveBeenCalledOnce();
  });

  it("retries only the failed read and observes each response once", async () => {
    const budget = createAPIBudget();
    const request = vi
      .fn()
      .mockRejectedValueOnce(apiError(502, { "x-ratelimit-remaining": "4001" }, "Server Error"))
      .mockResolvedValue(response());
    const pending = budget.request(request);
    await vi.runAllTimersAsync();
    await expect(pending).resolves.toEqual(response());
    expect(request).toHaveBeenCalledTimes(2);
    expect(budget.snapshot()).toMatchObject({ requests: 2, remaining: 4000 });
    expect(global.core.info).toHaveBeenCalledWith('[daily-workflow-aic] Retrying transient read: {"attempt":1,"maxAttempts":3,"status":502,"delayMs":1000,"operation":"unknown"}');
    expect(global.core.info).toHaveBeenCalledWith('[daily-workflow-aic] Transient read recovered: {"attempts":2,"status":200,"requests":2,"operation":"unknown"}');
  });

  it.each([500, 502, 503, 504])("stops HTTP %i after three attempts", async status => {
    const request = vi.fn(async () => response(status));
    const budget = createAPIBudget();
    const assertion = expect(budget.request(request)).rejects.toMatchObject({ status });
    await vi.runAllTimersAsync();
    await assertion;
    expect(request).toHaveBeenCalledTimes(3);
    expect(budget.snapshot().requests).toBe(3);
    expect(global.core.info).toHaveBeenCalledWith(`[daily-workflow-aic] Stopped daily AIC read: {"attempts":3,"status":${status},"reason":"retry_exhausted","requests":3,"operation":"unknown"}`);
  });

  it.each([401, 403, 404, 429])("never retries HTTP %i", async status => {
    const request = vi.fn().mockRejectedValue(apiError(status, {}, "denied"));
    await expect(createAPIBudget().request(request)).rejects.toMatchObject({ status });
    expect(request).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
    expect(global.core.info).toHaveBeenCalledWith(`[daily-workflow-aic] Stopped daily AIC read: {"attempts":1,"status":${status},"reason":"${status === 429 ? "quota_stop" : "non_transient"}","requests":1,"operation":"unknown"}`);
  });

  it.each([200, 502])("stops at the quota reserve even on HTTP %i", async status => {
    const request = vi.fn(async () => response(status, { "x-ratelimit-remaining": "100" }));
    await expect(createAPIBudget().request(request)).rejects.toMatchObject({ code: "DAILY_AIC_QUOTA_STOP" });
    expect(request).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
    expect(global.core.info).toHaveBeenCalledWith(`[daily-workflow-aic] Stopped daily AIC read: {"attempts":1,"status":${status},"reason":"quota_stop","requests":1,"operation":"unknown"}`);
  });

  it("classifies a synthetic HTTP 403 reserve stop without permission evidence as quota_reserve", () => {
    let stopped;
    try {
      createAPIBudget(100).observe(response(403, { "x-ratelimit-remaining": "50" }));
    } catch (error) {
      stopped = error;
    }
    expect(stopped).toMatchObject({ code: "DAILY_AIC_QUOTA_STOP", status: 403 });
    expect(getGitHubAPIDiagnostic(stopped)).toMatchObject({ quota: true, classification: "quota_reserve" });
  });

  it.each([
    [401, "Bad credentials", "50", "authentication_failed"],
    [403, "Resource not accessible by integration", "50", "permission_denied"],
    [403, "Resource not accessible by integration", "0", "permission_denied"],
    [403, "API rate limit exceeded for installation", "0", "quota_exhausted"],
    [403, "Unspecified error", "50", "quota_reserve"],
  ])("preserves original HTTP %i access evidence while stopping quota-restricted reads", async (status, message, remaining, classification) => {
    const original = apiError(status, { "x-ratelimit-remaining": remaining }, `${message}; secret-test-value`);
    const request = vi.fn().mockRejectedValue(original);
    const stopped = await createAPIBudget()
      .request(request)
      .catch(error => error);
    expect(stopped).toMatchObject({ code: "DAILY_AIC_QUOTA_STOP", status });
    expect(getGitHubAPIDiagnostic(stopped)).toMatchObject({ quota: true, classification });
    if (classification === "quota_exhausted" || classification === "quota_reserve") {
      expect(stopped.cause).toBeUndefined();
    } else {
      expect(stopped.cause).not.toBe(original);
      expect(stopped.cause).toMatchObject({ status, message: status === 401 ? "Bad credentials" : "Permission denied" });
      expect(Object.keys(stopped.cause)).toEqual(["status"]);
    }
    expect(request).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
    const logs = global.core.info.mock.calls.flat().join("\n");
    expect(logs).toContain(`GitHub API ${classification}`);
    expect(logs).toContain('"reason":"quota_stop"');
    expect(logs).not.toContain("secret-test-value");
  });

  it.each([
    [401, "Bad credentials", "authentication_failed"],
    [403, "Resource not accessible by integration", "permission_denied"],
    [403, "Resource not accessible by personal access token", "permission_denied"],
    [403, "API rate limit exceeded", "quota_exhausted"],
  ])("never exposes original HTTP %i error properties during terminal inspection", async (status, message, classification) => {
    const original = apiError(status, { "x-ratelimit-remaining": "0" }, `${message}; secret-message-value`);
    original.request = { url: "https://example.invalid/?token=secret-url-value", headers: { authorization: "secret-request-header-value" }, body: "secret-request-body-value" };
    original.response.headers.authorization = "secret-response-header-value";
    original.response.headers.location = "https://example.invalid/?token=secret-location-value";
    original.response.body = "secret-response-body-value";
    original.cause = new Error("secret-nested-cause-value");
    const stopped = await createAPIBudget()
      .request(() => Promise.reject(original))
      .catch(error => error);
    expect(stopped).toMatchObject({ code: "DAILY_AIC_QUOTA_STOP", status });
    expect(getGitHubAPIDiagnostic(stopped)).toMatchObject({ quota: true, classification });
    const terminal = inspect(stopped, { depth: null, showHidden: true });
    expect(terminal).not.toMatch(/secret-[\w-]+-value|example\.invalid/);
    expect(terminal).toContain("DAILY_AIC_QUOTA_STOP");
    expect(vi.getTimerCount()).toBe(0);
  });

  it("does not retry a server error with an explicit retry restriction", async () => {
    const request = vi.fn(async () => response(503, { "retry-after": "120" }));
    await expect(createAPIBudget().request(request)).rejects.toMatchObject({ status: 503 });
    expect(request).toHaveBeenCalledOnce();
    expect(global.core.info).toHaveBeenCalledWith('[daily-workflow-aic] Stopped daily AIC read: {"attempts":1,"status":503,"reason":"retry_restricted","requests":1,"operation":"unknown"}');
  });

  it.each([
    Object.assign(new Error("connection reset"), { code: "ECONNRESET" }),
    Object.assign(new Error("timeout"), { name: "TimeoutError" }),
    new Error("fetch failed", { cause: Object.assign(new Error("timeout"), { code: "UND_ERR_CONNECT_TIMEOUT" }) }),
    Object.assign(new Error("failed to read response body", { cause: Object.assign(new Error("body timeout"), { code: "UND_ERR_BODY_TIMEOUT" }) }), { response: response() }),
  ])("recovers recognized transport failures", async error => {
    const request = vi.fn().mockRejectedValueOnce(error).mockResolvedValue(response());
    const budget = createAPIBudget();
    const assertion = expect(budget.request(request)).resolves.toEqual(response());
    await vi.runAllTimersAsync();
    await assertion;
    expect(request).toHaveBeenCalledTimes(2);
    expect(budget.snapshot().requests).toBe(error.response ? 2 : 1);
  });

  it.each([{ "x-ratelimit-remaining": "100" }, { "retry-after": "120" }])("never retries body timeouts through response restrictions %j", async headers => {
    const error = Object.assign(new Error("failed to read response body", { cause: Object.assign(new Error("body timeout"), { code: "UND_ERR_BODY_TIMEOUT" }) }), { response: response(200, headers) });
    const request = vi.fn().mockRejectedValue(error);
    const budget = createAPIBudget();
    await expect(budget.request(request)).rejects.toThrow();
    expect(request).toHaveBeenCalledOnce();
    expect(budget.snapshot().requests).toBe(1);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("does not retry arbitrary accounting or programming errors", async () => {
    const request = vi.fn().mockRejectedValue(new Error("invalid accounting"));
    await expect(createAPIBudget().request(request)).rejects.toThrow("invalid accounting");
    expect(request).toHaveBeenCalledOnce();
    expect(isTransientError(new Error("invalid accounting"))).toBe(false);
  });

  it("does not count artifact storage responses as GitHub quota", async () => {
    const budget = createAPIBudget();
    await budget.request(async () => response(), false);
    expect(budget.snapshot().requests).toBe(0);
  });

  it("never logs error payloads, credentials, headers or signed download URLs", async () => {
    const sensitive = "sensitive-test-value";
    const error = apiError(502, { authorization: sensitive, location: `https://example.invalid/blob?token=${sensitive}` }, sensitive);
    error.request = { headers: { authorization: sensitive } };
    const request = vi.fn().mockRejectedValue(error);
    const assertion = expect(createAPIBudget().request(request)).rejects.toMatchObject({ status: 502 });
    await vi.runAllTimersAsync();
    await assertion;
    const logs = global.core.info.mock.calls.map(([message]) => message).join("\n");
    expect(logs).toContain('"reason":"retry_exhausted"');
    expect(logs).not.toContain(sensitive);
    expect(logs).not.toContain("example.invalid");
  });

  it.each(Object.values(API_READ_OPERATIONS))("attributes %s failures without changing error identity", async operation => {
    const error = apiError(502, {}, "private API payload");
    const request = vi.fn().mockRejectedValue(error);
    const assertion = expect(createAPIBudget().request(request, operation !== "artifact_download", operation)).rejects.toBe(error);
    await vi.runAllTimersAsync();
    await assertion;
    expect(getAPIReadFailure(error)).toEqual({ attempts: 3, status: 502, reason: "retry_exhausted", requests: operation === "artifact_download" ? 0 : 3, operation });
    expect(getAPIReadFailure(new Error("wrapper", { cause: error }))).toEqual(getAPIReadFailure(error));
    expect(Object.isFrozen(getAPIReadFailure(error))).toBe(true);
    expect(global.core.info.mock.calls.map(([message]) => message).join("\n")).not.toContain("private API payload");
  });

  it("rejects unknown operation labels without publishing them", async () => {
    const sensitive = "https://example.invalid/download?token=private";
    const operation = vi.fn();
    await expect(createAPIBudget().request(operation, true, sensitive)).rejects.toThrow("Unknown daily AIC read operation");
    expect(operation).not.toHaveBeenCalled();
    expect(global.core.info.mock.calls.map(([message]) => message).join("\n")).not.toContain(sensitive);
    const unrelated = new Error("unrelated");
    unrelated.cause = unrelated;
    expect(getAPIReadFailure(unrelated)).toBeNull();
  });

  it("does not trust caller-supplied failure metadata", async () => {
    const sensitive = "https://example.invalid/download?token=private";
    const error = Object.assign(apiError(403, {}, sensitive), { dailyAICReadFailure: { operation: sensitive } });
    const assertion = expect(createAPIBudget().request(() => Promise.reject(error), true, API_READ_OPERATIONS.WORKFLOW_HISTORY)).rejects.toBe(error);
    await assertion;
    expect(getAPIReadFailure(error)).toMatchObject({ operation: API_READ_OPERATIONS.WORKFLOW_HISTORY, attempts: 1, reason: "non_transient" });
    expect(global.core.info.mock.calls.map(([message]) => message).join("\n")).not.toContain(sensitive);
  });

  it("labels recovered reads without retaining the earlier failure as terminal", async () => {
    const error = apiError(502, {}, "private payload");
    const request = vi.fn().mockRejectedValueOnce(error).mockResolvedValue(response());
    const assertion = expect(createAPIBudget().request(request, true, API_READ_OPERATIONS.WORKFLOW_HISTORY)).resolves.toEqual(response());
    await vi.runAllTimersAsync();
    await assertion;
    expect(getAPIReadFailure(error)).toBeNull();
    expect(global.core.info).toHaveBeenCalledWith('[daily-workflow-aic] Transient read recovered: {"attempts":2,"status":200,"requests":2,"operation":"workflow_history"}');
  });
});

describe("daily AIC retry deadlines", () => {
  const now = Date.parse("2026-10-09T12:00:00Z");
  const reset = String(now / 1000 + 3600);

  it("ignores the rate-limit reset on an ordinary server failure", () => {
    expect(retryNotBefore({ "x-ratelimit-reset": reset, "x-ratelimit-remaining": "4000" }, now)).toBe("");
  });

  describe("bounded complete-phase quota recovery", () => {
    const start = Date.parse("2026-10-21T10:00:00Z");
    const exhausted = (headers = {}, status = 403, message = "API rate limit exceeded") => apiError(status, { "x-ratelimit-remaining": "0", "x-ratelimit-reset": String(start / 1000 + 60), ...headers }, message);

    function clock() {
      let current = start;
      return {
        now: () => current,
        sleep: vi.fn(async delay => {
          current += delay;
        }),
        runAttempt: "1",
      };
    }

    it("restarts the complete phase only after the later server deadline", async () => {
      const recovery = clock();
      const operation = vi
        .fn()
        .mockRejectedValueOnce(exhausted({ "retry-after": "120" }))
        .mockResolvedValue("verified");
      await expect(withQuotaResetRecovery(operation, recovery)).resolves.toBe("verified");
      expect(recovery.sleep).toHaveBeenCalledExactlyOnceWith(121000);
      expect(operation).toHaveBeenCalledTimes(2);
      expect(recovery.now()).toBe(start + 121000);
    });

    it.each([403, 429])("uses Retry-After rather than healthy primary quota for a budget-wrapped HTTP %i limit", async status => {
      const recovery = clock();
      const request = vi.fn().mockRejectedValue(apiError(status, { "x-ratelimit-remaining": "4000", "x-ratelimit-reset": String(start / 1000 + 3600), "retry-after": "60" }, "Secondary rate limit"));
      const operation = vi
        .fn()
        .mockImplementationOnce(() => createAPIBudget().request(request))
        .mockResolvedValue("verified");
      await expect(withQuotaResetRecovery(operation, recovery)).resolves.toBe("verified");
      expect(recovery.sleep).toHaveBeenCalledExactlyOnceWith(61000);
      expect(operation).toHaveBeenCalledTimes(2);
      expect(request).toHaveBeenCalledOnce();
    });

    it.each([false, true])("uses Retry-After for a direct HTTP 429 without trusting a caller-supplied reserve stop (forged=%s)", async forged => {
      const recovery = clock();
      const error = apiError(429, { "x-ratelimit-remaining": "4000", "x-ratelimit-reset": String(start / 1000 + 3600), "retry-after": "60" }, "Secondary rate limit");
      if (forged) Object.assign(error, { code: "DAILY_AIC_QUOTA_STOP", reserveStopped: true });
      const operation = vi.fn().mockRejectedValueOnce(error).mockResolvedValue("verified");
      await expect(withQuotaResetRecovery(operation, recovery)).resolves.toBe("verified");
      expect(recovery.sleep).toHaveBeenCalledExactlyOnceWith(61000);
      expect(operation).toHaveBeenCalledTimes(2);
    });

    it.each([
      [100, 99, 121000],
      [100, 100, 121000],
      [100, 101, 61000],
      [50, 49, 121000],
      [50, 50, 121000],
      [50, 51, 61000],
      [200, 150, 121000],
      [200, 201, 61000],
    ])("binds the primary reset at reserve %i only when remaining %i reaches it", async (reserve, remaining, delayMs) => {
      const recovery = clock();
      const request = vi.fn().mockRejectedValue(apiError(429, { "x-ratelimit-remaining": String(remaining), "x-ratelimit-reset": String(start / 1000 + 120), "retry-after": "60" }, "Secondary rate limit"));
      const operation = vi
        .fn()
        .mockImplementationOnce(async () => {
          try {
            return await createAPIBudget(reserve).request(request);
          } catch (error) {
            const deadline = new Date(start + delayMs - 1000).toISOString();
            expect(getGitHubAPIDiagnostic(error, start).retryNotBefore).toBe(deadline);
            expect(getGitHubAPIDiagnostic(new Error("artifact wrapper", { cause: error }), start).retryNotBefore).toBe(deadline);
            throw error;
          }
        })
        .mockResolvedValue("verified");
      await expect(withQuotaResetRecovery(operation, recovery)).resolves.toBe("verified");
      expect(recovery.sleep).toHaveBeenCalledExactlyOnceWith(delayMs);
      expect(operation).toHaveBeenCalledTimes(2);
    });

    it("retains primary reset recovery for a successful response stopped at the reserve", async () => {
      const recovery = clock();
      const operation = vi
        .fn()
        .mockImplementationOnce(() => createAPIBudget().request(async () => response(200, { "x-ratelimit-remaining": "100", "x-ratelimit-reset": String(start / 1000 + 60) })))
        .mockResolvedValue("verified");
      await expect(withQuotaResetRecovery(operation, recovery)).resolves.toBe("verified");
      expect(recovery.sleep).toHaveBeenCalledExactlyOnceWith(61000);
      expect(operation).toHaveBeenCalledTimes(2);
    });

    it.each([
      [403, false],
      [403, true],
      [429, false],
      [429, true],
    ])("fails closed for HTTP %i secondary limits without a known deadline (budget=%s)", async (status, budgetWrapped) => {
      const recovery = clock();
      const error = apiError(status, { "x-ratelimit-remaining": "4000", "x-ratelimit-reset": String(start / 1000 + 10) }, "You have exceeded a secondary rate limit");
      const operation = vi.fn(() => (budgetWrapped ? createAPIBudget().request(() => Promise.reject(error)) : Promise.reject(error)));
      await expect(withQuotaResetRecovery(operation, recovery)).rejects.toMatchObject({ status });
      expect(operation).toHaveBeenCalledOnce();
      expect(recovery.sleep).not.toHaveBeenCalled();
      expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining('"reason":"unknown_reset"'));
    });

    it.each([
      ["unknown reset", { "x-ratelimit-reset": "" }, {}, "unknown_reset"],
      ["invalid reset", { "x-ratelimit-reset": "9999999999999999" }, {}, "unknown_reset"],
      ["expired reset", { "x-ratelimit-reset": String(start / 1000 - 1) }, {}, "unknown_reset"],
      ["long reset", { "x-ratelimit-reset": String(start / 1000 + 1200) }, {}, "reset_exceeds_budget"],
      ["rerun", {}, { runAttempt: "2" }, "not_original_attempt"],
      ["missing attempt", {}, { runAttempt: "" }, "not_original_attempt"],
    ])("blocks %s without sleeping or another API attempt", async (_name, headers, options, reason) => {
      const recovery = { ...clock(), ...options };
      const error = exhausted(headers);
      const operation = vi.fn().mockRejectedValue(error);
      await expect(withQuotaResetRecovery(operation, recovery)).rejects.toBe(error);
      expect(operation).toHaveBeenCalledOnce();
      expect(recovery.sleep).not.toHaveBeenCalled();
      expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining(`"reason":"${reason}"`));
    });

    it("counts time spent in the failed phase against the deferral budget", async () => {
      const recovery = clock();
      const operation = vi.fn(async () => {
        await recovery.sleep(590000);
        throw exhausted({ "retry-after": "60" });
      });
      await expect(withQuotaResetRecovery(operation, recovery)).rejects.toMatchObject({ status: 403 });
      expect(operation).toHaveBeenCalledOnce();
      expect(recovery.sleep).toHaveBeenCalledExactlyOnceWith(590000);
      expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("reset_exceeds_budget"));
    });

    it.each(["early", "late"])("blocks a %s timer wake instead of issuing another read", async kind => {
      const recovery = clock();
      if (kind === "early") recovery.sleep.mockImplementation(async () => {});
      else {
        const sleep = recovery.sleep.getMockImplementation();
        recovery.sleep.mockImplementation(delay => sleep(delay + 660000));
      }
      const error = exhausted();
      const operation = vi.fn().mockRejectedValue(error);
      await expect(withQuotaResetRecovery(operation, recovery)).rejects.toBe(error);
      expect(operation).toHaveBeenCalledOnce();
      expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("missed its time budget"));
    });

    it("stops renewed exhaustion without a second deferral", async () => {
      const recovery = clock();
      const error = exhausted();
      const operation = vi.fn().mockRejectedValue(error);
      await expect(withQuotaResetRecovery(operation, recovery)).rejects.toBe(error);
      expect(recovery.sleep).toHaveBeenCalledOnce();
      expect(operation).toHaveBeenCalledTimes(2);
    });

    it.each([401, 403])("does not defer HTTP %i access failures even when quota headers are exhausted", async status => {
      const recovery = clock();
      const original = exhausted({}, status, status === 401 ? "Bad credentials" : "Resource not accessible by integration");
      let error;
      try {
        createAPIBudget().observe(original.response, original);
      } catch (stopped) {
        error = stopped;
      }
      const operation = vi.fn().mockRejectedValue(error);
      await expect(withQuotaResetRecovery(operation, recovery)).rejects.toBe(error);
      expect(operation).toHaveBeenCalledOnce();
      expect(recovery.sleep).not.toHaveBeenCalled();
    });
  });

  it("retains reset deadlines for quota reserve stops and exhausted quota", () => {
    expect(retryNotBefore({ "x-ratelimit-reset": reset }, now, true)).toBe(new Date(now + 3600000).toISOString());
    expect(retryNotBefore({ "x-ratelimit-reset": reset, "x-ratelimit-remaining": "0" }, now)).toBe(new Date(now + 3600000).toISOString());
  });

  it("honors Retry-After without an unrelated quota reset", () => {
    expect(retryNotBefore({ "x-ratelimit-reset": reset, "retry-after": "120" }, now)).toBe(new Date(now + 120000).toISOString());
    expect(retryNotBefore({ "retry-after": new Date(now + 120000).toUTCString() }, now)).toBe(new Date(now + 120000).toISOString());
  });
});
