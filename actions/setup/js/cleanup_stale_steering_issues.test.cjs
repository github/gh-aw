// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mockCore = { info: vi.fn(), warning: vi.fn(), debug: vi.fn(), setFailed: vi.fn() };
const mockContext = {
  repo: { owner: "testowner", repo: "testrepo" },
  serverUrl: "https://github.com",
};

describe("cleanup_stale_steering_issues", () => {
  it("preserves the compact cleanup marker identity after tree rendering", async () => {
    const { CLEANUP_COMMENT, CLEANUP_COMMENT_MARKER } = await import("./cleanup_stale_steering_issues.cjs");
    expect(CLEANUP_COMMENT_MARKER).toBe("<!-- gh-aw:stale-steering-issue-cleanup -->");
    expect(CLEANUP_COMMENT).toBe("This steering issue was left open after its linked workflow run completed successfully. Closing it during scheduled agentic maintenance.\n\n<!-- gh-aw:stale-steering-issue-cleanup -->");
  });

  let mockGithub;
  const oldCreatedAt = new Date(Date.now() - 2 * 24 * 60 * 60 * 1000).toISOString();
  const makeIssue = (overrides = {}) => ({
    number: 101,
    title: "[WIP] Example: work in progress",
    body: "This issue was created by [the workflow run](https://github.com/testowner/testrepo/actions/runs/12345).",
    created_at: oldCreatedAt,
    ...overrides,
  });
  const mockCandidates = () => {
    mockGithub.graphql.mockResolvedValueOnce({
      repository: {
        issues: {
          nodes: [101, 102].map(number => ({ number, title: "[WIP] Stale", createdAt: oldCreatedAt })),
          pageInfo: { hasNextPage: false, endCursor: null },
        },
      },
    });
    mockGithub.rest.issues.get.mockResolvedValue({ data: { body: makeIssue().body } });
    mockGithub.rest.actions.getWorkflowRun.mockResolvedValue({ data: { status: "completed", conclusion: "success" } });
    mockGithub.paginate.mockResolvedValue([]);
  };

  beforeEach(() => {
    vi.clearAllMocks();
    vi.resetModules();
    global.core = mockCore;
    global.context = mockContext;
    mockGithub = {
      graphql: vi.fn(),
      paginate: vi.fn(),
      rest: {
        issues: {
          get: vi.fn(),
          listComments: vi.fn(),
          createComment: vi.fn(),
          update: vi.fn(),
        },
        actions: {
          getWorkflowRun: vi.fn(),
        },
      },
    };
    global.github = mockGithub;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("comments on and closes stale steering issues linked to successful runs", async () => {
    mockGithub.graphql.mockResolvedValueOnce({
      repository: {
        issues: {
          nodes: [{ number: 101, title: "[WIP] Example: work in progress", createdAt: oldCreatedAt }],
          pageInfo: { hasNextPage: false, endCursor: null },
        },
      },
    });
    mockGithub.rest.issues.get.mockResolvedValueOnce({ data: { body: makeIssue().body } });
    mockGithub.paginate.mockResolvedValueOnce([]);
    mockGithub.rest.actions.getWorkflowRun.mockResolvedValue({ data: { status: "completed", conclusion: "success" } });

    const { main, CLEANUP_COMMENT } = await import("./cleanup_stale_steering_issues.cjs");
    await main();

    expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("issues(first: 100, after: $cursor, states: [OPEN]"), {
      owner: "testowner",
      repo: "testrepo",
      cursor: null,
    });
    expect(mockGithub.rest.issues.get).toHaveBeenCalledWith({ owner: "testowner", repo: "testrepo", issue_number: 101 });
    expect(mockGithub.rest.actions.getWorkflowRun).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      run_id: 12345,
    });
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      issue_number: 101,
      body: CLEANUP_COMMENT,
    });
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      issue_number: 101,
      state: "closed",
      state_reason: "completed",
    });
  });

  it("does not touch active, failed, malformed, or unrelated issues", async () => {
    const issues = [
      { number: 2, title: "Unrelated issue", createdAt: oldCreatedAt },
      { number: 3, title: "[WIP] Missing link", createdAt: oldCreatedAt },
      { number: 4, title: "[WIP] Invalid link", createdAt: oldCreatedAt },
      { number: 5, title: "[WIP] Failed run", createdAt: oldCreatedAt },
      { number: 6, title: "[WIP] Active run", createdAt: oldCreatedAt },
      { number: 1, title: "[WIP] Active example", createdAt: new Date().toISOString() },
    ];
    mockGithub.graphql.mockResolvedValueOnce({
      repository: { issues: { nodes: issues, pageInfo: { hasNextPage: false, endCursor: null } } },
    });
    mockGithub.rest.issues.get
      .mockResolvedValueOnce({ data: { body: "No workflow run link" } })
      .mockResolvedValueOnce({ data: { body: "https://example.com/owner/repo/actions/runs/12345" } })
      .mockResolvedValueOnce({ data: { body: makeIssue().body } })
      .mockResolvedValueOnce({ data: { body: makeIssue().body } });
    mockGithub.rest.actions.getWorkflowRun.mockResolvedValueOnce({ data: { status: "completed", conclusion: "failure" } }).mockResolvedValueOnce({ data: { status: "in_progress", conclusion: null } });

    const { main } = await import("./cleanup_stale_steering_issues.cjs");
    await main();

    expect(mockGithub.rest.actions.getWorkflowRun).toHaveBeenCalledTimes(2);
    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
    expect(mockGithub.rest.issues.update).not.toHaveBeenCalled();
  });

  it("does not duplicate the cleanup comment when closure needs another attempt", async () => {
    mockGithub.graphql.mockResolvedValueOnce({
      repository: {
        issues: {
          nodes: [{ number: 101, title: "[WIP] Example: work in progress", createdAt: oldCreatedAt }],
          pageInfo: { hasNextPage: false, endCursor: null },
        },
      },
    });
    mockGithub.rest.issues.get.mockResolvedValueOnce({ data: { body: makeIssue().body } });
    mockGithub.paginate.mockResolvedValueOnce([{ body: "Already handled <!-- gh-aw:stale-steering-issue-cleanup -->" }]);
    mockGithub.rest.actions.getWorkflowRun.mockResolvedValue({ data: { status: "completed", conclusion: "success" } });

    const { main } = await import("./cleanup_stale_steering_issues.cjs");
    await main();

    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
    expect(mockGithub.rest.issues.update).toHaveBeenCalledTimes(1);
  });

  it.each([{ status: 404 }, { response: { status: 404 } }])("skips missing workflow runs and continues cleanup (%j)", async status => {
    mockCandidates();
    mockGithub.rest.actions.getWorkflowRun.mockRejectedValueOnce(Object.assign(new Error("Not Found"), status));
    const { main } = await import("./cleanup_stale_steering_issues.cjs");
    await main();

    expect(mockGithub.rest.issues.update).toHaveBeenCalledTimes(1);
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 102 }));
    expect(mockCore.info).toHaveBeenCalledWith("Skipping stale steering issue #101: workflow run 12345 no longer exists");
    expect(mockCore.warning).not.toHaveBeenCalled();
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it.each(["get", "getWorkflowRun", "listComments", "createComment", "update"])("reports a %s failure while continuing with later candidates", async operation => {
    mockCandidates();
    const failedOperation = operation === "getWorkflowRun" ? mockGithub.rest.actions.getWorkflowRun : operation === "listComments" ? mockGithub.paginate : mockGithub.rest.issues[operation];
    failedOperation.mockRejectedValueOnce(Object.assign(new Error("Resource not accessible by integration"), { status: 403 }));
    const { main } = await import("./cleanup_stale_steering_issues.cjs");
    await main();

    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 102 }));
    if (operation === "listComments" || operation === "createComment") {
      expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 101 }));
    }
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringMatching(/#101:[\s\S]*Resource not accessible by integration/));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringMatching(/#101:[\s\S]*Resource not accessible by integration/));
    expect(failedOperation).toHaveBeenCalledTimes(2);
  });

  it("does not treat a missing issue as a missing workflow run", async () => {
    mockCandidates();
    mockGithub.rest.issues.get.mockRejectedValueOnce(Object.assign(new Error("Not Found"), { status: 404 }));
    const { main } = await import("./cleanup_stale_steering_issues.cjs");
    await main();

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("#101: Not Found"));
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("#101: Not Found"));
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 102 }));
  });

  it.each(["createComment", "update"])("retries a rate-limited %s using the longer installation retry policy", async operation => {
    vi.useFakeTimers();
    mockCandidates();
    mockGithub.rest.issues[operation].mockRejectedValueOnce(Object.assign(new Error("API rate limit exceeded for installation"), { status: 403 }));
    const startedAt = Date.now();
    const { main } = await import("./cleanup_stale_steering_issues.cjs");
    const cleanup = main();
    await vi.runAllTimersAsync();
    await cleanup;

    expect(Date.now() - startedAt).toBeGreaterThanOrEqual(30_000);
    expect(mockGithub.rest.issues[operation]).toHaveBeenCalledTimes(3);
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 102 }));
    expect(mockCore.setFailed).not.toHaveBeenCalled();
  });

  it.each(["createComment", "update"])("continues cleanup when %s exhausts its rate-limit retries", async operation => {
    vi.useFakeTimers();
    mockCandidates();
    mockGithub.rest.issues[operation].mockImplementation(async ({ issue_number }) => {
      if (issue_number === 101) {
        throw Object.assign(new Error("API rate limit exceeded for installation"), { status: 403 });
      }
      return { data: {} };
    });
    const { main } = await import("./cleanup_stale_steering_issues.cjs");
    const cleanup = main();
    await vi.runAllTimersAsync();
    await cleanup;

    expect(mockGithub.rest.issues[operation]).toHaveBeenCalledTimes(7);
    expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 102 }));
    if (operation === "createComment") {
      expect(mockGithub.rest.issues.update).toHaveBeenCalledWith(expect.objectContaining({ issue_number: 101 }));
    }
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("#101:"));
    expect(mockCore.setFailed).toHaveBeenCalledTimes(1);
    expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("#101:"));
  });

  it("paginates only through issues created before the stale cutoff", async () => {
    const { getStaleSteeringIssues, STEERING_ISSUE_MAX_AGE_MS } = await import("./cleanup_stale_steering_issues.cjs");
    const cutoff = Date.now() - STEERING_ISSUE_MAX_AGE_MS;
    const staleCreatedAt = new Date(cutoff - 60_000).toISOString();
    const recentCreatedAt = new Date(cutoff + 60_000).toISOString();
    mockGithub.graphql
      .mockResolvedValueOnce({
        repository: {
          issues: {
            nodes: [{ number: 101, title: "[WIP] Stale", createdAt: staleCreatedAt }],
            pageInfo: { hasNextPage: true, endCursor: "cursor-1" },
          },
        },
      })
      .mockResolvedValueOnce({
        repository: {
          issues: {
            nodes: [{ number: 102, title: "[WIP] Recent", createdAt: recentCreatedAt }],
            pageInfo: { hasNextPage: true, endCursor: "cursor-2" },
          },
        },
      });

    const candidates = await getStaleSteeringIssues("testowner", "testrepo", cutoff);

    expect(candidates.map(issue => issue.number)).toEqual([101]);
    expect(mockGithub.graphql).toHaveBeenCalledTimes(2);
    expect(mockGithub.graphql.mock.calls[1][1]).toEqual({ owner: "testowner", repo: "testrepo", cursor: "cursor-1" });
  });

  it("extracts only run IDs belonging to the current repository", async () => {
    const { getWorkflowRunId } = await import("./cleanup_stale_steering_issues.cjs");

    expect(getWorkflowRunId("Run: [the workflow run](https://github.com/testowner/testrepo/actions/runs/12345)", "testowner", "testrepo")).toBe(12345);
    expect(getWorkflowRunId("[the workflow run](https://github.com/other/repo/actions/runs/12345)", "testowner", "testrepo")).toBeNull();
    expect(getWorkflowRunId("[the workflow run](https://github.com/testowner/testrepo/actions/runs/not-a-number)", "testowner", "testrepo")).toBeNull();
  });
});
