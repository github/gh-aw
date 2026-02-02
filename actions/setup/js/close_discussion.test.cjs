// @ts-check
/// <reference types="@actions/github-script" />

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { main } from "./close_discussion.cjs";

describe("close_discussion", () => {
  let mockGithub;
  let mockContext;
  let mockCore;
  let originalGlobal;

  beforeEach(() => {
    // Save original global
    originalGlobal = { ...global };

    // Mock GitHub API
    mockGithub = {
      graphql: vi.fn(),
    };

    // Mock context
    mockContext = {
      repo: {
        owner: "testowner",
        repo: "testrepo",
      },
      payload: {},
    };

    // Mock core
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
    };

    // Set global mocks
    global.github = mockGithub;
    global.context = mockContext;
    global.core = mockCore;
  });

  afterEach(() => {
    // Restore original global
    global.github = originalGlobal.github;
    global.context = originalGlobal.context;
    global.core = originalGlobal.core;
  });

  it("should close a discussion with explicit discussion number", async () => {
    const handler = await main({});

    mockGithub.graphql
      .mockResolvedValueOnce({
        repository: {
          discussion: {
            id: "D_kwDOAB1234",
            title: "Test Discussion",
            category: { name: "General" },
            url: "https://github.com/testowner/testrepo/discussions/1",
            labels: {
              nodes: [],
              pageInfo: { hasNextPage: false, endCursor: null },
            },
          },
        },
      })
      .mockResolvedValueOnce({
        closeDiscussion: {
          discussion: {
            id: "D_kwDOAB1234",
            url: "https://github.com/testowner/testrepo/discussions/1",
          },
        },
      });

    const result = await handler({ discussion_number: 1 }, {});

    expect(result).toEqual({
      success: true,
      number: 1,
      url: "https://github.com/testowner/testrepo/discussions/1",
      commentUrl: undefined,
    });
    expect(mockGithub.graphql).toHaveBeenCalledTimes(2);
  });

  it("should close a discussion from context payload", async () => {
    mockContext.payload.discussion = { number: 42 };
    const handler = await main({});

    mockGithub.graphql
      .mockResolvedValueOnce({
        repository: {
          discussion: {
            id: "D_kwDOAB5678",
            title: "Context Discussion",
            category: { name: "Q&A" },
            url: "https://github.com/testowner/testrepo/discussions/42",
            labels: {
              nodes: [],
              pageInfo: { hasNextPage: false, endCursor: null },
            },
          },
        },
      })
      .mockResolvedValueOnce({
        closeDiscussion: {
          discussion: {
            id: "D_kwDOAB5678",
            url: "https://github.com/testowner/testrepo/discussions/42",
          },
        },
      });

    const result = await handler({}, {});

    expect(result.success).toBe(true);
    expect(result.number).toBe(42);
  });

  it("should close a discussion with a comment", async () => {
    const handler = await main({});

    mockGithub.graphql
      .mockResolvedValueOnce({
        repository: {
          discussion: {
            id: "D_kwDOAB1234",
            title: "Test Discussion",
            category: { name: "General" },
            url: "https://github.com/testowner/testrepo/discussions/1",
            labels: {
              nodes: [],
              pageInfo: { hasNextPage: false, endCursor: null },
            },
          },
        },
      })
      .mockResolvedValueOnce({
        addDiscussionComment: {
          comment: {
            id: "C_kwDOAB9876",
            url: "https://github.com/testowner/testrepo/discussions/1#discussioncomment-123",
          },
        },
      })
      .mockResolvedValueOnce({
        closeDiscussion: {
          discussion: {
            id: "D_kwDOAB1234",
            url: "https://github.com/testowner/testrepo/discussions/1",
          },
        },
      });

    const result = await handler(
      {
        discussion_number: 1,
        body: "This discussion is resolved.",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.commentUrl).toBe("https://github.com/testowner/testrepo/discussions/1#discussioncomment-123");
  });

  it("should close a discussion with a reason", async () => {
    const handler = await main({});

    mockGithub.graphql
      .mockResolvedValueOnce({
        repository: {
          discussion: {
            id: "D_kwDOAB1234",
            title: "Test Discussion",
            category: { name: "General" },
            url: "https://github.com/testowner/testrepo/discussions/1",
            labels: {
              nodes: [],
              pageInfo: { hasNextPage: false, endCursor: null },
            },
          },
        },
      })
      .mockResolvedValueOnce({
        closeDiscussion: {
          discussion: {
            id: "D_kwDOAB1234",
            url: "https://github.com/testowner/testrepo/discussions/1",
          },
        },
      });

    const result = await handler(
      {
        discussion_number: 1,
        reason: "RESOLVED",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(mockGithub.graphql).toHaveBeenNthCalledWith(2, expect.stringContaining("reason: DiscussionCloseReason"), expect.objectContaining({ reason: "RESOLVED" }));
  });

  it("should fail when no discussion number is provided", async () => {
    const handler = await main({});

    const result = await handler({}, {});

    expect(result).toEqual({
      success: false,
      error: "No discussion_number provided and not in discussion context",
    });
    expect(mockCore.warning).toHaveBeenCalled();
  });

  it("should fail with invalid discussion number", async () => {
    const handler = await main({});

    const result = await handler({ discussion_number: "invalid" }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Invalid discussion number");
    expect(mockCore.warning).toHaveBeenCalled();
  });

  it("should respect max count limit", async () => {
    const handler = await main({ max: 2 });

    mockGithub.graphql
      .mockResolvedValueOnce({
        repository: {
          discussion: {
            id: "D_1",
            title: "First",
            category: { name: "General" },
            url: "https://github.com/testowner/testrepo/discussions/1",
            labels: { nodes: [], pageInfo: { hasNextPage: false, endCursor: null } },
          },
        },
      })
      .mockResolvedValueOnce({
        closeDiscussion: { discussion: { id: "D_1", url: "https://url1" } },
      })
      .mockResolvedValueOnce({
        repository: {
          discussion: {
            id: "D_2",
            title: "Second",
            category: { name: "General" },
            url: "https://github.com/testowner/testrepo/discussions/2",
            labels: { nodes: [], pageInfo: { hasNextPage: false, endCursor: null } },
          },
        },
      })
      .mockResolvedValueOnce({
        closeDiscussion: { discussion: { id: "D_2", url: "https://url2" } },
      });

    const result1 = await handler({ discussion_number: 1 }, {});
    const result2 = await handler({ discussion_number: 2 }, {});
    const result3 = await handler({ discussion_number: 3 }, {});

    expect(result1.success).toBe(true);
    expect(result2.success).toBe(true);
    expect(result3.success).toBe(false);
    expect(result3.error).toContain("Max count of 2 reached");
  });

  it("should validate required labels", async () => {
    const handler = await main({ required_labels: ["bug", "verified"] });

    mockGithub.graphql.mockResolvedValueOnce({
      repository: {
        discussion: {
          id: "D_kwDOAB1234",
          title: "Test Discussion",
          category: { name: "General" },
          url: "https://github.com/testowner/testrepo/discussions/1",
          labels: {
            nodes: [{ name: "bug" }],
            pageInfo: { hasNextPage: false, endCursor: null },
          },
        },
      },
    });

    const result = await handler({ discussion_number: 1 }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Missing required labels: verified");
  });

  it("should pass when all required labels are present", async () => {
    const handler = await main({ required_labels: ["bug", "verified"] });

    mockGithub.graphql
      .mockResolvedValueOnce({
        repository: {
          discussion: {
            id: "D_kwDOAB1234",
            title: "Test Discussion",
            category: { name: "General" },
            url: "https://github.com/testowner/testrepo/discussions/1",
            labels: {
              nodes: [{ name: "bug" }, { name: "verified" }],
              pageInfo: { hasNextPage: false, endCursor: null },
            },
          },
        },
      })
      .mockResolvedValueOnce({
        closeDiscussion: {
          discussion: {
            id: "D_kwDOAB1234",
            url: "https://github.com/testowner/testrepo/discussions/1",
          },
        },
      });

    const result = await handler({ discussion_number: 1 }, {});

    expect(result.success).toBe(true);
  });

  it("should validate required title prefix", async () => {
    const handler = await main({ required_title_prefix: "[RESOLVED]" });

    mockGithub.graphql.mockResolvedValueOnce({
      repository: {
        discussion: {
          id: "D_kwDOAB1234",
          title: "Test Discussion",
          category: { name: "General" },
          url: "https://github.com/testowner/testrepo/discussions/1",
          labels: {
            nodes: [],
            pageInfo: { hasNextPage: false, endCursor: null },
          },
        },
      },
    });

    const result = await handler({ discussion_number: 1 }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain('Title doesn\'t start with "[RESOLVED]"');
  });

  it("should pass when title has required prefix", async () => {
    const handler = await main({ required_title_prefix: "[RESOLVED]" });

    mockGithub.graphql
      .mockResolvedValueOnce({
        repository: {
          discussion: {
            id: "D_kwDOAB1234",
            title: "[RESOLVED] Test Discussion",
            category: { name: "General" },
            url: "https://github.com/testowner/testrepo/discussions/1",
            labels: {
              nodes: [],
              pageInfo: { hasNextPage: false, endCursor: null },
            },
          },
        },
      })
      .mockResolvedValueOnce({
        closeDiscussion: {
          discussion: {
            id: "D_kwDOAB1234",
            url: "https://github.com/testowner/testrepo/discussions/1",
          },
        },
      });

    const result = await handler({ discussion_number: 1 }, {});

    expect(result.success).toBe(true);
  });

  it("should handle GraphQL errors gracefully", async () => {
    const handler = await main({});

    mockGithub.graphql.mockRejectedValueOnce(new Error("GraphQL API Error"));

    const result = await handler({ discussion_number: 1 }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("GraphQL API Error");
    expect(mockCore.error).toHaveBeenCalled();
  });

  it("should handle pagination for labels", async () => {
    const handler = await main({});

    mockGithub.graphql
      .mockResolvedValueOnce({
        repository: {
          discussion: {
            id: "D_kwDOAB1234",
            title: "Test Discussion",
            category: { name: "General" },
            url: "https://github.com/testowner/testrepo/discussions/1",
            labels: {
              nodes: Array.from({ length: 100 }, (_, i) => ({ name: `label${i}` })),
              pageInfo: { hasNextPage: true, endCursor: "cursor1" },
            },
          },
        },
      })
      .mockResolvedValueOnce({
        repository: {
          discussion: {
            id: "D_kwDOAB1234",
            title: "Test Discussion",
            category: { name: "General" },
            url: "https://github.com/testowner/testrepo/discussions/1",
            labels: {
              nodes: [{ name: "label100" }, { name: "label101" }],
              pageInfo: { hasNextPage: false, endCursor: null },
            },
          },
        },
      })
      .mockResolvedValueOnce({
        closeDiscussion: {
          discussion: {
            id: "D_kwDOAB1234",
            url: "https://github.com/testowner/testrepo/discussions/1",
          },
        },
      });

    const result = await handler({ discussion_number: 1 }, {});

    expect(result.success).toBe(true);
    expect(mockGithub.graphql).toHaveBeenCalledTimes(3); // 2 pagination calls + 1 close call
  });

  it("should throw error when discussion is not found", async () => {
    const handler = await main({});

    mockGithub.graphql.mockResolvedValueOnce({
      repository: {
        discussion: null,
      },
    });

    const result = await handler({ discussion_number: 999 }, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("Discussion #999 not found");
  });

  it("should use nullish coalescing for reason (undefined vs null)", async () => {
    const handler = await main({});

    mockGithub.graphql
      .mockResolvedValueOnce({
        repository: {
          discussion: {
            id: "D_kwDOAB1234",
            title: "Test Discussion",
            category: { name: "General" },
            url: "https://github.com/testowner/testrepo/discussions/1",
            labels: {
              nodes: [],
              pageInfo: { hasNextPage: false, endCursor: null },
            },
          },
        },
      })
      .mockResolvedValueOnce({
        closeDiscussion: {
          discussion: {
            id: "D_kwDOAB1234",
            url: "https://github.com/testowner/testrepo/discussions/1",
          },
        },
      });

    const result = await handler({ discussion_number: 1, reason: null }, {});

    expect(result.success).toBe(true);
    // Should use mutation without reason parameter when reason is null/undefined
    expect(mockGithub.graphql).toHaveBeenNthCalledWith(2, expect.stringContaining("mutation($dId: ID!)"), expect.objectContaining({ dId: "D_kwDOAB1234" }));
  });
});
