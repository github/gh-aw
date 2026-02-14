import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

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
      createReview: vi.fn(),
    },
  },
};

global.core = mockCore;
global.github = mockGithub;

const { createReviewBuffer } = require("./pr_review_buffer.cjs");

describe("pr_review_buffer (factory pattern)", () => {
  let buffer;
  let originalMessages;

  beforeEach(() => {
    vi.clearAllMocks();

    // Save and clear messages env var (generateFooterWithMessages reads this)
    originalMessages = process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
    delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;

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

    it("should fail when no review context is set", async () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });

      const result = await buffer.submitReview();

      expect(result.success).toBe(false);
      expect(result.error).toContain("No review context available");
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

    it("should skip footer when setIncludeFooter(false) is called", async () => {
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
      buffer.setIncludeFooter(false);

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
  });

  describe("reset", () => {
    it("should clear all state including includeFooter", () => {
      buffer.addComment({ path: "test.js", line: 1, body: "comment" });
      buffer.setReviewMetadata("body", "APPROVE");
      buffer.setReviewContext({
        repo: "owner/repo",
        repoParts: { owner: "owner", repo: "repo" },
        pullRequestNumber: 1,
        pullRequest: { head: { sha: "abc" } },
      });
      buffer.setIncludeFooter(false);

      buffer.reset();

      expect(buffer.hasBufferedComments()).toBe(false);
      expect(buffer.getBufferedCount()).toBe(0);
      expect(buffer.hasReviewMetadata()).toBe(false);
      expect(buffer.getReviewContext()).toBeNull();

      // After reset, footer should be re-enabled (default: true)
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
        // Footer should be included since includeFooter was reset to true
        expect(callArgs.body).toContain("test-workflow");
      });
    });
  });
});
