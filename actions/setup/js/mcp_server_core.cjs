// @ts-check
/// <reference types="@actions/github-script" />

const { createLogger } = require("./mcp_logger.cjs");
const moduleLogger = createLogger("mcp_server_core");

// Log immediately at module load time
moduleLogger.debug("Module is being loaded");

/**
 * MCP Server Core Module
 *
 * This module provides a reusable API for creating MCP (Model Context Protocol) servers.
 * It handles JSON-RPC 2.0 message parsing, tool registration, and server lifecycle.
 *
 * Usage:
 *   const { createServer, registerTool, start } = require("./mcp_server_core.cjs");
 *
 *   const server = createServer({ name: "my-server", version: "1.0.0" });
 *   registerTool(server, {
 *     name: "my_tool",
 *     description: "A tool",
 *     inputSchema: { type: "object", properties: {} },
 *     handler: (args) => ({ content: [{ type: "text", text: "result" }] })
 *   });
 *   start(server);
 */

const fs = require("fs");
const path = require("path");

const { ReadBuffer } = require("./read_buffer.cjs");
const { validateRequiredFields } = require("./safe_inputs_validation.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { generateEnhancedErrorMessage } = require("./mcp_enhanced_errors.cjs");

const encoder = new TextEncoder();

/**
 * @typedef {Object} ServerInfo
 * @property {string} name - Server name
 * @property {string} version - Server version
 */

/**
 * @typedef {Object} Tool
 * @property {string} name - Tool name
 * @property {string} description - Tool description
 * @property {Object} inputSchema - JSON Schema for tool inputs
 * @property {Function} [handler] - Tool handler function
 * @property {string} [handlerPath] - Optional file path to handler module (original path from config)
 * @property {number} [timeout] - Timeout in seconds for tool execution (default: 60)
 */

/**
 * @typedef {Object} MCPServer
 * @property {ServerInfo} serverInfo - Server information
 * @property {Object<string, Tool>} tools - Registered tools
 * @property {Function} debug - Debug logging function
 * @property {Function} debugError - Debug logging function for errors (extracts message from Error objects)
 * @property {Function} writeMessage - Write message to stdout
 * @property {Function} replyResult - Send a result response
 * @property {Function} replyError - Send an error response
 * @property {ReadBuffer} readBuffer - Message buffer
 * @property {string} [logDir] - Optional log directory
 * @property {string} [logFilePath] - Optional log file path
 * @property {boolean} logFileInitialized - Whether log file has been initialized
 */

/**
 * Initialize log file for the server
 * @param {MCPServer} server - The MCP server instance
 */
function initLogFile(server) {
  if (server.logFileInitialized || !server.logDir || !server.logFilePath) return;
  try {
    if (!fs.existsSync(server.logDir)) {
      fs.mkdirSync(server.logDir, { recursive: true });
    }
    // Initialize/truncate log file with header
    const timestamp = new Date().toISOString();
    fs.writeFileSync(server.logFilePath, `# ${server.serverInfo.name} MCP Server Log\n# Started: ${timestamp}\n# Version: ${server.serverInfo.version}\n\n`);
    server.logFileInitialized = true;
  } catch {
    // Silently ignore errors - logging to stderr will still work
  }
}

/**
 * Create a debug function for the server
 * @param {MCPServer} server - The MCP server instance
 * @returns {Function} Debug function
 */
function createDebugFunction(server) {
  return msg => {
    const timestamp = new Date().toISOString();
    const formattedMsg = `[${timestamp}] [${server.serverInfo.name}] ${msg}\n`;

    // Always write to stderr
    process.stderr.write(formattedMsg);

    // Also write to log file if log directory is set (initialize on first use)
    if (server.logDir && server.logFilePath) {
      if (!server.logFileInitialized) {
        initLogFile(server);
      }
      if (server.logFileInitialized) {
        try {
          fs.appendFileSync(server.logFilePath, formattedMsg);
        } catch {
          // Silently ignore file write errors - stderr logging still works
        }
      }
    }
  };
}

/**
 * Create a debugError function for the server that handles error casting
 * @param {MCPServer} server - The MCP server instance
 * @returns {Function} Debug error function that extracts message from Error objects
 */
function createDebugErrorFunction(server) {
  return (prefix, error) => {
    const errorMessage = getErrorMessage(error);
    server.debug(`${prefix}${errorMessage}`);
    if (error instanceof Error && error.stack) {
      server.debug(`${prefix}Stack trace: ${error.stack}`);
    }
  };
}

/**
 * Create a writeMessage function for the server
 * @param {MCPServer} server - The MCP server instance
 * @returns {Function} Write message function
 */
function createWriteMessageFunction(server) {
  return obj => {
    const json = JSON.stringify(obj);
    server.debug(`send: ${json}`);
    const message = json + "\n";
    const bytes = encoder.encode(message);
    fs.writeSync(1, bytes);
  };
}

/**
 * Create a replyResult function for the server
 * @param {MCPServer} server - The MCP server instance
 * @returns {Function} Reply result function
 */
function createReplyResultFunction(server) {
  return (id, result) => {
    if (id === undefined || id === null) return; // notification
    const res = { jsonrpc: "2.0", id, result };
    server.writeMessage(res);
  };
}

/**
 * Create a replyError function for the server
 * @param {MCPServer} server - The MCP server instance
 * @returns {Function} Reply error function
 */
function createReplyErrorFunction(server) {
  return (id, code, message) => {
    // Don't send error responses for notifications (id is null/undefined)
    if (id === undefined || id === null) {
      server.debug(`Error for notification: ${message}`);
      return;
    }

    const error = { code, message };
    const res = {
      jsonrpc: "2.0",
      id,
      error,
    };
    server.writeMessage(res);
  };
}

/**
 * Create a new MCP server instance
 * @param {ServerInfo} serverInfo - Server information (name and version)
 * @param {Object} [options] - Optional server configuration
 * @param {string} [options.logDir] - Directory for log file (optional)
 * @returns {MCPServer} The MCP server instance
 */
function createServer(serverInfo, options = {}) {
  const logDir = options.logDir || undefined;
  const logFilePath = logDir ? path.join(logDir, "server.log") : undefined;

  /** @type {MCPServer} */
  const server = {
    serverInfo,
    tools: {},
    debug: () => {}, // placeholder
    debugError: () => {}, // placeholder
    writeMessage: () => {}, // placeholder
    replyResult: () => {}, // placeholder
    replyError: () => {}, // placeholder
    readBuffer: new ReadBuffer(),
    logDir,
    logFilePath,
    logFileInitialized: false,
  };

  // Initialize functions with references to server
  server.debug = createDebugFunction(server);
  server.debugError = createDebugErrorFunction(server);
  server.writeMessage = createWriteMessageFunction(server);
  server.replyResult = createReplyResultFunction(server);
  server.replyError = createReplyErrorFunction(server);

  return server;
}

/**
 * Create a wrapped handler function that normalizes results to MCP format.
 * Extracted to avoid creating closures with excessive scope in loadToolHandlers.
 *
 * @param {MCPServer} server - The MCP server instance for logging
 * @param {string} toolName - Name of the tool for logging purposes
 * @param {Function} handlerFn - The original handler function to wrap
 * @returns {Function} Wrapped async handler function
 */
function createWrappedHandler(server, toolName, handlerFn) {
  return async args => {
    server.debug(`  [${toolName}] Invoking handler with args: ${JSON.stringify(args)}`);

    try {
      // Call the handler (may be sync or async)
      const result = await Promise.resolve(handlerFn(args));
      server.debug(`  [${toolName}] Handler returned result type: ${typeof result}`);

      // If the result is already in MCP format (has content array), return as-is
      if (result && typeof result === "object" && Array.isArray(result.content)) {
        server.debug(`  [${toolName}] Result is already in MCP format`);
        return result;
      }

      // Otherwise, serialize the result to text
      // Use try-catch for serialization to handle circular references and non-serializable values
      let serializedResult;
      try {
        serializedResult = JSON.stringify(result);
      } catch (serializationError) {
        server.debugError(`  [${toolName}] Serialization error: `, serializationError);
        // Fall back to String() for non-serializable values
        serializedResult = String(result);
      }
      server.debug(`  [${toolName}] Serialized result: ${serializedResult.substring(0, 200)}${serializedResult.length > 200 ? "..." : ""}`);

      return {
        content: [
          {
            type: "text",
            text: serializedResult,
          },
        ],
      };
    } catch (error) {
      server.debugError(`  [${toolName}] Handler threw error: `, error);
      throw error;
    }
  };
}

/**
 * Load handler functions from file paths specified in tools configuration.
 * This function iterates through tools and loads handler modules based on file extension:
 *
 * For JavaScript handlers (.js, .cjs, .mjs):
 *   - Executed in a separate Node.js process for isolation
 *   - Inputs are passed as JSON via stdin
 *   - Outputs are read from stdout (JSON format expected)
 *   - Handler signature: reads JSON from stdin, writes result to stdout
 *
 * For Shell script handlers (.sh):
 *   - Uses GitHub Actions convention for passing inputs/outputs
 *   - Inputs are passed as environment variables prefixed with INPUT_ (uppercased)
 *   - Outputs are read from GITHUB_OUTPUT file (key=value format per line)
 *   - Returns: { stdout, stderr, outputs }
 *
 * For Python script handlers (.py):
 *   - Inputs are passed as JSON via stdin
 *   - Outputs are read from stdout (JSON format expected)
 *   - Executed using python3 command
 *
 * For Go script handlers (.go):
 *   - Inputs are passed as JSON via stdin
 *   - Outputs are read from stdout (JSON format expected)
 *   - Executed using 'go run' command
 *
 * SECURITY NOTE: Handler paths are loaded from tools.json configuration file,
 * which should be controlled by the server administrator. When basePath is provided,
 * relative paths are resolved within it, preventing directory traversal outside
 * the intended directory. Absolute paths bypass this validation but are still
 * logged for auditing purposes.
 *
 * @param {MCPServer} server - The MCP server instance for logging
 * @param {Array<Object>} tools - Array of tool configurations from tools.json
 * @param {string} [basePath] - Optional base path for resolving relative handler paths.
 *                              When provided, relative paths are validated to be within this directory.
 * @returns {Array<Object>} The tools array with loaded handlers attached
 */
function loadToolHandlers(server, tools, basePath) {
  server.debug(`Loading tool handlers...`);
  server.debug(`  Total tools to process: ${tools.length}`);
  server.debug(`  Base path: ${basePath || "(not specified)"}`);

  let loadedCount = 0;
  let skippedCount = 0;
  let errorCount = 0;

  for (const tool of tools) {
    const toolName = tool.name || "(unnamed)";

    // Check if tool has a handler path specified
    if (!tool.handler) {
      server.debug(`  [${toolName}] No handler path specified, skipping handler load`);
      skippedCount++;
      continue;
    }

    const handlerPath = tool.handler;
    server.debug(`  [${toolName}] Handler path specified: ${handlerPath}`);

    // Resolve the handler path
    let resolvedPath = handlerPath;
    if (basePath && !path.isAbsolute(handlerPath)) {
      resolvedPath = path.resolve(basePath, handlerPath);
      server.debug(`  [${toolName}] Resolved relative path to: ${resolvedPath}`);

      // Security validation: Ensure resolved path is within basePath to prevent directory traversal
      const normalizedBase = path.resolve(basePath);
      const normalizedResolved = path.resolve(resolvedPath);
      if (!normalizedResolved.startsWith(normalizedBase + path.sep) && normalizedResolved !== normalizedBase) {
        server.debug(`  [${toolName}] ERROR: Handler path escapes base directory: ${resolvedPath} is not within ${basePath}`);
        errorCount++;
        continue;
      }
    } else if (path.isAbsolute(handlerPath)) {
      server.debug(`  [${toolName}] Using absolute path (bypasses basePath validation): ${handlerPath}`);
    }

    // Store the original handler path for reference
    tool.handlerPath = handlerPath;

    try {
      server.debug(`  [${toolName}] Loading handler from: ${resolvedPath}`);

      // Check if file exists before loading
      if (!fs.existsSync(resolvedPath)) {
        server.debug(`  [${toolName}] ERROR: Handler file does not exist: ${resolvedPath}`);
        errorCount++;
        continue;
      }

      // Detect handler type by file extension
      const ext = path.extname(resolvedPath).toLowerCase();
      server.debug(`  [${toolName}] Handler file extension: ${ext}`);

      if (ext === ".sh") {
        // Shell script handler - use GitHub Actions convention
        server.debug(`  [${toolName}] Detected shell script handler`);

        // Make sure the script is executable (on Unix-like systems)
        try {
          fs.accessSync(resolvedPath, fs.constants.X_OK);
          server.debug(`  [${toolName}] Shell script is executable`);
        } catch {
          // Try to make it executable
          try {
            fs.chmodSync(resolvedPath, 0o755);
            server.debug(`  [${toolName}] Made shell script executable`);
          } catch (chmodError) {
            server.debugError(`  [${toolName}] Warning: Could not make shell script executable: `, chmodError);
            // Continue anyway - it might work depending on the shell
          }
        }

        // Lazy-load shell handler module
        const { createShellHandler } = require("./mcp_handler_shell.cjs");
        const timeout = tool.timeout || 60; // Default to 60 seconds if not specified
        tool.handler = createShellHandler(server, toolName, resolvedPath, timeout);

        loadedCount++;
        server.debug(`  [${toolName}] Shell handler created successfully with timeout: ${timeout}s`);
      } else if (ext === ".py") {
        // Python script handler - use GitHub Actions convention
        server.debug(`  [${toolName}] Detected Python script handler`);

        // Make sure the script is executable (on Unix-like systems)
        try {
          fs.accessSync(resolvedPath, fs.constants.X_OK);
          server.debug(`  [${toolName}] Python script is executable`);
        } catch {
          // Try to make it executable
          try {
            fs.chmodSync(resolvedPath, 0o755);
            server.debug(`  [${toolName}] Made Python script executable`);
          } catch (chmodError) {
            server.debugError(`  [${toolName}] Warning: Could not make Python script executable: `, chmodError);
            // Continue anyway - python3 will be called explicitly
          }
        }

        // Lazy-load Python handler module
        const { createPythonHandler } = require("./mcp_handler_python.cjs");
        const timeout = tool.timeout || 60; // Default to 60 seconds if not specified
        tool.handler = createPythonHandler(server, toolName, resolvedPath, timeout);

        loadedCount++;
        server.debug(`  [${toolName}] Python handler created successfully with timeout: ${timeout}s`);
      } else if (ext === ".go") {
        // Go script handler - uses go run command
        server.debug(`  [${toolName}] Detected Go script handler`);

        // Lazy-load Go handler module
        const { createGoHandler } = require("./mcp_handler_go.cjs");
        const timeout = tool.timeout || 60; // Default to 60 seconds if not specified
        tool.handler = createGoHandler(server, toolName, resolvedPath, timeout);

        loadedCount++;
        server.debug(`  [${toolName}] Go handler created successfully with timeout: ${timeout}s`);
      } else {
        // JavaScript/CommonJS handler - execute in separate Node.js process
        server.debug(`  [${toolName}] Detected JavaScript handler`);

        // Lazy-load JavaScript handler module
        const { createJavaScriptHandler } = require("./mcp_handler_javascript.cjs");
        const timeout = tool.timeout || 60; // Default to 60 seconds if not specified
        tool.handler = createJavaScriptHandler(server, toolName, resolvedPath, timeout);

        loadedCount++;
        server.debug(`  [${toolName}] JavaScript handler created successfully with timeout: ${timeout}s`);
      }
    } catch (error) {
      server.debugError(`  [${toolName}] ERROR loading handler: `, error);
      errorCount++;
    }
  }

  server.debug(`Handler loading complete:`);
  server.debug(`  Loaded: ${loadedCount}`);
  server.debug(`  Skipped (no handler path): ${skippedCount}`);
  server.debug(`  Errors: ${errorCount}`);

  return tools;
}

/**
 * Register a tool with the server
 * @param {MCPServer} server - The MCP server instance
 * @param {Tool} tool - The tool to register
 */
function registerTool(server, tool) {
  const normalizedName = normalizeTool(tool.name);
  server.tools[normalizedName] = {
    ...tool,
    name: normalizedName,
  };
  server.debug(`Registered tool: ${normalizedName}`);
}

/**
 * Calculate Levenshtein distance between two strings
 * @param {string} a - First string
 * @param {string} b - Second string
 * @returns {number} Edit distance
 */
function levenshteinDistance(a, b) {
  const matrix = [];

  // Initialize first column
  for (let i = 0; i <= b.length; i++) {
    matrix[i] = [i];
  }

  // Initialize first row
  for (let j = 0; j <= a.length; j++) {
    matrix[0][j] = j;
  }

  // Fill in the rest of the matrix
  for (let i = 1; i <= b.length; i++) {
    for (let j = 1; j <= a.length; j++) {
      if (b.charAt(i - 1) === a.charAt(j - 1)) {
        matrix[i][j] = matrix[i - 1][j - 1];
      } else {
        matrix[i][j] = Math.min(
          matrix[i - 1][j - 1] + 1, // substitution
          matrix[i][j - 1] + 1, // insertion
          matrix[i - 1][j] + 1 // deletion
        );
      }
    }
  }

  return matrix[b.length][a.length];
}

/**
 * Find similar tool names from available tools
 * @param {string} requestedTool - The tool name that was requested
 * @param {Object} availableTools - Object of available tools
 * @param {number} maxSuggestions - Maximum number of suggestions to return
 * @returns {Array<{name: string, distance: number}>} Array of similar tool names with distances
 */
function findSimilarTools(requestedTool, availableTools, maxSuggestions = 3) {
  const normalizedRequested = normalizeTool(requestedTool);
  const suggestions = [];

  // Calculate distance for each available tool
  for (const toolName of Object.keys(availableTools)) {
    const distance = levenshteinDistance(normalizedRequested, toolName);
    suggestions.push({ name: toolName, distance });
  }

  // Sort by distance (closest first) and take top N
  suggestions.sort((a, b) => a.distance - b.distance);

  // Only return suggestions that are reasonably similar
  // (distance <= half the length of the requested tool name + 3)
  const maxDistance = Math.floor(normalizedRequested.length / 2) + 3;
  return suggestions.filter(s => s.distance <= maxDistance).slice(0, maxSuggestions);
}

/**
 * Normalize a tool name (convert dashes to underscores, lowercase)
 * @param {string} name - The tool name to normalize
 * @returns {string} Normalized tool name
 */
function normalizeTool(name) {
  return name.replace(/-/g, "_").toLowerCase();
}

/**
 * Handle an incoming JSON-RPC request and return a response (for HTTP transport)
 * This function is compatible with the MCPServer class's handleRequest method.
 * @param {MCPServer} server - The MCP server instance
 * @param {Object} request - The incoming JSON-RPC request
 * @param {Function} [defaultHandler] - Default handler for tools without a handler
 * @returns {Promise<Object|null>} JSON-RPC response object, or null for notifications
 */
async function handleRequest(server, request, defaultHandler) {
  const { id, method, params } = request;

  try {
    // Handle notifications per JSON-RPC 2.0 spec:
    // Requests without id field are notifications (no response)
    // Note: id can be null for valid requests, so we check for field presence with "in" operator
    if (!("id" in request)) {
      // No id field - this is a notification (no response)
      return null;
    }

    let result;

    if (method === "initialize") {
      const protocolVersion = params?.protocolVersion || "2024-11-05";
      result = {
        protocolVersion,
        serverInfo: server.serverInfo,
        capabilities: {
          tools: {},
        },
      };
    } else if (method === "ping") {
      result = {};
    } else if (method === "tools/list") {
      const list = [];
      Object.values(server.tools).forEach(tool => {
        const toolDef = {
          name: tool.name,
          description: tool.description,
          inputSchema: tool.inputSchema,
        };
        list.push(toolDef);
      });
      result = { tools: list };
    } else if (method === "tools/call") {
      const name = params?.name;
      const args = params?.arguments ?? {};
      if (!name || typeof name !== "string") {
        throw {
          code: -32602,
          message: "Invalid params: 'name' must be a string",
        };
      }
      const tool = server.tools[normalizeTool(name)];
      if (!tool) {
        // Find similar tools to suggest
        const similarTools = findSimilarTools(name, server.tools);
        let errorMessage = `Tool '${name}' not found`;

        if (similarTools.length > 0) {
          const suggestions = similarTools.map(s => s.name).join(", ");
          errorMessage += `. Did you mean one of these: ${suggestions}?`;
        }

        throw {
          code: -32602,
          message: errorMessage,
        };
      }

      // Use tool handler, or default handler, or error
      let handler = tool.handler;
      if (!handler && defaultHandler) {
        handler = defaultHandler(tool.name);
      }
      if (!handler) {
        throw {
          code: -32603,
          message: `No handler for tool: ${name}`,
        };
      }

      const missing = validateRequiredFields(args, tool.inputSchema);
      if (missing.length) {
        throw {
          code: -32602,
          message: generateEnhancedErrorMessage(missing, name, tool.inputSchema),
        };
      }

      // Call handler and await the result (supports both sync and async handlers)
      const handlerResult = await Promise.resolve(handler(args));
      const content = handlerResult && handlerResult.content ? handlerResult.content : [];
      result = { content, isError: false };
    } else if (/^notifications\//.test(method)) {
      // Notifications don't need a response
      return null;
    } else {
      throw {
        code: -32601,
        message: `Method not found: ${method}`,
      };
    }

    return {
      jsonrpc: "2.0",
      id,
      result,
    };
  } catch (error) {
    /** @type {any} */
    const err = error;
    return {
      jsonrpc: "2.0",
      id,
      error: {
        code: err.code || -32603,
        message: err.message || "Internal error",
      },
    };
  }
}

/**
 * Handle an incoming JSON-RPC message (for stdio transport)
 * @param {MCPServer} server - The MCP server instance
 * @param {Object} req - The incoming request
 * @param {Function} [defaultHandler] - Default handler for tools without a handler
 * @returns {Promise<void>}
 */
async function handleMessage(server, req, defaultHandler) {
  // Validate basic JSON-RPC structure
  if (!req || typeof req !== "object") {
    server.debug(`Invalid message: not an object`);
    return;
  }

  if (req.jsonrpc !== "2.0") {
    server.debug(`Invalid message: missing or invalid jsonrpc field`);
    return;
  }

  const { id, method, params } = req;

  // Validate method field
  if (!method || typeof method !== "string") {
    server.replyError(id, -32600, "Invalid Request: method must be a string");
    return;
  }

  try {
    if (method === "initialize") {
      const clientInfo = params?.clientInfo ?? {};
      server.debug(`client info: ${JSON.stringify(clientInfo)}`);
      const protocolVersion = params?.protocolVersion ?? undefined;
      const result = {
        serverInfo: server.serverInfo,
        ...(protocolVersion ? { protocolVersion } : {}),
        capabilities: {
          tools: {},
        },
      };
      server.replyResult(id, result);
    } else if (method === "tools/list") {
      const list = [];
      Object.values(server.tools).forEach(tool => {
        const toolDef = {
          name: tool.name,
          description: tool.description,
          inputSchema: tool.inputSchema,
        };
        list.push(toolDef);
      });
      server.replyResult(id, { tools: list });
    } else if (method === "tools/call") {
      const name = params?.name;
      const args = params?.arguments ?? {};
      if (!name || typeof name !== "string") {
        server.replyError(id, -32602, "Invalid params: 'name' must be a string");
        return;
      }
      const tool = server.tools[normalizeTool(name)];
      if (!tool) {
        // Find similar tools to suggest
        const similarTools = findSimilarTools(name, server.tools);
        let errorMessage = `Tool not found: ${name} (${normalizeTool(name)})`;

        if (similarTools.length > 0) {
          const suggestions = similarTools.map(s => s.name).join(", ");
          errorMessage += `. Did you mean one of these: ${suggestions}?`;
        }

        server.replyError(id, -32601, errorMessage);
        return;
      }

      // Use tool handler, or default handler, or error
      let handler = tool.handler;
      if (!handler && defaultHandler) {
        handler = defaultHandler(tool.name);
      }
      if (!handler) {
        server.replyError(id, -32603, `No handler for tool: ${name}`);
        return;
      }

      const missing = validateRequiredFields(args, tool.inputSchema);
      if (missing.length) {
        server.replyError(id, -32602, generateEnhancedErrorMessage(missing, name, tool.inputSchema));
        return;
      }

      // Call handler and await the result (supports both sync and async handlers)
      server.debug(`Calling handler for tool: ${name}`);
      const result = await Promise.resolve(handler(args));
      server.debug(`Handler returned for tool: ${name}`);
      const content = result && result.content ? result.content : [];
      server.replyResult(id, { content, isError: false });
    } else if (/^notifications\//.test(method)) {
      server.debug(`ignore ${method}`);
    } else {
      server.replyError(id, -32601, `Method not found: ${method}`);
    }
  } catch (e) {
    server.replyError(id, -32603, e instanceof Error ? e.message : String(e));
  }
}

/**
 * Process the read buffer and handle messages
 * @param {MCPServer} server - The MCP server instance
 * @param {Function} [defaultHandler] - Default handler for tools without a handler
 * @returns {Promise<void>}
 */
async function processReadBuffer(server, defaultHandler) {
  while (true) {
    try {
      const message = server.readBuffer.readMessage();
      if (!message) {
        break;
      }
      server.debug(`recv: ${JSON.stringify(message)}`);
      await handleMessage(server, message, defaultHandler);
    } catch (error) {
      // For parse errors, we can't know the request id, so we shouldn't send a response
      // according to JSON-RPC spec. Just log the error.
      server.debug(`Parse error: ${getErrorMessage(error)}`);
    }
  }
}

/**
 * Start the MCP server on stdio
 * @param {MCPServer} server - The MCP server instance
 * @param {Object} [options] - Start options
 * @param {Function} [options.defaultHandler] - Default handler for tools without a handler
 */
function start(server, options = {}) {
  const { defaultHandler } = options;

  server.debug(`v${server.serverInfo.version} ready on stdio`);
  server.debug(`  tools: ${Object.keys(server.tools).join(", ")}`);

  if (!Object.keys(server.tools).length) {
    throw new Error("No tools registered");
  }

  const onData = async chunk => {
    server.readBuffer.append(chunk);
    await processReadBuffer(server, defaultHandler);
  };

  process.stdin.on("data", onData);
  process.stdin.on("error", err => server.debug(`stdin error: ${err}`));
  process.stdin.resume();
  server.debug(`listening...`);
}

module.exports = {
  createServer,
  registerTool,
  normalizeTool,
  handleRequest,
  handleMessage,
  processReadBuffer,
  start,
  loadToolHandlers,
  findSimilarTools,
  levenshteinDistance,
};
