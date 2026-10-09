import { describe, expect, it } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const { ROUTING_REASONING_EFFORTS, mapAWFRoutingEffort, recordAWFModelRoutingOutcome, resolveAWFModelRoutingSelection } = require("./awf_model_routing.cjs");

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
      models_fetch_complete: true,
      endpoints: [
        {
          ...reflectData.endpoints[0],
          provider: "copilot",
          routing_models: [
            {
              model_id: "claude-opus-5",
              source: "provider",
              supported_endpoints: ["/v1/messages", "/chat/completions"],
              supported_reasoning_efforts: ["low", "medium", "high", "xhigh", "max"],
              context_window_tokens: 1000000,
              candidate_metadata_complete: true,
            },
            { model_id: "claude-haiku-4.5", source: "provider", supported_endpoints: ["/chat/completions", "/v1/messages"], supported_reasoning_efforts: [], context_window_tokens: 200000, candidate_metadata_complete: true },
          ],
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

  it("fails closed when multiple configured GitHub endpoints list the routed model", () => {
    const reflect = {
      ...reflectData,
      endpoints: [
        {
          provider: "copilot",
          configured: true,
          models: ["claude-opus-5"],
          routing_models: [{ model_id: "claude-opus-5", candidate_metadata_complete: true, supported_endpoints: ["/v1/messages"] }],
        },
        {
          provider: "github",
          configured: true,
          models: ["claude-opus-5"],
          routing_models: [{ model_id: "claude-opus-5", candidate_metadata_complete: true, supported_endpoints: ["/v1/messages"] }],
        },
      ],
    };
    const result = resolveAWFModelRoutingSelection(reflect, true, ["/v1/messages"]);
    expect(result.selection).toBeNull();
    expect(result.error).toContain("model claude-opus-5");
    expect(result.error).toContain("[copilot, github]");
  });

  it.each([
    [[{ model_id: "claude-opus-5", candidate_metadata_complete: false, supported_endpoints: ["/v1/messages"] }], "metadata is incomplete"],
    [
      [
        { model_id: "claude-opus-5", supported_endpoints: ["/v1/messages"] },
        { model_id: "claude-haiku-4.5", candidate_metadata_complete: true, supported_endpoints: ["/v1/messages"] },
      ],
      "metadata is incomplete",
    ],
    [[{ model_id: "claude-opus-5", candidate_metadata_complete: true, supported_endpoints: ["/responses"] }], "advertises endpoints"],
    [[], "metadata is incomplete"],
  ])("fails closed when the model endpoint metadata cannot verify a compatible endpoint: %s", (routing_models, error) => {
    const reflect = {
      ...reflectData,
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

  it.each(ROUTING_REASONING_EFFORTS)("preserves the %s effort for Copilot", effort => {
    expect(mapAWFRoutingEffort("copilot", effort)).toEqual({ effort, error: null });
  });

  it("records successful and refused harness outcomes only when routing is enabled", () => {
    const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "awf-routing-outcome-"));
    const env = { GH_AW_MODEL_ROUTING: "1", GH_AW_TMP_DIR: tmpDir };
    const recordPath = path.join(tmpDir, "agent", "awf-routing-outcome.json");
    try {
      expect(
        recordAWFModelRoutingOutcome(
          {
            status: "selected",
            wire_model: "gpt-5.6-luna",
            endpoint: "/responses",
            selected_endpoint: "/chat/completions",
            effort: "high",
            applied_effort: "high",
          },
          env
        )
      ).toBe(true);
      expect(JSON.parse(fs.readFileSync(recordPath, "utf8"))).toMatchObject({
        status: "selected",
        wire_model: "gpt-5.6-luna",
        endpoint: "/responses",
        selected_endpoint: "/chat/completions",
        effort: "high",
        applied_effort: "high",
      });
      expect(recordAWFModelRoutingOutcome({ status: "selected", wire_model: "gpt-5.6-luna", endpoint: "/invalid", selected_endpoint: "\n" }, env)).toBe(true);
      expect(JSON.parse(fs.readFileSync(recordPath, "utf8"))).not.toHaveProperty("endpoint");
      expect(JSON.parse(fs.readFileSync(recordPath, "utf8"))).not.toHaveProperty("selected_endpoint");
      expect(recordAWFModelRoutingOutcome({ status: "rejected", failure_code: "unsupported_effort" }, env)).toBe(true);
      expect(JSON.parse(fs.readFileSync(recordPath, "utf8"))).toMatchObject({ status: "rejected", failure_code: "unsupported_effort" });
      expect(recordAWFModelRoutingOutcome({ status: "selected", wire_model: "gpt-5.6-luna\nrouted: false" }, env)).toBe(false);
      expect(recordAWFModelRoutingOutcome({ status: "selected", wire_model: "gpt-5.6-luna" }, { ...env, GH_AW_MODEL_ROUTING: "0" })).toBe(false);
    } finally {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    }
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
