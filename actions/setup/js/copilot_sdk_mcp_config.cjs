// @ts-check
"use strict";

const fs = require("fs");

const MAX_MCP_CONFIG_BYTES = 1024 * 1024;

/**
 * @param {unknown} value
 * @returns {value is Record<string, unknown>}
 */
function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/**
 * Read only the compiler-produced gateway configuration, never discover
 * arbitrary process-backed servers from the agent's working directory.
 *
 * @param {string | undefined} filename
 * @returns {Record<string, import("@github/copilot-sdk").MCPServerConfig>}
 */
function loadCopilotSDKMCPConfig(filename) {
  if (typeof filename !== "string" || filename.trim() === "") {
    throw new Error("GH_AW_MCP_CONFIG is required when SDK MCP tools are enabled");
  }

  let contents;
  let fd;
  try {
    fd = fs.openSync(filename, "r");
    const stat = fs.fstatSync(fd);
    if (!stat.isFile() || stat.size > MAX_MCP_CONFIG_BYTES) {
      throw new Error("Invalid MCP configuration file");
    }
    const buffer = Buffer.alloc(MAX_MCP_CONFIG_BYTES + 1);
    let length = 0;
    while (length < buffer.length) {
      const count = fs.readSync(fd, buffer, length, buffer.length - length, null);
      if (count === 0) break;
      length += count;
    }
    if (length > MAX_MCP_CONFIG_BYTES) throw new Error("MCP configuration is too large");
    contents = buffer.subarray(0, length).toString("utf8");
  } catch {
    throw new Error("GH_AW_MCP_CONFIG must be a readable regular file of at most 1 MiB");
  } finally {
    if (fd !== undefined) fs.closeSync(fd);
  }

  /** @type {unknown} */
  let config;
  try {
    config = JSON.parse(contents);
  } catch {
    throw new Error("GH_AW_MCP_CONFIG contains invalid JSON");
  }
  return parseCopilotSDKMCPConfig(config);
}

/**
 * @param {unknown} config
 * @returns {Record<string, import("@github/copilot-sdk").MCPServerConfig>}
 */
function parseCopilotSDKMCPConfig(config) {
  if (!isRecord(config) || !isRecord(config.mcpServers)) {
    throw new Error("SDK MCP configuration must contain a mcpServers object");
  }
  /** @type {[string, import("@github/copilot-sdk").MCPServerConfig][]} */
  const servers = [];
  for (const [name, entry] of Object.entries(config.mcpServers)) {
    if (!/^[A-Za-z0-9_][A-Za-z0-9_.-]*$/.test(name) || ["__proto__", "constructor", "prototype"].includes(name)) {
      throw new Error("SDK MCP configuration contains an invalid server name");
    }
    if (!isRecord(entry) || typeof entry.url !== "string") {
      throw new Error("SDK MCP servers must use gateway HTTP or SSE URLs");
    }
    const type = entry.type ?? "http";
    if ((type !== "http" && type !== "sse") || entry.command !== undefined || entry.args !== undefined || entry.env !== undefined) {
      throw new Error("SDK MCP servers must use gateway HTTP or SSE URLs, not subprocesses");
    }
    let url;
    try {
      url = new URL(entry.url);
    } catch {
      throw new Error("SDK MCP server URL is invalid");
    }
    if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) {
      throw new Error("SDK MCP server URL must use HTTP or HTTPS without embedded credentials");
    }
    /** @type {import("@github/copilot-sdk").MCPHTTPServerConfig} */
    const server = { type, url: entry.url };
    if (entry.tools !== undefined) {
      if (!Array.isArray(entry.tools) || entry.tools.some(tool => typeof tool !== "string" || tool.trim() === "")) {
        throw new Error("SDK MCP server tools must be an array of nonempty names");
      }
      server.tools = entry.tools.slice();
    }
    if (entry.headers !== undefined) {
      if (!isRecord(entry.headers)) throw new Error("SDK MCP server headers must be a string map");
      /** @type {[string, string][]} */
      const headers = [];
      for (const [key, value] of Object.entries(entry.headers)) {
        if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(key) || typeof value !== "string" || /[\0\r\n]/.test(value)) {
          throw new Error("SDK MCP server headers contain an invalid name or value");
        }
        headers.push([key, value]);
      }
      server.headers = Object.fromEntries(headers);
    }
    if (entry.timeout !== undefined) {
      if (typeof entry.timeout !== "number" || !Number.isSafeInteger(entry.timeout) || entry.timeout <= 0 || entry.timeout > 2_147_483_647) {
        throw new Error("SDK MCP server timeout must be a positive, representable millisecond timeout");
      }
      server.timeout = entry.timeout;
    }
    servers.push([name, server]);
  }
  return Object.fromEntries(servers);
}

module.exports = { MAX_MCP_CONFIG_BYTES, loadCopilotSDKMCPConfig, parseCopilotSDKMCPConfig };
