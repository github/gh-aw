// @ts-check
import { describe, it, expect, beforeAll, beforeEach, vi } from "vitest";

let main;

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setOutput: vi.fn(),
};

const mockGithub = {
  graphql: vi.fn(),
};

const mockContext = {
  repo: {
    owner: "test-owner",
    repo: "test-repo",
  },
};

global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

beforeAll(async () => {
  const mod = await import("./create_project_status_update.cjs");
  main = mod.main;
});

describe("create_project_status_update", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("should create a project status update with all fields", async () => {
    // Mock GraphQL responses
    mockGithub.graphql
      .mockResolvedValueOnce({
        // First call: resolve project
        organization: {
          projectV2: {
            id: "PVT_test123",
            number: 42,
            title: "Test Project",
            url: "https://github.com/orgs/test-org/projects/42",
          },
        },
      })
      .mockResolvedValueOnce({
        // Second call: create status update
        createProjectV2StatusUpdate: {
          statusUpdate: {
            id: "PVTSU_test123",
            body: "Test status update",
            bodyHTML: "<p>Test status update</p>",
            startDate: "2025-01-01",
            targetDate: "2025-12-31",
            status: "ON_TRACK",
            createdAt: "2025-01-06T12:00:00Z",
          },
        },
      });

    const handler = await main({ max: 10 });

    const result = await handler(
      {
        project: "https://github.com/orgs/test-org/projects/42",
        body: "Test status update",
        status: "ON_TRACK",
        start_date: "2025-01-01",
        target_date: "2025-12-31",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.status_update_id).toBe("PVTSU_test123");
    expect(result.project_id).toBe("PVT_test123");
    expect(result.status).toBe("ON_TRACK");

    expect(mockGithub.graphql).toHaveBeenCalledTimes(2);
    expect(mockCore.setOutput).toHaveBeenCalledWith("status-update-id", "PVTSU_test123");
  });

  it("should handle missing project field", async () => {
    const handler = await main({ max: 10 });

    const result = await handler(
      {
        body: "Test status update",
      },
      {}
    );

    expect(result.success).toBe(false);
    expect(result.error).toBe("Missing required field: project");
    expect(mockCore.error).toHaveBeenCalled();
  });

  it("should handle missing body field", async () => {
    const handler = await main({ max: 10 });

    const result = await handler(
      {
        project: "https://github.com/orgs/test-org/projects/42",
      },
      {}
    );

    expect(result.success).toBe(false);
    expect(result.error).toBe("Missing required field: body");
    expect(mockCore.error).toHaveBeenCalled();
  });

  it("should default to ON_TRACK status if not provided", async () => {
    mockGithub.graphql
      .mockResolvedValueOnce({
        organization: {
          projectV2: {
            id: "PVT_test123",
            number: 42,
            title: "Test Project",
            url: "https://github.com/orgs/test-org/projects/42",
          },
        },
      })
      .mockResolvedValueOnce({
        createProjectV2StatusUpdate: {
          statusUpdate: {
            id: "PVTSU_test123",
            body: "Test",
            bodyHTML: "<p>Test</p>",
            startDate: "2025-01-06",
            targetDate: "2025-01-06",
            status: "ON_TRACK",
            createdAt: "2025-01-06T12:00:00Z",
          },
        },
      });

    const handler = await main({ max: 10 });

    const result = await handler(
      {
        project: "https://github.com/orgs/test-org/projects/42",
        body: "Test",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.status).toBe("ON_TRACK");
  });

  it("should validate status enum and fallback to ON_TRACK for invalid values", async () => {
    mockGithub.graphql
      .mockResolvedValueOnce({
        organization: {
          projectV2: {
            id: "PVT_test123",
            number: 42,
            title: "Test Project",
            url: "https://github.com/orgs/test-org/projects/42",
          },
        },
      })
      .mockResolvedValueOnce({
        createProjectV2StatusUpdate: {
          statusUpdate: {
            id: "PVTSU_test123",
            body: "Test",
            bodyHTML: "<p>Test</p>",
            startDate: "2025-01-06",
            targetDate: "2025-01-06",
            status: "ON_TRACK",
            createdAt: "2025-01-06T12:00:00Z",
          },
        },
      });

    const handler = await main({ max: 10 });

    const result = await handler(
      {
        project: "https://github.com/orgs/test-org/projects/42",
        body: "Test",
        status: "INVALID_STATUS",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Invalid status"));
  });

  it("should respect max count limit", async () => {
    const handler = await main({ max: 1 });

    // First call should succeed
    mockGithub.graphql
      .mockResolvedValueOnce({
        organization: {
          projectV2: {
            id: "PVT_test123",
            number: 42,
            title: "Test Project",
            url: "https://github.com/orgs/test-org/projects/42",
          },
        },
      })
      .mockResolvedValueOnce({
        createProjectV2StatusUpdate: {
          statusUpdate: {
            id: "PVTSU_test123",
            body: "Test",
            bodyHTML: "<p>Test</p>",
            startDate: "2025-01-06",
            targetDate: "2025-01-06",
            status: "ON_TRACK",
            createdAt: "2025-01-06T12:00:00Z",
          },
        },
      });

    const result1 = await handler(
      {
        project: "https://github.com/orgs/test-org/projects/42",
        body: "Test 1",
      },
      {}
    );

    expect(result1.success).toBe(true);

    // Second call should be rejected due to max count
    const result2 = await handler(
      {
        project: "https://github.com/orgs/test-org/projects/42",
        body: "Test 2",
      },
      {}
    );

    expect(result2.success).toBe(false);
    expect(result2.error).toBe("Max count of 1 reached");
  });

  it("should handle GraphQL errors gracefully", async () => {
    const graphQLError = new Error("GraphQL error: Insufficient permissions");
    graphQLError.errors = [
      {
        type: "INSUFFICIENT_SCOPES",
        message: "Insufficient permissions",
      },
    ];

    mockGithub.graphql
      .mockRejectedValueOnce(graphQLError) // First call: direct query fails
      .mockRejectedValueOnce(graphQLError); // Second call: list query also fails

    const handler = await main({ max: 10 });

    const result = await handler(
      {
        project: "https://github.com/orgs/test-org/projects/42",
        body: "Test",
      },
      {}
    );

    expect(result.success).toBe(false);
    expect(result.error).toContain("Insufficient permissions");
    expect(mockCore.error).toHaveBeenCalled();
  });

  it("should support user projects in addition to org projects", async () => {
    mockGithub.graphql
      .mockResolvedValueOnce({
        user: {
          projectV2: {
            id: "PVT_user123",
            number: 10,
            title: "User Project",
            url: "https://github.com/users/test-user/projects/10",
          },
        },
      })
      .mockResolvedValueOnce({
        createProjectV2StatusUpdate: {
          statusUpdate: {
            id: "PVTSU_user123",
            body: "User status update",
            bodyHTML: "<p>User status update</p>",
            startDate: "2025-01-06",
            targetDate: "2025-01-06",
            status: "ON_TRACK",
            createdAt: "2025-01-06T12:00:00Z",
          },
        },
      });

    const handler = await main({ max: 10 });

    const result = await handler(
      {
        project: "https://github.com/users/test-user/projects/10",
        body: "User status update",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.status_update_id).toBe("PVTSU_user123");
  });

  it("should fall back to list-based search when direct projectV2 query returns null", async () => {
    // Mock GraphQL responses
    mockGithub.graphql
      .mockResolvedValueOnce({
        // First call: direct query returns null (project not found)
        organization: {
          projectV2: null,
        },
      })
      .mockResolvedValueOnce({
        // Second call: list-based search finds the project
        organization: {
          projectsV2: {
            totalCount: 2,
            nodes: [
              {
                id: "PVT_test123",
                number: 42,
                title: "Test Project",
                url: "https://github.com/orgs/test-org/projects/42",
              },
              {
                id: "PVT_test456",
                number: 43,
                title: "Another Project",
                url: "https://github.com/orgs/test-org/projects/43",
              },
            ],
            edges: [
              {
                node: {
                  id: "PVT_test123",
                  number: 42,
                  title: "Test Project",
                  url: "https://github.com/orgs/test-org/projects/42",
                },
              },
              {
                node: {
                  id: "PVT_test456",
                  number: 43,
                  title: "Another Project",
                  url: "https://github.com/orgs/test-org/projects/43",
                },
              },
            ],
          },
        },
      })
      .mockResolvedValueOnce({
        // Third call: create status update
        createProjectV2StatusUpdate: {
          statusUpdate: {
            id: "PVTSU_test123",
            body: "Test status update",
            bodyHTML: "<p>Test status update</p>",
            startDate: "2025-01-01",
            targetDate: "2025-12-31",
            status: "ON_TRACK",
            createdAt: "2025-01-06T12:00:00Z",
          },
        },
      });

    const handler = await main({ max: 10 });

    const result = await handler(
      {
        project: "https://github.com/orgs/test-org/projects/42",
        body: "Test status update",
        status: "ON_TRACK",
        start_date: "2025-01-01",
        target_date: "2025-12-31",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.status_update_id).toBe("PVTSU_test123");
    expect(result.project_id).toBe("PVT_test123");
    expect(result.status).toBe("ON_TRACK");

    // Should have called graphql 3 times (direct query, list query, create mutation)
    expect(mockGithub.graphql).toHaveBeenCalledTimes(3);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("returned null"));
  });

  it("should fall back to list-based search when direct projectV2 query throws NOT_FOUND error", async () => {
    // Mock GraphQL responses
    const notFoundError = new Error("Could not resolve to a ProjectV2 with the number 42.");
    notFoundError.errors = [
      {
        type: "NOT_FOUND",
        message: "Could not resolve to a ProjectV2 with the number 42.",
        path: ["organization", "projectV2"],
      },
    ];

    mockGithub.graphql
      .mockRejectedValueOnce(notFoundError) // First call: direct query throws NOT_FOUND
      .mockResolvedValueOnce({
        // Second call: list-based search finds the project
        organization: {
          projectsV2: {
            totalCount: 1,
            nodes: [
              {
                id: "PVT_test123",
                number: 42,
                title: "Test Project",
                url: "https://github.com/orgs/test-org/projects/42",
              },
            ],
            edges: [
              {
                node: {
                  id: "PVT_test123",
                  number: 42,
                  title: "Test Project",
                  url: "https://github.com/orgs/test-org/projects/42",
                },
              },
            ],
          },
        },
      })
      .mockResolvedValueOnce({
        // Third call: create status update
        createProjectV2StatusUpdate: {
          statusUpdate: {
            id: "PVTSU_test123",
            body: "Test status update",
            bodyHTML: "<p>Test status update</p>",
            startDate: "2025-01-01",
            targetDate: "2025-12-31",
            status: "ON_TRACK",
            createdAt: "2025-01-06T12:00:00Z",
          },
        },
      });

    const handler = await main({ max: 10 });

    const result = await handler(
      {
        project: "https://github.com/orgs/test-org/projects/42",
        body: "Test status update",
        status: "ON_TRACK",
        start_date: "2025-01-01",
        target_date: "2025-12-31",
      },
      {}
    );

    expect(result.success).toBe(true);
    expect(result.status_update_id).toBe("PVTSU_test123");
    expect(result.project_id).toBe("PVT_test123");

    // Should have called graphql 3 times (direct query, list query, create mutation)
    expect(mockGithub.graphql).toHaveBeenCalledTimes(3);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("falling back to projectsV2 list search"));
  });

  it("should fail gracefully when both direct query and fallback list query fail", async () => {
    // Mock GraphQL responses - both queries fail
    const notFoundError = new Error("Could not resolve to a ProjectV2 with the number 42.");
    notFoundError.errors = [
      {
        type: "NOT_FOUND",
        message: "Could not resolve to a ProjectV2 with the number 42.",
        path: ["organization", "projectV2"],
      },
    ];

    const apiError = new Error("Request failed due to following response errors:\n - Something went wrong while executing your query.");
    apiError.errors = [
      {
        message: "Something went wrong while executing your query.",
      },
    ];

    mockGithub.graphql
      .mockRejectedValueOnce(notFoundError) // First call: direct query throws NOT_FOUND
      .mockRejectedValueOnce(apiError); // Second call: list-based search also fails

    const handler = await main({ max: 10 });

    const result = await handler(
      {
        project: "https://github.com/orgs/test-org/projects/42",
        body: "Test status update",
        status: "ON_TRACK",
        start_date: "2025-01-01",
        target_date: "2025-12-31",
      },
      {}
    );

    expect(result.success).toBe(false);
    expect(result.error).toContain("Unable to resolve project #42");
    expect(result.error).toContain("Both direct projectV2 query and fallback projectsV2 list query failed");
    expect(result.error).toContain("transient GitHub API error");

    // Should have called graphql 2 times (direct query, list query - both failed)
    expect(mockGithub.graphql).toHaveBeenCalledTimes(2);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("falling back to projectsV2 list search"));
  });

  it("should fail when project field is missing even if GH_AW_PROJECT_URL is set", async () => {
    // Set default project URL in environment (should be ignored)
    const defaultProjectUrl = "https://github.com/orgs/test-org/projects/42";
    process.env.GH_AW_PROJECT_URL = defaultProjectUrl;

    const handler = await main({ max: 10 });

    const messageWithoutProject = {
      body: "Default project status",
      status: "ON_TRACK",
      start_date: "2025-01-01",
      target_date: "2025-12-31",
    };

    const result = await handler(messageWithoutProject, {});

    expect(result.success).toBe(false);
    expect(result.error).toBe("Missing required field: project");
    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining('Missing required "project" field'));

    // Cleanup
    delete process.env.GH_AW_PROJECT_URL;
  });

  it("should succeed when project field is explicitly provided", async () => {
    // Set default project URL in environment (should be ignored since message has explicit project)
    process.env.GH_AW_PROJECT_URL = "https://github.com/orgs/test-org/projects/999";

    const messageProjectUrl = "https://github.com/orgs/test-org/projects/42";

    mockGithub.graphql
      .mockResolvedValueOnce({
        // First call: direct project query by number
        organization: {
          projectV2: {
            id: "PVT_test789",
            number: 42,
            title: "Test Project",
            url: messageProjectUrl,
          },
        },
      })
      .mockResolvedValueOnce({
        // Second call: create status update
        createProjectV2StatusUpdate: {
          statusUpdate: {
            id: "PVTSU_test789",
            body: "Message project status",
            bodyHTML: "<p>Message project status</p>",
            startDate: "2025-01-01",
            targetDate: "2025-12-31",
            status: "ON_TRACK",
            createdAt: "2025-01-06T12:00:00Z",
          },
        },
      });

    const handler = await main({ max: 10 });

    const messageWithProject = {
      project: messageProjectUrl,
      body: "Message project status",
      status: "ON_TRACK",
      start_date: "2025-01-01",
      target_date: "2025-12-31",
    };

    const result = await handler(messageWithProject, {});

    expect(result.success).toBe(true);
    expect(result.status_update_id).toBe("PVTSU_test789");

    // Cleanup
    delete process.env.GH_AW_PROJECT_URL;
  });
});
