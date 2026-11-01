// @ts-check

/**
 * Shared Process Execution Helper for MCP Script Handlers
 *
 * Provides a common execution envelope used by all language-specific MCP script handlers.
 * Centralises execFile invocation, stdout/stderr debug logging, timeout/maxBuffer setup,
 * stdout JSON parsing, error enrichment, and MCP content response construction.
 */

const { execFile } = require("child_process");

/**
 * Preserve only runtime paths/locales, not ambient network/registry/TLS
 * configuration, GitHub metadata, or runner/server credentials.
 * Additional handler configuration requires explicit per-tool bindings.
 * Resolve only this tool's bindings; transport names never reach the child.
 * @param {Record<string, string>} [bindings]
 * @returns {NodeJS.ProcessEnv}
 */
function createToolEnvironment(bindings = {}) {
  /** @type {NodeJS.ProcessEnv} */
  const env = {};
  const runtimeNames = ["PATH", "HOME", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "GITHUB_WORKSPACE", "RUNNER_TEMP", "GOROOT", "GOPATH", "GOCACHE", "GOMODCACHE"];
  for (const name of runtimeNames) {
    if (process.env[name] !== undefined) {
      env[name] = process.env[name];
    }
  }
  for (const [name, source] of Object.entries(bindings)) {
    if (typeof source !== "string" || !Object.hasOwn(process.env, source)) {
      throw new Error(`Missing MCP script environment binding for '${name}'`);
    }
    Object.defineProperty(env, name, { value: process.env[source], enumerable: true, writable: true, configurable: true });
  }
  return env;
}

/**
 * Builds an enhanced error message that includes stdout/stderr so the AI agent
 * can see what actually went wrong (not just "Command failed").
 * Preserves exit code, signal, and the original error message so timeout and
 * missing-interpreter failures remain accurately described.
 *
 * @param {import('child_process').ExecFileException} error - The original execution error
 * @param {string} scriptPath - Path to the script, used for context in the message
 * @param {string} stdout - Process stdout output
 * @param {string} stderr - Process stderr output
 * @returns {Error} Enhanced error with stdout/stderr context
 */
function buildEnhancedError(error, scriptPath, stdout, stderr) {
  const parts = [];

  if (typeof error.code === "number") {
    // Normal non-zero exit
    parts.push(`Command failed: ${scriptPath} (exit code: ${error.code})`);
  } else if (error.signal) {
    // Killed by signal (e.g. SIGTERM on timeout)
    parts.push(`Command failed: ${scriptPath} (signal: ${error.signal})`);
  } else {
    // Other OS-level failures (e.g. ENOENT for missing interpreter) — preserve original message
    parts.push(`Command failed: ${scriptPath} — ${error.message}`);
  }

  if (stderr && stderr.trim()) {
    parts.push(`stderr:\n${stderr.trim()}`);
  }
  if (stdout && stdout.trim()) {
    parts.push(`stdout:\n${stdout.trim()}`);
  }
  return new Error(parts.join("\n"));
}

/**
 * Parses stdout as JSON, falling back to { stdout, stderr } if parsing fails
 * or stdout is empty. Calls onParseFailure when JSON parsing fails.
 *
 * @param {string} stdout - Process stdout output
 * @param {string} stderr - Process stderr output
 * @param {Function} [onParseFailure] - Called when JSON parsing fails (e.g. for debug logging)
 * @returns {Object} Parsed result object
 */
function parseStdoutAsJson(stdout, stderr, onParseFailure) {
  try {
    if (stdout && stdout.trim()) {
      return JSON.parse(stdout.trim());
    }
    return { stdout: stdout || "", stderr: stderr || "" };
  } catch {
    if (onParseFailure) {
      onParseFailure();
    }
    return { stdout: stdout || "", stderr: stderr || "" };
  }
}

/**
 * Wraps a result object in MCP content format.
 *
 * @param {Object} result - Result object to wrap
 * @returns {{ content: Array<{ type: string, text: string }> }} MCP content response
 */
function wrapMCPContent(result) {
  return {
    content: [
      {
        type: "text",
        text: JSON.stringify(result),
      },
    ],
  };
}

/**
 * Executes a process and returns a Promise resolving to an MCP content response.
 * This is the shared execution envelope for all language-specific MCP script handlers.
 *
 * @param {Object} opts
 * @param {Object} opts.server - The MCP server instance for logging
 * @param {string} opts.toolName - Name of the tool for logging
 * @param {string} opts.languageLabel - Human-readable language label (e.g. "Go", "Python")
 * @param {string} opts.command - Command to execute
 * @param {string[]} opts.args - Command arguments
 * @param {Object} opts.env - Environment variables for the process
 * @param {string|null} opts.inputJson - JSON string to write to stdin, or null for no stdin input
 * @param {number} opts.timeoutSeconds - Timeout in seconds
 * @param {string} opts.scriptPath - Script path used in error messages
 * @param {Function} [opts.onError] - Optional cleanup callback invoked on error before rejecting. Receives (error, stdout, stderr) but may ignore them (e.g. for file cleanup).
 * @param {Function} [opts.buildResult] - Optional custom result builder: (stdout, stderr) => Object
 * @returns {Promise<{ content: Array<{ type: string, text: string }> }>} MCP content response
 */
function executeProcess(opts) {
  const { server, toolName, languageLabel, command, args, env, inputJson, timeoutSeconds, scriptPath, onError, buildResult } = opts;

  return new Promise((resolve, reject) => {
    server.debug(`  [${toolName}] Executing ${languageLabel} script...`);

    const child = execFile(
      command,
      args,
      {
        env,
        cwd: process.env.GITHUB_WORKSPACE || process.cwd(),
        timeout: timeoutSeconds * 1000, // Convert to milliseconds
        maxBuffer: 10 * 1024 * 1024, // 10MB buffer
      },
      (error, stdout, stderr) => {
        // Log stdout and stderr
        if (stdout) {
          server.debug(`  [${toolName}] stdout: ${stdout.substring(0, 500)}${stdout.length > 500 ? "..." : ""}`);
        }
        if (stderr) {
          server.debug(`  [${toolName}] stderr: ${stderr.substring(0, 500)}${stderr.length > 500 ? "..." : ""}`);
        }

        if (error) {
          server.debugError(`  [${toolName}] ${languageLabel} script error: `, error);
          if (onError) {
            try {
              onError(error, stdout, stderr);
            } catch (cleanupError) {
              server.debugError(`  [${toolName}] onError cleanup threw: `, cleanupError);
            }
          }
          reject(buildEnhancedError(error, scriptPath, stdout, stderr));
          return;
        }

        // Build result using custom builder or default JSON parsing
        let result;
        try {
          if (buildResult) {
            result = buildResult(stdout, stderr);
          } else {
            result = parseStdoutAsJson(stdout, stderr, () => {
              server.debug(`  [${toolName}] Output is not JSON, returning as text`);
            });
          }
        } catch (buildError) {
          server.debugError(`  [${toolName}] buildResult threw: `, buildError);
          result = { stdout: stdout || "", stderr: stderr || "" };
        }

        server.debug(`  [${toolName}] ${languageLabel} handler completed successfully`);

        resolve(wrapMCPContent(result));
      }
    );

    // Write input JSON to stdin if provided
    if (inputJson !== null && inputJson !== undefined && child.stdin) {
      child.stdin.write(inputJson);
      child.stdin.end();
    }
  });
}

module.exports = {
  createToolEnvironment,
  buildEnhancedError,
  parseStdoutAsJson,
  wrapMCPContent,
  executeProcess,
};
