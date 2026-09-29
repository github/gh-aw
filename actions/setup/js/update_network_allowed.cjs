// @ts-check
"use strict";

/**
 * update_network_allowed.cjs
 *
 * Updates the AWF config file's network.allowDomains list based on the
 * GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED environment variable and the configured
 * OTLP collector endpoints.
 *
 * The variable contains a comma-separated list of ecosystem tokens (e.g. "node,python")
 * or raw domain names. Each token is expanded to its known set of domains using the
 * ecosystem map embedded via the GH_AW_ECOSYSTEM_MAP_JSON environment variable.
 * Unknown tokens are treated as raw domain names.
 *
 * Environment variables:
 *   RUNNER_TEMP                       - GitHub Actions runner temp directory
 *   GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED - Comma-separated allowed tokens/domains
 *   GH_AW_ECOSYSTEM_MAP_JSON          - JSON object mapping ecosystem names to domain arrays
 *   GH_AW_OTLP_ENDPOINTS              - JSON array of OTLP endpoint entries
 *   OTEL_EXPORTER_OTLP_ENDPOINT       - Primary OTLP endpoint (fallback)
 *
 * Exit codes:
 *   0 — Success (including when no tokens are specified)
 *   1 — Fatal error (missing RUNNER_TEMP, unreadable/invalid config file, write failure)
 */

const fs = require("fs");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");

const NETWORK_ALLOWED_ENV_VAR = "GH_AW_WORKFLOW_CALL_NETWORK_ALLOWED";
/** @typedef {{allowDomains?: string[]}} AWFNetworkConfig */
/** @typedef {Record<string, unknown> & {network?: AWFNetworkConfig | unknown}} AWFConfig */

/**
 * @param {string} endpoint
 * @returns {string}
 */
function getOTLPEndpointDomain(endpoint) {
  try {
    const parsed = new URL(endpoint);
    if ((parsed.protocol === "http:" || parsed.protocol === "https:") && parsed.hostname) {
      return parsed.hostname;
    }
  } catch {
    // Ignore malformed endpoint values and leave firewall behavior unchanged.
  }
  return "";
}

/**
 * @param {any} value
 * @returns {AWFNetworkConfig}
 */
function toNetworkConfig(value) {
  return value;
}

/**
 * @param {any} value
 * @returns {string[]}
 */
function toStringArray(value) {
  return value;
}

/**
 * @returns {Promise<void>}
 */
async function main() {
  const runnerTemp = process.env.RUNNER_TEMP;
  if (!runnerTemp) {
    process.stderr.write("RUNNER_TEMP is not set\n");
    process.exit(1);
  }

  const configPath = path.join(runnerTemp, "gh-aw", "awf-config.json");

  /** @type {AWFConfig} */
  let config;
  try {
    config = JSON.parse(fs.readFileSync(configPath, "utf8"));
  } catch (/** @type {unknown} */ err) {
    const errCode = err && typeof err === "object" && "code" in err ? err.code : undefined;
    const errMessage = getErrorMessage(err);
    if (errCode === "ENOENT") {
      process.stderr.write(`Missing AWF config file at ${configPath}\n`);
    } else if (err instanceof SyntaxError) {
      process.stderr.write(`Invalid AWF config JSON at ${configPath}: ${errMessage}\n`);
    } else {
      process.stderr.write(`Failed to read AWF config file at ${configPath}: ${errMessage}\n`);
    }
    process.exit(1);
  }

  const networkAllowed = process.env[NETWORK_ALLOWED_ENV_VAR] || "";
  const tokens = networkAllowed
    .split(",")
    .map(t => t.trim())
    .filter(t => t.length > 0);

  /** @type {string[]} */
  const endpointDomains = [];
  const addEndpointDomain = endpoint => {
    if (typeof endpoint === "string") {
      const domain = getOTLPEndpointDomain(endpoint);
      if (domain) {
        endpointDomains.push(domain);
      }
    }
  };
  addEndpointDomain(process.env.OTEL_EXPORTER_OTLP_ENDPOINT || "");
  if (process.env.GH_AW_OTLP_ENDPOINTS) {
    try {
      const endpoints = JSON.parse(process.env.GH_AW_OTLP_ENDPOINTS);
      if (Array.isArray(endpoints)) {
        for (const endpoint of endpoints) {
          if (endpoint && typeof endpoint === "object" && !Array.isArray(endpoint)) {
            addEndpointDomain(endpoint.url);
          }
        }
      }
    } catch {
      // Intentionally ignore malformed endpoint-list JSON; primary endpoint handling continues.
    }
  }

  if (tokens.length > 0 || endpointDomains.length > 0) {
    /** @type {Record<string, string[]>} */
    let ecosystemMap = {};
    if (tokens.length > 0) {
      const ecosystemMapJSON = process.env.GH_AW_ECOSYSTEM_MAP_JSON;
      if (!ecosystemMapJSON) {
        process.stderr.write("GH_AW_ECOSYSTEM_MAP_JSON is not set\n");
        process.exit(1);
      }

      try {
        ecosystemMap = JSON.parse(ecosystemMapJSON);
      } catch (/** @type {unknown} */ err) {
        const errMessage = getErrorMessage(err);
        process.stderr.write(`Invalid GH_AW_ECOSYSTEM_MAP_JSON: ${errMessage}\n`);
        process.exit(1);
      }
    }

    // Arrays are treated as malformed for this field and reset to an object shape.
    if (!config.network || typeof config.network !== "object" || Array.isArray(config.network)) {
      config.network = {};
    }
    const network = toNetworkConfig(config.network);
    if (!Array.isArray(network.allowDomains)) {
      network.allowDomains = [];
    }
    const allowDomains = toStringArray(network.allowDomains);
    const seen = new Set(allowDomains);

    for (const domain of endpointDomains) {
      if (!seen.has(domain)) {
        allowDomains.push(domain);
        seen.add(domain);
      }
    }

    for (const token of tokens) {
      const domains = ecosystemMap[token] || [token];
      for (const domain of domains) {
        if (!seen.has(domain)) {
          allowDomains.push(domain);
          seen.add(domain);
        }
      }
    }
  }

  try {
    fs.writeFileSync(configPath, JSON.stringify(config) + "\n");
  } catch (/** @type {unknown} */ err) {
    const errMessage = getErrorMessage(err);
    process.stderr.write(`Failed to write AWF config file at ${configPath}: ${errMessage}\n`);
    process.exit(1);
  }
}

module.exports = { main };

if (require.main === module) {
  main().catch((/** @type {unknown} */ err) => {
    const errMessage = getErrorMessage(err);
    process.stderr.write(`Error: ${errMessage}\n`);
    process.exit(1);
  });
}
