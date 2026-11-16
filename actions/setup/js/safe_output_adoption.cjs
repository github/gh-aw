// @ts-check

const registry = require("./safe_output_adoption_rules.json");

const RULES = new Map(registry.rules.map(rule => [rule.type, Object.freeze({ ...rule })]));
const DELIVERY_FAILURE = new Set(["cancelled", "failed", "failure"]);
const DELIVERY_DEFERRED = new Set(["deferred", "delegated"]);

/**
 * @typedef {{
 *   state: string,
 *   signal: string,
 *   source: string,
 *   ruleVersion: string,
 *   effectClass?: string,
 *   fallbackToDelivery?: boolean,
 *   attributionEligible?: boolean,
 *   survivalRequired?: boolean,
 *   acceptanceKind?: string,
 *   adoptionAt?: string,
 * }} SafeOutputAdoptionEvaluation
 */

/** @param {string} outputType */
function safeOutputAdoptionRule(outputType) {
  const rule = RULES.get(outputType);
  return rule ? { ...rule } : null;
}

/** @param {unknown} value */
function observedTimestamp(value) {
  return typeof value === "string" && Number.isFinite(Date.parse(value)) ? value : undefined;
}

/** @param {string} eventType @param {unknown} status @param {unknown} operationOutcome */
function deliveryEvaluation(eventType, status, operationOutcome) {
  if (operationOutcome === "indeterminate") return { state: "unknown", signal: "indeterminate" };
  const normalized = typeof status === "string" ? status.toLowerCase() : "";
  if ((eventType === "safe_output.result" && !normalized) || normalized === "success" || normalized === "warning") {
    return { state: "delivered", signal: normalized || "safe_output.result" };
  }
  if (DELIVERY_FAILURE.has(normalized)) return { state: "failed", signal: normalized };
  if (normalized === "skipped") return { state: "skipped", signal: normalized };
  if (normalized === "staged") return { state: "staged", signal: normalized };
  if (DELIVERY_DEFERRED.has(normalized)) return { state: "deferred", signal: normalized };
  return { state: "unknown", signal: normalized || "delivery_status_unknown" };
}

/**
 * @param {string} outputType
 * @param {string | undefined} deliveredAt
 * @returns {SafeOutputAdoptionEvaluation}
 */
function evaluateSafeOutputDelivery(outputType, deliveredAt) {
  const rule = RULES.get(outputType);
  if (!rule) {
    return { state: "unknown", signal: "unsupported_safe_output", source: "adoption-registry", ruleVersion: registry.version };
  }
  const result = {
    ruleVersion: registry.version,
    effectClass: rule.effectClass,
    fallbackToDelivery: rule.fallbackToDelivery,
    attributionEligible: rule.attributionEligible,
    survivalRequired: rule.attributionEligible,
  };
  if (!rule.attributionEligible) {
    return { ...result, state: "not_applicable", signal: "no_repository_value_effect", source: "adoption-registry" };
  }
  if (rule.effectClass === "proposal" && !rule.fallbackToDelivery) {
    return { ...result, state: "pending", signal: "human_decision_pending", source: "adoption-registry" };
  }
  if (rule.fallbackToDelivery) {
    return {
      ...result,
      state: "accepted",
      signal: "delivered",
      source: "delivery-rule",
      acceptanceKind: "fallback_delivery",
      ...(deliveredAt ? { adoptionAt: deliveredAt } : {}),
    };
  }
  return { ...result, state: "unknown", signal: "missing_acceptance_rule", source: "adoption-registry" };
}

/**
 * @param {Record<string, any>} data
 * @param {string} eventType
 * @param {unknown} timestamp
 */
function projectSafeOutputLifecycle(data, eventType, timestamp) {
  if (eventType !== "safe_output.result" && eventType !== "safe_output.delivery") return {};
  const delivery = deliveryEvaluation(eventType, data.status, data.operation_outcome);
  const rule = typeof data.type === "string" ? RULES.get(data.type) : undefined;
  const base = {
    deliveryState: delivery.state,
    deliverySignal: delivery.signal,
    deliverySource: eventType,
    adoptionRuleVersion: registry.version,
    ...(rule
      ? {
          effectClass: rule.effectClass,
          fallbackToDelivery: rule.fallbackToDelivery,
          attributionEligible: rule.attributionEligible,
          survivalRequired: rule.attributionEligible,
        }
      : {}),
  };
  if (rule && !rule.attributionEligible) {
    return { ...base, adoptionState: "not_applicable", adoptionSignal: "no_repository_value_effect", adoptionSource: "adoption-registry" };
  }
  if (delivery.state !== "delivered") {
    return { ...base, adoptionState: "unknown", adoptionSignal: "delivery_not_established", adoptionSource: "delivery-status" };
  }
  const adoption = evaluateSafeOutputDelivery(data.type, observedTimestamp(timestamp));
  return {
    ...base,
    adoptionState: adoption.state,
    adoptionSignal: adoption.signal,
    adoptionSource: adoption.source,
    ...(adoption.acceptanceKind ? { acceptanceKind: adoption.acceptanceKind } : {}),
    ...(adoption.adoptionAt ? { adoptionAt: adoption.adoptionAt } : {}),
  };
}

function safeOutputAdoptionRules() {
  return [...RULES.values()].map(rule => ({ ...rule }));
}

module.exports = {
  ADOPTION_REGISTRY_VERSION: registry.version,
  evaluateSafeOutputDelivery,
  projectSafeOutputLifecycle,
  safeOutputAdoptionRule,
  safeOutputAdoptionRules,
};
