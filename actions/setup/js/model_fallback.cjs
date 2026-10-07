"use strict";

const { normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");
const { buildCatalogFromReflect, selectLatestGlobMatch, splitModelIdentifier } = require("./resolve_model_alias.cjs");

const MODEL_FALLBACK_ENV_VAR = "GH_AW_MODEL_FALLBACK";

function readTrimmedEnv(env, name) {
  return typeof env?.[name] === "string" ? env[name].trim() : "";
}

function resolveModelWithFallback(env, primaryEnvVar) {
  return readTrimmedEnv(env, primaryEnvVar) || readTrimmedEnv(env, MODEL_FALLBACK_ENV_VAR);
}

/**
 * @param {NodeJS.ProcessEnv} env
 * @param {string} primaryEnvVar
 * @param {(message: string) => void} [logger]
 * @returns {string}
 */
function applyModelFallback(env, primaryEnvVar, logger = () => {}) {
  const primary = readTrimmedEnv(env, primaryEnvVar);
  if (primary) {
    return primary;
  }
  const fallback = readTrimmedEnv(env, MODEL_FALLBACK_ENV_VAR);
  if (fallback) {
    env[primaryEnvVar] = fallback;
    logger(`applied ${MODEL_FALLBACK_ENV_VAR} to ${primaryEnvVar}`);
  }
  return fallback;
}

function injectModelFlagAfterExec(args, model) {
  const options = args.slice(0, args.indexOf("--") === -1 ? args.length : args.indexOf("--"));
  if (!model || options.some(arg => arg === "--model" || arg === "-m" || modelFlagPrefix(arg))) {
    return args;
  }
  const execIndex = args.indexOf("exec");
  if (execIndex === -1) {
    return [...args, "--model", model];
  }
  return [...args.slice(0, execIndex + 1), "--model", model, ...args.slice(execIndex + 1)];
}

/** @param {string} argument @returns {string|undefined} */
function modelFlagPrefix(argument) {
  for (const prefix of ["--model=", "-m=", "-m"]) {
    if (argument.startsWith(prefix)) return prefix;
  }
  return undefined;
}

/**
 * Prefixes must agree with the provisioned provider unless an explicit override wins.
 * @param {string} model
 * @param {string} provider
 * @param {{ env?: NodeJS.ProcessEnv, logger?: (message: string) => void }} [options]
 * @returns {string}
 */
function normalizeCodexModel(model, provider, options = {}) {
  const match = /^(openai|copilot|github|github-copilot|github_models|anthropic)\/(.+)$/i.exec(model.trim());
  if (!match) return model.trim();
  const normalize = value => (REFLECT_PROVIDER_ALIASES.github.has(normalizeReflectProviderName(value)) ? "github" : normalizeReflectProviderName(value));
  if (normalize(match[1]) !== normalize(provider.trim().toLowerCase())) {
    const env = options.env ?? process.env;
    if (env.GH_AW_LLM_PROVIDER_EXPLICIT === "1" && ["openai", "github"].includes(normalize(match[1]))) {
      options.logger?.(`model prefix '${match[1]}' overridden by explicitly configured provider; retaining existing endpoint and credentials`);
      return match[2];
    }
    throw new Error(`model prefix '${match[1]}' does not match configured provider '${provider}'; configure the provider and its credentials before running Codex`);
  }
  return match[2];
}

/**
 * @param {string} model
 * @param {string} provider
 * @param {{ reflectData?: import("./awf_reflect.cjs").ReflectData | null, logger?: (message: string) => void }} [options]
 * @returns {string}
 */
function resolveClaudeAutoModel(model, provider, options = {}) {
  const { base } = splitModelIdentifier(model);
  if (base.toLowerCase() !== "auto") return model;
  const suffix = model.slice(base.length);
  const normalizedProvider = normalizeReflectProviderName(provider, "anthropic");
  if (normalizedProvider === "anthropic") {
    options.logger?.("Claude auto model selection uses the native sonnet alias");
    return `sonnet${suffix}`;
  }
  if (!REFLECT_PROVIDER_ALIASES.github.has(normalizedProvider)) {
    throw new Error(`Claude auto model selection is not supported for provider '${provider}'`);
  }
  const endpoints = options.reflectData?.endpoints?.filter(endpoint => endpoint?.configured === true && REFLECT_PROVIDER_ALIASES.github.has(normalizeReflectProviderName(endpoint.provider))) || [];
  const catalog = buildCatalogFromReflect({ endpoints });
  // Claude's Messages API cannot use Copilot's general-purpose auto picker.
  for (const family of ["sonnet", "opus", "haiku"]) {
    const selected = selectLatestGlobMatch(`claude-*${family}*`, catalog);
    if (selected) {
      options.logger?.(`Claude auto model selection: '${model}' -> '${selected}${suffix}' (provider=${provider})`);
      return selected + suffix;
    }
  }
  throw new Error("Claude auto model selection requires an advertised Claude Sonnet, Opus, or Haiku model on the configured Copilot endpoint");
}

/**
 * @param {string} model
 * @param {string} provider
 * @param {NodeJS.ProcessEnv} env
 * @param {{ reflectData?: import("./awf_reflect.cjs").ReflectData | null, logger?: (message: string) => void }} [options]
 * @returns {string}
 */
function normalizeClaudeModel(model, provider, env, options = {}) {
  const match = /^copilot\/(.+)$/i.exec(model.trim());
  if (!match) return resolveClaudeAutoModel(model.trim(), provider, options);
  if (!REFLECT_PROVIDER_ALIASES.github.has(normalizeReflectProviderName(provider)) && env.GH_AW_LLM_PROVIDER_EXPLICIT !== "1") {
    throw new Error("A copilot/ Claude model requires engine.model-provider: github when the model is selected dynamically; configure the provider and its credentials before running Claude");
  }
  return resolveClaudeAutoModel(match[1], provider, options);
}

/**
 * @param {string[]} args
 * @param {string} provider
 * @param {NodeJS.ProcessEnv} env
 * @param {{ reflectData?: import("./awf_reflect.cjs").ReflectData | null, logger?: (message: string) => void }} [options]
 * @returns {string[]}
 */
function normalizeClaudeModelArgs(args, provider, env, options = {}) {
  const normalized = [...args];
  for (let i = 0; i < normalized.length; i++) {
    if (normalized[i] === "--") break;
    if (normalized[i] === "--model" && i + 1 < normalized.length) {
      normalized[++i] = normalizeClaudeModel(normalized[i], provider, env, options);
    } else if (normalized[i].startsWith("--model=")) {
      normalized[i] = `--model=${normalizeClaudeModel(normalized[i].slice("--model=".length), provider, env, options)}`;
    }
  }
  return normalized;
}

/** @param {string[]} args @param {string} provider @param {{ env?: NodeJS.ProcessEnv, logger?: (message: string) => void }} [options] @returns {string[]} */
function normalizeCodexModelArgs(args, provider, options = {}) {
  const normalized = [...args];
  for (let i = 0; i < normalized.length; i++) {
    if (normalized[i] === "--") break;
    if (normalized[i] === "--model" || normalized[i] === "-m") {
      if (i + 1 < normalized.length) normalized[++i] = normalizeCodexModel(normalized[i], provider, options);
    } else {
      const prefix = modelFlagPrefix(normalized[i]);
      if (prefix) normalized[i] = `${prefix}${normalizeCodexModel(normalized[i].slice(prefix.length), provider, options)}`;
    }
  }
  return normalized;
}

module.exports = {
  MODEL_FALLBACK_ENV_VAR,
  resolveModelWithFallback,
  applyModelFallback,
  injectModelFlagAfterExec,
  normalizeCodexModel,
  normalizeCodexModelArgs,
  normalizeClaudeModel,
  normalizeClaudeModelArgs,
};
