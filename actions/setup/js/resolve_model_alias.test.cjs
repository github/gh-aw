// @ts-check
import { describe, it, expect } from "vitest";
import { createRequire } from "module";

const require = createRequire(import.meta.url);
const fs = require("fs");
const path = require("path");
const { fileURLToPath } = require("url");
const __dirname = path.dirname(fileURLToPath(import.meta.url));
const { buildCatalogFromReflect, globMatch, ModelAliasResolutionError, normalizeForCopilotCLI, resolveConfiguredCopilotModel, resolveModelAlias, selectLatestGlobMatch } = require("./resolve_model_alias.cjs");

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

  it("does not throw for a concrete model even when the catalog is empty", () => {
    const resolved = resolveConfiguredCopilotModel({
      configuredModel: "claude-sonnet-4.6",
      aliasMap: ALIAS_MAP,
      reflectData: null,
    });
    expect(resolved).toBe("claude-sonnet-4.6");
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
