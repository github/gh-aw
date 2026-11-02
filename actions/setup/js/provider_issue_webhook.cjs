// @ts-check

const { ERR_VALIDATION } = require("./error_codes.cjs");

const PROVIDER_ISSUE_EVENTS = {
  linear_issue: ["create", "update", "remove"],
  azure_devops_issue: ["workitem.created", "workitem.updated", "workitem.deleted", "workitem.restored"],
};
const PROVIDER_ISSUE_ACTIVITIES = ["opened", "edited", "closed", "reopened", "labeled", "unlabeled", "deleted"];

function isObject(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function isString(value) {
  return typeof value === "string" && value.trim().length > 0;
}

function isStringArray(value) {
  return Array.isArray(value) && value.every(isString);
}

function isTimestamp(value) {
  return isString(value) && /^\d{4}-\d{2}-\d{2}T/.test(value) && Number.isFinite(Date.parse(value));
}

/** @returns {never} */
function reject(message) {
  throw new Error(`${ERR_VALIDATION}: Webhook validation failed: ${message}`);
}

function isProviderIssueEvent(event) {
  return Object.hasOwn(PROVIDER_ISSUE_EVENTS, event);
}

function nativeIssueEvent(event, payload) {
  const native = event === "linear_issue" ? payload.action : payload.eventType;
  if (!PROVIDER_ISSUE_EVENTS[event]?.includes(native) || (event === "linear_issue" && payload.type !== "Issue")) {
    reject("provider issue payload must contain a supported native issue event");
  }
  return native;
}

// Receiver-side evidence only; runner context accepts minimal notifications.
function resolveProviderIssueItem(event, payload) {
  const native = nativeIssueEvent(event, payload);
  if (event === "linear_issue") {
    if (!isObject(payload.data) || !isString(payload.data.id) || !isString(payload.organizationId)) {
      reject("Linear issue payload requires data.id and organizationId");
    }
    return { item_type: event, item_number: payload.data.id, repo: "", source: payload.organizationId };
  }
  const resource = payload.resource;
  const snapshot = native !== "workitem.updated" || (payload.resourceVersion === "1.0" && !resource?.revision);
  const id = resource?.workItemId ?? (snapshot ? resource?.id : undefined);
  const collection = payload.resourceContainers?.collection?.id;
  if (!isObject(resource) || !Number.isSafeInteger(id) || id <= 0 || !isString(collection)) {
    reject("Azure DevOps issue payload requires a positive work item ID and collection identity");
  }
  return { item_type: event, item_number: String(id), repo: "", source: collection };
}

function addLabelActivities(activities, before, after) {
  if (!isStringArray(before) || !isStringArray(after)) return;
  if (after.some(label => !before.includes(label))) activities.push("labeled");
  if (before.some(label => !after.includes(label))) activities.push("unlabeled");
}

function addClosedActivities(activities, before, after, closedStates) {
  if (!isString(before) || !isString(after)) return;
  if (!closedStates.includes(before) && closedStates.includes(after)) activities.push("closed");
  if (closedStates.includes(before) && !closedStates.includes(after)) activities.push("reopened");
}

function linearActivities(payload, native) {
  if (native === "create") return ["opened"];
  if (native === "remove") return ["deleted"];
  const activities = ["edited"];
  if (!isObject(payload.updatedFrom)) return activities;
  const before = payload.updatedFrom;
  const after = payload.data;
  if (["completedAt", "canceledAt"].some(field => Object.hasOwn(before, field))) {
    const terminal = value => {
      if (isTimestamp(value.completedAt) || isTimestamp(value.canceledAt)) return true;
      if (value.completedAt === null && value.canceledAt === null) return false;
      return undefined;
    };
    const wasClosed = terminal({ ...after, ...before });
    const isClosed = terminal(after);
    if (wasClosed === false && isClosed === true) activities.push("closed");
    if (wasClosed === true && isClosed === false) activities.push("reopened");
  }
  if (Object.hasOwn(before, "labelIds")) addLabelActivities(activities, before.labelIds, after.labelIds);
  return activities;
}

function azureActivities(payload, native, closedStates) {
  if (native === "workitem.created") return ["opened"];
  if (native === "workitem.deleted") return ["deleted"];
  if (native === "workitem.restored") return ["restored"];
  const activities = ["edited"];
  const fields = payload.resource?.fields;
  if (!isObject(fields)) return activities;
  const state = fields["System.State"];
  if (isObject(state)) addClosedActivities(activities, state.oldValue, state.newValue, closedStates);
  const tags = fields["System.Tags"];
  if (isObject(tags) && typeof tags.oldValue === "string" && typeof tags.newValue === "string") {
    const split = value =>
      value
        .split(";")
        .map(tag => tag.trim())
        .filter(Boolean);
    addLabelActivities(activities, split(tags.oldValue), split(tags.newValue));
  }
  return activities;
}

function providerIssueActivities(event, payload, normalization) {
  const native = nativeIssueEvent(event, payload);
  if (event === "linear_issue") return linearActivities(payload, native);
  return azureActivities(payload, native, normalization?.closed_states ?? []);
}

function validProviderIssueSubscription(subscription) {
  const provider = subscription.event.replace(/_issue$/, "").replaceAll("_", "-");
  const normalization = subscription.activity_normalization;
  return (
    isProviderIssueEvent(subscription.event) &&
    subscription.provider === provider &&
    subscription.types.length > 0 &&
    subscription.types.every(type => PROVIDER_ISSUE_ACTIVITIES.includes(type) || (subscription.event === "azure_devops_issue" && type === "restored")) &&
    isObject(normalization) &&
    normalization.policy === `${provider}-issue/v1` &&
    Array.isArray(subscription.native_events) &&
    subscription.native_events.length > 0 &&
    subscription.native_events.every(event => PROVIDER_ISSUE_EVENTS[subscription.event].includes(event)) &&
    (normalization.closed_states === undefined || (subscription.event === "azure_devops_issue" && isStringArray(normalization.closed_states) && normalization.closed_states.length > 0)) &&
    (!subscription.types.some(type => ["closed", "reopened"].includes(type)) || subscription.event !== "azure_devops_issue" || normalization.closed_states?.length > 0)
  );
}

function matchesProviderIssueSubscription(subscription, payload) {
  const [created, updated, deleted, restored] = PROVIDER_ISSUE_EVENTS[subscription.event];
  if (payload.type.includes("closed") && payload.type.includes("reopened")) return false;
  return payload.type.every(activity => {
    const native = activity === "opened" ? created : activity === "deleted" ? deleted : activity === "restored" ? restored : updated;
    return subscription.native_events.includes(native) && subscription.types.includes(activity);
  });
}

module.exports = { isProviderIssueEvent, resolveProviderIssueItem, providerIssueActivities, validProviderIssueSubscription, matchesProviderIssueSubscription };
