// @ts-check

const fs = require("fs");
const path = require("path");

const { computeInferenceAIC, findModelPricing, formatAIC } = require("./model_costs.cjs");

const TOKEN_USAGE_FILENAME = "token-usage.jsonl";

/**
 * @param {string} root
 * @returns {string[]}
 */
function findJSONLFiles(root) {
  /** @type {string[]} */
  const files = [];
  if (!root || !fs.existsSync(root)) {
    return files;
  }

  /** @type {string[]} */
  const queue = [root];
  for (let index = 0; index < queue.length; index++) {
    const current = queue[index];
    if (!current) continue;
    /** @type {fs.Dirent[]} */
    let entries = [];
    try {
      entries = fs.readdirSync(current, { withFileTypes: true });
    } catch {
      continue;
    }
    for (const entry of entries) {
      const fullPath = path.join(current, entry.name);
      if (entry.isDirectory()) {
        queue.push(fullPath);
        continue;
      }
      if (entry.isFile() && entry.name.endsWith(".jsonl")) {
        files.push(fullPath);
      }
    }
  }
  return files;
}

/**
 * @param {Array<string>} filePaths
 * @returns {number}
 */
function sumAICFromUsageJSONLFiles(filePaths, options = {}) {
  if (!Array.isArray(filePaths) || filePaths.length === 0) {
    if (options.strict) throw new Error("No daily AIC accounting files");
    return 0;
  }

  /**
   * @param {unknown} usage
   * @returns {Record<string, unknown> | null}
   */
  function normalizeUsageRecord(usage) {
    if (usage && typeof usage === "object" && !Array.isArray(usage)) {
      // prettier-ignore
      const record = /** @type {Record<string, unknown>} */ (usage);
      return record;
    }
    return null;
  }

  /**
   * @param {unknown} value
   * @returns {number | null}
   */
  function toFiniteNumber(value) {
    if (typeof value === "string" && !value.trim()) {
      return null;
    }

    const num = Number(value);
    return Number.isFinite(num) ? num : null;
  }

  function validatePresentNumbers(record) {
    const names = [
      "ai_credits",
      "aiCredits",
      "aic",
      "ai_credits_this_response",
      "ai_credits_total",
      "input_tokens",
      "inputTokens",
      "output_tokens",
      "outputTokens",
      "cache_read_tokens",
      "cacheReadTokens",
      "cache_write_tokens",
      "cacheWriteTokens",
      "reasoning_tokens",
      "reasoningTokens",
    ];
    for (const name of names) {
      if (!Object.hasOwn(record, name)) continue;
      const value = record[name];
      if ((typeof value !== "number" && typeof value !== "string") || (typeof value === "string" && !/^\s*(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?\s*$/.test(value)) || !Number.isFinite(Number(value)) || Number(value) < 0) {
        throw new Error(`Invalid numeric daily AIC field: ${name}`);
      }
    }
  }

  /**
   * @param {Record<string, unknown> | null} usage
   * @param {Record<string, unknown>} parsed
   * @param {string} snakeCase
   * @param {string} camelCase
   * @returns {number}
   */
  function getNumericField(usage, parsed, snakeCase, camelCase) {
    const candidates = [usage?.[snakeCase], usage?.[camelCase], parsed[snakeCase], parsed[camelCase]];
    for (const candidate of candidates) {
      const num = toFiniteNumber(candidate);
      if (num !== null) {
        return num;
      }
    }
    return 0;
  }

  /**
   * @param {Record<string, unknown> | null} usage
   * @param {Record<string, unknown>} parsed
   * @param {string[]} keys
   * @returns {number}
   */
  function getNumericAliasField(usage, parsed, keys) {
    for (const key of keys) {
      const candidates = [usage?.[key], parsed[key]];
      for (const candidate of candidates) {
        const num = toFiniteNumber(candidate);
        if (num !== null) {
          return num;
        }
      }
    }
    return 0;
  }

  /**
   * @param {Record<string, unknown> | null} usage
   * @param {Record<string, unknown>} parsed
   * @param {string} snakeCase
   * @param {string} camelCase
   * @returns {string}
   */
  function getStringField(usage, parsed, snakeCase, camelCase) {
    const candidates = [usage?.[snakeCase], usage?.[camelCase], parsed[snakeCase], parsed[camelCase]];
    for (const candidate of candidates) {
      if (typeof candidate === "string" && candidate.trim()) {
        return candidate;
      }
    }
    return "";
  }

  let total = 0;
  let observations = 0;
  const requestRecords = new Map();
  for (const filePath of filePaths) {
    if (!filePath || !fs.existsSync(filePath)) {
      continue;
    }

    let content;
    try {
      content = fs.readFileSync(filePath, "utf8");
    } catch (err) {
      throw new Error(`Failed to read file ${filePath}: ${String(err)}`, { cause: err });
    }
    if (!content.trim()) {
      continue;
    }

    for (const rawLine of content.split("\n")) {
      const line = rawLine.trim();
      if (!line || !line.startsWith("{")) {
        if (options.strict && line) throw new Error("Malformed daily AIC accounting record");
        continue;
      }

      try {
        const parsed = JSON.parse(line);
        if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
          if (options.strict) throw new Error("Invalid daily AIC accounting record");
          continue;
        }

        const usage = normalizeUsageRecord(parsed.usage);
        if (options.strict) {
          validatePresentNumbers(parsed);
          if (usage) validatePresentNumbers(usage);
          if (typeof parsed.request_id === "string" && parsed.request_id) {
            const key = `${parsed.event || "token_usage"}:${parsed.request_id}`;
            if (requestRecords.has(key)) {
              if (requestRecords.get(key) !== line) throw new Error("Conflicting daily AIC request records");
              continue;
            }
            requestRecords.set(key, line);
          }
          if (Object.hasOwn(parsed, "ai_credits_this_response")) {
            total += Number(parsed.ai_credits_this_response);
            observations++;
            continue;
          }
        }
        const explicitValues = ["ai_credits", "aiCredits", "aic"].flatMap(key => [usage?.[key], parsed[key]]).filter(value => value != null && value !== "");
        if (options.strict && explicitValues.some(value => !Number.isFinite(Number(value)) || Number(value) < 0)) {
          throw new Error("Invalid explicit daily AIC value");
        }
        if (explicitValues.length > 0) observations++;
        const explicitAICredits = getNumericAliasField(usage, parsed, ["ai_credits", "aiCredits"]);
        if (explicitAICredits > 0) {
          total += explicitAICredits;
          continue;
        }
        const explicitAIC = getNumericAliasField(usage, parsed, ["aic"]);
        if (explicitAIC > 0) {
          total += explicitAIC;
          continue;
        }

        const inference = {
          provider: getStringField(usage, parsed, "provider", "provider"),
          model: getStringField(usage, parsed, "model", "model"),
          inputTokens: getNumericField(usage, parsed, "input_tokens", "inputTokens"),
          outputTokens: getNumericField(usage, parsed, "output_tokens", "outputTokens"),
          cacheReadTokens: getNumericField(usage, parsed, "cache_read_tokens", "cacheReadTokens"),
          cacheWriteTokens: getNumericField(usage, parsed, "cache_write_tokens", "cacheWriteTokens"),
          reasoningTokens: getNumericField(usage, parsed, "reasoning_tokens", "reasoningTokens"),
          ...(options.strict && typeof parsed.input_tokens_include_cache === "boolean" ? { inputTokensIncludeCache: parsed.input_tokens_include_cache } : {}),
        };
        const hasTokens = [inference.inputTokens, inference.outputTokens, inference.cacheReadTokens, inference.cacheWriteTokens, inference.reasoningTokens].some(value => value > 0);
        if (options.strict && explicitValues.length === 0 && hasTokens && !findModelPricing(inference.provider, inference.model)) {
          throw new Error("No pricing for a daily AIC usage record");
        }
        const computed = computeInferenceAIC(inference);
        if (Number.isFinite(computed) && computed > 0) {
          total += computed;
          observations++;
        }
      } catch (error) {
        if (options.strict) throw new Error("Daily AIC accounting record could not be resolved", { cause: error });
        // Ignore malformed lines.
      }
    }
  }

  if (options.strict && observations === 0) {
    throw Object.assign(new Error("Daily AIC accounting has no complete usage observations"), { code: "AIC_USAGE_UNKNOWN" });
  }
  if (options.strict && !Number.isFinite(total)) {
    throw new Error("Daily AIC accounting total is not finite");
  }
  return total;
}

