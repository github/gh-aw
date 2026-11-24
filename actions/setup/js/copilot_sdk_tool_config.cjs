// @ts-check

"use strict";

const { readCopilotSDKMCPConfig } = require("./copilot_sdk_mcp_config.cjs");
const { resolveCopilotSDKMCPConfigPath } = require("./copilot_sdk_mcp_config_path.cjs");
const { createCopilotSDKWebFetchTool } = require("./copilot_sdk_web_fetch.cjs");
const { parseMaxToolCalls } = require("./copilot_sdk_tool_budget.cjs");

const COPILOT_SDK_TOOL_CONFIG_VERSION = 1;
const COPILOT_SDK_NEUTRAL_BUILTIN_TOOLS = Object.freeze(["view", "rg", "glob", "sql"]);
const COPILOT_SDK_SHELL_BUILTIN_TOOLS = Object.freeze(["bash", "read_bash", "stop_bash", "list_bash"]);
const COPILOT_SDK_EDIT_BUILTIN_TOOLS = Object.freeze(["apply_patch", "edit", "create", "delete", "move", "write_bash"]);
const COPILOT_SDK_WORKFLOW_BUILTIN_TOOLS = Object.freeze(["run_dynamic_workflow", "dynamic_workflows_manage"]);

/**
 * @typedef {{
 *   bash: boolean,
 *   edit: boolean,
 *   webFetch: boolean,
 *   webSearch: boolean,
 *   dynamicWorkflows: boolean,
 *   mcp: boolean,
 *   cliProxy: boolean,
 * }} CopilotSDKToolCapabilities
 */

/**
 * @typedef {{
 *   version: number,
 *   capabilities: CopilotSDKToolCapabilities,
 *   permissions: {allowedTools: string[]},
 *   maxToolCalls?: number,
 *   explicitlyDisabledTools: string[],
 * }} CopilotSDKToolConfig
 */

/**
 * @param {unknown} value
 * @returns {value is Record<string, unknown>}
 */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/**
 * @param {unknown} value
 * @param {string} field
 * @returns {string[]}
 */
function parseStringArray(value, field) {
  if (!Array.isArray(value) || value.some(entry => typeof entry !== "string" || entry.trim() === "")) {
    throw new Error(`${field} must be an array of non-empty strings`);
  }
  const normalized = value.map(entry => entry.trim());
  if (new Set(normalized).size !== normalized.length) {
    throw new Error(`${field} must not contain duplicate entries`);
  }
  return normalized;
}

/**
 * @param {Record<string, unknown>} value
 * @returns {CopilotSDKToolCapabilities}
 */
function parseCapabilities(value) {
  const fields = ["bash", "edit", "webFetch", "webSearch", "mcp", "cliProxy"];
  for (const field of fields) {
    if (typeof value[field] !== "boolean") {
      throw new Error(`capabilities.${field} must be a boolean`);
    }
  }
  if (value.dynamicWorkflows !== undefined && typeof value.dynamicWorkflows !== "boolean") {
    throw new Error("capabilities.dynamicWorkflows must be a boolean");
  }
  return /** @type {CopilotSDKToolCapabilities} */ {
    bash: Boolean(value.bash),
    edit: Boolean(value.edit),
    webFetch: Boolean(value.webFetch),
    webSearch: Boolean(value.webSearch),
    // Older version-1 contracts omit this capability and must not expose workflows.
    dynamicWorkflows: value.dynamicWorkflows === true,
    mcp: Boolean(value.mcp),
    cliProxy: Boolean(value.cliProxy),
  };
}

/**
 * Return true for SDK permission entries that are not MCP server grants.
 * web_fetch and web_search are intentionally only supported as unscoped
 * built-in/custom tool permissions; scoped entries like web_fetch(...) are not
 * part of the compiler contract and are deliberately treated as MCP grants so
 * validateToolPermissionParity's webFetch check catches them with a clear
 * "visibility and permissions differ" error instead of silently allowing them.
 *
 * @param {string} tool
 * @returns {boolean}
 */
