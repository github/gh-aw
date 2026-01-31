import { describe, it, expect, beforeAll, beforeEach, afterEach, vi } from "vitest";

let copyProject;
let parseProjectUrl;
let getProjectId;
let getOwnerId;

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  notice: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
  exportVariable: vi.fn(),
  getInput: vi.fn(),
  summary: {
    addRaw: vi.fn().mockReturnThis(),
    write: vi.fn().mockResolvedValue(),
  },
};

const mockGithub = {
  rest: {
    issues: {
      addLabels: vi.fn().mockResolvedValue({}),
    },
  },
  graphql: vi.fn(),
};

const mockContext = {
  runId: 12345,
  repo: {
    owner: "testowner",
    repo: "testrepo",
  },
  payload: {
    repository: {
      html_url: "https://github.com/testowner/testrepo",
    },
  },
};

global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

beforeAll(async () => {
  const mod = await import("./copy_project.cjs");
  const exports = mod.default || mod;
  copyProject = exports.copyProject;
  parseProjectUrl = exports.parseProjectUrl;
  getProjectId = exports.getProjectId;
  getOwnerId = exports.getOwnerId;
  // Call main to execute the module
  if (exports.main) {
    await exports.main();
  }
});

function clearMock(fn) {
  if (fn && typeof fn.mockClear === "function") {
    fn.mockClear();
  }
}

function clearCoreMocks() {
  clearMock(mockCore.debug);
  clearMock(mockCore.info);
  clearMock(mockCore.notice);
  clearMock(mockCore.warning);
  clearMock(mockCore.error);
  clearMock(mockCore.setFailed);
  clearMock(mockCore.setOutput);
  clearMock(mockCore.exportVariable);
  clearMock(mockCore.getInput);
  clearMock(mockCore.summary.addRaw);
  clearMock(mockCore.summary.write);
}

