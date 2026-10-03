// @ts-check
/// <reference types="@actions/github-script" />
"use strict";

const fs = require("fs");
const path = require("path");

const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Rewrite a gateway URL to use the configured domain and port.
 * Replaces http://<anything>/mcp/ with http://<domain>:<port>/mcp/.
 *
 * @param {string} url - Original URL from gateway output
 * @param {string} urlPrefix - Target URL prefix (e.g., http://host.docker.internal:80)
 * @returns {string} Rewritten URL
 */
function rewriteUrl(url, urlPrefix) {
  return url.replace(/^http:\/\/[^/]+\/mcp\//, `${urlPrefix}/mcp/`);
}

/**
 * Shallow-clone a gateway entry, apply provider-specific mutations, and rewrite URL.
 * Note: only the top-level object is cloned; nested fields (e.g., `headers`) are shared references.
 *
 * @param {Record<string, unknown>} entry
 * @param {string} urlPrefix
 * @param {(transformed: Record<string, unknown>) => void} [mutate]
 * @returns {Record<string, unknown>}
 */
function normalizeGatewayEntry(entry, urlPrefix, mutate) {
  const transformed = { ...entry };
  if (mutate) {
    mutate(transformed);
  }
  if (typeof transformed.url === "string") {
    transformed.url = rewriteUrl(transformed.url, urlPrefix);
  }
  return transformed;
}

/**
 * @param {string} name
 * @returns {string}
 */
function requireEnvVar(name) {
  const value = process.env[name];
  if (!value) {
    throw new Error(`${name} environment variable is required`);
  }
  return value;
}

/**
 * @param {{ extraRequiredEnv?: string[], keepCLIMountedServers?: boolean }} [options]
 * @returns {{
 *   gatewayOutput: string;
 *   domain: string;
 *   port: string;
 *   urlPrefix: string;
 *   cliServers: Set<string>;
 *   servers: Record<string, Record<string, unknown>>;
 *   extraEnv: Record<string, string>;
 * }}
 */
function loadGatewayContext(options = {}) {
  const extraRequiredEnv = options.extraRequiredEnv || [];
  const gatewayOutput = requireEnvVar("MCP_GATEWAY_OUTPUT");
  if (!fs.existsSync(gatewayOutput)) {
    throw new Error(`Gateway output file not found: ${gatewayOutput}`);
  }

  const domain = requireEnvVar("MCP_GATEWAY_DOMAIN");
  const port = requireEnvVar("MCP_GATEWAY_PORT");

  /** @type {Record<string, string>} */
  const extraEnv = {};
  for (const envVar of extraRequiredEnv) {
    extraEnv[envVar] = requireEnvVar(envVar);
  }

  /** @type {Set<string>} */
  let cliServers;
  try {
    cliServers = new Set(JSON.parse(process.env.GH_AW_MCP_CLI_SERVERS || "[]"));
  } catch (err) {
    throw new Error("Failed to parse GH_AW_MCP_CLI_SERVERS: " + getErrorMessage(err), { cause: err });
  }
  if (options.keepCLIMountedServers) cliServers.clear();

  /** @type {Record<string, unknown>} */
  let config;
  try {
    config = JSON.parse(fs.readFileSync(gatewayOutput, "utf8"));
  } catch (err) {
    throw new Error("Failed to parse gateway output file " + gatewayOutput + ": " + getErrorMessage(err), { cause: err });
  }
  const rawServers = config.mcpServers;
  /** @type {Record<string, Record<string, unknown>>} */
  let servers = {};
  if (rawServers && typeof rawServers === "object" && !Array.isArray(rawServers)) {
    servers = Object.fromEntries(Object.entries(rawServers));
  }

  return {
    gatewayOutput,
    domain,
    port,
    urlPrefix: `http://${domain}:${port}`,
    cliServers,
    servers,
    extraEnv,
  };
}

/**
 * @param {Set<string>} cliServers
 */
function logCLIFilters(cliServers) {
  if (cliServers.size > 0) {
    core.info(`CLI-mounted servers to filter: ${[...cliServers].join(", ")}`);
  }
}

/**
 * @param {Record<string, Record<string, unknown>>} servers
 * @param {Set<string>} cliServers
 * @param {(name: string, value: Record<string, unknown>) => Record<string, unknown>} transformServer
 * @returns {Record<string, Record<string, unknown>>}
 */
function filterAndTransformServers(servers, cliServers, transformServer) {
  /** @type {Record<string, Record<string, unknown>>} */
  const result = {};
  for (const [name, value] of Object.entries(servers)) {
    if (cliServers.has(name)) continue;
    const entry = { ...value };
    result[name] = transformServer(name, entry);
  }
  return result;
}

/**
 * @param {Record<string, Record<string, unknown>>} servers
 * @param {number} includedCount
 */
function logServerStats(servers, includedCount) {
  const totalCount = Object.keys(servers).length;
  const filteredCount = totalCount - includedCount;
  core.info(`Servers: ${includedCount} included, ${filteredCount} filtered (CLI-mounted)`);
}

/**
 * @param {string} outputPath
 * @param {string} output
 */
function writeSecureOutput(outputPath, output) {
  try {
    fs.mkdirSync(path.dirname(outputPath), { recursive: true });
    fs.writeFileSync(outputPath, output, { mode: 0o600 });
    fs.chmodSync(outputPath, 0o600);
  } catch (err) {
    throw new Error(`Failed to write file ${outputPath}: ${getErrorMessage(err)}`, { cause: err });
  }
}

/**
 * Run the common gateway configuration conversion pipeline.
 *
 * `getTargetDomain` and `getUrlPrefix` are intentionally separate so that
 * engines can diverge the log label from the actual URL prefix (for example,
 * Codex logs `host.docker.internal` but builds URLs with `172.30.0.1`).
 * When both are provided they MUST be kept consistent; when omitted, both
 * default to `context.domain` / `context.urlPrefix` respectively.
 *
 * @param {{
 *   format: string;
 *   engine: string;
 *   outputPath: string | ((context: ReturnType<typeof loadGatewayContext>) => string);
 *   contextOptions?: { extraRequiredEnv?: string[], keepCLIMountedServers?: boolean };
 *   getTargetDomain?: (context: ReturnType<typeof loadGatewayContext>) => string;
 *   getUrlPrefix?: (context: ReturnType<typeof loadGatewayContext>) => string;
 *   transformServer: (name: string, entry: Record<string, unknown>, urlPrefix: string, context: ReturnType<typeof loadGatewayContext>) => Record<string, unknown>;
 *   serialize: (servers: Record<string, Record<string, unknown>>, context: ReturnType<typeof loadGatewayContext>, urlPrefix: string) => string;
 * }} options
 * @returns {string}
 */
function runGatewayConversion(options) {
  const context = loadGatewayContext(options.contextOptions);
  const targetDomain = options.getTargetDomain ? options.getTargetDomain(context) : context.domain;

  core.info(`Converting gateway configuration to ${options.format} format...`);
  core.info(`Input: ${context.gatewayOutput}`);
  core.info(`Target domain: ${targetDomain}:${context.port}`);

  const urlPrefix = options.getUrlPrefix ? options.getUrlPrefix(context) : context.urlPrefix;
  logCLIFilters(context.cliServers);
  const servers = filterAndTransformServers(context.servers, context.cliServers, (name, entry) => options.transformServer(name, entry, urlPrefix, context));
  const output = options.serialize(servers, context, urlPrefix);

  logServerStats(context.servers, Object.keys(servers).length);
  const outputPath = typeof options.outputPath === "function" ? options.outputPath(context) : options.outputPath;
  writeSecureOutput(outputPath, output);

  core.info(`${options.engine} configuration written to ${outputPath}`);
  core.info(`Converted servers: ${Object.keys(servers).join(", ") || "(none)"}`);
  return output;
}

module.exports = {
  rewriteUrl,
  normalizeGatewayEntry,
  loadGatewayContext,
  logCLIFilters,
  filterAndTransformServers,
  logServerStats,
  writeSecureOutput,
  runGatewayConversion,
};
