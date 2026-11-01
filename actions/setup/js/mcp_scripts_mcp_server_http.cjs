// @ts-check
/// <reference types="@actions/github-script" />

/**
 * MCP Scripts Server with HTTP Transport
 *
 * This module extends the mcp-scripts MCP server to support HTTP transport
 * using the StreamableHTTPServerTransport from the MCP SDK.
 *
 * It provides both stateful and stateless HTTP modes, as well as SSE streaming.
 *
 * Usage:
 *   node mcp_scripts_mcp_server_http.cjs /path/to/tools.json [--port 3000] [--stateless]
 *   Requires GH_AW_MCP_SCRIPTS_API_KEY; POST requests send it in Authorization.
 *   Uploads are limited to 1 MiB and 30 seconds; GET /health is unauthenticated.
 *
 * Options:
 *   --port <number>    Port to listen on (default: 3000)
 *   --stateless        Run in stateless mode (no session management)
 *   --log-dir <path>   Directory for log files
 */

// Load core shim before any other modules so that global.core is available
// for modules that rely on it.
require("./shim.cjs");

const { randomUUID } = require("crypto");
const { MCPServer, MCPHTTPTransport } = require("./mcp_http_transport.cjs");
const { validateRequiredFields, validateStringInputLengths, buildStringLengthValidationError } = require("./schema-validation.cjs");
const { generateEnhancedErrorMessage } = require("./mcp_enhanced_errors.cjs");
const { createLogger } = require("./mcp_logger.cjs");
const { bootstrapMCPScriptsServer, cleanupConfigFile } = require("./mcp_scripts_bootstrap.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { redactMCPScriptLogMessage } = require("./diagnostic_sanitization.cjs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { runHttpServer, logStartupError, requireHttpApiKey } = require("./mcp_http_server_runner.cjs");

/**
 * Create and configure the MCP server with tools
 * @param {string} configPath - Path to the configuration JSON file
 * @param {Object} [options] - Additional options
 * @param {string} [options.logDir] - Override log directory from config
 * @returns {Object} Server instance and configuration
 */
function createMCPServer(configPath, options = {}) {
  // Create logger early
  const logger = createLogger("mcpscripts", redactMCPScriptLogMessage);

  logger.debug(`=== Creating MCP Server ===`);
  logger.debug(`Configuration file: ${configPath}`);

  // Bootstrap: load configuration and tools using shared logic
  const { config, tools } = bootstrapMCPScriptsServer(configPath, logger);

  // Create server with configuration
  const serverName = config.serverName || "mcpscripts";
  const version = config.version || "1.0.0";

  logger.debug(`Server name: ${serverName}`);
  logger.debug(`Server version: ${version}`);

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
      redactLogMessage: redactMCPScriptLogMessage,
    }
  );

  // Register all tools with the MCP SDK server using the tool() method
  logger.debug(`Registering tools with MCP server...`);
  let registeredCount = 0;
  let skippedCount = 0;

  for (const tool of tools) {
    if (!tool.handler) {
      logger.debug(`Skipping tool ${tool.name} - no handler loaded`);
      skippedCount++;
      continue;
    }

    logger.debug(`Registering tool: ${tool.name}`);

    // Register the tool with the MCP SDK using the high-level API
    // The callback receives the arguments directly as the first parameter
    server.tool(tool.name, tool.description || "", tool.inputSchema || { type: "object", properties: {} }, async args => {
      logger.debug(`Calling handler for tool: ${tool.name}`);

      // Validate required fields using helper
      const missing = validateRequiredFields(args, tool.inputSchema);
      if (missing.length) {
        throw new Error(`${ERR_VALIDATION}: ${generateEnhancedErrorMessage(missing, tool.name, tool.inputSchema)}`);
      }

      // SM-IS-01: Validate per-string input length limits (default 10 KB, or explicit schema maxLength when set).
      const oversized = validateStringInputLengths(args, tool.inputSchema);
      if (oversized.length) {
        throw new Error(`${ERR_VALIDATION}: ${buildStringLengthValidationError(tool.name, oversized)}`);
      }

      // Call the handler
      const result = await Promise.resolve(tool.handler(args));
      logger.debug(`Handler returned for tool: ${tool.name}`);

      // Normalize result to MCP format
      const content = result && result.content ? result.content : [];
      return { content, isError: false };
    });

    registeredCount++;
  }

  logger.debug(`Tool registration complete: ${registeredCount} registered, ${skippedCount} skipped`);
  logger.debug(`=== MCP Server Creation Complete ===`);

  // Cleanup: delete the configuration file after loading
  cleanupConfigFile(configPath, logger);

  return { server, config, logger };
}

