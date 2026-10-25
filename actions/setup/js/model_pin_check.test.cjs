// @ts-check
import { afterEach, describe, expect, it } from "vitest";
import crypto from "crypto";
import fs from "fs";
import os from "os";
import path from "path";
import { createRequire } from "module";
import { fileURLToPath } from "url";

const require = createRequire(import.meta.url);
const { buildModelPinReport, collectModelPins, main, renderModelPinReport, renderModelPinSummary } = require("./model_pin_check.cjs");
// Recorded /reflect from Design Decision Gate run 38945917145 (40 Copilot models; no claude-opus-5).
const { ddgReflect, incompleteDiscovery } = require("./fixtures/model_resolution_reflect.cjs");

/** @type {string[]} */
const tempDirs = [];

afterEach(() => {
  for (const dir of tempDirs.splice(0)) fs.rmSync(dir, { recursive: true, force: true });
});

/**
 * Write small fixture workflows into a temporary repository.
 * @param {Record<string, string>} files repository-relative path → content
 * @returns {string} repository root
 */
function fixtureRepo(files) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "model-pin-check-"));
  tempDirs.push(root);
  fs.mkdirSync(path.join(root, ".github", "workflows"), { recursive: true });
  for (const [rel, content] of Object.entries(files)) {
    const file = path.join(root, rel);
    fs.mkdirSync(path.dirname(file), { recursive: true });
    fs.writeFileSync(file, content);
  }
  return root;
}

/**
 * @param {Record<string, string>} files
 * @param {any} [reflectData]
 */
function check(files, reflectData = ddgReflect()) {
  const collected = collectModelPins({ repoRoot: fixtureRepo(files) });
  return buildModelPinReport({ ...collected, reflectData });
}

/**
 * @param {ReturnType<typeof buildModelPinReport>} report
 * @param {string} field
 */
function pin(report, field) {
  const found = report.pins.find(result => result.field === field);
  expect(found, `pin ${field}`).toBeDefined();
  return /** @type {NonNullable<typeof found>} */ found;
}

describe("model_pin_check acceptance cases", () => {
  it("uses a standardized code for missing CLI configuration", () => {
    expect(() => main([])).toThrow(/^ERR_MODEL_PIN_REFLECT_REQUIRED:/);
  });

  it("1. a retired copilot/claude-opus-5 pin is unresolvable and listed under Action required", () => {
    const report = check({ ".github/workflows/gate.md": "---\nengine: pi\nmodel: copilot/claude-opus-5\n---\n# Gate\n" });
    const result = pin(report, "model");
    expect(result).toMatchObject({ workflow: ".github/workflows/gate.md", configured: "copilot/claude-opus-5", status: "unresolvable", cause: "model_unavailable" });
    expect(report.action_required).toEqual([result]);
    const summary = renderModelPinSummary(report);
    expect(summary.startsWith("### Action required")).toBe(true);
    expect(summary).toContain("| `.github/workflows/gate.md` | `model` | `copilot/claude-opus-5` | `model_unavailable` |");
  });

  it("2. copilot/opus resolves to the newest live Opus", () => {
    const report = check({ ".github/workflows/opus.md": "---\nengine: copilot\nmodel: copilot/opus\n---\n" });
    expect(pin(report, "model")).toMatchObject({ status: "alias", resolved: "copilot/claude-opus-5.5" });
    expect(renderModelPinReport(report)).toContain("| alias resolves | → `copilot/claude-opus-5.5` |");
  });

  it("3. gpt-5-mini (an alias name and a live id) is exact", () => {
    const report = check({ ".github/workflows/mini.md": "---\nengine: copilot\nmodel: gpt-5-mini\n---\n" });
    expect(pin(report, "model")).toMatchObject({ status: "exact", resolved: "gpt-5-mini", cause: null });
  });

  it("4. an expression pin is not checked with reason expression", () => {
    const report = check({ ".github/workflows/expr.md": "---\nengine:\n  id: copilot\n  model: ${{ vars.GH_AW_MODEL_AGENT_COPILOT }}\n---\n" });
    expect(pin(report, "engine.model")).toMatchObject({ status: "not_checked", reason: "expression" });
  });

  it("5. a Claude workflow on the Anthropic provider is not checked", () => {
    const report = check({ ".github/workflows/claude.md": "---\nengine: claude\nmodel: claude-sonnet-4-6\n---\n" });
    expect(pin(report, "model")).toMatchObject({ provider: "anthropic", status: "not_checked", reason: "provider not in the Copilot catalogue" });
  });

  it("6. a workflow models: alias whose every candidate is gone is unresolvable", () => {
    const report = check({ ".github/workflows/alias.md": "---\nengine: copilot\nmodel: retired\nmodels:\n  retired:\n    - copilot/claude-opus-5\n    - copilot/claude-opus-4.8\n---\n" });
    expect(pin(report, "models.retired")).toMatchObject({ status: "unresolvable", cause: "model_unavailable" });
    expect(pin(report, "model")).toMatchObject({ status: "unresolvable", cause: "model_unavailable" });
    expect(report.action_required).toHaveLength(2);
  });

  it("7. no pins gives an empty table and no Action required section", () => {
    const report = check({ ".github/workflows/plain.md": "---\non: push\n---\n# Plain\n" });
    expect(report.pins).toEqual([]);
    expect(report.counts).toEqual({ total: 0, exact: 0, alias: 0, unresolvable: 0, not_checked: 0 });
    const full = renderModelPinReport(report);
    expect(full).not.toContain("Action required");
    expect(full).toMatch(/\| Workflow \| Field \| Configured \| Provider \| Result \| Detail \|\n\|---\|---\|---\|---\|---\|---\|\n$/);
  });
});

