// @ts-check

/**
 * Pi models.json generator
 *
 * Runs before the Pi CLI starts (when the AWF firewall/api-proxy sidecar is
 * enabled) and writes the models.json that registers the "aw-gateway"
 * provider Pi routes its inference calls through.
 *
 * The provider's gateway port is compiled into gh-aw as a fallback, but AWF's
 * actual port assignment is authoritative and can only be confirmed at
 * runtime via the api-proxy sidecar's /reflect endpoint (see
 * docs/src/content/docs/experimental/awf-reflect.md). This script queries
 * /reflect first and only falls back to the compile-time port when /reflect
 * is unavailable or does not report a configured endpoint for the resolved
 * provider, so Pi always targets the live gateway port instead of a
 * potentially stale compiled-in value.
 *
 * Environment variables:
 *   GH_AW_PI_MODEL_ID              — Pi model ID (without provider prefix)
 *   GH_AW_PI_CONTEXT_WINDOW        — optional model context window to write into models.json
 *   GH_AW_PI_GATEWAY_SECRET_ENV    — name of the env var holding the provider API key
 *   GH_AW_PI_GATEWAY_FALLBACK_PORT — compile-time api-proxy port, used when /reflect
 *                                    is unavailable or has no matching configured endpoint
 *   GH_AW_LLM_PROVIDER             — normalized provider name ("github", "anthropic", "openai")
 *   GH_AW_PI_MODELS_JSON_PATH      — output path (defaults to PI_CODING_AGENT_DIR/models.json)
 *   PI_CODING_AGENT_DIR            — Pi agent config directory (defaults to /tmp/gh-aw/pi-agent-dir)
 *   AWF_REFLECT_ENABLED            — "1" when the AWF api-proxy sidecar is running
 */

"use strict";

const fs = require("fs");
const path = require("path");
const { fetchAWFReflect, getCatalogModelEntry, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES, resolveProviderEndpointFromReflect } = require("./awf_reflect.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { loadModelsJson } = require("./model_costs.cjs");
const { loadPiSDK, nativePiProvider, parsePiConfig } = require("./pi_runtime.cjs");

const DEFAULT_PI_CODING_AGENT_DIR = "/tmp/gh-aw/pi-agent-dir";
const COPILOT_CLAUDE_SONNET_5_CONTEXT_WINDOW = 1000000;

// prettier-ignore
const DEFAULT_LOGGER = /** @type {(msg: string) => void} */ (msg => process.stderr.write(`[gh-aw/pi-models-json] ${new Date().toISOString()} ${msg}\n`));

/**
 * Resolve the models.json gateway base URL for the given provider, preferring
 * the live /reflect data and falling back to the compile-time port.
 *
 * @param {{
 *   provider: string,
 *   fallbackPort: number,
 *   reflectData?: any,
 *   logger: (msg: string) => void,
 * }} options
 * @returns {{ baseUrl: string, source: "reflect"|"fallback" }}
 */
function resolveGatewayBaseUrl(options) {
  const { provider, fallbackPort, reflectData, logger } = options;
  const fallbackBaseUrl = `http://api-proxy:${fallbackPort}`;
  if (!reflectData) {
    return { baseUrl: fallbackBaseUrl, source: "fallback" };
  }
  const resolved = resolveProviderEndpointFromReflect({ provider, reflectData, logger });
  const normalizedProvider = normalizeReflectProviderName(provider, "openai");
  const normalizedEndpointProvider = normalizeReflectProviderName(resolved?.endpointProvider);
  const providerAliases = REFLECT_PROVIDER_ALIASES[normalizedProvider] || new Set([normalizedProvider]);
  if (resolved && resolved.baseUrl && providerAliases.has(normalizedEndpointProvider)) {
    return { baseUrl: resolved.baseUrl, source: "reflect" };
  }
  if (resolved && resolved.baseUrl) {
    logger(`warning: /reflect resolved provider=${normalizedProvider} to endpointProvider=${normalizedEndpointProvider}; using fallback port ${fallbackPort}`);
  }
  return { baseUrl: fallbackBaseUrl, source: "fallback" };
}

/**
 * Build the Pi models.json payload that registers a single custom provider
 * named "aw-gateway" pointing at the resolved AWF LLM gateway base URL.
 *
 * Pi's resolveConfigValue() resolves the "apiKey" value by looking up
 * process.env[apiKey], so passing the secret env-var name (e.g.
 * "COPILOT_GITHUB_TOKEN") causes Pi to automatically use the value that is
 * already present in the container environment.
 *
 * @param {{ baseUrl: string, apiKeyEnvVar: string, modelId: string, api?: string, provider?: string, nativeProvider?: string, contextWindow?: number|string, metadata?: Record<string, any>, logger?: (msg: string) => void }} options
 * @returns {string}
 */
function buildModelsJSON(options) {
  const { baseUrl, modelId, api, provider, nativeProvider, metadata = {}, contextWindow: configuredContextWindow, logger = () => {} } = options;
  // Pi's built-in github-copilot catalog lists claude-sonnet-5 with a 1M context window.
  const fallbackContextWindow = provider === "github" && modelId === "claude-sonnet-5" ? COPILOT_CLAUDE_SONNET_5_CONTEXT_WINDOW : undefined;
  const resolvedContextWindow = resolveContextWindow(configuredContextWindow, logger);
  const contextWindow = resolvedContextWindow === undefined ? fallbackContextWindow : resolvedContextWindow;
  return JSON.stringify({
    providers: {
      "aw-gateway": {
        baseUrl,
        api: api || "openai-completions",
        // AWF owns the upstream credential; Pi only needs a non-secret placeholder.
        apiKey: "awf-proxy",
        models: [
          {
            ...metadata,
            id: modelId,
            ...(contextWindow ? { contextWindow } : {}),
            // Copilot's Responses adapter only supports apply_patch custom tools.
            ...(provider === "github" && api === "openai-responses" ? { compat: { ...metadata.compat, supportsOpenAIGrammarTools: false } } : {}),
          },
        ],
      },
      ...(nativeProvider && !["github-copilot", "anthropic", "openai", "google"].includes(nativeProvider) ? { [nativeProvider]: { baseUrl, apiKey: "awf-proxy" } } : {}),
    },
  });
}

/**
 * Resolve a configured context window, returning undefined for empty or invalid values so callers can apply a fallback.
 *
 * @param {number|string|undefined} value
 * @param {(msg: string) => void} logger
 * @returns {number|undefined}
 */
function resolveContextWindow(value, logger) {
  if (value === undefined || value === "") {
    return undefined;
  }
  const parsed = typeof value === "number" ? value : Number(value);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) {
    logger(`warning: ignoring invalid contextWindow=${JSON.stringify(value)}; expected a positive integer`);
    return undefined;
  }
  return parsed;
}

