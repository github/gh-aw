import { describe, expect, it } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const { ROUTING_REASONING_EFFORTS, mapAWFRoutingEffort, resolveAWFModelRoutingSelection } = require("./awf_model_routing.cjs");

const reflectData = {
  endpoints: [{ provider: "github", configured: true, models: ["claude-opus-5", "gpt-5.6-sol"] }],
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

  it("uses an engine-compatible endpoint from complete routing metadata and preserves AWF's selection", () => {
    const reflect = {
      ...reflectData,
      candidate_metadata_complete: true,
      endpoints: [
        {
          ...reflectData.endpoints[0],
          routing_models: [{ model_id: "claude-opus-5", supported_endpoints: ["/v1/messages", "/chat/completions"] }],
        },
      ],
      routing: {
        ...reflectData.routing,
        selection: { ...reflectData.routing.selection, endpoint: "/chat/completions" },
      },
    };
    expect(resolveAWFModelRoutingSelection(reflect, true, ["/v1/messages"], true).selection).toMatchObject({
      endpoint: "/v1/messages",
      selected_endpoint: "/chat/completions",
    });
  });

  it.each([
    [{ candidate_metadata_complete: false, routing_models: [{ model_id: "claude-opus-5", supported_endpoints: ["/v1/messages"] }] }, "metadata is incomplete"],
    [{ candidate_metadata_complete: true, routing_models: [{ model_id: "claude-opus-5", supported_endpoints: ["/responses"] }] }, "advertises endpoints"],
    [{ candidate_metadata_complete: true, routing_models: [] }, "metadata is incomplete"],
  ])("fails closed when the model endpoint metadata cannot verify a compatible endpoint: %s", ({ candidate_metadata_complete, routing_models }, error) => {
    const reflect = {
      ...reflectData,
      candidate_metadata_complete,
      endpoints: [{ ...reflectData.endpoints[0], routing_models }],
      routing: { ...reflectData.routing, selection: { ...reflectData.routing.selection, endpoint: "/chat/completions" } },
    };
    expect(resolveAWFModelRoutingSelection(reflect, true, ["/v1/messages"], true).error).toContain(error);
  });

  it("checks routed model availability only against configured GitHub provider aliases", () => {
    const mixedProviders = {
      ...reflectData,
      endpoints: [
        { provider: "openai", configured: true, models: ["claude-opus-5"] },
        { provider: "github-copilot", configured: true, models: ["gpt-5.6-sol"] },
      ],
    };
    expect(resolveAWFModelRoutingSelection(mixedProviders, true, ["/v1/messages"]).error).toContain("unavailable Copilot wire model");
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
    [{ ...reflectData, endpoints: [{ provider: "github", configured: true, models: ["gpt-5.6-sol"] }] }, "unavailable Copilot wire model"],
  ])("fails closed for incomplete routing data", (data, message) => {
    expect(resolveAWFModelRoutingSelection(data, true, ["/v1/messages"]).error).toContain(message);
  });
});
