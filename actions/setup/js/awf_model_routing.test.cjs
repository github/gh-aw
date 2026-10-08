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
      expect(recordAWFModelRoutingOutcome({ status: "selected", wire_model: "gpt-5.6-luna", effort: "high", applied_effort: "high" }, env)).toBe(true);
      expect(JSON.parse(fs.readFileSync(recordPath, "utf8"))).toMatchObject({
        status: "selected",
        wire_model: "gpt-5.6-luna",
        effort: "high",
        applied_effort: "high",
      });
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
