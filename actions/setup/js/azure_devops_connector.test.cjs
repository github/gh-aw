import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";

const require = createRequire(import.meta.url);
const { providerIssueActivities, resolveProviderIssueItem } = require("./provider_issue_webhook.cjs");
const fixtures = JSON.parse(readFileSync(new URL("../../../pkg/connectors/testdata/providers/azure-devops/native-events.json", import.meta.url), "utf8"));
const event = "azure_devops_issue";
const policy = { closed_states: fixtures.closedStates };
const update = fixtures.deliveries.find(delivery => delivery.name === "updated").payload;

describe("Azure DevOps connector native fixtures preserve built-in behavior", () => {
  it.each(fixtures.deliveries)("normalizes $name with collection-scoped work item identity", ({ payload, activities }) => {
    expect(resolveProviderIssueItem(event, payload)).toEqual({
      item_type: event,
      item_number: "123",
      source: fixtures.collection,
      repo: "",
    });
    expect(payload.resourceContainers.project.id).toBe(fixtures.project);
    expect(providerIssueActivities(event, payload, policy)).toEqual(activities);
  });

  it.each([
    ["reopened", { "System.State": { oldValue: "Done", newValue: "Active" } }, ["edited", "reopened"]],
    ["terminal-to-terminal", { "System.State": { oldValue: "Done", newValue: "Closed" } }, ["edited"]],
    ["missing-old-state", { "System.State": { newValue: "Done" } }, ["edited"]],
    ["missing-new-state", { "System.State": { oldValue: "Done" } }, ["edited"]],
    ["snapshot-state", { "System.State": "Done" }, ["edited"]],
    ["missing-old-tags", { "System.Tags": { newValue: "bug" } }, ["edited"]],
    ["missing-new-tags", { "System.Tags": { oldValue: "bug" } }, ["edited"]],
    ["snapshot-tags", { "System.Tags": "bug" }, ["edited"]],
    ["reordered-tags", { "System.Tags": { oldValue: "a; b", newValue: "b; a; a" } }, ["edited"]],
    ["added-tags", { "System.Tags": { oldValue: "", newValue: "bug" } }, ["edited", "labeled"]],
    ["removed-tags", { "System.Tags": { oldValue: "bug", newValue: "" } }, ["edited", "unlabeled"]],
  ])("requires delivered evidence: %s", (_name, fields, activities) => {
    const payload = { ...update, resource: { ...update.resource, fields } };
    expect(providerIssueActivities(event, payload, policy)).toEqual(activities);
  });

  it("does not assume terminal states without explicit configuration", () => {
    expect(providerIssueActivities(event, update)).toEqual(["edited", "labeled", "unlabeled"]);
  });

  it.each([
    { eventType: "workitem.commented" },
    { resourceContainers: { project: { id: fixtures.project } } },
    { resource: { id: 7, fields: update.resource.fields } },
    { resource: { workItemId: 0 } },
    { resource: { workItemId: Number.MAX_SAFE_INTEGER + 1 } },
  ])("rejects incomplete or unsupported native identity %#", replacement => {
    expect(() => resolveProviderIssueItem(event, { ...update, ...replacement })).toThrow(/Webhook validation failed/);
  });
});
