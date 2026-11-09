// @ts-check

const { getWorkflowMetadata, buildWorkflowRunUrl } = require("./workflow_metadata_helpers.cjs");

describe("getWorkflowMetadata", () => {
  let originalEnv;
  let originalContext;

  beforeEach(() => {
    // Save original environment
    originalEnv = { ...process.env };

    // Save and mock global context
    originalContext = global.context;
    global.context = {
      runId: 123456,
      serverUrl: "https://github.com",
      payload: {
        repository: {
          html_url: "https://github.com/test-owner/test-repo",
        },
      },
    };
  });

  afterEach(() => {
    // Restore environment by mutating process.env in place
    for (const key of Object.keys(process.env)) {
      if (!(key in originalEnv)) {
        delete process.env[key];
      }
    }
    Object.assign(process.env, originalEnv);

    // Restore original context
    global.context = originalContext;
  });

  it("should extract workflow metadata from environment and context", () => {
    // Set environment variables
    process.env.GH_AW_WORKFLOW_NAME = "Test Workflow";
    process.env.GH_AW_WORKFLOW_ID = "test-workflow-id";
    process.env.GITHUB_SERVER_URL = "https://github.com";

    const metadata = getWorkflowMetadata("test-owner", "test-repo");

    expect(metadata).toEqual({
      workflowName: "Test Workflow",
      workflowId: "test-workflow-id",
      runId: 123456,
      runUrl: "https://github.com/test-owner/test-repo/actions/runs/123456",
    });
  });

  it("should use defaults when environment variables are missing", () => {
    // Clear environment variables
    delete process.env.GH_AW_WORKFLOW_NAME;
    delete process.env.GH_AW_WORKFLOW_ID;
    delete process.env.GITHUB_SERVER_URL;

    const metadata = getWorkflowMetadata("test-owner", "test-repo");

    expect(metadata.workflowName).toBe("Workflow");
    expect(metadata.workflowId).toBe("");
    expect(metadata.runId).toBe(123456);
    expect(metadata.runUrl).toBe("https://github.com/test-owner/test-repo/actions/runs/123456");
  });

  it("should construct runUrl from githubServer when repository payload is missing", () => {
    // Mock context without repository payload
    global.context = {
      runId: 789012,
      payload: {},
    };

    process.env.GITHUB_SERVER_URL = "https://github.enterprise.com";

    const metadata = getWorkflowMetadata("enterprise-owner", "enterprise-repo");

    expect(metadata.runUrl).toBe("https://github.enterprise.com/enterprise-owner/enterprise-repo/actions/runs/789012");
  });

  it("should handle missing context gracefully", () => {
    // Mock context with missing runId
    global.context = {
      payload: {
        repository: {
          html_url: "https://github.com/test-owner/test-repo",
        },
      },
    };

    const metadata = getWorkflowMetadata("test-owner", "test-repo");

    expect(metadata.runId).toBe(0);
    expect(metadata.runUrl).toBe("https://github.com/test-owner/test-repo/actions/runs/0");
  });

  it("should prefer context.serverUrl over GITHUB_SERVER_URL env var", () => {
    global.context = {
      runId: 42,
      serverUrl: "https://context-server.example.com",
    };
    process.env.GITHUB_SERVER_URL = "https://env-server.example.com";

    const metadata = getWorkflowMetadata("owner", "repo");

    expect(metadata.runUrl).toBe("https://context-server.example.com/owner/repo/actions/runs/42");
  });

  it("should fall back to https://github.com when no server URL sources are available", () => {
    global.context = { runId: 7 };
    delete process.env.GITHUB_SERVER_URL;

    const metadata = getWorkflowMetadata("my-owner", "my-repo");

    expect(metadata.runUrl).toBe("https://github.com/my-owner/my-repo/actions/runs/7");
  });

  it("should use 0 as runId when context.runId is explicitly 0", () => {
    global.context = { runId: 0, serverUrl: "https://github.com" };

    const metadata = getWorkflowMetadata("owner", "repo");

    expect(metadata.runId).toBe(0);
    expect(metadata.runUrl).toBe("https://github.com/owner/repo/actions/runs/0");
  });

  it("should prefer an explicitly passed context over the global context", () => {
    const metadata = getWorkflowMetadata("owner", "repo", { runId: 777, serverUrl: "https://ghe.example.com" });

    expect(metadata.runId).toBe(777);
    expect(metadata.runUrl).toBe("https://ghe.example.com/owner/repo/actions/runs/777");
  });

  it("should fall back to runId 0 when the explicit context has no runId", () => {
    const metadata = getWorkflowMetadata("owner", "repo", {});

    expect(metadata.runId).toBe(0);
    expect(metadata.runUrl).toMatch(/\/owner\/repo\/actions\/runs\/0$/);
  });
});

