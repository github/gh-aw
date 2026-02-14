// @ts-check
import { describe, it, expect, beforeEach } from "vitest";
const { main } = require("./close_pull_request.cjs");

describe("close_pull_request", () => {
  let mockCore;
  let mockGithub;
  let mockContext;

  beforeEach(() => {
    // Reset mocks before each test
    mockCore = {
      info: () => {},
      warning: () => {},
      error: () => {},
      messages: [],
      infos: [],
      warnings: [],
      errors: [],
    };

    // Capture all logged messages
    mockCore.info = msg => {
      mockCore.infos.push(msg);
      mockCore.messages.push({ level: "info", message: msg });
    };
    mockCore.warning = msg => {
      mockCore.warnings.push(msg);
      mockCore.messages.push({ level: "warning", message: msg });
    };
    mockCore.error = msg => {
      mockCore.errors.push(msg);
      mockCore.messages.push({ level: "error", message: msg });
    };

    mockGithub = {
      rest: {
        pulls: {
          get: async ({ owner, repo, pull_number }) => ({
            data: {
              number: pull_number,
              title: "Test PR",
              labels: [{ name: "bug" }],
              html_url: `https://github.com/${owner}/${repo}/pull/${pull_number}`,
              state: "open",
            },
          }),
          update: async ({ owner, repo, pull_number }) => ({
            data: {
              number: pull_number,
              title: "Test PR",
              html_url: `https://github.com/${owner}/${repo}/pull/${pull_number}`,
            },
          }),
        },
        issues: {
          createComment: async () => ({
            data: {
              id: 456,
              html_url: "https://github.com/test-owner/test-repo/pull/1#issuecomment-456",
            },
          }),
        },
      },
    };

    mockContext = {
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
      payload: {
        pull_request: {
          number: 456,
        },
      },
    };

    // Set globals
    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;
  });

  describe("main factory", () => {
    it("should create a handler function with default configuration", async () => {
      const handler = await main();
      expect(typeof handler).toBe("function");
    });

    it("should create a handler function with custom configuration", async () => {
      const handler = await main({
        required_labels: ["bug"],
        required_title_prefix: "[bot]",
        max: 5,
      });
      expect(typeof handler).toBe("function");
    });

    it("should log configuration on initialization", async () => {
      await main({
        required_labels: ["bug", "automated"],
        required_title_prefix: "[bot]",
        max: 3,
      });
      expect(mockCore.infos.some(msg => msg.includes("max=3"))).toBe(true);
      expect(mockCore.infos.some(msg => msg.includes("bug, automated"))).toBe(true);
      expect(mockCore.infos.some(msg => msg.includes("[bot]"))).toBe(true);
    });
  });

  describe("handleClosePullRequest", () => {
    it("should close a pull request using explicit pull_request_number", async () => {
      const handler = await main({ max: 10 });
      const updateCalls = [];

      mockGithub.rest.pulls.update = async params => {
        updateCalls.push(params);
        return {
          data: {
            number: params.pull_number,
            title: "Test PR",
            html_url: `https://github.com/${params.owner}/${params.repo}/pull/${params.pull_number}`,
          },
        };
      };

      const result = await handler(
        {
          pull_request_number: 789,
          body: "Closing PR",
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.pull_request_number).toBe(789);
      expect(updateCalls.length).toBe(1);
      expect(updateCalls[0].pull_number).toBe(789);
      expect(updateCalls[0].state).toBe("closed");
    });

    it("should close a PR from context when pull_request_number not provided", async () => {
      const handler = await main({ max: 10 });
      const updateCalls = [];

      mockGithub.rest.pulls.update = async params => {
        updateCalls.push(params);
        return {
          data: {
            number: params.pull_number,
            title: "Test PR",
            html_url: `https://github.com/${params.owner}/${params.repo}/pull/${params.pull_number}`,
          },
        };
      };

      const result = await handler({ body: "Closing PR" }, {});

      expect(result.success).toBe(true);
      expect(result.pull_request_number).toBe(456);
      expect(updateCalls[0].owner).toBe("test-owner");
      expect(updateCalls[0].repo).toBe("test-repo");
    });

    it("should handle invalid pull_request_number", async () => {
      const handler = await main({ max: 10 });

      const result = await handler(
        {
          pull_request_number: "invalid",
          body: "Closing",
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error.includes("Invalid pull request number")).toBe(true);
    });

    it("should handle missing pull_request_number and no context", async () => {
      mockContext.payload = {};

      const handler = await main({ max: 10 });

      const result = await handler({ body: "Closing" }, {});

      expect(result.success).toBe(false);
      expect(result.error.includes("No pull_request_number provided")).toBe(true);
    });

    it("should respect max count limit", async () => {
      const handler = await main({ max: 2 });

      // First call succeeds
      const result1 = await handler({ pull_request_number: 1, body: "Close 1" }, {});
      expect(result1.success).toBe(true);

      // Second call succeeds
      const result2 = await handler({ pull_request_number: 2, body: "Close 2" }, {});
      expect(result2.success).toBe(true);

      // Third call should fail
      const result3 = await handler({ pull_request_number: 3, body: "Close 3" }, {});
      expect(result3.success).toBe(false);
      expect(result3.error.includes("Max count")).toBe(true);
    });

    it("should use body field from message over config comment", async () => {
      const handler = await main({ max: 10, comment: "Default comment" });

      const commentCalls = [];
      mockGithub.rest.issues.createComment = async params => {
        commentCalls.push(params);
        return {
          data: {
            id: 999,
            html_url: `https://github.com/${params.owner}/${params.repo}/pull/${params.issue_number}#issuecomment-999`,
          },
        };
      };

      const result = await handler({ pull_request_number: 100, body: "Message body comment" }, {});

      expect(result.success).toBe(true);
      expect(commentCalls.length).toBe(1);
      expect(commentCalls[0].body).toContain("Message body comment");
    });

    it("should require body field when no config comment is set", async () => {
      const handler = await main({ max: 10 });

      const result = await handler({ pull_request_number: 100 }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("No comment body provided");
    });

    it("should fail when body is empty string", async () => {
      const handler = await main({ max: 10 });

      const result = await handler({ pull_request_number: 100, body: "" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("No comment body provided");
    });

    it("should fail when body is whitespace-only and fall back to config comment", async () => {
      const handler = await main({ max: 10, comment: "Default close comment" });

      const commentCalls = [];
      mockGithub.rest.issues.createComment = async params => {
        commentCalls.push(params);
        return {
          data: {
            id: 999,
            html_url: `https://github.com/${params.owner}/${params.repo}/pull/${params.issue_number}#issuecomment-999`,
          },
        };
      };

      const result = await handler({ pull_request_number: 100, body: "   \n\t  " }, {});

      expect(result.success).toBe(true);
      expect(commentCalls.length).toBe(1);
      expect(commentCalls[0].body).toContain("Default close comment");
    });

    it("should fail when body is whitespace-only and no config comment", async () => {
      const handler = await main({ max: 10 });

      const result = await handler({ pull_request_number: 100, body: "   " }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("No comment body provided");
    });

    it("should still add comment to already closed PRs", async () => {
      const handler = await main({ max: 10 });

      let commentAdded = false;
      let prUpdateCalled = false;

      mockGithub.rest.pulls.get = async () => ({
        data: {
          number: 100,
          title: "Test PR",
          labels: [],
          html_url: "https://github.com/test-owner/test-repo/pull/100",
          state: "closed",
        },
      });

      mockGithub.rest.issues.createComment = async () => {
        commentAdded = true;
        return {
          data: {
            id: 789,
            html_url: "https://github.com/test-owner/test-repo/pull/100#issuecomment-789",
          },
        };
      };

      mockGithub.rest.pulls.update = async () => {
        prUpdateCalled = true;
        return {
          data: {
            number: 100,
            title: "Test PR",
            html_url: "https://github.com/test-owner/test-repo/pull/100",
          },
        };
      };

      const result = await handler({ pull_request_number: 100, body: "Test comment" }, {});

      expect(result.success).toBe(true);
      expect(result.alreadyClosed).toBe(true);
      expect(commentAdded).toBe(true);
      expect(prUpdateCalled).toBe(false); // Should not call update for already closed PR
    });

    it("should track comment posting status", async () => {
      const handler = await main({ max: 10 });

      const result = await handler({ pull_request_number: 100, body: "Closing comment" }, {});

      expect(result.success).toBe(true);
      expect(result.commentPosted).toBe(true);
    });

    it("should continue closing even if comment fails", async () => {
      const handler = await main({ max: 10 });

      mockGithub.rest.issues.createComment = async () => {
        throw new Error("Comment API error");
      };

      const result = await handler({ pull_request_number: 100, body: "Comment that fails" }, {});

      expect(result.success).toBe(true);
      expect(result.commentPosted).toBe(false);
      expect(mockCore.errors.some(msg => msg.includes("Failed to add comment"))).toBe(true);
    });

    it("should validate required labels", async () => {
      const handler = await main({
        required_labels: ["bug", "automated"],
        max: 10,
      });

      mockGithub.rest.pulls.get = async () => ({
        data: {
          number: 100,
          title: "Test PR",
          labels: [{ name: "bug" }], // Missing "automated" label - but has at least one required label (bug)
          html_url: "https://github.com/test-owner/test-repo/pull/100",
          state: "open",
        },
      });

      const result = await handler({ pull_request_number: 100, body: "Close" }, {});

      // The checkLabelFilter uses "some" so it passes if ANY required label matches
      // To fail, we need NO matching labels
      expect(result.success).toBe(true);
    });

    it("should fail validation when no required labels match", async () => {
      const handler = await main({
        required_labels: ["bug", "automated"],
        max: 10,
      });

      mockGithub.rest.pulls.get = async () => ({
        data: {
          number: 100,
          title: "Test PR",
          labels: [{ name: "enhancement" }], // No matching required labels
          html_url: "https://github.com/test-owner/test-repo/pull/100",
          state: "open",
        },
      });

      const result = await handler({ pull_request_number: 100, body: "Close" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("does not match required labels");
    });

    it("should validate required title prefix", async () => {
      const handler = await main({
        required_title_prefix: "[bot]",
        max: 10,
      });

      mockGithub.rest.pulls.get = async () => ({
        data: {
          number: 100,
          title: "Test PR", // Missing "[bot]" prefix
          labels: [],
          html_url: "https://github.com/test-owner/test-repo/pull/100",
          state: "open",
        },
      });

      const result = await handler({ pull_request_number: 100, body: "Close" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("does not start with required prefix");
    });

    it("should add comment before closing when configured", async () => {
      const handler = await main({
        max: 10,
        comment: "This PR is being closed automatically.",
      });

      const commentCalls = [];
      mockGithub.rest.issues.createComment = async params => {
        commentCalls.push(params);
        return {
          data: {
            id: 999,
            html_url: `https://github.com/${params.owner}/${params.repo}/pull/${params.issue_number}#issuecomment-999`,
          },
        };
      };

      const result = await handler({ pull_request_number: 100, body: "Closing comment" }, {});

      expect(result.success).toBe(true);
      expect(commentCalls.length).toBe(1);
      expect(commentCalls[0].body).toContain("Closing comment");
    });

    it("should handle API errors gracefully", async () => {
      const handler = await main({ max: 10 });

      mockGithub.rest.pulls.get = async () => {
        throw new Error("API Error: Not found");
      };

      const result = await handler({ pull_request_number: 100, body: "Close" }, {});

      expect(result.success).toBe(false);
      expect(result.error.includes("API Error")).toBe(true);
    });
  });
});
