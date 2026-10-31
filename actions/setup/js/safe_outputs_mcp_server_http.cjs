// @ts-check
/// <reference types="@actions/github-script" />

// Load the shim before any other module so that global.core and global.context
// are available even when this module is started directly (i.e. not through the
// safe-outputs-mcp-server.cjs entry point).  The shim is a no-op when those
// globals are already provided by the github-script environment.
require("./shim.cjs");

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
 * The server runs in stateless mode (no session management) because the MCP
 * gateway does not perform the MCP protocol initialization handshake and
 * directly calls methods like tools/list without an Mcp-Session-Id header.
 *
 * Usage:
 *   node safe_outputs_mcp_server_http.cjs [--port 3000]
 *   Requires GH_AW_SAFE_OUTPUTS_API_KEY; POST requests send it in Authorization.
 *   Uploads are limited to 1 MiB and 30 seconds; GET /health is unauthenticated.
 *
 * Options:
 *   --port <number>    Port to listen on (default: 3000)
 *   --log-dir <path>   Directory for log files
 */

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
const { normalizeTool } = require("./mcp_server_core.cjs");
const { attachHandlers, registerPredefinedTools, registerDynamicTools } = require("./safe_outputs_tools_loader.cjs");
moduleLogger.debug("Loaded safe_outputs_tools_loader.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { runHttpServer, logStartupError, requireHttpApiKey } = require("./mcp_http_server_runner.cjs");
const { normalizeSafeOutputToolArguments, stripInternalSafeOutputSchemaMetadata } = require("./safe_outputs_mcp_arguments.cjs");
moduleLogger.debug("All modules loaded successfully");

/**
 * Normalize a handler result into the MCP tool-call response shape, preserving
 * the handler-provided `isError` flag (e.g. safe-output handlers return
 * `isError: true` on error). Hardcoding `isError: false` here would mask
 * application-level errors from clients such as the samples replay driver.
 * @param {any} result - Raw result returned by a tool handler.
 * @returns {{ content: any[], isError: boolean }}
 */
function normalizeMcpToolResult(result) {
  const content = result && result.content ? result.content : [];
  const isError = !!(result && result.isError);
  return { content, isError };
}

/**
 * Check whether workflow metadata name is a non-empty string after trimming.
 * @param {any} workflowName
 * @returns {boolean}
 */
function hasValidWorkflowMetadataName(workflowName) {
  return typeof workflowName === "string" && workflowName.trim().length > 0;
}

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
  const normalizationSchemas = new Map();

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
      normalizeArguments: (toolName, args, tool) => normalizeSafeOutputToolArguments(toolName, args, logger, normalizationSchemas.get(normalizeTool(toolName)) || tool?.inputSchema),
    }
  );

  // Create append function
  const appendSafeOutput = createAppendFunction(outputFile);

  // Create handlers with configuration
  const handlers = createHandlers(logger, appendSafeOutput, safeOutputsConfig);
  const { defaultHandler } = handlers;

  // Attach handlers to tools
  const toolsWithHandlers = attachHandlers(ALL_TOOLS, handlers, logger);
  for (const tool of toolsWithHandlers) {
    if (tool?.name && tool?.inputSchema) {
      normalizationSchemas.set(normalizeTool(tool.name), tool.inputSchema);
      tool.inputSchema = stripInternalSafeOutputSchemaMetadata(tool.inputSchema);
    }
  }

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
    const isDispatchWorkflowTool = hasValidWorkflowMetadataName(tool._workflow_name);

    // Check if this is a dispatch_repository tool (has _dispatch_repository_tool metadata)
    // These tools are dynamically generated with tool-specific names
    const isDispatchRepositoryTool = tool._dispatch_repository_tool && typeof tool._dispatch_repository_tool === "string" && tool._dispatch_repository_tool.length > 0;

    // Check if this is a call_workflow tool (has _call_workflow_name metadata)
    // These tools are dynamically generated with workflow-specific names
    // The _call_workflow_name should be a non-empty string
    const isCallWorkflowTool = hasValidWorkflowMetadataName(tool._call_workflow_name);

    if (isDispatchWorkflowTool) {
      logger.debug(`Found dispatch_workflow tool: ${tool.name} (_workflow_name: ${tool._workflow_name})`);
      if (!safeOutputsConfig.dispatch_workflow) {
        logger.debug(`  WARNING: dispatch_workflow config is missing or falsy - tool will NOT be registered`);
        logger.debug(`  Config keys: ${Object.keys(safeOutputsConfig).join(", ")}`);
        logger.debug(`  config.dispatch_workflow value: ${JSON.stringify(safeOutputsConfig.dispatch_workflow)}`);
        continue;
      }
      logger.debug(`  dispatch_workflow config exists, registering tool`);
    } else if (isDispatchRepositoryTool) {
      logger.debug(`Found dispatch_repository tool: ${tool.name} (_dispatch_repository_tool: ${tool._dispatch_repository_tool})`);
      if (!safeOutputsConfig.dispatch_repository) {
        logger.debug(`  WARNING: dispatch_repository config is missing or falsy - tool will NOT be registered`);
        logger.debug(`  Config keys: ${Object.keys(safeOutputsConfig).join(", ")}`);
        logger.debug(`  config.dispatch_repository value: ${JSON.stringify(safeOutputsConfig.dispatch_repository)}`);
        continue;
      }
      logger.debug(`  dispatch_repository config exists, registering tool`);
    } else if (isCallWorkflowTool) {
      logger.debug(`Found call_workflow tool: ${tool.name} (_call_workflow_name: ${tool._call_workflow_name})`);
      if (!safeOutputsConfig.call_workflow) {
        logger.debug(`  WARNING: call_workflow config is missing or falsy - tool will NOT be registered`);
        logger.debug(`  Config keys: ${Object.keys(safeOutputsConfig).join(", ")}`);
        logger.debug(`  config.call_workflow value: ${JSON.stringify(safeOutputsConfig.call_workflow)}`);
        continue;
      }
      logger.debug(`  call_workflow config exists, registering tool`);
    } else {
      // Check if regular tool is enabled in configuration
      if (!enabledTools.has(tool.name)) {
        // Log tool metadata to help diagnose registration issues
        let toolMeta = "";
        if (tool._workflow_name !== undefined) {
          toolMeta = ` (_workflow_name: ${JSON.stringify(tool._workflow_name)})`;
        } else if (tool._dispatch_repository_tool !== undefined) {
          toolMeta = ` (_dispatch_repository_tool: ${JSON.stringify(tool._dispatch_repository_tool)})`;
        } else if (tool._call_workflow_name !== undefined) {
          toolMeta = ` (_call_workflow_name: ${JSON.stringify(tool._call_workflow_name)})`;
        }
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

      // Normalize result to MCP format; preserve isError from the handler result
      return normalizeMcpToolResult(result);
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

        // Normalize result to MCP format; preserve isError from the handler result
        return normalizeMcpToolResult(result);
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
 * @param {string} [options.apiKey] - API key (defaults to GH_AW_SAFE_OUTPUTS_API_KEY)
 * @param {string} [options.logDir] - Override log directory from config
 */
async function startHttpServer(options = {}) {
  const apiKey = requireHttpApiKey(options.apiKey ?? process.env.GH_AW_SAFE_OUTPUTS_API_KEY, "GH_AW_SAFE_OUTPUTS_API_KEY");
  const port = options.port ?? 3000;

  const logger = createMCPLogger("safe-outputs-startup");

  logger.debug(`startHttpServer called with port=${port}`);
  logger.debug(`=== Starting Safe Outputs MCP HTTP Server ===`);
  logger.debug(`Port: ${port}`);
  logger.debug(`Mode: stateless`);
  logger.debug(`Environment: NODE_VERSION=${process.version}, PLATFORM=${process.platform}`);

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
    // Create the HTTP transport in stateless mode (no session management)
    const transport = new MCPHTTPTransport({
      sessionIdGenerator: undefined,
      enableJsonResponse: true,
      enableDnsRebindingProtection: false, // Disable for local development
    });
    logger.debug(`HTTP transport created`);

    logger.debug(`Connecting server to transport...`);
    logger.debug(`About to call server.connect(transport)...`);
    await server.connect(transport);
    logger.debug(`server.connect(transport) completed successfully`);
    logger.debug(`Server connected to transport successfully`);

    logger.debug(`Creating HTTP server...`);
    return await runHttpServer({
      transport,
      port,
      apiKey,
      getHealthPayload: () => ({
        status: "ok",
        server: "safeoutputs",
        version: "1.0.0",
        tools: Object.keys(config).filter(k => config[k]).length,
      }),
      logger,
      serverLabel: "Safe Outputs MCP",
      configureServer: httpServer => {
        // Preserve long tool calls and idle gateway connections; the runner bounds uploads.
        httpServer.timeout = 0;
        httpServer.keepAliveTimeout = 0;
      },
    });
  } catch (error) {
    logStartupError(error, "safe-outputs-startup-error", { Port: port });
    return undefined;
  }
}

// If run directly, start the HTTP server with command-line arguments
if (require.main === module) {
  const args = process.argv.slice(2);

  const options = {
    port: 3000,
    /** @type {string | undefined} */
    logDir: undefined,
  };

  // Parse optional arguments
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--port" && args[i + 1]) {
      options.port = parseInt(args[i + 1], 10);
      i++;
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
  normalizeMcpToolResult,
};
