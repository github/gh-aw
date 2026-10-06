// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "fs";

// ---------------------------------------------------------------------------
// Globals injected by actions/github-script
// ---------------------------------------------------------------------------

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};

global.core = mockCore;

// ---------------------------------------------------------------------------
// Module import
// ---------------------------------------------------------------------------

const { logRateLimitFromResponse, fetchAndLogRateLimit, logRetryEvent, createRateLimitAwareGithub, GITHUB_RATE_LIMITS_JSONL_PATH } = await import("./github_rate_limit_logger.cjs?" + Date.now());

// ---------------------------------------------------------------------------
// logRateLimitFromResponse
// ---------------------------------------------------------------------------

describe("logRateLimitFromResponse", () => {
  let existsSpy, mkdirSpy, appendSpy;

  beforeEach(() => {
    existsSpy = vi.spyOn(fs, "existsSync").mockReturnValue(true);
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    vi.clearAllMocks();
    existsSpy.mockReturnValue(true);
  });

  afterEach(() => {
    existsSpy.mockRestore();
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
  });

  it("appends a JSONL entry when all rate-limit headers are present", () => {
    const response = {
      headers: {
        "x-ratelimit-limit": "5000",
        "x-ratelimit-remaining": "4900",
        "x-ratelimit-used": "100",
        "x-ratelimit-reset": "1700000000",
        "x-ratelimit-resource": "core",
      },
    };

    logRateLimitFromResponse(response, "issues.get");

    expect(appendSpy).toHaveBeenCalledOnce();
    const [filePath, content] = appendSpy.mock.calls[0];
    expect(filePath).toBe(GITHUB_RATE_LIMITS_JSONL_PATH);

    const entry = JSON.parse(content.trimEnd());
    expect(entry.source).toBe("response_headers");
    expect(entry.operation).toBe("issues.get");
    expect(entry.limit).toBe(5000);
    expect(entry.remaining).toBe(4900);
    expect(entry.used).toBe(100);
    expect(entry.resource).toBe("core");
    expect(typeof entry.timestamp).toBe("string");
    expect(typeof entry.reset).toBe("string");

    // JSONL: must end with a newline and contain no embedded newlines
    expect(content).toMatch(/\n$/);
    expect(content.trimEnd()).not.toContain("\n");
  });

  it("does not persist an invalid credential source", () => {
    logRateLimitFromResponse({ headers: { "x-ratelimit-limit": "5000", "x-ratelimit-remaining": "4900" } }, "issues.get", "ghp_secret_token");

    const entry = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
    expect(entry).not.toHaveProperty("credentialSource");
    expect(JSON.stringify(entry)).not.toContain("ghp_secret_token");
  });

  it("does nothing when there are no rate-limit headers", () => {
    logRateLimitFromResponse({ headers: { "content-type": "application/json" } }, "repos.get");
    expect(appendSpy).not.toHaveBeenCalled();
  });

  it("does nothing when headers property is absent", () => {
    logRateLimitFromResponse({}, "repos.get");
    expect(appendSpy).not.toHaveBeenCalled();
  });

  it("does nothing when response is null", () => {
    logRateLimitFromResponse(null, "repos.get");
    expect(appendSpy).not.toHaveBeenCalled();
  });

  it("does nothing when response is undefined", () => {
    logRateLimitFromResponse(undefined, "repos.get");
    expect(appendSpy).not.toHaveBeenCalled();
  });

  it("appends a separate entry per call", () => {
    const response = {
      headers: {
        "x-ratelimit-limit": "5000",
        "x-ratelimit-remaining": "4800",
        "x-ratelimit-reset": "1700000000",
      },
    };

    logRateLimitFromResponse(response, "issues.list");
    logRateLimitFromResponse({ ...response }, "issues.create");

    expect(appendSpy).toHaveBeenCalledTimes(2);
    expect(JSON.parse(appendSpy.mock.calls[0][1].trimEnd()).operation).toBe("issues.list");
    expect(JSON.parse(appendSpy.mock.calls[1][1].trimEnd()).operation).toBe("issues.create");
  });

  it("emits a warning when appendFileSync throws", () => {
    appendSpy.mockImplementation(() => {
      throw new Error("disk full");
    });

    const response = {
      headers: {
        "x-ratelimit-limit": "5000",
        "x-ratelimit-remaining": "4900",
        "x-ratelimit-reset": "1700000000",
      },
    };

    // Must not throw
    expect(() => logRateLimitFromResponse(response, "issues.get")).not.toThrow();
    expect(mockCore.warning).toHaveBeenCalled();
  });

  it("converts reset Unix timestamp to ISO 8601 string", () => {
    const resetSeconds = 1700000000;
    const response = {
      headers: {
        "x-ratelimit-limit": "5000",
        "x-ratelimit-remaining": "4900",
        "x-ratelimit-reset": String(resetSeconds),
      },
    };

    logRateLimitFromResponse(response, "repos.get");

    const entry = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
    expect(entry.reset).toBe(new Date(resetSeconds * 1000).toISOString());
  });
});

