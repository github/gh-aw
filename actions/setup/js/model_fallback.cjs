"use strict";

const { normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const MODEL_FALLBACK_ENV_VAR = "GH_AW_MODEL_FALLBACK";

/** @param {unknown} value @returns {string} */
function validateModelIdentifier(value) {
  if (typeof value !== "string") return "";
  // Reject rather than repair telemetry that could forge footer marker fields.
  if (!value || value.length > 128 || /[^A-Za-z0-9._/:@-]/.test(value)) return "";
  return value;
}

function readFallbackMetadata(filePath) {
  try {
    return JSON.parse(require("fs").readFileSync(filePath, "utf8"));
  } catch (error) {
    throw new Error(`Cannot read model fallback metadata '${filePath}': ${getErrorMessage(error)}`, { cause: error });
  }
}

function recordFallbackModel(model, env = process.env, infoPath = `${env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`) {
  const fs = require("fs");
  if (!validateModelIdentifier(model)) throw new Error("Invalid resolved fallback model");
  env.GH_AW_INFO_MODEL = model;
  const info = fs.existsSync(infoPath) ? readFallbackMetadata(infoPath) : {};
  const phase = env.GH_AW_PHASE || "agent";
  if (phase === "agent") {
    info.model = model;
    info.fallback_model = model;
  } else {
    info[`${phase}_fallback_model`] = model;
  }
  try {
    fs.mkdirSync(require("path").dirname(infoPath), { recursive: true });
    fs.writeFileSync(infoPath, JSON.stringify(info, null, 2));
  } catch (error) {
    throw new Error(`Cannot record model fallback metadata '${infoPath}': ${getErrorMessage(error)}`, { cause: error });
  }
}

function getFallbackModel(infoPath = `${process.env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`, phase = "agent") {
  const fs = require("fs");
  if (!fs.existsSync(infoPath)) return "";
  const info = readFallbackMetadata(infoPath);
  const model = phase === "agent" ? info.fallback_model : info[`${phase}_fallback_model`];
  return validateModelIdentifier(model);
}

function recordFallbackModelFromUsage(content, env = process.env, infoPath, logger = console.warn) {
  let model = "";
  for (const line of content.split("\n")) {
    if (!line.includes('"model_fallback"')) continue;
    let entry;
    try {
      entry = JSON.parse(line);
    } catch (error) {
      logger(`Skipping malformed AWF fallback usage record: ${getErrorMessage(error)}`);
      continue;
    }
    if (typeof entry?._schema !== "string" || !entry._schema.startsWith("token-usage/") || !entry.model_fallback || Number(entry.status) >= 400) continue;
    const selected = entry.model || entry.model_fallback.model;
    const validated = validateModelIdentifier(selected);
    if (validated) model = validated;
  }
  if (model) recordFallbackModel(model, env, infoPath);
  return model;
}

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

/** @param {string} model @param {string} provider @param {NodeJS.ProcessEnv} env @returns {string} */
function normalizeClaudeModel(model, provider, env) {
  const match = /^copilot\/(.+)$/i.exec(model.trim());
  if (!match) return model.trim();
  if (!REFLECT_PROVIDER_ALIASES.github.has(normalizeReflectProviderName(provider)) && env.GH_AW_LLM_PROVIDER_EXPLICIT !== "1") {
    throw new Error("A copilot/ Claude model requires engine.model-provider: github when the model is selected dynamically; configure the provider and its credentials before running Claude");
  }
  return match[1];
}

/** @param {string[]} args @param {string} provider @param {NodeJS.ProcessEnv} env @returns {string[]} */
function normalizeClaudeModelArgs(args, provider, env) {
  const normalized = [...args];
  for (let i = 0; i < normalized.length; i++) {
    if (normalized[i] === "--") break;
    if (normalized[i] === "--model" && i + 1 < normalized.length) {
      normalized[++i] = normalizeClaudeModel(normalized[i], provider, env);
    } else if (normalized[i].startsWith("--model=")) {
      normalized[i] = `--model=${normalizeClaudeModel(normalized[i].slice("--model=".length), provider, env)}`;
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
  validateModelIdentifier,
  recordFallbackModel,
  getFallbackModel,
  recordFallbackModelFromUsage,
  MODEL_FALLBACK_ENV_VAR,
  resolveModelWithFallback,
  applyModelFallback,
  injectModelFlagAfterExec,
  normalizeCodexModel,
  normalizeCodexModelArgs,
  normalizeClaudeModel,
  normalizeClaudeModelArgs,
};
