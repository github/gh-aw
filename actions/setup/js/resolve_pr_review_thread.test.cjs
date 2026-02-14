import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";

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

global.core = mockCore;

const mockGraphql = vi.fn();
const mockGithub = {
  graphql: mockGraphql,
};

global.github = mockGithub;

const mockContext = {
  repo: { owner: "test-owner", repo: "test-repo" },
  runId: 12345,
  eventName: "pull_request",
  payload: {
    pull_request: { number: 42 },
    repository: { html_url: "https://github.com/test-owner/test-repo" },
  },
};

global.context = mockContext;

/**
 * Helper to set up mockGraphql to handle both the lookup query and the resolve mutation.
 * @param {number} lookupPRNumber - PR number returned by the thread lookup query
 */
function mockGraphqlForThread(lookupPRNumber) {
  mockGraphql.mockImplementation(query => {
    if (query.includes("resolveReviewThread")) {
      // Mutation
      return Promise.resolve({
        resolveReviewThread: {
          thread: {
            id: "PRRT_kwDOABCD123456",
            isResolved: true,
          },
        },
      });
    }
    // Lookup query
    return Promise.resolve({
      node: {
        pullRequest: { number: lookupPRNumber },
      },
    });
  });
}

describe("resolve_pr_review_thread", () => {
  let handler;
  const originalPayload = mockContext.payload;

  beforeEach(async () => {
    vi.resetModules();
    vi.clearAllMocks();

    // Default: thread belongs to triggering PR #42
    mockGraphqlForThread(42);

    const { main } = require("./resolve_pr_review_thread.cjs");
    handler = await main({ max: 10 });
  });

  afterEach(() => {
    // Always restore the global context payload, even if assertions threw
    global.context.payload = originalPayload;
  });

  it("should return a function from main()", async () => {
    const { main } = require("./resolve_pr_review_thread.cjs");
    const result = await main({});
    expect(typeof result).toBe("function");
  });

  it("should successfully resolve a review thread on the triggering PR", async () => {
    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "PRRT_kwDOABCD123456",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.thread_id).toBe("PRRT_kwDOABCD123456");
    expect(result.is_resolved).toBe(true);
    // Should have made two GraphQL calls: lookup + resolve
    expect(mockGraphql).toHaveBeenCalledTimes(2);
    expect(mockGraphql).toHaveBeenCalledWith(expect.stringContaining("resolveReviewThread"), expect.objectContaining({ threadId: "PRRT_kwDOABCD123456" }));
  });

  it("should reject a thread that belongs to a different PR", async () => {
    // Thread belongs to PR #99, not triggering PR #42
    mockGraphqlForThread(99);

    const { main } = require("./resolve_pr_review_thread.cjs");
    const freshHandler = await main({ max: 10 });

    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "PRRT_kwDOOtherThread",
    };

    const result = await freshHandler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("PR #99");
    expect(result.error).toContain("triggering PR #42");
  });

  it("should reject when thread is not found", async () => {
    mockGraphql.mockImplementation(query => {
      if (query.includes("resolveReviewThread")) {
        return Promise.resolve({});
      }
      // Lookup returns null node
      return Promise.resolve({ node: null });
    });

    const { main } = require("./resolve_pr_review_thread.cjs");
    const freshHandler = await main({ max: 10 });

    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "PRRT_invalid",
    };

    const result = await freshHandler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("not found");
  });

  it("should reject when not in a pull request context", async () => {
    // Override context to non-PR event (afterEach restores the original payload)
    global.context.payload = {
      repository: { html_url: "https://github.com/test-owner/test-repo" },
    };

    const { main } = require("./resolve_pr_review_thread.cjs");
    const freshHandler = await main({ max: 10 });

    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "PRRT_kwDOABCD123456",
    };

    const result = await freshHandler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("pull request context");
  });

  it("should fail when thread_id is missing", async () => {
    const message = {
      type: "resolve_pull_request_review_thread",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("thread_id");
  });

  it("should fail when thread_id is empty string", async () => {
    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("thread_id");
  });

  it("should fail when thread_id is whitespace only", async () => {
    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "   ",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("thread_id");
  });

  it("should fail when thread_id is not a string", async () => {
    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: 12345,
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("thread_id");
  });

  it("should respect max count limit", async () => {
    const { main } = require("./resolve_pr_review_thread.cjs");
    const limitedHandler = await main({ max: 2 });

    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "PRRT_kwDOABCD123456",
    };

    const result1 = await limitedHandler(message, {});
    const result2 = await limitedHandler(message, {});
    const result3 = await limitedHandler(message, {});

    expect(result1.success).toBe(true);
    expect(result2.success).toBe(true);
    expect(result3.success).toBe(false);
    expect(result3.error).toContain("Max count of 2 reached");
  });

  it("should handle API errors gracefully", async () => {
    mockGraphql.mockRejectedValue(new Error("Could not resolve. Thread not found."));

    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "PRRT_invalid",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Could not resolve");
  });

  it("should handle unexpected resolve failure", async () => {
    mockGraphql.mockImplementation(query => {
      if (query.includes("resolveReviewThread")) {
        return Promise.resolve({
          resolveReviewThread: {
            thread: {
              id: "PRRT_kwDOABCD123456",
              isResolved: false,
            },
          },
        });
      }
      // Lookup succeeds - thread is on triggering PR
      return Promise.resolve({
        node: { pullRequest: { number: 42 } },
      });
    });

    const { main } = require("./resolve_pr_review_thread.cjs");
    const freshHandler = await main({ max: 10 });

    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "PRRT_kwDOABCD123456",
    };

    const result = await freshHandler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Failed to resolve");
  });

  it("should default max to 10", async () => {
    const { main } = require("./resolve_pr_review_thread.cjs");
    const defaultHandler = await main({});

    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "PRRT_kwDOABCD123456",
    };

    // Process 10 messages successfully
    for (let i = 0; i < 10; i++) {
      const result = await defaultHandler(message, {});
      expect(result.success).toBe(true);
    }

    // 11th should fail
    const result = await defaultHandler(message, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("Max count of 10 reached");
  });

  it("should work when triggered from issue_comment on a PR", async () => {
    // Simulate issue_comment event on a PR (afterEach restores the original payload)
    global.context.payload = {
      issue: { number: 42, pull_request: { url: "https://api.github.com/..." } },
      repository: { html_url: "https://github.com/test-owner/test-repo" },
    };

    const { main } = require("./resolve_pr_review_thread.cjs");
    const freshHandler = await main({ max: 10 });

    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "PRRT_kwDOABCD123456",
    };

    const result = await freshHandler(message, {});

    expect(result.success).toBe(true);
  });
});

