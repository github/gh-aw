// @ts-check

import { describe, it, expect, beforeEach, vi } from "vitest";
import { closeOlderEntities, MAX_CLOSE_COUNT, delay } from "./close_older_entities.cjs";
import { MAX_CLOSE_COUNT as SHARED_MAX_CLOSE_COUNT } from "./constants.cjs";

// Mock globals
global.core = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
};

describe("close_older_entities", () => {
  let mockGithub;
  let mockSearchOlderEntities;
  let mockGetCloseMessage;
  let mockAddComment;
  let mockCloseEntity;
  let mockConfig;

  beforeEach(() => {
    vi.clearAllMocks();
    mockGithub = {};

    mockSearchOlderEntities = vi.fn();
    mockGetCloseMessage = vi.fn();
    mockAddComment = vi.fn();
    mockCloseEntity = vi.fn();

    mockConfig = {
      entityType: "issue",
      entityTypePlural: "issues",
      searchOlderEntities: mockSearchOlderEntities,
      getCloseMessage: mockGetCloseMessage,
      addComment: mockAddComment,
      closeEntity: mockCloseEntity,
      delayMs: 100,
      getEntityId: entity => entity.number,
      getEntityUrl: entity => entity.html_url,
    };
  });

  describe("MAX_CLOSE_COUNT", () => {
    it("should be set to 10", () => {
      expect(MAX_CLOSE_COUNT).toBe(10);
    });

    it("re-exports the shared cleanup limit", () => {
      expect(MAX_CLOSE_COUNT).toBe(SHARED_MAX_CLOSE_COUNT);
    });
  });

  describe("delay", () => {
    it("should delay execution for specified milliseconds", async () => {
      const start = Date.now();
      await delay(50);
      const elapsed = Date.now() - start;
      expect(elapsed).toBeGreaterThanOrEqual(45); // Allow some tolerance
    });
  });

  describe("closeOlderEntities", () => {
    it("should return empty array when no older entities found", async () => {
      mockSearchOlderEntities.mockResolvedValue([]);

      const result = await closeOlderEntities(
        mockGithub,
        "owner",
        "repo",
        "test-workflow",
        { number: 100, html_url: "https://github.com/owner/repo/issues/100" },
        "Test Workflow",
        "https://github.com/owner/repo/actions/runs/123",
        mockConfig
      );

      expect(result).toEqual([]);
      expect(mockSearchOlderEntities).toHaveBeenCalledWith(mockGithub, "owner", "repo", "test-workflow", 100);
      expect(mockAddComment).not.toHaveBeenCalled();
      expect(mockCloseEntity).not.toHaveBeenCalled();
      expect(global.core.info).toHaveBeenCalledWith("✓ No older issues found to close - operation complete");
    });

    it("should close older entities successfully", async () => {
      mockSearchOlderEntities.mockResolvedValue([
        { number: 98, title: "Old Issue 1", html_url: "https://github.com/owner/repo/issues/98" },
        { number: 99, title: "Old Issue 2", html_url: "https://github.com/owner/repo/issues/99" },
      ]);

      mockGetCloseMessage.mockReturnValue("Closing message");
      mockAddComment.mockResolvedValue({ id: 123, html_url: "https://github.com/owner/repo/issues/98#issuecomment-123" });
      mockCloseEntity.mockResolvedValue({ number: 98, html_url: "https://github.com/owner/repo/issues/98" });

      const result = await closeOlderEntities(
        mockGithub,
        "owner",
        "repo",
        "test-workflow",
        { number: 100, html_url: "https://github.com/owner/repo/issues/100" },
        "Test Workflow",
        "https://github.com/owner/repo/actions/runs/123",
        mockConfig
      );

      expect(result).toHaveLength(2);
      expect(result[0].number).toBe(98);
      expect(result[1].number).toBe(99);
      expect(mockAddComment).toHaveBeenCalledTimes(2);
      expect(mockCloseEntity).toHaveBeenCalledTimes(2);
    });

    it("should limit closed entities to MAX_CLOSE_COUNT", async () => {
      const entities = Array.from({ length: 15 }, (_, i) => ({
        number: i + 1,
        title: `Issue ${i + 1}`,
        html_url: `https://github.com/owner/repo/issues/${i + 1}`,
      }));

      mockSearchOlderEntities.mockResolvedValue(entities);
      mockGetCloseMessage.mockReturnValue("Closing message");
      mockAddComment.mockResolvedValue({ id: 123, html_url: "https://github.com/owner/repo/issues/1#issuecomment-123" });
      mockCloseEntity.mockResolvedValue({ number: 1, html_url: "https://github.com/owner/repo/issues/1" });

      const result = await closeOlderEntities(
        mockGithub,
        "owner",
        "repo",
        "test-workflow",
        { number: 100, html_url: "https://github.com/owner/repo/issues/100" },
        "Test Workflow",
        "https://github.com/owner/repo/actions/runs/123",
        mockConfig
      );

      expect(result).toHaveLength(MAX_CLOSE_COUNT);
      expect(global.core.warning).toHaveBeenCalledWith(`⚠️  Found 15 older issues, but only closing the first ${MAX_CLOSE_COUNT}`);
    });

    it("should continue closing other entities if one fails", async () => {
      mockSearchOlderEntities.mockResolvedValue([
        { number: 98, title: "Will Fail", html_url: "https://github.com/owner/repo/issues/98" },
        { number: 99, title: "Will Succeed", html_url: "https://github.com/owner/repo/issues/99" },
      ]);

      mockGetCloseMessage.mockReturnValue("Closing message");

      // First entity fails
      mockAddComment.mockRejectedValueOnce(new Error("API Error"));

      // Second entity succeeds
      mockAddComment.mockResolvedValueOnce({ id: 124, html_url: "https://github.com/owner/repo/issues/99#issuecomment-124" });
      mockCloseEntity.mockResolvedValueOnce({ number: 99, html_url: "https://github.com/owner/repo/issues/99" });

      const result = await closeOlderEntities(
        mockGithub,
        "owner",
        "repo",
        "test-workflow",
        { number: 100, html_url: "https://github.com/owner/repo/issues/100" },
        "Test Workflow",
        "https://github.com/owner/repo/actions/runs/123",
        mockConfig
      );

      expect(result).toHaveLength(1);
      expect(result[0].number).toBe(99);
      expect(global.core.error).toHaveBeenCalledWith(expect.stringContaining("Failed to close issue #98"));
      expect(global.core.warning).toHaveBeenCalledWith("Failed to close 1 issue(s) - check logs above for details");
    });

    it("should log entity labels if present", async () => {
      mockSearchOlderEntities.mockResolvedValue([
        {
          number: 98,
          title: "Old Issue",
          html_url: "https://github.com/owner/repo/issues/98",
          labels: [{ name: "bug" }, { name: "help wanted" }],
        },
      ]);

      mockGetCloseMessage.mockReturnValue("Closing message");
      mockAddComment.mockResolvedValue({ id: 123, html_url: "https://github.com/owner/repo/issues/98#issuecomment-123" });
      mockCloseEntity.mockResolvedValue({ number: 98, html_url: "https://github.com/owner/repo/issues/98" });

      await closeOlderEntities(mockGithub, "owner", "repo", "test-workflow", { number: 100, html_url: "https://github.com/owner/repo/issues/100" }, "Test Workflow", "https://github.com/owner/repo/actions/runs/123", mockConfig);

      expect(global.core.info).toHaveBeenCalledWith(expect.stringContaining("Labels: bug, help wanted"));
    });

    it("should pass extra arguments to search function", async () => {
      mockSearchOlderEntities.mockResolvedValue([]);

      await closeOlderEntities(
        mockGithub,
        "owner",
        "repo",
        "test-workflow",
        { number: 100, html_url: "https://github.com/owner/repo/issues/100" },
        "Test Workflow",
        "https://github.com/owner/repo/actions/runs/123",
        mockConfig,
        "extra-arg-1",
        "extra-arg-2"
      );

      expect(mockSearchOlderEntities).toHaveBeenCalledWith(mockGithub, "owner", "repo", "test-workflow", "extra-arg-1", "extra-arg-2", 100);
    });

    it("should use url field if html_url is not present", async () => {
      mockSearchOlderEntities.mockResolvedValue([{ number: 98, title: "Discussion", url: "https://github.com/owner/repo/discussions/98", id: "D_98" }]);

      const discussionConfig = {
        ...mockConfig,
        entityType: "discussion",
        entityTypePlural: "discussions",
        getEntityId: entity => entity.id,
        getEntityUrl: entity => entity.url,
      };

      mockGetCloseMessage.mockReturnValue("Closing message");
      mockAddComment.mockResolvedValue({ id: "DC_123", url: "https://github.com/owner/repo/discussions/98#comment-123" });
      mockCloseEntity.mockResolvedValue({ id: "D_98", url: "https://github.com/owner/repo/discussions/98" });

      const result = await closeOlderEntities(
        mockGithub,
        "owner",
        "repo",
        "test-workflow",
        { number: 100, url: "https://github.com/owner/repo/discussions/100" },
        "Test Workflow",
        "https://github.com/owner/repo/actions/runs/123",
        discussionConfig
      );

      expect(result).toHaveLength(1);
      expect(result[0].url).toBe("https://github.com/owner/repo/discussions/98");
    });

    it("should call getCloseMessage with correct parameters", async () => {
      mockSearchOlderEntities.mockResolvedValue([{ number: 98, title: "Old Issue", html_url: "https://github.com/owner/repo/issues/98" }]);

      mockGetCloseMessage.mockReturnValue("Closing message");
      mockAddComment.mockResolvedValue({ id: 123, html_url: "https://github.com/owner/repo/issues/98#issuecomment-123" });
      mockCloseEntity.mockResolvedValue({ number: 98, html_url: "https://github.com/owner/repo/issues/98" });

      await closeOlderEntities(mockGithub, "owner", "repo", "test-workflow", { number: 100, html_url: "https://github.com/owner/repo/issues/100" }, "Test Workflow", "https://github.com/owner/repo/actions/runs/123", mockConfig);

      expect(mockGetCloseMessage).toHaveBeenCalledWith({
        newEntityUrl: "https://github.com/owner/repo/issues/100",
        newEntityNumber: 100,
        workflowName: "Test Workflow",
        runUrl: "https://github.com/owner/repo/actions/runs/123",
      });
    });

    it("should log error stack trace when available", async () => {
      mockSearchOlderEntities.mockResolvedValue([{ number: 98, title: "Will Fail", html_url: "https://github.com/owner/repo/issues/98" }]);

      mockGetCloseMessage.mockReturnValue("Closing message");

      const error = new Error("API Error");
      error.stack = "Error: API Error\n  at test.js:123";
      mockAddComment.mockRejectedValueOnce(error);

      await closeOlderEntities(mockGithub, "owner", "repo", "test-workflow", { number: 100, html_url: "https://github.com/owner/repo/issues/100" }, "Test Workflow", "https://github.com/owner/repo/actions/runs/123", mockConfig);

      expect(global.core.error).toHaveBeenCalledWith(expect.stringContaining("Stack trace:"));
    });
  });
});
