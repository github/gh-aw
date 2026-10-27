// @ts-check
"use strict";

/**
 * Deterministic workflow model pin check for the Daily Model Inventory Checker.
 *
 * Collects every model name configured by `.github/workflows/*.md` (and their local
 * imports, imported agent files and inline `## agent:` blocks), then classifies each
 * pin against the live Copilot catalogue from AWF `/reflect` with the same resolver the
 * engine harnesses use (`resolveConfiguredModel`). The resolver returns `exact` for a
 * non-alias name without checking liveness, so an exact name is also tested with
 * `isLiveModelId` and a non-live exact name is reported as `model_unavailable`.
 *
 * Classification:
 * - exact: a live Copilot model id.
 * - alias: an alias that resolves; the resolved model is reported.
 * - unresolvable: the resolver's cause (`model_unavailable` or `catalog_unavailable`).
 * - not_checked: with a reason:
 *   - `expression`: the value contains a GitHub Actions expression (`${{ … }}`).
 *   - `provider not in the Copilot catalogue`: the pin targets another provider (an
 *     explicit non-Copilot prefix, the engine's own provider such as Anthropic or
 *     OpenAI, or Copilot BYOK via `engine.env.COPILOT_PROVIDER_BASE_URL`).
 *   - `sentinel`: `inherited` (a sub-agent that uses its parent's model; the parent's
 *     pin is checked in its own row) or `auto` on an engine or field where `auto` is
 *     the gateway's own model selection rather than a gh-aw alias (every engine except
 *     Copilot, and `safe-outputs.assign-to-agent.model`).
 *
 * Usage (from the repository root):
 *   node actions/setup/js/model_pin_check.cjs --reflect <reflect.json> --out-dir <dir> [--repo <root>]
 * Writes <dir>/model-pins.json, <dir>/model-pins.md (every pin) and <dir>/model-pins-summary.md
 * (the issue section) and prints the summary.
 */

const fs = require("fs");
const path = require("path");
const { ERR_CONFIG, ERR_MODEL_PIN_REFLECT_REQUIRED } = require("./error_codes.cjs");
const { parseDocument } = require("./work_queue_yaml.cjs");
const { extractInlineSubAgents } = require("./extract_inline_sub_agents.cjs");
const { buildProviderCatalog, canonicalModelProvider, isLiveModelId, loadBuiltinModelAliases, ModelAliasResolutionError, resolveConfiguredModel, resolveModelAlias, splitModelIdentifier } = require("./resolve_model_alias.cjs");

/** Keys under `models:` that are not alias names (see reservedModelsKeys in pkg/workflow/model_aliases.go). */
const RESERVED_MODELS_KEYS = new Set(["providers", "default-ai-credits-pricing", "allowed", "blocked"]);

/** Native provider of each built-in engine when neither the model nor the engine names one. */
const ENGINE_DEFAULT_PROVIDERS = Object.freeze({ copilot: "copilot", claude: "anthropic", codex: "openai", pi: "copilot", gemini: "gemini", agy: "gemini" });

/** Engines whose provider follows a `copilot/` main model when no provider is configured (resolveEngineLLMProviderFromModel). */
const MODEL_PREFIX_PROVIDER_ENGINES = new Set(["claude", "codex", "pi"]);

const REASON_EXPRESSION = "expression";
const REASON_PROVIDER = "provider not in the Copilot catalogue";
const REASON_BYOK = "provider not in the Copilot catalogue (BYOK via COPILOT_PROVIDER_BASE_URL)";
const REASON_DETECTION_DISABLED = "threat detection disabled by the main workflow";
const REASON_INHERITED = "sentinel `inherited`: the sub-agent uses its parent's model, which is checked in its own row";
const REASON_AUTO = "sentinel `auto`: the gateway selects the model";

const STATUS_LABELS = Object.freeze({ unresolvable: "unresolvable", alias: "alias resolves", exact: "exact", not_checked: "not checked" });
const STATUS_ORDER = ["unresolvable", "alias", "exact", "not_checked"];

/**
 * @typedef {{
 *   workflow: string,
 *   source: string,
 *   field: string,
 *   model: string,
 *   kind: "model" | "alias-definition",
 *   provider: string,
 *   engine: string,
 *   autoSentinel: boolean,
 *   inactiveReason?: string,
 *   aliasEntries?: string[],
 *   aliasMap: Record<string, string[]>,
 * }} ModelPin
 */

/**
 * @typedef {{
 *   workflow: string,
 *   source: string,
 *   field: string,
 *   configured: string,
 *   engine: string,
 *   provider: string,
 *   status: "exact" | "alias" | "unresolvable" | "not_checked",
 *   resolved: string | null,
 *   cause: string | null,
 *   reason: string | null,
 * }} PinResult
 */

/**
 * Split a markdown file into its YAML frontmatter object and body.
 * @param {string} content
 * @returns {{ frontmatter: Record<string, any> | null, body: string, error: string | null }}
 */
function parseMarkdownFrontmatter(content) {
  const text = String(content || "").replace(/^\uFEFF/, "");
  const lines = text.split(/\r?\n/);
  if (lines.length === 0 || lines[0].trim() !== "---") return { frontmatter: null, body: text, error: null };
  const end = lines.findIndex((line, index) => index > 0 && line.trim() === "---");
  if (end < 0) return { frontmatter: null, body: text, error: "unterminated frontmatter" };
  const yaml = lines.slice(1, end).join("\n");
  const body = lines.slice(end + 1).join("\n");
  try {
    /** @type {any} */
    const document = parseDocument(yaml, { version: "1.2", uniqueKeys: false });
    if (!document) return { frontmatter: {}, body, error: null };
    if (document.errors.length) return { frontmatter: null, body, error: String(document.errors[0]) };
    const value = document.toJS();
    return { frontmatter: value && typeof value === "object" && !Array.isArray(value) ? value : {}, body, error: null };
  } catch (error) {
    return { frontmatter: null, body, error: error instanceof Error ? error.message : String(error) };
  }
}