beforeEach(() => {
  mockGithub.graphql.mockReset();
  mockGithub.rest.issues.addLabels.mockClear();
  clearCoreMocks();
  vi.useRealTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("parseProjectUrl", () => {
  it("should parse organization project URL", () => {
    const result = parseProjectUrl("https://github.com/orgs/myorg/projects/42");
    expect(result).toEqual({
      scope: "orgs",
      ownerLogin: "myorg",
      projectNumber: "42",
    });
  });

  it("should parse user project URL", () => {
    const result = parseProjectUrl("https://github.com/users/username/projects/5");
    expect(result).toEqual({
      scope: "users",
      ownerLogin: "username",
      projectNumber: "5",
    });
  });

  it("should throw error for invalid URL", () => {
    expect(() => parseProjectUrl("invalid-url")).toThrow(/Invalid project URL/);
  });

  it("should throw error for non-string input", () => {
    expect(() => parseProjectUrl(123)).toThrow(/Invalid project input/);
  });
});

describe("getOwnerId", () => {
  it("should get organization ID", async () => {
    mockGithub.graphql.mockResolvedValueOnce({
      organization: {
        id: "org123",
      },
    });

    const result = await getOwnerId("orgs", "myorg");
    expect(result).toBe("org123");
    expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("organization(login: $login)"), { login: "myorg" });
  });

  it("should get user ID", async () => {
    mockGithub.graphql.mockResolvedValueOnce({
      user: {
        id: "user123",
      },
    });

    const result = await getOwnerId("users", "username");
    expect(result).toBe("user123");
    expect(mockGithub.graphql).toHaveBeenCalledWith(expect.stringContaining("user(login: $login)"), { login: "username" });
  });
});

describe("getProjectId", () => {
  it("should get organization project ID", async () => {
    mockGithub.graphql.mockResolvedValueOnce({
      organization: {
        projectV2: {
          id: "project123",
        },
      },
    });

    const result = await getProjectId("orgs", "myorg", "42");
    expect(result).toBe("project123");
  });

  it("should get user project ID", async () => {
    mockGithub.graphql.mockResolvedValueOnce({
      user: {
        projectV2: {
          id: "project456",
        },
      },
    });

    const result = await getProjectId("users", "username", "5");
    expect(result).toBe("project456");
  });
});

describe("copyProject", () => {
  it("should copy project successfully with default includeDraftIssues", async () => {
    // Mock getProjectId for source project
    mockGithub.graphql
      .mockResolvedValueOnce({
        organization: {
          projectV2: {
            id: "sourceProject123",
          },
        },
      })
      // Mock getOwnerId for target owner (org)
      .mockResolvedValueOnce({
        organization: {
          id: "targetOrg123",
        },
      })
      // Mock copyProjectV2 mutation
      .mockResolvedValueOnce({
        copyProjectV2: {
          projectV2: {
            id: "newProject123",
            title: "Copied Project",
            url: "https://github.com/orgs/targetorg/projects/99",
          },
        },
      });

    const result = await copyProject({
      sourceProject: "https://github.com/orgs/sourceorg/projects/42",
      owner: "targetorg",
      title: "Copied Project",
    });

    expect(result).toEqual({
      projectId: "newProject123",
      projectTitle: "Copied Project",
      projectUrl: "https://github.com/orgs/targetorg/projects/99",
    });

    // Verify the mutation was called with includeDraftIssues: false (default)
    expect(mockGithub.graphql).toHaveBeenCalledWith(
      expect.stringContaining("copyProjectV2"),
      expect.objectContaining({
        includeDraftIssues: false,
      })
    );
  });

  it("should copy project with includeDraftIssues set to true", async () => {
    mockGithub.graphql
      .mockResolvedValueOnce({
        organization: {
          projectV2: {
            id: "sourceProject123",
          },
        },
      })
      .mockResolvedValueOnce({
        organization: {
          id: "targetOrg123",
        },
      })
      .mockResolvedValueOnce({
        copyProjectV2: {
          projectV2: {
            id: "newProject123",
            title: "Copied Project With Drafts",
            url: "https://github.com/orgs/targetorg/projects/100",
          },
        },
      });

    const result = await copyProject({
      sourceProject: "https://github.com/orgs/sourceorg/projects/42",
      owner: "targetorg",
      title: "Copied Project With Drafts",
      includeDraftIssues: true,
    });

    expect(result).toEqual({
      projectId: "newProject123",
      projectTitle: "Copied Project With Drafts",
      projectUrl: "https://github.com/orgs/targetorg/projects/100",
    });

    // Verify includeDraftIssues was set to true
    expect(mockGithub.graphql).toHaveBeenCalledWith(
      expect.stringContaining("copyProjectV2"),
      expect.objectContaining({
        includeDraftIssues: true,
      })
    );
  });

  it("should fall back to user when org lookup fails", async () => {
    mockGithub.graphql
      .mockResolvedValueOnce({
        organization: {
          projectV2: {
            id: "sourceProject123",
          },
        },
      })
      // First getOwnerId for org fails
      .mockRejectedValueOnce(new Error("Organization not found"))
      // Second getOwnerId for user succeeds
      .mockResolvedValueOnce({
        user: {
          id: "targetUser123",
        },
      })
      .mockResolvedValueOnce({
        copyProjectV2: {
          projectV2: {
            id: "newProject123",
            title: "User Project",
            url: "https://github.com/users/targetuser/projects/50",
          },
        },
      });

    const result = await copyProject({
      sourceProject: "https://github.com/orgs/sourceorg/projects/42",
      owner: "targetuser",
      title: "User Project",
    });

    expect(result.projectId).toBe("newProject123");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Target owner ID (user)"));
  });

  it("should throw error when source project is missing", async () => {
    await expect(
      copyProject({
        owner: "targetorg",
        title: "Test",
      })
    ).rejects.toThrow(/sourceProject.*required/);
  });

  it("should throw error when owner is missing", async () => {
    await expect(
      copyProject({
        sourceProject: "https://github.com/orgs/sourceorg/projects/42",
        title: "Test",
      })
    ).rejects.toThrow(/owner.*required/);
  });

  it("should throw error when title is missing", async () => {
    await expect(
      copyProject({
        sourceProject: "https://github.com/orgs/sourceorg/projects/42",
        owner: "targetorg",
      })
    ).rejects.toThrow(/title.*required/);
  });

  it("should throw error when both org and user lookup fail", async () => {
    mockGithub.graphql
      .mockResolvedValueOnce({
        organization: {
          projectV2: {
            id: "sourceProject123",
          },
        },
      })
      .mockRejectedValueOnce(new Error("Org not found"))
      .mockRejectedValueOnce(new Error("User not found"));

    await expect(
      copyProject({
        sourceProject: "https://github.com/orgs/sourceorg/projects/42",
        owner: "nonexistent",
        title: "Test",
      })
    ).rejects.toThrow(/Failed to find owner/);

    // Verify that error details were logged
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining('Failed to find "nonexistent" as organization'));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining('Failed to find "nonexistent" as user'));
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("GraphQL Error during"));
  });
});
