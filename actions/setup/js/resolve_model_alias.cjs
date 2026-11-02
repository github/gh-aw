// @ts-check

/**
 * Resolve gh-aw model aliases to concrete catalog model names (spec §8 fallback
 * resolution algorithm). `resolveEngineModel` is the shared "resolve configured
 * model" step that every engine harness runs before it launches its CLI.
 */

const fs = require("fs");
const path = require("path");

/** Runner-written record of configured and resolved models, relative to /tmp/gh-aw. */
const MODEL_RESOLUTION_RECORD_PATH = "agent/model-resolution.json";

/** Provider prefixes accepted for each canonical model provider. */
const MODEL_PROVIDER_PREFIXES = Object.freeze({
  copilot: ["copilot", "github-copilot", "github"],
  openai: ["openai", "codex"],
  anthropic: ["anthropic"],
  vertex: ["vertex"],
  gemini: ["gemini", "google"],
});

const CLAUDE_VERTEX_ALIAS_PATTERNS = Object.freeze({
  sonnet: ["vertex/*sonnet*"],
  "sonnet-6x": ["vertex/*sonnet-4-5-*", "vertex/*sonnet-4-6*", "vertex/*sonnet-5*"],
  haiku: ["vertex/*haiku*"],
  opus: ["vertex/*opus*"],
  fable: ["vertex/*fable*"],
  any: ["vertex/*"],
});

/**
 * A known alias could not resolve because its catalog is unavailable or no
 * catalog entry matches. Keep those causes distinct; neither permits forwarding
 * an unresolved alias to Copilot. `unsupported_endpoint` means the model is live but
 * its catalogue `supported_endpoints` do not include the endpoint the engine calls.
 */
class ModelAliasResolutionError extends Error {
  /**
   * @param {string} configuredModel
   * @param {"catalog_unavailable" | "model_unavailable" | "unsupported_endpoint"} [reason]
   * @param {{ provider?: string }} [options] The Copilot integration sentence appears only
   *   when the provider is Copilot or unknown.
   */
  constructor(configuredModel, reason = "catalog_unavailable", options = {}) {
    const provider = canonicalModelProvider(options.provider || "");
    const copilot = !provider || provider === "copilot";
    super(
      reason === "unsupported_endpoint"
        ? `The requested model cannot be used by this engine: '${configuredModel}' does not support the endpoint the engine calls in the workflow's live model catalog. Select a model offered to this workflow for that endpoint.`
        : reason === "model_unavailable"
          ? copilot
            ? `The requested model is not supported: '${configuredModel}' is not available in the workflow's live model catalog. For Copilot, availability also depends on the agentic-workflows integration, not just organization policy. Select a model offered to this workflow; enabling a missing model for that integration requires Copilot API support.`
            : `The requested model is not supported: '${configuredModel}' is not available in the workflow's live model catalog. Select a model offered to this workflow.`
          : `model-catalog retrieval prevented alias resolution for '${configuredModel}'`
    );
    this.name = "ModelAliasResolutionError";
    this.configuredModel = configuredModel;
    this.reason = reason;
    this.provider = provider || undefined;
  }
}

/**
 * @param {string} model
 * @returns {{ base: string, params: URLSearchParams }}
 */
function splitModelIdentifier(model) {
  const raw = String(model || "").trim();
  const qIndex = raw.indexOf("?");
  if (qIndex === -1) {
    return { base: raw, params: new URLSearchParams() };
  }
  const base = raw.slice(0, qIndex);
  const params = new URLSearchParams(raw.slice(qIndex + 1));
  return { base, params };
}

/**
 * Caller params win over entry params (spec §8.3 step 2b ii).
 * @param {URLSearchParams} caller
 * @param {URLSearchParams} entry
 * @returns {URLSearchParams}
 */
function mergeParams(caller, entry) {
  const merged = new URLSearchParams();
  for (const [key, value] of entry.entries()) {
    merged.set(key, value);
  }
  for (const [key, value] of caller.entries()) {
    merged.set(key, value);
  }
  return merged;
}

/**
 * @param {URLSearchParams} params
 * @returns {string}
 */
function marshalParams(params) {
  const serialized = params.toString();
  return serialized ? `?${serialized}` : "";
}