function isReservedSDKPermission(tool) {
  return (
    tool === "read" || tool === "write" || tool === "web_fetch" || tool === "web_search" || tool === "workflow" || tool === "shell" || (tool.startsWith("read(") && tool.endsWith(")")) || (tool.startsWith("shell(") && tool.endsWith(")"))
  );
}

/**
 * @param {CopilotSDKToolConfig} config
 */
function validateToolPermissionParity(config) {
  const allowed = new Set(config.permissions.allowedTools);
  const hasShellPermission = config.permissions.allowedTools.some(tool => tool === "shell" || (tool.startsWith("shell(") && tool.endsWith(")")));
  const hasMCPPermission = config.permissions.allowedTools.some(tool => !isReservedSDKPermission(tool));

  if (config.capabilities.bash !== hasShellPermission) {
    throw new Error("SDK tool contract mismatch: bash visibility and shell permissions differ");
  }
  if (config.capabilities.edit !== allowed.has("write")) {
    throw new Error("SDK tool contract mismatch: edit visibility and write permissions differ");
  }
  if (config.capabilities.webFetch !== allowed.has("web_fetch")) {
    throw new Error("SDK tool contract mismatch: web_fetch visibility and permissions differ");
  }
  if (config.capabilities.webSearch !== allowed.has("web_search")) {
    throw new Error("SDK tool contract mismatch: web_search visibility and permissions differ");
  }
  if (config.capabilities.webSearch) {
    throw new Error("Copilot native web_search is unavailable in offline BYOK mode; configure an MCP search server instead");
  }
  if (config.capabilities.dynamicWorkflows !== allowed.has("workflow")) {
    throw new Error("SDK tool contract mismatch: workflow visibility and permissions differ");
  }
  if (!config.capabilities.mcp && hasMCPPermission) {
    throw new Error("SDK tool contract mismatch: MCP permissions exist while MCP visibility is disabled");
  }
  // cli-proxy mounts MCP servers as CLI wrapper scripts on PATH; those scripts are
  // only reachable through the bash builtin, so a cliProxy capability without a
  // bash capability is an unreachable, misleading contract.
  if (config.capabilities.cliProxy && !config.capabilities.bash) {
    throw new Error("SDK tool contract mismatch: cliProxy capability requires bash capability");
  }
  const disabledCapability = new Map([
    ["bash", "bash"],
    ["edit", "edit"],
    ["web-fetch", "webFetch"],
    ["web-search", "webSearch"],
    ["cli-proxy", "cliProxy"],
  ]);
  for (const toolName of config.explicitlyDisabledTools) {
    const capability = disabledCapability.get(toolName);
    if (capability && config.capabilities[capability]) {
      throw new Error(`SDK tool contract mismatch: explicitly disabled ${toolName} is visible`);
    }
  }
}

/**
 * Parse the compiler-owned GH_AW_COPILOT_SDK_TOOL_CONFIG value. Missing,
 * malformed, inconsistent, or unsupported values fail closed.
 *
 * @param {string | undefined} value
 * @returns {CopilotSDKToolConfig}
 */
function parseCopilotSDKToolConfig(value) {
  if (!value) {
    throw new Error("GH_AW_COPILOT_SDK_TOOL_CONFIG is required");
  }
  let parsed;
  try {
    parsed = JSON.parse(value);
  } catch (error) {
    throw new Error(`GH_AW_COPILOT_SDK_TOOL_CONFIG must be valid JSON: ${error instanceof Error ? error.message : String(error)}`, { cause: error });
  }
  if (!isRecord(parsed)) {
    throw new Error("GH_AW_COPILOT_SDK_TOOL_CONFIG must be a JSON object");
  }
  if (parsed.version !== COPILOT_SDK_TOOL_CONFIG_VERSION) {
    throw new Error(`unsupported GH_AW_COPILOT_SDK_TOOL_CONFIG version: ${String(parsed.version)}`);
  }
  if (!isRecord(parsed.capabilities)) {
    throw new Error("GH_AW_COPILOT_SDK_TOOL_CONFIG capabilities must be an object");
  }
  if (!isRecord(parsed.permissions)) {
    throw new Error("GH_AW_COPILOT_SDK_TOOL_CONFIG permissions must be an object");
  }

  const allowedTools = parseStringArray(parsed.permissions.allowedTools, "permissions.allowedTools");
  if (allowedTools.length === 0) {
    throw new Error("permissions.allowedTools must not be empty");
  }
  const maxToolCalls = Object.hasOwn(parsed, "maxToolCalls") ? parseMaxToolCalls(parsed.maxToolCalls) : undefined;
  const config = {
    version: COPILOT_SDK_TOOL_CONFIG_VERSION,
    capabilities: parseCapabilities(parsed.capabilities),
    permissions: {
      allowedTools,
    },
    ...(maxToolCalls === undefined ? {} : { maxToolCalls }),
    explicitlyDisabledTools: parsed.explicitlyDisabledTools == null ? [] : parseStringArray(parsed.explicitlyDisabledTools, "explicitlyDisabledTools"),
  };
  validateToolPermissionParity(config);
  return config;
}

