// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";

describe("check_rate_limit", () => {
  let mockCore;
  let mockGithub;
  let mockContext;
  let checkRateLimit;

  beforeEach(async () => {
    // Mock @actions/core
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
      setFailed: vi.fn(),
    };

    // Mock @actions/github
    mockGithub = {
      rest: {
        actions: {
          listWorkflowRuns: vi.fn(),
          cancelWorkflowRun: vi.fn(),
        },
      },
    };

    // Mock context
    mockContext = {
      actor: "test-user",
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
      workflow: "test-workflow",
      eventName: "workflow_dispatch",
      runId: 123456,
    };

    // Setup global mocks
    global.core = mockCore;
    global.github = mockGithub;
    global.context = mockContext;

    // Reset environment variables
    delete process.env.GH_AW_RATE_LIMIT_MAX;
    delete process.env.GH_AW_RATE_LIMIT_WINDOW;
    delete process.env.GH_AW_RATE_LIMIT_EVENTS;

    // Reload the module to get fresh instance
    vi.resetModules();
    checkRateLimit = await import("./check_rate_limit.cjs");
  });

  it("should pass when no recent runs by actor", async () => {
    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "true");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Rate limit check passed"));
  });

  it("should pass when recent runs are below limit", async () => {
    const oneHourAgo = new Date(Date.now() - 30 * 60 * 1000); // 30 minutes ago

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [
          {
            id: 111111,
            run_number: 1,
            created_at: oneHourAgo.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
          {
            id: 222222,
            run_number: 2,
            created_at: oneHourAgo.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
        ],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "true");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Total recent runs in last 60 minutes: 2"));
  });

  it("should fail when rate limit is exceeded", async () => {
    process.env.GH_AW_RATE_LIMIT_MAX = "3";
    const recentTime = new Date(Date.now() - 10 * 60 * 1000); // 10 minutes ago

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [
          {
            id: 111111,
            run_number: 1,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
          {
            id: 222222,
            run_number: 2,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
          {
            id: 333333,
            run_number: 3,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
        ],
      },
    });

    mockGithub.rest.actions.cancelWorkflowRun.mockResolvedValue({});

    await checkRateLimit.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "false");
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Rate limit exceeded"));
    expect(mockGithub.rest.actions.cancelWorkflowRun).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      run_id: 123456,
    });
  });

  it("should only count runs by the same actor", async () => {
    const recentTime = new Date(Date.now() - 10 * 60 * 1000);

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [
          {
            id: 111111,
            run_number: 1,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
          {
            id: 222222,
            run_number: 2,
            created_at: recentTime.toISOString(),
            actor: { login: "other-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
          {
            id: 333333,
            run_number: 3,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
        ],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "true");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Total recent runs in last 60 minutes: 2"));
  });

  it("should exclude runs older than the time window", async () => {
    const twoHoursAgo = new Date(Date.now() - 120 * 60 * 1000);
    const recentTime = new Date(Date.now() - 10 * 60 * 1000);

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [
          {
            id: 111111,
            run_number: 1,
            created_at: twoHoursAgo.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
          {
            id: 222222,
            run_number: 2,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
        ],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "true");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Total recent runs in last 60 minutes: 1"));
  });

  it("should exclude the current run from the count", async () => {
    const recentTime = new Date(Date.now() - 10 * 60 * 1000);

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [
          {
            id: 123456, // Current run ID
            run_number: 1,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "in_progress",
          },
          {
            id: 222222,
            run_number: 2,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
        ],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "true");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Total recent runs in last 60 minutes: 1"));
  });

  it("should exclude cancelled runs from the count", async () => {
    const recentTime = new Date(Date.now() - 10 * 60 * 1000);

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [
          {
            id: 111111,
            run_number: 1,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
            conclusion: "cancelled",
          },
          {
            id: 222222,
            run_number: 2,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
            conclusion: "success",
          },
          {
            id: 333333,
            run_number: 3,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
            conclusion: "cancelled",
          },
        ],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "true");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Total recent runs in last 60 minutes: 1"));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping run 111111 - cancelled"));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping run 333333 - cancelled"));
  });

  it("should exclude runs that lasted less than 15 seconds", async () => {
    const recentTime = new Date(Date.now() - 10 * 60 * 1000);
    const tenSecondsLater = new Date(recentTime.getTime() + 10 * 1000);
    const twentySecondsLater = new Date(recentTime.getTime() + 20 * 1000);

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [
          {
            id: 111111,
            run_number: 1,
            created_at: recentTime.toISOString(),
            updated_at: tenSecondsLater.toISOString(), // 10 seconds duration
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
          {
            id: 222222,
            run_number: 2,
            created_at: recentTime.toISOString(),
            updated_at: twentySecondsLater.toISOString(), // 20 seconds duration
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
        ],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "true");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Total recent runs in last 60 minutes: 1"));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Skipping run 111111 - ran for less than 15s"));
  });

  it("should only count specified event types when events filter is set", async () => {
    process.env.GH_AW_RATE_LIMIT_EVENTS = "workflow_dispatch,issue_comment";
    const recentTime = new Date(Date.now() - 10 * 60 * 1000);

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [
          {
            id: 111111,
            run_number: 1,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
          {
            id: 222222,
            run_number: 2,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "push",
            status: "completed",
          },
          {
            id: 333333,
            run_number: 3,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "issue_comment",
            status: "completed",
          },
        ],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "true");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Total recent runs in last 60 minutes: 2"));
  });

  it("should skip rate limiting if current event is not in the events filter", async () => {
    process.env.GH_AW_RATE_LIMIT_EVENTS = "issue_comment,pull_request";
    mockContext.eventName = "workflow_dispatch";

    await checkRateLimit.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "true");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Event 'workflow_dispatch' is not subject to rate limiting"));
    expect(mockGithub.rest.actions.listWorkflowRuns).not.toHaveBeenCalled();
  });

  it("should use custom max and window values", async () => {
    process.env.GH_AW_RATE_LIMIT_MAX = "10";
    process.env.GH_AW_RATE_LIMIT_WINDOW = "30";

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("max=10 runs per 30 minutes"));
  });

  it("should short-circuit when max is exceeded during pagination", async () => {
    process.env.GH_AW_RATE_LIMIT_MAX = "2";
    const recentTime = new Date(Date.now() - 10 * 60 * 1000);

    // First page returns 2 runs (exceeds limit)
    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValueOnce({
      data: {
        workflow_runs: [
          {
            id: 111111,
            run_number: 1,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
          {
            id: 222222,
            run_number: 2,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
        ],
      },
    });

    mockGithub.rest.actions.cancelWorkflowRun.mockResolvedValue({});

    await checkRateLimit.main();

    // Should only call once, not fetch second page
    expect(mockGithub.rest.actions.listWorkflowRuns).toHaveBeenCalledTimes(1);
    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "false");
  });

  it("should fail-open on API errors", async () => {
    mockGithub.rest.actions.listWorkflowRuns.mockRejectedValue(new Error("API error"));

    await checkRateLimit.main();

    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Rate limit check failed"));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Allowing workflow to proceed"));
    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "true");
  });

  it("should continue even if cancellation fails", async () => {
    process.env.GH_AW_RATE_LIMIT_MAX = "1";
    const recentTime = new Date(Date.now() - 10 * 60 * 1000);

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [
          {
            id: 111111,
            run_number: 1,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
        ],
      },
    });

    mockGithub.rest.actions.cancelWorkflowRun.mockRejectedValue(new Error("Cancellation failed"));

    await checkRateLimit.main();

    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Failed to cancel workflow run"));
    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "false");
  });

  it("should provide breakdown by event type", async () => {
    const recentTime = new Date(Date.now() - 10 * 60 * 1000);

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [
          {
            id: 111111,
            run_number: 1,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
          {
            id: 222222,
            run_number: 2,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "issue_comment",
            status: "completed",
          },
          {
            id: 333333,
            run_number: 3,
            created_at: recentTime.toISOString(),
            actor: { login: "test-user" },
            event: "workflow_dispatch",
            status: "completed",
          },
        ],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Breakdown by event type:"));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("workflow_dispatch: 2 runs"));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("issue_comment: 1 runs"));
  });

  it("should skip rate limiting for non-programmatic events when no events filter is set", async () => {
    mockContext.eventName = "push";

    await checkRateLimit.main();

    expect(mockCore.setOutput).toHaveBeenCalledWith("rate_limit_ok", "true");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Event 'push' is not a programmatic trigger"));
    expect(mockGithub.rest.actions.listWorkflowRuns).not.toHaveBeenCalled();
  });

  it("should use workflow file from GITHUB_WORKFLOW_REF when available", async () => {
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/test.lock.yml@refs/heads/main";

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Using workflow file: test.lock.yml"));
    expect(mockGithub.rest.actions.listWorkflowRuns).toHaveBeenCalledWith(
      expect.objectContaining({
        workflow_id: "test.lock.yml",
      })
    );
  });

  it("should fall back to workflow name when GITHUB_WORKFLOW_REF is not parseable", async () => {
    process.env.GITHUB_WORKFLOW_REF = "invalid-format";

    mockGithub.rest.actions.listWorkflowRuns.mockResolvedValue({
      data: {
        workflow_runs: [],
      },
    });

    await checkRateLimit.main();

    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Using workflow name: test-workflow (fallback"));
  });
});
