// @ts-check
import { describe, it, expect } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const fs = require("fs");
const path = require("path");
const { fileURLToPath } = require("url");
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const {
  buildCatalogFromReflect,
  globMatch,
  isBundledModelId,
  loadBuiltinModelAliases,
  loadModelAliasMap,
  ModelAliasResolutionError,
  normalizeForCopilotCLI,
  resolveConfiguredCopilotModel,
  resolveEngineModel,
  resolveModelAlias,
  selectLatestGlobMatch,
} = require("./resolve_model_alias.cjs");
const { detectErrors } = require("./agent_error_patterns.cjs");

const ALIAS_MAP = JSON.parse(fs.readFileSync(path.join(__dirname, "../../../pkg/workflow/data/model_aliases.json"), "utf8")).aliases;

describe("resolve_model_alias", () => {
  const catalog = ["copilot/claude-haiku-4.5", "copilot/claude-sonnet-4.6", "copilot/gpt-5-mini", "copilot/gpt-5-nano", "copilot/gemini-2.5-flash-lite"];

  it("matches provider-scoped glob patterns case-insensitively", () => {
    expect(globMatch("copilot/*haiku*", "copilot/claude-haiku-4.5")).toBe(true);
    expect(globMatch("copilot/*haiku*", "anthropic/claude-haiku-4.5")).toBe(false);
  });

  it("selects the highest-version glob match", () => {
    const matches = selectLatestGlobMatch("copilot/*sonnet*", ["copilot/claude-sonnet-4.5", "copilot/claude-sonnet-4.6"]);
    expect(matches).toBe("copilot/claude-sonnet-4.6");
  });

  it.each(["auto", "gpt-5.6", "agent", "any"])("excludes Copilot utility variants from %s regardless of catalog ordering", configuredModel => {
    const models = ["gpt-5.6-luna-utility", "gpt-5.6-luna", "gpt-5.7-utility"];
    for (const ordered of [models, [...models].reverse()]) {
      expect(
        resolveConfiguredCopilotModel({
          configuredModel,
          aliasMap: ALIAS_MAP,
          reflectData: { endpoints: [{ configured: true, provider: "copilot", models: ordered }] },
        })
      ).toBe("gpt-5.6-luna");
    }
  });

  it("does not select a utility model when no agent model is available", () => {
    expect(() =>
      resolveConfiguredCopilotModel({
        configuredModel: "auto",
        aliasMap: ALIAS_MAP,
        reflectData: { endpoints: [{ configured: true, provider: "copilot", models: ["gpt-5.6-luna-utility"] }] },
      })
    ).toThrow(expect.objectContaining({ reason: "model_unavailable" }));
    expect(selectLatestGlobMatch("*", ["gpt-5.6-luna-utility"])).toBeNull();
  });

  it.each([{ models: [] }, { models: ["gpt-5.6-luna"] }])("reports an unavailable model, not catalog retrieval failure, when no eligible models remain in $models", async ({ models }) => {
    await expect(
      resolveEngineModel({
        engine: "copilot",
        provider: "copilot",
        configuredModel: "auto",
        aliasMap: ALIAS_MAP,
        reflectData: { endpoints: [{ configured: true, provider: "copilot", models, routing_models: [{ model_id: "gpt-5.6-luna", model_picker_enabled: false }] }] },
        record: false,
      })
    ).rejects.toMatchObject({ reason: "model_unavailable" });
  });

  it("preserves explicit utility pins and non-Copilot wildcard behavior", () => {
    expect(
      resolveConfiguredCopilotModel({
        configuredModel: "gpt-5.6-luna-utility",
        aliasMap: ALIAS_MAP,
        reflectData: null,
      })
    ).toBe("gpt-5.6-luna-utility");
    expect(selectLatestGlobMatch("openai/*", ["openai/example-utility"])).toBe("openai/example-utility");
  });

  it.each(["copilot", "github-copilot", "github"])("honors picker and policy metadata for %s aliases", async provider => {
    const routing_models = [
      { model_id: "gpt-5.9", model_picker_enabled: false },
      { model_id: "gpt-5.8", policy: { state: "disabled" } },
      { model_id: "gpt-5.7", policy: { state: "unconfigured" } },
      { model_id: "gpt-5.6-luna", model_picker_enabled: true, policy: { state: "enabled" } },
    ];
    for (const metadata of [{ routing_models }, {}]) {
      const reflectData = {
        ...metadata,
        endpoints: [{ configured: true, provider, models: routing_models.map(model => model.model_id), ...(!metadata.routing_models && { routing_models }) }],
      };
      expect((await resolveEngineModel({ engine: "copilot", provider: "copilot", configuredModel: "auto", aliasMap: ALIAS_MAP, reflectData, record: false })).modelId).toBe("gpt-5.6-luna");
    }
  });

  it.each([
    [["claude-haiku-4.5", "gpt-5.6-luna"], "claude-haiku-4.5"],
    [["gpt-5.6-mini", "gpt-5.6-luna"], "gpt-5.6-mini"],
    [["gemini-3.8-flash", "gpt-5.6-luna"], "gemini-3.8-flash"],
    [["gpt-5.6-luna-utility", "gpt-5.6-luna"], "gpt-5.6-luna"],
  ])("resolves detection against the live catalog %j", (models, expected) => {
    expect(
      resolveConfiguredCopilotModel({
        configuredModel: "detection",
        aliasMap: ALIAS_MAP,
        reflectData: { endpoints: [{ configured: true, provider: "copilot", models }] },
      })
    ).toBe(expected);
  });

  it("resolves small -> mini -> haiku glob chain", () => {
    const resolved = resolveModelAlias("small", ALIAS_MAP, catalog);
    expect(resolved).toBe("copilot/claude-haiku-4.5");
  });

  it("resolves gpt-6 to GPT-6.1 Sol when available", () => {
    const resolved = resolveModelAlias("gpt-6", ALIAS_MAP, ["copilot/gpt-6-sol", "copilot/gpt-6.1-sol"]);
    expect(resolved).toBe("copilot/gpt-6.1-sol");
  });

  it("strips copilot/ prefix for native CLI env var", () => {
    expect(normalizeForCopilotCLI("copilot/claude-haiku-4.5")).toBe("claude-haiku-4.5");
  });

  it("resolveConfiguredCopilotModel rewrites alias when reflect catalog is available", () => {
    const reflectData = {
      endpoints: [
        {
          configured: true,
          provider: "copilot",
          models: ["claude-haiku-4.5", "gpt-5-mini"],
        },
      ],
    };

    const resolved = resolveConfiguredCopilotModel({
      configuredModel: "small",
      aliasMap: ALIAS_MAP,
      reflectData,
    });
    expect(resolved).toBe("claude-haiku-4.5");
  });

  it("leaves concrete configured models unchanged", () => {
    const reflectData = {
      endpoints: [{ configured: true, provider: "copilot", models: ["claude-sonnet-4.6"] }],
    };
    const resolved = resolveConfiguredCopilotModel({
      configuredModel: "claude-sonnet-4.6",
      aliasMap: ALIAS_MAP,
      reflectData,
    });
    expect(resolved).toBe("claude-sonnet-4.6");
  });

  it.each(["smoke-copilot-mai", "actions-aw-linter-factory"])("resolves %s against the advertised MAI catalog", workflowName => {
    const workflow = fs.readFileSync(path.join(__dirname, `../../../.github/workflows/${workflowName}.md`), "utf8");
    const configuredModel = workflow.match(/^model: (.+)$/m)?.[1];
    if (!configuredModel) throw new Error(`Missing model in ${workflowName}`);
    expect(configuredModel).toMatch(/^(copilot\/)?mai-code-1\.1-flash$/);
    const reflectData = {
      models_fetch_complete: true,
      endpoints: [{ configured: true, provider: "copilot", models: ["mai-code-1.1-flash"] }],
    };
    expect(resolveConfiguredCopilotModel({ configuredModel, aliasMap: ALIAS_MAP, reflectData })).toBe("mai-code-1.1-flash");
    expect(ALIAS_MAP).not.toHaveProperty("mai-code-1-flash-picker");
  });

  it("resolves Linter Miner main and sub-agent models against its advertised catalog", () => {
    const workflow = fs.readFileSync(path.join(__dirname, "../../../.github/workflows/linter-miner.md"), "utf8");
    const models = [...workflow.matchAll(/^model: (.+)$/gm)].map(match => match[1]);
    const reflectData = {
      endpoints: [{ configured: true, provider: "copilot", models: ["claude-haiku-4.5", "claude-haiku-5.5", "claude-opus-5.5", "claude-sonnet-5", "claude-sonnet-5.5"] }],
    };
    expect(models).toHaveLength(4);
    for (const configuredModel of models) {
      const resolved = resolveConfiguredCopilotModel({ configuredModel, aliasMap: ALIAS_MAP, reflectData });
      expect(reflectData.endpoints[0].models).toContain(resolved);
    }
  });

  it("throws ModelAliasResolutionError for a known alias when the catalog is empty", () => {
    const logs = [];
    expect(() =>
      resolveConfiguredCopilotModel({
        configuredModel: "small",
        aliasMap: ALIAS_MAP,
        reflectData: null,
        logger: msg => logs.push(msg),
      })
    ).toThrow(ModelAliasResolutionError);
    expect(logs.some(l => l.includes("catalog unavailable") && l.includes("small"))).toBe(true);
  });

  it("throws ModelAliasResolutionError for a known alias when reflect endpoints have no models", () => {
    const reflectData = { endpoints: [{ configured: true, provider: "copilot", models: [] }] };
    expect(() =>
      resolveConfiguredCopilotModel({
        configuredModel: "small",
        aliasMap: ALIAS_MAP,
        reflectData,
      })
    ).toThrow(ModelAliasResolutionError);
  });

  it("throws ModelAliasResolutionError for a known alias when catalog is populated but does not include a matching model", () => {
    const reflectData = {
      endpoints: [{ configured: true, provider: "openai", models: ["gpt-4.1"] }],
    };
    expect(() =>
      resolveConfiguredCopilotModel({
        configuredModel: "small",
        aliasMap: ALIAS_MAP,
        reflectData,
      })
    ).toThrow(ModelAliasResolutionError);
  });

  it.each(["gemini-3.8-flash", "gemini-3.8-flash?effort=high"])("distinguishes unavailable %s from catalog retrieval failure", configuredModel => {
    const options = {
      configuredModel,
      aliasMap: { "gemini-3.8-flash": ["copilot/gemini-3.8-flash"] },
      reflectData: { endpoints: [{ configured: true, provider: "copilot", models: ["claude-sonnet-5"] }] },
    };
    expect(() => resolveConfiguredCopilotModel(options)).toThrow(/not available.*live model catalog/);
    expect(() => resolveConfiguredCopilotModel(options)).toThrow(/agentic-workflows integration/);
    expect(() => resolveConfiguredCopilotModel({ ...options, reflectData: null })).toThrow(/model-catalog retrieval prevented/);
    expect(detectErrors(new ModelAliasResolutionError(configuredModel, "model_unavailable").message).modelNotSupportedError).toBe(true);
  });

  it("does not throw for a concrete model even when the catalog is empty", () => {
    const resolved = resolveConfiguredCopilotModel({
      configuredModel: "claude-sonnet-4.6",
      aliasMap: ALIAS_MAP,
      reflectData: null,
    });
    expect(resolved).toBe("claude-sonnet-4.6");
  });

  it.each(["copilot", "github-copilot"])("preserves retrieval failure for a partial catalog with failed %s models", provider => {
    const options = {
      configuredModel: "gemini-3.8-flash?effort=high",
      aliasMap: { "gemini-3.8-flash": ["copilot/gemini-3.8-flash"] },
      reflectData: {
        endpoints: [
          { configured: true, provider, models: null },
          { configured: true, provider: "openai", models: ["gpt-4.1"] },
        ],
      },
    };
    expect(() => resolveConfiguredCopilotModel(options)).toThrow(expect.objectContaining({ reason: "catalog_unavailable" }));
    options.reflectData.endpoints[0].models = [];
    expect(() => resolveConfiguredCopilotModel(options)).toThrow(expect.objectContaining({ reason: "model_unavailable" }));
  });

  it("checks candidate providers through nested aliases without looping on cycles", () => {
    const options = {
      configuredModel: "nested",
      aliasMap: { nested: ["cycle", "gemini-flash"], cycle: ["nested"], "gemini-flash": ["copilot/gemini-*flash*"] },
      reflectData: {
        endpoints: [
          { configured: true, provider: "copilot", models: null },
          { configured: true, provider: "openai", models: ["gpt-4.1"] },
        ],
      },
    };
    expect(() => resolveConfiguredCopilotModel(options)).toThrow(expect.objectContaining({ reason: "catalog_unavailable" }));
  });

  it("requires complete retrieval from all configured candidate providers before diagnosing unavailability", () => {
    const options = {
      configuredModel: "gemini-flash",
      aliasMap: { "gemini-flash": ["copilot/gemini-*flash*", "google/gemini-*flash*"] },
      reflectData: {
        endpoints: [
          { configured: true, provider: "copilot", models: ["claude-sonnet-5"] },
          { configured: true, provider: "google" },
        ],
      },
    };
    expect(() => resolveConfiguredCopilotModel(options)).toThrow(expect.objectContaining({ reason: "catalog_unavailable" }));
  });

  it("treats failed configured providers as relevant to unqualified alias candidates", () => {
    const options = {
      configuredModel: "custom",
      aliasMap: { custom: ["gemini-*flash*"] },
      reflectData: {
        endpoints: [
          { configured: true, provider: "copilot", models: null },
          { configured: true, provider: "openai", models: ["gpt-4.1"] },
        ],
      },
    };
    expect(() => resolveConfiguredCopilotModel(options)).toThrow(expect.objectContaining({ reason: "catalog_unavailable" }));
  });

  it("ignores failed unrelated and unconfigured provider catalogs", () => {
    const options = {
      configuredModel: "gemini-flash",
      aliasMap: { "gemini-flash": ["copilot/gemini-*flash*"] },
      reflectData: {
        endpoints: [
          { configured: true, provider: "copilot", models: ["claude-sonnet-5"] },
          { configured: true, provider: "openai", models: null },
          { configured: false, provider: "copilot", models: null },
        ],
      },
    };
    expect(() => resolveConfiguredCopilotModel(options)).toThrow(expect.objectContaining({ reason: "model_unavailable" }));
  });

  it("still resolves available candidates when another candidate provider fetch failed", () => {
    const resolved = resolveConfiguredCopilotModel({
      configuredModel: "gpt-5",
      aliasMap: ALIAS_MAP,
      reflectData: {
        endpoints: [
          { configured: true, provider: "copilot", models: null },
          { configured: true, provider: "openai", models: ["gpt-5.4"] },
        ],
      },
    });
    expect(resolved).toBe("openai/gpt-5.4");
  });

  it("resolves the bare gemini engine-name meta-alias to a non-Gemini agent model", () => {
    const resolved = resolveConfiguredCopilotModel({
      configuredModel: "gemini",
      aliasMap: ALIAS_MAP,
      reflectData: { endpoints: [{ configured: true, provider: "copilot", models: ["claude-sonnet-5"] }] },
    });
    expect(resolved).toBe("claude-sonnet-5");
  });

  it("resolveConfiguredCopilotModel normalizes provider-qualified concrete id without alias map", () => {
    const resolved = resolveConfiguredCopilotModel({
      configuredModel: "copilot/claude-haiku-4.5",
      aliasMap: null,
      reflectData: null,
    });
    expect(resolved).toBe("claude-haiku-4.5");
  });

  it("resolveConfiguredCopilotModel normalizes provider-qualified concrete id not in alias map", () => {
    const reflectData = {
      endpoints: [{ configured: true, provider: "copilot", models: ["claude-sonnet-4.6"] }],
    };
    const resolved = resolveConfiguredCopilotModel({
      configuredModel: "copilot/claude-sonnet-4.6",
      aliasMap: ALIAS_MAP,
      reflectData,
    });
    expect(resolved).toBe("claude-sonnet-4.6");
  });

  it("resolves opusplan alias with effort query param", () => {
    const catalog = ["copilot/claude-opus-4.5"];
    const resolved = resolveModelAlias("opusplan", ALIAS_MAP, catalog);
    expect(resolved).toBe("copilot/claude-opus-4.5?effort=high");
  });

  it("buildCatalogFromReflect includes provider-scoped entries", () => {
    const built = buildCatalogFromReflect({
      endpoints: [{ configured: true, provider: "copilot", models: ["gpt-4.1"] }],
    });
    expect(built).toContain("copilot/gpt-4.1");
    expect(built).toContain("gpt-4.1");
  });

  it("selectLatestGlobMatch ranks date-stamped models by full date (Aug > May)", () => {
    const result = selectLatestGlobMatch("copilot/gpt-4o*", ["copilot/gpt-4o-2024-05-13", "copilot/gpt-4o-2024-08-06"]);
    expect(result).toBe("copilot/gpt-4o-2024-08-06");
  });

  it("resolves diamond alias graph without false circular-reference error", () => {
    const diamondAliasMap = {
      combo: ["path-a", "path-b"],
      "path-a": ["shared-fail", "shared"],
      "path-b": ["shared"],
      "shared-fail": ["nonexistent-model-xyz"],
      shared: ["copilot/*haiku*"],
    };
    // A non-null result proves no false circular-reference error was raised for "shared"
    // (which is reachable via both path-a and path-b).
    const result = resolveModelAlias("combo", diamondAliasMap, ["copilot/claude-haiku-4.5"]);
    expect(result).toBe("copilot/claude-haiku-4.5");
  });
});

