import { afterEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";

const require = createRequire(import.meta.url);
const { WEBHOOK_PAYLOAD_POLICY, WEBHOOK_TYPES, resolveWebhookResource } = require("./webhook_resource.cjs");
const { normalizeWebhookContext } = require("./webhook_context.cjs");
const { validateWebhook } = require("./validate_webhook.cjs");
const { COMMAND_SOURCE_ACTIONS, isGatewayCommand } = require("./gateway_command_webhook.cjs");
const notificationCatalog = require("./github_notification_webhooks.json");

const examples = [
  ["projects_v2_item", "https://api.github.com/orgs/org/projectsV2/1/items/123", ["edited"], "123", "https://api.github.com/orgs/org/projectsV2/1"],
  ["code_scanning_alert", "https://api.github.com/repos/org/repo/code-scanning/alerts/123", ["created"], "123", undefined],
  ["dependabot_alert", "https://api.github.com/repos/org/repo/dependabot/alerts/123", ["created"], "123", undefined],
  ["jira_issue", "https://tenant.atlassian.net/rest/api/3/issue/123", ["edited", "closed", "labeled"], "123", "https://tenant.atlassian.net"],
  ["linear_issue", "https://linear.app/workspace/issue/APP-123/example-title", ["edited", "closed"], "APP-123", "https://linear.app/workspace"],
];

afterEach(() => vi.unstubAllGlobals());

describe("minimal resource notification contract", () => {
  it("explicitly versions the projection independently of the batch envelope", () => {
    expect(WEBHOOK_PAYLOAD_POLICY).toBe("resource-url-types-filters/v2");
  });

  const githubCatalog = {
    ...JSON.parse(readFileSync(new URL("../../../pkg/workflow/data/webhook_events.json", import.meta.url), "utf8")),
    ...Object.fromEntries(Object.entries(notificationCatalog.events).map(([event, definition]) => [event, { ...definition, filters: { ...notificationCatalog.common_filters, ...definition.filters } }])),
  };
  const providerCatalog = JSON.parse(readFileSync(new URL("../../../pkg/workflow/data/provider_issue_webhooks.json", import.meta.url), "utf8"));
  it.each(Object.entries(WEBHOOK_TYPES))("keeps %s activities identical to the compiler catalog", (event, types) => {
    if (isGatewayCommand(event)) {
      expect([...types].sort()).toEqual(event === "label_command" ? ["labeled"] : [...new Set(Object.values(COMMAND_SOURCE_ACTIONS).flat())].sort());
      return;
    }
    expect([...types].sort()).toEqual([...(githubCatalog[event] ?? providerCatalog[event]).actions].sort());
  });

  it("validates every complete specification payload against the actual runtime", () => {
    const specification = readFileSync(new URL("../../../docs/src/content/docs/specs/webhook-gateway-specification.md", import.meta.url), "utf8");
    const payloads = [...specification.matchAll(/```json title="([^"]+)\.payload\.json"\n([\s\S]*?)\n```/g)];
    expect(payloads.map(match => match[1]).sort()).toEqual(Object.keys(WEBHOOK_TYPES).sort());
    for (const [, event, json] of payloads) {
      const payload = JSON.parse(json);
      expect(Object.keys(payload).sort()).toEqual(payload.filters ? ["filters", "type", "url"] : ["type", "url"]);
      expect(normalizeWebhookContext({ webhook: { event, deliveries: [{ delivery_id: "example", payload }] } }).items).toHaveLength(1);
      const definition = githubCatalog[event] ?? providerCatalog[event];
      const subscription = {
        schema_version: 2,
        event,
        types: payload.type,
        conditions: Object.entries(isGatewayCommand(event) ? {} : (payload.filters ?? {})).map(([filter, values]) => ({
          filter,
          path: definition.filters[filter].path,
          operator: "equals",
          value_type: definition.filters[filter].type,
          values,
        })),
        dispatch: { event_type: "specification", payload_projection: { policy: WEBHOOK_PAYLOAD_POLICY } },
        ...(definition?.native_event ? { native_events: [definition.native_event] } : {}),
        ...(isGatewayCommand(event)
          ? {
              native_events: [event === "slash_command" ? "issue_comment" : "issues"],
              command_filter: {
                policy: event === "slash_command" ? "slash-command/v1" : "label-command/v1",
                names: ["review"],
                events: [event === "slash_command" ? "issue_comment" : "issues"],
                strategy: "inline",
                ...(event === "label_command" ? { remove_label: true } : {}),
              },
            }
          : {}),
        ...(definition?.provider
          ? {
              provider: definition.provider,
              native_events: [...new Set(Object.values(definition.native_events))],
              activity_normalization: { policy: `${definition.provider}-issue/v1` },
            }
          : {}),
      };
      expect(
        validateWebhook(
          { action: "specification", client_payload: { schema_version: 2, aw_context: { webhook: { event, deliveries: [{ delivery_id: "example", payload }] } } } },
          { GITHUB_EVENT_NAME: "repository_dispatch", GH_AW_WEBHOOK_SUBSCRIPTIONS: JSON.stringify([subscription]) }
        ).skipped
      ).toBe(false);
    }
  });

  it.each([
    ["projects_v2_item", "body.from"],
    ["projects_v2_item", "body.to"],
  ])("allows only exact configured scalar literals for %s %s", (event, filter) => {
    const definition = githubCatalog[event] ?? providerCatalog[event];
    const selected = "reviewed literal";
    const filters = { project_id: ["approved-project"], [filter]: [selected] };
    const subscription = {
      schema_version: 2,
      event,
      types: ["edited"],
      conditions: Object.entries(filters).map(([name, values]) => ({
        filter: name,
        path: definition.filters[name].path,
        operator: "equals",
        value_type: "string",
        values,
      })),
      dispatch: { event_type: "literal-projection", payload_projection: { policy: WEBHOOK_PAYLOAD_POLICY } },
    };
    const payload = { url: examples.find(([family]) => family === event)[1], type: ["edited"], filters };
    const envelope = { action: "literal-projection", client_payload: { schema_version: 2, aw_context: { webhook: { event, deliveries: [{ delivery_id: "literal", payload }] } } } };
    const env = { GITHUB_EVENT_NAME: "repository_dispatch", GH_AW_WEBHOOK_SUBSCRIPTIONS: JSON.stringify([subscription]) };
    expect(validateWebhook(envelope, env).skipped).toBe(false);
    for (const values of [["reviewed literal plus unselected text"], [selected, "unselected"], [{ body: selected }]]) {
      payload.filters[filter] = values;
      expect(() => validateWebhook(envelope, env)).toThrow(/filter/);
    }
  });

  it.each(["webhook", "example.event"])("rejects custom resource family %s", event => {
    expect(() => resolveWebhookResource(event, { url: "https://service.example/releases/42", type: ["example.event"] })).toThrow(/supported event activities/);
    expect(() => normalizeWebhookContext({ webhook: { event, deliveries: [] } })).toThrow(/supported webhook event/);
  });

  it.each(examples)("derives %s identity from only URL and activities, without network access", (event, url, type, number, source) => {
    const fetch = vi.fn(() => {
      throw new Error("must not fetch");
    });
    vi.stubGlobal("fetch", fetch);
    const payload = { url, type };
    const before = JSON.stringify(payload);
    const item = resolveWebhookResource(event, payload);
    expect(item).toMatchObject({ item_type: event, item_number: number, url });
    expect(item.source).toBe(source);
    expect(JSON.stringify(payload)).toBe(before);
    expect(fetch).not.toHaveBeenCalled();
  });

  it.each(examples)("rejects resource details in %s instead of silently stripping them", (event, url, type) => {
    for (const key of ["title", "body", "description", "labels", "sender", "actor", "state", "issue", "data", "resource", "action", "organizationId", "project_id"]) {
      const payload = { url, type, [key]: "private-content-must-not-be-logged" };
      expect(() => resolveWebhookResource(event, payload)).toThrow(/accepts only/);
      expect(() => resolveWebhookResource(event, payload)).not.toThrow(/private-content-must-not-be-logged/);
    }
  });

  it.each(examples)("preserves only scalar filter projections without changing %s resource identity", (event, url, type) => {
    const filters = { state: ["ready"], approved: [false], attempt: [1] };
    const payload = { url, type, filters };
    expect(resolveWebhookResource(event, payload)).toEqual(resolveWebhookResource(event, { url, type }));
    expect(normalizeWebhookContext({ webhook: { event, deliveries: [{ delivery_id: "filtered", payload }] } }).webhook.deliveries[0].payload.filters).toEqual(filters);
    for (const invalid of [null, [], {}, { state: "ready" }, { state: ["ready", null] }, { state: [{ body: "private" }] }, { state: ["ready", true] }, { count: [Infinity] }, JSON.parse('{"__proto__":["private"]}')]) {
      expect(() => resolveWebhookResource(event, { url, type, filters: invalid })).toThrow(/filters/);
    }
  });
  it.each(examples)("requires known distinct nonempty activities for %s", (event, url) => {
    for (const type of [undefined, null, "", "edited", [], [""], [{}], ["not-a-type"], ["closed", "closed"], ["created", "private-description"]]) {
      expect(() => resolveWebhookResource(event, { url, type })).toThrow(/type/);
    }
  });

  it.each(examples)("rejects noncanonical, credentialed, queried and fragmented %s URLs", (event, url, type) => {
    const parsed = new URL(url);
    for (const invalid of [
      undefined,
      "",
      {},
      url.replace("https:", "http:"),
      url.replace("https:", "HTTPS:"),
      url.replace(parsed.hostname, parsed.hostname.toUpperCase()),
      url.replace(parsed.origin, `${parsed.origin}:443`),
      url.replace(parsed.pathname, `/./${parsed.pathname.slice(1)}`),
      `${url}?token=private`,
      `${url}?`,
      `${url}#details`,
      `${url}#`,
      `${url}/`,
      `${parsed.origin}/other/resource/123`,
      url.replace("://", "://user:private@"),
    ]) {
      expect(() => resolveWebhookResource(event, { url: invalid, type })).toThrow(/canonical/);
      expect(() => resolveWebhookResource(event, { url: invalid, type })).not.toThrow(/private/);
    }
  });

  it("accepts user-owned and Enterprise Projects API URLs, including IDs beyond JavaScript integer precision", () => {
    const url = "https://github.example.com/api/v3/users/alice/projectsV2/7/items/9007199254740993";
    expect(resolveWebhookResource("projects_v2_item", { url, type: ["deleted"] }, { GITHUB_API_URL: "https://github.example.com/api/v3" })).toMatchObject({
      item_number: "9007199254740993",
      source: "https://github.example.com/api/v3/users/alice/projectsV2/7",
    });
  });

  it.each([
    "https://linear.app/issue/APP-123",
    "https://other.example/workspace/issue/APP-123",
    "https://linear.app/workspace/project/APP-123",
    "https://linear.app/workspace/issue/APP-0",
    "https://linear.app/workspace/issue/APP-123#comment",
  ])("rejects ambiguous or non-issue Linear URL %#", url => {
    expect(() => resolveWebhookResource("linear_issue", { url, type: ["opened"] })).toThrow(/canonical/);
  });

  it.each([["linear_issue", "https://linear.app/workspace/issue/APP-123/%E6%BC%A2%E5%AD%97", "APP-123", "https://linear.app/workspace"]])(
    "accepts canonical percent-encoded %s URL segments without changing identity",
    (event, url, number, source) => {
      expect(resolveWebhookResource(event, { url, type: ["edited"] })).toMatchObject({ item_number: number, source, url });
    }
  );

  it.each([
    ["linear_issue", "https://linear.app/workspace/issue/APP-123/title%"],
    ["linear_issue", "https://linear.app/workspace/issue/APP-123/title%GG"],
  ])("rejects malformed escapes in %s URL segments", (event, url) => {
    expect(() => resolveWebhookResource(event, { url, type: ["edited"] })).toThrow(/canonical/);
  });

  it("keeps Projects identities scoped to their project URL", () => {
    const result = normalizeWebhookContext({
      webhook: {
        event: "projects_v2_item",
        deliveries: ["1", "2"].map(project => ({ delivery_id: project, payload: { url: `https://api.github.com/orgs/org/projectsV2/${project}/items/123`, type: ["edited"] } })),
      },
    });
    expect(result.items).toHaveLength(2);
    expect(result.items[0]).not.toHaveProperty("project_id");
  });

  it.each([[["projects_v2_item"]], [{ event: "projects_v2_item" }], ["constructor"], ["__proto__"], ["unsupported_issue"]])("rejects non-string and unsupported event families %#", event => {
    expect(() => normalizeWebhookContext({ webhook: { event, deliveries: [{ delivery_id: "d", payload: { url: examples[0][1], type: ["edited"] } }] } })).toThrow(/supported webhook event/);
  });

  it("does not split Linear identities when only a readable URL slug changes", () => {
    const result = normalizeWebhookContext({
      webhook: {
        event: "linear_issue",
        deliveries: ["before", "after"].map(slug => ({ delivery_id: slug, payload: { url: `https://linear.app/workspace/issue/APP-123/${slug}`, type: ["edited"] } })),
      },
    });
    expect(result.items).toHaveLength(1);
    expect(result.webhook.deliveries).toHaveLength(2);
  });
});
