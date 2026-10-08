"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");

const ROUTING_STATUSES = new Set(["selected", "pending", "failed", "rejected", "unavailable"]);
const ROUTING_EFFORTS = new Set(["none", "minimal", "low", "medium", "high", "xhigh", "max", "off"]);

/** @param {unknown} value @returns {string} */
function validateModelIdentifier(value) {
  if (typeof value !== "string") return "";
  // Reject rather than repair telemetry that could forge footer marker fields.
  if (!value || value.length > 128 || /[^A-Za-z0-9._/:@-]/.test(value)) return "";
  return value;
}

function readFallbackMetadata(filePath) {
  try {
    return JSON.parse(require("fs").readFileSync(filePath, "utf8"));
  } catch (error) {
    throw new Error(`Cannot read model fallback metadata '${filePath}': ${getErrorMessage(error)}`, { cause: error });
  }
}

function resolveAwInfoPath(infoPath) {
  const fs = require("fs");
  const agentInfoPath = require("path").join(require("path").dirname(infoPath), "agent", "aw_info.json");
  return fs.existsSync(agentInfoPath) ? agentInfoPath : infoPath;
}

function recordFallbackModel(model, env, infoPath) {
  const fs = require("fs");
  const info = fs.existsSync(infoPath) ? readFallbackMetadata(infoPath) : {};
  const phase = env.GH_AW_PHASE || "agent";
  if (phase === "agent") {
    info.model = model;
    info.fallback_model = model;
  } else {
    info[`${phase}_fallback_model`] = model;
  }
  try {
    fs.mkdirSync(require("path").dirname(infoPath), { recursive: true });
    fs.writeFileSync(infoPath, JSON.stringify(info, null, 2));
  } catch (error) {
    throw new Error(`Cannot record model fallback metadata '${infoPath}': ${getErrorMessage(error)}`, { cause: error });
  }
}

function getFallbackModel(infoPath = `${process.env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`, phase = "agent") {
  const fs = require("fs");
  const resolvedPath = resolveAwInfoPath(infoPath);
  if (!fs.existsSync(resolvedPath)) return "";
  const info = readFallbackMetadata(resolvedPath);
  const model = phase === "agent" ? info.fallback_model : info[`${phase}_fallback_model`];
  return validateModelIdentifier(model);
}

function validateRoutingEffort(value) {
  return typeof value === "string" && ROUTING_EFFORTS.has(value) ? value : "";
}

function recordModelRouting(routing, env = process.env, infoPath = `${env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`) {
  if (!routing || !ROUTING_STATUSES.has(routing.status)) return null;
  const wireModel = validateModelIdentifier(routing.wire_model || routing.model);
  if (routing.status === "selected" && !wireModel) return null;
  const effort = routing.effort == null ? "" : validateRoutingEffort(routing.effort);
  if (routing.effort != null && !effort) return null;
  const appliedEffort = routing.applied_effort == null ? "" : validateRoutingEffort(routing.applied_effort);
  if (routing.applied_effort != null && !appliedEffort) return null;

  const fs = require("fs");
  const info = fs.existsSync(infoPath) ? readFallbackMetadata(infoPath) : {};
  const requestedModel = validateModelIdentifier(info.requested_model || info.model || env.GH_AW_INFO_MODEL || env.GH_AW_ENGINE_MODEL);
  const modelRouting = {
    status: routing.status,
    source: "awf-routing",
    ...(routing.provider ? { provider: validateModelIdentifier(routing.provider) } : {}),
    ...(wireModel ? { wire_model: wireModel, model: validateModelIdentifier(routing.model) || wireModel } : {}),
    ...(effort ? { effort } : {}),
    ...(appliedEffort ? { applied_effort: appliedEffort } : {}),
    ...(routing.endpoint ? { endpoint: validateModelIdentifier(routing.endpoint) } : {}),
    ...(routing.mode ? { mode: validateModelIdentifier(routing.mode) } : {}),
    ...(routing.selected_id ? { selected_id: validateModelIdentifier(routing.selected_id) } : {}),
    ...(routing.router_version ? { router_version: validateModelIdentifier(routing.router_version) } : {}),
    ...(routing.failure_code ? { failure_code: validateModelIdentifier(routing.failure_code) } : {}),
    ...(routing.detail ? { detail: String(routing.detail).slice(0, 512) } : {}),
  };
  if (requestedModel) {
    info.requested_model = requestedModel;
    if (info.model !== wireModel) info.requested_model = validateModelIdentifier(info.requested_model);
  }
  info.model_routing = modelRouting;
  if (routing.status === "selected") {
    info.model = wireModel;
    info.routed_model = wireModel;
  }
  try {
    fs.mkdirSync(require("path").dirname(infoPath), { recursive: true });
    fs.writeFileSync(infoPath, JSON.stringify(info, null, 2));
  } catch (error) {
    throw new Error(`Cannot record model routing metadata '${infoPath}': ${getErrorMessage(error)}`, { cause: error });
  }
  return modelRouting;
}

