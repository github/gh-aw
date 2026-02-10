// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { main: createDiscussionMain } = require("./create_discussion.cjs");

describe("create_discussion category normalization", () => {
  let mockGithub;
  let mockCore;
  let mockContext;
  let mockExec;
  let originalEnv;

  beforeEach(() => {
    // Save original environment
    originalEnv = { ...process.env };

    // Mock GitHub API with discussion categories
    mockGithub = {
      rest: {},
      graphql: vi.fn().mockImplementation((query, variables) => {
        // Handle repository query (fetch categories)
        if (query.includes("discussionCategories")) {
          return Promise.resolve({
            repository: {
              id: "R_test123",
              discussionCategories: {
                nodes: [
                  {
                    id: "DIC_kwDOGFsHUM4BsUn1",
                    name: "General",
                    slug: "general",
                    description: "General discussions",
                  },
                  {
                    id: "DIC_kwDOGFsHUM4BsUn2",
                    name: "Audits",
                    slug: "audits",
                    description: "Audit reports",
                  },
                  {
                    id: "DIC_kwDOGFsHUM4BsUn3",
                    name: "Research",
                    slug: "research",
                    description: "Research discussions",
                  },
                ],
              },
            },
          });
        }
        // Handle create discussion mutation
        if (query.includes("createDiscussion")) {
          return Promise.resolve({
            createDiscussion: {
              discussion: {
                id: "D_test456",
                number: 42,
                title: variables.title,
                url: "https://github.com/test-owner/test-repo/discussions/42",
              },
            },
          });
        }
        return Promise.reject(new Error("Unknown GraphQL query"));
      }),
    };

    // Mock Core
    mockCore = {
      info: vi.fn(),
      warning: vi.fn(),
      error: vi.fn(),
      setOutput: vi.fn(),
    };

    // Mock Context
    mockContext = {
      repo: { owner: "test-owner", repo: "test-repo" },
      runId: 12345,
      payload: {
        repository: {
          html_url: "https://github.com/test-owner/test-repo",
        },
      },
    };

    // Mock Exec
    mockExec = {
      exec: vi.fn().mockResolvedValue(0),
    };

    // Set globals
    global.github = mockGithub;
    global.core = mockCore;
    global.context = mockContext;
    global.exec = mockExec;

    // Set required environment variables
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_WORKFLOW_ID = "test-workflow";
    process.env.GH_AW_WORKFLOW_SOURCE_URL = "https://github.com/owner/repo/blob/main/workflow.md";
    process.env.GITHUB_SERVER_URL = "https://github.com";
  });

  afterEach(() => {
    // Restore environment
    process.env = originalEnv;
    vi.clearAllMocks();
  });

  it("should match category name case-insensitively (lowercase config, capitalized repo)", async () => {
    const handler = await createDiscussionMain({
      max: 5,
      category: "audits", // lowercase config
    });

    const result = await handler(
      {
        title: "Test Discussion",
        body: "This is a test discussion.",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.number).toBe(42);

    // Verify the correct category ID was used (Audits with capital A)
    const createMutationCall = mockGithub.graphql.mock.calls.find(call => call[0].includes("createDiscussion"));
    expect(createMutationCall).toBeDefined();
    expect(createMutationCall[1].categoryId).toBe("DIC_kwDOGFsHUM4BsUn2"); // Audits category
  });

  it("should match category name case-insensitively (capitalized config, capitalized repo)", async () => {
    const handler = await createDiscussionMain({
      max: 5,
      category: "Audits", // Capitalized config (user error)
    });

    const result = await handler(
      {
        title: "Test Discussion",
        body: "This is a test discussion.",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.number).toBe(42);

    // Verify the correct category ID was used
    const createMutationCall = mockGithub.graphql.mock.calls.find(call => call[0].includes("createDiscussion"));
    expect(createMutationCall).toBeDefined();
    expect(createMutationCall[1].categoryId).toBe("DIC_kwDOGFsHUM4BsUn2"); // Audits category
  });

  it("should match category name case-insensitively (mixed case config)", async () => {
    const handler = await createDiscussionMain({
      max: 5,
      category: "AuDiTs", // Mixed case (should still match)
    });

    const result = await handler(
      {
        title: "Test Discussion",
        body: "This is a test discussion.",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.number).toBe(42);

    // Verify the correct category ID was used
    const createMutationCall = mockGithub.graphql.mock.calls.find(call => call[0].includes("createDiscussion"));
    expect(createMutationCall).toBeDefined();
    expect(createMutationCall[1].categoryId).toBe("DIC_kwDOGFsHUM4BsUn2"); // Audits category
  });

  it("should match category slug case-insensitively", async () => {
    const handler = await createDiscussionMain({
      max: 5,
      category: "RESEARCH", // Uppercase slug
    });

    const result = await handler(
      {
        title: "Test Discussion",
        body: "This is a test discussion.",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.number).toBe(42);

    // Verify the correct category ID was used (Research)
    const createMutationCall = mockGithub.graphql.mock.calls.find(call => call[0].includes("createDiscussion"));
    expect(createMutationCall).toBeDefined();
    expect(createMutationCall[1].categoryId).toBe("DIC_kwDOGFsHUM4BsUn3"); // Research category
  });

  it("should preserve category IDs (exact match, case-sensitive)", async () => {
    const handler = await createDiscussionMain({
      max: 5,
      category: "DIC_kwDOGFsHUM4BsUn3", // Direct category ID
    });

    const result = await handler(
      {
        title: "Test Discussion",
        body: "This is a test discussion.",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.number).toBe(42);

    // Verify the exact category ID was used
    const createMutationCall = mockGithub.graphql.mock.calls.find(call => call[0].includes("createDiscussion"));
    expect(createMutationCall).toBeDefined();
    expect(createMutationCall[1].categoryId).toBe("DIC_kwDOGFsHUM4BsUn3");
  });

  it("should use item category over config category (case-insensitive)", async () => {
    const handler = await createDiscussionMain({
      max: 5,
      category: "general", // Config says general
    });

    const result = await handler(
      {
        title: "Test Discussion",
        body: "This is a test discussion.",
        category: "AUDITS", // Item overrides with uppercase
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.number).toBe(42);

    // Verify Audits category was used (from item, not config)
    const createMutationCall = mockGithub.graphql.mock.calls.find(call => call[0].includes("createDiscussion"));
    expect(createMutationCall).toBeDefined();
    expect(createMutationCall[1].categoryId).toBe("DIC_kwDOGFsHUM4BsUn2"); // Audits category
  });

  it("should fallback to first category when no match found", async () => {
    const handler = await createDiscussionMain({
      max: 5,
      category: "NonExistentCategory",
    });

    const result = await handler(
      {
        title: "Test Discussion",
        body: "This is a test discussion.",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.number).toBe(42);

    // Verify fallback to first category (General)
    const createMutationCall = mockGithub.graphql.mock.calls.find(call => call[0].includes("createDiscussion"));
    expect(createMutationCall).toBeDefined();
    expect(createMutationCall[1].categoryId).toBe("DIC_kwDOGFsHUM4BsUn1"); // General (first)
  });
});
