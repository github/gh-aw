// @ts-check
/// <reference types="@actions/github-script" />

const fs = require("fs");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");
const { normalizeWebhookContext, WEBHOOK_SCHEMA_VERSION } = require("./webhook_context.cjs");
const { WEBHOOK_PAYLOAD_POLICY, isWebhookEvent, isWebhookFilterName, validWebhookFilterValues } = require("./webhook_resource.cjs");
const { isProviderIssueEvent, validProviderIssueSubscription, matchesProviderIssueSubscription } = require("./provider_issue_webhook.cjs");
const { isGatewayCommand, validGatewayCommandSubscription, matchesGatewayCommandSubscription } = require("./gateway_command_webhook.cjs");
const { isGitHubNotification, validGitHubNotificationSubscription } = require("./github_notification_webhook.cjs");

const MAX_CLIENT_PAYLOAD_BYTES = 65535;
const MAX_DISPATCH_REQUEST_BYTES = 65535;
const MAX_CLIENT_PAYLOAD_KEYS = 10;
const MAX_EVENT_TYPE_LENGTH = 100;

function isObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

/** @returns {value is string} */
function isNonemptyString(value) {
  return typeof value === "string" && value.trim().length > 0;
}

/** @returns {never} */
function reject(message) {
  throw new Error(`${ERR_VALIDATION}: Webhook validation failed: ${message}`);
}

function parseConfiguration(value, name) {
  try {
    return JSON.parse(value || "");
  } catch {
    return reject(`${name} must be valid compiler-generated JSON`);
  }
}

function eventName(env) {
  return env.GITHUB_EVENT_NAME ?? env.GH_AW_WEBHOOK_EVENT_NAME;
}

function hasValidRepositorySelectors(subscription) {
  if (subscription.repos === undefined) return true;
  return (
    (isGitHubNotification(subscription.event) || ["code_scanning_alert", "dependabot_alert", "slash_command", "label_command"].includes(subscription.event)) &&
    Array.isArray(subscription.repos) &&
    subscription.repos.length > 0 &&
    subscription.repos.every(repo => typeof repo === "string" && repo.trim() === repo && /^[a-zA-Z0-9_-]+\/([a-zA-Z0-9._-]+|\*)$/.test(repo) && !/\/\.{1,2}$/.test(repo))
  );
}

function matchesRepositorySelector(repo, selectors) {
  const source = repo.toLowerCase();
  return selectors.some(selector => {
    const pattern = selector.toLowerCase();
    return pattern.endsWith("/*") ? source.startsWith(pattern.slice(0, -1)) : source === pattern;
  });
}

function hasValidOrganizationSelectors(subscription) {
  return (
    subscription.orgs === undefined ||
    ((isGitHubNotification(subscription.event) || ["projects_v2_item", "code_scanning_alert", "dependabot_alert", "slash_command", "label_command"].includes(subscription.event)) &&
      Array.isArray(subscription.orgs) &&
      subscription.orgs.length > 0 &&
      subscription.orgs.every(org => typeof org === "string" && /^[a-zA-Z0-9*]([a-zA-Z0-9*-]{0,37}[a-zA-Z0-9*])?$/.test(org) && !/\s/.test(org)))
  );
}

function matchesOrganizationSelector(item, selectors) {
  const organization = item.repo ? item.repo.split("/")[0].toLowerCase() : item.source?.match(/\/orgs\/([^/]+)\/projectsV2\/[1-9][0-9]*$/)?.[1]?.toLowerCase();
  if (!organization) return false;
  return selectors.some(selector => {
    const pattern = selector.toLowerCase();
    if (!pattern.includes("*")) return organization === pattern;
    const parts = pattern.split("*");
    const prefix = parts[0];
    const suffix = parts.at(-1);
    if (!organization.startsWith(prefix) || !organization.endsWith(suffix)) return false;
    let offset = prefix.length;
    for (const part of parts.slice(1, -1)) {
      const found = organization.indexOf(part, offset);
      if (found === -1) return false;
      offset = found + part.length;
    }
    return offset <= organization.length - suffix.length;
  });
}

