// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { execFileSync } = require("node:child_process");
const { writeSecureOutput } = require("./convert_gateway_config_shared.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { INLINE_SKILL_MANIFEST, resolveWorkflowSkillPaths, writeWorkflowSkillArgs } = require("./pi_workflow_skills.cjs");

const DEFAULT_AGENT_DIR = "/tmp/gh-aw/pi-agent-dir";
const DEFAULT_SESSION_DIR = "/tmp/gh-aw/agent/pi-sessions";

/** @param {string} [command] @param {typeof execFileSync} [execute] @param {string} [minimumVersion] */
function verifyPiVersion(command = process.env.GH_AW_PI_COMMAND || "pi", execute = execFileSync, minimumVersion = "1.0.0") {
  let version;
  try {
    version = execute(command, ["--version"], { encoding: "utf8", timeout: 10000, maxBuffer: 4096 }).trim();
  } catch (error) {
    throw new Error(`Cannot verify installed Pi version: ${getErrorMessage(error)}`, { cause: error });
  }
  const match = /^(?:pi\s+)?v?(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$/i.exec(version);
  const minimum = minimumVersion.split(".").map(Number);
  const comparison = match ? [1, 2, 3].map((index, i) => Number(match[index]) - minimum[i]).find(value => value !== 0) || 0 : -1;
  if (!match || comparison < 0 || (comparison === 0 && match[4])) {
    throw new Error(`Pi v${minimumVersion} or newer is required${minimumVersion === "1.3.0" ? " for effort=max" : ""}; installed version is ${version}`);
  }
}

/** @param {string|null|undefined} effort */
function verifyPiThinkingEffort(effort) {
  if (effort === "max") verifyPiVersion(undefined, undefined, "1.3.0");
}

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
  const packageRoots = roots.map(root => path.join(root, "@earendil-works/pi-coding-agent"));
  if (process.env.GH_AW_PI_PACKAGE_ROOT) packageRoots.unshift(process.env.GH_AW_PI_PACKAGE_ROOT);
  for (const bin of (process.env.PATH || "").split(path.delimiter)) {
    if (!bin) continue;
    const executable = path.join(bin, "pi");
    if (!fs.existsSync(executable)) continue;
    let root = path.dirname(fs.realpathSync(executable));
    for (let depth = 0; depth < 4; depth++) {
      const manifest = path.join(root, "package.json");
      if (fs.existsSync(manifest)) {
        const pkg = JSON.parse(fs.readFileSync(manifest, "utf8"));
        if (pkg.name === "@earendil-works/pi-coding-agent") {
          packageRoots.push(root);
          break;
        }
      }
      root = path.dirname(root);
    }
  }
  for (const root of new Set(packageRoots)) {
    const entry = path.join(root, relativePath);
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
/**
 * Point GH_AW_PI_MODEL at the main model chosen by the shared "resolve configured model"
 * step in pi_models_json.cjs, so the SDK and RPC drivers launch the resolved model
 * rather than the configured alias. The configured provider prefix is kept.
 */
function applyResolvedPiModel() {
  if (process.env.GH_AW_PI_SUBAGENT_CHILD === "1" || !process.env.GH_AW_PI_MODEL) return;
  let resolved = "";
  try {
    resolved = fs.readFileSync(process.env.GH_AW_PI_MODEL_FILE || "/tmp/gh-aw/pi-model", "utf8").trim();
  } catch {
    return;
  }
  if (!resolved) return;
  const configured = process.env.GH_AW_PI_MODEL;
  const slash = configured.indexOf("/");
  process.env.GH_AW_PI_MODEL = slash > 0 ? `${configured.slice(0, slash)}/${resolved}` : resolved;
}

function preparePiRuntime(config = parsePiConfig()) {
  const agentDir = process.env.PI_CODING_AGENT_DIR || DEFAULT_AGENT_DIR;
  const routingSelection = readPiModelRoutingSelection(agentDir);
  if (!routingSelection) applyResolvedPiModel();
  const settingsPath = path.join(agentDir, "settings.json");
  let installedSettings = {};
  try {
    installedSettings = JSON.parse(fs.readFileSync(settingsPath, "utf8"));
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ENOENT") throw error;
  }
  const settings = {
    ...installedSettings,
    defaultTools: [...new Set([...(Array.isArray(installedSettings.defaultTools) ? installedSettings.defaultTools : []), "+codemode", "+tool_search"])],
    sessionDir: DEFAULT_SESSION_DIR,
    enableInstallTelemetry: false,
    ...config.settings,
    retry: {
      enabled: true,
      maxRetries: 2,
      baseDelayMs: 1000,
      maxAgentDelayMs: 30000,
      ...installedSettings.retry,
      ...config.settings?.retry,
    },
    defaultProjectTrust: "never",
  };
  if (routingSelection) {
    if (routingSelection.mapped_effort) settings.defaultThinkingLevel = routingSelection.mapped_effort;
    else delete settings.defaultThinkingLevel;
  }
  verifyPiThinkingEffort(settings.defaultThinkingLevel);
  for (const rawArgs of [process.env.GH_AW_PI_ARGS, process.env.GH_AW_PI_SUBAGENT_ARGS]) {
    if (!rawArgs) continue;
    const args = JSON.parse(rawArgs);
    if (Array.isArray(args)) {
      for (let i = 0; i < args.length; i++) {
        if (args[i] === "--") break;
        if (args[i] === "--thinking") verifyPiThinkingEffort(args[++i]);
        else if (args[i] === "--thinking=max") verifyPiThinkingEffort("max");
      }
    }
  }
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

  stagePiArtifacts(agentDir);
  // Bare mode disables skill discovery; explicitly load only the inline skills activation staged.
  const skillPaths = process.env.GH_AW_PI_BARE === "true" ? resolveWorkflowSkillPaths(agentDir) : [];
  writeWorkflowSkillArgs(agentDir, skillPaths);
  return { agentDir, settings, config, routingSelection, skillPaths };
}

/** @param {string} agentDir */
function stagePiArtifacts(agentDir) {
  for (const kind of ["skills", "agents"]) {
    const source = path.join(process.env.GH_AW_PI_STAGING_DIR || "/tmp/gh-aw/.pi", kind);
    if (fs.existsSync(source)) fs.cpSync(source, path.join(agentDir, kind), { recursive: true, dereference: false, filter: src => path.basename(src) !== INLINE_SKILL_MANIFEST });
  }
}

/**
 * @param {string} agentDir
 * @returns {{wire_model: string, mapped_effort: string|null}|null}
 */
function readPiModelRoutingSelection(agentDir) {
  if (process.env.GH_AW_MODEL_ROUTING !== "1") return null;
  const selectionPath = path.join(agentDir, "model-routing-selection.json");
  let selection;
  try {
    selection = JSON.parse(fs.readFileSync(selectionPath, "utf8"));
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`AWF model routing selection is unavailable to Pi: ${message}`);
  }
  if (typeof selection?.wire_model !== "string" || !selection.wire_model) {
    throw new Error("AWF model routing selection has no wire model; refusing to start Pi");
  }
  return { wire_model: selection.wire_model, mapped_effort: typeof selection.mapped_effort === "string" ? selection.mapped_effort : null };
}

/**
 * Run the shared "resolve configured model" step for the main model when AWF is disabled.
 * Without AWF there is no live /reflect catalogue: exact ids, and alias names that are
 * bundled model ids (gpt-5-mini), pass through; other aliases fail with catalog_unavailable.
 * @param {NodeJS.ProcessEnv} [env]
 */
async function resolvePiModelWithoutAWF(env = process.env) {
  if (env.AWF_REFLECT_ENABLED === "1" || !env.GH_AW_PI_MODEL) return null;
  const { loadModelAliasMap, resolveEngineModel } = require("./resolve_model_alias.cjs");
  return resolveEngineModel({
    engine: "pi",
    role: "main",
    configuredModel: env.GH_AW_PI_MODEL,
    aliasMap: loadModelAliasMap({ envVar: "GH_AW_PI_MODEL_ALIASES", env }),
    reflectData: null,
    preserveAuto: true,
    env,
    logger: message => process.stderr.write(`[gh-aw/pi-runtime] ${message}\n`),
  });
}

async function main() {
  verifyPiVersion();
  const { agentDir } = preparePiRuntime();
  await resolvePiModelWithoutAWF();
  if (process.env.AWF_REFLECT_ENABLED !== "1" && fs.existsSync(path.join(agentDir, "agents"))) {
    const sdk = await loadPiSDK();
    const provider = process.env.GH_AW_PI_NATIVE_PROVIDER || nativePiProvider("copilot");
    const { preparePiSubagents } = require("./pi_subagent_config.cjs");
    preparePiSubagents({
      agentDir,
      sdk,
      provider,
      reflectData: null,
      noCatalog: true,
      gateway: false,
      parentModel: process.env.GH_AW_PI_MODEL || "",
      logger: message => process.stderr.write(`[gh-aw/pi-subagent] ${message}\n`),
    });
  }
}

if (require.main === module) {
  main().catch(error => {
    process.stderr.write(`[gh-aw/pi-runtime] ${getErrorMessage(error)}\n`);
    process.exitCode = 1;
  });
}

module.exports = {
  parsePiConfig,
  loadPiSDK,
  resolvePiModelWithoutAWF,
  resolvePiPackageFile,
  nativePiProvider,
  preparePiRuntime,
  stagePiArtifacts,
  readPiModelRoutingSelection,
  verifyPiVersion,
  verifyPiThinkingEffort,
  DEFAULT_AGENT_DIR,
  DEFAULT_SESSION_DIR,
};