/**
 * SDK sessions need explicit server registration; allowing MCP tools does not
 * load the CLI's gateway configuration into the session. Only use the separate
 * converter-owned catalog, never the user-managed GH_AW_MCP_CONFIG CLI catalog.
 *
 * @param {CopilotSDKToolConfig} config
 * @param {string | undefined} [configPath]
 * @returns {Record<string, import("@microsoft/copilot-sdk").MCPServerConfig>}
 */
function loadCopilotSDKMCPServers(config, configPath) {
  if (!config.capabilities.mcp) return {};
  configPath ??= resolveCopilotSDKMCPConfigPath();
  if (!configPath) throw new Error("Copilot SDK MCP configuration path is required when SDK MCP tools are enabled");

  const source = readCopilotSDKMCPConfig(configPath);
  let parsed;
  try {
    parsed = JSON.parse(source);
  } catch {
    throw new Error("Copilot SDK MCP configuration must be valid JSON");
  }
  if (!isRecord(parsed) || !isRecord(parsed.mcpServers)) {
    throw new Error("Copilot SDK MCP configuration must contain an mcpServers object");
  }

  /** @type {Record<string, import("@microsoft/copilot-sdk").MCPServerConfig>} */
  const servers = {};
  for (const [name, entry] of Object.entries(parsed.mcpServers)) {
    if (name === "web-fetch" && isRecord(entry) && entry.type === "stdio" && config.permissions.allowedTools.includes(name)) {
      // Reconstruct the sole permitted local server from the setup path, not from config arguments.
      const { webFetchStdioConfig } = require("./convert_gateway_config_shared.cjs");
      servers[name] = { type: "stdio", ...webFetchStdioConfig(), tools: ["web_fetch"] };
      continue;
    }
    if (!isRecord(entry) || (entry.type !== "http" && entry.type !== "sse") || typeof entry.url !== "string") {
      throw new Error("Copilot SDK MCP configuration servers must use the generated HTTP gateway configuration");
    }
    /** @type {import("@microsoft/copilot-sdk").MCPHTTPServerConfig} */
    const server = { type: entry.type === "http" ? "http" : "sse", url: entry.url };
    if (entry.tools !== undefined) {
      if (!Array.isArray(entry.tools) || entry.tools.some(tool => typeof tool !== "string" || tool.trim() === "")) {
        throw new Error("MCP server tools must be an array of non-empty strings");
      }
      server.tools = [...new Set(entry.tools.map(tool => tool.trim()))];
    }
    if (entry.timeout !== undefined) {
      if (typeof entry.timeout !== "number" || !Number.isFinite(entry.timeout) || entry.timeout <= 0) {
        throw new Error("MCP server timeout must be a positive number");
      }
      server.timeout = entry.timeout;
    }
    if (entry.headers !== undefined) {
      if (!isRecord(entry.headers) || Object.values(entry.headers).some(value => typeof value !== "string")) {
        throw new Error("MCP server headers must be an object of strings");
      }
      server.headers = Object.fromEntries(Object.entries(entry.headers).map(([key, value]) => [key, String(value)]));
    }
    // Preserve names such as __proto__ as own keys without invoking inherited setters.
    Object.defineProperty(servers, name, { value: server, enumerable: true, configurable: true, writable: true });
  }
  return servers;
}

