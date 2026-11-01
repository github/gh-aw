// @ts-check

/**
 * Go Script Handler for MCP Scripts
 *
 * This module provides a handler for executing Go scripts in mcp-scripts tools.
 * It uses `go run` to execute Go source files with inputs via JSON on stdin.
 */

const { executeProcess } = require("./mcp_handler_process.cjs");

/**
 * Create a Go script handler function that executes a .go file using `go run`.
 * Inputs are passed as JSON via stdin:
 * - Inputs are passed as JSON object via stdin (similar to Python tools)
 * - Go script reads and parses JSON from stdin into inputs map
 * - Outputs are read from stdout (JSON format expected)
 *
 * @param {Object} server - The MCP server instance for logging
 * @param {string} toolName - Name of the tool for logging purposes
 * @param {string} scriptPath - Path to the Go script to execute
 * @param {number} [timeoutSeconds=60] - Timeout in seconds for script execution
 * @param {NodeJS.ProcessEnv} [env=process.env] - Environment variables for this tool only
 * @returns {Function} Async handler function that executes the Go script
 */
function createGoHandler(server, toolName, scriptPath, timeoutSeconds = 60, env = process.env) {
  return async args => {
    server.debug(`  [${toolName}] Invoking Go handler: ${scriptPath}`);
    server.debug(`  [${toolName}] Go handler args: ${JSON.stringify(args)}`);
    server.debug(`  [${toolName}] Timeout: ${timeoutSeconds}s`);

    const inputJson = JSON.stringify(args || {});
    server.debug(`  [${toolName}] Input JSON (${inputJson.length} bytes): ${inputJson.substring(0, 200)}${inputJson.length > 200 ? "..." : ""}`);

    return executeProcess({
      server,
      toolName,
      languageLabel: "Go",
      command: "go",
      args: ["run", scriptPath],
      env,
      inputJson,
      timeoutSeconds,
      scriptPath,
    });
  };
}

module.exports = {
  createGoHandler,
};