/**
 * Start the HTTP server with MCP protocol support
 * @param {string} configPath - Path to the configuration JSON file
 * @param {Object} options - Server options
 * @param {number} [options.port] - Port to listen on (default: 3000)
 * @param {boolean} [options.stateless] - Run in stateless mode (default: false)
 * @param {string} [options.apiKey] - API key (defaults to GH_AW_MCP_SCRIPTS_API_KEY)
 * @param {string} [options.logDir] - Override log directory from config
 */
async function startHttpServer(configPath, options = {}) {
  const apiKey = requireHttpApiKey(options.apiKey ?? process.env.GH_AW_MCP_SCRIPTS_API_KEY, "GH_AW_MCP_SCRIPTS_API_KEY");
  const port = options.port ?? 3000;
  const stateless = options.stateless || false;

  const logger = createLogger("mcp-scripts-startup");

  logger.debug(`=== Starting MCP Scripts HTTP Server ===`);
  logger.debug(`Configuration file: ${configPath}`);
  logger.debug(`Port: ${port}`);
  logger.debug(`Mode: ${stateless ? "stateless" : "stateful"}`);
  logger.debug(`Environment: NODE_VERSION=${process.version}, PLATFORM=${process.platform}`);

  try {
    const { server, config, logger: mcpLogger } = createMCPServer(configPath, { logDir: options.logDir });

    // Use the MCP logger for subsequent messages
    Object.assign(logger, mcpLogger);

    logger.debug(`MCP server created successfully`);
    logger.debug(`Server name: ${config.serverName || "mcpscripts"}`);
    logger.debug(`Server version: ${config.version || "1.0.0"}`);
    logger.debug(`Tools configured: ${config.tools.length}`);

    logger.debug(`Creating HTTP transport...`);
    const transport = new MCPHTTPTransport({
      sessionIdGenerator: stateless ? undefined : () => randomUUID(),
      enableJsonResponse: true,
      enableDnsRebindingProtection: false, // Disable for local development
      redactLogMessage: redactMCPScriptLogMessage,
    });
    logger.debug(`HTTP transport created`);

    logger.debug(`Connecting server to transport...`);
    await server.connect(transport);
    logger.debug(`Server connected to transport successfully`);

    logger.debug(`Creating HTTP server...`);
    return await runHttpServer({
      transport,
      port,
      apiKey,
      getHealthPayload: () => ({
        status: "ok",
        server: config.serverName || "mcpscripts",
        version: config.version || "1.0.0",
        tools: config.tools.length,
      }),
      logger,
      serverLabel: "MCP Scripts",
    });
  } catch (error) {
    logStartupError(error, "mcp-scripts-startup-error", {
      "Configuration file": configPath,
      Port: port,
    });
    return undefined;
  }
}

// If run directly, start the HTTP server with command-line arguments
if (require.main === module) {
  const args = process.argv.slice(2);

  if (args.length < 1) {
    console.error("Usage: node mcp_scripts_mcp_server_http.cjs <config.json> [--port <number>] [--stateless] [--log-dir <path>]");
    process.exit(1);
  }

  const configPath = args[0];
  const options = {
    port: 3000,
    stateless: false,
    /** @type {string | undefined} */
    logDir: undefined,
  };

  // Parse optional arguments
  for (let i = 1; i < args.length; i++) {
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

  startHttpServer(configPath, options).catch(error => {
    console.error(`Error starting HTTP server: ${getErrorMessage(error)}`);
    process.exit(1);
  });
}

module.exports = {
  startHttpServer,
  createMCPServer,
};
