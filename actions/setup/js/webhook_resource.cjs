// @ts-check

const { ERR_VALIDATION } = require("./error_codes.cjs");
const { isGatewayCommand, resolveGatewayCommandResource } = require("./gateway_command_webhook.cjs");
const { GITHUB_NOTIFICATION_EVENTS, isGitHubNotification, resolveGitHubNotificationResource } = require("./github_notification_webhook.cjs");

const WEBHOOK_PAYLOAD_POLICY = "resource-url-types-filters/v2";
const ISSUE_TYPES = ["opened", "edited", "closed", "reopened", "labeled", "unlabeled", "deleted"];
const WEBHOOK_TYPES = {
  projects_v2_item: ["archived", "converted", "created", "deleted", "edited", "reordered", "restored"],
  code_scanning_alert: ["appeared_in_branch", "closed_by_user", "created", "fixed", "reopened", "reopened_by_user", "updated_assignment"],
  dependabot_alert: ["assignees_changed", "auto_dismissed", "auto_reopened", "created", "dismissed", "fixed", "reintroduced", "reopened", "withdrawn"],
  linear_issue: ISSUE_TYPES,
  azure_devops_issue: [...ISSUE_TYPES, "restored"],
  slash_command: ["opened", "edited", "reopened", "created"],
  label_command: ["labeled"],
  ...Object.fromEntries(Object.entries(GITHUB_NOTIFICATION_EVENTS).map(([event, definition]) => [event, definition.actions])),
};

/** @returns {never} */
function reject(message) {
  throw new Error(`${ERR_VALIDATION}: Webhook validation failed: ${message}`);
}

function isWebhookEvent(event) {
  return typeof event === "string" && Object.hasOwn(WEBHOOK_TYPES, event);
}

function isWebhookFilterName(name) {
  return typeof name === "string" && name.split(".").every(key => /^(?:[A-Za-z_][A-Za-z0-9_-]*|\*)$/.test(key) && !["__proto__", "prototype", "constructor"].includes(key));
}

function isWebhookFilterScalar(value) {
  return typeof value === "string" || typeof value === "boolean" || Number.isSafeInteger(value);
}

function validWebhookFilterValues(values) {
  return Array.isArray(values) && values.length > 0 && values.every(value => isWebhookFilterScalar(value) && typeof value === typeof values[0]) && new Set(values).size === values.length;
}

function resourceURL(value, message, allowFragment = false) {
  let url;
  try {
    url = new URL(value);
  } catch {
    reject(message);
  }
  if (url.protocol !== "https:" || url.username || url.password || url.href.includes("?") || url.href.endsWith("#") || (!allowFragment && url.hash) || url.href !== value) {
    reject(message);
  }
  return url;
}

function resolveGitHubResource(event, url, env) {
  let base;
  try {
    base = new URL(env.GITHUB_API_URL || "https://api.github.com");
  } catch {
    reject("GITHUB_API_URL must be a valid URL");
  }
  const prefix = base.pathname.replace(/\/$/, "");
  const message = event === "projects_v2_item" ? "Projects payload requires a canonical GitHub API project item URL" : "security alert payload requires a canonical GitHub API alert URL";
  if (url.origin !== base.origin || !url.pathname.startsWith(`${prefix}/`)) reject(message);
  const path = url.pathname.slice(prefix.length);
  if (event === "projects_v2_item") {
    const match = path.match(/^\/(orgs|users)\/([a-zA-Z0-9_-]+)\/projectsV2\/([1-9][0-9]*)\/items\/([1-9][0-9]*)$/);
    if (!match) reject(message);
    return { item_type: event, item_number: match[4], repo: "", source: `${base.origin}${prefix}/${match[1]}/${match[2].toLowerCase()}/projectsV2/${match[3]}`, url: url.href };
  }
  const match = path.match(/^\/repos\/([a-zA-Z0-9_.-]+)\/([a-zA-Z0-9_.-]+)\/(code-scanning|dependabot)\/alerts\/([1-9][0-9]*)$/);
  if (!match || match[3] !== (event === "code_scanning_alert" ? "code-scanning" : "dependabot")) reject(message);
  return { item_type: event, item_number: match[4], repo: `${match[1]}/${match[2]}`, url: url.href };
}

