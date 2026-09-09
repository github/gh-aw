// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";
const { ERR_NOT_FOUND, ERR_VALIDATION, ERR_API } = require("./error_codes.cjs");

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
  summary: { addRaw: vi.fn().mockReturnThis(), write: vi.fn().mockResolvedValue(undefined) },
};

const mockGithub = {
  request: vi.fn(),
  graphql: vi.fn(),
  rest: { issues: { createComment: vi.fn() } },
};

const mockContext = {
  eventName: "issues",
  runId: 12345,
  repo: { owner: "testowner", repo: "testrepo" },
  payload: {
    issue: { number: 123 },
    repository: { html_url: "https://github.com/testowner/testrepo" },
  },
};

global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

// Helper to import the module fresh (bust module cache)
async function loadModule() {
  const { main, addCommentWithWorkflowLink, addReaction, addDiscussionReaction, resolveEventEndpoints, VALID_REACTIONS, expectRestEndpoint, parseDiscussionEndpoint, requireEventField } = await import(
    "./add_reaction_and_edit_comment.cjs?" + Date.now()
  );
  return { main, addCommentWithWorkflowLink, addReaction, addDiscussionReaction, resolveEventEndpoints, VALID_REACTIONS, expectRestEndpoint, parseDiscussionEndpoint, requireEventField };
}

