import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import path from "path";
import { fileURLToPath } from "url";

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};

const mockGithub = {
  rest: {
    pulls: {
      get: vi.fn(),
      createReview: vi.fn(),
      listFiles: vi.fn(),
      listReviews: vi.fn(),
      dismissReview: vi.fn(),
    },
  },
};

global.core = mockCore;
global.github = mockGithub;

const { createReviewBuffer, createPrReviewBufferRegistry } = require("./pr_review_buffer.cjs");

describe("createPrReviewBufferRegistry", () => {
  let savedCore;
  beforeEach(() => {
    savedCore = global.core;
    global.core = {
      debug: vi.fn(),
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
    };
  });
  afterEach(() => {
    global.core = savedCore;
  });

  it("returns separate buffers for different (repo, prNumber) pairs", () => {
    const registry = createPrReviewBufferRegistry();
    const buf1 = registry.getOrCreate("o/r", 1);
    const buf2 = registry.getOrCreate("o/r", 2);
    const buf3 = registry.getOrCreate("other/repo", 1);
    expect(buf1).not.toBe(buf2);
    expect(buf1).not.toBe(buf3);
    expect(buf2).not.toBe(buf3);
  });

  it("returns the same buffer for repeated calls with the same key", () => {
    const registry = createPrReviewBufferRegistry();
    const buf1 = registry.getOrCreate("o/r", 5);
    const buf2 = registry.getOrCreate("o/r", 5);
    expect(buf1).toBe(buf2);
  });

  it("returns null when repo is falsy", () => {
    const registry = createPrReviewBufferRegistry();
    expect(registry.getOrCreate(null, 1)).toBeNull();
    expect(registry.getOrCreate("", 1)).toBeNull();
  });

  it("returns null when prNumber is falsy", () => {
    const registry = createPrReviewBufferRegistry();
    expect(registry.getOrCreate("o/r", null)).toBeNull();
    expect(registry.getOrCreate("o/r", 0)).toBeNull();
  });

  it("getAllEntries returns entries in insertion order", () => {
    const registry = createPrReviewBufferRegistry();
    registry.getOrCreate("o/r", 3);
    registry.getOrCreate("o/r", 1);
    registry.getOrCreate("o/r", 2);
    const entries = registry.getAllEntries();
    expect(entries.map(e => e.prNumber)).toEqual([3, 1, 2]);
  });

  it("hasAnyContent returns false when all buffers are empty", () => {
    const registry = createPrReviewBufferRegistry();
    registry.getOrCreate("o/r", 1);
    expect(registry.hasAnyContent()).toBe(false);
  });

  it("hasAnyContent returns true when any buffer has metadata", () => {
    const registry = createPrReviewBufferRegistry();
    const buf = registry.getOrCreate("o/r", 1);
    buf.setReviewMetadata("body", "COMMENT");
    expect(registry.hasAnyContent()).toBe(true);
  });

  it("setDefaultFooterMode applies to newly created buffers", () => {
    const registry = createPrReviewBufferRegistry();
    registry.setDefaultFooterMode("none");
    // We can't directly inspect footerMode, but we can check that the
    // new buffer was created (no throw) and that setting metadata works
    const buf = registry.getOrCreate("o/r", 1);
    expect(buf).not.toBeNull();
    buf.setReviewMetadata("body", "COMMENT");
    expect(buf.hasReviewMetadata()).toBe(true);
  });
});

