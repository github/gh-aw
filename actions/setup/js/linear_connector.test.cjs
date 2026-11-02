import { describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { isProviderIssueEvent, resolveProviderIssueItem } = require("./provider_issue_webhook.cjs");
const { resolveWebhookResource } = require("./webhook_resource.cjs");
const { loadHandlers } = require("./safe_output_handler_manager.cjs");
const tools = require("./safe_outputs_tools.json");

describe("Linear uses only the shared connector runtime", () => {
  it("does not register legacy provider mutation handlers or tool schemas", async () => {
    vi.stubGlobal("core", { info: vi.fn(), debug: vi.fn() });
    try {
      for (const operation of ["linear_create_issue", "linear_add_comment", "linear_update_issue"]) {
        const handlers = await loadHandlers({ [operation]: {} });
        expect(handlers.has(operation)).toBe(false);
        expect(tools.some(tool => tool.name === operation)).toBe(false);
      }
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it("fails closed for the removed built-in ingress family", () => {
    expect(isProviderIssueEvent("linear_issue")).toBe(false);
    expect(() => resolveProviderIssueItem("linear_issue", { type: "Issue", action: "create", data: { id: "issue" }, organizationId: "org" })).toThrow(/Webhook validation failed/);
    expect(() => resolveWebhookResource("linear_issue", { url: "https://linear.app/workspace/issue/ENG-123", type: ["opened"] })).toThrow();
  });
});