// Without AWF no /reflect catalogue can exist. A name listed in the bundled model catalogue
// (models.json) is a plausible model id and passes through as exact; pure aliases fail.
describe("resolveEngineModel without a possible catalogue", () => {
  const run = (configuredModel, env = {}) => resolveEngineModel({ engine: "test", configuredModel, aliasMap: ALIAS_MAP, reflectData: null, env, record: false });

  it("defines plausible model ids by the bundled catalogue", () => {
    for (const id of ["gpt-5-mini", "gpt-5.4", "gpt-5.5", "gpt-5"]) expect(isBundledModelId(id)).toBe(true);
    for (const alias of ["small", "opus", "auto", "sonnet", "mini"]) expect(isBundledModelId(alias)).toBe(false);
  });

  it.each(["copilot/gpt-5.4", "copilot/gpt-5-mini", "gpt-5.5", "gpt-5", "copilot/gpt-5"])("passes %s through as exact", async model => {
    expect(await run(model)).toMatchObject({ engineModel: model, alias: false, status: "exact" });
  });

  it.each(["small", "copilot/opus", "auto"])("fails pure alias %s with catalog_unavailable", async model => {
    await expect(run(model)).rejects.toMatchObject({ reason: "catalog_unavailable", message: `model-catalog retrieval prevented alias resolution for '${model}'` });
  });

  it("keeps the with-AWF rule: incomplete discovery fails an alias that is also a model id", async () => {
    await expect(run("copilot/gpt-5-mini", { AWF_REFLECT_ENABLED: "1" })).rejects.toMatchObject({ reason: "catalog_unavailable" });
  });

  it("overlays GH_AW_MODEL_ALIASES on the bundled built-ins", () => {
    expect(loadModelAliasMap({ env: { GH_AW_MODEL_ALIASES: '{"opus":["anthropic/claude-opus-4-7"]}', GH_AW_AWF_CONFIG_PATH: "/nonexistent" } })).toEqual({ ...ALIAS_MAP, opus: ["anthropic/claude-opus-4-7"] });
  });
});