describe("add_reaction_and_edit_comment.cjs", () => {
  beforeEach(() => {
    vi.resetAllMocks();
    delete process.env.GH_AW_REACTION;
    delete process.env.GH_AW_COMMANDS;
    delete process.env.GH_AW_WORKFLOW_NAME;
    delete process.env.GH_AW_WORKFLOW_EMOJI;
    delete process.env.GH_AW_LOCK_FOR_AGENT;
    delete process.env.GITHUB_WORKFLOW;
    delete process.env.GH_AW_TRACKER_ID;
    delete process.env.GITHUB_SERVER_URL;

    global.context = {
      eventName: "issues",
      runId: 12345,
      repo: { owner: "testowner", repo: "testrepo" },
      payload: {
        issue: { number: 123 },
        repository: { html_url: "https://github.com/testowner/testrepo" },
      },
    };

    mockGithub.request.mockResolvedValue({ data: { id: 456, html_url: "https://github.com/testowner/testrepo/issues/123#issuecomment-456" } });
    mockGithub.graphql.mockResolvedValue({
      repository: { discussion: { id: "D_kwDOABcD1M4AaBbC", url: "https://github.com/testowner/testrepo/discussions/10" } },
      addReaction: { reaction: { id: "MDg6UmVhY3Rpb24xMjM0NTY3ODk=", content: "EYES" } },
      addDiscussionComment: { comment: { id: "DC_kwDOABcD1M4AaBbE", url: "https://github.com/testowner/testrepo/discussions/10#discussioncomment-999" } },
    });
  });

  describe("discussion endpoint validation", () => {
    it("rejects malformed and non-positive discussion endpoint numbers", async () => {
      const { parseDiscussionEndpoint } = await loadModule();

      for (const endpoint of ["discussion:5junk", "discussion:0", "discussion:5:extra"]) {
        expect(() => parseDiscussionEndpoint(endpoint, "discussion")).toThrow("Invalid discussion endpoint");
      }
      expect(() => parseDiscussionEndpoint("discussion_comment:5:2junk", "discussion_comment")).toThrow("Invalid discussion endpoint");
    });
  });

  describe("Issue reactions", () => {
    it("should add reaction to issue successfully", async () => {
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "issues";
      global.context.payload = { issue: { number: 123 }, repository: { html_url: "https://github.com/testowner/testrepo" } };
      mockGithub.request.mockResolvedValueOnce({ data: { id: 456 } });

      const { main } = await loadModule();
      await main();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", expect.objectContaining({ content: "eyes", owner: "testowner", repo: "testrepo", issue_number: 123 }));
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "456");
    });

    describe("Endpoint validation", () => {
      it("should throw a validation error when a REST endpoint is unexpectedly a string", async () => {
        const { expectRestEndpoint } = await loadModule();

        expect(() => expectRestEndpoint("discussion:12", "reaction", "issues")).toThrow(`${ERR_VALIDATION}: Unexpected reaction endpoint shape for event: issues`);
      });
    });

    it("should default to 'eyes' reaction when GH_AW_REACTION is not set", async () => {
      // GH_AW_REACTION is deleted in beforeEach
      global.context.eventName = "issues";
      global.context.payload = { issue: { number: 123 }, repository: { html_url: "https://github.com/testowner/testrepo" } };
      mockGithub.request.mockResolvedValueOnce({ data: { id: 456 } });

      const { main } = await loadModule();
      await main();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", expect.objectContaining({ content: "eyes", owner: "testowner", repo: "testrepo", issue_number: 123 }));
    });

    it("should reject invalid reaction type", async () => {
      process.env.GH_AW_REACTION = "invalid";
      global.context.eventName = "issues";

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Invalid reaction type: invalid"));
      expect(mockGithub.request).not.toHaveBeenCalled();
    });

    it("should fail when issue number is missing", async () => {
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "issues";
      global.context.payload = {};

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Issue number not found in event payload`);
      expect(mockGithub.request).not.toHaveBeenCalled();
    });
  });

  describe("Pull request reactions", () => {
    it("should add reaction to pull request and create comment", async () => {
      process.env.GH_AW_REACTION = "heart";
      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
      process.env.GH_AW_WORKFLOW_EMOJI = "🤖";
      global.context.eventName = "pull_request";
      global.context.payload = {
        pull_request: { number: 456 },
        repository: { html_url: "https://github.com/testowner/testrepo" },
      };
      mockGithub.request.mockResolvedValueOnce({ data: { id: 789 } }).mockResolvedValueOnce({ data: { id: 999, html_url: "https://github.com/testowner/testrepo/pull/456#issuecomment-999" } });

      const { main } = await loadModule();
      await main();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", expect.objectContaining({ content: "heart", owner: "testowner", repo: "testrepo", issue_number: 456 }));
      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/comments", expect.objectContaining({ body: expect.stringContaining("🤖 [Test Workflow]") }));
      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/comments", expect.objectContaining({ body: expect.stringContaining("has started processing this pull request") }));
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "789");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "999");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/testowner/testrepo/pull/456#issuecomment-999");
    });

    it("should fail when PR number is missing", async () => {
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "pull_request";
      global.context.payload = {};

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Pull request number not found in event payload`);
    });
  });

  describe("Issue comment reactions", () => {
    it("should create new comment for issue_comment event (not edit)", async () => {
      process.env.GH_AW_REACTION = "eyes";
      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
      global.context.eventName = "issue_comment";
      global.context.payload = {
        issue: { number: 123 },
        comment: { id: 456 },
        repository: { html_url: "https://github.com/testowner/testrepo" },
      };
      mockGithub.request.mockResolvedValueOnce({ data: { id: 111 } }).mockResolvedValueOnce({ data: { id: 789, html_url: "https://github.com/testowner/testrepo/issues/123#issuecomment-789" } });

      const { main } = await loadModule();
      await main();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/comments", expect.objectContaining({ body: expect.stringContaining("has started processing this issue comment") }));
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "789");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/testowner/testrepo/issues/123#issuecomment-789");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "testowner/testrepo");
    });

    it("should fail when comment ID is missing", async () => {
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "issue_comment";
      global.context.payload = { issue: { number: 123 } };

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_VALIDATION}: Comment ID not found in event payload`);
    });
  });

  describe("repository_dispatch reactions", () => {
    it("should use workflow repo for run URL and event repo for reaction/comment APIs", async () => {
      process.env.GH_AW_REACTION = "eyes";
      global.context = {
        eventName: "repository_dispatch",
        runId: 12345,
        repo: { owner: "sideowner", repo: "siderepo" },
        payload: {
          action: "issue_comment",
          client_payload: {
            issue: { number: 123 },
            comment: { id: 456 },
            repository: { owner: { login: "targetowner" }, name: "targetrepo" },
          },
        },
      };
      mockGithub.request.mockResolvedValueOnce({ data: { id: 111 } }).mockResolvedValueOnce({ data: { id: 789, html_url: "https://github.com/targetowner/targetrepo/issues/123#issuecomment-789" } });

      const { main } = await loadModule();
      await main();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/comments/{comment_id}/reactions", expect.objectContaining({ content: "eyes", owner: "targetowner", repo: "targetrepo", comment_id: 456 }));
      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/comments", expect.objectContaining({ body: expect.stringContaining("https://github.com/sideowner/siderepo/actions/runs/12345") }));
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "targetowner/targetrepo");
    });
  });

  describe("Pull request review comment reactions", () => {
    it("should create new comment for pull_request_review_comment event (not edit)", async () => {
      process.env.GH_AW_REACTION = "rocket";
      process.env.GH_AW_WORKFLOW_NAME = "PR Review Bot";
      global.context.eventName = "pull_request_review_comment";
      global.context.payload = {
        pull_request: { number: 456 },
        comment: { id: 789 },
        repository: { html_url: "https://github.com/testowner/testrepo" },
      };
      mockGithub.request.mockResolvedValueOnce({ data: { id: 222 } }).mockResolvedValueOnce({ data: { id: 999, html_url: "https://github.com/testowner/testrepo/pull/456#discussion_r999" } });

      const { main } = await loadModule();
      await main();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/comments", expect.objectContaining({ body: expect.stringContaining("has started processing this pull request review comment") }));
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "999");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/testowner/testrepo/pull/456#discussion_r999");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "testowner/testrepo");
    });
  });

  describe("Discussion reactions", () => {
    it("should add reaction to discussion using GraphQL", async () => {
      mockGithub.graphql.mockResolvedValueOnce({ addReaction: { reaction: { id: "MDg6UmVhY3Rpb24xMjM0NTY3ODk=", content: "ROCKET" } } });

      const { addDiscussionReaction } = await loadModule();
      await addDiscussionReaction("D_kwDOABcD1M4AaBbC", "rocket");

      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("mutation"), expect.objectContaining({ subjectId: "D_kwDOABcD1M4AaBbC", content: "ROCKET" }));
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "MDg6UmVhY3Rpb24xMjM0NTY3ODk=");
    });

    it("should map all reaction types correctly for GraphQL", async () => {
      const reactionTests = [
        { input: "+1", expected: "THUMBS_UP" },
        { input: "-1", expected: "THUMBS_DOWN" },
        { input: "laugh", expected: "LAUGH" },
        { input: "confused", expected: "CONFUSED" },
        { input: "heart", expected: "HEART" },
        { input: "hooray", expected: "HOORAY" },
        { input: "rocket", expected: "ROCKET" },
        { input: "eyes", expected: "EYES" },
      ];

      for (const test of reactionTests) {
        vi.clearAllMocks();
        mockGithub.graphql.mockResolvedValueOnce({ addReaction: { reaction: { id: "abc", content: test.expected } } });

        const { addDiscussionReaction } = await loadModule();
        await addDiscussionReaction("D_kwDOABcD1M4AaBbC", test.input);

        expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("mutation"), expect.objectContaining({ content: test.expected }));
      }
    });

    it("should create comment on discussion", async () => {
      process.env.GH_AW_REACTION = "eyes";
      process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
      global.context.eventName = "discussion";
      global.context.payload = {
        discussion: { number: 10 },
        repository: { html_url: "https://github.com/testowner/testrepo" },
      };
      mockGithub.graphql
        .mockResolvedValueOnce({ repository: { discussion: { id: "D_kwDOABcD1M4AaBbC", url: "https://github.com/testowner/testrepo/discussions/10" } } })
        .mockResolvedValueOnce({ addReaction: { reaction: { id: "MDg6UmVhY3Rpb24xMjM0NTY3ODk=", content: "EYES" } } })
        .mockResolvedValueOnce({ repository: { discussion: { id: "D_kwDOABcD1M4AaBbC", url: "https://github.com/testowner/testrepo/discussions/10" } } })
        .mockResolvedValueOnce({ addDiscussionComment: { comment: { id: "DC_kwDOABcD1M4AaBbE", url: "https://github.com/testowner/testrepo/discussions/10#discussioncomment-999" } } });

      const { main } = await loadModule();
      await main();

      expect(mockGithub.graphql).toHaveBeenCalledTimes(4);
      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("addDiscussionComment"), expect.objectContaining({ dId: "D_kwDOABcD1M4AaBbC", body: expect.stringContaining("has started processing this discussion") }));
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "MDg6UmVhY3Rpb24xMjM0NTY3ODk=");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "DC_kwDOABcD1M4AaBbE");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/testowner/testrepo/discussions/10#discussioncomment-999");
    });

    it("should fail when discussion number is missing", async () => {
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "discussion";
      global.context.payload = { repository: { html_url: "https://github.com/testowner/testrepo" } };

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Discussion number not found in event payload`);
    });
  });

  describe("Discussion comment reactions", () => {
    it("should add reaction to discussion comment using GraphQL", async () => {
      process.env.GH_AW_REACTION = "heart";
      global.context.eventName = "discussion_comment";
      global.context.payload = {
        discussion: { number: 10 },
        comment: { id: 123, node_id: "DC_kwDOABcD1M4AaBbC", html_url: "https://github.com/testowner/testrepo/discussions/10#discussioncomment-123" },
        repository: { html_url: "https://github.com/testowner/testrepo" },
      };
      mockGithub.graphql.mockResolvedValueOnce({ addReaction: { reaction: { id: "MDg6UmVhY3Rpb24xMjM0NTY3ODk=", content: "HEART" } } });

      const { addDiscussionReaction } = await loadModule();
      await addDiscussionReaction("DC_kwDOABcD1M4AaBbC", "heart");

      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("mutation"), expect.objectContaining({ subjectId: "DC_kwDOABcD1M4AaBbC", content: "HEART" }));
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "MDg6UmVhY3Rpb24xMjM0NTY3ODk=");
    });

    it("should fail when discussion comment node_id is missing", async () => {
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "discussion_comment";
      global.context.payload = {
        discussion: { number: 10 },
        comment: { id: 123 },
        repository: { html_url: "https://github.com/testowner/testrepo" },
      };

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Discussion comment node ID not found in event payload`);
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });

    it("should create threaded comment for discussion_comment events", async () => {
      process.env.GH_AW_REACTION = "eyes";
      process.env.GH_AW_WORKFLOW_NAME = "Discussion Bot";
      global.context.eventName = "discussion_comment";
      global.context.payload = {
        discussion: { number: 10 },
        comment: { id: 123, node_id: "DC_kwDOABcD1M4AaBbC" },
        repository: { html_url: "https://github.com/testowner/testrepo" },
      };
      mockGithub.graphql
        .mockResolvedValueOnce({ addReaction: { reaction: { id: "MDg6UmVhY3Rpb24xMjM0NTY3ODk=", content: "EYES" } } })
        .mockResolvedValueOnce({ repository: { discussion: { id: "D_kwDOABcD1M4AaBbC", url: "https://github.com/testowner/testrepo/discussions/10" } } })
        .mockResolvedValueOnce({ node: { replyTo: null } }) // resolveTopLevelDiscussionCommentId: top-level comment
        .mockResolvedValueOnce({
          addDiscussionComment: {
            comment: { id: "DC_kwDOABcD1M4AaBbE", url: "https://github.com/testowner/testrepo/discussions/10#discussioncomment-789" },
          },
        });

      const { main } = await loadModule();
      await main();

      expect(mockGithub.graphql).toHaveBeenCalledWith(
        expect.stringContaining("addDiscussionComment"),
        expect.objectContaining({
          dId: "D_kwDOABcD1M4AaBbC",
          body: expect.stringContaining("has started processing this discussion comment"),
          replyToId: "DC_kwDOABcD1M4AaBbC",
        })
      );
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-id", "DC_kwDOABcD1M4AaBbE");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-url", "https://github.com/testowner/testrepo/discussions/10#discussioncomment-789");
      expect(mockCore.setOutput).toHaveBeenCalledWith("comment-repo", "testowner/testrepo");
    });

    it("should fail when discussion or comment fields are missing", async () => {
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "discussion_comment";
      global.context.payload = {
        discussion: { number: 10 },
        // Missing comment field
      };

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Discussion or comment information not found in event payload`);
    });
  });

  describe("Unsupported event types", () => {
    it("should fail for unsupported event type", async () => {
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "push";
      global.context.payload = { repository: { html_url: "https://github.com/testowner/testrepo" } };

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_VALIDATION}: Unsupported event type: push`);
    });
  });

  describe("Error handling", () => {
    it("should silently ignore locked issue errors (status 403 + locked message)", async () => {
      const lockedError = new Error("Issue is locked");
      /** @type {any} */ lockedError.status = 403;
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "issues";
      global.context.payload = { issue: { number: 123 }, repository: { html_url: "https://github.com/testowner/testrepo" } };
      mockGithub.request.mockRejectedValueOnce(lockedError);

      const { main } = await loadModule();
      await main();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("resource is locked"));
      expect(mockCore.error).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail for errors with 'locked' message but non-403 status", async () => {
      const lockedError = new Error("Lock conversation is enabled");
      /** @type {any} */ lockedError.status = 500;
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "issues";
      global.context.payload = { issue: { number: 123 }, repository: { html_url: "https://github.com/testowner/testrepo" } };
      mockGithub.request.mockRejectedValueOnce(lockedError);

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to process reaction"));
    });

    it("should fail for 403 errors that don't mention locked", async () => {
      const forbiddenError = new Error("Forbidden: insufficient permissions");
      /** @type {any} */ forbiddenError.status = 403;
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "issues";
      global.context.payload = { issue: { number: 123 }, repository: { html_url: "https://github.com/testowner/testrepo" } };
      mockGithub.request.mockRejectedValueOnce(forbiddenError);

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to process reaction"));
    });

    it("should warn and continue for rate limit 403 errors", async () => {
      const rateLimitError = new Error("API rate limit exceeded for installation");
      /** @type {any} */ rateLimitError.status = 403;
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "issues";
      global.context.payload = { issue: { number: 123 }, repository: { html_url: "https://github.com/testowner/testrepo" } };
      mockGithub.request.mockRejectedValueOnce(rateLimitError);

      const { main } = await loadModule();
      await main();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("GitHub API rate limiting"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail for other non-403 errors", async () => {
      const serverError = new Error("Internal server error");
      /** @type {any} */ serverError.status = 500;
      process.env.GH_AW_REACTION = "eyes";
      global.context.eventName = "issues";
      global.context.payload = { issue: { number: 123 }, repository: { html_url: "https://github.com/testowner/testrepo" } };
      mockGithub.request.mockRejectedValueOnce(serverError);

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(`${ERR_API}: Failed to process reaction`));
    });
  });

  describe("addCommentWithWorkflowLink() - markers", () => {
    it("should include workflow-id marker when GITHUB_WORKFLOW is set", async () => {
      process.env.GITHUB_WORKFLOW = "test-workflow.yml";
      mockGithub.request.mockResolvedValueOnce({ data: { id: 123, html_url: "https://example.com" } });

      const { addCommentWithWorkflowLink } = await loadModule();
      await addCommentWithWorkflowLink(
        { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "testowner", repo: "testrepo", issue_number: 123 } },
        "https://github.com/testowner/testrepo/actions/runs/12345",
        "issues"
      );

      expect(mockGithub.request).toHaveBeenCalledWith(expect.stringContaining("POST"), expect.objectContaining({ body: expect.stringContaining("<!-- gh-aw-workflow-id: test-workflow.yml -->") }));
    });

    it("should include tracker-id marker when GH_AW_TRACKER_ID is set", async () => {
      process.env.GH_AW_TRACKER_ID = "tracker-123";
      mockGithub.request.mockResolvedValueOnce({ data: { id: 123, html_url: "https://example.com" } });

      const { addCommentWithWorkflowLink } = await loadModule();
      await addCommentWithWorkflowLink(
        { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "testowner", repo: "testrepo", issue_number: 123 } },
        "https://github.com/testowner/testrepo/actions/runs/12345",
        "issues"
      );

      expect(mockGithub.request).toHaveBeenCalledWith(expect.stringContaining("POST"), expect.objectContaining({ body: expect.stringContaining("<!-- gh-aw-tracker-id: tracker-123 -->") }));
    });

    it("should always include reaction comment type marker", async () => {
      mockGithub.request.mockResolvedValueOnce({ data: { id: 123, html_url: "https://example.com" } });

      const { addCommentWithWorkflowLink } = await loadModule();
      await addCommentWithWorkflowLink(
        { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "testowner", repo: "testrepo", issue_number: 123 } },
        "https://github.com/testowner/testrepo/actions/runs/12345",
        "issues"
      );

      expect(mockGithub.request).toHaveBeenCalledWith(expect.stringContaining("POST"), expect.objectContaining({ body: expect.stringContaining("<!-- gh-aw-comment-type: reaction -->") }));
    });

    it("should add lock notice for issues event when GH_AW_LOCK_FOR_AGENT=true", async () => {
      process.env.GH_AW_LOCK_FOR_AGENT = "true";
      mockGithub.request.mockResolvedValueOnce({ data: { id: 123, html_url: "https://example.com" } });

      const { addCommentWithWorkflowLink } = await loadModule();
      await addCommentWithWorkflowLink(
        { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "testowner", repo: "testrepo", issue_number: 123 } },
        "https://github.com/testowner/testrepo/actions/runs/12345",
        "issues"
      );

      expect(mockGithub.request).toHaveBeenCalledWith(expect.stringContaining("POST"), expect.objectContaining({ body: expect.stringContaining("🔒 This issue has been locked") }));
    });

    it("should add lock notice for issue_comment event when GH_AW_LOCK_FOR_AGENT=true", async () => {
      process.env.GH_AW_LOCK_FOR_AGENT = "true";
      mockGithub.request.mockResolvedValueOnce({ data: { id: 123, html_url: "https://example.com" } });

      const { addCommentWithWorkflowLink } = await loadModule();
      await addCommentWithWorkflowLink(
        { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "testowner", repo: "testrepo", issue_number: 123 } },
        "https://github.com/testowner/testrepo/actions/runs/12345",
        "issue_comment"
      );

      expect(mockGithub.request).toHaveBeenCalledWith(expect.stringContaining("POST"), expect.objectContaining({ body: expect.stringContaining("🔒 This issue has been locked") }));
    });

    it("should not add lock notice for discussion events when GH_AW_LOCK_FOR_AGENT=true", async () => {
      process.env.GH_AW_LOCK_FOR_AGENT = "true";
      mockGithub.graphql
        .mockResolvedValueOnce({ repository: { discussion: { id: "D_kwDOABcD1M4AaBbC", url: "https://github.com/testowner/testrepo/discussions/10" } } })
        .mockResolvedValueOnce({ addDiscussionComment: { comment: { id: "DC_kwDOABcD1M4AaBbE", url: "https://github.com/testowner/testrepo/discussions/10#discussioncomment-999" } } });

      const { addCommentWithWorkflowLink } = await loadModule();
      await addCommentWithWorkflowLink("discussion:10", "https://github.com/testowner/testrepo/actions/runs/12345", "discussion");

      const graphqlCall = mockGithub.graphql.mock.calls.find(call => String(call[0]).includes("addDiscussionComment"));
      expect(graphqlCall).toBeDefined();
      expect(graphqlCall?.[1]?.body).not.toContain("🔒 This issue has been locked");
    });

    it("should not add lock notice for pull_request events", async () => {
      process.env.GH_AW_LOCK_FOR_AGENT = "true";
      mockGithub.request.mockResolvedValueOnce({ data: { id: 123, html_url: "https://example.com" } });

      const { addCommentWithWorkflowLink } = await loadModule();
      await addCommentWithWorkflowLink(
        { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "testowner", repo: "testrepo", issue_number: 123 } },
        "https://github.com/testowner/testrepo/actions/runs/12345",
        "pull_request"
      );

      expect(mockGithub.request).toHaveBeenCalledWith(expect.stringContaining("POST"), expect.objectContaining({ body: expect.not.stringContaining("🔒 This issue has been locked") }));
    });
  });

  describe("issue_comment event - missing issue number", () => {
    it("should fail when issue number is missing for issue_comment event", async () => {
      global.context = {
        ...global.context,
        eventName: "issue_comment",
        payload: {
          comment: { id: 789 },
          // No issue field
        },
      };

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(`${ERR_NOT_FOUND}: Issue number not found`));
    });
  });

  describe("pull_request_review_comment event - missing PR number", () => {
    it("should fail when PR number is missing for pull_request_review_comment event", async () => {
      global.context = {
        ...global.context,
        eventName: "pull_request_review_comment",
        payload: {
          comment: { id: 555 },
          // No pull_request field
        },
      };

      const { main } = await loadModule();
      await main();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(`${ERR_NOT_FOUND}: Pull request number not found`));
    });
  });

  describe("addCommentWithWorkflowLink() - error handling", () => {
    it("should warn (not fail) when comment creation throws", async () => {
      mockGithub.request.mockRejectedValueOnce(new Error("Network failure"));

      const { addCommentWithWorkflowLink } = await loadModule();
      await addCommentWithWorkflowLink(
        { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "testowner", repo: "testrepo", issue_number: 123 } },
        "https://github.com/testowner/testrepo/actions/runs/12345",
        "issues"
      );

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to create comment with workflow link"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("addReaction()", () => {
    it("should add reaction via REST API and set output", async () => {
      mockGithub.request.mockResolvedValueOnce({ data: { id: 789 } });

      const { addReaction } = await loadModule();
      await addReaction("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", { owner: "testowner", repo: "testrepo", issue_number: 123 }, "eyes");

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", expect.objectContaining({ content: "eyes", owner: "testowner", repo: "testrepo", issue_number: 123 }));
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "789");
    });

    it("should set empty reaction-id when response has no id", async () => {
      mockGithub.request.mockResolvedValueOnce({ data: {} });

      const { addReaction } = await loadModule();
      await addReaction("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", { owner: "testowner", repo: "testrepo", issue_number: 123 }, "eyes");

      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "");
    });
  });

  describe("VALID_REACTIONS", () => {
    it("should export the list of valid reaction types", async () => {
      const { VALID_REACTIONS } = await loadModule();
      expect(VALID_REACTIONS).toEqual(["+1", "-1", "laugh", "confused", "heart", "hooray", "rocket", "eyes"]);
    });
  });

  describe("resolveEventEndpoints()", () => {
    it("should resolve endpoints for issues event", async () => {
      const { resolveEventEndpoints } = await loadModule();
      const payload = { issue: { number: 42 } };
      const result = await resolveEventEndpoints("issues", "owner", "repo", payload);
      expect(result).toEqual({
        reactionEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", params: { owner: "owner", repo: "repo", issue_number: 42 } },
        commentUpdateEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "owner", repo: "repo", issue_number: 42 } },
      });
    });

    it("should return null and call setFailed when issue number is missing", async () => {
      const { resolveEventEndpoints } = await loadModule();
      const result = await resolveEventEndpoints("issues", "owner", "repo", {});
      expect(result).toBeNull();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(ERR_NOT_FOUND));
    });

    it("should resolve endpoints for pull_request event", async () => {
      const { resolveEventEndpoints } = await loadModule();
      const payload = { pull_request: { number: 7 } };
      const result = await resolveEventEndpoints("pull_request", "owner", "repo", payload);
      expect(result).toEqual({
        reactionEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", params: { owner: "owner", repo: "repo", issue_number: 7 } },
        commentUpdateEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "owner", repo: "repo", issue_number: 7 } },
      });
    });

    it("should resolve endpoints for issue_comment event", async () => {
      const { resolveEventEndpoints } = await loadModule();
      const payload = { comment: { id: 55 }, issue: { number: 10 } };
      const result = await resolveEventEndpoints("issue_comment", "owner", "repo", payload);
      expect(result).toEqual({
        reactionEndpoint: { route: "POST /repos/{owner}/{repo}/issues/comments/{comment_id}/reactions", params: { owner: "owner", repo: "repo", comment_id: 55 } },
        commentUpdateEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "owner", repo: "repo", issue_number: 10 } },
      });
    });

    it("should resolve endpoints for pull_request_review_comment event", async () => {
      const { resolveEventEndpoints } = await loadModule();
      const payload = { comment: { id: 99 }, pull_request: { number: 3 } };
      const result = await resolveEventEndpoints("pull_request_review_comment", "owner", "repo", payload);
      expect(result).toEqual({
        reactionEndpoint: { route: "POST /repos/{owner}/{repo}/pulls/comments/{comment_id}/reactions", params: { owner: "owner", repo: "repo", comment_id: 99 } },
        commentUpdateEndpoint: { route: "POST /repos/{owner}/{repo}/issues/{issue_number}/comments", params: { owner: "owner", repo: "repo", issue_number: 3 } },
      });
    });

    it("should resolve endpoints for discussion event using GraphQL node ID", async () => {
      mockGithub.graphql.mockResolvedValueOnce({ repository: { discussion: { id: "D_node123", url: "https://github.com/testowner/testrepo/discussions/5" } } });
      const { resolveEventEndpoints } = await loadModule();
      const payload = { discussion: { number: 5 } };
      const result = await resolveEventEndpoints("discussion", "owner", "repo", payload);
      expect(result).toEqual({
        reactionEndpoint: "D_node123",
        commentUpdateEndpoint: "discussion:5",
      });
    });

    it("should resolve endpoints for discussion_comment event", async () => {
      const { resolveEventEndpoints } = await loadModule();
      const payload = { discussion: { number: 5 }, comment: { id: 88, node_id: "DC_node88" } };
      const result = await resolveEventEndpoints("discussion_comment", "owner", "repo", payload);
      expect(result).toEqual({
        reactionEndpoint: "DC_node88",
        commentUpdateEndpoint: "discussion_comment:5:88",
      });
    });

    it("should return null and call setFailed for unknown event type", async () => {
      const { resolveEventEndpoints } = await loadModule();
      const result = await resolveEventEndpoints("push", "owner", "repo", {});
      expect(result).toBeNull();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(ERR_VALIDATION));
    });
  });

  describe("requireEventField()", () => {
    it("should return true and not call setFailed for a valid value", async () => {
      const { requireEventField } = await loadModule();
      const result = requireEventField(42, "Issue number", ERR_NOT_FOUND);
      expect(result).toBe(true);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should return true for the number 0", async () => {
      const { requireEventField } = await loadModule();
      const result = requireEventField(0, "Issue number", ERR_NOT_FOUND);
      expect(result).toBe(true);
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should call setFailed and return false for null", async () => {
      const { requireEventField } = await loadModule();
      const result = requireEventField(null, "Issue number", ERR_NOT_FOUND);
      expect(result).toBe(false);
      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Issue number not found in event payload`);
    });

    it("should call setFailed and return false for undefined", async () => {
      const { requireEventField } = await loadModule();
      const result = requireEventField(undefined, "Comment ID", ERR_VALIDATION);
      expect(result).toBe(false);
      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_VALIDATION}: Comment ID not found in event payload`);
    });
  });
});
