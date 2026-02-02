// @ts-check
import { describe, it, expect, beforeEach, vi } from "vitest";
import fs from "fs";
import path from "path";

/**
 * Tests for MCP error message format validation
 *
 * This test suite validates that:
 * 1. Missing parameter errors include parameter names
 * 2. Error messages include expected format/examples when available
 * 3. Error format is consistent across tools
 *
 * Related to github/gh-aw#7950
 */

describe("Safe Outputs MCP Error Message Validation", () => {
  let createServer, registerTool, handleMessage, validateRequiredFields;
  let tools;

  beforeEach(async () => {
    vi.resetModules();

    // Suppress stderr output during tests
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);

    // Import modules
    const mcpCore = await import("./mcp_server_core.cjs");
    createServer = mcpCore.createServer;
    registerTool = mcpCore.registerTool;
    handleMessage = mcpCore.handleMessage;

    const validation = await import("./safe_inputs_validation.cjs");
    validateRequiredFields = validation.validateRequiredFields;

    // Load tools schema
    const toolsPath = path.join(process.cwd(), "safe_outputs_tools.json");
    const toolsContent = fs.readFileSync(toolsPath, "utf8");
    tools = JSON.parse(toolsContent);
  });

  describe("validateRequiredFields Function", () => {
    it("should return empty array when all required fields are present", () => {
      const schema = {
        type: "object",
        required: ["title", "body"],
        properties: {
          title: { type: "string" },
          body: { type: "string" },
        },
      };

      const args = {
        title: "Test Title",
        body: "Test Body",
      };

      const missing = validateRequiredFields(args, schema);
      expect(missing).toEqual([]);
    });

    it("should detect missing required fields", () => {
      const schema = {
        type: "object",
        required: ["title", "body"],
        properties: {
          title: { type: "string" },
          body: { type: "string" },
        },
      };

      const args = {
        title: "Test Title",
        // body is missing
      };

      const missing = validateRequiredFields(args, schema);
      expect(missing).toEqual(["body"]);
    });

    it("should detect multiple missing required fields", () => {
      const schema = {
        type: "object",
        required: ["title", "body", "labels"],
        properties: {
          title: { type: "string" },
          body: { type: "string" },
          labels: { type: "array" },
        },
      };

      const args = {}; // All fields missing

      const missing = validateRequiredFields(args, schema);
      expect(missing).toEqual(["title", "body", "labels"]);
    });

    it("should treat empty strings as missing", () => {
      const schema = {
        type: "object",
        required: ["title"],
        properties: {
          title: { type: "string" },
        },
      };

      const args = {
        title: "   ", // Whitespace only
      };

      const missing = validateRequiredFields(args, schema);
      expect(missing).toEqual(["title"]);
    });

    it("should treat null as missing", () => {
      const schema = {
        type: "object",
        required: ["title"],
        properties: {
          title: { type: "string" },
        },
      };

      const args = {
        title: null,
      };

      const missing = validateRequiredFields(args, schema);
      expect(missing).toEqual(["title"]);
    });

    it("should treat undefined as missing", () => {
      const schema = {
        type: "object",
        required: ["title"],
        properties: {
          title: { type: "string" },
        },
      };

      const args = {
        title: undefined,
      };

      const missing = validateRequiredFields(args, schema);
      expect(missing).toEqual(["title"]);
    });
  });

  describe("Error Message Format in MCP Server", () => {
    let server;
    let results;

    beforeEach(() => {
      results = [];
      server = createServer({ name: "test-server", version: "1.0.0" });

      // Override message functions to capture results
      server.writeMessage = msg => {
        results.push(msg);
      };
      server.replyResult = (id, result) => {
        if (id === undefined || id === null) return;
        results.push({ jsonrpc: "2.0", id, result });
      };
      server.replyError = (id, code, message) => {
        if (id === undefined || id === null) return;
        results.push({ jsonrpc: "2.0", id, error: { code, message } });
      };
    });

    it("should include parameter name in error message for single missing field", async () => {
      registerTool(server, {
        name: "test_tool",
        description: "Test tool",
        inputSchema: {
          type: "object",
          required: ["title"],
          properties: {
            title: { type: "string", description: "Test title" },
          },
        },
        handler: args => ({ content: [{ type: "text", text: "ok" }] }),
      });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "test_tool",
          arguments: {}, // missing 'title'
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error).toBeDefined();
      expect(results[0].error.code).toBe(-32602);
      expect(results[0].error.message).toContain("'title'");
      expect(results[0].error.message).toContain("missing or empty");
    });

    it("should include all parameter names in error message for multiple missing fields", async () => {
      registerTool(server, {
        name: "test_tool",
        description: "Test tool",
        inputSchema: {
          type: "object",
          required: ["title", "body"],
          properties: {
            title: { type: "string", description: "Test title" },
            body: { type: "string", description: "Test body" },
          },
        },
        handler: args => ({ content: [{ type: "text", text: "ok" }] }),
      });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "test_tool",
          arguments: {}, // missing both fields
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error).toBeDefined();
      expect(results[0].error.code).toBe(-32602);
      expect(results[0].error.message).toContain("'title'");
      expect(results[0].error.message).toContain("'body'");
      expect(results[0].error.message).toContain("missing or empty");
    });

    it("should use consistent error format across different tools", async () => {
      // Register multiple tools
      const testTools = [
        {
          name: "tool_one",
          required: ["field_a"],
        },
        {
          name: "tool_two",
          required: ["field_b", "field_c"],
        },
      ];

      const errorMessages = [];

      for (const testTool of testTools) {
        results = []; // Reset results
        server = createServer({ name: "test-server", version: "1.0.0" });
        server.writeMessage = msg => {
          results.push(msg);
        };
        server.replyResult = (id, result) => {
          if (id === undefined || id === null) return;
          results.push({ jsonrpc: "2.0", id, result });
        };
        server.replyError = (id, code, message) => {
          if (id === undefined || id === null) return;
          results.push({ jsonrpc: "2.0", id, error: { code, message } });
        };

        registerTool(server, {
          name: testTool.name,
          description: "Test tool",
          inputSchema: {
            type: "object",
            required: testTool.required,
            properties: testTool.required.reduce((acc, field) => {
              acc[field] = { type: "string", description: `Test ${field}` };
              return acc;
            }, {}),
          },
          handler: args => ({ content: [{ type: "text", text: "ok" }] }),
        });

        await handleMessage(server, {
          jsonrpc: "2.0",
          id: 1,
          method: "tools/call",
          params: {
            name: testTool.name,
            arguments: {},
          },
        });

        if (results[0] && results[0].error) {
          errorMessages.push(results[0].error.message);
        }
      }

      // All error messages should follow the same format
      expect(errorMessages.length).toBe(testTools.length);

      errorMessages.forEach(message => {
        expect(message).toMatch(/Invalid arguments: missing or empty/);
        expect(message).toMatch(/'[^']+'/); // Should contain quoted field names
      });
    });
  });

  describe("Error Message Quality for Real Tools", () => {
    it("should have informative error messages when required fields are missing", async () => {
      const server = createServer({ name: "test-server", version: "1.0.0" });
      const results = [];

      server.writeMessage = msg => {
        results.push(msg);
      };
      server.replyResult = (id, result) => {
        if (id === undefined || id === null) return;
        results.push({ jsonrpc: "2.0", id, result });
      };
      server.replyError = (id, code, message) => {
        if (id === undefined || id === null) return;
        results.push({ jsonrpc: "2.0", id, error: { code, message } });
      };

      // Test a few key tools from the real schema
      const toolsToTest = tools.filter(tool => tool.inputSchema.required && tool.inputSchema.required.length > 0).slice(0, 5);

      for (const tool of toolsToTest) {
        results.length = 0; // Clear results

        // Add a handler to the tool so it doesn't fail with "No handler" error
        const toolWithHandler = {
          ...tool,
          handler: args => ({ content: [{ type: "text", text: "ok" }] }),
        };

        registerTool(server, toolWithHandler);

        await handleMessage(server, {
          jsonrpc: "2.0",
          id: 1,
          method: "tools/call",
          params: {
            name: tool.name,
            arguments: {}, // Empty arguments to trigger missing field errors
          },
        });

        expect(results).toHaveLength(1);
        expect(results[0].error).toBeDefined();
        expect(results[0].error.message).toMatch(/missing or empty/);

        // Error should mention the missing required fields
        tool.inputSchema.required.forEach(field => {
          expect(results[0].error.message).toContain(`'${field}'`);
        });
      }
    });
  });

  describe("Error Message Enhancement Opportunities", () => {
    /**
     * These tests document potential enhancements to error messages.
     * They are designed to guide future improvements to make error messages
     * more helpful by including examples from the schema descriptions.
     */

    it("should document current error message format", async () => {
      const server = createServer({ name: "test-server", version: "1.0.0" });
      const results = [];

      server.writeMessage = msg => {
        results.push(msg);
      };
      server.replyResult = (id, result) => {
        if (id === undefined || id === null) return;
        results.push({ jsonrpc: "2.0", id, result });
      };
      server.replyError = (id, code, message) => {
        if (id === undefined || id === null) return;
        results.push({ jsonrpc: "2.0", id, error: { code, message } });
      };

      registerTool(server, {
        name: "create_issue",
        description: "Create a GitHub issue",
        inputSchema: {
          type: "object",
          required: ["title", "body"],
          properties: {
            title: {
              type: "string",
              description: "Issue title (e.g., 'Fix bug in login')",
            },
            body: {
              type: "string",
              description: "Issue description with details",
            },
          },
        },
        handler: args => ({ content: [{ type: "text", text: "ok" }] }),
      });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "create_issue",
          arguments: {}, // Missing required fields
        },
      });

      expect(results).toHaveLength(1);
      const errorMessage = results[0].error.message;

      // Document current format
      expect(errorMessage).toContain("Invalid arguments");
      expect(errorMessage).toContain("missing or empty");
      expect(errorMessage).toContain("'title'");
      expect(errorMessage).toContain("'body'");

      // Current format: "Invalid arguments: missing or empty 'title', 'body'"
      // This is good but could be enhanced to include examples from descriptions
    });

    it("should verify schema descriptions are available for enhancement", () => {
      // Verify that tools with required fields have descriptions that could
      // be used to enhance error messages
      const toolsWithRequiredFields = tools.filter(tool => tool.inputSchema.required && tool.inputSchema.required.length > 0);

      expect(toolsWithRequiredFields.length).toBeGreaterThan(0);

      toolsWithRequiredFields.forEach(tool => {
        tool.inputSchema.required.forEach(field => {
          const property = tool.inputSchema.properties[field];

          // Verify description exists and could be used for better error messages
          expect(property).toBeDefined();
          expect(property.description).toBeDefined();
          expect(property.description.length).toBeGreaterThan(0);

          // This demonstrates that the schema has the information needed
          // to provide enhanced error messages with examples
        });
      });
    });
  });
});
