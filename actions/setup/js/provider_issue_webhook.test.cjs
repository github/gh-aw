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

function linear(action = "update", data = {}, updatedFrom = {}) {
  return { type: "Issue", action, organizationId: "workspace-a", data: { id: "issue-a", completedAt: null, canceledAt: null, labelIds: [], ...data }, updatedFrom };
}

describe("provider issue native identity", () => {
  it.each([
    ["jira_issue", jira(), "123", "https://example.atlassian.net"],
    ["linear_issue", linear(), "issue-a", "workspace-a"],
  ])("uses source-scoped identities, not GitHub repositories: %s", (event, payload, number, source) => {
    expect(resolveProviderIssueItem(event, payload)).toMatchObject({ item_type: event, item_number: number, source, repo: "" });
  });

  it.each([
    ["jira_issue", { ...jira(), webhookEvent: "comment_created" }],
    ["jira_issue", { ...jira(), issue: { id: 123 } }],
    ["jira_issue", { ...jira(), issue: { id: "123", self: "http://example.atlassian.net/rest/api/2/issue/123" } }],
    ["jira_issue", { ...jira(), issue: { id: "123", self: "https://example.atlassian.net/browse/ABC-1" } }],
    ["linear_issue", { ...linear(), type: "IssueLabel" }],
    ["linear_issue", { ...linear(), organizationId: "" }],
    ["linear_issue", { ...linear(), action: "closed" }],
  ])("rejects unsupported events and incomplete identities %#", (event, payload) => {
    expect(() => resolveProviderIssueItem(event, payload)).toThrow(/Webhook validation failed/);
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

describe("Linear normalized issue activities", () => {
  it.each([
    ["create", {}, {}, ["opened"]],
    ["remove", {}, {}, ["deleted"]],
    ["update", { completedAt: "2026-10-01T00:00:00Z" }, { completedAt: null }, ["edited", "closed"]],
    ["update", { canceledAt: "2026-10-01T00:00:00Z" }, { canceledAt: null }, ["edited", "closed"]],
    ["update", {}, { completedAt: "2026-10-01T00:00:00Z" }, ["edited", "reopened"]],
    ["update", { canceledAt: "2026-10-02T00:00:00Z" }, { completedAt: "2026-10-01T00:00:00Z", canceledAt: null }, ["edited"]],
    ["update", { completedAt: "2026-10-02T00:00:00Z" }, { completedAt: "2026-10-01T00:00:00Z" }, ["edited"]],
    ["update", { completedAt: "2026-10-01T00:00:00Z" }, {}, ["edited"]],
    ["update", { completedAt: "not-a-timestamp" }, { completedAt: null }, ["edited"]],
    ["update", { completedAt: "2026-10-01T00:00:00Z", canceledAt: undefined }, { completedAt: null }, ["edited"]],
    ["update", { labelIds: ["new", "shared"] }, { labelIds: ["old", "shared"] }, ["edited", "labeled", "unlabeled"]],
    ["update", { labelIds: ["bug"] }, { labelIds: [] }, ["edited", "labeled"]],
    ["update", { labelIds: [] }, { labelIds: ["bug"] }, ["edited", "unlabeled"]],
    ["update", { labelIds: ["bug"] }, {}, ["edited"]],
    ["update", { labelIds: ["bug"] }, { labelIds: null }, ["edited"]],
    ["update", { labelIds: ["b", "a"] }, { labelIds: ["a", "b"] }, ["edited"]],
  ])("derives lifecycle and label deltas from updatedFrom %#", (action, data, before, expected) => {
    expect(providerIssueActivities("linear_issue", linear(action, data, before))).toEqual(expected);
  });
});

describe("provider subscription normalization policy", () => {
  const subscription = {
    event: "linear_issue",
    provider: "linear",
    native_events: ["update"],
    types: ["closed"],
    activity_normalization: { policy: "linear-issue/v1" },
  };

  it("accepts the versioned compiler policy", () => {
    expect(validProviderIssueSubscription(subscription)).toBe(true);
  });

  it.each([
    { provider: "github" },
    { types: ["created"] },
    { types: ["restored"], event: "jira_issue", provider: "jira", native_events: ["jira:issue_updated"], activity_normalization: { policy: "jira-issue/v1" } },
    { native_events: ["comment"] },
    { activity_normalization: { policy: "linear-issue/v2" } },
    { activity_normalization: { policy: "linear-issue/v1", closed_states: [] } },
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
        activity_normalization: { policy: `${definition.provider}-issue/v1` },
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
      linear_issue: `https://linear.app/workspace-${source}/issue/APP-123`,
    };
    return { url: urls[event], type };
  }

  it.each([
    ["jira_issue", "jira:issue_created"],
    ["linear_issue", "create"],
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

  it.each(["jira_issue", "linear_issue"])("deduplicates %s by URL-derived source and issue, independently of deliveries", event => {
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
    const original = linear("update", { completedAt: "2026-10-01T00:00:00Z" }, { completedAt: null });
    const activities = providerIssueActivities("linear_issue", original);
    expect(activities).toEqual(["edited", "closed"]);
    const closed = notification("linear_issue", ["closed"]);
    const env = providerEnvironment("linear_issue", ["closed"], ["update"]);
    expect(validateWebhook(providerDispatch("linear_issue", [closed]), env).skipped).toBe(false);
    expect(() => validateWebhook(providerDispatch("linear_issue", [notification("linear_issue", activities)]), env)).toThrow(/activity/);
    expect(() => validateWebhook(providerDispatch("linear_issue", [closed, notification("linear_issue")]), env)).toThrow(/activity/);
  });

  it("rejects contradictory closed and reopened activities rather than forwarding a full transition", () => {
    const payload = notification("linear_issue", ["closed", "reopened"]);
    expect(() => validateWebhook(providerDispatch("linear_issue", [payload]), providerEnvironment("linear_issue", ["closed", "reopened"], ["update"]))).toThrow(/activity/);
  });

  it("checks the selected native registration as well as normalized activities", () => {
    const dispatch = providerDispatch("linear_issue", [notification("linear_issue", ["opened"])]);
    expect(() => validateWebhook(dispatch, providerEnvironment("linear_issue", ["opened"], ["update"]))).toThrow(/activity/);
  });

  it("rejects missing or unknown policy metadata rather than accepting native action strings as normalized types", () => {
    const env = providerEnvironment("linear_issue", ["opened"], ["create"]);
    const subscriptions = JSON.parse(env.GH_AW_WEBHOOK_SUBSCRIPTIONS);
    delete subscriptions[0].activity_normalization;
    env.GH_AW_WEBHOOK_SUBSCRIPTIONS = JSON.stringify(subscriptions);
    expect(() => validateWebhook(providerDispatch("linear_issue", [notification("linear_issue", ["opened"])]), env)).toThrow(/GH_AW_WEBHOOK_SUBSCRIPTIONS/);
  });

  it.each([
    ["jira_issue", jira()],
    ["linear_issue", linear()],
  ])("rejects full native %s bodies rather than stripping them after admission", (event, payload) => {
    expect(() => normalizeWebhookContext(providerDispatch(event, [notification(event), payload]).client_payload.aw_context)).toThrow(/accepts only/);
  });
});
