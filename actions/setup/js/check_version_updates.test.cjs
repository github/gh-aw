// @ts-check
import { describe, it, expect, beforeEach, vi, afterEach } from "vitest";
import { syncRuntimePromptTemplates } from "./test_prompt_templates.js";

syncRuntimePromptTemplates(import.meta.url);

describe("check_version_updates", () => {
  let mockCore;
  let mockFetch;
  let checkVersionUpdates;

  beforeEach(async () => {
    vi.useFakeTimers();

    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      debug: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
      summary: {
        addRaw: vi.fn().mockReturnThis(),
        write: vi.fn().mockResolvedValue(undefined),
      },
    };

    global.core = mockCore;
    global.github = {};
    global.context = { repo: { owner: "owner", repo: "repo" }, workflow: "Test workflow", runId: 123 };

    mockFetch = vi.fn();
    vi.stubGlobal("fetch", mockFetch);

    delete process.env.GH_AW_COMPILED_VERSION;
    delete process.env.GH_AW_BLOCKED_VERSION_REPORT_AS_ISSUE;
    delete process.env.GH_AW_WORKFLOW_NAME;
    delete process.env.GITHUB_REPOSITORY;
    delete process.env.GITHUB_RUN_ID;
    delete process.env.GITHUB_SERVER_URL;

    vi.resetModules();

    checkVersionUpdates = await import("./check_version_updates.cjs");
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.clearAllMocks();
    delete global.core;
    delete global.github;
    delete global.context;
  });

  /**
   * Helper to mock a successful fetch response with the given body.
   * @param {string} body
   */
  function mockFetchSuccess(body) {
    mockFetch.mockResolvedValue({
      ok: true,
      status: 200,
      text: () => Promise.resolve(body),
    });
  }

  /**
   * Helper to mock a failed fetch request (network error).
   * @param {Error} err
   */
  function mockFetchError(err) {
    mockFetch.mockRejectedValue(err);
  }

  /**
   * Run main() and advance all pending timers to process retry delays.
   * @returns {Promise<void>}
   */
  async function runMain() {
    const promise = checkVersionUpdates.main();
    await vi.runAllTimersAsync();
    return promise;
  }

  // ---------------------------------------------------------------------------
  // Skip cases — version not subject to check
  // ---------------------------------------------------------------------------

  it("should skip check when version is 'dev'", async () => {
    process.env.GH_AW_COMPILED_VERSION = "dev";
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockFetch).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("dev"));
  });

  it("should skip check when version is empty", async () => {
    process.env.GH_AW_COMPILED_VERSION = "";
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockFetch).not.toHaveBeenCalled();
  });

  it("should skip check when version has no 'v' prefix (not an official release)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "1.0.0";
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockFetch).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("not an official release version"));
  });

  it("should skip check when version is a non-semver string", async () => {
    process.env.GH_AW_COMPILED_VERSION = "latest";
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockFetch).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("not an official release version"));
  });

  it("should skip check when version has only two parts (v1.0 is invalid)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0";
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockFetch).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("not an official release version"));
  });

  it("should skip check when version has non-numeric parts (e.g. v1.0.alpha)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.alpha";
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockFetch).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("not an official release version"));
  });

  it("should skip check when version has extra dot segments (v1.2.3.4 is not vMAJOR.MINOR.PATCH)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.2.3.4";
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockFetch).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("not an official release version"));
  });

  // ---------------------------------------------------------------------------
  // Network / download failure cases (soft fail)
  // ---------------------------------------------------------------------------

  it("should skip check when all fetch attempts fail (ECONNREFUSED)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchError(new Error("ECONNREFUSED"));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not fetch update configuration"));
  });

  it("should skip check when all fetch attempts fail (ECONNRESET)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchError(new Error("ECONNRESET"));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not fetch update configuration"));
  });

  it("should retry and succeed when fetch fails transiently", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.2.0";
    // First call fails transiently; second call succeeds
    mockFetch.mockRejectedValueOnce(new Error("ECONNRESET")).mockResolvedValueOnce({
      ok: true,
      status: 200,
      text: () => Promise.resolve(JSON.stringify({ blockedVersions: [], minimumVersion: "v1.0.0" })),
    });
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockFetch).toHaveBeenCalledTimes(2);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  it("should skip check when server returns 404", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetch.mockResolvedValue({ ok: false, status: 404, text: () => Promise.resolve("") });
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not fetch update configuration"));
  });

  it("should skip check when server returns 500 after exhausting retries", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    // 500 should trigger retries; after all retries exhausted, soft-fail
    mockFetch.mockResolvedValue({ ok: false, status: 500, text: () => Promise.resolve("") });
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not fetch update configuration"));
    // Should have retried (default 3 retries = 4 total attempts: 1 initial + 3 retries)
    expect(mockFetch).toHaveBeenCalledTimes(4);
  });

  it("should retry on 500 and succeed if a later attempt succeeds", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.2.0";
    // First call returns 500, second succeeds
    mockFetch.mockResolvedValueOnce({ ok: false, status: 500, text: () => Promise.resolve("") }).mockResolvedValueOnce({
      ok: true,
      status: 200,
      text: () => Promise.resolve(JSON.stringify({ blockedVersions: [], minimumVersion: "v1.0.0" })),
    });
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockFetch).toHaveBeenCalledTimes(2);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  it("should skip check when response is not valid JSON (soft fail)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetch.mockResolvedValue({
      ok: true,
      status: 200,
      text: () => Promise.resolve("not json {{{"),
    });
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not fetch update configuration"));
  });

  it("should skip check when response is empty string (soft fail)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetch.mockResolvedValue({
      ok: true,
      status: 200,
      text: () => Promise.resolve(""),
    });
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not fetch update configuration"));
  });

  it("should skip check when response is an HTML error page (soft fail)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetch.mockResolvedValue({
      ok: true,
      status: 200,
      text: () => Promise.resolve("<!DOCTYPE html><html><body>Error</body></html>"),
    });
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Could not fetch update configuration"));
  });

  // ---------------------------------------------------------------------------
  // Version check passes
  // ---------------------------------------------------------------------------

  it("should pass when version is not blocked and meets minimum", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.2.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["v0.9.0"], minimumVersion: "v1.0.0" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  it("should pass when version exactly equals minimum", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "v1.0.0" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  it("should pass when blockedVersions is empty and no minimumVersion", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  it("should pass when config has no blockedVersions field", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ minimumVersion: "v0.5.0" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should pass when config has no minimumVersion field", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["v0.9.0"] }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  it("should pass when config is an empty object", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({}));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  // ---------------------------------------------------------------------------
  // Blocked version cases
  // ---------------------------------------------------------------------------

  it("should fail when version is in blocked list (exact match)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.1.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["v1.1.0", "v1.2.0"], minimumVersion: "" }));
    await runMain();
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Blocked compile-agentic version"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("v1.1.0"));
  });

  it("should fail when version is in blocked list (one of many)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v2.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["v1.0.0", "v2.0.0", "v3.0.0"], minimumVersion: "" }));
    await runMain();
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Blocked compile-agentic version"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("v2.0.0"));
  });

  it("should NOT block version when blocked list entry has no 'v' prefix (unknown format — ignore)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["1.0.0"], minimumVersion: "" }));
    // "1.0.0" in blocked list has no v prefix — it should be ignored, not matched
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  it("should NOT block when blocked list contains unknown/garbage versions", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["latest", "stable", "1.0.0", "vfoo"], minimumVersion: "" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  it("should correctly identify block when blocked list mixes valid and invalid entries", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["1.0.0", "latest", "v1.0.0", "v999.0.0"], minimumVersion: "" }));
    // Only "v1.0.0" is a valid entry and it matches
    await runMain();
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Blocked compile-agentic version"));
  });

  it("should write summary when version is blocked", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["v1.0.0"], minimumVersion: "" }));
    await runMain();
    expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Blocked compile-agentic version"));
    expect(mockCore.summary.write).toHaveBeenCalled();
  });

  it("should create a deduplicated issue when version is blocked", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    process.env.GH_AW_WORKFLOW_NAME = "Blocked workflow";
    process.env.GITHUB_REPOSITORY = "owner/repo";
    process.env.GITHUB_RUN_ID = "456";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["v1.0.0"], minimumVersion: "" }));
    const searchMock = vi.fn().mockResolvedValue({ data: { items: [] } });
    const createMock = vi.fn().mockResolvedValue({ data: { number: 42, html_url: "https://github.com/owner/repo/issues/42" } });
    global.github = {
      rest: {
        search: { issuesAndPullRequests: searchMock },
        issues: { create: createMock, update: vi.fn() },
      },
    };

    await runMain();

    expect(searchMock).toHaveBeenCalledWith({
      q: 'repo:owner/repo is:issue is:open in:title "[aw] Workflows blocked by compile-agentic v1.0.0"',
      per_page: 10,
    });
    expect(createMock).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "owner",
        repo: "repo",
        title: "[aw] Workflows blocked by compile-agentic v1.0.0",
        body: expect.stringContaining("Workflow: `Blocked workflow`"),
      })
    );
    expect(createMock.mock.calls[0][0].body).toContain("https://github.com/owner/repo/actions/runs/456");
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Blocked compile-agentic version"));
  });

  it("should update an existing blocked version issue when version is blocked", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["v1.0.0"], minimumVersion: "" }));
    const searchMock = vi.fn().mockResolvedValue({
      data: {
        items: [{ number: 7, html_url: "https://github.com/owner/repo/issues/7", title: "[aw] Workflows blocked by compile-agentic v1.0.0" }],
      },
    });
    const updateMock = vi.fn().mockResolvedValue({ data: { number: 7, html_url: "https://github.com/owner/repo/issues/7" } });
    const createMock = vi.fn();
    global.github = {
      rest: {
        search: { issuesAndPullRequests: searchMock },
        issues: { create: createMock, update: updateMock },
      },
    };

    await runMain();

    expect(updateMock).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "owner",
        repo: "repo",
        issue_number: 7,
        title: "[aw] Workflows blocked by compile-agentic v1.0.0",
      })
    );
    expect(createMock).not.toHaveBeenCalled();
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Blocked compile-agentic version"));
  });

  it("should continue failing the version check when blocked issue creation fails", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["v1.0.0"], minimumVersion: "" }));
    global.github = {
      rest: {
        search: { issuesAndPullRequests: vi.fn().mockResolvedValue({ data: { items: [] } }) },
        issues: { create: vi.fn().mockRejectedValue(new Error("Resource not accessible by integration")), update: vi.fn() },
      },
    };

    await runMain();

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not create or update blocked compiler version issue"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Blocked compile-agentic version"));
  });

  it("should skip issue notification when github APIs are unavailable", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["v1.0.0"], minimumVersion: "" }));
    global.github = {}; // no rest.search / rest.issues

    await runMain();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GitHub issue APIs are unavailable"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Blocked compile-agentic version"));
  });

  it("should retry the issue search on a transient error before creating an issue", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["v1.0.0"], minimumVersion: "" }));
    const searchMock = vi
      .fn()
      .mockRejectedValueOnce(new Error("secondary rate limit exceeded"))
      .mockResolvedValueOnce({ data: { items: [] } });
    const createMock = vi.fn().mockResolvedValue({ data: { number: 42, html_url: "https://github.com/owner/repo/issues/42" } });
    global.github = {
      rest: {
        search: { issuesAndPullRequests: searchMock },
        issues: { create: createMock, update: vi.fn() },
      },
    };

    await runMain();

    expect(searchMock).toHaveBeenCalledTimes(2);
    expect(createMock).toHaveBeenCalledTimes(1);
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Blocked compile-agentic version"));
  });

  it("should skip blocked version issue creation when disabled", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    process.env.GH_AW_BLOCKED_VERSION_REPORT_AS_ISSUE = "false";
    mockFetchSuccess(JSON.stringify({ blockedVersions: ["v1.0.0"], minimumVersion: "" }));
    const createMock = vi.fn();
    global.github = {
      rest: {
        search: { issuesAndPullRequests: vi.fn() },
        issues: { create: createMock, update: vi.fn() },
      },
    };

    await runMain();

    expect(createMock).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith("Blocked compiler version issue reporting is disabled");
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Blocked compile-agentic version"));
  });

  // ---------------------------------------------------------------------------
  // Minimum version cases
  // ---------------------------------------------------------------------------

  it("should fail when version is below minimum", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.8.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "v1.0.0" }));
    await runMain();
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Outdated compile-agentic version"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("v0.8.0"));
  });

  it("should fail when major version is older (v0.x.x vs min v1.0.0)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.99.99";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "v1.0.0" }));
    await runMain();
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Outdated compile-agentic version"));
  });

  it("should fail when minor version is older (v1.0.5 vs min v1.1.0)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.5";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "v1.1.0" }));
    await runMain();
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Outdated compile-agentic version"));
  });

  it("should fail when patch version is older (v1.0.0 vs min v1.0.1)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "v1.0.1" }));
    await runMain();
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Outdated compile-agentic version"));
  });

  it("should skip minimum check when minimumVersion is empty string", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.1.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should skip minimum check when minimumVersion has no 'v' prefix (unknown format — ignore)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.1.0";
    // minimumVersion "2.0.0" has no v prefix — should be ignored
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "2.0.0" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  it("should skip minimum check when minimumVersion is a garbage string", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.1.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "latest" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  it("should write summary when version is below minimum", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.8.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "v1.0.0" }));
    await runMain();
    expect(mockCore.summary.addRaw).toHaveBeenCalledWith(expect.stringContaining("Outdated compile-agentic version"));
    expect(mockCore.summary.write).toHaveBeenCalled();
  });

  // ---------------------------------------------------------------------------
  // Config structure edge cases
  // ---------------------------------------------------------------------------

  it("should pass when blockedVersions is null (treated as missing)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: null, minimumVersion: "v0.5.0" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should pass when minimumVersion is null (treated as missing)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: null }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should pass when config has extra unknown fields", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(
      JSON.stringify({
        blockedVersions: [],
        minimumVersion: "v0.5.0",
        futureField: "some-value",
        anotherField: 42,
      })
    );
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should pass when config is a JSON array (not object) — treats as empty config", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify([]));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it("should pass when config body is JSON null — treats as empty config", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess("null");
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Version check passed"));
  });

  // ---------------------------------------------------------------------------
  // minRecommendedVersion (soft check — warning only, no failure)
  // ---------------------------------------------------------------------------

  it("should warn when version is below minRecommendedVersion", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.9.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "", minRecommendedVersion: "v1.0.0" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Recommended upgrade"));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("v0.9.0"));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("v1.0.0"));
  });

  it("should pass without warning when version equals minRecommendedVersion", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.0.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "", minRecommendedVersion: "v1.0.0" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("should pass without warning when version is above minRecommendedVersion", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v1.1.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "", minRecommendedVersion: "v1.0.0" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("should skip recommended check when minRecommendedVersion is empty string", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.5.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "", minRecommendedVersion: "" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("should skip recommended check when minRecommendedVersion has no 'v' prefix (unknown format — ignore)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.5.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "", minRecommendedVersion: "2.0.0" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("should skip recommended check when minRecommendedVersion is a garbage string", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.5.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "", minRecommendedVersion: "latest" }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("should skip recommended check when minRecommendedVersion is null (treated as missing)", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.5.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "", minRecommendedVersion: null }));
    await runMain();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
    expect(mockCore.warning).not.toHaveBeenCalled();
  });

  it("should fail hard (not just warn) when version is also below minimumVersion", async () => {
    process.env.GH_AW_COMPILED_VERSION = "v0.8.0";
    mockFetchSuccess(JSON.stringify({ blockedVersions: [], minimumVersion: "v1.0.0", minRecommendedVersion: "v1.0.0" }));
    await runMain();
    // Should fail, not just warn
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("minimum supported version"));
  });
});