describe("pr_review_buffer (factory pattern)", () => {
  let buffer;
  let originalMessages;
  let originalPromptsDir;

  beforeEach(() => {
    vi.clearAllMocks();

    // Save and clear messages env var (generateFooterWithMessages reads this)
    originalMessages = process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
    delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;

    // Point GH_AW_PROMPTS_DIR to the source md/ directory so getPromptPath()
    // resolves template files from the source tree in test environments where
    // RUNNER_TEMP is set but the runtime prompts directory is not populated.
    originalPromptsDir = process.env.GH_AW_PROMPTS_DIR;
    process.env.GH_AW_PROMPTS_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), "../md");

    // Default: return empty file list so path filtering is skipped unless explicitly mocked
    mockGithub.rest.pulls.get.mockResolvedValue({
      data: {
        requested_reviewers: [{ login: "existing-reviewer" }],
        requested_teams: [],
      },
    });
    mockGithub.rest.pulls.listFiles.mockResolvedValue({ data: [] });
    mockGithub.rest.pulls.listReviews.mockResolvedValue({ data: [] });

    // Create a fresh buffer instance for each test (no shared global state)
    buffer = createReviewBuffer();
  });

  afterEach(() => {
    // Restore original environment
    if (originalMessages !== undefined) {
      process.env.GH_AW_SAFE_OUTPUT_MESSAGES = originalMessages;
    } else {
      delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
    }
    if (originalPromptsDir !== undefined) {
      process.env.GH_AW_PROMPTS_DIR = originalPromptsDir;
    } else {
      delete process.env.GH_AW_PROMPTS_DIR;
    }
  });

  it("should create independent buffer instances", () => {
    const buffer1 = createReviewBuffer();
    const buffer2 = createReviewBuffer();

    buffer1.addComment({ path: "file.js", line: 1, body: "comment" });

    expect(buffer1.getBufferedCount()).toBe(1);
    expect(buffer2.getBufferedCount()).toBe(0);
  });

  describe("addComment", () => {
    it("should buffer a single comment", () => {
      buffer.addComment({ path: "src/index.js", line: 10, body: "Fix this" });

      expect(buffer.hasBufferedComments()).toBe(true);
      expect(buffer.getBufferedCount()).toBe(1);
    });

    it("should buffer multiple comments", () => {
      buffer.addComment({ path: "src/index.js", line: 10, body: "Fix this" });
      buffer.addComment({ path: "src/utils.js", line: 20, body: "And this" });
      buffer.addComment({
        path: "src/app.js",
        line: 30,
        body: "Also this",
        start_line: 25,
      });

      expect(buffer.getBufferedCount()).toBe(3);
    });
  });

  describe("setReviewMetadata", () => {
    it("should store review body and event", () => {
      buffer.setReviewMetadata("Great changes!", "APPROVE");

      expect(buffer.hasReviewMetadata()).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("event=APPROVE"));
    });
  });

  describe("setReviewContext", () => {
    it("should set context on first call and return true", () => {
      const ctx = {
        repo: "test-owner/test-repo",
        repoParts: { owner: "test-owner", repo: "test-repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      };

      const result = buffer.setReviewContext(ctx);

      expect(result).toBe(true);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("test-owner/test-repo#42"));
    });

    it("should not override context on subsequent calls and return false", () => {
      const ctx1 = {
        repo: "owner/repo1",
        repoParts: { owner: "owner", repo: "repo1" },
        pullRequestNumber: 1,
        pullRequest: { head: { sha: "sha1" } },
      };

      const ctx2 = {
        repo: "owner/repo2",
        repoParts: { owner: "owner", repo: "repo2" },
        pullRequestNumber: 2,
        pullRequest: { head: { sha: "sha2" } },
      };

      const result1 = buffer.setReviewContext(ctx1);
      const result2 = buffer.setReviewContext(ctx2);

      expect(result1).toBe(true);
      expect(result2).toBe(false);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("owner/repo1#1"));
      expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("owner/repo2#2"));
    });
  });

  describe("getReviewContext", () => {
    it("should return null when no context set", () => {
      expect(buffer.getReviewContext()).toBeNull();
    });

    it("should return the set context", () => {
      const ctx = {
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 1,
        pullRequest: { head: { sha: "abc" } },
      };
      buffer.setReviewContext(ctx);
      expect(buffer.getReviewContext()).toEqual(ctx);
    });
  });

  describe("hasBufferedComments / getBufferedCount / hasReviewMetadata", () => {
    it("should return false/0 when empty", () => {
      expect(buffer.hasBufferedComments()).toBe(false);
      expect(buffer.getBufferedCount()).toBe(0);
      expect(buffer.hasReviewMetadata()).toBe(false);
    });

    it("should return true/count after adding comments", () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });

      expect(buffer.hasBufferedComments()).toBe(true);
      expect(buffer.getBufferedCount()).toBe(1);
    });

    it("should report metadata after setting it", () => {
      buffer.setReviewMetadata("body", "APPROVE");
      expect(buffer.hasReviewMetadata()).toBe(true);
    });
  });

  describe("submitReview", () => {
    it("should skip when no comments and no metadata are present", async () => {
      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);
      expect(mockGithub.rest.pulls.createReview).not.toHaveBeenCalled();
    });

    it("should skip when no review context is set", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.skipped).toBe(true);
      expect(result.reason).toContain("No review context available");
    });

    it("should fail when PR head SHA is missing", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 1,
        pullRequest: {},
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(false);
      expect(result.error).toContain("head SHA not available");
      expect(mockGithub.rest.pulls.get).not.toHaveBeenCalled();
      expect(mockGithub.rest.pulls.listReviews).not.toHaveBeenCalled();
    });

    it("should submit review with default COMMENT event when no metadata set", async () => {
      buffer.addComment({ path: "src/index.js", line: 10, body: "Fix this" });
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 100,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-100",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.event).toBe("COMMENT");
      expect(result.comment_count).toBe(1);
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledWith({
        owner: "owner",
        repo: "repo",
        pull_number: 42,
        commit_id: "abc123",
        event: "COMMENT",
        comments: [{ path: "src/index.js", line: 10, body: "Fix this" }],
      });
    });

    it("should continue when before-state capture is rate-limited", async () => {
      buffer.setReviewMetadata("Looks good", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      const rateLimitError = new Error("API rate limit exceeded for installation");
      rateLimitError.response = {
        status: 403,
        headers: {
          "x-ratelimit-remaining": "0",
          "x-ratelimit-reset": String(Math.floor(Date.now() / 1000) + 60),
        },
      };
      mockGithub.rest.pulls.get.mockRejectedValueOnce(rateLimitError);
      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 101,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-101",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.before_state).toBeUndefined();
      expect(result.after_state).toBeUndefined();
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(1);
      expect(mockGithub.rest.pulls.listReviews).not.toHaveBeenCalled();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to capture before PR review state"));
    });

    it("should retry createReview once on rate-limit errors", async () => {
      const setTimeoutSpy = vi.spyOn(global, "setTimeout").mockImplementation(handler => {
        if (typeof handler === "function") {
          handler();
        }
        return 0;
      });
      try {
        buffer.setReviewMetadata("Looks good", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        const rateLimitError = new Error("API rate limit exceeded for installation");
        rateLimitError.response = {
          status: 403,
          headers: {
            "x-ratelimit-remaining": "0",
            "retry-after": "1",
          },
        };
        mockGithub.rest.pulls.createReview.mockRejectedValueOnce(rateLimitError).mockResolvedValueOnce({
          data: {
            id: 102,
            html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-102",
          },
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
        expect(setTimeoutSpy).toHaveBeenCalledTimes(1);
        expect(setTimeoutSpy.mock.calls[0][1]).toBeGreaterThanOrEqual(1000);
        expect(setTimeoutSpy.mock.calls[0][1]).toBeLessThan(2000);
      } finally {
        setTimeoutSpy.mockRestore();
      }
    });

    it("should return success:false when createReview rate-limit retry is exhausted", async () => {
      const setTimeoutSpy = vi.spyOn(global, "setTimeout").mockImplementation(handler => {
        if (typeof handler === "function") {
          handler();
        }
        return 0;
      });
      try {
        buffer.setReviewMetadata("Looks good", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        const rateLimitError = new Error("API rate limit exceeded for installation");
        rateLimitError.response = {
          status: 403,
          headers: {
            "x-ratelimit-remaining": "0",
            "retry-after": "1",
          },
        };
        mockGithub.rest.pulls.createReview.mockRejectedValue(rateLimitError);

        const result = await buffer.submitReview();

        expect(result.success).toBe(false);
        expect(result.error).toBeDefined();
        expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
        expect(setTimeoutSpy).toHaveBeenCalledTimes(1);
      } finally {
        setTimeoutSpy.mockRestore();
      }
    });

    it("should capture before and after review state metadata", async () => {
      buffer.setReviewMetadata("Looks good", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.listReviews.mockResolvedValueOnce({ data: [{ id: 1, user: { login: "existing-reviewer" }, state: "COMMENTED" }] });
      mockGithub.rest.pulls.listReviews.mockResolvedValueOnce({
        data: [
          { id: 1, user: { login: "existing-reviewer" }, state: "COMMENTED" },
          { id: 2, user: { login: "github-actions[bot]" }, state: "COMMENTED" },
        ],
      });
      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 2,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-2",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.before_state).toEqual({
        requested_reviewers: ["existing-reviewer"],
        requested_team_reviewers: [],
        reviews: [{ id: 1, user: "existing-reviewer", state: "COMMENTED" }],
      });
      expect(result.after_state).toEqual({
        requested_reviewers: ["existing-reviewer"],
        requested_team_reviewers: [],
        reviews: [
          { id: 1, user: "existing-reviewer", state: "COMMENTED" },
          { id: 2, user: "github-actions[bot]", state: "COMMENTED" },
        ],
      });
    });

    it("should submit review with metadata when set", async () => {
      buffer.addComment({ path: "src/index.js", line: 10, body: "Fix this" });
      buffer.setReviewMetadata("Please address these issues.", "REQUEST_CHANGES");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 200,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-200",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.event).toBe("REQUEST_CHANGES");
      expect(result.review_id).toBe(200);
      expect(result.url).toBe("https://github.com/owner/repo/pull/42#pullrequestreview-200");
      expect(result.number).toBe(42);
      expect(result.metadata).toEqual({
        review_id: 200,
        review_event: "REQUEST_CHANGES",
      });

      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      expect(callArgs.event).toBe("REQUEST_CHANGES");
      expect(callArgs.body).toContain("Please address these issues.");
    });

    it("should submit body-only review when metadata set but no comments", async () => {
      buffer.setReviewMetadata("LGTM! Approving this change.", "APPROVE");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 600,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-600",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.event).toBe("APPROVE");
      expect(result.comment_count).toBe(0);

      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      expect(callArgs.event).toBe("APPROVE");
      expect(callArgs.body).toContain("LGTM! Approving this change.");
      // No comments array when empty
      expect(callArgs.comments).toBeUndefined();
    });

    it("should use GH_AW_HEAD_SHA from env instead of live PR head SHA", async () => {
      const previousHeadSHA = process.env.GH_AW_HEAD_SHA;
      process.env.GH_AW_HEAD_SHA = "original-reviewed-sha";
      try {
        buffer.setReviewMetadata("Review with pinned commit", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "live-head-sha-pushed-later" } },
        });

        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: {
            id: 700,
            html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-700",
          },
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        // The env var sha must be passed as commit_id, not the live head sha
        const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
        expect(callArgs.commit_id).toBe("original-reviewed-sha");
      } finally {
        if (previousHeadSHA !== undefined) {
          process.env.GH_AW_HEAD_SHA = previousHeadSHA;
        } else {
          delete process.env.GH_AW_HEAD_SHA;
        }
      }
    });

    it("should fall back to pullRequest.head.sha when GH_AW_HEAD_SHA is not set", async () => {
      const previousHeadSHA = process.env.GH_AW_HEAD_SHA;
      delete process.env.GH_AW_HEAD_SHA;
      try {
        buffer.setReviewMetadata("Regular review", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "current-head-sha" } },
        });

        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: {
            id: 800,
            html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-800",
          },
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
        expect(callArgs.commit_id).toBe("current-head-sha");
      } finally {
        if (previousHeadSHA !== undefined) {
          process.env.GH_AW_HEAD_SHA = previousHeadSHA;
        }
      }
    });

    it("should include multi-line comment fields with side fallback for start_side", async () => {
      buffer.addComment({
        path: "src/index.js",
        line: 15,
        body: "Multi-line issue",
        start_line: 10,
        side: "RIGHT",
      });
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 300,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-300",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);

      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // When start_side is not explicitly set, falls back to side
      expect(callArgs.comments[0]).toEqual({
        path: "src/index.js",
        line: 15,
        body: "Multi-line issue",
        start_line: 10,
        side: "RIGHT",
        start_side: "RIGHT",
      });
    });

    it("should use explicit start_side when provided", async () => {
      buffer.addComment({
        path: "src/index.js",
        line: 15,
        body: "Cross-side comment",
        start_line: 10,
        side: "RIGHT",
        start_side: "LEFT",
      });
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 300,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-300",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);

      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      expect(callArgs.comments[0]).toEqual({
        path: "src/index.js",
        line: 15,
        body: "Cross-side comment",
        start_line: 10,
        side: "RIGHT",
        start_side: "LEFT",
      });
    });

    it("should append footer when footer context is set", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("Review body", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 400,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-400",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);

      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      expect(callArgs.body).toContain("Review body");
      // Footer generated by messages_footer.cjs
      expect(callArgs.body).toContain("test-workflow");
    });

    it("should append body footer independently after the generated footer", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("Review body", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        bodyFooter: "Policy for {workflow_name}",
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: { id: 406, html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-406" },
      });
      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const body = mockGithub.rest.pulls.createReview.mock.calls[0][0].body;
      expect(body).toContain("Policy for test-workflow");
      expect(body.indexOf("Generated by")).toBeLessThan(body.indexOf("Policy for"));
    });

    it("should append workflow-call-id marker to review body when available", async () => {
      const previousCallerWorkflowId = process.env.GH_AW_CALLER_WORKFLOW_ID;
      process.env.GH_AW_CALLER_WORKFLOW_ID = "owner/repo/CallerA";
      try {
        buffer.addComment({ path: "test.js", line: 1, body: "comment" });
        buffer.setReviewMetadata("Review body", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });
        buffer.setFooterContext({
          workflowName: "test-workflow",
          runUrl: "https://github.com/owner/repo/actions/runs/123",
          workflowSource: "owner/repo/workflows/test.md@v1",
          workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
        });

        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: {
            id: 405,
            html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-405",
          },
        });

        const result = await buffer.submitReview();
        expect(result.success).toBe(true);

        const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
        expect(callArgs.body).toContain("<!-- gh-aw-workflow-call-id: owner/repo/CallerA -->");
      } finally {
        if (previousCallerWorkflowId === undefined) {
          delete process.env.GH_AW_CALLER_WORKFLOW_ID;
        } else {
          process.env.GH_AW_CALLER_WORKFLOW_ID = previousCallerWorkflowId;
        }
      }
    });

    it("should skip footer when setIncludeFooter('none') is called", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("Review body", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });
      buffer.setIncludeFooter("none");

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 401,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-401",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      expect(callArgs.body).toBe("Review body");
      expect(callArgs.body).not.toContain("test-workflow");
    });

    it("should add footer even when review body is empty", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("", "APPROVE");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 402,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-402",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // Footer should still be added to track which workflow submitted the review
      expect(callArgs.body).toContain("test-workflow");
    });

    it("should separate body from footer with a blank line for proper markdown rendering", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("Review body content", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 403,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-403",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // The footer blockquote must be preceded by at least one blank line
      // so Markdown parses the "> Generated by" quote section correctly.
      expect(callArgs.body).toMatch(/\n\n> /);
      expect(callArgs.body).not.toMatch(/[^\n]\n> /);
    });

    it("should retry with COMMENT when APPROVE is rejected on own PR", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("LGTM", "APPROVE");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" }, user: { login: "bot-user" } },
      });

      mockGithub.rest.pulls.createReview.mockRejectedValueOnce(new Error("Can not approve your own pull request")).mockResolvedValueOnce({
        data: {
          id: 700,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-700",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.event).toBe("COMMENT");
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
      const retryArgs = mockGithub.rest.pulls.createReview.mock.calls[1][0];
      expect(retryArgs.event).toBe("COMMENT");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Cannot submit APPROVE review on own PR"));
    });

    it("should retry rate-limited own-PR fallback submission", async () => {
      const setTimeoutSpy = vi.spyOn(global, "setTimeout").mockImplementation(handler => {
        if (typeof handler === "function") {
          handler();
        }
        return 0;
      });
      try {
        buffer.addComment({ path: "test.js", line: 1, body: "comment" });
        buffer.setReviewMetadata("LGTM", "APPROVE");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" }, user: { login: "bot-user" } },
        });

        const ownPrError = new Error("Can not approve your own pull request");
        const rateLimitError = new Error("API rate limit exceeded for installation");
        rateLimitError.response = {
          status: 403,
          headers: {
            "x-ratelimit-remaining": "0",
            "retry-after": "1",
          },
        };

        mockGithub.rest.pulls.createReview
          .mockRejectedValueOnce(ownPrError)
          .mockRejectedValueOnce(rateLimitError)
          .mockResolvedValueOnce({
            data: {
              id: 1700,
              html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-1700",
            },
          });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(result.event).toBe("COMMENT");
        expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(3);
        expect(setTimeoutSpy).toHaveBeenCalledTimes(1);
      } finally {
        setTimeoutSpy.mockRestore();
      }
    });

    it("should retry with COMMENT when REQUEST_CHANGES is rejected on own PR", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("Fix this", "REQUEST_CHANGES");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" }, user: { login: "bot-user" } },
      });

      mockGithub.rest.pulls.createReview.mockRejectedValueOnce(new Error("Can not request changes on your own pull request")).mockResolvedValueOnce({
        data: {
          id: 701,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-701",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.event).toBe("COMMENT");
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
      const retryArgs = mockGithub.rest.pulls.createReview.mock.calls[1][0];
      expect(retryArgs.event).toBe("COMMENT");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Cannot submit REQUEST_CHANGES review on own PR"));
    });

    it("should not retry when APPROVE succeeds (reviewer is different from PR author)", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("LGTM", "APPROVE");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" }, user: { login: "pr-author" } },
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 702,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-702",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.event).toBe("APPROVE");
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(1);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      expect(callArgs.event).toBe("APPROVE");
    });

    it("should not retry with COMMENT when event is already COMMENT and API error occurs", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("Some feedback", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" }, user: { login: "bot-user" } },
      });

      mockGithub.rest.pulls.createReview.mockRejectedValue(new Error("Validation Failed"));

      const result = await buffer.submitReview();

      expect(result.success).toBe(false);
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(1);
    });

    it("should return failure when retry also fails", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("LGTM", "APPROVE");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" }, user: { login: "bot-user" } },
      });

      mockGithub.rest.pulls.createReview.mockRejectedValueOnce(new Error("Can not approve your own pull request")).mockRejectedValueOnce(new Error("Some other error"));

      const result = await buffer.submitReview();

      expect(result.success).toBe(false);
      expect(result.error).toContain("Some other error");
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
    });

    it("should fall back to body-only COMMENT when own-PR COMMENT retry also fails with line-resolution error", async () => {
      buffer.addComment({ path: "file.go", line: 10, body: "Inline comment" });
      buffer.setReviewMetadata("Fix this", "REQUEST_CHANGES");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" }, user: { login: "bot-user" } },
      });

      mockGithub.rest.pulls.createReview
        // Initial attempt — own-PR 422
        .mockRejectedValueOnce(new Error("Can not request changes on your own pull request"))
        // COMMENT retry with inline comments — line unresolvable
        .mockRejectedValueOnce(new Error('Unprocessable Entity: "Line could not be resolved"'))
        // Body-only COMMENT fallback — success
        .mockResolvedValueOnce({
          data: {
            id: 800,
            html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-800",
          },
        });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.event).toBe("COMMENT");
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(3);
      const bodyOnlyArgs = mockGithub.rest.pulls.createReview.mock.calls[2][0];
      expect(bodyOnlyArgs.event).toBe("COMMENT");
      expect(bodyOnlyArgs.comments).toBeUndefined();
      expect(bodyOnlyArgs.body).toContain("Inline comment");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("COMMENT retry on own PR failed with unresolvable line(s)"));
    });

    it("should skip (success:true, skipped:true) when PR is permanently locked after all retries", async () => {
      const setTimeoutSpy = vi.spyOn(global, "setTimeout").mockImplementation(handler => {
        if (typeof handler === "function") {
          handler();
        }
        return 0;
      });
      try {
        buffer.setReviewMetadata("Looks good", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        const lockedError = Object.assign(new Error("lock prevents review"), { status: 422 });
        mockGithub.rest.pulls.createReview.mockRejectedValue(lockedError);

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(result.skipped).toBe(true);
        expect(result.pr_locked).toBe(true);
        expect(result.reason).toContain("locked");
        // 1 initial + 3 retries
        expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(4);
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("lock prevents review"));
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Review skipped"));
      } finally {
        setTimeoutSpy.mockRestore();
      }
    });

    it("should succeed when locked-PR retry eventually unlocks", async () => {
      const setTimeoutSpy = vi.spyOn(global, "setTimeout").mockImplementation(handler => {
        if (typeof handler === "function") {
          handler();
        }
        return 0;
      });
      try {
        buffer.setReviewMetadata("Looks good", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        const lockedError = Object.assign(new Error("lock prevents review"), { status: 422 });
        mockGithub.rest.pulls.createReview
          .mockRejectedValueOnce(lockedError)
          .mockRejectedValueOnce(lockedError)
          .mockResolvedValueOnce({
            data: {
              id: 999,
              html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-999",
            },
          });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(result.skipped).toBeUndefined();
        expect(result.review_id).toBe(999);
        // 1 initial + 2 locked retries (third attempt succeeds)
        expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(3);
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Retrying 3 time(s)"));
      } finally {
        setTimeoutSpy.mockRestore();
      }
    });

    it("should return failure when lock retry encounters a different error", async () => {
      const setTimeoutSpy = vi.spyOn(global, "setTimeout").mockImplementation(handler => {
        if (typeof handler === "function") {
          handler();
        }
        return 0;
      });
      try {
        buffer.setReviewMetadata("Looks good", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        const lockedError = Object.assign(new Error("lock prevents review"), { status: 422 });
        const otherError = Object.assign(new Error("internal server error"), { status: 500 });
        mockGithub.rest.pulls.createReview.mockRejectedValueOnce(lockedError).mockRejectedValueOnce(otherError);

        const result = await buffer.submitReview();

        expect(result.success).toBe(false);
        expect(result.error).toContain("internal server error");
        expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
      } finally {
        setTimeoutSpy.mockRestore();
      }
    });

    it("should dismiss older reviews matching workflow-call-id when supersede mode is enabled", async () => {
      const previousWorkflowId = process.env.GH_AW_WORKFLOW_ID;
      const previousCallerWorkflowId = process.env.GH_AW_CALLER_WORKFLOW_ID;
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";
      process.env.GH_AW_CALLER_WORKFLOW_ID = "owner/repo/CallerA";
      try {
        buffer.setSupersedeOlderReviews(true);
        buffer.setReviewMetadata("Updated review", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: {
            id: 900,
            html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-900",
          },
        });
        mockGithub.rest.pulls.listReviews.mockResolvedValue({
          data: [
            { id: 100, state: "CHANGES_REQUESTED", user: { login: "github-actions[bot]", type: "Bot" }, body: "<!-- gh-aw-workflow-call-id: owner/repo/CallerA -->\nOld blocking review" },
            { id: 101, state: "CHANGES_REQUESTED", user: { login: "human-user", type: "User" }, body: "<!-- gh-aw-workflow-call-id: owner/repo/CallerA -->" },
            { id: 102, state: "APPROVED", user: { login: "github-actions[bot]", type: "Bot" }, body: "<!-- gh-aw-workflow-call-id: owner/repo/CallerA -->" },
            { id: 103, state: "CHANGES_REQUESTED", user: { login: "github-actions[bot]", type: "Bot" }, body: "<!-- gh-aw-workflow-call-id: owner/repo/CallerB -->" },
            { id: 104, state: "CHANGES_REQUESTED", user: { login: "github-actions[bot]", type: "Bot" }, body: "<!-- gh-aw-workflow-id: test-workflow -->" },
          ],
        });
        mockGithub.rest.pulls.dismissReview.mockResolvedValue({ data: {} });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        // submitReview() reads reviews before superseding, during supersede
        // candidate selection, and again after review creation for after-state.
        expect(mockGithub.rest.pulls.listReviews).toHaveBeenCalledTimes(3);
        expect(mockGithub.rest.pulls.dismissReview).toHaveBeenCalledTimes(1);
        expect(mockGithub.rest.pulls.dismissReview).toHaveBeenCalledWith({
          owner: "owner",
          repo: "repo",
          pull_number: 42,
          review_id: 100,
          message: "Superseded by updated review from same workflow.",
        });
      } finally {
        if (previousWorkflowId === undefined) {
          delete process.env.GH_AW_WORKFLOW_ID;
        } else {
          process.env.GH_AW_WORKFLOW_ID = previousWorkflowId;
        }
        if (previousCallerWorkflowId === undefined) {
          delete process.env.GH_AW_CALLER_WORKFLOW_ID;
        } else {
          process.env.GH_AW_CALLER_WORKFLOW_ID = previousCallerWorkflowId;
        }
      }
    });

    it("should warn and continue when stale review dismissal fails", async () => {
      const previousWorkflowId = process.env.GH_AW_WORKFLOW_ID;
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";
      try {
        buffer.setSupersedeOlderReviews(true);
        buffer.setReviewMetadata("Updated review", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: {
            id: 901,
            html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-901",
          },
        });
        mockGithub.rest.pulls.listReviews.mockResolvedValue({
          data: [{ id: 200, state: "CHANGES_REQUESTED", user: { login: "github-actions[bot]", type: "Bot" }, body: "<!-- gh-aw-workflow-id: test-workflow -->" }],
        });
        mockGithub.rest.pulls.dismissReview.mockRejectedValue(new Error("permission denied"));

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to dismiss stale review #200"));
      } finally {
        if (previousWorkflowId === undefined) {
          delete process.env.GH_AW_WORKFLOW_ID;
        } else {
          process.env.GH_AW_WORKFLOW_ID = previousWorkflowId;
        }
      }
    });

    it("should warn and continue when stale review listing fails", async () => {
      const previousWorkflowId = process.env.GH_AW_WORKFLOW_ID;
      process.env.GH_AW_WORKFLOW_ID = "test-workflow";
      try {
        buffer.setSupersedeOlderReviews(true);
        buffer.setReviewMetadata("Updated review", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: {
            id: 902,
            html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-902",
          },
        });
        mockGithub.rest.pulls.listReviews.mockResolvedValueOnce({ data: [] }).mockRejectedValueOnce(new Error("rate limited")).mockResolvedValueOnce({ data: [] });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to supersede older reviews"));
        expect(mockGithub.rest.pulls.dismissReview).not.toHaveBeenCalled();
      } finally {
        if (previousWorkflowId === undefined) {
          delete process.env.GH_AW_WORKFLOW_ID;
        } else {
          process.env.GH_AW_WORKFLOW_ID = previousWorkflowId;
        }
      }
    });

    it("should handle API errors gracefully", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockRejectedValue(new Error("Validation Failed"));

      const result = await buffer.submitReview();

      expect(result.success).toBe(false);
      expect(result.error).toContain("Validation Failed");
    });

    it("should retry as body-only review when Line could not be resolved error occurs", async () => {
      buffer.addComment({ path: ".changeset/some-file.md", line: 1, body: "Review comment on line 1" });
      buffer.addComment({ path: ".github/workflows/ace-editor.lock.yml", line: 1, body: "Another review comment" });
      buffer.addComment({ path: "src/new_file.js", line: 42, body: "A third inline comment that should be preserved in the fallback body" });
      buffer.setReviewMetadata("Reviewed with comments.", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 21946,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockRejectedValueOnce(new Error('Unprocessable Entity: "Line could not be resolved"')).mockResolvedValueOnce({
        data: {
          id: 800,
          html_url: "https://github.com/owner/repo/pull/21946#pullrequestreview-800",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.review_id).toBe(800);
      expect(result.comment_count).toBe(0);
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
      // Second call should have no comments array
      const retryArgs = mockGithub.rest.pulls.createReview.mock.calls[1][0];
      expect(retryArgs.comments).toBeUndefined();
      expect(retryArgs.body).toContain("### Comments that could not be inline-anchored");
      expect(retryArgs.body).toContain("<details><summary>.changeset/some-file.md:1</summary>");
      expect(retryArgs.body).toContain("Review comment on line 1");
      expect(retryArgs.body).toContain("<details><summary>.github/workflows/ace-editor.lock.yml:1</summary>");
      expect(retryArgs.body).toContain("Another review comment");
      expect(retryArgs.body).toContain("<details><summary>src/new_file.js:42</summary>");
      expect(retryArgs.body).toContain("A third inline comment that should be preserved in the fallback body");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Line could not be resolved"));
    });

    it("should retry with resolvable inline comments when API identifies specific unresolvable comment indexes", async () => {
      buffer.addComment({ path: "src/valid-one.js", line: 11, body: "First resolvable inline comment" });
      buffer.addComment({ path: "src/unresolved.js", line: 99, body: "This one cannot be anchored" });
      buffer.addComment({ path: "src/valid-two.js", line: 22, body: "Second resolvable inline comment" });
      buffer.setReviewMetadata("Reviewed with comments.", "REQUEST_CHANGES");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 21946,
        pullRequest: { head: { sha: "abc123" } },
      });

      const unresolvedError = new Error('Unprocessable Entity: "Line could not be resolved"');
      // @ts-ignore - Simulate Octokit error response payload with indexed comment field.
      unresolvedError.response = { data: { errors: [{ field: "comments[1].line", message: "Line could not be resolved" }] } };

      mockGithub.rest.pulls.createReview.mockRejectedValueOnce(unresolvedError).mockResolvedValueOnce({
        data: {
          id: 803,
          html_url: "https://github.com/owner/repo/pull/21946#pullrequestreview-803",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.review_id).toBe(803);
      expect(result.comment_count).toBe(2);
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
      const retryArgs = mockGithub.rest.pulls.createReview.mock.calls[1][0];
      expect(retryArgs.comments).toHaveLength(2);
      expect(retryArgs.comments.map(comment => comment.path)).toEqual(["src/valid-one.js", "src/valid-two.js"]);
      expect(retryArgs.body).toContain("### Comments that could not be inline-anchored");
      expect(retryArgs.body).toContain("<details><summary>src/unresolved.js:99</summary>");
      expect(retryArgs.body).toContain("This one cannot be anchored");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Retrying with 2 resolvable inline comment(s)"));
    });

    it("should retry with resolvable inline comments when API identifies a specific unresolvable path index", async () => {
      buffer.addComment({ path: "src/valid-one.js", line: 11, body: "First resolvable inline comment" });
      buffer.addComment({ path: "src/missing.js", line: 99, body: "This path is not in the diff" });
      buffer.addComment({ path: "src/valid-two.js", line: 22, body: "Second resolvable inline comment" });
      buffer.setReviewMetadata("Reviewed with comments.", "REQUEST_CHANGES");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 21946,
        pullRequest: { head: { sha: "abc123" } },
      });

      const unresolvedError = new Error('Unprocessable Entity: "Path could not be resolved"');
      // @ts-ignore - Simulate Octokit error response payload with indexed comment field.
      unresolvedError.response = { data: { errors: [{ field: "comments[1].path", message: "Path could not be resolved" }] } };

      mockGithub.rest.pulls.createReview.mockRejectedValueOnce(unresolvedError).mockResolvedValueOnce({
        data: {
          id: 804,
          html_url: "https://github.com/owner/repo/pull/21946#pullrequestreview-804",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.review_id).toBe(804);
      expect(result.comment_count).toBe(2);
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
      const retryArgs = mockGithub.rest.pulls.createReview.mock.calls[1][0];
      expect(retryArgs.comments).toHaveLength(2);
      expect(retryArgs.comments.map(comment => comment.path)).toEqual(["src/valid-one.js", "src/valid-two.js"]);
      expect(retryArgs.body).toContain("### Comments that could not be inline-anchored");
      expect(retryArgs.body).toContain("<details><summary>src/missing.js:99</summary>");
      expect(retryArgs.body).toContain("This path is not in the diff");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Retrying with 2 resolvable inline comment(s)"));
    });

    it("should fall back to body-only with all comments when partial-anchor retry also fails", async () => {
      buffer.addComment({ path: "src/valid-one.js", line: 11, body: "First resolvable inline comment" });
      buffer.addComment({ path: "src/unresolved.js", line: 99, body: "This one cannot be anchored" });
      buffer.addComment({ path: "src/valid-two.js", line: 22, body: "Second resolvable inline comment" });
      buffer.setReviewMetadata("Reviewed with partial failures.", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 21946,
        pullRequest: { head: { sha: "abc123" } },
      });

      const unresolvedError = new Error('Unprocessable Entity: "Line could not be resolved"');
      // @ts-ignore - Simulate Octokit error response payload with indexed comment field.
      unresolvedError.response = { data: { errors: [{ field: "comments[1].line", message: "Line could not be resolved" }] } };

      mockGithub.rest.pulls.createReview
        .mockRejectedValueOnce(unresolvedError)
        .mockRejectedValueOnce(new Error("Partial retry also failed"))
        .mockResolvedValueOnce({
          data: {
            id: 805,
            html_url: "https://github.com/owner/repo/pull/21946#pullrequestreview-805",
          },
        });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.review_id).toBe(805);
      expect(result.comment_count).toBe(0);
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(3);
      // Third call (body-only fallback) must have no comments
      const bodyOnlyArgs = mockGithub.rest.pulls.createReview.mock.calls[2][0];
      expect(bodyOnlyArgs.comments).toBeUndefined();
      // All three original comments must appear in the fallback body
      expect(bodyOnlyArgs.body).toContain("### Comments that could not be inline-anchored");
      expect(bodyOnlyArgs.body).toContain("src/valid-one.js");
      expect(bodyOnlyArgs.body).toContain("First resolvable inline comment");
      expect(bodyOnlyArgs.body).toContain("src/unresolved.js");
      expect(bodyOnlyArgs.body).toContain("This one cannot be anchored");
      expect(bodyOnlyArgs.body).toContain("src/valid-two.js");
      expect(bodyOnlyArgs.body).toContain("Second resolvable inline comment");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to submit partially anchored PR review"));
    });

    it("should retry as body-only review when Path could not be resolved error occurs", async () => {
      buffer.addComment({ path: ".changeset/some-file.md", line: 1, body: "Review comment on line 1" });
      buffer.addComment({ path: "src/new_file.js", line: 42, body: "A second inline comment" });
      buffer.setReviewMetadata("Reviewed with comments.", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 21946,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockRejectedValueOnce(new Error('Unprocessable Entity: "Path could not be resolved"')).mockResolvedValueOnce({
        data: {
          id: 801,
          html_url: "https://github.com/owner/repo/pull/21946#pullrequestreview-801",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.review_id).toBe(801);
      expect(result.comment_count).toBe(0);
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
      // Second call should have no comments array
      const retryArgs = mockGithub.rest.pulls.createReview.mock.calls[1][0];
      expect(retryArgs.comments).toBeUndefined();
      expect(retryArgs.body).toContain("### Comments that could not be inline-anchored");
      expect(retryArgs.body).toContain("<details><summary>.changeset/some-file.md:1</summary>");
      expect(retryArgs.body).toContain("Review comment on line 1");
      expect(retryArgs.body).toContain("<details><summary>src/new_file.js:42</summary>");
      expect(retryArgs.body).toContain("A second inline comment");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Path could not be resolved"));
    });

    it("should return failure when body-only retry also fails after Path could not be resolved", async () => {
      buffer.addComment({ path: "some-file.md", line: 1, body: "Review comment" });
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockRejectedValueOnce(new Error("Path could not be resolved")).mockRejectedValueOnce(new Error("Some other error on retry"));

      const result = await buffer.submitReview();

      expect(result.success).toBe(false);
      expect(result.error).toContain("Some other error on retry");
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
    });

    it("should return failure when body-only retry also fails after Line could not be resolved", async () => {
      buffer.addComment({ path: "some-file.md", line: 1, body: "Review comment" });
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockRejectedValueOnce(new Error("Line could not be resolved")).mockRejectedValueOnce(new Error("Some other error on retry"));

      const result = await buffer.submitReview();

      expect(result.success).toBe(false);
      expect(result.error).toContain("Some other error on retry");
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(2);
    });

    it("should retry body-only as COMMENT when body-only REQUEST_CHANGES is rejected on own PR after line-resolution failure", async () => {
      buffer.addComment({ path: "file.go", line: 5, body: "Nil dereference on this line" });
      buffer.setReviewMetadata("Fix the blocking issues.", "REQUEST_CHANGES");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 47375,
        pullRequest: { head: { sha: "abc123" }, user: { login: "linter-miner[bot]" } },
      });

      mockGithub.rest.pulls.createReview
        // Initial attempt — line resolution fails (this triggers the body-only path)
        .mockRejectedValueOnce(new Error('Unprocessable Entity: "Line could not be resolved"'))
        // Body-only REQUEST_CHANGES — own-PR 422
        .mockRejectedValueOnce(new Error("Can not request changes on your own pull request"))
        // Body-only COMMENT retry — success
        .mockResolvedValueOnce({
          data: {
            id: 900,
            html_url: "https://github.com/owner/repo/pull/47375#pullrequestreview-900",
          },
        });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.event).toBe("COMMENT");
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(3);
      const bodyOnlyCommentArgs = mockGithub.rest.pulls.createReview.mock.calls[2][0];
      expect(bodyOnlyCommentArgs.event).toBe("COMMENT");
      expect(bodyOnlyCommentArgs.comments).toBeUndefined();
      expect(bodyOnlyCommentArgs.body).toContain("Nil dereference on this line");
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Body-only REQUEST_CHANGES review rejected on own PR"));
    });

    it("should return failure when body-only COMMENT retry also fails on own PR", async () => {
      buffer.addComment({ path: "file.go", line: 5, body: "Review comment" });
      buffer.setReviewMetadata("Feedback.", "REQUEST_CHANGES");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview
        .mockRejectedValueOnce(new Error("Line could not be resolved"))
        .mockRejectedValueOnce(new Error("Can not request changes on your own pull request"))
        .mockRejectedValueOnce(new Error("Unexpected server error"));

      const result = await buffer.submitReview();

      expect(result.success).toBe(false);
      expect(result.error).toContain("Unexpected server error");
      expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(3);
    });

    it("should escape HTML-sensitive characters in fallback summary and body", async () => {
      buffer.addComment({
        path: "src/<unsafe>&\"'.js",
        line: 9,
        body: "unsafe </summary><b>tag</b> & \"quote\" 'single'",
      });
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockRejectedValueOnce(new Error("Line could not be resolved")).mockResolvedValueOnce({
        data: {
          id: 801,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-801",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const retryArgs = mockGithub.rest.pulls.createReview.mock.calls[1][0];
      expect(retryArgs.body).toContain("src/&lt;unsafe&gt;&amp;&quot;&#39;.js:9");
      expect(retryArgs.body).toContain("&lt;b&gt;tag&lt;/b&gt;");
      expect(retryArgs.body).toContain("&amp; &quot;quote&quot; &#39;single&#39;");
      expect(retryArgs.body).not.toContain("</summary><b>tag</b>");
    });

    it("should avoid appending large inline bodies when fallback has no excerpt budget", async () => {
      for (let i = 0; i < 8; i++) {
        buffer.addComment({ path: `src/file-${i}.js`, line: i + 1, body: `comment-${i}-` + "z".repeat(600) });
      }
      buffer.setReviewMetadata("x".repeat(64980), "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockRejectedValueOnce(new Error("Line could not be resolved")).mockResolvedValueOnce({
        data: {
          id: 802,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-802",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const retryArgs = mockGithub.rest.pulls.createReview.mock.calls[1][0];
      expect(retryArgs.body.length).toBeLessThanOrEqual(65000);
      expect(retryArgs.body).not.toContain("comment-0-");
      expect(
        retryArgs.body.includes("_(empty comment body)_") ||
          retryArgs.body.includes("_(Unanchored comment details omitted to fit GitHub length limits.)_") ||
          retryArgs.body.includes("_(Fallback review body truncated to fit GitHub length limits.)_")
      ).toBe(true);
    });

    it("should submit multiple comments in a single review", async () => {
      buffer.addComment({ path: "file1.js", line: 5, body: "Comment 1" });
      buffer.addComment({ path: "file2.js", line: 10, body: "Comment 2" });
      buffer.addComment({ path: "file3.js", line: 15, body: "Comment 3" });
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 500,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-500",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      expect(result.comment_count).toBe(3);

      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      expect(callArgs.comments).toHaveLength(3);
    });

    describe("Sub-pattern A: empty review guard", () => {
      it("should return failure when review metadata has empty body and no comments", async () => {
        buffer.setReviewMetadata("", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(false);
        expect(result.error).toContain("Empty review");
        expect(result.error).toContain("Skipping POST to avoid 422");
        expect(mockGithub.rest.pulls.createReview).not.toHaveBeenCalled();
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Empty review"));
      });

      it("should NOT block review when body is whitespace-only (truthy string passes guard)", async () => {
        buffer.setReviewMetadata("   \n  ", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });
        // Whitespace body is truthy so guard should NOT trigger; POST proceeds.

        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: { id: 999, html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-999" },
        });

        const result = await buffer.submitReview();

        // Whitespace body is truthy so should still POST (GitHub may accept or reject it)
        expect(result.success).toBe(true);
        expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(1);
      });

      it("should return failure when metadata has no body and footerContext is null (body stays empty)", async () => {
        // Simulate Sub-pattern A from the issue: event=COMMENT, comments=0, bodyLength=0
        buffer.setReviewMetadata("", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });
        // footerContext is NOT set → no footer added → body remains ""

        const result = await buffer.submitReview();

        expect(result.success).toBe(false);
        expect(result.error).toContain("Empty review");
        expect(mockGithub.rest.pulls.createReview).not.toHaveBeenCalled();
      });

      it("should NOT guard when body is empty but comments are present", async () => {
        buffer.addComment({ path: "src/main.js", line: 5, body: "Missing null check" });
        buffer.setReviewMetadata("", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: { id: 701, html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-701" },
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(1);
      });

      it("should NOT guard when body is non-empty but no comments are present", async () => {
        buffer.setReviewMetadata("LGTM! Ship it.", "APPROVE");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: { id: 702, html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-702" },
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(mockGithub.rest.pulls.createReview).toHaveBeenCalledTimes(1);
      });
    });

    describe("Sub-pattern B: path validation against PR diff", () => {
      it("should filter out comments at paths not in the PR diff", async () => {
        buffer.addComment({ path: "src/valid.js", line: 10, body: "Valid comment" });
        buffer.addComment({ path: "src/not-in-diff.js", line: 5, body: "Invalid path" });
        buffer.setReviewMetadata("Code review", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        mockGithub.rest.pulls.listFiles.mockResolvedValue({
          data: [{ filename: "src/valid.js" }, { filename: "README.md" }],
        });
        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: { id: 800, html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-800" },
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(result.comment_count).toBe(1);
        const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
        expect(callArgs.comments).toHaveLength(1);
        expect(callArgs.comments[0].path).toBe("src/valid.js");
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("src/not-in-diff.js"));
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("path not found in PR"));
      });

      it("should return failure when all comment paths are outside the PR diff and body is empty", async () => {
        buffer.addComment({ path: "unrelated/file.js", line: 1, body: "This won't post" });
        buffer.setReviewMetadata("", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        mockGithub.rest.pulls.listFiles.mockResolvedValue({
          data: [{ filename: "src/main.js" }],
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(false);
        expect(result.error).toContain("Empty review");
        expect(result.error).toContain("all comment paths were outside the PR diff");
        expect(mockGithub.rest.pulls.createReview).not.toHaveBeenCalled();
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("unrelated/file.js"));
      });

      it("should proceed without filtering when listFiles returns an empty array", async () => {
        buffer.addComment({ path: "any/path.js", line: 1, body: "Comment" });
        buffer.setReviewMetadata("Review", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        // Default mock: listFiles returns { data: [] } → changedPaths.size === 0 → no filtering
        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: { id: 801, html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-801" },
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(result.comment_count).toBe(1);
        // No warnings about path filtering
        expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("path not found in PR"));
      });

      it("should proceed without filtering when listFiles API call fails", async () => {
        buffer.addComment({ path: "any/path.js", line: 1, body: "Comment" });
        buffer.setReviewMetadata("Review", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        mockGithub.rest.pulls.listFiles.mockRejectedValue(new Error("API rate limit exceeded"));
        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: { id: 802, html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-802" },
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(result.comment_count).toBe(1);
        expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to validate comment paths against PR diff"));
      });

      it("should handle paginated listFiles correctly", async () => {
        buffer.addComment({ path: "page2/file.js", line: 1, body: "Comment on page 2 file" });
        buffer.setReviewMetadata("Review", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        // First page returns 100 files (full page → trigger next page fetch)
        const page1Files = Array.from({ length: 100 }, (_, i) => ({ filename: `page1/file${i}.js` }));
        // Second page returns the file we want
        const page2Files = [{ filename: "page2/file.js" }];
        mockGithub.rest.pulls.listFiles.mockResolvedValueOnce({ data: page1Files }).mockResolvedValueOnce({ data: page2Files });
        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: { id: 803, html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-803" },
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(result.comment_count).toBe(1);
        expect(mockGithub.rest.pulls.listFiles).toHaveBeenCalledTimes(2);
        // No warning about invalid paths
        expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("path not found in PR"));
      });

      it("should accept comments targeting a renamed file's previous path", async () => {
        buffer.addComment({ path: "old/path.js", line: 1, body: "Comment on old path" });
        buffer.setReviewMetadata("Review", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        // File was renamed; both filename and previous_filename appear in the API response
        mockGithub.rest.pulls.listFiles.mockResolvedValue({
          data: [{ filename: "new/path.js", previous_filename: "old/path.js" }],
        });
        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: { id: 804, html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-804" },
        });

        const result = await buffer.submitReview();

        expect(result.success).toBe(true);
        expect(result.comment_count).toBe(1);
        // The old path must NOT be flagged as invalid
        expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("path not found in PR"));
      });

      it("should skip path filtering and keep all comments when the pagination cap is reached with a full last page", async () => {
        buffer.addComment({ path: "file-beyond-cap.js", line: 1, body: "Comment on file past cap" });
        buffer.setReviewMetadata("Review", "COMMENT");
        buffer.setReviewContext({
          repo: "owner/repo",
          repoParts: { owner: "owner", repo: "repo" },
          pullRequestNumber: 42,
          pullRequest: { head: { sha: "abc123" } },
        });

        // Simulate 10 full pages of 100 files each — the loop exits because of the cap,
        // not because the last page was partial.
        const fullPage = Array.from({ length: 100 }, (_, i) => ({ filename: `page/file${i}.js` }));
        // All 10 pages return full results
        for (let i = 0; i < 10; i++) {
          mockGithub.rest.pulls.listFiles.mockResolvedValueOnce({ data: fullPage });
        }
        mockGithub.rest.pulls.createReview.mockResolvedValue({
          data: { id: 805, html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-805" },
        });

        const result = await buffer.submitReview();

        // Cap reached with full page → fail-open → no filtering → comment is kept
        expect(result.success).toBe(true);
        expect(result.comment_count).toBe(1);
        expect(mockGithub.rest.pulls.listFiles).toHaveBeenCalledTimes(10);
        // No "path not found" warning because filtering was skipped
        expect(mockCore.warning).not.toHaveBeenCalledWith(expect.stringContaining("path not found in PR"));
      });
    });
  }); // closes submitReview describe

  describe("reset", () => {
    it("should clear all state including footer mode", () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("body", "APPROVE");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 1,
        pullRequest: { head: { sha: "abc" } },
      });
      buffer.setFooterMode("none");

      buffer.reset();

      expect(buffer.hasBufferedComments()).toBe(false);
      expect(buffer.getBufferedCount()).toBe(0);
      expect(buffer.hasReviewMetadata()).toBe(false);
      expect(buffer.getReviewContext()).toBeNull();

      // After reset, footer should be "always" (default)
      // Verify by submitting a review with footer context and checking body
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("Review after reset", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 1,
        pullRequest: { head: { sha: "abc" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: { id: 600, html_url: "https://github.com/test" },
      });

      return buffer.submitReview().then(result => {
        expect(result.success).toBe(true);
        const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
        // Footer should be included since footer mode was reset to "always"
        expect(callArgs.body).toContain("test-workflow");
      });
    });
  });

  describe("footer mode", () => {
    it("should support 'always' mode (default)", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("", "APPROVE"); // Empty body
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });
      buffer.setFooterMode("always");

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 500,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-500",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // Footer should be included even with empty body
      expect(callArgs.body).toContain("test-workflow");
    });

    it("should support 'none' mode", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("Review body", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });
      buffer.setFooterMode("none");

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 501,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-501",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // Footer should not be included
      expect(callArgs.body).toBe("Review body");
      expect(callArgs.body).not.toContain("test-workflow");
    });

    it("should support 'if-body' mode with non-empty body", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("Review body", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });
      buffer.setFooterMode("if-body");

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 502,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-502",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // Footer should be included because body is non-empty
      expect(callArgs.body).toContain("Review body");
      expect(callArgs.body).toContain("test-workflow");
    });

    it("should support 'if-body' mode with empty body", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("", "APPROVE"); // Empty body
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });
      buffer.setFooterMode("if-body");

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 503,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-503",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // Footer should NOT be included because body is empty
      // Body should be undefined (not included in API call) when empty
      expect(callArgs.body).toBeUndefined();
      expect(callArgs.body || "").not.toContain("test-workflow");
    });

    it("should support 'if-body' mode with whitespace-only body", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("   \n  ", "APPROVE"); // Whitespace-only body
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });
      buffer.setFooterMode("if-body");

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 504,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-504",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // Footer should NOT be included because body is whitespace-only (trimmed length is 0)
      // Original whitespace body is preserved in the API call
      expect(callArgs.body).toBe("   \n  ");
      expect(callArgs.body).not.toContain("test-workflow");
    });

    it("should normalize boolean false to 'none' mode", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("Review body", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });
      buffer.setFooterMode(false);

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 505,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-505",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // Footer should NOT be included because false maps to "none"
      expect(callArgs.body).toBe("Review body");
      expect(callArgs.body).not.toContain("test-workflow");
      // Verify normalization was logged
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining('Normalized boolean footer config (false) to mode: "none"'));
    });

    it("should normalize boolean true to 'always' mode", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("", "APPROVE"); // Empty body
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });
      buffer.setFooterMode(true);

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 506,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-506",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // Footer should be included because true maps to "always"
      expect(callArgs.body).toContain("test-workflow");
      // Verify normalization was logged
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining('Normalized boolean footer config (true) to mode: "always"'));
    });

    it("should handle setIncludeFooter(false) backward compatibility", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("Review body", "COMMENT");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });
      // Use the backward-compatible alias with boolean (the original API contract)
      buffer.setIncludeFooter(false);

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 508,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-508",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // Footer should NOT be included because false maps to "none"
      expect(callArgs.body).toBe("Review body");
      expect(callArgs.body).not.toContain("test-workflow");
    });

    it("should default to 'always' for invalid string mode", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("", "APPROVE");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 42,
        pullRequest: { head: { sha: "abc123" } },
      });
      buffer.setFooterContext({
        workflowName: "test-workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
        workflowSource: "owner/repo/workflows/test.md@v1",
        workflowSourceURL: "https://github.com/owner/repo/blob/main/test.md",
      });
      buffer.setFooterMode("invalid-mode");

      mockGithub.rest.pulls.createReview.mockResolvedValue({
        data: {
          id: 507,
          html_url: "https://github.com/owner/repo/pull/42#pullrequestreview-507",
        },
      });

      const result = await buffer.submitReview();

      expect(result.success).toBe(true);
      const callArgs = mockGithub.rest.pulls.createReview.mock.calls[0][0];
      // Should default to "always" and include footer
      expect(callArgs.body).toContain("test-workflow");
    });
  });
});
