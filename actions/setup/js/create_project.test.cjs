// @ts-check
import { describe, it, expect, beforeAll, beforeEach, vi } from "vitest";

let main;

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setOutput: vi.fn(),
  debug: vi.fn(),
};

const mockGithub = {
  graphql: vi.fn(),
  request: vi.fn(),
};

const mockContext = {
  repo: {
    owner: "test-owner",
    repo: "test-repo",
  },
  payload: {},
};

global.core = mockCore;
global.github = mockGithub;
global.context = mockContext;

/** Standard mock responses for a successful project creation */
const ORG_OWNER_RESPONSE = { organization: { id: "ORG_abc123" } };
const CREATED_PROJECT_RESPONSE = {
  createProjectV2: {
    projectV2: {
      id: "PVT_proj1",
      number: 1,
      title: "Test Project",
      url: "https://github.com/orgs/test-org/projects/1",
    },
  },
};

/**
 * Helper: create a handler with a mock github client wired to the module
 */
async function makeHandler(config = {}) {
  return main({ target_owner: "test-org", max: 5, ...config }, mockGithub);
}

beforeAll(async () => {
  const mod = await import("./create_project.cjs");
  main = mod.main;
});

beforeEach(() => {
  vi.clearAllMocks();
  mockContext.payload = {};
});

describe("create_project view layouts", () => {
  for (const ownerType of ["org", "user"]) {
    for (const layout of ["table", "board", "roadmap"]) {
      it(`passes a validated ${layout} layout to the ${ownerType} views endpoint`, async () => {
        const owner = ownerType === "org" ? "test-org" : "test-user";
        const scope = ownerType === "org" ? "orgs" : "users";
        const project = { ...CREATED_PROJECT_RESPONSE.createProjectV2.projectV2, url: `https://github.com/${scope}/${owner}/projects/1` };
        mockGithub.graphql.mockResolvedValueOnce(ownerType === "org" ? ORG_OWNER_RESPONSE : { user: { id: "USER_abc123" } }).mockResolvedValueOnce({ createProjectV2: { projectV2: project } });
        mockGithub.request.mockResolvedValueOnce({ data: { id: 17 } });
        const handler = await makeHandler({ views: [{ name: " Overview ", layout: ` ${layout} `, filter: "state:open", visible_fields: [2, 3] }] });

        const result = await handler({ title: "My Project", owner, owner_type: ownerType });

        expect(result.success).toBe(true);
        expect(mockGithub.request).toHaveBeenCalledExactlyOnceWith(ownerType === "org" ? "POST /orgs/{org}/projectsV2/{project_number}/views" : "POST /users/{user_id}/projectsV2/{project_number}/views", {
          ...(ownerType === "org" ? { org: owner } : { user_id: owner }),
          project_number: 1,
          name: "Overview",
          layout,
          filter: "state:open",
          ...(layout === "roadmap" ? {} : { visible_fields: [2, 3] }),
        });
        if (layout === "roadmap") expect(mockCore.warning).toHaveBeenCalledWith('visible_fields is not applicable to layout "roadmap"; ignoring.');
      });
    }
  }

  it.each(["", "kanban", 1, null, {}, true])("rejects invalid layout %j before calling the views endpoint", async layout => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);
    const handler = await makeHandler({ views: [{ name: "Invalid view", layout }] });

    const result = await handler({ title: "My Project" });

    expect(result.success).toBe(true);
    expect(mockGithub.request).not.toHaveBeenCalled();
    expect(mockCore.error).toHaveBeenCalledWith("Failed to create configured view 1: Invalid view");
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Invalid view layout"));
  });
});

// ─── temporary_id field ───────────────────────────────────────────────────────

