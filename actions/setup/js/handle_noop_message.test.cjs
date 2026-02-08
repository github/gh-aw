// @ts-check

const { describe, it, expect, beforeEach, vi } = require("vitest");

describe("handle_noop_message", () => {
  let mockCore;
  let mockGithub;
  let mockContext;
  let originalEnv;

  beforeEach(() => {
    // Save original environment
    originalEnv = { ...process.env };

    // Mock core
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
    };

    // Mock GitHub API
    mockGithub = {
      rest: {
        search: {
          issuesAndPullRequests: vi.fn(),
        },
        issues: {
          create: vi.fn(),
          createComment: vi.fn(),
        },
      },
    };

    // Mock context
    mockContext = {
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
    };

    // Setup globals
    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;
  });

  afterEach(() => {
    // Restore environment
    process.env = originalEnv;
    vi.clearAllMocks();
  });

  it("should skip if no noop message is present", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "";

    const { main } = require("./handle_noop_message.cjs");
    await main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No no-op message found, skipping"));
    expect(mockGithub.rest.search.issuesAndPullRequests).not.toHaveBeenCalled();
  });

  it("should create agent runs issue if it doesn't exist", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/123456";
    process.env.GH_AW_NOOP_MESSAGE = "No updates needed";

    // Mock search to return no results
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 0,
        items: [],
      },
    });

    // Mock issue creation
    mockGithub.rest.issues.create.mockResolvedValue({
      data: {
        number: 42,
        node_id: "MDU6SXNzdWU0Mg==",
        html_url: "https://github.com/test-owner/test-repo/issues/42",
      },
    });

    // Mock comment creation
    mockGithub.rest.issues.createComment.mockResolvedValue({
      data: {
        id: 1,
        html_url: "https://github.com/test-owner/test-repo/issues/42#issuecomment-1",
      },
    });

    const { main } = require("./handle_noop_message.cjs");
    await main();

    // Verify search was performed
    expect(mockGithub.rest.search.issuesAndPullRequests).toHaveBeenCalledWith({
      q: expect.stringContaining("[agentic-workflows] Agent runs"),
      per_page: 1,
    });

    // Verify issue was created with correct title
    const createCall = mockGithub.rest.issues.create.mock.calls[0][0];
    expect(createCall.title).toBe("[agentic-workflows] Agent runs");
    expect(createCall.labels).toContain("agentic-workflows");
    expect(createCall.body).toContain("tracks all no-op runs");

    // Verify comment was posted
    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.issue_number).toBe(42);
    expect(commentCall.body).toContain("Test Workflow");
    expect(commentCall.body).toContain("No updates needed");
    expect(commentCall.body).toContain("123456");
  });

  it("should use existing agent runs issue if it exists", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Another Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/789";
    process.env.GH_AW_NOOP_MESSAGE = "Everything is up to date";

    // Mock search to return existing issue
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 1,
        items: [
          {
            number: 99,
            node_id: "MDU6SXNzdWU5OQ==",
            html_url: "https://github.com/test-owner/test-repo/issues/99",
          },
        ],
      },
    });

    // Mock comment creation
    mockGithub.rest.issues.createComment.mockResolvedValue({
      data: {
        id: 2,
        html_url: "https://github.com/test-owner/test-repo/issues/99#issuecomment-2",
      },
    });

    const { main } = require("./handle_noop_message.cjs");
    await main();

    // Verify issue was not created
    expect(mockGithub.rest.issues.create).not.toHaveBeenCalled();

    // Verify comment was posted to existing issue
    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.issue_number).toBe(99);
    expect(commentCall.body).toContain("Another Workflow");
    expect(commentCall.body).toContain("Everything is up to date");
  });

  it("should handle comment creation failure gracefully", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/456";
    process.env.GH_AW_NOOP_MESSAGE = "No action required";

    // Mock existing issue
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: {
        total_count: 1,
        items: [{ number: 10, node_id: "MDU6SXNzdWUxMA==", html_url: "https://github.com/test-owner/test-repo/issues/10" }],
      },
    });

    // Mock comment creation failure
    mockGithub.rest.issues.createComment.mockRejectedValue(new Error("API rate limit exceeded"));

    const { main } = require("./handle_noop_message.cjs");
    await main();

    // Verify warning was logged but workflow didn't fail
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to post comment"));
  });

  it("should handle issue creation failure gracefully", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test-owner/test-repo/actions/runs/789";
    process.env.GH_AW_NOOP_MESSAGE = "All checks passed";

    // Mock no existing issue
    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 0, items: [] },
    });

    // Mock issue creation failure
    mockGithub.rest.issues.create.mockRejectedValue(new Error("Insufficient permissions"));

    const { main } = require("./handle_noop_message.cjs");
    await main();

    // Verify warning was logged but workflow didn't fail
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Could not create agent runs issue"));
  });

  it("should extract run ID from URL correctly", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test";
    process.env.GH_AW_RUN_URL = "https://github.com/owner/repo/actions/runs/987654321";
    process.env.GH_AW_NOOP_MESSAGE = "Done";

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });

    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = require("./handle_noop_message.cjs");
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    expect(commentCall.body).toContain("987654321");
  });

  it("should sanitize workflow name in comment", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test <script>alert('xss')</script> Workflow";
    process.env.GH_AW_RUN_URL = "https://github.com/test/test/actions/runs/123";
    process.env.GH_AW_NOOP_MESSAGE = "Clean";

    mockGithub.rest.search.issuesAndPullRequests.mockResolvedValue({
      data: { total_count: 1, items: [{ number: 1, node_id: "ID", html_url: "url" }] },
    });

    mockGithub.rest.issues.createComment.mockResolvedValue({ data: {} });

    const { main } = require("./handle_noop_message.cjs");
    await main();

    const commentCall = mockGithub.rest.issues.createComment.mock.calls[0][0];
    // Verify XSS attempt was sanitized (specific behavior depends on sanitizeContent implementation)
    expect(commentCall.body).not.toContain("<script>");
  });
});
