// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";

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

  describe("issue events", () => {
    it("should add reaction to an issue", async () => {
      global.context = {
        eventName: "issues",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { issue: { number: 456 } },
      };

      await runScript();

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/testowner/testrepo/issues/456/reactions", expect.objectContaining({ content: "eyes" }));
      expect(mockCore.setOutput).toHaveBeenCalledWith("reaction-id", "12345");
    });

    it("should fail when issue number is missing", async () => {
      global.context = {
        eventName: "issues",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith("Issue number not found in event payload");
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

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/testowner/testrepo/issues/comments/789/reactions", expect.objectContaining({ content: "eyes" }));
    });

    it("should fail when comment ID is missing", async () => {
      global.context = {
        eventName: "issue_comment",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith("Comment ID not found in event payload");
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

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/testowner/testrepo/issues/999/reactions", expect.objectContaining({ content: "eyes" }));
    });

    it("should fail when PR number is missing", async () => {
      global.context = {
        eventName: "pull_request",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith("Pull request number not found in event payload");
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

      expect(mockGithub.request).toHaveBeenCalledWith("POST /repos/testowner/testrepo/pulls/comments/555/reactions", expect.objectContaining({ content: "eyes" }));
    });

    it("should fail when review comment ID is missing", async () => {
      global.context = {
        eventName: "pull_request_review_comment",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith("Review comment ID not found in event payload");
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

      expect(mockCore.setFailed).toHaveBeenCalledWith("Discussion number not found in event payload");
    });

    it("should handle discussion not found error", async () => {
      global.context = {
        eventName: "discussion",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: { discussion: { number: 999 } },
      };

      mockGithub.graphql.mockResolvedValueOnce({ repository: null });

      await runScript();

      expect(mockCore.error).toHaveBeenCalled();
      expect(mockCore.setFailed).toHaveBeenCalled();
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

      expect(mockCore.setFailed).toHaveBeenCalledWith("Discussion comment node ID not found in event payload");
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
    it("should fail for unsupported event types", async () => {
      global.context = {
        eventName: "push",
        repo: { owner: "testowner", repo: "testrepo" },
        payload: {},
      };

      await runScript();

      expect(mockCore.setFailed).toHaveBeenCalledWith("Unsupported event type: push");
      expect(mockGithub.request).not.toHaveBeenCalled();
    });
  });

  describe("error handling", () => {
    it("should handle API errors gracefully", async () => {
      mockGithub.request.mockRejectedValueOnce(new Error("API Error"));

      await runScript();

      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Failed to add reaction"));
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

      expect(mockCore.error).toHaveBeenCalled();
      expect(mockCore.setFailed).toHaveBeenCalled();
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
  });
});