describe("create_project temporary_id field", () => {
  it("uses declared temporary_id (bare aw_xxx) and returns it in result", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler();
    const temporaryIdMap = new Map();

    const result = await handler({ title: "My Project", temporary_id: "aw_proj1" }, {}, temporaryIdMap);

    expect(result.success).toBe(true);
    expect(result.temporaryId).toBe("aw_proj1");
    expect(temporaryIdMap.get("aw_proj1")).toEqual({ projectUrl: "https://github.com/orgs/test-org/projects/1" });
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Stored temporary ID mapping: aw_proj1"));
  });

  it("normalises declared temporary_id with '#' prefix to bare form", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler();
    const temporaryIdMap = new Map();

    const result = await handler({ title: "My Project", temporary_id: "#aw_proj1" }, {}, temporaryIdMap);

    expect(result.success).toBe(true);
    expect(result.temporaryId).toBe("aw_proj1");
    expect(temporaryIdMap.has("aw_proj1")).toBe(true);
    expect(temporaryIdMap.has("#aw_proj1")).toBe(false);
  });

  it("normalises declared temporary_id to lowercase", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler();
    const temporaryIdMap = new Map();

    const result = await handler({ title: "My Project", temporary_id: "aw_MYPROJ" }, {}, temporaryIdMap);

    expect(result.success).toBe(true);
    expect(result.temporaryId).toBe("aw_myproj");
    expect(temporaryIdMap.has("aw_myproj")).toBe(true);
  });

  it("normalises '#aw_' prefix and uppercase together", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler();
    const temporaryIdMap = new Map();

    const result = await handler({ title: "My Project", temporary_id: "#aw_MyProj1" }, {}, temporaryIdMap);

    expect(result.success).toBe(true);
    expect(result.temporaryId).toBe("aw_myproj1");
    expect(temporaryIdMap.has("aw_myproj1")).toBe(true);
  });

  it("auto-generates temporary_id when omitted", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler();
    const temporaryIdMap = new Map();

    const result = await handler({ title: "My Project" }, {}, temporaryIdMap);

    expect(result.success).toBe(true);
    expect(result.temporaryId).toMatch(/^aw_[A-Za-z0-9]{8}$/);
    expect(temporaryIdMap.size).toBe(1);
  });

  it("auto-generates temporary_id and warns when format is invalid", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler();
    const temporaryIdMap = new Map();

    const result = await handler({ title: "My Project", temporary_id: "bad-format" }, {}, temporaryIdMap);

    expect(result.success).toBe(true);
    expect(result.temporaryId).toMatch(/^aw_[A-Za-z0-9]{8}$/);
    expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Invalid temporary_id format"));
  });

  it("supports underscore-containing temporary_id (e.g. aw_pr_fix)", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler();
    const temporaryIdMap = new Map();

    const result = await handler({ title: "My Project", temporary_id: "aw_pr_fix" }, {}, temporaryIdMap);

    expect(result.success).toBe(true);
    expect(result.temporaryId).toBe("aw_pr_fix");
    expect(temporaryIdMap.has("aw_pr_fix")).toBe(true);
  });
});

// ─── item_url temporary ID resolution ────────────────────────────────────────

const ISSUE_NODE_RESPONSE = { repository: { issue: { id: "ISSUE_node123" } } };
const ADD_ITEM_RESPONSE = { addProjectV2ItemById: { item: { id: "PVTI_item1" } } };

/** Mock sequence for: getOwnerId + createProject + getIssueNodeId + addItemToProject */
function mockSuccessWithItem() {
  mockGithub.graphql
    .mockResolvedValueOnce(ORG_OWNER_RESPONSE) // getOwnerId
    .mockResolvedValueOnce(CREATED_PROJECT_RESPONSE) // createProjectV2
    .mockResolvedValueOnce(ISSUE_NODE_RESPONSE) // getIssueNodeId
    .mockResolvedValueOnce(ADD_ITEM_RESPONSE); // addItemToProject
}

