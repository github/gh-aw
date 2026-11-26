// @ts-check
"use strict";

// Ensures global.core is available when running outside github-script context
require("./shim.cjs");

/**
 * convert_gateway_config_gemini.cjs
 *
 * Converts the MCP gateway's standard HTTP-based configuration to the JSON
 * format expected by Gemini CLI (.gemini/settings.json). Reads the gateway
 * output JSON, filters out CLI-mounted servers, removes the "type" field
 * (Gemini uses transport auto-detection), rewrites URLs to use the correct
 * domain, and adds /tmp/ to context.includeDirectories.
 *
 * Gemini CLI reads MCP server configuration from settings.json files:
 * - Global: ~/.gemini/settings.json
 * - Project: .gemini/settings.json (used here)
 *
 * See: https://geminicli.com/docs/tools/mcp-server/
 *
 * Required environment variables:
 * - MCP_GATEWAY_OUTPUT: Path to gateway output configuration file
 * - MCP_GATEWAY_DOMAIN: Domain for MCP server URLs (required by loadGatewayContext)
 * - MCP_GATEWAY_HOST_DOMAIN: Host-side domain for Gemini MCP URLs (e.g., localhost)
 * - MCP_GATEWAY_PORT: Port for MCP gateway (e.g., 80)
 * - GITHUB_WORKSPACE: Workspace directory for project-level settings
 *
 * Optional:
 * - GH_AW_MCP_CLI_SERVERS: JSON array of server names to exclude from agent config
 */

const path = require("path");
const { rewriteUrl, normalizeGatewayEntry, runGatewayConversion, addWebFetchMCPServer } = require("./convert_gateway_config_shared.cjs");

/**
 * @param {Record<string, unknown>} entry
 * @param {string} urlPrefix
 * @returns {Record<string, unknown>}
 */
function transformGeminiEntry(entry, urlPrefix) {
  return normalizeGatewayEntry(entry, urlPrefix, transformed => {
    // Remove "type" field — Gemini uses transport auto-detection from url/httpUrl
    delete transformed.type;
  });
}

function getGeminiHostDomain() {
  return process.env.MCP_GATEWAY_HOST_DOMAIN || "localhost";
}

function main() {
  const hostDomain = getGeminiHostDomain();
  return runGatewayConversion({
    format: "Gemini",
    engine: "Gemini",
    contextOptions: { extraRequiredEnv: ["GITHUB_WORKSPACE"] },
    outputPath: ({ extraEnv }) => path.join(extraEnv.GITHUB_WORKSPACE, ".gemini", "settings.json"),
    getTargetDomain: () => hostDomain,
    getUrlPrefix: ({ port }) => `http://${hostDomain}:${port}`,
    transformServer: (_name, entry, urlPrefix) => transformGeminiEntry(entry, urlPrefix),
    serialize: servers => {
      const mcpServers = { ...servers };
      addWebFetchMCPServer(mcpServers, entry => entry);
      return JSON.stringify(
        {
          mcpServers,
          context: { includeDirectories: ["/tmp/"] },
        },
        null,
        2
      );
    },
  });
}

if (require.main === module) {
  main();
}

module.exports = { rewriteUrl, transformGeminiEntry, main };
