// @ts-check
"use strict";

/** @typedef {NonNullable<import("@github/copilot-sdk").ToolInvocation["availableTools"]>[number]} ToolMetadata */

/**
 * @param {ToolMetadata} tool
 * @param {Set<string>} permissions
 * @param {Record<string, import("@github/copilot-sdk").MCPServerConfig>} servers
 */
function authorizedMCPTool(tool, permissions, servers) {
  const server = tool.mcpServerName;
  const name = tool.mcpToolName;
  if (!server || !name || !Object.hasOwn(servers, server)) return false;
  const configured = servers[server].tools;
  return (permissions.has(server) || permissions.has(`${server}(${name})`)) && (!configured || configured.includes("*") || configured.includes(name));
}

/**
 * @param {import("@github/copilot-sdk").CopilotSession} session
 * @param {{
 * ToolSet: typeof import("@github/copilot-sdk").ToolSet,
 * availableTools: import("@github/copilot-sdk").ToolSet,
 * allowedTools: string[],
 * mcpServers: Record<string, import("@github/copilot-sdk").MCPServerConfig>,
 * }} options
 * @returns {Promise<ToolMetadata[]>}
 */
async function restrictTaskCatalog(session, { ToolSet, availableTools, allowedTools, mcpServers }) {
  if (!session.rpc?.tools?.initializeAndValidate || !session.rpc.tools.getCurrentMetadata || !session.rpc.options?.update) {
    throw new Error("Locked tasks require SDK native catalog initialization and tool-filter update APIs");
  }
  const permissions = new Set(allowedTools);
  const base = availableTools.toArray().filter(selector => selector !== "mcp:*");
  let expired = false;
  /** @type {NodeJS.Timeout | undefined} */
  let timer;
  function requireActive() {
    if (expired) throw new Error("SDK locked-tasks catalog initialization expired before inference");
  }
  /** @returns {Promise<ToolMetadata[]>} */
  async function metadata() {
    requireActive();
    await session.rpc.tools.initializeAndValidate();
    requireActive();
    const result = await session.rpc.tools.getCurrentMetadata();
    requireActive();
    if (!Array.isArray(result.tools)) throw new Error("SDK locked-tasks catalog metadata is unavailable");
    const names = new Set();
    for (const tool of result.tools) {
      if (!tool || typeof tool.name !== "string" || !tool.name || names.has(tool.name) || Boolean(tool.mcpServerName) !== Boolean(tool.mcpToolName)) {
        throw new Error("SDK locked-tasks catalog metadata contains invalid or duplicate tools");
      }
      names.add(tool.name);
    }
    return result.tools;
  }
  /** @param {ToolMetadata[]} tools */
  function requireTask(tools) {
    const tasks = tools.filter(tool => tool.mcpServerName === "locked-tasks");
    if (tasks.length !== 1 || tasks[0].mcpToolName !== "run_task" || tasks[0].deferLoading === true) {
      throw new Error("SDK locked-tasks catalog must expose exactly the native locked-tasks.run_task tool");
    }
  }
  async function initialize() {
    const initial = await metadata();
    requireTask(initial);
    const approved = initial.filter(tool => authorizedMCPTool(tool, permissions, mcpServers));
    const concrete = new ToolSet();
    for (const tool of approved) concrete.addMcp(tool.name);
    requireActive();
    const update = await session.rpc.options.update({ availableTools: [...base, ...concrete.toArray()] });
    requireActive();
    if (update.success !== true) throw new Error("SDK rejected the concrete locked-tasks tool catalog");
    const restricted = await metadata();
    requireTask(restricted);
    for (const tool of restricted.filter(tool => tool.mcpServerName)) {
      if (!authorizedMCPTool(tool, permissions, mcpServers) || !approved.some(entry => entry.name === tool.name && entry.mcpServerName === tool.mcpServerName && entry.mcpToolName === tool.mcpToolName)) {
        throw new Error("SDK locked-tasks catalog exposes an unauthorized MCP tool");
      }
    }
    return restricted;
  }
  try {
    /** @type {Promise<ToolMetadata[]>} */
    const deadline = new Promise((_, reject) => {
      timer = setTimeout(() => {
        expired = true;
        reject(new Error("SDK locked-tasks catalog initialization timed out before inference"));
      }, 60_000);
    });
    return await Promise.race([initialize(), deadline]);
  } finally {
    expired = true;
    clearTimeout(timer);
  }
}

module.exports = { authorizedMCPTool, restrictTaskCatalog };
