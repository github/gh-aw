// @ts-check
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { createRequire } from "module";
import { mkdtempSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";

const req = createRequire(import.meta.url);
const { buildConfig, mergeConfig, serializeConfig, directToolCatalog } = req("./codex_config.cjs");

describe("Codex GitHub direct-tool catalog", () => {
  it("overrides model-selected code mode without changing other metadata", () => {
    const catalog = {
      models: [
        { slug: "gpt-5.6-sol", tool_mode: "code_mode_only", context_window: 400000, model_messages: { instructions: "preserved" } },
        { slug: "gpt-5.6-terra", tool_mode: "code_mode" },
        { slug: "gpt-5.3-codex", tool_mode: null },
      ],
    };
    const original = structuredClone(catalog);
    expect(directToolCatalog(catalog)).toEqual({ models: catalog.models.map(model => ({ ...model, tool_mode: "direct" })) });
    expect(catalog).toEqual(original);
  });

  it.each([null, {}, { models: [] }, { models: [null] }, { models: [{}] }, { models: [{ slug: "" }] }])("rejects malformed catalogs: %j", catalog => {
    expect(() => directToolCatalog(catalog)).toThrow("Invalid bundled Codex model catalog");
  });
});

describe("Codex effective configuration", () => {
  let dir;
  let saved;
  beforeEach(() => {
    dir = mkdtempSync(join(tmpdir(), "codex-native-config-"));
    saved = { ...process.env };
    process.env.GH_AW_CODEX_CONFIG = join(dir, "config.json");
    delete process.env.GH_AW_TOOL_TIMEOUT;
    delete process.env.GH_AW_STARTUP_TIMEOUT;
  });
  afterEach(() => {
    process.env = saved;
    rmSync(dir, { recursive: true, force: true });
  });

  function writeConfig(overrides = {}, disablePlugins = true) {
    writeFileSync(
      process.env.GH_AW_CODEX_CONFIG,
      JSON.stringify({
        defaults: {
          history: { persistence: "none" },
          otel: { metrics_exporter: "none" },
          features: { plugins: !disablePlugins },
          mcp_servers: {
            github: { startup_timeout_sec: 120, tool_timeout_sec: 60, http_headers: { "User-Agent": "test-workflow" } },
            cli: { tool_timeout_sec: 60 },
          },
        },
        overrides,
        disablePlugins,
      })
    );
  }

  it("merges root and nested settings without dropping defaults or duplicating keys", () => {
    writeConfig({ model_reasoning_effort: "high", features: { plugins: false, shell_tool: false } });
    const config = buildConfig({ github: { headers: { Authorization: "Bearer token" } } }, "http://gateway:80");
    expect(config.model_reasoning_effort).toBe("high");
    expect(config.otel).toEqual({ metrics_exporter: "none" });
    expect(config.features).toEqual({ plugins: false, shell_tool: false });
    expect(serializeConfig(config).match(/plugins = false/g)).toHaveLength(1);
  });

  it("replaces legacy shell filters with canonical filters without mutating defaults", () => {
    const defaults = { shell_environment_policy: { inherit: "all", ignore_default_excludes: false, include_only: ["PATH", "HOME"], exclude: ["PRIVATE_*"] } };
    const original = structuredClone(defaults);
    const config = mergeConfig(defaults, { shell_environment_policy: { filters: { PATH: "include" } } });
    expect(config.shell_environment_policy).toEqual({ inherit: "all", ignore_default_excludes: false, filters: { PATH: "include" } });
    expect(defaults).toEqual(original);
    const output = serializeConfig(config);
    expect(output).toContain("[shell_environment_policy.filters]");
    expect(output).not.toContain("include_only");
    expect(output).not.toContain("exclude =");
  });

  it.each(["include_only", "exclude"])("replaces canonical filters with the explicit legacy %s representation", key => {
    const config = mergeConfig({ shell_environment_policy: { inherit: "all", filters: { PATH: "include" } } }, { shell_environment_policy: { [key]: ["HOME"] } });
    expect(config.shell_environment_policy.filters).toBeUndefined();
    expect(config.shell_environment_policy[key]).toEqual(["HOME"]);
    expect(config.shell_environment_policy.inherit).toBe("all");
    expect(() => mergeConfig({}, { shell_environment_policy: { filters: {}, [key]: [] } })).toThrow("cannot mix");
  });

  it("keeps runtime timeouts and gives explicit per-server tuning precedence", () => {
    writeConfig({ mcp_servers: { github: { tool_timeout_sec: 180 } } });
    process.env.GH_AW_TOOL_TIMEOUT = "90";
    process.env.GH_AW_STARTUP_TIMEOUT = "240";
    const config = buildConfig({ github: { headers: {} } }, "http://gateway:80");
    expect(config.mcp_servers.github.tool_timeout_sec).toBe(180);
    expect(config.mcp_servers.github.startup_timeout_sec).toBe(240);
  });

  it("preserves client headers and filters servers that are absent from gateway output", () => {
    writeConfig();
    const config = buildConfig({ github: { headers: { Authorization: "Bearer token" } } }, "http://gateway:80");
    expect(config.mcp_servers.github.http_headers).toEqual({ "User-Agent": "test-workflow", Authorization: "Bearer token" });
    expect(config.mcp_servers.cli).toBeUndefined();
  });

  it("expands environment values as data and escapes their TOML representation", () => {
    writeConfig({ model: "${CODEX_TEST_MODEL}" });
    process.env.CODEX_TEST_MODEL = 'quoted "model"\\name\n$(not-a-shell-command)';
    const config = buildConfig({}, "");
    expect(config.model).toBe(process.env.CODEX_TEST_MODEL);
    expect(serializeConfig(config)).toContain(`model = ${JSON.stringify(process.env.CODEX_TEST_MODEL)}`);
  });

  it("does not expand placeholder-shaped data again after bootstrap", () => {
    writeConfig({ model: "${NOT_A_VARIABLE}" });
    const payload = JSON.parse(req("fs").readFileSync(process.env.GH_AW_CODEX_CONFIG, "utf8"));
    payload.envExpanded = true;
    writeFileSync(process.env.GH_AW_CODEX_CONFIG, JSON.stringify(payload));
    expect(buildConfig({}, "").model).toBe("${NOT_A_VARIABLE}");
  });

  it("fails explicitly for missing variables, malformed metadata, and invalid timeout values", () => {
    writeConfig({ model: "${CODEX_MISSING_VALUE}" });
    delete process.env.CODEX_MISSING_VALUE;
    expect(() => buildConfig({}, "")).toThrow("CODEX_MISSING_VALUE is not set");
    writeFileSync(process.env.GH_AW_CODEX_CONFIG, "{}");
    expect(() => buildConfig({}, "")).toThrow("Invalid compiled Codex configuration");
    writeConfig();
    process.env.GH_AW_TOOL_TIMEOUT = "not-a-number";
    expect(() => buildConfig({}, "")).toThrow("positive integer");
  });

  it("does not prototype-pollute during a structural merge", () => {
    const merged = mergeConfig({}, JSON.parse('{"__proto__":{"polluted":true},"constructor":{"prototype":{"bad":true}}}'));
    expect(Object.hasOwn(merged, "__proto__")).toBe(true);
    expect({}.polluted).toBeUndefined();
    expect({}.bad).toBeUndefined();
  });

  it("rejects unsupported serializer values rather than silently dropping them", () => {
    expect(() => serializeConfig({ invalid: null })).toThrow("unsupported TOML value");
    expect(() => serializeConfig({ invalid: Infinity })).toThrow("unsupported TOML value");
  });
});
