import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";

const require = createRequire(import.meta.url);
const { isProviderIssueEvent, providerIssueActivities, resolveProviderIssueItem, validProviderIssueSubscription, matchesProviderIssueSubscription } = require("./provider_issue_webhook.cjs");
const { normalizeWebhookContext } = require("./webhook_context.cjs");
const { validateWebhook } = require("./validate_webhook.cjs");

describe("removed native provider issue routes", () => {
  it.each(["jira_issue", "linear_issue", "azure_devops_issue"])("rejects native %s identities, activities, and subscriptions", event => {
    const subscription = {
      event,
      provider: event.replace(/_issue$/, "").replaceAll("_", "-"),
      types: ["opened"],
      native_events: ["create"],
      activity_normalization: { policy: `${event.replaceAll("_", "-")}/v1` },
      schema_version: 2,
      conditions: [],
      dispatch: { event_type: "gh-aw-webhook-provider", payload_projection: { policy: "resource-url-types-filters/v2" } },
    };
    const payload = { url: "https://provider.example/issue/123", type: ["opened"] };
    const context = { webhook: { event, deliveries: [{ delivery_id: "d0", payload }] } };
    expect(isProviderIssueEvent(event)).toBe(false);
    expect(() => resolveProviderIssueItem(event, payload)).toThrow(/unsupported provider issue event/);
    expect(() => providerIssueActivities(event, payload)).toThrow(/unsupported provider issue event/);
    expect(validProviderIssueSubscription(subscription)).toBe(false);
    expect(matchesProviderIssueSubscription(subscription, payload)).toBe(false);
    expect(() => normalizeWebhookContext(context)).toThrow(/supported webhook event/);
    expect(() =>
      validateWebhook({ action: "gh-aw-webhook-provider", client_payload: { schema_version: 2, aw_context: context } }, { GITHUB_EVENT_NAME: "repository_dispatch", GH_AW_WEBHOOK_SUBSCRIPTIONS: JSON.stringify([subscription]) })
    ).toThrow(/GH_AW_WEBHOOK_SUBSCRIPTIONS/);
  });

  it("does not advertise native provider normalization policies", () => {
    const catalog = JSON.parse(readFileSync(new URL("../../../pkg/workflow/data/provider_issue_webhooks.json", import.meta.url), "utf8"));
    expect(catalog).toEqual({});
  });
});
