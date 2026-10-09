"use strict";

const { getErrorMessage } = require("./error_helpers.cjs");
const { computeInferenceAIC } = require("./model_costs.cjs");

const ROUTING_STATUSES = new Set(["selected", "pending", "failed", "rejected", "unavailable"]);
const ROUTING_EFFORTS = new Set(["none", "minimal", "low", "medium", "high", "xhigh", "max", "off"]);
const ROUTING_TASK_TYPES = new Set(["explain", "plan", "fix", "refactor", "chore", "implement", "unknown"]);
const ROUTING_SCOPES = new Set(["local", "multi_file", "subsystem", "cross_system", "unknown"]);
const ROUTING_COMPLEXITIES = new Set(["trivial", "easy", "medium", "hard", "expert", "unknown"]);
const MODEL_ROUTING_LOG_PATHS = ["sandbox/firewall/logs/api-proxy-logs/model-routing.jsonl", "sandbox/firewall/audit/api-proxy-logs/model-routing.jsonl", "sandbox/firewall-audit-logs/api-proxy-logs/model-routing.jsonl"];

/** @typedef {{ status: string, mode: string, router_version: string, failure_code?: string, objective: string, task_type: string, scope: string, complexity: string, degraded?: boolean, classifier_aic?: number, deviated_requests?: number }} ModelRoutingSummary */

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
  return infoPath;
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
    ...(routing.selected_endpoint ? { selected_endpoint: validateModelIdentifier(routing.selected_endpoint) } : {}),
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

function readModelRoutingSession(sessionPath) {
  const fs = require("fs");
  let content;
  try {
    content = fs.readFileSync(sessionPath, "utf8");
  } catch {
    return [];
  }
  const events = [];
  for (const line of content.split("\n")) {
    try {
      const event = JSON.parse(line);
      const provenance = event?.provenance;
      const agentEvent =
        (event?.type === "workflow.info" && provenance?.component === "workflow" && provenance?.phase === "agent") ||
        (event?.type === "model_routing.outcome" && provenance?.component === "agent" && provenance?.phase === "agent") ||
        ((event?.type === "firewall.model_routing" || event?.type === "firewall.token_usage") && provenance?.component === "firewall" && provenance?.phase === "agent");
      if (agentEvent) events.push(event);
    } catch {
      // Ignore partial session records.
    }
  }
  return events;
}

function readModelRoutingProxyRecords(ghAwDir) {
  const fs = require("fs");
  for (const relativePath of MODEL_ROUTING_LOG_PATHS) {
    let content;
    try {
      content = fs.readFileSync(require("path").join(ghAwDir, relativePath), "utf8");
    } catch {
      continue;
    }
    const records = [];
    for (const line of content.split("\n")) {
      try {
        const record = JSON.parse(line);
        if (typeof record?._schema === "string" && record._schema.startsWith("model-routing/")) records.push(record);
      } catch {
        // Ignore incomplete proxy records.
      }
    }
    return records;
  }
  return [];
}

function isLegacyEndpointOnlyDeviation(schema) {
  const match = /^model-routing\/v?(\d+)\.(\d+)\.(\d+)/.exec(schema || "");
  if (!match) return false;
  const [, major, minor, patch] = match.map(Number);
  return major === 0 && (minor < 28 || (minor === 28 && patch < 39));
}

function countDeviatedModelRoutingRequests(records, schema) {
  const legacyEndpointOnly = isLegacyEndpointOnlyDeviation(schema);
  return records.filter(record => {
    if (record?.routed !== "deviated") return false;
    const deviations = record.deviations;
    return !(legacyEndpointOnly && Array.isArray(deviations) && deviations.length === 1 && deviations[0] === "endpoint");
  }).length;
}

function classifierAIC(sessionEvents) {
  const classifierUsage = sessionEvents.filter(event => event.type === "firewall.token_usage" && event.data?.purpose === "routing_classification");
  if (classifierUsage.length === 0) return undefined;

  return classifierUsage.reduce((total, event) => {
    const data = event.data;
    const reportedAIC = data.aic;
    if (typeof reportedAIC === "number" && Number.isFinite(reportedAIC) && reportedAIC >= 0) return total + reportedAIC;

    const usage = data.usage ?? {};
    const tokenCount = value => (typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : 0);
    const estimatedAIC = computeInferenceAIC({
      provider: typeof data.provider === "string" ? data.provider : "",
      model: typeof data.model === "string" ? data.model : "",
      inputTokens: tokenCount(usage.inputTokens),
      outputTokens: tokenCount(usage.outputTokens),
      cacheReadTokens: tokenCount(usage.cacheReadInputTokens),
      cacheWriteTokens: tokenCount(usage.cacheCreationInputTokens),
      reasoningTokens: tokenCount(usage.reasoningOutputTokens),
      ...(typeof usage.inputTokensIncludeCache === "boolean" ? { inputTokensIncludeCache: usage.inputTokensIncludeCache } : {}),
    });
    return total + (Number.isFinite(estimatedAIC) && estimatedAIC >= 0 ? estimatedAIC : 0);
  }, 0);
}

