// @ts-check
"use strict";

const fs = require("fs");
const { isIP } = require("net");

/**
 * Translate AWF domain rules into Copilot URL patterns. AWF includes subdomains
 * and both protocols unless restricted; Copilot requires separate approvals.
 * @param {unknown} domains
 * @param {{block?: boolean}} options
 * @returns {string[]}
 */
function domainURLPatterns(domains, { block = false } = {}) {
  if (domains === undefined) return [];
  if (!Array.isArray(domains)) throw new Error("AWF network domains must be an array");
  const patterns = new Set();
  for (const domain of domains) {
    if (typeof domain !== "string" || !domain.trim()) throw new Error("AWF network domain must be a non-empty string");
    let authority = domain.trim().toLowerCase().replace(/\/$/, "");
    const protocols = [];
    if (authority.startsWith("https://")) {
      protocols.push("https");
      authority = authority.slice(8);
    } else if (authority.startsWith("http://")) {
      protocols.push("http");
      authority = authority.slice(7);
    } else {
      protocols.push("http", "https");
    }
    // A global wildcard must never become an unrestricted URL approval.
    if (authority === "*") {
      if (block) throw new Error("Global AWF domain blocks cannot be translated to scoped Copilot URL permissions");
      throw new Error("Global AWF domain allows cannot be translated to scoped Copilot URL permissions");
    }
    let host = authority;
    let port = "";
    if (host.startsWith("[")) {
      const match = /^(\[[^\]]+\])(?::([0-9]+))?$/.exec(host);
      if (!match || isIP(match[1].slice(1, -1)) !== 6) throw new Error("Invalid AWF IPv6 domain rule");
      host = match[1];
      port = match[2] || "";
    } else if (isIP(host) === 6) {
      host = `[${host}]`;
    } else {
      const match = /^([^:]+)(?::([0-9]+))?$/.exec(host);
      if (!match) throw new Error("Invalid AWF domain rule");
      host = match[1].replace(/^\*\./, "").replace(/\.$/, "");
      port = match[2] || "";
      if (!host.split(".").every(label => /^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$/.test(label))) {
        throw new Error("Invalid AWF domain hostname");
      }
    }
    if (port && (Number(port) < 1 || Number(port) > 65535)) throw new Error("Invalid AWF domain port");
    const hosts = host.startsWith("[") || isIP(host) ? [host] : [host, `*.${host}`];
    for (const protocol of protocols) {
      for (const hostname of hosts) {
        patterns.add(`${protocol}://${hostname}${port ? `:${port}` : ""}`);
      }
    }
  }
  return [...patterns];
}

/**
 * Read the finalized AWF network config. The compiler copies it after expanding
 * network_allowed, so it is read at startup, not at compilation. Returns undefined
 * without the AWF sandbox (no API-proxy reflection) or without a network section.
 * @param {{env?: NodeJS.ProcessEnv, configPath: string, fallbackConfigPath?: string}} options
 * @returns {{allowDomains?: unknown, blockDomains?: unknown} | undefined}
 */
function readAWFNetworkConfig({ env = process.env, configPath, fallbackConfigPath }) {
  if (env.AWF_REFLECT_ENABLED !== "1") return undefined;
  let configContents;
  try {
    configContents = fs.readFileSync(configPath, "utf8");
  } catch (err) {
    const errAny = /** @type {any} */ err;
    if (!fallbackConfigPath || errAny?.code !== "ENOENT") {
      const readError = /** @type {Error} */ err;
      throw new Error(`Cannot read AWF config for Copilot URL approvals (${readError.message}); check the generated awf-config.json`);
    }
    try {
      configContents = fs.readFileSync(fallbackConfigPath, "utf8");
    } catch (fallbackErr) {
      const fallbackError = /** @type {Error} */ fallbackErr;
      throw new Error(`Cannot read AWF config for Copilot URL approvals (${fallbackError.message}); check the generated awf-config.json`);
    }
  }
  let config;
  try {
    config = JSON.parse(configContents);
  } catch (err) {
    const parseError = /** @type {Error} */ err;
    throw new Error(`Cannot parse AWF config for Copilot URL approvals (${parseError.message}); check the generated awf-config.json`);
  }
  if (!config || typeof config !== "object" || Array.isArray(config)) throw new Error("Invalid AWF config for Copilot URL approvals");
  const network = config.network;
  if (network === undefined) return undefined;
  if (!network || typeof network !== "object" || Array.isArray(network)) throw new Error("Invalid AWF network config for Copilot URL approvals");
  return network;
}

/**
 * Add Copilot CLI --allow-url/--deny-url flags for the AWF network rules.
 * @param {string[]} args
 * @param {{env?: NodeJS.ProcessEnv, sdkMode?: boolean, configPath: string, fallbackConfigPath?: string}} options
 * @returns {string[]}
 */
function addCopilotURLPermissions(args, { env = process.env, sdkMode = false, configPath, fallbackConfigPath }) {
  if (sdkMode) return args;
  const network = readAWFNetworkConfig({ env, configPath, fallbackConfigPath });
  if (!network) return args;
  const permissions = [];
  for (const pattern of domainURLPatterns(network.allowDomains)) permissions.push("--allow-url", pattern);
  for (const pattern of domainURLPatterns(network.blockDomains, { block: true })) permissions.push("--deny-url", pattern);
  // Keep any explicit CLI argument delimiter in its original role.
  const delimiter = args.indexOf("--");
  const split = delimiter < 0 ? args.length : delimiter;
  return [...args.slice(0, split), ...permissions, ...args.slice(split)];
}

/**
 * Whether a Copilot URL pattern from domainURLPatterns matches a parsed URL. A pattern
 * without a port matches any port, like AWF domain rules, which never restrict ports.
 * @param {string} pattern
 * @param {URL} url
 */
function urlPatternMatches(pattern, url) {
  const separator = pattern.indexOf("://");
  if (pattern.slice(0, separator + 1) !== url.protocol) return false;
  const authority = pattern.slice(separator + 3);
  // Hosts are bracketed IPv6 literals or colon-free names, so a port follows the last ':' after any ']'.
  const portIndex = authority.lastIndexOf(":");
  const hasPort = portIndex > authority.lastIndexOf("]");
  const host = hasPort ? authority.slice(0, portIndex) : authority;
  if (hasPort && authority.slice(portIndex + 1) !== (url.port || (url.protocol === "https:" ? "443" : "80"))) return false;
  if (host.startsWith("*.")) return url.hostname.endsWith(host.slice(1));
  return url.hostname === host;
}

/**
 * Build a URL approval check for Copilot SDK `url` permission requests from the AWF
 * network rules: the same scope the CLI engine receives as --allow-url/--deny-url.
 * Block rules take precedence; anything unparseable or outside http(s) is denied.
 * @param {{allowDomains?: unknown, blockDomains?: unknown} | undefined} network
 * @returns {(url: unknown) => boolean}
 */
function createURLPermissionCheck(network) {
  if (!network) return () => false;
  const allow = domainURLPatterns(network.allowDomains);
  const block = domainURLPatterns(network.blockDomains, { block: true });
  return value => {
    if (typeof value !== "string") return false;
    let url;
    try {
      url = new URL(value);
    } catch {
      return false;
    }
    if (url.protocol !== "http:" && url.protocol !== "https:") return false;
    if (block.some(pattern => urlPatternMatches(pattern, url))) return false;
    return allow.some(pattern => urlPatternMatches(pattern, url));
  };
}

module.exports = { domainURLPatterns, addCopilotURLPermissions, readAWFNetworkConfig, createURLPermissionCheck };
