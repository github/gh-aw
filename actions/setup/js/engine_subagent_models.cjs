// @ts-check
"use strict";

/**
 * Claude and Codex sub-agent model resolution before the engine CLI starts.
 *
 * Every declared sub-agent model goes through the shared "resolve configured model" step
 * (`resolveEngineModel`) in the engine's provisioned provider scope, then a live check
 * against the configured AWF `/reflect` endpoints of that provider. `/reflect` is refreshed
 * at most once for the whole batch. Each sub-agent with a model gets a `model.resolution`
 * record whose role is the agent name; `inherit` (Claude's native inheritance instruction)
 * means the main model and gets no record.
 *
 * Claude: workflow-declared inline agents fail closed and their generated workspace copy
 * gets the concrete id; repository agents are resolved and recorded, but their files are
 * never changed — a warning says when the resolved id is not applied.
 *
 * Codex: workflow-declared inline agents fail closed; each staged Markdown agent becomes a
 * native TOML role file in `$CODEX_HOME/agents` carrying the resolved concrete id, because
 * Codex discovers custom agents only from TOML role files.
 *
 * The sub-agent name `main` is reserved for both engines: it is the role of the main model's
 * `model.resolution` record, which a sub-agent record with the same role would replace. A
 * workflow-declared `main` stops the engine; a repository `main` is skipped with a warning.
 */

const fs = require("fs");
const os = require("os");
const path = require("path");
const { getErrorMessage } = require("./error_helpers.cjs");
const { normalizeReflectProviderName, REFLECT_PROVIDER_ALIASES } = require("./awf_reflect.cjs");
const { isModelAvailableInReflectData } = require("./awf_model_routing.cjs");
const { MODEL_NOT_SUPPORTED_PATTERN } = require("./detect_agent_errors.cjs");
const { inlineArtifactTarget } = require("./inline_artifact_target.cjs");
const { getEngineSubAgentTarget } = require("./extract_inline_sub_agents.cjs");
const { loadModelAliasMap, ModelAliasResolutionError, recordModelResolution, resolveConfiguredModel, resolveEngineModel, splitModelIdentifier } = require("./resolve_model_alias.cjs");

/** The `model.resolution` role of the engine's main model; no sub-agent may use it. */
const RESERVED_SUBAGENT_NAME = "main";

/**
 * Log why workflow-declared sub-agents with the reserved name stop the engine.
 * @param {"claude" | "codex"} engine
 * @param {Array<{ path: string }>} agents
 * @param {(msg: string) => void} logger
 * @returns {boolean} true when at least one agent uses the reserved name
 */
function rejectReservedDeclaredNames(engine, agents, logger) {
  for (const agent of agents) {
    logger(
      `${engine} sub-agent file ${agent.path}: the sub-agent name '${RESERVED_SUBAGENT_NAME}' is reserved for the ${engine === "claude" ? "Claude" : "Codex"} main model's resolution record. Rename the agent in the workflow. Refusing to start ${engine === "claude" ? "Claude" : "Codex"}.`
    );
  }
  return agents.length > 0;
}

/**
 * Determine whether the current run phase is threat detection.
 * @param {string | undefined | null} phase
 * @returns {boolean}
 */
function isDetectionPhase(phase) {
  return (
    String(phase || "")
      .trim()
      .toLowerCase() === "detection"
  );
}

/**
 * The `/reflect` provider names an engine provider covers.
 * @param {string} provider
 * @returns {Set<string>}
 */
function reflectProvidersFor(provider) {
  const normalized = normalizeReflectProviderName(provider);
  if (REFLECT_PROVIDER_ALIASES.github.has(normalized)) return REFLECT_PROVIDER_ALIASES.github;
  return Object.prototype.hasOwnProperty.call(REFLECT_PROVIDER_ALIASES, normalized) ? REFLECT_PROVIDER_ALIASES[normalized] : new Set([normalized]);
}

/**
 * Parse the frontmatter of a Markdown agent file.
 * @param {string} content
 * @returns {Record<string, any>}
 */
function parseAgentFrontmatter(content) {
  const { extractFrontmatterAndBody } = require("./frontmatter_hash_pure.cjs");
  const { parseDocument } = require("./work_queue_yaml.cjs");
  const { frontmatterText } = extractFrontmatterAndBody(content);
  /** @type {any} */
  const document = parseDocument(frontmatterText);
  if (!document || document.errors.length) throw new Error("cannot parse agent frontmatter");
  const frontmatter = document.toJS() || {};
  if (typeof frontmatter !== "object" || Array.isArray(frontmatter)) throw new Error("agent frontmatter must be a mapping");
  return frontmatter;
}

/**
 * Read the Markdown agent files (`*.md`) in one directory.
 * `name` is the trimmed frontmatter `name` string, or null when absent;
 * `model` is the trimmed frontmatter `model` string, when declared; a declared model that is
 * not a string (for example `model: [small]` or `model: false`) is reported as
 * `invalidModel`, its JSON form, so callers can fail instead of inheriting the main model.
 * A directory or file that cannot be read or parsed is skipped with a warning, unless
 * `onInvalid` is given: it then receives the path and error instead, so the caller can
 * fail closed.
 * @param {string} directory
 * @param {(msg: string) => void} logger
 * @param {{ onInvalid?: (filePath: string, error: unknown) => void }} [options]
 * @returns {Array<{ fileName: string, path: string, name: string | null, model?: string, invalidModel?: string }>}
 */
function listMarkdownAgentFiles(directory, logger, options = {}) {
  const invalid = options.onInvalid || ((/** @type {string} */ filePath, /** @type {unknown} */ error) => logger(`warning: cannot read custom agent ${filePath}: ${getErrorMessage(error)}`));
  let entries;
  try {
    entries = fs.readdirSync(directory, { withFileTypes: true });
  } catch (error) {
    if (!(error instanceof Error && "code" in error && error.code === "ENOENT")) invalid(directory, error);
    return [];
  }
  const agents = [];
  for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
    if (!entry.isFile() || !entry.name.endsWith(".md")) continue;
    const agentPath = path.join(directory, entry.name);
    try {
      const frontmatter = parseAgentFrontmatter(fs.readFileSync(agentPath, "utf8"));
      const name = typeof frontmatter.name === "string" && frontmatter.name.trim() ? frontmatter.name.trim() : null;
      const model = frontmatter.model;
      const declared = typeof model === "string" ? { model: model.trim() } : model !== undefined && model !== null ? { invalidModel: JSON.stringify(model) } : {};
      agents.push({ fileName: entry.name, path: agentPath, name, ...declared });
    } catch (error) {
      invalid(agentPath, error);
    }
  }
  return agents;
}

