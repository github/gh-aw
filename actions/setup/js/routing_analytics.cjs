// @ts-check
"use strict";

/**
 * Routing analytics recorded in the unified session: the runner-written
 * configured model policy (aw_info.model_config) and the collector-written
 * counterfactual baseline estimate. All values are identifier-shaped metadata;
 * prompts, transcripts, secrets and free-form diagnostics are never copied.
 */

const fs = require("fs");
const path = require("path");
const { computeInferenceAIC, findModelPricing } = require("./model_costs.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { loadModelAliasMap, resolveConfiguredCopilotModel } = require("./resolve_model_alias.cjs");

/** Model IDs, aliases and policy patterns, such as `github-copilot/*haiku*`. */
const MODEL_IDENTIFIER = /^[A-Za-z0-9*][A-Za-z0-9._:/*@+-]{0,127}$/;
/** Provider error identifiers; matches AWF's identifier-only extraction. */
const ERROR_IDENTIFIER = /^[A-Za-z0-9_.:-]{1,64}$/;
const ENGINE_HARNESSES = new Set(["copilot-cli", "copilot-sdk"]);
const NATIVE_ROUTING_PROVIDERS = new Set(["anthropic", "openai"]);
const MAX_AI_CREDITS_SOURCES = new Set(["frontmatter", "default"]);
const MAX_AI_CREDITS = /^[1-9][0-9]{0,9}$/;
const MAX_AI_CREDITS_LIMIT = 2147483647;

/** @param {unknown} value @returns {value is Record<string, any>} */
const isObject = value => value !== null && typeof value === "object" && !Array.isArray(value);

/** @param {unknown} value @returns {value is string} */
const isModelIdentifier = value => typeof value === "string" && MODEL_IDENTIFIER.test(value);

/** @param {unknown} value @returns {value is string} */
const isErrorIdentifier = value => typeof value === "string" && ERROR_IDENTIFIER.test(value);

/** @param {unknown} value @returns {value is number} */
const isBudget = value => Number.isSafeInteger(value) && Number(value) >= 1 && Number(value) <= MAX_AI_CREDITS_LIMIT;

/**
 * Copy an identifier list only when every entry is identifier-shaped, so a
 * partially valid policy is unknown rather than silently narrowed.
 * @param {unknown} value
 * @returns {string[] | undefined}
 */
function identifierList(value) {
  return Array.isArray(value) && value.every(isModelIdentifier) ? [...value] : undefined;
}

/**
 * @param {unknown} value
 * @returns {string}
 */
function normalizeEngineHarness(value) {
  return typeof value === "string" && ENGINE_HARNESSES.has(value) ? value : "";
}

/**
 * Validate a snake_case model configuration as written to aw_info.json.
 * Returns undefined when the routing state itself is not recorded.
 * @param {unknown} value
 * @returns {Record<string, any> | undefined}
 */
function sanitizeModelConfig(value) {
  if (!isObject(value) || !isObject(value.routing) || typeof value.routing.enabled !== "boolean") return undefined;
  /** @type {Record<string, any>} */
  const routing = { enabled: value.routing.enabled };
  for (const key of ["goal", "mode"]) {
    if (isModelIdentifier(value.routing[key])) routing[key] = value.routing[key];
  }
  if (NATIVE_ROUTING_PROVIDERS.has(value.routing.provider)) routing.provider = value.routing.provider;
  const allowedModels = identifierList(value.routing.allowed_models);
  if (allowedModels) routing.allowed_models = allowedModels;
  const candidateModels = identifierList(value.routing.candidate_models);
  if (candidateModels) routing.candidate_models = candidateModels;
  /** @type {Record<string, any>} */
  const config = { routing };
  if (isObject(value.models)) {
    const allowed = identifierList(value.models.allowed);
    const blocked = identifierList(value.models.blocked);
    if (allowed && blocked) config.models = { allowed, blocked };
  }
  if (MAX_AI_CREDITS_SOURCES.has(value.max_ai_credits_source)) {
    config.max_ai_credits_source = value.max_ai_credits_source;
    if (isBudget(value.max_ai_credits)) config.max_ai_credits = value.max_ai_credits;
  }
  if (isModelIdentifier(value.baseline_model) && value.baseline_model !== "auto") config.baseline_model = value.baseline_model;
  return config;
}

/**
 * Parse the compiler-wired configuration published by generate_aw_info.
 * The budget is the evaluated literal or repository-default expression.
 * @param {string | undefined} rawConfig GH_AW_INFO_MODEL_CONFIG
 * @param {string | undefined} rawBudget GH_AW_INFO_MAX_AI_CREDITS
 * @param {(message: string) => void} [warn]
 * @returns {Record<string, any> | undefined}
 */
function parseCompiledModelConfig(rawConfig, rawBudget, warn = () => {}) {
  if (!rawConfig) return undefined;
  let parsed;
  try {
    parsed = JSON.parse(rawConfig);
  } catch {
    warn("Ignoring GH_AW_INFO_MODEL_CONFIG: invalid JSON");
    return undefined;
  }
  const budget = typeof rawBudget === "string" ? rawBudget.trim() : "";
  const withBudget = isObject(parsed) && MAX_AI_CREDITS.test(budget) && Number(budget) <= MAX_AI_CREDITS_LIMIT ? { ...parsed, max_ai_credits: Number(budget) } : parsed;
  const config = sanitizeModelConfig(withBudget);
  if (!config) warn("Ignoring GH_AW_INFO_MODEL_CONFIG: routing state is not recorded");
  return config;
}

/**
 * Determine the concrete main model the run would use without routing, from
 * runner-written evidence. Never returns the routed selection or `auto`.
 * Resolution is side-effect free: it neither records nor changes the harness model.
 * @param {{
 *   info: Record<string, any>,
 *   resolutions?: any[],
 *   aliasMap?: Record<string, string[]> | null,
 *   reflectData?: any,
 * }} options
 * @returns {string | undefined}
 */
function resolveBaselineModel({ info, resolutions = [], aliasMap, reflectData }) {
  const config = info?.model_config;
  if (!isObject(config) || !isObject(config.routing)) return undefined;
  const recorded = resolutions.find(record => isObject(record) && record.role === "main" && (record.status === "resolved" || record.status === "exact"));
  const accept = value => (isModelIdentifier(value) && value !== "auto" ? value : undefined);
  if (recorded) return accept(recorded.resolved_model);
  if (config.routing.enabled !== true) {
    const selection = info.model_selection;
    return isObject(selection) && selection.status !== "unavailable" ? accept(selection.resolved_model) : undefined;
  }
  if (info.engine_id !== "copilot") return undefined;
  // Routing replaces aw_info.model; requested_model then holds the configured model.
  const configured = isObject(info.model_routing) ? info.requested_model : info.requested_model || info.model;
  if (!isModelIdentifier(configured)) return undefined;
  try {
    return accept(resolveConfiguredCopilotModel({ configuredModel: configured, aliasMap, reflectData }));
  } catch {
    return undefined;
  }
}

/** @param {string} file @returns {any} */
function readJSON(file) {
  try {
    return JSON.parse(fs.readFileSync(file, "utf8"));
  } catch {
    return undefined;
  }
}

/**
 * Record model_config.baseline_model in aw_info.json after the agent exits.
 * Agent phase only; an unknown baseline is left absent.
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {string | undefined}
 */
function persistModelConfigBaseline(env = process.env) {
  if (env.GH_AW_PHASE && env.GH_AW_PHASE !== "agent") return undefined;
  const root = env.GH_AW_TMP_DIR || "/tmp/gh-aw";
  const file = path.join(root, "aw_info.json");
  const info = readJSON(file);
  if (!isObject(info) || !isObject(info.model_config) || info.model_config.baseline_model !== undefined) return undefined;
  const resolutions = readJSON(path.join(root, "agent/model-resolution.json"));
  const reflectData = [path.join(root, "agent/awf-reflect.json"), path.join(root, "sandbox/firewall/awf-reflect.json")].map(readJSON).find(isObject);
  const baseline = resolveBaselineModel({
    info,
    resolutions: Array.isArray(resolutions) ? resolutions : [],
    aliasMap: loadModelAliasMap({ reflectData, env }),
    reflectData,
  });
  if (!baseline) return undefined;
  info.model_config = { ...info.model_config, baseline_model: baseline };
  try {
    fs.writeFileSync(file, JSON.stringify(info, null, 2));
  } catch (error) {
    throw new Error(`Cannot record baseline model: ${getErrorMessage(error)}`, { cause: error });
  }
  return baseline;
}

/**
 * Project aw_info.model_config as the unified `model.config` payload.
 * A validated admission allowance replaces the configured budget for this run.
 * @param {unknown} value
 * @param {Record<string, any>} [admission] Projected guardrail.admission data
 * @returns {Record<string, any> | undefined}
 */
function projectModelConfig(value, admission) {
  const config = sanitizeModelConfig(value);
  if (!config) return undefined;
  const { allowed_models: allowedModels, candidate_models: candidateModels, ...routing } = config.routing;
  /** @type {Record<string, any>} */
  const data = { routing: { ...routing, ...(allowedModels ? { allowedModels } : {}), ...(candidateModels ? { candidateModels } : {}) } };
  if (config.models) data.models = config.models;
  if (config.max_ai_credits_source) {
    if (admission?.status === "admitted" && isBudget(admission.maxAiCredits)) {
      data.maxAiCredits = admission.maxAiCredits;
      data.maxAiCreditsSource = "admission";
    } else {
      if (config.max_ai_credits !== undefined) data.maxAiCredits = config.max_ai_credits;
      data.maxAiCreditsSource = config.max_ai_credits_source;
    }
  }
  if (config.baseline_model) data.baselineModel = config.baseline_model;
  return data;
}

/** @param {string} pattern @returns {RegExp} */
function globPattern(pattern) {
  return new RegExp(`^${pattern.replace(/[.+?^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*")}$`, "i");
}

/**
 * Whether a requested model matches a runner-declared sub-agent model.
 * Matching is deliberately broad so a possible sub-agent is never counted as main.
 * @param {string} requested
 * @param {unknown} provider
 * @param {unknown} declarations aw_info.sub_agent_models
 * @returns {boolean}
 */
function matchesDeclaredSubAgent(requested, provider, declarations) {
  if (!Array.isArray(declarations)) return false;
  const bare = requested.includes("/") ? requested.slice(requested.indexOf("/") + 1) : requested;
  const candidates = [requested, bare, `github-copilot/${bare}`, `copilot/${bare}`, ...(typeof provider === "string" ? [`${provider}/${bare}`] : [])].map(value => value.toLowerCase());
  return declarations.some(entry => {
    if (!isObject(entry)) return false;
    const patterns = [entry.model, ...(Array.isArray(entry.patterns) ? entry.patterns : [])].filter(value => typeof value === "string" && value);
    return patterns.some(pattern => candidates.some(candidate => globPattern(pattern).test(candidate)));
  });
}

/** @param {unknown} value @returns {number | undefined} */
const tokenCount = value => (value === undefined || value === null ? 0 : typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : undefined);

/**
 * Reprice main-agent requests at the configured baseline model.
 * Attribution uses only proxy-written request records and runner-declared sub-agents:
 * classifier calls (`purpose: routing_classification`) and declared sub-agent requests
 * that AWF recorded as `deviated` are excluded; `as_selected` requests not matching a
 * declared sub-agent are main; anything else is unattributed and keeps the estimate incomplete.
 * Returns undefined unless routing is explicitly enabled.
 * @param {{
 *   config: Record<string, any> | undefined,
 *   subAgentModels?: unknown,
 *   tokenUsage: Record<string, any>[],
 *   routing: Record<string, any>[],
 * }} options
 * @returns {Record<string, any> | undefined}
 */
function estimateBaseline({ config, subAgentModels, tokenUsage, routing }) {
  if (config?.routing?.enabled !== true) return undefined;
  const baseline = config.baselineModel;
  /** @type {Map<string, Record<string, any> | null>} */
  const requests = new Map();
  for (const record of routing) {
    if (!isObject(record) || record.stage !== "request" || typeof record.request_id !== "string" || !record.request_id) continue;
    // Conflicting proxy records for one request are ambiguous.
    const previous = requests.get(record.request_id);
    requests.set(record.request_id, previous === undefined || (previous && previous.routed === record.routed && previous.requested_model === record.requested_model) ? record : null);
  }
  const seen = new Set();
  let mainRequests = 0;
  let pricedRequests = 0;
  let unpricedRequests = 0;
  let unattributedRequests = 0;
  let aic = 0;
  for (const record of tokenUsage) {
    if (!isObject(record)) continue;
    const id = record.request_id;
    if (typeof id !== "string" || !id) {
      unattributedRequests++;
      continue;
    }
    // Count each proxy request once, even when it appears in replicated logs.
    if (seen.has(id)) continue;
    seen.add(id);
    if (record.purpose === "routing_classification") continue;
    const route = requests.get(id);
    const requested = route?.requested_model;
    const declared = typeof requested === "string" && matchesDeclaredSubAgent(requested, route?.provider ?? record.provider, subAgentModels);
    if (route?.routed === "deviated" && declared) continue;
    if (route?.routed !== "as_selected" || declared) {
      unattributedRequests++;
      continue;
    }
    mainRequests++;
    const provider = typeof record.provider === "string" ? record.provider : "";
    const tokens = {
      inputTokens: tokenCount(record.input_tokens),
      outputTokens: tokenCount(record.output_tokens),
      cacheReadTokens: tokenCount(record.cache_read_tokens),
      cacheWriteTokens: tokenCount(record.cache_write_tokens),
      reasoningTokens: tokenCount(record.reasoning_tokens),
    };
    // computeInferenceAIC returns zero for unknown pricing; unknown pricing is unpriced, never free.
    if (!baseline || !provider || !findModelPricing(provider, baseline) || Object.values(tokens).some(value => value === undefined) || record.input_tokens === undefined || record.output_tokens === undefined) {
      unpricedRequests++;
      continue;
    }
    aic += computeInferenceAIC({
      provider,
      model: baseline,
      inputTokens: tokens.inputTokens ?? 0,
      outputTokens: tokens.outputTokens ?? 0,
      cacheReadTokens: tokens.cacheReadTokens ?? 0,
      cacheWriteTokens: tokens.cacheWriteTokens ?? 0,
      reasoningTokens: tokens.reasoningTokens ?? 0,
      ...(typeof record.input_tokens_include_cache === "boolean" ? { inputTokensIncludeCache: record.input_tokens_include_cache } : {}),
    });
    pricedRequests++;
  }
  return {
    estimated: true,
    ...(baseline ? { baselineModel: baseline } : {}),
    ...(pricedRequests > 0 ? { aic: Number(aic.toFixed(6)) } : {}),
    mainRequests,
    pricedRequests,
    unpricedRequests,
    unattributedRequests,
    complete: Boolean(baseline) && mainRequests > 0 && unpricedRequests === 0 && unattributedRequests === 0,
  };
}

module.exports = {
  ERROR_IDENTIFIER,
  isErrorIdentifier,
  normalizeEngineHarness,
  sanitizeModelConfig,
  parseCompiledModelConfig,
  resolveBaselineModel,
  persistModelConfigBaseline,
  projectModelConfig,
  matchesDeclaredSubAgent,
  estimateBaseline,
};