describe("bundled built-in model aliases", () => {
  const env = { GH_AW_AWF_CONFIG_PATH: "/nonexistent" };

  it("ships a copy identical to pkg/workflow/data/model_aliases.json", () => {
    expect(fs.readFileSync(path.join(__dirname, "model_aliases.json"), "utf8")).toBe(fs.readFileSync(path.join(__dirname, "../../../pkg/workflow/data/model_aliases.json"), "utf8"));
  });

  it("loads the built-ins from the bundled file with no alias env var", () => {
    expect(loadBuiltinModelAliases()).toEqual(ALIAS_MAP);
    expect(loadModelAliasMap({ env })).toEqual(ALIAS_MAP);
    expect(loadModelAliasMap({ env, envVar: "GH_AW_PI_MODEL_ALIASES" })).toEqual(ALIAS_MAP);
  });

  it("lets workflow and imported aliases from the env var override and extend the built-ins", () => {
    const overrides = { opus: ["anthropic/claude-opus-4-7"], team: ["copilot/gpt-5.4"] };
    const merged = loadModelAliasMap({ env: { ...env, GH_AW_MODEL_ALIASES: JSON.stringify(overrides) } });
    expect(merged).toEqual({ ...ALIAS_MAP, ...overrides });
    expect(merged?.sonnet).toEqual(ALIAS_MAP.sonnet);
    expect(loadModelAliasMap({ env: { ...env, GH_AW_PI_MODEL_ALIASES: JSON.stringify(overrides) }, envVar: "GH_AW_PI_MODEL_ALIASES" })).toEqual({ ...ALIAS_MAP, ...overrides });
  });

  it("does not let callers mutate the cached built-ins", () => {
    const first = loadBuiltinModelAliases();
    first?.sonnet.push("mutated");
    expect(loadBuiltinModelAliases()?.sonnet).toEqual(ALIAS_MAP.sonnet);
  });
});
