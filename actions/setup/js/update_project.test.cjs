import { describe, it, expect, beforeAll, beforeEach, afterEach, vi } from "vitest";

let updateProject;
let parseProjectInput;
let updateProjectHandlerFactory;

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
  request: vi.fn(),
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
  const mod = await import("./update_project.cjs");
  const exports = mod.default || mod;
  updateProject = exports.updateProject;
  parseProjectInput = exports.parseProjectInput;
  updateProjectHandlerFactory = exports.main;
  // Call main to execute the module
  if (exports.main) {
    await exports.main();
  }
});

describe("update_project handler config: field_definitions", () => {
  it("auto-creates configured fields before first message", async () => {
    const callOrder = [];
    const projectUrl = "https://github.com/orgs/testowner/projects/60";

    mockGithub.graphql.mockImplementation(async (query, vars) => {
      const q = String(query);

      if (q.includes("repository(owner:") || q.includes("repository(owner:")) {
        return repoResponse("Organization");
      }
      if (q.includes("viewer") && !vars) {
        return viewerResponse("test-bot");
      }
      if (q.includes("projectV2(number:") && q.includes("organization")) {
        return orgProjectV2Response(projectUrl, 60, "project123", "testowner");
      }
      if (q.includes("createProjectV2Field")) {
        callOrder.push("createField");
        return {
          createProjectV2Field: {
            projectV2Field: {
              id: "field123",
              name: vars?.name || "unknown",
              dataType: vars?.dataType || "TEXT",
              options: [],
            },
          },
        };
      }

      throw new Error(`Unexpected graphql query in test: ${q}`);
    });

    mockGithub.request.mockImplementation(async () => {
      callOrder.push("createView");
      return { data: { id: 999, url: "https://github.com/orgs/testowner/projects/60/views/999" } };
    });

    const handler = await updateProjectHandlerFactory({
      max: 10,
      field_definitions: [{ name: "classification", data_type: "TEXT" }],
    });

    await handler(
      {
        type: "update_project",
        project: projectUrl,
        operation: "create_view",
        view: { name: "Test View", layout: "table" },
      },
      {}
    );

    expect(callOrder).toContain("createField");
    expect(callOrder).toContain("createView");
    expect(callOrder.indexOf("createField")).toBeLessThan(callOrder.indexOf("createView"));
  });
});

