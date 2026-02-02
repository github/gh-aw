// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import { loadTools, attachHandlers, registerPredefinedTools, registerDynamicTools } from "./safe_outputs_tools_loader.cjs";

describe("safe_outputs_tools_loader", () => {
  let mockServer;
  let testToolsPath;

  beforeEach(() => {
    mockServer = {
      debug: vi.fn(),
      tools: {},
    };

    const testId = Math.random().toString(36).substring(7);
    testToolsPath = `/tmp/test-tools-loader-${testId}/tools.json`;
    process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = testToolsPath;
  });

  afterEach(() => {
    try {
      if (fs.existsSync(testToolsPath)) {
        fs.unlinkSync(testToolsPath);
      }
      const testDir = path.dirname(testToolsPath);
      if (fs.existsSync(testDir)) {
        fs.rmSync(testDir, { recursive: true, force: true });
      }
    } catch (error) {
      // Ignore cleanup errors
    }

    delete process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;
  });

  describe("loadTools", () => {
    it("should load tools from valid JSON file", () => {
      const toolsDir = path.dirname(testToolsPath);
      fs.mkdirSync(toolsDir, { recursive: true });

      const tools = [
        { name: "tool1", description: "Tool 1" },
        { name: "tool2", description: "Tool 2" },
      ];
      fs.writeFileSync(testToolsPath, JSON.stringify(tools));

      const result = loadTools(mockServer);

      expect(result).toEqual(tools);
      expect(mockServer.debug).toHaveBeenCalledWith(expect.stringContaining("Successfully parsed 2 tools"));
    });

    it("should return empty array when file doesn't exist", () => {
      const result = loadTools(mockServer);

      expect(result).toEqual([]);
      expect(mockServer.debug).toHaveBeenCalledWith(expect.stringContaining("does not exist"));
    });

    it("should return empty array when JSON is invalid", () => {
      const toolsDir = path.dirname(testToolsPath);
      fs.mkdirSync(toolsDir, { recursive: true });
      fs.writeFileSync(testToolsPath, "{ invalid json }");

      const result = loadTools(mockServer);

      expect(result).toEqual([]);
      expect(mockServer.debug).toHaveBeenCalledWith(expect.stringContaining("Error reading tools file"));
    });

    it("should use default path when env var not set", () => {
      delete process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;

      // Clean up the default path to ensure isolation from other test runs/jobs
      const defaultPath = "/opt/gh-aw/safeoutputs/tools.json";
      const defaultDir = path.dirname(defaultPath);
      if (fs.existsSync(defaultPath)) {
        fs.unlinkSync(defaultPath);
      }
      if (fs.existsSync(defaultDir)) {
        fs.rmSync(defaultDir, { recursive: true, force: true });
      }

      const result = loadTools(mockServer);

      expect(result).toEqual([]);
      expect(mockServer.debug).toHaveBeenCalledWith(expect.stringContaining("/opt/gh-aw/safeoutputs/tools.json"));
    });
  });

  describe("attachHandlers", () => {
    it("should attach create_pull_request handler", () => {
      const tools = [
        { name: "create_pull_request", description: "Create PR" },
        { name: "other_tool", description: "Other" },
      ];
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
      };

      const result = attachHandlers(tools, handlers);

      expect(result[0].handler).toBe(handlers.createPullRequestHandler);
      expect(result[1].handler).toBeUndefined();
    });

    it("should attach push_to_pull_request_branch handler", () => {
      const tools = [{ name: "push_to_pull_request_branch", description: "Push to PR" }];
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
      };

      const result = attachHandlers(tools, handlers);

      expect(result[0].handler).toBe(handlers.pushToPullRequestBranchHandler);
    });

    it("should attach upload_asset handler", () => {
      const tools = [{ name: "upload_asset", description: "Upload Asset" }];
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
      };

      const result = attachHandlers(tools, handlers);

      expect(result[0].handler).toBe(handlers.uploadAssetHandler);
    });

    it("should attach multiple handlers", () => {
      const tools = [
        { name: "create_pull_request", description: "Create PR" },
        { name: "upload_asset", description: "Upload" },
        { name: "push_to_pull_request_branch", description: "Push" },
      ];
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
      };

      const result = attachHandlers(tools, handlers);

      expect(result[0].handler).toBe(handlers.createPullRequestHandler);
      expect(result[1].handler).toBe(handlers.uploadAssetHandler);
      expect(result[2].handler).toBe(handlers.pushToPullRequestBranchHandler);
    });

    it("should not modify tools without matching handlers", () => {
      const tools = [{ name: "unknown_tool", description: "Unknown" }];
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
      };

      const result = attachHandlers(tools, handlers);

      expect(result[0].handler).toBeUndefined();
    });

    it("should attach dispatch_workflow handler for tools with _workflow_name", () => {
      const tools = [{ name: "test_workflow", description: "Test workflow", _workflow_name: "test-workflow" }];
      const defaultHandler = vi.fn(type => vi.fn());
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
        defaultHandler: defaultHandler,
      };

      const result = attachHandlers(tools, handlers);

      // Handler should be attached
      expect(result[0].handler).toBeDefined();
      expect(typeof result[0].handler).toBe("function");

      // Call the handler to verify it uses dispatch_workflow type
      const mockArgs = { test_param: "value" };
      result[0].handler(mockArgs);

      // Verify defaultHandler was called with dispatch_workflow type
      expect(defaultHandler).toHaveBeenCalledWith("dispatch_workflow");
    });

    it("should wrap args in inputs property for dispatch_workflow handler", () => {
      const tools = [{ name: "ci_workflow", description: "CI workflow", _workflow_name: "ci" }];
      const mockHandlerFunction = vi.fn();
      const defaultHandler = vi.fn(() => mockHandlerFunction);
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
        defaultHandler: defaultHandler,
      };

      const result = attachHandlers(tools, handlers);

      // Call the handler
      const mockArgs = { input1: "value1", input2: "value2" };
      result[0].handler(mockArgs);

      // Verify the handler function was called with workflow_name and inputs wrapped
      expect(mockHandlerFunction).toHaveBeenCalledWith({
        workflow_name: "ci",
        inputs: {
          input1: "value1",
          input2: "value2",
        },
      });
    });

    it("should handle dispatch_workflow with no inputs (empty object)", () => {
      const tools = [{ name: "no_inputs_workflow", description: "No inputs workflow", _workflow_name: "no-inputs" }];
      const mockHandlerFunction = vi.fn();
      const defaultHandler = vi.fn(() => mockHandlerFunction);
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
        defaultHandler: defaultHandler,
      };

      const result = attachHandlers(tools, handlers);

      // Call the handler with empty object (typical for MCP tools with no inputs)
      result[0].handler({});

      // Verify inputs is still included as empty object
      expect(mockHandlerFunction).toHaveBeenCalledWith({
        workflow_name: "no-inputs",
        inputs: {},
      });
    });

    it("should handle dispatch_workflow with undefined args", () => {
      const tools = [{ name: "undefined_workflow", description: "Undefined workflow", _workflow_name: "undefined-test" }];
      const mockHandlerFunction = vi.fn();
      const defaultHandler = vi.fn(() => mockHandlerFunction);
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
        defaultHandler: defaultHandler,
      };

      const result = attachHandlers(tools, handlers);

      // Call the handler with undefined (edge case)
      result[0].handler(undefined);

      // When args is undefined, inputs should not be included
      // The dispatch_workflow handler will handle missing inputs property
      expect(mockHandlerFunction).toHaveBeenCalledWith({
        workflow_name: "undefined-test",
      });
    });
  });

  describe("registerPredefinedTools", () => {
    it("should register enabled tools", () => {
      const tools = [
        { name: "create_pull_request", description: "Create PR" },
        { name: "upload_asset", description: "Upload" },
      ];
      const config = {
        create_pull_request: true,
      };
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerPredefinedTools(mockServer, tools, config, registerTool, normalizeTool);

      expect(registerTool).toHaveBeenCalledWith(mockServer, tools[0]);
      expect(registerTool).not.toHaveBeenCalledWith(mockServer, tools[1]);
    });

    it("should handle config with dashes", () => {
      const tools = [{ name: "create_pull_request", description: "Create PR" }];
      const config = {
        "create-pull-request": true,
      };
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerPredefinedTools(mockServer, tools, config, registerTool, normalizeTool);

      expect(registerTool).toHaveBeenCalledWith(mockServer, tools[0]);
    });

    it("should not register disabled tools", () => {
      const tools = [{ name: "create_pull_request", description: "Create PR" }];
      const config = {
        upload_asset: true,
      };
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerPredefinedTools(mockServer, tools, config, registerTool, normalizeTool);

      expect(registerTool).not.toHaveBeenCalled();
    });

    it("should register dispatch_workflow tools with _workflow_name metadata", () => {
      const tools = [
        { name: "test_workflow", description: "Test workflow", _workflow_name: "test-workflow" },
        { name: "ci_workflow", description: "CI workflow", _workflow_name: "ci" },
        { name: "other_tool", description: "Other tool" },
      ];
      const config = {
        dispatch_workflow: {
          workflows: ["test-workflow", "ci"],
          max: 2,
        },
      };
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerPredefinedTools(mockServer, tools, config, registerTool, normalizeTool);

      // Should register both dispatch_workflow tools
      expect(registerTool).toHaveBeenCalledTimes(2);
      expect(registerTool).toHaveBeenCalledWith(mockServer, tools[0]);
      expect(registerTool).toHaveBeenCalledWith(mockServer, tools[1]);
      // Should NOT register the tool without _workflow_name
      expect(registerTool).not.toHaveBeenCalledWith(mockServer, tools[2]);
    });

    it("should not register dispatch_workflow tools when dispatch_workflow is not in config", () => {
      const tools = [{ name: "test_workflow", description: "Test workflow", _workflow_name: "test-workflow" }];
      const config = {
        create_issue: true,
      };
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerPredefinedTools(mockServer, tools, config, registerTool, normalizeTool);

      // Should not register dispatch_workflow tool when config doesn't include it
      expect(registerTool).not.toHaveBeenCalled();
    });

    it("should register both regular and dispatch_workflow tools", () => {
      const tools = [
        { name: "create_pull_request", description: "Create PR" },
        { name: "test_workflow", description: "Test workflow", _workflow_name: "test-workflow" },
      ];
      const config = {
        create_pull_request: true,
        dispatch_workflow: {
          workflows: ["test-workflow"],
          max: 1,
        },
      };
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerPredefinedTools(mockServer, tools, config, registerTool, normalizeTool);

      // Should register both the regular tool and dispatch_workflow tool
      expect(registerTool).toHaveBeenCalledTimes(2);
      expect(registerTool).toHaveBeenCalledWith(mockServer, tools[0]);
      expect(registerTool).toHaveBeenCalledWith(mockServer, tools[1]);
    });
  });

  describe("registerDynamicTools", () => {
    it("should register dynamic safe-job tool", () => {
      const tools = [];
      const config = {
        custom_job: {
          description: "Custom job",
          output: "Job completed",
        },
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      expect(registerTool).toHaveBeenCalled();
      const toolArg = registerTool.mock.calls[0][1];
      expect(toolArg.name).toBe("custom_job");
      expect(toolArg.description).toBe("Custom job");
      expect(toolArg.handler).toBeDefined();
    });

    it("should not register predefined tools as dynamic tools", () => {
      mockServer.tools = { create_pull_request: {} };

      const tools = [{ name: "create_pull_request", description: "Create PR" }];
      const config = {
        create_pull_request: true,
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      expect(registerTool).not.toHaveBeenCalled();
    });

    it("should create dynamic tool with input schema", () => {
      const tools = [];
      const config = {
        custom_job: {
          description: "Custom job",
          inputs: {
            required_field: {
              type: "string",
              description: "Required field",
              required: true,
            },
            optional_field: {
              type: "number",
              description: "Optional field",
            },
          },
        },
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      const toolArg = registerTool.mock.calls[0][1];
      expect(toolArg.inputSchema.properties).toBeDefined();
      expect(toolArg.inputSchema.properties.required_field).toBeDefined();
      expect(toolArg.inputSchema.properties.optional_field).toBeDefined();
      expect(toolArg.inputSchema.required).toEqual(["required_field"]);
    });

    it("should create dynamic tool with enum options", () => {
      const tools = [];
      const config = {
        custom_job: {
          inputs: {
            status: {
              type: "string",
              options: ["success", "failure", "pending"],
            },
          },
        },
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      const toolArg = registerTool.mock.calls[0][1];
      expect(toolArg.inputSchema.properties.status.enum).toEqual(["success", "failure", "pending"]);
    });

    it("should convert choice type to string type with enum", () => {
      const tools = [];
      const config = {
        custom_job: {
          inputs: {
            environment: {
              type: "choice",
              description: "Target environment",
              required: true,
              options: ["staging", "production"],
            },
          },
        },
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      const toolArg = registerTool.mock.calls[0][1];
      expect(toolArg.inputSchema.properties.environment.type).toBe("string");
      expect(toolArg.inputSchema.properties.environment.enum).toEqual(["staging", "production"]);
      expect(toolArg.inputSchema.properties.environment.description).toBe("Target environment");
      expect(toolArg.inputSchema.required).toEqual(["environment"]);
    });

    it("should use default description if not provided", () => {
      const tools = [];
      const config = {
        "custom-job": {},
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      const toolArg = registerTool.mock.calls[0][1];
      expect(toolArg.description).toBe("Custom safe-job: custom-job");
    });
  });
});
