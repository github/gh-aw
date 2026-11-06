// @ts-check
"use strict";

const { iterateJsonl } = require("./jsonl_helpers.cjs");
const fs = require("fs");
const path = require("path");
const os = require("os");
const { validateModelIdentifier } = require("./model_attribution.cjs");
const { normalizeForCopilotCLI, splitModelIdentifier } = require("./resolve_model_alias.cjs");
const { copilotSessionContext } = require("./copilot_session.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

// Bounds the per-run model history persisted in the selection log (16 KiB) and aw_info.json.
const MAX_OBSERVED_MODELS = 32;

// Exit code the SDK driver uses to report a main-session model verification failure (substitution
// or unverifiable evidence) so the harness stops without retrying, as it does in CLI mode.
const COPILOT_MODEL_VERIFICATION_EXIT_CODE = 79;

function modelID(value) {
  return validateModelIdentifier(normalizeForCopilotCLI(splitModelIdentifier(String(value || "")).base));
}

function modelSubstitutionDiagnostic(expected, model) {
  return `Model '${expected}' is not available as configured: Copilot substituted '${model}'. Refusing silent model substitution. Select a model offered to this workflow's agentic-workflows integration; do not change the integration ID to bypass model access policy.`;
}

function verificationFailureDiagnostic(error) {
  return `Copilot model verification failed: ${getErrorMessage(error)}. Refusing to report an unverified run as successful.`;
}

function copilotSessionStateDirectories(env = process.env) {
  const root = env.GH_AW_TMP_DIR || "/tmp/gh-aw";
  return [path.join(env.COPILOT_HOME || path.join(env.HOME || os.homedir(), ".copilot"), "session-state"), path.join(root, "sandbox/agent/session-state"), path.join(root, "sandbox/agent/logs/copilot-session-state")];
}

function recordCopilotModelSelection(selection, env = process.env) {
  if (env.GH_AW_PHASE && env.GH_AW_PHASE !== "agent") return;
  const file = path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", "sandbox/agent/logs/copilot-model-selection.json");
  try {
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, JSON.stringify(selection));
  } catch (error) {
    throw new Error(`Cannot record Copilot model selection log: ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * Models already observed by an earlier attempt in this agent phase, so a retried or resumed
 * session extends the history instead of overwriting it.
 * @param {NodeJS.ProcessEnv} env
 * @returns {string[]}
 */
function previouslyObservedModels(env) {
  const file = path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", "sandbox/agent/logs/copilot-model-selection.json");
  try {
    if (!fs.existsSync(file)) return [];
    const record = JSON.parse(fs.readFileSync(file, "utf8"));
    if (record?.status !== "observed") return [];
    const models = Array.isArray(record.observed_models) ? record.observed_models : [record.effective_model];
    return [
      ...new Set(
        models
          .filter(model => typeof model === "string")
          .map(modelID)
          .filter(Boolean)
      ),
    ].slice(-MAX_OBSERVED_MODELS);
  } catch {
    return [];
  }
}

// Only the runner post-step updates workflow metadata; the harness writes a log.
function persistCopilotModelSelection(env = process.env) {
  if (env.GH_AW_PHASE && env.GH_AW_PHASE !== "agent") return;
  const log = path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", "sandbox/agent/logs/copilot-model-selection.json");
  if (!fs.existsSync(log)) return;
  const file = path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", "aw_info.json");
  try {
    const info = fs.existsSync(file) ? JSON.parse(fs.readFileSync(file, "utf8")) : {};
    if ((env.GH_AW_ENGINE_ID || info.engine_id) !== "copilot") return;
    const stat = fs.lstatSync(log);
    if (!stat.isFile() || stat.size > 16384) throw new Error("Invalid Copilot model selection log");
    const record = JSON.parse(fs.readFileSync(log, "utf8"));
    const selection = {
      status: record?.status,
      configured_model: typeof record?.configured_model === "string" ? validateModelIdentifier(splitModelIdentifier(record.configured_model).base) : "",
      resolved_model: typeof record?.resolved_model === "string" ? modelID(record.resolved_model) : "",
      effective_model: typeof record?.effective_model === "string" ? modelID(record.effective_model) : "",
    };
    const observedModels = Array.isArray(record?.observed_models)
      ? [
          ...new Set(
            record.observed_models
              .filter(model => typeof model === "string")
              .map(modelID)
              .filter(Boolean)
          ),
        ].slice(0, MAX_OBSERVED_MODELS)
      : [];
    if (observedModels.length > 1) selection.observed_models = observedModels;
    if (!selection.configured_model || !["observed", "substituted", "unavailable"].includes(selection.status)) throw new Error("Invalid Copilot model selection");
    if (selection.status === "unavailable") {
      selection.resolved_model = "";
      selection.effective_model = "";
    } else if (!selection.resolved_model || !selection.effective_model) {
      throw new Error("Invalid Copilot model selection models");
    }
    info.requested_model ||= selection.configured_model;
    info.model = selection.effective_model;
    info.model_selection = selection;
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, JSON.stringify(info, null, 2));
  } catch (error) {
    throw new Error(`Cannot record Copilot model selection: ${getErrorMessage(error)}`, { cause: error });
  }
}

/**
 * Observe only the invocation's exact session and newly appended records, not other sessions or subagents.
 * Native CLI startup fallback is not an authorized AWF fallback.
 * @param {{ configuredModel: string, resolvedModel: string, sessionId?: string, env?: NodeJS.ProcessEnv, sessionDirs?: string[], logger?: (message: string) => void }} options
 */
function createCopilotModelObserver({ configuredModel, resolvedModel, sessionId, env = process.env, sessionDirs, logger = () => {} }) {
  const directories = sessionDirs || copilotSessionStateDirectories(env);
  /** @type {Map<string, number>} */
  const offsets = new Map();
  /** @type {Map<string, string>} */
  const partial = new Map();
  let diagnostic = "";
  let observedModel = "";
  const expected = modelID(resolvedModel);
  if (!expected || (env.GH_AW_PHASE && env.GH_AW_PHASE !== "agent")) return { check: () => "" };
  const observedModels = new Set(previouslyObservedModels(env));
  if (!sessionId || !/^[a-z0-9_-]+$/i.test(sessionId)) throw new Error("Copilot model verification requires an exact invocation session ID");
  const ownedSessionId = sessionId;

  function eventFiles() {
    const files = new Set();
    for (const directory of directories) {
      const file = path.join(directory, ownedSessionId, "events.jsonl");
      try {
        fs.statSync(file);
      } catch (error) {
        if (error !== null && typeof error === "object" && "code" in error && error.code === "ENOENT") continue;
        throw error;
      }
      files.add(file);
    }
    return files;
  }

  try {
    for (const file of eventFiles()) offsets.set(file, fs.statSync(file).size);
  } catch (error) {
    throw new Error(`Cannot initialize Copilot model verification: ${getErrorMessage(error)}`, { cause: error });
  }

  function check(final = false) {
    try {
      if (diagnostic) return diagnostic;
      for (const file of eventFiles()) {
        const size = fs.statSync(file).size;
        let offset = offsets.get(file) || 0;
        if (size < offset) {
          offset = 0;
          partial.delete(file);
        }
        const content = Buffer.alloc(size - offset);
        const fd = fs.openSync(file, "r");
        let bytesRead;
        try {
          bytesRead = fs.readSync(fd, content, 0, content.length, offset);
        } finally {
          fs.closeSync(fd);
        }
        const text = (partial.get(file) || "") + content.subarray(0, bytesRead).toString("utf8");
        offsets.set(file, offset + bytesRead);
        const end = final ? text.length : text.lastIndexOf("\n") + 1;
        partial.set(file, text.slice(end));
        for (const event of iterateJsonl(text.slice(0, end))) {
          const context = copilotSessionContext(event);
          if (context.agentId !== undefined || context.parentToolUseId || (context.sessionId !== undefined && context.sessionId !== ownedSessionId)) continue;
          const model = modelID(event.type === "session.model_change" && event.data?.source === "startup" ? event.data.newModel : event.type === "assistant.message" ? event.data?.model : "");
          if (!model || model === observedModel) continue;
          observedModel = model;
          if (!observedModels.has(model)) {
            const oldest = observedModels.values().next().value;
            if (observedModels.size >= MAX_OBSERVED_MODELS && oldest !== undefined) observedModels.delete(oldest);
            observedModels.add(model);
          }
          const substituted = expected.toLowerCase() !== "auto" && model.toLowerCase() !== expected.toLowerCase();
          recordCopilotModelSelection(
            {
              status: substituted ? "substituted" : "observed",
              configured_model: configuredModel,
              resolved_model: resolvedModel,
              effective_model: model,
              ...(observedModels.size > 1 ? { observed_models: [...observedModels] } : {}),
            },
            env
          );
          if (substituted) {
            diagnostic = modelSubstitutionDiagnostic(expected, model);
            logger(diagnostic);
            return diagnostic;
          }
        }
      }
      return "";
    } catch (error) {
      diagnostic = verificationFailureDiagnostic(error);
      logger(diagnostic);
      return diagnostic;
    }
  }
  return { check };
}

/** @param {any} event */
function sdkEventModel(event) {
  const data = event?.data;
  switch (event?.type) {
    case "session.start":
      return data?.selectedModel;
    case "session.model_change":
      return data?.newModel;
    case "assistant.message":
      return data?.model;
    case "session.shutdown":
      return data?.currentModel;
    default:
      return undefined;
  }
}

/**
 * Observe the Copilot SDK main session's native model-bearing events; sub-agent events are ignored.
 * The expected model is the qualified session model (the router's wire model on routed runs);
 * the configured SDK provider mapping reconciles provider-qualified and bare identifiers.
 * A recorded substitution is final: later matching events cannot clear it.
 * @param {{ configuredModel: string, expectedModel: string, sessionId?: string, providerConfig?: { providers: Array<{ name: string }>, models: Array<{ provider: string, id: string }> } | null, env?: NodeJS.ProcessEnv, sessionDirs?: string[], logger?: (message: string) => void }} options
 */
function createCopilotSDKModelObserver({ configuredModel, expectedModel, sessionId: restoredSessionId, providerConfig = null, env = process.env, sessionDirs, logger = () => {} }) {
  if (!configuredModel || (env.GH_AW_PHASE && env.GH_AW_PHASE !== "agent")) return { observe: () => "", finalize: () => "", setSessionId: () => {} };
  const rows = providerConfig ? providerConfig.models.filter(entry => providerConfig.providers.some(provider => provider.name === entry.provider)) : [];
  /** @param {string} base */
  const providerRow = base => rows.find(entry => `${entry.provider}/${entry.id}` === base);
  const expectedBase = splitModelIdentifier(String(expectedModel || "").trim()).base;
  const expectedRow = providerRow(expectedBase);
  const expected = modelID(expectedRow ? expectedRow.id : expectedBase);
  const unconstrained = !expected || expected.toLowerCase() === "auto";
  const resolvedModel = expected || "auto";
  let sessionId = "";
  let diagnostic = "";
  let recorded = "";
  const observedModels = new Set();
  const initialOffsets = new Map();
  if (restoredSessionId) {
    if (!/^[a-z0-9_-]+$/i.test(restoredSessionId)) throw new Error("Copilot model verification requires an exact restored session ID");
    for (const directory of sessionDirs || copilotSessionStateDirectories(env)) {
      const file = path.join(directory, restoredSessionId, "events.jsonl");
      if (fs.existsSync(file)) initialOffsets.set(file, fs.statSync(file).size);
    }
  }

  /** @param {any} event */
  function observe(event) {
    if (diagnostic) return diagnostic;
    try {
      const context = copilotSessionContext(event);
      if (context.agentId !== undefined || context.parentToolUseId || (sessionId && context.sessionId !== undefined && context.sessionId !== sessionId)) return "";
      const value = sdkEventModel(event);
      if (typeof value !== "string" || !value.trim()) return "";
      const base = splitModelIdentifier(value.trim()).base;
      const row = providerRow(base);
      const model = modelID(row ? row.id : base);
      if (!model) return "";
      // A qualified id outside the configured provider mapping cannot match a provider-qualified expectation.
      const unmapped = !row && base.includes("/");
      const matches = unconstrained || (expectedRow && (row || unmapped) ? row === expectedRow : model.toLowerCase() === expected.toLowerCase());
      const effective = matches ? (unconstrained ? model : expected) : validateModelIdentifier(base) || model;
      if (matches && effective === recorded) return "";
      recorded = effective;
      if (!observedModels.has(effective)) {
        if (observedModels.size >= MAX_OBSERVED_MODELS) observedModels.delete(observedModels.values().next().value);
        observedModels.add(effective);
      }
      recordCopilotModelSelection(
        {
          status: matches ? "observed" : "substituted",
          configured_model: configuredModel,
          resolved_model: resolvedModel,
          effective_model: effective,
          ...(observedModels.size > 1 ? { observed_models: [...observedModels] } : {}),
        },
        env
      );
      if (matches) return "";
      diagnostic = modelSubstitutionDiagnostic(expected, effective);
    } catch (error) {
      diagnostic = verificationFailureDiagnostic(error);
    }
    logger(diagnostic);
    return diagnostic;
  }

  /**
   * Scope observation to the main session and replay its native records, including the
   * shutdown written while disconnecting, before success is reported.
   * @param {string} id
   */
  function finalize(id) {
    if (diagnostic) return diagnostic;
    try {
      if (!id || !/^[a-z0-9_-]+$/i.test(id)) throw new Error("Copilot model verification requires the SDK session ID");
      sessionId = id;
      for (const directory of sessionDirs || copilotSessionStateDirectories(env)) {
        const file = path.join(directory, id, "events.jsonl");
        if (!fs.existsSync(file)) continue;
        const bytes = fs.readFileSync(file);
        const offset = initialOffsets.get(file) || 0;
        if (offset > bytes.length) throw new Error("Restored Copilot transcript was truncated during model verification");
        for (const event of iterateJsonl(bytes.subarray(offset).toString("utf8"))) if (observe(event)) return diagnostic;
      }
    } catch (error) {
      diagnostic = verificationFailureDiagnostic(error);
      logger(diagnostic);
    }
    return diagnostic;
  }

  return {
    observe,
    finalize,
    /** @param {string} id */
    setSessionId: id => {
      sessionId = id;
    },
  };
}

/**
 * The refusal diagnostic for a substitution the SDK driver recorded in this agent phase.
 * @param {NodeJS.ProcessEnv} [env]
 */
function readCopilotModelSubstitution(env = process.env) {
  if (env.GH_AW_PHASE && env.GH_AW_PHASE !== "agent") return "";
  const file = path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", "sandbox/agent/logs/copilot-model-selection.json");
  try {
    if (!fs.existsSync(file)) return "";
    const record = JSON.parse(fs.readFileSync(file, "utf8"));
    if (record?.status !== "substituted") return "";
    return modelSubstitutionDiagnostic(modelID(record.resolved_model) || "(unknown)", modelID(record.effective_model) || "(unknown)");
  } catch (error) {
    return verificationFailureDiagnostic(error);
  }
}

module.exports = { COPILOT_MODEL_VERIFICATION_EXIT_CODE, copilotSessionStateDirectories, createCopilotModelObserver, createCopilotSDKModelObserver, readCopilotModelSubstitution, recordCopilotModelSelection, persistCopilotModelSelection };
