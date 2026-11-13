// @ts-check

import fs from "fs";
import os from "os";
import path from "path";
import { afterEach, describe, expect, it } from "vitest";

const tmpDirs = [];

afterEach(async () => {
  delete process.env.GH_AW_MODELS_JSON_PATH;
  const { _resetModelCostsCache } = await import("./model_costs.cjs");
  _resetModelCostsCache();
  for (const dir of tmpDirs.splice(0)) {
    fs.rmSync(dir, { recursive: true, force: true });
  }
});

function writeModelsFixture(providers) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gh-aw-model-costs-"));
  tmpDirs.push(dir);
  const file = path.join(dir, "models.json");
  fs.writeFileSync(file, JSON.stringify({ providers }, null, 2));
  process.env.GH_AW_MODELS_JSON_PATH = file;
}

describe("model_costs.cjs", () => {
  it("does not supply default pricing or metadata without an explicit overlay", async () => {
    const { getModelsPath, loadModelsJson, loadCatalog, findModelPricing, computeInferenceAIC } = await import("./model_costs.cjs");
    expect(getModelsPath()).toBeNull();
    expect(loadModelsJson()).toBeNull();
    expect(loadCatalog()).toEqual([]);
    expect(findModelPricing("copilot", "gpt-5-mini")).toBeNull();
    expect(computeInferenceAIC({ provider: "copilot", model: "gpt-5-mini", inputTokens: 1000, outputTokens: 200, cacheReadTokens: 0, cacheWriteTokens: 0 })).toBe(0);
  });

  it("preserves explicit routing metadata alongside pricing", async () => {
    const entry = { provider_type: "openai", wire_api: "responses", cost: { input: "0.000002", output: "0.000004" } };
    writeModelsFixture({ custom: { models: { "custom-model": entry } } });
    const { loadModelsJson, computeInferenceAIC } = await import("./model_costs.cjs");
    expect(loadModelsJson().providers.custom.models["custom-model"]).toEqual(entry);
    expect(computeInferenceAIC({ provider: "custom", model: "custom-model", inputTokens: 1000, outputTokens: 200, cacheReadTokens: 0, cacheWriteTokens: 0 })).toBeCloseTo(0.28);
  });

  it("computes inference AIC using provider-specific pricing", async () => {
    writeModelsFixture({
      anthropic: {
        models: {
          "claude-sonnet-4.6": {
            cost: {
              input: "0.000003",
              output: "0.000015",
              cache_read: "0.0000003",
              cache_write: "0.00000375",
              reasoning: "0.000015",
            },
          },
        },
      },
    });

    const { computeInferenceAIC } = await import("./model_costs.cjs");
    const aic = computeInferenceAIC({
      provider: "anthropic",
      model: "claude-sonnet-4.6-20250514",
      inputTokens: 1000,
      outputTokens: 200,
      cacheReadTokens: 400,
      cacheWriteTokens: 50,
      reasoningTokens: 25,
    });

    expect(aic).toBeCloseTo(0.54825, 6);
  });

  it("formats AI Credits with their existing precision", async () => {
    const { formatAIC } = await import("./model_costs.cjs");
    expect(formatAIC(0.125)).toBe("0.125");
    expect(formatAIC(1.25)).toBe("1.25");
    expect(formatAIC(12.5)).toBe("12.5");
  });

  it.each([
    [0.33 + 0.56 + 0.11, "~1"],
    [0.66 + 1.12 + 0.22, "~2"],
    [1.000000000001, "~2"],
    [Number.MIN_VALUE, "~1"],
    [0.000000000001, "~1"],
    [12.4, "~13"],
    [1000.1, "~1001"],
    [1000000.1, "~1000001"],
    [0, ""],
    [-1, ""],
    [NaN, ""],
    [Infinity, ""],
  ])("formats %s as the whole-credit estimate %s", async (value, formatted) => {
    const { formatEstimatedAIC } = await import("./model_costs.cjs");
    expect(formatEstimatedAIC(value)).toBe(formatted);
  });

  it("resolves 'copilot' provider alias to 'github-copilot' for AIC lookup", async () => {
    writeModelsFixture({
      "github-copilot": {
        models: {
          "claude-sonnet-4.6": {
            cost: {
              input: "0.000003",
              output: "0.000015",
              cache_read: "0.0000003",
            },
          },
        },
      },
    });

    const { computeInferenceAIC } = await import("./model_costs.cjs");

    // provider="copilot" should be treated as "github-copilot"
    const aicViaProvider = computeInferenceAIC({
      provider: "copilot",
      model: "claude-sonnet-4.6",
      inputTokens: 1000,
      outputTokens: 100,
      cacheReadTokens: 0,
      cacheWriteTokens: 0,
    });
    expect(aicViaProvider).toBeGreaterThan(0);

    // model="copilot/claude-sonnet-4.6" (embedded provider prefix) should also resolve
    const aicViaEmbedded = computeInferenceAIC({
      provider: "",
      model: "copilot/claude-sonnet-4.6",
      inputTokens: 1000,
      outputTokens: 100,
      cacheReadTokens: 0,
      cacheWriteTokens: 0,
    });
    expect(aicViaEmbedded).toBeGreaterThan(0);
    expect(aicViaEmbedded).toBeCloseTo(aicViaProvider, 6);
  });

  it("resolves 'github_models' provider alias to 'github-copilot' for AIC lookup", async () => {
    writeModelsFixture({
      "github-copilot": {
        models: {
          "gpt-5-mini": {
            cost: {
              input: "0.00000025",
              output: "0.000002",
            },
          },
        },
      },
    });

    const { computeInferenceAIC } = await import("./model_costs.cjs");

    // provider="github_models" (written by the AWF proxy for Copilot engine runs)
    // should be treated as "github-copilot" so AIC is computed and emitted
    const aic = computeInferenceAIC({
      provider: "github_models",
      model: "gpt-5-mini",
      inputTokens: 1000,
      outputTokens: 100,
      cacheReadTokens: 0,
      cacheWriteTokens: 0,
    });
    expect(aic).toBeGreaterThan(0);
  });

  it("subtracts cache reads from input for github-copilot provider (§3.5, no double-charge)", async () => {
    // github-copilot proxies OpenAI and Anthropic models, which bundle cache-read tokens
    // inside the reported input total.  §3.5 requires the cache reads to be subtracted
    // from input_tokens before the input price is applied, preventing double-charging.
    //
    // Pricing used in this fixture:
    //   input:      $0.000003 / token
    //   output:     $0.000015 / token
    //   cache_read: $0.0000003 / token
    //
    // With 1000 input, 200 output, 400 cache_read:
    //   net input = 1000 − 400 = 600
    //   cost = 600×0.000003 + 200×0.000015 + 400×0.0000003 = 0.0018 + 0.003 + 0.00012 = 0.00492
    //   AIC  = 0.00492 / 0.01 = 0.492
    writeModelsFixture({
      "github-copilot": {
        models: {
          "claude-sonnet-4.6": {
            cost: {
              input: "0.000003",
              output: "0.000015",
              cache_read: "0.0000003",
            },
          },
        },
      },
    });

    const { computeInferenceAIC } = await import("./model_costs.cjs");

    for (const provider of ["github-copilot", "github_models", "github", "copilot"]) {
      const aic = computeInferenceAIC({
        provider,
        model: "claude-sonnet-4.6",
        inputTokens: 1000,
        outputTokens: 200,
        cacheReadTokens: 400,
        cacheWriteTokens: 0,
      });
      expect(aic).toBeCloseTo(0.492, 6);
    }
  });

  it("clamps net input to 0 when cache_read exceeds input (§3.5 boundary)", async () => {
    writeModelsFixture({
      "github-copilot": {
        models: {
          "claude-sonnet-4.6": {
            cost: {
              input: "0.000003",
              output: "0.000015",
              cache_read: "0.0000003",
            },
          },
        },
      },
    });

    const { computeInferenceAIC } = await import("./model_costs.cjs");
    const aic = computeInferenceAIC({
      provider: "github-copilot",
      model: "claude-sonnet-4.6",
      inputTokens: 1000,
      outputTokens: 200,
      cacheReadTokens: 1200,
      cacheWriteTokens: 0,
    });

    // net input = max(1000 − 1200, 0) = 0
    // cost = 0×0.000003 + 200×0.000015 + 1200×0.0000003 = 0.00336
    // AIC  = 0.00336 / 0.01 = 0.336
    expect(aic).toBeCloseTo(0.336, 6);
  });

  it("uses explicit inclusive cache semantics when present", async () => {
    writeModelsFixture({
      "github-copilot": {
        models: {
          "gpt-4o-mini": {
            cost: {
              input: "0.00000015",
              output: "0.0000006",
              cache_read: "0.000000075",
            },
          },
        },
      },
    });

    const { computeInferenceAIC } = await import("./model_costs.cjs");
    const aic = computeInferenceAIC({
      provider: "copilot",
      model: "gpt-4o-mini",
      inputTokens: 1000,
      outputTokens: 100,
      cacheReadTokens: 400,
      cacheWriteTokens: 100,
      inputTokensIncludeCache: true,
    });

    expect(aic).toBeCloseTo(0.018, 6);
  });

  it("uses explicit additive cache semantics when present", async () => {
    writeModelsFixture({
      "github-copilot": {
        models: {
          "gpt-4o-mini": {
            cost: {
              input: "0.00000015",
              output: "0.0000006",
              cache_read: "0.000000075",
            },
          },
        },
      },
    });

    const { computeInferenceAIC } = await import("./model_costs.cjs");
    const aic = computeInferenceAIC({
      provider: "copilot",
      model: "gpt-4o-mini",
      inputTokens: 1000,
      outputTokens: 100,
      cacheReadTokens: 400,
      cacheWriteTokens: 100,
      inputTokensIncludeCache: false,
    });

    expect(aic).toBeCloseTo(0.0255, 6);
  });

  it.each(["missing-models.json", ""])("does not substitute a catalog for an unavailable override %s", async filename => {
    process.env.GH_AW_MODELS_JSON_PATH = filename ? path.join(import.meta.dirname, filename) : " ";
    const { loadModelsJson, loadCatalog } = await import("./model_costs.cjs");
    expect(loadModelsJson()).toBeNull();
    expect(loadCatalog()).toEqual([]);
  });
});
