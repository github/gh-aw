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
 * Removes the private temporary output directory.
 *
 * @param {Object} server - The MCP server instance for logging
 * @param {string} outputDir - Path to the directory to remove
 */
function cleanupOutputDirectory(server, outputDir) {
  try {
    fs.rmSync(outputDir, { recursive: true, force: true });
  } catch (error) {
    server.debugError("Error removing shell output directory: ", error);
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
    let outputDir;
    try {
      outputDir = fs.mkdtempSync(path.join(os.tmpdir(), "mcp-shell-output-"));
    } catch (error) {
      throw new Error(`Failed to create shell output directory for ${toolName}: ${String(error)}`, { cause: error });
    }
    const outputFile = path.join(outputDir, "output.txt");
    env.GITHUB_OUTPUT = outputFile;
    server.debug(`  [${toolName}] Output file: ${outputFile}`);

    let outputFd;
    try {
      outputFd = fs.openSync(outputFile, "wx+", 0o600);
    } catch (err) {
      cleanupOutputDirectory(server, outputDir);
      throw new Error(`Failed to write file ${outputFile}: ${String(err)}`, { cause: err });
    }

    try {
      return await executeProcess({
        server,
        toolName,
        languageLabel: "Shell",
        command: scriptPath,
        args: [],
        env,
        inputJson: null, // Shell uses env vars instead of stdin
        timeoutSeconds,
        scriptPath,
        buildResult: (stdout, stderr) => {
          // Read outputs from the GITHUB_OUTPUT file
          /** @type {Record<string, string>} */
          const outputs = {};
          try {
            // Read the original file, not a path the script could have replaced.
            const outputContent = fs.readFileSync(outputFd, "utf-8");
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
          } catch (readError) {
            server.debugError(`  [${toolName}] Error reading output file: `, readError);
          }

          server.debug(`  [${toolName}] Shell handler completed, outputs: ${Object.keys(outputs).join(", ") || "(none)"}`);

          return { stdout: stdout || "", stderr: stderr || "", outputs };
        },
      });
    } finally {
      try {
        fs.closeSync(outputFd);
      } finally {
        cleanupOutputDirectory(server, outputDir);
      }
    }
  };
}

module.exports = {
  createShellHandler,
};
