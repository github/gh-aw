// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const path = require("path");
const { AWF_REFLECT_OUTPUT_PATH: AWF_REFLECT_PATH, AWF_REFLECT_LEGACY_PATH } = require("./awf_reflect_paths.cjs");

const AWF_CONFIG_PATH = "/tmp/gh-aw/awf-config.json";
const AWF_REFLECT_ARTIFACT_PATH = "/tmp/gh-aw/sandbox/firewall/awf-reflect.json";
const AWF_MODELS_PATH = "/tmp/gh-aw/sandbox/firewall/models.json";

/**
 * Read the AWF reflect payload that was persisted to disk by copilot_harness.cjs.
 * Returns null when the file is absent or unparseable (AWF not running / not enabled).
 * @returns {any|null}
 */
function readReflectData() {
  const source = fs.existsSync(AWF_REFLECT_PATH) ? AWF_REFLECT_PATH : AWF_REFLECT_LEGACY_PATH;
  if (!fs.existsSync(source)) {
    return null;
  }
  try {
    return JSON.parse(fs.readFileSync(source, "utf8"));
  } catch {
    return null;
  }
}

/**
 * Read the AWF config payload when available.
 * Returns null when the file is absent or unparseable.
 *
 * @returns {any|null}
 */
function readAWFConfigData() {
  if (!fs.existsSync(AWF_CONFIG_PATH)) {
    return null;
  }
  try {
    return JSON.parse(fs.readFileSync(AWF_CONFIG_PATH, "utf8"));
  } catch {
    return null;
  }
}

/**
 * Read the sandbox.firewall models.json payload when available.
 * Returns null when the file is absent or unparseable.
 *
 * @returns {any|null}
 */
function readRuntimeModelsData() {
  if (!fs.existsSync(AWF_MODELS_PATH)) {
    return null;
  }
  try {
    return JSON.parse(fs.readFileSync(AWF_MODELS_PATH, "utf8"));
  } catch {
    return null;
  }
}

/**
 * Format a list of model IDs into a compact comma-separated string, capping the output
 * at `maxModels` entries and appending "… +N more" when the list is longer.
 * @param {string[]|null|undefined} models
 * @param {number} maxModels
 * @returns {string}
 */
function formatModelList(models, maxModels) {
  if (!Array.isArray(models) || models.length === 0) {
    return "—";
  }
  if (models.length <= maxModels) {
    return models.join(", ");
  }
  const shown = models.slice(0, maxModels);
  const remaining = models.length - maxModels;
  return `${shown.join(", ")} … +${remaining} more`;
}

/**
 * Normalize runtime model entries to a common table-friendly shape.
 *
 * Supported payload shapes:
 *   - { endpoints: [...] }
 *   - { providers: { [providerName]: { ... } } }
 *   - { provider: "x", models: [...] }
 *
 * @param {any} runtimeModelsData
 * @returns {Array<{provider: string, endpoint: string, models: string[]}>}
 */
function normalizeRuntimeModelRows(runtimeModelsData) {
  if (!runtimeModelsData || typeof runtimeModelsData !== "object") {
    return [];
  }

  /** @type {Array<{provider: string, endpoint: string, models: string[]}>} */
  const rows = [];

  /**
   * @param {string} provider
   * @param {any} entry
   */
  function pushRow(provider, entry) {
    const modelIds = extractRuntimeModelIds(entry?.models || entry?.available_models || entry?.detected_models || entry?.model_ids || entry?.availableModels);
    rows.push({
      provider: String(provider || entry?.provider || entry?.name || "unknown"),
      endpoint: String(entry?.endpoint || entry?.base_url || entry?.baseUrl || entry?.url || entry?.models_url || entry?.modelsUrl || "—"),
      models: modelIds,
    });
  }

  if (Array.isArray(runtimeModelsData.endpoints)) {
    for (const entry of runtimeModelsData.endpoints) {
      pushRow(entry?.provider, entry);
    }
  }

  if (runtimeModelsData.providers && typeof runtimeModelsData.providers === "object") {
    for (const [provider, entry] of Object.entries(runtimeModelsData.providers)) {
      pushRow(provider, entry);
    }
  }

  if (typeof runtimeModelsData.provider === "string" && Array.isArray(runtimeModelsData.models)) {
    pushRow(runtimeModelsData.provider, runtimeModelsData);
  }

  return rows.sort((a, b) => a.provider.localeCompare(b.provider) || a.endpoint.localeCompare(b.endpoint));
}

/**
 * Extract model IDs from runtime models.json payload entries.
 *
 * @param {any} models
 * @returns {string[]}
 */
function extractRuntimeModelIds(models) {
  if (!Array.isArray(models)) {
    return [];
  }

  return models
    .map(model => {
      if (typeof model === "string") return model;
      if (!model || typeof model !== "object") return null;
      return model.id || model.name || model.model || null;
    })
    .filter(Boolean)
    .sort();
}

/**
 * Normalize model aliases from awf-config.json into a table-friendly shape.
 *
 * @param {any} awfConfigData
 * @returns {Array<{alias: string, label: string, targets: string[]}>}
 */
