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
  paginate: vi.fn(),
  rest: {
    issues: {
      update: vi.fn(),
      listMilestones: vi.fn(),
      createMilestone: vi.fn(),
      getMilestone: vi.fn(),
    },
  },
};

global.core = mockCore;
global.context = mockContext;
global.github = mockGithub;

describe("assign_milestone (Handler Factory Architecture)", () => {
  let handler;

  /**
   * Sets up the paginate mock to call the callback with one page of `items`.
   * This simulates the Octokit paginate behavior: the callback receives
   * `{ data: items }` and a `done` function, then populates milestoneCache.
   * @param {Array} items
   */
  function mockPaginateWith(items) {
    mockGithub.paginate.mockImplementation(async (_method, _params, callback) => {
      if (callback) {
        const done = vi.fn();
        callback({ data: items }, done);
      }
      return items;
    });
  }

  beforeEach(async () => {
    vi.clearAllMocks();

    const { main } = require("./assign_milestone.cjs");
    handler = await main({
      max: 10,
      allowed: [],
      target: "*",
    });
  });

  it("should return a function from main()", async () => {
    const { main } = require("./assign_milestone.cjs");
    const result = await main({});
    expect(typeof result).toBe("function");
  });

  it("should assign milestone successfully", async () => {
    mockGithub.rest.issues.update.mockResolvedValue({});

    const message = {
      type: "assign_milestone",
      issue_number: 42,
      milestone_number: 5,
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.issue_number).toBe(42);
    expect(result.milestone_number).toBe(5);
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 42,
      milestone: 5,
    });
  });

  it("should validate against allowed milestones list", async () => {
    const { main } = require("./assign_milestone.cjs");
    const handlerWithAllowed = await main({
      max: 10,
      allowed: ["v1.0", "v2.0"],
    });

    mockGithub.rest.issues.getMilestone.mockResolvedValue({ data: { number: 5, title: "v1.0" } });
    mockGithub.rest.issues.update.mockResolvedValue({});

    const message = {
      type: "assign_milestone",
      issue_number: 42,
      milestone_number: 5,
    };

    const result = await handlerWithAllowed(message, {});

    expect(result.success).toBe(true);
    expect(mockGithub.rest.issues.getMilestone).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      milestone_number: 5,
    });
    expect(mockGithub.rest.issues.update).toHaveBeenCalled();
  });

  it("should reject milestone not in allowed list", async () => {
    const { main } = require("./assign_milestone.cjs");
    const handlerWithAllowed = await main({
      max: 10,
      allowed: ["v1.0", "v2.0"],
    });

    mockGithub.rest.issues.getMilestone.mockResolvedValue({ data: { number: 6, title: "v3.0" } });

    const message = {
      type: "assign_milestone",
      issue_number: 42,
      milestone_number: 6,
    };

    const result = await handlerWithAllowed(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("is not in the allowed list");
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
  });

  it("should respect max count configuration", async () => {
    const { main } = require("./assign_milestone.cjs");
    const limitedHandler = await main({ max: 1 });

    mockGithub.rest.issues.update.mockResolvedValue({});

    const message1 = {
      type: "assign_milestone",
      issue_number: 1,
      milestone_number: 5,
    };

    const message2 = {
      type: "assign_milestone",
      issue_number: 2,
      milestone_number: 5,
    };

    // First call should succeed
    const result1 = await limitedHandler(message1, {});
    expect(result1.success).toBe(true);

    // Second call should fail
    const result2 = await limitedHandler(message2, {});
    expect(result2.success).toBe(false);
    expect(result2.error).toContain("Max count");
  });

  it("should handle API errors gracefully", async () => {
    const apiError = new Error("API rate limit exceeded");
    mockGithub.rest.issues.update.mockRejectedValue(apiError);

    const message = {
      type: "assign_milestone",
      issue_number: 42,
      milestone_number: 5,
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("API rate limit exceeded");
  });

  it("should handle invalid issue numbers", async () => {
    const message = {
      type: "assign_milestone",
      issue_number: -1,
      milestone_number: 5,
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid issue_number");
  });

  it("should handle invalid milestone numbers", async () => {
    const message = {
      type: "assign_milestone",
      issue_number: 42,
      milestone_number: "not-a-number",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Either milestone_number or milestone_title must be provided");
  });

  it("should resolve a temporary ID for issue_number", async () => {
    mockGithub.rest.issues.update.mockResolvedValue({});

    const message = {
      type: "assign_milestone",
      issue_number: "aw_abc123",
      milestone_number: 5,
    };

    const resolvedTemporaryIds = {
      aw_abc123: { repo: "test-owner/test-repo", number: 42 },
    };

    const result = await handler(message, resolvedTemporaryIds);

    expect(result.success).toBe(true);
    expect(result.issue_number).toBe(42);
    expect(result.milestone_number).toBe(5);
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 42,
      milestone: 5,
    });
  });

  it("should defer when temporary ID is not yet resolved", async () => {
    const message = {
      type: "assign_milestone",
      issue_number: "aw_pending1",
      milestone_number: 5,
    };

    // No resolved temporary IDs provided
    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.deferred).toBe(true);
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
  });

  it("should resolve milestone by title when milestone_number is not provided", async () => {
    const { main } = require("./assign_milestone.cjs");
    const handlerWithTitle = await main({ max: 10, target: "*" });

    mockPaginateWith([
      { number: 5, title: "v1.0" },
      { number: 6, title: "v2.0" },
    ]);
    mockGithub.rest.issues.update.mockResolvedValue({});

    const message = {
      type: "assign_milestone",
      issue_number: 42,
      milestone_title: "v1.0",
    };

    const result = await handlerWithTitle(message, {});

    expect(result.success).toBe(true);
    expect(result.milestone_number).toBe(5);
    expect(mockGithub.paginate).toHaveBeenCalled();
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 42,
      milestone: 5,
    });
  });

  it("should error when milestone_title not found and auto_create is false", async () => {
    const { main } = require("./assign_milestone.cjs");
    const handlerNoAutoCreate = await main({ max: 10 });

    mockPaginateWith([{ number: 5, title: "v1.0" }]);

    const message = {
      type: "assign_milestone",
      issue_number: 42,
      milestone_title: "v3.0",
    };

    const result = await handlerNoAutoCreate(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("not found in repository");
    expect(result.error).toContain("auto_create: true");
    expect(mockGithub.rest.issues.createMilestone).not.toHaveBeenCalled();
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
  });

  it("should auto-create milestone when auto_create is true and title not found", async () => {
    const { main } = require("./assign_milestone.cjs");
    const handlerAutoCreate = await main({ max: 10, auto_create: true, target: "*" });

    mockPaginateWith([]);
    mockGithub.rest.issues.createMilestone.mockResolvedValue({
      data: { number: 7, title: "v3.0" },
    });
    mockGithub.rest.issues.update.mockResolvedValue({});

    const message = {
      type: "assign_milestone",
      issue_number: 42,
      milestone_title: "v3.0",
    };

    const result = await handlerAutoCreate(message, {});

    expect(result.success).toBe(true);
    expect(result.milestone_number).toBe(7);
    expect(mockGithub.rest.issues.createMilestone).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      title: "v3.0",
    });
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 42,
      milestone: 7,
    });
  });

  it("should error when milestone number not found in repository", async () => {
    const { main } = require("./assign_milestone.cjs");
    const handlerWithAllowed = await main({
      max: 10,
      allowed: ["v1.0", "v2.0"],
    });

    mockGithub.rest.issues.getMilestone.mockRejectedValue(new Error("Not Found"));

    const message = {
      type: "assign_milestone",
      issue_number: 42,
      milestone_number: 99,
    };

    const result = await handlerWithAllowed(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("not found or failed to validate");
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
  });

  it("should error when neither milestone_number nor milestone_title is provided", async () => {
    const message = {
      type: "assign_milestone",
      issue_number: 42,
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toBe("Either milestone_number or milestone_title must be provided");
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
  });

  describe("target-repo support", () => {
    it("should use target-repo config for all API calls", async () => {
      const { main } = require("./assign_milestone.cjs");
      const handlerWithTarget = await main({
        max: 10,
        "target-repo": "external-org/external-repo",
      });

      mockGithub.rest.issues.update.mockResolvedValue({});
      const paginateCalls = [];
      mockGithub.paginate.mockImplementation(async (_method, params, callback) => {
        paginateCalls.push(params);
        if (callback) {
          const done = vi.fn();
          callback({ data: [] }, done);
        }
        return [];
      });

      const result = await handlerWithTarget({ type: "assign_milestone", issue_number: 42, milestone_number: 5 }, {});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ owner: "external-org", repo: "external-repo" }));
    });

    it("should use context.repo as default when no target-repo configured", async () => {
      mockGithub.rest.issues.update.mockResolvedValue({});

      const result = await handler({ type: "assign_milestone", issue_number: 42, milestone_number: 5 }, {});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ owner: "test-owner", repo: "test-repo" }));
    });

    it("should use repo from message when allowed_repos is configured", async () => {
      const { main } = require("./assign_milestone.cjs");
      const handlerWithAllowedRepos = await main({
        max: 10,
        "target-repo": "default-org/default-repo",
        allowed_repos: ["cross-org/cross-repo"],
      });

      mockGithub.rest.issues.update.mockResolvedValue({});

      const result = await handlerWithAllowedRepos({ type: "assign_milestone", issue_number: 42, milestone_number: 5, repo: "cross-org/cross-repo" }, {});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ owner: "cross-org", repo: "cross-repo" }));
    });

    it("should reject repo not in allowed_repos list", async () => {
      const { main } = require("./assign_milestone.cjs");
      const handlerWithAllowedRepos = await main({
        max: 10,
        "target-repo": "default-org/default-repo",
        allowed_repos: ["allowed-org/allowed-repo"],
      });

      const result = await handlerWithAllowedRepos({ type: "assign_milestone", issue_number: 42, milestone_number: 5, repo: "unauthorized-org/unauthorized-repo" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("not in the allowed-repos list");
    });
  });

  describe("cross-repo milestone cache isolation", () => {
    it("should not return cached milestone from repo1 when querying repo2 (cache collision)", async () => {
      const { main } = require("./assign_milestone.cjs");
      const crossRepoHandler = await main({
        max: 20,
        allowed_repos: ["org/repo1", "org/repo2"],
      });

      // repo1: v1.0 = #5; repo2: v1.0 = #3 — same title, different numbers
      mockGithub.paginate.mockImplementation(async (_method, params, callback) => {
        const data = params.repo === "repo1" ? [{ title: "v1.0", number: 5 }] : [{ title: "v1.0", number: 3 }];
        if (callback) {
          const done = vi.fn();
          callback({ data }, done);
        }
        return data;
      });
      mockGithub.rest.issues.update.mockResolvedValue({});

      // First call: repo1
      const result1 = await crossRepoHandler({ type: "assign_milestone", issue_number: 10, milestone_title: "v1.0", repo: "org/repo1" }, {});
      expect(result1.success).toBe(true);
      expect(result1.milestone_number).toBe(5);

      // Second call: repo2 — must NOT reuse repo1's cached #5
      const result2 = await crossRepoHandler({ type: "assign_milestone", issue_number: 20, milestone_title: "v1.0", repo: "org/repo2" }, {});
      expect(result2.success).toBe(true);
      expect(result2.milestone_number).toBe(3);

      // The last issues.update call must target repo2 with milestone #3
      expect(mockGithub.rest.issues.update).toHaveBeenLastCalledWith(expect.objectContaining({ owner: "org", repo: "repo2", milestone: 3 }));
    });

    it("should not skip repo2 search after repo1 is exhausted (false exhaustion)", async () => {
      const { main } = require("./assign_milestone.cjs");
      const crossRepoHandler = await main({
        max: 20,
        allowed_repos: ["org/repo1", "org/repo2"],
      });

      // repo1: no milestones; repo2: sprint-42 = #7
      mockGithub.paginate.mockImplementation(async (_method, params, callback) => {
        const data = params.repo === "repo1" ? [] : [{ title: "sprint-42", number: 7 }];
        if (callback) {
          const done = vi.fn();
          callback({ data }, done);
        }
        return data;
      });
      mockGithub.rest.issues.update.mockResolvedValue({});

      // First call: repo1 — exhausts repo1, milestone not found
      const result1 = await crossRepoHandler({ type: "assign_milestone", issue_number: 10, milestone_title: "sprint-42", repo: "org/repo1" }, {});
      expect(result1.success).toBe(false);

      // Second call: repo2 — must still search even though repo1 was exhausted
      const result2 = await crossRepoHandler({ type: "assign_milestone", issue_number: 20, milestone_title: "sprint-42", repo: "org/repo2" }, {});
      expect(result2.success).toBe(true);
      expect(result2.milestone_number).toBe(7);
    });

    it("should use repo-scoped milestones in error message when title not found", async () => {
      const { main } = require("./assign_milestone.cjs");
      const crossRepoHandler = await main({
        max: 20,
        allowed_repos: ["org/repo1", "org/repo2"],
      });

      // repo1: only has "v1.0"; repo2: only has "v2.0"
      mockGithub.paginate.mockImplementation(async (_method, params, callback) => {
        const data = params.repo === "repo1" ? [{ title: "v1.0", number: 5 }] : [{ title: "v2.0", number: 3 }];
        if (callback) {
          const done = vi.fn();
          callback({ data }, done);
        }
        return data;
      });

      // Search for "missing" in repo2 — error message should list "v2.0", not "v1.0" from repo1
      await crossRepoHandler({ type: "assign_milestone", issue_number: 10, milestone_title: "v1.0", repo: "org/repo1" }, {});
      mockGithub.rest.issues.update.mockResolvedValue({});

      const result = await crossRepoHandler({ type: "assign_milestone", issue_number: 20, milestone_title: "missing", repo: "org/repo2" }, {});
      expect(result.success).toBe(false);
      // Warning should list repo2's milestones ("v2.0"), not repo1's ("v1.0")
      const warnCall = mockCore.warning.mock.calls.find(c => c[0].includes("not found in repository"));
      expect(warnCall).toBeDefined();
      expect(warnCall[0]).toContain("v2.0");
      expect(warnCall[0]).not.toContain("v1.0");
    });
  });
});