describe("getTriggeringPRNumber", () => {
  it("should return pull_request.number for pull_request events", () => {
    const { getTriggeringPRNumber } = require("./resolve_pr_review_thread.cjs");
    const payload = { pull_request: { number: 7 } };
    expect(getTriggeringPRNumber(payload)).toBe(7);
  });

  it("should return issue.number for issue_comment events on a PR", () => {
    const { getTriggeringPRNumber } = require("./resolve_pr_review_thread.cjs");
    const payload = { issue: { number: 15, pull_request: { url: "https://api.github.com/..." } } };
    expect(getTriggeringPRNumber(payload)).toBe(15);
  });

  it("should return undefined when payload has no PR context", () => {
    const { getTriggeringPRNumber } = require("./resolve_pr_review_thread.cjs");
    const payload = { repository: { html_url: "https://github.com/owner/repo" } };
    expect(getTriggeringPRNumber(payload)).toBeUndefined();
  });

  it("should return undefined for an empty payload", () => {
    const { getTriggeringPRNumber } = require("./resolve_pr_review_thread.cjs");
    expect(getTriggeringPRNumber({})).toBeUndefined();
  });

  it("should return undefined for a nullish payload", () => {
    const { getTriggeringPRNumber } = require("./resolve_pr_review_thread.cjs");
    expect(getTriggeringPRNumber(null)).toBeUndefined();
    expect(getTriggeringPRNumber(undefined)).toBeUndefined();
  });

  it("should prefer pull_request.number over issue.number", () => {
    const { getTriggeringPRNumber } = require("./resolve_pr_review_thread.cjs");
    const payload = {
      pull_request: { number: 10 },
      issue: { number: 20, pull_request: { url: "https://api.github.com/..." } },
    };
    expect(getTriggeringPRNumber(payload)).toBe(10);
  });

  it("should not return issue.number when issue has no pull_request field", () => {
    const { getTriggeringPRNumber } = require("./resolve_pr_review_thread.cjs");
    const payload = { issue: { number: 30 } };
    expect(getTriggeringPRNumber(payload)).toBeUndefined();
  });
});
