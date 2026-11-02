import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";

const require = createRequire(import.meta.url);
const { providerIssueActivities, resolveProviderIssueItem, validProviderIssueSubscription } = require("./provider_issue_webhook.cjs");
const { normalizeWebhookContext, redactWebhookPayloads } = require("./webhook_context.cjs");
const { validateWebhook } = require("./validate_webhook.cjs");

function jira(native = "jira:issue_updated", changes = []) {
  return {
    webhookEvent: native,
    issue: { id: "123", self: "https://example.atlassian.net/rest/api/2/issue/123", fields: { labels: ["bug"] } },
    changelog: { items: changes.map(change => ({ fieldtype: "jira", ...change })) },
  };
}

function azure(eventType = "workitem.updated", fields = {}) {
  return { eventType, resource: { id: 5, workItemId: 123, fields }, resourceContainers: { collection: { id: "collection-a" } } };
}

describe("provider issue native identity", () => {
  it.each([
    ["jira_issue", jira(), "123", "https://example.atlassian.net"],
    ["azure_devops_issue", azure(), "123", "collection-a"],
  ])("uses source-scoped identities, not GitHub repositories: %s", (event, payload, number, source) => {
    expect(resolveProviderIssueItem(event, payload)).toMatchObject({ item_type: event, item_number: number, source, repo: "" });
  });

  it.each([
    ["jira_issue", { ...jira(), webhookEvent: "comment_created" }],
    ["jira_issue", { ...jira(), issue: { id: 123 } }],
    ["jira_issue", { ...jira(), issue: { id: "123", self: "http://example.atlassian.net/rest/api/2/issue/123" } }],
    ["jira_issue", { ...jira(), issue: { id: "123", self: "https://example.atlassian.net/browse/ABC-1" } }],
    ["azure_devops_issue", { ...azure(), eventType: "workitem.commented" }],
    ["azure_devops_issue", { ...azure(), resource: { workItemId: 0 } }],
    ["azure_devops_issue", { ...azure(), resource: { id: 7, fields: { "System.State": { oldValue: "Active", newValue: "Done" } } } }],
    ["azure_devops_issue", { ...azure(), resourceContainers: {} }],
  ])("rejects unsupported events and incomplete identities %#", (event, payload) => {
    expect(() => resolveProviderIssueItem(event, payload)).toThrow(/Webhook validation failed/);
  });

  it("accepts Azure's documented version 1.0 snapshot identity without confusing update IDs", () => {
    const payload = { ...azure(), resourceVersion: "1.0", resource: { id: 123, fields: { "System.State": "Done" } } };
    expect(resolveProviderIssueItem("azure_devops_issue", payload).item_number).toBe("123");
    expect(providerIssueActivities("azure_devops_issue", payload, { closed_states: ["Done"] })).toEqual(["edited"]);
    payload.resource.revision = { id: 123 };
    expect(() => resolveProviderIssueItem("azure_devops_issue", payload)).toThrow(/work item ID/);
  });
});

describe("Jira normalized issue activities", () => {
  it.each([
    ["jira:issue_created", ["opened"]],
    ["jira:issue_deleted", ["deleted"]],
    ["jira:issue_updated", ["edited"]],
  ])("maps %s without fabricating lifecycle actions", (native, expected) => {
    expect(providerIssueActivities("jira_issue", jira(native))).toEqual(expected);
  });

  it.each([
    [{ field: "resolution", from: null, to: "1" }, ["edited", "closed"]],
    [{ field: "resolution", from: "1", to: null }, ["edited", "reopened"]],
    [{ field: "resolution", from: "1", to: "2" }, ["edited"]],
    [{ field: "resolution", to: "1" }, ["edited"]],
    [{ field: "status", fromString: "Doing", toString: "Done" }, ["edited"]],
    [{ field: "labels", fromString: null, toString: "bug" }, ["edited", "labeled"]],
    [{ field: "labels", fromString: "old", toString: null }, ["edited"]],
    [{ field: "labels", fromString: "old other", toString: "bug" }, ["edited"]],
    [{ field: "labels", fromString: null, toString: "not-current" }, ["edited"]],
    [{ field: "resolution", fieldtype: "custom", from: null, to: "1" }, ["edited"]],
  ])("requires explicit unambiguous changelog evidence %#", (change, expected) => {
    expect(providerIssueActivities("jira_issue", jira("jira:issue_updated", [change]))).toEqual(expected);
  });

  it("recognizes clearing labels only with an empty delivered current label set", () => {
    const payload = jira("jira:issue_updated", [{ field: "labels", fromString: "bug", toString: null }]);
    payload.issue.fields.labels = [];
    expect(providerIssueActivities("jira_issue", payload)).toEqual(["edited", "unlabeled"]);
  });
});

