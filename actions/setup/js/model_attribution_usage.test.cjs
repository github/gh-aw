import { describe, expect, it } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const require = createRequire(import.meta.url);
const { recordFallbackModelFromUsage, getFallbackModel } = require("./model_attribution.cjs");

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
});
