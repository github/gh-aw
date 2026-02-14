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

const { createReviewBuffer } = require("./pr_review_buffer.cjs");

describe("submit_pr_review (Handler Factory Architecture)", () => {
  let handler;
  let buffer;

  beforeEach(async () => {
    vi.clearAllMocks();

    // Create a fresh buffer for each test (factory pattern, no global state)
    buffer = createReviewBuffer();

    const { main } = require("./submit_pr_review.cjs");
    handler = await main({ max: 1, _prReviewBuffer: buffer });
  });

  it("should return a function from main()", async () => {
    const { main } = require("./submit_pr_review.cjs");
    const localBuffer = createReviewBuffer();
    const result = await main({ _prReviewBuffer: localBuffer });
    expect(typeof result).toBe("function");
  });

  it("should return error when no buffer provided", async () => {
    const { main } = require("./submit_pr_review.cjs");
    const noBufferHandler = await main({});
    const result = await noBufferHandler({}, {});
    expect(result.success).toBe(false);
    expect(result.error).toContain("No PR review buffer available");
  });

  it("should set review metadata for APPROVE event", async () => {
    const message = {
      type: "submit_pull_request_review",
      body: "LGTM! Great changes.",
      event: "APPROVE",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.event).toBe("APPROVE");
    expect(result.body_length).toBe(20);
    expect(buffer.hasReviewMetadata()).toBe(true);
  });

  it("should set review metadata for REQUEST_CHANGES event", async () => {
    const message = {
      type: "submit_pull_request_review",
      body: "Please fix the issues.",
      event: "REQUEST_CHANGES",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.event).toBe("REQUEST_CHANGES");
  });

  it("should set review metadata for COMMENT event", async () => {
    const message = {
      type: "submit_pull_request_review",
      body: "Some general feedback.",
      event: "COMMENT",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.event).toBe("COMMENT");
  });

  it("should normalize event to uppercase", async () => {
    const message = {
      type: "submit_pull_request_review",
      body: "Looks good",
      event: "approve",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.event).toBe("APPROVE");
  });

  it("should default event to COMMENT when missing", async () => {
    const message = {
      type: "submit_pull_request_review",
      body: "Some feedback",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.event).toBe("COMMENT");
  });

  it("should reject invalid event values", async () => {
    const message = {
      type: "submit_pull_request_review",
      body: "Bad event",
      event: "INVALID",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid review event");
  });

  it("should allow empty body for APPROVE event", async () => {
    const message = {
      type: "submit_pull_request_review",
      body: "",
      event: "APPROVE",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.event).toBe("APPROVE");
    expect(result.body_length).toBe(0);
  });

  it("should require body for REQUEST_CHANGES event", async () => {
    const message = {
      type: "submit_pull_request_review",
      body: "",
      event: "REQUEST_CHANGES",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Review body is required");
  });

  it("should allow empty body for COMMENT event", async () => {
    const message = {
      type: "submit_pull_request_review",
      event: "COMMENT",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.event).toBe("COMMENT");
  });

  it("should allow no event and no body (defaults to COMMENT)", async () => {
    const message = {
      type: "submit_pull_request_review",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.event).toBe("COMMENT");
    expect(result.body_length).toBe(0);
  });

  it("should respect max count configuration", async () => {
    const message1 = {
      type: "submit_pull_request_review",
      body: "First review",
      event: "COMMENT",
    };

    const message2 = {
      type: "submit_pull_request_review",
      body: "Second review",
      event: "COMMENT",
    };

    // First call should succeed
    const result1 = await handler(message1, {});
    expect(result1.success).toBe(true);

    // Second call should fail (max=1)
    const result2 = await handler(message2, {});
    expect(result2.success).toBe(false);
    expect(result2.error).toContain("Max count");
  });

  it("should not consume max count slot on validation failure", async () => {
    // Invalid event should not consume a slot
    const invalidMessage = {
      type: "submit_pull_request_review",
      body: "Bad event",
      event: "INVALID",
    };

    const result1 = await handler(invalidMessage, {});
    expect(result1.success).toBe(false);

    // Valid message should still succeed since the invalid one didn't count
    const validMessage = {
      type: "submit_pull_request_review",
      body: "Good review",
      event: "APPROVE",
    };

    const result2 = await handler(validMessage, {});
    expect(result2.success).toBe(true);
    expect(result2.event).toBe("APPROVE");
  });

  it("should not consume max count slot when body is missing for REQUEST_CHANGES", async () => {
    // Missing body for REQUEST_CHANGES should not consume a slot
    const noBodyMessage = {
      type: "submit_pull_request_review",
      body: "",
      event: "REQUEST_CHANGES",
    };

    const result1 = await handler(noBodyMessage, {});
    expect(result1.success).toBe(false);

    // Valid message should still succeed
    const validMessage = {
      type: "submit_pull_request_review",
      body: "Now with body",
      event: "REQUEST_CHANGES",
    };

    const result2 = await handler(validMessage, {});
    expect(result2.success).toBe(true);
  });

  it("should set review context from triggering PR for body-only reviews", async () => {
    // Simulate a PR trigger context
    global.context = {
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {
        pull_request: { number: 42, head: { sha: "abc123" } },
      },
    };

    const localBuffer = createReviewBuffer();
    const { main } = require("./submit_pr_review.cjs");
    const localHandler = await main({ max: 1, _prReviewBuffer: localBuffer });

    const message = {
      type: "submit_pull_request_review",
      body: "LGTM!",
      event: "APPROVE",
    };

    const result = await localHandler(message, {});

    expect(result.success).toBe(true);
    expect(localBuffer.hasReviewMetadata()).toBe(true);

    // The handler should have set review context from triggering PR
    const ctx = localBuffer.getReviewContext();
    expect(ctx).not.toBeNull();
    expect(ctx.repo).toBe("test-owner/test-repo");
    expect(ctx.pullRequestNumber).toBe(42);

    // Clean up
    delete global.context;
  });

  it("should not override existing review context from comments", async () => {
    // Pre-set context as if a comment handler already set it
    buffer.setReviewContext({
      repo: "comment-owner/comment-repo",
      repoParts: { owner: "comment-owner", repo: "comment-repo" },
      pullRequestNumber: 99,
      pullRequest: { head: { sha: "comment-sha" } },
    });

    global.context = {
      repo: { owner: "trigger-owner", repo: "trigger-repo" },
      payload: {
        pull_request: { number: 1, head: { sha: "trigger-sha" } },
      },
    };

    const message = {
      type: "submit_pull_request_review",
      body: "Review body",
      event: "COMMENT",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);

    // Context should still be from the comment handler, not overridden
    const ctx = buffer.getReviewContext();
    expect(ctx.repo).toBe("comment-owner/comment-repo");
    expect(ctx.pullRequestNumber).toBe(99);

    // Clean up
    delete global.context;
  });
});
