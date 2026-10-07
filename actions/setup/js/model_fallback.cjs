"use strict";

const { normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES, resolveProviderEndpointFromReflect } = require("./awf_reflect.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

const MODEL_FALLBACK_ENV_VAR = "GH_AW_MODEL_FALLBACK";

function readFallbackModels(env = process.env) {
  if (!env.GH_AW_FALLBACK_MODELS) return [];
  let models;
  try {
    models = JSON.parse(env.GH_AW_FALLBACK_MODELS);
  } catch (error) {
    throw new Error(`Cannot parse GH_AW_FALLBACK_MODELS: ${getErrorMessage(error)}`, { cause: error });
  }
  if (!Array.isArray(models) || models.length === 0 || models.some(model => typeof model !== "string" || !/^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,199}$/.test(model))) {
    throw new Error("GH_AW_FALLBACK_MODELS must be a non-empty JSON array of model identifiers");
  }
  return [...new Set(models)];
}

function isModelFallbackFailure(result) {
  const { collectAgentExecution, agentErrorDiagnosticText } = require("./agent_execution.cjs");
  const { detectNonRetryableHarnessGuard } = require("./harness_error_patterns.cjs");
  const text = agentErrorDiagnosticText(result.output);
  const data = collectAgentExecution({ content: result.output })?.data;
  const categories = new Set(data?.categories || []);
  const codes = (data?.errorCodes || []).map(String);
  const types = data?.errorTypes || [];
  const guards = detectNonRetryableHarnessGuard(text);
  const { isCrashSignalExitCode } = require("./harness_crash_signals.cjs");
  if (
    result.cancelled ||
    result.runtimeGuardFired ||
    result.watchdogFired ||
    isCrashSignalExitCode(result.exitCode) ||
    [130, 137, 143].includes(result.exitCode) ||
    guards.maxRunsExceeded ||
    guards.aiCreditsExceeded ||
    guards.awfAPIProxyBlockingRequests ||
    guards.goalAlreadyActive ||
    guards.apiProxyGuardRejection ||
    codes.some(code => ["401", "403", "429"].includes(code)) ||
    [...codes, ...types].some(type => ["authentication_error", "authentication_failed", "unauthorized", "permission_error", "rate_limit_error", "mcp_policy_error", "model_policy_violation"].includes(type)) ||
    ["authentication_failed", "inference_access_error", "mcp_policy_error", "capi_quota_exceeded_error", "model_policy_violation"].some(category => categories.has(category)) ||
    /No authentication information found|Session was not created with authentication info or custom provider|Personal Access Tokens are not supported|MCP servers were blocked by policy:/i.test(text)
  ) {
    return false;
  }
  return (
    categories.has("model_not_supported_error") ||
    categories.has("capi_server_error") ||
    codes.some(code => /^5\d{2}$/.test(code)) ||
    [...codes, ...types].some(type => ["model_not_supported", "unsupported_model", "invalid_model", "model_not_found", "timeout_error", "request_timeout", "overloaded_error"].includes(type)) ||
    (codes.includes("400") && /"param"\s*:\s*"model"/.test(text)) ||
    /\bmodel\b[^\n]*(?:does not exist|not found|not supported|not available)/i.test(text) ||
    /Timeout after \d+ms waiting for session\.idle|(?:ETIMEDOUT|request timed out|connection timed out)|400[^\n]*no model endpoints available given user constraints/i.test(text)
  );
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
  if (typeof model !== "string" || !model || /[\r\n]/.test(model)) throw new Error("Invalid resolved fallback model");
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
  return typeof model === "string" ? model : "";
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
    if (typeof selected === "string" && selected && !/[\r\n]/.test(selected)) model = selected;
  }
  if (model) recordFallbackModel(model, env, infoPath);
  return model;
}

function resolveFallbackSelection(model, defaultProvider, reflectData, aliasMap) {
  const normalize = provider => (REFLECT_PROVIDER_ALIASES.github.has(normalizeReflectProviderName(provider)) ? "github" : normalizeReflectProviderName(provider));
  if (!aliasMap) {
    const fs = require("fs");
    const configPath = process.env.GH_AW_AWF_CONFIG_PATH || `${process.env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/awf-config.json`;
    if (fs.existsSync(configPath)) aliasMap = readFallbackMetadata(configPath)?.apiProxy?.models;
  }
  let resolvedModel = model;
  if (aliasMap && Object.prototype.hasOwnProperty.call(aliasMap, model)) {
    const { buildCatalogFromReflect, resolveModelAlias, ModelAliasResolutionError } = require("./resolve_model_alias.cjs");
    const primaryCatalog = buildCatalogFromReflect({
      endpoints: (reflectData?.endpoints || []).filter(endpoint => normalize(endpoint.provider) === normalize(defaultProvider)),
    });
    const catalog = buildCatalogFromReflect(reflectData).filter(entry => entry.includes("/") || primaryCatalog.includes(entry));
    resolvedModel = resolveModelAlias(model, aliasMap, catalog);
    if (!resolvedModel) throw new ModelAliasResolutionError(model);
  }
  const match = /^(copilot|github|github-copilot|github_models|openai|anthropic)\/(.+)$/i.exec(resolvedModel);
  const provider = normalize(match ? match[1] : defaultProvider);
  const wireModel = match ? match[2] : resolvedModel;
  if (!reflectData) {
    if (provider !== normalize(defaultProvider)) throw new Error(`Fallback provider '${provider}' requires AWF /reflect`);
    return { provider, model: wireModel, resolvedModel, baseUrl: "" };
  }
  const resolved = resolveProviderEndpointFromReflect({ provider, reflectData });
  if (!resolved || normalize(resolved.endpointProvider) !== provider) {
    throw new Error(`Fallback provider '${provider}' has no configured AWF endpoint; configure its credentials`);
  }
  return { provider, model: wireModel, resolvedModel, baseUrl: resolved.baseUrl };
}

/**
 * Replace only model options, never prompt text or arguments after `--`.
 * @param {string[]} args
 * @param {string} model
 * @returns {string[]}
 */
function replaceModelArgs(args, model) {
  const result = [];
  let replaced = false;
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--") {
      result.push(...args.slice(i));
      break;
    }
    if (["--prompt", "-p", "--system-prompt", "--append-system-prompt"].includes(args[i]) && i + 1 < args.length) {
      result.push(args[i], args[++i]);
      continue;
    }
    if (args[i] === "--model" || args[i] === "-m") {
      result.push(args[i], model);
      i++;
      replaced = true;
    } else if (modelFlagPrefix(args[i])) {
      result.push(`${modelFlagPrefix(args[i])}${model}`);
      replaced = true;
    } else {
      result.push(args[i]);
    }
  }
  if (replaced) return result;
  const execIndex = result.indexOf("exec");
  const separatorIndex = result.indexOf("--");
  const insertAt = execIndex >= 0 ? execIndex + 1 : separatorIndex >= 0 ? separatorIndex : result.length;
  return [...result.slice(0, insertAt), "--model", model, ...result.slice(insertAt)];
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
  readFallbackModels,
  isModelFallbackFailure,
  recordFallbackModel,
  getFallbackModel,
  recordFallbackModelFromUsage,
  replaceModelArgs,
  resolveFallbackSelection,
  MODEL_FALLBACK_ENV_VAR,
  resolveModelWithFallback,
  applyModelFallback,
  injectModelFlagAfterExec,
  normalizeCodexModel,
  normalizeCodexModelArgs,
  normalizeClaudeModel,
  normalizeClaudeModelArgs,
};
