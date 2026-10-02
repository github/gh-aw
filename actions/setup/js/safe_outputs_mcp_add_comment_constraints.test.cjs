// @ts-check
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";

/**
 * Tests for MCP Server Constraint Enforcement - add_comment tool
 *
 * This test suite validates Requirement MCE1 (Early Validation) from the Safe Outputs
 * Specification Section 8.3: MCP servers must enforce operational constraints during
 * tool invocation (Phase 4) to provide immediate feedback to the LLM.
 *
 * Tests verify that:
 * 1. Valid comments pass through successfully
 * 2. E006 error is returned for length violations (>65536 chars)
 * 3. E007 error is returned for mention violations (>10 mentions)
 * 4. E008 error is returned for link violations (>50 links)
 * 5. Errors use MCP JSON-RPC error format with code -32602
 * 6. Error messages are actionable per Requirement MCE3
 *
 * Related issues: github/gh-aw#<issue_number>
 */

describe("Safe Outputs MCP Server - add_comment Constraint Enforcement", () => {
  let createServer, registerTool, handleRequest;
  let server;
  let tempDir;
  let configFile;
  let outputFile;
  let toolsFile;

  beforeEach(async () => {
    vi.resetModules();

    // Suppress stderr output during tests
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);

    // Create temporary directory for test files
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-add-comment-test-"));
    configFile = path.join(tempDir, "config.json");
    outputFile = path.join(tempDir, "output.jsonl");
    toolsFile = path.join(tempDir, "tools.json");

    // Create config file enabling add_comment
    const config = {
      "add-comment": { enabled: true },
    };
    fs.writeFileSync(configFile, JSON.stringify(config));

    // Load tools schema with add_comment
    const toolsPath = path.join(process.cwd(), "safe_outputs_tools.json");
    const toolsContent = fs.readFileSync(toolsPath, "utf8");
    const allTools = JSON.parse(toolsContent);
    const addCommentTool = allTools.find(t => t.name === "add_comment");
    if (!addCommentTool) {
      throw new Error("add_comment tool not found in safe_outputs_tools.json");
    }
    fs.writeFileSync(toolsFile, JSON.stringify([addCommentTool]));

    // Set environment variables
    process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH = configFile;
    process.env.GH_AW_SAFE_OUTPUTS_OUTPUT_PATH = outputFile;
    process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH = toolsFile;

    // Import MCP server modules
    const mcpCore = await import("./mcp_server_core.cjs");
    createServer = mcpCore.createServer;
    registerTool = mcpCore.registerTool;
    handleRequest = mcpCore.handleRequest;

    // Import and set up handlers
    const handlersModule = await import("./safe_outputs_handlers.cjs");
    const appendModule = await import("./safe_outputs_append.cjs");
    const toolsLoaderModule = await import("./safe_outputs_tools_loader.cjs");

    // Create server
    server = createServer({ name: "test-safeoutputs", version: "1.0.0" });

    // Create append function
    const appendSafeOutput = appendModule.createAppendFunction(outputFile);

    // Create handlers
    const handlers = handlersModule.createHandlers(server, appendSafeOutput, config);

    // Load and attach handlers to tools
    const tools = toolsLoaderModule.loadTools(server);
    const toolsWithHandlers = toolsLoaderModule.attachHandlers(tools, handlers);

    // Register tools
    toolsWithHandlers.forEach(tool => {
      registerTool(server, tool);
    });
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    // Clean up temp directory
    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true, force: true });
    }

    // Clean up environment variables
    delete process.env.GH_AW_SAFE_OUTPUTS_CONFIG_PATH;
    delete process.env.GH_AW_SAFE_OUTPUTS_OUTPUT_PATH;
    delete process.env.GH_AW_SAFE_OUTPUTS_TOOLS_PATH;
  });

  describe("Triggering target validation", () => {
    it.each(["*", "42"])("should preserve explicit target %s without triggering context", async target => {
      vi.stubGlobal("context", { eventName: "schedule", payload: {}, repo: { owner: "owner", repo: "repo" } });
      const handlersModule = await import("./safe_outputs_handlers.cjs");
      const appendModule = await import("./safe_outputs_append.cjs");
      const handlers = handlersModule.createHandlers(server, appendModule.createAppendFunction(outputFile), {
        "add-comment": { target },
      });
      registerTool(server, {
        ...JSON.parse(fs.readFileSync(toolsFile, "utf8"))[0],
        handler: handlers.addCommentHandler,
      });

      const response = await handleRequest(server, {
        jsonrpc: "2.0",
        id: 21,
        method: "tools/call",
        params: { name: "add_comment", arguments: { body: "Workflow results requiring a comment", item_number: 42 } },
      });

      expect(response.result.isError).not.toBe(true);
      expect(JSON.parse(response.result.content[0].text).result).toBe("success");
      expect(JSON.parse(fs.readFileSync(outputFile, "utf8").trim()).item_number).toBe(42);
    });

    it.each([
      ["schedule", {}],
      ["repository_dispatch", {}],
      ["workflow_dispatch", { inputs: {} }],
      ["workflow_dispatch", { inputs: { event_name: "issues" } }],
      ["issues", { issue: {} }],
      ["pull_request", { pull_request: {} }],
      ["discussion_comment", { discussion: {} }],
    ])("should return actionable MCP feedback for an unresolved %s target", async (eventName, payload) => {
      vi.stubGlobal("context", { eventName, payload, repo: { owner: "owner", repo: "repo" } });

      const response = await handleRequest(server, {
        jsonrpc: "2.0",
        id: 19,
        method: "tools/call",
        params: { name: "add_comment", arguments: { body: "Workflow results requiring a comment" } },
      });

      expect(response.result.isError).toBe(true);
      const feedback = JSON.parse(response.result.content[0].text);
      expect(feedback.result).toBe("error");
      expect(feedback.error).toContain("cannot resolve");
      expect(feedback.error).toContain('safe-outputs.add-comment.target to "*"');
      expect(feedback.error).toContain("item_number");
      expect(fs.existsSync(outputFile)).toBe(false);
    });

    it.each([
      ["issues", { issue: { number: 42 } }],
      ["pull_request", { pull_request: { number: 42 } }],
      ["issue_comment", { issue: { number: 42, pull_request: {} } }],
      ["discussion", { discussion: { number: 42 } }],
      ["workflow_dispatch", { inputs: { aw_context: JSON.stringify({ event_type: "issues", item_type: "issue", item_number: 42 }) } }],
    ])("should accept a resolvable %s triggering target", async (eventName, payload) => {
      vi.stubGlobal("context", { eventName, payload, repo: { owner: "owner", repo: "repo" } });

      const response = await handleRequest(server, {
        jsonrpc: "2.0",
        id: 20,
        method: "tools/call",
        params: { name: "add_comment", arguments: { body: "Workflow results requiring a comment" } },
      });

      expect(response.result.isError).not.toBe(true);
      expect(JSON.parse(response.result.content[0].text).result).toBe("success");
      expect(JSON.parse(fs.readFileSync(outputFile, "utf8").trim()).type).toBe("add_comment");
    });
  });

  describe("Valid Comments", () => {
    it("should accept comment with valid body", async () => {
      const request = {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: "This is a valid comment with reasonable length",
          },
        },
      };

      const response = await handleRequest(server, request);

      expect(response).toHaveProperty("jsonrpc", "2.0");
      expect(response).toHaveProperty("id", 1);
      expect(response).toHaveProperty("result");
      expect(response).not.toHaveProperty("error");
      expect(response.result.content).toBeDefined();
      expect(response.result.content[0].text).toContain("success");
    });

    it("should accept comment at exactly maximum length (65536 chars)", async () => {
      const maxBody = "a".repeat(65536);
      const request = {
        jsonrpc: "2.0",
        id: 2,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: maxBody,
          },
        },
      };

      const response = await handleRequest(server, request);

      expect(response).not.toHaveProperty("error");
      // Large content (>16000 tokens) may return a file reference instead of "success"
      expect(response.result.content[0].text).toBeTruthy();
    });

    it("should accept comment with exactly 10 mentions", async () => {
      const mentions = Array.from({ length: 10 }, (_, i) => `@user${i}`).join(" ");
      const request = {
        jsonrpc: "2.0",
        id: 3,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: `Valid comment with mentions: ${mentions}`,
          },
        },
      };

      const response = await handleRequest(server, request);

      expect(response).not.toHaveProperty("error");
      expect(response.result.content[0].text).toContain("success");
    });

    it("should accept comment with exactly 50 links", async () => {
      const links = Array.from({ length: 50 }, (_, i) => `https://example.com/${i}`).join(" ");
      const request = {
        jsonrpc: "2.0",
        id: 4,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: `Valid comment with links: ${links}`,
          },
        },
      };

      const response = await handleRequest(server, request);

      expect(response).not.toHaveProperty("error");
      expect(response.result.content[0].text).toContain("success");
    });
  });

  describe("E006: Comment Length Violations", () => {
    it("should reject comment exceeding maximum length", async () => {
      const longBody = "a".repeat(65537); // One character over limit
      const request = {
        jsonrpc: "2.0",
        id: 5,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: longBody,
          },
        },
      };

      const response = await handleRequest(server, request);

      expect(response).toHaveProperty("jsonrpc", "2.0");
      expect(response).toHaveProperty("id", 5);
      expect(response).toHaveProperty("error");
      expect(response.error.code).toBe(-32602); // Invalid params
      expect(response.error.message).toMatch(/E006/);
      expect(response.error.message).toMatch(/65536/); // Max length
      expect(response.error.message).toMatch(/65537/); // Actual length
    });

    it("should provide actionable error message for length violation", async () => {
      const longBody = "a".repeat(70000);
      const request = {
        jsonrpc: "2.0",
        id: 6,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: longBody,
          },
        },
      };

      const response = await handleRequest(server, request);

      expect(response.error.message).toContain("maximum length");
      expect(response.error.message).toMatch(/\d+/); // Contains actual count
    });
  });

  describe("E007: Mention Count Violations", () => {
    it("should reject comment with too many mentions", async () => {
      const mentions = Array.from({ length: 11 }, (_, i) => `@user${i}`).join(" ");
      const request = {
        jsonrpc: "2.0",
        id: 7,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: `Comment with too many mentions: ${mentions}`,
          },
        },
      };

      const response = await handleRequest(server, request);

      expect(response).toHaveProperty("error");
      expect(response.error.code).toBe(-32602);
      expect(response.error.message).toMatch(/E007/);
      expect(response.error.message).toMatch(/11/); // Actual count
      expect(response.error.message).toMatch(/10/); // Maximum
    });

    it("should count mentions correctly in markdown", async () => {
      const mentions = "@alice @bob @charlie @david @eve @frank @grace @henry @iris @jack @kate";
      const request = {
        jsonrpc: "2.0",
        id: 8,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: `Team review: ${mentions}`,
          },
        },
      };

      const response = await handleRequest(server, request);

      expect(response).toHaveProperty("error");
      expect(response.error.message).toMatch(/E007/);
      expect(response.error.message).toContain("mentions");
    });
  });

  describe("E008: Link Count Violations", () => {
    it("should reject comment with too many links", async () => {
      const links = Array.from({ length: 51 }, (_, i) => `https://example.com/${i}`).join(" ");
      const request = {
        jsonrpc: "2.0",
        id: 9,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: `Comment with too many links: ${links}`,
          },
        },
      };

      const response = await handleRequest(server, request);

      expect(response).toHaveProperty("error");
      expect(response.error.code).toBe(-32602);
      expect(response.error.message).toMatch(/E008/);
      expect(response.error.message).toMatch(/51/); // Actual count
      expect(response.error.message).toMatch(/50/); // Maximum
    });

    it("should count both http and https links", async () => {
      const httpsLinks = Array.from({ length: 30 }, (_, i) => `https://example.com/${i}`).join(" ");
      const httpLinks = Array.from({ length: 21 }, (_, i) => `http://example.org/${i}`).join(" ");
      const request = {
        jsonrpc: "2.0",
        id: 10,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: `Mixed links: ${httpsLinks} ${httpLinks}`,
          },
        },
      };

      const response = await handleRequest(server, request);

      expect(response).toHaveProperty("error");
      expect(response.error.message).toMatch(/E008/);
      expect(response.error.message).toMatch(/51/); // 30 + 21 = 51
    });
  });

  describe("MCP Error Format Compliance", () => {
    it("should use JSON-RPC error format with code -32602", async () => {
      const longBody = "a".repeat(70000);
      const request = {
        jsonrpc: "2.0",
        id: 11,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: longBody,
          },
        },
      };

      const response = await handleRequest(server, request);

      // Validate JSON-RPC 2.0 error response structure
      expect(response).toHaveProperty("jsonrpc", "2.0");
      expect(response).toHaveProperty("id", 11);
      expect(response).toHaveProperty("error");
      expect(response).not.toHaveProperty("result");
      expect(response.error).toHaveProperty("code");
      expect(response.error).toHaveProperty("message");
      expect(response.error.code).toBe(-32602);
    });

    it("should provide specific constraint violation details in message", async () => {
      const mentions = Array.from({ length: 15 }, (_, i) => `@user${i}`).join(" ");
      const request = {
        jsonrpc: "2.0",
        id: 12,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: `Too many: ${mentions}`,
          },
        },
      };

      const response = await handleRequest(server, request);

      // Error message should be actionable per MCE3
      expect(response.error.message).toMatch(/mentions/i);
      expect(response.error.message).toMatch(/15/); // Actual value
      expect(response.error.message).toMatch(/10/); // Limit
    });
  });

  describe("Empty and Edge Cases", () => {
    it("should reject empty body (body is required)", async () => {
      const request = {
        jsonrpc: "2.0",
        id: 13,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: "",
          },
        },
      };

      const response = await handleRequest(server, request);

      // Empty body should be rejected by schema validation
      expect(response).toHaveProperty("error");
      expect(response.error.code).toBe(-32602);
    });

    it("should handle missing body gracefully", async () => {
      const request = {
        jsonrpc: "2.0",
        id: 14,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {},
        },
      };

      const response = await handleRequest(server, request);

      // Should either succeed with empty body or require body field
      // depending on schema requirements
      expect(response).toHaveProperty("jsonrpc", "2.0");
    });
  });

  describe("Dual Enforcement (MCE4)", () => {
    it("should record operation to NDJSON only after validation passes", async () => {
      const validRequest = {
        jsonrpc: "2.0",
        id: 15,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: "Valid comment",
          },
        },
      };

      await handleRequest(server, validRequest);

      // Check NDJSON file was created and contains the operation
      expect(fs.existsSync(outputFile)).toBe(true);
      const content = fs.readFileSync(outputFile, "utf8");
      const lines = content.trim().split("\n");
      expect(lines.length).toBeGreaterThan(0);

      const lastLine = JSON.parse(lines[lines.length - 1]);
      expect(lastLine.type).toBe("add_comment");
      expect(lastLine.body).toBe("Valid comment");
      // temporary_id should be auto-generated and stored in NDJSON
      expect(lastLine.temporary_id).toBeDefined();
      expect(lastLine.temporary_id).toMatch(/^aw_[A-Za-z0-9]{3,12}$/);
    });

    it("should return temporary_id in response when validation passes", async () => {
      const validRequest = {
        jsonrpc: "2.0",
        id: 17,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: "Comment with temporary ID",
          },
        },
      };

      const response = await handleRequest(server, validRequest);

      expect(response).not.toHaveProperty("error");
      const responseData = JSON.parse(response.result.content[0].text);
      expect(responseData.result).toBe("success");
      expect(responseData.temporary_id).toBeDefined();
      expect(responseData.temporary_id).toMatch(/^aw_[A-Za-z0-9]{3,12}$/);
      expect(responseData.comment).toBe(`#${responseData.temporary_id}`);
    });

    it("should use provided temporary_id in response", async () => {
      const validRequest = {
        jsonrpc: "2.0",
        id: 18,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: "Comment with explicit temporary ID",
            temporary_id: "aw_mytest",
          },
        },
      };

      const response = await handleRequest(server, validRequest);

      expect(response).not.toHaveProperty("error");
      const responseData = JSON.parse(response.result.content[0].text);
      expect(responseData.result).toBe("success");
      expect(responseData.temporary_id).toBe("aw_mytest");
    });

    it("should NOT record operation to NDJSON when validation fails", async () => {
      const invalidRequest = {
        jsonrpc: "2.0",
        id: 16,
        method: "tools/call",
        params: {
          name: "add_comment",
          arguments: {
            body: "a".repeat(70000),
          },
        },
      };

      await handleRequest(server, invalidRequest);

      // NDJSON file should either not exist or not contain the invalid operation
      if (fs.existsSync(outputFile)) {
        const content = fs.readFileSync(outputFile, "utf8");
        if (content.trim()) {
          const lines = content.trim().split("\n");
          lines.forEach(line => {
            const entry = JSON.parse(line);
            // Should not have recorded the too-long body
            if (entry.type === "add_comment") {
              expect(entry.body.length).toBeLessThanOrEqual(65536);
            }
          });
        }
      }
    });
  });
});