describe("Azure DevOps normalized issue activities", () => {
  const policy = { closed_states: ["Done", "Closed"] };

  it.each([
    ["workitem.created", ["opened"]],
    ["workitem.deleted", ["deleted"]],
    ["workitem.restored", ["restored"]],
    ["workitem.updated", ["edited"]],
  ])("maps %s without treating deletion as closure", (native, expected) => {
    expect(providerIssueActivities("azure_devops_issue", azure(native), policy)).toEqual(expected);
  });

  it.each([
    [{ "System.State": { oldValue: "Active", newValue: "Done" } }, ["edited", "closed"]],
    [{ "System.State": { oldValue: "Done", newValue: "Active" } }, ["edited", "reopened"]],
    [{ "System.State": { oldValue: "Closed", newValue: "Done" } }, ["edited"]],
    [{ "System.State": { newValue: "Done" } }, ["edited"]],
    [{ "System.State": "Done" }, ["edited"]],
    [{ "System.Tags": { oldValue: "", newValue: "bug; needs review" } }, ["edited", "labeled"]],
    [{ "System.Tags": { oldValue: "old;shared", newValue: "new; shared" } }, ["edited", "labeled", "unlabeled"]],
    [{ "System.Tags": { oldValue: "bug", newValue: "" } }, ["edited", "unlabeled"]],
    [{ "System.Tags": { newValue: "bug" } }, ["edited"]],
    [{ "System.Tags": "bug" }, ["edited"]],
  ])("requires native old/new deltas and configured terminal states %#", (fields, expected) => {
    expect(providerIssueActivities("azure_devops_issue", azure("workitem.updated", fields), policy)).toEqual(expected);
  });

  it("does not hard-code a closed state or look it up", () => {
    expect(providerIssueActivities("azure_devops_issue", azure("workitem.updated", { "System.State": { oldValue: "Active", newValue: "Closed" } }))).toEqual(["edited"]);
  });
});

describe("provider subscription normalization policy", () => {
  const subscription = {
    event: "azure_devops_issue",
    provider: "azure-devops",
    native_events: ["workitem.updated"],
    types: ["closed"],
    activity_normalization: { policy: "azure-devops-issue/v1", closed_states: ["Done"] },
  };

  it("accepts the versioned compiler policy", () => {
    expect(validProviderIssueSubscription(subscription)).toBe(true);
  });

  it.each([
    { provider: "github" },
    { types: ["created"] },
    { types: ["restored"], event: "jira_issue", provider: "jira", native_events: ["jira:issue_updated"], activity_normalization: { policy: "jira-issue/v1" } },
    { native_events: ["workitem.commented"] },
    { activity_normalization: { policy: "azure-devops-issue/v2", closed_states: ["Done"] } },
    { activity_normalization: { policy: "azure-devops-issue/v1" } },
    { activity_normalization: { policy: "azure-devops-issue/v1", closed_states: [] } },
  ])("rejects unsupported normalization %#", override => {
    expect(validProviderIssueSubscription({ ...subscription, ...override })).toBe(false);
  });

  const catalog = JSON.parse(readFileSync(new URL("../../../pkg/workflow/data/provider_issue_webhooks.json", import.meta.url), "utf8"));
  it.each(Object.entries(catalog))("accepts every compiler-cataloged policy and native event for %s", (event, definition) => {
    expect(
      validProviderIssueSubscription({
        event,
        provider: definition.provider,
        types: definition.actions,
        native_events: [...new Set(Object.values(definition.native_events))],
        activity_normalization: { policy: `${definition.provider}-issue/v1`, ...(event === "azure_devops_issue" ? { closed_states: ["Done"] } : {}) },
      })
    ).toBe(true);
  });
});