function getModelRouting(infoPath = `${process.env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`, phase = "agent") {
  if (phase !== "agent") return null;
  const fs = require("fs");
  const resolvedPath = resolveAwInfoPath(infoPath);
  if (!fs.existsSync(resolvedPath)) return null;
  const info = readFallbackMetadata(resolvedPath);
  const routing = info.model_routing;
  if (!routing || !ROUTING_STATUSES.has(routing.status)) return null;
  return {
    ...routing,
    wire_model: validateModelIdentifier(routing.wire_model),
    effort: validateRoutingEffort(routing.effort) || "",
    applied_effort: validateRoutingEffort(routing.applied_effort) || "",
    requested_model: validateModelIdentifier(info.requested_model),
  };
}

function resolveEffectiveModel(infoPath = `${process.env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`, phase = process.env.GH_AW_PHASE || "agent", env = process.env) {
  let routing = getModelRouting(infoPath, phase);
  if (!routing && ROUTING_STATUSES.has(env.GH_AW_MODEL_ROUTING_STATUS)) {
    routing = {
      status: env.GH_AW_MODEL_ROUTING_STATUS,
      wire_model: env.GH_AW_MODEL_ROUTING_STATUS === "selected" ? validateModelIdentifier(env.GH_AW_ENGINE_MODEL) : "",
      effort: validateRoutingEffort(env.GH_AW_ENGINE_MODEL_EFFORT),
      applied_effort: validateRoutingEffort(env.GH_AW_ENGINE_MODEL_EFFORT),
      requested_model: validateModelIdentifier(env.GH_AW_INFO_MODEL || env.GH_AW_ENGINE_MODEL),
    };
  }
  const fallbackModel = getFallbackModel(infoPath, phase);
  const routedModel = routing?.status === "selected" ? validateModelIdentifier(routing.wire_model) : "";
  const primaryModel = validateModelIdentifier(env.GH_AW_PRIMARY_MODEL);
  const configuredModel = validateModelIdentifier(env.GH_AW_ENGINE_MODEL);
  const model = fallbackModel || (routing ? (routing.status === "selected" ? routedModel || primaryModel || configuredModel : "") : configuredModel);
  return {
    model,
    fallbackModel,
    routing,
    requestedModel: validateModelIdentifier(routing?.requested_model || (routing ? env.GH_AW_INFO_MODEL || env.GH_AW_ENGINE_MODEL : "")),
    effort: routing?.status === "selected" ? routing.applied_effort || routing.effort : "",
  };
}

function getEffectiveModelLabel(infoPath = `${process.env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`, phase = process.env.GH_AW_PHASE || "agent", env = process.env) {
  const attribution = resolveEffectiveModel(infoPath, phase, env);
  const { reduceModelNameToIdentifier } = require("./model_aliases.cjs");
  const shortModel = reduceModelNameToIdentifier(attribution.model);
  if (!attribution.routing) return shortModel;
  if (attribution.routing.status === "selected") {
    const routedModel = reduceModelNameToIdentifier(attribution.routing.wire_model);
    const routed = `routed: ${routedModel}${attribution.effort ? ` ${attribution.effort}` : ""}`;
    return attribution.fallbackModel ? `fallback: ${shortModel} (${routed})` : routed;
  }
  const failureCode = validateModelIdentifier(attribution.routing.failure_code);
  return `routing ${attribution.routing.status}${failureCode ? ` (${failureCode})` : ""}`;
}

function recordFallbackModelFromUsage(content, env = process.env, infoPath = `${env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`, logger = console.warn) {
  let model = "";
  for (const line of content.split("\n")) {
    if (!line.includes('"model_fallback"')) continue;
    let entry;
    try {
      entry = JSON.parse(line);
    } catch (error) {
      logger(`Skipping malformed AWF fallback usage record: ${getErrorMessage(error)}`);
      continue;
    }
    if (typeof entry?._schema !== "string" || !entry._schema.startsWith("token-usage/") || !entry.model_fallback || Number(entry.status) >= 400) continue;
    const validated = validateModelIdentifier(entry.model || entry.model_fallback.model);
    if (validated) model = validated;
  }
  if (model) recordFallbackModel(model, env, infoPath);
  return model;
}

module.exports = {
  validateModelIdentifier,
  getFallbackModel,
  recordFallbackModelFromUsage,
  recordModelRouting,
  getModelRouting,
  resolveEffectiveModel,
  getEffectiveModelLabel,
  validateRoutingEffort,
};
