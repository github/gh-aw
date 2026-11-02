import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const fixtures = require("./fixtures/connectors/linear-events.json");
const { providerIssueActivities, resolveProviderIssueItem } = require("./provider_issue_webhook.cjs");

function nativePayload(patch) {
  return { ...fixtures.base, ...patch, data: { ...fixtures.base.data, ...patch.data } };
}

describe("Linear connector native evidence fixtures", () => {
  it.each(fixtures.cases)("$name", fixture => {
    const payload = nativePayload(fixture.patch);
    if (fixture.reject) {
      expect(() => resolveProviderIssueItem("linear_issue", payload)).toThrow(/Webhook validation failed/);
      return;
    }
    expect(resolveProviderIssueItem("linear_issue", payload)).toMatchObject({
      item_type: "linear_issue",
      item_number: fixtures.base.data.id,
      source: fixtures.base.organizationId,
    });
    expect(providerIssueActivities("linear_issue", payload)).toEqual(fixture.activities);
  });
});
