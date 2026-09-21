// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};

global.core = mockCore;

const mockGraphql = vi.fn();
const mockRestPullsGet = vi.fn();
const mockRestPullsUpdate = vi.fn();
const mockRestIssuesCreateComment = vi.fn();

const mockGithub = {
  graphql: mockGraphql,
  rest: {
    pulls: {
      get: mockRestPullsGet,
      update: mockRestPullsUpdate,
    },
    issues: {
      createComment: mockRestIssuesCreateComment,
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

/**
 * Build a default mock pull request object for use in REST API responses.
 * @param {number} prNumber
 * @param {{ draft?: boolean, node_id?: string }} [overrides]
 */
function makePR(prNumber, overrides = {}) {
  return {
    number: prNumber,
    title: "Test PR",
    html_url: `https://github.com/test-owner/test-repo/pull/${prNumber}`,
    draft: overrides.draft !== undefined ? overrides.draft : true,
    node_id: overrides.node_id || "PR_kwDOABCD123456",
  };
}

/**
 * Set up default mock behaviour: REST get returns a draft PR, GraphQL mutation succeeds.
 * @param {number} [prNumber]
 */
function setupDefaultMocks(prNumber = 42) {
  mockRestPullsGet.mockResolvedValue({ data: makePR(prNumber, { draft: true }) });

  mockGraphql.mockResolvedValue({
    markPullRequestReadyForReview: {
      pullRequest: {
        number: prNumber,
        isDraft: false,
        url: `https://github.com/test-owner/test-repo/pull/${prNumber}`,
        title: "Test PR",
      },
    },
  });

  mockRestIssuesCreateComment.mockResolvedValue({
    data: {
      id: 456,
      html_url: `https://github.com/test-owner/test-repo/pull/${prNumber}#issuecomment-456`,
    },
  });
}

describe("mark_pull_request_as_ready_for_review", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    setupDefaultMocks();
  });

  describe("main factory", () => {
    it("should create a handler function with default configuration", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main();
      expect(typeof handler).toBe("function");
    });

    it("should create a handler function with custom max configuration", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 5 });
      expect(typeof handler).toBe("function");
    });

    it("should log configuration on initialization", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      await main({ max: 3 });
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("max=3"));
    });
  });

  describe("handleMarkPullRequestAsReadyForReview", () => {
    it("should use GraphQL mutation to mark a draft PR as ready for review", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      const result = await handler({ pull_request_number: 42, reason: "All tests passing" }, {});

      expect(result.success).toBe(true);
      expect(result.number).toBe(42);
      // GraphQL mutation must have been called (not REST pulls.update)
      expect(mockGraphql).toHaveBeenCalledWith(expect.stringContaining("markPullRequestReadyForReview"), expect.objectContaining({ pullRequestId: "PR_kwDOABCD123456" }));
    });

    it("should NOT call REST pulls.update (the broken endpoint)", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      await handler({ pull_request_number: 42, reason: "Ready!" }, {});

      expect(mockRestPullsUpdate).not.toHaveBeenCalled();
    });

    it("should use context PR number when pull_request_number is not provided", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "triggering" });

      const result = await handler({ reason: "Ready for review" }, {});

      expect(result.success).toBe(true);
      expect(mockRestPullsGet).toHaveBeenCalledWith(expect.objectContaining({ pull_number: 42 }));
    });

    it("should return success with correct fields on success", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      const result = await handler({ pull_request_number: 42, reason: "LGTM" }, {});

      expect(result.success).toBe(true);
      expect(result.number).toBe(42);
      expect(result.url).toBe("https://github.com/test-owner/test-repo/pull/42");
      expect(result.title).toBe("Test PR");
    });

    it("should skip and return alreadyReady=true when PR is not a draft", async () => {
      mockRestPullsGet.mockResolvedValue({ data: makePR(42, { draft: false }) });

      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      const result = await handler({ pull_request_number: 42, reason: "Already ready" }, {});

      expect(result.success).toBe(true);
      expect(result.alreadyReady).toBe(true);
      // GraphQL mutation must NOT have been called
      expect(mockGraphql).not.toHaveBeenCalled();
    });

    it("should return failure when GraphQL mutation reports PR is still a draft", async () => {
      mockGraphql.mockResolvedValue({
        markPullRequestReadyForReview: {
          pullRequest: {
            number: 42,
            isDraft: true,
            url: "https://github.com/test-owner/test-repo/pull/42",
            title: "Test PR",
          },
        },
      });

      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      const result = await handler({ pull_request_number: 42, reason: "Ready" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("still in draft state");
    });

    it("should add a comment after successfully marking ready for review", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      await handler({ pull_request_number: 42, reason: "All checks passing" }, {});

      expect(mockRestIssuesCreateComment).toHaveBeenCalledWith(
        expect.objectContaining({
          owner: "test-owner",
          repo: "test-repo",
          issue_number: 42,
          body: expect.stringContaining("All checks passing"),
        })
      );
    });

    it("should return failure for invalid pull_request_number", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      const result = await handler({ pull_request_number: "not-a-number", reason: "Ready" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Invalid pull_request_number");
    });

    it("should return failure when no PR number and not in PR context", async () => {
      const originalPayload = global.context.payload;
      global.context.payload = { repository: { html_url: "https://github.com/test-owner/test-repo" } };

      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "triggering" });

      const result = await handler({ reason: "Ready" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("no pull request found");

      global.context.payload = originalPayload;
    });

    it("should return failure when reason is missing", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      const result = await handler({ pull_request_number: 42 }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Reason is required");
    });

    it("should return failure when reason is empty string", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      const result = await handler({ pull_request_number: 42, reason: "" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Reason is required");
    });

    it("should return failure when reason is whitespace only", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      const result = await handler({ pull_request_number: 42, reason: "   " }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Reason is required");
    });

    it("should respect max count limit", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 2, target: "*" });

      // First two succeed
      const r1 = await handler({ pull_request_number: 42, reason: "Ready 1" }, {});
      const r2 = await handler({ pull_request_number: 42, reason: "Ready 2" }, {});
      // Third is blocked
      const r3 = await handler({ pull_request_number: 42, reason: "Ready 3" }, {});

      expect(r1.success).toBe(true);
      expect(r2.success).toBe(true);
      expect(r3.success).toBe(false);
      expect(r3.error).toContain("Max count of 2 reached");
    });

    it("should handle GraphQL API errors gracefully", async () => {
      mockGraphql.mockRejectedValue(new Error("GraphQL request failed: Resource not accessible by integration"));

      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      const result = await handler({ pull_request_number: 42, reason: "Ready" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("GraphQL request failed");
    });

    it("should handle REST get PR errors gracefully", async () => {
      mockRestPullsGet.mockRejectedValue(new Error("Not Found"));

      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      const result = await handler({ pull_request_number: 42, reason: "Ready" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("Not Found");
    });

    it("should pass node_id from REST response as pullRequestId to GraphQL mutation", async () => {
      mockRestPullsGet.mockResolvedValue({
        data: makePR(42, { draft: true, node_id: "PR_kwDOCustomNodeId" }),
      });

      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      await handler({ pull_request_number: 42, reason: "Ready" }, {});

      expect(mockGraphql).toHaveBeenCalledWith(expect.stringContaining("markPullRequestReadyForReview"), expect.objectContaining({ pullRequestId: "PR_kwDOCustomNodeId" }));
    });

    it("should use staged mode without executing GraphQL mutation", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, staged: true, target: "*" });

      const result = await handler({ pull_request_number: 42, reason: "Staged test" }, {});

      expect(result.success).toBe(true);
      expect(result.staged).toBe(true);
      expect(mockGraphql).not.toHaveBeenCalled();
    });
  });

  describe("target-repo support", () => {
    it("should use target-repo config for PR fetch and comment", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({
        max: 10,
        "target-repo": "external-org/external-repo",
        target: "*",
      });

      setupDefaultMocks(42);

      const result = await handler({ pull_request_number: 42, reason: "Ready for review" }, {});

      expect(result.success).toBe(true);
      expect(mockRestPullsGet).toHaveBeenCalledWith(expect.objectContaining({ owner: "external-org", repo: "external-repo" }));
      expect(mockRestIssuesCreateComment).toHaveBeenCalledWith(expect.objectContaining({ owner: "external-org", repo: "external-repo" }));
    });

    it("should use context.repo as default when no target-repo configured", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({ max: 10, target: "*" });

      setupDefaultMocks(42);

      const result = await handler({ pull_request_number: 42, reason: "Ready" }, {});

      expect(result.success).toBe(true);
      expect(mockRestPullsGet).toHaveBeenCalledWith(expect.objectContaining({ owner: "test-owner", repo: "test-repo" }));
    });

    it("should use repo from message when allowed_repos is configured", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({
        max: 10,
        "target-repo": "default-org/default-repo",
        allowed_repos: ["cross-org/cross-repo"],
        target: "*",
      });

      setupDefaultMocks(42);

      const result = await handler({ pull_request_number: 42, reason: "Ready", repo: "cross-org/cross-repo" }, {});

      expect(result.success).toBe(true);
      expect(mockRestPullsGet).toHaveBeenCalledWith(expect.objectContaining({ owner: "cross-org", repo: "cross-repo" }));
    });

    it("should reject repo not in allowed_repos list", async () => {
      const { main } = require("./mark_pull_request_as_ready_for_review.cjs");
      const handler = await main({
        max: 10,
        "target-repo": "default-org/default-repo",
        allowed_repos: ["allowed-org/allowed-repo"],
      });

      const result = await handler({ pull_request_number: 42, reason: "Ready", repo: "unauthorized-org/unauthorized-repo" }, {});

      expect(result.success).toBe(false);
      expect(result.error).toContain("not in the allowed-repos list");
    });
  });
});