/**
 * @param {unknown} value
 * @returns {value is Record<string, any>}
 */
function isObject(value) {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

/**
 * @param {unknown} value
 * @returns {string}
 */
function stringValue(value) {
  return typeof value === "string" ? value.trim() : "";
}

/**
 * Canonical provider named by a model's `provider/` prefix, or "" when unprefixed.
 * @param {string} model
 * @returns {string}
 */
function modelPrefixProvider(model) {
  const { base } = splitModelIdentifier(model);
  const slash = base.indexOf("/");
  return slash > 0 ? canonicalModelProvider(base.slice(0, slash)) : "";
}

/**
 * The model id without provider prefix and parameters, lower-cased.
 * @param {string} model
 * @returns {string}
 */
function bareModelName(model) {
  const { base } = splitModelIdentifier(model);
  const slash = base.indexOf("/");
  return (slash >= 0 ? base.slice(slash + 1) : base).toLowerCase();
}

/**
 * Import specs from an `imports:` value: strings, `{path|uses, inputs|with}` objects,
 * or an object with an `aw:` list (parseImportSpecsFromField in pkg/parser/import_bfs.go).
 * @param {unknown} imports
 * @returns {string[]}
 */
function importPaths(imports) {
  const list = Array.isArray(imports) ? imports : isObject(imports) && Array.isArray(imports.aw) ? imports.aw : [];
  /** @type {string[]} */
  const paths = [];
  for (const item of list) {
    const value = typeof item === "string" ? item : isObject(item) ? stringValue(item.path) || stringValue(item.uses) : "";
    if (value) paths.push(value.trim());
  }
  return paths;
}

/**
 * Whether an import path is a remote workflowspec (owner/repo/path[@ref]); mirrors
 * IsWorkflowSpec in pkg/parser/remote_path_predicates.go.
 * @param {string} importPath
 * @returns {boolean}
 */
function isRemoteImport(importPath) {
  const clean = importPath.split("#")[0].split("@")[0];
  const parts = clean.split("/");
  if (parts.length < 3) return false;
  if (clean.includes("://")) return true;
  if (clean.startsWith(".") || clean.startsWith("shared/") || clean.startsWith("/")) return false;
  return !!parts[0] && !!parts[1];
}

/**
 * Resolve a local import path the way the compiler does (determineNestedBaseDir and
 * computeIncludeResolveAndSecurityBases in pkg/parser): `.github/…` and `/…` paths are
 * repository-relative; a nested import without a directory (or starting with `./`) is
 * relative to the importing file; anything else is relative to the workflow directory.
 * @param {string} importPath
 * @param {{ importerDir: string, workflowDir: string, repoRoot: string, nested: boolean }} bases absolute directories
 * @returns {string}
 */
function resolveLocalImport(importPath, bases) {
  const clean = importPath.split("#")[0];
  if (clean.startsWith(".github/")) return path.join(bases.repoRoot, clean);
  if (clean.startsWith("/")) return path.join(bases.repoRoot, clean.slice(1));
  const localRelative = !clean.includes("/") || clean.startsWith("./");
  return path.join(bases.nested && localRelative ? bases.importerDir : bases.workflowDir, clean);
}

/**
 * @param {string} file absolute path
 * @param {string} repoRoot
 * @returns {string}
 */
function relativePath(file, repoRoot) {
  return path.relative(repoRoot, file).split(path.sep).join("/");
}

/**
 * @typedef {{ file: string, rel: string, frontmatter: Record<string, any>, body: string, agentFile: boolean }} LoadedFile
 */

/**
 * Load a workflow and its local imports in breadth-first order (first import first),
 * skipping remote and `@builtin:` imports.
 * @param {string} workflowFile absolute path
 * @param {string} repoRoot
 * @returns {{ files: LoadedFile[], skipped: Array<{ workflow: string, import: string, reason: string }>, errors: Array<{ file: string, error: string }> }}
 */
function loadWorkflowFiles(workflowFile, repoRoot) {
  /** @type {LoadedFile[]} */
  const files = [];
  /** @type {Array<{ workflow: string, import: string, reason: string }>} */
  const skipped = [];
  /** @type {Array<{ file: string, error: string }>} */
  const errors = [];
  const workflowRel = relativePath(workflowFile, repoRoot);
  const visited = new Set();
  /** @type {Array<{ file: string, importPath: string | null }>} */
  const queue = [{ file: workflowFile, importPath: null }];
  while (queue.length > 0) {
    const item = queue.shift();
    if (!item) break;
    const file = path.resolve(item.file);
    if (visited.has(file)) continue;
    visited.add(file);
    const rel = relativePath(file, repoRoot);
    let content;
    try {
      content = fs.readFileSync(file, "utf8");
    } catch (error) {
      errors.push({ file: rel, error: `cannot read import${item.importPath ? ` '${item.importPath}'` : ""}: ${error instanceof Error ? error.message : String(error)}` });
      continue;
    }
    const parsed = parseMarkdownFrontmatter(content);
    if (parsed.error) errors.push({ file: rel, error: `frontmatter: ${parsed.error}` });
    const frontmatter = parsed.frontmatter || {};
    const agentFile = item.importPath !== null && /(^|\/)\.github\/agents\/[^/]+\.md$/i.test(rel);
    files.push({ file, rel, frontmatter, body: parsed.body, agentFile });
    // Imported agent files contribute only their model (handleAgentImportItem); they have no nested imports.
    if (agentFile) continue;
    for (const importPath of importPaths(frontmatter.imports)) {
      if (importPath.startsWith("@builtin:")) continue;
      if (isRemoteImport(importPath)) {
        skipped.push({ workflow: workflowRel, import: importPath, reason: "remote import" });
        continue;
      }
      queue.push({ file: resolveLocalImport(importPath, { importerDir: path.dirname(file), workflowDir: path.dirname(workflowFile), repoRoot, nested: item.importPath !== null }), importPath });
    }
  }
  return { files, skipped, errors };
}

/**
 * @typedef {{ id: string, explicitProvider: string, byok: boolean, definitionProvider: string, env?: Record<string, any> }} EngineInfo
 */

/**
 * Describe an `engine:` value (string id, engine object, or inline definition).
 * @param {unknown} engine
 * @returns {EngineInfo | null}
 */
function describeEngine(engine) {
  if (typeof engine === "string" && engine.trim()) return { id: engine.trim().toLowerCase(), explicitProvider: "", byok: false, definitionProvider: "" };
  if (!isObject(engine)) return null;
  const runtime = engine.runtime;
  const id = (stringValue(engine.id) || (typeof runtime === "string" ? runtime.trim() : isObject(runtime) ? stringValue(runtime.id) : "")).toLowerCase();
  const provider = engine.provider;
  const explicitProvider = stringValue(engine["model-provider"]) || (typeof provider === "string" ? provider.trim() : isObject(provider) && !isObject(engine.behaviors) ? stringValue(provider.id) : "");
  const definitionProvider = isObject(provider) ? stringValue(provider.name) || stringValue(provider.id) : "";
  const env = isObject(engine.env) ? engine.env : {};
  const byok = stringValue(env.COPILOT_PROVIDER_BASE_URL) !== "";
  return { id, explicitProvider, byok, definitionProvider, env };
}

/**
 * @param {EngineInfo | null} defaults
 * @param {EngineInfo | null} overrides
 * @returns {EngineInfo | null}
 */
function mergeEngineInfo(defaults, overrides) {
  if (!defaults) return overrides;
  if (!overrides) return defaults;
  const env = { ...(defaults.env || {}), ...(overrides.env || {}) };
  return {
    id: overrides.id || defaults.id,
    explicitProvider: overrides.explicitProvider || defaults.explicitProvider,
    byok: stringValue(env.COPILOT_PROVIDER_BASE_URL) !== "",
    definitionProvider: overrides.definitionProvider || defaults.definitionProvider,
    env,
  };
}

/**
 * @param {unknown} engine
 * @param {unknown} topLevelModel
 * @param {unknown} fallbackModel
 * @returns {string}
 */
function effectiveEngineModel(engine, topLevelModel, fallbackModel) {
  return (isObject(engine) && stringValue(engine.model)) || stringValue(topLevelModel) || stringValue(fallbackModel);
}

/**
 * @param {LoadedFile} file
 * @returns {{ present: boolean, value: any }}
 */
function threatDetectionConfig(file) {
  const safeOutputs = file.frontmatter["safe-outputs"];
  if (!isObject(safeOutputs) || !Object.prototype.hasOwnProperty.call(safeOutputs, "threat-detection")) return { present: false, value: undefined };
  return { present: true, value: safeOutputs["threat-detection"] };
}

/**
 * Imports provide defaults, while any main-workflow threat-detection value replaces or
 * overrides them. In particular, an explicit false in the main workflow disables an
 * imported detector.
 * @param {LoadedFile[]} imports
 * @param {LoadedFile} main
 * @returns {any}
 */
function effectiveThreatDetectionConfig(imports, main) {
  const imported = imports.map(threatDetectionConfig).find(config => config.present);
  const mainConfig = threatDetectionConfig(main);
  if (!mainConfig.present) return imported?.value;
  if (!isObject(mainConfig.value) || !isObject(imported?.value)) return mainConfig.value;
  return { ...imported.value, ...mainConfig.value };
}

/**
 * @param {EngineInfo} engine
 * @param {string} provider
 * @param {Record<string, string>} definitionDetectionEngines
 * @returns {string}
 */
function threatDetectionEngineID(engine, provider, definitionDetectionEngines) {
  const id = engine.id || "copilot";
  if (id === "pi") return "copilot";
  if (Object.prototype.hasOwnProperty.call(ENGINE_DEFAULT_PROVIDERS, id)) {
    if (provider === "copilot") return "copilot";
    if (provider === "anthropic") return "claude";
    if (provider === "openai") return "codex";
    return id === "copilot" || id === "claude" || id === "codex" ? id : "copilot";
  }
  const declared = definitionDetectionEngines[id];
  return declared === "copilot" || declared === "claude" || declared === "codex" ? declared : "copilot";
}

/**
 * The provider a bare (unprefixed) model is sent to for an engine.
 * @param {EngineInfo} engine
 * @param {string} mainModel
 * @param {Record<string, string>} definitionProviders engine id → provider from imported engine definitions
 * @returns {string} canonical provider, or "byok"
 */
function engineProvider(engine, mainModel, definitionProviders) {
  if (engine.byok) return "byok";
  if (engine.explicitProvider) return canonicalModelProvider(engine.explicitProvider);
  const id = engine.id || "copilot";
  if (id === "copilot") return "copilot";
  const mainPrefix = modelPrefixProvider(mainModel);
  if (MODEL_PREFIX_PROVIDER_ENGINES.has(id)) return mainPrefix || ENGINE_DEFAULT_PROVIDERS[/** @type {keyof typeof ENGINE_DEFAULT_PROVIDERS} */ id];
  if (id === "gemini" || id === "agy") return "gemini";
  if (definitionProviders[id]) return canonicalModelProvider(definitionProviders[id]);
  // Universal consumer engines default to the Copilot backend (resolveBackend).
  return mainPrefix || "copilot";
}

/**
 * Whether resolving a model alias could depend on a GitHub Actions expression before
 * reaching a statically resolvable candidate. A live exact model ID always takes
 * precedence over an alias with the same name.
 * @param {string} model
 * @param {Record<string, string[]>} aliasMap
 * @param {string[]} catalog
 * @returns {boolean}
 */
function aliasResolutionUsesExpression(model, aliasMap, catalog) {
  const { base } = splitModelIdentifier(model);
  const slash = base.indexOf("/");
  const aliasKey = slash > 0 ? base.slice(slash + 1) : base;
  if (isLiveModelId(aliasKey, catalog)) return false;

  /**
   * @param {string} key
   * @param {Set<string>} visited
   * @returns {boolean}
   */
  function hasExpressionBeforeResolution(key, visited) {
    if (isLiveModelId(key, catalog) || visited.has(key) || !Array.isArray(aliasMap[key])) return false;
    const nextVisited = new Set(visited);
    nextVisited.add(key);
    for (const entry of aliasMap[key]) {
      if (entry.includes("${{")) return true;
      const { base: entryBase } = splitModelIdentifier(entry);
      const entrySlash = entryBase.indexOf("/");
      const entryProvider = entrySlash > 0 ? canonicalModelProvider(entryBase.slice(0, entrySlash)) : "";
      const entryAlias = entrySlash > 0 ? entryBase.slice(entrySlash + 1) : entryBase;
      if (!entryProvider && Object.prototype.hasOwnProperty.call(aliasMap, entryAlias)) {
        if (hasExpressionBeforeResolution(entryAlias, nextVisited)) return true;
      }
      if ((!entryProvider || entryProvider === "copilot") && isLiveModelId(entryAlias, catalog)) return false;
      if (resolveModelAlias(entry, aliasMap, catalog)) return false;
    }
    return false;
  }

  return hasExpressionBeforeResolution(aliasKey, new Set());
}

/**
 * Collect the model pins of one workflow.
 * @param {string} workflowFile absolute path
 * @param {{ repoRoot: string, builtinAliases: Record<string, string[]> }} options
 * @returns {{ pins: ModelPin[], skipped: Array<{ workflow: string, import: string, reason: string }>, errors: Array<{ file: string, error: string }> }}
 */
function collectWorkflowPins(workflowFile, options) {
  const { repoRoot, builtinAliases } = options;
  const { files, skipped, errors } = loadWorkflowFiles(workflowFile, repoRoot);
  const workflow = relativePath(workflowFile, repoRoot);
  if (files.length === 0) return { pins: [], skipped, errors };
  const main = files[0];
  const imports = files.slice(1).filter(file => !file.agentFile);

  // Merged alias map: built-ins, then imports (first definition wins), then the main workflow.
  /** @type {Record<string, string[]>} */
  const aliasMap = { ...builtinAliases };
  /** @type {Set<string>} */
  const importedAliasKeys = new Set();
  /** @type {Array<{ file: LoadedFile, name: string, entries: string[] }>} */
  const aliasDefinitions = [];
  for (const file of [...imports, main]) {
    const models = file.frontmatter.models;
    if (!isObject(models)) continue;
    for (const [name, value] of Object.entries(models)) {
      if (RESERVED_MODELS_KEYS.has(name) || !Array.isArray(value)) continue;
      const entries = value.filter(entry => typeof entry === "string" && entry.trim()).map(entry => entry.trim());
      if (entries.length === 0) continue;
      if (file === main) {
        aliasMap[name] = entries;
        aliasDefinitions.push({ file, name, entries });
      } else if (!Object.prototype.hasOwnProperty.call(aliasMap, name) && !importedAliasKeys.has(name)) {
        importedAliasKeys.add(name);
        aliasMap[name] = entries;
        aliasDefinitions.push({ file, name, entries });
      }
    }
  }

  /** @type {Record<string, string>} */
  const definitionProviders = {};
  /** @type {Record<string, string>} */
  const definitionDetectionEngines = {};
  /** @type {EngineInfo | null} */
  let importedEngine = null;
  let importedModel = "";
  for (const file of imports) {
    const info = describeEngine(file.frontmatter.engine);
    if (!info) continue;
    if (info.id && info.definitionProvider && !definitionProviders[info.id]) definitionProviders[info.id] = info.definitionProvider;
    const detectionEngine = stringValue(file.frontmatter.engine?.["detection-engine"]);
    if (info.id && detectionEngine && !definitionDetectionEngines[info.id]) definitionDetectionEngines[info.id] = detectionEngine;
    if (!importedEngine && info.id && !isObject(file.frontmatter.engine?.behaviors)) {
      importedEngine = info;
      importedModel = effectiveEngineModel(file.frontmatter.engine, file.frontmatter.model, "");
    }
  }
  const configuredMainEngine = describeEngine(main.frontmatter.engine);
  const engine = configuredMainEngine?.id ? configuredMainEngine : mergeEngineInfo(importedEngine, configuredMainEngine) || { id: "copilot", explicitProvider: "", byok: false, definitionProvider: "" };
  const mainModel = effectiveEngineModel(main.frontmatter.engine, main.frontmatter.model, importedModel);
  const mainProvider = engineProvider(engine, mainModel, definitionProviders);

  const threatDetection = effectiveThreatDetectionConfig(imports, main);
  const detectionDisabled = threatDetection === false || (isObject(threatDetection) && (threatDetection.enabled === false || threatDetection.engine === false));
  const detectionEngineValue = isObject(threatDetection) ? (threatDetection.engine !== undefined && threatDetection.engine !== false ? threatDetection.engine : threatDetection["engine-config"]) : undefined;
  const detectionInfo = describeEngine(detectionEngineValue);
  const detectionModel = (isObject(detectionEngineValue) && stringValue(detectionEngineValue.model)) || (isObject(threatDetection) && stringValue(threatDetection.model)) || mainModel;
  const detectionEngineID = detectionInfo?.id
    ? threatDetectionEngineID(detectionInfo, engineProvider(detectionInfo, detectionModel, definitionProviders), definitionDetectionEngines)
    : threatDetectionEngineID(engine, mainProvider, definitionDetectionEngines);
  const detectionEnv = detectionEngineID === engine.id ? { ...(engine.env || {}), ...(detectionInfo?.env || {}) } : detectionInfo?.env || {};
  const detectionEngine = {
    ...(detectionInfo || { id: detectionEngineID, explicitProvider: "", definitionProvider: "" }),
    id: detectionEngineID,
    env: detectionEnv,
    byok: stringValue(detectionEnv.COPILOT_PROVIDER_BASE_URL) !== "",
  };
  const detectionProvider = engineProvider(detectionEngine, detectionModel, definitionProviders);

  /** @type {ModelPin[]} */
  const pins = [];
  /**
   * @param {LoadedFile} file
   * @param {string} field
   * @param {unknown} value
   * @param {{ engine: EngineInfo, provider: string, autoSentinel?: boolean, inactiveReason?: string }} scope
   */
  const addPin = (file, field, value, scope) => {
    const model = stringValue(value);
    if (!model) return;
    pins.push({
      workflow,
      source: file.rel,
      field,
      model,
      kind: "model",
      provider: scope.provider,
      engine: scope.engine.id || "copilot",
      autoSentinel: scope.autoSentinel ?? scope.engine.id !== "copilot",
      ...(scope.inactiveReason ? { inactiveReason: scope.inactiveReason } : {}),
      aliasMap,
    });
  };
  const mainScope = { engine, provider: mainProvider };
  const detectionScope = { engine: detectionEngine, provider: detectionProvider, ...(detectionDisabled ? { inactiveReason: REASON_DETECTION_DISABLED } : {}) };
  const copilotScope = { engine: { id: "copilot", explicitProvider: "", byok: false, definitionProvider: "" }, provider: "copilot" };

  /**
   * Pins of one engine object (engine:, threat-detection.engine:, threat-detection.engine-config:).
   * @param {LoadedFile} file
   * @param {string} prefix
   * @param {unknown} engineValue
   * @param {{ engine: EngineInfo, provider: string }} scope
   */
  const addEnginePins = (file, prefix, engineValue, scope) => {
    if (!isObject(engineValue)) return;
    addPin(file, `${prefix}.model`, engineValue.model, scope);
    if (Array.isArray(engineValue["fallback-models"])) engineValue["fallback-models"].forEach((model, index) => addPin(file, `${prefix}.fallback-models[${index}]`, model, scope));
    if (isObject(engineValue.provider)) addPin(file, `${prefix}.provider.model`, engineValue.provider.model, scope);
    if (isObject(engineValue.models)) {
      addPin(file, `${prefix}.models.default`, engineValue.models.default, scope);
      if (Array.isArray(engineValue.models.supported)) engineValue.models.supported.forEach((model, index) => addPin(file, `${prefix}.models.supported[${index}]`, model, scope));
    }
    const routing = engineValue["model-routing"];
    if (isObject(routing) && Array.isArray(routing["allowed-models"])) {
      // Model routing always selects among Copilot models.
      routing["allowed-models"].forEach((model, index) => addPin(file, `${prefix}.model-routing.allowed-models[${index}]`, model, { ...scope, provider: scope.provider === "byok" ? "byok" : "copilot" }));
    }
  };

  for (const file of files) {
    const fm = file.frontmatter;
    if (file.agentFile) {
      addPin(file, "model (imported agent file)", fm.model, mainScope);
      continue;
    }
    addPin(file, "model", fm.model, mainScope);
    addEnginePins(file, "engine", fm.engine, mainScope);

    const safeOutputs = isObject(fm["safe-outputs"]) ? fm["safe-outputs"] : {};
    const detection = safeOutputs["threat-detection"];
    if (isObject(detection)) {
      addPin(file, "safe-outputs.threat-detection.model", detection.model, detectionScope);
      addEnginePins(file, "safe-outputs.threat-detection.engine", detection.engine, detectionScope);
      addEnginePins(file, "safe-outputs.threat-detection.engine-config", detection["engine-config"], detectionScope);
    }
    if (isObject(safeOutputs["assign-to-agent"])) addPin(file, "safe-outputs.assign-to-agent.model", safeOutputs["assign-to-agent"].model, { ...copilotScope, autoSentinel: true });

    const evals = fm.evals;
    if (Array.isArray(evals)) {
      evals.forEach((question, index) => isObject(question) && addPin(file, `evals[${index}].model`, question.model, scopeForModel(question.model)));
    } else if (isObject(evals)) {
      addPin(file, "evals.model", evals.model, scopeForModel(evals.model));
      if (Array.isArray(evals.questions)) evals.questions.forEach((question, index) => isObject(question) && addPin(file, `evals.questions[${index}].model`, question.model, scopeForModel(question.model || evals.model)));
    }

    /**
     * @param {unknown} model
     */
    function scopeForModel(model) {
      return { engine, provider: engineProvider(engine, stringValue(model) || mainModel, definitionProviders) };
    }

    if (Array.isArray(fm.enclaves)) {
      fm.enclaves.forEach((enclave, index) => {
        if (!isObject(enclave) || !isObject(enclave.agent)) return;
        const enclaveEngine = describeEngine(stringValue(enclave.agent.engine) || "copilot") || copilotScope.engine;
        addPin(file, `enclaves[${index}].agent.model`, enclave.agent.model, { engine: enclaveEngine, provider: engineProvider(enclaveEngine, "", definitionProviders) });
      });
    }

    let agents = [];
    try {
      agents = extractInlineSubAgents(file.body).agents;
    } catch (error) {
      errors.push({ file: file.rel, error: `inline sub-agents: ${error instanceof Error ? error.message : String(error)}` });
    }
    for (const agent of agents) {
      const parsed = parseMarkdownFrontmatter(agent.content);
      if (parsed.frontmatter) addPin(file, `## agent: '${agent.name}' model`, parsed.frontmatter.model, mainScope);
    }
  }

  for (const definition of aliasDefinitions) {
    // An alias can reach the Copilot catalogue through a copilot/ entry, or through a bare
    // entry when the workflow's engine sends bare models to Copilot.
    const targetsCopilot = definition.entries.some(entry => {
      const prefix = modelPrefixProvider(entry);
      return prefix ? prefix === "copilot" : mainProvider === "copilot";
    });
    pins.push({
      workflow,
      source: definition.file.rel,
      field: `models.${definition.name}`,
      model: definition.name,
      kind: "alias-definition",
      provider: targetsCopilot ? "copilot" : mainProvider === "byok" ? "byok" : modelPrefixProvider(definition.entries[0]) || mainProvider,
      engine: engine.id || "copilot",
      autoSentinel: false,
      aliasEntries: definition.entries,
      aliasMap,
    });
  }
  return { pins, skipped, errors };
}

/**
 * Collect the model pins of every `*.md` workflow in a directory.
 * @param {{ repoRoot: string, workflowsDir?: string, builtinAliases?: Record<string, string[]> | null }} options
 * @returns {{ pins: ModelPin[], workflows: string[], skipped: Array<{ workflow: string, import: string, reason: string }>, errors: Array<{ file: string, error: string }> }}
 */
function collectModelPins(options) {
  const repoRoot = path.resolve(options.repoRoot);
  const workflowsDir = path.resolve(repoRoot, options.workflowsDir || ".github/workflows");
  const builtinAliases = options.builtinAliases || loadBuiltinModelAliases() || {};
  const workflows = fs
    .readdirSync(workflowsDir, { withFileTypes: true })
    .filter(entry => entry.isFile() && entry.name.endsWith(".md"))
    .map(entry => entry.name)
    .sort();
  /** @type {ModelPin[]} */
  const pins = [];
  /** @type {Array<{ workflow: string, import: string, reason: string }>} */
  const skipped = [];
  /** @type {Array<{ file: string, error: string }>} */
  const errors = [];
  for (const name of workflows) {
    const result = collectWorkflowPins(path.join(workflowsDir, name), { repoRoot, builtinAliases });
    pins.push(...result.pins);
    skipped.push(...result.skipped);
    errors.push(...result.errors);
  }
  return { pins, workflows: workflows.map(name => relativePath(path.join(workflowsDir, name), repoRoot)), skipped, errors };
}

/**
 * Classify one pin against the live Copilot catalogue.
 * @param {ModelPin} pin
 * @param {any} reflectData
 * @returns {PinResult}
 */
function classifyPin(pin, reflectData) {
  /** @type {PinResult} */
  const result = {
    workflow: pin.workflow,
    source: pin.source,
    field: pin.field,
    configured: pin.kind === "alias-definition" ? `${pin.model}: [${(pin.aliasEntries || []).join(", ")}]` : pin.model,
    engine: pin.engine,
    provider: pin.provider,
    status: "not_checked",
    resolved: null,
    cause: null,
    reason: null,
  };
  const values = pin.kind === "alias-definition" ? [pin.model, ...(pin.aliasEntries || [])] : [pin.model];
  if (values.some(value => value.includes("${{"))) return { ...result, reason: REASON_EXPRESSION };
  const name = pin.kind === "model" ? bareModelName(pin.model) : "";
  if (name === "inherited") return { ...result, reason: REASON_INHERITED };
  if (pin.inactiveReason) return { ...result, reason: pin.inactiveReason };
  if (pin.provider === "byok") return { ...result, reason: REASON_BYOK };
  const prefix = pin.kind === "model" ? modelPrefixProvider(pin.model) : "";
  if ((prefix || pin.provider) !== "copilot") return { ...result, provider: prefix || pin.provider, reason: REASON_PROVIDER };
  if (name === "auto" && pin.autoSentinel) return { ...result, provider: "copilot", reason: REASON_AUTO };

  const catalog = buildProviderCatalog({ reflectData, provider: "copilot" });
  if (pin.kind === "model" && aliasResolutionUsesExpression(pin.model, pin.aliasMap, catalog)) {
    return { ...result, provider: "copilot", reason: REASON_EXPRESSION };
  }
  try {
    // An alias definition is checked with its own entries, even when another file overrides the name.
    const aliasMap = pin.kind === "alias-definition" ? { ...pin.aliasMap, [pin.model]: pin.aliasEntries || [] } : pin.aliasMap;
    const resolution = resolveConfiguredModel({ configuredModel: pin.model, aliasMap, reflectData, provider: "copilot" });
    if (resolution.status === "resolved") return { ...result, provider: "copilot", status: "alias", resolved: resolution.resolvedModel };
    if (catalog.length === 0) return { ...result, provider: "copilot", status: "unresolvable", cause: "catalog_unavailable" };
    if (isLiveModelId(splitModelIdentifier(resolution.modelId).base, catalog)) return { ...result, provider: "copilot", status: "exact", resolved: resolution.resolvedModel };
    return { ...result, provider: "copilot", status: "unresolvable", cause: "model_unavailable" };
  } catch (error) {
    if (error instanceof ModelAliasResolutionError) return { ...result, provider: "copilot", status: "unresolvable", cause: error.reason };
    throw error;
  }
}

/**
 * Summary of the Copilot endpoint in a /reflect payload.
 * @param {any} reflectData
 * @returns {{ available: boolean, model_count: number, error: string | null }}
 */
function describeCatalogue(reflectData) {
  const catalog = buildProviderCatalog({ reflectData, provider: "copilot" });
  const endpoint = (Array.isArray(reflectData?.endpoints) ? reflectData.endpoints : []).find(entry => entry?.configured === true && canonicalModelProvider(entry.provider) === "copilot");
  const models = Array.isArray(endpoint?.models) ? endpoint.models : [];
  const error = typeof reflectData?.error === "string" ? reflectData.error : !endpoint ? "no configured Copilot endpoint in /reflect" : !Array.isArray(endpoint.models) ? "Copilot endpoint returned no model list" : null;
  return { available: catalog.length > 0, model_count: models.length, error };
}

/**
 * Classify every pin and build the report data.
 * @param {{ pins: ModelPin[], reflectData: any, workflows?: string[], skipped?: Array<{ workflow: string, import: string, reason: string }>, errors?: Array<{ file: string, error: string }> }} options
 */
function buildModelPinReport(options) {
  const results = options.pins.map(pin => classifyPin(pin, options.reflectData));
  /** @type {Record<string, number>} */
  const counts = { total: results.length, exact: 0, alias: 0, unresolvable: 0, not_checked: 0 };
  for (const result of results) counts[result.status] += 1;
  return {
    catalogue: describeCatalogue(options.reflectData),
    counts,
    action_required: results.filter(result => result.status === "unresolvable"),
    pins: results,
    workflows: options.workflows || [],
    skipped_imports: options.skipped || [],
    errors: options.errors || [],
  };
}

/**
 * @param {string | null} value
 * @returns {string}
 */
function cell(value) {
  return String(value ?? "")
    .replace(/\|/g, "\\|")
    .replace(/\r?\n/g, " ");
}

/**
 * @param {string | null} value
 * @returns {string}
 */
function code(value) {
  return `\`${cell(value).replace(/`/g, "'")}\``;
}

