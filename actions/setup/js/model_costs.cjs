// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");

/** @type {Array<{id: string, provider: string, model: string, pricing: Record<string, number>}> | null | undefined} */
let _catalog = undefined;

function getModelsPath() {
  const override = process.env.GH_AW_MODELS_JSON_PATH;
  return override && override.trim() ? override : null;
}

/**
 * @param {unknown} value
 * @returns {number}
 */
function parsePrice(value) {
  if (typeof value === "number" && Number.isFinite(value)) return value;
  if (typeof value === "string" && value.trim()) {
    const parsed = Number.parseFloat(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return 0;
}

/**
 * @returns {Array<{id: string, provider: string, model: string, pricing: Record<string, number>}>}
 */
function loadCatalog() {
  if (_catalog !== undefined) {
    return _catalog || [];
  }

  const modelsPath = getModelsPath();
  if (!modelsPath) return [];
  try {
    const raw = fs.readFileSync(modelsPath, "utf8");
    const parsed = JSON.parse(raw);
    const providers = parsed?.providers && typeof parsed.providers === "object" ? parsed.providers : {};
    _catalog = Object.entries(providers).flatMap(([providerName, providerData]) => {
      const provider = normalizeProvider(providerName);
      if (!provider) return [];
      const models = providerData?.models && typeof providerData.models === "object" ? providerData.models : {};
      return Object.entries(models).map(([modelName, modelData]) => {
        const model = normalizeModel(modelName);
        const id = `${provider}/${model}`;
        /** @type {Record<string, number>} */
        const pricing = {};
        for (const [key, value] of Object.entries(modelData?.cost || {})) {
          pricing[key] = parsePrice(value);
        }
        return { id, provider, model, pricing };
      });
    });
  } catch {
    _catalog = null;
  }

  return _catalog || [];
}

/**
 * @param {string} provider
 * @returns {string}
 */
function normalizeProvider(provider) {
  const normalized = String(provider || "")
    .trim()
    .toLowerCase();
  if (normalized === "github" || normalized === "copilot" || normalized === "github_models") return "github-copilot";
  return normalized;
}

/**
 * @param {string} model
 * @returns {string}
 */
function normalizeModel(model) {
  return String(model || "")
    .trim()
    .toLowerCase();
}

/**
 * @param {string} value
 * @returns {string}
 */
function normalizeComparableID(value) {
  return normalizeModel(value).replace(/[._]+/g, "-");
}

/**
 * @param {string} provider
 * @returns {boolean}
 */
function providerIncludesCacheReadsInInput(provider) {
  switch (normalizeProvider(provider)) {
    case "":
    case "anthropic":
    case "openai":
    case "azure-openai":
    case "azure_openai":
    case "github-copilot": // proxies OpenAI and Anthropic models that bundle cache reads in input
      return true;
    default:
      return false;
  }
}

/**
 * @param {string} provider
 * @param {string} model
 * @returns {Record<string, number> | null}
 */
function findModelPricing(provider, model) {
  const catalog = loadCatalog();
  const normalizedProvider = normalizeProvider(provider);
  const normalizedModel = normalizeModel(model);
  const comparableModel = normalizeComparableID(model);
  if (!normalizedModel) return null;

  // When the model name embeds a provider prefix (e.g., "copilot/claude-sonnet-4.6"),
  // normalize that embedded prefix so it matches the catalog (e.g., "github-copilot/claude-sonnet-4.6").
  let fullID;
  if (normalizedModel.includes("/")) {
    const slashIdx = normalizedModel.indexOf("/");
    const embeddedProvider = normalizeProvider(normalizedModel.slice(0, slashIdx));
    fullID = `${embeddedProvider}/${normalizedModel.slice(slashIdx + 1)}`;
  } else {
    fullID = normalizedProvider ? `${normalizedProvider}/${normalizedModel}` : "";
  }
  const comparableFullID = normalizeComparableID(fullID);

  for (const entry of catalog) {
    if ((fullID && entry.id === fullID) || (comparableFullID && normalizeComparableID(entry.id) === comparableFullID)) {
      return entry.pricing;
    }
  }

  /** @type {Record<string, number> | null} */
  let bestProviderScoped = null;
  let bestProviderScopedLen = -1;
  /** @type {Record<string, number> | null} */
  let bestGeneric = null;
  let bestGenericLen = -1;

  for (const entry of catalog) {
    const comparableEntryModel = normalizeComparableID(entry.model);
    if (entry.model === normalizedModel || comparableEntryModel === comparableModel) {
      if (normalizedProvider && entry.provider === normalizedProvider) {
        return entry.pricing;
      }
      if (!bestGeneric) {
        bestGeneric = entry.pricing;
      }
      continue;
    }

    if (normalizedModel.startsWith(entry.model) || comparableModel.startsWith(comparableEntryModel)) {
      if (normalizedProvider && entry.provider === normalizedProvider && entry.model.length > bestProviderScopedLen) {
        bestProviderScoped = entry.pricing;
        bestProviderScopedLen = entry.model.length;
      }
      if (entry.model.length > bestGenericLen) {
        bestGeneric = entry.pricing;
        bestGenericLen = entry.model.length;
      }
    }
  }

  return bestProviderScoped || bestGeneric;
}

/**
 * @param {number} usd
 * @returns {number}
 */
function usdToAIC(usd) {
  return usd / 0.01;
}

/**
 * @param {Object} params
 * @param {string} params.provider
 * @param {string} params.model
 * @param {number} params.inputTokens
 * @param {number} params.outputTokens
 * @param {number} params.cacheReadTokens
 * @param {number} params.cacheWriteTokens
 * @param {number} [params.reasoningTokens]
 * @param {boolean} [params.inputTokensIncludeCache]
 * @returns {number}
 */
function computeInferenceCostUSD({ provider, model, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, reasoningTokens = 0, inputTokensIncludeCache }) {
  const pricing = findModelPricing(provider, model);
  if (!pricing) return 0;

  const input = inputTokens || 0;
  const output = outputTokens || 0;
  const cacheRead = cacheReadTokens || 0;
  const cacheWrite = cacheWriteTokens || 0;
  const reasoning = reasoningTokens || 0;
  let effectiveInput = input;
  if (inputTokensIncludeCache === true) {
    effectiveInput = Math.max(input - cacheRead - cacheWrite, 0);
  } else if (typeof inputTokensIncludeCache !== "boolean" && providerIncludesCacheReadsInInput(provider)) {
    effectiveInput = Math.max(input - cacheRead, 0);
  }

  const promptPrice = pricing.input || 0;
  const completionPrice = pricing.output || 0;
  const cacheReadPrice = pricing.cache_read || promptPrice;
  const cacheWritePrice = pricing.cache_write || promptPrice;
  const reasoningPrice = pricing.reasoning || completionPrice;

  return effectiveInput * promptPrice + output * completionPrice + cacheRead * cacheReadPrice + cacheWrite * cacheWritePrice + reasoning * reasoningPrice;
}

/**
 * @param {Object} params
 * @param {string} params.provider
 * @param {string} params.model
 * @param {number} params.inputTokens
 * @param {number} params.outputTokens
 * @param {number} params.cacheReadTokens
 * @param {number} params.cacheWriteTokens
 * @param {number} [params.reasoningTokens]
 * @param {boolean} [params.inputTokensIncludeCache]
 * @returns {number}
 */
function computeInferenceAIC(params) {
  return usdToAIC(computeInferenceCostUSD(params));
}

/**
 * @param {number} value
 * @returns {string}
 */
function formatAIC(value) {
  if (!Number.isFinite(value) || value <= 0) return "";
  if (value >= 1000) {
    /** @type {[number, string][]} */
    const units = [
      [1_000_000, "M"],
      [1_000, "K"],
    ];
    for (const [threshold, suffix] of units) {
      if (value >= threshold) {
        return `${(value / threshold).toFixed(1).replace(/\.0$/, "")}${suffix}`;
      }
    }
  }
  if (value >= 10) return value.toFixed(1).replace(/\.0$/, "");
  if (value >= 1) return value.toFixed(2).replace(/0$/, "").replace(/\.0$/, "");
  return value.toFixed(3).replace(/0+$/, "").replace(/\.$/, "");
}

/**
 * Whole-credit footer estimates intentionally omit compact K/M notation.
 * @param {number} value
 * @returns {string}
 */
function formatEstimatedAIC(value) {
  if (!Number.isFinite(value) || value <= 0) return "";
  const nearestInteger = Math.round(value);
  // Normalize arithmetic noise only for display, without rounding tiny costs to zero.
  const normalized = nearestInteger > 0 && Math.abs(value - nearestInteger) <= Number.EPSILON * value ? nearestInteger : value;
  return `~${Math.ceil(normalized)}`;
}

/** @type {Record<string, unknown> | null | undefined} */
let _modelsJson = undefined;

/**
 * Parse a models.json file at the given path.
 * Returns the parsed object, or null if the file does not exist or is not a plain object.
 *
 * @param {string} filePath
 * @returns {Record<string, unknown> | null}
 */
function parseModelsJsonFile(filePath) {
  try {
    const raw = fs.readFileSync(filePath, "utf8");
    const parsed = JSON.parse(raw);
    const isPlainObject = parsed !== null && typeof parsed === "object" && !Array.isArray(parsed);
    return isPlainObject ? /** @type {Record<string, unknown>} */ parsed : null;
  } catch {
    return null;
  }
}

/**
 * Load and return an explicitly configured models.json overlay.
 *
 * Returns the full JSON object (including `provider_type` fields per model entry)
 * so callers can perform custom lookups like provider-type resolution.
 * The result is cached after the first successful read.
 *
 * Without a readable GH_AW_MODELS_JSON_PATH, callers use AWF metadata and
 * reported costs rather than a local default pricing catalog.
 *
 * @returns {Record<string, unknown> | null}
 */
function loadModelsJson() {
  if (_modelsJson !== undefined) {
    return _modelsJson ?? null;
  }
  const modelsPath = getModelsPath();
  _modelsJson = modelsPath ? parseModelsJsonFile(modelsPath) : null;
  return _modelsJson ?? null;
}

function _resetModelCostsCache() {
  _catalog = undefined;
  _modelsJson = undefined;
}

module.exports = {
  computeInferenceAIC,
  computeInferenceCostUSD,
  findModelPricing,
  formatAIC,
  formatEstimatedAIC,
  getModelsPath,
  loadCatalog,
  loadModelsJson,
  usdToAIC,
  _resetModelCostsCache,
};