/**
 * Case-insensitive glob match; * does not cross / (spec §8.4).
 * @param {string} pattern
 * @param {string} entry
 * @returns {boolean}
 */
function globMatch(pattern, entry) {
  const patternParts = pattern.split("/", 2);
  const entryParts = entry.split("/", 2);
  if (patternParts.length === 1) {
    if (!pattern.includes("*")) {
      return pattern.toLowerCase() === entry.toLowerCase();
    }
    const escapedPattern = escapeRegex(pattern).replace(/\*/g, "[^/]*");
    const regex = new RegExp(`^${escapedPattern}$`, "i");
    return regex.test(entry);
  }
  if (entryParts.length === 1) {
    return false;
  }
  if (patternParts[0].toLowerCase() !== entryParts[0].toLowerCase()) {
    return false;
  }
  const escapedSuffixPattern = escapeRegex(patternParts[1]).replace(/\*/g, "[^/]*");
  const regex = new RegExp(`^${escapedSuffixPattern}$`, "i");
  return regex.test(entryParts[1]);
}

/**
 * @param {string} value
 * @returns {string}
 */
function escapeRegex(value) {
  return value.replace(/[.+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * @param {string} modelToken
 * @returns {number[]}
 */
function extractVersionTuple(modelToken) {
  const match = modelToken.match(/(\d+(?:\.\d+)*)/g);
  if (!match || match.length === 0) {
    return [0, 0, 0];
  }
  const tuple = [];
  for (const m of match) {
    for (const part of m.split(".")) {
      tuple.push(Number.parseInt(part, 10) || 0);
    }
  }
  return tuple;
}

/**
 * @param {number[]} left
 * @param {number[]} right
 * @returns {number}
 */
function compareVersionTuples(left, right) {
  const maxLen = Math.max(left.length, right.length);
  for (let i = 0; i < maxLen; i += 1) {
    const a = left[i] ?? 0;
    const b = right[i] ?? 0;
    if (a !== b) {
      return a - b;
    }
  }
  return 0;
}

/**
 * @param {string} pattern
 * @param {string[]} catalog
 * @returns {string|null}
 */
function selectLatestGlobMatch(pattern, catalog) {
  const matches = catalog.filter(entry => globMatch(pattern, entry));
  if (matches.length === 0) {
    return null;
  }
  matches.sort((left, right) => {
    const leftToken = left.includes("/") ? left.split("/", 2)[1] : left;
    const rightToken = right.includes("/") ? right.split("/", 2)[1] : right;
    return compareVersionTuples(extractVersionTuple(rightToken), extractVersionTuple(leftToken));
  });
  return matches[0];
}

/**
 * Build provider-scoped catalog entries from AWF /reflect data.
 * @param {any} reflectData
 * @returns {string[]}
 */
function buildCatalogFromReflect(reflectData) {
  /** @type {Set<string>} */
  const catalog = new Set();
  const endpoints = Array.isArray(reflectData?.endpoints) ? reflectData.endpoints : [];
  for (const endpoint of endpoints) {
    if (!endpoint || endpoint.configured !== true || !Array.isArray(endpoint.models)) {
      continue;
    }
    const providerRaw = String(endpoint.provider || "")
      .trim()
      .toLowerCase();
    const provider = providerRaw === "github-copilot" ? "copilot" : providerRaw;
    for (const model of endpoint.models) {
      const modelId = String(model || "").trim();
      if (!modelId) {
        continue;
      }
      if (modelId.includes("/")) {
        catalog.add(modelId);
        continue;
      }
      if (provider) {
        catalog.add(`${provider}/${modelId}`);
      }
      catalog.add(modelId);
    }
  }
  return [...catalog];
}

/**
 * @param {string} target
 * @param {Record<string, string[]>} aliasMap
 * @param {string[]} catalog
 * @param {{ visited?: Set<string>, logger?: (msg: string) => void }} [options]
 * @returns {string|null}
 */
function resolveModelAlias(target, aliasMap, catalog, options = {}) {
  const visited = options.visited ?? new Set();
  const logger = options.logger ?? (() => {});
  const { base, params } = splitModelIdentifier(target);
  if (!base) {
    return null;
  }

  if (Object.prototype.hasOwnProperty.call(aliasMap, base)) {
    if (visited.has(base)) {
      logger(`model alias resolution: circular alias reference at '${base}'`);
      return null;
    }
    visited.add(base);
    const entries = aliasMap[base];
    if (!Array.isArray(entries)) {
      return null;
    }
    for (const entry of entries) {
      const resolved = resolveAliasEntry(String(entry), params, aliasMap, catalog, new Set(visited), logger);
      if (resolved) {
        return resolved;
      }
    }
  }

  const exact = catalog.find(entry => {
    const entryBase = splitModelIdentifier(entry).base;
    return entryBase.toLowerCase() === base.toLowerCase() || entry.toLowerCase() === base.toLowerCase();
  });
  if (exact) {
    return exact + marshalParams(params);
  }

  return null;
}

/**
 * @param {string} entry
 * @param {URLSearchParams} inheritedParams
 * @param {Record<string, string[]>} aliasMap
 * @param {string[]} catalog
 * @param {Set<string>} visited
 * @param {(msg: string) => void} logger
 * @returns {string|null}
 */
function resolveAliasEntry(entry, inheritedParams, aliasMap, catalog, visited, logger) {
  const { base: eBase, params: eParams } = splitModelIdentifier(entry);
  const mergedParams = mergeParams(inheritedParams, eParams);
  const suffix = marshalParams(mergedParams);

  if (Object.prototype.hasOwnProperty.call(aliasMap, eBase)) {
    return resolveModelAlias(eBase + suffix, aliasMap, catalog, { visited, logger });
  }

  if (eBase.includes("*")) {
    const match = selectLatestGlobMatch(eBase, catalog);
    if (match) {
      return match + suffix;
    }
    return null;
  }

  if (eBase.includes("/")) {
    const found = catalog.find(item => item.toLowerCase() === eBase.toLowerCase());
    return found ? found + suffix : null;
  }

  const bare = catalog.find(item => splitModelIdentifier(item).base.toLowerCase() === eBase.toLowerCase());
  return bare ? bare + suffix : null;
}

/**
 * Copilot CLI expects bare model ids (no copilot/ prefix).
 * @param {string} resolved
 * @returns {string}
 */
function normalizeForCopilotCLI(resolved) {
  const { base, params } = splitModelIdentifier(resolved);
  const slash = base.indexOf("/");
  if (slash > 0) {
    const provider = base.slice(0, slash).toLowerCase();
    if (provider === "copilot" || provider === "github-copilot") {
      return base.slice(slash + 1) + marshalParams(params);
    }
  }
  return resolved;
}

/**
 * A failed candidate-provider fetch cannot establish that an alias is unavailable.
 * @param {string} aliasKey
 * @param {Record<string, string[]>} aliasMap
 * @param {any} reflectData
 * @returns {boolean}
 */
function hasIncompleteCandidateCatalog(aliasKey, aliasMap, reflectData) {
  const endpoints = Array.isArray(reflectData?.endpoints) ? reflectData.endpoints : [];
  const failedEndpoints = endpoints.filter(endpoint => endpoint?.configured === true && !Array.isArray(endpoint.models));
  const visited = new Set();
  function hasFailedCandidate(target) {
    const { base } = splitModelIdentifier(target);
    if (Object.prototype.hasOwnProperty.call(aliasMap, base)) {
      if (visited.has(base)) return false;
      visited.add(base);
      const entries = aliasMap[base];
      return Array.isArray(entries) && entries.some(entry => hasFailedCandidate(String(entry)));
    }
    const slash = base.indexOf("/");
    if (slash === -1) return failedEndpoints.length > 0;
    const provider = base.slice(0, slash).toLowerCase();
    return failedEndpoints.some(endpoint => {
      const endpointProvider = String(endpoint.provider || "")
        .trim()
        .toLowerCase();
      return (endpointProvider === "github-copilot" ? "copilot" : endpointProvider) === provider;
    });
  }
  return hasFailedCandidate(aliasKey);
}

/**
 * Canonical provider name used to scope alias lookups (copilot, openai, anthropic, gemini).
 * @param {string} provider
 * @returns {string}
 */
function canonicalModelProvider(provider) {
  const normalized = String(provider || "")
    .trim()
    .toLowerCase();
  if (!normalized) return "";
  for (const [canonical, prefixes] of Object.entries(MODEL_PROVIDER_PREFIXES)) {
    if (prefixes.includes(normalized)) return canonical;
  }
  return normalized;
}

/**
 * Build the catalog for one provider. Entries carry every accepted prefix for that
 * provider plus the bare id, so alias entries such as `google/gemini-*` and
 * `gemini/gemini-*` both match the Gemini endpoint.
 * @param {{ reflectData?: any, catalog?: string[], provider: string }} options
 * @returns {string[]}
 */
function buildProviderCatalog(options) {
  const scope = canonicalModelProvider(options.provider);
  let source = options.catalog;
  if (!Array.isArray(source)) {
    const endpoints = Array.isArray(options.reflectData?.endpoints) ? options.reflectData.endpoints : [];
    source = buildCatalogFromReflect({ endpoints: endpoints.filter(endpoint => endpoint?.configured === true && canonicalModelProvider(endpoint.provider) === scope) });
  }
  const prefixes = (MODEL_PROVIDER_PREFIXES[scope] || [scope]).filter(prefix => prefix !== "github");
  /** @type {Set<string>} */
  const catalog = new Set();
  for (const entry of source) {
    // Gemini's native model list names models as "models/<id>".
    const value = String(entry || "")
      .trim()
      .replace(/^models\//i, "");
    const slash = value.indexOf("/");
    if (slash >= 0 && canonicalModelProvider(value.slice(0, slash)) !== scope) continue;
    const modelId = slash >= 0 ? value.slice(slash + 1) : value;
    if (!modelId) continue;
    for (const prefix of prefixes) catalog.add(`${prefix}/${modelId}`);
    catalog.add(modelId);
  }
  return [...catalog];
}

/**
 * Whether `modelId` (no provider prefix, no parameters) exactly names a live catalogue
 * entry, either bare or behind one provider prefix (`copilot/gpt-5-mini`, `models/...`).
 * @param {string} modelId
 * @param {string[]} catalog
 * @returns {boolean}
 */
function isLiveModelId(modelId, catalog) {
  if (!modelId) return false;
  return catalog.some(entry => {
    const entryBase = splitModelIdentifier(entry).base;
    if (entryBase === modelId) return true;
    const slash = entryBase.indexOf("/");
    return slash >= 0 && entryBase.slice(slash + 1) === modelId;
  });
}

/** @type {Set<string>|null} */
let bundledModelIds = null;

/**
 * Whether `modelId` (no provider prefix, no parameters) is listed, under any provider, in
 * the bundled model catalogue (`models.json`, shipped with the setup action). This is the
 * "plausible model id" test used when no `/reflect` catalogue can exist: a name such as
 * `gpt-5-mini` is a model id there, while a pure alias such as `small`, `opus` or `auto`
 * is not.
 * @param {string} modelId
 * @returns {boolean}
 */
function isBundledModelId(modelId) {
  if (!modelId) return false;
  if (!bundledModelIds) {
    bundledModelIds = new Set();
    try {
      const providers = JSON.parse(fs.readFileSync(path.join(__dirname, "models.json"), "utf8"))?.providers || {};
      for (const section of Object.values(providers)) {
        for (const id of Object.keys(section?.models || {})) bundledModelIds.add(id.toLowerCase());
      }
    } catch {
      // No bundled catalogue: nothing counts as a plausible model id.
    }
  }
  return bundledModelIds.has(modelId.toLowerCase());
}

/**
 * @typedef {{
 *   configuredModel: string,
 *   resolvedModel: string,
 *   modelId: string,
 *   provider: string,
 *   alias: boolean,
 *   status: "resolved" | "exact",
 * }} ConfiguredModelResolution
 */

/**
 * Resolve a configured model with the rules shared by every engine:
 * - a gh-aw alias (built-in or workflow-defined), optionally behind a `provider/` prefix,
 *   resolves to the newest live match in the `/reflect` catalogue;
 * - a provider prefix (or the engine's `provider`) limits the lookup to that provider;
 * - exact ids are never rewritten: a value whose id (after removing the provider prefix)
 *   exactly matches a live id in that provider's catalogue is returned unchanged even when
 *   an alias has the same name (for example `gpt-5-mini`); the alias map is used only when
 *   there is no exact live match;
 * - `?effort=` and other parameters carry through.
 * - when no `/reflect` catalogue can exist on this run (`noCatalog`, set when AWF is
 *   disabled), a value whose id is listed in the bundled model catalogue
 *   (`isBundledModelId`) is exact and passes through even when an alias has the same
 *   name; any other alias fails with catalog_unavailable.
 *
 * `resolvedModel` keeps the configured form: a provider-prefixed alias resolves to a
 * provider-prefixed id, a provider-scoped bare alias resolves to a bare id, and an
 * unscoped alias resolves to the catalogue entry.
 *
 * @param {{
 *   configuredModel: string,
 *   aliasMap: Record<string, string[]>|null|undefined,
 *   reflectData?: any,
 *   catalog?: string[],
 *   provider?: string,
 *   preserveAuto?: boolean,
 *   noCatalog?: boolean,
 *   logger?: (msg: string) => void,
 *   logPrefix?: string,
 * }} options
 * @returns {ConfiguredModelResolution}
 * @throws {ModelAliasResolutionError} When the model is an alias that cannot resolve.
 */
function resolveConfiguredModel(options) {
  const configuredModel = String(options.configuredModel || "").trim();
  const logger = options.logger ?? (() => {});
  const logPrefix = options.logPrefix || "model alias resolution";
  const aliasMap = options.aliasMap && typeof options.aliasMap === "object" && !Array.isArray(options.aliasMap) ? options.aliasMap : null;
  const { base, params } = splitModelIdentifier(configuredModel);
  const slash = base.indexOf("/");
  const prefix = slash > 0 ? base.slice(0, slash) : "";
  const aliasKey = slash > 0 ? base.slice(slash + 1) : base;
  const scope = canonicalModelProvider(prefix || options.provider || "");
  /** @type {ConfiguredModelResolution} */
  const exact = { configuredModel, resolvedModel: configuredModel, modelId: slash > 0 ? configuredModel.slice(slash + 1) : configuredModel, provider: scope, alias: false, status: "exact" };
  if (!configuredModel || !aliasMap || !Object.prototype.hasOwnProperty.call(aliasMap, aliasKey)) {
    return exact;
  }
  if (options.preserveAuto && aliasKey.toLowerCase() === "auto") {
    return exact;
  }
  if (options.noCatalog && isBundledModelId(aliasKey)) {
    // No live catalogue can exist (AWF disabled): a bundled model id stays exact.
    logger(`${logPrefix}: no live catalogue on this run; '${configuredModel}' is a bundled model id${scope ? ` (provider=${scope})` : ""}; passing it through as exact`);
    return exact;
  }

  const catalog = scope ? buildProviderCatalog({ reflectData: options.reflectData, catalog: options.catalog, provider: scope }) : Array.isArray(options.catalog) ? options.catalog : buildCatalogFromReflect(options.reflectData);
  if (isLiveModelId(aliasKey, catalog)) {
    // A live exact id wins over an alias with the same name (for example gpt-5-mini).
    logger(`${logPrefix}: '${configuredModel}' is a live model id${scope ? ` (provider=${scope})` : ""}; not resolving it as an alias`);
    return exact;
  }
  if (catalog.length === 0) {
    // Do not silently forward a known alias unchanged: the API proxy rejects unresolved
    // aliases with a misleading "no AI credits pricing" error instead of surfacing the
    // real cause (catalog retrieval failure). Callers should retry/refresh the catalog
    // and, if it still cannot be built, stop before invoking the engine.
    logger(`${logPrefix}: catalog unavailable from awf-reflect for known alias '${configuredModel}'${scope ? ` (provider=${scope})` : ""}`);
    throw new ModelAliasResolutionError(configuredModel, "catalog_unavailable", { provider: scope });
  }

  const resolved = resolveModelAlias(aliasKey + marshalParams(params), aliasMap, catalog, { logger });
  if (!resolved) {
    if (!scope && !Array.isArray(options.catalog) && hasIncompleteCandidateCatalog(aliasKey, aliasMap, options.reflectData)) {
      logger(`${logPrefix}: candidate provider catalog unavailable from awf-reflect for known alias '${configuredModel}'`);
      throw new ModelAliasResolutionError(configuredModel, "catalog_unavailable", { provider: scope });
    }
    logger(`${logPrefix}: known alias '${configuredModel}' did not resolve against catalog${scope ? ` (provider=${scope})` : ""}`);
    throw new ModelAliasResolutionError(configuredModel, "model_unavailable", { provider: scope });
  }

  const resolvedParts = splitModelIdentifier(resolved);
  const resolvedSlash = resolvedParts.base.indexOf("/");
  const modelId = (resolvedSlash >= 0 ? resolvedParts.base.slice(resolvedSlash + 1) : resolvedParts.base) + marshalParams(resolvedParts.params);
  const resolvedProvider = resolvedSlash >= 0 ? canonicalModelProvider(resolvedParts.base.slice(0, resolvedSlash)) : scope;
  const resolvedModel = prefix ? `${prefix}/${modelId}` : scope ? modelId : resolved;
  return { configuredModel, resolvedModel, modelId, provider: resolvedProvider, alias: true, status: "resolved" };
}

/**
 * Resolve configured COPILOT_MODEL when it is a gh-aw alias.
 * @param {{
 *   configuredModel: string,
 *   aliasMap: Record<string, string[]>|null|undefined,
 *   reflectData: object|null|undefined,
 *   logger?: (msg: string) => void,
 * }} options
 * @returns {string}
 * @throws {ModelAliasResolutionError} When `configuredModel` is a known alias key in
 *   `aliasMap` but alias resolution cannot produce a concrete model from the catalog
 *   built from `reflectData` (including empty or incomplete catalogs). Callers must not
 *   forward the unresolved alias to Copilot in this case.
 */
function resolveConfiguredCopilotModel(options) {
  const logger = options.logger ?? (() => {});
  const result = resolveConfiguredModel({ ...options, logPrefix: "copilot model alias resolution" });
  const normalized = normalizeForCopilotCLI(result.resolvedModel);
  if (result.alias && normalized !== result.configuredModel) {
    logger(`copilot model alias resolution: '${result.configuredModel}' -> '${normalized}'`);
  }
  return normalized;
}

/**
 * Run `resolveConfiguredModel`, refetching `/reflect` once when resolution fails.
 * A `model_unavailable` failure from the first attempt wins over a later
 * `catalog_unavailable` failure, because the first catalogue was complete.
 * @param {Parameters<typeof resolveConfiguredModel>[0] & { refetchReflectData?: () => Promise<any> }} options
 * @returns {Promise<ConfiguredModelResolution & { reflectData: any }>}
 */
async function resolveConfiguredModelWithRefresh(options) {
  const logger = options.logger ?? (() => {});
  const logPrefix = options.logPrefix || "model alias resolution";
  try {
    return { ...resolveConfiguredModel(options), reflectData: options.reflectData };
  } catch (err) {
    if (!(err instanceof ModelAliasResolutionError)) throw err;
    logger(`${logPrefix}: retrying awf-reflect model-catalog fetch once before failing for '${err.configuredModel}'`);
    const refreshedReflectData = options.refetchReflectData ? await options.refetchReflectData() : null;
    try {
      return { ...resolveConfiguredModel({ ...options, reflectData: refreshedReflectData, noCatalog: options.noCatalog && !refreshedReflectData }), reflectData: refreshedReflectData };
    } catch (retryErr) {
      if (!(retryErr instanceof ModelAliasResolutionError)) throw retryErr;
      throw err.reason === "model_unavailable" ? err : retryErr;
    }
  }
}

/**
 * Environment variable the compiler sets on engine steps with the workflow-defined and
 * imported aliases (only entries that differ from the built-ins). It is omitted when the
 * workflow defines and imports no aliases.
 */
const MODEL_ALIASES_ENV_VAR = "GH_AW_MODEL_ALIASES";

/** @type {Record<string, string[]>|null|undefined} */
let builtinModelAliases;

/**
 * The built-in alias map shipped with the setup action (`model_aliases.json`, a copy of
 * pkg/workflow/data/model_aliases.json). Returns a fresh copy, or null when the bundled
 * file is missing or invalid.
 * @returns {Record<string, string[]>|null}
 */
function loadBuiltinModelAliases() {
  if (builtinModelAliases === undefined) {
    builtinModelAliases = null;
    try {
      const aliases = JSON.parse(fs.readFileSync(path.join(__dirname, "model_aliases.json"), "utf8"))?.aliases;
      if (aliases && typeof aliases === "object" && !Array.isArray(aliases)) builtinModelAliases = aliases;
    } catch {
      // No bundled built-ins.
    }
  }
  return builtinModelAliases ? Object.fromEntries(Object.entries(builtinModelAliases).map(([alias, entries]) => [alias, Array.isArray(entries) ? [...entries] : entries])) : null;
}

/**
 * Load the merged alias map: a base map overlaid with the workflow-defined and imported
 * aliases from an explicit environment variable or `GH_AW_MODEL_ALIASES`. The base map is
 * the AWF config written by the compiler (`apiProxy.models`, already merged), then
 * `/reflect` `model_aliases`, then the built-in aliases bundled with the setup action
 * (the only source when AWF is disabled).
 * @param {{ reflectData?: any, env?: NodeJS.ProcessEnv, envVar?: string }} [options]
 * @returns {Record<string, string[]>|null}
 */
function loadModelAliasMap(options = {}) {
  const env = options.env || process.env;
  const isMap = value => value && typeof value === "object" && !Array.isArray(value);
  /** @type {Record<string, string[]>|null} */
  let overrides = null;
  for (const envVar of [options.envVar, MODEL_ALIASES_ENV_VAR]) {
    if (!envVar || !env[envVar]) continue;
    try {
      const parsed = JSON.parse(String(env[envVar]));
      if (isMap(parsed)) {
        overrides = parsed;
        break;
      }
    } catch {
      // Fall through to the next source.
    }
  }
  /** @type {Record<string, string[]>|null} */
  let base = null;
  try {
    const configPath = env.GH_AW_AWF_CONFIG_PATH || path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", "awf-config.json");
    const models = JSON.parse(fs.readFileSync(configPath, "utf8"))?.apiProxy?.models;
    if (isMap(models)) base = models;
  } catch {
    // Fall through to /reflect.
  }
  if (!base && isMap(options.reflectData?.model_aliases)) base = options.reflectData.model_aliases;
  if (!base) base = loadBuiltinModelAliases();
  if (!base && !overrides) return null;
  return { ...(base || {}), ...(overrides || {}) };
}

/**
 * Append (or replace, per engine and role) one entry in agent/model-resolution.json.
 * Best effort: recording never changes the run outcome.
 * @param {{ engine: string, role: string, configured_model: string, resolved_model: string|null, provider: string|null, status: "resolved"|"exact"|"failed", cause: "model_unavailable"|"catalog_unavailable"|"unsupported_endpoint"|null }} record
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {boolean}
 */
function recordModelResolution(record, env = process.env) {
  if (env.GH_AW_PHASE && env.GH_AW_PHASE !== "agent") return false;
  try {
    const file = path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", MODEL_RESOLUTION_RECORD_PATH);
    /** @type {any[]} */
    let records = [];
    try {
      const existing = JSON.parse(fs.readFileSync(file, "utf8"));
      if (Array.isArray(existing)) records = existing;
    } catch {
      records = [];
    }
    const bound = value => (typeof value === "string" ? value.slice(0, 200) : null);
    const entry = {
      engine: bound(record.engine) || "unknown",
      role: bound(record.role) || "main",
      configured_model: bound(record.configured_model) || "",
      resolved_model: bound(record.resolved_model),
      provider: bound(record.provider) || null,
      status: record.status,
      cause: record.cause || null,
    };
    records = records.filter(existing => !(existing?.engine === entry.engine && existing?.role === entry.role));
    records.push(entry);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, JSON.stringify(records, null, 2) + "\n");
    return true;
  } catch {
    return false;
  }
}

/**
 * The shared "resolve configured model" step. Every harness calls this before it
 * launches its CLI, for the main model and for sub-agent models where the engine has
 * them. It resolves the model, refetches `/reflect` once on failure, logs the
 * configured and resolved model, and records the outcome under `agent/`.
 *
 * Engine-specific handling (prefix checks, CLI id overrides, `auto`) runs after
 * resolution through `normalize`, which returns the model handed to the CLI.
 *
 * `noCatalog` defaults to true when AWF is disabled (`AWF_REFLECT_ENABLED` is not "1")
 * and neither `/reflect` data nor a catalogue is supplied.
 *
 * @param {Omit<Parameters<typeof resolveConfiguredModel>[0], "aliasMap"> & {
 *   aliasMap?: Record<string, string[]>|null,
 *   engine: string,
 *   role?: string,
 *   refetchReflectData?: () => Promise<any>,
 *   normalize?: (result: ConfiguredModelResolution) => string,
 *   env?: NodeJS.ProcessEnv,
 *   record?: boolean,
 * }} options
 * @returns {Promise<ConfiguredModelResolution & { reflectData: any, engineModel: string }>}
 */
async function resolveEngineModel(options) {
  const logger = options.logger ?? (() => {});
  const env = options.env || process.env;
  const role = options.role || "main";
  const configuredModel = String(options.configuredModel || "").trim();
  let aliasMap = options.aliasMap !== undefined ? options.aliasMap : loadModelAliasMap({ reflectData: options.reflectData, env });
  if (options.engine === "claude" && canonicalModelProvider(options.provider || "") === "vertex") {
    const vertexAliasMap = { ...(aliasMap && typeof aliasMap === "object" && !Array.isArray(aliasMap) ? aliasMap : {}) };
    for (const [alias, patterns] of Object.entries(CLAUDE_VERTEX_ALIAS_PATTERNS)) {
      const existingPatterns = Array.isArray(vertexAliasMap[alias]) ? vertexAliasMap[alias] : [];
      vertexAliasMap[alias] = [...existingPatterns, ...patterns.filter(pattern => !existingPatterns.includes(pattern))];
    }
    aliasMap = vertexAliasMap;
  }
  // Without AWF no /reflect catalogue can exist; an explicit catalogue or /reflect data overrides.
  const noCatalog = options.noCatalog ?? (env.AWF_REFLECT_ENABLED !== "1" && !options.reflectData && !Array.isArray(options.catalog));
  const record = (/** @type {any} */ fields) => {
    if (options.record !== false) recordModelResolution({ engine: options.engine, role, configured_model: configuredModel, ...fields }, env);
  };
  let result;
  try {
    result = await resolveConfiguredModelWithRefresh({ ...options, aliasMap, noCatalog });
  } catch (err) {
    if (err instanceof ModelAliasResolutionError) {
      logger(`model resolution: engine=${options.engine} role=${role} configured=${configuredModel} failed cause=${err.reason}: ${err.message}`);
      record({ resolved_model: null, provider: err.provider || canonicalModelProvider(options.provider || "") || null, status: "failed", cause: err.reason });
    }
    throw err;
  }
  const engineModel = options.normalize ? options.normalize(result) : result.resolvedModel;
  logger(`model resolution: engine=${options.engine} role=${role} configured=${configuredModel} resolved=${engineModel} provider=${result.provider || "(unscoped)"} status=${result.status}`);
  record({ resolved_model: engineModel, provider: result.provider || null, status: result.status, cause: null });
  return { ...result, engineModel };
}

module.exports = {
  buildCatalogFromReflect,
  buildProviderCatalog,
  canonicalModelProvider,
  globMatch,
  isBundledModelId,
  isLiveModelId,
  loadBuiltinModelAliases,
  loadModelAliasMap,
  mergeParams,
  MODEL_ALIASES_ENV_VAR,
  MODEL_RESOLUTION_RECORD_PATH,
  ModelAliasResolutionError,
  normalizeForCopilotCLI,
  recordModelResolution,
  resolveConfiguredCopilotModel,
  resolveConfiguredModel,
  resolveConfiguredModelWithRefresh,
  resolveEngineModel,
  resolveModelAlias,
  selectLatestGlobMatch,
  splitModelIdentifier,
};
