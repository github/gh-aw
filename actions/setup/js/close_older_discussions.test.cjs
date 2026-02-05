import { describe, it, expect, beforeEach, vi } from "vitest";

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
              category: { id: "DIC_test123" },
              closed: false,
            },
            {
              id: "D_new",
              number: 10, // This is the new discussion, should be excluded
              title: "Weekly Report - 2024-02",
              url: "https://github.com/testowner/testrepo/discussions/10",
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
              category: { id: "DIC_test123" },
              closed: false,
            },
            {
              id: "D_closed",
              number: 6,
              title: "Closed Report",
              url: "https://github.com/testowner/testrepo/discussions/6",
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
              category: { id: "DIC_reports" },
              closed: false,
            },
            {
              id: "D_wrongcat",
              number: 6,
              title: "Report in wrong category",
              url: "https://github.com/testowner/testrepo/discussions/6",
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
              category: { id: "DIC_reports" },
              closed: false,
            },
            {
              id: "D_cat2",
              number: 6,
              title: "Report 2",
              url: "https://github.com/testowner/testrepo/discussions/6",
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
              category: { id: "DIC_test123" },
              closed: false,
            },
            {
              id: "D_success",
              number: 6,
              title: "Will Succeed",
              url: "https://github.com/testowner/testrepo/discussions/6",
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
