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
      removeAssignees: vi.fn(),
    },
  },
};

global.core = mockCore;
global.context = mockContext;
global.github = mockGithub;

describe("unassign_from_user (Handler Factory Architecture)", () => {
  let handler;

  beforeEach(async () => {
    vi.clearAllMocks();

    const { main } = require("./unassign_from_user.cjs");
    handler = await main({
      max: 10,
      allowed: ["user1", "user2", "user3"],
    });
  });

  it("should return a function from main()", async () => {
    const { main } = require("./unassign_from_user.cjs");
    const result = await main({});
    expect(typeof result).toBe("function");
  });

  it("should unassign users successfully", async () => {
    mockGithub.rest.issues.removeAssignees.mockResolvedValue({});

    const message = {
      type: "unassign_from_user",
      assignees: ["user1", "user2"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.issueNumber).toBe(123);
    expect(result.assigneesRemoved).toEqual(["user1", "user2"]);
    expect(mockGithub.rest.issues.removeAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["user1", "user2"],
    });
  });

  it("should support singular assignee field", async () => {
    mockGithub.rest.issues.removeAssignees.mockResolvedValue({});

    const message = {
      type: "unassign_from_user",
      assignee: "user1",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.assigneesRemoved).toEqual(["user1"]);
    expect(mockGithub.rest.issues.removeAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["user1"],
    });
  });

  it("should use explicit issue number from message", async () => {
    mockGithub.rest.issues.removeAssignees.mockResolvedValue({});

    const message = {
      type: "unassign_from_user",
      issue_number: 456,
      assignees: ["user1"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.issueNumber).toBe(456);
    expect(mockGithub.rest.issues.removeAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 456,
      assignees: ["user1"],
    });
  });

  it("should filter by allowed assignees", async () => {
    mockGithub.rest.issues.removeAssignees.mockResolvedValue({});

    const message = {
      type: "unassign_from_user",
      assignees: ["user1", "user2", "unauthorized"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.assigneesRemoved).toEqual(["user1", "user2"]);
    expect(mockGithub.rest.issues.removeAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["user1", "user2"],
    });
  });

  it("should respect max count configuration", async () => {
    const { main } = require("./unassign_from_user.cjs");
    const limitedHandler = await main({ max: 1, allowed: ["user1", "user2"] });

    mockGithub.rest.issues.removeAssignees.mockResolvedValue({});

    const message1 = {
      type: "unassign_from_user",
      assignees: ["user1"],
    };

    const message2 = {
      type: "unassign_from_user",
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
      type: "unassign_from_user",
      assignees: ["user1"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("No issue number available");
    expect(mockGithub.rest.issues.removeAssignees).not.toHaveBeenCalled();

    // Restore context
    global.context = mockContext;
  });

  it("should handle API errors gracefully", async () => {
    const apiError = new Error("API error");
    mockGithub.rest.issues.removeAssignees.mockRejectedValue(apiError);

    const message = {
      type: "unassign_from_user",
      assignees: ["user1"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("API error");
  });

  it("should return success with empty array when no valid assignees", async () => {
    const message = {
      type: "unassign_from_user",
      assignees: [],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.assigneesRemoved).toEqual([]);
    expect(result.message).toContain("No valid assignees found");
    expect(mockGithub.rest.issues.removeAssignees).not.toHaveBeenCalled();
  });

  it("should deduplicate assignees", async () => {
    mockGithub.rest.issues.removeAssignees.mockResolvedValue({});

    const message = {
      type: "unassign_from_user",
      assignees: ["user1", "user2", "user1", "user2"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.assigneesRemoved).toEqual(["user1", "user2"]);
    expect(mockGithub.rest.issues.removeAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      assignees: ["user1", "user2"],
    });
  });

  it("should support cross-repository unassignment", async () => {
    const { main } = require("./unassign_from_user.cjs");
    const crossRepoHandler = await main({
      max: 10,
      allowed: ["user1"],
      allowed_repos: ["test-owner/other-repo"],
    });

    mockGithub.rest.issues.removeAssignees.mockResolvedValue({});

    const message = {
      type: "unassign_from_user",
      issue_number: 789,
      assignees: ["user1"],
      repo: "test-owner/other-repo",
    };

    const result = await crossRepoHandler(message, {});

    expect(result.success).toBe(true);
    expect(result.repo).toBe("test-owner/other-repo");
    expect(mockGithub.rest.issues.removeAssignees).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "other-repo",
      issue_number: 789,
      assignees: ["user1"],
    });
  });
});
