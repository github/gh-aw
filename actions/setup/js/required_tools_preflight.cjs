// @ts-check
"use strict";

const { fetchMCPToolsResult } = require("./mount_mcp_as_cli.cjs");
const { ERR_CONFIG, ERR_SYSTEM } = require("./error_codes.cjs");

/**
 * Validate compiler-recorded requirements against live, filtered tools/list.
 * Never infer or expand permissions from the prompt.
 *
 * @param {Record<string, unknown>} gatewayOutput
 * @param {Record<string, string[]>} required
 * @param {string} gatewayPort
 * @param {string} agentId
 * @param {typeof import("@actions/core")} core
 * @param {typeof fetchMCPToolsResult} [fetchTools]
 */
async function preflightRequiredTools(gatewayOutput, required, gatewayPort, agentId, core, fetchTools = fetchMCPToolsResult) {
  if (!required || typeof required !== "object" || Array.isArray(required)) {
    throw new Error(`${ERR_CONFIG}: Invalid required MCP tools configuration`);
  }
  for (const [server, tools] of Object.entries(required)) {
    if (!/^[a-zA-Z0-9_-]+$/.test(server) || !Array.isArray(tools) || !tools.every(tool => typeof tool === "string" && /^[a-zA-Z0-9_-]+$/.test(tool))) {
      throw new Error(`${ERR_CONFIG}: Invalid required MCP tools configuration`);
    }
    if (tools.length === 0) continue;
    const servers = gatewayOutput.mcpServers;
    if (!servers || typeof servers !== "object" || !Object.hasOwn(servers, server)) {
      throw new Error(`${ERR_CONFIG}: Required-tool preflight: server "${server}" is not exposed. Check tools.${server} and its transport configuration. The agent will not start.`);
    }
    if (!/^\d+$/.test(gatewayPort) || !agentId) {
      throw new Error(`${ERR_CONFIG}: Required-tool preflight needs a gateway port and agent ID`);
    }
    const url = `http://localhost:${gatewayPort}/mcp/${server}`;
    const result = await fetchTools(url, agentId, core, 10000, tools);
    if (!result.emptyWasSuccessful) {
      throw new Error(`${ERR_SYSTEM}: Required-tool preflight could not list tools for "${server}". Check gateway authentication and backend startup logs. The agent will not start.`);
    }
    const exposed = new Set(result.tools.map(tool => tool.name));
    const missing = tools.filter(tool => !exposed.has(tool));
    if (missing.length > 0) {
      const configuration = server === "safeoutputs" ? "safe-outputs" : server === "mcpscripts" ? "mcp-scripts" : `tools.${server} / mcp-servers.${server}`;
      throw new Error(
        `${ERR_CONFIG}: Required-tool preflight: "${server}" does not expose required tool(s): ${missing.join(", ")}. ` +
          `Check ${configuration}, its allowlist, enabled toolsets, and the MCP server version, or remove the requirement from the prompt. Permissions have not been expanded. The agent will not start.`
      );
    }
    core.info(`Required-tool preflight passed for ${server}: ${tools.join(", ")}`);
  }
}

module.exports = { preflightRequiredTools };
