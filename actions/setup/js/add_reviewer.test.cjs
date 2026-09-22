import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock the global objects that GitHub Actions provides
const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  notice: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

const mockGithub = {
  graphql: vi.fn().mockResolvedValue({}),
  rest: {
    pulls: {
      get: vi.fn().mockResolvedValue({
        data: {
          requested_reviewers: [{ login: "existing-reviewer" }],
          requested_teams: [],
        },
      }),
      listReviews: vi.fn().mockResolvedValue({ data: [{ id: 1, user: { login: "reviewer-a" }, state: "COMMENTED" }] }),
      requestReviewers: vi.fn().mockResolvedValue({}),
    },
    users: {
      getByUsername: vi.fn().mockResolvedValue({ data: { node_id: "BOT_kgDOCnlnWA" } }),
    },
  },
};

const mockContext = {
  eventName: "pull_request",
  repo: {
    owner: "testowner",
    repo: "testrepo",
  },
  payload: {
    pull_request: {
      number: 123,
    },
  },
};

// Set up global mocks before importing the module
global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

describe("add_reviewer (Handler Factory Architecture)", () => {
  let handler;

  beforeEach(async () => {
    // Reset all mocks before each test
    vi.clearAllMocks();

    // Reset context
    global.context = mockContext;

    // Load the module and create handler
    const { main } = require("./add_reviewer.cjs");
    handler = await main({
      max: 10,
      allowed: ["user1", "user2", "copilot"],
    });
  });

  it("should return a function from main()", async () => {
    const { main } = require("./add_reviewer.cjs");
    const result = await main({});
    expect(typeof result).toBe("function");
  });

  it("should add reviewers to PR", async () => {
    const message = {
      type: "add_reviewer",
      reviewers: ["user1", "user2"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.prNumber).toBe(123);
    expect(result.number).toBe(123);
    expect(result.repo).toBe("testowner/testrepo");
    expect(result.reviewersAdded).toEqual(["user1", "user2"]);
    expect(result.metadata).toEqual({
      requested_reviewers: ["user1", "user2"],
      requested_team_reviewers: [],
    });
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: ["user1", "user2"],
    });
    expect(result.before_state).toEqual({
      requested_reviewers: ["existing-reviewer"],
      requested_team_reviewers: [],
      reviews: [{ id: 1, user: "reviewer-a", state: "COMMENTED" }],
    });
  });

  it("should add team reviewers to PR", async () => {
    const message = {
      type: "add_reviewer",
      team_reviewers: ["platform-team", "security-team"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.teamReviewersAdded).toEqual(["platform-team", "security-team"]);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: [],
      team_reviewers: ["platform-team", "security-team"],
    });
  });

  it("should add copilot reviewer separately", async () => {
    mockGithub.graphql.mockResolvedValueOnce({ repository: { pullRequest: { id: "PR_NODE_ID" } } }).mockResolvedValueOnce({ requestReviews: { pullRequest: { id: "PR_NODE_ID" } } });

    const message = {
      type: "add_reviewer",
      reviewers: ["user1", "copilot"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.reviewersAdded).toEqual(["user1", "copilot"]);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledTimes(1);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: ["user1"],
    });
    expect(mockGithub.rest.users.getByUsername).toHaveBeenCalledWith({ username: "copilot-pull-request-reviewer[bot]" });
    expect(mockGithub.graphql).toHaveBeenNthCalledWith(1, expect.stringContaining("pullRequest(number: $number)"), expect.objectContaining({ owner: "testowner", repo: "testrepo", number: 123 }));
    expect(mockGithub.graphql).toHaveBeenNthCalledWith(2, expect.stringContaining("requestReviews(input"), expect.objectContaining({ pullRequestId: "PR_NODE_ID", botIds: ["BOT_kgDOCnlnWA"] }));
  });

  it("should fall back to built-in node ID when users API fails", async () => {
    mockGithub.rest.users.getByUsername.mockRejectedValueOnce(new Error("Not Found"));
    mockGithub.graphql.mockResolvedValueOnce({ repository: { pullRequest: { id: "PR_NODE_ID" } } }).mockResolvedValueOnce({ requestReviews: { pullRequest: { id: "PR_NODE_ID" } } });

    const { main } = require("./add_reviewer.cjs");
    const fallbackHandler = await main({ max: 10, allowed: ["copilot"] });

    const result = await fallbackHandler({ type: "add_reviewer", reviewers: ["copilot"] }, {});

    expect(result.success).toBe(true);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not resolve Copilot reviewer bot node ID"));
    expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("requestReviews(input"), expect.objectContaining({ botIds: ["BOT_kgDOCnlnWA"] }));
  });

  it("should cache the resolved bot node ID across calls", async () => {
    mockGithub.graphql
      .mockResolvedValueOnce({ repository: { pullRequest: { id: "PR_NODE_ID_1" } } })
      .mockResolvedValueOnce({ requestReviews: { pullRequest: { id: "PR_NODE_ID_1" } } })
      .mockResolvedValueOnce({ repository: { pullRequest: { id: "PR_NODE_ID_2" } } })
      .mockResolvedValueOnce({ requestReviews: { pullRequest: { id: "PR_NODE_ID_2" } } });

    const { main } = require("./add_reviewer.cjs");
    const cachingHandler = await main({ max: 10, allowed: ["copilot"] });

    await cachingHandler({ type: "add_reviewer", reviewers: ["copilot"] }, {});
    await cachingHandler({ type: "add_reviewer", reviewers: ["copilot"] }, {});

    // users.getByUsername should only be called once despite two copilot reviewer requests
    expect(mockGithub.rest.users.getByUsername).toHaveBeenCalledTimes(1);
  });

  it("should keep team reviewers with non-copilot reviewers when copilot is requested", async () => {
    mockGithub.graphql.mockResolvedValueOnce({ repository: { pullRequest: { id: "PR_NODE_ID" } } }).mockResolvedValueOnce({ requestReviews: { pullRequest: { id: "PR_NODE_ID" } } });

    const message = {
      type: "add_reviewer",
      reviewers: ["user1", "copilot"],
      team_reviewers: ["platform-team"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.reviewersAdded).toEqual(["user1", "copilot"]);
    expect(result.teamReviewersAdded).toEqual(["platform-team"]);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenNthCalledWith(1, {
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: ["user1"],
      team_reviewers: ["platform-team"],
    });
    expect(mockGithub.graphql).toHaveBeenCalledTimes(2);
  });

  it("should filter by allowed reviewers", async () => {
    const message = {
      type: "add_reviewer",
      reviewers: ["user1", "user2", "unauthorized"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.reviewersAdded).toEqual(["user1", "user2"]);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: ["user1", "user2"],
    });
  });

  it("should filter by allowed team reviewers", async () => {
    const { main } = require("./add_reviewer.cjs");
    const restrictedHandler = await main({ max: 10, allowed_team_reviewers: ["platform-team"] });

    const message = {
      type: "add_reviewer",
      team_reviewers: ["platform-team", "security-team"],
    };

    const result = await restrictedHandler(message, {});

    expect(result.success).toBe(true);
    expect(result.teamReviewersAdded).toEqual(["platform-team"]);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: [],
      team_reviewers: ["platform-team"],
    });
  });

  it("should enforce max count limit", async () => {
    const { main } = require("./add_reviewer.cjs");
    const limitedHandler = await main({ max: 1, allowed: ["user1", "user2"] });

    const message1 = {
      type: "add_reviewer",
      reviewers: ["user1"],
    };

    const message2 = {
      type: "add_reviewer",
      reviewers: ["user2"],
    };

    // First call should succeed
    const result1 = await limitedHandler(message1, {});
    expect(result1.success).toBe(true);

    // Second call should fail
    const result2 = await limitedHandler(message2, {});
    expect(result2.success).toBe(false);
    expect(result2.error).toContain("Max count");
  });

  it("should use explicit PR number from message", async () => {
    const { main } = require("./add_reviewer.cjs");
    handler = await main({ max: 10, allowed: ["user1"], target: "*" });
    const message = {
      type: "add_reviewer",
      reviewers: ["user1"],
      pull_request_number: 456,
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.prNumber).toBe(456);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 456,
      reviewers: ["user1"],
    });
  });

  it("should handle missing PR context", async () => {
    // Change context to remove PR
    global.context = {
      eventName: "issues",
      repo: {
        owner: "testowner",
        repo: "testrepo",
      },
      payload: {
        issue: {
          number: 123,
        },
      },
    };

    const message = {
      type: "add_reviewer",
      reviewers: ["user1"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("not running in pull request context");
    expect(mockGithub.rest.pulls.requestReviewers).not.toHaveBeenCalled();
  });

  it("should handle API errors gracefully", async () => {
    mockGithub.rest.pulls.requestReviewers.mockRejectedValueOnce(new Error("API Error"));

    const message = {
      type: "add_reviewer",
      reviewers: ["user1"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("API Error");
  });

  it("should persist before and after reviewer metadata", async () => {
    mockGithub.rest.pulls.requestReviewers.mockResolvedValueOnce({
      data: {
        requested_reviewers: [{ login: "existing-reviewer" }, { login: "user1" }],
        requested_teams: [{ slug: "platform-team" }],
      },
    });

    const result = await handler(
      {
        type: "add_reviewer",
        reviewers: ["user1"],
        team_reviewers: ["platform-team"],
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.repo).toBe("testowner/testrepo");
    expect(result.pull_request_number).toBe(123);
    expect(result.pull_request_url).toBe("https://github.com/testowner/testrepo/pull/123");
    expect(result.before_state).toEqual({
      requested_reviewers: ["existing-reviewer"],
      requested_team_reviewers: [],
      reviews: [{ id: 1, user: "reviewer-a", state: "COMMENTED" }],
    });
    expect(result.after_state).toEqual({
      requested_reviewers: ["existing-reviewer", "user1"],
      requested_team_reviewers: ["platform-team"],
      reviews: [{ id: 1, user: "reviewer-a", state: "COMMENTED" }],
    });
  });

  it("should deduplicate reviewers", async () => {
    const message = {
      type: "add_reviewer",
      reviewers: ["user1", "user2", "user1", "user2"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.reviewersAdded).toEqual(["user1", "user2"]);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: ["user1", "user2"],
    });
  });

  it("should return success with empty array when no valid reviewers", async () => {
    const message = {
      type: "add_reviewer",
      reviewers: [],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.skipped).toBe(true);
    expect(result.reviewersAdded).toEqual([]);
    expect(result.message).toContain("No valid reviewers found");
    expect(mockGithub.rest.pulls.requestReviewers).not.toHaveBeenCalled();
  });

  it("should preview in staged mode without calling API", async () => {
    const originalEnv = process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";

    try {
      const { main } = require("./add_reviewer.cjs");
      const stagedHandler = await main({ max: 10, allowed: ["user1"] });

      const message = {
        type: "add_reviewer",
        reviewers: ["user1"],
      };

      const result = await stagedHandler(message, {});

      expect(result.success).toBe(true);
      expect(result.staged).toBe(true);
      expect(result.previewInfo).toBeDefined();
      expect(result.previewInfo.reviewers).toEqual(["user1"]);
      expect(mockGithub.rest.pulls.requestReviewers).not.toHaveBeenCalled();
    } finally {
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = originalEnv;
    }
  });

  it("should handle copilot reviewer failure gracefully", async () => {
    mockGithub.rest.pulls.requestReviewers.mockResolvedValueOnce({});
    mockGithub.graphql.mockResolvedValueOnce({ repository: { pullRequest: { id: "PR_NODE_ID" } } }).mockRejectedValueOnce(new Error("Copilot not available"));

    const message = {
      type: "add_reviewer",
      reviewers: ["user1", "copilot"],
    };

    const result = await handler(message, {});

    // Should still succeed even if copilot reviewer fails
    expect(result.success).toBe(true);
    expect(result.reviewersAdded).toEqual(["user1", "copilot"]);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to add copilot as reviewer"));
  });

  it("should handle undefined reviewers as empty array", async () => {
    const message = {
      type: "add_reviewer",
      // reviewers intentionally omitted
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.reviewersAdded).toEqual([]);
    expect(result.message).toContain("No valid reviewers found");
    expect(mockGithub.rest.pulls.requestReviewers).not.toHaveBeenCalled();
  });

  it("should add only copilot when copilot is the only reviewer", async () => {
    mockGithub.graphql.mockResolvedValueOnce({ repository: { pullRequest: { id: "PR_NODE_ID" } } }).mockResolvedValueOnce({ requestReviews: { pullRequest: { id: "PR_NODE_ID" } } });

    const message = {
      type: "add_reviewer",
      reviewers: ["copilot"],
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.reviewersAdded).toEqual(["copilot"]);
    expect(mockGithub.rest.pulls.requestReviewers).not.toHaveBeenCalled();
    expect(mockGithub.graphql).toHaveBeenCalledTimes(2);
  });

  it("should honor target-repo for cross-repository reviewer requests", async () => {
    const { main } = require("./add_reviewer.cjs");
    const crossRepoHandler = await main({ max: 10, allowed: ["user1"], "target-repo": "microsoft/vscode", allowed_repos: ["microsoft/vscode"] });

    const message = {
      type: "add_reviewer",
      reviewers: ["user1"],
    };

    const result = await crossRepoHandler(message, {});

    expect(result.success).toBe(true);
    expect(result.repo).toBe("microsoft/vscode");
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "microsoft",
      repo: "vscode",
      pull_number: 123,
      reviewers: ["user1"],
    });
  });

  it("should use default config values when no options provided", async () => {
    const { main } = require("./add_reviewer.cjs");
    const defaultHandler = await main({});

    const message = {
      type: "add_reviewer",
      reviewers: ["anyuser"],
    };

    // With no allowed list, all reviewers are allowed
    const result = await defaultHandler(message, {});

    expect(result.success).toBe(true);
    expect(result.reviewersAdded).toEqual(["anyuser"]);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: ["anyuser"],
    });
  });

  it("should return error for invalid pull_request_number", async () => {
    const { main } = require("./add_reviewer.cjs");
    handler = await main({ max: 10, allowed: ["user1"], target: "*" });
    const invalidValues = ["not-a-number", null, "abc123"];

    for (const invalidValue of invalidValues) {
      vi.clearAllMocks();
      const message = {
        type: "add_reviewer",
        reviewers: ["user1"],
        pull_request_number: invalidValue,
      };

      const result = await handler(message, {});

      expect(result.success).toBe(false);
      expect(result.error).toMatch(/Invalid pull_request_number|no pull_request_number/);
      expect(mockGithub.rest.pulls.requestReviewers).not.toHaveBeenCalled();
    }
  });

  it("should filter out copilot when not in allowed list", async () => {
    const { main } = require("./add_reviewer.cjs");
    // allowed list does NOT include "copilot"
    const restrictedHandler = await main({ max: 10, allowed: ["user1", "user2"] });

    const message = {
      type: "add_reviewer",
      reviewers: ["user1", "copilot"],
    };

    const result = await restrictedHandler(message, {});

    expect(result.success).toBe(true);
    expect(result.reviewersAdded).toEqual(["user1"]);
    // Only called once — for user1 only; copilot was filtered out
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledTimes(1);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: ["user1"],
    });
  });

  it("should sanitize null and falsy values in reviewers array", async () => {
    const { main } = require("./add_reviewer.cjs");
    const permissiveHandler = await main({ max: 10, allowed: [] });

    const message = {
      type: "add_reviewer",
      // null, undefined, false, empty string, and whitespace-only strings are all stripped
      reviewers: [null, undefined, false, "", "   ", "user1", "  user2  "],
    };

    const result = await permissiveHandler(message, {});

    expect(result.success).toBe(true);
    // Only real usernames survive; whitespace is trimmed; whitespace-only strings are dropped
    expect(result.reviewersAdded).toEqual(["user1", "user2"]);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: ["user1", "user2"],
    });
  });

  it("should trim reviewer list to maxCount within a single message", async () => {
    const { main } = require("./add_reviewer.cjs");
    const tightHandler = await main({ max: 2, allowed: [] });

    const message = {
      type: "add_reviewer",
      reviewers: ["user1", "user2", "user3", "user4"],
    };

    const result = await tightHandler(message, {});

    expect(result.success).toBe(true);
    // Only first 2 reviewers should be added
    expect(result.reviewersAdded).toEqual(["user1", "user2"]);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: ["user1", "user2"],
    });
  });

  it("should enforce maxCount across reviewers and team_reviewers combined", async () => {
    const { main } = require("./add_reviewer.cjs");
    const tightHandler = await main({ max: 3, allowed: [], allowed_team_reviewers: [] });

    const message = {
      type: "add_reviewer",
      reviewers: ["user1", "user2", "user3"],
      team_reviewers: ["platform-team", "security-team"],
    };

    const result = await tightHandler(message, {});

    expect(result.success).toBe(true);
    expect(result.reviewersAdded).toEqual(["user1", "user2", "user3"]);
    expect(result.teamReviewersAdded).toEqual([]);
    expect(mockGithub.rest.pulls.requestReviewers).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 123,
      reviewers: ["user1", "user2", "user3"],
    });
  });

  it("should count staged calls toward max processedCount", async () => {
    const originalEnv = process.env.GH_AW_SAFE_OUTPUTS_STAGED;
    process.env.GH_AW_SAFE_OUTPUTS_STAGED = "true";

    try {
      const { main } = require("./add_reviewer.cjs");
      const stagedHandler = await main({ max: 1, allowed: ["user1"] });

      const message = {
        type: "add_reviewer",
        reviewers: ["user1"],
      };

      // First call in staged mode — should succeed as a preview
      const result1 = await stagedHandler(message, {});
      expect(result1.success).toBe(true);
      expect(result1.staged).toBe(true);

      // Second call should be blocked by max count (processedCount was incremented in staged mode)
      const result2 = await stagedHandler(message, {});
      expect(result2.success).toBe(false);
      expect(result2.error).toContain("Max count");
    } finally {
      process.env.GH_AW_SAFE_OUTPUTS_STAGED = originalEnv;
    }
  });
});
