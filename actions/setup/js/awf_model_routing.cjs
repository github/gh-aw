// @ts-check

"use strict";

const { REFLECT_PROVIDER_ALIASES, normalizeReflectProviderName } = require("./awf_reflect.cjs");
const fs = require("fs");
const path = require("path");

const ROUTING_REASONING_EFFORTS = Object.freeze(["none", "minimal", "low", "medium", "high", "xhigh", "max"]);
const ROUTING_ENDPOINTS = Object.freeze(["/v1/messages", "/responses", "/chat/completions"]);
const AWF_MODEL_ROUTING_POLICIES = Object.freeze({
  copilot: Object.freeze({ endpoints: Object.freeze(["/responses", "/chat/completions"]), allowEndpointOverride: false }),
  claude: Object.freeze({ endpoints: Object.freeze(["/v1/messages"]), allowEndpointOverride: true }),
  codex: Object.freeze({ endpoints: Object.freeze(["/responses"]), allowEndpointOverride: true }),
  pi: Object.freeze({ endpoints: ROUTING_ENDPOINTS, allowEndpointOverride: true }),
});
const EMPTY_MODEL_ROUTING_POLICY = Object.freeze({ endpoints: Object.freeze([]), allowEndpointOverride: false });

/**
 * @param {string} engine
 * @returns {{endpoints: string[], allowEndpointOverride: boolean}}
 */
function getAWFModelRoutingPolicy(engine) {
  return AWF_MODEL_ROUTING_POLICIES[String(engine || "").toLowerCase()] || EMPTY_MODEL_ROUTING_POLICY;
}

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
 * @param {boolean} [allowEndpointOverride]
 * @returns {{selection: {provider: string, model: string, wire_model: string, effort: string|null, endpoint: string, selected_endpoint: string}|null, error: string|null}}
 */
function resolveAWFModelRoutingSelection(reflectData, routingRequired = false, allowedEndpoints = null, allowEndpointOverride = false) {
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
  const selectedEndpoint = typeof selection.selected_endpoint === "string" ? selection.selected_endpoint.trim() : endpoint;
  if (!["copilot", "github-copilot", "github"].includes(provider) || !wireModel || !endpoint) {
    return { selection: null, error: "AWF /reflect returned an incomplete or unsupported Copilot routing selection" };
  }
  if (endpoint !== selectedEndpoint && allowedEndpoints) {
    if (!allowEndpointOverride) {
      return { selection: null, error: `AWF /reflect selected endpoint ${endpoint}, which is not supported by this engine` };
    }
    const routingModel = getAWFRoutingModel(reflectData, wireModel);
    const supportedEndpoints = routingModel?.supported_endpoints;
    if (routingModel?.candidate_metadata_complete !== true || !Array.isArray(supportedEndpoints) || !supportedEndpoints.every(value => typeof value === "string")) {
      return { selection: null, error: `AWF /reflect cannot verify endpoints for model ${wireModel}; candidate metadata is incomplete for engine API ${allowedEndpoints.join(", ")}` };
    }
    if (!supportedEndpoints.includes(endpoint)) {
      return { selection: null, error: `AWF model ${wireModel} does not advertise endpoint ${endpoint}` };
    }
  }
  if (!isModelAvailableInReflectData(wireModel, reflectData, REFLECT_PROVIDER_ALIASES.github)) {
    return { selection: null, error: `AWF /reflect selected unavailable Copilot wire model ${wireModel}` };
  }
  const routingModelAmbiguity = getAWFRoutingModelAmbiguityError(reflectData, wireModel);
  if (routingModelAmbiguity) return { selection: null, error: routingModelAmbiguity };
  let effectiveEndpoint = endpoint;
  if (allowedEndpoints && !allowedEndpoints.includes(endpoint)) {
    if (!allowEndpointOverride || !ROUTING_ENDPOINTS.includes(endpoint)) {
      return { selection: null, error: `AWF /reflect selected endpoint ${endpoint}, which is not supported by this engine` };
    }
    const routingModel = getAWFRoutingModel(reflectData, wireModel);
    const supportedEndpoints = routingModel?.supported_endpoints;
    if (routingModel?.candidate_metadata_complete !== true || !Array.isArray(supportedEndpoints) || !supportedEndpoints.every(value => typeof value === "string")) {
      return { selection: null, error: `AWF /reflect cannot verify endpoints for model ${wireModel}; candidate metadata is incomplete for engine API ${allowedEndpoints.join(", ")}` };
    }
    effectiveEndpoint = allowedEndpoints.find(candidate => supportedEndpoints.includes(candidate)) || "";
    if (!effectiveEndpoint) {
      return {
        selection: null,
        error: `AWF model ${wireModel} advertises endpoints [${supportedEndpoints.join(", ")}], none compatible with this engine API [${allowedEndpoints.join(", ")}]`,
      };
    }
  }
  const effort = typeof selection.effort === "string" && selection.effort.trim() ? selection.effort.trim().toLowerCase() : null;
  if (effort && !isRoutingReasoningEffort(effort)) {
    return { selection: null, error: `AWF /reflect returned unsupported reasoning effort ${effort}` };
  }
  return {
    selection: { provider, model: String(selection.model || ""), wire_model: wireModel, effort, endpoint: effectiveEndpoint, selected_endpoint: endpoint },
    error: null,
  };
}

