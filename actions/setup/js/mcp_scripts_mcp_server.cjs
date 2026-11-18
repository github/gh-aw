// @ts-check
/// <reference types="@actions/github-script" />

/**
 * MCP Scripts Server Module
 *
 * This module provides a reusable MCP server for mcp-scripts configuration.
 * It uses the mcp_server_core module for JSON-RPC handling and tool registration.
 *
 * The server reads tool configuration from a JSON file and loads handlers from
 * JavaScript (.cjs), shell script (.sh), or Python script (.py) files.
 *
 * Usage:
 *   node mcp_scripts_mcp_server.cjs /path/to/tools.json
 *
 * Or as a module:
 *   const { startMCPScriptsServer } = require("./mcp_scripts_mcp_server.cjs");
 *   startMCPScriptsServer("/path/to/tools.json");
 */

const { createServer, registerTool, start } = require("./mcp_server_core.cjs");
const { loadConfig } = require("./mcp_scripts_config_loader.cjs");
const { createToolConfig } = require("./mcp_scripts_tool_factory.cjs");
const { bootstrapMCPScriptsServer, cleanupConfigFile } = require("./mcp_scripts_bootstrap.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { redactMCPScriptLogMessage } = require("./diagnostic_sanitization.cjs");
const path = require("path");

function resolveConfigLogDir(logDir, setupRoot = process.env.GH_AW_SETUP_ROOT || "/tmp/gh-aw") {
  const setupRootPrefix = "${GH_AW_SETUP_ROOT}/";
  if (typeof logDir === "string" && logDir.startsWith(setupRootPrefix)) {
    return path.join(setupRoot, logDir.slice(setupRootPrefix.length));
  }
  return logDir;
}

/**
 * @typedef {Object} MCPScriptsToolConfig
 * @property {string} name - Tool name
 * @property {string} description - Tool description
 * @property {Object} inputSchema - JSON Schema for tool inputs
 * @property {string} [handler] - Path to handler file (.cjs, .sh, or .py)
 * @property {string[]} [dependencies] - Runtime dependencies installed before first invocation
 */

/**
 * @typedef {Object} MCPScriptsConfig
 * @property {string} [serverName] - Server name (defaults to "mcpscripts")
 * @property {string} [version] - Server version (defaults to "1.0.0")
 * @property {string} [logDir] - Log directory path
 * @property {MCPScriptsToolConfig[]} tools - Array of tool configurations
 */

/**
 * Start the mcp-scripts MCP server with the given configuration
 * @param {string} configPath - Path to the configuration JSON file
 * @param {Object} [options] - Additional options
 * @param {string} [options.logDir] - Override log directory from config
 * @param {boolean} [options.skipCleanup] - Skip deletion of config file (useful for stdio mode with agent restarts)
 */
function startMCPScriptsServer(configPath, options = {}) {
  // Create server first to have logger available
  const logDir = options.logDir || undefined;
  const server = createServer({ name: "mcpscripts", version: "1.0.0" }, { logDir, redactLogMessage: redactMCPScriptLogMessage });

  // Bootstrap: load configuration and tools using shared logic
  const { config, tools } = bootstrapMCPScriptsServer(configPath, server);

  // Update server info with actual config values
  server.serverInfo.name = config.serverName || "mcpscripts";
  server.serverInfo.version = config.version || "1.0.0";

  // Use logDir from config if not overridden by options
  const configLogDir = resolveConfigLogDir(config.logDir);
  if (!options.logDir && configLogDir) {
    server.logDir = configLogDir;
  }

  // Register all tools with the server
  for (const tool of tools) {
    registerTool(server, tool);
  }

  // Cleanup: delete the configuration file after loading (unless skipCleanup is true)
  if (!options.skipCleanup) {
    cleanupConfigFile(configPath, server);
  }

  // Start the server
  start(server);
}

// If run directly, start the server with command-line arguments
if (require.main === module) {
  const args = process.argv.slice(2);

  if (args.length < 1) {
    console.error("Usage: node mcp_scripts_mcp_server.cjs <config.json> [--log-dir <path>]");
    process.exit(1);
  }

  const configPath = args[0];
  const options = {};

  // Parse optional arguments
  for (let i = 1; i < args.length; i++) {
    if (args[i] === "--log-dir" && args[i + 1]) {
      options.logDir = args[i + 1];
      i++;
    }
  }

  try {
    startMCPScriptsServer(configPath, options);
  } catch (error) {
    console.error(`Error starting mcp-scripts server: ${getErrorMessage(error)}`);
    process.exit(1);
  }
}

module.exports = {
  startMCPScriptsServer,
  resolveConfigLogDir,
  // Re-export helpers for convenience
  loadConfig,
  createToolConfig,
};