describe("model_pin_check collection and rules", () => {
  it("collects engine, detection, evals, sub-agent and imported pins with file and field", () => {
    const report = check({
      ".github/workflows/full.md": [
        "---",
        "engine:",
        "  id: copilot",
        "  model: gpt-5-mini",
        "  fallback-models: [copilot/claude-opus-5]",
        "imports:",
        "  - shared/models.md",
        "  - ../agents/helper.md",
        "safe-outputs:",
        "  threat-detection:",
        "    model: sonnet",
        "evals:",
        "  - question: ok?",
        "    model: mini",
        "permissions:",
        "  models: read",
        "---",
        "# Full",
        "",
        "## agent: `nano`",
        "---",
        "model: inherited",
        "---",
        "Help.",
      ].join("\n"),
      ".github/workflows/shared/models.md": "---\nmodels:\n  providers:\n    copilot:\n      models: {}\n  team-fast:\n    - copilot/claude-haiku-4.5\n---\n",
      ".github/agents/helper.md": "---\nname: helper\nmodel: copilot/claude-opus-4.8\n---\nHelper.\n",
    });
    const rows = report.pins.map(result => [result.source, result.field, result.status]);
    expect(rows).toEqual(
      expect.arrayContaining([
        [".github/workflows/full.md", "engine.model", "exact"],
        [".github/workflows/full.md", "engine.fallback-models[0]", "unresolvable"],
        [".github/workflows/full.md", "safe-outputs.threat-detection.model", "alias"],
        [".github/workflows/full.md", "evals[0].model", "alias"],
        [".github/workflows/full.md", "## agent: 'nano' model", "not_checked"],
        [".github/agents/helper.md", "model (imported agent file)", "unresolvable"],
        [".github/workflows/shared/models.md", "models.team-fast", "alias"],
      ])
    );
    expect(report.pins.some(result => result.field.includes("providers"))).toBe(false);
    expect(pin(report, "## agent: 'nano' model").reason).toMatch(/^sentinel `inherited`/);
  });

  it("resolves nested imports like the compiler (shared/ paths from the workflow directory)", () => {
    const report = check({
      ".github/workflows/nested.md": "---\nengine: copilot\nimports:\n  - shared/a.md\n---\n",
      ".github/workflows/shared/a.md": "---\nimports:\n  - shared/b.md\n  - ./c.md\n---\n",
      ".github/workflows/shared/b.md": "---\nmodels:\n  b-alias: [copilot/gpt-5-mini]\n---\n",
      ".github/workflows/shared/c.md": "---\nmodels:\n  c-alias: [copilot/gpt-5-mini]\n---\n",
    });
    expect(report.errors).toEqual([]);
    expect(pin(report, "models.b-alias").source).toBe(".github/workflows/shared/b.md");
    expect(pin(report, "models.c-alias").source).toBe(".github/workflows/shared/c.md");
  });

  it("does not let imported aliases override built-ins, but keeps main-workflow overrides", () => {
    const report = check({
      ".github/workflows/aliases.md": "---\nimports:\n  - shared/aliases.md\nengine: copilot\nmodel: sonnet\n---\n",
      ".github/workflows/shared/aliases.md": "---\nmodels:\n  sonnet: [copilot/claude-opus-5]\n---\n",
    });
    expect(pin(report, "model")).toMatchObject({ status: "alias", resolved: "claude-sonnet-5.5" });
    expect(report.action_required).toEqual([]);
    expect(report.pins.some(result => result.field === "models.sonnet")).toBe(false);

    const mainOverride = check({
      ".github/workflows/aliases.md": "---\nimports:\n  - shared/aliases.md\nengine: copilot\nmodel: sonnet\nmodels:\n  sonnet: [copilot/claude-opus-5]\n---\n",
      ".github/workflows/shared/aliases.md": "---\nmodels:\n  sonnet: [copilot/claude-opus-5]\n---\n",
    });
    expect(pin(mainOverride, "model")).toMatchObject({ status: "unresolvable", cause: "model_unavailable" });
  });

  it("lets an imported engine supply a missing ID while retaining the main model preference", () => {
    const report = check({
      ".github/workflows/preference.md": "---\nimports:\n  - shared/engine.md\nengine:\n  model: claude-sonnet-4-6\n---\n",
      ".github/workflows/shared/engine.md": "---\nengine: claude\n---\n",
    });
    expect(pin(report, "engine.model")).toMatchObject({ engine: "claude", provider: "anthropic", status: "not_checked", reason: "provider not in the Copilot catalogue" });
  });

  it("uses engine.model before top-level model and an imported model as fallback", () => {
    const explicit = check({
      ".github/workflows/precedence.md": ["---", "engine:", "  id: claude", "  model: copilot/gpt-5-mini", "  fallback-models: [claude-opus-5]", "model: anthropic/claude-sonnet-4-6", "---"].join("\n"),
    });
    expect(pin(explicit, "engine.fallback-models[0]")).toMatchObject({ provider: "copilot", status: "unresolvable", cause: "model_unavailable" });

    const imported = check({
      ".github/workflows/default.md": "---\nimports:\n  - shared/default.md\n---\n",
      ".github/workflows/shared/default.md": "---\nengine: claude\nmodel: copilot/claude-opus-5\n---\n",
    });
    expect(pin(imported, "model")).toMatchObject({ engine: "claude", provider: "copilot", status: "unresolvable", cause: "model_unavailable" });
  });

  it("uses imported detection configuration unless the main workflow overrides or disables it", () => {
    const imported = {
      ".github/workflows/detection.md": "---\nengine: claude\nimports:\n  - shared/detection.md\n---\n",
      ".github/workflows/shared/detection.md": "---\nsafe-outputs:\n  threat-detection:\n    engine: copilot\n    model: claude-opus-5\n---\n",
    };
    expect(pin(check(imported), "safe-outputs.threat-detection.model")).toMatchObject({
      engine: "copilot",
      provider: "copilot",
      status: "unresolvable",
      cause: "model_unavailable",
    });

    const overridden = check({
      ...imported,
      ".github/workflows/detection.md": "---\nengine: claude\nimports:\n  - shared/detection.md\nsafe-outputs:\n  threat-detection:\n    model: anthropic/claude-sonnet-4-6\n---\n",
    });
    expect(pin(overridden, "safe-outputs.threat-detection.model")).toMatchObject({
      provider: "anthropic",
      status: "not_checked",
      reason: "provider not in the Copilot catalogue",
    });

    const disabled = check({
      ...imported,
      ".github/workflows/detection.md": "---\nengine: claude\nimports:\n  - shared/detection.md\nsafe-outputs:\n  threat-detection: false\n---\n",
    });
    expect(pin(disabled, "safe-outputs.threat-detection.model")).toMatchObject({
      status: "not_checked",
      reason: "threat detection disabled by the main workflow",
    });
    expect(disabled.action_required).toEqual([]);
  });

  it("inherits detector BYOK settings for matching engines and honors an explicit empty override", () => {
    const inherited = check({
      ".github/workflows/byok-detection.md": [
        "---",
        "engine:",
        "  id: copilot",
        "  env:",
        "    COPILOT_PROVIDER_BASE_URL: https://example.invalid/openai/v1",
        "safe-outputs:",
        "  threat-detection:",
        "    engine: copilot",
        "    model: o4-mini-aw",
        "---",
      ].join("\n"),
    });
    expect(pin(inherited, "safe-outputs.threat-detection.model")).toMatchObject({ provider: "byok", status: "not_checked", reason: expect.stringContaining("BYOK") });

    const overridden = check({
      ".github/workflows/byok-detection.md": [
        "---",
        "engine:",
        "  id: copilot",
        "  env:",
        "    COPILOT_PROVIDER_BASE_URL: https://example.invalid/openai/v1",
        "safe-outputs:",
        "  threat-detection:",
        "    engine:",
        "      id: copilot",
        "      env:",
        "        COPILOT_PROVIDER_BASE_URL: ''",
        "    model: claude-opus-5",
        "---",
      ].join("\n"),
    });
    expect(pin(overridden, "safe-outputs.threat-detection.model")).toMatchObject({ provider: "copilot", status: "unresolvable", cause: "model_unavailable" });
  });

  it("uses detection engine.model and the effective detector selected from the main provider", () => {
    const explicitModel = check({
      ".github/workflows/detection-model.md": [
        "---",
        "engine: claude",
        "safe-outputs:",
        "  threat-detection:",
        "    model: anthropic/claude-sonnet-4-6",
        "    engine:",
        "      id: claude",
        "      model: copilot/gpt-5-mini",
        "      fallback-models: [claude-opus-5]",
        "---",
      ].join("\n"),
    });
    expect(pin(explicitModel, "safe-outputs.threat-detection.model")).toMatchObject({ provider: "anthropic", status: "not_checked" });
    expect(pin(explicitModel, "safe-outputs.threat-detection.engine.fallback-models[0]")).toMatchObject({ provider: "copilot", status: "unresolvable", cause: "model_unavailable" });

    const providerSelected = check({
      ".github/workflows/provider-detection.md": "---\nengine: claude\nmodel: copilot/gpt-5-mini\nsafe-outputs:\n  threat-detection:\n    model: claude-opus-5\n---\n",
    });
    expect(pin(providerSelected, "safe-outputs.threat-detection.model")).toMatchObject({ engine: "copilot", provider: "copilot", status: "unresolvable", cause: "model_unavailable" });
  });

  it("uses a custom engine's declared detection engine", () => {
    const report = check({
      ".github/workflows/custom-detection.md": "---\nimports:\n  - shared/custom-engine.md\nengine: cursor\nsafe-outputs:\n  threat-detection:\n    model: claude-opus-5\n---\n",
      ".github/workflows/shared/custom-engine.md": "---\nengine:\n  id: cursor\n  provider: anthropic\n  detection-engine: copilot\n---\n",
    });
    expect(pin(report, "safe-outputs.threat-detection.model")).toMatchObject({ engine: "copilot", provider: "copilot", status: "unresolvable", cause: "model_unavailable" });
  });

  it("resolves eval model providers from the eval override", () => {
    const report = check({
      ".github/workflows/eval.md": "---\nengine: claude\nmodel: copilot/gpt-5-mini\nevals:\n  model: claude-sonnet-4-6\n---\n",
    });
    expect(pin(report, "model")).toMatchObject({ provider: "copilot", status: "exact" });
    expect(pin(report, "evals.model")).toMatchObject({ engine: "claude", provider: "anthropic", status: "not_checked" });

    const explicitProvider = check({
      ".github/workflows/eval-provider.md": "---\nengine:\n  id: claude\n  model-provider: openai\nmodel: copilot/gpt-5-mini\nevals:\n  model: claude-sonnet-4-6\n---\n",
    });
    expect(pin(explicitProvider, "evals.model")).toMatchObject({ provider: "openai", status: "not_checked" });

    const byok = check({
      ".github/workflows/eval-byok.md": "---\nengine:\n  id: copilot\n  env:\n    COPILOT_PROVIDER_BASE_URL: https://example.invalid/openai/v1\nevals:\n  model: o4-mini-aw\n---\n",
    });
    expect(pin(byok, "evals.model")).toMatchObject({ provider: "byok", status: "not_checked", reason: expect.stringContaining("BYOK") });
  });

  it("does not check direct or nested expression aliases, but preserves ordered fallbacks and exact-ID precedence", () => {
    const direct = check({
      ".github/workflows/direct.md": '---\nengine: copilot\nmodel: dynamic\nmodels:\n  dynamic: ["copilot/${{ vars.MODEL }}"]\n---\n',
    });
    expect(pin(direct, "model")).toMatchObject({ status: "not_checked", reason: "expression" });

    const nested = check({
      ".github/workflows/nested.md": '---\nengine: copilot\nmodel: dynamic\nmodels:\n  dynamic: [intermediate]\n  intermediate: ["copilot/${{ vars.MODEL }}"]\n---\n',
    });
    expect(pin(nested, "model")).toMatchObject({ status: "not_checked", reason: "expression" });

    const imported = check({
      ".github/workflows/imported.md": "---\nengine: copilot\nmodel: dynamic\nimports:\n  - shared/models.md\n---\n",
      ".github/workflows/shared/models.md": '---\nmodels:\n  dynamic: ["copilot/${{ vars.MODEL }}"]\n---\n',
    });
    expect(pin(imported, "model")).toMatchObject({ status: "not_checked", reason: "expression" });

    const ordered = check({
      ".github/workflows/ordered.md": '---\nengine: copilot\nmodel: dynamic\nmodels:\n  dynamic: [copilot/gpt-5-mini, "copilot/${{ vars.MODEL }}"]\n---\n',
    });
    expect(pin(ordered, "model")).toMatchObject({ status: "alias", resolved: "gpt-5-mini" });

    const exact = check({
      ".github/workflows/exact.md": '---\nengine: copilot\nmodel: gpt-5-mini\nmodels:\n  gpt-5-mini: ["copilot/${{ vars.MODEL }}"]\n---\n',
    });
    expect(pin(exact, "model")).toMatchObject({ status: "exact", resolved: "gpt-5-mini" });
  });

  it("does not check Copilot BYOK pins", () => {
    const report = check({ ".github/workflows/byok.md": "---\nengine:\n  id: copilot\n  env:\n    COPILOT_PROVIDER_BASE_URL: https://example.invalid/openai/v1\nmodel: o4-mini-aw\n---\n" });
    expect(pin(report, "model")).toMatchObject({ status: "not_checked", reason: expect.stringContaining("BYOK") });
  });

  it("honours a copilot/ prefix over the engine provider and keeps Claude copilot/auto as a sentinel", () => {
    const report = check({ ".github/workflows/claude.md": "---\nengine:\n  id: claude\n  model: copilot/auto\n  fallback-models: [copilot/claude-sonnet-4.6, claude-sonnet-4-6]\n---\n" });
    expect(pin(report, "engine.fallback-models[0]")).toMatchObject({ provider: "copilot", status: "unresolvable", cause: "model_unavailable" });
    expect(pin(report, "engine.model")).toMatchObject({ status: "not_checked", reason: expect.stringMatching(/^sentinel `auto`/) });
    // A bare fallback follows the engine provider, which a copilot/ main model selects.
    expect(pin(report, "engine.fallback-models[1]")).toMatchObject({ provider: "copilot", status: "unresolvable", cause: "model_unavailable" });
  });

  it("reports catalog_unavailable when /reflect has no Copilot model list", () => {
    const report = check({ ".github/workflows/mini.md": "---\nengine: copilot\nmodel: gpt-5-mini\n---\n" }, incompleteDiscovery());
    expect(pin(report, "model")).toMatchObject({ status: "unresolvable", cause: "catalog_unavailable" });
    const summary = renderModelPinSummary(report);
    expect(summary.startsWith("### Action required")).toBe(true);
    expect(summary).toContain("1 Copilot workflow model pin(s) could not be checked");
    expect(summary).not.toContain("| Workflow | Field | Configured | Cause |");
    expect(summary).toContain("Live Copilot catalogue: **unavailable**");
  });

  it("main writes the JSON, full and summary reports", () => {
    const root = fixtureRepo({ ".github/workflows/gate.md": "---\nengine: pi\nmodel: copilot/claude-opus-5\n---\n" });
    const reflectFile = path.join(root, "reflect.json");
    fs.writeFileSync(reflectFile, JSON.stringify(ddgReflect()));
    const outDir = path.join(root, "out");
    const write = process.stdout.write;
    /** @type {string[]} */
    const printed = [];
    process.stdout.write = /** @type {any} */ chunk => printed.push(String(chunk));
    try {
      main(["--reflect", reflectFile, "--out-dir", outDir, "--repo", root]);
    } finally {
      process.stdout.write = write;
    }
    const json = JSON.parse(fs.readFileSync(path.join(outDir, "model-pins.json"), "utf8"));
    expect(json.counts.unresolvable).toBe(1);
    expect(fs.readFileSync(path.join(outDir, "model-pins.md"), "utf8")).toContain("### All Workflow Model Pins");
    expect(fs.readFileSync(path.join(outDir, "model-pins-summary.md"), "utf8")).toBe(printed.join(""));
  });

  it("requires /reflect input with a stable error code", () => {
    expect(() => main([])).toThrow("ERR_MODEL_PIN_REFLECT_REQUIRED: --reflect <path to /reflect JSON> is required");
  });
});

