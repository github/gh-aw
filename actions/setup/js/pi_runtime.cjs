// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { writeSecureOutput } = require("./convert_gateway_config_shared.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const DEFAULT_AGENT_DIR = "/tmp/gh-aw/pi-agent-dir";
const DEFAULT_SESSION_DIR = "/tmp/gh-aw/agent/pi-sessions";

/** @param {string} [raw] @returns {Record<string, any>} */
function parsePiConfig(raw = process.env.GH_AW_PI_CONFIG || "{}") {
  let config;
  try {
    config = JSON.parse(raw);
  } catch (error) {
    throw new Error(`Invalid Pi engine.config JSON: ${getErrorMessage(error)}`, { cause: error });
  }
  if (!config || typeof config !== "object" || Array.isArray(config)) throw new Error("Pi engine.config must be a JSON object");
  for (const [key, value] of Object.entries(config)) {
    if (!["settings", "model", "mcp", "session"].includes(key)) throw new Error(`Unknown Pi engine.config field: ${key}`);
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error(`Pi engine.config.${key} must be an object`);
  }
  if (config.mcp?.exposure !== undefined && !["direct", "deferred", "codemode", "hidden"].includes(config.mcp.exposure)) {
    throw new Error("Pi MCP exposure must be direct, deferred, codemode, or hidden");
  }
  return config;
}

/** @param {string} relativePath @returns {string} */
function resolvePiPackageFile(relativePath) {
  const roots = [
    ...(process.env.RUNNER_TEMP ? [path.join(process.env.RUNNER_TEMP, "gh-aw/engine-cli/node_modules")] : []),
    ...(require.resolve.paths("@earendil-works/pi-coding-agent") || []),
    ...(process.env.NODE_PATH || "").split(path.delimiter).filter(Boolean),
  ];
  for (const root of new Set(roots)) {
    const entry = path.join(root, "@earendil-works/pi-coding-agent", relativePath);
    if (fs.existsSync(entry)) return entry;
  }
  throw new Error("Pi coding-agent SDK is missing; install @earendil-works/pi-coding-agent@1.0.0 or newer");
}

/** @returns {Promise<any>} */
async function loadPiSDK() {
  return import(pathToFileURL(resolvePiPackageFile("dist/index.js")).href);
}

/** @param {string} provider @returns {string} */
function nativePiProvider(provider) {
  return { copilot: "github-copilot", github: "github-copilot", codex: "openai", gemini: "google" }[provider] || provider || "github-copilot";
}

/** @param {Record<string, any>} config */
function preparePiRuntime(config = parsePiConfig()) {
  const agentDir = process.env.PI_CODING_AGENT_DIR || DEFAULT_AGENT_DIR;
  const settingsPath = path.join(agentDir, "settings.json");
  let installedSettings = {};
  try {
    installedSettings = JSON.parse(fs.readFileSync(settingsPath, "utf8"));
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ENOENT") throw error;
  }
  const settings = {
    ...installedSettings,
    defaultTools: ["+codemode", "+tool_search"],
    sessionDir: DEFAULT_SESSION_DIR,
    enableInstallTelemetry: false,
    ...config.settings,
    defaultProjectTrust: "never",
  };
  writeSecureOutput(settingsPath, JSON.stringify(settings, null, 2));

  const gatewayPath = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw/mcp-config/mcp-servers.json");
  let gateway = { mcpServers: {} };
  try {
    gateway = JSON.parse(fs.readFileSync(gatewayPath, "utf8"));
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ENOENT") throw error;
  }
  for (const [name, server] of Object.entries(gateway.mcpServers || {})) {
    server.exposure = config.mcp?.exposure || server.exposure || "deferred";
    if (config.mcp?.toolExposure?.[name]) server.toolExposure = config.mcp.toolExposure[name];
  }
  writeSecureOutput(path.join(agentDir, "mcp.json"), JSON.stringify(gateway, null, 2));

  const skillsDir = "/tmp/gh-aw/.pi/skills";
  if (fs.existsSync(skillsDir)) fs.cpSync(skillsDir, path.join(agentDir, "skills"), { recursive: true, dereference: false });
  return { agentDir, settings, config };
}

async function main() {
  preparePiRuntime();
}

if (require.main === module) {
  main().catch(error => {
    process.stderr.write(`[gh-aw/pi-runtime] ${getErrorMessage(error)}\n`);
    process.exitCode = 1;
  });
}

module.exports = { parsePiConfig, loadPiSDK, resolvePiPackageFile, nativePiProvider, preparePiRuntime, DEFAULT_AGENT_DIR, DEFAULT_SESSION_DIR };
