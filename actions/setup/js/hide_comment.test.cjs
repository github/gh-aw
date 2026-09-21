// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};

const mockGithub = {
  graphql: vi.fn(),
  rest: {
    issues: {
      getComment: vi.fn(),
    },
  },
};

const mockContext = {
  eventName: "issue_comment",
  repo: { owner: "testowner", repo: "testrepo" },
  payload: { issue: { number: 42 } },
};

global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

async function loadModule() {
  const { main } = await import("./hide_comment.cjs?" + Date.now());
  return { main };
}

describe("hide_comment.cjs", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;

    mockGithub.graphql.mockImplementation(query => {
      if (query.includes("query ($nodeId")) {
        return Promise.resolve({
          node: {
            __typename: "IssueComment",
            issue: {
              number: 42,
              repository: { nameWithOwner: "testowner/testrepo" },
            },
          },
        });
      }
      return Promise.resolve({
        minimizeComment: { minimizedComment: { isMinimized: true } },
      });
    });
    mockGithub.rest.issues.getComment.mockResolvedValue({
      data: {
        node_id: "IC_kwDOABCD123456",
        issue_url: "https://api.github.com/repos/testowner/testrepo/issues/42",
      },
    });
  });

  describe("main factory", () => {
    it("should return a handler function", async () => {
      const { main } = await loadModule();
      const handler = await main();
      expect(typeof handler).toBe("function");
    });

    it("should log configuration on initialization", async () => {
      const { main } = await loadModule();
      await main({ max: 3 });
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("max=3"));
    });

    it("should log allowed reasons when configured", async () => {
      const { main } = await loadModule();
      await main({ allowed_reasons: ["SPAM", "ABUSE"] });
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("SPAM, ABUSE"));
    });
  });

  describe("handleHideComment", () => {
    it("should hide a comment successfully", async () => {
      const { main } = await loadModule();
      const handler = await main();

      const result = await handler({ comment_id: "IC_kwDOABCD123456", reason: "SPAM" }, {});

      expect(result.success).toBe(true);
      expect(result.comment_id).toBe("IC_kwDOABCD123456");
      expect(result.is_hidden).toBe(true);
    });

    it("should use SPAM as default reason when not provided", async () => {
      const { main } = await loadModule();
      const handler = await main();

      await handler({ comment_id: "IC_kwDOABCD123456" }, {});

      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ classifier: "SPAM" }));
    });

    it("should normalize reason to uppercase", async () => {
      const { main } = await loadModule();
      const handler = await main();

      await handler({ comment_id: "IC_kwDOABCD123456", reason: "abuse" }, {});

      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ classifier: "ABUSE" }));
    });

    it("should pass through LOW_QUALITY reason", async () => {
      const { main } = await loadModule();
      const handler = await main();

      await handler({ comment_id: "IC_kwDOABCD123456", reason: "low_quality" }, {});

      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ classifier: "LOW_QUALITY" }));
    });

    it("should fail when comment_id is missing", async () => {
      const { main } = await loadModule();
      const handler = await main();

      const result = await handler({ reason: "SPAM" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("comment_id is required");
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });

    it("should resolve numeric comment_id to a GraphQL node ID", async () => {
      const { main } = await loadModule();
      const handler = await main();

      const result = await handler({ comment_id: 12345, reason: "SPAM" }, {});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.issues.getComment).toHaveBeenCalledWith({
        owner: "testowner",
        repo: "testrepo",
        comment_id: 12345,
      });
      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ nodeId: "IC_kwDOABCD123456" }));
      expect(result.comment_id).toBe("IC_kwDOABCD123456");
    });

    it("should resolve numeric string comment_id to a GraphQL node ID", async () => {
      const { main } = await loadModule();
      const handler = await main();

      const result = await handler({ comment_id: "12345", reason: "SPAM" }, {});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.issues.getComment).toHaveBeenCalledWith({
        owner: "testowner",
        repo: "testrepo",
        comment_id: 12345,
      });
      expect(result.comment_id).toBe("IC_kwDOABCD123456");
    });

    it("should fail when comment_id is an empty string", async () => {
      const { main } = await loadModule();
      const handler = await main();

      const result = await handler({ comment_id: "", reason: "SPAM" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("comment_id is required");
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });

    it("should fail when comment_id is a whitespace-only string", async () => {
      const { main } = await loadModule();
      const handler = await main();

      const result = await handler({ comment_id: "   ", reason: "SPAM" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("comment_id is required");
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });

    it("should fail for invalid numeric comment_id", async () => {
      const { main } = await loadModule();
      const handler = await main();

      const result = await handler({ comment_id: 0, reason: "SPAM" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toBe("ERR_VALIDATION: comment_id must be a GraphQL node ID string or a positive numeric REST comment ID");
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });

    it("should fail when repo context is unavailable for numeric comment_id", async () => {
      global.context = { eventName: "issue_comment", payload: {} };
      const { main } = await loadModule();
      const handler = await main();

      const result = await handler({ comment_id: 12345, reason: "SPAM" }, {});

      global.context = mockContext;

      expect(result.success).toBe(false);
      expect(result.error).toContain("repository context");
      expect(mockGithub.rest.issues.getComment).not.toHaveBeenCalled();
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });

    it("should reject a discussion comment for target: triggering when triggered by an issue with the same number", async () => {
      // Issue #42 triggered the run, but the resolved comment belongs to discussion #42
      // (discussions use a separate numbering sequence from issues/PRs).
      mockGithub.graphql.mockImplementation(query => {
        if (query.includes("query ($nodeId")) {
          return Promise.resolve({
            node: {
              __typename: "DiscussionComment",
              discussion: {
                number: 42,
                repository: { nameWithOwner: "testowner/testrepo" },
              },
            },
          });
        }
        return Promise.resolve({
          minimizeComment: { minimizedComment: { isMinimized: true } },
        });
      });

      const { main } = await loadModule();
      const handler = await main({ target: "triggering" });

      const result = await handler({ comment_id: "IC_someDiscussionCommentId", reason: "SPAM" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("discussion");
    });

    it("should enforce max count limit", async () => {
      const { main } = await loadModule();
      const handler = await main({ max: 2 });

      await handler({ comment_id: "IC_1" }, {});
      await handler({ comment_id: "IC_2" }, {});
      const result = await handler({ comment_id: "IC_3" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Max count");
      expect(mockGithub.graphql).toHaveBeenCalledTimes(4);
    });

    it("should reject reason not in allowed-reasons list", async () => {
      const { main } = await loadModule();
      const handler = await main({ allowed_reasons: ["SPAM", "ABUSE"] });

      const result = await handler({ comment_id: "IC_kwDOABCD123456", reason: "OFF_TOPIC" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("not in allowed-reasons list");
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });

    it("should accept allowed reason case-insensitively", async () => {
      const { main } = await loadModule();
      const handler = await main({ allowed_reasons: ["spam", "abuse"] });

      const result = await handler({ comment_id: "IC_kwDOABCD123456", reason: "SPAM" }, {});

      expect(result.success).toBe(true);
    });

    it("should handle GraphQL API errors gracefully", async () => {
      const { main } = await loadModule();
      mockGithub.graphql.mockRejectedValue(new Error("GraphQL error: Forbidden"));
      const handler = await main();

      const result = await handler({ comment_id: "IC_kwDOABCD123456" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("GraphQL error");
      expect(mockCore.error).toHaveBeenCalled();
    });

    it("should return failure when comment is not minimized", async () => {
      const { main } = await loadModule();
      mockGithub.graphql.mockImplementation(query => {
        if (query.includes("query ($nodeId")) {
          return Promise.resolve({
            node: {
              __typename: "IssueComment",
              issue: {
                number: 42,
                repository: { nameWithOwner: "testowner/testrepo" },
              },
            },
          });
        }
        return Promise.resolve({
          minimizeComment: { minimizedComment: { isMinimized: false } },
        });
      });
      const handler = await main();

      const result = await handler({ comment_id: "IC_kwDOABCD123456" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Failed to hide comment");
    });

    it("should return staged preview in staged mode", async () => {
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
      const { main } = await loadModule();
      const handler = await main();

      const result = await handler({ comment_id: "IC_kwDOABCD123456", reason: "ABUSE" }, {});

      expect(result.success).toBe(true);
      expect(result.staged).toBe(true);
      expect(result.previewInfo?.commentId).toBe("IC_kwDOABCD123456");
      expect(result.previewInfo?.reason).toBe("ABUSE");
      expect(mockGithub.graphql).toHaveBeenCalledTimes(1);
      expect(mockGithub.graphql).not.toHaveBeenCalledWith(expect.stringContaining("minimizeComment"), expect.any(Object));
    });
  });
});