describe("create_project item_url temporary ID resolution", () => {
  it("resolves plain temporary ID in item_url (aw_xxx form)", async () => {
    mockSuccessWithItem();

    const handler = await makeHandler();
    const temporaryIdMap = new Map();
    temporaryIdMap.set("aw_issue1", { repo: "test-owner/test-repo", number: 42 });

    const result = await handler(
      {
        title: "My Project",
        item_url: "aw_issue1",
        temporary_id: "aw_proj1",
      },
      Object.fromEntries(temporaryIdMap),
      temporaryIdMap
    );

    expect(result.success).toBe(true);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved temporary ID aw_issue1 in item_url"));
  });

  it("resolves temporary ID with '#' prefix in item_url (#aw_xxx form)", async () => {
    mockSuccessWithItem();

    const handler = await makeHandler();
    const temporaryIdMap = new Map();
    temporaryIdMap.set("aw_issue1", { repo: "test-owner/test-repo", number: 42 });

    const result = await handler(
      {
        title: "My Project",
        item_url: "#aw_issue1",
        temporary_id: "aw_proj1",
      },
      Object.fromEntries(temporaryIdMap),
      temporaryIdMap
    );

    expect(result.success).toBe(true);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved temporary ID #aw_issue1 in item_url"));
  });

  it("resolves temporary ID embedded in full GitHub URL (aw_xxx in URL path)", async () => {
    mockSuccessWithItem();

    const handler = await makeHandler();
    const temporaryIdMap = new Map();
    temporaryIdMap.set("aw_issue1", { repo: "test-owner/test-repo", number: 42 });

    const result = await handler(
      {
        title: "My Project",
        item_url: "https://github.com/test-owner/test-repo/issues/aw_issue1",
        temporary_id: "aw_proj1",
      },
      Object.fromEntries(temporaryIdMap),
      temporaryIdMap
    );

    expect(result.success).toBe(true);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved temporary ID aw_issue1 in item_url"));
  });

  it("resolves '#aw_xxx' embedded in full GitHub URL path", async () => {
    mockSuccessWithItem();

    const handler = await makeHandler();
    const temporaryIdMap = new Map();
    temporaryIdMap.set("aw_issue1", { repo: "test-owner/test-repo", number: 42 });

    const result = await handler(
      {
        title: "My Project",
        item_url: "https://github.com/test-owner/test-repo/issues/#aw_issue1",
        temporary_id: "aw_proj1",
      },
      Object.fromEntries(temporaryIdMap),
      temporaryIdMap
    );

    expect(result.success).toBe(true);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved temporary ID #aw_issue1 in item_url"));
  });

  it("resolves via resolvedTemporaryIds (plain object) when temporaryIdMap is null", async () => {
    mockSuccessWithItem();

    const handler = await makeHandler();
    const resolvedTemporaryIds = { aw_issue1: { repo: "test-owner/test-repo", number: 42 } };

    const result = await handler(
      {
        title: "My Project",
        item_url: "aw_issue1",
        temporary_id: "aw_proj1",
      },
      resolvedTemporaryIds,
      null
    );

    expect(result.success).toBe(true);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Resolved temporary ID aw_issue1 in item_url"));
  });

  it("fails with error when item_url temporary ID is not in temporaryIdMap", async () => {
    // No graphql calls expected — fails before API calls
    const handler = await makeHandler();
    const temporaryIdMap = new Map(); // empty — ID not yet resolved

    const result = await handler(
      {
        title: "My Project",
        item_url: "aw_missing",
        temporary_id: "aw_proj1",
      },
      {},
      temporaryIdMap
    );

    expect(result.success).toBe(false);
    expect(result.error).toContain("aw_missing");
    expect(result.error).toContain("item_url not found");
    expect(mockGithub.graphql).not.toHaveBeenCalled();
  });

  it("passes through a real item_url without treating it as a temporary ID", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE).mockResolvedValueOnce(ISSUE_NODE_RESPONSE).mockResolvedValueOnce(ADD_ITEM_RESPONSE);

    const handler = await makeHandler();
    const temporaryIdMap = new Map();

    const result = await handler(
      {
        title: "My Project",
        item_url: "https://github.com/test-owner/test-repo/issues/42",
        temporary_id: "aw_proj1",
      },
      {},
      temporaryIdMap
    );

    expect(result.success).toBe(true);
    // Should not log a "Resolved temporary ID" message for a real URL
    expect(mockCore.info).not.toHaveBeenCalledWith(expect.stringContaining("Resolved temporary ID"));
  });
});

// ─── temporaryIdMap storage ───────────────────────────────────────────────────

