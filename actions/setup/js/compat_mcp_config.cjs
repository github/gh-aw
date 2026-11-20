// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { getErrorMessage } = require("./error_helpers.cjs");

const COMPAT_ENV_NAMES = ["HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy", "NODE_EXTRA_CA_CERTS", "SSL_CERT_FILE", "SSL_CERT_DIR"];

/** @param {any} server @returns {boolean} */
function isManagedCompatServer(server) {
  return Boolean(
    server &&
    ["stdio", "local"].includes(server.type) &&
    ["node", process.execPath].includes(server.command) &&
    Array.isArray(server.args) &&
    server.args.length === 1 &&
    server.args[0] === path.join(__dirname, "compat_mcp_server.cjs") &&
    typeof server.env?.NODE_PATH === "string"
  );
}

/**
 * Bind only proxy and CA settings from the AWF runtime, not the setup host.
 * MCP clients may otherwise strip these variables from stdio child processes.
 * @param {Record<string, any>} config
 * @param {NodeJS.ProcessEnv} [env]
 */
function bindCompatEnvironment(config, env = process.env) {
  const compat = config.mcpServers?.compat;
  if (!isManagedCompatServer(compat)) {
    throw new Error("Missing compiler-managed compat MCP server");
  }
  const runtimeEnv = { NODE_PATH: compat.env.NODE_PATH };
  for (const name of COMPAT_ENV_NAMES) {
    if (env[name]) runtimeEnv[name] = env[name];
  }
  if (!runtimeEnv.HTTP_PROXY && !runtimeEnv.HTTPS_PROXY && !runtimeEnv.http_proxy && !runtimeEnv.https_proxy) {
    throw new Error("compat WebFetch requires an AWF proxy at agent runtime");
  }
  return { ...config, mcpServers: { ...config.mcpServers, compat: { ...compat, command: process.execPath, env: runtimeEnv } } };
}

/**
 * @param {string} source
 * @param {string} destination
 */
function prepareCompatMCPConfig(source, destination) {
  const input = fs.openSync(source, fs.constants.O_RDONLY | fs.constants.O_NOFOLLOW);
  let config;
  try {
    if (!fs.fstatSync(input).isFile()) throw new Error("MCP configuration must be a regular file");
    config = bindCompatEnvironment(JSON.parse(fs.readFileSync(input, "utf8")));
  } catch (error) {
    throw new Error(`Failed to read compat MCP configuration: ${getErrorMessage(error)}`, { cause: error });
  } finally {
    fs.closeSync(input);
  }
  let output;
  try {
    fs.mkdirSync(path.dirname(destination), { recursive: true });
    output = fs.openSync(destination, fs.constants.O_WRONLY | fs.constants.O_CREAT | fs.constants.O_NOFOLLOW, 0o600);
  } catch (error) {
    throw new Error(`Failed to open compat MCP configuration destination: ${getErrorMessage(error)}`, { cause: error });
  }
  try {
    const stat = fs.fstatSync(output);
    if (!stat.isFile() || stat.nlink !== 1) throw new Error("MCP configuration destination must be a regular, non-linked file");
    fs.fchmodSync(output, 0o600);
    fs.ftruncateSync(output);
    fs.writeFileSync(output, JSON.stringify(config));
  } catch (error) {
    throw new Error(`Failed to write compat MCP configuration: ${getErrorMessage(error)}`, { cause: error });
  } finally {
    fs.closeSync(output);
  }
}

if (require.main === module) {
  try {
    const [source, destination] = process.argv.slice(2);
    if (!source || !destination) throw new Error("Expected source and destination MCP configuration paths");
    prepareCompatMCPConfig(source, destination);
  } catch (error) {
    process.stderr.write(`compat MCP configuration: ${getErrorMessage(error)}\n`);
    process.exitCode = 1;
  }
}

module.exports = { COMPAT_ENV_NAMES, isManagedCompatServer, bindCompatEnvironment, prepareCompatMCPConfig };
