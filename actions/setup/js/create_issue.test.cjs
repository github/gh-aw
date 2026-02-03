// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { main, getIssuesToAssignCopilot, resetIssuesToAssignCopilot } = require("./create_issue.cjs");

describe("create_issue", () => {
  let mockGithub;
  let mockCore;
  let mockContext;
  let mockExec;
  let originalEnv;

  beforeEach(() => {
    // Save original environment
    originalEnv = { ...process.env };

    // Reset copilot assignment tracking
    resetIssuesToAssignCopilot();

    // Mock GitHub API
    mockGithub = {
      rest: {
        issues: {
          create: vi.fn().mockResolvedValue({
            data: {
              number: 123,
              html_url: "https://github.com/owner/repo/issues/123",
              title: "Test Issue",
            },
          }),
          createComment: vi.fn().mockResolvedValue({}),
        },
        search: {
          issuesAndPullRequests: vi.fn().mockResolvedValue({
            data: {
              total_count: 0,
              items: [],
            },
          }),
        },
      },
      graphql: vi.fn(),
    };

    // Mock Core
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
    };

    // Mock Context
    mockContext = {
      repo: { owner: "test-owner", repo: "test-repo" },
      runId: 12345,
      payload: {
        repository: {
          html_url: "https://github.com/test-owner/test-repo",
        },
      },
    };

    // Mock Exec
    mockExec = {
      exec: vi.fn().mockResolvedValue(0),
    };

    // Set globals
    global.github = mockGithub;
    global.core = mockCore;
    global.context = mockContext;
    global.exec = mockExec;

    // Set required environment variables
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GH_AW_WORKFLOW_SOURCE_URL = "https://github.com/owner/repo/blob/main/workflow.md";
  });

  afterEach(() => {
    // Restore original environment
    process.env = originalEnv;
    vi.clearAllMocks();
  });

  describe("basic issue creation", () => {
    it("should create issue with title and body", async () => {
      const handler = await main({});
      const result = await handler({
        title: "Test Issue",
        body: "Test body content",
      });

      expect(result.success).toBe(true);
      expect(result.number).toBe(123);
      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          owner: "test-owner",
          repo: "test-repo",
          title: "Test Issue",
          body: expect.stringContaining("Test body content"),
        })
      );
    });

    it("should use body as title when title is missing", async () => {
      const handler = await main({});
      const result = await handler({
        body: "This is the body",
      });

      expect(result.success).toBe(true);
      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "This is the body",
        })
      );
    });

    it("should use 'Agent Output' as title when both title and body are missing", async () => {
      const handler = await main({});
      const result = await handler({});

      expect(result.success).toBe(true);
      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Agent Output",
        })
      );
    });
  });

  describe("labels handling", () => {
    it("should apply default labels from config", async () => {
      const handler = await main({
        labels: ["bug", "enhancement"],
      });
      await handler({ title: "Test" });

      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          labels: expect.arrayContaining(["bug", "enhancement"]),
        })
      );
    });

    it("should merge message labels with config labels", async () => {
      const handler = await main({
        labels: ["config-label"],
      });
      await handler({
        title: "Test",
        labels: ["message-label"],
      });

      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          labels: expect.arrayContaining(["config-label", "message-label"]),
        })
      );
    });

    it("should deduplicate labels", async () => {
      const handler = await main({
        labels: ["bug", "duplicate"],
      });
      await handler({
        title: "Test",
        labels: ["duplicate", "enhancement"],
      });

      const call = mockGithub.rest.issues.create.mock.calls[0][0];
      expect(call.labels.filter(l => l === "duplicate")).toHaveLength(1);
    });

    it("should truncate labels to 64 characters", async () => {
      const longLabel = "a".repeat(100);
      const handler = await main({
        labels: [longLabel],
      });
      await handler({ title: "Test" });

      const call = mockGithub.rest.issues.create.mock.calls[0][0];
      expect(call.labels[0]).toHaveLength(64);
    });
  });

  describe("assignees handling", () => {
    it("should apply default assignees from config", async () => {
      const handler = await main({
        assignees: ["user1", "user2"],
      });
      await handler({ title: "Test" });

      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          assignees: expect.arrayContaining(["user1", "user2"]),
        })
      );
    });

    it("should filter out 'copilot' from assignees", async () => {
      const handler = await main({
        assignees: ["user1", "copilot", "user2"],
      });
      await handler({ title: "Test" });

      const call = mockGithub.rest.issues.create.mock.calls[0][0];
      expect(call.assignees).not.toContain("copilot");
      expect(call.assignees).toContain("user1");
      expect(call.assignees).toContain("user2");
    });

    it("should track copilot assignment when enabled", async () => {
      process.env.GH_AW_ASSIGN_COPILOT = "true";
      const handler = await main({
        assignees: ["copilot"],
      });
      await handler({ title: "Test" });

      const issuesToAssign = getIssuesToAssignCopilot();
      expect(issuesToAssign).toContain("test-owner/test-repo:123");
    });
  });

  describe("max count limit", () => {
    it("should respect max count limit", async () => {
      const handler = await main({ max: 2 });

      const result1 = await handler({ title: "Issue 1" });
      const result2 = await handler({ title: "Issue 2" });
      const result3 = await handler({ title: "Issue 3" });

      expect(result1.success).toBe(true);
      expect(result2.success).toBe(true);
      expect(result3.success).toBe(false);
      expect(result3.error).toContain("Max count of 2 reached");
    });
  });

  describe("title prefix", () => {
    it("should apply title prefix", async () => {
      const handler = await main({
        title_prefix: "[AUTO] ",
      });
      await handler({ title: "Test Issue" });

      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "[AUTO] Test Issue",
        })
      );
    });

    it("should not duplicate prefix if already present", async () => {
      const handler = await main({
        title_prefix: "[AUTO] ",
      });
      await handler({ title: "[AUTO] Test Issue" });

      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "[AUTO] Test Issue",
        })
      );
    });
  });

  describe("repository targeting", () => {
    it("should create issue in specified repo", async () => {
      const handler = await main({
        allowed_repos: "owner/other-repo,test-owner/test-repo",
      });
      await handler({
        title: "Test",
        repo: "owner/other-repo",
      });

      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          owner: "owner",
          repo: "other-repo",
        })
      );
    });

    it("should reject disallowed repository", async () => {
      const handler = await main({
        allowed_repos: "owner/allowed-repo",
      });
      const result = await handler({
        title: "Test",
        repo: "owner/disallowed-repo",
      });

      expect(result.success).toBe(false);
      expect(result.error).toContain("is not in the allowed-repos list");
    });

    it("should use default repo when message repo is not specified", async () => {
      const handler = await main({});
      await handler({ title: "Test" });

      expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
        expect.objectContaining({
          owner: "test-owner",
          repo: "test-repo",
        })
      );
    });
  });

  describe("temporary ID management", () => {
    it("should generate temporary ID when not provided", async () => {
      const handler = await main({});
      const result = await handler({ title: "Test" });

      expect(result.temporaryId).toMatch(/^aw_[0-9a-f]{12}$/);
    });

    it("should use provided temporary ID", async () => {
      const handler = await main({});
      const result = await handler({
        title: "Test",
        temporary_id: "aw_abc123def456",
      });

      expect(result.temporaryId).toBe("aw_abc123def456");
    });

    it("should track temporary ID after creating issue", async () => {
      const handler = await main({});

      const result = await handler({
        title: "Test Issue",
        temporary_id: "aw_test123456",
      });

      expect(result.success).toBe(true);
      expect(result.temporaryId).toBe("aw_test123456");
      expect(result.number).toBe(123);
    });
  });

  describe("error handling", () => {
    it("should handle issues disabled error gracefully", async () => {
      mockGithub.rest.issues.create.mockRejectedValueOnce(new Error("Issues has been disabled in this repository"));

      const handler = await main({});
      const result = await handler({ title: "Test" });

      expect(result.success).toBe(false);
      expect(result.error).toBe("Issues disabled for repository");
    });

    it("should handle generic API errors", async () => {
      mockGithub.rest.issues.create.mockRejectedValueOnce(new Error("API Error"));

      const handler = await main({});
      const result = await handler({ title: "Test" });

      expect(result.success).toBe(false);
      expect(result.error).toBe("API Error");
    });
  });

  describe("parent issue relationships", () => {
    it("should add 'Related to' reference when parent is numeric", async () => {
      const handler = await main({});
      await handler({
        title: "Test",
        parent: 456,
      });

      const createCall = mockGithub.rest.issues.create.mock.calls[0][0];
      expect(createCall.body).toContain("Related to #456");
    });

    it("should add parent reference for numeric parent", async () => {
      const handler = await main({});
      await handler({
        title: "Child Issue",
        parent: 456,
      });

      const createCall = mockGithub.rest.issues.create.mock.calls[0][0];
      expect(createCall.body).toContain("Related to #456");
    });
  });
});