/**
 * @param {PinResult} result
 * @returns {string}
 */
function workflowCell(result) {
  return result.source === result.workflow ? code(result.workflow) : `${code(result.workflow)} (via ${code(result.source)})`;
}

/**
 * @param {PinResult} result
 * @returns {string}
 */
function detailCell(result) {
  if (result.status === "alias") return `→ ${code(result.resolved)}`;
  if (result.status === "unresolvable") return code(result.cause);
  if (result.status === "not_checked") return cell(result.reason);
  return "";
}

/**
 * @param {ReturnType<typeof buildModelPinReport>} report
 * @returns {string[]}
 */
function renderActionRequired(report) {
  if (report.action_required.length === 0) return [];
  const lines = ["### Action required", ""];
  // Without a catalogue every Copilot pin is catalog_unavailable; report that once instead of per pin.
  const uncatalogued = report.action_required.filter(result => result.cause === "catalog_unavailable");
  const rows = report.action_required.filter(result => result.cause !== "catalog_unavailable");
  if (uncatalogued.length > 0) lines.push(`${uncatalogued.length} Copilot workflow model pin(s) could not be checked because the live Copilot catalogue is unavailable (\`catalog_unavailable\`); they are listed in \`model-pins.md\`.`, "");
  if (rows.length > 0) {
    lines.push(`${rows.length} workflow model pin(s) do not resolve against the live Copilot catalogue.`, "");
    lines.push("| Workflow | Field | Configured | Cause |", "|---|---|---|---|");
    for (const result of rows) lines.push(`| ${workflowCell(result)} | ${code(result.field)} | ${code(result.configured)} | ${code(result.cause)} |`);
    lines.push("");
  }
  return lines;
}

