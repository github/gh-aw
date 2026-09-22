// @ts-check
import { describe, it, expect, beforeEach } from "vitest";
const { main: createHandler, parseDuplicateOf } = require("./close_issue.cjs");
const main = (config = {}) => createHandler({ target: "*", ...config });

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
      debug: () => {},
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
              node_id: `node_${issue_number}`,
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
      eventName: "issues",
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
          body: "Closing this issue",
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(456);
      expect(updateCalls.length).toBe(1);
      expect(updateCalls[0].issue_number).toBe(456);
      expect(updateCalls[0].state).toBe("closed");
    });

    it("should include issue-intent metadata on close when enabled", async () => {
      const handler = await main({ max: 10 });
      const requestCalls = [];
      mockGithub.request = async (route, params) => {
        requestCalls.push({ route, params });
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
          body: "Closing this issue",
          rationale: "Duplicate confirmed",
          confidence: "medium",
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(requestCalls).toHaveLength(1);
      expect(requestCalls[0].route).toBe("PATCH /repos/{owner}/{repo}/issues/{issue_number}");
      expect(requestCalls[0].params.state).toEqual({ value: "closed", rationale: "Duplicate confirmed", confidence: "MEDIUM" });
      expect(requestCalls[0].params.rationale).toBeUndefined();
      expect(requestCalls[0].params.confidence).toBeUndefined();
      expect(requestCalls[0].params.headers).toBeUndefined();
    });

    it("should skip issue-intent metadata when explicitly disabled", async () => {
      const handler = await main({ max: 10, issue_intent: false });
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
          body: "Closing this issue",
          rationale: "Duplicate confirmed",
          confidence: "high",
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(updateCalls).toHaveLength(1);
      expect(updateCalls[0].rationale).toBeUndefined();
      expect(updateCalls[0].confidence).toBeUndefined();
      expect(updateCalls[0].headers).toBeUndefined();
    });

    it("should close an issue from context when issue_number not provided", async () => {
      const handler = await main({ max: 10, target: "triggering" });
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

      const result = await handler({ body: "Closing from context" }, {});

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
          body: "Closing issue",
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("Invalid item_number/issue_number");
    });

    it("should handle missing issue_number and no context", async () => {
      mockContext.payload = {};

      const handler = await main({ max: 10, target: "triggering" });

      const result = await handler({ body: "Trying to close" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("no issue found");
    });

    it("should respect max count limit", async () => {
      const handler = await main({ max: 2 });

      // First call succeeds
      const result1 = await handler({ issue_number: 1, body: "Close #1" }, {});
      expect(result1.success).toBe(true);

      // Second call succeeds
      const result2 = await handler({ issue_number: 2, body: "Close #2" }, {});
      expect(result2.success).toBe(true);

      // Third call should fail
      const result3 = await handler({ issue_number: 3, body: "Close #3" }, {});
      expect(result3.success).toBe(false);
      expect(result3.error.includes("Max count")).toBe(true);
    });

    it("should still add comment to already closed issues", async () => {
      const handler = await main({ max: 10, comment: "Test comment" });

      let commentAdded = false;
      let issueUpdateCalled = false;

      mockGithub.rest.issues.get = async () => ({
        data: {
          number: 100,
          title: "Test Issue",
          labels: [],
          html_url: "https://github.com/test-owner/test-repo/issues/100",
          state: "closed",
        },
      });

      mockGithub.rest.issues.createComment = async () => {
        commentAdded = true;
        return {
          data: {
            id: 456,
            html_url: "https://github.com/test-owner/test-repo/issues/100#issuecomment-456",
          },
        };
      };

      mockGithub.rest.issues.update = async () => {
        issueUpdateCalled = true;
        return {
          data: {
            number: 100,
            title: "Test Issue",
            html_url: "https://github.com/test-owner/test-repo/issues/100",
          },
        };
      };

      const result = await handler({ issue_number: 100, body: "Test comment" }, {});

      expect(result.success).toBe(true);
      expect(result.alreadyClosed).toBe(true);
      expect(commentAdded).toBe(true);
      expect(issueUpdateCalled).toBe(false); // Should not call update for already closed issue
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

      const result = await handler({ issue_number: 100, body: "Closing" }, {});

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

      const result = await handler({ issue_number: 100, body: "Closing" }, {});

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
      expect(commentCalls[0].body).toContain("for #123");
      expect(commentCalls[0].body).toContain("gh-aw-agentic-workflow:");
    });

    it("should use body field from message over config comment", async () => {
      const handler = await main({
        max: 10,
        comment: "Default comment from config",
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

      const result = await handler(
        {
          issue_number: 100,
          body: "Custom body from message",
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(commentCalls.length).toBe(1);
      // Should use the body from message, not the config comment
      expect(commentCalls[0].body).toContain("Custom body from message");
      expect(commentCalls[0].body).toContain("for #123");
      expect(commentCalls[0].body).toContain("gh-aw-agentic-workflow:");
      expect(commentCalls[0].body).not.toContain("Default comment from config");
    });

    it("should require body field when no config comment is set", async () => {
      const handler = await main({ max: 10 });

      const result = await handler({ issue_number: 100 }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("No comment body provided");
    });

    it("should fail when body is empty string", async () => {
      const handler = await main({ max: 10 });

      const result = await handler({ issue_number: 100, body: "" }, {});

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
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}#issuecomment-999`,
          },
        };
      };

      const result = await handler({ issue_number: 100, body: "   \n\t  " }, {});

      expect(result.success).toBe(true);
      expect(commentCalls.length).toBe(1);
      expect(commentCalls[0].body).toContain("Default close comment");
    });

    it("should fail when body is whitespace-only and no config comment", async () => {
      const handler = await main({ max: 10 });

      const result = await handler({ issue_number: 100, body: "   " }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("No comment body provided");
    });

    it("should use body field from message for already closed issues", async () => {
      const handler = await main({ max: 10 });

      let commentBody = "";
      let issueUpdateCalled = false;

      mockGithub.rest.issues.get = async () => ({
        data: {
          number: 100,
          title: "Test Issue",
          labels: [],
          html_url: "https://github.com/test-owner/test-repo/issues/100",
          state: "closed",
        },
      });

      mockGithub.rest.issues.createComment = async params => {
        commentBody = params.body;
        return {
          data: {
            id: 456,
            html_url: "https://github.com/test-owner/test-repo/issues/100#issuecomment-456",
          },
        };
      };

      mockGithub.rest.issues.update = async () => {
        issueUpdateCalled = true;
        return {
          data: {
            number: 100,
            title: "Test Issue",
            html_url: "https://github.com/test-owner/test-repo/issues/100",
          },
        };
      };

      const result = await handler({ issue_number: 100, body: "Closing comment with details" }, {});

      expect(result.success).toBe(true);
      expect(result.alreadyClosed).toBe(true);
      expect(commentBody).toContain("Closing comment with details");
      expect(issueUpdateCalled).toBe(false); // Should not call update for already closed issue
    });

    it("should handle API errors gracefully", async () => {
      const handler = await main({ max: 10 });

      mockGithub.rest.issues.get = async () => {
        throw new Error("API Error: Not found");
      };

      const result = await handler({ issue_number: 100, body: "Closing" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("API Error");
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

      const result = await handler({ issue_number: 100, body: "Closing" }, {});

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
          body: "Closing cross-repo issue",
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
          body: "Trying to close",
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
          body: "Closing",
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(updateCalls[0].owner).toBe("github");
      expect(updateCalls[0].repo).toBe("gh-aw");
    });

    it("should close issue in any repo when target-repo is wildcard *", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "*",
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
          issue_number: 200,
          repo: "any-org/any-repo",
          body: "Closing",
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(updateCalls[0].owner).toBe("any-org");
      expect(updateCalls[0].repo).toBe("any-repo");
    });

    it("should use default state_reason 'COMPLETED' when not specified", async () => {
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

      const result = await handler({ issue_number: 100, body: "Closing" }, {});

      expect(result.success).toBe(true);
      expect(updateCalls[0].state_reason).toBe("completed");
    });

    it("should use item-level state_reason 'DUPLICATE' when specified in message", async () => {
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

      const result = await handler({ issue_number: 100, body: "Duplicate of #50", state_reason: "DUPLICATE" }, {});

      expect(result.success).toBe(true);
      expect(updateCalls[0].state_reason).toBe("duplicate");
    });

    it("should use item-level state_reason 'NOT_PLANNED' when specified in message", async () => {
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

      const result = await handler({ issue_number: 100, body: "Won't fix", state_reason: "NOT_PLANNED" }, {});

      expect(result.success).toBe(true);
      expect(updateCalls[0].state_reason).toBe("not_planned");
    });

    it("should use config-level state_reason as default for all closes", async () => {
      const handler = await main({ max: 10, state_reason: "DUPLICATE" });
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

      const result = await handler({ issue_number: 100, body: "Duplicate" }, {});

      expect(result.success).toBe(true);
      expect(updateCalls[0].state_reason).toBe("duplicate");
    });

    it("should enforce scalar state_reason from config regardless of item-level value", async () => {
      const handler = await main({ max: 10, state_reason: "NOT_PLANNED" });
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

      // Agent provides DUPLICATE but scalar config is NOT_PLANNED — config wins.
      const result = await handler({ issue_number: 100, body: "Duplicate of #50", state_reason: "DUPLICATE" }, {});

      expect(result.success).toBe(true);
      expect(updateCalls[0].state_reason).toBe("not_planned");
    });

    it("should use first item in allowed_state_reason list as default when agent omits state_reason", async () => {
      const handler = await main({ max: 10, allowed_state_reason: ["not_planned", "duplicate"] });
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

      const result = await handler({ issue_number: 100, body: "Closing" }, {});

      expect(result.success).toBe(true);
      expect(updateCalls[0].state_reason).toBe("not_planned");
    });

    it("should accept item-level state_reason from allowed_state_reason list", async () => {
      const handler = await main({ max: 10, allowed_state_reason: ["not_planned", "duplicate"] });
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

      const result = await handler({ issue_number: 100, body: "Duplicate", state_reason: "DUPLICATE" }, {});

      expect(result.success).toBe(true);
      expect(updateCalls[0].state_reason).toBe("duplicate");
    });

    it("should reject item-level state_reason not in allowed_state_reason list", async () => {
      const handler = await main({ max: 10, allowed_state_reason: ["not_planned", "duplicate"] });

      const result = await handler({ issue_number: 100, body: "Completed", state_reason: "COMPLETED" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toMatch(/not permitted/i);
      expect(result.error).toMatch(/COMPLETED/);
    });

    it("should accept item-level state_reason for omitted config (all three values)", async () => {
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

      for (const reason of ["COMPLETED", "NOT_PLANNED", "DUPLICATE"]) {
        updateCalls.length = 0;
        const result = await handler({ issue_number: 100, body: "Closing", state_reason: reason }, {});
        expect(result.success).toBe(true);
        expect(updateCalls[0].state_reason).toBe(reason.toLowerCase());
      }
    });

    it("should reject invalid state_reason for omitted config", async () => {
      const handler = await main({ max: 10 });

      const result = await handler({ issue_number: 100, body: "Closing", state_reason: "INVALID" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toMatch(/not a supported value/i);
    });

    it("should default to COMPLETED when omitted config and agent omits state_reason", async () => {
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

      const result = await handler({ issue_number: 100, body: "Closing" }, {});

      expect(result.success).toBe(true);
      expect(updateCalls[0].state_reason).toBe("completed");
    });

    it("should resolve temporary ID in issue_number field", async () => {
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
          issue_number: "aw_analysis",
          body: "Closing issue created with temporary ID",
        },
        {
          aw_analysis: { repo: "test-owner/test-repo", number: 789 },
        }
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(789);
      expect(updateCalls[0].issue_number).toBe(789);
    });

    it("should defer when temporary ID is not yet resolved in close_issue", async () => {
      const handler = await main({ max: 10 });

      const result = await handler(
        {
          issue_number: "aw_pending",
          body: "Attempting to close unresolved issue",
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.deferred).toBe(true);
      expect(result.error).toContain("aw_pending");
    });
  });

  describe("allow-body: false", () => {
    it("should close without comment when allow-body is false and body is empty", async () => {
      const handler = await main({ max: 10, allow_body: false });
      const commentCalls = [];

      mockGithub.rest.issues.createComment = async params => {
        commentCalls.push(params);
        return { data: { id: 1, html_url: "url" } };
      };

      const result = await handler({ issue_number: 100, body: "" }, {});

      expect(result.success).toBe(true);
      expect(commentCalls.length).toBe(0);
      expect(mockCore.infos.some(msg => msg.includes("allow-body is false"))).toBe(true);
    });

    it("should close without comment and warn when allow-body is false and agent provides non-empty body", async () => {
      const handler = await main({ max: 10, allow_body: false });
      const commentCalls = [];

      mockGithub.rest.issues.createComment = async params => {
        commentCalls.push(params);
        return { data: { id: 1, html_url: "url" } };
      };

      const result = await handler({ issue_number: 100, body: "This summary should be dropped" }, {});

      expect(result.success).toBe(true);
      expect(commentCalls.length).toBe(0);
      expect(mockCore.warnings.some(msg => msg.includes("allow-body is false") && msg.includes("dropping"))).toBe(true);
    });

    it("should close without comment when allow-body is false and no body is provided", async () => {
      const handler = await main({ max: 10, allow_body: false });
      const commentCalls = [];

      mockGithub.rest.issues.createComment = async params => {
        commentCalls.push(params);
        return { data: { id: 1, html_url: "url" } };
      };

      const result = await handler({ issue_number: 100 }, {});

      expect(result.success).toBe(true);
      expect(commentCalls.length).toBe(0);
    });

    it("should still require body when allow-body is not set (default behavior)", async () => {
      const handler = await main({ max: 10 });

      const result = await handler({ issue_number: 100 }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("No comment body provided");
    });

    it("should still add comment when allow-body is explicitly true", async () => {
      const handler = await main({ max: 10, allow_body: true });
      const commentCalls = [];

      mockGithub.rest.issues.createComment = async params => {
        commentCalls.push(params);
        return { data: { id: 1, html_url: "url" } };
      };

      const result = await handler({ issue_number: 100, body: "Closing summary" }, {});

      expect(result.success).toBe(true);
      expect(commentCalls.length).toBe(1);
    });
  });

  describe("parseDuplicateOf", () => {
    it("should parse a bare number", () => {
      const result = parseDuplicateOf(123, "owner", "repo");
      expect(result).toEqual({ owner: "owner", repo: "repo", issueNumber: 123 });
    });

    it("should parse a numeric string", () => {
      const result = parseDuplicateOf("456", "owner", "repo");
      expect(result).toEqual({ owner: "owner", repo: "repo", issueNumber: 456 });
    });

    it("should parse a #-prefixed number", () => {
      const result = parseDuplicateOf("#789", "owner", "repo");
      expect(result).toEqual({ owner: "owner", repo: "repo", issueNumber: 789 });
    });

    it("should parse owner/repo#number format", () => {
      const result = parseDuplicateOf("other-org/other-repo#42", "owner", "repo");
      expect(result).toEqual({ owner: "other-org", repo: "other-repo", issueNumber: 42 });
    });

    it("should parse a full GitHub issue URL", () => {
      const result = parseDuplicateOf("https://github.com/my-org/my-repo/issues/101", "owner", "repo");
      expect(result).toEqual({ owner: "my-org", repo: "my-repo", issueNumber: 101 });
    });

    it("should return null for undefined", () => {
      expect(parseDuplicateOf(undefined, "owner", "repo")).toBeNull();
    });

    it("should return null for null", () => {
      expect(parseDuplicateOf(null, "owner", "repo")).toBeNull();
    });

    it("should return null for empty string", () => {
      expect(parseDuplicateOf("", "owner", "repo")).toBeNull();
    });

    it("should return null for unparseable value", () => {
      expect(parseDuplicateOf("not-a-valid-ref", "owner", "repo")).toBeNull();
    });

    it("should return null for issue number 0", () => {
      expect(parseDuplicateOf(0, "owner", "repo")).toBeNull();
      expect(parseDuplicateOf("#0", "owner", "repo")).toBeNull();
      expect(parseDuplicateOf("0", "owner", "repo")).toBeNull();
    });

    it("should return null for owner/repo with extra slash in repo", () => {
      expect(parseDuplicateOf("org/sub/repo#1", "owner", "repo")).toBeNull();
    });

    it("should parse URL and ignore trailing path segment after issue number", () => {
      // The URL should still parse — extra trailing segments are allowed by the anchor pattern
      const result = parseDuplicateOf("https://github.com/org/repo/issues/42/extra", "owner", "repo");
      // extra path after issue number is captured by the (?:[?#/].*)? anchor — verify it strips cleanly
      expect(result).toEqual({ owner: "org", repo: "repo", issueNumber: 42 });
    });

    it("should return null for issue numbers with more than 15 digits", () => {
      expect(parseDuplicateOf("9999999999999999", "owner", "repo")).toBeNull();
      expect(parseDuplicateOf("org/repo#9999999999999999", "owner", "repo")).toBeNull();
      expect(parseDuplicateOf("https://github.com/org/repo/issues/9999999999999999", "owner", "repo")).toBeNull();
    });
  });

  describe("duplicate_of native marking", () => {
    it("should call markAsDuplicate mutation when duplicate_of and state_reason DUPLICATE are set", async () => {
      const handler = await main({ max: 10, state_reason: "DUPLICATE" });
      const graphqlCalls = [];
      const updateCalls = [];

      mockGithub.rest.issues.get = async ({ owner, repo, issue_number }) => ({
        data: {
          number: issue_number,
          title: "Test Issue",
          labels: [],
          html_url: `https://github.com/${owner}/${repo}/issues/${issue_number}`,
          state: "open",
          node_id: `node_${issue_number}`,
        },
      });

      mockGithub.rest.issues.update = async params => {
        updateCalls.push(params);
        return {
          data: {
            number: params.issue_number,
            title: "Test Issue",
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
            node_id: `node_${params.issue_number}`,
          },
        };
      };

      mockGithub.graphql = async (mutation, variables) => {
        graphqlCalls.push({ mutation, variables });
        return { markAsDuplicate: { duplicate: { id: "node_100", number: 100 } } };
      };

      const result = await handler({ issue_number: 100, body: "Closing as duplicate", duplicate_of: 50 }, {});

      expect(result.success).toBe(true);
      expect(graphqlCalls.length).toBe(1);
      expect(graphqlCalls[0].variables.duplicateId).toBe("node_100");
      expect(graphqlCalls[0].variables.canonicalId).toBe("node_50");
    });

    it("should call markAsDuplicate with item-level state_reason DUPLICATE", async () => {
      const handler = await main({ max: 10 });
      const graphqlCalls = [];

      mockGithub.rest.issues.get = async ({ owner, repo, issue_number }) => ({
        data: {
          number: issue_number,
          title: "Test Issue",
          labels: [],
          html_url: `https://github.com/${owner}/${repo}/issues/${issue_number}`,
          state: "open",
          node_id: `node_${issue_number}`,
        },
      });

      mockGithub.rest.issues.update = async params => ({
        data: {
          number: params.issue_number,
          title: "Test Issue",
          html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          node_id: `node_${params.issue_number}`,
        },
      });

      mockGithub.graphql = async (mutation, variables) => {
        graphqlCalls.push(variables);
        return {};
      };

      const result = await handler({ issue_number: 200, body: "Duplicate", state_reason: "DUPLICATE", duplicate_of: "#99" }, {});

      expect(result.success).toBe(true);
      expect(graphqlCalls.length).toBe(1);
      expect(graphqlCalls[0].canonicalId).toBe("node_99");
    });

    it("should skip markAsDuplicate when state_reason is not DUPLICATE", async () => {
      const handler = await main({ max: 10 });
      const graphqlCalls = [];

      mockGithub.graphql = async (mutation, variables) => {
        graphqlCalls.push(variables);
        return {};
      };

      const result = await handler({ issue_number: 300, body: "Completed", state_reason: "COMPLETED", duplicate_of: 50 }, {});

      expect(result.success).toBe(true);
      expect(graphqlCalls.length).toBe(0);
    });

    it("should skip markAsDuplicate when duplicate_of is absent", async () => {
      const handler = await main({ max: 10, state_reason: "DUPLICATE" });
      const graphqlCalls = [];

      mockGithub.graphql = async (mutation, variables) => {
        graphqlCalls.push(variables);
        return {};
      };

      const result = await handler({ issue_number: 400, body: "Duplicate no ref" }, {});

      expect(result.success).toBe(true);
      expect(graphqlCalls.length).toBe(0);
    });

    it("should warn and skip markAsDuplicate for unparseable duplicate_of", async () => {
      const handler = await main({ max: 10, state_reason: "DUPLICATE" });
      const graphqlCalls = [];

      mockGithub.graphql = async (mutation, variables) => {
        graphqlCalls.push(variables);
        return {};
      };

      const result = await handler({ issue_number: 500, body: "Duplicate", duplicate_of: "not-parseable" }, {});

      expect(result.success).toBe(true);
      expect(graphqlCalls.length).toBe(0);
      expect(mockCore.warnings.some(w => w.includes("could not be parsed"))).toBe(true);
    });

    it("should continue close successfully when markAsDuplicate mutation fails", async () => {
      const handler = await main({ max: 10, state_reason: "DUPLICATE" });

      mockGithub.rest.issues.get = async ({ owner, repo, issue_number }) => ({
        data: {
          number: issue_number,
          title: "Test Issue",
          labels: [],
          html_url: `https://github.com/${owner}/${repo}/issues/${issue_number}`,
          state: "open",
          node_id: `node_${issue_number}`,
        },
      });

      mockGithub.rest.issues.update = async params => ({
        data: {
          number: params.issue_number,
          title: "Test Issue",
          html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          node_id: `node_${params.issue_number}`,
        },
      });

      mockGithub.graphql = async () => {
        throw new Error("GraphQL mutation failed: forbidden");
      };

      const result = await handler({ issue_number: 600, body: "Duplicate close", duplicate_of: 10 }, {});

      expect(result.success).toBe(true);
      expect(mockCore.warnings.some(w => w.includes("Failed to mark native duplicate"))).toBe(true);
    });

    it("should parse owner/repo#number duplicate_of and call mutation with correct repo", async () => {
      const handler = await main({ max: 10, state_reason: "DUPLICATE", allowed_repos: ["other-org/other-repo"] });
      const graphqlCalls = [];
      const getIssueCalls = [];

      mockGithub.rest.issues.get = async ({ owner, repo, issue_number }) => {
        getIssueCalls.push({ owner, repo, issue_number });
        return {
          data: {
            number: issue_number,
            title: "Test Issue",
            labels: [],
            html_url: `https://github.com/${owner}/${repo}/issues/${issue_number}`,
            state: "open",
            node_id: `node_${owner}_${repo}_${issue_number}`,
          },
        };
      };

      mockGithub.rest.issues.update = async params => ({
        data: {
          number: params.issue_number,
          title: "Test Issue",
          html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          node_id: `node_${params.owner}_${params.repo}_${params.issue_number}`,
        },
      });

      mockGithub.graphql = async (mutation, variables) => {
        graphqlCalls.push(variables);
        return {};
      };

      const result = await handler(
        {
          issue_number: 700,
          body: "Duplicate of cross-repo issue",
          duplicate_of: "other-org/other-repo#123",
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(graphqlCalls.length).toBe(1);
      // The canonical node ID should come from the cross-repo issue
      expect(graphqlCalls[0].canonicalId).toBe("node_other-org_other-repo_123");
      // The duplicate node ID should come from the current repo issue (via closedEntity.node_id)
      expect(graphqlCalls[0].duplicateId).toBe("node_test-owner_test-repo_700");
    });

    it("should skip native duplicate marking when canonical repo is not in allowed-repos", async () => {
      // No allowed_repos configured — cross-repo duplicate_of should be rejected
      const handler = await main({ max: 10, state_reason: "DUPLICATE" });
      const graphqlCalls = [];

      mockGithub.graphql = async (mutation, variables) => {
        graphqlCalls.push(variables);
        return {};
      };

      const result = await handler({ issue_number: 800, body: "Duplicate", duplicate_of: "other-org/other-repo#1" }, {});

      expect(result.success).toBe(true);
      expect(graphqlCalls.length).toBe(0);
      expect(mockCore.warnings.some(w => w.includes("not in the allowed-repos list"))).toBe(true);
    });

    it("should skip native duplicate marking when duplicate_of points to the same issue", async () => {
      const handler = await main({ max: 10, state_reason: "DUPLICATE" });
      const graphqlCalls = [];

      mockGithub.graphql = async (mutation, variables) => {
        graphqlCalls.push(variables);
        return {};
      };

      // issue_number 900 pointing to itself
      const result = await handler({ issue_number: 900, body: "Duplicate", duplicate_of: 900 }, {});

      expect(result.success).toBe(true);
      expect(graphqlCalls.length).toBe(0);
      expect(mockCore.warnings.some(w => w.includes("cannot be a duplicate of itself"))).toBe(true);
    });

    it("should call markAsDuplicate when state_reason is lowercase 'duplicate'", async () => {
      const handler = await main({ max: 10 });
      const graphqlCalls = [];

      mockGithub.rest.issues.get = async ({ owner, repo, issue_number }) => ({
        data: {
          number: issue_number,
          title: "Test Issue",
          labels: [],
          html_url: `https://github.com/${owner}/${repo}/issues/${issue_number}`,
          state: "open",
          node_id: `node_${issue_number}`,
        },
      });

      mockGithub.rest.issues.update = async params => ({
        data: {
          number: params.issue_number,
          title: "Test Issue",
          html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          node_id: `node_${params.issue_number}`,
        },
      });

      mockGithub.graphql = async (mutation, variables) => {
        graphqlCalls.push(variables);
        return {};
      };

      const result = await handler({ issue_number: 1000, body: "Duplicate", state_reason: "duplicate", duplicate_of: "#99" }, {});

      expect(result.success).toBe(true);
      expect(graphqlCalls.length).toBe(1);
      expect(graphqlCalls[0].canonicalId).toBe("node_99");
    });

    it("should warn and succeed when REST fetch for canonical issue fails", async () => {
      const handler = await main({ max: 10, state_reason: "DUPLICATE" });

      mockGithub.rest.issues.update = async params => ({
        data: {
          number: params.issue_number,
          title: "Test Issue",
          html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          node_id: `node_${params.issue_number}`,
        },
      });

      // issues.get for canonical issue (10) throws; all other calls succeed
      mockGithub.rest.issues.get = async ({ owner, repo, issue_number }) => {
        if (issue_number === 10) throw new Error("Not Found");
        return {
          data: {
            number: issue_number,
            title: "Test Issue",
            labels: [],
            html_url: `https://github.com/${owner}/${repo}/issues/${issue_number}`,
            state: "open",
          },
        };
      };

      const result = await handler({ issue_number: 1001, body: "Duplicate close", duplicate_of: 10 }, {});

      expect(result.success).toBe(true);
      expect(mockCore.warnings.some(w => w.includes("Failed to mark native duplicate"))).toBe(true);
    });

    it("should warn when duplicate_of is set but state_reason is not DUPLICATE", async () => {
      const handler = await main({ max: 10 });
      const graphqlCalls = [];

      mockGithub.graphql = async (mutation, variables) => {
        graphqlCalls.push(variables);
        return {};
      };

      const result = await handler({ issue_number: 1002, body: "Completed", state_reason: "COMPLETED", duplicate_of: 50 }, {});

      expect(result.success).toBe(true);
      expect(graphqlCalls.length).toBe(0);
      expect(mockCore.warnings.some(w => w.includes("not DUPLICATE"))).toBe(true);
    });

    it("should mark native duplicate when using allowed_state_reason list with DUPLICATE and duplicate_of", async () => {
      const handler = await main({ max: 10, allowed_state_reason: ["not_planned", "duplicate"] });
      const graphqlCalls = [];
      const requestCalls = [];

      mockGithub.rest.issues.get = async ({ owner, repo, issue_number }) => ({
        data: {
          number: issue_number,
          title: "Test Issue",
          labels: [],
          html_url: `https://github.com/${owner}/${repo}/issues/${issue_number}`,
          state: "open",
          node_id: `node_${owner}_${repo}_${issue_number}`,
        },
      });

      mockGithub.rest.issues.update = async params => ({
        data: {
          number: params.issue_number,
          title: "Test Issue",
          html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
          node_id: `node_${params.owner}_${params.repo}_${params.issue_number}`,
        },
      });

      mockGithub.request = async (route, params) => {
        requestCalls.push({ route, params });
        return {
          data: {
            number: params.issue_number,
            title: "Test Issue",
            html_url: `https://github.com/${params.owner}/${params.repo}/issues/${params.issue_number}`,
            node_id: `node_${params.owner}_${params.repo}_${params.issue_number}`,
          },
        };
      };

      mockGithub.graphql = async (mutation, variables) => {
        graphqlCalls.push(variables);
        return { markAsDuplicate: { duplicate: { id: variables.duplicateId, number: 1003 } } };
      };

      const result = await handler({ issue_number: 1003, body: "Duplicate report", state_reason: "DUPLICATE", duplicate_of: 50, suggest: true, rationale: "Same CSV defect.", confidence: "HIGH" }, {});

      expect(result.success).toBe(true);

      // Verify issue-intent PATCH was called with the intent metadata intact (suggest, rationale, confidence)
      expect(requestCalls).toHaveLength(1);
      expect(requestCalls[0].route).toBe("PATCH /repos/{owner}/{repo}/issues/{issue_number}");
      expect(requestCalls[0].params.state).toMatchObject({ value: "closed", suggest: true, rationale: "Same CSV defect.", confidence: "HIGH" });

      // Verify the native duplicate GraphQL mutation was also executed
      expect(graphqlCalls.length).toBeGreaterThan(0);
      const mutation = graphqlCalls.find(c => "duplicateId" in c || "canonicalId" in c);
      expect(mutation).toBeDefined();
    });
  });
});
