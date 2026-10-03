"use strict";

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
  const normalize = value => (/^(copilot|github|github-copilot|github_models)$/.test(value.toLowerCase()) ? "github" : value.toLowerCase());
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
};