/**
 * Replace the single top-level frontmatter `model:` scalar of an agent file, including any
 * block-scalar continuation lines.
 * @param {string} content
 * @param {string} model
 * @returns {string}
 */
function replaceAgentModel(content, model) {
  const { extractFrontmatterAndBody } = require("./frontmatter_hash_pure.cjs");
  const { parseDocument } = require("./work_queue_yaml.cjs");
  const { frontmatterText } = extractFrontmatterAndBody(content);
  /** @type {any} */
  const document = parseDocument(frontmatterText);
  if (!document || document.errors.length || !document.contents || !Array.isArray(document.contents.items)) {
    throw new Error("cannot parse agent frontmatter");
  }
  const pairs = document.contents.items.filter(pair => pair?.key?.value === "model");
  if (pairs.length !== 1 || typeof pairs[0].value?.value !== "string" || !Array.isArray(pairs[0].value.range)) {
    throw new Error("expected exactly one top-level string 'model:' scalar in the frontmatter");
  }
  const frontmatterStart = content.indexOf("\n") + 1;
  const start = frontmatterStart + pairs[0].value.range[0];
  const end = frontmatterStart + pairs[0].value.range[1];
  const lineEnding = content.slice(start, end).match(/(?:\r?\n)+$/)?.[0] || "";
  return content.slice(0, start) + JSON.stringify(model) + lineEnding + content.slice(end);
}

/**
 * Why a model is not listed for the engine's providers: `catalog_unavailable` when
 * discovery is missing or incomplete for those providers, otherwise `model_unavailable`.
 * @param {any} reflectData
 * @param {Set<string>} providers
 * @returns {"catalog_unavailable" | "model_unavailable"}
 */
function classifyUnlistedModel(reflectData, providers) {
  const configured = (Array.isArray(reflectData?.endpoints) ? reflectData.endpoints : []).filter(endpoint => endpoint?.configured === true);
  const relevant = configured.filter(endpoint => providers.has(normalizeReflectProviderName(endpoint.provider)));
  const incomplete = !reflectData || reflectData.models_fetch_complete === false || relevant.length === 0 || relevant.some(endpoint => !Array.isArray(endpoint.models) || endpoint.models.length === 0);
  return incomplete ? "catalog_unavailable" : "model_unavailable";
}

/**
 * Whether the Vertex provider has no model catalogue endpoint in this AWF response.
 * @param {any} reflectData
 * @returns {boolean}
 */
function hasNoVertexModelCatalog(reflectData) {
  if (!reflectData || !Array.isArray(reflectData.endpoints)) return false;
  const endpoints = reflectData.endpoints.filter(endpoint => endpoint?.configured === true && normalizeReflectProviderName(endpoint.provider) === "vertex");
  return endpoints.length === 0 || endpoints.every(endpoint => !Array.isArray(endpoint.models) && endpoint.models_url == null);
}

/**
 * The per-model endpoint metadata (`routing_models`) published by the configured `/reflect`
 * endpoints of the engine's providers, or null when none of them publishes it.
 * @param {any} reflectData
 * @param {Set<string>} providers
 * @returns {any[] | null}
 */
function routingModelsFor(reflectData, providers) {
  const endpoints = (Array.isArray(reflectData?.endpoints) ? reflectData.endpoints : []).filter(
    endpoint => endpoint?.configured === true && providers.has(normalizeReflectProviderName(endpoint.provider)) && Array.isArray(endpoint.routing_models)
  );
  return endpoints.length > 0 ? endpoints.flatMap(endpoint => endpoint.routing_models) : null;
}

/**
 * What the `routing_models` entries of one model say about `wireApi`: "supported" when an
 * entry lists it, "unsupported" when every entry has complete metadata
 * (`candidate_metadata_complete: true`) without it, otherwise "unknown" (no entry, or
 * incomplete metadata, as `copilot_wire_api.cjs` and `awf_model_routing.cjs` treat it).
 * @param {string} model
 * @param {any[]} routingModels
 * @param {string} wireApi
 * @returns {"supported" | "unsupported" | "unknown"}
 */
function endpointSupportIn(model, routingModels, wireApi) {
  const id = String(model || "").toLowerCase();
  const entries = routingModels.filter(candidate => typeof candidate?.model_id === "string" && candidate.model_id.toLowerCase() === id);
  if (entries.some(entry => Array.isArray(entry.supported_endpoints) && entry.supported_endpoints.includes(wireApi))) return "supported";
  const complete =
    entries.length > 0 && entries.every(entry => entry.candidate_metadata_complete === true && Array.isArray(entry.supported_endpoints) && entry.supported_endpoints.every(/** @param {unknown} value */ value => typeof value === "string"));
  return complete ? "unsupported" : "unknown";
}

/**
 * Whether `/reflect` shows that `model` supports `wireApi` on the engine's providers (see
 * `endpointSupportIn`). Null when those providers publish no per-model endpoint metadata.
 * @param {string} model
 * @param {any} reflectData
 * @param {Set<string>} providers
 * @param {string} wireApi
 * @returns {"supported" | "unsupported" | "unknown" | null}
 */
function modelEndpointSupport(model, reflectData, providers, wireApi) {
  const routingModels = routingModelsFor(reflectData, providers);
  return routingModels ? endpointSupportIn(model, routingModels, wireApi) : null;
}

/**
 * A copy of `/reflect` data whose model lists, on the engine's providers that publish
 * per-model endpoint metadata, drop the models confirmed not to support `wireApi`, so
 * aliases resolve among models the engine can call. Models whose endpoints cannot be
 * verified stay candidates.
 * @param {any} reflectData
 * @param {Set<string>} providers
 * @param {string} wireApi
 * @returns {any}
 */
function restrictReflectToEndpoint(reflectData, providers, wireApi) {
  if (!reflectData || !Array.isArray(reflectData.endpoints)) return reflectData;
  return {
    ...reflectData,
    endpoints: reflectData.endpoints.map(endpoint => {
      if (endpoint?.configured !== true || !providers.has(normalizeReflectProviderName(endpoint.provider)) || !Array.isArray(endpoint.routing_models) || !Array.isArray(endpoint.models)) return endpoint;
      return { ...endpoint, models: endpoint.models.filter(/** @param {any} model */ model => endpointSupportIn(String(model), endpoint.routing_models, wireApi) !== "unsupported") };
    }),
  };
}