/**
 * @param {CopilotSDKToolConfig | null | undefined} config
 * @param {{
 *   ToolSet?: typeof import("@microsoft/copilot-sdk").ToolSet,
 *   BuiltInTools?: typeof import("@microsoft/copilot-sdk").BuiltInTools,
 *   defineTool?: typeof import("@microsoft/copilot-sdk").defineTool,
 * }} sdk
 * @param {{fetchImpl?: typeof fetch, timeoutMs?: number, maxRedirects?: number, mcpOnly?: boolean}} [options]
 * @returns {import("@microsoft/copilot-sdk").CreateSessionOptions["tools"]}
 */
function buildCopilotSDKSessionToolConfig(config, sdk, options = {}) {
  // An empty allowlist denies all tools; omitting it leaves the SDK catalog unfiltered.
  if (config == null) return { availableTools: [], definitions: [] };
  if (typeof sdk.ToolSet !== "function" || !sdk.BuiltInTools || !Array.isArray(sdk.BuiltInTools.Isolated)) {
    throw new Error("Copilot SDK ToolSet and BuiltInTools.Isolated are required for compiler-controlled tool filtering");
  }

  const availableTools = new sdk.ToolSet();
  if (options.mcpOnly) {
    if (config.capabilities.mcp) availableTools.addMcp("*");
    return { availableTools, definitions: [] };
  }
  availableTools.addBuiltIn(sdk.BuiltInTools.Isolated.filter(name => name !== "ask_user" && !COPILOT_SDK_WORKFLOW_BUILTIN_TOOLS.includes(name)));
  availableTools.addBuiltIn(COPILOT_SDK_NEUTRAL_BUILTIN_TOOLS);
  if (config.capabilities.bash) availableTools.addBuiltIn(COPILOT_SDK_SHELL_BUILTIN_TOOLS);
  if (config.capabilities.edit) availableTools.addBuiltIn(COPILOT_SDK_EDIT_BUILTIN_TOOLS);
  if (config.capabilities.webSearch) {
    throw new Error("Copilot native web_search is unavailable in offline BYOK mode; configure an MCP search server instead");
  }
  if (config.capabilities.dynamicWorkflows) availableTools.addBuiltIn(COPILOT_SDK_WORKFLOW_BUILTIN_TOOLS);
  if (config.capabilities.mcp) availableTools.addMcp("*");
  // cliProxy mounts MCP servers as CLI wrapper scripts on PATH; those scripts are
  // invoked through the bash builtin (already added above when capabilities.bash
  // is true), so cliProxy needs no separate SDK tool/availableTools entry here.
  // validateToolPermissionParity enforces that cliProxy can only be true when
  // bash is also true, so this capability is never silently unreachable.

  /** @type {import("@microsoft/copilot-sdk").Tool<any>[]} */
  const tools = [];
  if (config.capabilities.webFetch) {
    if (typeof sdk.defineTool !== "function") {
      throw new Error("Copilot SDK defineTool is required when tools.web-fetch is enabled");
    }
    tools.push(createCopilotSDKWebFetchTool(sdk.defineTool, options));
    // The runtime classifies a same-name replacement as the built-in tool it
    // overrides, rather than as a custom tool.
    availableTools.addBuiltIn("web_fetch");
  }
  return { availableTools, definitions: tools };
}

module.exports = {
  loadCopilotSDKMCPServers,
  COPILOT_SDK_TOOL_CONFIG_VERSION,
  COPILOT_SDK_NEUTRAL_BUILTIN_TOOLS,
  COPILOT_SDK_SHELL_BUILTIN_TOOLS,
  COPILOT_SDK_EDIT_BUILTIN_TOOLS,
  COPILOT_SDK_WORKFLOW_BUILTIN_TOOLS,
  parseStringArray,
  parseCapabilities,
  isReservedSDKPermission,
  validateToolPermissionParity,
  parseCopilotSDKToolConfig,
  buildCopilotSDKSessionToolConfig,
};
