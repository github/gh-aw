// @ts-check
"use strict";

// Ensures global.core is available when running outside github-script context
require("./shim.cjs");

/**
 * convert_gateway_config_claude.cjs
 *
 * Converts the MCP gateway's standard HTTP-based configuration to the JSON
 * format expected by Claude. Reads the gateway output JSON, filters out
 * CLI-mounted servers, sets type:"http", rewrites URLs to use the correct
 * domain, and writes the result to ${RUNNER_TEMP}/gh-aw/mcp-config/mcp-servers.json.
 *
 * Required environment variables:
 * - MCP_GATEWAY_OUTPUT: Path to gateway output configuration file
 * - MCP_GATEWAY_DOMAIN: Domain for MCP server URLs (e.g., host.docker.internal)
 * - MCP_GATEWAY_PORT: Port for MCP gateway (e.g., 80)
 * - RUNNER_TEMP: GitHub Actions runner temp directory
 *
 * Optional:
 * - GH_AW_MCP_CLI_SERVERS: JSON array of server names to exclude from agent config
 */

const path = require("path");
const { normalizeGatewayEntry, runGatewayConversion, addWebFetchMCPServer } = require("./convert_gateway_config_shared.cjs");

const OUTPUT_PATH = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json");

/**
 * @param {Record<string, unknown>} entry
 * @param {string} urlPrefix
 * @returns {Record<string, unknown>}
 */
function transformClaudeEntry(entry, urlPrefix) {
  return normalizeGatewayEntry(entry, urlPrefix, transformed => {
    // Claude uses "type": "http" for HTTP-based MCP servers
    transformed.type = "http";
    // The MCP gateway may include a "tools" field for Copilot, but Claude's
    // MCP config format does not support that field.
    delete transformed.tools;
  });
}

function main() {
  return runGatewayConversion({
    format: "Claude",
    engine: "Claude",
    outputPath: OUTPUT_PATH,
    transformServer: (_name, entry, urlPrefix) => transformClaudeEntry(entry, urlPrefix),
    serialize: servers => {
      addWebFetchMCPServer(servers, config => ({ ...config, type: "stdio" }));
      return JSON.stringify({ mcpServers: servers }, null, 2);
    },
  });
}

if (require.main === module) {
  main();
}

module.exports = { transformClaudeEntry, main };