describe("update_project token guardrails", () => {
  it("fails fast with a clear error when authenticated as github-actions[bot]", async () => {
    delete process.env.GH_AW_PROJECT_GITHUB_TOKEN;

    const projectUrl = "https://github.com/orgs/github/projects/146";

    mockGithub.graphql.mockImplementation(async (query, vars) => {
      const q = String(query);

      if (q.includes("repository(owner:") || q.includes("repository(owner:")) {
        return repoResponse("Organization");
      }
      if (q.includes("viewer") && !vars) {
        return viewerResponse("github-actions[bot]");
      }

      throw new Error(`Unexpected graphql query in test (should fail fast before project resolution): ${q}`);
    });

    await expect(
      updateProject(
        {
          project: projectUrl,
          content_type: "issue",
          content_number: 1,
        },
        new Map(),
        mockGithub
      )
    ).rejects.toThrow(/Projects v2 operations require.*github-actions\[bot\].*GH_AW_PROJECT_GITHUB_TOKEN/i);
  });
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
  mockGithub.request.mockReset();
  mockGithub.rest.issues.addLabels.mockClear();
  clearCoreMocks();
  vi.useRealTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

const repoResponse = (ownerType = "Organization") => ({
  repository: {
    id: "repo123",
    owner: {
      id: ownerType === "User" ? "owner-user-123" : "owner123",
      __typename: ownerType,
    },
  },
});

const viewerResponse = (login = "test-bot") => ({
  viewer: {
    login,
  },
});

const orgProjectV2Response = (url, number = 60, id = "project123", orgLogin = "testowner") => ({
  organization: {
    projectV2: {
      id,
      number,
      title: "Test Project",
      url,
      owner: { __typename: "Organization", login: orgLogin },
    },
  },
});

const userProjectV2Response = (url, number = 60, id = "project123", userLogin = "testowner") => ({
  user: {
    projectV2: {
      id,
      number,
      title: "Test Project",
      url,
      owner: { __typename: "User", login: userLogin },
    },
  },
});

const orgProjectNullResponse = () => ({ organization: { projectV2: null } });
const userProjectNullResponse = () => ({ user: { projectV2: null } });

const issueResponse = (id, body = null) => ({ repository: { issue: { id, body } } });

const pullRequestResponse = (id, body = null) => ({ repository: { pullRequest: { id, body } } });

const emptyItemsResponse = () => ({
  node: {
    items: {
      nodes: [],
      pageInfo: { hasNextPage: false, endCursor: null },
    },
  },
});

const existingItemResponse = (contentId, itemId = "existing-item") => ({
  node: {
    items: {
      nodes: [{ id: itemId, content: { id: contentId } }],
      pageInfo: { hasNextPage: false, endCursor: null },
    },
  },
});

const fieldsResponse = nodes => ({ node: { fields: { nodes } } });

const updateFieldValueResponse = () => ({
  updateProjectV2ItemFieldValue: {
    projectV2Item: {
      id: "item123",
    },
  },
});

const addDraftIssueResponse = (itemId = "draft-item") => ({
  addProjectV2DraftIssue: {
    projectItem: {
      id: itemId,
    },
  },
});

const existingDraftItemResponse = (title, itemId = "existing-draft-item") => ({
  node: {
    items: {
      nodes: [
        {
          id: itemId,
          content: {
            __typename: "DraftIssue",
            id: `draft-content-${itemId}`,
            title: title,
          },
        },
      ],
      pageInfo: { hasNextPage: false, endCursor: null },
    },
  },
});

function queueResponses(responses) {
  responses.forEach(response => {
    mockGithub.graphql.mockResolvedValueOnce(response);
  });
}

function getOutput(name) {
  const call = mockCore.setOutput.mock.calls.find(([key]) => key === name);
  return call ? call[1] : undefined;
}

describe("parseProjectInput", () => {
  it("extracts the project number from a GitHub URL", () => {
    expect(parseProjectInput("https://github.com/orgs/acme/projects/42")).toBe("42");
  });

  it("rejects a numeric string", () => {
    expect(() => parseProjectInput("17")).toThrow(/full GitHub project URL/);
  });

  it("rejects a project name", () => {
    expect(() => parseProjectInput("Engineering Roadmap")).toThrow(/full GitHub project URL/);
  });

  it("throws when the project input is missing", () => {
    expect(() => parseProjectInput(undefined)).toThrow(/Invalid project input/);
  });
});

describe("updateProject", () => {
  it("creates a view for an org-owned project", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      operation: "create_view",
      view: {
        name: "Sprint Board",
        layout: "board",
        filter: "is:issue is:open label:sprint",
        visible_fields: [123, 456, 789],
        description: "Optional description (ignored)",
      },
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-view")]);
    mockGithub.request.mockResolvedValueOnce({ data: { id: 101, name: "Sprint Board" } });

    await updateProject(output);

    expect(mockGithub.request).toHaveBeenCalledWith(
      "POST /orgs/{org}/projectsV2/{project_number}/views",
      expect.objectContaining({
        org: "testowner",
        project_number: 60,
        name: "Sprint Board",
        layout: "board",
        filter: "is:issue is:open label:sprint",
        visible_fields: [123, 456, 789],
      })
    );

    expect(getOutput("view-id")).toBe(101);
  });

  it("creates a view for a user-owned project", async () => {
    const projectUrl = "https://github.com/users/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      operation: "create_view",
      view: {
        name: "All Issues",
        layout: "table",
        filter: "is:issue",
      },
    };

    queueResponses([repoResponse(), viewerResponse(), userProjectV2Response(projectUrl, 60, "project-user-view")]);
    mockGithub.request.mockResolvedValueOnce({ data: { id: 202, name: "All Issues" } });

    await updateProject(output);

    expect(mockGithub.request).toHaveBeenCalledWith(
      "POST /users/{user_id}/projectsV2/{project_number}/views",
      expect.objectContaining({
        user_id: "testowner",
        project_number: 60,
        name: "All Issues",
        layout: "table",
        filter: "is:issue",
      })
    );

    expect(getOutput("view-id")).toBe(202);
  });

  it("ignores visible_fields for roadmap views", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      operation: "create_view",
      view: {
        name: "Product Roadmap",
        layout: "roadmap",
        visible_fields: [123],
      },
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-roadmap")]);
    mockGithub.request.mockResolvedValueOnce({ data: { id: 303, name: "Product Roadmap" } });

    await updateProject(output);

    const callArgs = mockGithub.request.mock.calls[0][1];
    expect(callArgs).toEqual(
      expect.objectContaining({
        org: "testowner",
        project_number: 60,
        name: "Product Roadmap",
        layout: "roadmap",
      })
    );
    expect(callArgs.visible_fields).toBeUndefined();
  });

  it("rejects project URL when project not found", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/99";

    const output = { type: "update_project", project: projectUrl };

    queueResponses([repoResponse(), viewerResponse(), orgProjectNullResponse()]);

    await expect(updateProject(output)).rejects.toThrow(/not found or not accessible/);
  });

  it("adds an issue to a project board", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = { type: "update_project", project: projectUrl, content_type: "issue", content_number: 42 };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project123"), issueResponse("issue-id-42"), emptyItemsResponse(), { addProjectV2ItemById: { item: { id: "item123" } } }]);

    await updateProject(output);

    // update_project no longer adds labels as a side effect
    expect(mockGithub.rest.issues.addLabels).not.toHaveBeenCalled();
    expect(getOutput("item-id")).toBe("item123");
  });

  it("adds a draft issue to a project board", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_title: "Draft title",
      draft_body: "Draft body",
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-draft"),
      emptyItemsResponse(), // No existing drafts with this title
      addDraftIssueResponse("draft-item-1"),
    ]);

    await updateProject(output);

    expect(mockGithub.graphql.mock.calls.some(([query]) => query.includes("addProjectV2DraftIssue"))).toBe(true);
    expect(mockGithub.rest.issues.addLabels).not.toHaveBeenCalled();
    expect(getOutput("item-id")).toBe("draft-item-1");
    expect(mockCore.info).toHaveBeenCalledWith('✓ Created new draft issue "Draft title"');
  });

  it("adds a draft issue when agent emits camelCase keys", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const title = "Test *draft issue* for `smoke-project`";

    const output = {
      type: "update_project",
      project: projectUrl,
      contentType: "draft_issue",
      draftTitle: title,
      draftBody: "Body",
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-draft"),
      emptyItemsResponse(), // No existing drafts with this title
      addDraftIssueResponse("draft-item-camel"),
    ]);

    await updateProject(output);

    expect(mockGithub.graphql.mock.calls.some(([query]) => query.includes("addProjectV2DraftIssue"))).toBe(true);
    expect(getOutput("item-id")).toBe("draft-item-camel");
    expect(mockCore.info).toHaveBeenCalledWith(`✓ Created new draft issue "${title}"`);
  });

  it("returns temporary_id when draft issue is created with temporary_id", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const temporaryId = "aw_abc123def456";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_title: "Draft with temp ID",
      draft_body: "Draft body",
      temporary_id: temporaryId,
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-draft"),
      emptyItemsResponse(), // No existing drafts with this title
      addDraftIssueResponse("draft-item-2"),
    ]);

    const result = await updateProject(output);

    expect(result).toBeDefined();
    expect(result.temporaryId).toBe(temporaryId);
    expect(result.draftItemId).toBe("draft-item-2");
    expect(getOutput("item-id")).toBe("draft-item-2");
    expect(getOutput("temporary-id")).toBe(temporaryId);
    expect(mockCore.info).toHaveBeenCalledWith(`✓ Stored temporary_id mapping: ${temporaryId} -> draft-item-2`);
  });

  it("rejects draft issues without a title", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_title: "   ",
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-draft")]);

    await expect(updateProject(output)).rejects.toThrow(/draft_title/);
  });

  it("reuses existing draft issue instead of creating duplicate", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_title: "Existing Draft",
      fields: { Status: "In Progress" },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-draft"),
      existingDraftItemResponse("Existing Draft", "existing-draft-123"), // Draft with same title exists
      fieldsResponse([{ id: "field-status", name: "Status" }]),
      updateFieldValueResponse(),
    ]);

    await updateProject(output);

    // Should NOT call addProjectV2DraftIssue since draft already exists
    expect(mockGithub.graphql.mock.calls.some(([query]) => query.includes("addProjectV2DraftIssue"))).toBe(false);
    // Should call updateProjectV2ItemFieldValue to update the existing draft
    expect(mockGithub.graphql.mock.calls.some(([query]) => query.includes("updateProjectV2ItemFieldValue"))).toBe(true);
    expect(mockGithub.rest.issues.addLabels).not.toHaveBeenCalled();
    expect(getOutput("item-id")).toBe("existing-draft-123");
    expect(mockCore.info).toHaveBeenCalledWith('✓ Found existing draft issue "Existing Draft" - updating fields instead of creating duplicate');
  });

  it("creates draft issue with temporary_id and stores mapping", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const temporaryIdMap = new Map();
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_title: "Draft with temp ID",
      draft_body: "Body content",
      temporary_id: "aw_9f11121ed7df",
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-draft"),
      emptyItemsResponse(), // No existing drafts
      addDraftIssueResponse("draft-item-temp"),
    ]);

    await updateProject(output, temporaryIdMap);

    expect(mockGithub.graphql.mock.calls.some(([query]) => query.includes("addProjectV2DraftIssue"))).toBe(true);
    expect(getOutput("item-id")).toBe("draft-item-temp");
    expect(getOutput("temporary-id")).toBe("aw_9f11121ed7df");
    expect(temporaryIdMap.get("aw_9f11121ed7df")).toEqual({ draftItemId: "draft-item-temp" });
    expect(mockCore.info).toHaveBeenCalledWith('✓ Created new draft issue "Draft with temp ID"');
    expect(mockCore.info).toHaveBeenCalledWith("✓ Stored temporary_id mapping: aw_9f11121ed7df -> draft-item-temp");
  });

  it("creates draft issue with temporary_id (with # prefix) and strips prefix", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const temporaryIdMap = new Map();
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_title: "Draft with hash prefix",
      temporary_id: "#aw_abc123def456",
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-draft"), emptyItemsResponse(), addDraftIssueResponse("draft-item-hash")]);

    await updateProject(output, temporaryIdMap);

    expect(getOutput("temporary-id")).toBe("aw_abc123def456");
    expect(temporaryIdMap.get("aw_abc123def456")).toEqual({ draftItemId: "draft-item-hash" });
    expect(mockCore.info).toHaveBeenCalledWith("✓ Stored temporary_id mapping: aw_abc123def456 -> draft-item-hash");
  });

  it("updates draft issue via draft_issue_id using temporary ID map", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const temporaryIdMap = new Map();
    temporaryIdMap.set("aw_9f11121ed7df", { draftItemId: "draft-item-existing" });

    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_issue_id: "aw_9f11121ed7df",
      fields: { Priority: "High" },
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-draft"), fieldsResponse([{ id: "field-priority", name: "Priority" }]), updateFieldValueResponse()]);

    await updateProject(output, temporaryIdMap);

    // Should NOT create new draft
    expect(mockGithub.graphql.mock.calls.some(([query]) => query.includes("addProjectV2DraftIssue"))).toBe(false);
    // Should update fields on existing draft
    expect(mockGithub.graphql.mock.calls.some(([query]) => query.includes("updateProjectV2ItemFieldValue"))).toBe(true);
    expect(getOutput("item-id")).toBe("draft-item-existing");
    expect(mockCore.info).toHaveBeenCalledWith('✓ Resolved draft_issue_id "aw_9f11121ed7df" to item draft-item-existing');
  });

  it("updates draft issue via draft_issue_id with # prefix", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const temporaryIdMap = new Map();
    temporaryIdMap.set("aw_abc123def456", { draftItemId: "draft-item-ref" });

    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_issue_id: "#aw_abc123def456",
      fields: { Status: "Done" },
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-draft"), fieldsResponse([{ id: "field-status", name: "Status" }]), updateFieldValueResponse()]);

    await updateProject(output, temporaryIdMap);

    expect(getOutput("item-id")).toBe("draft-item-ref");
    expect(mockCore.info).toHaveBeenCalledWith('✓ Resolved draft_issue_id "aw_abc123def456" to item draft-item-ref');
  });

  it("returns temporaryId and draftItemId when updating draft issue via draft_issue_id", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const draftIssueId = "aw_9f11121ed7df";
    const temporaryIdMap = new Map();
    temporaryIdMap.set(draftIssueId, { draftItemId: "draft-item-existing" });

    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_issue_id: draftIssueId,
      fields: { Status: "In Progress" },
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-draft"), fieldsResponse([{ id: "field-status", name: "Status" }]), updateFieldValueResponse()]);

    const result = await updateProject(output, temporaryIdMap);

    // Verify the function returns the temporary ID mapping for the handler manager
    expect(result).toBeDefined();
    expect(result.temporaryId).toBe(draftIssueId);
    expect(result.draftItemId).toBe("draft-item-existing");
    expect(getOutput("temporary-id")).toBe(draftIssueId);
  });

  it("falls back to title lookup when draft_issue_id not in map but title provided", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const temporaryIdMap = new Map(); // Empty map

    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_issue_id: "aw_aefe5b4b9585",
      draft_title: "Fallback Draft",
      fields: { Status: "In Progress" },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-draft"),
      existingDraftItemResponse("Fallback Draft", "draft-item-fallback"),
      fieldsResponse([{ id: "field-status", name: "Status" }]),
      updateFieldValueResponse(),
    ]);

    await updateProject(output, temporaryIdMap);

    expect(getOutput("item-id")).toBe("draft-item-fallback");
    expect(mockCore.info).toHaveBeenCalledWith('✓ Found draft issue "Fallback Draft" by title fallback');
  });

  it("throws error when draft_issue_id not found and no title for fallback", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const temporaryIdMap = new Map();

    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_issue_id: "aw_1a2b3c4d5e6f",
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-draft")]);

    await expect(updateProject(output, temporaryIdMap)).rejects.toThrow(/draft_issue_id.*not found.*no draft_title/);
  });

  it("throws error when draft_issue_id not in map and title not found", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const temporaryIdMap = new Map();

    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_issue_id: "aw_27a9a9bfcc4e",
      draft_title: "Non-existent Draft",
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-draft"),
      emptyItemsResponse(), // No drafts found
    ]);

    await expect(updateProject(output, temporaryIdMap)).rejects.toThrow(/draft_issue_id.*not found.*no draft with title/);
  });

  it("allows user-friendly temporary_id like 'draft-1' when creating draft", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const temporaryIdMap = new Map();
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_title: "User Friendly Draft",
      temporary_id: "draft-1",
      fields: { Priority: "High" },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-draft"),
      emptyItemsResponse(),
      addDraftIssueResponse("draft-item-friendly"),
      fieldsResponse([{ id: "field-priority", name: "Priority" }]),
      updateFieldValueResponse(),
    ]);

    const result = await updateProject(output, temporaryIdMap);

    expect(result).toBeDefined();
    expect(result.temporaryId).toBe("draft-1");
    expect(result.draftItemId).toBe("draft-item-friendly");
    expect(temporaryIdMap.get("draft-1")).toEqual({ draftItemId: "draft-item-friendly" });
    expect(mockCore.info).toHaveBeenCalledWith("✓ Stored temporary_id mapping: draft-1 -> draft-item-friendly");
  });

  it("allows user-friendly draft_issue_id like 'draft-1' when updating draft", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const temporaryIdMap = new Map();
    temporaryIdMap.set("draft-1", { draftItemId: "draft-item-friendly" });

    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_issue_id: "draft-1",
      fields: { Status: "In Progress" },
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-draft"), fieldsResponse([{ id: "field-status", name: "Status" }]), updateFieldValueResponse()]);

    const result = await updateProject(output, temporaryIdMap);

    expect(result).toBeDefined();
    expect(result.temporaryId).toBe("draft-1");
    expect(result.draftItemId).toBe("draft-item-friendly");
    expect(mockCore.info).toHaveBeenCalledWith('✓ Resolved draft_issue_id "draft-1" to item draft-item-friendly');
  });

  it("rejects malformed auto-generated temporary_id with aw_ prefix", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_title: "Test Draft",
      temporary_id: "aw_invalid",
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-draft")]);

    await expect(updateProject(output)).rejects.toThrow(/Invalid temporary_id format.*aw_ followed by 12 hex characters/);
  });

  it("rejects malformed auto-generated draft_issue_id with aw_ prefix", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const temporaryIdMap = new Map();
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_issue_id: "aw_bad",
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-draft")]);

    await expect(updateProject(output, temporaryIdMap)).rejects.toThrow(/Invalid draft_issue_id format.*aw_ followed by 12 hex characters/);
  });

  it("rejects draft_issue without title when creating (no draft_issue_id)", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      // No draft_title, no draft_issue_id
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-draft")]);

    await expect(updateProject(output)).rejects.toThrow(/draft_title.*required/);
  });

  it("skips adding an issue that already exists on the board", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = { type: "update_project", project: projectUrl, content_type: "issue", content_number: 99 };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project123"), issueResponse("issue-id-99"), existingItemResponse("issue-id-99", "item-existing")]);

    await updateProject(output);

    expect(mockGithub.rest.issues.addLabels).not.toHaveBeenCalled();
    expect(mockCore.info).toHaveBeenCalledWith("✓ Item already on board");
    expect(getOutput("item-id")).toBe("item-existing");
  });

  it("adds a pull request to the project board", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = { type: "update_project", project: projectUrl, content_type: "pull_request", content_number: 17 };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-pr"), pullRequestResponse("pr-id-17"), emptyItemsResponse(), { addProjectV2ItemById: { item: { id: "pr-item" } } }]);

    await updateProject(output);

    // update_project no longer adds labels as a side effect
    expect(mockGithub.rest.issues.addLabels).not.toHaveBeenCalled();
  });

  it("falls back to legacy issue field when content_number missing", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = { type: "update_project", project: projectUrl, issue: "101" };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "legacy-project"), issueResponse("issue-id-101"), emptyItemsResponse(), { addProjectV2ItemById: { item: { id: "legacy-item" } } }]);

    await updateProject(output);

    expect(mockCore.warning).toHaveBeenCalledWith('Field "issue" deprecated; use "content_number" instead.');

    // update_project no longer adds labels as a side effect
    expect(mockGithub.rest.issues.addLabels).not.toHaveBeenCalled();
    expect(getOutput("item-id")).toBe("legacy-item");
  });

  it("rejects invalid content numbers", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = { type: "update_project", project: projectUrl, content_number: "ABC" };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "invalid-project")]);

    await expect(updateProject(output)).rejects.toThrow(/Invalid content number/);
  });

  it("resolves temporary IDs in content_number", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: "aw_abc123def456",
    };

    // Create temporary ID map with the mapping
    const temporaryIdMap = new Map([["aw_abc123def456", { repo: "testowner/testrepo", number: 42 }]]);

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project123"), issueResponse("issue-id-42"), emptyItemsResponse(), { addProjectV2ItemById: { item: { id: "item123" } } }]);

    await updateProject(output, temporaryIdMap);

    // Verify that the temporary ID was resolved and the issue was added
    const getOutput = key => {
      const calls = mockCore.setOutput.mock.calls;
      const call = calls.find(c => c[0] === key);
      return call ? call[1] : undefined;
    };

    expect(getOutput("item-id")).toBe("item123");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved temporary ID aw_abc123def456 to issue #42"));
  });

  it("rejects unresolved temporary IDs in content_number", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: "aw_abc123def789", // Valid format but not in map
    };

    const temporaryIdMap = new Map(); // Empty map - ID not resolved

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project123")]);

    await expect(updateProject(output, temporaryIdMap)).rejects.toThrow(/Temporary ID 'aw_abc123def789' not found in map/);
  });

  it("updates an existing text field", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 10,
      fields: { Status: "In Progress" },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-field"),
      issueResponse("issue-id-10"),
      existingItemResponse("issue-id-10", "item-field"),
      fieldsResponse([{ id: "field-status", name: "Status" }]),
      updateFieldValueResponse(),
    ]);

    await updateProject(output);

    const updateCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateCall).toBeDefined();
    expect(mockGithub.rest.issues.addLabels).not.toHaveBeenCalled();
  });

  it("updates fields on a draft issue item", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "draft_issue",
      draft_title: "Draft title",
      fields: { Status: "In Progress" },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-draft-fields"),
      emptyItemsResponse(), // No existing drafts with this title
      addDraftIssueResponse("draft-item-fields"),
      fieldsResponse([{ id: "field-status", name: "Status" }]),
      updateFieldValueResponse(),
    ]);

    await updateProject(output);

    const updateCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateCall).toBeDefined();
    expect(mockGithub.rest.issues.addLabels).not.toHaveBeenCalled();
    expect(getOutput("item-id")).toBe("draft-item-fields");
  });

  it("updates a single select field when the option exists", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 15,
      fields: { Priority: "High" },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-priority"),
      issueResponse("issue-id-15"),
      existingItemResponse("issue-id-15", "item-priority"),
      fieldsResponse([
        {
          id: "field-priority",
          name: "Priority",
          options: [
            { id: "opt-low", name: "Low" },
            { id: "opt-high", name: "High" },
          ],
        },
      ]),
      updateFieldValueResponse(),
    ]);

    await updateProject(output);

    const updateCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateCall).toBeDefined();
  });

  it("warns when attempting to add a new option to a single select field", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 16,
      fields: { Status: "Closed - Not Planned" },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-status"),
      issueResponse("issue-id-16"),
      existingItemResponse("issue-id-16", "item-status"),
      fieldsResponse([
        {
          id: "field-status",
          name: "Status",
          options: [
            { id: "opt-todo", name: "Todo", color: "GRAY" },
            { id: "opt-in-progress", name: "In Progress", color: "YELLOW" },
            { id: "opt-done", name: "Done", color: "GREEN" },
            { id: "opt-closed", name: "Closed", color: "PURPLE" },
          ],
        },
      ]),
    ]);

    await updateProject(output);

    // The updateProjectV2Field mutation does not exist in GitHub's API
    // Verify that no attempt was made to call it
    const updateFieldCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2Field"));
    expect(updateFieldCall).toBeUndefined();

    // Verify that a warning was logged about the missing option
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Option "Closed - Not Planned" not found in field "Status"'));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Available options: Todo, In Progress, Done, Closed"));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("please update the field manually in the GitHub Projects UI"));
  });

  it("warns when a field cannot be created", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 20,
      fields: { NonExistentField: "Some Value" },
    };

    queueResponses([repoResponse(), viewerResponse(), orgProjectV2Response(projectUrl, 60, "project-test"), issueResponse("issue-id-20"), existingItemResponse("issue-id-20", "item-test"), fieldsResponse([])]);

    mockGithub.graphql.mockRejectedValueOnce(new Error("Failed to create field"));

    await updateProject(output);

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Failed to create field "NonExistentField"'));
  });

  it("rejects non-URL project identifier", async () => {
    const output = { type: "update_project", project: "Engineering Roadmap" };
    await expect(updateProject(output)).rejects.toThrow(/full GitHub project URL/);
  });

  it("correctly identifies DATE fields and uses date format (not singleSelectOptionId)", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 75,
      fields: {
        deadline: "2025-12-31",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-date-field"),
      issueResponse("issue-id-75"),
      existingItemResponse("issue-id-75", "item-date-field"),
      // DATE field with dataType explicitly set to "DATE"
      // This tests that the code checks dataType before checking for options
      fieldsResponse([{ id: "field-deadline", name: "Deadline", dataType: "DATE" }]),
      updateFieldValueResponse(),
    ]);

    await updateProject(output);

    // Verify the field value is set using date format, not singleSelectOptionId
    const updateCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateCall).toBeDefined();
    expect(updateCall[1].value).toEqual({ date: "2025-12-31" });
    // Explicitly verify it's NOT using singleSelectOptionId
    expect(updateCall[1].value).not.toHaveProperty("singleSelectOptionId");
  });

  it("correctly handles NUMBER fields with numeric values", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 80,
      fields: {
        story_points: 5,
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-number-field"),
      issueResponse("issue-id-80"),
      existingItemResponse("issue-id-80", "item-number-field"),
      fieldsResponse([{ id: "field-story-points", name: "Story Points", dataType: "NUMBER" }]),
      updateFieldValueResponse(),
    ]);

    await updateProject(output);

    const updateCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateCall).toBeDefined();
    expect(updateCall[1].value).toEqual({ number: 5 });
  });

  it("correctly converts string to number for NUMBER fields", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 81,
      fields: {
        story_points: "8.5",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-number-field"),
      issueResponse("issue-id-81"),
      existingItemResponse("issue-id-81", "item-number-field-string"),
      fieldsResponse([{ id: "field-story-points", name: "Story Points", dataType: "NUMBER" }]),
      updateFieldValueResponse(),
    ]);

    await updateProject(output);

    const updateCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateCall).toBeDefined();
    expect(updateCall[1].value).toEqual({ number: 8.5 });
  });

  it("handles invalid NUMBER field values with warning", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 82,
      fields: {
        story_points: "not-a-number",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-number-field"),
      issueResponse("issue-id-82"),
      existingItemResponse("issue-id-82", "item-number-field-invalid"),
      fieldsResponse([{ id: "field-story-points", name: "Story Points", dataType: "NUMBER" }]),
    ]);

    await updateProject(output);

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Invalid number value "not-a-number"'));
  });

  it("correctly handles ITERATION fields by matching title", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 85,
      fields: {
        sprint: "Sprint 42",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-iteration-field"),
      issueResponse("issue-id-85"),
      existingItemResponse("issue-id-85", "item-iteration-field"),
      fieldsResponse([
        {
          id: "field-sprint",
          name: "Sprint",
          dataType: "ITERATION",
          configuration: {
            iterations: [
              { id: "iter-41", title: "Sprint 41", startDate: "2026-01-01", duration: 2 },
              { id: "iter-42", title: "Sprint 42", startDate: "2026-01-15", duration: 2 },
              { id: "iter-43", title: "Sprint 43", startDate: "2026-01-29", duration: 2 },
            ],
          },
        },
      ]),
      updateFieldValueResponse(),
    ]);

    await updateProject(output);

    const updateCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateCall).toBeDefined();
    expect(updateCall[1].value).toEqual({ iterationId: "iter-42" });
  });

  it("handles case-insensitive iteration title matching", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 86,
      fields: {
        sprint: "sprint 42",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-iteration-field"),
      issueResponse("issue-id-86"),
      existingItemResponse("issue-id-86", "item-iteration-field-case"),
      fieldsResponse([
        {
          id: "field-sprint",
          name: "Sprint",
          dataType: "ITERATION",
          configuration: {
            iterations: [{ id: "iter-42", title: "Sprint 42", startDate: "2026-01-15", duration: 2 }],
          },
        },
      ]),
      updateFieldValueResponse(),
    ]);

    await updateProject(output);

    const updateCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateCall).toBeDefined();
    expect(updateCall[1].value).toEqual({ iterationId: "iter-42" });
  });

  it("handles ITERATION field with non-existent iteration with warning", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 87,
      fields: {
        sprint: "Sprint 99",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-iteration-field"),
      issueResponse("issue-id-87"),
      existingItemResponse("issue-id-87", "item-iteration-field-missing"),
      fieldsResponse([
        {
          id: "field-sprint",
          name: "Sprint",
          dataType: "ITERATION",
          configuration: {
            iterations: [
              { id: "iter-41", title: "Sprint 41", startDate: "2026-01-01", duration: 2 },
              { id: "iter-42", title: "Sprint 42", startDate: "2026-01-15", duration: 2 },
            ],
          },
        },
      ]),
    ]);

    await updateProject(output);

    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Iteration "Sprint 99" not found'));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Available iterations: Sprint 41, Sprint 42"));
  });

  it("creates a new DATE field when field doesn't exist and value is in YYYY-MM-DD format", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 90,
      fields: {
        start_date: "2026-01-15",
        end_date: "2026-02-28",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-create-date-field"),
      issueResponse("issue-id-90"),
      existingItemResponse("issue-id-90", "item-create-date-field"),
      // No existing fields - will need to create them
      fieldsResponse([]),
      // Response for creating start_date field as DATE type
      {
        createProjectV2Field: {
          projectV2Field: {
            id: "field-start-date",
            name: "Start Date",
            dataType: "DATE",
          },
        },
      },
      updateFieldValueResponse(),
      // Response for creating end_date field as DATE type
      {
        createProjectV2Field: {
          projectV2Field: {
            id: "field-end-date",
            name: "End Date",
            dataType: "DATE",
          },
        },
      },
      updateFieldValueResponse(),
    ]);

    await updateProject(output);

    // Verify that DATE fields were created (not SINGLE_SELECT)
    const createCalls = mockGithub.graphql.mock.calls.filter(([query]) => query.includes("createProjectV2Field"));
    expect(createCalls.length).toBe(2);

    // Check that both fields were created with DATE type
    expect(createCalls[0][1].dataType).toBe("DATE");
    expect(createCalls[0][1].name).toBe("Start Date");
    expect(createCalls[1][1].dataType).toBe("DATE");
    expect(createCalls[1][1].name).toBe("End Date");

    // Verify the field values were set using date format
    const updateCalls = mockGithub.graphql.mock.calls.filter(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateCalls.length).toBe(2);
    expect(updateCalls[0][1].value).toEqual({ date: "2026-01-15" });
    expect(updateCalls[1][1].value).toEqual({ date: "2026-02-28" });
  });

  it("warns when date field name is detected but value is not in YYYY-MM-DD format", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 91,
      fields: {
        start_date: "January 15, 2026",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-invalid-date-format"),
      issueResponse("issue-id-91"),
      existingItemResponse("issue-id-91", "item-invalid-date"),
      // No existing fields
      fieldsResponse([]),
    ]);

    await updateProject(output);

    // Verify a warning was logged about the invalid date format
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Field "start_date" looks like a date field but value "January 15, 2026" is not in YYYY-MM-DD format'));
  });

  it("warns and skips when field name conflicts with unsupported built-in type (REPOSITORY)", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 95,
      fields: {
        repository: "github/gh-aw",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-repository-conflict"),
      issueResponse("issue-id-95"),
      existingItemResponse("issue-id-95", "item-repository-conflict"),
      // No existing fields - would try to create if not blocked
      fieldsResponse([]),
    ]);

    await updateProject(output);

    // Verify a warning was logged about the unsupported built-in type
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Field "repository" conflicts with unsupported GitHub built-in field type REPOSITORY'));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Please use a different field name (e.g., "repo", "source_repository", "linked_repo")'));

    // Verify that no attempt was made to create the field
    const createFieldCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("createProjectV2Field"));
    expect(createFieldCall).toBeUndefined();

    // Verify that no attempt was made to update the field value
    const updateFieldCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateFieldCall).toBeUndefined();
  });

  it("warns and skips when existing field has unsupported built-in type (REPOSITORY)", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 96,
      fields: {
        repository: "github/gh-aw",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-repository-existing"),
      issueResponse("issue-id-96"),
      existingItemResponse("issue-id-96", "item-repository-existing"),
      // Field already exists with REPOSITORY type
      fieldsResponse([{ id: "field-repository", name: "Repository", dataType: "REPOSITORY" }]),
    ]);

    await updateProject(output);

    // When the field NAME "repository" is used, it's caught by the name check before type checking
    // This is correct because "repository" normalizes to "Repository" which uppercases to "REPOSITORY"
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Field "repository" conflicts with unsupported GitHub built-in field type REPOSITORY'));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Please use a different field name"));

    // Verify that no attempt was made to update the field value
    const updateFieldCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateFieldCall).toBeUndefined();
  });

  it("warns and skips when existing field has REPOSITORY dataType with different name", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 97,
      fields: {
        // Using "repo" as field name, but it's actually a REPOSITORY type field in the project
        repo: "github/gh-aw",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-repo-datatype"),
      issueResponse("issue-id-97"),
      existingItemResponse("issue-id-97", "item-repo-datatype"),
      // Field exists as "Repo" with REPOSITORY dataType (GitHub auto-created it as REPOSITORY type)
      fieldsResponse([{ id: "field-repo", name: "Repo", dataType: "REPOSITORY" }]),
    ]);

    await updateProject(output);

    // When a field EXISTS with REPOSITORY dataType (but name doesn't match "repository"),
    // the type mismatch check should catch it and show the special REPOSITORY warning
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('Field type mismatch for "repo": Expected SINGLE_SELECT but found REPOSITORY'));
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining('The field "REPOSITORY" is a GitHub built-in type that is not supported for updates via the API'));

    // Verify that no attempt was made to update the field value
    const updateFieldCall = mockGithub.graphql.mock.calls.find(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateFieldCall).toBeUndefined();
  });

  it("creates classification field as TEXT type (not SINGLE_SELECT)", async () => {
    const projectUrl = "https://github.com/orgs/testowner/projects/60";
    const output = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 100,
      fields: {
        classification: "high",
      },
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(projectUrl, 60, "project-id-60"),
      issueResponse("issue-id-100"),
      existingItemResponse("issue-id-100", "item-id-100"),
      // No existing fields - will need to create Classification as TEXT
      fieldsResponse([]),
      // Response for creating Classification field as TEXT type
      {
        createProjectV2Field: {
          projectV2Field: {
            id: "field-id-classification",
            name: "Classification",
          },
        },
      },
      updateFieldValueResponse(),
    ]);

    await updateProject(output);

    // Verify that field was created with TEXT type (not SINGLE_SELECT)
    const createCalls = mockGithub.graphql.mock.calls.filter(([query]) => query.includes("createProjectV2Field"));
    expect(createCalls.length).toBe(1);

    // Check that the field was created with TEXT dataType
    expect(createCalls[0][1].dataType).toBe("TEXT");
    expect(createCalls[0][1].name).toBe("Classification");
    // Verify that singleSelectOptions was NOT provided (which would indicate SINGLE_SELECT)
    expect(createCalls[0][1].singleSelectOptions).toBeUndefined();

    // Verify the field value was set using text format
    const updateCalls = mockGithub.graphql.mock.calls.filter(([query]) => query.includes("updateProjectV2ItemFieldValue"));
    expect(updateCalls.length).toBe(1);
    expect(updateCalls[0][1].value).toEqual({ text: "high" });
  });

  it("should reject update_project message with missing project field", async () => {
    const messageHandler = await updateProjectHandlerFactory({});

    const messageWithoutProject = {
      type: "update_project",
      content_type: "draft_issue",
      draft_title: "Test Draft Issue",
      draft_body: "This is a test",
      fields: {
        status: "Todo",
      },
      // Missing "project" field - this should fail
    };

    const result = await messageHandler(messageWithoutProject, new Map());

    expect(result.success).toBe(false);
    expect(result.error).toContain('Missing required "project" field');
    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Missing required"));
  });

  it("should reject update_project message with empty project field", async () => {
    const messageHandler = await updateProjectHandlerFactory({});

    const messageWithEmptyProject = {
      type: "update_project",
      project: "",
      content_type: "issue",
      content_number: 123,
      fields: {
        status: "Todo",
      },
    };

    const result = await messageHandler(messageWithEmptyProject, new Map());

    expect(result.success).toBe(false);
    expect(result.error).toContain('Missing required "project" field');
    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining("Missing required"));
  });

  it("should fail when project field is missing even if GH_AW_PROJECT_URL is set", async () => {
    // Set default project URL in environment (should be ignored)
    const defaultProjectUrl = "https://github.com/orgs/testowner/projects/60";
    process.env.GH_AW_PROJECT_URL = defaultProjectUrl;

    const messageHandler = await updateProjectHandlerFactory({});

    const messageWithoutProject = {
      type: "update_project",
      content_type: "draft_issue",
      draft_title: "Test Draft Issue",
      draft_body: "This is a test",
    };

    const result = await messageHandler(messageWithoutProject, new Map());

    expect(result.success).toBe(false);
    expect(result.error).toBe('Missing required "project" field. The agent must explicitly include the project URL in the output message.');
    expect(mockCore.error).toHaveBeenCalledWith(expect.stringContaining('Missing required "project" field'));

    // Cleanup
    delete process.env.GH_AW_PROJECT_URL;
  });

  it("should succeed when project field is explicitly provided", async () => {
    // Set default project URL in environment (should be ignored since message has explicit project)
    process.env.GH_AW_PROJECT_URL = "https://github.com/orgs/testowner/projects/999";

    const messageHandler = await updateProjectHandlerFactory({});

    const messageProjectUrl = "https://github.com/orgs/testowner/projects/60";
    const messageWithProject = {
      type: "update_project",
      project: messageProjectUrl,
      content_type: "draft_issue",
      draft_title: "Test Draft Issue",
      draft_body: "This is a test",
    };

    queueResponses([
      repoResponse(),
      viewerResponse(),
      orgProjectV2Response(messageProjectUrl, 60, "project-message"),
      emptyItemsResponse(), // No existing drafts with this title
      addDraftIssueResponse("draft-item-message"),
    ]);

    const result = await messageHandler(messageWithProject, new Map());

    expect(result.success).toBe(true);
    expect(getOutput("item-id")).toBe("draft-item-message");

    // Cleanup
    delete process.env.GH_AW_PROJECT_URL;
  });

  it("should fail gracefully when both direct query and fallback list query fail", async () => {
    const messageHandler = await updateProjectHandlerFactory({});

    // Mock GraphQL responses - both queries fail
    const notFoundError = new Error("Could not resolve to a ProjectV2 with the number 146.");
    notFoundError.errors = [
      {
        type: "NOT_FOUND",
        message: "Could not resolve to a ProjectV2 with the number 146.",
        path: ["organization", "projectV2"],
      },
    ];

    const apiError = new Error("Request failed due to following response errors:\n - Something went wrong while executing your query.");
    apiError.errors = [
      {
        message: "Something went wrong while executing your query.",
      },
    ];

    // Setup mocks: repo query, viewer query, direct project query (fails), fallback list query (fails)
    mockGithub.graphql
      .mockResolvedValueOnce(repoResponse()) // Repository query
      .mockResolvedValueOnce(viewerResponse()) // Viewer query
      .mockRejectedValueOnce(notFoundError) // Direct projectV2 query fails
      .mockRejectedValueOnce(apiError); // Fallback projectsV2 list query fails

    const projectUrl = "https://github.com/orgs/testowner/projects/146";
    const messageWithProject = {
      type: "update_project",
      project: projectUrl,
      content_type: "issue",
      content_number: 123,
    };

    const result = await messageHandler(messageWithProject, new Map());

    expect(result.success).toBe(false);
    expect(result.error).toContain("Unable to resolve project #146");
    expect(result.error).toContain("Both direct projectV2 query and fallback projectsV2 list query failed");
    expect(result.error).toContain("transient GitHub API error");
  });
});
