import { describe, expect, it } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { ROUTING_REASONING_EFFORTS, mapAWFRoutingEffort, resolveAWFModelRoutingSelection } = require("./awf_model_routing.cjs");

const reflectData = {
  endpoints: [{ configured: true, models: ["claude-opus-5", "gpt-5.6-sol"] }],
  routing: {
    status: "selected",
    selection: { provider: "github", model: "github-copilot/claude-opus-5", wire_model: "claude-opus-5", effort: "high", endpoint: "/v1/messages" },
  },
};

describe("awf_model_routing.cjs", () => {
  it("accepts the AWF effort set and validates endpoint compatibility", () => {
    expect(ROUTING_REASONING_EFFORTS).toEqual(["none", "minimal", "low", "medium", "high", "xhigh", "max"]);
    expect(resolveAWFModelRoutingSelection(reflectData, true, ["/v1/messages"]).selection).toMatchObject({
      wire_model: "claude-opus-5",
      endpoint: "/v1/messages",
      effort: "high",
    });
    expect(resolveAWFModelRoutingSelection(reflectData, true, ["/responses"]).error).toContain("not supported by this engine");
  });

  it.each([
    [null, null],
    ["none", "off"],
    ["minimal", "minimal"],
    ["max", "max"],
  ])("maps Pi effort %s to %s", (input, expected) => {
    expect(mapAWFRoutingEffort("pi", input)).toEqual({ effort: expected, error: null });
  });

  it.each([
    ["claude", "none"],
    ["claude", "minimal"],
    ["codex", "none"],
    ["codex", "max"],
  ])("rejects unsupported %s effort %s", (engine, effort) => {
    expect(mapAWFRoutingEffort(engine, effort).error).toContain(`"${effort}"`);
  });

  it.each([
    [null, "required model-routing selection"],
    [{ routing: { status: "pending" } }, "pending"],
    [{ ...reflectData, routing: { ...reflectData.routing, selection: { ...reflectData.routing.selection, endpoint: "/responses" } } }, "not supported by this engine"],
    [{ ...reflectData, endpoints: [{ configured: true, models: ["gpt-5.6-sol"] }] }, "unavailable Copilot wire model"],
  ])("fails closed for incomplete routing data", (data, message) => {
    expect(resolveAWFModelRoutingSelection(data, true, ["/v1/messages"]).error).toContain(message);
  });
});
