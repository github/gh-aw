// @ts-check
import { describe, it, expect, beforeEach } from "vitest";
const { main } = require("./remove_labels.cjs");

describe("remove_labels", () => {
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
          removeLabel: async () => ({}),
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
        allowed: ["bug", "enhancement"],
        max: 5,
      });
      expect(typeof handler).toBe("function");
    });

    it("should log configuration on initialization", async () => {
      await main({ allowed: ["bug", "enhancement"], max: 3 });
      expect(mockCore.infos.some(msg => msg.includes("max=3"))).toBe(true);
      expect(mockCore.infos.some(msg => msg.includes("bug, enhancement"))).toBe(true);
    });
  });

  describe("handleRemoveLabels", () => {
    it("should remove labels from an issue using explicit item_number", async () => {
      const handler = await main({ max: 10 });
      const removeLabelCalls = [];

      mockGithub.rest.issues.removeLabel = async params => {
        removeLabelCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 456,
          labels: ["bug", "enhancement"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(456);
      expect(result.labelsRemoved).toEqual(["bug", "enhancement"]);
      expect(removeLabelCalls.length).toBe(2);
      expect(removeLabelCalls[0].issue_number).toBe(456);
      expect(removeLabelCalls[0].name).toBe("bug");
      expect(removeLabelCalls[1].name).toBe("enhancement");
    });

    it("should remove labels from an issue from context when item_number not provided", async () => {
      const handler = await main({ max: 10 });
      const removeLabelCalls = [];

      mockGithub.rest.issues.removeLabel = async params => {
        removeLabelCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          labels: ["documentation"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(123);
      expect(result.labelsRemoved).toEqual(["documentation"]);
      expect(result.contextType).toBe("issue");
    });

    it("should remove labels from a pull request from context", async () => {
      mockContext.payload = {
        pull_request: {
          number: 789,
        },
      };

      const handler = await main({ max: 10 });
      const removeLabelCalls = [];

      mockGithub.rest.issues.removeLabel = async params => {
        removeLabelCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          labels: ["needs-review"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.number).toBe(789);
      expect(result.contextType).toBe("pull request");
    });

    it("should handle invalid item_number", async () => {
      const handler = await main({ max: 10 });

      const result = await handler(
        {
          item_number: "invalid",
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error.includes("Invalid item number")).toBe(true);
    });

    it("should handle missing item_number and no context", async () => {
      mockContext.payload = {};

      const handler = await main({ max: 10 });

      const result = await handler(
        {
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error.includes("No issue/PR number available")).toBe(true);
    });

    it("should respect max count limit", async () => {
      const handler = await main({ max: 2 });

      // First call succeeds
      const result1 = await handler(
        {
          item_number: 1,
          labels: ["bug"],
        },
        {}
      );
      expect(result1.success).toBe(true);

      // Second call succeeds
      const result2 = await handler(
        {
          item_number: 2,
          labels: ["enhancement"],
        },
        {}
      );
      expect(result2.success).toBe(true);

      // Third call should fail
      const result3 = await handler(
        {
          item_number: 3,
          labels: ["documentation"],
        },
        {}
      );
      expect(result3.success).toBe(false);
      expect(result3.error.includes("Max count")).toBe(true);
    });

    it("should filter labels based on allowed list", async () => {
      const handler = await main({
        allowed: ["bug", "enhancement"],
        max: 10,
      });

      const removeLabelCalls = [];
      mockGithub.rest.issues.removeLabel = async params => {
        removeLabelCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug", "invalid-label", "enhancement"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsRemoved).toEqual(["bug", "enhancement"]);
    });

    it("should handle empty labels array", async () => {
      const handler = await main({ max: 10 });

      const result = await handler(
        {
          item_number: 100,
          labels: [],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("No labels provided");
      expect(result.error).toContain("current labels");
    });

    it("should handle missing labels field", async () => {
      const handler = await main({ max: 10 });

      const result = await handler(
        {
          item_number: 100,
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("No labels provided");
      expect(result.error).toContain("current labels");
    });

    it("should return allowed labels list when labels missing and allowed list configured", async () => {
      const handler = await main({
        allowed: ["bug", "enhancement", "documentation"],
        max: 10,
      });

      const result = await handler(
        {
          item_number: 100,
          labels: [],
        },
        {}
      );

      expect(result.success).toBe(false);
      expect(result.error).toContain("No labels provided");
      expect(result.error).toContain("allowed list");
      expect(result.error).toContain("bug");
      expect(result.error).toContain("enhancement");
      expect(result.error).toContain("documentation");
    });

    it("should handle label not present on issue gracefully", async () => {
      const handler = await main({ max: 10 });

      mockGithub.rest.issues.removeLabel = async params => {
        if (params.name === "not-present") {
          const error = new Error("Label does not exist");
          throw error;
        }
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug", "not-present"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsRemoved).toEqual(["bug"]);
    });

    it("should handle 404 errors gracefully", async () => {
      const handler = await main({ max: 10 });

      mockGithub.rest.issues.removeLabel = async params => {
        if (params.name === "missing") {
          const error = new Error("404 Not Found");
          throw error;
        }
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug", "missing"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsRemoved).toEqual(["bug"]);
    });

    it("should handle API errors gracefully", async () => {
      const handler = await main({ max: 10 });

      mockGithub.rest.issues.removeLabel = async () => {
        throw new Error("API Error: Forbidden");
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsRemoved).toEqual([]);
      expect(result.failedLabels).toBeDefined();
      expect(result.failedLabels.length).toBe(1);
      expect(result.failedLabels[0].label).toBe("bug");
      expect(result.failedLabels[0].error.includes("Forbidden")).toBe(true);
    });

    it("should deduplicate labels", async () => {
      const handler = await main({ max: 10 });
      const removeLabelCalls = [];

      mockGithub.rest.issues.removeLabel = async params => {
        removeLabelCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug", "bug", "enhancement", "bug"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsRemoved).toEqual(["bug", "enhancement"]);
    });

    it("should sanitize and trim label names", async () => {
      const handler = await main({ max: 10 });
      const removeLabelCalls = [];

      mockGithub.rest.issues.removeLabel = async params => {
        removeLabelCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["  bug  ", " enhancement ", "documentation"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(result.labelsRemoved.length).toBeGreaterThan(0);
    });

    it("should use spread operator for context.repo", async () => {
      const handler = await main({ max: 10 });
      const removeLabelCalls = [];

      mockGithub.rest.issues.removeLabel = async params => {
        removeLabelCalls.push(params);
        return {};
      };

      await handler(
        {
          item_number: 100,
          labels: ["bug"],
        },
        {}
      );

      expect(removeLabelCalls[0].owner).toBe("test-owner");
      expect(removeLabelCalls[0].repo).toBe("test-repo");
    });

    it("should support target-repo from config", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "external-org/external-repo",
      });
      const removeLabelCalls = [];

      mockGithub.rest.issues.removeLabel = async params => {
        removeLabelCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug"],
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(removeLabelCalls[0].owner).toBe("external-org");
      expect(removeLabelCalls[0].repo).toBe("external-repo");
    });

    it("should support repo field in message for cross-repository operations", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "default-org/default-repo",
        allowed_repos: ["cross-org/cross-repo"],
      });
      const removeLabelCalls = [];

      mockGithub.rest.issues.removeLabel = async params => {
        removeLabelCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 456,
          labels: ["bug"],
          repo: "cross-org/cross-repo",
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(removeLabelCalls[0].owner).toBe("cross-org");
      expect(removeLabelCalls[0].repo).toBe("cross-repo");
    });

    it("should reject repo not in allowed-repos list", async () => {
      const handler = await main({
        max: 10,
        "target-repo": "default-org/default-repo",
        allowed_repos: ["allowed-org/allowed-repo"],
      });

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug"],
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
      const removeLabelCalls = [];

      mockGithub.rest.issues.removeLabel = async params => {
        removeLabelCalls.push(params);
        return {};
      };

      const result = await handler(
        {
          item_number: 100,
          labels: ["bug"],
          repo: "gh-aw", // Bare repo name
        },
        {}
      );

      expect(result.success).toBe(true);
      expect(removeLabelCalls[0].owner).toBe("github");
      expect(removeLabelCalls[0].repo).toBe("gh-aw");
    });
  });
});
