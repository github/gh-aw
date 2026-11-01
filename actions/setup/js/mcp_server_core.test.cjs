import { describe, it, expect, beforeEach, vi } from "vitest";
import { Readable, Writable } from "stream";

// Mock the stdin/stdout for server testing
let mockStdinData = [];
let mockStdoutData = [];

describe("mcp_server_core.cjs", () => {
  beforeEach(() => {
    vi.resetModules();
    mockStdinData = [];
    mockStdoutData = [];
    delete process.env.GH_AW_MCP_LOG_DIR;
  });

  describe("createServer", () => {
    it("should create a server with the given info", async () => {
      const { createServer } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "test-server", version: "1.0.0" });

      expect(server.serverInfo).toEqual({ name: "test-server", version: "1.0.0" });
      expect(server.tools).toEqual({});
      expect(typeof server.debug).toBe("function");
      expect(typeof server.writeMessage).toBe("function");
      expect(typeof server.replyResult).toBe("function");
      expect(typeof server.replyError).toBe("function");
    });

    it("should accept log directory option", async () => {
      const { createServer } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "test-server", version: "1.0.0" }, { logDir: "/tmp/test-logs" });

      expect(server.logDir).toBe("/tmp/test-logs");
      expect(server.logFilePath).toBe("/tmp/test-logs/server.log");
    });
  });

  describe("registerTool", () => {
    it("should preserve generic tools on servers without metadata", async () => {
      const { registerTool, handleRequest } = await import("./mcp_server_core.cjs");
      const server = { tools: {}, debug() {} };

      registerTool(server, {
        name: "Missing-Tool",
        description: "A generic tool",
        inputSchema: { type: "object", properties: {} },
        handler: () => ({ content: [{ type: "text", text: "ok" }] }),
      });

      const listed = await handleRequest(server, { jsonrpc: "2.0", id: 1, method: "tools/list" });
      expect(listed.result.tools.map(tool => tool.name)).toEqual(["missing_tool"]);

      const called = await handleRequest(server, {
        jsonrpc: "2.0",
        id: 2,
        method: "tools/call",
        params: { name: "Missing-Tool", arguments: {} },
      });
      expect(called.result.content).toEqual([{ type: "text", text: "ok" }]);
    });

    it("should register a tool with the server", async () => {
      const { createServer, registerTool } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "test-server", version: "1.0.0" });

      registerTool(server, {
        name: "test_tool",
        description: "A test tool",
        inputSchema: { type: "object", properties: {} },
        handler: () => ({ content: [{ type: "text", text: "ok" }] }),
      });

      expect(server.tools["test_tool"]).toBeDefined();
      expect(server.tools["test_tool"].name).toBe("test_tool");
      expect(server.tools["test_tool"].description).toBe("A test tool");
    });

    it("should normalize tool names with dashes to underscores", async () => {
      const { createServer, registerTool } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "test-server", version: "1.0.0" });

      registerTool(server, {
        name: "test-tool",
        description: "A test tool",
        inputSchema: { type: "object", properties: {} },
      });

      expect(server.tools["test_tool"]).toBeDefined();
      expect(server.tools["test_tool"].name).toBe("test_tool");
    });

    it("should normalize tool names to lowercase", async () => {
      const { createServer, registerTool } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "test-server", version: "1.0.0" });

      registerTool(server, {
        name: "Test-Tool",
        description: "A test tool",
        inputSchema: { type: "object", properties: {} },
      });

      expect(server.tools["test_tool"]).toBeDefined();
    });
  });

  describe("normalizeTool", () => {
    it("should normalize tool names", async () => {
      const { normalizeTool } = await import("./mcp_server_core.cjs");

      expect(normalizeTool("test-tool")).toBe("test_tool");
      expect(normalizeTool("Test-Tool")).toBe("test_tool");
      expect(normalizeTool("create_issue")).toBe("create_issue");
      expect(normalizeTool("CREATE-ISSUE")).toBe("create_issue");
    });

    it("should handle empty string input", async () => {
      const { normalizeTool } = await import("./mcp_server_core.cjs");

      expect(normalizeTool("")).toBe("");
    });
  });

  describe("handleMessage", () => {
    let server;
    let results = [];

    beforeEach(async () => {
      vi.resetModules();
      results = [];

      // Suppress stderr output during tests
      vi.spyOn(process.stderr, "write").mockImplementation(() => true);

      const { createServer, registerTool } = await import("./mcp_server_core.cjs");
      server = createServer({ name: "test-server", version: "1.0.0" });

      // Override writeMessage to capture results
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
        name: "test_tool",
        description: "A test tool",
        inputSchema: {
          type: "object",
          properties: { input: { type: "string", description: "Input text to process" } },
          required: ["input"],
          additionalProperties: false,
        },
        handler: args => ({
          content: [{ type: "text", text: `received: ${args.input}` }],
        }),
      });
    });

    it("should handle initialize method", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: { protocolVersion: "2024-11-05" },
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.serverInfo).toEqual({ name: "test-server", version: "1.0.0" });
      expect(results[0].result.protocolVersion).toBe("2024-11-05");
      expect(results[0].result.capabilities).toEqual({ tools: {} });
    });

    it("should handle tools/list method", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/list",
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.tools).toHaveLength(1);
      expect(results[0].result.tools[0].name).toBe("test_tool");
    });

    it("should handle tools/call method with handler", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "test_tool",
          arguments: { input: "hello" },
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.content[0].text).toBe("received: hello");
      expect(results[0].result.isError).toBe(false);
    });

    it("should propagate isError:true from handler result to MCP response", async () => {
      // Reuse the server, capture hooks, and stderr spy already set up in beforeEach.
      const { registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      registerTool(server, {
        name: "failing_tool",
        description: "A tool that returns an application-level error",
        inputSchema: {
          type: "object",
          properties: { input: { type: "string", description: "Input" } },
          required: ["input"],
        },
        handler: () => ({
          content: [{ type: "text", text: JSON.stringify({ result: "error", error: "something went wrong" }) }],
          isError: true,
        }),
      });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 42,
        method: "tools/call",
        params: { name: "failing_tool", arguments: { input: "test" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.isError).toBe(true);
      expect(results[0].result.content[0].text).toContain('"result":"error"');
    });

    it("should normalize tool arguments before required-field validation", async () => {
      const { createServer, registerTool, handleMessage } = await import("./mcp_server_core.cjs");
      results = [];
      server = createServer(
        { name: "test-server", version: "1.0.0" },
        {
          normalizeArguments: (toolName, args) => (args && args[toolName] ? args[toolName] : args),
        }
      );

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
        name: "test_tool",
        description: "A test tool",
        inputSchema: {
          type: "object",
          properties: { input: { type: "string", description: "Input text to process" } },
          required: ["input"],
        },
        handler: args => ({
          content: [{ type: "text", text: `received: ${args.input}` }],
        }),
      });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "test_tool",
          arguments: { test_tool: { input: "hello" } },
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.content[0].text).toBe("received: hello");
    });

    it("should return error for unknown tool", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "unknown_tool",
          arguments: {},
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32601);
      expect(results[0].error.message).toContain("Tool not found");
    });

    it("should reject @filepath local file references in tool arguments", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "test_tool",
          arguments: { input: "@/tmp/gh-aw/agent/issue_body.md" },
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32602);
      expect(results[0].error.message).toContain("@filepath");
      expect(results[0].error.message).toContain("not supported");
      expect(results[0].error.message).toContain("Do not attempt to inline files");
    });

    it("should reject relative @filepath local file references in tool arguments", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "test_tool",
          arguments: { input: "@./notes.md" },
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32602);

      results.length = 0;

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 2,
        method: "tools/call",
        params: {
          name: "test_tool",
          arguments: { input: "@../notes.md" },
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32602);
    });

    it("should return error for empty arguments object (probe detection)", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "test_tool",
          arguments: {}, // completely empty — probe attempt
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32602);
      expect(results[0].error.message).toContain("write-once, not a discovery probe");
      expect(results[0].error.message).toContain("tools/list");
      expect(results[0].error.message).toContain("noop");
      // Schema guidance should be included so the model can retry without calling tools/list
      expect(results[0].error.message).toContain("Example:");
      expect(results[0].error.message).toContain("Required parameter");
    });

    it("should return enhanced error for partially-supplied but invalid required fields", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "test_tool",
          arguments: { input: "" }, // present but empty string — not a probe
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32602);
      expect(results[0].error.message).toContain("missing or empty");
      // Verify enhanced error message includes guidance
      expect(results[0].error.message).toContain("Required parameter");
      expect(results[0].error.message).toContain("Example:");
    });

    it("should reject unknown parameters with closest valid suggestions", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "test_tool",
          arguments: { inpt: "hello" },
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32602);
      expect(results[0].error.message).toContain("unknown parameter");
      expect(results[0].error.message).toContain("'inpt'");
      expect(results[0].error.message).toContain("'input'");
      expect(results[0].error.message).toContain("Supported parameters for this tool");
    });

    it("rejects strict add_labels string arrays synchronously before handler execution", async () => {
      const { registerTool, handleMessage } = await import("./mcp_server_core.cjs");
      const handler = vi.fn(() => ({ content: [{ type: "text", text: "ok" }] }));
      registerTool(server, {
        name: "add_labels",
        description: "Add labels",
        inputSchema: {
          type: "object",
          properties: {
            item_number: { type: "integer" },
            labels: {
              type: "array",
              items: {
                type: "object",
                properties: {
                  name: { type: "string" },
                  rationale: { type: "string" },
                  confidence: { type: "string", enum: ["LOW", "MEDIUM", "HIGH"] },
                },
                required: ["name", "rationale", "confidence"],
              },
            },
          },
          required: ["item_number", "labels"],
        },
        handler,
      });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 2,
        method: "tools/call",
        params: {
          name: "add_labels",
          arguments: { item_number: 123, labels: ["bug"] },
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32602);
      expect(results[0].error.message).toContain("Invalid arguments for add_labels:");
      expect(results[0].error.message).toContain("labels[0] must be an object (string shorthand is not supported).");
      expect(handler).not.toHaveBeenCalled();
    });

    it("should return error for unknown method", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "unknown/method",
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32601);
      expect(results[0].error.message).toContain("Method not found");
    });

    it("should ignore notifications (no response)", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        method: "notifications/initialized",
        // no id - this is a notification
      });

      expect(results).toHaveLength(0);
    });

    it("should validate JSON-RPC version", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "1.0", // wrong version
        id: 1,
        method: "test",
      });

      // Should not produce a response (invalid message silently ignored)
      expect(results).toHaveLength(0);
    });

    it("should use default handler when tool has no handler", async () => {
      const { handleMessage, registerTool } = await import("./mcp_server_core.cjs");

      // Register tool without handler
      registerTool(server, {
        name: "no_handler_tool",
        description: "A tool without handler",
        inputSchema: { type: "object", properties: {} },
      });

      const defaultHandler = type => args => ({
        content: [{ type: "text", text: `default handler for ${type}` }],
      });

      await handleMessage(
        server,
        {
          jsonrpc: "2.0",
          id: 1,
          method: "tools/call",
          params: {
            name: "no_handler_tool",
            arguments: {},
          },
        },
        defaultHandler
      );

      expect(results).toHaveLength(1);
      expect(results[0].result.content[0].text).toBe("default handler for no_handler_tool");
    });

    it("should return the code and message from a thrown plain object (not Error instance)", async () => {
      const { registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      registerTool(server, {
        name: "plain_throw_tool",
        description: "A tool that throws a plain object",
        inputSchema: { type: "object", properties: { input: { type: "string" } }, required: ["input"] },
        handler: () => {
          throw { code: -32602, message: "ERR_VALIDATION: body required" };
        },
      });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 99,
        method: "tools/call",
        params: { name: "plain_throw_tool", arguments: { input: "x" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32602);
      expect(results[0].error.message).toBe("ERR_VALIDATION: body required");
      expect(results[0].error.message).not.toContain("[object Object]");
    });

    it("should fall back to -32603 when thrown plain object has no valid error code", async () => {
      const { registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      registerTool(server, {
        name: "no_code_throw_tool",
        description: "A tool that throws a plain object without a code",
        inputSchema: { type: "object", properties: { input: { type: "string" } }, required: ["input"] },
        handler: () => {
          throw { message: "something went wrong" };
        },
      });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 100,
        method: "tools/call",
        params: { name: "no_code_throw_tool", arguments: { input: "x" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32603);
      expect(results[0].error.message).toBe("something went wrong");
    });

    it("should fall back to -32603 when thrown plain object has a positive code", async () => {
      const { registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      registerTool(server, {
        name: "positive_code_throw_tool",
        description: "A tool that throws a plain object with a positive code",
        inputSchema: { type: "object", properties: { input: { type: "string" } }, required: ["input"] },
        handler: () => {
          throw { code: 1, message: "process exited with code 1" };
        },
      });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 101,
        method: "tools/call",
        params: { name: "positive_code_throw_tool", arguments: { input: "x" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32603);
      expect(results[0].error.message).toBe("process exited with code 1");
    });

    it("should use Internal error when thrown plain object has no message property", async () => {
      const { registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      registerTool(server, {
        name: "no_message_throw_tool",
        description: "A tool that throws a plain object without a message",
        inputSchema: { type: "object", properties: { input: { type: "string" } }, required: ["input"] },
        handler: () => {
          throw { code: -32602 };
        },
      });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 102,
        method: "tools/call",
        params: { name: "no_message_throw_tool", arguments: { input: "x" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32602);
      expect(results[0].error.message).toBe("Internal error");
    });

    it("should fall back to -32603 when thrown plain object has a non-integer negative code", async () => {
      const { registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      registerTool(server, {
        name: "non_integer_code_throw_tool",
        description: "A tool that throws a plain object with a non-integer code",
        inputSchema: { type: "object", properties: { input: { type: "string" } }, required: ["input"] },
        handler: () => {
          throw { code: -1.5, message: "fractional code should be ignored" };
        },
      });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 103,
        method: "tools/call",
        params: { name: "non_integer_code_throw_tool", arguments: { input: "x" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32603);
      expect(results[0].error.message).toBe("fractional code should be ignored");
    });
  });

  describe("handleRequest", () => {
    beforeEach(() => {
      // Suppress stderr output during tests
      vi.spyOn(process.stderr, "write").mockImplementation(() => true);
    });

    it("should propagate isError:true from handler result into the JSON-RPC response", async () => {
      const { createServer, registerTool, handleRequest } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "test-server", version: "1.0.0" });

      registerTool(server, {
        name: "failing_tool",
        description: "A tool that returns an application-level error",
        inputSchema: {
          type: "object",
          properties: { input: { type: "string", description: "Input" } },
          required: ["input"],
        },
        handler: () => ({
          content: [{ type: "text", text: JSON.stringify({ result: "error", error: "something went wrong" }) }],
          isError: true,
        }),
      });

      const response = await handleRequest(server, {
        jsonrpc: "2.0",
        id: 7,
        method: "tools/call",
        params: { name: "failing_tool", arguments: { input: "test" } },
      });

      expect(response.id).toBe(7);
      expect(response.result.isError).toBe(true);
      expect(response.result.content[0].text).toContain('"result":"error"');
    });

    it("should report isError:false for a successful handler result", async () => {
      const { createServer, registerTool, handleRequest } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "test-server", version: "1.0.0" });

      registerTool(server, {
        name: "ok_tool",
        description: "A tool that succeeds",
        inputSchema: {
          type: "object",
          properties: { input: { type: "string", description: "Input" } },
          required: ["input"],
        },
        handler: args => ({
          content: [{ type: "text", text: `received: ${args.input}` }],
        }),
      });

      const response = await handleRequest(server, {
        jsonrpc: "2.0",
        id: 8,
        method: "tools/call",
        params: { name: "ok_tool", arguments: { input: "hello" } },
      });

      expect(response.id).toBe(8);
      expect(response.result.isError).toBe(false);
      expect(response.result.content[0].text).toBe("received: hello");
    });

    it("should reject create_issue with a short body via handleRequest", async () => {
      const { createServer, registerTool, handleRequest } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "safe-outputs", version: "1.0.0" });

      registerTool(server, {
        name: "create_issue",
        description: "Create an issue",
        inputSchema: {
          type: "object",
          properties: {
            title: { type: "string", description: "Issue title" },
            body: { type: "string", minLength: 20, description: "Issue body" },
          },
          required: ["title", "body"],
        },
        handler: args => ({
          content: [{ type: "text", text: JSON.stringify({ result: "success" }) }],
        }),
      });

      const response = await handleRequest(server, {
        jsonrpc: "2.0",
        id: 9,
        method: "tools/call",
        params: { name: "create_issue", arguments: { title: "My Issue", body: "." } },
      });

      expect(response.error).toBeDefined();
      expect(response.error.code).toBe(-32602);
      expect(response.error.message).toContain("body");
      expect(response.error.message).toContain("too short");
      expect(response.error.message).toContain("20");
    });

    it("should return error for empty arguments object (probe detection) via handleRequest", async () => {
      const { createServer, registerTool, handleRequest } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "test-server", version: "1.0.0" });

      registerTool(server, {
        name: "probe_tool",
        description: "A tool to test probe detection",
        inputSchema: {
          type: "object",
          properties: { input: { type: "string", description: "Some input" } },
          required: ["input"],
        },
        handler: args => ({
          content: [{ type: "text", text: `received: ${args.input}` }],
        }),
      });

      const response = await handleRequest(server, {
        jsonrpc: "2.0",
        id: 10,
        method: "tools/call",
        params: {
          name: "probe_tool",
          arguments: {}, // completely empty — probe attempt
        },
      });

      expect(response.error).toBeDefined();
      expect(response.error.code).toBe(-32602);
      expect(response.error.message).toContain("write-once, not a discovery probe");
      expect(response.error.message).toContain("tools/list");
      expect(response.error.message).toContain("noop");
      // Schema guidance should be included so the model can retry without calling tools/list
      expect(response.error.message).toContain("Example:");
      expect(response.error.message).toContain("Required parameter");
    });
  });

  describe("loadToolHandlers", () => {
    let server;
    const fs = require("fs");
    const path = require("path");
    const os = require("os");
    let tempDir;

    beforeEach(async () => {
      vi.resetModules();

      // Suppress stderr output during tests
      vi.spyOn(process.stderr, "write").mockImplementation(() => true);

      const { createServer } = await import("./mcp_server_core.cjs");
      server = createServer({ name: "test-server", version: "1.0.0" });

      // Create a temporary directory for test handler files
      tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-test-handlers-"));
    });

    afterEach(() => {
      // Clean up temporary directory
      if (tempDir && fs.existsSync(tempDir)) {
        fs.rmSync(tempDir, { recursive: true });
      }
    });

    it("should load a sync handler from file path", async () => {
      const { loadToolHandlers, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      // Create a test handler file that reads from stdin and writes to stdout
      const handlerPath = path.join(tempDir, "sync_handler.cjs");
      fs.writeFileSync(
        handlerPath,
        `let input = '';
process.stdin.on('data', chunk => { input += chunk; });
process.stdin.on('end', () => {
  const args = JSON.parse(input);
  const result = { result: "sync result: " + args.input };
  console.log(JSON.stringify(result));
});`
      );

      // Create tool with handler path
      const tools = [
        {
          name: "test_sync_tool",
          description: "A tool with sync handler",
          inputSchema: { type: "object", properties: { input: { type: "string" } } },
          handler: handlerPath,
        },
      ];

      // Load handlers
      loadToolHandlers(server, tools, tempDir);

      // Verify handler was loaded
      expect(typeof tools[0].handler).toBe("function");

      // Register and call tool
      registerTool(server, tools[0]);

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "test_sync_tool", arguments: { input: "hello" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.content[0].text).toContain("sync result: hello");
    });

    it("should load an async handler from file path", async () => {
      const { loadToolHandlers, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      // Create a test async handler file that reads from stdin and writes to stdout
      const handlerPath = path.join(tempDir, "async_handler.cjs");
      fs.writeFileSync(
        handlerPath,
        `let input = '';
process.stdin.on('data', chunk => { input += chunk; });
process.stdin.on('end', async () => {
  const args = JSON.parse(input);
  await new Promise(resolve => setTimeout(resolve, 10));
  const result = { result: "async result: " + args.input };
  console.log(JSON.stringify(result));
});`
      );

      // Create tool with handler path
      const tools = [
        {
          name: "test_async_tool",
          description: "A tool with async handler",
          inputSchema: { type: "object", properties: { input: { type: "string" } } },
          handler: handlerPath,
        },
      ];

      // Load handlers
      loadToolHandlers(server, tools, tempDir);

      // Verify handler was loaded
      expect(typeof tools[0].handler).toBe("function");

      // Register and call tool
      registerTool(server, tools[0]);

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "test_async_tool", arguments: { input: "world" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.content[0].text).toContain("async result: world");
    });

    it("should handle handler that returns MCP format directly", async () => {
      const { loadToolHandlers, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      // Create a handler that reads from stdin and returns string to stdout
      const handlerPath = path.join(tempDir, "mcp_format_handler.cjs");
      fs.writeFileSync(
        handlerPath,
        `let input = '';
process.stdin.on('data', chunk => { input += chunk; });
process.stdin.on('end', () => {
  const args = JSON.parse(input);
  console.log("MCP format: " + args.input);
});`
      );

      const tools = [
        {
          name: "test_mcp_format",
          description: "A tool returning MCP format",
          inputSchema: { type: "object", properties: { input: { type: "string" } } },
          handler: handlerPath,
        },
      ];

      loadToolHandlers(server, tools, tempDir);
      registerTool(server, tools[0]);

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "test_mcp_format", arguments: { input: "test" } },
      });

      expect(results).toHaveLength(1);
      // Non-JSON output is wrapped in stdout/stderr format
      const parsed = JSON.parse(results[0].result.content[0].text);
      expect(parsed.stdout).toContain("MCP format: test");
    });

    it("should handle handler with module.default export pattern", async () => {
      const { loadToolHandlers, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      // Create a handler that reads from stdin (module.default doesn't apply to separate processes)
      const handlerPath = path.join(tempDir, "default_export_handler.cjs");
      fs.writeFileSync(
        handlerPath,
        `let input = '';
process.stdin.on('data', chunk => { input += chunk; });
process.stdin.on('end', () => {
  const args = JSON.parse(input);
  const result = { result: "default export: " + args.input };
  console.log(JSON.stringify(result));
});`
      );

      const tools = [
        {
          name: "test_default_export",
          description: "A tool with default export",
          inputSchema: { type: "object", properties: { input: { type: "string" } } },
          handler: handlerPath,
        },
      ];

      loadToolHandlers(server, tools, tempDir);
      registerTool(server, tools[0]);

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "test_default_export", arguments: { input: "hi" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.content[0].text).toContain("default export: hi");
    });

    it("should skip tools without handler path", async () => {
      const { loadToolHandlers } = await import("./mcp_server_core.cjs");

      const tools = [
        {
          name: "tool_without_handler",
          description: "A tool without handler path",
          inputSchema: { type: "object", properties: {} },
        },
      ];

      const result = loadToolHandlers(server, tools, tempDir);

      // Handler should remain undefined
      expect(tools[0].handler).toBeUndefined();
      expect(result).toBe(tools); // Should return the same array
    });

    it("should handle non-existent handler file", async () => {
      const { loadToolHandlers } = await import("./mcp_server_core.cjs");

      const tools = [
        {
          name: "tool_with_missing_handler",
          description: "A tool with missing handler file",
          inputSchema: { type: "object", properties: {} },
          handler: "/non/existent/handler.cjs",
        },
      ];

      // Should not throw, but handler should remain the string path (or be updated to undefined)
      loadToolHandlers(server, tools, tempDir);

      // Handler should still be the original string (load failed)
      expect(tools[0].handler).toBe("/non/existent/handler.cjs");
    });

    it("should handle handler that is not a function", async () => {
      const { loadToolHandlers } = await import("./mcp_server_core.cjs");

      // Create a handler file - in separate process mode, it will create a handler function
      const handlerPath = path.join(tempDir, "not_a_function.cjs");
      fs.writeFileSync(handlerPath, `console.log(JSON.stringify({ result: "ok" }));`);

      const tools = [
        {
          name: "tool_with_invalid_handler",
          description: "A tool with invalid handler",
          inputSchema: { type: "object", properties: {} },
          handler: handlerPath,
        },
      ];

      loadToolHandlers(server, tools, tempDir);

      // Handler should be a function now (JavaScript handler always creates a function)
      expect(typeof tools[0].handler).toBe("function");
    });

    it("should handle handler that throws error", async () => {
      const { loadToolHandlers, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      // Create a handler that exits with error code (separate process)
      const handlerPath = path.join(tempDir, "error_handler.cjs");
      fs.writeFileSync(handlerPath, `process.exit(1);`);

      const tools = [
        {
          name: "test_error_handler",
          description: "A tool that throws",
          inputSchema: { type: "object", properties: { input: { type: "string" } } },
          handler: handlerPath,
        },
      ];

      loadToolHandlers(server, tools, tempDir);
      registerTool(server, tools[0]);

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "test_error_handler", arguments: { input: "oops" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32603);
    });

    it("should resolve relative paths from basePath", async () => {
      const { loadToolHandlers, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      // Create a handler in a subdirectory
      const subDir = path.join(tempDir, "handlers");
      fs.mkdirSync(subDir, { recursive: true });
      const handlerPath = path.join(subDir, "relative_handler.cjs");
      fs.writeFileSync(
        handlerPath,
        `let input = '';
process.stdin.on('data', chunk => { input += chunk; });
process.stdin.on('end', () => {
  const args = JSON.parse(input);
  const result = { result: "relative: " + args.input };
  console.log(JSON.stringify(result));
});`
      );

      // Use relative path in tool definition
      const tools = [
        {
          name: "test_relative_path",
          description: "A tool with relative handler path",
          inputSchema: { type: "object", properties: { input: { type: "string" } } },
          handler: "handlers/relative_handler.cjs",
        },
      ];

      loadToolHandlers(server, tools, tempDir);

      expect(typeof tools[0].handler).toBe("function");

      registerTool(server, tools[0]);

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "test_relative_path", arguments: { input: "path" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.content[0].text).toContain("relative: path");
    });

    it("should prevent directory traversal attacks with relative paths", async () => {
      const { loadToolHandlers } = await import("./mcp_server_core.cjs");

      // Create a handler outside of tempDir
      const outsideDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-outside-"));
      const outsideHandlerPath = path.join(outsideDir, "outside_handler.cjs");
      fs.writeFileSync(outsideHandlerPath, `module.exports = function() { return "should not be loaded"; };`);

      // Use relative path that tries to escape basePath
      const tools = [
        {
          name: "traversal_attack_tool",
          description: "A tool trying to escape basePath",
          inputSchema: { type: "object", properties: {} },
          handler: "../../../" + path.basename(outsideDir) + "/outside_handler.cjs",
        },
      ];

      loadToolHandlers(server, tools, tempDir);

      // Handler should remain the original string (validation blocked it)
      expect(tools[0].handler).toBe("../../../" + path.basename(outsideDir) + "/outside_handler.cjs");

      // Clean up
      fs.rmSync(outsideDir, { recursive: true });
    });

    it("should handle handler returning non-serializable value (circular reference)", async () => {
      const { loadToolHandlers, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      // Create a handler - in separate process, we just output string directly
      const handlerPath = path.join(tempDir, "circular_handler.cjs");
      fs.writeFileSync(handlerPath, `console.log("[object Object]");`);

      const tools = [
        {
          name: "test_circular",
          description: "A tool returning circular reference",
          inputSchema: { type: "object", properties: { input: { type: "string" } } },
          handler: handlerPath,
        },
      ];

      loadToolHandlers(server, tools, tempDir);
      registerTool(server, tools[0]);

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "test_circular", arguments: { input: "test" } },
      });

      // Should handle non-JSON output (wrapped in stdout/stderr format)
      expect(results).toHaveLength(1);
      const parsed = JSON.parse(results[0].result.content[0].text);
      expect(parsed.stdout).toContain("[object Object]");
    });

    it("should load and execute shell script handler", async () => {
      const { loadToolHandlers, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      // Create a shell script handler
      const handlerPath = path.join(tempDir, "test_handler.sh");
      fs.writeFileSync(
        handlerPath,
        `#!/bin/bash
echo "Hello from shell script"
echo "Input was: $INPUT_NAME"
echo "result=success" >> "$GITHUB_OUTPUT"
`,
        { mode: 0o755 }
      );

      const tools = [
        {
          name: "test_shell",
          description: "A shell script tool",
          inputSchema: { type: "object", properties: { name: { type: "string" } } },
          handler: handlerPath,
        },
      ];

      loadToolHandlers(server, tools, tempDir);

      expect(typeof tools[0].handler).toBe("function");

      registerTool(server, tools[0]);

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "test_shell", arguments: { name: "world" } },
      });

      expect(results).toHaveLength(1);
      const resultContent = JSON.parse(results[0].result.content[0].text);
      expect(resultContent.stdout).toContain("Hello from shell script");
      expect(resultContent.stdout).toContain("Input was: world");
      expect(resultContent.outputs.result).toBe("success");
    });

    it("should handle shell script with multiple outputs", async () => {
      const { loadToolHandlers, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      // Create a shell script with multiple outputs
      const handlerPath = path.join(tempDir, "multi_output.sh");
      fs.writeFileSync(
        handlerPath,
        `#!/bin/bash
{
  echo "first=value1"
  echo "second=value2"
  echo "third=value with spaces"
} >> "$GITHUB_OUTPUT"`,
        { mode: 0o755 }
      );

      const tools = [
        {
          name: "test_multi_output",
          description: "Shell script with multiple outputs",
          inputSchema: { type: "object", properties: {} },
          handler: handlerPath,
        },
      ];

      loadToolHandlers(server, tools, tempDir);
      registerTool(server, tools[0]);

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "test_multi_output", arguments: {} },
      });

      expect(results).toHaveLength(1);
      const resultContent = JSON.parse(results[0].result.content[0].text);
      expect(resultContent.outputs.first).toBe("value1");
      expect(resultContent.outputs.second).toBe("value2");
      expect(resultContent.outputs.third).toBe("value with spaces");
    });

    it("should handle shell script errors", async () => {
      const { loadToolHandlers, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      // Create a shell script that exits with error
      const handlerPath = path.join(tempDir, "error_handler.sh");
      fs.writeFileSync(
        handlerPath,
        `#!/bin/bash
echo "About to fail" >&2
exit 1
`,
        { mode: 0o755 }
      );

      const tools = [
        {
          name: "test_shell_error",
          description: "Shell script that errors",
          inputSchema: { type: "object", properties: {} },
          handler: handlerPath,
        },
      ];

      loadToolHandlers(server, tools, tempDir);
      registerTool(server, tools[0]);

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "test_shell_error", arguments: {} },
      });

      // Should receive an error response
      expect(results).toHaveLength(1);
      expect(results[0].error).toBeDefined();
    });

    it("should convert input names with dashes to underscores", async () => {
      const { loadToolHandlers, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      // Create a shell script that echoes input env vars
      const handlerPath = path.join(tempDir, "env_handler.sh");
      fs.writeFileSync(
        handlerPath,
        `#!/bin/bash
echo "my-input value: $INPUT_MY_INPUT"
echo "result=$INPUT_MY_INPUT" >> "$GITHUB_OUTPUT"
`,
        { mode: 0o755 }
      );

      const tools = [
        {
          name: "test_env_conversion",
          description: "Tests env var conversion",
          inputSchema: { type: "object", properties: { "my-input": { type: "string" } } },
          handler: handlerPath,
        },
      ];

      loadToolHandlers(server, tools, tempDir);
      registerTool(server, tools[0]);

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "test_env_conversion", arguments: { "my-input": "test-value" } },
      });

      expect(results).toHaveLength(1);
      const resultContent = JSON.parse(results[0].result.content[0].text);
      expect(resultContent.outputs.result).toBe("test-value");
    });
  });

  describe("levenshteinDistance", () => {
    it("should calculate correct distance for identical strings", async () => {
      const { levenshteinDistance } = await import("./mcp_server_core.cjs");
      expect(levenshteinDistance("test", "test")).toBe(0);
    });

    it("should calculate correct distance for single character change", async () => {
      const { levenshteinDistance } = await import("./mcp_server_core.cjs");
      expect(levenshteinDistance("cat", "bat")).toBe(1);
      expect(levenshteinDistance("cat", "cut")).toBe(1);
    });

    it("should calculate correct distance for insertions/deletions", async () => {
      const { levenshteinDistance } = await import("./mcp_server_core.cjs");
      expect(levenshteinDistance("cat", "ca")).toBe(1);
      expect(levenshteinDistance("cat", "cats")).toBe(1);
    });

    it("should calculate correct distance for multiple changes", async () => {
      const { levenshteinDistance } = await import("./mcp_server_core.cjs");
      expect(levenshteinDistance("cat", "dog")).toBe(3);
    });
  });

  describe("start", () => {
    it("serializes pipelined stdin chunks and avoids overlapping tool dispatch", async () => {
      const { createServer, registerTool, start } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "test-server", version: "1.0.0" });

      let activeHandlers = 0;
      let maxActiveHandlers = 0;
      let callCount = 0;
      let releaseFirstCall;
      const firstCallGate = new Promise(resolve => {
        releaseFirstCall = resolve;
      });

      registerTool(server, {
        name: "slow_tool",
        description: "A test tool",
        inputSchema: { type: "object", properties: {}, additionalProperties: false },
        handler: async () => {
          callCount++;
          activeHandlers++;
          maxActiveHandlers = Math.max(maxActiveHandlers, activeHandlers);
          if (callCount === 1) {
            await firstCallGate;
          }
          activeHandlers--;
          return { content: [{ type: "text", text: "ok" }] };
        },
      });

      // Avoid writing to real stdout/stderr during this test.
      server.writeMessage = () => {};
      server.debug = () => {};

      let dataHandler;
      const onSpy = vi.spyOn(process.stdin, "on").mockImplementation((event, handler) => {
        if (event === "data") {
          dataHandler = handler;
        }
        return process.stdin;
      });
      const resumeSpy = vi.spyOn(process.stdin, "resume").mockImplementation(() => process.stdin);

      try {
        start(server);
        expect(typeof dataHandler).toBe("function");

        const message1 = Buffer.from(`${JSON.stringify({ jsonrpc: "2.0", id: 1, method: "tools/call", params: { name: "slow_tool", arguments: {} } })}\n`);
        const message2 = Buffer.from(`${JSON.stringify({ jsonrpc: "2.0", id: 2, method: "tools/call", params: { name: "slow_tool", arguments: {} } })}\n`);

        const first = dataHandler(message1);
        const second = dataHandler(message2);

        // Let queued tasks run before releasing first call.
        await Promise.resolve();
        await Promise.resolve();
        expect(callCount).toBe(1);

        releaseFirstCall();
        await Promise.all([first, second]);

        expect(callCount).toBe(2);
        expect(maxActiveHandlers).toBe(1);
      } finally {
        onSpy.mockRestore();
        resumeSpy.mockRestore();
      }
    });
  });

  describe("findSimilarTools", () => {
    it("should find tools with typos", async () => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      const tools = {
        add_comment: {},
        add_name: {},
        report_missing_tool: {},
      };

      const similar = findSimilarTools("add_comentt", tools, 3);
      expect(similar.length).toBeGreaterThan(0);
      expect(similar[0].name).toBe("add_comment");
      expect(similar[0].distance).toBeLessThanOrEqual(2);
    });

    it("should find tools with dashes normalized", async () => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      const tools = {
        dispatch_workflow: {},
        add_comment: {},
      };

      const similar = findSimilarTools("dispatch-workflow", tools, 3);
      expect(similar.length).toBeGreaterThan(0);
      expect(similar[0].name).toBe("dispatch_workflow");
      expect(similar[0].distance).toBe(0); // Should match exactly after normalization
    });

    it("should return empty array for completely different names", async () => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      const tools = {
        short: {},
      };

      const similar = findSimilarTools("verylongdifferenttoolname", tools, 3);
      expect(similar.length).toBe(0); // Distance too large
    });

    it.each([2, 4])("should return exactly %i results when enough suggestions match", async maxSuggestions => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      const tools = {
        tool_a: {},
        tool_b: {},
        tool_c: {},
        tool_d: {},
        tool_e: {},
      };

      const similar = findSimilarTools("tool_x", tools, maxSuggestions);
      expect(similar).toHaveLength(maxSuggestions);
    });

    it("should sort by distance (closest first)", async () => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      const tools = {
        add_name: {},
        add_comment: {},
        report_missing_tool: {},
      };

      const similar = findSimilarTools("add_nam", tools, 3);
      expect(similar.length).toBeGreaterThan(0);
      expect(similar[0].name).toBe("add_name");
      expect(similar[0].distance).toBe(1);
    });
  });

  describe("suggestion resource bounds", () => {
    it.each([127, 128, 129, 1024 * 1024 - 256])("bounds suggestion work for a %i-code-unit tool identifier", async length => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      const candidate = "a".repeat(127) + "b";
      const requested = "a".repeat(length);
      const minimum = vi.spyOn(Math, "min");
      let similar;
      let comparisons;
      try {
        similar = findSimilarTools(requested, { [candidate]: {} });
        comparisons = minimum.mock.calls.length;
      } finally {
        minimum.mockRestore();
      }
      if (length <= 128) {
        expect(similar).toEqual([{ name: candidate, distance: 1 }]);
        expect(comparisons).toBe(length * 128);
      } else {
        expect(similar).toEqual([]);
        expect(comparisons).toBe(0);
      }
    });

    it("skips oversized candidates and normalization that expands past the limit", async () => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      const minimum = vi.spyOn(Math, "min");
      let results;
      let comparisons;
      try {
        results = [findSimilarTools("a", { ["a".repeat(129)]: {} }), findSimilarTools("\u0130".repeat(65), { a: {} })];
        comparisons = minimum.mock.calls.length;
      } finally {
        minimum.mockRestore();
      }
      expect(results).toEqual([[], []]);
      expect(comparisons).toBe(0);
    });

    it("caps total matrix work across a large tool registry", async () => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      const tools = Object.fromEntries(Array.from({ length: 200 }, (_, i) => ["a".repeat(125) + String(i).padStart(3, "0"), {}]));
      const minimum = vi.spyOn(Math, "min");
      let similar;
      let comparisons;
      try {
        similar = findSimilarTools("a".repeat(128), tools);
        comparisons = minimum.mock.calls.length;
      } finally {
        minimum.mockRestore();
      }
      // Each matrix costs 129 * 129 cells; only three fit in 65,536.
      expect(similar).toHaveLength(3);
      expect(comparisons).toBe(3 * 128 * 128);
      expect(comparisons).toBeLessThanOrEqual(65536);
    });

    it.each([128, 129])("charges each visited length-pruned tool candidate of length %i", async length => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      let visits = 0;
      const candidates = new Proxy(Array(70000).fill("z".repeat(length)), {
        get(target, property, receiver) {
          if (typeof property === "string" && /^\d+$/.test(property)) {
            visits++;
          }
          return Reflect.get(target, property, receiver);
        },
      });
      const tools = {};
      const keys = Object.keys;
      const registryKeys = vi.spyOn(Object, "keys").mockImplementation(object => (object === tools ? candidates : keys(object)));
      const minimum = vi.spyOn(Math, "min");
      let comparisons;
      let similar;
      try {
        similar = findSimilarTools("x", tools);
        comparisons = minimum.mock.calls.length;
      } finally {
        registryKeys.mockRestore();
        minimum.mockRestore();
      }
      expect(similar).toEqual([]);
      expect(visits).toBe(65536);
      expect(comparisons).toBe(0);
    });

    describe.each(["HTTP", "stdio"])("%s errors", transport => {
      async function call(server, params) {
        const { handleRequest, handleMessage } = await import("./mcp_server_core.cjs");
        const request = { jsonrpc: "2.0", id: 42, method: "tools/call", params };
        if (transport === "HTTP") {
          return handleRequest(server, request);
        }
        let response;
        server.replyError = (id, code, message) => {
          response = { jsonrpc: "2.0", id, error: { code, message } };
        };
        server.replyResult = (id, result) => {
          response = { jsonrpc: "2.0", id, result };
        };
        await handleMessage(server, request);
        return response;
      }

      it.each([129, 1024 * 1024 - 256])("returns a bounded explicit error for an oversized tool name of length %i", async length => {
        const { createServer, registerTool } = await import("./mcp_server_core.cjs");
        const server = createServer({ name: "test-server", version: "1.0.0" });
        const handler = vi.fn();
        registerTool(server, { name: "test_tool", description: "", inputSchema: {}, handler });
        const response = await call(server, { name: "a".repeat(length) });
        expect(response.id).toBe(42);
        expect(response.error.code).toBe(transport === "HTTP" ? -32602 : -32601);
        expect(response.error.message).toContain("not found");
        expect(response.error.message).toContain("identifier too long for suggestions");
        expect(response.error.message.length).toBeLessThan(600);
        expect(handler).not.toHaveBeenCalled();
      });

      it("formats a 300-code-unit unknown tool name only once", async () => {
        const { createServer } = await import("./mcp_server_core.cjs");
        const server = createServer({ name: "test-server", version: "1.0.0" });
        const response = await call(server, { name: "Q".repeat(300) });
        expect(response.error.code).toBe(transport === "HTTP" ? -32602 : -32601);
        expect(response.error.message.match(/identifier too long for suggestions/g)).toHaveLength(1);
        expect(response.error.message).toContain("maximum 128 UTF-16 code units, got 300");
        expect(response.error.message).toContain("Q".repeat(128) + "...");
      });

      it.each(["tool", "parameter"])("does not split a surrogate pair in a truncated unknown %s name", async kind => {
        const { createServer, registerTool } = await import("./mcp_server_core.cjs");
        const server = createServer({ name: "test-server", version: "1.0.0" });
        registerTool(server, { name: "test_tool", description: "", inputSchema: { properties: {}, additionalProperties: false }, handler: vi.fn() });
        const name = "Q".repeat(127) + "😀";
        const params = kind === "tool" ? { name } : { name: "test_tool", arguments: { [name]: true } };
        const response = await call(server, params);
        expect(response.error.code).toBe(kind === "tool" && transport === "stdio" ? -32601 : -32602);
        expect(response.error.message).toContain("Q".repeat(127) + "...");
        expect(response.error.message).toContain("maximum 128 UTF-16 code units, got 129");
        expect(response.error.message.isWellFormed()).toBe(true);
      });

      it.each([127, 128, 129, 1024 * 1024 - 256])("bounds parameter suggestion work at length %i", async length => {
        const { createServer, registerTool } = await import("./mcp_server_core.cjs");
        const server = createServer({ name: "test-server", version: "1.0.0" });
        const candidate = "a".repeat(127) + "b";
        const handler = vi.fn();
        registerTool(server, { name: "test_tool", description: "", inputSchema: { properties: { [candidate]: {} }, additionalProperties: false }, handler });
        const minimum = vi.spyOn(Math, "min");
        let response;
        let comparisons;
        try {
          response = await call(server, { name: "test_tool", arguments: { ["a".repeat(length)]: true } });
          comparisons = minimum.mock.calls.length;
        } finally {
          minimum.mockRestore();
        }
        expect(response.error.code).toBe(-32602);
        expect(response.error.message).toContain("unknown parameter");
        expect(handler).not.toHaveBeenCalled();
        if (length <= 128) {
          expect(response.error.message).toContain(`closest: '${candidate}'`);
          expect(comparisons).toBe(length * 128);
        } else {
          expect(response.error.message).toContain("identifier too long for suggestions");
          expect(response.error.message.length).toBeLessThan(600);
          expect(comparisons).toBe(0);
        }
      });

      it("shares one matrix budget across all unknown parameters", async () => {
        const { createServer, registerTool } = await import("./mcp_server_core.cjs");
        const server = createServer({ name: "test-server", version: "1.0.0" });
        const properties = Object.fromEntries(Array.from({ length: 200 }, (_, i) => ["a".repeat(125) + String(i).padStart(3, "0"), {}]));
        const args = Object.fromEntries(Array.from({ length: 20 }, (_, i) => ["a".repeat(125) + String(i + 200), true]));
        const handler = vi.fn();
        registerTool(server, { name: "test_tool", description: "", inputSchema: { properties, additionalProperties: false }, handler });
        const minimum = vi.spyOn(Math, "min");
        let response;
        let comparisons;
        try {
          response = await call(server, { name: "test_tool", arguments: args });
          comparisons = minimum.mock.calls.length;
        } finally {
          minimum.mockRestore();
        }
        expect(response.error.code).toBe(-32602);
        expect(response.error.message).toContain("unknown parameters");
        expect(handler).not.toHaveBeenCalled();
        expect(comparisons).toBe(3 * 128 * 128);
        expect(comparisons).toBeLessThanOrEqual(65536);
      });

      it("bounds normalization work across many short unknown parameters and long registered names", async () => {
        const { createServer, registerTool } = await import("./mcp_server_core.cjs");
        const server = createServer({ name: "test-server", version: "1.0.0" });
        const properties = Object.fromEntries(Array.from({ length: 2048 }, (_, i) => ["z".repeat(124) + String(i).padStart(4, "0"), {}]));
        const args = Object.fromEntries(Array.from({ length: 40 }, (_, i) => [`x${i}`, true]));
        const handler = vi.fn();
        registerTool(server, { name: "test_tool", description: "", inputSchema: { properties, additionalProperties: false }, handler });
        const replace = String.prototype.replace;
        let normalizations = 0;
        const replacement = vi.spyOn(String.prototype, "replace").mockImplementation(function (...args) {
          if (this.length === 128 && this.startsWith("z".repeat(124))) {
            normalizations++;
          }
          return replace.apply(this, args);
        });
        const minimum = vi.spyOn(Math, "min");
        let response;
        let comparisons;
        try {
          response = await call(server, { name: "test_tool", arguments: args });
          comparisons = minimum.mock.calls.length;
        } finally {
          replacement.mockRestore();
          minimum.mockRestore();
        }
        expect(response.error.code).toBe(-32602);
        expect(handler).not.toHaveBeenCalled();
        expect(normalizations).toBe(65536);
        expect(comparisons).toBe(0);
      });

      it("preserves valid registered tool and parameter names above the suggestion limit", async () => {
        const { createServer, registerTool } = await import("./mcp_server_core.cjs");
        const server = createServer({ name: "test-server", version: "1.0.0" });
        const name = "a".repeat(129);
        const handler = vi.fn(() => ({ content: [] }));
        registerTool(server, { name, description: "", inputSchema: { properties: { [name]: {} }, additionalProperties: false }, handler });
        const args = { [name]: true };
        const response = await call(server, { name, arguments: args });
        expect(response.result).toEqual({ content: [], isError: false });
        expect(handler).toHaveBeenCalledWith(args);
      });
    });
  });

  describe("tool not found error with suggestions", () => {
    it("should suggest similar tools when tool is not found", async () => {
      const { createServer, registerTool, handleMessage } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "test-server", version: "1.0.0" });

      // Register some tools
      registerTool(server, {
        name: "add_comment",
        description: "Add comment",
        inputSchema: { type: "object", properties: {} },
        handler: () => ({ content: [{ type: "text", text: "ok" }] }),
      });
      registerTool(server, {
        name: "add_name",
        description: "Add name",
        inputSchema: { type: "object", properties: {} },
        handler: () => ({ content: [{ type: "text", text: "ok" }] }),
      });

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      // Try to call a tool that doesn't exist but is similar
      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "add_comentt", arguments: {} },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error).toBeDefined();
      expect(results[0].error.message).toContain("not found");
      expect(results[0].error.message).toContain("Did you mean one of these");
      expect(results[0].error.message).toContain("add_comment");
    });

    it("should not suggest tools if none are similar", async () => {
      const { createServer, registerTool, handleMessage } = await import("./mcp_server_core.cjs");
      const server = createServer({ name: "test-server", version: "1.0.0" });

      registerTool(server, {
        name: "x",
        description: "Test",
        inputSchema: { type: "object", properties: {} },
        handler: () => ({ content: [{ type: "text", text: "ok" }] }),
      });

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      // Try to call a completely different tool
      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "completelydifferenttoolname", arguments: {} },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error).toBeDefined();
      expect(results[0].error.message).toContain("not found");
      expect(results[0].error.message).not.toContain("Did you mean");
    });
  });
});
