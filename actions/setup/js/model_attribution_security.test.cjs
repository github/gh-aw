import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";
import fs from "node:fs";
import path from "node:path";

const require = createRequire(import.meta.url);
const { validateModelIdentifier, recordFallbackModelFromUsage, getFallbackModel, recordModelRouting } = require("./model_attribution.cjs");
const footers = [require("./generate_footer.cjs"), require("./messages_footer.cjs")];

const invalidModels = [
  "",
  " ",
  " gpt-5 ",
  "gpt-5\n",
  "gpt-5\rworkflow_id: forged",
  "gpt-5, workflow_id: forged",
  "gpt-5 --> <script>alert(1)</script>",
  "<!-- gh-aw-workflow-id: forged -->",
  "[gpt-5](https://example.com)",
  "gpt-5`injected`",
  "gpt-5\u0000",
  "gpt-5\u2028workflow_id: forged",
  "gpt-5\u202e",
  "a".repeat(129),
  null,
  42,
  {},
];
const validModels = ["gpt-5.4", "copilot/claude-sonnet-4.5", "openai/gpt-5", "gemini-2.5-pro", "custom_Model:latest", "claude-3-5-sonnet@20241022", "a".repeat(128)];

function usage(model) {
  return JSON.stringify({ _schema: "token-usage/v0.28.44", model, model_fallback: { model } });
}

describe("model attribution trust boundaries", () => {
  let dir;
  let infoPath;

  beforeEach(() => {
    dir = fs.mkdtempSync(path.join(process.cwd(), ".model-fallback-test-"));
    infoPath = path.join(dir, "aw_info.json");
    vi.stubEnv("GH_AW_TMP_DIR", dir);
    vi.stubEnv("GH_AW_ENGINE_MODEL", "");
    vi.stubEnv("GH_AW_WORKFLOW_ID", "real-workflow");
  });

  afterEach(() => {
    vi.unstubAllEnvs();
    fs.rmSync(dir, { recursive: true, force: true });
  });

  it.each(invalidModels)("rejects unsafe identifiers %j before persistence and when reading metadata", model => {
    expect(validateModelIdentifier(model)).toBe("");
    const env = { GH_AW_INFO_MODEL: "primary" };
    expect(recordFallbackModelFromUsage(usage(model), env, infoPath)).toBe("");
    expect(env.GH_AW_INFO_MODEL).toBe("primary");
    expect(fs.existsSync(infoPath)).toBe(false);
    fs.writeFileSync(infoPath, JSON.stringify({ fallback_model: model, detection_fallback_model: model }));
    expect(getFallbackModel(infoPath)).toBe("");
    expect(getFallbackModel(infoPath, "detection")).toBe("");
  });

  it.each(validModels)("preserves valid identifiers %s in metadata and both footer implementations", model => {
    expect(validateModelIdentifier(model)).toBe(model);
    expect(recordFallbackModelFromUsage(usage(model), {}, infoPath)).toBe(model);
    expect(getFallbackModel(infoPath)).toBe(model);
    expect(JSON.parse(fs.readFileSync(infoPath, "utf8")).model).toBe(model);
    vi.stubEnv("GH_AW_ENGINE_MODEL", "primary");
    for (const footer of footers) {
      expect(footer.generateXMLMarker("Workflow", "https://github.com/run")).toContain(`model: ${model},`);
    }
    fs.rmSync(infoPath);
    vi.stubEnv("GH_AW_ENGINE_MODEL", model);
    for (const footer of footers) {
      expect(footer.generateXMLMarker("Workflow", "https://github.com/run")).toContain(`model: ${model},`);
    }
  });

  it("selects the latest valid telemetry entry and ignores malicious later entries", () => {
    const content = [usage("first"), usage("copilot/gpt-5.4"), ...invalidModels.map(usage)].join("\n");
    expect(recordFallbackModelFromUsage(content, {}, infoPath)).toBe("copilot/gpt-5.4");
    expect(getFallbackModel(infoPath)).toBe("copilot/gpt-5.4");
  });

  it("validates nested fallback model evidence too", () => {
    expect(recordFallbackModelFromUsage(JSON.stringify({ _schema: "token-usage/v0.28.44", model_fallback: { model: "gpt-5 -->" } }), {}, infoPath)).toBe("");
    expect(recordFallbackModelFromUsage(JSON.stringify({ _schema: "token-usage/v0.28.44", model_fallback: { model: "gpt-5.4" } }), {}, infoPath)).toBe("gpt-5.4");
  });

  it.each(["gpt-5\nworkflow_id: forged", "gpt-5, run: forged", "gpt-5 -->", "a".repeat(129)])("rejects forged routed model identifiers %j", model => {
    expect(recordModelRouting({ status: "selected", wire_model: model, effort: "high" }, {}, infoPath)).toBeNull();
    expect(fs.existsSync(infoPath)).toBe(false);
  });

  it.each(["high\nrouted: false", "high, workflow_id: forged", "a".repeat(129)])("rejects forged routed effort %j", effort => {
    expect(recordModelRouting({ status: "selected", wire_model: "gpt-5.4", effort }, {}, infoPath)).toBeNull();
    expect(fs.existsSync(infoPath)).toBe(false);
  });

  it("keeps a failed routed status but never promotes its model", () => {
    fs.writeFileSync(infoPath, JSON.stringify({ model: "auto" }));
    recordModelRouting({ status: "rejected", wire_model: "not-a-model", failure_code: "unsupported_effort" }, {}, infoPath);
    expect(JSON.parse(fs.readFileSync(infoPath, "utf8"))).toMatchObject({
      model: "auto",
      requested_model: "auto",
      model_routing: { status: "rejected", source: "awf-routing" },
    });
  });

  it("adds routed effort metadata to markers and retains only a safe failure status", () => {
    fs.writeFileSync(infoPath, JSON.stringify({ model: "auto" }));
    recordModelRouting({ status: "selected", wire_model: "gpt-5.6-luna", effort: "xhigh" }, {}, infoPath);
    for (const footer of footers) {
      expect(footer.generateXMLMarker("Workflow", "https://github.com/run")).toContain("model: gpt-5.6-luna, effort: xhigh, routed: true");
    }
    recordModelRouting({ status: "failed", failure_code: "no_route" }, {}, infoPath);
    for (const footer of footers) {
      expect(footer.generateXMLMarker("Workflow", "https://github.com/run")).toContain("model: auto, routed: failed");
      expect(footer.generateXMLMarker("Workflow", "https://github.com/run")).not.toContain("model: auto, engine:");
    }
  });

  it.each(invalidModels.filter(model => typeof model === "string" && !model.includes("\u0000")))("prevents footer marker breakout and spoofing from metadata or env: %j", model => {
    fs.writeFileSync(infoPath, JSON.stringify({ fallback_model: model }));
    vi.stubEnv("GH_AW_ENGINE_MODEL", model);
    for (const footer of footers) {
      const marker = footer.generateXMLMarker("Workflow", "https://github.com/run");
      expect(marker).not.toContain("model:");
      expect(marker.match(/<!--/g)).toHaveLength(1);
      expect(marker.match(/-->/g)).toHaveLength(1);
      expect(marker.match(/workflow_id:/g)).toHaveLength(1);
      expect(marker).toContain("workflow_id: real-workflow,");
    }
    vi.stubEnv("GH_AW_ENGINE_MODEL", "gpt-5.4");
    for (const footer of footers) {
      expect(footer.generateXMLMarker("Workflow", "https://github.com/run")).toContain("model: gpt-5.4,");
    }
  });
});