function resolveProviderResource(event, url) {
  if (event === "linear_issue") {
    const match = url.pathname.match(/^\/([a-zA-Z0-9_-]+)\/issue\/([A-Z][A-Z0-9_-]*-[1-9][0-9]*)(?:\/(?:[a-zA-Z0-9_-]|%[0-9a-fA-F]{2})+)?$/);
    if (url.origin !== "https://linear.app" || !match || match[1] === "issue") {
      reject("Linear payload requires a canonical workspace-qualified Linear issue URL");
    }
    return { item_type: event, item_number: match[2], repo: "", source: `${url.origin}/${match[1]}`, url: url.href };
  }
  // Work item IDs are collection-scoped, not update/revision IDs. The optional
  // project segment does not change that identity.
  const match = url.pathname.match(/^\/([a-zA-Z0-9_-]+)(?:\/(?:[a-zA-Z0-9_.-]|%[0-9a-fA-F]{2})+)?\/_apis\/wit\/work[Ii]tems\/([1-9][0-9]*)$/);
  if (!match || (url.hostname !== "dev.azure.com" && !/^[a-zA-Z0-9-]+\.visualstudio\.com$/.test(url.hostname))) {
    reject("Azure DevOps payload requires a canonical collection work item API URL");
  }
  return { item_type: event, item_number: match[2], repo: "", source: `${url.origin}/${match[1]}`, url: url.href };
}

function resolveWebhookResource(event, payload, env = process.env) {
  if (Object.keys(payload).some(key => !["url", "type", "filters"].includes(key))) {
    reject("webhook payload accepts only a canonical resource URL, type array, and declared filter values; fetch resource details with an authenticated provider client");
  }
  if (
    Object.hasOwn(payload, "filters") &&
    (payload.filters === null ||
      typeof payload.filters !== "object" ||
      Array.isArray(payload.filters) ||
      Object.keys(payload.filters).length === 0 ||
      !Object.entries(payload.filters).every(([name, values]) => isWebhookFilterName(name) && validWebhookFilterValues(values)))
  ) {
    reject("webhook payload filters must contain only named, distinct homogeneous scalar value arrays");
  }
  const supported = isWebhookEvent(event) ? WEBHOOK_TYPES[event] : undefined;
  if (!supported || !Array.isArray(payload.type) || payload.type.length === 0 || payload.type.some(type => !supported.includes(type)) || new Set(payload.type).size !== payload.type.length) {
    reject("webhook payload type must be a non-empty array of distinct supported event activities");
  }
  if (!event.endsWith("_issue") && payload.type.length !== 1) {
    reject("GitHub webhook payload type must contain exactly one event activity");
  }
  const message =
    event === "projects_v2_item"
      ? "Projects payload requires a canonical GitHub API project item URL"
      : event.endsWith("_alert")
        ? "security alert payload requires a canonical GitHub API alert URL"
        : "provider payload requires a canonical HTTPS resource URL";
  const url = resourceURL(payload.url, message, isGatewayCommand(event) || isGitHubNotification(event));
  if (isGatewayCommand(event)) return resolveGatewayCommandResource(event, url, payload, env);
  if (isGitHubNotification(event)) return resolveGitHubNotificationResource(event, url, env);
  return event.endsWith("_issue") ? resolveProviderResource(event, url) : resolveGitHubResource(event, url, env);
}

module.exports = { WEBHOOK_PAYLOAD_POLICY, WEBHOOK_TYPES, isWebhookEvent, isWebhookFilterName, validWebhookFilterValues, resolveWebhookResource };
