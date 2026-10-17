import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { preparePiSubagents } = await import("./pi_subagent_config.cjs");
const reflectFixtures = await import("./fixtures/model_resolution_reflect.cjs");
let dir;
let frontmatter;
const sdk = { parseFrontmatter: () => ({ frontmatter, body: "Read the requested file." }) };
const catalog = ["github-copilot/claude-haiku-4.5", "github-copilot/gpt-5.6-luna", "anthropic/claude-haiku-4-5"];

beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-subagents-"));
  fs.mkdirSync(path.join(dir, "agents"));
  fs.writeFileSync(path.join(dir, "agents/reader.md"), "Fixture");
  frontmatter = { description: "Read files", model: "claude-haiku-4.5", tools: ["read"] };
  vi.stubEnv("GH_AW_PI_MODEL_ALIASES", JSON.stringify({ small: ["copilot/*haiku*", "anthropic/*haiku*"], recursive: ["small"] }));
  vi.stubEnv("GH_AW_PI_COMMAND", path.join(dir, "pi"));
  vi.stubEnv("GH_AW_TMP_DIR", dir);
  fs.writeFileSync(path.join(dir, "pi"), "#!/bin/sh\nprintf '1.3.0\\n'\n", { mode: 0o700 });
});
afterEach(() => {
  vi.unstubAllEnvs();
  fs.rmSync(dir, { recursive: true, force: true });
});

const prepare = overrides => preparePiSubagents({ agentDir: dir, sdk, provider: "github-copilot", catalog, gateway: true, parentModel: "github-copilot/gpt-5.6-luna", ...overrides });

