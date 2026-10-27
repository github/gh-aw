import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  notice: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  exportVariable: vi.fn(),
  setSecret: vi.fn(),
  getInput: vi.fn(),
  getBooleanInput: vi.fn(),
  getMultilineInput: vi.fn(),
  getState: vi.fn(),
  saveState: vi.fn(),
  startGroup: vi.fn(),
  endGroup: vi.fn(),
  group: vi.fn(),
  addPath: vi.fn(),
  setCommandEcho: vi.fn(),
  isDebug: vi.fn().mockReturnValue(false),
  getIDToken: vi.fn(),
  toPlatformPath: vi.fn(),
  toPosixPath: vi.fn(),
  toWin32Path: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

const mockGithub = {
  rest: {
    pulls: {
      createReviewComment: vi.fn(),
      get: vi.fn(),
    },
  },
};

const mockContext = {
  eventName: "pull_request",
  runId: 12345,
  repo: { owner: "testowner", repo: "testrepo" },
  payload: {
    pull_request: { number: 123, head: { sha: "abc123def456" } },
    repository: {
      html_url: "https://github.com/testowner/testrepo",
    },
  },
};

global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

const { createReviewBuffer, createPrReviewBufferRegistry } = require("./pr_review_buffer.cjs");

describe("create_pr_review_comment.cjs", () => {
  let createPRReviewCommentScript;
  let buffer;

  beforeEach(() => {
    vi.clearAllMocks();

    // Create a fresh buffer for each test (factory pattern, no global state)
    buffer = createReviewBuffer();

    const scriptPath = path.join(__dirname, "create_pr_review_comment.cjs");
    createPRReviewCommentScript = fs.readFileSync(scriptPath, "utf8");

    delete process.env.GH_AW_AGENT_OUTPUT;
    delete process.env.GH_AW_PR_REVIEW_COMMENT_SIDE;
    delete process.env.GH_AW_PR_REVIEW_COMMENT_TARGET;
    delete process.env.GH_AW_WORKFLOW_NAME;
    delete process.env.GH_AW_WORKFLOW_SOURCE;
    delete process.env.GH_AW_WORKFLOW_SOURCE_URL;
    delete process.env.GH_AW_PROMPTS_DIR;

    global.context = {
      eventName: "pull_request",
      runId: 12345,
      repo: { owner: "testowner", repo: "testrepo" },
      payload: {
        pull_request: { number: 123, head: { sha: "abc123def456" } },
        repository: {
          html_url: "https://github.com/testowner/testrepo",
        },
      },
    };
  });

  /** Helper to create a handler with the buffer injected via config */
  async function createHandler(extraConfig = {}) {
    // buffer is a local var accessible in eval scope; JSON.stringify can't serialize it (has functions)
    const configStr = JSON.stringify(extraConfig);
    return await eval(`(async () => { ${createPRReviewCommentScript}; return await main(Object.assign({ _prReviewBuffer: buffer }, ${configStr})); })()`);
  }

  it("should return error when no buffer provided", async () => {
    const handler = await eval(`(async () => { ${createPRReviewCommentScript}; return await main({}); })()`);
    const result = await handler({}, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("No PR review buffer available");
  });

  it("should buffer a single PR review comment with basic configuration", async () => {
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "Consider using const instead of let here.",
    };
    const handler = await createHandler();
    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.buffered).toBe(true);
    expect(result.pull_request_number).toBe(123);
    expect(buffer.getBufferedCount()).toBe(1);
    // Individual comments should NOT be posted directly
    expect(mockGithub.rest.pulls.createReviewComment).not.toHaveBeenCalled();
  });

  it("fits oversized inline comments before buffering and retains the full footer", async () => {
    const bodyFooter = "POLICY-START " + "f".repeat(1000) + " POLICY-END";
    const addComment = vi.spyOn(buffer, "addComment");
    const handler = await createHandler({ body_footer: bodyFooter });
    const result = await handler({ type: "create_pull_request_review_comment", path: "src/main.js", line: 10, body: "x".repeat(100000) }, {});
    expect(result.success).toBe(true);
    const comment = addComment.mock.calls[0][0];
    expect(comment.body.length).toBeLessThanOrEqual(65536);
    expect(comment.body).toContain("Content redacted to fit size limit.");
    expect(comment.body).toContain(bodyFooter);
    expect(comment.bodyFooterBlocks).toEqual([{ type: "paragraph", agentTainted: false, children: [{ type: "text", value: bodyFooter, agentTainted: false }] }]);
  });

  it("should buffer a multi-line PR review comment", async () => {
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/utils.js",
      line: 25,
      start_line: 20,
      side: "LEFT",
      body: "This entire function could be simplified using modern JS features.",
    };
    const handler = await createHandler();
    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.buffered).toBe(true);
    expect(buffer.getBufferedCount()).toBe(1);
    expect(mockGithub.rest.pulls.createReviewComment).not.toHaveBeenCalled();
  });

  it("should buffer multiple review comments", async () => {
    const handler = await createHandler();
    const message1 = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "First comment",
    };
    const message2 = {
      type: "create_pull_request_review_comment",
      path: "src/utils.js",
      line: 25,
      body: "Second comment",
    };
    const result1 = await handler(message1, {});
    const result2 = await handler(message2, {});

    expect(result1.success).toBe(true);
    expect(result2.success).toBe(true);
    expect(buffer.getBufferedCount()).toBe(2);
    expect(mockGithub.rest.pulls.createReviewComment).not.toHaveBeenCalled();
  });

  it("should enforce max count limit", async () => {
    const handler = await createHandler({ max: 2 });
    const message1 = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "First comment",
    };
    const message2 = {
      type: "create_pull_request_review_comment",
      path: "src/utils.js",
      line: 20,
      body: "Second comment",
    };
    const message3 = {
      type: "create_pull_request_review_comment",
      path: "src/test.js",
      line: 30,
      body: "Third comment",
    };

    const result1 = await handler(message1, {});
    const result2 = await handler(message2, {});
    const result3 = await handler(message3, {});

    expect(result1.success).toBe(true);
    expect(result2.success).toBe(true);
    expect(result3.success).toBe(false);
    expect(result3.error).toContain("Max count of 2 reached");
    expect(buffer.getBufferedCount()).toBe(2);
  });

  it("should use configured side from config", async () => {
    const handler = await createHandler({ side: "LEFT" });
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "Comment on left side",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.buffered).toBe(true);
    expect(buffer.getBufferedCount()).toBe(1);
  });

  it("should skip when not in pull request context", async () => {
    global.context = {
      ...mockContext,
      eventName: "issues",
      payload: {
        issue: { number: 123 },
        repository: mockContext.payload.repository,
      },
    };
    const handler = await createHandler();
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "This should not be created",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Not in pull request context");
    expect(result.skipped).toBe(true);
    expect(buffer.getBufferedCount()).toBe(0);
  });

  it("should validate required fields and skip invalid items", async () => {
    const handler = await createHandler();

    // Missing path
    const result1 = await handler({ type: "create_pull_request_review_comment", line: 10, body: "Missing path" }, {});
    expect(result1.success).toBe(false);
    expect(result1.error).toContain('Missing required field "path"');

    // Missing line
    const result2 = await handler(
      {
        type: "create_pull_request_review_comment",
        path: "src/main.js",
        body: "Missing line",
      },
      {}
    );
    expect(result2.success).toBe(false);
    expect(result2.error).toContain('Missing or invalid required field "line"');

    // Missing body
    const result3 = await handler(
      {
        type: "create_pull_request_review_comment",
        path: "src/main.js",
        line: 10,
      },
      {}
    );
    expect(result3.success).toBe(false);
    expect(result3.error).toContain('Missing or invalid required field "body"');

    expect(buffer.getBufferedCount()).toBe(0);
  });

  it("should validate start_line is not greater than line", async () => {
    const handler = await createHandler();
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      start_line: 15,
      body: "Invalid range",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid start_line");
    expect(buffer.getBufferedCount()).toBe(0);
  });

  it("should validate side values", async () => {
    const handler = await createHandler();
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      side: "INVALID_SIDE",
      body: "Invalid side value",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid side value");
    expect(buffer.getBufferedCount()).toBe(0);
  });

  it("should buffer comment body without footer (footer is added at review level)", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    const handler = await createHandler();
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "Original comment",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.buffered).toBe(true);
    expect(buffer.getBufferedCount()).toBe(1);
    // Footer is added at review submission time, not per-comment
    expect(mockGithub.rest.pulls.createReviewComment).not.toHaveBeenCalled();
  });

  it("should preserve allowlisted mentions and neutralize others before buffering", async () => {
    const addCommentSpy = vi.spyOn(buffer, "addComment");
    const handler = await createHandler({
      mentions: { allowTeamMembers: false, allowContext: false, allowed: ["copilot"] },
    });
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "Please ask @copilot and @someone-else to review this.",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(addCommentSpy).toHaveBeenCalledTimes(1);
    const bufferedComment = addCommentSpy.mock.calls[0][0];
    expect(bufferedComment.body).toContain("@copilot");
    expect(bufferedComment.body).not.toContain("`@copilot`");
    expect(bufferedComment.body).toContain("`@someone-else`");
  });

  it("should respect target configuration for specific PR number", async () => {
    mockGithub.rest.pulls.get.mockResolvedValue({
      data: { number: 456, head: { sha: "def456abc789" } },
    });
    const handler = await createHandler({ target: "456" });
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "Review comment on specific PR",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.buffered).toBe(true);
    expect(result.pull_request_number).toBe(456);
    expect(mockGithub.rest.pulls.get).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 456,
    });
    expect(buffer.getBufferedCount()).toBe(1);
  });

  it('should respect target "*" configuration with pull_request_number in item', async () => {
    mockGithub.rest.pulls.get.mockResolvedValue({
      data: { number: 789, head: { sha: "xyz789abc456" } },
    });
    const handler = await createHandler({ target: "*" });
    const message = {
      type: "create_pull_request_review_comment",
      pull_request_number: 789,
      path: "src/utils.js",
      line: 20,
      body: "Review comment on any PR",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.buffered).toBe(true);
    expect(result.pull_request_number).toBe(789);
    expect(mockGithub.rest.pulls.get).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 789,
    });
    expect(buffer.getBufferedCount()).toBe(1);
  });

  it('should skip item when target is "*" but no pull_request_number specified', async () => {
    const handler = await createHandler({ target: "*" });
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "Review comment without PR number",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain('Target is "*" but no pull_request_number specified');
    expect(buffer.getBufferedCount()).toBe(0);
  });

  it("should skip comment creation when target is triggering but not in PR context", async () => {
    global.context = {
      eventName: "issues",
      runId: 12345,
      repo: { owner: "testowner", repo: "testrepo" },
      payload: {
        issue: { number: 10 },
        repository: {
          html_url: "https://github.com/testowner/testrepo",
        },
      },
    };
    const handler = await createHandler({ target: "triggering" });
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "This should not be created",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Not in pull request context");
    expect(result.skipped).toBe(true);
    expect(buffer.getBufferedCount()).toBe(0);
  });

  it("should succeed when target is triggering and event is pull_request_target", async () => {
    global.context = {
      eventName: "pull_request_target",
      runId: 12345,
      repo: { owner: "testowner", repo: "testrepo" },
      payload: {
        pull_request: { number: 99, head: { sha: "prt123abc456" } },
        repository: {
          html_url: "https://github.com/testowner/testrepo",
        },
      },
    };
    const handler = await createHandler({ target: "triggering" });
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 5,
      body: "Review comment from pull_request_target trigger",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.buffered).toBe(true);
    expect(result.pull_request_number).toBe(99);
    expect(buffer.getBufferedCount()).toBe(1);
  });

  it("should use the original PR context for centralized slash-command dispatches", async () => {
    global.context = {
      eventName: "workflow_dispatch",
      runId: 12345,
      repo: { owner: "testowner", repo: "testrepo" },
      payload: {
        inputs: {
          event_name: "issue_comment",
          event_payload: JSON.stringify({
            issue: { number: 456, pull_request: {} },
            repository: mockContext.payload.repository,
          }),
        },
      },
    };
    mockGithub.rest.pulls.get.mockResolvedValue({
      data: { number: 456, head: { sha: "dispatch123abc" } },
    });
    const handler = await createHandler({ target: "triggering" });
    const message = {
      type: "create_pull_request_review_comment",
      pull_request_number: 456,
      path: "src/main.js",
      line: 5,
      body: "Review comment from centralized slash command",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.buffered).toBe(true);
    expect(result.pull_request_number).toBe(456);
    expect(mockGithub.rest.pulls.get).toHaveBeenCalledWith({
      owner: "testowner",
      repo: "testrepo",
      pull_number: 456,
    });
    expect(buffer.getBufferedCount()).toBe(1);
  });

  it("falls back to raw context when resolveInvocationContext returns empty object", async () => {
    const invocationHelpersPath = path.join(__dirname, "invocation_context_helpers.cjs");
    const invocationHelpers = require(invocationHelpersPath);
    const resolveInvocationContextSpy = vi.spyOn(invocationHelpers, "resolveInvocationContext").mockReturnValue({});
    try {
      const handler = await createHandler({ target: "triggering" });
      const message = {
        type: "create_pull_request_review_comment",
        path: "src/main.js",
        line: 5,
        body: "Review comment from fallback context",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.buffered).toBe(true);
      expect(result.pull_request_number).toBe(123);
      expect(buffer.getBufferedCount()).toBe(1);
      expect(resolveInvocationContextSpy).toHaveBeenCalled();
    } finally {
      resolveInvocationContextSpy.mockRestore();
    }
  });

  it("falls back to raw context when resolveInvocationContext throws a non-validation error", async () => {
    const invocationHelpersPath = path.join(__dirname, "invocation_context_helpers.cjs");
    const invocationHelpers = require(invocationHelpersPath);
    const resolveInvocationContextSpy = vi.spyOn(invocationHelpers, "resolveInvocationContext").mockImplementation(() => {
      throw new Error("boom");
    });
    try {
      const handler = await createHandler({ target: "triggering" });
      const message = {
        type: "create_pull_request_review_comment",
        path: "src/main.js",
        line: 5,
        body: "Review comment from thrown context resolver",
      };

      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.buffered).toBe(true);
      expect(result.pull_request_number).toBe(123);
      expect(buffer.getBufferedCount()).toBe(1);
      expect(resolveInvocationContextSpy).toHaveBeenCalled();
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("failed to resolve invocation context"));
    } finally {
      resolveInvocationContextSpy.mockRestore();
    }
  });

  it("should reject comments targeting a different PR than the first comment", async () => {
    // First comment sets context to PR #123
    const handler = await createHandler();
    const message1 = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "First comment on PR 123",
    };
    const result1 = await handler(message1, {});
    expect(result1.success).toBe(true);
    expect(buffer.getBufferedCount()).toBe(1);

    // Simulate a second comment targeting a different PR by changing context
    global.context = {
      eventName: "pull_request",
      runId: 12345,
      repo: { owner: "testowner", repo: "testrepo" },
      payload: {
        pull_request: { number: 456, head: { sha: "def456" } },
        repository: {
          html_url: "https://github.com/testowner/testrepo",
        },
      },
    };

    // Need a new handler that sees the different context but shares the same buffer
    const handler2 = await createHandler();
    const message2 = {
      type: "create_pull_request_review_comment",
      path: "src/other.js",
      line: 20,
      body: "Comment on different PR",
    };
    const result2 = await handler2(message2, {});

    expect(result2.success).toBe(false);
    expect(result2.error).toContain("must target the same PR");
    expect(buffer.getBufferedCount()).toBe(1); // Still only 1 comment
  });

  it("should set footer context for review-level footer generation", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_WORKFLOW_SOURCE = "githubnext/agentics/workflows/ci-doctor.md@v1.0.0";
    process.env.GH_AW_WORKFLOW_SOURCE_URL = "https://github.com/githubnext/agentics/tree/v1.0.0/workflows/ci-doctor.md";

    const handler = await createHandler();
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "Test review comment",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.buffered).toBe(true);
    // Footer context is set on the buffer for review-level footer generation
    expect(buffer.getBufferedCount()).toBe(1);
  });

  it("should use GH_AW_HEAD_SHA env var as commit_id when submitting review", async () => {
    const previousHeadSHA = process.env.GH_AW_HEAD_SHA;
    process.env.GH_AW_HEAD_SHA = "trigger-time-sha-xyz789";
    try {
      // The buffer's submitReview uses GH_AW_HEAD_SHA automatically — verify the
      // review context is set correctly (no commitId field needed on context)
      const handler = await createHandler();
      const message = {
        type: "create_pull_request_review_comment",
        path: "src/main.js",
        line: 10,
        body: "Review comment body",
      };
      const result = await handler(message, {});

      expect(result.success).toBe(true);
      expect(result.buffered).toBe(true);

      const ctx = buffer.getReviewContext();
      expect(ctx).not.toBeNull();
      // No commitId on the context; the env var is used at submit time instead
      expect(ctx.commitId).toBeUndefined();
      // The live PR head SHA should still be in pullRequest.head.sha
      expect(ctx.pullRequest.head.sha).toBe("abc123def456");
    } finally {
      if (previousHeadSHA !== undefined) {
        process.env.GH_AW_HEAD_SHA = previousHeadSHA;
      } else {
        delete process.env.GH_AW_HEAD_SHA;
      }
    }
  });

  it("review context has no commitId field regardless of handler config", async () => {
    const handler = await createHandler();
    const message = {
      type: "create_pull_request_review_comment",
      path: "src/main.js",
      line: 10,
      body: "Review comment without pinning",
    };
    const result = await handler(message, {});

    expect(result.success).toBe(true);
    const ctx = buffer.getReviewContext();
    expect(ctx).not.toBeNull();
    expect(ctx.commitId).toBeUndefined();
  });
});