function validFilterCondition(condition) {
  return (
    isObject(condition) &&
    isWebhookFilterName(condition.filter) &&
    Array.isArray(condition.path) &&
    condition.path.length > 0 &&
    condition.path.every(isNonemptyString) &&
    condition.operator === "equals" &&
    validWebhookFilterValues(condition.values) &&
    condition.values.every(value => (condition.value_type === "integer" ? Number.isSafeInteger(value) : ["string", "boolean"].includes(condition.value_type) && typeof value === condition.value_type))
  );
}

function validSubscriptionFilters(subscription) {
  return (
    Array.isArray(subscription.conditions) &&
    subscription.conditions.every(validFilterCondition) &&
    (subscription.any_of === undefined || (Array.isArray(subscription.any_of) && subscription.any_of.length > 0 && subscription.any_of.every(group => Array.isArray(group) && group.length > 0 && group.every(validFilterCondition))))
  );
}

function matchesProjectedFilters(subscription, payload) {
  if (isGatewayCommand(subscription.event)) return matchesGatewayCommandSubscription(subscription, payload);
  const projected = payload.filters ?? {};
  const groups = subscription.any_of ?? [[]];
  return groups.some(group => {
    const conditions = [...subscription.conditions, ...group];
    const names = new Set(conditions.map(condition => condition.filter));
    return (
      Object.keys(projected).length === names.size &&
      Object.keys(projected).every(name => names.has(name)) &&
      conditions.every(condition => Object.hasOwn(projected, condition.filter) && projected[condition.filter].every(value => condition.values.includes(value)))
    );
  });
}

/**
 * Validate the dispatch transport without replacing the GitHub authorization
 * context. Same-kind deliveries are carried in client_payload.aw_context.webhook.
 * The gateway forwards only resource URLs, event types, and matched filter values.
 * The compiler-owned gateway_provenance job authenticates the CAO App sender.
 * The receiver evaluates original source predicates; the runner bounds the
 * projected filter keys and values against the compiled conditions.
 *
 * @param {any} event - Actual repository_dispatch event, never the inner webhook
 * @param {NodeJS.ProcessEnv} [env]
 * @returns {{skipped: boolean, event?: string, delivery_ids?: string[]}}
 */