/**
 * Resolve the models of Claude or Codex sub-agents in one batch. Agents without a model or
 * with `inherit` are skipped without a record. A `<provider>/` prefix must name the engine's
 * provider; another credential route is never opened. The returned `model` is the bare,
 * engine-normalized id; the effective model parameters (the caller's over an alias's, for
 * example `?effort=low`) are returned as `params` when there are any. A declared model that
 * is not a string (`invalidModel`) fails with cause `model_unavailable`.
 *
 * Failures are recorded as `failed` and returned, never thrown. `fatal` agents log the shared
 * ModelAliasResolutionError diagnostic; other agents get a warning without it, so a failure
 * that does not stop the run is not classified as an unsupported-model run failure.
 *
 * With `wireApi`, the engine calls sub-agent models only through that endpoint (Codex:
 * `/responses`). Where the provider's `/reflect` endpoint publishes per-model
 * `supported_endpoints`, a configured value that is a live exact id is checked against the
 * unfiltered catalogue first, so it is never resolved through an alias of the same name;
 * aliases resolve among models not confirmed to lack `wireApi`. A selected model whose
 * complete metadata omits `wireApi` fails with cause `unsupported_endpoint`, also when an
 * alias would resolve only to such models; a selected model whose endpoints cannot be
 * verified (no routing entry or incomplete metadata, also after one refresh) fails with cause
 * `catalog_unavailable`. Providers without per-model endpoint metadata are not filtered.
 *
 * @param {{
 *   engine: "claude" | "codex",
 *   agents: Array<{ name: string, model?: string, invalidModel?: string, path?: string, fatal: boolean, origin: string }>,
 *   provider: string,
 *   normalize: (model: string) => string,
 *   reflectData: any,
 *   refetchReflectData?: () => Promise<any>,
 *   aliasMap?: Record<string, string[]> | null,
 *   wireApi?: string,
 *   appliedParams?: string[],
 *   nonFatalOutcome: string,
 *   env?: NodeJS.ProcessEnv,
 *   logger: (msg: string) => void,
 * }} options `nonFatalOutcome` says what happens to a non-fatal agent whose model fails;
 *   `appliedParams` names the model parameters the engine applies to the agent definition.
 * @returns {Promise<{
 *   resolutions: Array<{ name: string, configuredModel: string, model: string, status: string, params?: Record<string, string> }>,
 *   failures: Array<{ name: string, configuredModel: string, fatal: boolean, failure: InstanceType<typeof ModelAliasResolutionError> }>,
 * }>}
 */
