// @ts-check

/**
 * Shell Script Handler for MCP Scripts
 *
 * This module provides a handler for executing shell scripts in mcp-scripts tools.
 * It follows GitHub Actions conventions for passing inputs and reading outputs.
 */

const fs = require("fs");
const path = require("path");
const os = require("os");

const { executeProcess, createToolEnvironment } = require("./mcp_handler_process.cjs");

/**
 * Removes the specified temporary output file, suppressing any errors.
 *
 * @param {string} outputFile - Path to the file to remove
 */
function cleanupOutputFile(outputFile) {
  try {
    if (fs.existsSync(outputFile)) {
      fs.unlinkSync(outputFile);
    }
  } catch {
    // Ignore cleanup errors
  }
}

/**
 * Create a shell script handler function that executes a .sh file.
 * Uses GitHub Actions convention for passing inputs/outputs:
 * - Inputs are passed as environment variables prefixed with INPUT_ (uppercased, dashes replaced with underscores)
 * - Outputs are read from GITHUB_OUTPUT file (key=value format, one per line)
 * - Returns: { stdout, stderr, outputs }
 *
 * @param {Object} server - The MCP server instance for logging
 * @param {string} toolName - Name of the tool for logging purposes
 * @param {string} scriptPath - Path to the shell script to execute
 * @param {number} [timeoutSeconds=60] - Timeout in seconds for script execution
 * @param {Record<string, string>} [envBindings] - Tool-local environment references
 * @returns {Function} Async handler function that executes the shell script
 */
function createShellHandler(server, toolName, scriptPath, timeoutSeconds = 60, envBindings = {}) {
  return async args => {
    server.debug(`  [${toolName}] Invoking shell handler: ${scriptPath}`);
    server.debug(`  [${toolName}] Shell handler args: ${JSON.stringify(args)}`);
    server.debug(`  [${toolName}] Timeout: ${timeoutSeconds}s`);

    // Create environment variables from args (GitHub Actions convention: INPUT_NAME)
    const env = createToolEnvironment(envBindings);
    for (const [key, value] of Object.entries(args || {})) {
      const envKey = `INPUT_${key.toUpperCase().replace(/-/g, "_")}`;
      env[envKey] = String(value);
      server.debug(`  [${toolName}] Set env: ${envKey}=${String(value).substring(0, 100)}${String(value).length > 100 ? "..." : ""}`);
    }

    // Create a temporary file for outputs (GitHub Actions convention: GITHUB_OUTPUT)
    const outputFile = path.join(os.tmpdir(), `mcp-shell-output-${Date.now()}-${Math.random().toString(36).substring(2)}.txt`);
    env.GITHUB_OUTPUT = outputFile;
    server.debug(`  [${toolName}] Output file: ${outputFile}`);

    // Create the output file (empty)
    try {
      fs.writeFileSync(outputFile, "");
    } catch (err) {
      throw new Error(`Failed to write file ${outputFile}: ${String(err)}`, { cause: err });
    }

    return executeProcess({
      server,
      toolName,
      languageLabel: "Shell",
      command: scriptPath,
      args: [],
      env,
      inputJson: null, // Shell uses env vars instead of stdin
      timeoutSeconds,
      scriptPath,
      onError: () => cleanupOutputFile(outputFile),
      buildResult: (stdout, stderr) => {
        // Read outputs from the GITHUB_OUTPUT file
        /** @type {Record<string, string>} */
        const outputs = {};
        try {
          if (fs.existsSync(outputFile)) {
            const outputContent = fs.readFileSync(outputFile, "utf-8");
            server.debug(`  [${toolName}] Output file content: ${outputContent.substring(0, 500)}${outputContent.length > 500 ? "..." : ""}`);

            // Parse outputs (key=value format, one per line)
            const lines = outputContent.split("\n");
            for (const line of lines) {
              const trimmed = line.trim();
              if (trimmed && trimmed.includes("=")) {
                const eqIndex = trimmed.indexOf("=");
                const key = trimmed.substring(0, eqIndex);
                const value = trimmed.substring(eqIndex + 1);
                outputs[key] = value;
                server.debug(`  [${toolName}] Parsed output: ${key}=${value.substring(0, 100)}${value.length > 100 ? "..." : ""}`);
              }
            }
          }
        } catch (readError) {
          server.debugError(`  [${toolName}] Error reading output file: `, readError);
        }

        cleanupOutputFile(outputFile);

        server.debug(`  [${toolName}] Shell handler completed, outputs: ${Object.keys(outputs).join(", ") || "(none)"}`);

        return { stdout: stdout || "", stderr: stderr || "", outputs };
      },
    });
  };
}

module.exports = {
  createShellHandler,
};
