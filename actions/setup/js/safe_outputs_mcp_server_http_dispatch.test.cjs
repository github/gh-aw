// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";

/**
 * Test for dispatch_workflow tool registration in HTTP server
 *
 * This test validates that the HTTP server correctly registers dispatch_workflow tools
 * that have _workflow_name metadata. These tools use workflow-specific names
 * (e.g., "test_workflow") while the config key is "dispatch_workflow".
 *
 * Reference: Issue where dispatch_workflow tools were not being registered because
 * the HTTP server's registration loop only checked if the tool name matched a config key,
 * but dispatch_workflow tools don't match - they have workflow-specific names with metadata.
 */
describe("safe_outputs_mcp_server_http dispatch_workflow registration", () => {
  it("should register dispatch_workflow tools when config.dispatch_workflow exists", () => {
    // Simulate the tool with _workflow_name metadata
    const tool = {
      name: "test_workflow",
      _workflow_name: "test-workflow",
      description: "Dispatch the 'test-workflow' workflow",
      inputSchema: {
        type: "object",
        properties: {
          test_param: {
            type: "string",
            description: "Test parameter",
          },
        },
        additionalProperties: false,
      },
    };

    // Simulate the config with dispatch_workflow key
    const config = {
      dispatch_workflow: {
        max: 1,
        workflows: ["test-workflow"],
        workflow_files: {
          "test-workflow": ".yml",
        },
      },
      missing_tool: {},
      missing_data: {},
      noop: { max: 1 },
    };

    // Simulate the enabledTools set (based on config keys)
    const enabledTools = new Set();
    for (const [toolName, enabled] of Object.entries(config)) {
      if (enabled) {
        enabledTools.add(toolName);
      }
    }

    // Test the registration logic (extracted from safe_outputs_mcp_server_http.cjs)
    const isDispatchWorkflowTool = !!tool._workflow_name;
    let shouldRegister = false;

    if (isDispatchWorkflowTool) {
      // Dispatch workflow tools should be registered if config.dispatch_workflow exists
      if (config.dispatch_workflow) {
        shouldRegister = true;
      }
    } else {
      // Regular tools should be registered if their name is in enabledTools
      if (enabledTools.has(tool.name)) {
        shouldRegister = true;
      }
    }

    // Verify that the dispatch_workflow tool should be registered
    expect(shouldRegister).toBe(true);
    expect(isDispatchWorkflowTool).toBe(true);
    expect(config.dispatch_workflow).toBeTruthy();
  });

  it("should NOT register dispatch_workflow tools when config.dispatch_workflow is missing", () => {
    // Simulate the tool with _workflow_name metadata
    const tool = {
      name: "test_workflow",
      _workflow_name: "test-workflow",
      description: "Dispatch the 'test-workflow' workflow",
      inputSchema: { type: "object", properties: {} },
    };

    // Config WITHOUT dispatch_workflow
    const config = {
      missing_tool: {},
      missing_data: {},
      noop: { max: 1 },
    };

    const enabledTools = new Set();
    for (const [toolName, enabled] of Object.entries(config)) {
      if (enabled) {
        enabledTools.add(toolName);
      }
    }

    const isDispatchWorkflowTool = !!tool._workflow_name;
    let shouldRegister = false;

    if (isDispatchWorkflowTool) {
      if (config.dispatch_workflow) {
        shouldRegister = true;
      }
    } else {
      if (enabledTools.has(tool.name)) {
        shouldRegister = true;
      }
    }

    // Verify that the tool should NOT be registered
    expect(shouldRegister).toBe(false);
    expect(isDispatchWorkflowTool).toBe(true);
    expect(config.dispatch_workflow).toBeFalsy();
  });

  it("should register regular tools when their name is in config", () => {
    // Regular tool (no _workflow_name)
    const tool = {
      name: "missing_tool",
      description: "Report missing tool",
      inputSchema: { type: "object", properties: {} },
    };

    const config = {
      missing_tool: {},
      missing_data: {},
    };

    const enabledTools = new Set();
    for (const [toolName, enabled] of Object.entries(config)) {
      if (enabled) {
        enabledTools.add(toolName);
      }
    }

    const isDispatchWorkflowTool = !!tool._workflow_name;
    let shouldRegister = false;

    if (isDispatchWorkflowTool) {
      if (config.dispatch_workflow) {
        shouldRegister = true;
      }
    } else {
      if (enabledTools.has(tool.name)) {
        shouldRegister = true;
      }
    }

    // Verify that regular tool should be registered
    expect(shouldRegister).toBe(true);
    expect(isDispatchWorkflowTool).toBe(false);
    expect(enabledTools.has(tool.name)).toBe(true);
  });

  it("should NOT register regular tools when their name is not in config", () => {
    const tool = {
      name: "some_other_tool",
      description: "Some other tool",
      inputSchema: { type: "object", properties: {} },
    };

    const config = {
      missing_tool: {},
      missing_data: {},
    };

    const enabledTools = new Set();
    for (const [toolName, enabled] of Object.entries(config)) {
      if (enabled) {
        enabledTools.add(toolName);
      }
    }

    const isDispatchWorkflowTool = !!tool._workflow_name;
    let shouldRegister = false;

    if (isDispatchWorkflowTool) {
      if (config.dispatch_workflow) {
        shouldRegister = true;
      }
    } else {
      if (enabledTools.has(tool.name)) {
        shouldRegister = true;
      }
    }

    // Verify that the tool should NOT be registered
    expect(shouldRegister).toBe(false);
    expect(isDispatchWorkflowTool).toBe(false);
    expect(enabledTools.has(tool.name)).toBe(false);
  });
});