/**
 * Render the issue section: unresolvable pins first under "Action required" (omitted
 * when there are none), then the catalogue status and counts. Headers are h3 or lower.
 * @param {ReturnType<typeof buildModelPinReport>} report
 * @returns {string}
 */
function renderModelPinSummary(report) {
  const lines = renderActionRequired(report);
  lines.push("### Workflow Model Pins", "");
  const catalogue = report.catalogue;
  lines.push(catalogue.available ? `Live Copilot catalogue: ${catalogue.model_count} model(s).` : `Live Copilot catalogue: **unavailable** (${catalogue.error || "empty model list"}); Copilot pins could not be checked.`, "");
  lines.push("| Result | Pins |", "|---|---:|");
  for (const status of STATUS_ORDER) lines.push(`| ${STATUS_LABELS[status]} | ${report.counts[status]} |`);
  lines.push(`| **total** | **${report.counts.total}** |`, "");
  /** @type {Map<string, number>} */
  const reasons = new Map();
  for (const result of report.pins) if (result.status === "not_checked") reasons.set(result.reason || "", (reasons.get(result.reason || "") || 0) + 1);
  if (reasons.size > 0) {
    lines.push("Not checked:", "");
    for (const [reason, count] of [...reasons].sort((left, right) => right[1] - left[1] || (left[0] < right[0] ? -1 : 1))) lines.push(`- ${count} × ${cell(reason)}`);
    lines.push("");
  }
  if (report.skipped_imports.length > 0 || report.errors.length > 0) lines.push(`Not scanned: ${report.skipped_imports.length} remote import(s), ${report.errors.length} unreadable file(s) (listed in \`model-pins.md\`).`, "");
  lines.push(`Every pin of the ${report.workflows.length} workflow(s) with its classification is in \`model-pins.md\` and \`model-pins.json\` (run artifact \`model-pins\`).`);
  return lines.join("\n") + "\n";
}

