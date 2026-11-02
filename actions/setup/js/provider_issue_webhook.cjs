// @ts-check

const { ERR_VALIDATION } = require("./error_codes.cjs");

const PROVIDER_ISSUE_EVENTS = {
  jira_issue: ["jira:issue_created", "jira:issue_updated", "jira:issue_deleted"],
  linear_issue: ["create", "update", "remove"],
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
  const native = event === "jira_issue" ? payload.webhookEvent : payload.action;
  if (!PROVIDER_ISSUE_EVENTS[event]?.includes(native) || (event === "linear_issue" && payload.type !== "Issue")) {
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
  if (event === "linear_issue") {
    if (!isObject(payload.data) || !isString(payload.data.id) || !isString(payload.organizationId)) {
      reject("Linear issue payload requires data.id and organizationId");
    }
    return { item_type: event, item_number: payload.data.id, repo: "", source: payload.organizationId };
  }
  reject("unsupported provider issue event");
}

function addLabelActivities(activities, before, after) {
  if (!isStringArray(before) || !isStringArray(after)) return;
  if (after.some(label => !before.includes(label))) activities.push("labeled");
  if (before.some(label => !after.includes(label))) activities.push("unlabeled");
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

function providerIssueActivities(event, payload) {
  const native = nativeIssueEvent(event, payload);
  if (event === "jira_issue") return jiraActivities(payload, native);
  if (event === "linear_issue") return linearActivities(payload, native);
  reject("unsupported provider issue event");
}

function validProviderIssueSubscription(subscription) {
  const provider = subscription.event.replace(/_issue$/, "").replaceAll("_", "-");
  const normalization = subscription.activity_normalization;
  return (
    subscription.provider === provider &&
    subscription.types.length > 0 &&
    subscription.types.every(type => PROVIDER_ISSUE_ACTIVITIES.includes(type)) &&
    isObject(normalization) &&
    normalization.policy === `${provider}-issue/v1` &&
    Array.isArray(subscription.native_events) &&
    subscription.native_events.length > 0 &&
    subscription.native_events.every(event => PROVIDER_ISSUE_EVENTS[subscription.event]?.includes(event)) &&
    normalization.closed_states === undefined
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
