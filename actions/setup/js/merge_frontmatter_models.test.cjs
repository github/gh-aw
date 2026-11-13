// @ts-check

import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import fs from "fs";
import path from "path";
import os from "os";

const mockCore = {
  info: vi.fn(),
  warning: vi.fn(),
};

describe("merge_frontmatter_models.cjs", () => {
  let mergeModelCosts;
  let writeMergedModelsJSON;
  let isPlainObject;
  let MERGED_MODELS_JSON_PATH;
  /** @type {string[]} */
  const tmpDirs = [];

  beforeEach(async () => {
    vi.clearAllMocks();
    const mod = await import("./merge_frontmatter_models.cjs");
    mergeModelCosts = mod.mergeModelCosts;
    const outputDir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-models-output-"));
    tmpDirs.push(outputDir);
    MERGED_MODELS_JSON_PATH = path.join(outputDir, "models.json");
    writeMergedModelsJSON = core => mod.writeMergedModelsJSON(core, MERGED_MODELS_JSON_PATH);
    isPlainObject = mod.isPlainObject;
  });

  afterEach(() => {
    delete process.env.GH_AW_INFO_MODEL_COSTS;
    delete process.env.GH_AW_MODELS_JSON_SRC_PATH;
    for (const dir of tmpDirs.splice(0)) {
      fs.rmSync(dir, { recursive: true, force: true });
    }
  });

  // ──────────────────────────────────────────────────────────────────────────
  // isPlainObject
  // ──────────────────────────────────────────────────────────────────────────

  describe("isPlainObject", () => {
    it("returns true for plain objects", () => {
      expect(isPlainObject({})).toBe(true);
      expect(isPlainObject({ a: 1 })).toBe(true);
    });

    it("returns false for non-objects", () => {
      expect(isPlainObject(null)).toBe(false);
      expect(isPlainObject("string")).toBe(false);
      expect(isPlainObject(42)).toBe(false);
      expect(isPlainObject([])).toBe(false);
      expect(isPlainObject(undefined)).toBe(false);
    });
  });

  // ──────────────────────────────────────────────────────────────────────────
  // mergeModelCosts
  // ──────────────────────────────────────────────────────────────────────────

  describe("mergeModelCosts", () => {
    it("returns base unchanged when overlay has no providers", () => {
      const base = { providers: { anthropic: { models: { "claude-opus": { cost: { input: "1e-5" } } } } } };
      const overlay = {};
      const result = mergeModelCosts(base, overlay);
      expect(result).toEqual(base);
    });

    it("adds a new provider from overlay", () => {
      const base = { providers: { anthropic: { models: { "claude-opus": { cost: { input: "1e-5" } } } } } };
      const overlay = { providers: { openai: { models: { "gpt-5": { cost: { input: "5e-6" } } } } } };
      const result = mergeModelCosts(base, overlay);
      expect(result.providers).toHaveProperty("anthropic");
      expect(result.providers).toHaveProperty("openai");
      expect(result.providers["openai"]).toEqual({ models: { "gpt-5": { cost: { input: "5e-6" } } } });
    });

    it("overrides an existing model within a shared provider", () => {
      const base = {
        providers: {
          anthropic: {
            models: {
              "claude-opus": { cost: { input: "1e-5", output: "3e-5" } },
              "claude-haiku": { cost: { input: "2e-7" } },
            },
          },
        },
      };
      const overlay = {
        providers: {
          anthropic: {
            models: {
              "claude-opus": { cost: { input: "9e-6", output: "2.5e-5" } },
            },
          },
        },
      };
      const result = mergeModelCosts(base, overlay);
      const anthropic = result.providers["anthropic"];
      expect(anthropic.models["claude-opus"]).toEqual({ cost: { input: "9e-6", output: "2.5e-5" } });
      // haiku must be preserved from base
      expect(anthropic.models["claude-haiku"]).toEqual({ cost: { input: "2e-7" } });
    });

    it("fills in a new model within an existing provider", () => {
      const base = {
        providers: {
          anthropic: {
            models: {
              "claude-opus": { cost: { input: "1e-5" } },
            },
          },
        },
      };
      const overlay = {
        providers: {
          anthropic: {
            models: {
              "my-custom-model": { cost: { input: "2e-6", output: "8e-6" } },
            },
          },
        },
      };
      const result = mergeModelCosts(base, overlay);
      const models = result.providers["anthropic"].models;
      expect(models).toHaveProperty("claude-opus");
      expect(models).toHaveProperty("my-custom-model");
      expect(models["my-custom-model"]).toEqual({ cost: { input: "2e-6", output: "8e-6" } });
    });

    it("handles base with no providers key", () => {
      const base = {};
      const overlay = { providers: { openai: { models: { "gpt-5": { cost: {} } } } } };
      const result = mergeModelCosts(base, overlay);
      expect(result.providers["openai"].models["gpt-5"]).toEqual({ cost: {} });
    });

    it("handles overlay with empty providers object", () => {
      const base = { providers: { anthropic: { models: {} } } };
      const overlay = { providers: {} };
      const result = mergeModelCosts(base, overlay);
      expect(result.providers["anthropic"]).toBeDefined();
    });

    it("preserves non-providers top-level keys from base", () => {
      const base = { version: "1.0", providers: {} };
      const overlay = { providers: {} };
      const result = mergeModelCosts(base, overlay);
      expect(result.version).toBe("1.0");
    });

    it("merges multiple providers in one call", () => {
      const base = {
        providers: {
          anthropic: { models: { "claude-opus": { cost: { input: "1e-5" } } } },
          openai: { models: { "gpt-4": { cost: { input: "3e-5" } } } },
        },
      };
      const overlay = {
        providers: {
          anthropic: { models: { "custom-claude": { cost: { input: "5e-6" } } } },
          openai: { models: { "gpt-4": { cost: { input: "2e-5" } } } },
        },
      };
      const result = mergeModelCosts(base, overlay);
      expect(result.providers["anthropic"].models).toHaveProperty("claude-opus");
      expect(result.providers["anthropic"].models).toHaveProperty("custom-claude");
      expect(result.providers["openai"].models["gpt-4"]).toEqual({ cost: { input: "2e-5" } });
    });
  });

  // ──────────────────────────────────────────────────────────────────────────
  // writeMergedModelsJSON
  // ──────────────────────────────────────────────────────────────────────────

  describe("writeMergedModelsJSON", () => {
    /** @returns {string} path to temp dir with a models.json */
    function makeTempModelsDir(content) {
      const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-models-"));
      tmpDirs.push(dir);
      fs.writeFileSync(path.join(dir, "models.json"), JSON.stringify(content));
      return dir;
    }

    it("writes the merged file to MERGED_MODELS_JSON_PATH", () => {
      const base = { providers: { anthropic: { models: { "claude-opus": { cost: { input: "1e-5" } } } } } };
      const dir = makeTempModelsDir(base);
      process.env.GH_AW_MODELS_JSON_SRC_PATH = path.join(dir, "models.json");

      writeMergedModelsJSON(mockCore);

      expect(fs.existsSync(MERGED_MODELS_JSON_PATH)).toBe(true);
      const written = JSON.parse(fs.readFileSync(MERGED_MODELS_JSON_PATH, "utf8"));
      expect(written.providers["anthropic"].models["claude-opus"]).toEqual({ cost: { input: "1e-5" } });
    });

    it("merges overlay from GH_AW_INFO_MODEL_COSTS into base", () => {
      const base = { providers: { anthropic: { models: { "claude-opus": { cost: { input: "1e-5" } } } } } };
      const overlay = { providers: { openai: { models: { "gpt-5": { cost: { input: "5e-6" } } } } } };
      const dir = makeTempModelsDir(base);
      process.env.GH_AW_MODELS_JSON_SRC_PATH = path.join(dir, "models.json");
      process.env.GH_AW_INFO_MODEL_COSTS = JSON.stringify(overlay);

      writeMergedModelsJSON(mockCore);

      const written = JSON.parse(fs.readFileSync(MERGED_MODELS_JSON_PATH, "utf8"));
      expect(written.providers["anthropic"]).toBeDefined();
      expect(written.providers["openai"].models["gpt-5"]).toEqual({ cost: { input: "5e-6" } });
    });

    it("warns and uses empty base when base file is missing", () => {
      process.env.GH_AW_MODELS_JSON_SRC_PATH = "/nonexistent/models.json";

      writeMergedModelsJSON(mockCore);

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("not found"));
      const written = JSON.parse(fs.readFileSync(MERGED_MODELS_JSON_PATH, "utf8"));
      expect(written).toEqual({});
    });

    it("warns and ignores non-object GH_AW_INFO_MODEL_COSTS", () => {
      const base = { providers: {} };
      const dir = makeTempModelsDir(base);
      process.env.GH_AW_MODELS_JSON_SRC_PATH = path.join(dir, "models.json");
      process.env.GH_AW_INFO_MODEL_COSTS = '["not-an-object"]';

      writeMergedModelsJSON(mockCore);

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("JSON object"));
      const written = JSON.parse(fs.readFileSync(MERGED_MODELS_JSON_PATH, "utf8"));
      expect(written).toEqual(base);
    });

    it("warns and ignores invalid JSON in GH_AW_INFO_MODEL_COSTS", () => {
      const base = { providers: {} };
      const dir = makeTempModelsDir(base);
      process.env.GH_AW_MODELS_JSON_SRC_PATH = path.join(dir, "models.json");
      process.env.GH_AW_INFO_MODEL_COSTS = "not-json";

      writeMergedModelsJSON(mockCore);

      expect(mockCore.warning).toHaveBeenCalledWith(expect.stringContaining("Failed to parse GH_AW_INFO_MODEL_COSTS"));
    });

    it("frontier overlay overrides an existing model's cost", () => {
      const base = {
        providers: {
          anthropic: { models: { "claude-opus": { cost: { input: "1e-5", output: "3e-5" } } } },
        },
      };
      const overlay = {
        providers: {
          anthropic: { models: { "claude-opus": { cost: { input: "9e-6" } } } },
        },
      };
      const dir = makeTempModelsDir(base);
      process.env.GH_AW_MODELS_JSON_SRC_PATH = path.join(dir, "models.json");
      process.env.GH_AW_INFO_MODEL_COSTS = JSON.stringify(overlay);

      writeMergedModelsJSON(mockCore);

      const written = JSON.parse(fs.readFileSync(MERGED_MODELS_JSON_PATH, "utf8"));
      expect(written.providers["anthropic"].models["claude-opus"]).toEqual({ cost: { input: "9e-6" } });
    });

    it("writes only explicit frontmatter overrides when no source is configured", () => {
      const overlay = { providers: { custom: { models: { example: { cost: { input: "2e-6" }, wire_api: "responses" } } } } };
      process.env.GH_AW_INFO_MODEL_COSTS = JSON.stringify(overlay);
      writeMergedModelsJSON(mockCore);
      expect(mockCore.warning).not.toHaveBeenCalled();
      expect(JSON.parse(fs.readFileSync(MERGED_MODELS_JSON_PATH, "utf8"))).toEqual(overlay);
    });

    it("writes an empty overlay without warning when no overrides are configured", () => {
      writeMergedModelsJSON(mockCore);
      expect(mockCore.warning).not.toHaveBeenCalled();
      expect(JSON.parse(fs.readFileSync(MERGED_MODELS_JSON_PATH, "utf8"))).toEqual({});
    });
  });
});
