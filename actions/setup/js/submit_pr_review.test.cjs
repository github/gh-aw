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

const mockContext = {
  eventName: "pull_request",
  repo: {
    owner: "test-owner",
    repo: "test-repo",
  },
  payload: {
    pull_request: {
      number: 123,
      head: { sha: "test-sha" },
    },
  },
};

global.core = mockCore;
global.context = mockContext;
global.github = {
  rest: {
    pulls: {
      get: vi.fn().mockResolvedValue({ data: {} }),
    },
  },
  graphql: vi.fn().mockResolvedValue({}),
};

const { createReviewBuffer, createPrReviewBufferRegistry } = require("./pr_review_buffer.cjs");

describe("submit_pr_review (Handler Factory Architecture)", () => {
  let handler;
  let buffer;

  beforeEach(async () => {
    vi.clearAllMocks();

    // Reset context to default for each test
    global.context = {
      eventName: "pull_request",
      repo: {
        owner: "test-owner",
        repo: "test-repo",
      },
      payload: {
        pull_request: {
          number: 123,
          head: { sha: "test-sha" },
        },
      },
    };

    // Reset github to default mock for each test (some tests delete global.github)
    global.github = {
      rest: {
        pulls: {
          get: vi.fn().mockResolvedValue({ data: {} }),
        },
      },
      graphql: vi.fn().mockResolvedValue({}),
    };

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

  it("should enable supersede mode on review buffer when configured", async () => {
    const { main } = require("./submit_pr_review.cjs");
    const localBuffer = createReviewBuffer();
    const supersedeSpy = vi.spyOn(localBuffer, "setSupersedeOlderReviews");

    await main({
      max: 1,
      _prReviewBuffer: localBuffer,
      supersede_older_reviews: true,
    });

    expect(supersedeSpy).toHaveBeenCalledWith(true);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("supersede-older-reviews is best-effort"));
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
    expect(result.deferred_manifest).toBe(true);
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

  it("should allow all events when allowed_events not configured", async () => {
    const { main } = require("./submit_pr_review.cjs");

    for (const event of ["APPROVE", "COMMENT", "REQUEST_CHANGES"]) {
      const localBuffer = createReviewBuffer();
      const h = await main({ max: 5, _prReviewBuffer: localBuffer });
      const result = await h({ event, body: event === "REQUEST_CHANGES" ? "body" : "" }, {});
      expect(result.success).toBe(true);
      expect(result.event).toBe(event);
    }
  });

  it("should block APPROVE when allowed_events is [COMMENT, REQUEST_CHANGES]", async () => {
    const { main } = require("./submit_pr_review.cjs");
    const localBuffer = createReviewBuffer();
    const localHandler = await main({
      max: 5,
      _prReviewBuffer: localBuffer,
      allowed_events: ["COMMENT", "REQUEST_CHANGES"],
    });

    const result = await localHandler({ event: "APPROVE", body: "" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("not allowed");
    expect(result.error).toContain("APPROVE");
  });

  it("should allow COMMENT when allowed_events is [COMMENT, REQUEST_CHANGES]", async () => {
    const { main } = require("./submit_pr_review.cjs");
    const localBuffer = createReviewBuffer();
    const localHandler = await main({
      max: 5,
      _prReviewBuffer: localBuffer,
      allowed_events: ["COMMENT", "REQUEST_CHANGES"],
    });

    const result = await localHandler({ event: "COMMENT", body: "some feedback" }, {});

    expect(result.success).toBe(true);
    expect(result.event).toBe("COMMENT");
  });

  it("should normalize event case before checking allowed_events", async () => {
    const { main } = require("./submit_pr_review.cjs");
    const localBuffer = createReviewBuffer();
    const localHandler = await main({
      max: 5,
      _prReviewBuffer: localBuffer,
      allowed_events: ["COMMENT"],
    });

    const result = await localHandler({ event: "comment", body: "feedback" }, {});

    expect(result.success).toBe(true);
    expect(result.event).toBe("COMMENT");
  });

  it("should not consume max count slot when event is blocked by allowed_events", async () => {
    const { main } = require("./submit_pr_review.cjs");
    const localBuffer = createReviewBuffer();
    const localHandler = await main({
      max: 1,
      _prReviewBuffer: localBuffer,
      allowed_events: ["COMMENT"],
    });

    // Blocked event should not consume a slot
    const blocked = await localHandler({ event: "APPROVE", body: "" }, {});
    expect(blocked.success).toBe(false);

    // Allowed event should still succeed since blocked one didn't count
    const allowed = await localHandler({ event: "COMMENT", body: "feedback" }, {});
    expect(allowed.success).toBe(true);
    expect(allowed.event).toBe("COMMENT");
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
    // Simulate a PR trigger context (resolveTarget uses eventName)
    global.context = {
      eventName: "pull_request",
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

    delete global.context;
  });

  it("should set review context from issue_comment on a PR for body-only reviews", async () => {
    global.github = {
      rest: {
        pulls: {
          get: vi.fn().mockResolvedValue({
            data: {
              number: 42,
              head: { sha: "issue-comment-pr-sha" },
            },
          }),
        },
      },
      graphql: vi.fn().mockResolvedValue({}),
    };

    global.context = {
      eventName: "issue_comment",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {
        issue: {
          number: 42,
          pull_request: { url: "https://api.github.com/repos/test-owner/test-repo/pulls/42" },
        },
      },
    };

    const localBuffer = createReviewBuffer();
    const { main } = require("./submit_pr_review.cjs");
    const localHandler = await main({ max: 1, _prReviewBuffer: localBuffer });

    const result = await localHandler(
      {
        type: "submit_pull_request_review",
        body: "Looks good overall.",
        event: "COMMENT",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(localBuffer.hasReviewMetadata()).toBe(true);
    const ctx = localBuffer.getReviewContext();
    expect(ctx).not.toBeNull();
    expect(ctx.repo).toBe("test-owner/test-repo");
    expect(ctx.pullRequestNumber).toBe(42);
    expect(global.github.rest.pulls.get).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      pull_number: 42,
    });

    delete global.context;
    delete global.github;
  });

  it("should set review context from target config when explicit PR number (e.g. workflow_dispatch)", async () => {
    const fetchedPR = {
      number: 99,
      head: { sha: "fetched-sha" },
      user: { login: "author" },
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "my-owner", repo: "my-repo" },
      payload: {},
    };
    global.github = {
      rest: {
        pulls: {
          get: vi.fn().mockResolvedValue({ data: fetchedPR }),
        },
      },
    };

    const localBuffer = createReviewBuffer();
    const { main } = require("./submit_pr_review.cjs");
    const localHandler = await main({ max: 1, target: "99", _prReviewBuffer: localBuffer });

    const message = {
      type: "submit_pull_request_review",
      body: "Approved via target",
      event: "APPROVE",
    };

    const result = await localHandler(message, {});

    expect(result.success).toBe(true);
    expect(localBuffer.hasReviewMetadata()).toBe(true);
    const ctx = localBuffer.getReviewContext();
    expect(ctx).not.toBeNull();
    expect(ctx.repo).toBe("my-owner/my-repo");
    expect(ctx.pullRequestNumber).toBe(99);
    expect(ctx.pullRequest.head.sha).toBe("fetched-sha");
    expect(global.github.rest.pulls.get).toHaveBeenCalledWith({
      owner: "my-owner",
      repo: "my-repo",
      pull_number: 99,
    });

    delete global.context;
    delete global.github;
  });

  it("should not set review context when target is triggering and not in PR context", async () => {
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "o", repo: "r" },
      payload: {},
    };

    const localBuffer = createReviewBuffer();
    const { main } = require("./submit_pr_review.cjs");
    const localHandler = await main({ max: 1, _prReviewBuffer: localBuffer });

    const message = {
      type: "submit_pull_request_review",
      body: "Review",
      event: "COMMENT",
    };

    const result = await localHandler(message, {});

    expect(result.success).toBe(true);
    expect(localBuffer.hasReviewMetadata()).toBe(true);
    expect(localBuffer.getReviewContext()).toBeNull();

    delete global.context;
  });

  it("should set review context from target '*' with message.pull_request_number", async () => {
    const fetchedPR = {
      number: 5,
      head: { sha: "sha5" },
      user: { login: "u" },
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "o", repo: "r" },
      payload: {},
    };
    global.github = {
      rest: {
        pulls: {
          get: vi.fn().mockResolvedValue({ data: fetchedPR }),
        },
      },
    };

    const localBuffer = createReviewBuffer();
    const { main } = require("./submit_pr_review.cjs");
    const localHandler = await main({ max: 1, target: "*", _prReviewBuffer: localBuffer });

    const message = {
      type: "submit_pull_request_review",
      body: "Review",
      event: "APPROVE",
      pull_request_number: 5,
    };

    const result = await localHandler(message, {});

    expect(result.success).toBe(true);
    const ctx = localBuffer.getReviewContext();
    expect(ctx).not.toBeNull();
    expect(ctx.pullRequestNumber).toBe(5);
    expect(global.github.rest.pulls.get).toHaveBeenCalledWith({
      owner: "o",
      repo: "r",
      pull_number: 5,
    });

    delete global.context;
    delete global.github;
  });

  it("should use target-repo for cross-repo PR review submission", async () => {
    const fetchedPR = {
      number: 1,
      head: { sha: "consumer-sha" },
      user: { login: "author" },
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "provider-org", repo: "provider-repo" },
      payload: {},
    };
    global.github = {
      rest: {
        pulls: {
          get: vi.fn().mockResolvedValue({ data: fetchedPR }),
        },
      },
    };

    const localBuffer = createReviewBuffer();
    const { main } = require("./submit_pr_review.cjs");
    const localHandler = await main({
      max: 1,
      target: "1",
      "target-repo": "consumer-org/consumer-repo",
      _prReviewBuffer: localBuffer,
    });

    const message = {
      type: "submit_pull_request_review",
      body: "LGTM",
      event: "APPROVE",
    };

    const result = await localHandler(message, {});

    expect(result.success).toBe(true);
    const ctx = localBuffer.getReviewContext();
    expect(ctx).not.toBeNull();
    expect(ctx.repo).toBe("consumer-org/consumer-repo");
    expect(ctx.pullRequestNumber).toBe(1);
    expect(ctx.pullRequest.head.sha).toBe("consumer-sha");
    // Ensure API was called on consumer-repo, not provider-repo
    expect(global.github.rest.pulls.get).toHaveBeenCalledWith({
      owner: "consumer-org",
      repo: "consumer-repo",
      pull_number: 1,
    });

    delete global.context;
    delete global.github;
  });

  it("should use target-repo with allowed-repos for cross-repo review", async () => {
    const fetchedPR = {
      number: 7,
      head: { sha: "allowed-sha" },
      user: { login: "author" },
    };
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "provider-org", repo: "provider-repo" },
      payload: {},
    };
    global.github = {
      rest: {
        pulls: {
          get: vi.fn().mockResolvedValue({ data: fetchedPR }),
        },
      },
    };

    const localBuffer = createReviewBuffer();
    const { main } = require("./submit_pr_review.cjs");
    const localHandler = await main({
      max: 1,
      target: "7",
      "target-repo": "consumer-org/consumer-repo",
      allowed_repos: ["consumer-org/other-repo"],
      _prReviewBuffer: localBuffer,
    });

    // Message with explicit repo matching an allowed repo
    const message = {
      type: "submit_pull_request_review",
      body: "Looks good",
      event: "APPROVE",
      repo: "consumer-org/other-repo",
    };

    const result = await localHandler(message, {});

    expect(result.success).toBe(true);
    const ctx = localBuffer.getReviewContext();
    expect(ctx).not.toBeNull();
    expect(ctx.repo).toBe("consumer-org/other-repo");
    expect(global.github.rest.pulls.get).toHaveBeenCalledWith({
      owner: "consumer-org",
      repo: "other-repo",
      pull_number: 7,
    });

    delete global.context;
    delete global.github;
  });

  it("should reject cross-repo review when repo is not in allowed list", async () => {
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "provider-org", repo: "provider-repo" },
      payload: {},
    };

    const localBuffer = createReviewBuffer();
    const { main } = require("./submit_pr_review.cjs");
    const localHandler = await main({
      max: 1,
      target: "1",
      "target-repo": "consumer-org/consumer-repo",
      _prReviewBuffer: localBuffer,
    });

    // Message with explicit repo that is not allowed
    const message = {
      type: "submit_pull_request_review",
      body: "Attempting to review disallowed repo",
      event: "APPROVE",
      repo: "attacker-org/attacker-repo",
    };

    const result = await localHandler(message, {});

    // Metadata is stored successfully (success refers to buffering, not final submission)
    expect(result.success).toBe(true);
    expect(localBuffer.hasReviewMetadata()).toBe(true);

    // Review context should NOT be set because the disallowed repo was rejected.
    // submitReview() will subsequently be skipped due to missing review context.
    expect(localBuffer.getReviewContext()).toBeNull();

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

describe("submit_pr_review multi-buffer (registry mode)", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    global.context = {
      eventName: "workflow_dispatch",
      repo: { owner: "o", repo: "r" },
      payload: {},
    };
    global.github = {
      rest: {
        pulls: {
          get: vi.fn().mockImplementation(({ pull_number }) => Promise.resolve({ data: { number: pull_number, head: { sha: `sha-${pull_number}` } } })),
        },
      },
    };
  });

  afterEach(() => {
    delete global.context;
    delete global.github;
  });

  it("two calls targeting distinct PRs each populate their own buffer", async () => {
    const registry = createPrReviewBufferRegistry();
    const { main } = require("./submit_pr_review.cjs");
    const handler = await main({ max: 5, target: "*", _prReviewBufferRegistry: registry });

    const result1 = await handler({ type: "submit_pull_request_review", body: "Body for PR 1", event: "COMMENT", pull_request_number: 1 }, {});
    const result2 = await handler({ type: "submit_pull_request_review", body: "Body for PR 2", event: "APPROVE", pull_request_number: 2 }, {});

    expect(result1.success).toBe(true);
    expect(result1.pull_request_number).toBe(1);
    expect(result1.repo).toBe("o/r");

    expect(result2.success).toBe(true);
    expect(result2.pull_request_number).toBe(2);
    expect(result2.repo).toBe("o/r");

    const entries = registry.getAllEntries();
    expect(entries).toHaveLength(2);

    const [e1, e2] = entries;
    expect(e1.prNumber).toBe(1);
    expect(e1.buffer.hasReviewMetadata()).toBe(true);

    expect(e2.prNumber).toBe(2);
    expect(e2.buffer.hasReviewMetadata()).toBe(true);

    // Buffers are independent — metadata does not leak between them
    const ctx1 = e1.buffer.getReviewContext();
    const ctx2 = e2.buffer.getReviewContext();
    expect(ctx1.pullRequestNumber).toBe(1);
    expect(ctx2.pullRequestNumber).toBe(2);
  });

  it("second call targeting the same PR is rejected with an error", async () => {
    const registry = createPrReviewBufferRegistry();
    const { main } = require("./submit_pr_review.cjs");
    const handler = await main({ max: 5, target: "*", _prReviewBufferRegistry: registry });

    const result1 = await handler({ body: "First call", event: "COMMENT", pull_request_number: 7 }, {});
    const result2 = await handler({ body: "Second call", event: "APPROVE", pull_request_number: 7 }, {});

    expect(result1.success).toBe(true);
    // Second call to the same PR must be rejected — only one submit per PR per run is allowed
    expect(result2.success).toBe(false);
    expect(result2.error).toMatch(/already has a pending review submission/);

    // Only one buffer entry created (PR 7)
    const entries = registry.getAllEntries();
    expect(entries).toHaveLength(1);
    expect(entries[0].prNumber).toBe(7);
  });

  it("returns a skipped result when triggering target is outside PR context in registry mode", async () => {
    // workflow_dispatch with target:triggering => soft skip because no PR context exists
    const registry = createPrReviewBufferRegistry();
    const { main } = require("./submit_pr_review.cjs");
    const handler = await main({ max: 1, target: "triggering", _prReviewBufferRegistry: registry });

    const result = await handler({ body: "Review", event: "COMMENT" }, {});

    expect(result).toMatchObject({ success: false, skipped: true });
    expect(result.error).toContain("not running in pull request context");
    // Registry should be empty — nothing was buffered
    expect(registry.getAllEntries()).toHaveLength(0);
  });

  it("returns a failure when target resolution has a hard error in registry mode", async () => {
    const registry = createPrReviewBufferRegistry();
    const { main } = require("./submit_pr_review.cjs");
    const handler = await main({ max: 1, target: "*", _prReviewBufferRegistry: registry });

    const result = await handler({ body: "Review", event: "COMMENT" }, {});

    expect(result.success).toBe(false);
    expect(result.skipped).toBeUndefined();
    expect(result.error).toContain('Target is "*"');
    // Registry should be empty — nothing was buffered
    expect(registry.getAllEntries()).toHaveLength(0);
  });

  it("staged defaults propagate to each new buffer via registry.setDefaultStaged", async () => {
    const registry = createPrReviewBufferRegistry();
    const setDefaultStagedSpy = vi.spyOn(registry, "setDefaultStaged");
    const { main } = require("./submit_pr_review.cjs");
    await main({ max: 1, target: "*", staged: true, _prReviewBufferRegistry: registry });
    expect(setDefaultStagedSpy).toHaveBeenCalledWith(true);
  });

  it("supersede_older_reviews default propagates via registry.setDefaultSupersedeOlderReviews", async () => {
    const registry = createPrReviewBufferRegistry();
    const setSuperSpy = vi.spyOn(registry, "setDefaultSupersedeOlderReviews");
    const { main } = require("./submit_pr_review.cjs");
    await main({ max: 1, target: "*", supersede_older_reviews: true, _prReviewBufferRegistry: registry });
    expect(setSuperSpy).toHaveBeenCalledWith(true);
  });
});