describe("buildWorkflowRunUrl", () => {
  it("should build run URL from context.serverUrl and explicit workflowRepo", () => {
    const ctx = { serverUrl: "https://github.com", runId: 42000 };
    const url = buildWorkflowRunUrl(ctx, { owner: "wf-owner", repo: "wf-repo" });
    expect(url).toBe("https://github.com/wf-owner/wf-repo/actions/runs/42000");
  });

  it("should fall back to GITHUB_SERVER_URL when context.serverUrl is absent", () => {
    const originalEnv = process.env.GITHUB_SERVER_URL;
    process.env.GITHUB_SERVER_URL = "https://ghes.example.com";
    try {
      const ctx = { runId: 99 };
      const url = buildWorkflowRunUrl(ctx, { owner: "ent-owner", repo: "ent-repo" });
      expect(url).toBe("https://ghes.example.com/ent-owner/ent-repo/actions/runs/99");
    } finally {
      if (originalEnv === undefined) {
        delete process.env.GITHUB_SERVER_URL;
      } else {
        process.env.GITHUB_SERVER_URL = originalEnv;
      }
    }
  });

  it("should use the workflowRepo, not a cross-repo target", () => {
    // Simulates the cross-repo case: context.repo is the target but workflowRepo is the workflow owner
    const ctx = { serverUrl: "https://github.com", runId: 7777, repo: { owner: "cross-owner", repo: "cross-repo" } };
    const workflowRepo = { owner: "wf-owner", repo: "wf-repo" };
    const url = buildWorkflowRunUrl(ctx, workflowRepo);
    expect(url).toBe("https://github.com/wf-owner/wf-repo/actions/runs/7777");
    expect(url).not.toContain("cross-owner");
    expect(url).not.toContain("cross-repo");
  });

  it("should fall back to https://github.com when no server URL sources are available", () => {
    const originalEnv = process.env.GITHUB_SERVER_URL;
    delete process.env.GITHUB_SERVER_URL;
    try {
      const url = buildWorkflowRunUrl({ runId: 1 }, { owner: "owner", repo: "repo" });
      expect(url).toBe("https://github.com/owner/repo/actions/runs/1");
    } finally {
      if (originalEnv === undefined) {
        delete process.env.GITHUB_SERVER_URL;
      } else {
        process.env.GITHUB_SERVER_URL = originalEnv;
      }
    }
  });

  it("should handle runId of 0", () => {
    const url = buildWorkflowRunUrl({ serverUrl: "https://github.com", runId: 0 }, { owner: "owner", repo: "repo" });
    expect(url).toBe("https://github.com/owner/repo/actions/runs/0");
  });

  it("should fall back to GITHUB_REPOSITORY when workflowRepo owner/repo are empty", () => {
    const originalEnv = process.env.GITHUB_REPOSITORY;
    process.env.GITHUB_REPOSITORY = "central-owner/central-repo";
    try {
      // Simulates the spread-context case where repo getter is lost: { owner: "", repo: "" }
      const url = buildWorkflowRunUrl({ serverUrl: "https://github.com", runId: 999 }, { owner: "", repo: "" });
      expect(url).toBe("https://github.com/central-owner/central-repo/actions/runs/999");
    } finally {
      if (originalEnv === undefined) {
        delete process.env.GITHUB_REPOSITORY;
      } else {
        process.env.GITHUB_REPOSITORY = originalEnv;
      }
    }
  });

  it("should fall back to GITHUB_REPOSITORY when workflowRepo is missing", () => {
    const originalEnv = process.env.GITHUB_REPOSITORY;
    process.env.GITHUB_REPOSITORY = "env-owner/env-repo";
    try {
      const url = buildWorkflowRunUrl({ serverUrl: "https://github.com", runId: 42 }, null);
      expect(url).toBe("https://github.com/env-owner/env-repo/actions/runs/42");
    } finally {
      if (originalEnv === undefined) {
        delete process.env.GITHUB_REPOSITORY;
      } else {
        process.env.GITHUB_REPOSITORY = originalEnv;
      }
    }
  });

  it("should use explicit owner but fall back GITHUB_REPOSITORY for missing repo", () => {
    const originalEnv = process.env.GITHUB_REPOSITORY;
    process.env.GITHUB_REPOSITORY = "env-owner/env-repo";
    try {
      const url = buildWorkflowRunUrl({ serverUrl: "https://github.com", runId: 5 }, { owner: "explicit-owner", repo: "" });
      expect(url).toBe("https://github.com/explicit-owner/env-repo/actions/runs/5");
    } finally {
      if (originalEnv === undefined) {
        delete process.env.GITHUB_REPOSITORY;
      } else {
        process.env.GITHUB_REPOSITORY = originalEnv;
      }
    }
  });

  it("should use explicit repo but fall back GITHUB_REPOSITORY for missing owner", () => {
    const originalEnv = process.env.GITHUB_REPOSITORY;
    process.env.GITHUB_REPOSITORY = "env-owner/env-repo";
    try {
      const url = buildWorkflowRunUrl({ serverUrl: "https://github.com", runId: 6 }, { owner: "", repo: "explicit-repo" });
      expect(url).toBe("https://github.com/env-owner/explicit-repo/actions/runs/6");
    } finally {
      if (originalEnv === undefined) {
        delete process.env.GITHUB_REPOSITORY;
      } else {
        process.env.GITHUB_REPOSITORY = originalEnv;
      }
    }
  });

  it("should not fall back to GITHUB_REPOSITORY when both owner and repo are set", () => {
    const originalEnv = process.env.GITHUB_REPOSITORY;
    process.env.GITHUB_REPOSITORY = "env-owner/env-repo";
    try {
      const url = buildWorkflowRunUrl({ serverUrl: "https://github.com", runId: 8 }, { owner: "wf-owner", repo: "wf-repo" });
      expect(url).toBe("https://github.com/wf-owner/wf-repo/actions/runs/8");
      expect(url).not.toContain("env-owner");
      expect(url).not.toContain("env-repo");
    } finally {
      if (originalEnv === undefined) {
        delete process.env.GITHUB_REPOSITORY;
      } else {
        process.env.GITHUB_REPOSITORY = originalEnv;
      }
    }
  });

  it("should use string runId in URL", () => {
    const url = buildWorkflowRunUrl({ serverUrl: "https://github.com", runId: "run-abc123" }, { owner: "owner", repo: "repo" });
    expect(url).toBe("https://github.com/owner/repo/actions/runs/run-abc123");
  });
});