function validateWebhook(event, env = process.env) {
  if (eventName(env) !== "repository_dispatch") {
    return { skipped: true };
  }

  const subscriptions = parseConfiguration(env.GH_AW_WEBHOOK_SUBSCRIPTIONS, "GH_AW_WEBHOOK_SUBSCRIPTIONS");
  if (
    !Array.isArray(subscriptions) ||
    subscriptions.length === 0 ||
    !subscriptions.every(
      subscription =>
        isObject(subscription) &&
        isWebhookEvent(subscription.event) &&
        isObject(subscription.dispatch) &&
        isNonemptyString(subscription.dispatch.event_type) &&
        subscription.dispatch.event_type.length <= MAX_EVENT_TYPE_LENGTH &&
        subscription.schema_version === WEBHOOK_SCHEMA_VERSION &&
        isObject(subscription.dispatch.payload_projection) &&
        Object.keys(subscription.dispatch.payload_projection).length === 1 &&
        subscription.dispatch.payload_projection.policy === WEBHOOK_PAYLOAD_POLICY &&
        validSubscriptionFilters(subscription) &&
        Array.isArray(subscription.types) &&
        subscription.types.length > 0 &&
        subscription.types.every(isNonemptyString) &&
        hasValidRepositorySelectors(subscription) &&
        hasValidOrganizationSelectors(subscription) &&
        (!isProviderIssueEvent(subscription.event) || validProviderIssueSubscription(subscription)) &&
        (!isGitHubNotification(subscription.event) || validGitHubNotificationSubscription(subscription)) &&
        (isGatewayCommand(subscription.event) ? validGatewayCommandSubscription(subscription) : subscription.command_filter === undefined)
    )
  ) {
    reject("GH_AW_WEBHOOK_SUBSCRIPTIONS must contain valid version 2 batch subscriptions with resource-url-types-filters/v2 projection");
  }

  if (!isObject(event) || !isNonemptyString(event.action) || event.action.length > MAX_EVENT_TYPE_LENGTH) {
    reject("repository_dispatch action must be a non-empty event_type of at most 100 characters");
  }
  const matching = subscriptions.filter(subscription => subscription.dispatch.event_type === event.action);
  if (matching.length === 0) {
    return { skipped: true };
  }
  const envelope = event.client_payload;
  if (!isObject(envelope)) {
    reject("client_payload must be an object");
  }
  if (Object.keys(envelope).length > MAX_CLIENT_PAYLOAD_KEYS) {
    reject("client_payload must have at most 10 root keys");
  }
  if (Buffer.byteLength(JSON.stringify(envelope), "utf8") > MAX_CLIENT_PAYLOAD_BYTES) {
    reject("client_payload exceeds the 65535-byte inline payload limit; oversized payloads are not fetched");
  }
  if (Buffer.byteLength(JSON.stringify({ event_type: event.action, client_payload: envelope }), "utf8") > MAX_DISPATCH_REQUEST_BYTES) {
    reject("repository_dispatch request must be less than 65536 bytes including event_type and client_payload");
  }
  if (envelope.schema_version !== WEBHOOK_SCHEMA_VERSION) {
    reject("client_payload.schema_version must match subscription version 2");
  }
  if (Object.keys(envelope).some(key => !["schema_version", "aw_context"].includes(key))) {
    reject("client_payload accepts only schema_version and aw_context; singleton webhook envelopes are unsupported");
  }
  if (isObject(envelope.aw_context) && Object.keys(envelope.aw_context).some(key => key !== "webhook")) {
    reject("gateway aw_context accepts only webhook; resource details and derived items must not be supplied");
  }
  const normalized = normalizeWebhookContext(envelope.aw_context, env);
  if (!normalized) {
    reject("client_payload.aw_context.webhook is required");
  }
  const eventSubscriptions = matching.filter(subscription => subscription.event === normalized.webhook.event);
  if (eventSubscriptions.length !== 1) {
    reject("aw_context.webhook.event must match the webhook subscription");
  }
  const subscription = eventSubscriptions[0];
  if (subscription.repos !== undefined && normalized.items.some(item => !matchesRepositorySelector(item.repo, subscription.repos))) {
    reject("webhook source repository does not match the subscription repos");
  }
  if (subscription.orgs !== undefined && normalized.items.some(item => !matchesOrganizationSelector(item, subscription.orgs))) {
    reject("webhook source organization does not match the subscription orgs");
  }
  for (const delivery of normalized.webhook.deliveries) {
    if (!matchesProjectedFilters(subscription, delivery.payload)) {
      if (isGatewayCommand(subscription.event)) reject("gateway command filters must contain exactly one declared matching source event and command/label value");
      reject("webhook payload filters must contain exactly the declared matching values for all shared conditions and one complete alternative");
    }
    if (isProviderIssueEvent(subscription.event)) {
      if (!matchesProviderIssueSubscription(subscription, delivery.payload)) {
        reject("provider issue activity does not match the subscription types or native events");
      }
    } else if (!delivery.payload.type.every(type => subscription.types.includes(type))) {
      reject("webhook payload type does not match the subscription types");
    }
  }
  return { skipped: false, event: normalized.webhook.event, delivery_ids: normalized.webhook.deliveries.map(delivery => delivery.delivery_id) };
}

/**
 * Read the runner-owned event file, rather than interpolating webhook data into
 * shell commands or trusting a supplied payload.sender.
 */
async function main() {
  if (eventName(process.env) !== "repository_dispatch") {
    return { skipped: true };
  }
  let event;
  try {
    if (!process.env.GITHUB_EVENT_PATH) {
      reject("GITHUB_EVENT_PATH is required for repository_dispatch");
    }
    event = JSON.parse(fs.readFileSync(process.env.GITHUB_EVENT_PATH, "utf8"));
  } catch {
    reject("GITHUB_EVENT_PATH must contain a readable JSON event");
  }
  const result = validateWebhook(event);
  if (!result.skipped && typeof core !== "undefined") {
    core.info(`Webhook batch validated (${result.delivery_ids?.length} unique deliveries).`);
  }
  return result;
}

module.exports = { main, validateWebhook, MAX_CLIENT_PAYLOAD_BYTES, MAX_DISPATCH_REQUEST_BYTES, MAX_CLIENT_PAYLOAD_KEYS, MAX_EVENT_TYPE_LENGTH };

if (require.main === module) {
  main().catch(error => {
    console.error(getErrorMessage(error));
    process.exitCode = 1;
  });
}