describe("create_project temporaryIdMap storage", () => {
  it("stores project URL in temporaryIdMap under the normalised key", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler();
    const temporaryIdMap = new Map();

    await handler({ title: "My Project", temporary_id: "aw_proj1" }, {}, temporaryIdMap);

    expect(temporaryIdMap.get("aw_proj1")).toEqual({
      projectUrl: "https://github.com/orgs/test-org/projects/1",
    });
  });

  it("stores project URL under lowercase key when temporary_id is uppercase", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler();
    const temporaryIdMap = new Map();

    await handler({ title: "My Project", temporary_id: "aw_PROJ1" }, {}, temporaryIdMap);

    expect(temporaryIdMap.has("aw_proj1")).toBe(true);
    expect(temporaryIdMap.has("aw_PROJ1")).toBe(false);
  });

  it("stores project URL under lowercase key when temporary_id has '#' prefix", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler();
    const temporaryIdMap = new Map();

    await handler({ title: "My Project", temporary_id: "#aw_proj1" }, {}, temporaryIdMap);

    expect(temporaryIdMap.has("aw_proj1")).toBe(true);
    expect(temporaryIdMap.has("#aw_proj1")).toBe(false);
  });

  it("does not store in map when temporaryIdMap is null", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler();

    // Pass null as temporaryIdMap (backward compat) — should not throw
    const result = await handler({ title: "My Project", temporary_id: "aw_proj1" }, {}, null);

    expect(result.success).toBe(true);
    expect(result.temporaryId).toBe("aw_proj1");
  });
});

// ─── staged mode ─────────────────────────────────────────────────────────────

describe("create_project staged mode", () => {
  it("returns previewInfo with temporaryId without calling API", async () => {
    const handler = await makeHandler({ staged: true });
    const temporaryIdMap = new Map();

    const result = await handler({ title: "My Project", temporary_id: "aw_proj1" }, {}, temporaryIdMap);

    expect(result.success).toBe(true);
    expect(result.staged).toBe(true);
    expect(result.previewInfo.temporaryId).toBe("aw_proj1");
    expect(mockGithub.graphql).not.toHaveBeenCalled();
  });

  it("normalises '#' prefix in temporary_id during staged mode", async () => {
    const handler = await makeHandler({ staged: true });
    const temporaryIdMap = new Map();

    const result = await handler({ title: "My Project", temporary_id: "#aw_proj1" }, {}, temporaryIdMap);

    expect(result.success).toBe(true);
    expect(result.staged).toBe(true);
    expect(result.previewInfo.temporaryId).toBe("aw_proj1");
  });
});

// ─── max count ────────────────────────────────────────────────────────────────

describe("create_project max count", () => {
  it("succeeds on first call and rejects on second when max=1", async () => {
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce(CREATED_PROJECT_RESPONSE);

    const handler = await makeHandler({ max: 1 });
    const temporaryIdMap = new Map();

    const first = await handler({ title: "Project A", temporary_id: "aw_proja" }, {}, temporaryIdMap);
    expect(first.success).toBe(true);

    const second = await handler({ title: "Project B", temporary_id: "aw_projb" }, {}, temporaryIdMap);
    expect(second.success).toBe(false);
    expect(second.error).toContain("Max count");
  });
});

// ─── title auto-generation ────────────────────────────────────────────────────

describe("create_project title auto-generation", () => {
  it("auto-generates title from issue context when title is missing", async () => {
    mockContext.payload = { issue: { number: 7, title: "Fix the bug" } };
    mockGithub.graphql.mockResolvedValueOnce(ORG_OWNER_RESPONSE).mockResolvedValueOnce({
      createProjectV2: {
        projectV2: {
          id: "PVT_auto",
          number: 2,
          title: "Project: Fix the bug",
          url: "https://github.com/orgs/test-org/projects/2",
        },
      },
    });

    const handler = await makeHandler({ title_prefix: "Project" });

    const result = await handler({ temporary_id: "aw_proj1" }, {}, new Map());

    expect(result.success).toBe(true);
    expect(mockCore.info).toHaveBeenCalledWith(expect.stringContaining("Generated title from issue"));
  });

  it("fails when title is missing and context has no issue", async () => {
    mockContext.payload = {};
    const handler = await makeHandler();

    const result = await handler({ temporary_id: "aw_proj1" }, {}, new Map());

    expect(result.success).toBe(false);
    expect(result.error).toContain("Missing required field 'title'");
  });

  it("fails when target_owner is missing", async () => {
    // Create handler without target_owner
    const handler = await main({ max: 1 }, mockGithub);

    const result = await handler({ title: "My Project", temporary_id: "aw_proj1" }, {}, new Map());

    expect(result.success).toBe(false);
    expect(result.error).toContain("No owner specified");
  });
});
