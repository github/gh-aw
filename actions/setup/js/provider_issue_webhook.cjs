// @ts-check

const { ERR_VALIDATION } = require("./error_codes.cjs");

const PROVIDER_ISSUE_EVENTS = {
  jira_issue: ["jira:issue_created", "jira:issue_updated", "jira:issue_deleted"],
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

/** @returns {never} */
function reject(message) {
  throw new Error(`${ERR_VALIDATION}: Webhook validation failed: ${message}`);
}

function sourceURL(value) {
  let url;
  try {
    url = new URL(value);
  } catch {
    reject("provider issue must contain a valid HTTPS resource URL");
  }
  if (url.protocol !== "https:" || url.username || url.password || url.search || url.hash) {
    reject("provider issue must contain a valid HTTPS resource URL");
  }
  return url;
}

function isProviderIssueEvent(event) {
  return Object.hasOwn(PROVIDER_ISSUE_EVENTS, event);
}

function nativeIssueEvent(event, payload) {
  const native = event === "jira_issue" ? payload.webhookEvent : payload.eventType;
  if (!PROVIDER_ISSUE_EVENTS[event]?.includes(native)) {
    reject("provider issue payload must contain a supported native issue event");
  }
  return native;
}

// Receiver-side evidence only; runner context accepts minimal notifications.
function resolveProviderIssueItem(event, payload) {
  const native = nativeIssueEvent(event, payload);
  if (event === "jira_issue") {
    const issue = payload.issue;
    if (!isObject(issue) || !isString(issue.id) || !/^[1-9][0-9]*$/.test(issue.id)) {
      reject("Jira issue payload requires a positive decimal issue.id");
    }
    const url = sourceURL(issue.self);
    if (!/\/rest\/api\/[23]\/issue\/[^/]+$/.test(url.pathname)) {
      reject("Jira issue.self must identify a Jira REST issue");
    }
    return { item_type: event, item_number: issue.id, repo: "", source: url.origin, url: url.href };
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

function jiraActivities(payload, native) {
  if (native === "jira:issue_created") return ["opened"];
  if (native === "jira:issue_deleted") return ["deleted"];
  const activities = ["edited"];
  const changes = payload.changelog?.items;
  if (!Array.isArray(changes)) return activities;
  for (const change of changes) {
    if (!isObject(change) || change.fieldtype !== "jira") continue;
    if (change.field === "resolution" && Object.hasOwn(change, "from") && Object.hasOwn(change, "to")) {
      if (change.from === null && isString(change.to)) activities.push("closed");
      if (isString(change.from) && change.to === null) activities.push("reopened");
    }
    // Only unambiguous single-label transitions are declared. Jira does not
    // document a lossless serialization for multi-label changelog strings.
    if (change.field === "labels" && isStringArray(payload.issue?.fields?.labels)) {
      const current = payload.issue.fields.labels;
      if (change.fromString === null && isString(change.toString) && current.length === 1 && current[0] === change.toString) activities.push("labeled");
      if (isString(change.fromString) && change.toString === null && current.length === 0) activities.push("unlabeled");
    }
  }
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
  if (event === "jira_issue") return jiraActivities(payload, native);
  return azureActivities(payload, native, normalization?.closed_states ?? []);
}

function validProviderIssueSubscription(subscription) {
  const provider = subscription.event.replace(/_issue$/, "").replaceAll("_", "-");
  const normalization = subscription.activity_normalization;
  return (
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
