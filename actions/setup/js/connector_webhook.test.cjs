import { describe, expect, it } from "vitest";
import { createHash } from "node:crypto";
import { validateWebhook } from "./validate_webhook.cjs";
import { normalizeWebhookContext } from "./webhook_context.cjs";

function fixture() {
  const connector = {
    id: "aw.github.com/tracker",
    alias: "tracker",
    trigger: "issue-created",
    digest: "a".repeat(64),
    verification_profile: "example.com/hmac-sha256",
    native_events: ["issue.created"],
    normalize: { subject: "/issue/id", url: "/issue/url" },
    origins: ["https://issues.example.com"],
  };
  const event =
    "connector_" +
    createHash("sha256")
      .update(connector.alias + "\0" + connector.trigger + "\0" + connector.digest)
      .digest("hex")
      .slice(0, 24);
  const subscription = {
    schema_version: 2,
    event,
    types: [connector.trigger],
    connector,
    conditions: [
      { filter: "connector_match_0", path: ["event"], operator: "equals", value_type: "string", values: ["issue.created"] },
      { filter: "connector_scope_0", path: ["issue", "id"], operator: "equals", value_type: "string", values: ["APP-1", "APP-2"] },
    ],
    dispatch: {
      event_type: "gh-aw-webhook-example",
      payload_projection: { policy: "resource-url-types-filters/v2" },
      max_payload_bytes: 65535,
      max_request_bytes: 65535,
    },
  };
  const delivery = {
    delivery_id: "verified-native-delivery",
    payload: {
      url: "https://issues.example.com/issues/APP-1",
      type: ["issue-created"],
      filters: { connector_match_0: ["issue.created"], connector_scope_0: ["APP-1"] },
    },
  };
  const dispatch = { action: subscription.dispatch.event_type, client_payload: { schema_version: 2, aw_context: { webhook: { event, deliveries: [delivery] } } } };
  const env = subscriptions => ({ GITHUB_EVENT_NAME: "repository_dispatch", GH_AW_WEBHOOK_SUBSCRIPTIONS: JSON.stringify(subscriptions) });
  return { subscription, dispatch, delivery, env };
}

describe("connector webhook routing", () => {
  it("requires the exact source evaluator version and closed bounded rules", () => {
    const { subscription, dispatch, env } = fixture();
    const source = subscription.connector;
    source.evaluation_version = "gh-aw/event-predicates/v1";
    source.when = { path: "/state/new", other: "/state/old", op: "enters", values: ["Done"] };
    source.filters = { sender: { path: "/actor/id", type: "string", values: ["approved"] } };
    subscription.conditions.push({ filter: "connector_filter_sender", path: ["actor", "id"], operator: "equals", value_type: "string", values: ["approved"] });
    dispatch.client_payload.aw_context.webhook.deliveries[0].payload.filters.connector_filter_sender = ["approved"];
    expect(validateWebhook(dispatch, env([subscription])).skipped).toBe(false);
    for (const version of ["", "gh-aw/event-predicates/v2", undefined]) {
      source.evaluation_version = version;
      expect(() => validateWebhook(dispatch, env([subscription]))).toThrow();
    }
    source.evaluation_version = "gh-aw/event-predicates/v1";
    source.when.op = "execute";
    expect(() => validateWebhook(dispatch, env([subscription]))).toThrow();
  });

  it("uses the existing closed envelope and yields reference-only, non-GitHub identities", () => {
    const { subscription, dispatch, env } = fixture();
    expect(validateWebhook(dispatch, env([subscription]))).toMatchObject({ skipped: false, delivery_ids: ["verified-native-delivery"] });
    const normalized = normalizeWebhookContext(dispatch.client_payload.aw_context);
    expect(normalized.items).toHaveLength(1);
    expect(normalized.items[0]).toMatchObject({ repo: "", source: "https://issues.example.com", item_number: expect.stringMatching(/^uri:[0-9a-f]{64}$/) });
    expect(normalized.items[0]).not.toHaveProperty("body");
  });

  it.each([
    value => {
      value.connector.verification_profile = "";
    },
    value => {
      value.connector.origins = ["http://issues.example.com"];
    },
    value => {
      value.connector.origins = ["https://issues.example.com/path"];
    },
    value => {
      value.connector.origins = ["https://user:pass@issues.example.com"];
    },
    value => {
      value.connector.origins = ["https://*.example.com"];
    },
    value => {
      value.connector.native_events = [];
    },
    value => {
      value.connector.digest = "b".repeat(64);
    },
    value => {
      value.connector.alias = "different";
    },
    value => {
      value.connector.unexpected = true;
    },
    value => {
      value.conditions = [];
    },
    value => {
      value.connector = undefined;
    },
  ])("rejects malformed or mismatched compiler source metadata", mutate => {
    const { subscription, dispatch, env } = fixture();
    mutate(subscription);
    expect(() => validateWebhook(dispatch, env([subscription]))).toThrow();
  });

  it.each([
    payload => {
      payload.url = "https://evil.example.com/issues/APP-1";
    },
    payload => {
      payload.url = "https://user:pass@issues.example.com/issues/APP-1";
    },
    payload => {
      payload.url += "?untrusted=1";
    },
    payload => {
      payload.type = ["issue-updated"];
    },
    payload => {
      payload.type = ["issue-created", "issue-updated"];
    },
    payload => {
      payload.filters.connector_scope_0 = ["OTHER"];
    },
    payload => {
      payload.filters.connector_scope_0 = ["APP-1", "APP-2"];
    },
    payload => {
      delete payload.filters.connector_scope_0;
    },
    payload => {
      payload.filters.extra = ["unfiltered"];
    },
    payload => {
      payload.body = "unfiltered native event";
    },
  ])("rejects unapproved origins, types, widened evidence and native bodies", mutate => {
    const { subscription, dispatch, delivery, env } = fixture();
    mutate(delivery.payload);
    expect(() => validateWebhook(dispatch, env([subscription]))).toThrow();
  });

  it("supports distinct dynamic trigger subscriptions without a provider registry", () => {
    const first = fixture();
    const second = fixture();
    second.subscription.connector.trigger = "issue-updated";
    second.subscription.types = ["issue-updated"];
    const source = second.subscription.connector;
    second.subscription.event =
      "connector_" +
      createHash("sha256")
        .update(source.alias + "\0" + source.trigger + "\0" + source.digest)
        .digest("hex")
        .slice(0, 24);
    second.dispatch.client_payload.aw_context.webhook.event = second.subscription.event;
    second.delivery.payload.type = ["issue-updated"];
    expect(validateWebhook(second.dispatch, second.env([first.subscription, second.subscription]))).toMatchObject({ skipped: false, event: second.subscription.event });
  });

  it("preserves strict safe-integer scope evidence without string coercion", () => {
    const { subscription, dispatch, delivery, env } = fixture();
    subscription.conditions[1] = { filter: "connector_scope_0", path: ["issue", "id"], operator: "equals", value_type: "integer", values: [42] };
    delivery.payload.filters.connector_scope_0 = [42];
    expect(validateWebhook(dispatch, env([subscription])).skipped).toBe(false);
    for (const value of ["42", 43, 42.5, 9007199254740992]) {
      delivery.payload.filters.connector_scope_0 = [value];
      expect(() => validateWebhook(dispatch, env([subscription]))).toThrow();
    }
  });
});
