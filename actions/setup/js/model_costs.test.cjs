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
  it.each([
    ["github-copilot", "gpt-5.6-luna-utility", [2e-7, 1.2e-6, 2e-8, 2.5e-7], 0.03805],
    ["github-copilot", "gpt-6.1-terra", [1e-6, 5e-6, 5e-8, 1.25e-6], 0.16825],
    ["openai", "gpt-6.1-terra", [1e-6, 5e-6, 5e-8, 1.25e-6], 0.16825],
  ])("prices bundled %s/%s with explicit pricing and routing metadata", async (provider, model, rates, expectedAIC) => {
    const catalog = JSON.parse(fs.readFileSync(new URL("./models.json", import.meta.url), "utf8"));
    const entry = catalog.providers[provider].models[model];
    expect(entry).toBeDefined();
    expect(entry.provider_type).toBe("openai");
    expect(entry.wire_api).toBe("responses");
    expect(["input", "output", "cache_read", "cache_write"].map(key => Number(entry.cost[key]))).toEqual(rates);

    const { computeInferenceAIC } = await import("./model_costs.cjs");
    for (const pricingProvider of provider === "github-copilot" ? [provider, "copilot", "github_models"] : [provider]) {
      expect(
        computeInferenceAIC({
          provider: pricingProvider,
          model,
          inputTokens: 1000,
          outputTokens: 200,
          cacheReadTokens: 400,
          cacheWriteTokens: 50,
        })
      ).toBeCloseTo(expectedAIC, 10);
    }
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

  it("falls back to bundled models.json when GH_AW_MODELS_JSON_PATH points to a non-existent file", async () => {
    // Simulate the detection/evals job scenario: GH_AW_MODELS_JSON_PATH is set to
    // /tmp/gh-aw/models.json but that file was never downloaded from the activation artifact.
    process.env.GH_AW_MODELS_JSON_PATH = "/tmp/gh-aw-test-nonexistent-path/models.json";
    const { loadModelsJson } = await import("./model_costs.cjs");
    const result = loadModelsJson();
    // Should fall back to the bundled models.json and return a valid catalog, not null.
    expect(result).not.toBeNull();
    expect(typeof result).toBe("object");
    expect(result).toHaveProperty("providers");
  });
});
