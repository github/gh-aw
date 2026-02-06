// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";
import { main } from "./dispatch_workflow.cjs";

// Mock dependencies
global.core = {
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
};

global.context = {
  repo: {
    owner: "test-owner",
    repo: "test-repo",
  },
  ref: "refs/heads/main",
  payload: {
    repository: {
      default_branch: "main",
    },
  },
};

global.github = {
  rest: {
    actions: {
      createWorkflowDispatch: vi.fn().mockResolvedValue({}),
    },
    repos: {
      get: vi.fn().mockResolvedValue({
        data: {
          default_branch: "main",
        },
      }),
    },
  },
};

describe("dispatch_workflow handler factory", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    process.env.GITHUB_REF = "refs/heads/main";
    delete process.env.GITHUB_HEAD_REF; // Clean up PR environment variable
  });

  it("should create a handler function", async () => {
    const handler = await main({});
    expect(typeof handler).toBe("function");
  });

  it("should dispatch workflows with valid configuration", async () => {
    const config = {
      workflows: ["test-workflow"],
      workflow_files: {
        "test-workflow": ".lock.yml",
      },
      max: 5,
    };
    const handler = await main(config);

    const message = {
      type: "dispatch_workflow",
      workflow_name: "test-workflow",
      inputs: {
        param1: "value1",
        param2: 42,
      },
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(result.workflow_name).toBe("test-workflow");
    // Should use the extension from config
    expect(github.rest.actions.createWorkflowDispatch).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      workflow_id: "test-workflow.lock.yml",
      ref: expect.any(String),
      inputs: {
        param1: "value1",
        param2: "42",
      },
    });
  });

  it("should reject workflows not in allowed list", async () => {
    const config = {
      workflows: ["allowed-workflow"],
      max: 5,
    };
    const handler = await main(config);

    const message = {
      type: "dispatch_workflow",
      workflow_name: "unauthorized-workflow",
      inputs: {},
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("not in the allowed workflows list");
    expect(github.rest.actions.createWorkflowDispatch).not.toHaveBeenCalled();
  });

  it("should enforce max count", async () => {
    const config = {
      workflows: ["workflow1", "workflow2"],
      workflow_files: {
        workflow1: ".lock.yml",
        workflow2: ".yml",
      },
      max: 1,
    };
    const handler = await main(config);

    // First message should succeed
    const message1 = {
      type: "dispatch_workflow",
      workflow_name: "workflow1",
      inputs: {},
    };
    const result1 = await handler(message1, {});
    expect(result1.success).toBe(true);

    // Second message should be rejected due to max count
    const message2 = {
      type: "dispatch_workflow",
      workflow_name: "workflow2",
      inputs: {},
    };
    const result2 = await handler(message2, {});
    expect(result2.success).toBe(false);
    expect(result2.error).toContain("Max count");
  });

  it("should handle empty workflow name", async () => {
    const handler = await main({});

    const message = {
      type: "dispatch_workflow",
      workflow_name: "",
      inputs: {},
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("empty");
    expect(github.rest.actions.createWorkflowDispatch).not.toHaveBeenCalled();
  });

  it("should handle dispatch errors", async () => {
    const handler = await main({
      workflows: ["missing-workflow"],
      workflow_files: {}, // No extension for missing-workflow
    });

    const message = {
      type: "dispatch_workflow",
      workflow_name: "missing-workflow",
      inputs: {},
    };

    const result = await handler(message, {});

    expect(result.success).toBe(false);
    expect(result.error).toContain("not found in configuration");
  });

  it("should convert input values to strings", async () => {
    const config = {
      workflows: ["test-workflow"],
      workflow_files: {
        "test-workflow": ".lock.yml",
      },
    };
    const handler = await main(config);

    const message = {
      type: "dispatch_workflow",
      workflow_name: "test-workflow",
      inputs: {
        string: "hello",
        number: 42,
        boolean: true,
        object: { key: "value" },
        null: null,
        undefined: undefined,
      },
    };

    await handler(message, {});

    expect(github.rest.actions.createWorkflowDispatch).toHaveBeenCalledWith(
      expect.objectContaining({
        inputs: {
          string: "hello",
          number: "42",
          boolean: "true",
          object: '{"key":"value"}',
          null: "",
          undefined: "",
        },
      })
    );
  });

  it("should handle workflows with no inputs", async () => {
    const config = {
      workflows: ["no-inputs-workflow"],
      workflow_files: {
        "no-inputs-workflow": ".lock.yml",
      },
    };
    const handler = await main(config);

    // Test with inputs property missing entirely
    const message = {
      type: "dispatch_workflow",
      workflow_name: "no-inputs-workflow",
    };

    const result = await handler(message, {});

    expect(result.success).toBe(true);
    expect(github.rest.actions.createWorkflowDispatch).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      workflow_id: "no-inputs-workflow.lock.yml",
      ref: expect.any(String),
      inputs: {}, // Should pass empty object even when inputs property is missing
    });
  });

  it("should delay 5 seconds between dispatches", async () => {
    const config = {
      workflows: ["workflow1", "workflow2"],
      workflow_files: {
        workflow1: ".lock.yml",
        workflow2: ".yml",
      },
      max: 5,
    };
    const handler = await main(config);

    const message1 = {
      type: "dispatch_workflow",
      workflow_name: "workflow1",
      inputs: {},
    };

    const message2 = {
      type: "dispatch_workflow",
      workflow_name: "workflow2",
      inputs: {},
    };

    // Dispatch first workflow
    const startTime = Date.now();
    await handler(message1, {});
    const firstDispatchTime = Date.now();

    // Dispatch second workflow (should be delayed)
    await handler(message2, {});
    const secondDispatchTime = Date.now();

    // Verify first dispatch had no delay
    expect(firstDispatchTime - startTime).toBeLessThan(1000);

    // Verify second dispatch was delayed by approximately 5 seconds
    // Use a slightly lower threshold (4995ms) to account for timing jitter
    expect(secondDispatchTime - firstDispatchTime).toBeGreaterThanOrEqual(4995);
    expect(secondDispatchTime - firstDispatchTime).toBeLessThan(6000);
  });

  it("should use PR branch ref when GITHUB_HEAD_REF is set", async () => {
    // Simulate PR context where GITHUB_REF is the merge ref
    process.env.GITHUB_REF = "refs/pull/123/merge";
    process.env.GITHUB_HEAD_REF = "feature-branch";

    const config = {
      workflows: ["test-workflow"],
      workflow_files: {
        "test-workflow": ".lock.yml",
      },
    };
    const handler = await main(config);

    const message = {
      type: "dispatch_workflow",
      workflow_name: "test-workflow",
      inputs: {},
    };

    await handler(message, {});

    // Should use the PR branch ref, not the merge ref
    expect(github.rest.actions.createWorkflowDispatch).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      workflow_id: "test-workflow.lock.yml",
      ref: "refs/heads/feature-branch",
      inputs: {},
    });
  });

  it("should use GITHUB_REF when not in PR context", async () => {
    process.env.GITHUB_REF = "refs/heads/main";
    delete process.env.GITHUB_HEAD_REF;

    const config = {
      workflows: ["test-workflow"],
      workflow_files: {
        "test-workflow": ".lock.yml",
      },
    };
    const handler = await main(config);

    const message = {
      type: "dispatch_workflow",
      workflow_name: "test-workflow",
      inputs: {},
    };

    await handler(message, {});

    // Should use GITHUB_REF directly
    expect(github.rest.actions.createWorkflowDispatch).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      workflow_id: "test-workflow.lock.yml",
      ref: "refs/heads/main",
      inputs: {},
    });
  });

  it("should handle PR context with slashes in branch names", async () => {
    process.env.GITHUB_REF = "refs/pull/456/merge";
    process.env.GITHUB_HEAD_REF = "feature/add-new-feature";

    const config = {
      workflows: ["test-workflow"],
      workflow_files: {
        "test-workflow": ".lock.yml",
      },
    };
    const handler = await main(config);

    const message = {
      type: "dispatch_workflow",
      workflow_name: "test-workflow",
      inputs: {},
    };

    await handler(message, {});

    // Should correctly handle branch names with slashes
    expect(github.rest.actions.createWorkflowDispatch).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      workflow_id: "test-workflow.lock.yml",
      ref: "refs/heads/feature/add-new-feature",
      inputs: {},
    });
  });

  it("should use repository default branch when no GITHUB_REF is set", async () => {
    delete process.env.GITHUB_REF;
    delete process.env.GITHUB_HEAD_REF;
    global.context.ref = undefined;
    global.context.payload.repository.default_branch = "develop";

    const config = {
      workflows: ["test-workflow"],
      workflow_files: {
        "test-workflow": ".lock.yml",
      },
    };
    const handler = await main(config);

    const message = {
      type: "dispatch_workflow",
      workflow_name: "test-workflow",
      inputs: {},
    };

    await handler(message, {});

    // Should use the repository's default branch from context
    expect(github.rest.actions.createWorkflowDispatch).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      workflow_id: "test-workflow.lock.yml",
      ref: "refs/heads/develop",
      inputs: {},
    });
  });

  it("should fall back to API when context payload is missing", async () => {
    delete process.env.GITHUB_REF;
    delete process.env.GITHUB_HEAD_REF;
    global.context.ref = undefined;
    global.context.payload = {};

    github.rest.repos.get.mockResolvedValueOnce({
      data: {
        default_branch: "staging",
      },
    });

    const config = {
      workflows: ["test-workflow"],
      workflow_files: {
        "test-workflow": ".lock.yml",
      },
    };
    const handler = await main(config);

    const message = {
      type: "dispatch_workflow",
      workflow_name: "test-workflow",
      inputs: {},
    };

    await handler(message, {});

    // Should fetch default branch from API
    expect(github.rest.repos.get).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
    });

    expect(github.rest.actions.createWorkflowDispatch).toHaveBeenCalledWith({
      owner: "test-owner",
      repo: "test-repo",
      workflow_id: "test-workflow.lock.yml",
      ref: "refs/heads/staging",
      inputs: {},
    });
  });
});
