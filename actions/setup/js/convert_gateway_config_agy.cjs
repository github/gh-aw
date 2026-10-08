// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { loadGatewayContext, normalizeGatewayEntry, writeSecureOutput } = require("./convert_gateway_config_shared.cjs");
const context = loadGatewayContext({ extraRequiredEnv: ["GITHUB_WORKSPACE"] });
const raw = JSON.parse(fs.readFileSync(context.gatewayOutput, "utf8"));
if (!raw.mcpServers || typeof raw.mcpServers !== "object" || Array.isArray(raw.mcpServers)) {
  throw new Error("Agy MCP gateway configuration requires an mcpServers object");
}
const servers = Object.create(null);
for (const [name, entry] of Object.entries(raw.mcpServers)) {
  if (context.cliServers.has(name)) continue;
  if (!entry || typeof entry !== "object" || Array.isArray(entry)) throw new Error("Agy MCP server must be an object");
  if (typeof entry.url !== "string" || entry.command !== undefined) throw new Error("Agy requires gateway-backed HTTP MCP servers");
  const transformed = normalizeGatewayEntry(entry, context.urlPrefix);
  if (typeof transformed.url !== "string") throw new Error("Agy MCP endpoint URL is required");
  const url = new URL(transformed.url);
  if (url.origin !== new URL(context.urlPrefix).origin || !url.pathname.startsWith("/mcp/") || url.username || url.password || url.search || url.hash) {
    throw new Error("Agy MCP endpoints must use the configured gateway");
  }
  const server = { serverUrl: transformed.url };
  if (transformed.headers !== undefined) {
    if (!transformed.headers || typeof transformed.headers !== "object" || Array.isArray(transformed.headers) || Object.values(transformed.headers).some(value => typeof value !== "string")) {
      throw new Error("Agy MCP headers must be a string-valued object");
    }
    server.headers = transformed.headers;
  }
  servers[name] = server;
}
const directory = path.join(context.extraEnv.GITHUB_WORKSPACE, ".agents");
const output = path.join(directory, "mcp_config.json");
for (const target of [directory, output]) {
  if (fs.lstatSync(target, { throwIfNoEntry: false })?.isSymbolicLink()) throw new Error("Agy MCP configuration must not use symlinks");
}
writeSecureOutput(output, JSON.stringify({ mcpServers: servers }, null, 2));
