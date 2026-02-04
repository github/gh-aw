// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { main: createDiscussionMain } = require("./create_discussion.cjs");
const { resetIssuesToAssignCopilot } = require("./create_issue.cjs");

describe("create_discussion fallback with close_older_discussions", () => {
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
              number: 456,
              html_url: "https://github.com/owner/repo/issues/456",
              title: "Test Issue (Fallback)",
            },
          }),
          createComment: vi.fn().mockResolvedValue({
            data: {
              id: 999,
              html_url: "https://github.com/owner/repo/issues/123#issuecomment-999",
            },
          }),
          update: vi.fn().mockResolvedValue({
            data: {
              number: 123,
              html_url: "https://github.com/owner/repo/issues/123",
            },
          }),
        },
        search: {
          issuesAndPullRequests: vi.fn().mockResolvedValue({
            data: {
              total_count: 1,
              items: [
                {
                  number: 123,
                  title: "Old Discussion Report",
                  html_url: "https://github.com/owner/repo/issues/123",
                  labels: [],
                  state: "open",
                },
              ],
            },
          }),
        },
      },
      graphql: vi.fn().mockRejectedValue(new Error("Resource not accessible by personal access token")),
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
    process.env.GITHUB_SERVER_URL = "https://github.com";
  });

  afterEach(() => {
    // Restore environment
    process.env = originalEnv;
    vi.clearAllMocks();
  });

  it("should close older issues when close_older_discussions is enabled and fallback to issue occurs", async () => {
    // Create handler with close_older_discussions enabled
    const handler = await createDiscussionMain({
      max: 5,
      fallback_to_issue: true,
      close_older_discussions: true,
    });

    // Call handler with a discussion message
    const result = await handler(
      {
        title: "Test Discussion Report",
        body: "This is a test discussion content.",
      },
      {}
    );

    // Verify fallback to issue occurred
    expect(result.success).toBe(true);
    expect(result.fallback).toBe("issue");
    expect(result.number).toBe(456);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Close older discussions enabled"));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Falling back to create-issue"));

    // Verify issue was created
    expect(mockGithub.rest.issues.create).toHaveBeenCalledWith(
      expect.objectContaining({
        owner: "test-owner",
        repo: "test-repo",
        title: "Test Discussion Report",
        body: expect.stringContaining("This was intended to be a discussion"),
      })
    );

    // Verify search for older issues was performed
    expect(mockGithub.rest.search.issuesAndPullRequests).toHaveBeenCalledWith({
      q: 'repo:test-owner/test-repo is:issue is:open "gh-aw-workflow-id: test-workflow" in:body',
      per_page: 50,
    });

    // Verify comment was added to older issue
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      body: expect.stringContaining("This issue is being closed as outdated"),
    });

    // Verify older issue was closed as "not planned"
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      issue_number: 123,
      state: "closed",
      state_reason: "not_planned",
    });
  });

  it("should not close older issues when close_older_discussions is disabled", async () => {
    // Create handler WITHOUT close_older_discussions
    const handler = await createDiscussionMain({
      max: 5,
      fallback_to_issue: true,
      close_older_discussions: false,
    });

    // Call handler with a discussion message
    const result = await handler(
      {
        title: "Test Discussion Report",
        body: "This is a test discussion content.",
      },
      {}
    );

    // Verify fallback to issue occurred
    expect(result.success).toBe(true);
    expect(result.fallback).toBe("issue");
    expect(result.number).toBe(456);

    // Verify issue was created
    expect(mockGithub.rest.issues.create).toHaveBeenCalled();

    // Verify search for older issues was NOT performed
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();

    // Verify no comments were added
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();

    // Verify no issues were closed
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
  });

  it("should handle permissions error during discussion fetch and apply close-older-issues", async () => {
    // Create handler with close_older_discussions enabled
    const handler = await createDiscussionMain({
      max: 5,
      fallback_to_issue: true,
      close_older_discussions: true,
      title_prefix: "[Report] ",
    });

    // Call handler with a discussion message
    const result = await handler(
      {
        title: "Weekly Report",
        body: "This is the weekly report content.",
      },
      {}
    );

    // Verify fallback to issue occurred with permissions error
    expect(result.success).toBe(true);
    expect(result.fallback).toBe("issue");
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("permissions"));

    // Verify older issue cleanup was triggered
    expect(mockGithub.rest.search.issuesAndPullRequests).toHaveBeenCalled();
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(
      expect.objectContaining({
        state: "closed",
        state_reason: "not_planned",
      })
    );
  });
});