/**
 * Resolve the Pi API family for a given normalized GH_AW_LLM_PROVIDER value.
 *
 * Real OpenAI models are only published under the "openai-responses" API in Pi's
 * upstream model catalog (@earendil-works/pi-ai) — the "openai-completions" family
 * is reserved for OpenAI-compatible-but-not-OpenAI providers (Groq, DeepSeek, etc.).
 * Since OpenAI's Chat Completions endpoint rejects function tools whenever
 * reasoning_effort is anything other than "none" (see
 * https://developers.openai.com/api/docs/guides/responses-vs-chat-completions),
 * routing the "openai" provider through /responses keeps tool calling working for
 * all reasoning-capable models without requiring workflow authors to opt in.
 *
 * GitHub/Copilot models keep their chat-completions-style gateway protocol.
 * Anthropic uses its native Messages API so the proxy receives /v1/messages and
 * can apply Anthropic prompt caching.
 *
 * @param {string} provider - normalized GH_AW_LLM_PROVIDER value (e.g. "openai", "anthropic", "github")
 * @returns {string}
 */
function resolvePiApiForProvider(provider) {
  if (provider === "openai" || provider === "codex") {
    return "openai-responses";
  }
  if (provider === "google" || provider === "gemini") return "google-generative-ai";
  return provider === "anthropic" ? "anthropic-messages" : "openai-completions";
}

/**
 * Fail before inference when a completed proxy model inventory excludes the selected model.
 *
 * @param {{ provider: string, modelId: string, reflectData?: any, logger?: (msg: string) => void }} options
 */
function validatePiModelAvailability(options) {
  const { provider, modelId, reflectData, logger = () => {} } = options;
  if (!reflectData) {
    logger("awf-reflect: model availability check skipped (reflection data unavailable)");
    return;
  }
  if (reflectData.models_fetch_complete !== true) {
    logger("awf-reflect: model availability check skipped (model discovery incomplete)");
    return;
  }

  const normalizedProvider = normalizeReflectProviderName(provider);
  const aliases = REFLECT_PROVIDER_ALIASES[normalizedProvider] || new Set([normalizedProvider]);
  const endpoint = reflectData.endpoints?.find(endpoint => endpoint?.configured === true && aliases.has(normalizeReflectProviderName(endpoint.provider)));
  if (!endpoint) {
    logger(`awf-reflect: model availability check skipped (no configured endpoint for provider=${normalizedProvider})`);
    return;
  }
  if (!Array.isArray(endpoint.models)) {
    logger(`awf-reflect: model availability check skipped (endpoint=${endpoint.provider} has no model inventory)`);
    return;
  }

  const normalizedModelId = modelId.split("?")[0].toLowerCase();
  const advertisedModels = endpoint.models.map(model => {
    if (typeof model === "string") return model.toLowerCase();
    if (model && typeof model === "object") return String(model.id || model.name || "").toLowerCase();
    return "";
  });
  if (advertisedModels.includes(normalizedModelId)) {
    logger(`awf-reflect: model availability confirmed (provider=${endpoint.provider}, model=${modelId})`);
    return;
  }
  logger(`warning: awf-reflect model availability check failed (provider=${endpoint.provider}, model=${modelId})`);
  throw new Error(`Pi model "${modelId}" is not advertised by the configured ${endpoint.provider} proxy endpoint; choose a model listed by AWF /reflect`);
}