async function resolveEngineSubagentModels(options) {
  const { engine, logger } = options;
  const env = options.env || process.env;
  const providers = reflectProvidersFor(options.provider);
  const withModel = options.agents.filter(agent => agent.invalidModel !== undefined || (typeof agent.model === "string" && agent.model.trim() && agent.model.trim().toLowerCase() !== "inherit"));
  /** @type {Awaited<ReturnType<typeof resolveEngineSubagentModels>>} */
  const outcome = { resolutions: [], failures: [] };
  if (withModel.length === 0) return outcome;
  const aliasMap = options.aliasMap !== undefined ? options.aliasMap : loadModelAliasMap({ reflectData: options.reflectData, env });
  let reflectData = options.reflectData;
  /** @type {Promise<any> | null} */
  let refresh = null;
  const refreshOnce = () => {
    // A failed refresh counts as no refreshed catalogue, so every agent is still resolved,
    // recorded and classified (catalog_unavailable when the catalogue stays incomplete).
    refresh ??= Promise.resolve()
      .then(() => (options.refetchReflectData ? options.refetchReflectData() : null))
      .then(
        refreshed => {
          if (refreshed) reflectData = refreshed;
          return refreshed || null;
        },
        error => {
          logger(`warning: ${engine} sub-agent model resolution: cannot refresh the awf-reflect catalogue: ${getErrorMessage(error)}`);
          return null;
        }
      );
    return refresh;
  };
  const liveCheck = env.AWF_REFLECT_ENABLED === "1" || Boolean(options.reflectData);
  const wireApi = options.wireApi || "";
  const appliedParams = new Set(options.appliedParams || []);
  // The engine id an alias resolves to without the endpoint constraint, or null; never recorded.
  const resolveUnconstrained = (/** @type {string} */ configuredModel, /** @type {any} */ data, /** @type {(resolution: { resolvedModel: string }) => string} */ engineId) => {
    try {
      return engineId(resolveConfiguredModel({ configuredModel, aliasMap, reflectData: data, provider: options.provider }));
    } catch {
      return null;
    }
  };
  const stripProvider = (/** @type {string} */ model) => {
    const { base } = splitModelIdentifier(model);
    const slash = base.indexOf("/");
    return slash > 0 && providers.has(normalizeReflectProviderName(base.slice(0, slash))) ? base.slice(slash + 1) : base;
  };

  for (const agent of withModel) {
    const configuredModel = agent.invalidModel !== undefined ? agent.invalidModel : String(agent.model).trim();
    const agentLogger = agent.fatal ? logger : (/** @type {string} */ message) => (MODEL_NOT_SUPPORTED_PATTERN.test(message) ? undefined : logger(message));
    const fail = (/** @type {InstanceType<typeof ModelAliasResolutionError>} */ failure, /** @type {string} */ detail) => {
      recordModelResolution({ engine, role: agent.name, configured_model: configuredModel, resolved_model: null, provider: failure.provider || null, status: "failed", cause: failure.reason }, env);
      outcome.failures.push({ name: agent.name, configuredModel, fatal: agent.fatal, failure });
      if (agent.fatal) {
        logger(`${engine} sub-agent '${agent.name}': ${failure.message}${detail ? ` ${detail}` : ""} Refusing to start ${engine === "claude" ? "Claude" : "Codex"} with a different sub-agent model.`);
      } else {
        logger(`warning: ${agent.origin} '${agent.name}' (${agent.path || "unknown file"}) declares model '${configuredModel}', which could not be resolved (cause: ${failure.reason}); ${options.nonFatalOutcome}.`);
      }
    };

    if (agent.invalidModel !== undefined) {
      fail(new ModelAliasResolutionError(configuredModel, "model_unavailable", { provider: options.provider }), `Sub-agent model ${configuredModel} is not a string; declare a model id or alias, or omit 'model' to inherit the main model.`);
      continue;
    }

    const { base } = splitModelIdentifier(configuredModel);
    const slash = base.indexOf("/");
    const prefix = slash > 0 ? base.slice(0, slash) : "";
    if (prefix && ![...reflectProvidersFor(prefix)].some(name => providers.has(name))) {
      fail(new ModelAliasResolutionError(configuredModel, "model_unavailable", { provider: prefix }), `Sub-agent model '${configuredModel}' names provider '${prefix}', but ${engine} is provisioned for '${options.provider}'.`);
      continue;
    }

    const engineId = (/** @type {{ resolvedModel: string }} */ resolution) => stripProvider(options.normalize(resolution.resolvedModel));
    // A live exact id (checked against the unfiltered catalogue) is never filtered, so it is
    // not resolved through an alias of the same name; only alias lookups are restricted.
    const isLiveExact = (/** @type {any} */ data) => {
      try {
        return resolveConfiguredModel({ configuredModel, aliasMap, reflectData: data, provider: options.provider }).status === "exact";
      } catch {
        return false;
      }
    };
    const constrain = (/** @type {any} */ data) => (wireApi && !isLiveExact(data) ? restrictReflectToEndpoint(data, providers, wireApi) : data);
    const failEndpoint = (/** @type {string} */ model) =>
      fail(
        new ModelAliasResolutionError(configuredModel, "unsupported_endpoint", { provider: options.provider }),
        `Sub-agent model '${configuredModel}'${model !== configuredModel ? ` (resolved to '${model}')` : ""} does not list ${wireApi} in its supported_endpoints on the configured awf-reflect endpoints, and ${engine === "claude" ? "Claude" : "Codex"} sub-agents call models only through ${wireApi}.`
      );
    // The model an alias resolves to only without the endpoint constraint, when that model
    // lacks the endpoint: the alias then fails with unsupported_endpoint, not model_unavailable.
    const endpointOnlyMatch = () => {
      if (!wireApi || resolveUnconstrained(configuredModel, constrain(reflectData), engineId)) return null;
      const unconstrained = resolveUnconstrained(configuredModel, reflectData, engineId);
      return unconstrained && modelEndpointSupport(unconstrained, reflectData, providers, wireApi) === "unsupported" ? unconstrained : null;
    };
    const blocked = endpointOnlyMatch();
    if (blocked) {
      failEndpoint(blocked);
      continue;
    }

    let result;
    try {
      result = await resolveEngineModel({
        engine,
        role: agent.name,
        configuredModel,
        aliasMap,
        reflectData: constrain(reflectData),
        provider: options.provider,
        refetchReflectData: async () => constrain(await refreshOnce()),
        logPrefix: `${engine} sub-agent '${agent.name}' model alias resolution`,
        normalize: engineId,
        env,
        logger: agentLogger,
      });
    } catch (error) {
      if (error instanceof ModelAliasResolutionError) {
        // The refreshed catalogue may show that the alias matches only models without the endpoint.
        const refreshedBlocked = endpointOnlyMatch();
        if (refreshedBlocked) {
          failEndpoint(refreshedBlocked);
          continue;
        }
        fail(error, "");
        continue;
      }
      fail(new ModelAliasResolutionError(configuredModel, "model_unavailable", { provider: options.provider }), getErrorMessage(error));
      continue;
    }
    const model = result.engineModel;
    const resolvedNote = model !== configuredModel ? ` (resolved to '${model}')` : "";
    if (liveCheck && !isModelAvailableInReflectData(model, reflectData, providers)) {
      const initialReason = classifyUnlistedModel(reflectData, providers);
      if (options.provider === "vertex" && result.status === "exact" && initialReason === "catalog_unavailable" && hasNoVertexModelCatalog(reflectData)) {
        logger(`claude sub-agent '${agent.name}': Vertex does not publish a model catalogue through awf-reflect; passing through concrete model id '${model}'`);
      } else {
        const refreshed = await refreshOnce();
        if (!refreshed || !isModelAvailableInReflectData(model, refreshed, providers)) {
          // A complete catalogue without the model already proves model_unavailable; only an
          // incomplete one is reclassified, and only when a refreshed catalogue exists.
          const reason = initialReason === "catalog_unavailable" && refreshed ? classifyUnlistedModel(refreshed, providers) : initialReason;
          fail(new ModelAliasResolutionError(configuredModel, reason, { provider: result.provider || options.provider }), `Sub-agent model '${configuredModel}'${resolvedNote} is not listed on the configured awf-reflect endpoints.`);
          continue;
        }
      }
    }
    if (wireApi) {
      let support = modelEndpointSupport(model, reflectData, providers, wireApi);
      if (support === "unknown" && (await refreshOnce())) support = modelEndpointSupport(model, reflectData, providers, wireApi);
      if (support === "unsupported") {
        failEndpoint(model);
        continue;
      }
      if (support === "unknown") {
        fail(
          new ModelAliasResolutionError(configuredModel, "catalog_unavailable", { provider: result.provider || options.provider }),
          `Sub-agent model '${configuredModel}'${resolvedNote} has no complete endpoint metadata on the configured awf-reflect endpoints, so it cannot be verified that it supports ${wireApi}, the only endpoint ${engine === "claude" ? "Claude" : "Codex"} sub-agents call.`
        );
        continue;
      }
    }
    const params = Object.fromEntries(splitModelIdentifier(result.resolvedModel).params);
    const unapplied = Object.keys(params).filter(key => !appliedParams.has(key));
    if (unapplied.length > 0) logger(`${engine} sub-agent '${agent.name}': model parameters (${unapplied.join(", ")}) in '${result.resolvedModel}' are not applied to the agent definition`);
    logger(`${engine} sub-agent '${agent.name}': model '${configuredModel}' -> '${model}'`);
    outcome.resolutions.push({ name: agent.name, configuredModel, model, status: result.status, ...(Object.keys(params).length > 0 ? { params } : {}) });
  }
  return outcome;
}