function normalizeModelAliasRows(awfConfigData) {
  const aliasMap = awfConfigData?.apiProxy?.models;
  if (!aliasMap || typeof aliasMap !== "object" || Array.isArray(aliasMap)) {
    return [];
  }

  return Object.entries(aliasMap)
    .filter(([, targets]) => Array.isArray(targets))
    .map(([alias, targets]) => ({
      alias,
      label: alias === "" ? "(default)" : alias,
      targets: targets.map(target => String(target)),
    }))
    .sort((a, b) => {
      if (a.alias === "") return -1;
      if (b.alias === "") return 1;
      return a.alias.localeCompare(b.alias);
    });
}

/**
 * Build a markdown step summary from AWF /reflect response data.
 *
 * The summary is wrapped in a <details>/<summary> block so it stays collapsed by
 * default and does not dominate the step output. Each row of the table shows:
 *   - Provider name
 *   - Port the endpoint listens on
 *   - Whether a key/token is configured
 *   - Available models (first `maxModels` entries, with overflow indicator)
 *
 * @param {any} reflectData - Parsed /reflect JSON response
 * @param {{ maxModels?: number, runtimeModelsData?: object, awfConfigData?: object }} options
 * @returns {string}
 */
function buildReflectSummary(reflectData, options) {
  const maxModels = options && options.maxModels != null ? options.maxModels : 5;
  const endpoints = Array.isArray(reflectData.endpoints) ? reflectData.endpoints : [];
  const fetchComplete = reflectData.models_fetch_complete === true;
  const runtimeModelRows = normalizeRuntimeModelRows(options && options.runtimeModelsData);
  const modelAliasRows = normalizeModelAliasRows(options && options.awfConfigData);

  const lines = [];
  lines.push("<details>");

  const configuredCount = endpoints.filter(ep => ep.configured).length;
  lines.push(`<summary>AWF API proxy: ${configuredCount} of ${endpoints.length} provider${endpoints.length !== 1 ? "s" : ""} configured</summary>`);
  lines.push("");

  if (endpoints.length === 0) {
    lines.push("No endpoint information available.");
  } else {
    const fetchNote = fetchComplete ? "" : " *(model list may be incomplete — fetch in progress)*";
    lines.push("Configured endpoints");
    lines.push("");
    lines.push(`| Provider | Port | Configured | Available models${fetchNote} |`);
    lines.push("|----------|------|:----------:|-----------------|");

    for (const ep of endpoints) {
      const provider = String(ep.provider || "unknown");
      const port = ep.port != null ? String(ep.port) : "—";
      const configured = ep.configured ? "✅" : "❌";
      const modelStr = formatModelList(ep.models, maxModels);
      lines.push(`| ${provider} | ${port} | ${configured} | ${modelStr} |`);
    }
  }

  if (runtimeModelRows.length > 0) {
    lines.push("");
    lines.push("Runtime models.json");
    lines.push("");
    lines.push("| Provider | Endpoint | Available models |");
    lines.push("|----------|----------|------------------|");
    for (const row of runtimeModelRows) {
      lines.push(`| ${row.provider} | ${row.endpoint} | ${formatModelList(row.models, maxModels)} |`);
    }
  }

  if (modelAliasRows.length > 0) {
    lines.push("");
    lines.push("Model aliases");
    lines.push("");
    lines.push("| Alias | Resolution order |");
    lines.push("|-------|------------------|");
    for (const row of modelAliasRows) {
      lines.push(`| ${row.label} | ${formatModelList(row.targets, maxModels)} |`);
    }
  }

  lines.push("");
  lines.push("</details>");
  lines.push("");

  return lines.join("\n");
}

async function main() {
  const awfConfigData = readAWFConfigData();
  const reflectData = readReflectData();
  const runtimeModelsData = readRuntimeModelsData();

  if (!reflectData) {
    core.info("AWF reflect data not available (AWF not enabled or /reflect not reachable), skipping summary");
    return;
  }

  try {
    fs.mkdirSync(path.dirname(AWF_REFLECT_ARTIFACT_PATH), { recursive: true });
    const source = fs.existsSync(AWF_REFLECT_PATH) ? AWF_REFLECT_PATH : AWF_REFLECT_LEGACY_PATH;
    fs.copyFileSync(source, AWF_REFLECT_ARTIFACT_PATH);
  } catch (err) {
    core.info(`Unable to stage AWF reflect data for artifact upload: ${err instanceof Error ? err.message : String(err)}`);
  }

  const markdown = buildReflectSummary(reflectData, { awfConfigData, runtimeModelsData });
  await core.summary.addRaw(markdown).write();
  core.info(markdown);
  core.info("AWF reflect summary written to step summary");
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    AWF_CONFIG_PATH,
    AWF_MODELS_PATH,
    AWF_REFLECT_ARTIFACT_PATH,
    AWF_REFLECT_PATH,
    buildReflectSummary,
    extractRuntimeModelIds,
    formatModelList,
    main,
    normalizeModelAliasRows,
    readAWFConfigData,
    readReflectData,
    readRuntimeModelsData,
    normalizeRuntimeModelRows,
  };
}