/**
 * Resolve a model-specific Pi API from model metadata, rejecting an explicit
 * chat-completions override when the model only supports the Responses API.
 *
 * @param {{ provider: string, modelId: string, model?: any, modelsJson?: any, overrideApi?: string, logger?: (msg: string) => void }} options
 * @returns {string}
 */
function resolvePiApiForModel(options) {
  const { provider, modelId, model, modelsJson, overrideApi, logger = () => {} } = options;
  const catalogProvider = ["github", "copilot", "github-copilot"].includes(provider) ? "github-copilot" : provider;
  const catalogEntry = getCatalogModelEntry(modelsJson, modelId, catalogProvider);
  const wireApi = String(catalogEntry?.wire_api || catalogEntry?.wireApi || "")
    .toLowerCase()
    .trim();
  const requiresResponses = model?.api === "openai-responses" || wireApi === "responses";
  logger(`Pi model API metadata (provider=${provider}, model=${modelId}, catalog_wire_api=${wireApi || "(unset)"}, catalog_api=${model?.api || "(unset)"}, override_api=${overrideApi || "(unset)"})`);
  if (requiresResponses && overrideApi && overrideApi !== "openai-responses") {
    logger(`warning: Pi model API override conflicts with Responses-only model (model=${modelId}, override_api=${overrideApi})`);
    throw new Error(`Pi model "${modelId}" requires the OpenAI Responses API, but engine.config.model.api is "${overrideApi}"`);
  }
  const api = requiresResponses ? "openai-responses" : overrideApi || model?.api || resolvePiApiForProvider(provider);
  const source = overrideApi ? "engine.config.model.api" : model?.api ? "Pi model catalog" : wireApi === "responses" ? "AWF model catalog" : "provider default";
  logger(`resolved model API=${api} (provider=${provider}, model=${modelId}, source=${source})`);
  return api;
}

/**
 * Resolve reasoning support from the selected provider's reflected model metadata.
 *
 * @param {{ provider: string, modelId: string, reflectData?: any }} options
 * @returns {boolean|undefined}
 */
function resolvePiReasoningForModel({ provider, modelId, reflectData }) {
  const normalizedProvider = normalizeReflectProviderName(provider);
  const aliases = REFLECT_PROVIDER_ALIASES[normalizedProvider] || new Set([normalizedProvider]);
  const endpoint = reflectData?.endpoints?.find(endpoint => endpoint?.configured === true && aliases.has(normalizeReflectProviderName(endpoint.provider)));
  if (!endpoint) return undefined;
  const normalizedModelId = modelId.split("?")[0].toLowerCase();
  const routingModel = endpoint.routing_models?.find(model => typeof model?.model_id === "string" && model.model_id.toLowerCase() === normalizedModelId);
  const model = endpoint.model_metadata?.find(model => typeof model?.id === "string" && model.id.toLowerCase() === normalizedModelId);
  const supports = model?.capabilities?.supports;
  const efforts = routingModel?.supported_reasoning_efforts ?? model?.supportedReasoningEfforts ?? supports?.reasoning_effort;
  if (Array.isArray(efforts) && efforts.every(effort => typeof effort === "string")) {
    return efforts.some(effort => effort !== "none");
  }
  if (supports?.reasoningEffort === false) return false;
  // Copilot omits reasoning_effort for models without effort control.
  if (normalizedProvider === "github" && supports && typeof supports === "object" && !Array.isArray(supports) && !Object.hasOwn(supports, "reasoning_effort") && !Object.hasOwn(supports, "reasoningEffort")) {
    return false;
  }
  return undefined;
}