/**
 * Claude: resolve the models of the sub-agents Claude Code loads from the project before it
 * starts. Workflow-declared inline agents are the files staged by the activation job in
 * `<agentsBaseDir>/.claude/agents`; their generated workspace copy (restored by
 * `restore_inline_sub_agents.sh`) is rewritten with the resolved concrete id. When one of them
 * cannot be read, parsed, resolved or written, `ok` is false and Claude must not start. Repository agents
 * in `<workingDirectory>/.claude/agents` are resolved and recorded, but their files are left
 * unchanged: Claude Code ranks project agents above user agents, so no file-based override
 * exists without editing tracked content. Agent files without a `name` are skipped because
 * Claude Code does not register them.
 *
 * With `engine.bare: true`, Claude loads the workflow agents through the temporary plugin in
 * `pluginDir`, whose `agents` entry links to the staged directory. That link is replaced by a
 * run-local copy of the staged definitions carrying the resolved ids, so the staged files and
 * repository files stay unchanged and the copy is removed with the plugin.
 *
 * @param {{
 *   reflectData: any,
 *   refetchReflectData?: () => Promise<any>,
 *   provider: string,
 *   pluginDir?: string,
 *   workspaceDir?: string,
 *   workingDirectory?: string,
 *   agentsBaseDir?: string,
 *   aliasMap?: Record<string, string[]> | null,
 *   env?: NodeJS.ProcessEnv,
 *   logger: (msg: string) => void,
 * }} options
 * @returns {Promise<{ ok: boolean, resolutions: Array<{ name: string, configuredModel: string, model: string, status: string }> }>}
 */
async function applyClaudeSubagentModels(options) {
  const { logger } = options;
  const env = options.env || process.env;
  const { normalizeClaudeModel } = require("./model_fallback.cjs");
  const workspaceDir = options.workspaceDir || env.GITHUB_WORKSPACE || process.cwd();
  const workingDirectory = options.workingDirectory || path.resolve(workspaceDir, env.GH_AW_ENGINE_CWD || ".");
  const agentsBaseDir = options.agentsBaseDir || path.join(os.tmpdir(), "gh-aw");
  const target = inlineArtifactTarget("SUB_AGENT", getEngineSubAgentTarget("claude"));
  /** @type {string[]} */
  const invalidDeclared = [];
  const stagedDir = path.resolve(agentsBaseDir, target.dir);
  const declared = listMarkdownAgentFiles(stagedDir, logger, {
    onInvalid: (filePath, error) => {
      invalidDeclared.push(filePath);
      logger(`claude sub-agent file ${filePath}: cannot read or parse the staged workflow sub-agent: ${getErrorMessage(error)}.`);
    },
  }).map(agent => ({
    ...agent,
    name: agent.name || agent.fileName.slice(0, -".md".length),
    workspacePath: path.resolve(workspaceDir, target.dir, agent.fileName),
  }));
  if (invalidDeclared.length > 0) {
    logger(`Refusing to start Claude: ${invalidDeclared.length} workflow-declared sub-agent file(s) cannot be read or parsed, so their models cannot be resolved.`);
    return { ok: false, resolutions: [] };
  }
  if (
    rejectReservedDeclaredNames(
      "claude",
      declared.filter(agent => agent.name === RESERVED_SUBAGENT_NAME),
      logger
    )
  ) {
    return { ok: false, resolutions: [] };
  }
  const declaredPaths = new Set(declared.map(agent => agent.workspacePath));
  const declaredNames = new Set(declared.map(agent => agent.name));
  const repository = [];
  for (const agent of listMarkdownAgentFiles(path.resolve(workingDirectory, ".claude", "agents"), logger)) {
    if (declaredPaths.has(path.resolve(agent.path))) continue;
    if (!agent.name) {
      if (agent.model) logger(`warning: repository agent ${agent.path} has no frontmatter 'name', so Claude Code does not register it; its model '${agent.model}' is not resolved.`);
      continue;
    }
    if (agent.name === RESERVED_SUBAGENT_NAME) {
      logger(`warning: repository agent ${agent.path} uses the reserved name '${RESERVED_SUBAGENT_NAME}' (the role of the Claude main model's resolution record); its model is not resolved or recorded.`);
      continue;
    }
    if (declaredNames.has(agent.name)) continue;
    repository.push({ ...agent, name: agent.name });
  }
  const { resolutions, failures } = await resolveEngineSubagentModels({
    engine: "claude",
    agents: [
      ...declared.map(agent => ({ name: agent.name, model: agent.model, invalidModel: agent.invalidModel, path: agent.path, fatal: true, origin: "workflow sub-agent" })),
      ...repository.map(agent => ({ name: agent.name, model: agent.model, invalidModel: agent.invalidModel, path: agent.path, fatal: false, origin: "repository agent" })),
    ],
    provider: options.provider,
    normalize: model => normalizeClaudeModel(model, options.provider, env, { reflectData: options.reflectData, logger }),
    reflectData: options.reflectData,
    refetchReflectData: options.refetchReflectData,
    aliasMap: options.aliasMap,
    nonFatalOutcome: "Claude Code uses the agent file unchanged",
    env,
    logger,
  });
  if (failures.some(failure => failure.fatal)) return { ok: false, resolutions };

  /** @type {Map<string, string>} */
  const resolvedDefinitions = new Map();
  for (const resolution of resolutions) {
    const agent = declared.find(candidate => candidate.name === resolution.name);
    if (!agent) {
      const repositoryAgent = repository.find(candidate => candidate.name === resolution.name);
      if (resolution.model !== resolution.configuredModel) {
        logger(
          `warning: repository agent '${resolution.name}' (${repositoryAgent?.path || "unknown file"}) declares model '${resolution.configuredModel}', resolved to '${resolution.model}', but the resolved id is not applied: the agent file is left unchanged and Claude Code receives '${resolution.configuredModel}'.`
        );
      }
      continue;
    }
    try {
      const staged = fs.readFileSync(agent.path, "utf8");
      const content = resolution.model === resolution.configuredModel ? staged : replaceAgentModel(staged, resolution.model);
      resolvedDefinitions.set(agent.fileName, content);
      const existing = fs.existsSync(agent.workspacePath) ? fs.readFileSync(agent.workspacePath, "utf8") : null;
      if (existing !== content) {
        fs.mkdirSync(path.dirname(agent.workspacePath), { recursive: true });
        fs.writeFileSync(agent.workspacePath, content);
        logger(`claude sub-agent '${resolution.name}': wrote model '${resolution.model}' to ${agent.workspacePath}`);
      }
    } catch (error) {
      logger(`claude sub-agent '${resolution.name}': cannot write the generated agent copy ${agent.workspacePath}: ${getErrorMessage(error)}. Refusing to start Claude with an unresolved sub-agent model.`);
      return { ok: false, resolutions };
    }
  }
  if (options.pluginDir) {
    try {
      writePluginAgents(options.pluginDir, stagedDir, resolvedDefinitions, logger);
    } catch (error) {
      logger(`claude sub-agents: cannot write the resolved agent definitions into the bare-mode plugin ${options.pluginDir}: ${getErrorMessage(error)}. Refusing to start Claude with unresolved sub-agent models.`);
      return { ok: false, resolutions };
    }
  }
  return { ok: true, resolutions };
}

