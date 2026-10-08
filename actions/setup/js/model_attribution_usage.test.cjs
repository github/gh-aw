import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const { recordFallbackModelFromUsage, getFallbackModel, recordModelRouting, getModelRouting, resolveEffectiveModel, getEffectiveModelLabel } = require("./model_attribution.cjs");

describe("AWF native fallback attribution", () => {
  it("uses actual-model evidence and isolates detection from the main agent", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "awf-native-fallback-"));
    const infoPath = path.join(dir, "aw_info.json");
    try {
      fs.writeFileSync(infoPath, '{"model":"primary"}');
      const record = { _schema: "token-usage/v0.28.44", model: "secondary", model_fallback: { requested_model: "primary", model: "secondary", status: 503, attempt: 1 } };
      const env = { GH_AW_INFO_MODEL: "primary", COPILOT_MODEL: "primary" };
      expect(recordFallbackModelFromUsage(JSON.stringify(record), env, infoPath)).toBe("secondary");
      expect(env).toEqual({ GH_AW_INFO_MODEL: "primary", COPILOT_MODEL: "primary" });
      expect(getFallbackModel(infoPath)).toBe("secondary");
      expect(JSON.parse(fs.readFileSync(infoPath, "utf8")).model).toBe("secondary");
      record.model = "detector";
      recordFallbackModelFromUsage(JSON.stringify(record), { GH_AW_PHASE: "detection" }, infoPath);
      expect(getFallbackModel(infoPath)).toBe("secondary");
      expect(getFallbackModel(infoPath, "detection")).toBe("detector");
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });

  it("does not attribute failed requests or unrelated transcript records", () => {
    const record = { _schema: "token-usage/v0.28.44", model: "secondary", model_fallback: { model: "secondary" } };
    for (const entry of [
      { ...record, status: 503 },
      { ...record, _schema: "tool-result/v1" },
      { ...record, model_fallback: null },
    ]) {
      expect(recordFallbackModelFromUsage(JSON.stringify(entry))).toBe("");
    }
  });

  it("reports malformed fallback records explicitly", () => {
    const warnings = [];
    expect(recordFallbackModelFromUsage('{"model_fallback":', {}, undefined, message => warnings.push(message))).toBe("");
    expect(warnings).toHaveLength(1);
    expect(warnings[0]).toContain("Skipping malformed AWF fallback usage record");
  });

  it("records routed model and effort while preserving the requested placeholder", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "awf-routed-model-"));
    const infoPath = path.join(dir, "aw_info.json");
    try {
      fs.writeFileSync(infoPath, JSON.stringify({ model: "auto" }));
      recordModelRouting({ status: "selected", model: "gpt-5.6-luna", wire_model: "gpt-5.6-luna", effort: "xhigh", endpoint: "/responses" }, {}, infoPath);
      expect(JSON.parse(fs.readFileSync(infoPath, "utf8"))).toMatchObject({
        model: "gpt-5.6-luna",
        requested_model: "auto",
        model_routing: { status: "selected", source: "awf-routing", wire_model: "gpt-5.6-luna", effort: "xhigh" },
      });
      expect(getModelRouting(infoPath).requested_model).toBe("auto");
      expect(resolveEffectiveModel(infoPath, "agent", { GH_AW_PRIMARY_MODEL: "usage-model", GH_AW_ENGINE_MODEL: "auto" }).model).toBe("gpt-5.6-luna");
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_ID: "copilot", GH_AW_ENGINE_MODEL: "auto" })).toBe("routed: gpt56 xhigh");
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });

  it("gives fallback usage precedence and suppresses placeholders for rejected routes", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "awf-routed-precedence-"));
    const infoPath = path.join(dir, "aw_info.json");
    try {
      fs.writeFileSync(infoPath, JSON.stringify({ model: "gpt-5.6-luna", requested_model: "agent", fallback_model: "gpt-5.4", model_routing: { status: "selected", wire_model: "gpt-5.6-luna", effort: "high" } }));
      expect(resolveEffectiveModel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "agent" }).model).toBe("gpt-5.4");
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_ID: "pi", GH_AW_ENGINE_MODEL: "agent" })).toBe("fallback: gpt54 (routed: gpt56 high)");
      fs.writeFileSync(infoPath, JSON.stringify({ model: "agent", requested_model: "agent", model_routing: { status: "rejected", failure_code: "unsupported_effort" } }));
      expect(resolveEffectiveModel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "agent" }).model).toBe("");
      expect(resolveEffectiveModel(infoPath, "agent", { GH_AW_PRIMARY_MODEL: "router-classifier", GH_AW_ENGINE_MODEL: "agent" }).model).toBe("");
      expect(getEffectiveModelLabel(infoPath, "agent", { GH_AW_ENGINE_MODEL: "agent" })).toBe("routing rejected (unsupported_effort)");
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });

  it("preserves configured model attribution for workflows without routing", () => {
    const dir = fs.mkdtempSync(path.join(os.tmpdir(), "awf-non-routed-model-"));
    const infoPath = path.join(dir, "aw_info.json");
    try {
      fs.writeFileSync(infoPath, JSON.stringify({ model: "configured-alias" }));
      expect(resolveEffectiveModel(infoPath, "agent", { GH_AW_PRIMARY_MODEL: "observed-model", GH_AW_ENGINE_MODEL: "configured-alias" }).model).toBe("configured-alias");
      expect(JSON.parse(fs.readFileSync(infoPath, "utf8"))).not.toHaveProperty("model_routing");
    } finally {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });
});