/** @param {{ loadSDK?: typeof loadPiSDK, loadModelsJson?: typeof loadModelsJson }} [options] */
async function main(options = {}) {
  const logger = DEFAULT_LOGGER;
  const modelId = process.env.GH_AW_PI_MODEL_ID || "";
  const apiKeyEnvVar = process.env.GH_AW_PI_GATEWAY_SECRET_ENV || "";
  const contextWindow = process.env.GH_AW_PI_CONTEXT_WINDOW || "";
  const fallbackPort = Number.parseInt(process.env.GH_AW_PI_GATEWAY_FALLBACK_PORT || "", 10);
  const provider = process.env.GH_AW_LLM_PROVIDER || "github";
  const agentDir = process.env.PI_CODING_AGENT_DIR || DEFAULT_PI_CODING_AGENT_DIR;
  const outputPath = process.env.GH_AW_PI_MODELS_JSON_PATH || path.join(agentDir, "models.json");

  if (!modelId || !apiKeyEnvVar || !Number.isFinite(fallbackPort)) {
    logger("fatal: missing required env vars (GH_AW_PI_MODEL_ID, GH_AW_PI_GATEWAY_SECRET_ENV, GH_AW_PI_GATEWAY_FALLBACK_PORT)");
    process.exitCode = 1;
    return;
  }

  /** @type {any} */
  let reflectData = null;
  if (process.env.AWF_REFLECT_ENABLED === "1") {
    try {
      const result = await fetchAWFReflect({ logger });
      if (result && result.ok && result.reflectData) {
        reflectData = result.reflectData;
      }
    } catch (error) {
      logger(`warning: /reflect fetch failed: ${getErrorMessage(error)}`);
    }
  }

  validatePiModelAvailability({ provider, modelId, reflectData, logger });
  const { baseUrl, source } = resolveGatewayBaseUrl({ provider, fallbackPort, reflectData, logger });
  logger(`resolved gateway baseUrl=${baseUrl} (source=${source}, provider=${provider}, fallbackPort=${fallbackPort})`);

  const overrides = parsePiConfig().model || {};
  const nativeProvider = nativePiProvider(process.env.GH_AW_PI_NATIVE_PROVIDER || provider);
  const modelsJson = (options.loadModelsJson || loadModelsJson)();
  let catalogModel;
  if (["reasoning", "input", "contextWindow", "maxTokens"].every(key => Object.hasOwn(overrides, key))) {
    logger(`using explicit model metadata for ${nativeProvider}/${modelId}`);
  } else {
    const sdk = await (options.loadSDK || loadPiSDK)();
    const runtime = await sdk.ModelRuntime.create({ modelsPath: null });
    catalogModel = runtime.getModel(nativeProvider, modelId.split("?")[0]);
  }
  let api = resolvePiApiForModel({ provider, modelId, model: catalogModel, modelsJson, overrideApi: overrides.api, logger });
  logger(`resolved gateway api=${api} (provider=${provider}, model=${modelId})`);
  const metadata = {};
  if (catalogModel) {
    for (const key of ["name", "reasoning", "thinkingLevelMap", "input", "inputLimits", "cost", "promptCache", "contextWindow", "maxTokens", "samplingParams", "compat"]) {
      if (catalogModel[key] !== undefined) metadata[key] = catalogModel[key];
    }
  } else {
    logger(`warning: Pi has no catalog metadata for ${nativeProvider}/${modelId}; configure engine.config.model for a custom model`);
  }
  for (const key of Object.keys(overrides)) {
    if (!["api", "name", "reasoning", "thinkingLevelMap", "input", "inputLimits", "cost", "promptCache", "contextWindow", "maxTokens", "samplingParams", "compat"].includes(key)) {
      throw new Error(`Unsupported Pi model metadata field: ${key}`);
    }
  }
  if (overrides.api !== undefined) {
    if (!["openai-completions", "openai-responses", "anthropic-messages", "google-generative-ai"].includes(overrides.api)) throw new Error("Pi model API must match a supported AWF protocol");
    api = overrides.api;
  }
  Object.assign(metadata, overrides);
  const reasoning = resolvePiReasoningForModel({ provider, modelId, reflectData });
  if (reasoning !== undefined) {
    metadata.reasoning = reasoning;
    logger(`resolved reasoning=${reasoning} from AWF /reflect (provider=${provider}, model=${modelId})`);
  } else {
    logger(`awf-reflect: reasoning metadata unavailable; retaining Pi model configuration (provider=${provider}, model=${modelId})`);
  }
  const modelsJSON = buildModelsJSON({ baseUrl, apiKeyEnvVar, modelId, api, provider, nativeProvider, contextWindow, metadata, logger });
  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  fs.writeFileSync(outputPath, modelsJSON, { encoding: "utf8", mode: 0o600 });
  logger(`wrote ${outputPath}`);
}

if (require.main === module) {
  main().catch(error => {
    process.stderr.write(`[gh-aw/pi-models-json] fatal: ${getErrorMessage(error)}\n`);
    process.exitCode = 1;
  });
}

module.exports = { main, resolveGatewayBaseUrl, buildModelsJSON, resolvePiApiForProvider, resolvePiApiForModel, resolvePiReasoningForModel, validatePiModelAvailability, DEFAULT_PI_CODING_AGENT_DIR };
