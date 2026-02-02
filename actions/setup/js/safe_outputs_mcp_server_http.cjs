// @ts-check
/// <reference types="@actions/github-script" />

const { createLogger } = require("./mcp_logger.cjs");
const moduleLogger = createLogger("safe_outputs_mcp_server_http");

// Log immediately at module load time (before any requires)
moduleLogger.debug("Module is being loaded");

/**
 * Safe Outputs MCP Server with HTTP Transport
 *
 * This module extends the safe-outputs MCP server to support HTTP transport
 * using the StreamableHTTPServerTransport from the MCP SDK.
 *
 * It provides both stateful and stateless HTTP modes, as well as SSE streaming.
 *
 * Usage:
 *   node safe_outputs_mcp_server_http.cjs [--port 3000] [--stateless]
 *
 * Options:
 *   --port <number>    Port to listen on (default: 3000)
 *   --stateless        Run in stateless mode (no session management)
 *   --log-dir <path>   Directory for log files
 */

const http = require("http");
moduleLogger.debug("Loaded http");
const { randomUUID } = require("crypto");
moduleLogger.debug("Loaded crypto");
const { MCPServer, MCPHTTPTransport } = require("./mcp_http_transport.cjs");
moduleLogger.debug("Loaded mcp_http_transport.cjs");
const { createLogger: createMCPLogger } = require("./mcp_logger.cjs");
moduleLogger.debug("Loaded mcp_logger.cjs");
const { bootstrapSafeOutputsServer, cleanupConfigFile } = require("./safe_outputs_bootstrap.cjs");
moduleLogger.debug("Loaded safe_outputs_bootstrap.cjs");
const { createAppendFunction } = require("./safe_outputs_append.cjs");
moduleLogger.debug("Loaded safe_outputs_append.cjs");
const { createHandlers } = require("./safe_outputs_handlers.cjs");
moduleLogger.debug("Loaded safe_outputs_handlers.cjs");
const { attachHandlers, registerPredefinedTools, registerDynamicTools } = require("./safe_outputs_tools_loader.cjs");
moduleLogger.debug("Loaded safe_outputs_tools_loader.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
moduleLogger.debug("All modules loaded successfully");

/**
 * Create and configure the MCP server with tools
 * @param {Object} [options] - Additional options
 * @param {string} [options.logDir] - Override log directory from config
 * @returns {Object} Server instance and configuration
 */
function createMCPServer(options = {}) {
  // Create logger early
  const logger = createMCPLogger("safeoutputs");

  logger.debug(`=== Creating MCP Server ===`);

  // Bootstrap: load configuration and tools using shared logic
  const { config: safeOutputsConfig, outputFile, tools: ALL_TOOLS } = bootstrapSafeOutputsServer(logger);

  // Create server with configuration
  const serverName = "safeoutputs";
  const version = "1.0.0";

  logger.debug(`Server name: ${serverName}`);
  logger.debug(`Server version: ${version}`);
  logger.debug(`Output file: ${outputFile}`);
  logger.debug(`Config: ${JSON.stringify(safeOutputsConfig)}`);

  // Create MCP Server instance
  const server = new MCPServer(
    {
      name: serverName,
      version: version,
    },
    {
      capabilities: {
        tools: {},
      },
    }
  );

  // Create append function
  const appendSafeOutput = createAppendFunction(outputFile);

  // Create handlers with configuration
  const handlers = createHandlers(logger, appendSafeOutput, safeOutputsConfig);
  const { defaultHandler } = handlers;

  // Attach handlers to tools
  const toolsWithHandlers = attachHandlers(ALL_TOOLS, handlers);

  // Register predefined tools that are enabled in configuration
  logger.debug(`Registering predefined tools...`);
  let registeredCount = 0;

  // Track which tools are enabled based on configuration
  const enabledTools = new Set();
  for (const [toolName, enabled] of Object.entries(safeOutputsConfig)) {
    if (enabled) {
      enabledTools.add(toolName);
    }
  }

  // Register predefined tools
  for (const tool of toolsWithHandlers) {
    // Check if this is a dispatch_workflow tool (has _workflow_name metadata)
    // These tools are dynamically generated with workflow-specific names
    // The _workflow_name should be a non-empty string
    const isDispatchWorkflowTool = tool._workflow_name && typeof tool._workflow_name === "string" && tool._workflow_name.length > 0;

    if (isDispatchWorkflowTool) {
      logger.debug(`Found dispatch_workflow tool: ${tool.name} (_workflow_name: ${tool._workflow_name})`);
      if (!safeOutputsConfig.dispatch_workflow) {
        logger.debug(`  WARNING: dispatch_workflow config is missing or falsy - tool will NOT be registered`);
        logger.debug(`  Config keys: ${Object.keys(safeOutputsConfig).join(", ")}`);
        logger.debug(`  config.dispatch_workflow value: ${JSON.stringify(safeOutputsConfig.dispatch_workflow)}`);
        continue;
      }
      logger.debug(`  dispatch_workflow config exists, registering tool`);
    } else {
      // Check if regular tool is enabled in configuration
      if (!enabledTools.has(tool.name)) {
        // Log tool metadata to help diagnose registration issues
        const toolMeta = tool._workflow_name !== undefined ? ` (_workflow_name: ${JSON.stringify(tool._workflow_name)})` : "";
        logger.debug(`Skipping tool ${tool.name}${toolMeta} - not enabled in config (tool has ${Object.keys(tool).length} properties: ${Object.keys(tool).join(", ")})`);
        continue;
      }
    }

    logger.debug(`Registering tool: ${tool.name}`);

    // Use tool-specific handler if available, otherwise use defaultHandler with tool name
    const toolHandler = tool.handler || defaultHandler(tool.name);

    // Register the tool with the MCP SDK using the high-level API
    server.tool(tool.name, tool.description || "", tool.inputSchema || { type: "object", properties: {} }, async args => {
      logger.debug(`Calling handler for tool: ${tool.name}`);

      // Call the handler
      const result = await Promise.resolve(toolHandler(args));
      logger.debug(`Handler returned for tool: ${tool.name}`);

      // Normalize result to MCP format
      const content = result && result.content ? result.content : [];
      return { content, isError: false };
    });

    registeredCount++;
  }

  // Register dynamic tools (safe-jobs)
  logger.debug(`Registering dynamic tools...`);
  const dynamicTools = [];
  if (safeOutputsConfig["safe-jobs"]) {
    // Get list of jobs from config
    const safeJobs = safeOutputsConfig["safe-jobs"];
    for (const jobName of Object.keys(safeJobs)) {
      const toolName = `safe-job-${jobName}`;
      const description = `Execute the ${jobName} job and collect safe outputs`;
      const inputSchema = {
        type: "object",
        properties: {
          input: {
            type: "string",
            description: "Input data for the job (JSON string)",
          },
        },
        required: [],
      };

      logger.debug(`Registering dynamic tool: ${toolName}`);

      server.tool(toolName, description, inputSchema, async args => {
        logger.debug(`Calling handler for dynamic tool: ${toolName}`);

        // Use the default handler for safe-jobs
        const result = await Promise.resolve(defaultHandler({ toolName, ...args }));
        logger.debug(`Handler returned for dynamic tool: ${toolName}`);

        // Normalize result to MCP format
        const content = result && result.content ? result.content : [];
        return { content, isError: false };
      });

      registeredCount++;
      dynamicTools.push(toolName);
    }
  }

  logger.debug(`Tool registration complete: ${registeredCount} registered`);
  logger.debug(`=== MCP Server Creation Complete ===`);

  // Note: We do NOT cleanup the config file here because it's needed by the ingestion
  // phase (collect_ndjson_output.cjs) that runs after the MCP server completes.
  // The config file only contains schema information (no secrets), so it's safe to leave.

  return { server, config: safeOutputsConfig, logger };
}

/**
 * Start the HTTP server with MCP protocol support
 * @param {Object} options - Server options
 * @param {number} [options.port] - Port to listen on (default: 3000)
 * @param {boolean} [options.stateless] - Run in stateless mode (default: false)
 * @param {string} [options.logDir] - Override log directory from config
 */
async function startHttpServer(options = {}) {
  const port = options.port || 3000;
  const stateless = options.stateless || false;

  const logger = createMCPLogger("safe-outputs-startup");

  logger.debug(`startHttpServer called with port=${port}, stateless=${stateless}`);
  logger.debug(`=== Starting Safe Outputs MCP HTTP Server ===`);
  logger.debug(`Port: ${port}`);
  logger.debug(`Mode: ${stateless ? "stateless" : "stateful"}`);
  logger.debug(`Environment: NODE_VERSION=${process.version}, PLATFORM=${process.platform}`);

  // Create the MCP server
  try {
    logger.debug(`About to call createMCPServer...`);
    const { server, config, logger: mcpLogger } = createMCPServer({ logDir: options.logDir });

    // Use the MCP logger for subsequent messages
    Object.assign(logger, mcpLogger);

    logger.debug(`MCP server created successfully`);
    logger.debug(`Server name: safeoutputs`);
    logger.debug(`Server version: 1.0.0`);
    logger.debug(`Tools configured: ${Object.keys(config).filter(k => config[k]).length}`);

    logger.debug(`Creating HTTP transport...`);
    // Create the HTTP transport
    const transport = new MCPHTTPTransport({
      sessionIdGenerator: stateless ? undefined : () => randomUUID(),
      enableJsonResponse: true,
      enableDnsRebindingProtection: false, // Disable for local development
    });
    logger.debug(`HTTP transport created`);

    // Connect server to transport
    logger.debug(`Connecting server to transport...`);
    logger.debug(`About to call server.connect(transport)...`);
    await server.connect(transport);
    logger.debug(`server.connect(transport) completed successfully`);
    logger.debug(`Server connected to transport successfully`);

    // Create HTTP server
    logger.debug(`Creating HTTP server...`);
    const httpServer = http.createServer(async (req, res) => {
      // Set CORS headers for development
      res.setHeader("Access-Control-Allow-Origin", "*");
      res.setHeader("Access-Control-Allow-Methods", "GET, POST, OPTIONS");
      res.setHeader("Access-Control-Allow-Headers", "Content-Type, Accept");

      // Handle OPTIONS preflight
      if (req.method === "OPTIONS") {
        res.writeHead(200);
        res.end();
        return;
      }

      // Handle GET /health endpoint for health checks
      if (req.method === "GET" && req.url === "/health") {
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(
          JSON.stringify({
            status: "ok",
            server: "safeoutputs",
            version: "1.0.0",
            tools: Object.keys(config).filter(k => config[k]).length,
          })
        );
        return;
      }

      // Only handle POST requests for MCP protocol
      if (req.method !== "POST") {
        res.writeHead(405, { "Content-Type": "application/json" });
        res.end(JSON.stringify({ error: "Method not allowed" }));
        return;
      }

      try {
        // Parse request body for POST requests
        let body = null;
        if (req.method === "POST") {
          const chunks = [];
          for await (const chunk of req) {
            chunks.push(chunk);
          }
          const bodyStr = Buffer.concat(chunks).toString();
          try {
            body = bodyStr ? JSON.parse(bodyStr) : null;
          } catch (parseError) {
            res.writeHead(400, { "Content-Type": "application/json" });
            res.end(
              JSON.stringify({
                jsonrpc: "2.0",
                error: {
                  code: -32700,
                  message: "Parse error: Invalid JSON in request body",
                },
                id: null,
              })
            );
            return;
          }
        }

        // Let the transport handle the request
        await transport.handleRequest(req, res, body);
      } catch (error) {
        // Log the full error with stack trace on the server for debugging
        logger.debugError("Error handling request: ", error);

        if (!res.headersSent) {
          res.writeHead(500, { "Content-Type": "application/json" });
          res.end(
            JSON.stringify({
              jsonrpc: "2.0",
              error: {
                code: -32603,
                message: "Internal server error",
              },
              id: null,
            })
          );
        }
      }
    });

    // Start listening
    logger.debug(`Attempting to bind to port ${port}...`);
    httpServer.listen(port, () => {
      logger.debug(`=== Safe Outputs MCP HTTP Server Started Successfully ===`);
      logger.debug(`HTTP server listening on http://localhost:${port}`);
      logger.debug(`MCP endpoint: POST http://localhost:${port}/`);
      logger.debug(`Server name: safeoutputs`);
      logger.debug(`Server version: 1.0.0`);
      logger.debug(`Tools available: ${Object.keys(config).filter(k => config[k]).length}`);
      logger.debug(`Server is ready to accept requests`);
    });

    // Handle bind errors
    httpServer.on("error", error => {
      /** @type {NodeJS.ErrnoException} */
      const errnoError = error;
      if (errnoError.code === "EADDRINUSE") {
        logger.debugError(`ERROR: Port ${port} is already in use. `, error);
      } else if (errnoError.code === "EACCES") {
        logger.debugError(`ERROR: Permission denied to bind to port ${port}. `, error);
      } else {
        logger.debugError(`ERROR: Failed to start HTTP server: `, error);
      }
      process.exit(1);
    });

    // Handle shutdown gracefully
    process.on("SIGINT", () => {
      logger.debug("Received SIGINT, shutting down...");
      httpServer.close(() => {
        logger.debug("HTTP server closed");
        process.exit(0);
      });
    });

    process.on("SIGTERM", () => {
      logger.debug("Received SIGTERM, shutting down...");
      httpServer.close(() => {
        logger.debug("HTTP server closed");
        process.exit(0);
      });
    });

    return httpServer;
  } catch (error) {
    // Log detailed error information for startup failures
    const errorLogger = createLogger("safe-outputs-startup-error");
    errorLogger.debug(`=== FATAL ERROR: Failed to start Safe Outputs MCP HTTP Server ===`);
    if (error && typeof error === "object") {
      if ("constructor" in error && error.constructor) {
        errorLogger.debug(`Error type: ${error.constructor.name}`);
      }
      if ("message" in error) {
        errorLogger.debug(`Error message: ${error.message}`);
      }
      if ("stack" in error && error.stack) {
        errorLogger.debug(`Stack trace:\n${error.stack}`);
      }
      if ("code" in error && error.code) {
        errorLogger.debug(`Error code: ${error.code}`);
      }
    }
    errorLogger.debug(`Port: ${port}`);

    // Re-throw the error to be caught by the caller
    throw error;
  }
}

// If run directly, start the HTTP server with command-line arguments
if (require.main === module) {
  const args = process.argv.slice(2);

  const options = {
    port: 3000,
    stateless: false,
    /** @type {string | undefined} */
    logDir: undefined,
  };

  // Parse optional arguments
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--port" && args[i + 1]) {
      options.port = parseInt(args[i + 1], 10);
      i++;
    } else if (args[i] === "--stateless") {
      options.stateless = true;
    } else if (args[i] === "--log-dir" && args[i + 1]) {
      options.logDir = args[i + 1];
      i++;
    }
  }

  startHttpServer(options).catch(error => {
    console.error(`Error starting HTTP server: ${getErrorMessage(error)}`);
    process.exit(1);
  });
}

module.exports = {
  startHttpServer,
  createMCPServer,
};