/**
 * Render the full report: the summary followed by every pin with its classification.
 * @param {ReturnType<typeof buildModelPinReport>} report
 * @returns {string}
 */
function renderModelPinReport(report) {
  const lines = [renderModelPinSummary(report)];
  lines.push("### All Workflow Model Pins", "");
  lines.push("| Workflow | Field | Configured | Provider | Result | Detail |", "|---|---|---|---|---|---|");
  const ordered = [...report.pins].sort((left, right) => STATUS_ORDER.indexOf(left.status) - STATUS_ORDER.indexOf(right.status) || (left.workflow < right.workflow ? -1 : left.workflow > right.workflow ? 1 : 0));
  for (const result of ordered) {
    lines.push(`| ${workflowCell(result)} | ${code(result.field)} | ${code(result.configured)} | ${cell(result.provider)} | ${STATUS_LABELS[result.status]} | ${detailCell(result)} |`);
  }
  if (report.skipped_imports.length > 0 || report.errors.length > 0) {
    lines.push("", "### Not Scanned", "");
    for (const item of report.skipped_imports) lines.push(`- ${code(item.workflow)}: ${item.reason} ${code(item.import)}`);
    for (const item of report.errors) lines.push(`- ${code(item.file)}: ${cell(item.error)}`);
  }
  return lines.join("\n") + "\n";
}

