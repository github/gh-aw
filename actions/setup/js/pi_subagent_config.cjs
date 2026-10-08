// @ts-check
"use strict";

const fs = require("node:fs");
const path = require("node:path");
const { resolveModelAlias, splitModelIdentifier } = require("./resolve_model_alias.cjs");
const { writeSecureOutput } = require("./convert_gateway_config_shared.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/** @param {string} provider */
function aliasProvider(provider) {
  return { github: "copilot", "github-copilot": "copilot", codex: "openai", gemini: "google" }[provider] || provider;
}

/** @param {string} agentDir @param {any} sdk */
function readPiSubagents(agentDir, sdk) {
  const dir = path.join(agentDir, "agents");
  if (!fs.existsSync(dir)) return [];
  const names = new Set();
  try {
    return fs
      .readdirSync(dir, { withFileTypes: true })
      .filter(entry => entry.isFile() && entry.name.endsWith(".md"))
      .map(entry => {
        const { frontmatter, body } = sdk.parseFrontmatter(fs.readFileSync(path.join(dir, entry.name), "utf8"));
        const filenameName = entry.name.replace(/(?:\.agent)?\.md$/, "");
        const name = frontmatter.name ?? filenameName;
        if (typeof name !== "string" || !/^[a-z][a-z0-9_-]*$/.test(name) || names.has(name)) throw new Error(`Invalid or duplicate Pi sub-agent name in ${entry.name}`);
        if (name !== filenameName) throw new Error(`Pi sub-agent "${name}" name must match its agent filename`);
        names.add(name);
        if (typeof frontmatter.description !== "string" || !frontmatter.description.trim()) throw new Error(`Pi sub-agent "${name}" requires a non-empty description`);
        if (frontmatter.model !== undefined && (typeof frontmatter.model !== "string" || !frontmatter.model.trim())) throw new Error(`Pi sub-agent "${name}" model must be a non-empty string`);
        const tools = typeof frontmatter.tools === "string" ? frontmatter.tools.split(",").map(tool => tool.trim()) : frontmatter.tools;
        if (tools !== undefined && (!Array.isArray(tools) || tools.some(tool => typeof tool !== "string" || !/^[a-zA-Z0-9_*-]+$/.test(tool)))) throw new Error(`Pi sub-agent "${name}" tools must be a list of tool names`);
        return { name, description: frontmatter.description, prompt: body, declaredModel: frontmatter.model, tools };
      });
  } catch (error) {
    throw new Error(`Cannot read Pi sub-agent definitions: ${getErrorMessage(error)}`, { cause: error });
  }
}

/** @param {{agentDir: string, sdk: any, provider: string, catalog: string[], gateway: boolean, parentModel: string, logger?: (message: string) => void}} options */
function preparePiSubagents(options) {
  const { agentDir, sdk, provider, catalog, gateway, parentModel, logger = () => {} } = options;
  let aliases;
  try {
    aliases = JSON.parse(process.env.GH_AW_PI_MODEL_ALIASES || "{}");
  } catch (error) {
    throw new Error("Cannot parse Pi sub-agent model aliases", { cause: error });
  }
  if (!aliases || typeof aliases !== "object" || Array.isArray(aliases)) throw new Error("Pi sub-agent model aliases must be an object");
  const scopedProvider = aliasProvider(provider);
  // Restrict alias resolution to the parent's provisioned provider, never another credential route.
  const scopedCatalog = catalog.flatMap(model => {
    const slash = model.indexOf("/");
    if (slash < 0) return [model, `${scopedProvider}/${model}`];
    const prefix = aliasProvider(model.slice(0, slash));
    return prefix === scopedProvider ? [`${prefix}/${model.slice(slash + 1)}`, model.slice(slash + 1)] : [];
  });
  const agents = readPiSubagents(agentDir, sdk).map(agent => {
    const requested = agent.declaredModel || parentModel;
    const { base, params } = splitModelIdentifier(requested);
    if (!base || /[\s*$[\]]/.test(base)) throw new Error(`Pi sub-agent "${agent.name}" model must be a literal model or alias`);
    const isAlias = Object.hasOwn(aliases, base);
    const slash = base.indexOf("/");
    if (!isAlias && slash >= 0 && aliasProvider(base.slice(0, slash)) !== scopedProvider) throw new Error(`Pi sub-agent "${agent.name}" model "${requested}" uses a different provider than ${provider}`);
    const normalized = !isAlias && slash >= 0 ? `${scopedProvider}/${base.slice(slash + 1)}` : base;
    const resolved = isAlias ? resolveModelAlias(requested, aliases, scopedCatalog, { logger }) : normalized;
    if (!resolved) throw new Error(`Pi sub-agent "${agent.name}" model alias "${requested}" did not resolve on ${provider}`);
    const selected = splitModelIdentifier(resolved);
    const modelId = selected.base.includes("/") ? selected.base.slice(selected.base.indexOf("/") + 1) : selected.base;
    const effort = selected.params.get("effort") || params.get("effort");
    if ([...selected.params.keys(), ...params.keys()].some(key => key !== "effort")) throw new Error(`Pi sub-agent "${agent.name}" supports only the effort model parameter`);
    if (effort && !["none", "minimal", "low", "medium", "high", "xhigh"].includes(effort)) throw new Error(`Pi sub-agent "${agent.name}" has unsupported thinking effort "${effort}"`);
    logger(`sub-agent ${agent.name}: requested=${requested} resolved=${provider}/${modelId}`);
    return { ...agent, modelId, model: `${gateway ? "aw-gateway" : provider}/${modelId}`, ...(effort ? { thinking: effort === "none" ? "off" : effort } : {}) };
  });
  writeSecureOutput(path.join(agentDir, "subagents.json"), JSON.stringify(agents, null, 2));
  return agents;
}

module.exports = { readPiSubagents, preparePiSubagents, aliasProvider };
