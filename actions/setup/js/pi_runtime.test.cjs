import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";

const { parsePiConfig, preparePiRuntime, nativePiProvider, resolvePiPackageFile, resolvePiModelWithoutAWF, verifyPiVersion } = await import("./pi_runtime.cjs");
const { INLINE_SKILL_MANIFEST, readWorkflowSkillArgs } = await import("./pi_workflow_skills.cjs");
let dir;

beforeEach(() => {
  dir = fs.mkdtempSync(path.join(os.tmpdir(), "pi-runtime-"));
  vi.stubEnv("PI_CODING_AGENT_DIR", path.join(dir, "agent"));
  vi.stubEnv("RUNNER_TEMP", dir);
  vi.stubEnv("GH_AW_PI_STAGING_DIR", path.join(dir, "staging"));
  vi.stubEnv("GH_AW_PI_COMMAND", path.join(dir, "pi"));
  fs.writeFileSync(path.join(dir, "pi"), "#!/bin/sh\nprintf '1.3.0\\n'\n", { mode: 0o700 });
});
afterEach(() => {
  vi.unstubAllEnvs();
  fs.rmSync(dir, { recursive: true, force: true });
});

describe("Pi runtime configuration", () => {
  it("copies activation-staged agents and skills into the managed user scope", () => {
    fs.mkdirSync(path.join(dir, "staging/agents"), { recursive: true });
    fs.mkdirSync(path.join(dir, "staging/skills/report"), { recursive: true });
    fs.writeFileSync(path.join(dir, "staging/agents/reader.md"), "Agent.");
    fs.writeFileSync(path.join(dir, "staging/skills/report/SKILL.md"), "Skill.");
    preparePiRuntime({});
    expect(fs.readFileSync(path.join(dir, "agent/agents/reader.md"), "utf8")).toBe("Agent.");
    expect(fs.readFileSync(path.join(dir, "agent/skills/report/SKILL.md"), "utf8")).toBe("Skill.");
  });
  describe("bare workflow inline skills", () => {
    const hint = "Use the `reporting` skill when producing a report, issue, or PR comment.";
    const reporting = `${hint}\n\n## skill: \`reporting\`\n---\ndescription: Report formatting\n---\n\nUse ### headers.\n\n## end skill: \`reporting\`\n`;
    const reportingPath = () => path.join(dir, "agent/skills/reporting/SKILL.md");

    /** Runs the activation pipeline (runtime imports, then inline-skill extraction) into the staging artifact. */
    async function activate(content, engine = "pi") {
      vi.stubGlobal("core", { info: () => {}, warning: () => {}, error: () => {}, debug: () => {} });
      const { processRuntimeImports } = require("./runtime_import.cjs");
      const { writeInlineSkills } = require("./extract_inline_skills.cjs");
      const workspace = path.join(dir, "workspace");
      const shared = path.join(workspace, ".github/workflows/shared");
      fs.mkdirSync(shared, { recursive: true });
      fs.writeFileSync(path.join(shared, "reporting.md"), reporting);
      fs.writeFileSync(path.join(shared, "wrapper.md"), "{{#runtime-import .github/workflows/shared/reporting.md}}\n");
      // Ambient repository skill: must never be loaded in bare mode.
      fs.mkdirSync(path.join(workspace, ".pi/skills/ambient-helper"), { recursive: true });
      fs.writeFileSync(path.join(workspace, ".pi/skills/ambient-helper/SKILL.md"), "Ambient.");
      const expanded = await processRuntimeImports(content, workspace);
      const prompt = writeInlineSkills(expanded, workspace, path.join(dir, "activation"), engine);
      vi.unstubAllGlobals();
      vi.stubEnv("GH_AW_PI_STAGING_DIR", path.join(dir, "activation/.pi"));
      return prompt;
    }

    it.each([
      ["directly declared", reporting],
      ["body-level runtime import", "# Task\n\n{{#runtime-import .github/workflows/shared/reporting.md}}\n\nSay hello.\n"],
      ["runtime import nested in an import", "# Task\n\n{{#runtime-import .github/workflows/shared/wrapper.md}}\n\nSay hello.\n"],
    ])("loads exactly the skills activation extracted (%s)", async (_name, content) => {
      const prompt = await activate(content);
      expect(prompt).toContain(hint);
      expect(prompt).not.toContain("## skill:");
      vi.stubEnv("GH_AW_PI_BARE", "true");
      const { skillPaths } = preparePiRuntime({});
      expect(skillPaths).toEqual([reportingPath()]);
      expect(fs.readFileSync(reportingPath(), "utf8")).toContain("Use ### headers.");
      expect(fs.readFileSync(path.join(dir, "agent/workflow-skill-args"), "utf8")).toBe(`--skill\n${reportingPath()}\n`);
      expect(readWorkflowSkillArgs()).toEqual(["--skill", reportingPath()]);
      expect(fs.existsSync(path.join(dir, "agent/skills", INLINE_SKILL_MANIFEST))).toBe(false);
    });

    it("ignores staged skills activation did not extract and ambient repository skills", async () => {
      await activate(reporting);
      fs.mkdirSync(path.join(dir, "activation/.pi/skills/installed"), { recursive: true });
      fs.writeFileSync(path.join(dir, "activation/.pi/skills/installed/SKILL.md"), "Other.");
      vi.stubEnv("GH_AW_PI_BARE", "true");
      const { skillPaths } = preparePiRuntime({});
      expect(skillPaths).toEqual([reportingPath()]);
      expect(fs.readFileSync(path.join(dir, "agent/workflow-skill-args"), "utf8")).not.toMatch(/installed|ambient/);
    });

    it("loads nothing explicitly outside bare mode or without inline skills", async () => {
      await activate(reporting);
      expect(preparePiRuntime({}).skillPaths).toEqual([]);
      expect(fs.readFileSync(path.join(dir, "agent/workflow-skill-args"), "utf8")).toBe("");
      fs.rmSync(path.join(dir, "activation"), { recursive: true });
      vi.stubEnv("GH_AW_PI_BARE", "true");
      expect(preparePiRuntime({}).skillPaths).toEqual([]);
      expect(readWorkflowSkillArgs()).toEqual([]);
    });

    it("writes no Pi manifest for other engines", async () => {
      await activate(reporting, "claude");
      expect(fs.existsSync(path.join(dir, "activation/.claude/skills/reporting.md"))).toBe(true);
      expect(fs.readdirSync(path.join(dir, "activation/.claude/skills"))).toEqual(["reporting.md"]);
    });

    it.each([['["../escape"]'], ['{"name":"reporting"}'], ["[1]"]])("rejects a malformed manifest: %s", manifest => {
      fs.mkdirSync(path.join(dir, "staging/skills"), { recursive: true });
      fs.writeFileSync(path.join(dir, "staging/skills", INLINE_SKILL_MANIFEST), manifest);
      vi.stubEnv("GH_AW_PI_BARE", "true");
      expect(() => preparePiRuntime({})).toThrow(/Invalid Pi inline skill/);
    });

    it("rejects tampered skill arguments", () => {
      fs.mkdirSync(path.join(dir, "agent"), { recursive: true });
      fs.writeFileSync(path.join(dir, "agent/workflow-skill-args"), "--skill\n/etc/passwd\n");
      expect(() => readWorkflowSkillArgs()).toThrow("Invalid Pi workflow skill arguments");
    });
  });
  it.each(["0.99.2", "1.0.0-beta.1", "unexpected"])("rejects an incompatible actual CLI version: %s", version => {
    expect(() => verifyPiVersion("pi", () => version)).toThrow("v1.0.0 or newer");
  });
  it.each(["1.0.0", "1.1.0", "1.3.0", "1.4.0", "2.0.0", "Pi 1.0.0"])("accepts an actual compatible version: %s", version => {
    expect(() => verifyPiVersion("pi", () => version)).not.toThrow();
  });
  it.each(["1.0.0", "1.2.9", "1.3.0-beta.1"])("rejects max effort on an older CLI: %s", version => {
    expect(() => verifyPiVersion("pi", () => version, "1.3.0")).toThrow("v1.3.0 or newer is required for effort=max");
  });
  it.each(["1.3.0", "v1.3.0+build.1", "1.4.0", "1.10.0", "2.0.0"])("accepts max effort on a supported CLI: %s", version => {
    expect(() => verifyPiVersion("pi", () => version, "1.3.0")).not.toThrow();
  });

  it("uses the install-time package root even when sandbox npm paths differ", () => {
    const root = path.join(dir, "host-toolcache/pi");
    fs.mkdirSync(path.join(root, "dist"), { recursive: true });
    fs.writeFileSync(path.join(root, "dist/index.js"), "export {};");
    vi.stubEnv("GH_AW_PI_PACKAGE_ROOT", root);
    expect(fs.realpathSync(resolvePiPackageFile("dist/index.js"))).toBe(fs.realpathSync(path.join(root, "dist/index.js")));
  });

  it("finds the package from the real CLI symlink without depending on NODE_PATH", () => {
    const root = path.join(dir, "alternate-node/lib/node_modules/@earendil-works/pi-coding-agent");
    const bin = path.join(dir, "alternate-node/bin");
    fs.mkdirSync(path.join(root, "dist/bundle"), { recursive: true });
    fs.mkdirSync(bin, { recursive: true });
    fs.writeFileSync(path.join(root, "package.json"), '{"name":"@earendil-works/pi-coding-agent"}');
    fs.writeFileSync(path.join(root, "dist/index.js"), "export {};");
    fs.writeFileSync(path.join(root, "dist/bundle/cli.js"), "");
    fs.symlinkSync(path.join(root, "dist/bundle/cli.js"), path.join(bin, "pi"));
    vi.stubEnv("PATH", bin);
    vi.stubEnv("NODE_PATH", "");
    vi.stubEnv("GH_AW_PI_PACKAGE_ROOT", "");
    expect(fs.realpathSync(resolvePiPackageFile("dist/index.js"))).toBe(fs.realpathSync(path.join(root, "dist/index.js")));
  });

  it("retains installed package declarations in the same runtime directory", () => {
    fs.mkdirSync(path.join(dir, "agent"));
    fs.writeFileSync(path.join(dir, "agent/settings.json"), '{"packages":["npm:example@1.0.0"]}');
    const result = preparePiRuntime({ settings: { defaultThinkingLevel: "high", defaultProjectTrust: "always" } });
    expect(result.settings.packages).toEqual(["npm:example@1.0.0"]);
    expect(result.settings.retry).toEqual({ enabled: true, maxRetries: 2, baseDelayMs: 1000, maxAgentDelayMs: 30000 });
    expect(result.settings.defaultTools).toEqual(["+codemode", "+tool_search"]);
    expect(result.settings.defaultProjectTrust).toBe("never");
    expect(result.settings.defaultThinkingLevel).toBe("high");
  });

  it("merges retry defaults with partial installed and workflow settings", () => {
    fs.mkdirSync(path.join(dir, "agent"));
    fs.writeFileSync(path.join(dir, "agent/settings.json"), JSON.stringify({ retry: { maxRetries: 4 } }));
    const result = preparePiRuntime({ settings: { retry: { baseDelayMs: 500 } } });
    expect(result.settings.retry).toEqual({ enabled: true, maxRetries: 4, baseDelayMs: 500, maxAgentDelayMs: 30000 });
  });

  it("merges installed tool selections with managed defaults and forwards current release settings", () => {
    fs.mkdirSync(path.join(dir, "agent"));
    fs.writeFileSync(path.join(dir, "agent/settings.json"), JSON.stringify({ defaultTools: ["+grep", "+codemode"] }));
    const result = preparePiRuntime({ settings: { sessionShareDestination: "radius", defaultThinkingLevel: "max", codemode: { mode: "only", inlineBudget: 0 } } });
    expect(result.settings).toMatchObject({
      defaultTools: ["+grep", "+codemode", "+tool_search"],
      sessionShareDestination: "radius",
      defaultThinkingLevel: "max",
      codemode: { mode: "only", inlineBudget: 0 },
    });
    expect(preparePiRuntime({ settings: { defaultTools: ["read"] } }).settings.defaultTools).toEqual(["read"]);
  });

  it.each([["+grep"], [], null, "read"])("restores missing managed default tools from installed settings: %j", defaultTools => {
    fs.mkdirSync(path.join(dir, "agent"));
    fs.writeFileSync(path.join(dir, "agent/settings.json"), JSON.stringify({ defaultTools }));
    expect(preparePiRuntime({}).settings.defaultTools).toEqual([...(Array.isArray(defaultTools) ? defaultTools : []), "+codemode", "+tool_search"]);
  });

  it("rejects max in effective settings before rewriting the installed settings", () => {
    fs.writeFileSync(path.join(dir, "pi"), "#!/bin/sh\nprintf '1.2.0\\n'\n");
    fs.mkdirSync(path.join(dir, "agent"));
    const settingsPath = path.join(dir, "agent/settings.json");
    fs.writeFileSync(settingsPath, JSON.stringify({ defaultThinkingLevel: "max" }));
    expect(() => preparePiRuntime({})).toThrow("v1.3.0 or newer is required for effort=max");
    expect(JSON.parse(fs.readFileSync(settingsPath, "utf8"))).toEqual({ defaultThinkingLevel: "max" });
    expect(preparePiRuntime({ settings: { defaultThinkingLevel: "high" } }).settings.defaultThinkingLevel).toBe("high");
    expect(() => preparePiRuntime({ settings: { defaultThinkingLevel: "max" } })).toThrow("v1.3.0 or newer");
  });

  it.each(["GH_AW_PI_ARGS", "GH_AW_PI_SUBAGENT_ARGS"])("rejects max CLI arguments on older Pi through %s", env => {
    fs.writeFileSync(path.join(dir, "pi"), "#!/bin/sh\nprintf '1.2.0\\n'\n");
    for (const args of [["--thinking", "max"], ["--thinking=max"]]) {
      vi.stubEnv(env, JSON.stringify(args));
      expect(() => preparePiRuntime({})).toThrow("v1.3.0 or newer is required for effort=max");
    }
    vi.stubEnv(env, JSON.stringify(["--thinking", "high"]));
    expect(() => preparePiRuntime({})).not.toThrow();
  });

  it.each(["GH_AW_PI_ARGS", "GH_AW_PI_SUBAGENT_ARGS"])("does not interpret positional prompt text after -- as options through %s", env => {
    fs.writeFileSync(path.join(dir, "pi"), "#!/bin/sh\nprintf '1.2.0\\n'\n");
    for (const args of [
      ["--", "--thinking=max"],
      ["--thinking", "high", "--", "--thinking", "max"],
    ]) {
      vi.stubEnv(env, JSON.stringify(args));
      expect(() => preparePiRuntime({})).not.toThrow();
    }
    vi.stubEnv(env, JSON.stringify(["--thinking=max", "--", "prompt"]));
    expect(() => preparePiRuntime({})).toThrow("v1.3.0 or newer is required for effort=max");
  });

  it("applies the routed model effort instead of a fixed setting", () => {
    fs.mkdirSync(path.join(dir, "agent"), { recursive: true });
    fs.writeFileSync(path.join(dir, "agent/model-routing-selection.json"), JSON.stringify({ wire_model: "claude-opus-5", mapped_effort: "off" }));
    vi.stubEnv("GH_AW_MODEL_ROUTING", "1");
    const result = preparePiRuntime({ settings: { defaultThinkingLevel: "high" } });
    expect(result.routingSelection).toEqual({ wire_model: "claude-opus-5", mapped_effort: "off" });
    expect(result.settings.defaultThinkingLevel).toBe("off");
  });

  it("fails closed when the routed selection file is missing", () => {
    vi.stubEnv("GH_AW_MODEL_ROUTING", "1");
    expect(() => preparePiRuntime({})).toThrow("AWF model routing selection is unavailable to Pi");
  });

  it("checks the routed thinking effort rather than the overridden fixed setting", () => {
    fs.writeFileSync(path.join(dir, "pi"), "#!/bin/sh\nprintf '1.2.0\\n'\n");
    fs.mkdirSync(path.join(dir, "agent"));
    const selectionPath = path.join(dir, "agent/model-routing-selection.json");
    fs.writeFileSync(selectionPath, JSON.stringify({ wire_model: "claude-opus-5", mapped_effort: "max" }));
    vi.stubEnv("GH_AW_MODEL_ROUTING", "1");
    expect(() => preparePiRuntime({})).toThrow("v1.3.0 or newer is required for effort=max");
    fs.writeFileSync(selectionPath, JSON.stringify({ wire_model: "claude-opus-5", mapped_effort: "high" }));
    expect(preparePiRuntime({ settings: { defaultThinkingLevel: "max" } }).settings.defaultThinkingLevel).toBe("high");
  });

  it("uses only gateway-authorized MCP servers and preserves scoped headers", () => {
    fs.mkdirSync(path.join(dir, "gh-aw/mcp-config"), { recursive: true });
    fs.writeFileSync(path.join(dir, "gh-aw/mcp-config/mcp-servers.json"), JSON.stringify({ mcpServers: { safeoutputs: { url: "http://gateway/mcp/safeoutputs", headers: { Authorization: "gateway-session-token" } } } }));
    preparePiRuntime({ mcp: { exposure: "codemode", toolExposure: { safeoutputs: { noop: "direct" } } } });
    const mcp = JSON.parse(fs.readFileSync(path.join(dir, "agent/mcp.json"), "utf8"));
    expect(mcp.mcpServers.safeoutputs).toMatchObject({ exposure: "codemode", toolExposure: { noop: "direct" }, headers: { Authorization: "gateway-session-token" } });
    expect(fs.statSync(path.join(dir, "agent/mcp.json")).mode & 0o777).toBe(0o600);
  });

  it("exposes smoke-test MCP tools directly without changing gateway authorization", () => {
    const servers = {
      safeoutputs: { url: "http://gateway/mcp/safeoutputs", headers: { Authorization: "scoped-test-token" } },
      web_fetch: { url: "http://gateway/mcp/web_fetch", headers: { Authorization: "web-test-token" } },
    };
    fs.mkdirSync(path.join(dir, "gh-aw/mcp-config"), { recursive: true });
    fs.writeFileSync(path.join(dir, "gh-aw/mcp-config/mcp-servers.json"), JSON.stringify({ mcpServers: servers }));
    preparePiRuntime({ mcp: { exposure: "direct" } });
    const mcp = JSON.parse(fs.readFileSync(path.join(dir, "agent/mcp.json"), "utf8"));
    expect(mcp.mcpServers).toEqual(Object.fromEntries(Object.entries(servers).map(([name, server]) => [name, { ...server, exposure: "direct" }])));
    expect(fs.statSync(path.join(dir, "agent/mcp.json")).mode & 0o777).toBe(0o600);
  });

  it("reports malformed settings instead of discarding installed packages", () => {
    fs.mkdirSync(path.join(dir, "agent"));
    fs.writeFileSync(path.join(dir, "agent/settings.json"), "invalid");
    expect(() => preparePiRuntime({})).toThrow();
  });

  it.each(["null", "[]", '{"other":{}}', '{"settings":false}', '{"mcp":{"exposure":"invalid"}}'])("rejects invalid configuration: %s", raw => {
    expect(() => parsePiConfig(raw)).toThrow();
  });

  it("preserves native provider identifiers instead of defaulting to Copilot", () => {
    expect(nativePiProvider("google")).toBe("google");
    expect(nativePiProvider("openrouter")).toBe("openrouter");
    expect(nativePiProvider("codex")).toBe("openai");
  });
});