/**
 * Replace the bare-mode plugin's `agents` link to the staged directory with a run-local copy
 * of the staged definitions, using the resolved content where a model was resolved. Nothing
 * happens when the plugin has no `agents` entry.
 * @param {string} pluginDir
 * @param {string} stagedDir
 * @param {Map<string, string>} resolvedDefinitions file name -> resolved definition
 * @param {(msg: string) => void} logger
 */
function writePluginAgents(pluginDir, stagedDir, resolvedDefinitions, logger) {
  const pluginAgents = path.join(pluginDir, "agents");
  try {
    fs.lstatSync(pluginAgents);
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return;
    throw error;
  }
  /** @type {string | undefined} */
  let copy;
  try {
    copy = fs.mkdtempSync(path.join(pluginDir, ".agents-"));
    fs.cpSync(stagedDir, copy, { recursive: true, dereference: true });
    for (const [fileName, content] of resolvedDefinitions) fs.writeFileSync(path.join(copy, fileName), content, { mode: 0o600 });
    fs.rmSync(pluginAgents, { recursive: true, force: true });
    fs.renameSync(copy, pluginAgents);
  } catch (error) {
    try {
      if (copy) fs.rmSync(copy, { recursive: true, force: true });
    } catch (cleanupError) {
      logger(`warning: cannot remove the partial bare-mode agents copy ${copy}: ${getErrorMessage(cleanupError)}; the plugin directory is removed with the plugin.`);
    }
    throw error;
  }
  logger(`claude sub-agents: the bare-mode plugin loads a run-local copy of the resolved definitions from ${pluginAgents}`);
}

/**
 * Split a TOML dotted key (bare or quoted parts) into its parts, or null when it is not one.
 * @param {string} key
 * @returns {string[] | null}
 */
function splitTomlDottedKey(key) {
  /** @type {string[]} */
  const parts = [];
  const pattern = /\s*("(?:[^"\\]|\\.)*"|'[^']*'|[A-Za-z0-9_-]+)\s*(\.|$)/y;
  let match;
  while (pattern.lastIndex < key.length && (match = pattern.exec(key))) {
    const part = match[1];
    parts.push(part.startsWith('"') ? JSON.parse(part) : part.startsWith("'") ? part.slice(1, -1) : part);
    if (!match[2]) return pattern.lastIndex === key.length ? parts : null;
  }
  return null;
}

/**
 * The role names of explicit `[agents.<name>]` entries in a Codex `config.toml`. Table
 * headers, dotted keys and inline tables are recognized; multi-line strings are skipped.
 * @param {string} content
 * @returns {string[]}
 */
function tomlAgentRoleNames(content) {
  /** @type {Set<string>} */
  const names = new Set();
  /** @type {string[]} */
  let table = [];
  /** @type {string | null} */
  let multiline = null;
  for (const rawLine of content.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (multiline) {
      if (line.split(multiline).length % 2 === 0) multiline = null;
      continue;
    }
    const header = /^\[\[?([^\]]+)\]\]?\s*(?:#.*)?$/.exec(line);
    if (header) {
      table = splitTomlDottedKey(header[1]) || [];
      if (table[0] === "agents" && table.length >= 2) names.add(table[1]);
      continue;
    }
    const assignment = /^((?:"(?:[^"\\]|\\.)*"|'[^']*'|[A-Za-z0-9_.\s-])+?)\s*=\s*(.*)$/.exec(line);
    if (!assignment) continue;
    const key = [...table, ...(splitTomlDottedKey(assignment[1]) || [])];
    if (key[0] === "agents" && (key.length >= 3 || (key.length === 2 && assignment[2].startsWith("{")))) names.add(key[1]);
    const delimiter = ['"""', "'''"].find(candidate => assignment[2].startsWith(candidate));
    if (delimiter && assignment[2].split(delimiter).length % 2 === 0) multiline = delimiter;
  }
  return [...names];
}

/**
 * The explicit `[agents.<name>]` roles Codex loads from its configuration: the compiled
 * `engine.config` (`GH_AW_CODEX_CONFIG_JSON` or the `GH_AW_CODEX_CONFIG` file) and
 * `$CODEX_HOME/config.toml`, mapped to where each is defined. Scalar `[agents]` settings
 * such as `max_threads` are not roles.
 * @param {NodeJS.ProcessEnv} env
 * @param {string} codexHome
 * @returns {Map<string, string>}
 */
function explicitCodexAgentRoles(env, codexHome) {
  const { isTable } = require("./codex_config.cjs");
  /** @type {Map<string, string>} */
  const roles = new Map();
  const inline = env.GH_AW_CODEX_CONFIG_JSON;
  const compiledPath = env.GH_AW_CODEX_CONFIG;
  if (inline || (compiledPath && fs.existsSync(compiledPath))) {
    const compiled = JSON.parse(inline || fs.readFileSync(compiledPath || "", "utf8"));
    for (const layer of [compiled?.defaults, compiled?.overrides]) {
      const agents = isTable(layer) ? layer.agents : null;
      if (!isTable(agents)) continue;
      for (const [name, value] of Object.entries(agents)) if (isTable(value)) roles.set(name, "engine.config");
    }
  }
  const configPath = path.join(codexHome, "config.toml");
  if (fs.existsSync(configPath)) {
    for (const name of tomlAgentRoleNames(fs.readFileSync(configPath, "utf8"))) if (!roles.has(name)) roles.set(name, configPath);
  }
  return roles;
}

const CODEX_MANAGED_ROLE_MANIFEST = ".gh-aw-managed-roles.json";

function readManagedCodexRoleNames(rolesDir) {
  const manifestPath = path.join(rolesDir, CODEX_MANAGED_ROLE_MANIFEST);
  let manifest;
  try {
    manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  } catch (error) {
    if (error instanceof Error && "code" in error && error.code === "ENOENT") return [];
    throw new Error(`cannot read the generated-role manifest ${manifestPath}: ${getErrorMessage(error)}`, { cause: error });
  }
  if (!manifest || manifest.version !== 1 || !Array.isArray(manifest.roles) || manifest.roles.some(name => typeof name !== "string" || !/^[a-z][a-z0-9_-]*$/.test(name)) || new Set(manifest.roles).size !== manifest.roles.length) {
    throw new Error(`the generated-role manifest ${manifestPath} is invalid`);
  }
  return manifest.roles;
}