// ---------------------------------------------------------------------------
// fetchAndLogRateLimit
// ---------------------------------------------------------------------------

describe("fetchAndLogRateLimit", () => {
  let existsSpy, mkdirSpy, appendSpy;

  beforeEach(() => {
    existsSpy = vi.spyOn(fs, "existsSync").mockReturnValue(true);
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    vi.clearAllMocks();
    existsSpy.mockReturnValue(true);
  });

  afterEach(() => {
    existsSpy.mockRestore();
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
  });

  it("writes one JSONL entry per resource category", async () => {
    const mockGithub = {
      rest: {
        rateLimit: {
          get: vi.fn().mockResolvedValue({
            data: {
              resources: {
                core: { limit: 5000, remaining: 4900, used: 100, reset: 1700000000 },
                search: { limit: 30, remaining: 28, used: 2, reset: 1700000000 },
              },
            },
          }),
        },
      },
    };

    await fetchAndLogRateLimit(mockGithub, "startup");

    expect(appendSpy).toHaveBeenCalledTimes(2);

    const entries = appendSpy.mock.calls.map(([, content]) => JSON.parse(content.trimEnd()));
    const coreEntry = entries.find(e => e.resource === "core");
    const searchEntry = entries.find(e => e.resource === "search");

    expect(coreEntry).toBeDefined();
    expect(coreEntry.source).toBe("rate_limit_api");
    expect(coreEntry.operation).toBe("startup");
    expect(coreEntry.limit).toBe(5000);
    expect(coreEntry.remaining).toBe(4900);
    expect(typeof coreEntry.reset).toBe("string");

    expect(searchEntry).toBeDefined();
    expect(searchEntry.limit).toBe(30);
  });

  it("emits a warning and does not throw when the API call fails", async () => {
    const mockGithub = {
      rest: {
        rateLimit: {
          get: vi.fn().mockRejectedValue(new Error("API unavailable")),
        },
      },
    };

    await expect(fetchAndLogRateLimit(mockGithub)).resolves.toBeNull();
    expect(mockCore.warning).toHaveBeenCalled();
    expect(appendSpy).not.toHaveBeenCalled();
  });

  it("uses 'fetch' as the default operation label", async () => {
    const mockGithub = {
      rest: {
        rateLimit: {
          get: vi.fn().mockResolvedValue({
            data: {
              resources: {
                core: { limit: 5000, remaining: 5000, used: 0, reset: 1700000000 },
              },
            },
          }),
        },
      },
    };

    await fetchAndLogRateLimit(mockGithub);

    const entry = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
    expect(entry.operation).toBe("fetch");
  });

  it("returns core rate-limit snapshot on success", async () => {
    const resetSeconds = 1700000000;
    const mockGithub = {
      rest: {
        rateLimit: {
          get: vi.fn().mockResolvedValue({
            data: {
              resources: {
                core: { limit: 5000, remaining: 4321, used: 679, reset: resetSeconds },
                search: { limit: 30, remaining: 28, used: 2, reset: resetSeconds },
              },
            },
          }),
        },
      },
    };

    const result = await fetchAndLogRateLimit(mockGithub, "guardrail-start");

    expect(result).not.toBeNull();
    expect(result.remaining).toBe(4321);
    expect(result.limit).toBe(5000);
    expect(result.used).toBe(679);
    expect(result.reset).toBe(new Date(resetSeconds * 1000).toISOString());
  });

  it("returns null when resources are absent", async () => {
    const mockGithub = {
      rest: {
        rateLimit: {
          get: vi.fn().mockResolvedValue({ data: {} }),
        },
      },
    };

    const result = await fetchAndLogRateLimit(mockGithub, "guardrail-end");

    expect(result).toBeNull();
    expect(appendSpy).not.toHaveBeenCalled();
  });

  it("returns null when core resource is absent", async () => {
    const mockGithub = {
      rest: {
        rateLimit: {
          get: vi.fn().mockResolvedValue({
            data: {
              resources: {
                search: { limit: 30, remaining: 28, used: 2, reset: 1700000000 },
              },
            },
          }),
        },
      },
    };

    const result = await fetchAndLogRateLimit(mockGithub, "guardrail-end");

    expect(result).toBeNull();
  });

  it("returns null when core used field is missing", async () => {
    const mockGithub = {
      rest: {
        rateLimit: {
          get: vi.fn().mockResolvedValue({
            data: {
              resources: {
                core: { limit: 5000, remaining: 4900, reset: 1700000000 },
              },
            },
          }),
        },
      },
    };

    const result = await fetchAndLogRateLimit(mockGithub, "guardrail-end");

    expect(result).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// createRateLimitAwareGithub
// ---------------------------------------------------------------------------

describe("createRateLimitAwareGithub", () => {
  let existsSpy, mkdirSpy, appendSpy;

  beforeEach(() => {
    existsSpy = vi.spyOn(fs, "existsSync").mockReturnValue(true);
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    vi.clearAllMocks();
    existsSpy.mockReturnValue(true);
  });

  afterEach(() => {
    existsSpy.mockRestore();
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
  });

  it("proxies a REST call and logs rate-limit headers from the response", async () => {
    const mockResponse = {
      data: { id: 1 },
      headers: {
        "x-ratelimit-limit": "5000",
        "x-ratelimit-remaining": "4999",
        "x-ratelimit-reset": "1700000000",
        "x-ratelimit-resource": "core",
      },
    };

    const mockIssuesGet = vi.fn().mockResolvedValue(mockResponse);
    const mockGithub = {
      rest: {
        issues: { get: mockIssuesGet },
      },
    };

    const gh = createRateLimitAwareGithub(mockGithub);
    const result = await gh.rest.issues.get({ owner: "o", repo: "r", issue_number: 1 });

    // Original response is returned unchanged
    expect(result).toBe(mockResponse);
    // Underlying function was called with correct args
    expect(mockIssuesGet).toHaveBeenCalledWith({ owner: "o", repo: "r", issue_number: 1 });

    // Rate limit was logged
    expect(appendSpy).toHaveBeenCalledOnce();
    const entry = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
    expect(entry.operation).toBe("issues.get");
    expect(entry.remaining).toBe(4999);
  });

  it("passes through non-REST properties unchanged", () => {
    const mockGraphql = vi.fn();
    const mockGithub = {
      rest: { issues: { get: vi.fn() } },
      graphql: mockGraphql,
      auth: "token abc",
    };

    const gh = createRateLimitAwareGithub(mockGithub);

    expect(gh.graphql).toBe(mockGraphql);
    expect(gh.auth).toBe("token abc");
  });

  it("does not log when response has no rate-limit headers", async () => {
    const mockResponse = {
      data: { id: 99 },
      headers: { "content-type": "application/json" },
    };

    const mockGithub = {
      rest: {
        repos: { get: vi.fn().mockResolvedValue(mockResponse) },
      },
    };

    const gh = createRateLimitAwareGithub(mockGithub);
    const result = await gh.rest.repos.get({ owner: "o", repo: "r" });

    expect(result).toBe(mockResponse);
    expect(appendSpy).not.toHaveBeenCalled();
  });

  it("logs separate entries for consecutive calls in the same namespace", async () => {
    const makeResponse = remaining => ({
      data: {},
      headers: {
        "x-ratelimit-limit": "5000",
        "x-ratelimit-remaining": String(remaining),
        "x-ratelimit-reset": "1700000000",
      },
    });

    const mockGithub = {
      rest: {
        issues: {
          get: vi.fn().mockResolvedValueOnce(makeResponse(4999)).mockResolvedValueOnce(makeResponse(4998)),
        },
      },
    };

    const gh = createRateLimitAwareGithub(mockGithub);
    await gh.rest.issues.get({ owner: "o", repo: "r", issue_number: 1 });
    await gh.rest.issues.get({ owner: "o", repo: "r", issue_number: 2 });

    expect(appendSpy).toHaveBeenCalledTimes(2);
    const first = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
    const second = JSON.parse(appendSpy.mock.calls[1][1].trimEnd());
    expect(first.remaining).toBe(4999);
    expect(second.remaining).toBe(4998);
  });

  it("preserves .endpoint on the wrapped method for github.paginate() compatibility", () => {
    // Octokit endpoint-decorated methods (e.g. github.rest.checks.listForRef) carry
    // a .endpoint property used by github.paginate() internally.  The proxy must
    // forward .endpoint to the original function so paginate() doesn't throw
    // "route.endpoint is not a function".
    const endpointObj = { merge: vi.fn(), defaults: vi.fn() };
    const mockListForRef = vi.fn().mockResolvedValue({ data: [], headers: {} });
    mockListForRef.endpoint = endpointObj;

    const mockGithub = {
      rest: {
        checks: { listForRef: mockListForRef },
      },
    };

    const gh = createRateLimitAwareGithub(mockGithub);
    const wrapped = gh.rest.checks.listForRef;

    // The wrapped function must expose .endpoint from the original
    expect(typeof wrapped).toBe("function");
    expect(wrapped.endpoint).toBe(endpointObj);
    expect(wrapped.endpoint.merge).toBe(endpointObj.merge);
  });

  it("allows github.paginate() to call endpoint.merge on the wrapped method", () => {
    // Simulate how @octokit/plugin-paginate-rest v9+ uses the endpoint method:
    //   const options = route.endpoint.merge(parameters)
    const mergedOptions = { url: "/repos/o/r/commits/main/check-runs", per_page: 100 };
    const endpointObj = { merge: vi.fn().mockReturnValue(mergedOptions) };
    const mockListForRef = vi.fn().mockResolvedValue({ data: [], headers: {} });
    mockListForRef.endpoint = endpointObj;

    const mockGithub = {
      rest: { checks: { listForRef: mockListForRef } },
    };

    const gh = createRateLimitAwareGithub(mockGithub);
    const wrapped = gh.rest.checks.listForRef;

    // paginate() would call this internally
    const result = wrapped.endpoint.merge({ owner: "o", repo: "r", ref: "main", per_page: 100 });
    expect(result).toBe(mergedOptions);
    expect(endpointObj.merge).toHaveBeenCalledWith({ owner: "o", repo: "r", ref: "main", per_page: 100 });
  });
});

// ---------------------------------------------------------------------------
// logRetryEvent
// ---------------------------------------------------------------------------

describe("logRetryEvent", () => {
  let existsSpy, mkdirSpy, appendSpy;

  beforeEach(() => {
    existsSpy = vi.spyOn(fs, "existsSync").mockReturnValue(true);
    mkdirSpy = vi.spyOn(fs, "mkdirSync").mockImplementation(() => undefined);
    appendSpy = vi.spyOn(fs, "appendFileSync").mockImplementation(() => undefined);
    vi.clearAllMocks();
    existsSpy.mockReturnValue(true);
  });

  afterEach(() => {
    existsSpy.mockRestore();
    mkdirSpy.mockRestore();
    appendSpy.mockRestore();
  });

  it("writes a retry JSONL entry with source=retry", () => {
    const error = { message: "rate limit exceeded" };
    logRetryEvent(error, "issues.create", 1, 30000);

    expect(appendSpy).toHaveBeenCalledOnce();
    const entry = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
    expect(entry.source).toBe("retry");
    expect(entry.operation).toBe("issues.create");
    expect(entry.attempt).toBe(1);
    expect(entry.delay_ms).toBe(30000);
    expect(typeof entry.timestamp).toBe("string");
  });

  it("includes HTTP status when present in error", () => {
    const error = { response: { status: 429, headers: {} } };
    logRetryEvent(error, "issues.create", 1, 15000);

    const entry = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
    expect(entry.status).toBe(429);
  });

  it("includes rate-limit headers from the error response when present", () => {
    const resetSeconds = 1700000000;
    const error = {
      response: {
        status: 429,
        headers: {
          "x-ratelimit-limit": "5000",
          "x-ratelimit-remaining": "0",
          "x-ratelimit-reset": String(resetSeconds),
          "x-ratelimit-resource": "core",
        },
      },
    };

    logRetryEvent(error, "lock-issue", 2, 60000);

    const entry = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
    expect(entry.limit).toBe(5000);
    expect(entry.remaining).toBe(0);
    expect(entry.reset).toBe(new Date(resetSeconds * 1000).toISOString());
    expect(entry.resource).toBe("core");
  });

  it("also reads headers from top-level error.headers", () => {
    const error = {
      status: 429,
      headers: { "x-ratelimit-remaining": "10", "x-ratelimit-limit": "5000" },
    };

    logRetryEvent(error, "add-labels", 1, 30000);

    const entry = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
    expect(entry.remaining).toBe(10);
    expect(entry.limit).toBe(5000);
  });

  it("omits optional fields when error has no response headers", () => {
    logRetryEvent(new Error("network timeout"), "create-issue", 3, 120000);

    const entry = JSON.parse(appendSpy.mock.calls[0][1].trimEnd());
    expect(entry.source).toBe("retry");
    expect(entry.status).toBeUndefined();
    expect(entry.remaining).toBeUndefined();
    expect(entry.limit).toBeUndefined();
    expect(entry.reset).toBeUndefined();
    expect(entry.resource).toBeUndefined();
  });

  it("writes valid JSONL (ends with newline, no embedded newlines)", () => {
    logRetryEvent({}, "test-op", 1, 5000);

    const content = appendSpy.mock.calls[0][1];
    expect(content).toMatch(/\n$/);
    expect(content.trimEnd()).not.toContain("\n");
  });
});
