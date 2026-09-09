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
const { fetchAWFReflect, normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES, resolveProviderEndpointFromReflect } = require("./awf_reflect.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const DEFAULT_PI_CODING_AGENT_DIR = "/tmp/gh-aw/pi-agent-dir";

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
 * @param {{ baseUrl: string, apiKeyEnvVar: string, modelId: string, api?: string }} options
 * @returns {string}
 */
function buildModelsJSON(options) {
  const { baseUrl, apiKeyEnvVar, modelId, api } = options;
  return JSON.stringify({
    providers: {
      "aw-gateway": {
        baseUrl,
        api: api || "openai-completions",
        apiKey: apiKeyEnvVar,
        models: [{ id: modelId }],
      },
    },
  });
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
  return provider === "anthropic" ? "anthropic-messages" : "openai-completions";
}

async function main() {
  const logger = DEFAULT_LOGGER;
  const modelId = process.env.GH_AW_PI_MODEL_ID || "";
  const apiKeyEnvVar = process.env.GH_AW_PI_GATEWAY_SECRET_ENV || "";
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

  const { baseUrl, source } = resolveGatewayBaseUrl({ provider, fallbackPort, reflectData, logger });
  logger(`resolved gateway baseUrl=${baseUrl} (source=${source}, provider=${provider}, fallbackPort=${fallbackPort})`);

  const api = resolvePiApiForProvider(provider);
  logger(`resolved gateway api=${api} (provider=${provider})`);

  const modelsJSON = buildModelsJSON({ baseUrl, apiKeyEnvVar, modelId, api });
  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  fs.writeFileSync(outputPath, modelsJSON, "utf8");
  logger(`wrote ${outputPath}`);
}

if (require.main === module) {
  main().catch(error => {
    process.stderr.write(`[gh-aw/pi-models-json] fatal: ${getErrorMessage(error)}\n`);
    process.exitCode = 1;
  });
}

module.exports = { main, resolveGatewayBaseUrl, buildModelsJSON, resolvePiApiForProvider, DEFAULT_PI_CODING_AGENT_DIR };
