// @ts-check
"use strict";

/** @param {string | undefined} raw @param {string} name @returns {number | undefined} */
function timeoutMilliseconds(raw, name) {
  if (raw == null || raw === "") return undefined;
  if (!/^\d+$/.test(raw.trim())) throw new Error(`${name} must resolve to a positive integer number of seconds`);
  const milliseconds = Number(raw) * 1000;
  if (!Number.isSafeInteger(milliseconds) || milliseconds <= 0 || milliseconds > 2147483647) {
    throw new Error(`${name} must resolve to a positive timeout of at most 2147483 seconds`);
  }
  return milliseconds;
}

/** @param {NodeJS.ProcessEnv} env */
function applyClaudeRuntimeTimeouts(env) {
  const startup = timeoutMilliseconds(env.GH_AW_STARTUP_TIMEOUT, "tools.startup-timeout");
  const tool = timeoutMilliseconds(env.GH_AW_TOOL_TIMEOUT, "tools.timeout");
  if (startup !== undefined) env.MCP_TIMEOUT = String(startup);
  if (tool !== undefined) {
    for (const key of ["MCP_TOOL_TIMEOUT", "BASH_DEFAULT_TIMEOUT_MS", "BASH_MAX_TIMEOUT_MS"]) env[key] = String(tool);
  }
}

/** @param {Record<string, any>} config @param {NodeJS.ProcessEnv} env */
function applyGatewayRuntimeTimeouts(config, env) {
  const startup = timeoutMilliseconds(env.GH_AW_STARTUP_TIMEOUT, "tools.startup-timeout");
  const tool = timeoutMilliseconds(env.GH_AW_TOOL_TIMEOUT, "tools.timeout");
  if (startup !== undefined) {
    config.gateway.startupTimeout = startup / 1000;
    env.GH_AW_MCP_GATEWAY_BACKEND_STARTUP_TIMEOUT_MS = String(startup);
  }
  if (tool !== undefined && !config.gateway.toolTimeout) config.gateway.toolTimeout = `${tool / 1000}s`;
}

module.exports = { timeoutMilliseconds, applyClaudeRuntimeTimeouts, applyGatewayRuntimeTimeouts };