/**
 * @param {any} reflectData
 * @param {string} wireModel
 * @returns {any|null}
 */
function getAWFRoutingModel(reflectData, wireModel) {
  const matches = getAWFRoutingModelMatches(reflectData, wireModel);
  return matches.length === 1 ? matches[0].model : null;
}

/**
 * @param {any} reflectData
 * @param {string} wireModel
 * @returns {string|null}
 */
function getAWFRoutingModelAmbiguityError(reflectData, wireModel) {
  const matches = getAWFRoutingModelMatches(reflectData, wireModel);
  if (matches.length <= 1) return null;
  const endpoints = matches.map(match => match.endpoint).join(", ");
  return `AWF /reflect has ambiguous routing metadata for model ${wireModel}; configured GitHub endpoints [${endpoints}] all list this model`;
}

/**
 * @param {any} reflectData
 * @param {string} wireModel
 * @returns {{endpoint: string, model: any}[]}
 */
function getAWFRoutingModelMatches(reflectData, wireModel) {
  const endpoints = Array.isArray(reflectData?.endpoints) ? reflectData.endpoints : [];
  const normalizedModel = wireModel.toLowerCase();
  const matches = [];
  for (const endpoint of endpoints) {
    if (endpoint?.configured !== true || !REFLECT_PROVIDER_ALIASES.github.has(normalizeReflectProviderName(endpoint.provider))) continue;
    const model = endpoint.routing_models?.find(candidate => typeof candidate?.model_id === "string" && candidate.model_id.toLowerCase() === normalizedModel);
    if (model) matches.push({ endpoint: typeof endpoint.provider === "string" ? endpoint.provider.trim().toLowerCase() : "unknown", model });
  }
  return matches;
}

function recordAWFModelRoutingOutcome(outcome, env = process.env, filePath = path.join(env.GH_AW_TMP_DIR || "/tmp/gh-aw", "agent", "awf-routing-outcome.json")) {
  if (env.GH_AW_MODEL_ROUTING !== "1" || !outcome || !["selected", "failed", "rejected"].includes(outcome.status)) return false;
  const wireModel = typeof outcome.wire_model === "string" && outcome.wire_model.length <= 128 && /^[A-Za-z0-9._/:@-]+$/.test(outcome.wire_model) ? outcome.wire_model : "";
  if (outcome.status === "selected" && !wireModel) return false;
  const endpoint = ROUTING_ENDPOINTS.includes(outcome.endpoint) ? outcome.endpoint : "";
  const selectedEndpoint = ROUTING_ENDPOINTS.includes(outcome.selected_endpoint) ? outcome.selected_endpoint : "";
  const effort = outcome.effort == null ? null : isRoutingReasoningEffort(outcome.effort) ? outcome.effort : null;
  if (outcome.effort != null && effort === null) return false;
  const appliedEffort = outcome.applied_effort == null ? null : outcome.applied_effort === "off" || isRoutingReasoningEffort(outcome.applied_effort) ? outcome.applied_effort : null;
  if (outcome.applied_effort != null && appliedEffort === null) return false;
  const record = {
    status: outcome.status,
    ...(wireModel ? { wire_model: wireModel } : {}),
    ...(endpoint ? { endpoint } : {}),
    ...(selectedEndpoint ? { selected_endpoint: selectedEndpoint } : {}),
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
  ROUTING_ENDPOINTS,
  AWF_MODEL_ROUTING_POLICIES,
  getAWFModelRoutingPolicy,
  isRoutingReasoningEffort,
  mapAWFRoutingEffort,
  resolveAWFModelRoutingSelection,
  getAWFRoutingModel,
  getAWFRoutingModelAmbiguityError,
  isModelAvailableInReflectData,
  recordAWFModelRoutingOutcome,
};
