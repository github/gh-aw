// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

describe("update_release", () => {
  let updateReleaseScript;
  let tempFilePath;
  let mockCore;
  let mockGithub;
  let mockContext;
  let originalGlobals;

  beforeEach(() => {
    originalGlobals = {
      core: global.core,
      github: global.github,
      context: global.context,
    };

    mockCore = {
      debug: vi.fn(),
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setFailed: vi.fn(),
      setOutput: vi.fn(),
      summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) },
    };

    mockGithub = {
      rest: {
        repos: {
          getReleaseByTag: vi.fn(),
          updateRelease: vi.fn(),
          getRelease: vi.fn(),
          listReleases: vi.fn(),
        },
      },
      paginate: vi.fn().mockImplementation(async () => []),
    };

    mockContext = {
      repo: { owner: "test-owner", repo: "test-repo" },
      serverUrl: "https://github.com",
      runId: 123456,
    };

    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;

    delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    delete process.env.GH_AW_AGENT_OUTPUT;
    delete process.env.GH_AW_WORKFLOW_NAME;

    updateReleaseScript = fs.readFileSync(path.join(__dirname, "update_release.cjs"), "utf8");
  });

  afterEach(() => {
    vi.useRealTimers();
    global.core = originalGlobals.core;
    global.github = originalGlobals.github;
    global.context = originalGlobals.context;

    if (tempFilePath && fs.existsSync(tempFilePath)) {
      fs.unlinkSync(tempFilePath);
      tempFilePath = undefined;
    }
  });

  /** @param {object} [config] @param {object} [message] */
  const evalHandler = (config = {}, message = {}) =>
    eval(`(async () => {
      ${updateReleaseScript};
      const handler = await main(${JSON.stringify(config)});
      return await handler(${JSON.stringify(message)});
    })()`);

  it("should skip in staged mode with empty message", async () => {
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";

    const result = await evalHandler();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("🎭 Staged Mode Preview"));
    expect(mockGithub.rest.repos.getReleaseByTag).not.toHaveBeenCalled();
    expect(result).toEqual({ skipped: true, reason: "staged_mode" });
  });

  it("should skip in staged mode with a provided tag", async () => {
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";

    const result = await evalHandler({}, { tag: "v1.0.0", operation: "replace", body: "New notes" });

    expect(result.skipped).toBe(true);
    expect(result.reason).toBe("staged_mode");
    expect(mockGithub.rest.repos.getReleaseByTag).not.toHaveBeenCalled();
    expect(mockGithub.rest.repos.updateRelease).not.toHaveBeenCalled();
  });

  it("should handle replace operation", async () => {
    const mockRelease = {
      id: 1,
      tag_name: "v1.0.0",
      name: "Release v1.0.0",
      body: "Old release notes",
      html_url: "https://github.com/test-owner/test-repo/releases/tag/v1.0.0",
    };
    mockGithub.rest.repos.getReleaseByTag.mockResolvedValue({ data: mockRelease });
    mockGithub.rest.repos.updateRelease.mockResolvedValue({ data: { ...mockRelease, body: "New release notes" } });
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";

    const result = await evalHandler({}, { tag: "v1.0.0", operation: "replace", body: "New release notes" });

    expect(mockGithub.rest.repos.getReleaseByTag).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      tag: "v1.0.0",
    });
    const callArgs = mockGithub.rest.repos.updateRelease.mock.calls[0][0];
    expect(callArgs.owner).toBe("test-owner");
    expect(callArgs.repo).toBe("test-repo");
    expect(callArgs.release_id).toBe(1);
    expect(callArgs.body).toContain("New release notes");
    expect(callArgs.body).not.toContain("Old release notes");
    expect(callArgs.body).toContain("Test Workflow");
    expect(callArgs.body).toContain("https://github.com/test-owner/test-repo/actions/runs/123456");
    expect(result.tag).toBe("v1.0.0");
    expect(result.id).toBe(1);
  });

  it("should handle append operation", async () => {
    const mockRelease = {
      id: 2,
      tag_name: "v2.0.0",
      name: "Release v2.0.0",
      body: "Original release notes",
      html_url: "https://github.com/test-owner/test-repo/releases/tag/v2.0.0",
    };
    mockGithub.rest.repos.getReleaseByTag.mockResolvedValue({ data: mockRelease });
    mockGithub.rest.repos.updateRelease.mockResolvedValue({ data: { ...mockRelease, body: "Updated body" } });
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";

    await evalHandler({}, { tag: "v2.0.0", operation: "append", body: "Additional notes" });

    const callArgs = mockGithub.rest.repos.updateRelease.mock.calls[0][0];
    expect(callArgs.owner).toBe("test-owner");
    expect(callArgs.repo).toBe("test-repo");
    expect(callArgs.release_id).toBe(2);
    expect(callArgs.body).toContain("Original release notes");
    expect(callArgs.body).toContain("---");
    expect(callArgs.body).toContain("Additional notes");
    expect(callArgs.body).toContain("Test Workflow");
    expect(callArgs.body).toContain("https://github.com/test-owner/test-repo/actions/runs/123456");
  });

  it("should handle prepend operation", async () => {
    const mockRelease = {
      id: 3,
      tag_name: "v3.0.0",
      name: "Release v3.0.0",
      body: "Existing release notes",
      html_url: "https://github.com/test-owner/test-repo/releases/tag/v3.0.0",
    };
    mockGithub.rest.repos.getReleaseByTag.mockResolvedValue({ data: mockRelease });
    mockGithub.rest.repos.updateRelease.mockResolvedValue({ data: { ...mockRelease, body: "Updated body" } });
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";

    await evalHandler({}, { tag: "v3.0.0", operation: "prepend", body: "Prepended notes" });

    const callArgs = mockGithub.rest.repos.updateRelease.mock.calls[0][0];
    expect(callArgs.body).toContain("Prepended notes");
    expect(callArgs.body).toContain("Test Workflow");
    expect(callArgs.body).toContain("https://github.com/test-owner/test-repo/actions/runs/123456");
    expect(callArgs.body).toContain("---");
    expect(callArgs.body).toContain("Existing release notes");
    expect(callArgs.body.indexOf("Prepended notes")).toBeLessThan(callArgs.body.indexOf("Existing release notes"));
  });

  it("should handle release not found error", async () => {
    const notFoundError = new Error("Not Found");
    notFoundError.status = 404;
    mockGithub.rest.repos.getReleaseByTag.mockRejectedValue(notFoundError);

    await expect(evalHandler({}, { tag: "v99.99.99", operation: "replace", body: "New notes" })).rejects.toThrow(
      "ERR_VALIDATION: No GitHub Release exists for tag 'v99.99.99' in test-owner/test-repo (checked published and draft releases). A Git tag alone is not enough; create the release at https://github.com/test-owner/test-repo/releases/new?tag=v99.99.99, then retry."
    );
    expect(mockGithub.paginate).toHaveBeenCalledWith(mockGithub.rest.repos.listReleases, expect.objectContaining({ owner: "test-owner", repo: "test-repo" }), expect.any(Function));
  });

  it("should fall back to a draft release when the tag lookup 404s", async () => {
    const notFoundError = new Error("Not Found");
    notFoundError.status = 404;
    mockGithub.rest.repos.getReleaseByTag.mockRejectedValue(notFoundError);

    const draftRelease = {
      id: 42,
      tag_name: "v0.0.8",
      draft: true,
      body: "Draft notes",
      html_url: "https://github.com/test-owner/test-repo/releases/tag/untagged-abc123",
    };
    mockGithub.paginate.mockImplementation(async (_method, _params, callback) => {
      if (callback) {
        const done = vi.fn();
        callback({ data: [{ id: 1, tag_name: "v0.0.1" }, draftRelease] }, done);
      }
      return [draftRelease];
    });
    mockGithub.rest.repos.updateRelease.mockResolvedValue({ data: { ...draftRelease, body: "Draft notes\n\nNew notes", html_url: draftRelease.html_url } });

    const result = await evalHandler({}, { tag: "v0.0.8", operation: "append", body: "New notes" });

    expect(mockGithub.rest.repos.updateRelease).toHaveBeenCalledWith(expect.objectContaining({ release_id: 42 }));
    expect(result.tag).toBe("v0.0.8");
  });

  it("should report a missing release when no draft matches either", async () => {
    const notFoundError = new Error("Not Found");
    notFoundError.status = 404;
    mockGithub.rest.repos.getReleaseByTag.mockRejectedValue(notFoundError);
    mockGithub.paginate.mockImplementation(async (_method, _params, callback) => {
      if (callback) {
        const done = vi.fn();
        callback({ data: [{ id: 1, tag_name: "v0.0.1" }] }, done);
      }
      return [];
    });

    await expect(evalHandler({}, { tag: "v0.0.8", operation: "replace", body: "New notes" })).rejects.toThrow("ERR_VALIDATION: No GitHub Release exists for tag 'v0.0.8' in test-owner/test-repo (checked published and draft releases).");
  });

  it("should still report the missing-release diagnostic when the draft search itself fails", async () => {
    const notFoundError = new Error("Not Found");
    notFoundError.status = 404;
    mockGithub.rest.repos.getReleaseByTag.mockRejectedValue(notFoundError);
    const forbiddenError = new Error("Forbidden");
    forbiddenError.status = 403;
    mockGithub.paginate.mockRejectedValue(forbiddenError);

    await expect(evalHandler({}, { tag: "v0.0.8", operation: "replace", body: "New notes" })).rejects.toThrow(
      "ERR_VALIDATION: No GitHub Release exists for tag 'v0.0.8' in test-owner/test-repo (checked published and draft releases). A Git tag alone is not enough; create the release at https://github.com/test-owner/test-repo/releases/new?tag=v0.0.8, then retry. Draft releases could not be checked (the search itself failed, possibly due to insufficient permissions); if a draft release exists, verify the token has push access to this repository."
    );
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not search draft releases for tag 'v0.0.8'"));
  });

  it("should retry transient release lookup failures", async () => {
    vi.useFakeTimers();
    const transientError = new Error("503 Service Unavailable");
    const mockRelease = {
      id: 1,
      tag_name: "v1.0.0",
      body: "Old release notes",
      html_url: "https://github.com/test-owner/test-repo/releases/tag/v1.0.0",
    };
    mockGithub.rest.repos.getReleaseByTag.mockRejectedValueOnce(transientError).mockResolvedValue({ data: mockRelease });
    mockGithub.rest.repos.updateRelease.mockResolvedValue({ data: mockRelease });

    const resultPromise = evalHandler({}, { tag: "v1.0.0", operation: "replace", body: "New notes" });
    await vi.runAllTimersAsync();
    const result = await resultPromise;

    expect(mockGithub.rest.repos.getReleaseByTag).toHaveBeenCalledTimes(2);
    expect(result.tag).toBe("v1.0.0");
  });

  it("should report an update API 404 as an API error", async () => {
    const mockRelease = {
      id: 1,
      tag_name: "v1.0.0",
      body: "Old release notes",
      html_url: "https://github.com/test-owner/test-repo/releases/tag/v1.0.0",
    };
    mockGithub.rest.repos.getReleaseByTag.mockResolvedValue({ data: mockRelease });
    mockGithub.rest.repos.updateRelease.mockRejectedValue(new Error("Not Found"));

    await expect(evalHandler({}, { tag: "v1.0.0", operation: "replace", body: "New notes" })).rejects.toThrow("ERR_API: Failed to update release with tag v1.0.0: Not Found");
  });

  it("should wrap generic API errors with ERR_API prefix", async () => {
    mockGithub.rest.repos.getReleaseByTag.mockRejectedValue(new Error("Internal Server Error"));

    await expect(evalHandler({}, { tag: "v1.0.0", operation: "replace", body: "New notes" })).rejects.toThrow(/^ERR_API: Failed to update release with tag v1\.0\.0:/);
  });

  it("should handle multiple release updates with the same handler", async () => {
    const mockRelease1 = { id: 1, tag_name: "v1.0.0", body: "Release 1", html_url: "https://github.com/test-owner/test-repo/releases/tag/v1.0.0" };
    const mockRelease2 = { id: 2, tag_name: "v2.0.0", body: "Release 2", html_url: "https://github.com/test-owner/test-repo/releases/tag/v2.0.0" };
    mockGithub.rest.repos.getReleaseByTag.mockResolvedValueOnce({ data: mockRelease1 }).mockResolvedValueOnce({ data: mockRelease2 });
    mockGithub.rest.repos.updateRelease.mockResolvedValueOnce({ data: { ...mockRelease1, body: "Updated 1" } }).mockResolvedValueOnce({ data: { ...mockRelease2, body: "Updated 2" } });

    const handler = await eval(`(async () => { ${updateReleaseScript}; return await main(); })()`);
    await handler({ tag: "v1.0.0", operation: "replace", body: "Updated 1" });
    await handler({ tag: "v2.0.0", operation: "replace", body: "Updated 2" });

    expect(mockGithub.rest.repos.getReleaseByTag).toHaveBeenCalledTimes(2);
    expect(mockGithub.rest.repos.updateRelease).toHaveBeenCalledTimes(2);
  });

  it("should infer tag from release event context", async () => {
    mockContext.eventName = "release";
    mockContext.payload = { release: { tag_name: "v1.5.0", name: "Version 1.5.0", body: "Original release body" } };

    const mockRelease = { id: 1, tag_name: "v1.5.0", body: "Original release body", html_url: "https://github.com/test-owner/test-repo/releases/tag/v1.5.0" };
    mockGithub.rest.repos.getReleaseByTag.mockResolvedValue({ data: mockRelease });
    mockGithub.rest.repos.updateRelease.mockResolvedValue({ data: { ...mockRelease, body: "Updated body" } });

    await evalHandler({}, { operation: "replace", body: "Updated body" });

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Inferred release tag from event context: v1.5.0"));
    expect(mockGithub.rest.repos.getReleaseByTag).toHaveBeenCalledWith({ owner: "test-owner", repo: "test-repo", tag: "v1.5.0" });
    const callArgs = mockGithub.rest.repos.updateRelease.mock.calls[0][0];
    expect(callArgs.body).toContain("Updated body");
    expect(callArgs.body).toContain("GitHub Agentic Workflow");
    expect(callArgs.body).toContain("https://github.com/test-owner/test-repo/actions/runs/123456");
  });

  it("should infer tag from workflow_dispatch release_url input", async () => {
    mockContext.eventName = "workflow_dispatch";
    mockContext.payload = { inputs: { release_url: "https://github.com/test-owner/test-repo/releases/tag/v4.0.0" } };

    const mockRelease = { id: 4, tag_name: "v4.0.0", body: "", html_url: "https://github.com/test-owner/test-repo/releases/tag/v4.0.0" };
    mockGithub.rest.repos.getReleaseByTag.mockResolvedValue({ data: mockRelease });
    mockGithub.rest.repos.updateRelease.mockResolvedValue({ data: { ...mockRelease, body: "New notes" } });

    await evalHandler({}, { operation: "replace", body: "New notes" });

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Inferred release tag from release_url input: v4.0.0"));
    expect(mockGithub.rest.repos.getReleaseByTag).toHaveBeenCalledWith({ owner: "test-owner", repo: "test-repo", tag: "v4.0.0" });
  });

  it("should infer tag from workflow_dispatch release_id input", async () => {
    mockContext.eventName = "workflow_dispatch";
    mockContext.payload = { inputs: { release_id: "42" } };

    mockGithub.rest.repos.getRelease.mockResolvedValue({ data: { tag_name: "v5.0.0" } });
    const mockRelease = { id: 42, tag_name: "v5.0.0", body: "", html_url: "https://github.com/test-owner/test-repo/releases/tag/v5.0.0" };
    mockGithub.rest.repos.getReleaseByTag.mockResolvedValue({ data: mockRelease });
    mockGithub.rest.repos.updateRelease.mockResolvedValue({ data: { ...mockRelease, body: "New notes" } });

    await evalHandler({}, { operation: "replace", body: "New notes" });

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Fetching release with ID: 42"));
    expect(mockGithub.rest.repos.getRelease).toHaveBeenCalledWith({ owner: "test-owner", repo: "test-repo", release_id: 42 });
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Inferred release tag from release_id input: v5.0.0"));
  });

  it("should infer tag when message tag is empty", async () => {
    mockContext.eventName = "release";
    mockContext.payload = { release: { tag_name: "v6.0.0" } };
    const mockRelease = { id: 6, tag_name: "v6.0.0", body: "", html_url: "https://github.com/test-owner/test-repo/releases/tag/v6.0.0" };
    mockGithub.rest.repos.getReleaseByTag.mockResolvedValue({ data: mockRelease });
    mockGithub.rest.repos.updateRelease.mockResolvedValue({ data: { ...mockRelease, body: "New notes" } });

    await evalHandler({}, { tag: "   ", operation: "replace", body: "New notes" });

    expect(mockGithub.rest.repos.getReleaseByTag).toHaveBeenCalledWith({ owner: "test-owner", repo: "test-repo", tag: "v6.0.0" });
  });

  it("should fail fast for invalid workflow_dispatch release_id input", async () => {
    mockContext.eventName = "workflow_dispatch";
    mockContext.payload = { inputs: { release_id: "42abc" } };

    await expect(evalHandler({}, { operation: "replace", body: "New notes" })).rejects.toThrow("ERR_VALIDATION: Invalid release_id input '42abc'. Expected a positive integer.");
    expect(mockGithub.rest.repos.getRelease).not.toHaveBeenCalled();
  });

  it("should fail when tag is missing and cannot be inferred", async () => {
    mockContext.eventName = "push";
    mockContext.payload = {};

    await expect(evalHandler({}, { operation: "replace", body: "Updated body" })).rejects.toThrow("Release tag is required");
  });

  it("should exclude footer when config.footer is false", async () => {
    const mockRelease = {
      id: 1,
      tag_name: "v1.0.0",
      body: "Original notes",
      html_url: "https://github.com/test-owner/test-repo/releases/tag/v1.0.0",
    };
    mockGithub.rest.repos.getReleaseByTag.mockResolvedValue({ data: mockRelease });
    mockGithub.rest.repos.updateRelease.mockResolvedValue({ data: { ...mockRelease, body: "New notes" } });
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";

    await evalHandler({ footer: false }, { tag: "v1.0.0", operation: "replace", body: "New notes" });

    const callArgs = mockGithub.rest.repos.updateRelease.mock.calls[0][0];
    expect(callArgs.body).not.toContain("Test Workflow");
  });
});
