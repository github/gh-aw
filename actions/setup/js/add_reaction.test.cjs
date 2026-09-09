// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";
const { ERR_NOT_FOUND, ERR_VALIDATION, ERR_API } = require("./error_codes.cjs");

// Mock the global objects that GitHub Actions provides
const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};

const mockGithub = {
  request: vi.fn(),
  graphql: vi.fn(),
};

const mockContext = {
  eventName: "issues",
  repo: {
    owner: "testowner",
    repo: "testrepo",
  },
  payload: {
    issue: {
      number: 123,
    },
  },
};

// Set up global mocks before importing the module
global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

describe("add_reaction", () => {
  beforeEach(() => {
    // Reset all mocks before each test
    vi.clearAllMocks();
    vi.resetModules();

    // Reset environment variables
    delete process.env.GH_AW_REACTION;

    // Reset context to default
    global.context = {
      eventName: "issues",
      repo: {
        owner: "testowner",
        repo: "testrepo",
      },
      payload: {
        issue: {
          number: 123,
        },
      },
    };

    // Reset default mock implementations
    mockGithub.request.mockResolvedValue({
      data: { id: 12345 },
    });

    mockGithub.graphql.mockResolvedValue({
      addReaction: {
        reaction: {
          id: "R_67890",
          content: "EYES",
        },
      },
    });
  });

  // Helper function to run the script
  async function runScript() {
    const { main } = await import("./add_reaction.cjs?" + Date.now());
    await main();
  }

  // Helper to import module helpers directly
  async function importHelpers() {
    return import("./add_reaction.cjs?" + Date.now());
  }

  describe("reaction validation", () => {
    it("should use 'eyes' as default reaction when GH_AW_REACTION is not set", async () => {
      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(expect.stringContaining("POST"), expect.objectContaining({ content: "eyes" }));
    });

    it("should use reaction from GH_AW_REACTION environment variable", async () => {
      process.env.GH_AW_REACTION = "rocket";

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith(expect.stringContaining("POST"), expect.objectContaining({ content: "rocket" }));
    });

    it("should fail for invalid reaction type", async () => {
      process.env.GH_AW_REACTION = "invalid";

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Invalid reaction type"));
      expect(mockGithub.request).not.toHaveBeenCalled();
    });

    it("should reject inherited property names as invalid reaction types", async () => {
      process.env.GH_AW_REACTION = "constructor";

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Invalid reaction type"));
      expect(mockGithub.request).not.toHaveBeenCalled();
    });

    it("should accept all valid reaction types", async () => {
      const validReactions = ["+1", "-1", "laugh", "confused", "heart", "hooray", "rocket", "eyes"];

      for (const reaction of validReactions) {
        vi.clearAllMocks();
        process.env.GH_AW_REACTION = reaction;

        await runScript();

        expect(mockCore.setFailed).not.toHaveBeenCalled();
        expect(mockGithub.request).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ content: reaction }));
      }
    });
  });

  describe("REACTION_MAP", () => {
    it("should export REACTION_MAP with all 8 reaction types", async () => {
      const { REACTION_MAP } = await importHelpers();
      expect(Object.keys(REACTION_MAP)).toHaveLength(8);
    });

    it("should map all reactions to correct GraphQL enum values", async () => {
      const { REACTION_MAP } = await importHelpers();
      expect(REACTION_MAP["+1"]).toBe("THUMBS_UP");
      expect(REACTION_MAP["-1"]).toBe("THUMBS_DOWN");
      expect(REACTION_MAP["laugh"]).toBe("LAUGH");
      expect(REACTION_MAP["confused"]).toBe("CONFUSED");
      expect(REACTION_MAP["heart"]).toBe("HEART");
      expect(REACTION_MAP["hooray"]).toBe("HOORAY");
      expect(REACTION_MAP["rocket"]).toBe("ROCKET");
      expect(REACTION_MAP["eyes"]).toBe("EYES");
    });
  });

  describe("issue events", () => {
    it("should add reaction to an issue", async () => {
      global.context = {
        eventName: "issues",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { issue: { number: 456 } },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", expect.objectContaining({ content: "eyes", owner: "testowner", repo: "testrepo", issue_number: 456 }));
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "12345");
    });

    it("should fail when issue number is missing", async () => {
      global.context = {
        eventName: "issues",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Issue number not found in event payload`);
      expect(mockCore.setFailed).toHaveBeenCalledTimes(1);
      expect(mockGithub.request).not.toHaveBeenCalled();
    });
  });

  describe("issue_comment events", () => {
    it("should add reaction to an issue comment", async () => {
      global.context = {
        eventName: "issue_comment",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { comment: { id: 789 } },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/comments/{comment_id}/reactions", expect.objectContaining({ content: "eyes", owner: "testowner", repo: "testrepo", comment_id: 789 }));
    });

    it("should fail when comment ID is missing", async () => {
      global.context = {
        eventName: "issue_comment",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_VALIDATION}: Comment ID not found in event payload`);
    });
  });

  describe("pull_request events", () => {
    it("should add reaction to a pull request", async () => {
      global.context = {
        eventName: "pull_request",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { pull_request: { number: 999 } },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", expect.objectContaining({ content: "eyes", owner: "testowner", repo: "testrepo", issue_number: 999 }));
    });

    it("should fail when PR number is missing", async () => {
      global.context = {
        eventName: "pull_request",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Pull request number not found in event payload`);
    });
  });

  describe("pull_request_review_comment events", () => {
    it("should add reaction to a PR review comment", async () => {
      global.context = {
        eventName: "pull_request_review_comment",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { comment: { id: 555 } },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/pulls/comments/{comment_id}/reactions", expect.objectContaining({ content: "eyes", owner: "testowner", repo: "testrepo", comment_id: 555 }));
    });

    it("should fail when review comment ID is missing", async () => {
      global.context = {
        eventName: "pull_request_review_comment",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_VALIDATION}: Review comment ID not found in event payload`);
    });
  });

  describe("pull_request_review events", () => {
    it("should silently skip pull_request_review events", async () => {
      global.context = {
        eventName: "pull_request_review",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { pull_request: { number: 999 }, review: { id: 321 } },
      };

      await runScript();

      expect(mockGithub.request).not.toHaveBeenCalled();
      expect(mockGithub.graphql).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("discussion events", () => {
    beforeEach(() => {
      mockGithub.graphql.mockImplementation(query => {
        if (query.includes("query")) {
          return Promise.resolve({
            repository: {
              discussion: {
                id: "D_kwDOABCD1234",
                url: "https://github.com/testowner/testrepo/discussions/100",
              },
            },
          });
        }
        return Promise.resolve({
          addReaction: {
            reaction: {
              id: "R_67890",
              content: "EYES",
            },
          },
        });
      });
    });

    it("should add reaction to a discussion using GraphQL", async () => {
      global.context = {
        eventName: "discussion",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { discussion: { number: 100 } },
      };

      await runScript();

      expect(mockGithub.graphql).toHaveBeenCalledTimes(2); // Query + Mutation
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "R_67890");
    });

    it("should fail when discussion number is missing", async () => {
      global.context = {
        eventName: "discussion",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Discussion number not found in event payload`);
    });

    it("should handle discussion not found error when repository is null", async () => {
      global.context = {
        eventName: "discussion",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { discussion: { number: 999 } },
      };

      mockGithub.graphql.mockResolvedValueOnce({ repository: null });

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalled();
    });

    it("should handle discussion not found error when discussion is null", async () => {
      global.context = {
        eventName: "discussion",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { discussion: { number: 999 } },
      };

      mockGithub.graphql.mockResolvedValueOnce({ repository: { discussion: null } });

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("not found"));
    });

    it("should silently ignore locked discussion errors", async () => {
      global.context = {
        eventName: "discussion",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { discussion: { number: 100 } },
      };

      const lockedError = new Error("Issue is locked");
      lockedError.status = 403;
      // First call succeeds (getDiscussionNodeId query), second throws (addDiscussionReaction mutation)
      mockGithub.graphql.mockResolvedValueOnce({ repository: { discussion: { id: "D_kwDOABCD1234" } } }).mockRejectedValueOnce(lockedError);

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("resource is locked"));
      expect(mockCore.error).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("discussion_comment events", () => {
    it("should add reaction to a discussion comment using GraphQL", async () => {
      global.context = {
        eventName: "discussion_comment",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { comment: { node_id: "DC_kwDOABCD5678" } },
      };

      process.env.GH_AW_REACTION = "heart";

      await runScript();

      expect(mockGithub.graphql).toHaveBeenCalledWith(
        expect.stringContaining("mutation"),
        expect.objectContaining({
          subjectId: "DC_kwDOABCD5678",
          content: "HEART",
        })
      );
    });

    it("should fail when discussion comment node_id is missing", async () => {
      global.context = {
        eventName: "discussion_comment",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_NOT_FOUND}: Discussion comment node ID not found in event payload`);
    });

    it("should silently ignore locked discussion comment errors", async () => {
      global.context = {
        eventName: "discussion_comment",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { comment: { node_id: "DC_kwDOABCD5678" } },
      };

      const lockedError = new Error("Issue is locked");
      lockedError.status = 403;
      mockGithub.graphql.mockRejectedValueOnce(lockedError);

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("resource is locked"));
      expect(mockCore.error).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("reaction mapping for GraphQL", () => {
    it("should map all valid reactions to GraphQL enum values", async () => {
      const reactionMapping = {
        "+1": "THUMBS_UP",
        "-1": "THUMBS_DOWN",
        laugh: "LAUGH",
        confused: "CONFUSED",
        heart: "HEART",
        hooray: "HOORAY",
        rocket: "ROCKET",
        eyes: "EYES",
      };

      for (const [reaction, graphqlValue] of Object.entries(reactionMapping)) {
        vi.clearAllMocks();
        global.context = {
          eventName: "discussion_comment",
          repo: { owner: "testowner", repo: "testrepo" },
          payload: { comment: { node_id: "DC_test" } },
        };
        process.env.GH_AW_REACTION = reaction;

        await runScript();

        expect(mockGithub.graphql).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({ content: graphqlValue }));
      }
    });
  });

  describe("unsupported events", () => {
    it("supports workflow_dispatch with aw_context metadata", async () => {
      global.context = {
        eventName: "workflow_dispatch",
        repo: { owner: "workflow-owner", repo: "workflow-repo" },
        payload: {
          inputs: {
            aw_context: JSON.stringify({
              event_type: "issue_comment",
              item_type: "issue",
              item_number: "123",
              comment_id: "456",
              repo: "target-owner/target-repo",
            }),
          },
        },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/comments/{comment_id}/reactions", expect.objectContaining({ content: "eyes", owner: "target-owner", repo: "target-repo", comment_id: 456 }));
    });

    it("should fail for unsupported event types", async () => {
      global.context = {
        eventName: "push",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(`${ERR_VALIDATION}: Unsupported event type: push`);
      expect(mockGithub.request).not.toHaveBeenCalled();
    });
  });

  describe("error handling", () => {
    it("should handle API errors gracefully", async () => {
      mockGithub.request.mockRejectedValueOnce(new Error("API Error"));

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to add reaction"));
    });

    it("should handle GraphQL errors gracefully", async () => {
      global.context = {
        eventName: "discussion_comment",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { comment: { node_id: "DC_test" } },
      };

      mockGithub.graphql.mockRejectedValueOnce(new Error("GraphQL Error"));

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalled();
    });

    it("should silently ignore locked issue errors (status 403)", async () => {
      const lockedError = new Error("Issue is locked");
      lockedError.status = 403;
      mockGithub.request.mockRejectedValueOnce(lockedError);

      await runScript();

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("resource is locked"));
      expect(mockCore.error).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail for errors with 'locked' message but non-403 status", async () => {
      // Errors mentioning "locked" should only be ignored if they have 403 status
      const lockedError = new Error("Lock conversation is enabled");
      lockedError.status = 500; // Not 403
      mockGithub.request.mockRejectedValueOnce(lockedError);

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to add reaction"));
    });

    it("should fail for 403 errors that don't mention locked", async () => {
      const forbiddenError = new Error("Forbidden: insufficient permissions");
      forbiddenError.status = 403;
      mockGithub.request.mockRejectedValueOnce(forbiddenError);

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to add reaction"));
    });

    it("should warn and continue for rate limit 403 errors", async () => {
      const rateLimitError = new Error("API rate limit exceeded for installation");
      /** @type {any} */ rateLimitError.status = 403;
      mockGithub.request.mockRejectedValueOnce(rateLimitError);

      await runScript();

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("GitHub API rate limiting"));
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should fail for other non-403 errors", async () => {
      const serverError = new Error("Internal server error");
      serverError.status = 500;
      mockGithub.request.mockRejectedValueOnce(serverError);

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Failed to add reaction"));
    });
  });

  describe("output handling", () => {
    it("should set reaction-id output when API returns ID", async () => {
      mockGithub.request.mockResolvedValueOnce({
        data: { id: 99999 },
      });

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "99999");
    });

    it("should set empty reaction-id when API doesn't return ID", async () => {
      mockGithub.request.mockResolvedValueOnce({
        data: {},
      });

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "");
    });

    it("should stringify numeric reaction-id to string", async () => {
      mockGithub.request.mockResolvedValueOnce({
        data: { id: 42 },
      });

      await runScript();

      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "42");
    });
  });

  describe("addReaction (direct)", () => {
    it("should call github.request with correct endpoint and content", async () => {
      const { addReaction } = await importHelpers();
      mockGithub.request.mockResolvedValueOnce({ data: { id: 111 } });

      await addReaction("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", { owner: "owner", repo: "repo", issue_number: 1 }, "+1");

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", {
        owner: "owner",
        repo: "repo",
        issue_number: 1,
        content: "+1",
        headers: { Accept: "application/vnd.github+json" },
      });
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "111");
    });

    it("should set empty reaction-id when response has no id", async () => {
      const { addReaction } = await importHelpers();
      mockGithub.request.mockResolvedValueOnce({ data: {} });

      await addReaction("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", { owner: "owner", repo: "repo", issue_number: 1 }, "eyes");

      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "");
    });

    it("should log success message with reaction id", async () => {
      const { addReaction } = await importHelpers();
      mockGithub.request.mockResolvedValueOnce({ data: { id: 777 } });

      await addReaction("POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", { owner: "owner", repo: "repo", issue_number: 1 }, "rocket");

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("rocket"));
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("777"));
    });
  });

  describe("addDiscussionReaction (direct)", () => {
    it("should call github.graphql with correct mutation and content", async () => {
      const { addDiscussionReaction } = await importHelpers();
      mockGithub.graphql.mockResolvedValueOnce({
        addReaction: { reaction: { id: "R_abc123", content: "THUMBS_UP" } },
      });

      await addDiscussionReaction("D_nodeId", "+1");

      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("addReaction"), { subjectId: "D_nodeId", content: "THUMBS_UP" });
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "R_abc123");
    });

    it("should throw for unknown reaction type", async () => {
      const { addDiscussionReaction } = await importHelpers();

      await expect(addDiscussionReaction("D_nodeId", "unknown_reaction")).rejects.toThrow("Invalid reaction type for GraphQL");
    });
  });

  describe("getDiscussionNodeId (direct)", () => {
    it("should return node ID for a valid discussion", async () => {
      const { getDiscussionNodeId } = await importHelpers();
      mockGithub.graphql.mockResolvedValueOnce({
        repository: { discussion: { id: "D_node123" } },
      });

      const id = await getDiscussionNodeId("owner", "repo", 42);

      expect(id).toBe("D_node123");
      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("discussion(number"), { owner: "owner", repo: "repo", num: 42 });
    });

    it("should throw ERR_NOT_FOUND when repository is null", async () => {
      const { getDiscussionNodeId } = await importHelpers();
      mockGithub.graphql.mockResolvedValueOnce({ repository: null });

      await expect(getDiscussionNodeId("owner", "repo", 99)).rejects.toThrow(ERR_NOT_FOUND);
    });

    it("should throw ERR_NOT_FOUND when discussion is null", async () => {
      const { getDiscussionNodeId } = await importHelpers();
      mockGithub.graphql.mockResolvedValueOnce({ repository: { discussion: null } });

      await expect(getDiscussionNodeId("owner", "repo", 99)).rejects.toThrow(ERR_NOT_FOUND);
    });
  });

  describe("handleReactionError (direct)", () => {
    it("should silently ignore locked errors (status 403 + locked message)", async () => {
      const { handleReactionError } = await importHelpers();
      const lockedError = new Error("Issue is locked");
      lockedError.status = 403;

      handleReactionError(lockedError);

      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("resource is locked"));
      expect(mockCore.error).not.toHaveBeenCalled();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should call core.setFailed for non-locked errors", async () => {
      const { handleReactionError } = await importHelpers();

      handleReactionError(new Error("Some API error"));

      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining(`${ERR_API}: Failed to add reaction`));
    });
  });

  describe("resolveRestEndpoint (direct)", () => {
    it("should return issues endpoint", async () => {
      global.context = { eventName: "issues", repo: { owner: "o", repo: "r" }, payload: { issue: { number: 1 } } };
      const { resolveRestEndpoint } = await importHelpers();
      expect(resolveRestEndpoint("issues", "o", "r", global.context.payload)).toEqual({ route: "POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", params: { owner: "o", repo: "r", issue_number: 1 } });
    });

    it("should return null and setFailed when issue number is missing", async () => {
      global.context = { eventName: "issues", repo: { owner: "o", repo: "r" }, payload: {} };
      const { resolveRestEndpoint } = await importHelpers();
      expect(resolveRestEndpoint("issues", "o", "r", global.context.payload)).toBeNull();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Issue number not found"));
    });

    it("should return issue_comment endpoint", async () => {
      global.context = { eventName: "issue_comment", repo: { owner: "o", repo: "r" }, payload: { comment: { id: 42 } } };
      const { resolveRestEndpoint } = await importHelpers();
      expect(resolveRestEndpoint("issue_comment", "o", "r", global.context.payload)).toEqual({ route: "POST /repos/{owner}/{repo}/issues/comments/{comment_id}/reactions", params: { owner: "o", repo: "r", comment_id: 42 } });
    });

    it("should return null and setFailed when comment id is missing", async () => {
      global.context = { eventName: "issue_comment", repo: { owner: "o", repo: "r" }, payload: {} };
      const { resolveRestEndpoint } = await importHelpers();
      expect(resolveRestEndpoint("issue_comment", "o", "r", global.context.payload)).toBeNull();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Comment ID not found"));
    });

    it("should return pull_request endpoint using issues path", async () => {
      global.context = { eventName: "pull_request", repo: { owner: "o", repo: "r" }, payload: { pull_request: { number: 7 } } };
      const { resolveRestEndpoint } = await importHelpers();
      expect(resolveRestEndpoint("pull_request", "o", "r", global.context.payload)).toEqual({ route: "POST /repos/{owner}/{repo}/issues/{issue_number}/reactions", params: { owner: "o", repo: "r", issue_number: 7 } });
    });

    it("should return null and setFailed when PR number is missing", async () => {
      global.context = { eventName: "pull_request", repo: { owner: "o", repo: "r" }, payload: {} };
      const { resolveRestEndpoint } = await importHelpers();
      expect(resolveRestEndpoint("pull_request", "o", "r", global.context.payload)).toBeNull();
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Pull request number not found"));
    });

    it("should return pull_request_review_comment endpoint", async () => {
      global.context = { eventName: "pull_request_review_comment", repo: { owner: "o", repo: "r" }, payload: { comment: { id: 55 } } };
      const { resolveRestEndpoint } = await importHelpers();
      expect(resolveRestEndpoint("pull_request_review_comment", "o", "r", global.context.payload)).toEqual({ route: "POST /repos/{owner}/{repo}/pulls/comments/{comment_id}/reactions", params: { owner: "o", repo: "r", comment_id: 55 } });
    });

    it("should return null without failure for pull_request_review events", async () => {
      global.context = { eventName: "pull_request_review", repo: { owner: "o", repo: "r" }, payload: { pull_request: { number: 7 }, review: { id: 88 } } };
      const { resolveRestEndpoint } = await importHelpers();
      expect(resolveRestEndpoint("pull_request_review", "o", "r", global.context.payload)).toBeNull();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should return null without failure for pull_request_review even with empty payload", async () => {
      const { resolveRestEndpoint } = await importHelpers();
      expect(resolveRestEndpoint("pull_request_review", "o", "r", {})).toBeNull();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
      expect(mockCore.error).not.toHaveBeenCalled();
    });

    it("should return null for discussion events (handled via GraphQL)", async () => {
      global.context = { eventName: "discussion", repo: { owner: "o", repo: "r" }, payload: { discussion: { number: 3 } } };
      const { resolveRestEndpoint } = await importHelpers();
      expect(resolveRestEndpoint("discussion", "o", "r", global.context.payload)).toBeNull();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });

    it("should return null for unknown events", async () => {
      global.context = { eventName: "push", repo: { owner: "o", repo: "r" }, payload: {} };
      const { resolveRestEndpoint } = await importHelpers();
      expect(resolveRestEndpoint("push", "o", "r", global.context.payload)).toBeNull();
      expect(mockCore.setFailed).not.toHaveBeenCalled();
    });
  });

  describe("handleGraphQLOrUnknownEvent (direct)", () => {
    beforeEach(() => {
      mockGithub.graphql.mockImplementation(query => {
        if (query.includes("query")) {
          return Promise.resolve({ repository: { discussion: { id: "D_abc" } } });
        }
        return Promise.resolve({ addReaction: { reaction: { id: "R_xyz", content: "EYES" } } });
      });
    });

    it("should handle discussion event with GraphQL", async () => {
      global.context = { eventName: "discussion", repo: { owner: "o", repo: "r" }, payload: { discussion: { number: 5 } } };
      const { handleGraphQLOrUnknownEvent } = await importHelpers();
      await handleGraphQLOrUnknownEvent("discussion", "o", "r", global.context.payload, "eyes");
      expect(mockGithub.graphql).toHaveBeenCalledTimes(2);
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "R_xyz");
    });

    it("should setFailed when discussion number missing", async () => {
      global.context = { eventName: "discussion", repo: { owner: "o", repo: "r" }, payload: {} };
      const { handleGraphQLOrUnknownEvent } = await importHelpers();
      await handleGraphQLOrUnknownEvent("discussion", "o", "r", global.context.payload, "eyes");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Discussion number not found"));
    });

    it("should handle discussion_comment event with GraphQL", async () => {
      global.context = { eventName: "discussion_comment", repo: { owner: "o", repo: "r" }, payload: { comment: { node_id: "DC_abc" } } };
      const { handleGraphQLOrUnknownEvent } = await importHelpers();
      await handleGraphQLOrUnknownEvent("discussion_comment", "o", "r", global.context.payload, "heart");
      expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("mutation"), expect.objectContaining({ content: "HEART" }));
    });

    it("should setFailed when discussion_comment node_id missing", async () => {
      global.context = { eventName: "discussion_comment", repo: { owner: "o", repo: "r" }, payload: {} };
      const { handleGraphQLOrUnknownEvent } = await importHelpers();
      await handleGraphQLOrUnknownEvent("discussion_comment", "o", "r", global.context.payload, "eyes");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Discussion comment node ID not found"));
    });

    it("should setFailed for unknown event types", async () => {
      global.context = { eventName: "push", repo: { owner: "o", repo: "r" }, payload: {} };
      const { handleGraphQLOrUnknownEvent } = await importHelpers();
      await handleGraphQLOrUnknownEvent("push", "o", "r", global.context.payload, "eyes");
      expect(mockCore.setFailed).toHaveBeenCalledWith(expect.stringContaining("Unsupported event type: push"));
    });
  });
});
