// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
};

const mockContext = {
  repo: {
    owner: "testowner",
    repo: "testrepo",
  },
};

global.core = mockCore;
global.context = mockContext;

describe("close_agentic_workflows_issues", () => {
  let mockGithub;

  afterEach(() => vi.unstubAllEnvs());

  beforeEach(() => {
    vi.clearAllMocks();
    vi.resetModules();
    mockGithub = {
      paginate: vi.fn(),
      graphql: vi.fn(),
      rest: {
        issues: {
          listForRepo: vi.fn(),
          createComment: vi.fn(),
        },
      },
    };
    global.github = mockGithub;
  });

  it("closes only open issues (not pull requests) with not_planned state reason via GraphQL", async () => {
    mockGithub.paginate.mockResolvedValueOnce([
      { number: 101, title: "Issue A", node_id: "I_101" },
      { number: 102, title: "PR B", node_id: "PR_102", pull_request: { url: "https://example.com/pr/102" } },
    ]);

    const module = await import("./close_agentic_workflows_issues.cjs");
    await module.main();

    expect(mockGithub.paginate).toHaveBeenCalledWith(mockGithub.rest.issues.listForRepo, {
      owner: "testowner",
      repo: "testrepo",
      labels: "agentic-workflows",
      state: "open",
      per_page: 100,
    });

    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledTimes(1);
    expect(mockGithub.rest.issues.createComment).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      issue_number: 101,
      body: module.getNoReproCommentBody(),
    });

    expect(mockGithub.graphql).toHaveBeenCalledTimes(1);
    expect(mockGithub.graphql).toHaveBeenCalledWith(module.CLOSE_ISSUE_MUTATION, {
      issueId: "I_101",
      stateReason: "NOT_PLANNED",
    });
  });

  it("still closes issues that reached the comment limit", async () => {
    mockGithub.paginate.mockResolvedValueOnce([
      { number: 201, title: "Full issue", node_id: "I_201" },
      { number: 202, title: "Next issue", node_id: "I_202" },
    ]);
    mockGithub.rest.issues.createComment.mockRejectedValueOnce(Object.assign(new Error("Commenting is disabled on issues with more than 2500 comments"), { status: 403 })).mockResolvedValueOnce({});

    const { main, CLOSE_ISSUE_MUTATION } = await import("./close_agentic_workflows_issues.cjs");
    await main();

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("#201"));
    expect(mockGithub.graphql).toHaveBeenCalledWith(CLOSE_ISSUE_MUTATION, { issueId: "I_201", stateReason: "NOT_PLANNED" });
    expect(mockGithub.graphql).toHaveBeenCalledWith(CLOSE_ISSUE_MUTATION, { issueId: "I_202", stateReason: "NOT_PLANNED" });
  });

  it("rethrows unrelated comment failures", async () => {
    mockGithub.paginate.mockResolvedValueOnce([{ number: 301, title: "Issue", node_id: "I_301" }]);
    mockGithub.rest.issues.createComment.mockRejectedValueOnce(Object.assign(new Error("Resource not accessible by integration"), { status: 403 }));

    const { main } = await import("./close_agentic_workflows_issues.cjs");
    await expect(main()).rejects.toThrow("Resource not accessible by integration");
    expect(mockGithub.graphql).not.toHaveBeenCalled();
  });

  it("does nothing when no target issues are found", async () => {
    mockGithub.paginate.mockResolvedValueOnce([]);

    const { main } = await import("./close_agentic_workflows_issues.cjs");
    await main();

    expect(mockGithub.rest.issues.createComment).not.toHaveBeenCalled();
    expect(mockGithub.graphql).not.toHaveBeenCalled();
  });

  it("uses the configured label for cleanup", async () => {
    vi.stubEnv("GH_AW_ISSUE_LABEL", "area:agentic-workflows");
    mockGithub.paginate.mockResolvedValueOnce([]);
    const { main } = await import("./close_agentic_workflows_issues.cjs");
    await main();
    expect(mockGithub.paginate).toHaveBeenCalledWith(mockGithub.rest.issues.listForRepo, expect.objectContaining({ labels: "area:agentic-workflows" }));
  });

  it("sanitizes and validates the close comment body", async () => {
    const module = await import("./close_agentic_workflows_issues.cjs");
    const { sanitizeContent } = await import("./sanitize_content.cjs");
    const sanitize = vi.fn().mockReturnValue(module.NO_REPRO_MESSAGE);

    expect(module.getNoReproCommentBody()).toBe(sanitizeContent(module.NO_REPRO_MESSAGE, { maxLength: module.MAX_COMMENT_BODY_LENGTH }));
    expect(module.getNoReproCommentBody(sanitize)).toBe(module.NO_REPRO_MESSAGE);
    expect(sanitize).toHaveBeenCalledWith(module.NO_REPRO_MESSAGE, { maxLength: module.MAX_COMMENT_BODY_LENGTH });
    expect(() => module.getNoReproCommentBody(() => "")).toThrow("Close comment body is empty after sanitization");
    expect(() => module.getNoReproCommentBody(() => "   ")).toThrow("Close comment body is empty after sanitization");
    expect(() => module.getNoReproCommentBody(() => " \n\t ")).toThrow("Close comment body is empty after sanitization");
    expect(() => module.getNoReproCommentBody(() => 42)).toThrow("Close comment body sanitization must return a string");
  });
});