function firstValidated(...values) {
  for (const value of values) {
    const validated = validateModelIdentifier(value);
    if (validated) return validated;
  }
  return "";
}

function firstAllowed(allowedValues, ...values) {
  return values.find(value => typeof value === "string" && allowedValues.has(value)) || "";
}

function resolveModelRoutingSummary({
  infoPath = `${process.env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`,
  sessionPath = `${process.env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/usage/aw_session.jsonl`,
  ghAwDir = process.env.GH_AW_TMP_DIR || "/tmp/gh-aw",
} = {}) {
  const sessionEvents = readModelRoutingSession(sessionPath);
  const workflowInfo = sessionEvents.find(event => event.type === "workflow.info")?.data;
  const outcome = sessionEvents.find(event => event.type === "model_routing.outcome")?.data;
  const sessionRouting = workflowInfo?.modelRouting ?? workflowInfo?.model_routing ?? {};
  const sessionRecords = sessionEvents.filter(event => event.type === "firewall.model_routing").map(event => event.data);
  const sessionSelection = sessionRecords.find(record => record?.stage === "selection");
  const sessionFailure = sessionRecords.find(record => record?.stage === "failure");
  const infoRouting = getModelRouting(infoPath);

  // The agent post-step runs before the usage-artifact step writes aw_session.jsonl.
  // Use the session when available, then aw_info.json and proxy logs as fallbacks.
  const proxyRecords = readModelRoutingProxyRecords(ghAwDir);
  const proxySelection = proxyRecords.filter(record => record.stage === "selection").at(-1);
  const proxyFailure = proxyRecords.filter(record => record.stage === "failure").at(-1);
  const proxyRequests = proxyRecords.filter(record => record.stage === "request");
  const proxySchema = proxyRecords.find(record => record?._schema)?.["_schema"] || "";
  const sessionRequests = sessionRecords.filter(record => record?.stage === "request");
  const requestRecords = sessionRequests.length ? sessionRequests : proxyRequests;
  const requestSchema = sessionRecords.find(record => typeof (record?._schema || record?.schema) === "string")?._schema || sessionRecords.find(record => typeof record?.schema === "string")?.schema || proxySchema;

  const status = [outcome?.status, sessionRouting.status, sessionSelection ? "selected" : "", sessionFailure ? "failed" : "", infoRouting?.status, proxySelection ? "selected" : "", proxyFailure ? "failed" : ""].find(
    value => typeof value === "string" && ROUTING_STATUSES.has(value)
  );
  if (!status) return null;

  const selection = sessionSelection || proxySelection || {};
  const objective = selection.objective?.goal ?? selection.objective?.Goal;
  const labels = selection.labels ?? {};
  const classifierAic = classifierAIC(sessionEvents);
  /** @type {ModelRoutingSummary} */
  const result = {
    status,
    mode: firstValidated(sessionRouting.mode, sessionRouting.Mode, selection.mode, infoRouting?.mode, proxySelection?.mode),
    router_version: firstValidated(sessionRouting.routerVersion, sessionRouting.router_version, sessionRouting.router?.version, selection.router?.version, infoRouting?.router_version, proxySelection?.router?.version),
    ...(status !== "selected"
      ? {
          failure_code: firstValidated(outcome?.failureCode, outcome?.failure_code, sessionRouting.failureCode, sessionRouting.failure_code, sessionFailure?.code, infoRouting?.failure_code, proxyFailure?.code),
        }
      : {}),
    objective: firstValidated(objective),
    task_type: firstAllowed(ROUTING_TASK_TYPES, labels.task_type, labels.taskType),
    scope: firstAllowed(ROUTING_SCOPES, labels.scope),
    complexity: firstAllowed(ROUTING_COMPLEXITIES, labels.task_complexity, labels.taskComplexity),
    ...(typeof classifierAic === "number" ? { classifier_aic: classifierAic } : {}),
  };
  if (typeof selection.degraded_classification === "boolean") {
    result.degraded = selection.degraded_classification;
  } else if (typeof selection.degradedClassification === "boolean") {
    result.degraded = selection.degradedClassification;
  }
  if (requestRecords.length > 0) {
    result.deviated_requests = countDeviatedModelRoutingRequests(requestRecords, requestSchema);
  }
  return result;
}

function resolveEffectiveModel(infoPath = `${process.env.GH_AW_TMP_DIR || "/tmp/gh-aw"}/aw_info.json`, phase = process.env.GH_AW_PHASE || "agent", env = process.env) {
  let routing = getModelRouting(infoPath, phase);
  if (!routing && typeof env.GH_AW_MODEL_ROUTING_STATUS === "string" && ROUTING_STATUSES.has(env.GH_AW_MODEL_ROUTING_STATUS)) {
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
  resolveModelRoutingSummary,
  resolveEffectiveModel,
  getEffectiveModelLabel,
  validateRoutingEffort,
};