function providerDispatch(event, payloads) {
  return {
    action: "gh-aw-webhook-provider",
    client_payload: { schema_version: 2, aw_context: { webhook: { event, deliveries: payloads.map((payload, i) => ({ delivery_id: `d${i}`, payload })) } } },
  };
}

function providerEnvironment(event, types, nativeEvents, normalization = {}) {
  const provider = event.replace(/_issue$/, "").replaceAll("_", "-");
  return {
    GITHUB_EVENT_NAME: "repository_dispatch",
    GH_AW_WEBHOOK_SUBSCRIPTIONS: JSON.stringify([
      {
        event,
        provider,
        types,
        conditions: [],
        native_events: nativeEvents,
        schema_version: 2,
        activity_normalization: { policy: `${provider}-issue/v1`, ...normalization },
        dispatch: { event_type: "gh-aw-webhook-provider", payload_projection: { policy: "resource-url-types-filters/v2" } },
      },
    ]),
  };
}

describe("provider issue batch transport", () => {
  function notification(event, type = ["edited"], source = "a") {
    const urls = {
      jira_issue: `https://workspace-${source}.atlassian.net/rest/api/2/issue/123`,
      azure_devops_issue: `https://dev.azure.com/collection-${source}/_apis/wit/workItems/123`,
    };
    return { url: urls[event], type };
  }

  it.each([
    ["jira_issue", "jira:issue_created"],
    ["azure_devops_issue", "workitem.created"],
  ])("validates %s minimal notifications without fetching resource bodies", (event, native) => {
    const payload = notification(event, ["opened"]);
    const dispatch = providerDispatch(event, [payload]);
    const before = JSON.stringify(dispatch);
    expect(validateWebhook(dispatch, providerEnvironment(event, ["opened"], [native])).skipped).toBe(false);
    expect(JSON.stringify(dispatch)).toBe(before);
    expect(redactWebhookPayloads(dispatch.client_payload.aw_context).webhook.deliveries).toEqual([{ delivery_id: "d0" }]);
    const mismatched = providerEnvironment(event, ["deleted"], [native]);
    expect(() => validateWebhook(dispatch, mismatched)).toThrow(/activity/);
  });

  it.each(["jira_issue", "azure_devops_issue"])("deduplicates %s by URL-derived source and issue, independently of deliveries", event => {
    const payload = notification(event);
    const otherSource = notification(event, ["edited"], "b");
    const raw = providerDispatch(event, [payload, otherSource, payload]).client_payload.aw_context;
    raw.items = [{ item_type: "issue", item_number: "forged", repo: "forged/repo" }];
    const result = normalizeWebhookContext(raw);
    expect(result.items).toHaveLength(2);
    expect(result.items.every(item => item.item_type === event && item.repo === "")).toBe(true);
    expect(result.webhook.deliveries).toHaveLength(3);
    expect(normalizeWebhookContext(result)).toEqual(result);
    raw.webhook.deliveries.push({ delivery_id: "d0", payload });
    expect(normalizeWebhookContext(raw).webhook.deliveries).toHaveLength(3);
    raw.webhook.deliveries.push({ delivery_id: "d0", payload: otherSource });
    expect(() => normalizeWebhookContext(raw)).toThrow(/conflicting payloads/);
  });

  it("does not trust a forged GitHub-style action in a provider payload", () => {
    const payload = { ...notification("jira_issue"), action: "closed" };
    expect(() => validateWebhook(providerDispatch("jira_issue", [payload]), providerEnvironment("jira_issue", ["closed"], ["jira:issue_updated"]))).toThrow(/accepts only/);
  });

  it("requires every forwarded activity to be selected, not just one activity in each delivery", () => {
    const original = jira("jira:issue_updated", [{ field: "resolution", from: null, to: "1" }]);
    const activities = providerIssueActivities("jira_issue", original);
    expect(activities).toEqual(["edited", "closed"]);
    const closed = notification("jira_issue", ["closed"]);
    const env = providerEnvironment("jira_issue", ["closed"], ["jira:issue_updated"]);
    expect(validateWebhook(providerDispatch("jira_issue", [closed]), env).skipped).toBe(false);
    expect(() => validateWebhook(providerDispatch("jira_issue", [notification("jira_issue", activities)]), env)).toThrow(/activity/);
    expect(() => validateWebhook(providerDispatch("jira_issue", [closed, notification("jira_issue")]), env)).toThrow(/activity/);
  });

  it.each(["closed", "reopened"])("projects only the matching declared Azure terminal state for %s", activity => {
    const payload = notification("azure_devops_issue", [activity]);
    payload.filters = { "closed-states": ["Done"] };
    const env = providerEnvironment("azure_devops_issue", [activity], ["workitem.updated"], { closed_states: ["Done", "Closed"] });
    expect(validateWebhook(providerDispatch("azure_devops_issue", [payload]), env).skipped).toBe(false);
    for (const filters of [
      undefined,
      {},
      { "closed-states": [] },
      { "closed-states": ["private-active-state"] },
      { "closed-states": ["Done", "Closed"] },
      { "closed-states": ["Done", "private-active-state"] },
      { "closed-states": [true] },
      { "closed-states": ["done"] },
      { "closed-states": ["Done"], state: ["private-active-state"] },
    ]) {
      expect(() => validateWebhook(providerDispatch("azure_devops_issue", [{ ...payload, filters }]), env)).toThrow(/filters/);
      expect(() => validateWebhook(providerDispatch("azure_devops_issue", [{ ...payload, filters }]), env)).not.toThrow(/private-active-state/);
    }
  });

  it("does not forward configured terminal names when no selected lifecycle transition matched", () => {
    const env = providerEnvironment("azure_devops_issue", ["edited"], ["workitem.updated"], { closed_states: ["Done"] });
    const payload = notification("azure_devops_issue");
    expect(validateWebhook(providerDispatch("azure_devops_issue", [payload]), env).skipped).toBe(false);
    payload.filters = { "closed-states": ["Done"] };
    expect(() => validateWebhook(providerDispatch("azure_devops_issue", [payload]), env)).toThrow(/filters/);
    const other = notification("jira_issue", ["closed"]);
    other.filters = { "closed-states": ["Done"] };
    expect(() => validateWebhook(providerDispatch("jira_issue", [other]), providerEnvironment("jira_issue", ["closed"], ["jira:issue_updated"]))).toThrow(/filters/);
  });

  it("rejects contradictory closed and reopened activities rather than forwarding a full transition", () => {
    const payload = notification("jira_issue", ["closed", "reopened"]);
    expect(() => validateWebhook(providerDispatch("jira_issue", [payload]), providerEnvironment("jira_issue", ["closed", "reopened"], ["jira:issue_updated"]))).toThrow(/activity/);
  });

  it("rejects undeclared Azure lifecycle filters through the validation error path", () => {
    const payload = notification("azure_devops_issue", ["closed"]);
    payload.filters = { "closed-states": ["Done"] };
    const env = providerEnvironment("azure_devops_issue", ["edited"], ["workitem.updated"]);
    expect(() => validateWebhook(providerDispatch("azure_devops_issue", [payload]), env)).toThrow(/Webhook validation failed:.*filters/);
  });

  it("checks the selected native registration as well as normalized activities", () => {
    const dispatch = providerDispatch("jira_issue", [notification("jira_issue", ["opened"])]);
    expect(() => validateWebhook(dispatch, providerEnvironment("jira_issue", ["opened"], ["jira:issue_updated"]))).toThrow(/activity/);
  });

  it("rejects missing or unknown policy metadata rather than accepting native action strings as normalized types", () => {
    const env = providerEnvironment("jira_issue", ["opened"], ["jira:issue_created"]);
    const subscriptions = JSON.parse(env.GH_AW_WEBHOOK_SUBSCRIPTIONS);
    delete subscriptions[0].activity_normalization;
    env.GH_AW_WEBHOOK_SUBSCRIPTIONS = JSON.stringify(subscriptions);
    expect(() => validateWebhook(providerDispatch("jira_issue", [notification("jira_issue", ["opened"])]), env)).toThrow(/GH_AW_WEBHOOK_SUBSCRIPTIONS/);
  });

  it.each([
    ["jira_issue", jira()],
    ["azure_devops_issue", azure()],
  ])("rejects full native %s bodies rather than stripping them after admission", (event, payload) => {
    expect(() => normalizeWebhookContext(providerDispatch(event, [notification(event), payload]).client_payload.aw_context)).toThrow(/accepts only/);
  });
});
