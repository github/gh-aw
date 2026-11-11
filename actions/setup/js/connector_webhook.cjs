// @ts-check

const { createHash } = require("node:crypto");
const { EVENT_EVALUATION_VERSION, validEventPredicate, validEventFilters, eventPredicateConfigured, validEventURLBinding } = require("./event_predicate.cjs");

const connectorEvent = /^connector_[0-9a-f]{24}$/;
const name = /^[a-z][a-z0-9-]{0,62}$/;
const profile = /^[A-Za-z0-9][A-Za-z0-9./_-]{0,255}$/;
const object = value => value !== null && typeof value === "object" && !Array.isArray(value);

function isConnectorWebhookEvent(event) {
  return typeof event === "string" && connectorEvent.test(event);
}

function validConnectorWebhookSubscription(subscription) {
  const source = subscription.connector;
  const fields = ["id", "alias", "trigger", "digest", "verification_profile", "native_events", "normalize", "types", "origins", "evaluation_version", "when", "filters", "variants", "url_identity", "url_path_prefix"];
  const rich = source?.when !== undefined || source?.filters !== undefined || source?.variants !== undefined || source?.url_identity !== undefined || source?.url_path_prefix !== undefined;
  const variants = source?.variants;
  if (
    !object(source) ||
    Object.keys(source).some(key => !fields.includes(key)) ||
    !profile.test(source.id || "") ||
    !name.test(source.alias || "") ||
    !name.test(source.trigger || "") ||
    !/^[0-9a-f]{64}$/.test(source.digest || "") ||
    !profile.test(source.verification_profile || "") ||
    !Array.isArray(source.native_events) ||
    source.native_events.length === 0 ||
    !source.native_events.every(value => typeof value === "string" && value.length > 0) ||
    !object(source.normalize) ||
    Object.keys(source.normalize).length === 0 ||
    !Object.entries(source.normalize).every(([key, value]) => name.test(key) && typeof value === "string" && value.startsWith("/")) ||
    !Array.isArray(source.origins) ||
    source.origins.length === 0 ||
    source.origins.length > 16 ||
    !source.origins.every(validOrigin) ||
    !Array.isArray(subscription.types) ||
    subscription.types.length !== 1 ||
    subscription.types[0] !== source.trigger ||
    !Array.isArray(subscription.conditions) ||
    (rich && source.evaluation_version !== EVENT_EVALUATION_VERSION) ||
    (!rich && source.evaluation_version !== undefined) ||
    (source.when !== undefined && (!validEventPredicate(source.when) || !eventPredicateConfigured(source.when))) ||
    !validEventURLBinding(source.url_identity, source.url_path_prefix, source.normalize) ||
    (source.filters !== undefined && !validEventFilters(source.filters)) ||
    (source.filters !== undefined && !matchesFilterDefinitions(source.filters, subscription.conditions)) ||
    (variants !== undefined && (!Array.isArray(variants) || variants.length === 0 || variants.length > 4 || !variants.every(validVariant))) ||
    !(
      subscription.conditions.some(condition => /^connector_scope_[0-9]+$/.test(condition.filter || "")) ||
      (variants?.length && subscription.any_of?.length === variants.length && subscription.any_of.every(group => group.some(condition => /^connector_scope_[0-9]+$/.test(condition.filter || ""))))
    ) ||
    (subscription.any_of !== undefined && !variants?.length) ||
    subscription.provider !== undefined ||
    subscription.command_filter !== undefined
  )
    return false;
  const digest = createHash("sha256")
    .update(source.alias + "\0" + source.trigger + "\0" + source.digest)
    .digest("hex")
    .slice(0, 24);
  return subscription.event === "connector_" + digest;
}

function matchesFilterDefinitions(filters, conditions) {
  for (const [name, filter] of Object.entries(filters)) {
    if (!filter.values?.length) continue;
    const condition = conditions.find(condition => condition.filter === "connector_filter_" + name.replaceAll("-", "_"));
    const path = filter.path
      .slice(1)
      .split("/")
      .map(part => part.replaceAll("~1", "/").replaceAll("~0", "~"));
    if (!condition || condition.value_type !== filter.type || JSON.stringify(condition.path) !== JSON.stringify(path) || JSON.stringify(condition.values) !== JSON.stringify(filter.values)) return false;
  }
  return true;
}

function validVariant(variant) {
  return (
    object(variant) &&
    Object.keys(variant).every(key => ["when", "normalize", "resources", "types"].includes(key)) &&
    validEventPredicate(variant.when) &&
    eventPredicateConfigured(variant.when) &&
    object(variant.normalize) &&
    Object.keys(variant.normalize).length > 0 &&
    Object.entries(variant.normalize).every(([key, value]) => name.test(key) && typeof value === "string" && value.startsWith("/")) &&
    object(variant.resources) &&
    Object.keys(variant.resources).length > 0 &&
    Object.values(variant.resources).every(value => name.test(value))
  );
}

function validOrigin(value) {
  try {
    const url = new URL(value);
    return url.protocol === "https:" && !url.username && !url.password && url.origin === value && !value.includes("*");
  } catch {
    return false;
  }
}

function matchesConnectorWebhookSubscription(subscription, payload) {
  if (!validConnectorWebhookSubscription(subscription) || payload.type?.length !== 1 || payload.type[0] !== subscription.connector.trigger) return false;
  let url;
  try {
    url = new URL(payload.url);
  } catch {
    return false;
  }
  if (!subscription.connector.origins.includes(url.origin)) return false;
  return object(payload.filters) && Object.values(payload.filters).every(values => Array.isArray(values) && values.length === 1);
}

// This identity deduplicates references only; it is never a provider ID or grant.
function resolveConnectorWebhookResource(event, url) {
  return { item_type: event, item_number: "uri:" + createHash("sha256").update(url.href).digest("hex"), repo: "", source: url.origin, url: url.href };
}

module.exports = { isConnectorWebhookEvent, validConnectorWebhookSubscription, matchesConnectorWebhookSubscription, resolveConnectorWebhookResource };