describe("Pi sub-agent model resolution", () => {
  it("derives name from the marker filename and qualifies bare models through the gateway", () => {
    expect(prepare()).toEqual([expect.objectContaining({ name: "reader", model: "aw-gateway/claude-haiku-4.5", tools: ["read"] })]);
    expect(JSON.parse(fs.readFileSync(path.join(dir, "subagents.json"), "utf8"))[0].declaredModel).toBe("claude-haiku-4.5");
  });

  it.each(["small", "recursive"])("resolves alias %s only on the parent's provider", model => {
    frontmatter.model = model;
    expect(prepare()[0].model).toBe("aw-gateway/claude-haiku-4.5");
  });

  it("inherits an unspecified model and supports native execution", () => {
    delete frontmatter.model;
    expect(prepare({ gateway: false })[0].model).toBe("github-copilot/gpt-5.6-luna");
  });

  it.each(["none", "minimal", "low", "medium", "high", "xhigh", "max"])("translates %s effort into Pi thinking settings", effort => {
    frontmatter.model = `small?effort=${effort}`;
    expect(prepare()[0]).toMatchObject({ modelId: "claude-haiku-4.5", thinking: effort === "none" ? "off" : effort });
  });

  it("rejects unsupported thinking efforts", () => {
    frontmatter.model = "small?effort=invalid";
    expect(() => prepare()).toThrow("unsupported thinking effort");
  });

  it.each([true, false])("rejects resolved max effort on older Pi before writing launch configuration (gateway=%s)", gateway => {
    fs.writeFileSync(path.join(dir, "pi"), "#!/bin/sh\nprintf '1.2.0\\n'\n");
    frontmatter.model = "small?effort=max";
    expect(() => prepare({ gateway })).toThrow("v1.3.0 or newer is required for effort=max");
    expect(fs.existsSync(path.join(dir, "subagents.json"))).toBe(false);
    frontmatter.model = "small?effort=high";
    expect(prepare({ gateway })[0].thinking).toBe("high");
  });

  it("checks inherited and alias-selected max effort on older Pi", () => {
    fs.writeFileSync(path.join(dir, "pi"), "#!/bin/sh\nprintf '1.2.0\\n'\n");
    delete frontmatter.model;
    expect(() => prepare({ parentModel: "github-copilot/gpt-5.6-luna?effort=max" })).toThrow("v1.3.0 or newer");
    vi.stubEnv("GH_AW_PI_MODEL_ALIASES", JSON.stringify({ small: ["copilot/*haiku*?effort=max"] }));
    frontmatter.model = "small";
    expect(() => prepare()).toThrow("v1.3.0 or newer");
  });

  it("refuses unresolved aliases instead of silently using the parent model", () => {
    frontmatter.model = "small";
    expect(() => prepare({ catalog: ["github-copilot/gpt-5.6-luna"] })).toThrow("did not resolve");
  });

  it("workflow-defined alias overrides built-in", () => {
    const reflectData = reflectFixtures.withCopilotModels(["claude-opus-6"]);
    frontmatter.model = "opus";
    // No workflow aliases: the variable is omitted and built-ins come from the bundled file.
    vi.stubEnv("GH_AW_PI_MODEL_ALIASES", "");
    expect(prepare({ catalog: undefined, reflectData })[0].modelId).toBe("claude-opus-6");
    vi.stubEnv("GH_AW_PI_MODEL_ALIASES", JSON.stringify({ opus: ["copilot/claude-opus-5*"] }));
    expect(prepare({ catalog: undefined, reflectData })[0].modelId).toBe("claude-opus-5.5");
    expect(JSON.parse(fs.readFileSync(path.join(dir, "agent/model-resolution.json"), "utf8"))).toEqual([
      { engine: "pi", role: "reader", configured_model: "opus", resolved_model: "claude-opus-5.5", provider: "copilot", status: "resolved", cause: null },
    ]);
  });

  // gpt-5-mini is both a built-in alias and a live id in the recorded /reflect of run
  // 38945917145; the "absent" case removes it from a copy of the recording.
  it("a live exact id wins over an alias with the same name", () => {
    vi.stubEnv("GH_AW_PI_MODEL_ALIASES", JSON.stringify(reflectFixtures.builtinAliases()));
    for (const model of ["copilot/gpt-5-mini", "gpt-5-mini"]) {
      frontmatter.model = model;
      expect(prepare({ catalog: undefined, reflectData: reflectFixtures.ddgReflect() })[0]).toMatchObject({ model: "aw-gateway/gpt-5-mini", modelId: "gpt-5-mini" });
      expect(JSON.parse(fs.readFileSync(path.join(dir, "agent/model-resolution.json"), "utf8"))).toEqual([
        { engine: "pi", role: "reader", configured_model: model, resolved_model: "gpt-5-mini", provider: "copilot", status: "exact", cause: null },
      ]);
    }
    frontmatter.model = "gpt-5-mini";
    expect(prepare({ catalog: undefined, reflectData: reflectFixtures.withoutCopilotModels(["gpt-5-mini"]) })[0].modelId).toBe("gpt-5.4-mini");
  });

  // Incomplete discovery is derived from the recorded /reflect of run 38945917145
  // (models_fetch_complete: false, Copilot models: null); no recorded run has this shape.
  it("incomplete discovery passes exact ids through and fails aliases with catalog_unavailable", () => {
    const reflectData = reflectFixtures.incompleteDiscovery();
    vi.stubEnv("GH_AW_PI_MODEL_ALIASES", JSON.stringify(reflectFixtures.builtinAliases()));
    frontmatter.model = "claude-haiku-5.5";
    expect(prepare({ catalog: undefined, reflectData })[0].modelId).toBe("claude-haiku-5.5");
    frontmatter.model = "haiku";
    let error;
    try {
      prepare({ catalog: undefined, reflectData });
    } catch (err) {
      error = err;
    }
    expect(error).toMatchObject({ name: "ModelAliasResolutionError", reason: "catalog_unavailable" });
    expect(error.message).toContain('Pi sub-agent "reader" model alias "haiku" did not resolve');
  });

  it("refuses other provider routes", () => {
    frontmatter.model = "anthropic/claude-haiku-4-5";
    expect(() => prepare()).toThrow("different provider");
  });

  it.each([{ model: 123 }, { tools: { read: true } }, { description: "" }])("rejects malformed frontmatter %j", invalid => {
    Object.assign(frontmatter, invalid);
    expect(() => prepare()).toThrow();
  });

  it("rejects symlinked agent files rather than reading outside the staging directory", () => {
    fs.unlinkSync(path.join(dir, "agents/reader.md"));
    fs.symlinkSync(path.join(dir, "outside.md"), path.join(dir, "agents/reader.md"));
    expect(prepare()).toEqual([]);
  });
});
