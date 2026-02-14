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
    write: vi.fn().mockResolvedValue(undefined),
  },
};

global.core = mockCore;

const mockCreateReplyForReviewComment = vi.fn().mockResolvedValue({
  data: {
    id: 999,
    html_url: "https://github.com/test-owner/test-repo/pull/42#discussion_r999",
  },
});

const mockGithub = {
  graphql: vi.fn(),
  rest: {
    pulls: {
      createReplyForReviewComment: mockCreateReplyForReviewComment,
    },
  },
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

describe("reply_to_pr_review_comment", () => {
  let handler;
  const originalPayload = mockContext.payload;

  beforeEach(async () => {
    vi.resetModules();
    vi.clearAllMocks();

    mockCreateReplyForReviewComment.mockResolvedValue({
      data: {
        id: 999,
        html_url: "https://github.com/test-owner/test-repo/pull/42#discussion_r999",
      },
    });

    const { main } = require("./reply_to_pr_review_comment.cjs");
    handler = await main({ max: 10 });
  });

  afterEach(() => {
    // Always restore the global context payload, even if assertions threw
    global.context.payload = originalPayload;
  });

  it("should return a function from main()", async () => {
    const { main } = require("./reply_to_pr_review_comment.cjs");
    const result = await main({});
    expect(typeof result).toBe("function");
  });

  it("should successfully reply to a review comment", async () => {
    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 123,
      body: "Thanks for the feedback, I've updated the code.",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.comment_id).toBe(123);
    expect(result.reply_id).toBe(999);
    expect(result.reply_url).toContain("discussion_r999");
    // Footer is enabled by default, so body will include footer text
    const calledWith = mockCreateReplyForReviewComment.mock.calls[0][0];
    expect(calledWith.owner).toBe("test-owner");
    expect(calledWith.repo).toBe("test-repo");
    expect(calledWith.pull_number).toBe(42);
    expect(calledWith.comment_id).toBe(123);
    expect(calledWith.body).toContain("Thanks for the feedback, I've updated the code.");
  });

  it("should accept comment_id as a string", async () => {
    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: "456",
      body: "Fixed!",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.comment_id).toBe(456);
    expect(mockCreateReplyForReviewComment).toHaveBeenCalledWith(expect.objectContaining({ comment_id: 456 }));
  });

  it("should reject when not in a pull request context", async () => {
    // Override context to non-PR event (afterEach restores the original payload)
    global.context.payload = {
      repository: { html_url: "https://github.com/test-owner/test-repo" },
    };

    const { main } = require("./reply_to_pr_review_comment.cjs");
    const freshHandler = await main({ max: 10 });

    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 123,
      body: "Reply text",
    };

    const result = await freshHandler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("pull request context");
  });

  it("should work when triggered from issue_comment on a PR", async () => {
    // Simulate issue_comment event on a PR (afterEach restores the original payload)
    global.context.payload = {
      issue: { number: 42, pull_request: { url: "https://api.github.com/..." } },
      repository: { html_url: "https://github.com/test-owner/test-repo" },
    };

    const { main } = require("./reply_to_pr_review_comment.cjs");
    const freshHandler = await main({ max: 10 });

    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 123,
      body: "Reply text",
    };

    const result = await freshHandler(message, {});

    expect(result.success).toBe(true);
  });

  it("should fail when comment_id is missing", async () => {
    const message = {
      type: "reply_to_pull_request_review_comment",
      body: "Reply text",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("comment_id");
  });

  it("should fail when comment_id is zero", async () => {
    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 0,
      body: "Reply text",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("comment_id");
  });

  it("should fail when comment_id is negative", async () => {
    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: -5,
      body: "Reply text",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("comment_id");
  });

  it("should fail when comment_id is a non-numeric string", async () => {
    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: "abc",
      body: "Reply text",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("comment_id");
  });

  it("should fail when body is missing", async () => {
    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 123,
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("body");
  });

  it("should fail when body is empty string", async () => {
    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 123,
      body: "",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("body");
  });

  it("should fail when body is whitespace only", async () => {
    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 123,
      body: "   ",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("body");
  });

  it("should respect max count limit", async () => {
    const { main } = require("./reply_to_pr_review_comment.cjs");
    const limitedHandler = await main({ max: 2 });

    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 123,
      body: "Reply",
    };

    const result1 = await limitedHandler(message, {});
    const result2 = await limitedHandler(message, {});
    const result3 = await limitedHandler(message, {});

    expect(result1.success).toBe(true);
    expect(result2.success).toBe(true);
    expect(result3.success).toBe(false);
    expect(result3.error).toContain("Max count of 2 reached");
  });

  it("should default max to 10", async () => {
    const { main } = require("./reply_to_pr_review_comment.cjs");
    const defaultHandler = await main({});

    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 123,
      body: "Reply",
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

  it("should append footer to reply body by default", async () => {
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";

    const { main } = require("./reply_to_pr_review_comment.cjs");
    const footerHandler = await main({ max: 10 });

    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 123,
      body: "Great point, fixed.",
    };

    const result = await footerHandler(message, {});

    expect(result.success).toBe(true);
    const calledBody = mockCreateReplyForReviewComment.mock.calls[0][0].body;
    expect(calledBody).toContain("Great point, fixed.");
    // Footer should include workflow metadata (XML marker)
    expect(calledBody).toContain("<!-- ");

    delete process.env.GH_AW_WORKFLOW_NAME;
  });

  it("should omit footer when footer config is false", async () => {
    const { main } = require("./reply_to_pr_review_comment.cjs");
    const noFooterHandler = await main({ max: 10, footer: false });

    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 123,
      body: "Acknowledged, thanks!",
    };

    const result = await noFooterHandler(message, {});

    expect(result.success).toBe(true);
    expect(mockCreateReplyForReviewComment).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      pull_number: 42,
      comment_id: 123,
      body: "Acknowledged, thanks!",
    });
  });

  it("should handle API errors gracefully", async () => {
    mockCreateReplyForReviewComment.mockRejectedValue(new Error("Not Found"));

    const message = {
      type: "reply_to_pull_request_review_comment",
      comment_id: 123,
      body: "Reply text",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Not Found");
  });
});

describe("getTriggeringPRNumber", () => {
  it("should return pull_request.number for pull_request events", () => {
    const { getTriggeringPRNumber } = require("./reply_to_pr_review_comment.cjs");
    const payload = { pull_request: { number: 7 } };
    expect(getTriggeringPRNumber(payload)).toBe(7);
  });

  it("should return issue.number for issue_comment events on a PR", () => {
    const { getTriggeringPRNumber } = require("./reply_to_pr_review_comment.cjs");
    const payload = { issue: { number: 15, pull_request: { url: "https://api.github.com/..." } } };
    expect(getTriggeringPRNumber(payload)).toBe(15);
  });

  it("should return undefined when payload has no PR context", () => {
    const { getTriggeringPRNumber } = require("./reply_to_pr_review_comment.cjs");
    const payload = { repository: { html_url: "https://github.com/owner/repo" } };
    expect(getTriggeringPRNumber(payload)).toBeUndefined();
  });

  it("should return undefined for a nullish payload", () => {
    const { getTriggeringPRNumber } = require("./reply_to_pr_review_comment.cjs");
    expect(getTriggeringPRNumber(null)).toBeUndefined();
    expect(getTriggeringPRNumber(undefined)).toBeUndefined();
  });
});