/**
 * @param {Array<{aic:number}>} runs
 * @returns {{count:number,total:number,average:number,min:number,max:number,stddev:number}}
 */
function calculateDailyAICStats(runs) {
  const values = runs.map(run => Number(run?.aic || 0)).filter(value => Number.isFinite(value) && value > 0);
  if (values.length === 0) {
    return { count: 0, total: 0, average: 0, min: 0, max: 0, stddev: 0 };
  }

  const total = values.reduce((sum, value) => sum + value, 0);
  const average = total / values.length;
  const min = values.reduce((smallest, value) => (value < smallest ? value : smallest), values[0]);
  const max = values.reduce((largest, value) => (value > largest ? value : largest), values[0]);
  const variance = values.length > 1 ? values.reduce((sum, value) => sum + (value - average) ** 2, 0) / (values.length - 1) : 0;

  return {
    count: values.length,
    total,
    average,
    min,
    max,
    stddev: Math.sqrt(variance),
  };
}

/**
 * @param {number | string | undefined} value
 * @returns {string}
 */
function formatAICCredits(value) {
  const numericValue = Number(value || 0);
  const safeValue = Number.isFinite(numericValue) ? Math.max(0, Math.ceil(numericValue)) : 0;
  return formatAIC(safeValue);
}

module.exports = {
  findJSONLFiles,
  sumAICFromUsageJSONLFiles,
  calculateDailyAICStats,
  formatAICCredits,
};
