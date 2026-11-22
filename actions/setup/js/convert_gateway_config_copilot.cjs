// @ts-check
"use strict";

// Ensures global.core is available when running outside github-script context
require("./shim.cjs");

/**
 * convert_gateway_config_copilot.cjs
 *
 * Converts the MCP gateway's standard HTTP-based configuration to the format
 * expected by GitHub Copilot CLI. Reads the gateway output JSON, filters out
 * CLI-mounted servers, adds tools:["*"] if missing, rewrites URLs to use the
 * correct domain, and writes the result to $HOME/.copilot/mcp-config.json
 * (typically /home/runner/.copilot/mcp-config.json on GitHub-hosted runners,
 * but may differ on self-hosted or containerized runners where HOME varies).
 *
 * Required environment variables:
 * - MCP_GATEWAY_OUTPUT: Path to gateway output configuration file
 * - MCP_GATEWAY_DOMAIN: Domain for MCP server URLs (e.g., host.docker.internal)
 * - MCP_GATEWAY_PORT: Port for MCP gateway (e.g., 80)
 * - HOME: User home directory (standard POSIX env var inherited by the runner)
 * - RUNNER_TEMP: Per-run directory for the separate SDK MCP configuration
 * - GITHUB_RUN_ID, GITHUB_RUN_ATTEMPT, GITHUB_JOB: Catalog integrity scope
 * - GITHUB_ENV: Trusted environment file for publishing catalog integrity
 *
 * Optional:
 * - GH_AW_MCP_CLI_SERVERS: JSON array of server names to exclude from agent config
 */

const path = require("path");
const { rewriteUrl, normalizeGatewayEntry, runGatewayConversion, addWebFetchMCPServer } = require("./convert_gateway_config_shared.cjs");
const { publishCopilotSDKMCPConfig } = require("./copilot_sdk_mcp_config.cjs");
const { resolveCopilotSDKMCPConfigPath } = require("./copilot_sdk_mcp_config_path.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Resolves the Copilot CLI MCP config output path from the runtime $HOME.
 * The Copilot CLI uses ~/.copilot, which is /home/runner/.copilot on standard
 * GitHub-hosted runners (HOME=/home/runner) but may differ on self-hosted or
 * containerized runners. HOME is a standard POSIX environment variable inherited
 * from the runner's parent process and passed through to shell steps; other
 * generators (copilot_mcp.go, copilot_engine_execution.go) rely on it the same way.
 *
 * Exported for testability; throws Error rather than exiting so tests can
 * exercise the missing-HOME branch.
 *
 * @returns {string}
 */
function resolveCopilotConfigOutputPath() {
  const home = process.env.HOME;
  if (!home) {
    throw new Error("HOME environment variable is not set; cannot locate Copilot CLI config directory");
  }
  return path.join(home, ".copilot", "mcp-config.json");
}

/**
 * @param {Record<string, unknown>} entry
 * @param {string} urlPrefix
 * @returns {Record<string, unknown>}
 */
function transformCopilotEntry(entry, urlPrefix) {
  return normalizeGatewayEntry(entry, urlPrefix, transformed => {
    // The gateway expresses backend tool timeouts in seconds, while Copilot
    // expects its client-side per-server timeout in milliseconds.
    if (transformed.timeout === undefined && typeof transformed.toolTimeout === "number" && Number.isFinite(transformed.toolTimeout) && transformed.toolTimeout > 0) {
      transformed.timeout = transformed.toolTimeout * 1000;
    }
    delete transformed.toolTimeout;

    // Add tools field if not present
    if (!transformed.tools) {
      transformed.tools = ["*"];
    }
  });
}

function main() {
  let outputPath;
  try {
    outputPath = resolveCopilotConfigOutputPath();
    resolveCopilotSDKMCPConfigPath();
  } catch (err) {
    core.setFailed(`ERROR: ${getErrorMessage(err)}`);
    return;
  }

  return runGatewayConversion({
    format: "Copilot",
    engine: "Copilot",
    outputPath,
    transformServer: (_name, entry, urlPrefix) => transformCopilotEntry(entry, urlPrefix),
    serialize: servers => {
      addWebFetchMCPServer(servers, config => ({ ...config, type: "stdio", tools: ["web_fetch"] }));
      const output = JSON.stringify({ mcpServers: servers }, null, 2);
      publishCopilotSDKMCPConfig(output);
      return output;
    },
  });
}

if (require.main === module) {
  main();
}

module.exports = { rewriteUrl, transformCopilotEntry, resolveCopilotConfigOutputPath, main };
