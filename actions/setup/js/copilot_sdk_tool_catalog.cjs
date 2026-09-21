// @ts-check

"use strict";

/** @typedef {NonNullable<import("@github/copilot-sdk").ToolInvocation["availableTools"]>[number]} ToolMetadata */
/** @typedef {import("@github/copilot-sdk").CopilotSession["rpc"]} SessionRPC */

const REPOSITORY_NATIVE_TOOLS = new Set(["view", "grep", "glob", "apply_patch", "edit", "create", "delete", "move", "web_fetch", "go_repository"]);
const CATALOG_TIMEOUT_MS = 60_000;

/**
 * MCP permission identifiers are not model-facing tool names. Use the SDK's
 * source metadata, never a guessed namespace prefix, to join the two.
 *
 * @param {ToolMetadata} tool
 * @param {Set<string>} permissions
 * @param {Record<string, import("@github/copilot-sdk").MCPServerConfig>} servers
 */
function isAuthorizedMCPTool(tool, permissions, servers) {
  const serverName = tool.mcpServerName;
  const toolName = tool.mcpToolName;
  if (!serverName || !toolName || !Object.hasOwn(servers, serverName)) return false;
  if (!permissions.has(serverName) && !permissions.has(`${serverName}(${toolName})`)) return false;
  const configuredTools = servers[serverName].tools;
  return !configuredTools || configuredTools.includes("*") || configuredTools.includes(toolName);
}

/**
 * @param {ToolMetadata[] | null} tools
 * @param {Set<string>} requestedNativeNames
 * @returns {ToolMetadata[]}
 */
function requireMetadata(tools, requestedNativeNames) {
  if (!Array.isArray(tools) || tools.length === 0) throw new Error("SDK tool catalog is empty or unavailable");
  const names = new Set();
  for (const tool of tools) {
    if (!tool || typeof tool.name !== "string" || !/^[A-Za-z0-9_.-]{1,200}$/.test(tool.name) || names.has(tool.name)) {
      throw new Error("SDK tool catalog contains invalid or duplicate names");
    }
    names.add(tool.name);
    if (Boolean(tool.mcpServerName) !== Boolean(tool.mcpToolName)) throw new Error("SDK tool catalog contains incomplete MCP source metadata");
    if (!tool.mcpServerName && (!REPOSITORY_NATIVE_TOOLS.has(tool.name) || !requestedNativeNames.has(tool.name))) {
      throw new Error("SDK repository catalog unexpectedly exposes an unrequested native tool");
    }
  }
  return tools;
}

/**
 * @param {ToolMetadata[]} tools
 */
function requireRepositoryTools(tools) {
  const nativeNames = new Set(tools.filter(tool => !tool.mcpServerName).map(tool => tool.name));
  for (const name of ["view", "grep", "glob", "go_repository"]) {
    if (!nativeNames.has(name)) throw new Error(`SDK repository catalog is missing required native tool: ${name}`);
  }
  if ((!nativeNames.has("edit") || !nativeNames.has("create")) && !nativeNames.has("apply_patch")) {
    throw new Error("SDK repository catalog is missing native file editing tools");
  }
  for (const name of ["noop", "create_pull_request"]) {
    if (!tools.some(tool => tool.mcpServerName === "safeoutputs" && tool.mcpToolName === name)) {
      throw new Error(`SDK repository catalog is missing required safe-output tool: ${name}`);
    }
  }
}

/**
 * Resolve and lock the actual model-facing catalog before sending any prompt.
 * The temporary MCP wildcard is used for metadata discovery only. The session
 * cannot proceed if the SDK fails to apply and verify the concrete allowlist.
 *
 * @param {{rpc: {
 *   tools: Pick<SessionRPC["tools"], "initializeAndValidate" | "getCurrentMetadata">,
 *   options: Pick<SessionRPC["options"], "update">,
 * }}} session
 * @param {{
 *   ToolSet: typeof import("@github/copilot-sdk").ToolSet,
 *   availableTools: import("@github/copilot-sdk").ToolSet,
 *   allowedTools: string[],
 *   mcpServers: Record<string, import("@github/copilot-sdk").MCPServerConfig>,
 *   timeoutMs?: number,
 *   signal?: AbortSignal,
 * }} options
 * @returns {Promise<ToolMetadata[]>}
 */