describe("create_pr_review_comment.cjs — registry mode (multiple reviews)", () => {
  let createPRReviewCommentScript;

  beforeEach(() => {
    vi.clearAllMocks();

    const scriptPath = require("path").join(__dirname, "create_pr_review_comment.cjs");
    createPRReviewCommentScript = require("fs").readFileSync(scriptPath, "utf8");

    delete process.env.GH_AW_AGENT_OUTPUT;
    delete process.env.GH_AW_PR_REVIEW_COMMENT_SIDE;
    delete process.env.GH_AW_PR_REVIEW_COMMENT_TARGET;
    delete process.env.GH_AW_WORKFLOW_NAME;
    delete process.env.GH_AW_WORKFLOW_SOURCE;
    delete process.env.GH_AW_WORKFLOW_SOURCE_URL;

    mockGithub.rest.pulls.get.mockImplementation(({ pull_number }) => Promise.resolve({ data: { number: pull_number, head: { sha: `sha-pr${pull_number}` } } }));

    global.context = {
      eventName: "pull_request",
      runId: 12345,
      repo: { owner: "testowner", repo: "testrepo" },
      payload: {
        pull_request: { number: 123, head: { sha: "abc123" } },
        repository: { html_url: "https://github.com/testowner/testrepo" },
      },
    };
  });

  afterEach(() => {
    global.context = mockContext;
  });

  async function createRegistryHandler(registry, extraConfig = {}) {
    const configStr = JSON.stringify(extraConfig);
    return await eval(`(async () => { ${createPRReviewCommentScript}; return await main(Object.assign({ _prReviewBufferRegistry: registry, target: "*" }, ${configStr})); })()`);
  }

  it("routes comments for two different PRs into separate buffers", async () => {
    const registry = createPrReviewBufferRegistry();
    const handler = await createRegistryHandler(registry);

    const result1 = await handler({ type: "create_pull_request_review_comment", path: "a.js", line: 1, body: "comment on PR 1", pull_request_number: 1 }, {});
    const result2 = await handler({ type: "create_pull_request_review_comment", path: "b.js", line: 2, body: "comment on PR 2", pull_request_number: 2 }, {});

    expect(result1.success).toBe(true);
    expect(result1.buffered).toBe(true);
    expect(result2.success).toBe(true);
    expect(result2.buffered).toBe(true);

    const entries = registry.getAllEntries();
    expect(entries).toHaveLength(2);
    expect(entries[0].prNumber).toBe(1);
    expect(entries[0].buffer.getBufferedCount()).toBe(1);
    expect(entries[1].prNumber).toBe(2);
    expect(entries[1].buffer.getBufferedCount()).toBe(1);
  });

  it("accumulates multiple comments for the same PR in one buffer", async () => {
    const registry = createPrReviewBufferRegistry();
    const handler = await createRegistryHandler(registry);

    await handler({ type: "create_pull_request_review_comment", path: "a.js", line: 1, body: "first", pull_request_number: 5 }, {});
    await handler({ type: "create_pull_request_review_comment", path: "b.js", line: 2, body: "second", pull_request_number: 5 }, {});

    const entries = registry.getAllEntries();
    expect(entries).toHaveLength(1);
    expect(entries[0].prNumber).toBe(5);
    expect(entries[0].buffer.getBufferedCount()).toBe(2);
  });

  it("sets footer context on the per-PR buffer", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "My Workflow";
    const registry = createPrReviewBufferRegistry();
    const handler = await createRegistryHandler(registry);

    await handler({ type: "create_pull_request_review_comment", path: "c.js", line: 3, body: "body", pull_request_number: 10 }, {});

    const entry = registry.getAllEntries()[0];
    const ctx = entry.buffer.getFooterContext?.();
    if (ctx !== undefined) {
      expect(ctx.workflowName).toBe("My Workflow");
    }
  });

  it("returns success:false when pull_request_number is missing in target:* mode", async () => {
    const registry = createPrReviewBufferRegistry();
    const handler = await createRegistryHandler(registry);

    const result = await handler({ type: "create_pull_request_review_comment", path: "x.js", line: 1, body: "no PR" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("pull_request_number");
    expect(registry.getAllEntries()).toHaveLength(0);
  });
});