function writeManagedCodexRoleNames(rolesDir, names) {
  const manifestPath = path.join(rolesDir, CODEX_MANAGED_ROLE_MANIFEST);
  const temporaryPath = `${manifestPath}.tmp`;
  try {
    fs.writeFileSync(temporaryPath, `${JSON.stringify({ version: 1, roles: names }, null, 2)}\n`, { mode: 0o600 });
    fs.renameSync(temporaryPath, manifestPath);
  } catch (error) {
    throw new Error(`cannot update the generated-role manifest ${manifestPath}: ${getErrorMessage(error)}`, { cause: error });
  }
}

function prepareCodexRoleDirectory(codexHome, names) {
  const rolesDir = path.join(codexHome, "agents");
  const manifestPath = path.join(rolesDir, CODEX_MANAGED_ROLE_MANIFEST);
  if (names.length === 0 && !fs.existsSync(manifestPath)) return { rolesDir, managedNames: [] };

  try {
    fs.mkdirSync(rolesDir, { recursive: true });
  } catch (error) {
    throw new Error(`cannot create the native role directory ${rolesDir}: ${getErrorMessage(error)}`, { cause: error });
  }
  const previousNames = readManagedCodexRoleNames(rolesDir);
  const previouslyManaged = new Set(previousNames);
  for (const name of names) {
    const rolePath = path.join(rolesDir, `${name}.toml`);
    if (!previouslyManaged.has(name) && fs.existsSync(rolePath)) {
      throw new Error(`the role file ${rolePath} already exists and is not tracked as generated by gh-aw`);
    }
  }
  const managedNames = [...new Set([...previousNames, ...names])].sort();
  if (managedNames.length > 0) {
    writeManagedCodexRoleNames(rolesDir, managedNames);
    for (const name of managedNames) {
      const rolePath = path.join(rolesDir, `${name}.toml`);
      try {
        fs.rmSync(rolePath, { force: true });
      } catch (error) {
        throw new Error(`cannot remove the previously generated role file ${rolePath}: ${getErrorMessage(error)}`, { cause: error });
      }
    }
  }
  return { rolesDir, managedNames };
}

/**
 * Codex: generate one native TOML role file per workflow-declared sub-agent before Codex
 * starts. Codex discovers custom agents only from `.toml` role files (verified for Codex
 * 0.163.0), so the Markdown agents staged by the activation job in
 * `<agentsBaseDir>/.codex/agents` are converted into `<codexHome>/agents/<name>.toml` with
 * the declared block name, the frontmatter `description`, the Markdown body as
 * `developer_instructions` and the resolved concrete model. The effective `?effort=` model
 * parameter (the agent's own, else the one its alias carries) becomes
 * `model_reasoning_effort`; an agent without a model (or with `inherit`) inherits the main
 * model. Roles inherit the main agent's provider, tools and MCP servers, so no tool list is
 * written.
 *
 * Workflow-declared agents fail closed: when one cannot be read, parsed, resolved or written,
 * declares a non-string model or an effort Codex does not accept, uses the reserved name
 * `main`, or has the name of an explicit `[agents.<name>]` role in the Codex configuration
 * (`engine.config` or `$CODEX_HOME/config.toml`), `ok` is false and Codex must not start.
 * Codex 0.163.0 loads explicit `[agents.<name>]` entries before discovered role files and
 * ignores a discovered duplicate, so the generated role would not be used.
 *
 * @param {{
 *   reflectData: any,
 *   refetchReflectData?: () => Promise<any>,
 *   provider: string,
 *   codexHome?: string,
 *   agentsBaseDir?: string,
 *   aliasMap?: Record<string, string[]> | null,
 *   env?: NodeJS.ProcessEnv,
 *   logger: (msg: string) => void,
 * }} options
 * @returns {Promise<{ ok: boolean, agents: string[], resolutions: Array<{ name: string, configuredModel: string, model: string, status: string }>, failures: Array<{ name: string }> }>}
 */
