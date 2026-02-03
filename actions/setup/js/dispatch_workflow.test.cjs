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
};

global.github = {
  rest: {
    actions: {
      createWorkflowDispatch: vi.fn().mockResolvedValue({}),
    },
  },
};

describe("dispatch_workflow handler factory", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    process.env.GITHUB_REF = "refs/heads/main";
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
});
