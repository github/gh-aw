import { describe, it, expect, beforeEach, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);

// Mock the global objects that GitHub Actions provides
const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};

const mockGithub = {
  graphql: vi.fn(),
};

// Set up global mocks
global.core = mockCore;
global.github = mockGithub;

describe("close_older_discussions.cjs", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.resetModules();
    // Clear environment variables
    delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
    delete process.env.GH_AW_WORKFLOW_NAME;
    delete process.env.GITHUB_SERVER_URL;
  });

  describe("searchOlderDiscussions", () => {
    it("should return empty array when no discussions found", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [],
        },
      });

      const result = await searchOlderDiscussions(mockGithub, "testowner", "testrepo", "test-workflow", "DIC_test123", 1);

      expect(result).toEqual([]);
    });

    it("should filter out the newly created discussion", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [
            {
              id: "D_old1",
              number: 5,
              title: "Weekly Report - 2024-01",
              url: "https://github.com/testowner/testrepo/discussions/5",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
            {
              id: "D_new",
              number: 10, // This is the new discussion, should be excluded
              title: "Weekly Report - 2024-02",
              url: "https://github.com/testowner/testrepo/discussions/10",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
          ],
        },
      });

      const result = await searchOlderDiscussions(
        mockGithub,
        "testowner",
        "testrepo",
        "test-workflow",
        "DIC_test123",
        10 // Exclude discussion #10
      );

      expect(result).toHaveLength(1);
      expect(result[0].number).toBe(5);
    });

    it("should filter out already closed discussions", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [
            {
              id: "D_open",
              number: 5,
              title: "Open Report",
              url: "https://github.com/testowner/testrepo/discussions/5",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
            {
              id: "D_closed",
              number: 6,
              title: "Closed Report",
              url: "https://github.com/testowner/testrepo/discussions/6",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_test123" },
              closed: true, // Already closed
            },
          ],
        },
      });

      const result = await searchOlderDiscussions(mockGithub, "testowner", "testrepo", "test-workflow", "DIC_test123", 10);

      expect(result).toHaveLength(1);
      expect(result[0].number).toBe(5);
    });

    it("should search for discussions with workflow ID marker", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [
            {
              id: "D_match",
              number: 5,
              title: "Matching Report",
              url: "https://github.com/testowner/testrepo/discussions/5",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
          ],
        },
      });

      const result = await searchOlderDiscussions(mockGithub, "testowner", "testrepo", "test-workflow", "DIC_test123", 10);

      expect(result).toHaveLength(1);
      expect(result[0].number).toBe(5);
    });

    it("should return empty array if no workflow ID provided", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      const result = await searchOlderDiscussions(mockGithub, "testowner", "testrepo", "", "DIC_test123", 10);

      expect(result).toHaveLength(0);
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });

    it("should filter by category when specified", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [
            {
              id: "D_rightcat",
              number: 5,
              title: "Report in right category",
              url: "https://github.com/testowner/testrepo/discussions/5",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_reports" },
              closed: false,
            },
            {
              id: "D_wrongcat",
              number: 6,
              title: "Report in wrong category",
              url: "https://github.com/testowner/testrepo/discussions/6",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_general" },
              closed: false,
            },
          ],
        },
      });

      const result = await searchOlderDiscussions(
        mockGithub,
        "testowner",
        "testrepo",
        "test-workflow",
        "DIC_reports", // Filter by specific category
        10
      );

      expect(result).toHaveLength(1);
      expect(result[0].number).toBe(5);
    });

    it("should include all categories when categoryId is undefined", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [
            {
              id: "D_cat1",
              number: 5,
              title: "Report 1",
              url: "https://github.com/testowner/testrepo/discussions/5",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_reports" },
              closed: false,
            },
            {
              id: "D_cat2",
              number: 6,
              title: "Report 2",
              url: "https://github.com/testowner/testrepo/discussions/6",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_general" },
              closed: false,
            },
          ],
        },
      });

      const result = await searchOlderDiscussions(
        mockGithub,
        "testowner",
        "testrepo",
        "test-workflow",
        undefined, // No category filter
        10
      );

      expect(result).toHaveLength(2);
    });

    it("should exclude discussions whose body does not contain the exact marker", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [
            {
              id: "D_exact",
              number: 5,
              title: "Exact match",
              url: "https://github.com/testowner/testrepo/discussions/5",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
            {
              id: "D_substr",
              number: 6,
              title: "Substring match - should be excluded",
              url: "https://github.com/testowner/testrepo/discussions/6",
              // Related-but-longer workflow ID - GitHub search may match this
              // but exact filtering should exclude it
              body: "<!-- gh-aw-workflow-id: test-workflow-extended -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
            {
              id: "D_nomarker",
              number: 7,
              title: "No marker - should be excluded",
              url: "https://github.com/testowner/testrepo/discussions/7",
              body: "Discussion without any marker",
              category: { id: "DIC_test123" },
              closed: false,
            },
          ],
        },
      });

      const result = await searchOlderDiscussions(mockGithub, "testowner", "testrepo", "test-workflow", "DIC_test123", 99);

      expect(result).toHaveLength(1);
      expect(result[0].number).toBe(5);
    });

    it("should filter by gh-aw-workflow-call-id when callerWorkflowId is provided", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [
            {
              id: "D_same_caller",
              number: 5,
              title: "Same caller - should be included",
              url: "https://github.com/testowner/testrepo/discussions/5",
              body: "<!-- gh-aw-workflow-id: my-reusable-workflow -->\n<!-- gh-aw-workflow-call-id: owner/repo/CallerA -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
            {
              id: "D_diff_caller",
              number: 6,
              title: "Different caller - should be excluded",
              url: "https://github.com/testowner/testrepo/discussions/6",
              body: "<!-- gh-aw-workflow-id: my-reusable-workflow -->\n<!-- gh-aw-workflow-call-id: owner/repo/CallerB -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
            {
              id: "D_old",
              number: 7,
              title: "Old discussion without call-id - should be excluded",
              url: "https://github.com/testowner/testrepo/discussions/7",
              body: "<!-- gh-aw-workflow-id: my-reusable-workflow -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
          ],
        },
      });

      const result = await searchOlderDiscussions(mockGithub, "testowner", "testrepo", "my-reusable-workflow", "DIC_test123", 99, "owner/repo/CallerA");

      expect(result).toHaveLength(1);
      expect(result[0].number).toBe(5);
    });

    it("should use close-key marker as primary search term when closeOlderKey is provided", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [
            {
              id: "D_with_key",
              number: 5,
              title: "Has close-key marker - should be included",
              url: "https://github.com/testowner/testrepo/discussions/5",
              body: "<!-- gh-aw-workflow-id: some-workflow -->\n<!-- gh-aw-close-key: my-stable-key -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
            {
              id: "D_no_key",
              number: 6,
              title: "Missing close-key marker - should be excluded",
              url: "https://github.com/testowner/testrepo/discussions/6",
              body: "<!-- gh-aw-workflow-id: some-workflow -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
          ],
        },
      });

      const result = await searchOlderDiscussions(mockGithub, "testowner", "testrepo", "some-workflow", "DIC_test123", 99, undefined, "my-stable-key");

      expect(result).toHaveLength(1);
      expect(result[0].number).toBe(5);
      // Should search by the close-key marker, not the workflow-id marker
      expect(mockGithub.graphql).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          searchTerms: expect.stringContaining("gh-aw-close-key: my-stable-key"),
        })
      );
    });

    it("should work with close-key when workflowId is empty", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [
            {
              id: "D_with_key",
              number: 5,
              title: "Has close-key marker",
              url: "https://github.com/testowner/testrepo/discussions/5",
              body: "<!-- gh-aw-close-key: team-report -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
          ],
        },
      });

      const result = await searchOlderDiscussions(mockGithub, "testowner", "testrepo", "", "DIC_test123", 99, undefined, "team-report");

      expect(result).toHaveLength(1);
      expect(result[0].number).toBe(5);
      expect(mockGithub.graphql).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          searchTerms: expect.stringContaining("gh-aw-close-key: team-report"),
        })
      );
    });

    it("should return empty array when neither workflowId nor closeOlderKey is provided", async () => {
      const { searchOlderDiscussions } = await import("./close_older_discussions.cjs");

      const result = await searchOlderDiscussions(mockGithub, "testowner", "testrepo", "", "DIC_test123", 99, undefined, undefined);

      expect(result).toHaveLength(0);
      expect(mockGithub.graphql).not.toHaveBeenCalled();
    });
  });

  describe("cleanup delivery receipts", () => {
    it.each(["comment", "close"])("should capture the exact %s receipt under an active Claim", async type => {
      const { addDiscussionComment, closeDiscussionAsOutdated } = await import("./close_older_discussions.cjs");
      const { withClaimExecution, currentClaimHandle } = require("./work_queue_claim_scope.cjs");
      const { graphEffectIdentity } = require("./work_queue_effect_client.cjs");
      const discussionId = "D_old";
      const entity = { id: type === "comment" ? "DC_comment" : discussionId, url: "https://github.com/testowner/testrepo/discussions/5" };
      const response = type === "comment" ? { addDiscussionComment: { comment: entity } } : { closeDiscussion: { discussion: entity } };
      const receipts = [];
      const assignment = {
        version: 3,
        dispatch_id: "dispatch-1",
        request_id: "request-1",
        commit_id: "commit-1",
        policy_epoch: "epoch-1",
        pool: "pool-1",
        worker_profile: "worker-1",
        claims: [{ handle: "handle-1", claim_id: "claim-1", work_id: "work-1", work: {}, result_refs: [] }],
      };
      mockGithub.graphql.mockResolvedValueOnce(response);

      const result = await withClaimExecution({ assignment, claim_handle: "handle-1" }, () => {
        expect(currentClaimHandle()).toBe("handle-1");
        return type === "comment" ? addDiscussionComment(mockGithub, "testowner", "testrepo", discussionId, "Superseded", receipts) : closeDiscussionAsOutdated(mockGithub, "testowner", "testrepo", discussionId, receipts);
      });

      const [query, variables] = mockGithub.graphql.mock.calls[0];
      expect(result).toEqual(entity);
      expect(receipts).toEqual([
        {
          type,
          discussionId,
          id: entity.id,
          ...(type === "comment" ? { body: variables.body } : {}),
          effectId: graphEffectIdentity(query, variables, response),
        },
      ]);
      expect(receipts[0].effectId).toEqual(expect.any(String));
      expect(receipts[0].effectId.length).toBeGreaterThan(0);
      expect(currentClaimHandle()).toBeUndefined();
    });

    it.each(["comment", "close"])("should not capture a %s receipt outside a Claim", async type => {
      const { addDiscussionComment, closeDiscussionAsOutdated } = await import("./close_older_discussions.cjs");
      const entity = { id: type === "comment" ? "DC_comment" : "D_old", url: "https://github.com/testowner/testrepo/discussions/5" };
      mockGithub.graphql.mockResolvedValueOnce(type === "comment" ? { addDiscussionComment: { comment: entity } } : { closeDiscussion: { discussion: entity } });
      const receipts = [];

      const result = type === "comment" ? await addDiscussionComment(mockGithub, "testowner", "testrepo", "D_old", "Superseded", receipts) : await closeDiscussionAsOutdated(mockGithub, "testowner", "testrepo", "D_old", receipts);

      expect(result).toEqual(entity);
      expect(receipts).toEqual([]);
    });
  });

  describe("closeOlderDiscussions", () => {
    it("should close older discussions and add comments", async () => {
      const { closeOlderDiscussions } = await import("./close_older_discussions.cjs");

      // Mock search response
      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [
            {
              id: "D_old1",
              number: 5,
              title: "Old Report",
              url: "https://github.com/testowner/testrepo/discussions/5",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
          ],
        },
      });

      // Mock add comment response
      mockGithub.graphql.mockResolvedValueOnce({
        addDiscussionComment: {
          comment: {
            id: "DC_comment1",
            url: "https://github.com/testowner/testrepo/discussions/5#comment-1",
          },
        },
      });

      // Mock close discussion response
      mockGithub.graphql.mockResolvedValueOnce({
        closeDiscussion: {
          discussion: {
            id: "D_old1",
            url: "https://github.com/testowner/testrepo/discussions/5",
          },
        },
      });

      const result = await closeOlderDiscussions(
        mockGithub,
        "testowner",
        "testrepo",
        "test-workflow",
        "DIC_test123",
        { number: 10, url: "https://github.com/testowner/testrepo/discussions/10" },
        "Test Workflow",
        "https://github.com/testowner/testrepo/actions/runs/123"
      );

      expect(result).toHaveLength(1);
      expect(result[0].number).toBe(5);
      expect(mockGithub.graphql).toHaveBeenCalledTimes(3); // search + comment + close
    });

    it("should limit closed discussions to MAX_CLOSE_COUNT (10)", async () => {
      const { closeOlderDiscussions, MAX_CLOSE_COUNT } = await import("./close_older_discussions.cjs");

      // Create 15 discussions to test the limit
      const discussions = Array.from({ length: 15 }, (_, i) => ({
        id: `D_old${i}`,
        number: i + 1,
        title: `Old Report ${i + 1}`,
        url: `https://github.com/testowner/testrepo/discussions/${i + 1}`,
        body: "<!-- gh-aw-workflow-id: test-workflow -->",
        category: { id: "DIC_test123" },
        closed: false,
      }));

      // Mock search response with 15 discussions
      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: discussions,
        },
      });

      // Mock comment and close responses for each closed discussion
      for (let i = 0; i < MAX_CLOSE_COUNT; i++) {
        mockGithub.graphql.mockResolvedValueOnce({
          addDiscussionComment: {
            comment: { id: `DC_comment${i}`, url: `https://github.com/testowner/testrepo/discussions/${i + 1}#comment-1` },
          },
        });
        mockGithub.graphql.mockResolvedValueOnce({
          closeDiscussion: {
            discussion: { id: `D_old${i}`, url: `https://github.com/testowner/testrepo/discussions/${i + 1}` },
          },
        });
      }

      const result = await closeOlderDiscussions(
        mockGithub,
        "testowner",
        "testrepo",
        "test-workflow",
        "DIC_test123",
        { number: 100, url: "https://github.com/testowner/testrepo/discussions/100" },
        "Test Workflow",
        "https://github.com/testowner/testrepo/actions/runs/123"
      );

      expect(result).toHaveLength(MAX_CLOSE_COUNT);
      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining(`Found 15 older discussions, but only closing the first ${MAX_CLOSE_COUNT}`));
    });

    it("should return empty array when no older discussions found", async () => {
      const { closeOlderDiscussions } = await import("./close_older_discussions.cjs");

      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [],
        },
      });

      const result = await closeOlderDiscussions(
        mockGithub,
        "testowner",
        "testrepo",
        "test-workflow",
        "DIC_test123",
        { number: 10, url: "https://github.com/testowner/testrepo/discussions/10" },
        "Test Workflow",
        "https://github.com/testowner/testrepo/actions/runs/123"
      );

      expect(result).toEqual([]);
      expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("No older discussions found to close"));
    });

    it("should continue closing other discussions if one fails", async () => {
      const { closeOlderDiscussions } = await import("./close_older_discussions.cjs");

      // Mock search response with 2 discussions
      mockGithub.graphql.mockResolvedValueOnce({
        search: {
          nodes: [
            {
              id: "D_fail",
              number: 5,
              title: "Will Fail",
              url: "https://github.com/testowner/testrepo/discussions/5",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
            {
              id: "D_success",
              number: 6,
              title: "Will Succeed",
              url: "https://github.com/testowner/testrepo/discussions/6",
              body: "<!-- gh-aw-workflow-id: test-workflow -->",
              category: { id: "DIC_test123" },
              closed: false,
            },
          ],
        },
      });

      // First discussion: add comment fails
      mockGithub.graphql.mockRejectedValueOnce(new Error("GraphQL error"));

      // Second discussion: success
      mockGithub.graphql.mockResolvedValueOnce({
        addDiscussionComment: {
          comment: { id: "DC_comment2", url: "https://github.com/testowner/testrepo/discussions/6#comment-1" },
        },
      });
      mockGithub.graphql.mockResolvedValueOnce({
        closeDiscussion: {
          discussion: { id: "D_success", url: "https://github.com/testowner/testrepo/discussions/6" },
        },
      });

      const result = await closeOlderDiscussions(
        mockGithub,
        "testowner",
        "testrepo",
        "test-workflow",
        "DIC_test123",
        { number: 10, url: "https://github.com/testowner/testrepo/discussions/10" },
        "Test Workflow",
        "https://github.com/testowner/testrepo/actions/runs/123"
      );

      expect(result).toHaveLength(1);
      expect(result[0].number).toBe(6);
      expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Failed to close discussion #5"));
    });
  });

  describe("MAX_CLOSE_COUNT", () => {
    it("should be set to 10", async () => {
      const { MAX_CLOSE_COUNT } = await import("./close_older_discussions.cjs");
      expect(MAX_CLOSE_COUNT).toBe(10);
    });
  });

  describe("GRAPHQL_DELAY_MS", () => {
    it("should be set to 500ms", async () => {
      const { GRAPHQL_DELAY_MS } = await import("./close_older_discussions.cjs");
      expect(GRAPHQL_DELAY_MS).toBe(500);
    });
  });
});
