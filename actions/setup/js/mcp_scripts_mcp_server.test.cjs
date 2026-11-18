import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";

describe("mcp_scripts_mcp_server.cjs", () => {
  let tempDir;

  beforeEach(() => {
    vi.resetModules();
    // Suppress stderr output during tests
    vi.spyOn(process.stderr, "write").mockImplementation(() => true);

    // Create a temporary directory for test files
    tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-scripts-test-"));
  });

  afterEach(() => {
    // Clean up temporary directory
    if (tempDir && fs.existsSync(tempDir)) {
      fs.rmSync(tempDir, { recursive: true });
    }
  });

  describe("loadConfig", () => {
    it("should load configuration from a valid JSON file", async () => {
      const { loadConfig } = await import("./mcp_scripts_mcp_server.cjs");

      // Create a test configuration file
      const configPath = path.join(tempDir, "config.json");
      const config = {
        serverName: "test-server",
        version: "1.0.0",
        tools: [
          {
            name: "test_tool",
            description: "A test tool",
            inputSchema: { type: "object", properties: {} },
          },
        ],
      };
      fs.writeFileSync(configPath, JSON.stringify(config));

      const loadedConfig = loadConfig(configPath);

      expect(loadedConfig.serverName).toBe("test-server");
      expect(loadedConfig.version).toBe("1.0.0");
      expect(loadedConfig.tools).toHaveLength(1);
      expect(loadedConfig.tools[0].name).toBe("test_tool");
    });

    it("should throw error for non-existent file", async () => {
      const { loadConfig } = await import("./mcp_scripts_mcp_server.cjs");

      expect(() => loadConfig("/non/existent/config.json")).toThrow("Configuration file not found");
    });

    it("should throw error for invalid JSON", async () => {
      const { loadConfig } = await import("./mcp_scripts_mcp_server.cjs");

      const configPath = path.join(tempDir, "invalid.json");
      fs.writeFileSync(configPath, "not valid json");

      expect(() => loadConfig(configPath)).toThrow();
    });

    it("should throw error for missing tools array", async () => {
      const { loadConfig } = await import("./mcp_scripts_mcp_server.cjs");

      const configPath = path.join(tempDir, "no-tools.json");
      fs.writeFileSync(configPath, JSON.stringify({ serverName: "test" }));

      expect(() => loadConfig(configPath)).toThrow("Configuration must contain a 'tools' array");
    });

    it("should throw error for tools that is not an array", async () => {
      const { loadConfig } = await import("./mcp_scripts_mcp_server.cjs");

      const configPath = path.join(tempDir, "tools-not-array.json");
      fs.writeFileSync(configPath, JSON.stringify({ tools: "not an array" }));

      expect(() => loadConfig(configPath)).toThrow("Configuration must contain a 'tools' array");
    });
  });

  describe("resolveConfigLogDir", () => {
    it("resolves the private setup root placeholder at runtime", async () => {
      const { resolveConfigLogDir } = await import("./mcp_scripts_mcp_server.cjs");

      expect(resolveConfigLogDir("${GH_AW_SETUP_ROOT}/mcp-scripts/logs", "/tmp/gh-aw-setup.test")).toBe(path.join("/tmp/gh-aw-setup.test", "mcp-scripts", "logs"));
      expect(resolveConfigLogDir("/tmp/legacy-logs", "/tmp/gh-aw-setup.test")).toBe("/tmp/legacy-logs");
    });
  });

  describe("createToolConfig", () => {
    it("should create a valid tool configuration", async () => {
      const { createToolConfig } = await import("./mcp_scripts_mcp_server.cjs");

      const config = createToolConfig("my_tool", "My tool description", { type: "object", properties: { input: { type: "string" } } }, "my_tool.cjs");

      expect(config.name).toBe("my_tool");
      expect(config.description).toBe("My tool description");
      expect(config.inputSchema).toEqual({ type: "object", properties: { input: { type: "string" } } });
      expect(config.handler).toBe("my_tool.cjs");
    });
  });

  describe("startMCPScriptsServer integration", () => {
    it("redacts stdio and file logs without changing successful or failed tool responses", async () => {
      const { spawn } = await import("child_process");
      const secret = 'private-input-"quoted"\nsecond-line';
      const handlerPath = path.join(tempDir, "echo.cjs");
      fs.writeFileSync(
        handlerPath,
        `let input = "";
process.stdin.on("data", chunk => { input += chunk; });
process.stdin.on("end", () => {
  const args = JSON.parse(input);
  console.error(args.message);
  console.log(JSON.stringify({ echo: args.message }));
  if (args.fail) process.exitCode = 1;
});`
      );
      const logDir = path.join(tempDir, "logs");
      const configPath = path.join(tempDir, "config.json");
      fs.writeFileSync(
        configPath,
        JSON.stringify({
          tools: [{ name: "echo", handler: handlerPath, inputSchema: { type: "object", properties: { message: { type: "string" }, fail: { type: "boolean" } } } }],
        })
      );
      const child = spawn(process.execPath, [path.join(__dirname, "mcp_scripts_mcp_server.cjs"), configPath, "--log-dir", logDir], { timeout: 10000, env: { ...process.env, DEBUG: "*" } });
      let stdout = "";
      let stderr = "";
      child.stdout.on("data", chunk => {
        stdout += chunk.toString();
      });
      child.stderr.on("data", chunk => {
        stderr += chunk.toString();
      });
      const closed = new Promise((resolve, reject) => {
        child.on("error", reject);
        child.on("close", code => resolve(code));
      });
      const requests = [false, true].map((fail, id) => ({ jsonrpc: "2.0", id, method: "tools/call", params: { name: "echo", arguments: { message: secret, fail } } }));
      const notification = { jsonrpc: "2.0", method: "tools/call", params: { name: "echo", arguments: { message: secret, fail: true } } };
      child.stdin.end([...requests, notification].map(message => JSON.stringify(message)).join("\n") + "\n");
      expect(await closed).toBe(0);
      const responses = stdout
        .trim()
        .split("\n")
        .map(line => JSON.parse(line));
      expect(responses).toHaveLength(2);
      expect(JSON.parse(responses[0].result.content[0].text).echo).toBe(secret);
      expect(responses[1].error.message).toContain(secret);
      for (const log of [stderr, fs.readFileSync(path.join(logDir, "server.log"), "utf8")]) {
        expect(log).not.toContain("private-input");
        expect(log).not.toContain("second-line");
        expect(log).toContain("[REDACTED]");
        expect(log).toContain("Calling handler for tool: echo");
        expect(log).toContain("Error for notification: [REDACTED]");
      }
    });

    it("should start server with JavaScript handler", async () => {
      // Create a handler file that reads from stdin and writes to stdout
      const handlerPath = path.join(tempDir, "test_handler.cjs");
      fs.writeFileSync(
        handlerPath,
        `let input = '';
process.stdin.on('data', chunk => { input += chunk; });
process.stdin.on('end', () => {
  const args = JSON.parse(input);
  const result = { result: "hello " + args.name };
  console.log(JSON.stringify(result));
});`
      );

      // Create config file
      const configPath = path.join(tempDir, "config.json");
      const config = {
        serverName: "test-mcp-scripts",
        version: "1.0.0",
        tools: [
          {
            name: "greet",
            description: "Greet someone",
            inputSchema: { type: "object", properties: { name: { type: "string" } } },
            handler: "test_handler.cjs",
          },
        ],
      };
      fs.writeFileSync(configPath, JSON.stringify(config));

      // Import and load config
      const { loadConfig } = await import("./mcp_scripts_mcp_server.cjs");
      const { createServer, registerTool, loadToolHandlers, handleMessage } = await import("./mcp_server_core.cjs");

      const loadedConfig = loadConfig(configPath);
      const server = createServer({ name: loadedConfig.serverName || "mcpscripts", version: loadedConfig.version || "1.0.0" });

      // Load handlers
      const tools = loadToolHandlers(server, loadedConfig.tools, tempDir);

      // Register tools
      for (const tool of tools) {
        registerTool(server, tool);
      }

      // Test tool call
      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "greet", arguments: { name: "world" } },
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.content[0].text).toContain("hello world");
    });

    it("should start server with shell script handler", async () => {
      // Create a shell script handler
      const handlerPath = path.join(tempDir, "test_handler.sh");
      fs.writeFileSync(
        handlerPath,
        `#!/bin/bash
echo "Shell says: $INPUT_NAME"
echo "greeting=Hello from shell" >> "$GITHUB_OUTPUT"
`,
        { mode: 0o755 }
      );

      // Create config file
      const configPath = path.join(tempDir, "config.json");
      const config = {
        tools: [
          {
            name: "shell_greet",
            description: "Greet from shell",
            inputSchema: { type: "object", properties: { name: { type: "string" } } },
            handler: "test_handler.sh",
          },
        ],
      };
      fs.writeFileSync(configPath, JSON.stringify(config));

      // Import and load config
      const { loadConfig } = await import("./mcp_scripts_mcp_server.cjs");
      const { createServer, registerTool, loadToolHandlers, handleMessage } = await import("./mcp_server_core.cjs");

      const loadedConfig = loadConfig(configPath);
      const server = createServer({ name: "mcpscripts", version: "1.0.0" });

      // Load handlers
      const tools = loadToolHandlers(server, loadedConfig.tools, tempDir);

      // Register tools
      for (const tool of tools) {
        registerTool(server, tool);
      }

      // Test tool call
      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "shell_greet", arguments: { name: "tester" } },
      });

      expect(results).toHaveLength(1);
      const resultContent = JSON.parse(results[0].result.content[0].text);
      expect(resultContent.stdout).toContain("Shell says: tester");
      expect(resultContent.outputs.greeting).toBe("Hello from shell");
    });

    it("should handle tools/list request", async () => {
      // Create config file with multiple tools
      const configPath = path.join(tempDir, "config.json");
      const config = {
        serverName: "test-server",
        version: "2.0.0",
        tools: [
          {
            name: "tool_one",
            description: "First tool",
            inputSchema: { type: "object", properties: { a: { type: "string" } } },
          },
          {
            name: "tool_two",
            description: "Second tool",
            inputSchema: { type: "object", properties: { b: { type: "number" } } },
          },
        ],
      };
      fs.writeFileSync(configPath, JSON.stringify(config));

      const { loadConfig } = await import("./mcp_scripts_mcp_server.cjs");
      const { createServer, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      const loadedConfig = loadConfig(configPath);
      const server = createServer({ name: loadedConfig.serverName, version: loadedConfig.version });

      // Register tools (no handlers, just for listing)
      for (const tool of loadedConfig.tools) {
        registerTool(server, tool);
      }

      // Test tools/list
      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/list",
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.tools).toHaveLength(2);

      const toolNames = results[0].result.tools.map(t => t.name);
      expect(toolNames).toContain("tool_one");
      expect(toolNames).toContain("tool_two");
    });

    it("should handle initialize request", async () => {
      const configPath = path.join(tempDir, "config.json");
      fs.writeFileSync(configPath, JSON.stringify({ tools: [{ name: "mock", description: "mock", inputSchema: {} }] }));

      const { loadConfig } = await import("./mcp_scripts_mcp_server.cjs");
      const { createServer, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      const loadedConfig = loadConfig(configPath);
      const server = createServer({ name: "mcpscripts", version: "1.0.0" });

      for (const tool of loadedConfig.tools) {
        registerTool(server, tool);
      }

      const results = [];
      server.writeMessage = msg => results.push(msg);
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: { protocolVersion: "2024-11-05" },
      });

      expect(results).toHaveLength(1);
      expect(results[0].result.serverInfo).toEqual({ name: "mcpscripts", version: "1.0.0" });
      expect(results[0].result.protocolVersion).toBe("2024-11-05");
      expect(results[0].result.capabilities).toEqual({ tools: {} });
    });

    it("should use default server name and version if not provided", async () => {
      const configPath = path.join(tempDir, "config.json");
      fs.writeFileSync(
        configPath,
        JSON.stringify({
          tools: [{ name: "test", description: "test", inputSchema: {} }],
        })
      );

      const { loadConfig } = await import("./mcp_scripts_mcp_server.cjs");
      const { createServer, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      const loadedConfig = loadConfig(configPath);
      const serverName = loadedConfig.serverName || "mcpscripts";
      const version = loadedConfig.version || "1.0.0";
      const server = createServer({ name: serverName, version });

      for (const tool of loadedConfig.tools) {
        registerTool(server, tool);
      }

      const results = [];
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "initialize",
        params: {},
      });

      expect(results[0].result.serverInfo.name).toBe("mcpscripts");
      expect(results[0].result.serverInfo.version).toBe("1.0.0");
    });
  });

  describe("error handling", () => {
    it("should return error for unknown tool", async () => {
      const configPath = path.join(tempDir, "config.json");
      fs.writeFileSync(configPath, JSON.stringify({ tools: [{ name: "known_tool", description: "test", inputSchema: {} }] }));

      const { loadConfig } = await import("./mcp_scripts_mcp_server.cjs");
      const { createServer, registerTool, handleMessage } = await import("./mcp_server_core.cjs");

      const loadedConfig = loadConfig(configPath);
      const server = createServer({ name: "mcpscripts", version: "1.0.0" });

      for (const tool of loadedConfig.tools) {
        registerTool(server, tool);
      }

      const results = [];
      server.replyResult = (id, result) => results.push({ jsonrpc: "2.0", id, result });
      server.replyError = (id, code, message) => results.push({ jsonrpc: "2.0", id, error: { code, message } });

      await handleMessage(server, {
        jsonrpc: "2.0",
        id: 1,
        method: "tools/call",
        params: { name: "unknown_tool", arguments: {} },
      });

      expect(results).toHaveLength(1);
      expect(results[0].error.code).toBe(-32601);
      expect(results[0].error.message).toContain("Tool not found");
    });
  });

  describe.skip("end-to-end server process", () => {
    it("should write files, launch server, initialize, call echo tool and verify result", async () => {
      const { spawn } = await import("child_process");

      // 1. Write the mcp-scripts files

      // Copy mcp_server_core.cjs
      const mcpServerCorePath = path.join(tempDir, "mcp_server_core.cjs");
      const mcpServerCoreContent = fs.readFileSync(path.join(__dirname, "mcp_server_core.cjs"), "utf-8");
      fs.writeFileSync(mcpServerCorePath, mcpServerCoreContent);

      // Copy read_buffer.cjs
      const readBufferPath = path.join(tempDir, "read_buffer.cjs");
      const readBufferContent = fs.readFileSync(path.join(__dirname, "read_buffer.cjs"), "utf-8");
      fs.writeFileSync(readBufferPath, readBufferContent);

      // Copy mcp_scripts_config_loader.cjs
      const configLoaderPath = path.join(tempDir, "mcp_scripts_config_loader.cjs");
      const configLoaderContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_config_loader.cjs"), "utf-8");
      fs.writeFileSync(configLoaderPath, configLoaderContent);

      // Copy mcp_scripts_tool_factory.cjs
      const toolFactoryPath = path.join(tempDir, "mcp_scripts_tool_factory.cjs");
      const toolFactoryContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_tool_factory.cjs"), "utf-8");
      fs.writeFileSync(toolFactoryPath, toolFactoryContent);

      // Copy schema-validation.cjs
      const validationPath = path.join(tempDir, "schema-validation.cjs");
      const validationContent = fs.readFileSync(path.join(__dirname, "schema-validation.cjs"), "utf-8");
      fs.writeFileSync(validationPath, validationContent);

      // Copy mcp_scripts_bootstrap.cjs
      const bootstrapPath = path.join(tempDir, "mcp_scripts_bootstrap.cjs");
      const bootstrapContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_bootstrap.cjs"), "utf-8");
      fs.writeFileSync(bootstrapPath, bootstrapContent);

      // Copy mcp_handler_python.cjs
      const pythonHandlerPath = path.join(tempDir, "mcp_handler_python.cjs");
      const pythonHandlerContent = fs.readFileSync(path.join(__dirname, "mcp_handler_python.cjs"), "utf-8");
      fs.writeFileSync(pythonHandlerPath, pythonHandlerContent);

      // Copy mcp_handler_shell.cjs
      const shellHandlerPath = path.join(tempDir, "mcp_handler_shell.cjs");
      const shellHandlerContent = fs.readFileSync(path.join(__dirname, "mcp_handler_shell.cjs"), "utf-8");
      fs.writeFileSync(shellHandlerPath, shellHandlerContent);

      // Copy mcp_scripts_mcp_server.cjs
      const safeinputsServerPath = path.join(tempDir, "mcp_scripts_mcp_server.cjs");
      const safeinputsServerContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_mcp_server.cjs"), "utf-8");
      fs.writeFileSync(safeinputsServerPath, safeinputsServerContent);

      // Create an echo tool handler that reads from stdin and writes to stdout
      const echoHandlerPath = path.join(tempDir, "echo.cjs");
      fs.writeFileSync(
        echoHandlerPath,
        `let input = '';
process.stdin.on('data', chunk => { input += chunk; });
process.stdin.on('end', () => {
  const args = JSON.parse(input);
  const result = { message: "Echo: " + args.message };
  console.log(JSON.stringify(result));
});`
      );

      // Create the tools.json configuration with echo tool
      const toolsConfigPath = path.join(tempDir, "tools.json");
      const toolsConfig = {
        serverName: "test-safeinputs",
        version: "1.0.0",
        tools: [
          {
            name: "echo",
            description: "Echoes the input message back",
            inputSchema: {
              type: "object",
              properties: {
                message: { type: "string", description: "The message to echo" },
              },
              required: ["message"],
            },
            handler: "echo.cjs",
          },
        ],
      };
      fs.writeFileSync(toolsConfigPath, JSON.stringify(toolsConfig, null, 2));

      // 2. Launch the server as a child process
      const serverProcess = spawn("node", [safeinputsServerPath, toolsConfigPath], {
        cwd: tempDir,
        stdio: ["pipe", "pipe", "pipe"],
        env: { ...process.env },
      });

      // Collect stderr output for debugging
      let stderrOutput = "";
      serverProcess.stderr.on("data", chunk => {
        stderrOutput += chunk.toString();
      });

      // Set up promise-based message handling
      let stdoutBuffer = "";
      const receivedMessages = [];

      serverProcess.stdout.on("data", chunk => {
        stdoutBuffer += chunk.toString();
        // Parse complete JSON messages (newline-delimited)
        const lines = stdoutBuffer.split("\n");
        stdoutBuffer = lines.pop() || ""; // Keep incomplete line in buffer
        for (const line of lines) {
          if (line.trim()) {
            try {
              receivedMessages.push(JSON.parse(line));
            } catch (e) {
              // Ignore parse errors for incomplete messages
            }
          }
        }
      });

      /**
       * Send a JSON-RPC message and wait for a response
       * @param {object} message - The JSON-RPC message to send
       * @param {number} timeoutMs - Timeout in milliseconds
       * @returns {Promise<object>} The response message
       */
      function sendAndWait(message, timeoutMs = 5000) {
        return new Promise((resolve, reject) => {
          const startLength = receivedMessages.length;
          const startTime = Date.now();

          // Send the message
          serverProcess.stdin.write(JSON.stringify(message) + "\n");

          // Poll for response
          const checkInterval = setInterval(() => {
            if (receivedMessages.length > startLength) {
              clearInterval(checkInterval);
              resolve(receivedMessages[receivedMessages.length - 1]);
            } else if (Date.now() - startTime > timeoutMs) {
              clearInterval(checkInterval);
              reject(new Error(`Timeout waiting for response to ${message.method}. Stderr: ${stderrOutput}`));
            }
          }, 10);
        });
      }

      try {
        // Give the server a moment to start
        await new Promise(resolve => setTimeout(resolve, 100));

        // 3. Send initialize request
        const initResponse = await sendAndWait({
          jsonrpc: "2.0",
          id: 1,
          method: "initialize",
          params: { protocolVersion: "2024-11-05" },
        });

        expect(initResponse.jsonrpc).toBe("2.0");
        expect(initResponse.id).toBe(1);
        expect(initResponse.result).toBeDefined();
        expect(initResponse.result.serverInfo.name).toBe("test-safeinputs");
        expect(initResponse.result.serverInfo.version).toBe("1.0.0");
        expect(initResponse.result.protocolVersion).toBe("2024-11-05");
        expect(initResponse.result.capabilities).toEqual({ tools: {} });

        // Send initialized notification (MCP protocol requirement)
        serverProcess.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" }) + "\n");

        // 4. Call the echo tool
        const echoResponse = await sendAndWait({
          jsonrpc: "2.0",
          id: 2,
          method: "tools/call",
          params: {
            name: "echo",
            arguments: { message: "Hello, MCP!" },
          },
        });

        expect(echoResponse.jsonrpc).toBe("2.0");
        expect(echoResponse.id).toBe(2);
        expect(echoResponse.result).toBeDefined();
        expect(echoResponse.result.content).toBeDefined();
        expect(echoResponse.result.content.length).toBe(1);
        expect(echoResponse.result.content[0].type).toBe("text");

        // 5. Verify the result
        const echoResult = JSON.parse(echoResponse.result.content[0].text);
        expect(echoResult.message).toBe("Echo: Hello, MCP!");
      } finally {
        // Clean up: kill the server process
        serverProcess.kill("SIGTERM");
        await new Promise(resolve => serverProcess.on("close", resolve));
      }
    }, 15000); // 15 second timeout for the full test

    it("should handle tools/list request in spawned process", async () => {
      const { spawn } = await import("child_process");

      // Write the necessary files
      const mcpServerCorePath = path.join(tempDir, "mcp_server_core.cjs");
      const mcpServerCoreContent = fs.readFileSync(path.join(__dirname, "mcp_server_core.cjs"), "utf-8");
      fs.writeFileSync(mcpServerCorePath, mcpServerCoreContent);

      const readBufferPath = path.join(tempDir, "read_buffer.cjs");
      const readBufferContent = fs.readFileSync(path.join(__dirname, "read_buffer.cjs"), "utf-8");
      fs.writeFileSync(readBufferPath, readBufferContent);

      const configLoaderPath = path.join(tempDir, "mcp_scripts_config_loader.cjs");
      const configLoaderContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_config_loader.cjs"), "utf-8");
      fs.writeFileSync(configLoaderPath, configLoaderContent);

      const toolFactoryPath = path.join(tempDir, "mcp_scripts_tool_factory.cjs");
      const toolFactoryContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_tool_factory.cjs"), "utf-8");
      fs.writeFileSync(toolFactoryPath, toolFactoryContent);

      const validationPath = path.join(tempDir, "schema-validation.cjs");
      const validationContent = fs.readFileSync(path.join(__dirname, "schema-validation.cjs"), "utf-8");
      fs.writeFileSync(validationPath, validationContent);

      const bootstrapPath = path.join(tempDir, "mcp_scripts_bootstrap.cjs");
      const bootstrapContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_bootstrap.cjs"), "utf-8");
      fs.writeFileSync(bootstrapPath, bootstrapContent);

      const pythonHandlerPath = path.join(tempDir, "mcp_handler_python.cjs");
      const pythonHandlerContent = fs.readFileSync(path.join(__dirname, "mcp_handler_python.cjs"), "utf-8");
      fs.writeFileSync(pythonHandlerPath, pythonHandlerContent);

      const shellHandlerPath = path.join(tempDir, "mcp_handler_shell.cjs");
      const shellHandlerContent = fs.readFileSync(path.join(__dirname, "mcp_handler_shell.cjs"), "utf-8");
      fs.writeFileSync(shellHandlerPath, shellHandlerContent);

      const safeinputsServerPath = path.join(tempDir, "mcp_scripts_mcp_server.cjs");
      const safeinputsServerContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_mcp_server.cjs"), "utf-8");
      fs.writeFileSync(safeinputsServerPath, safeinputsServerContent);

      // Create tools.json with multiple tools (no handlers needed for list)
      const toolsConfigPath = path.join(tempDir, "tools.json");
      const toolsConfig = {
        serverName: "list-test-server",
        version: "2.0.0",
        tools: [
          {
            name: "tool_one",
            description: "First test tool",
            inputSchema: { type: "object", properties: { a: { type: "string" } } },
          },
          {
            name: "tool_two",
            description: "Second test tool",
            inputSchema: { type: "object", properties: { b: { type: "number" } } },
          },
        ],
      };
      fs.writeFileSync(toolsConfigPath, JSON.stringify(toolsConfig, null, 2));

      const serverProcess = spawn("node", [safeinputsServerPath, toolsConfigPath], {
        cwd: tempDir,
        stdio: ["pipe", "pipe", "pipe"],
      });

      let stderrOutput = "";
      serverProcess.stderr.on("data", chunk => {
        stderrOutput += chunk.toString();
      });

      let stdoutBuffer = "";
      const receivedMessages = [];

      serverProcess.stdout.on("data", chunk => {
        stdoutBuffer += chunk.toString();
        const lines = stdoutBuffer.split("\n");
        stdoutBuffer = lines.pop() || "";
        for (const line of lines) {
          if (line.trim()) {
            try {
              receivedMessages.push(JSON.parse(line));
            } catch (e) {
              // Ignore
            }
          }
        }
      });

      function sendAndWait(message, timeoutMs = 5000) {
        return new Promise((resolve, reject) => {
          const startLength = receivedMessages.length;
          const startTime = Date.now();
          serverProcess.stdin.write(JSON.stringify(message) + "\n");

          const checkInterval = setInterval(() => {
            if (receivedMessages.length > startLength) {
              clearInterval(checkInterval);
              resolve(receivedMessages[receivedMessages.length - 1]);
            } else if (Date.now() - startTime > timeoutMs) {
              clearInterval(checkInterval);
              reject(new Error(`Timeout. Stderr: ${stderrOutput}`));
            }
          }, 10);
        });
      }

      try {
        await new Promise(resolve => setTimeout(resolve, 100));

        // Initialize
        await sendAndWait({
          jsonrpc: "2.0",
          id: 1,
          method: "initialize",
          params: {},
        });

        // List tools
        const listResponse = await sendAndWait({
          jsonrpc: "2.0",
          id: 2,
          method: "tools/list",
        });

        expect(listResponse.result.tools).toHaveLength(2);
        const toolNames = listResponse.result.tools.map(t => t.name);
        expect(toolNames).toContain("tool_one");
        expect(toolNames).toContain("tool_two");
      } finally {
        serverProcess.kill("SIGTERM");
        await new Promise(resolve => serverProcess.on("close", resolve));
      }
    }, 15000);

    it("should handle shell script echo tool in spawned process", async () => {
      const { spawn } = await import("child_process");

      // Write the necessary files
      const mcpServerCorePath = path.join(tempDir, "mcp_server_core.cjs");
      const mcpServerCoreContent = fs.readFileSync(path.join(__dirname, "mcp_server_core.cjs"), "utf-8");
      fs.writeFileSync(mcpServerCorePath, mcpServerCoreContent);

      const readBufferPath = path.join(tempDir, "read_buffer.cjs");
      const readBufferContent = fs.readFileSync(path.join(__dirname, "read_buffer.cjs"), "utf-8");
      fs.writeFileSync(readBufferPath, readBufferContent);

      const configLoaderPath = path.join(tempDir, "mcp_scripts_config_loader.cjs");
      const configLoaderContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_config_loader.cjs"), "utf-8");
      fs.writeFileSync(configLoaderPath, configLoaderContent);

      const toolFactoryPath = path.join(tempDir, "mcp_scripts_tool_factory.cjs");
      const toolFactoryContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_tool_factory.cjs"), "utf-8");
      fs.writeFileSync(toolFactoryPath, toolFactoryContent);

      const validationPath = path.join(tempDir, "schema-validation.cjs");
      const validationContent = fs.readFileSync(path.join(__dirname, "schema-validation.cjs"), "utf-8");
      fs.writeFileSync(validationPath, validationContent);

      const bootstrapPath = path.join(tempDir, "mcp_scripts_bootstrap.cjs");
      const bootstrapContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_bootstrap.cjs"), "utf-8");
      fs.writeFileSync(bootstrapPath, bootstrapContent);

      const pythonHandlerPath = path.join(tempDir, "mcp_handler_python.cjs");
      const pythonHandlerContent = fs.readFileSync(path.join(__dirname, "mcp_handler_python.cjs"), "utf-8");
      fs.writeFileSync(pythonHandlerPath, pythonHandlerContent);

      const shellHandlerPath = path.join(tempDir, "mcp_handler_shell.cjs");
      const shellHandlerContent = fs.readFileSync(path.join(__dirname, "mcp_handler_shell.cjs"), "utf-8");
      fs.writeFileSync(shellHandlerPath, shellHandlerContent);

      const safeinputsServerPath = path.join(tempDir, "mcp_scripts_mcp_server.cjs");
      const safeinputsServerContent = fs.readFileSync(path.join(__dirname, "mcp_scripts_mcp_server.cjs"), "utf-8");
      fs.writeFileSync(safeinputsServerPath, safeinputsServerContent);

      // Create a shell script echo handler
      const echoShPath = path.join(tempDir, "echo.sh");
      fs.writeFileSync(
        echoShPath,
        `#!/bin/bash
echo "Shell echo: $INPUT_MESSAGE"
echo "result=Shell echo: $INPUT_MESSAGE" >> "$GITHUB_OUTPUT"
`,
        { mode: 0o755 }
      );

      // Create tools.json with shell script handler
      const toolsConfigPath = path.join(tempDir, "tools.json");
      const toolsConfig = {
        serverName: "shell-echo-server",
        version: "1.0.0",
        tools: [
          {
            name: "shell_echo",
            description: "Echoes message using shell script",
            inputSchema: {
              type: "object",
              properties: { message: { type: "string" } },
              required: ["message"],
            },
            handler: "echo.sh",
          },
        ],
      };
      fs.writeFileSync(toolsConfigPath, JSON.stringify(toolsConfig, null, 2));

      const serverProcess = spawn("node", [safeinputsServerPath, toolsConfigPath], {
        cwd: tempDir,
        stdio: ["pipe", "pipe", "pipe"],
      });

      let stderrOutput = "";
      serverProcess.stderr.on("data", chunk => {
        stderrOutput += chunk.toString();
      });

      let stdoutBuffer = "";
      const receivedMessages = [];

      serverProcess.stdout.on("data", chunk => {
        stdoutBuffer += chunk.toString();
        const lines = stdoutBuffer.split("\n");
        stdoutBuffer = lines.pop() || "";
        for (const line of lines) {
          if (line.trim()) {
            try {
              receivedMessages.push(JSON.parse(line));
            } catch (e) {
              // Ignore
            }
          }
        }
      });

      function sendAndWait(message, timeoutMs = 5000) {
        return new Promise((resolve, reject) => {
          const startLength = receivedMessages.length;
          const startTime = Date.now();
          serverProcess.stdin.write(JSON.stringify(message) + "\n");

          const checkInterval = setInterval(() => {
            if (receivedMessages.length > startLength) {
              clearInterval(checkInterval);
              resolve(receivedMessages[receivedMessages.length - 1]);
            } else if (Date.now() - startTime > timeoutMs) {
              clearInterval(checkInterval);
              reject(new Error(`Timeout. Stderr: ${stderrOutput}`));
            }
          }, 10);
        });
      }

      try {
        await new Promise(resolve => setTimeout(resolve, 100));

        // Initialize
        await sendAndWait({
          jsonrpc: "2.0",
          id: 1,
          method: "initialize",
          params: {},
        });

        // Call shell echo tool
        const echoResponse = await sendAndWait({
          jsonrpc: "2.0",
          id: 2,
          method: "tools/call",
          params: {
            name: "shell_echo",
            arguments: { message: "Hello from shell!" },
          },
        });

        expect(echoResponse.result).toBeDefined();
        expect(echoResponse.result.content).toBeDefined();

        const resultContent = JSON.parse(echoResponse.result.content[0].text);
        expect(resultContent.stdout).toContain("Shell echo: Hello from shell!");
        expect(resultContent.outputs.result).toBe("Shell echo: Hello from shell!");
      } finally {
        serverProcess.kill("SIGTERM");
        await new Promise(resolve => serverProcess.on("close", resolve));
      }
    }, 15000);
  });
});