async function restrictCopilotSDKRepositoryCatalog(session, options) {
  const { ToolSet, availableTools, allowedTools, mcpServers, timeoutMs = CATALOG_TIMEOUT_MS, signal } = options;
  if (!session.rpc?.tools?.initializeAndValidate || !session.rpc.tools.getCurrentMetadata || !session.rpc.options?.update) {
    throw new Error("SDK repository profile requires native metadata and session tool-filter update APIs");
  }
  if (!mcpServers || !Object.hasOwn(mcpServers, "safeoutputs") || Object.hasOwn(mcpServers, "go_repository")) {
    throw new Error("SDK repository profile requires compiler-owned safeoutputs MCP configuration and reserves go_repository");
  }
  if (!Number.isInteger(timeoutMs) || timeoutMs <= 0 || timeoutMs > CATALOG_TIMEOUT_MS) throw new Error("Invalid SDK catalog initialization timeout");
  let expired = false;
  const deadlineAt = Date.now() + timeoutMs;
  function requireActive() {
    if (expired || signal?.aborted || Date.now() >= deadlineAt) throw new Error("SDK repository catalog initialization expired or was cancelled before inference");
  }
  requireActive();
  const permissions = new Set(allowedTools);
  const baseSelectors = availableTools.toArray().filter(selector => selector !== "mcp:*");
  if (baseSelectors.some(selector => selector.includes("*") || selector.startsWith("mcp:"))) {
    throw new Error("SDK repository profile requires explicit source-qualified native selectors");
  }
  const requestedNativeNames = new Set(
    baseSelectors.map(selector => {
      const match = /^(?:builtin|custom):([A-Za-z0-9_]+)$/.exec(selector);
      if (!match) throw new Error("SDK repository profile contains an invalid native selector");
      return match[1] === "rg" ? "grep" : match[1];
    })
  );

  /**
   * Native transport errors can contain server URLs or headers.
   * @template T
   * @param {string} stage
   * @param {() => Promise<T>} call
   * @returns {Promise<T>}
   */
  async function callRPC(stage, call) {
    requireActive();
    let result;
    try {
      result = await call();
    } catch {
      throw new Error(`SDK native tool ${stage} failed before inference; inspect the MCP gateway and runtime logs`);
    }
    requireActive();
    return result;
  }

  async function initialize() {
    await callRPC("initialization", () => session.rpc.tools.initializeAndValidate());
    const initial = requireMetadata((await callRPC("metadata discovery", () => session.rpc.tools.getCurrentMetadata())).tools, requestedNativeNames);
    const approvedMCP = initial.filter(tool => isAuthorizedMCPTool(tool, permissions, mcpServers));
    for (const name of Object.keys(mcpServers)) {
      const required = permissions.has(name) || allowedTools.some(permission => permission.startsWith(`${name}(`));
      if (required && !approvedMCP.some(tool => tool.mcpServerName === name)) {
        throw new Error("SDK repository catalog has no tools for a configured, authorized MCP server");
      }
    }
    const mcpSelectors = new ToolSet();
    for (const tool of approvedMCP) mcpSelectors.addMcp(tool.name);
    const available = [...baseSelectors, ...mcpSelectors.toArray()];
    const update = await callRPC("filter update", () => session.rpc.options.update({ availableTools: available }));
    if (update.success !== true) throw new Error("SDK rejected the repository tool allowlist");
    await callRPC("filter verification", () => session.rpc.tools.initializeAndValidate());
    const restricted = requireMetadata((await callRPC("restricted metadata discovery", () => session.rpc.tools.getCurrentMetadata())).tools, requestedNativeNames);
    if (restricted.length > 128 || restricted.some(tool => tool.deferLoading === true)) {
      throw new Error("SDK repository catalog must preload at most 128 approved tools without deferred discovery");
    }
    const approvedByName = new Map(approvedMCP.map(tool => [tool.name, tool]));
    for (const tool of restricted) {
      const approved = approvedByName.get(tool.name);
      if (tool.mcpServerName && (!approved || approved.mcpServerName !== tool.mcpServerName || approved.mcpToolName !== tool.mcpToolName || !isAuthorizedMCPTool(tool, permissions, mcpServers))) {
        throw new Error("SDK repository catalog still exposes an unauthorized MCP tool");
      }
    }
    requireRepositoryTools(restricted);
    return restricted;
  }

  /** @type {ReturnType<typeof setTimeout> | undefined} */
  let timer;
  let onAbort = () => {};
  try {
    /** @type {Promise<ToolMetadata[]>} */
    const deadline = new Promise((_, reject) => {
      timer = setTimeout(() => {
        expired = true;
        reject(new Error("SDK repository catalog initialization timed out before inference"));
      }, timeoutMs);
      onAbort = () => {
        expired = true;
        reject(new Error("SDK repository catalog initialization was cancelled before inference"));
      };
      signal?.addEventListener("abort", onAbort, { once: true });
      if (signal?.aborted) onAbort();
    });
    return await Promise.race([initialize(), deadline]);
  } finally {
    expired = true;
    if (timer) clearTimeout(timer);
    signal?.removeEventListener("abort", onAbort);
  }
}

module.exports = { CATALOG_TIMEOUT_MS, isAuthorizedMCPTool, restrictCopilotSDKRepositoryCatalog };
