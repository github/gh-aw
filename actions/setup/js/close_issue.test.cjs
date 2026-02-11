// @ts-check
import { describe, it, expect, beforeEach } from "vitest";
const { main } = require("./close_issue.cjs");

describe("close_issue", () => {
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
        issues: {
          get: async ({ owner, repo, issue_number }) => ({
            data: {
              number: issue_number,
              title: "Test Issue",
              labels: [{ name: "bug" }],
              html_url: `https://github.com/${owner}/${repo}/issues/${issue_number}`,
              state: "open",
            },
          }),
          update: async ({ owner, repo, issue_number }) => ({
            data: {
              number: issue_number,
              title: "Test Issue",
              html_url: `https://github.com/${owner}/${repo}/issues/${issue_number}`,
            },
          }),
          createComment: async () => ({
            data: {
              id: 123,
              html_url: "https://github.com/test-owner/test-repo/issues/1#issuecomment-123",
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
        issue: {
          number: 123,
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

  describe("handleCloseIssue", () => {
    it("should close an issue using explicit issue_number", async () => {
      const handler = await main({ max: 10 });
      const updateCalls = [];

      mockGithub.rest.issues.update = async params => {
        updateCalls.push(params);
        return {
          data: {
            number: params.issue_number,
            title: "Test Issue",
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          },
        };
      };

      const result = await handler(
        {
          issue_number: 456,
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(456);
      expect(updateCalls.length).toBe(1);
      expect(updateCalls[0].issue_number).toBe(456);
      expect(updateCalls[0].state).toBe("closed");
    });

    it("should close an issue from context when issue_number not provided", async () => {
      const handler = await main({ max: 10 });
      const updateCalls = [];

      mockGithub.rest.issues.update = async params => {
        updateCalls.push(params);
        return {
          data: {
            number: params.issue_number,
            title: "Test Issue",
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          },
        };
      };

      const result = await handler({}, {});

      expect(result.success).toBe(true);
      expect(result.number).toBe(123);
      expect(updateCalls[0].owner).toBe("test-owner");
      expect(updateCalls[0].repo).toBe("test-repo");
    });

    it("should handle invalid issue_number", async () => {
      const handler = await main({ max: 10 });

      const result = await handler(
        {
          issue_number: "invalid",
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error.includes("Invalid issue number")).toBe(true);
    });

    it("should handle missing issue_number and no context", async () => {
      mockContext.payload = {};

      const handler = await main({ max: 10 });

      const result = await handler({}, {});

      expect(result.success).toBe(false);
      expect(result.error.includes("No issue number available")).toBe(true);
    });

    it("should respect max count limit", async () => {
      const handler = await main({ max: 2 });

      // First call succeeds
      const result1 = await handler({ issue_number: 1 }, {});
      expect(result1.success).toBe(true);

      // Second call succeeds
      const result2 = await handler({ issue_number: 2 }, {});
      expect(result2.success).toBe(true);

      // Third call should fail
      const result3 = await handler({ issue_number: 3 }, {});
      expect(result3.success).toBe(false);
      expect(result3.error.includes("Max count")).toBe(true);
    });

    it("should skip already closed issues", async () => {
      const handler = await main({ max: 10 });

      mockGithub.rest.issues.get = async () => ({
        data: {
          number: 100,
          title: "Test Issue",
          labels: [],
          html_url: "https://github.com/test-owner/test-repo/issues/100",
          state: "closed",
        },
      });

      const result = await handler({ issue_number: 100 }, {});

      expect(result.success).toBe(true);
      expect(result.alreadyClosed).toBe(true);
    });

    it("should validate required labels", async () => {
      const handler = await main({
        required_labels: ["bug", "automated"],
        max: 10,
      });

      mockGithub.rest.issues.get = async () => ({
        data: {
          number: 100,
          title: "Test Issue",
          labels: [{ name: "bug" }], // Missing "automated" label
          html_url: "https://github.com/test-owner/test-repo/issues/100",
          state: "open",
        },
      });

      const result = await handler({ issue_number: 100 }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Missing required labels");
      expect(result.error).toContain("automated");
    });

    it("should validate required title prefix", async () => {
      const handler = await main({
        required_title_prefix: "[bot]",
        max: 10,
      });

      mockGithub.rest.issues.get = async () => ({
        data: {
          number: 100,
          title: "Test Issue", // Missing "[bot]" prefix
          labels: [],
          html_url: "https://github.com/test-owner/test-repo/issues/100",
          state: "open",
        },
      });

      const result = await handler({ issue_number: 100 }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("doesn't start with");
      expect(result.error).toContain("[bot]");
    });

    it("should add comment before closing when configured", async () => {
      const handler = await main({
        max: 10,
        comment: "This issue is being closed automatically.",
      });

      const commentCalls = [];
      mockGithub.rest.issues.createComment = async params => {
        commentCalls.push(params);
        return {
          data: {
            id: 999,
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}#issuecomment-999`,
          },
        };
      };

      const result = await handler({ issue_number: 100 }, {});

      expect(result.success).toBe(true);
      expect(commentCalls.length).toBe(1);
      expect(commentCalls[0].body).toContain("This issue is being closed automatically");
    });

    it("should handle API errors gracefully", async () => {
      const handler = await main({ max: 10 });

      mockGithub.rest.issues.get = async () => {
        throw new Error("API Error: Not found");
      };

      const result = await handler({ issue_number: 100 }, {});

      expect(result.success).toBe(false);
      expect(result.error.includes("API Error")).toBe(true);
    });

    it("should support target-repo from config", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "external-org/external-repo",
      });
      const updateCalls = [];

      mockGithub.rest.issues.get = async params => ({
        data: {
          number: params.issue_number,
          title: "Test Issue",
          labels: [],
          html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          state: "open",
        },
      });

      mockGithub.rest.issues.update = async params => {
        updateCalls.push(params);
        return {
          data: {
            number: params.issue_number,
            title: "Test Issue",
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          },
        };
      };

      const result = await handler({ issue_number: 100 }, {});

      expect(result.success).toBe(true);
      expect(updateCalls[0].owner).toBe("external-org");
      expect(updateCalls[0].repo).toBe("external-repo");
    });

    it("should support repo field in message for cross-repository operations", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "default-org/default-repo",
        allowed_repos: ["cross-org/cross-repo"],
      });
      const updateCalls = [];

      mockGithub.rest.issues.get = async params => ({
        data: {
          number: params.issue_number,
          title: "Test Issue",
          labels: [],
          html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          state: "open",
        },
      });

      mockGithub.rest.issues.update = async params => {
        updateCalls.push(params);
        return {
          data: {
            number: params.issue_number,
            title: "Test Issue",
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          },
        };
      };

      const result = await handler(
        {
          issue_number: 456,
          repo: "cross-org/cross-repo",
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(updateCalls[0].owner).toBe("cross-org");
      expect(updateCalls[0].repo).toBe("cross-repo");
    });

    it("should reject repo not in allowed-repos list", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "default-org/default-repo",
        allowed_repos: ["allowed-org/allowed-repo"],
      });

      const result = await handler(
        {
          issue_number: 100,
          repo: "unauthorized-org/unauthorized-repo",
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("not in the allowed-repos list");
    });

    it("should qualify bare repo name with default repo org", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "github/default-repo",
        allowed_repos: ["github/gh-aw"],
      });
      const updateCalls = [];

      mockGithub.rest.issues.get = async params => ({
        data: {
          number: params.issue_number,
          title: "Test Issue",
          labels: [],
          html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          state: "open",
        },
      });

      mockGithub.rest.issues.update = async params => {
        updateCalls.push(params);
        return {
          data: {
            number: params.issue_number,
            title: "Test Issue",
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          },
        };
      };

      const result = await handler(
        {
          issue_number: 100,
          repo: "gh-aw", // Bare name without org
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(updateCalls[0].owner).toBe("github");
      expect(updateCalls[0].repo).toBe("gh-aw");
    });
  });
});