async function applyCodexSubagentModels(options) {
  const { logger } = options;
  const env = options.env || process.env;
  const { normalizeCodexModel } = require("./model_fallback.cjs");
  const { extractFrontmatterAndBody } = require("./frontmatter_hash_pure.cjs");
  const { serializeConfig } = require("./codex_config.cjs");
  const { getAWFModelRoutingPolicy, mapAWFRoutingEffort } = require("./awf_model_routing.cjs");
  const agentsBaseDir = options.agentsBaseDir || path.join(os.tmpdir(), "gh-aw");
  const directory = path.resolve(agentsBaseDir, inlineArtifactTarget("SUB_AGENT", getEngineSubAgentTarget("codex")).dir);
  /** @type {string[]} */
  const invalid = [];
  const agents = listMarkdownAgentFiles(directory, logger, {
    onInvalid: (filePath, error) => {
      invalid.push(filePath);
      logger(`codex sub-agent file ${filePath}: cannot read or parse the staged workflow sub-agent: ${getErrorMessage(error)}.`);
    },
  }).map(agent => ({ ...agent, name: agent.fileName.slice(0, -".md".length) }));
  const names = agents.map(agent => agent.name);
  if (invalid.length > 0) {
    logger(`Refusing to start Codex: ${invalid.length} workflow-declared sub-agent file(s) cannot be read or parsed, so their native role files cannot be generated.`);
    return { ok: false, agents: names, resolutions: [], failures: [] };
  }
  const codexHome = String(options.codexHome || "").trim();
  if (agents.length === 0) {
    if (!codexHome) return { ok: true, agents: [], resolutions: [], failures: [] };
    try {
      const { rolesDir, managedNames } = prepareCodexRoleDirectory(codexHome, []);
      if (managedNames.length > 0) writeManagedCodexRoleNames(rolesDir, []);
    } catch (error) {
      logger(`Refusing to start Codex: cannot remove previously generated sub-agent roles: ${getErrorMessage(error)}.`);
      return { ok: false, agents: [], resolutions: [], failures: [] };
    }
    return { ok: true, agents: [], resolutions: [], failures: [] };
  }
  if (
    rejectReservedDeclaredNames(
      "codex",
      agents.filter(agent => agent.name === RESERVED_SUBAGENT_NAME),
      logger
    )
  ) {
    return { ok: false, agents: names, resolutions: [], failures: [] };
  }
  if (!codexHome) {
    logger(`Refusing to start Codex: CODEX_HOME is not set, so the ${agents.length} workflow-declared sub-agent(s) (${names.join(", ")}) cannot be registered as native Codex roles.`);
    return { ok: false, agents: names, resolutions: [], failures: [] };
  }

  let explicitRoles;
  try {
    explicitRoles = explicitCodexAgentRoles(env, codexHome);
  } catch (error) {
    logger(`Refusing to start Codex: cannot read the Codex configuration to check the sub-agent role names: ${getErrorMessage(error)}.`);
    return { ok: false, agents: names, resolutions: [], failures: [] };
  }
  const collisions = agents.filter(agent => explicitRoles.has(agent.name));
  if (collisions.length > 0) {
    for (const agent of collisions) {
      logger(
        `codex sub-agent file ${agent.path}: the Codex configuration (${explicitRoles.get(agent.name)}) already defines the role [agents.${agent.name}], which Codex loads instead of the generated role file. Rename the workflow sub-agent or remove the explicit role. Refusing to start Codex.`
      );
    }
    return { ok: false, agents: names, resolutions: [], failures: [] };
  }

  /** @type {Array<{ name: string, path: string, description: string, instructions: string, configuredModel: string, effort: string | null }>} */
  const definitions = [];
  for (const agent of agents) {
    try {
      const content = fs.readFileSync(agent.path, "utf8");
      const frontmatter = parseAgentFrontmatter(content);
      const description = typeof frontmatter.description === "string" ? frontmatter.description.trim() : "";
      if (!description) throw new Error("the frontmatter has no nonblank 'description', which Codex requires for a custom role");
      const instructions = extractFrontmatterAndBody(content).markdown.trim();
      if (!instructions) throw new Error("the agent has no instructions after its frontmatter, which Codex requires for a custom role");
      if (!/^[a-z][a-z0-9_-]*$/.test(agent.name)) throw new Error(`'${agent.name}' is not a valid sub-agent name`);
      const configuredModel = typeof agent.model === "string" ? agent.model : "";
      const declaredEffort = configuredModel ? splitModelIdentifier(configuredModel).params.get("effort") : null;
      const mapped = mapAWFRoutingEffort("codex", declaredEffort ? declaredEffort.trim().toLowerCase() : null);
      if (mapped.error) throw new Error(`model '${configuredModel}': ${mapped.error}`);
      definitions.push({ name: agent.name, path: agent.path, description, instructions, configuredModel, effort: mapped.effort });
    } catch (error) {
      logger(`codex sub-agent file ${agent.path}: ${getErrorMessage(error)}. Refusing to start Codex without the declared sub-agent.`);
      return { ok: false, agents: names, resolutions: [], failures: [] };
    }
  }

  const { resolutions, failures } = await resolveEngineSubagentModels({
    engine: "codex",
    agents: agents.map(agent => ({ name: agent.name, model: agent.model, invalidModel: agent.invalidModel, path: agent.path, fatal: true, origin: "workflow sub-agent" })),
    provider: options.provider,
    normalize: model => normalizeCodexModel(model, options.provider, { env, logger }),
    reflectData: options.reflectData,
    refetchReflectData: options.refetchReflectData,
    aliasMap: options.aliasMap,
    wireApi: getAWFModelRoutingPolicy("codex").endpoints[0],
    appliedParams: ["effort"],
    nonFatalOutcome: "Codex does not start",
    env,
    logger,
  });
  if (failures.length > 0) return { ok: false, agents: names, resolutions, failures };

  // The effective effort is the resolved one: the agent's own parameter wins over its alias's.
  for (const definition of definitions) {
    const resolvedEffort = resolutions.find(candidate => candidate.name === definition.name)?.params?.effort;
    if (resolvedEffort === undefined) continue;
    const mapped = mapAWFRoutingEffort("codex", resolvedEffort.trim().toLowerCase());
    if (mapped.error) {
      logger(`codex sub-agent '${definition.name}': model '${definition.configuredModel}' resolves with effort '${resolvedEffort}': ${mapped.error}. Refusing to start Codex without the declared sub-agent.`);
      return { ok: false, agents: names, resolutions, failures };
    }
    definition.effort = mapped.effort;
  }

  let rolesDir;
  let managedNames;
  try {
    ({ rolesDir, managedNames } = prepareCodexRoleDirectory(
      codexHome,
      definitions.map(definition => definition.name)
    ));
  } catch (error) {
    logger(`Refusing to start Codex: cannot reconcile previously generated sub-agent roles: ${getErrorMessage(error)}.`);
    return { ok: false, agents: names, resolutions, failures };
  }

  for (const definition of definitions) {
    const resolution = resolutions.find(candidate => candidate.name === definition.name);
    /** @type {Record<string, string>} */
    const role = { name: definition.name, description: definition.description, developer_instructions: definition.instructions };
    if (resolution) role.model = resolution.model;
    if (definition.effort) role.model_reasoning_effort = definition.effort;
    const rolePath = path.join(rolesDir, `${definition.name}.toml`);
    try {
      fs.writeFileSync(rolePath, serializeConfig(role), { mode: 0o600 });
    } catch (error) {
      logger(`codex sub-agent '${definition.name}': cannot write the native role file ${rolePath}: ${getErrorMessage(error)}. Refusing to start Codex without the declared sub-agent.`);
      for (const name of managedNames) {
        try {
          fs.rmSync(path.join(rolesDir, `${name}.toml`), { force: true });
        } catch (cleanupError) {
          logger(`codex sub-agent '${name}': cannot remove a partial native role file: ${getErrorMessage(cleanupError)}.`);
        }
      }
      return { ok: false, agents: names, resolutions, failures };
    }
    logger(`codex sub-agent '${definition.name}': wrote native role ${rolePath} (model: ${resolution ? resolution.model : "inherits the main model"}${definition.effort ? `, effort: ${definition.effort}` : ""})`);
  }
  try {
    writeManagedCodexRoleNames(rolesDir, definitions.map(definition => definition.name).sort());
  } catch (error) {
    logger(`Refusing to start Codex: cannot update the generated-role manifest: ${getErrorMessage(error)}.`);
    for (const name of managedNames) {
      try {
        fs.rmSync(path.join(rolesDir, `${name}.toml`), { force: true });
      } catch (cleanupError) {
        logger(`codex sub-agent '${name}': cannot remove a partial native role file: ${getErrorMessage(cleanupError)}.`);
      }
    }
    return { ok: false, agents: names, resolutions, failures };
  }
  return { ok: true, agents: names, resolutions, failures };
}

module.exports = {
  applyClaudeSubagentModels,
  applyCodexSubagentModels,
  isDetectionPhase,
  listMarkdownAgentFiles,
  replaceAgentModel,
  resolveEngineSubagentModels,
};
