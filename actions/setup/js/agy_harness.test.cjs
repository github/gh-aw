import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "module";
import fs from "fs";
import os from "os";
import path from "path";

const require = createRequire(import.meta.url);
const { resolveAgyModel } = require("./agy_harness.cjs");
const reflectFixtures = require("./fixtures/model_resolution_reflect.cjs");

// The recorded /reflect from run 38945917145 did not configure Gemini, so these tests derive a
// configured Gemini endpoint from it (withGeminiModels); the alias map is the recorded one.
const geminiModels = ["gemini-3.5-pro", "gemini-3.8-flash", "gemini-3.9-flash"];

describe("agy_harness.cjs resolveAgyModel", () => {
  let tmpDir;
  let env;
  beforeEach(() => {
    tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "agy-model-resolution-"));
    env = { GH_AW_TMP_DIR: tmpDir, GH_AW_AWF_CONFIG_PATH: path.join(tmpDir, "missing-awf-config.json") };
  });
  afterEach(() => fs.rmSync(tmpDir, { recursive: true, force: true }));
  const records = () => JSON.parse(fs.readFileSync(path.join(tmpDir, "agent/model-resolution.json"), "utf8"));
  const resolve = (model, reflectData, options = {}) => resolveAgyModel({ model, reflectData, env, logger: () => {}, ...options });
  const withAliases = (reflect, aliases) => ({ ...reflect, model_aliases: { ...reflect.model_aliases, ...aliases } });

  it("passes an exact slug unchanged", async () => {
    expect(await resolve("gemini-3.8-flash", reflectFixtures.withGeminiModels(geminiModels))).toBe("gemini-3.8-flash");
    expect(await resolve("gemini-9-unlisted", reflectFixtures.withGeminiModels(geminiModels))).toBe("gemini-9-unlisted");
    expect(records().at(-1)).toEqual({ engine: "agy", role: "main", configured_model: "gemini-9-unlisted", resolved_model: "gemini-9-unlisted", provider: "gemini", status: "exact", cause: null });
  });

  it("resolves an alias to a native Gemini slug", async () => {
    const reflect = withAliases(reflectFixtures.withGeminiModels(geminiModels), { flash: ["gemini/gemini-*-flash"] });
    const logs = [];
    expect(await resolve("flash", reflect, { logger: message => logs.push(message) })).toBe("gemini-3.9-flash");
    expect(await resolve("gemini/flash", reflect)).toBe("gemini-3.9-flash");
    expect(logs.filter(line => line.startsWith("model resolution: engine=agy"))).toEqual([expect.stringContaining("configured=flash resolved=gemini-3.9-flash")]);
    expect(records()).toEqual([{ engine: "agy", role: "main", configured_model: "gemini/flash", resolved_model: "gemini-3.9-flash", provider: "gemini", status: "resolved", cause: null }]);
  });

  // The recorded alias map predates the built-in gemini-3.8-flash alias, so the test adds the
  // built-in definition; gemini-3.8-flash is then both an alias and a live slug.
  it("a live exact slug wins over an alias with the same name", async () => {
    const aliases = { "gemini-3.8-flash": ["copilot/gemini-3.8*flash*", "google/gemini-3.8*flash*", "gemini/gemini-3.8*flash*"] };
    const live = withAliases(reflectFixtures.withGeminiModels(["gemini-3.8-flash-lite", ...geminiModels]), aliases);
    expect(await resolve("gemini-3.8-flash", live)).toBe("gemini-3.8-flash");
    expect(records().at(-1)).toEqual({ engine: "agy", role: "main", configured_model: "gemini-3.8-flash", resolved_model: "gemini-3.8-flash", provider: "gemini", status: "exact", cause: null });
    const absent = withAliases(reflectFixtures.withGeminiModels(["gemini-3.5-pro", "gemini-3.8-flash-lite"]), aliases);
    expect(await resolve("gemini-3.8-flash", absent)).toBe("gemini-3.8-flash-lite");
  });

  it("rejects provider/model syntax after resolution", async () => {
    await expect(resolve("gemini/gemini-3.8-flash", reflectFixtures.withGeminiModels(geminiModels))).rejects.toThrow("Agy requires a native model slug");
  });

  it("workflow-defined alias overrides built-in", async () => {
    const reflect = reflectFixtures.withGeminiModels(geminiModels);
    expect(await resolve("any", reflect)).toBe("gemini-3.9-flash");
    expect(await resolve("any", withAliases(reflect, { any: ["gemini/gemini-3.8*"] }))).toBe("gemini-3.8-flash");
  });

  // Incomplete discovery is derived from the recorded file (models_fetch_complete: false);
  // the Gemini endpoint is configured but has no model list. No recorded run has this shape.
  it("incomplete discovery passes exact ids through and fails aliases after one refetch", async () => {
    const incomplete = () => ({ ...reflectFixtures.withGeminiModels(null), models_fetch_complete: false });
    expect(await resolve("gemini-3.8-flash", incomplete())).toBe("gemini-3.8-flash");
    const refetchReflectData = vi.fn().mockResolvedValue(incomplete());
    await expect(resolve("any", incomplete(), { refetchReflectData })).rejects.toMatchObject({ name: "ModelAliasResolutionError", reason: "catalog_unavailable" });
    expect(refetchReflectData).toHaveBeenCalledTimes(1);
    expect(records().at(-1)).toEqual({ engine: "agy", role: "main", configured_model: "any", resolved_model: null, provider: "gemini", status: "failed", cause: "catalog_unavailable" });
  });

  it("does not use the Copilot integration sentence for Gemini failures", async () => {
    const reflect = withAliases(reflectFixtures.withGeminiModels(geminiModels), { flash: ["gemini/gemini-*-ultra"] });
    const error = await resolve("flash", reflect).catch(err => err);
    expect(error).toMatchObject({ name: "ModelAliasResolutionError", reason: "model_unavailable" });
    expect(error.message).toContain("The requested model is not supported");
    expect(error.message).not.toContain("agentic-workflows integration");
  });
});
