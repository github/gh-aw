// @ts-check

/**
 * JavaScript Handler for MCP Scripts
 *
 * This module provides a handler for executing JavaScript (.cjs) files in mcp-scripts tools.
 * It executes JavaScript handlers in a separate Node.js process for isolation.
 */

const { executeProcess, createToolEnvironment } = require("./mcp_handler_process.cjs");

/**
 * Create a JavaScript handler function that executes a .cjs file in a separate Node.js process.
 * Inputs are passed as JSON via stdin:
 * - Inputs are passed as JSON object via stdin
 * - JavaScript script reads and parses JSON from stdin
 * - Outputs are read from stdout (JSON format expected)
 *
 * @param {Object} server - The MCP server instance for logging
 * @param {string} toolName - Name of the tool for logging purposes
 * @param {string} scriptPath - Path to the JavaScript script to execute
 * @param {number} [timeoutSeconds=60] - Timeout in seconds for script execution
 * @param {Record<string, string>} [envBindings] - Tool-local environment references
 * @returns {Function} Async handler function that executes the JavaScript script
 */
function createJavaScriptHandler(server, toolName, scriptPath, timeoutSeconds = 60, envBindings = {}) {
  return async args => {
    server.debug(`  [${toolName}] Invoking JavaScript handler: ${scriptPath}`);
    server.debug(`  [${toolName}] JavaScript handler args: ${JSON.stringify(args)}`);
    server.debug(`  [${toolName}] Timeout: ${timeoutSeconds}s`);

    // Pass inputs as JSON via stdin
    const inputJson = JSON.stringify(args || {});
    server.debug(`  [${toolName}] Input JSON (${inputJson.length} bytes): ${inputJson.substring(0, 200)}${inputJson.length > 200 ? "..." : ""}`);

    return executeProcess({
      server,
      toolName,
      languageLabel: "JavaScript",
      command: process.execPath, // Use the same Node.js binary as the current process
      args: [scriptPath],
      env: createToolEnvironment(envBindings),
      inputJson,
      timeoutSeconds,
      scriptPath,
    });
  };
}

module.exports = {
  createJavaScriptHandler,
};
