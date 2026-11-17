#!/usr/bin/env node
// @ts-check

// Safe-outputs MCP Server Entry Point
// This is the main entry point script for the safe-outputs MCP server
// It starts the HTTP server on the configured port

// Load core shim before any other modules so that global.core is available
// for modules that rely on it (e.g. generate_git_patch.cjs).
require("./shim.cjs");

const { createLogger } = require("./mcp_logger.cjs");
const { ERR_CONFIG } = require("./error_codes.cjs");
const logger = createLogger("safe-outputs-entry");

// Log immediately to verify Node.js execution starts
logger.debug("Entry point script executing");

const { startHttpServer } = require("./safe_outputs_mcp_server_http.cjs");

logger.debug("Successfully required safe_outputs_mcp_server_http.cjs");

// If run directly, start the HTTP server
// The server reads configuration from ${RUNNER_TEMP}/gh-aw/safeoutputs/config.json
// Port and API key are configured via environment variables:
// - GH_AW_SAFE_OUTPUTS_PORT
// - GH_AW_SAFE_OUTPUTS_API_KEY
// Log directory is configured via GH_AW_MCP_LOG_DIR environment variable
if (require.main === module) {
  logger.debug("In require.main === module block");
  const port = require("./runtime_paths.cjs").runtimePort("GH_AW_SAFE_OUTPUTS_PORT", 3001);
  if (!Number.isFinite(port) || !Number.isSafeInteger(port) || port <= 0 || port > 65535) {
    throw new Error(`${ERR_CONFIG}: GH_AW_SAFE_OUTPUTS_PORT must be a valid port number`);
  }
  const logDir = process.env.GH_AW_MCP_LOG_DIR;
  logger.debug(`Port: ${port}, LogDir: ${logDir}`);
  logger.debug("Calling startHttpServer...");

  startHttpServer({ port, logDir }).catch(error => {
    logger.debugError("Failed to start safe-outputs HTTP server: ", error);
    process.exit(1);
  });

  logger.debug("startHttpServer call initiated (async)");
}

module.exports = { startHttpServer };
