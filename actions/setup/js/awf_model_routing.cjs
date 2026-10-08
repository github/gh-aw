// @ts-check

"use strict";

const { REFLECT_PROVIDER_ALIASES, normalizeReflectProviderName } = require("./awf_reflect.cjs");
const fs = require("fs");
const path = require("path");

const ROUTING_REASONING_EFFORTS = Object.freeze(["none", "minimal", "low", "medium", "high", "xhigh", "max"]);

/**
 * @param {unknown} effort
 * @returns {boolean}
 */
function isRoutingReasoningEffort(effort) {
  return typeof effort === "string" && ROUTING_REASONING_EFFORTS.includes(effort);
}

/**
 * @param {string} engine
 * @param {string|null} effort
 * @returns {{effort: string|null, error: string|null}}
 */
function mapAWFRoutingEffort(engine, effort) {
  if (effort === null) return { effort: null, error: null };
  if (!isRoutingReasoningEffort(effort)) {
    return { effort: null, error: `AWF model routing effort "${effort}" is not supported by the ${engine} engine` };
  }
  if (engine === "copilot") return { effort, error: null };
  const supported = {
    claude: new Set(["low", "medium", "high", "xhigh", "max"]),
    codex: new Set(["minimal", "low", "medium", "high", "xhigh"]),
    pi: new Set(ROUTING_REASONING_EFFORTS),
  }[engine];
  if (engine === "pi" && effort === "none") return { effort: "off", error: null };
  if (!supported || !supported.has(effort)) {
    return { effort: null, error: `AWF model routing effort "${effort}" is not supported by the ${engine} engine` };
  }
  return { effort, error: null };
}

/**
 * Resolve the task-level AWF model selection when routing is enabled.
 * @param {any} reflectData
 * @param {boolean} routingRequired
 * @param {string[]|null} [allowedEndpoints]
 * @returns {{selection: {provider: string, model: string, wire_model: string, effort: string|null, endpoint: string}|null, error: string|null}}
 */
function resolveAWFModelRoutingSelection(reflectData, routingRequired = false, allowedEndpoints = null) {
  const routing = reflectData && typeof reflectData === "object" ? reflectData.routing : null;
  if (routing == null) {
    return routingRequired ? { selection: null, error: "AWF /reflect did not return the required model-routing selection" } : { selection: null, error: null };
  }
  if (routing.status !== "selected") {
    const failure = typeof routing.failure_code === "string" ? ` (${routing.failure_code})` : "";
    return { selection: null, error: `AWF model routing is ${String(routing.status || "pending")}${failure}` };
  }
  const selection = routing.selection;
  if (!selection || typeof selection !== "object") {
    return { selection: null, error: "AWF /reflect reported selected routing without a selection object" };
  }
  const provider = typeof selection.provider === "string" ? selection.provider.trim().toLowerCase() : "";
  const wireModel = typeof selection.wire_model === "string" ? selection.wire_model.trim() : "";
  const endpoint = typeof selection.endpoint === "string" ? selection.endpoint.trim() : "";
  if (!["copilot", "github-copilot", "github"].includes(provider) || !wireModel || !endpoint) {
    return { selection: null, error: "AWF /reflect returned an incomplete or unsupported Copilot routing selection" };
  }
  if (allowedEndpoints && !allowedEndpoints.includes(endpoint)) {
    return { selection: null, error: `AWF /reflect selected endpoint ${endpoint}, which is not supported by this engine` };
  }
  if (!isModelAvailableInReflectData(wireModel, reflectData, REFLECT_PROVIDER_ALIASES.github)) {
    return { selection: null, error: `AWF /reflect selected unavailable Copilot wire model ${wireModel}` };
  }
  const effort = typeof selection.effort === "string" && selection.effort.trim() ? selection.effort.trim().toLowerCase() : null;
  if (effort && !isRoutingReasoningEffort(effort)) {
    return { selection: null, error: `AWF /reflect returned unsupported reasoning effort ${effort}` };
  }
  return { selection: { provider, model: String(selection.model || ""), wire_model: wireModel, effort, endpoint }, error: null };
}

function recordAWFModelRoutingOutcome(outcome, env = process.env, filePath = path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", "agent", "awf-routing-outcome.json")) {
  if (env.GH_AW_MODEL_ROUTING !== "1" || !outcome || !["selected", "failed", "rejected"].includes(outcome.status)) return false;
  const wireModel = typeof outcome.wire_model === "string" && outcome.wire_model.length <= 128 && /^[A-Za-z0-9._/:@-]+$/.test(outcome.wire_model) ? outcome.wire_model : "";
  if (outcome.status === "selected" && !wireModel) return false;
  const effort = outcome.effort == null ? null : isRoutingReasoningEffort(outcome.effort) ? outcome.effort : null;
  if (outcome.effort != null && effort === null) return false;
  const appliedEffort = outcome.applied_effort == null ? null : outcome.applied_effort === "off" || isRoutingReasoningEffort(outcome.applied_effort) ? outcome.applied_effort : null;
  if (outcome.applied_effort != null && appliedEffort === null) return false;
  const record = {
    status: outcome.status,
    ...(wireModel ? { wire_model: wireModel } : {}),
    ...(effort ? { effort } : {}),
    ...(appliedEffort ? { applied_effort: appliedEffort } : {}),
    ...(typeof outcome.failure_code === "string" && /^[A-Za-z0-9_-]{1,64}$/.test(outcome.failure_code) ? { failure_code: outcome.failure_code } : {}),
    ...(typeof outcome.detail === "string" ? { detail: outcome.detail.slice(0, 512) } : {}),
  };
  try {
    fs.mkdirSync(path.dirname(filePath), { recursive: true });
    fs.writeFileSync(filePath, JSON.stringify(record) + "\n");
    return true;
  } catch {
    return false;
  }
}

/**
 * Check whether a model is present in AWF /reflect endpoint data.
 * @param {string} model
 * @param {unknown} reflectData
 * @param {Set<string>|null} [allowedProviders]
 * @returns {boolean}
 */
function isModelAvailableInReflectData(model, reflectData, allowedProviders = null) {
  const normalizedModel = typeof model === "string" ? model.trim() : "";
  if (!normalizedModel || !reflectData || typeof reflectData !== "object") return false;
  const endpoints = "endpoints" in reflectData && Array.isArray(reflectData.endpoints) ? reflectData.endpoints : [];
  return endpoints.some(
    endpoint => endpoint?.configured === true && (!allowedProviders || allowedProviders.has(normalizeReflectProviderName(endpoint.provider))) && Array.isArray(endpoint.models) && endpoint.models.includes(normalizedModel)
  );
}

module.exports = {
  ROUTING_REASONING_EFFORTS,
  isRoutingReasoningEffort,
  mapAWFRoutingEffort,
  resolveAWFModelRoutingSelection,
  isModelAvailableInReflectData,
  recordAWFModelRoutingOutcome,
};
