// @ts-check

"use strict";

const { REFLECT_PROVIDER_ALIASES, normalizeReflectProviderName } = require("./awf_reflect.cjs");

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
  if (!["copilot", "github-copilot", "github"].includes(provider) || !wireModel || !endpoint) {
    return { selection: null, error: "AWF /reflect returned an incomplete or unsupported Copilot routing selection" };
  }
  if (!isModelAvailableInReflectData(wireModel, reflectData, REFLECT_PROVIDER_ALIASES.github)) {
    return { selection: null, error: `AWF /reflect selected unavailable Copilot wire model ${wireModel}` };
  }
  const routingModelAmbiguity = getAWFRoutingModelAmbiguityError(reflectData, wireModel);
  if (routingModelAmbiguity) return { selection: null, error: routingModelAmbiguity };
  let effectiveEndpoint = endpoint;
  if (allowedEndpoints && !allowedEndpoints.includes(endpoint)) {
    if (!allowEndpointOverride) {
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
  getAWFRoutingModel,
  getAWFRoutingModelAmbiguityError,
  isModelAvailableInReflectData,
};