describe("submit_pr_review: GH_AW_HEAD_SHA automatic commit pinning", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    delete global.context;
    delete global.github;
  });

  it("review context has no commitId field (GH_AW_HEAD_SHA is used at submit time instead)", async () => {
    global.context = {
      eventName: "pull_request",
      repo: { owner: "test-owner", repo: "test-repo" },
      payload: {
        pull_request: { number: 42, head: { sha: "live-head-sha" } },
      },
    };
    global.github = {
      rest: { pulls: { get: vi.fn() } },
      graphql: vi.fn(),
    };

    const localBuffer = createReviewBuffer();
    const { main } = require("./submit_pr_review.cjs");
    const localHandler = await main({
      max: 1,
      _prReviewBuffer: localBuffer,
    });

    const result = await localHandler({ type: "submit_pull_request_review", body: "Review", event: "COMMENT" }, {});

    expect(result.success).toBe(true);
    const ctx = localBuffer.getReviewContext();
    expect(ctx).not.toBeNull();
    // No commitId on the context; GH_AW_HEAD_SHA is read at submitReview() time
    expect(ctx.commitId).toBeUndefined();
    expect(ctx.pullRequest.head.sha).toBe("live-head-sha");
    expect(global.github.rest.pulls.get).not.toHaveBeenCalled();
  });

  it("review context has no commitId in workflow_run scenario (GH_AW_HEAD_SHA used automatically)", async () => {
    const fetchedPR = { number: 55, head: { sha: "live-head-after-push" } };
    global.context = {
      eventName: "workflow_run",
      repo: { owner: "org", repo: "repo" },
      payload: {},
    };
    global.github = {
      rest: {
        pulls: {
          get: vi.fn().mockResolvedValue({ data: fetchedPR }),
        },
      },
      graphql: vi.fn(),
    };

    const localBuffer = createReviewBuffer();
    const { main } = require("./submit_pr_review.cjs");
    const localHandler = await main({
      max: 1,
      target: "55",
      _prReviewBuffer: localBuffer,
    });

    const result = await localHandler({ type: "submit_pull_request_review", body: "Review", event: "COMMENT" }, {});

    expect(result.success).toBe(true);
    const ctx = localBuffer.getReviewContext();
    expect(ctx).not.toBeNull();
    // No commitId on context; GH_AW_HEAD_SHA env var is used at submit time
    expect(ctx.commitId).toBeUndefined();
    expect(ctx.pullRequest.head.sha).toBe("live-head-after-push");
  });
});
