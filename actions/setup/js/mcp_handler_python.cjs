// @ts-check

/**
 * Python Script Handler for MCP Scripts
 *
 * This module provides a handler for executing Python scripts in mcp-scripts tools.
 * It uses a Pythonic approach for passing inputs via JSON on stdin.
 */

const { executeProcess } = require("./mcp_handler_process.cjs");

/**
 * Create a Python script handler function that executes a .py file.
 * Inputs are passed as JSON via stdin for a more Pythonic approach:
 * - Inputs are passed as JSON object via stdin (similar to JavaScript tools)
 * - Python script reads and parses JSON from stdin into 'inputs' dictionary
 * - Outputs are read from stdout (JSON format expected)
 *
 * @param {Object} server - The MCP server instance for logging
 * @param {string} toolName - Name of the tool for logging purposes
 * @param {string} scriptPath - Path to the Python script to execute
 * @param {number} [timeoutSeconds=60] - Timeout in seconds for script execution
 * @param {NodeJS.ProcessEnv} [env=process.env] - Environment variables for this tool only
 * @returns {Function} Async handler function that executes the Python script
 */
function createPythonHandler(server, toolName, scriptPath, timeoutSeconds = 60, env = process.env) {
  return async args => {
    server.debug(`  [${toolName}] Invoking Python handler: ${scriptPath}`);
    server.debug(`  [${toolName}] Python handler args: ${JSON.stringify(args)}`);
    server.debug(`  [${toolName}] Timeout: ${timeoutSeconds}s`);

    // Pass inputs as JSON via stdin (more Pythonic approach)
    const inputJson = JSON.stringify(args || {});
    server.debug(`  [${toolName}] Input JSON (${inputJson.length} bytes): ${inputJson.substring(0, 200)}${inputJson.length > 200 ? "..." : ""}`);

    return executeProcess({
      server,
      toolName,
      languageLabel: "Python",
      command: "python3",
      args: [scriptPath],
      env,
      inputJson,
      timeoutSeconds,
      scriptPath,
    });
  };
}

module.exports = {
  createPythonHandler,
};
