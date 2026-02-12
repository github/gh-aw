import { describe, it, expect, beforeEach, vi } from "vitest";

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
    expect(result.error).toContain("No issue number available");
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
});
