import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

const mockContext = {
  repo: {
    owner: "test-owner",
    repo: "test-repo",
  },
  eventName: "issues",
  payload: {
    issue: {
      number: 123,
    },
  },
};

const mockGithub = {
  rest: {
    issues: {
      addAssignees: vi.fn(),
    },
  },
};

global.core = mockCore;
global.context = mockContext;
global.github = mockGithub;

describe("assign_to_user (Handler Factory Architecture)", () => {
  let handler;

  beforeEach(async () => {
    vi.clearAllMocks();

    const { main } = require("./assign_to_user.cjs");
    handler = await main({
      max: 10,
      allowed: ["user1", "user2", "user3"],
    });
  });

  it("should return a function from main()", async () => {
    const { main } = require("./assign_to_user.cjs");
    const result = await main({});
    expect(typeof result).toBe("function");
  });

  it("should assign users successfully", async () => {
    mockGithub.rest.issues.addAssignees.mockResolvedValue({});

    const message = {
      type: "assign_to_user",
      assignees: ["user1", "user2"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.issueNumber).toBe(123);
    expect(result.assigneesAdded).toEqual(["user1", "user2"]);
    expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["user1", "user2"],
    });
  });

  it("should include issue-intent metadata when enabled", async () => {
    mockGithub.rest.issues.addAssignees.mockResolvedValue({});

    const message = {
      type: "assign_to_user",
      assignees: ["user1"],
      rationale: "Primary maintainer for impacted area",
      confidence: "low",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["user1"],
      rationale: "Primary maintainer for impacted area",
      confidence: "LOW",
    });
  });

  it("should omit issue-intent metadata when disabled", async () => {
    const { main } = require("./assign_to_user.cjs");
    const noIntentHandler = await main({
      max: 10,
      allowed: ["user1", "user2", "user3"],
      issue_intent: false,
    });
    mockGithub.rest.issues.addAssignees.mockResolvedValue({});

    const result = await noIntentHandler(
      {
        type: "assign_to_user",
        assignees: ["user1"],
        rationale: "Primary maintainer for impacted area",
        confidence: "high",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["user1"],
    });
  });

  it("should support singular assignee field", async () => {
    mockGithub.rest.issues.addAssignees.mockResolvedValue({});

    const message = {
      type: "assign_to_user",
      assignee: "user1",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.assigneesAdded).toEqual(["user1"]);
    expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["user1"],
    });
  });

  it("should use explicit issue number from message", async () => {
    const { main } = require("./assign_to_user.cjs");
    handler = await main({ max: 10, allowed: ["user1"], target: "*" });
    mockGithub.rest.issues.addAssignees.mockResolvedValue({});

    const message = {
      type: "assign_to_user",
      issue_number: 456,
      assignees: ["user1"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.issueNumber).toBe(456);
    expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 456,
      assignees: ["user1"],
    });
  });

  it("should filter by allowed assignees", async () => {
    mockGithub.rest.issues.addAssignees.mockResolvedValue({});

    const message = {
      type: "assign_to_user",
      assignees: ["user1", "user2", "unauthorized"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.assigneesAdded).toEqual(["user1", "user2"]);
    expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["user1", "user2"],
    });
  });

  it("should respect max count configuration", async () => {
    const { main } = require("./assign_to_user.cjs");
    const limitedHandler = await main({ max: 1, allowed: ["user1", "user2"] });

    mockGithub.rest.issues.addAssignees.mockResolvedValue({});

    const message1 = {
      type: "assign_to_user",
      assignees: ["user1"],
    };

    const message2 = {
      type: "assign_to_user",
      assignees: ["user2"],
    };

    // First call should succeed
    const result1 = await limitedHandler(message1, {});
    expect(result1.success).toBe(true);

    // Second call should fail
    const result2 = await limitedHandler(message2, {});
    expect(result2.success).toBe(false);
    expect(result2.error).toContain("Max count");
  });

  it("should handle missing issue context", async () => {
    global.context = {
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
      eventName: "push",
      payload: {},
    };

    const message = {
      type: "assign_to_user",
      assignees: ["user1"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("not running in issue context");
    expect(mockGithub.rest.issues.addAssignees).not.toHaveBeenCalled();

    // Restore context
    global.context = mockContext;
  });

  it("should handle API errors gracefully", async () => {
    const apiError = new Error("API error");
    mockGithub.rest.issues.addAssignees.mockRejectedValue(apiError);

    const message = {
      type: "assign_to_user",
      assignees: ["user1"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("API error");
  });

  it("should return success with empty array when no valid assignees", async () => {
    const message = {
      type: "assign_to_user",
      assignees: [],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.assigneesAdded).toEqual([]);
    expect(result.message).toContain("No valid assignees found");
    expect(mockGithub.rest.issues.addAssignees).not.toHaveBeenCalled();
  });

  it("should deduplicate assignees", async () => {
    mockGithub.rest.issues.addAssignees.mockResolvedValue({});

    const message = {
      type: "assign_to_user",
      assignees: ["user1", "user2", "user1", "user2"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.assigneesAdded).toEqual(["user1", "user2"]);
    expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["user1", "user2"],
    });
  });

  it("should support target-repo from config", async () => {
    vi.clearAllMocks();
    const { main } = require("./assign_to_user.cjs");
    const targetRepoHandler = await main({
      max: 10,
      target: "*",
      "target-repo": "external-org/external-repo",
    });
    const addAssigneesCalls = [];

    mockGithub.rest.issues.addAssignees = async params => {
      addAssigneesCalls.push(params);
      return {};
    };

    const result = await targetRepoHandler(
      {
        issue_number: 100,
        assignees: ["user1"],
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(addAssigneesCalls[0].owner).toBe("external-org");
    expect(addAssigneesCalls[0].repo).toBe("external-repo");
  });

  it("should support repo field in message for cross-repository operations", async () => {
    vi.clearAllMocks();
    const { main } = require("./assign_to_user.cjs");
    const crossRepoHandler = await main({
      max: 10,
      target: "*",
      "target-repo": "default-org/default-repo",
      allowed_repos: ["cross-org/cross-repo"],
    });
    const addAssigneesCalls = [];

    mockGithub.rest.issues.addAssignees = async params => {
      addAssigneesCalls.push(params);
      return {};
    };

    const result = await crossRepoHandler(
      {
        issue_number: 456,
        assignees: ["user1"],
        repo: "cross-org/cross-repo",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(addAssigneesCalls[0].owner).toBe("cross-org");
    expect(addAssigneesCalls[0].repo).toBe("cross-repo");
  });

  it("should reject repo not in allowed-repos list", async () => {
    vi.clearAllMocks();
    const { main } = require("./assign_to_user.cjs");
    const handler = await main({
      max: 10,
      "target-repo": "default-org/default-repo",
      allowed_repos: ["allowed-org/allowed-repo"],
    });

    const result = await handler(
      {
        issue_number: 100,
        assignees: ["user1"],
        repo: "unauthorized-org/unauthorized-repo",
      },
      {}
    );

    expect(result.success).toBe(false);
    expect(result.error).toContain("not in the allowed-repos list");
  });

  it("should qualify bare repo name with default repo org", async () => {
    vi.clearAllMocks();
    const { main } = require("./assign_to_user.cjs");
    const handler = await main({
      max: 10,
      target: "*",
      "target-repo": "github/default-repo",
      allowed_repos: ["github/gh-aw"],
    });
    const addAssigneesCalls = [];

    mockGithub.rest.issues.addAssignees = async params => {
      addAssigneesCalls.push(params);
      return {};
    };

    const result = await handler(
      {
        issue_number: 100,
        assignees: ["user1"],
        repo: "gh-aw", // Bare repo name
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(addAssigneesCalls[0].owner).toBe("github");
    expect(addAssigneesCalls[0].repo).toBe("gh-aw");
  });

  it("should unassign current assignees when unassign_first is true", async () => {
    vi.clearAllMocks(); // Clear all mocks before this test

    const { main } = require("./assign_to_user.cjs");
    const handler = await main({
      max: 10,
      unassign_first: true,
    });

    // Mock getting current assignees
    mockGithub.rest.issues.get = vi.fn().mockResolvedValue({
      data: {
        assignees: [{ login: "old-user1" }, { login: "old-user2" }],
      },
    });

    mockGithub.rest.issues.removeAssignees = vi.fn().mockResolvedValue({});
    mockGithub.rest.issues.addAssignees = vi.fn().mockResolvedValue({}); // Recreate the mock

    const message = {
      type: "assign_to_user",
      assignees: ["new-user1"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.assigneesAdded).toEqual(["new-user1"]);

    // Verify that get was called to fetch current assignees
    expect(mockGithub.rest.issues.get).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
    });

    // Verify that removeAssignees was called with current assignees
    expect(mockGithub.rest.issues.removeAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["old-user1", "old-user2"],
    });

    // Verify that addAssignees was called with new assignees
    expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["new-user1"],
    });
  });

  it("should skip unassignment when there are no current assignees", async () => {
    vi.clearAllMocks(); // Clear all mocks before this test

    const { main } = require("./assign_to_user.cjs");
    const handler = await main({
      max: 10,
      unassign_first: true,
    });

    // Mock getting no current assignees
    mockGithub.rest.issues.get = vi.fn().mockResolvedValue({
      data: {
        assignees: [],
      },
    });

    mockGithub.rest.issues.removeAssignees = vi.fn().mockResolvedValue({});
    mockGithub.rest.issues.addAssignees = vi.fn().mockResolvedValue({}); // Recreate the mock

    const message = {
      type: "assign_to_user",
      assignees: ["new-user1"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.assigneesAdded).toEqual(["new-user1"]);

    // Verify that get was called
    expect(mockGithub.rest.issues.get).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
    });

    // Verify that removeAssignees was NOT called since there are no current assignees
    expect(mockGithub.rest.issues.removeAssignees).not.toHaveBeenCalled();

    // Verify that addAssignees was still called
    expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["new-user1"],
    });
  });

  it("should not unassign when unassign_first is false (default)", async () => {
    vi.clearAllMocks(); // Clear all mocks before this test

    const { main } = require("./assign_to_user.cjs");
    const handler = await main({
      max: 10,
      unassign_first: false, // explicitly false
    });

    mockGithub.rest.issues.get = vi.fn().mockResolvedValue({});
    mockGithub.rest.issues.removeAssignees = vi.fn().mockResolvedValue({});
    mockGithub.rest.issues.addAssignees = vi.fn().mockResolvedValue({}); // Recreate the mock

    const message = {
      type: "assign_to_user",
      assignees: ["new-user1"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);

    // Verify that get was NOT called
    expect(mockGithub.rest.issues.get).not.toHaveBeenCalled();

    // Verify that removeAssignees was NOT called
    expect(mockGithub.rest.issues.removeAssignees).not.toHaveBeenCalled();

    // Verify that addAssignees was called directly
    expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["new-user1"],
    });
  });

  describe("staged mode", () => {
    beforeEach(() => {
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";
    });

    afterEach(() => {
      delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    });

    it("should preview without executing in staged mode", async () => {
      vi.clearAllMocks();
      const { main } = require("./assign_to_user.cjs");
      const stagedHandler = await main({ max: 10 });

      const message = {
        type: "assign_to_user",
        assignees: ["user1"],
      };

      const result = await stagedHandler(message, {});

      expect(result.success).toBe(true);
      expect(result.staged).toBe(true);
      expect(result.previewInfo).toMatchObject({
        issueNumber: 123,
        repo: "test-owner/test-repo",
        assignees: ["user1"],
        unassignFirst: false,
      });
      expect(mockGithub.rest.issues.addAssignees).not.toHaveBeenCalled();
    });

    it("should preview unassign_first in staged mode", async () => {
      vi.clearAllMocks();
      const { main } = require("./assign_to_user.cjs");
      const stagedHandler = await main({ max: 10, unassign_first: true });

      const message = {
        type: "assign_to_user",
        assignees: ["user1"],
      };

      const result = await stagedHandler(message, {});

      expect(result.success).toBe(true);
      expect(result.staged).toBe(true);
      expect(result.previewInfo.unassignFirst).toBe(true);
      expect(mockGithub.rest.issues.addAssignees).not.toHaveBeenCalled();
    });
  });

  describe("blocked patterns", () => {
    it("should filter out blocked users by exact match", async () => {
      const { main } = require("./assign_to_user.cjs");
      const handler = await main({
        max: 10,
        blocked: ["copilot", "admin"],
      });

      mockGithub.rest.issues.addAssignees.mockResolvedValue({});

      const message = {
        type: "assign_to_user",
        assignees: ["user1", "copilot", "admin", "user2"],
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.assigneesAdded).toEqual(["user1", "user2"]);
      expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        issue_number: 123,
        assignees: ["user1", "user2"],
      });
    });

    it("should filter out blocked users by pattern", async () => {
      const { main } = require("./assign_to_user.cjs");
      const handler = await main({
        max: 10,
        blocked: ["*[bot]"],
      });

      mockGithub.rest.issues.addAssignees.mockResolvedValue({});

      const message = {
        type: "assign_to_user",
        assignees: ["user1", "dependabot[bot]", "github-actions[bot]", "user2"],
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.assigneesAdded).toEqual(["user1", "user2"]);
      expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        issue_number: 123,
        assignees: ["user1", "user2"],
      });
    });

    it("should combine allowed and blocked filters", async () => {
      const { main } = require("./assign_to_user.cjs");
      const handler = await main({
        max: 10,
        allowed: ["user1", "user2", "copilot", "github-actions[bot]"],
        blocked: ["copilot", "*[bot]"],
      });

      mockGithub.rest.issues.addAssignees.mockResolvedValue({});

      const message = {
        type: "assign_to_user",
        assignees: ["user1", "user2", "copilot", "github-actions[bot]", "unauthorized"],
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      // Should only include user1 and user2 (allowed and not blocked)
      expect(result.assigneesAdded).toEqual(["user1", "user2"]);
      expect(mockGithub.rest.issues.addAssignees).toHaveBeenCalledWith({
        owner: "test-owner",
        repo: "test-repo",
        issue_number: 123,
        assignees: ["user1", "user2"],
      });
    });

    it("should return success with empty array when all assignees are blocked", async () => {
      const { main } = require("./assign_to_user.cjs");
      const handler = await main({
        max: 10,
        blocked: ["*[bot]"],
      });

      const message = {
        type: "assign_to_user",
        assignees: ["dependabot[bot]", "github-actions[bot]"],
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.assigneesAdded).toEqual([]);
      expect(result.message).toContain("No valid assignees found");
      expect(mockGithub.rest.issues.addAssignees).not.toHaveBeenCalled();
    });
  });
});
