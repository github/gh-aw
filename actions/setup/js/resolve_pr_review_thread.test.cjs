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

describe("resolve_pr_review_thread", () => {
  let handler;

  beforeEach(async () => {
    vi.clearAllMocks();

    mockGraphql.mockResolvedValue({
      resolveReviewThread: {
        thread: {
          id: "PRRT_kwDOABCD123456",
          isResolved: true,
        },
      },
    });

    const { main } = require("./resolve_pr_review_thread.cjs");
    handler = await main({ max: 10 });
  });

  it("should return a function from main()", async () => {
    const { main } = require("./resolve_pr_review_thread.cjs");
    const result = await main({});
    expect(typeof result).toBe("function");
  });

  it("should successfully resolve a review thread", async () => {
    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "PRRT_kwDOABCD123456",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.thread_id).toBe("PRRT_kwDOABCD123456");
    expect(result.is_resolved).toBe(true);
    expect(mockGraphql).toHaveBeenCalledWith(
      expect.stringContaining("resolveReviewThread"),
      expect.objectContaining({
        threadId: "PRRT_kwDOABCD123456",
      })
    );
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
    mockGraphql.mockResolvedValue({
      resolveReviewThread: {
        thread: {
          id: "PRRT_kwDOABCD123456",
          isResolved: false,
        },
      },
    });

    const message = {
      type: "resolve_pull_request_review_thread",
      thread_id: "PRRT_kwDOABCD123456",
    };

    const result = await handler(message, {});

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
});