// network.firewall: false (no AWF): no /reflect catalogue can exist on the run.
describe("Pi main model without AWF", () => {
  const builtinAliases = fs.readFileSync(path.join(import.meta.dirname, "../../../pkg/workflow/data/model_aliases.json"), "utf8");
  const envFor = model => ({ GH_AW_PI_MODEL: model, GH_AW_PI_MODEL_ALIASES: JSON.stringify(JSON.parse(builtinAliases).aliases), GH_AW_TMP_DIR: dir });
  const records = () => JSON.parse(fs.readFileSync(path.join(dir, "agent/model-resolution.json"), "utf8"));
  beforeEach(() => vi.spyOn(process.stderr, "write").mockImplementation(() => true));

  it.each(["copilot/gpt-5.4", "copilot/gpt-5-mini"])("runs %s as an exact id although an alias has the same name", async model => {
    const result = await resolvePiModelWithoutAWF(envFor(model));
    expect(result).toMatchObject({ engineModel: model, alias: false, status: "exact" });
    expect(records().at(-1)).toEqual({ engine: "pi", role: "main", configured_model: model, resolved_model: model, provider: "copilot", status: "exact", cause: null });
  });

  it("fails a pure alias with catalog_unavailable", async () => {
    await expect(resolvePiModelWithoutAWF(envFor("copilot/opus"))).rejects.toMatchObject({
      name: "ModelAliasResolutionError",
      reason: "catalog_unavailable",
      message: "model-catalog retrieval prevented alias resolution for 'copilot/opus'",
    });
    expect(records().at(-1)).toMatchObject({ engine: "pi", configured_model: "copilot/opus", status: "failed", cause: "catalog_unavailable" });
  });

  it("does nothing when AWF is enabled", async () => {
    expect(await resolvePiModelWithoutAWF({ ...envFor("copilot/opus"), AWF_REFLECT_ENABLED: "1" })).toBeNull();
  });
});
