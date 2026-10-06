// @ts-check
"use strict";

// Ensures global.core is available when running outside github-script context
require("./shim.cjs");

/**
 * convert_gateway_config_codex.cjs
 *
 * Converts the MCP gateway's standard HTTP-based configuration to the TOML
 * format expected by Codex. Reads the gateway output JSON, filters out
 * CLI-mounted servers, resolves host.docker.internal to 172.30.0.1 for Rust
 * DNS compatibility, and writes the result to ${RUNNER_TEMP}/gh-aw/mcp-config/config.toml.
 *
 * Required environment variables:
 * - MCP_GATEWAY_OUTPUT: Path to gateway output configuration file
 * - MCP_GATEWAY_DOMAIN: Domain for MCP server URLs (e.g., host.docker.internal)
 * - MCP_GATEWAY_PORT: Port for MCP gateway (e.g., 80)
 * - RUNNER_TEMP: GitHub Actions runner temp directory
 *
 * Optional:
 * - GH_AW_MCP_CLI_SERVERS: JSON array of server names to exclude from agent config
 */

const path = require("path");
const fs = require("fs");
const { runGatewayConversion, writeSecureOutput } = require("./convert_gateway_config_shared.cjs");
const { buildConfig, serializeConfig, tomlValue, loadCompiledConfig, mergeConfig, expandConfigEnv, isTable, directToolCatalog } = require("./codex_config.cjs");

/**
 * @param {string} name
 * @param {Record<string, unknown>} value
 * @param {string} urlPrefix
 * @returns {string}
 */
function toCodexTomlSection(name, value, urlPrefix) {
  const url = `${urlPrefix}/mcp/${encodeURIComponent(name)}`;
  const rawHeaders = value.headers;
  /** @type {Record<string, string>} */
  const headers = rawHeaders && typeof rawHeaders === "object" && !Array.isArray(rawHeaders) ? Object.fromEntries(Object.entries(rawHeaders).filter(([, headerValue]) => typeof headerValue === "string")) : {};
  const authKey = headers.Authorization || "";
  let section = `[mcp_servers.${/^[A-Za-z0-9_-]+$/.test(name) ? name : JSON.stringify(name)}]\n`;
  section += `url = ${tomlValue(url)}\n`;
  section += `http_headers = { Authorization = ${tomlValue(authKey)} }\n`;
  section += "\n";
  return section;
}

function main() {
  if (process.argv.includes("--direct-tools")) {
    const home = process.env.CODEX_HOME;
    if (!home) throw new Error("CODEX_HOME is required to configure Codex direct tools");
    const catalog = directToolCatalog(JSON.parse(fs.readFileSync(0, "utf8")));
    const output = JSON.stringify(catalog);
    writeSecureOutput(path.join(home, "models.json"), output);
    return output;
  }
  const outputPath = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/config.toml");
  if (process.argv.includes("--bootstrap")) {
    const compiled = loadCompiledConfig();
    const overrides = expandConfigEnv(compiled.overrides);
    if (!isTable(overrides)) throw new Error("Invalid Codex configuration overrides");
    compiled.overrides = overrides;
    compiled.envExpanded = true;
    let config = mergeConfig(compiled.defaults, compiled.overrides);
    if (compiled.disablePlugins) config = mergeConfig(config, { features: { plugins: false } });
    const output = serializeConfig(config);
    writeSecureOutput(outputPath, output);
    writeSecureOutput(path.join(path.dirname(outputPath), "codex-config.json"), JSON.stringify(compiled));
    return output;
  }
  if (process.argv.includes("--config-only")) {
    const output = serializeConfig(buildConfig({}, ""));
    writeSecureOutput(outputPath, output);
    const home = process.env.CODEX_HOME;
    if (!home) throw new Error("CODEX_HOME is required to configure Codex");
    writeSecureOutput(path.join(home, "config.toml"), output);
    return output;
  }
  return runGatewayConversion({
    format: "Codex TOML",
    engine: "Codex",
    outputPath,
    getUrlPrefix: ({ domain, port }) => {
      if (domain === "host.docker.internal") {
        core.info("Resolving host.docker.internal to gateway IP: 172.30.0.1");
        return `http://172.30.0.1:${port}`;
      }
      return `http://${domain}:${port}`;
    },
    transformServer: (_name, entry) => entry,
    serialize: (servers, _context, urlPrefix) => {
      return serializeConfig(buildConfig(servers, urlPrefix));
    },
  });
}

if (require.main === module) {
  main();
}

module.exports = { toCodexTomlSection, main };
