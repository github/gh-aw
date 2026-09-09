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
      const defaultPath = `${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/tools.json`;
      const defaultDir = path.dirname(defaultPath);
      if (fs.existsSync(defaultPath)) {
        fs.unlinkSync(defaultPath);
      }
      if (fs.existsSync(defaultDir)) {
        fs.rmSync(defaultDir, { recursive: true, force: true });
      }

      const result = loadTools(mockServer);

      expect(result).toEqual([]);
      expect(mockServer.debug).toHaveBeenCalledWith(expect.stringContaining(`${process.env.RUNNER_TEMP}/gh-aw/safeoutputs/tools.json`));
    });
  });

  describe("attachHandlers", () => {
    it("attaches Azure DevOps handlers to namespaced public tool names", () => {
      const tools = [{ name: "ado_create_work_item" }, { name: "ado_update_work_item" }, { name: "ado_comment_on_work_item" }, { name: "ado_assign_work_item" }, { name: "ado_link_work_items" }, { name: "ado_upload_workitem_attachment" }];
      const handlers = {
        createWorkItemHandler: vi.fn(),
        updateWorkItemHandler: vi.fn(),
        commentOnWorkItemHandler: vi.fn(),
        assignWorkItemHandler: vi.fn(),
        linkWorkItemsHandler: vi.fn(),
        uploadWorkItemAttachmentHandler: vi.fn(),
      };

      const result = attachHandlers(tools, handlers);

      expect(result.map(tool => tool.handler)).toEqual([
        handlers.createWorkItemHandler,
        handlers.updateWorkItemHandler,
        handlers.commentOnWorkItemHandler,
        handlers.assignWorkItemHandler,
        handlers.linkWorkItemsHandler,
        handlers.uploadWorkItemAttachmentHandler,
      ]);
    });

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

    it("should attach create_issue handler", () => {
      const tools = [{ name: "create_issue", description: "Create issue" }];
      const handlers = {
        createIssueHandler: vi.fn(),
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
      };

      const result = attachHandlers(tools, handlers);

      expect(result[0].handler).toBe(handlers.createIssueHandler);
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

    it("should not attach dispatch_workflow handler for whitespace-only _workflow_name", () => {
      const tools = [{ name: "test_workflow", description: "Test workflow", _workflow_name: "   " }];
      const mockHandlerFunction = vi.fn();
      const defaultHandler = vi.fn(() => mockHandlerFunction);
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
        defaultHandler: defaultHandler,
      };

      const result = attachHandlers(tools, handlers);

      expect(result[0].handler).toBeDefined();
      result[0].handler({ test_param: "value" });
      expect(mockHandlerFunction).toHaveBeenCalledWith({ test_param: "value" });
      expect(defaultHandler).toHaveBeenCalledWith("test_workflow");
      expect(defaultHandler).not.toHaveBeenCalledWith("dispatch_workflow");
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

    it("should pass ref as top-level field for dispatch_workflow handler", () => {
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

      result[0].handler({ ref: "feature-branch", input1: "value1" });

      expect(mockHandlerFunction).toHaveBeenCalledWith({
        workflow_name: "ci",
        ref: "feature-branch",
        inputs: {
          input1: "value1",
        },
      });
      expect(mockHandlerFunction).not.toHaveBeenCalledWith(
        expect.objectContaining({
          inputs: expect.objectContaining({
            ref: expect.anything(),
          }),
        })
      );
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

    it("should drop unknown keys for strict schema tools", () => {
      const createIssueHandler = vi.fn();
      const tools = [
        {
          name: "create_issue",
          description: "Create issue",
          inputSchema: {
            type: "object",
            properties: {
              title: { type: "string" },
              body: { type: "string" },
            },
            additionalProperties: false,
          },
        },
      ];
      const handlers = {
        createIssueHandler,
      };

      const result = attachHandlers(tools, handlers);
      result[0].handler({
        title: "hello",
        body: "world",
        unknown_field: "extra",
      });

      expect(createIssueHandler).toHaveBeenCalledWith({
        title: "hello",
        body: "world",
      });
      expect(createIssueHandler).not.toHaveBeenCalledWith(
        expect.objectContaining({
          unknown_field: expect.anything(),
        })
      );
    });

    it("should log stripped key names for strict schema tools", () => {
      const createIssueHandler = vi.fn();
      const logger = { debug: vi.fn() };
      const tools = [
        {
          name: "create_issue",
          description: "Create issue",
          inputSchema: {
            type: "object",
            properties: {
              title: { type: "string" },
            },
            additionalProperties: false,
          },
        },
      ];
      const handlers = {
        createIssueHandler,
      };

      const result = attachHandlers(tools, handlers, logger);
      result[0].handler({
        title: "hello",
        unknown_field_a: "extra-a",
        unknown_field_b: "extra-b",
      });

      expect(logger.debug).toHaveBeenCalledWith('Stripped unknown keys for strict schema tool \'create_issue\': ["unknown_field_a","unknown_field_b"]');
    });

    it("should drop unknown keys before wrapping dispatch_workflow inputs", () => {
      const mockHandlerFunction = vi.fn();
      const defaultHandler = vi.fn(() => mockHandlerFunction);
      const tools = [
        {
          name: "dispatch_ci",
          description: "Dispatch CI",
          _workflow_name: "ci",
          inputSchema: {
            type: "object",
            properties: {
              issue_number: { type: "string" },
            },
            additionalProperties: false,
          },
        },
      ];
      const handlers = {
        defaultHandler,
      };

      const result = attachHandlers(tools, handlers);
      result[0].handler({
        issue_number: "123",
        unknown_input: "extra",
      });

      expect(mockHandlerFunction).toHaveBeenCalledWith({
        workflow_name: "ci",
        inputs: {
          issue_number: "123",
        },
      });
      expect(mockHandlerFunction).not.toHaveBeenCalledWith(
        expect.objectContaining({
          inputs: expect.objectContaining({
            unknown_input: expect.anything(),
          }),
        })
      );
    });

    it("should keep strict-schema ref top-level for dispatch_workflow handler", () => {
      const mockHandlerFunction = vi.fn();
      const defaultHandler = vi.fn(() => mockHandlerFunction);
      const tools = [
        {
          name: "dispatch_ci",
          description: "Dispatch CI",
          _workflow_name: "ci",
          inputSchema: {
            type: "object",
            properties: {
              ref: { type: "string" },
              issue_number: { type: "string" },
            },
            additionalProperties: false,
          },
        },
      ];
      const handlers = {
        defaultHandler,
      };

      const result = attachHandlers(tools, handlers);
      result[0].handler({
        ref: "feature-branch",
        issue_number: "123",
      });

      expect(mockHandlerFunction).toHaveBeenCalledWith({
        workflow_name: "ci",
        ref: "feature-branch",
        inputs: {
          issue_number: "123",
        },
      });
    });

    it("should log stripped key names before wrapping dispatch_workflow inputs", () => {
      const mockHandlerFunction = vi.fn();
      const defaultHandler = vi.fn(() => mockHandlerFunction);
      const logger = { debug: vi.fn() };
      const tools = [
        {
          name: "dispatch_ci",
          description: "Dispatch CI",
          _workflow_name: "ci",
          inputSchema: {
            type: "object",
            properties: {
              issue_number: { type: "string" },
            },
            additionalProperties: false,
          },
        },
      ];
      const handlers = {
        defaultHandler,
      };

      const result = attachHandlers(tools, handlers, logger);
      result[0].handler({
        issue_number: "123",
        unknown_input: "extra",
      });

      expect(logger.debug).toHaveBeenCalledWith("Stripped unknown keys for strict schema tool 'dispatch_ci': [\"unknown_input\"]");
    });

    it("should sanitize strict-schema args for tools using defaultHandler fallback", () => {
      const mockHandlerFunction = vi.fn();
      const defaultHandler = vi.fn(() => mockHandlerFunction);
      const tools = [
        {
          name: "some_tool",
          description: "Some tool",
          inputSchema: {
            type: "object",
            properties: {
              allowed: { type: "string" },
            },
            additionalProperties: false,
          },
        },
      ];
      const handlers = {
        defaultHandler,
      };

      const result = attachHandlers(tools, handlers);
      result[0].handler({
        allowed: "value",
        unknown_input: "extra",
      });

      expect(defaultHandler).toHaveBeenCalledWith("some_tool");
      expect(mockHandlerFunction).toHaveBeenCalledWith({
        allowed: "value",
      });
      expect(mockHandlerFunction).not.toHaveBeenCalledWith(
        expect.objectContaining({
          unknown_input: expect.anything(),
        })
      );
    });

    it("should attach create_pull_request_review_comment handler", () => {
      const tools = [{ name: "create_pull_request_review_comment", description: "Create review comment" }];
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
        createPullRequestReviewCommentHandler: vi.fn(),
        submitPullRequestReviewHandler: vi.fn(),
      };

      const result = attachHandlers(tools, handlers);

      expect(result[0].handler).toBe(handlers.createPullRequestReviewCommentHandler);
    });

    it("should attach submit_pull_request_review handler", () => {
      const tools = [{ name: "submit_pull_request_review", description: "Submit PR review" }];
      const handlers = {
        createPullRequestHandler: vi.fn(),
        pushToPullRequestBranchHandler: vi.fn(),
        uploadAssetHandler: vi.fn(),
        createPullRequestReviewCommentHandler: vi.fn(),
        submitPullRequestReviewHandler: vi.fn(),
      };

      const result = attachHandlers(tools, handlers);

      expect(result[0].handler).toBe(handlers.submitPullRequestReviewHandler);
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

      expect(registerTool).toHaveBeenCalledWith(
        mockServer,
        expect.objectContaining({
          name: "create_pull_request",
          description: expect.stringContaining("real pull request intent"),
        })
      );
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

      expect(registerTool).toHaveBeenCalledWith(
        mockServer,
        expect.objectContaining({
          name: "create_pull_request",
          description: expect.stringContaining("real pull request intent"),
        })
      );
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

    it("should not register dispatch_workflow tools with whitespace-only _workflow_name", () => {
      const tools = [{ name: "test_workflow", description: "Test workflow", _workflow_name: "   " }];
      const config = {
        dispatch_workflow: {
          workflows: ["test-workflow"],
          max: 1,
        },
      };
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerPredefinedTools(mockServer, tools, config, registerTool, normalizeTool);

      expect(registerTool).not.toHaveBeenCalled();
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
      expect(registerTool).toHaveBeenCalledWith(
        mockServer,
        expect.objectContaining({
          name: "create_pull_request",
          description: expect.stringContaining("real pull request intent"),
        })
      );
      expect(registerTool).toHaveBeenCalledWith(mockServer, tools[1]);
    });

    it("should enrich create_pull_request description with target repo and safety guidance", () => {
      const tools = [
        {
          name: "create_pull_request",
          description: "Create PR",
          inputSchema: { properties: { repo: { description: "Target repo" } } },
        },
      ];
      const config = {
        create_pull_request: {
          "target-repo": "octo/docs",
        },
      };
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerPredefinedTools(mockServer, tools, config, registerTool, normalizeTool);

      const registeredTool = registerTool.mock.calls[0][1];
      expect(registeredTool.description).toContain("configured to create pull requests in 'octo/docs'");
      expect(registeredTool.description).toContain("Do not use it for tests, auth checks, or probing");
      expect(registeredTool.inputSchema.properties.repo.description).toContain("Configured default: 'octo/docs'");
    });

    it("should enrich other write-intent tool descriptions with safety guidance", () => {
      const tools = [
        { name: "create_issue", description: "Create issue" },
        { name: "add_comment", description: "Add comment" },
        { name: "push_to_pull_request_branch", description: "Push to PR" },
      ];
      const config = {
        create_issue: {},
        add_comment: {},
        push_to_pull_request_branch: {},
      };
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerPredefinedTools(mockServer, tools, config, registerTool, normalizeTool);

      expect(registerTool).toHaveBeenNthCalledWith(
        1,
        mockServer,
        expect.objectContaining({
          name: "create_issue",
          description: expect.stringContaining("real issue intent"),
        })
      );
      expect(registerTool).toHaveBeenNthCalledWith(
        2,
        mockServer,
        expect.objectContaining({
          name: "add_comment",
          description: expect.stringContaining("real comment intent"),
        })
      );
      expect(registerTool).toHaveBeenNthCalledWith(
        3,
        mockServer,
        expect.objectContaining({
          name: "push_to_pull_request_branch",
          description: expect.stringContaining("real PR branch update intent"),
        })
      );
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

    it("should not register a generic call_workflow tool when a renamed call_workflow tool exists", () => {
      const tools = [{ name: "agent_sandbox_stack", description: "Call agent-sandbox-stack", _call_workflow_name: "agent-sandbox-stack" }];
      const config = {
        call_workflow: { workflows: ["agent-sandbox-stack"] },
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      expect(registerTool).not.toHaveBeenCalled();
    });

    it("should register generic call_workflow tool when renamed call_workflow metadata is invalid", () => {
      const tools = [{ name: "agent_sandbox_stack", description: "Call agent-sandbox-stack", _call_workflow_name: "   " }];
      const config = {
        call_workflow: { workflows: ["agent-sandbox-stack"] },
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      expect(registerTool).toHaveBeenCalledTimes(1);
      expect(registerTool.mock.calls[0][1].name).toBe("call_workflow");
    });

    it("should not register generic dispatch_workflow or dispatch_repository tools when renamed tools exist", () => {
      const tools = [
        { name: "my_workflow", description: "Dispatch my-workflow", _workflow_name: "my-workflow" },
        { name: "my_dispatch", description: "Dispatch repository event", _dispatch_repository_tool: "my_dispatch" },
      ];
      const config = {
        dispatch_workflow: { workflows: ["my-workflow"] },
        dispatch_repository: { tools: ["my_dispatch"] },
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      expect(registerTool).not.toHaveBeenCalled();
    });

    it("should register generic dispatch_workflow tool when renamed dispatch_workflow metadata is invalid", () => {
      const tools = [{ name: "my_workflow", description: "Dispatch my-workflow", _workflow_name: 123 }];
      const config = {
        dispatch_workflow: { workflows: ["my-workflow"] },
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      expect(registerTool).toHaveBeenCalledTimes(1);
      expect(registerTool.mock.calls[0][1].name).toBe("dispatch_workflow");
    });

    it("should not register generic tools for handler-only or global config keys", () => {
      const tools = [{ name: "report_incomplete", description: "Report incomplete" }];
      const config = {
        report_incomplete: {},
        create_report_incomplete_issue: {},
        mentions: { allowed: [] },
        max_bot_mentions: 3,
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      expect(registerTool).not.toHaveBeenCalled();
    });

    it("should still register a generic tool for unrelated safe-job config keys", () => {
      const tools = [{ name: "agent_sandbox_stack", description: "Call agent-sandbox-stack", _call_workflow_name: "agent-sandbox-stack" }];
      const config = {
        call_workflow: { workflows: ["agent-sandbox-stack"] },
        custom_job: { description: "Custom job" },
      };
      const outputFile = "/tmp/test-output.jsonl";
      const registerTool = vi.fn();
      const normalizeTool = name => name.replace(/-/g, "_");

      registerDynamicTools(mockServer, tools, config, outputFile, registerTool, normalizeTool);

      expect(registerTool).toHaveBeenCalledTimes(1);
      expect(registerTool.mock.calls[0][1].name).toBe("custom_job");
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