/**
 * @param {string} file
 * @returns {any}
 */
function readReflect(file) {
  try {
    return JSON.parse(fs.readFileSync(file, "utf8"));
  } catch (error) {
    return { endpoints: [], error: `cannot read ${file}: ${error instanceof Error ? error.message : String(error)}` };
  }
}

/**
 * @param {string[]} argv
 * @returns {Record<string, string>}
 */
function parseArgs(argv) {
  /** @type {Record<string, string>} */
  const args = {};
  for (let index = 0; index < argv.length; index += 1) {
    const match = argv[index].match(/^--([a-z-]+)(?:=(.*))?$/);
    if (!match) throw new Error(`${ERR_CONFIG}: unexpected argument: ${argv[index]}`);
    args[match[1]] = match[2] !== undefined ? match[2] : argv[++index] || "";
  }
  return args;
}

/**
 * CLI entry point.
 * @param {string[]} argv
 * @returns {ReturnType<typeof buildModelPinReport>}
 */
function main(argv = process.argv.slice(2)) {
  const args = parseArgs(argv);
  if (!args.reflect) throw new Error(`${ERR_MODEL_PIN_REFLECT_REQUIRED}: --reflect <path to /reflect JSON> is required`);
  const repoRoot = path.resolve(args.repo || process.cwd());
  const collected = collectModelPins({ repoRoot, workflowsDir: args["workflows-dir"] });
  const report = buildModelPinReport({ ...collected, reflectData: readReflect(args.reflect) });
  const summary = renderModelPinSummary(report);
  if (args["out-dir"]) {
    fs.mkdirSync(args["out-dir"], { recursive: true });
    fs.writeFileSync(path.join(args["out-dir"], "model-pins.json"), JSON.stringify(report, null, 2) + "\n");
    fs.writeFileSync(path.join(args["out-dir"], "model-pins.md"), renderModelPinReport(report));
    fs.writeFileSync(path.join(args["out-dir"], "model-pins-summary.md"), summary);
  }
  process.stdout.write(summary);
  return report;
}

if (require.main === module) {
  main();
}

module.exports = {
  buildModelPinReport,
  classifyPin,
  collectModelPins,
  collectWorkflowPins,
  describeEngine,
  engineProvider,
  isRemoteImport,
  main,
  parseMarkdownFrontmatter,
  renderModelPinReport,
  renderModelPinSummary,
  REASON_AUTO,
  REASON_BYOK,
  REASON_EXPRESSION,
  REASON_INHERITED,
  REASON_PROVIDER,
};