describe("repository-pins-reflection-regression", () => {
  // Recorded byte-exact /reflect from smoke-copilot run 39506475007 (36 Copilot models);
  // see test_data/awf_reflect/README.md for provenance.
  const TEST_DIR = path.dirname(fileURLToPath(import.meta.url));
  const REFLECT_FILE = path.join(TEST_DIR, "test_data", "awf_reflect", "run-39506475007-awf-reflect.json");
  const REFLECT_SHA256 = "3d9fa7af7e07e93100bb614e45eb85abafbd16a3bbb2cb62a687a04cb5fff9b3";
  const REPO_ROOT = path.resolve(TEST_DIR, "..", "..", "..");

  // Pins that did not resolve against this catalogue before github/gh-aw#70649, by workflow and field.
  const FIXED_PINS = [
    [".github/workflows/daily-model-resolution.md", "## agent: 'run-analyzer' model"],
    [".github/workflows/engine-conformance-aider.md", "model"],
    [".github/workflows/engine-conformance-crush.md", "model"],
    [".github/workflows/engine-conformance-deepseek-harness.md", "model"],
    [".github/workflows/engine-conformance-goose.md", "model"],
    [".github/workflows/engine-conformance-opencode.md", "model"],
    [".github/workflows/engine-conformance-pi.md", "model"],
    [".github/workflows/engine-conformance-pydantic-ai.md", "model"],
    [".github/workflows/smoke-aider.md", "model"],
    [".github/workflows/smoke-copilot-arm.md", "engine.fallback-models[0]"],
    [".github/workflows/smoke-copilot-routed.md", "engine.model-routing.allowed-models[0]"],
    [".github/workflows/smoke-copilot-sdk-routed-claude.md", "## agent: 'mini-whoami' model"],
    [".github/workflows/smoke-copilot-sdk.md", "engine.fallback-models[1]"],
    [".github/workflows/smoke-copilot-sub-agents.md", "## agent: 'gpt5-whoami' model"],
    [".github/workflows/smoke-copilot-sub-agents.md", "## agent: 'terra-whoami' model"],
    [".github/workflows/smoke-copilot.md", "engine.fallback-models[0]"],
    [".github/workflows/smoke-goose.md", "model"],
    [".github/workflows/smoke-pi-routed.md", "## agent: 'mini-whoami' model"],
    [".github/workflows/smoke-pi-sub-agents.md", "## agent: 'luna-whoami' model"],
    [".github/workflows/smoke-pi.md", "engine.fallback-models[0]"],
    [".github/workflows/smoke-service-ports.md", "engine.fallback-models[0]"],
    [".github/workflows/smoke-service-ports.md", "engine.fallback-models[1]"],
    [".github/workflows/smoke-work-queue.md", "engine.fallback-models[0]"],
    [".github/workflows/smoke-work-queue.md", "engine.fallback-models[1]"],
  ];

  // Known unresolvable pins on main, by exact identity; any other unresolvable pin fails.
  const KNOWN_MAIN_FAILURES = [];

  function repositoryReport() {
    const bytes = fs.readFileSync(REFLECT_FILE);
    expect(crypto.createHash("sha256").update(bytes).digest("hex")).toBe(REFLECT_SHA256);
    const collected = collectModelPins({ repoRoot: REPO_ROOT });
    return buildModelPinReport({ ...collected, reflectData: JSON.parse(bytes.toString("utf8")) });
  }

  it("classifies the repository's workflow pins against the run 39506475007 catalogue", () => {
    const report = repositoryReport();
    expect(report.catalogue).toEqual({ available: true, model_count: 36, error: null });
    expect(report.errors).toEqual([]);

    const fixedWorkflows = [...new Set(FIXED_PINS.map(([workflow]) => workflow))];
    expect(fixedWorkflows).toHaveLength(21);
    for (const workflow of fixedWorkflows) {
      expect(report.workflows).toContain(workflow);
      const unresolved = report.pins.filter(result => result.workflow === workflow && result.status === "unresolvable");
      expect(unresolved, `${workflow} has unresolvable pins`).toEqual([]);
    }
    for (const [workflow, field] of FIXED_PINS) {
      const rows = report.pins.filter(result => result.workflow === workflow && result.field === field);
      expect(rows, `${workflow} ${field}`).toHaveLength(1);
      expect(["exact", "alias"], `${workflow} ${field} is ${rows[0].status} (${rows[0].configured})`).toContain(rows[0].status);
      expect(rows[0].resolved, `${workflow} ${field}`).toBeTruthy();
    }

    const unresolvable = report.action_required.map(({ workflow, field, configured, cause }) => ({ workflow, field, configured, cause }));
    expect(unresolvable).toEqual([]);
    expect(report.counts.unresolvable).toBe(0);
  });

  it("resolves the sub-agent aliases and keeps the GPT sub-agents on GPT models", () => {
    const report = repositoryReport();
    const row = (/** @type {string} */ workflow, /** @type {string} */ field) => report.pins.find(result => result.workflow === `.github/workflows/${workflow}` && result.field === field);
    expect(row("smoke-copilot-sub-agents.md", "## agent: 'gpt5-whoami' model")).toMatchObject({ configured: "gpt-5", status: "alias", resolved: "gpt-5.6-luna" });
    expect(row("smoke-copilot-sub-agents.md", "## agent: 'terra-whoami' model")).toMatchObject({ configured: "gpt-5.6-terra", status: "exact", resolved: "gpt-5.6-terra" });
    // The GPT sub-agents must serve distinct models, neither the main model, so proxy requests identify each one.
    const subAgentModels = ["gpt5-whoami", "terra-whoami"].map(agent => row("smoke-copilot-sub-agents.md", `## agent: '${agent}' model`)?.resolved);
    expect(new Set([...subAgentModels, "gpt-5.3-codex"]).size).toBe(3);
    expect(row("smoke-copilot-sdk-routed-claude.md", "## agent: 'mini-whoami' model")).toMatchObject({ status: "exact", resolved: "gpt-5.6-luna" });
    expect(row("smoke-pi-routed.md", "## agent: 'mini-whoami' model")).toMatchObject({ status: "exact", resolved: "copilot/gpt-5.3-codex" });
    expect(row("smoke-pi-sub-agents.md", "## agent: 'luna-whoami' model")).toMatchObject({ status: "exact", resolved: "copilot/gpt-5.6-luna" });
    expect(row("daily-model-resolution.md", "## agent: 'run-analyzer' model")).toMatchObject({ configured: "mini", status: "alias", resolved: "claude-haiku-5.5" });
  });
});
