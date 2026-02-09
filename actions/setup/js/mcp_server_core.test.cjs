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

    it("should return error for missing required fields", async () => {
      const { handleMessage } = await import("./mcp_server_core.cjs");

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: {
          name: "test_tool",
          arguments: {}, // missing required 'input'
        },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32602);
      expect(results[0].error.message).toContain("missing or empty");
      // Verify enhanced error message includes guidance
      expect(results[0].error.message).toContain("Required parameter");
      expect(results[0].error.message).toContain("Example:");
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
echo "result=success" >> $GITHUB_OUTPUT
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
} >> $GITHUB_OUTPUT
`,
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
echo "result=$INPUT_MY_INPUT" >> $GITHUB_OUTPUT
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

  describe("findSimilarTools", () => {
    it("should find tools with typos", async () => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      const tools = {
        add_comment: {},
        add_name: {},
        missing_tool: {},
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

    it("should limit results to maxSuggestions", async () => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      const tools = {
        tool_a: {},
        tool_b: {},
        tool_c: {},
        tool_d: {},
        tool_e: {},
      };

      const similar = findSimilarTools("tool_x", tools, 2);
      expect(similar.length).toBeLessThanOrEqual(2);
    });

    it("should sort by distance (closest first)", async () => {
      const { findSimilarTools } = await import("./mcp_server_core.cjs");
      const tools = {
        add_name: {},
        add_comment: {},
        missing_tool: {},
      };

      const similar = findSimilarTools("add_nam", tools, 3);
      expect(similar.length).toBeGreaterThan(0);
      expect(similar[0].name).toBe("add_name");
      expect(similar[0].distance).toBe(1);
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
