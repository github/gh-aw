// @ts-check
"use strict";

const fs = require("node:fs");
const { isRecord } = require("./tasks_config.cjs");
const { tasksMCPConfig } = require("./tasks_runtime.cjs");

/** @param {string | undefined} filename @returns {Record<string, import("@github/copilot-sdk").MCPServerConfig>} */
function loadCopilotSDKMCPConfig(filename) {
  /** @type {Record<string, import("@github/copilot-sdk").MCPServerConfig>} */
  const servers = {};
  if (filename) {
    const stat = fs.statSync(filename);
    if (!stat.isFile() || stat.size > 1024 * 1024) throw new Error("SDK MCP config must be a regular file of at most 1 MiB");
    const config = JSON.parse(fs.readFileSync(filename, "utf8"));
    if (!isRecord(config) || !isRecord(config.mcpServers)) throw new Error("SDK MCP config requires mcpServers");
    for (const [name, entry] of Object.entries(config.mcpServers)) {
      if (!/^[A-Za-z][A-Za-z0-9_.-]*$/.test(name) || ["__proto__", "constructor", "prototype", "locked-tasks"].includes(name)) throw new Error("SDK MCP config contains an invalid or reserved server name");
      if (!isRecord(entry) || typeof entry.url !== "string" || (entry.type !== undefined && !["http", "sse"].includes(String(entry.type))) || entry.command !== undefined || entry.args !== undefined || entry.env !== undefined)
        throw new Error("SDK MCP config only supports compiler-converted HTTP/SSE servers");
      const url = new URL(entry.url);
      if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) throw new Error("SDK MCP server URL is invalid");
      /** @type {import("@github/copilot-sdk").MCPHTTPServerConfig} */
      const server = {
        type: entry.type === "sse" ? "sse" : "http",
        url: entry.url,
      };
      if (entry.headers !== undefined) {
        if (!isRecord(entry.headers)) throw new Error("SDK MCP server headers are invalid");
        server.headers = {};
        for (const [key, value] of Object.entries(entry.headers)) {
          if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(key) || typeof value !== "string" || /[\r\n\0]/.test(value)) throw new Error("SDK MCP server headers are invalid");
          server.headers[key] = value;
        }
      }
      if (entry.tools !== undefined) {
        if (!Array.isArray(entry.tools) || entry.tools.some(tool => typeof tool !== "string" || !tool)) throw new Error("SDK MCP server tools must be strings");
        server.tools = entry.tools;
      }
      if (entry.timeout !== undefined) {
        if (typeof entry.timeout !== "number" || !Number.isSafeInteger(entry.timeout) || entry.timeout <= 0 || entry.timeout > 2_147_483_647) throw new Error("SDK MCP server timeout must be a positive millisecond timeout");
        server.timeout = entry.timeout;
      }
      servers[name] = server;
    }
  }
  const tasks = tasksMCPConfig();
  if (tasks) servers["locked-tasks"] = tasks;
  return servers;
}

module.exports = { loadCopilotSDKMCPConfig };
