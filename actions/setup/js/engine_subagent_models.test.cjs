import { afterEach, describe, it, expect, vi } from "vitest";
import { createRequire } from "module";
import crypto from "crypto";
import fs from "fs";
import os from "os";
import path from "path";
import { parse as parseToml } from "smol-toml";

const require = createRequire(import.meta.url);
global.core = { info: () => {}, warning: () => {}, error: () => {}, setFailed: () => {} };
const { applyClaudeSubagentModels, applyCodexSubagentModels, isDetectionPhase, replaceAgentModel } = require("./engine_subagent_models.cjs");
const { extractInlineSubAgents, writeInlineSubAgents } = require("./extract_inline_sub_agents.cjs");
const { claudeBareCapabilities, removeClaudePlugin } = require("./claude_runtime.cjs");
const { extractFrontmatterAndBody } = require("./frontmatter_hash_pure.cjs");
const { parseDocument } = require("./work_queue_yaml.cjs");
const { recordModelResolution } = require("./resolve_model_alias.cjs");
const runs = require("./fixtures/claude_codex_subagent_runs.cjs");

const repoRoot = path.resolve(path.dirname(require.resolve("./engine_subagent_models.cjs")), "../../..");
const tempDirs = [];
afterEach(() => {
  for (const dir of tempDirs.splice(0)) fs.rmSync(dir, { recursive: true, force: true });
});

const frontmatterOf = content => parseDocument(extractFrontmatterAndBody(content).frontmatterText).toJS() || {};
const sha = content => crypto.createHash("sha256").update(content).digest("hex");

/**
 * A run layout: the activation artifact under `<tmp>/gh-aw`, restored into the workspace as
 * restore_inline_sub_agents.sh does, and `GH_AW_TMP_DIR` for model-resolution records.
 */
function layout() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "engine-subagent-models-"));
  tempDirs.push(dir);
  const agentsBaseDir = path.join(dir, "gh-aw");
  const workspaceDir = path.join(dir, "workspace");
  const env = { GH_AW_TMP_DIR: path.join(dir, "records"), GH_AW_AWF_CONFIG_PATH: path.join(dir, "missing-awf-config.json"), AWF_REFLECT_ENABLED: "1" };
  const records = () => {
    const file = path.join(env.GH_AW_TMP_DIR, "agent", "model-resolution.json");
    return fs.existsSync(file) ? JSON.parse(fs.readFileSync(file, "utf8")) : [];
  };
  const logs = [];
  const logger = message => logs.push(message);
  return { dir, agentsBaseDir, workspaceDir, env, records, logs, logger };
}

/** Stage inline agents through the activation writer and restore them into the workspace. */
function stageInline(run, engine, agents) {
  const markdown = ["# Workflow", "", "Main prompt.", ...Object.entries(agents).flatMap(([name, content]) => ["", `## agent: \`${name}\``, content.trimEnd()])].join("\n");
  writeInlineSubAgents(markdown, run.workspaceDir, run.agentsBaseDir, engine);
  const dir = engine === "claude" ? ".claude/agents" : ".codex/agents";
  fs.mkdirSync(path.join(run.workspaceDir, dir), { recursive: true });
  for (const name of fs.readdirSync(path.join(run.agentsBaseDir, dir))) fs.copyFileSync(path.join(run.agentsBaseDir, dir, name), path.join(run.workspaceDir, dir, name));
}

const typistAgent = () => fs.readFileSync(path.join(runs.typistAgentsDir, "duplicate-type-finder.md"), "utf8");
const arxivAgents = () => Object.fromEntries(fs.readdirSync(runs.arxivAgentsDir).map(name => [name.slice(0, -".md".length), fs.readFileSync(path.join(runs.arxivAgentsDir, name), "utf8")]));

describe("Claude sub-agent models (Typist run 39415087125)", () => {
  it("claude_declared_small_resolves_before_spawn", async () => {
    const run = layout();
    stageInline(run, "claude", { "duplicate-type-finder": typistAgent() });

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome.ok).toBe(true);
    expect(outcome.resolutions).toEqual([{ name: "duplicate-type-finder", configuredModel: "small", model: "claude-haiku-5-5", status: "resolved" }]);
    const workspaceCopy = fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/duplicate-type-finder.md"), "utf8");
    expect(frontmatterOf(workspaceCopy)).toMatchObject({ name: runs.typistAgentToolUse.input.subagent_type, model: "claude-haiku-5-5" });
    expect(extractFrontmatterAndBody(workspaceCopy).markdown).toBe(extractFrontmatterAndBody(typistAgent()).markdown);
    expect(frontmatterOf(fs.readFileSync(path.join(run.agentsBaseDir, ".claude/agents/duplicate-type-finder.md"), "utf8")).model).toBe("small");
    expect(run.records()).toEqual([{ engine: "claude", role: "duplicate-type-finder", configured_model: "small", resolved_model: "claude-haiku-5-5", provider: "anthropic", status: "resolved", cause: null }]);
  });

  it.each([
    // Fixture mutation: the recorded catalogue with claude-haiku-5-5 removed.
    ["a requested id absent from the catalogue", "claude-haiku-5-5", reflect => reflect.endpoints.forEach(endpoint => endpoint.models && (endpoint.models = endpoint.models.filter(model => model !== "claude-haiku-5-5")))],
    // Fixture mutation: an injected unknown selector.
    ["an unknown selector", "claude-nonexistent-9", () => {}],
  ])("claude_declared_dead_or_unknown_model_fails_before_spawn: %s", async (_name, model, mutate) => {
    const run = layout();
    stageInline(run, "claude", { "duplicate-type-finder": typistAgent().replace("model: small", `model: ${model}`) });
    const reflectData = runs.typistReflect();
    mutate(reflectData);
    let refreshes = 0;
    const before = fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/duplicate-type-finder.md"), "utf8");

    const outcome = await applyClaudeSubagentModels({
      reflectData,
      refetchReflectData: async () => (refreshes++, reflectData),
      provider: "anthropic",
      workspaceDir: run.workspaceDir,
      agentsBaseDir: run.agentsBaseDir,
      env: run.env,
      logger: run.logger,
    });

    expect(outcome.ok).toBe(false);
    expect(refreshes).toBe(1);
    expect(run.records()).toEqual([{ engine: "claude", role: "duplicate-type-finder", configured_model: model, resolved_model: null, provider: "anthropic", status: "failed", cause: "model_unavailable" }]);
    expect(run.logs).toContainEqual(expect.stringContaining(`claude sub-agent 'duplicate-type-finder': The requested model is not supported: '${model}' is not available in the workflow's live model catalog.`));
    expect(run.logs).toContainEqual(expect.stringContaining("Refusing to start Claude with a different sub-agent model."));
    expect(fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/duplicate-type-finder.md"), "utf8")).toBe(before);
  });

  it("records catalog_unavailable when the provider's catalogue is missing", async () => {
    const run = layout();
    stageInline(run, "claude", { "duplicate-type-finder": typistAgent().replace("model: small", "model: claude-haiku-5-5") });
    // Fixture mutation: model discovery incomplete with no Anthropic models.
    const reflectData = runs.typistReflect();
    reflectData.models_fetch_complete = false;
    reflectData.endpoints.forEach(endpoint => (endpoint.models = null));

    const outcome = await applyClaudeSubagentModels({ reflectData, provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome.ok).toBe(false);
    expect(run.records()).toEqual([expect.objectContaining({ role: "duplicate-type-finder", status: "failed", cause: "catalog_unavailable" })]);
  });

  it("records every agent as catalog_unavailable when the /reflect refresh rejects", async () => {
    const run = layout();
    stageInline(run, "claude", { "first-worker": typistAgent().replace("model: small", "model: claude-haiku-5-5"), "second-worker": typistAgent().replace("model: small", "model: claude-sonnet-5-5") });
    // Fixture mutation: model discovery incomplete, and the refresh fails.
    const reflectData = runs.typistReflect();
    reflectData.models_fetch_complete = false;
    reflectData.endpoints.forEach(endpoint => (endpoint.models = null));
    let refreshes = 0;

    const outcome = await applyClaudeSubagentModels({
      reflectData,
      refetchReflectData: async () => {
        refreshes++;
        throw new Error("connect ECONNREFUSED");
      },
      provider: "anthropic",
      workspaceDir: run.workspaceDir,
      agentsBaseDir: run.agentsBaseDir,
      env: run.env,
      logger: run.logger,
    });

    expect(outcome.ok).toBe(false);
    expect(refreshes).toBe(1);
    expect(run.records()).toEqual([expect.objectContaining({ role: "first-worker", status: "failed", cause: "catalog_unavailable" }), expect.objectContaining({ role: "second-worker", status: "failed", cause: "catalog_unavailable" })]);
    expect(run.logs).toContainEqual(expect.stringContaining("cannot refresh the awf-reflect catalogue: connect ECONNREFUSED"));
  });

  it("fails closed when a staged workflow sub-agent file cannot be parsed", async () => {
    const run = layout();
    stageInline(run, "claude", { "duplicate-type-finder": typistAgent() });
    // Fixture mutation: a second staged agent with malformed frontmatter.
    fs.writeFileSync(path.join(run.agentsBaseDir, ".claude/agents/broken-worker.md"), "---\nname: [unterminated\nmodel: small\n---\nBody.\n");
    const before = fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/duplicate-type-finder.md"), "utf8");

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome).toEqual({ ok: false, resolutions: [] });
    expect(run.logs).toContainEqual(expect.stringContaining(`claude sub-agent file ${path.join(run.agentsBaseDir, ".claude/agents/broken-worker.md")}: cannot read or parse the staged workflow sub-agent`));
    expect(run.logs).toContainEqual(expect.stringContaining("Refusing to start Claude: 1 workflow-declared sub-agent file(s) cannot be read or parsed"));
    expect(fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/duplicate-type-finder.md"), "utf8")).toBe(before);
  });

  it("keeps a malformed repository agent file lenient", async () => {
    const run = layout();
    stageInline(run, "claude", { "duplicate-type-finder": typistAgent() });
    // Fixture mutation: a repository agent with malformed frontmatter.
    const repositoryPath = path.join(run.workspaceDir, ".claude/agents/broken-repo.md");
    fs.writeFileSync(repositoryPath, "---\nname: [unterminated\n---\nBody.\n");

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome.ok).toBe(true);
    expect(run.logs).toContainEqual(expect.stringContaining(`warning: cannot read custom agent ${repositoryPath}`));
  });

  it("claude_inherit_uses_main_without_subagent_record", async () => {
    const run = layout();
    const source = fs.readFileSync(path.join(repoRoot, ".github/workflows/agent-persona-explorer.md"), "utf8");
    const persona = extractInlineSubAgents(extractFrontmatterAndBody(source).markdown).agents.find(agent => agent.name === "persona-evaluator");
    expect(frontmatterOf(persona.content).model).toBe("inherit");
    stageInline(run, "claude", { "persona-evaluator": persona.content });
    const before = fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/persona-evaluator.md"), "utf8");

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome).toEqual({ ok: true, resolutions: [] });
    expect(run.records()).toEqual([]);
    expect(fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/persona-evaluator.md"), "utf8")).toBe(before);
    expect(frontmatterOf(before).model).toBe("inherit");
  });

  it("claude_native_aliases_use_shared_resolution", async () => {
    const run = layout();
    const agent = model => typistAgent().replace("model: small", `model: ${model}`);
    stageInline(run, "claude", { "haiku-worker": agent("haiku"), "sonnet-worker": agent("sonnet"), "opus-worker": agent("opus") });

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome.ok).toBe(true);
    expect(Object.fromEntries(outcome.resolutions.map(resolution => [resolution.name, [resolution.model, resolution.status]]))).toEqual({
      "haiku-worker": ["claude-haiku-5-5", "resolved"],
      "opus-worker": ["claude-opus-5-5", "resolved"],
      "sonnet-worker": ["claude-sonnet-5-5", "resolved"],
    });
    for (const name of ["haiku-worker", "sonnet-worker", "opus-worker"]) {
      const record = run.records().find(entry => entry.role === name);
      expect(record).toMatchObject({ engine: "claude", status: "resolved", provider: "anthropic" });
      expect(frontmatterOf(fs.readFileSync(path.join(run.workspaceDir, ".claude/agents", `${name}.md`), "utf8")).model).toBe(record.resolved_model);
    }
  });

  it("repository_unresolvable_model_warns_records_and_continues", async () => {
    const run = layout();
    stageInline(run, "claude", { "duplicate-type-finder": typistAgent() });
    // Fixture mutation: a repository agent with an unknown model.
    const repositoryAgent = "---\nname: repo-reviewer\ndescription: Reviews code\nmodel: claude-nonexistent-9\ntools: Read, Grep\n---\nReview.\n";
    fs.writeFileSync(path.join(run.workspaceDir, ".claude/agents/repo-reviewer.md"), repositoryAgent);

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome.ok).toBe(true);
    expect(sha(fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/repo-reviewer.md"), "utf8"))).toBe(sha(repositoryAgent));
    expect(run.records()).toContainEqual({ engine: "claude", role: "repo-reviewer", configured_model: "claude-nonexistent-9", resolved_model: null, provider: "anthropic", status: "failed", cause: "model_unavailable" });
    expect(run.logs).toContainEqual(expect.stringContaining("warning: repository agent 'repo-reviewer'"));
    expect(run.logs.join("\n")).not.toMatch(/The requested model is not supported/);
  });

  it("claude_repository_override_respects_native_precedence", async () => {
    const run = layout();
    const home = path.join(run.dir, "home");
    // Claude Code ranks project agents above user agents, so a user-level copy cannot override
    // a project agent; no tested session-local override exists. The project file stays as is.
    const repositoryAgent = "---\nname: repo-reviewer\ndescription: Reviews code\nmodel: small\ntools: Read, Grep\npermissionMode: plan\n---\nReview.\n";
    fs.mkdirSync(path.join(run.workspaceDir, ".claude/agents"), { recursive: true });
    fs.writeFileSync(path.join(run.workspaceDir, ".claude/agents/repo-reviewer.md"), repositoryAgent);

    const outcome = await applyClaudeSubagentModels({
      reflectData: runs.typistReflect(),
      provider: "anthropic",
      workspaceDir: run.workspaceDir,
      agentsBaseDir: run.agentsBaseDir,
      env: { ...run.env, HOME: home },
      logger: run.logger,
    });

    expect(outcome.ok).toBe(true);
    expect(outcome.resolutions).toEqual([{ name: "repo-reviewer", configuredModel: "small", model: "claude-haiku-5-5", status: "resolved" }]);
    expect(sha(fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/repo-reviewer.md"), "utf8"))).toBe(sha(repositoryAgent));
    expect(fs.existsSync(path.join(home, ".claude"))).toBe(false);
    expect(run.records()).toEqual([expect.objectContaining({ role: "repo-reviewer", resolved_model: "claude-haiku-5-5", status: "resolved" })]);
    expect(run.logs).toContainEqual(expect.stringContaining("resolved to 'claude-haiku-5-5', but the resolved id is not applied"));
  });

  it("skips repository agents without a name, which Claude Code does not register", async () => {
    const run = layout();
    fs.mkdirSync(path.join(run.workspaceDir, ".claude/agents"), { recursive: true });
    fs.writeFileSync(path.join(run.workspaceDir, ".claude/agents/unnamed.md"), "---\nmodel: claude-nonexistent-9\n---\nBody\n");

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome).toEqual({ ok: true, resolutions: [] });
    expect(run.records()).toEqual([]);
    expect(run.logs).toContainEqual(expect.stringContaining("has no frontmatter 'name'"));
  });

  it("rejects a provider prefix the engine is not provisioned for", async () => {
    const run = layout();
    stageInline(run, "claude", { "duplicate-type-finder": typistAgent().replace("model: small", "model: openai/gpt-5.4-mini") });

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome.ok).toBe(false);
    expect(run.records()).toEqual([expect.objectContaining({ role: "duplicate-type-finder", status: "failed", cause: "model_unavailable", provider: "openai" })]);
  });

  it("fails closed for a workflow sub-agent model that is not a string", async () => {
    const run = layout();
    // Fixture mutation: the recorded Typist agent with its selector wrapped in a list.
    stageInline(run, "claude", { "duplicate-type-finder": typistAgent().replace("model: small", "model: [small]") });

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome.ok).toBe(false);
    expect(run.records()).toEqual([expect.objectContaining({ role: "duplicate-type-finder", configured_model: '["small"]', status: "failed", cause: "model_unavailable" })]);
    expect(run.logs).toContainEqual(expect.stringMatching(/claude sub-agent 'duplicate-type-finder': .*is not a string.*Refusing to start Claude/));
  });

  it("gives the bare-mode plugin a run-local copy of the resolved definitions", async () => {
    const run = layout();
    stageInline(run, "claude", { "duplicate-type-finder": typistAgent(), "persona-evaluator": "---\ndescription: Evaluates\nmodel: inherit\n---\nEvaluate.\n" });
    const managedDir = path.join(run.agentsBaseDir, ".claude");
    const stagedPath = path.join(managedDir, "agents/duplicate-type-finder.md");
    const staged = fs.readFileSync(stagedPath, "utf8");
    const repositoryAgent = "---\nname: repo-reviewer\nmodel: small\n---\nReview.\n";
    fs.writeFileSync(path.join(run.workspaceDir, ".claude/agents/repo-reviewer.md"), repositoryAgent);
    // The plugin Claude loads with --bare, as the harness prepares it.
    const { args, pluginDir } = claudeBareCapabilities(["--bare"], managedDir, path.join(run.dir, "agent"));
    expect(args.slice(-2)).toEqual(["--plugin-dir", pluginDir]);
    expect(fs.realpathSync(path.join(pluginDir, "agents"))).toBe(fs.realpathSync(path.join(managedDir, "agents")));

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", pluginDir, workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome.ok).toBe(true);
    const pluginAgents = path.join(pluginDir, "agents");
    expect(fs.lstatSync(pluginAgents).isSymbolicLink()).toBe(false);
    expect(fs.readdirSync(pluginAgents).sort()).toEqual(["duplicate-type-finder.md", "persona-evaluator.md"]);
    const loaded = fs.readFileSync(path.join(pluginAgents, "duplicate-type-finder.md"), "utf8");
    expect(frontmatterOf(loaded)).toMatchObject({ name: "duplicate-type-finder", model: "claude-haiku-5-5" });
    expect(loaded).toBe(fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/duplicate-type-finder.md"), "utf8"));
    expect(frontmatterOf(fs.readFileSync(path.join(pluginAgents, "persona-evaluator.md"), "utf8"))).toMatchObject({ name: "persona-evaluator", model: "inherit" });
    // Staged and repository files are unchanged; removing the plugin removes the copy only.
    expect(fs.readFileSync(stagedPath, "utf8")).toBe(staged);
    expect(frontmatterOf(staged).model).toBe("small");
    expect(fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/repo-reviewer.md"), "utf8")).toBe(repositoryAgent);
    removeClaudePlugin(pluginDir);
    expect(fs.existsSync(pluginDir)).toBe(false);
    expect(fs.readFileSync(stagedPath, "utf8")).toBe(staged);
  });

  it("fails closed for a workflow-declared sub-agent named main", async () => {
    const run = layout();
    stageInline(run, "claude", { main: typistAgent(), "duplicate-type-finder": typistAgent() });
    const before = fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/main.md"), "utf8");

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome).toEqual({ ok: false, resolutions: [] });
    expect(run.records()).toEqual([]);
    expect(run.logs).toContainEqual(expect.stringContaining("the sub-agent name 'main' is reserved for the Claude main model's resolution record. Rename the agent in the workflow. Refusing to start Claude."));
    expect(fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/main.md"), "utf8")).toBe(before);
  });

  it("skips a repository agent named main, so the main and worker records both survive", async () => {
    const run = layout();
    stageInline(run, "claude", { "duplicate-type-finder": typistAgent() });
    const repositoryAgent = "---\nname: main\nmodel: claude-nonexistent-9\n---\nShadow.\n";
    fs.writeFileSync(path.join(run.workspaceDir, ".claude/agents/main-agent.md"), repositoryAgent);
    const mainRecord = { engine: "claude", role: "main", configured_model: "sonnet", resolved_model: "claude-sonnet-5-5", provider: "anthropic", status: "resolved", cause: null };
    recordModelResolution(mainRecord, run.env);

    const outcome = await applyClaudeSubagentModels({ reflectData: runs.typistReflect(), provider: "anthropic", workspaceDir: run.workspaceDir, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger });

    expect(outcome.ok).toBe(true);
    expect(outcome.resolutions.map(resolution => resolution.name)).toEqual(["duplicate-type-finder"]);
    expect(run.records()).toEqual([mainRecord, { engine: "claude", role: "duplicate-type-finder", configured_model: "small", resolved_model: "claude-haiku-5-5", provider: "anthropic", status: "resolved", cause: null }]);
    expect(run.logs).toContainEqual(expect.stringContaining("warning: repository agent " + path.join(run.workspaceDir, ".claude/agents/main-agent.md") + " uses the reserved name 'main'"));
    expect(fs.readFileSync(path.join(run.workspaceDir, ".claude/agents/main-agent.md"), "utf8")).toBe(repositoryAgent);
  });

  it("replaces only the model scalar", () => {
    expect(replaceAgentModel('---\nname: "a"\nmodel: small\ntools: Read\n---\nBody model: small\n', "claude-haiku-5-5")).toBe('---\nname: "a"\nmodel: "claude-haiku-5-5"\ntools: Read\n---\nBody model: small\n');
  });
});

/** Parse a generated Codex role file with a TOML 1.0 parser. */
const parseRoleToml = content => parseToml(content);

describe("Codex sub-agent models (daily-arxiv-researcher run 39394965127)", () => {
  const codexLayout = () => {
    const run = layout();
    return { ...run, codexHome: path.join(run.dir, "codex-home") };
  };
  const apply = (run, options = {}) => applyCodexSubagentModels({ reflectData: runs.arxivReflect(), provider: "openai", codexHome: run.codexHome, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger, ...options });
  const roleFiles = run =>
    fs.existsSync(path.join(run.codexHome, "agents"))
      ? fs
          .readdirSync(path.join(run.codexHome, "agents"))
          .filter(name => name.endsWith(".toml"))
          .sort()
      : [];
  const readRole = (run, name) => parseRoleToml(fs.readFileSync(path.join(run.codexHome, "agents", `${name}.toml`), "utf8"));
  const writeCodexConfig = (run, content) => {
    fs.mkdirSync(run.codexHome, { recursive: true });
    fs.writeFileSync(path.join(run.codexHome, "config.toml"), content);
  };
  const bodyOf = content => extractFrontmatterAndBody(content).markdown.trim();

  it("codex_declared_small_writes_native_toml_before_spawn", async () => {
    const run = codexLayout();
    // Fixture mutation: the recorded paper-screener with its selector changed to `small`.
    const agent = arxivAgents()["paper-screener"].replace("model: openai/gpt-5.4-mini", "model: small");
    stageInline(run, "codex", { "paper-screener": agent });

    const outcome = await apply(run);

    expect(outcome.ok).toBe(true);
    expect(outcome.resolutions).toEqual([{ name: "paper-screener", configuredModel: "small", model: "gpt-5-mini-2025-08-07", status: "resolved" }]);
    expect(roleFiles(run)).toEqual(["paper-screener.toml"]);
    expect(readRole(run, "paper-screener")).toEqual({
      name: "paper-screener",
      description: frontmatterOf(agent).description,
      developer_instructions: bodyOf(agent),
      model: "gpt-5-mini-2025-08-07",
    });
    expect(run.records()).toEqual([{ engine: "codex", role: "paper-screener", configured_model: "small", resolved_model: "gpt-5-mini-2025-08-07", provider: "openai", status: "resolved", cause: null }]);
    expect(run.logs.join("\n")).not.toMatch(/not applied|not loaded/);
  });

  it("writes one native role per recorded arXiv agent with the exact live ids", async () => {
    const run = codexLayout();
    const agents = arxivAgents();
    stageInline(run, "codex", agents);

    const outcome = await apply(run);

    expect(outcome.ok).toBe(true);
    expect(outcome.failures).toEqual([]);
    expect(outcome.resolutions.map(resolution => [resolution.name, resolution.configuredModel, resolution.model, resolution.status])).toEqual([
      ["opportunity-extractor", "openai/gpt-5.6-sol", "gpt-5.6-sol", "exact"],
      ["paper-screener", "openai/gpt-5.4-mini", "gpt-5.4-mini", "exact"],
      ["preliminary-investigator", "openai/gpt-5.4-mini", "gpt-5.4-mini", "exact"],
      ["relevance-ranker", "openai/gpt-5.4-mini", "gpt-5.4-mini", "exact"],
    ]);
    expect(roleFiles(run)).toEqual(["opportunity-extractor.toml", "paper-screener.toml", "preliminary-investigator.toml", "relevance-ranker.toml"]);
    for (const [name, content] of Object.entries(agents)) {
      expect(readRole(run, name)).toEqual({ name, description: frontmatterOf(content).description, developer_instructions: bodyOf(content), model: outcome.resolutions.find(resolution => resolution.name === name).model });
    }
    // Same record fields as the run's recorded main-role model.resolution.
    for (const record of run.records()) {
      expect(Object.keys(record)).toEqual(Object.keys(runs.arxivMainModelResolution.data));
      expect(record).toMatchObject({ engine: "codex", provider: runs.arxivMainModelResolution.data.provider, status: "exact", cause: null });
    }
    // The staged Markdown and its workspace copy are left unchanged.
    for (const [name, content] of Object.entries(agents)) {
      const expected = content.endsWith("\n") ? content : `${content}\n`;
      expect(fs.readFileSync(path.join(run.agentsBaseDir, ".codex/agents", `${name}.md`), "utf8")).toBe(expected);
      expect(fs.readFileSync(path.join(run.workspaceDir, ".codex/agents", `${name}.md`), "utf8")).toBe(expected);
    }
  });

  it.each([
    ["triple quotes", "Write \"\"\"docstrings\"\"\" and '''literals''' verbatim."],
    ["control characters", "Tab\there, bell \u0007, escape \u001b[0m, backspace \b, form feed \f, DEL \u007f and NUL-adjacent \u0001 stay."],
    ["non-ASCII text", "Résumé naïve café — 日本語のテキスト, emoji 🚀 and Ω≈ç√."],
    ["CRLF line endings", "First line.\r\nSecond line.\r\n\r\nThird paragraph."],
    ["lines starting with # or [", "# Not a comment\n[not.a.table]\n[[not.an.array]]\nkey = \"not a key\"\n#also = 'not a comment'"],
  ])("round-trips %s in developer_instructions through a TOML parser", async (_label, extra) => {
    const run = codexLayout();
    // Fixture mutation: the recorded relevance-ranker with extra instruction text, staged
    // directly so the activation writer cannot normalize it.
    const agent = `${arxivAgents()["relevance-ranker"].trimEnd()}\n\n${extra}\n`;
    const stagedDir = path.join(run.agentsBaseDir, ".codex/agents");
    fs.mkdirSync(stagedDir, { recursive: true });
    fs.writeFileSync(path.join(stagedDir, "relevance-ranker.md"), agent);

    const outcome = await apply(run);

    expect(outcome.ok).toBe(true);
    const role = readRole(run, "relevance-ranker");
    expect(Object.keys(role).sort()).toEqual(["description", "developer_instructions", "model", "name"]);
    expect(role.developer_instructions).toBe(bodyOf(agent));
    expect(role.developer_instructions.endsWith(extra)).toBe(true);
    expect(role).toMatchObject({ name: "relevance-ranker", description: frontmatterOf(agent).description, model: "gpt-5.4-mini" });
  });

  it("round-trips quotes, backslashes, multi-line instructions and a declared effort", async () => {
    const run = codexLayout();
    // Fixture mutation: an effort parameter and an instruction line with quotes and backslashes.
    const agent = `${arxivAgents()["relevance-ranker"].replace("model: openai/gpt-5.4-mini", "model: openai/gpt-5.4-mini?effort=low").trimEnd()}\n\nQuote "this" and keep C:\\path\\to and \\n literally.\n`;
    stageInline(run, "codex", { "relevance-ranker": agent });

    const outcome = await apply(run);

    expect(outcome.ok).toBe(true);
    const role = readRole(run, "relevance-ranker");
    expect(role.developer_instructions).toBe(bodyOf(agent));
    expect(role.developer_instructions).toContain('Quote "this" and keep C:\\path\\to and \\n literally.');
    expect(role).toMatchObject({ model: "gpt-5.4-mini", model_reasoning_effort: "low" });
  });

  it("codex_declared_dead_model_fails_before_spawn", async () => {
    const run = codexLayout();
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"] });
    // Fixture mutation: the recorded catalogue (and its refresh) with gpt-5.4-mini removed.
    const reflectData = runs.arxivReflect();
    reflectData.endpoints.forEach(endpoint => endpoint.models && (endpoint.models = endpoint.models.filter(model => model !== "gpt-5.4-mini")));
    let refreshes = 0;

    const outcome = await apply(run, { reflectData, refetchReflectData: async () => (refreshes++, reflectData) });

    expect(outcome.ok).toBe(false);
    expect(refreshes).toBe(1);
    expect(run.records()).toEqual([{ engine: "codex", role: "paper-screener", configured_model: "openai/gpt-5.4-mini", resolved_model: null, provider: "openai", status: "failed", cause: "model_unavailable" }]);
    expect(run.logs).toContainEqual(expect.stringContaining("Refusing to start Codex with a different sub-agent model."));
    expect(roleFiles(run)).toEqual([]);
  });

  it("records catalog_unavailable when the provider's catalogue is missing", async () => {
    const run = codexLayout();
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"] });
    // Fixture mutation: model discovery incomplete with no OpenAI models.
    const reflectData = runs.arxivReflect();
    reflectData.models_fetch_complete = false;
    reflectData.endpoints.forEach(endpoint => (endpoint.models = null));

    const outcome = await apply(run, { reflectData });

    expect(outcome.ok).toBe(false);
    expect(run.records()).toEqual([expect.objectContaining({ role: "paper-screener", resolved_model: null, status: "failed", cause: "catalog_unavailable" })]);
    expect(roleFiles(run)).toEqual([]);
  });

  it("fails closed for a staged agent without a description", async () => {
    const run = codexLayout();
    // Fixture mutation: the recorded paper-screener without its description.
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"].replace(/^description:.*\n/m, "") });

    const outcome = await apply(run);

    expect(outcome.ok).toBe(false);
    expect(run.logs).toContainEqual(expect.stringContaining("no nonblank 'description'"));
    expect(run.records()).toEqual([]);
    expect(roleFiles(run)).toEqual([]);
  });

  it("fails closed when CODEX_HOME is unknown", async () => {
    const run = codexLayout();
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"] });

    const outcome = await apply(run, { codexHome: "" });

    expect(outcome.ok).toBe(false);
    expect(run.logs).toContainEqual(expect.stringContaining("CODEX_HOME is not set"));
  });

  it("fails closed for a workflow-declared sub-agent named main and keeps the main record", async () => {
    const run = codexLayout();
    const agents = arxivAgents();
    stageInline(run, "codex", { main: agents["paper-screener"], "paper-screener": agents["paper-screener"] });
    recordModelResolution(runs.arxivMainModelResolution.data, run.env);

    const outcome = await apply(run);

    expect(outcome).toEqual({ ok: false, agents: ["main", "paper-screener"], resolutions: [], failures: [] });
    expect(run.records()).toEqual([runs.arxivMainModelResolution.data]);
    expect(run.logs).toContainEqual(expect.stringContaining("the sub-agent name 'main' is reserved for the Codex main model's resolution record. Rename the agent in the workflow. Refusing to start Codex."));
    expect(roleFiles(run)).toEqual([]);
  });

  it("records the worker next to the main record", async () => {
    const run = codexLayout();
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"] });
    recordModelResolution(runs.arxivMainModelResolution.data, run.env);

    const outcome = await apply(run);

    expect(outcome.ok).toBe(true);
    expect(run.records()).toEqual([runs.arxivMainModelResolution.data, { engine: "codex", role: "paper-screener", configured_model: "openai/gpt-5.4-mini", resolved_model: "gpt-5.4-mini", provider: "openai", status: "exact", cause: null }]);
  });

  it.each([
    ["a list", "model: [small]", '["small"]'],
    ["a boolean", "model: false", "false"],
  ])("fails closed for a model declared as %s instead of inheriting the main model", async (_label, declaration, configured) => {
    const run = codexLayout();
    // Fixture mutation: the recorded paper-screener with a non-string model declaration.
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"].replace("model: openai/gpt-5.4-mini", declaration) });

    const outcome = await apply(run);

    expect(outcome.ok).toBe(false);
    expect(run.records()).toEqual([{ engine: "codex", role: "paper-screener", configured_model: configured, resolved_model: null, provider: "openai", status: "failed", cause: "model_unavailable" }]);
    expect(run.logs).toContainEqual(expect.stringMatching(/codex sub-agent 'paper-screener': .*is not a string.*Refusing to start Codex/));
    expect(roleFiles(run)).toEqual([]);
  });

  it.each([
    ["the alias's effort", "fast", "low"],
    ["the agent's effort over the alias's", "fast?effort=high", "high"],
  ])("writes %s into model_reasoning_effort", async (_label, model, effort) => {
    const run = codexLayout();
    run.env.GH_AW_MODEL_ALIASES = JSON.stringify({ fast: ["openai/gpt-5.4-mini?effort=low"] });
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"].replace("model: openai/gpt-5.4-mini", `model: ${model}`) });

    const outcome = await apply(run);

    expect(outcome.ok).toBe(true);
    expect(outcome.resolutions).toEqual([{ name: "paper-screener", configuredModel: model, model: "gpt-5.4-mini", status: "resolved", params: { effort } }]);
    expect(readRole(run, "paper-screener")).toMatchObject({ model: "gpt-5.4-mini", model_reasoning_effort: effort });
    expect(run.logs.join("\n")).not.toMatch(/not applied/);
  });

  it("applies an alias effort Codex accepts", async () => {
    const run = codexLayout();
    run.env.GH_AW_MODEL_ALIASES = JSON.stringify({ fast: ["openai/gpt-5.4-mini?effort=none"] });
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"].replace("model: openai/gpt-5.4-mini", "model: fast") });

    const outcome = await apply(run);

    expect(outcome.ok).toBe(true);
    expect(outcome.resolutions).toEqual([{ name: "paper-screener", configuredModel: "fast", model: "gpt-5.4-mini", status: "resolved", params: { effort: "none" } }]);
    expect(readRole(run, "paper-screener")).toMatchObject({ model: "gpt-5.4-mini", model_reasoning_effort: "none" });
    expect(run.logs.join("\n")).not.toMatch(/not applied/);
  });

  it.each([
    ["engine.config", run => (run.env.GH_AW_CODEX_CONFIG_JSON = JSON.stringify({ defaults: {}, overrides: { agents: { max_threads: 4, "paper-screener": { description: "Other role", model: "gpt-5.6-sol" } } }, disablePlugins: false }))],
    ["a config.toml table", run => writeCodexConfig(run, '[agents]\nmax_threads = 4\n\n[agents.paper-screener]\ndescription = "Other role"\n')],
    ["a config.toml dotted key", run => writeCodexConfig(run, 'agents."paper-screener".description = "Other role"\n')],
    ["a config.toml inline table", run => writeCodexConfig(run, '[agents]\npaper-screener = { description = "Other role" }\n')],
  ])("fails closed when %s defines a role with a generated name", async (_label, configure) => {
    const run = codexLayout();
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"] });
    configure(run);

    const outcome = await apply(run);

    expect(outcome.ok).toBe(false);
    expect(run.logs).toContainEqual(expect.stringMatching(/already defines the role \[agents\.paper-screener\], which Codex loads instead of the generated role file.*Refusing to start Codex/));
    expect(run.records()).toEqual([]);
    expect(roleFiles(run)).toEqual([]);
  });

  it("keeps [agents] settings and other roles that do not collide", async () => {
    const run = codexLayout();
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"] });
    run.env.GH_AW_CODEX_CONFIG_JSON = JSON.stringify({ defaults: {}, overrides: { agents: { max_threads: 4, reviewer: { description: "Reviewer" } } }, disablePlugins: false });
    writeCodexConfig(run, '[agents]\nmax_threads = 4\n\n[agents.reviewer]\ndescription = "Reviewer"\ndeveloper_instructions = """\n[agents.paper-screener]\n"""\n');

    const outcome = await apply(run);

    expect(outcome.ok).toBe(true);
    expect(roleFiles(run)).toEqual(["paper-screener.toml"]);
  });

  it("removes only previously generated roles when workflow declarations change", async () => {
    const run = codexLayout();
    const agents = arxivAgents();
    stageInline(run, "codex", { "paper-screener": agents["paper-screener"], "relevance-ranker": agents["relevance-ranker"] });
    expect((await apply(run)).ok).toBe(true);
    fs.writeFileSync(path.join(run.codexHome, "agents", "user-role.toml"), 'name = "user-role"\n');

    fs.rmSync(path.join(run.agentsBaseDir, ".codex/agents"), { recursive: true, force: true });
    stageInline(run, "codex", { "paper-screener": agents["paper-screener"] });
    const outcome = await apply(run);

    expect(outcome.ok).toBe(true);
    expect(roleFiles(run)).toEqual(["paper-screener.toml", "user-role.toml"]);
  });

  it("does not overwrite an untracked user role with the same name", async () => {
    const run = codexLayout();
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"] });
    fs.mkdirSync(path.join(run.codexHome, "agents"), { recursive: true });
    const userRole = 'name = "paper-screener"\ndescription = "User role"\n';
    fs.writeFileSync(path.join(run.codexHome, "agents", "paper-screener.toml"), userRole);

    const outcome = await apply(run);

    expect(outcome.ok).toBe(false);
    expect(fs.readFileSync(path.join(run.codexHome, "agents", "paper-screener.toml"), "utf8")).toBe(userRole);
    expect(run.logs).toContainEqual(expect.stringContaining("already exists and is not tracked as generated by gh-aw"));
  });

  it("removes generated roles when no workflow-declared agents remain", async () => {
    const run = codexLayout();
    stageInline(run, "codex", { "paper-screener": arxivAgents()["paper-screener"] });
    expect((await apply(run)).ok).toBe(true);
    fs.writeFileSync(path.join(run.codexHome, "agents", "user-role.toml"), 'name = "user-role"\n');
    fs.rmSync(path.join(run.agentsBaseDir, ".codex/agents"), { recursive: true, force: true });

    const outcome = await apply(run);

    expect(outcome).toEqual({ ok: true, agents: [], resolutions: [], failures: [] });
    expect(roleFiles(run)).toEqual(["user-role.toml"]);
  });

  it("removes partially written role files after a write failure", async () => {
    const run = codexLayout();
    stageInline(run, "codex", arxivAgents());
    const originalWriteFileSync = fs.writeFileSync;
    const writeSpy = vi.spyOn(fs, "writeFileSync").mockImplementation((file, ...args) => {
      if (String(file).endsWith("paper-screener.toml")) throw new Error("disk full");
      return originalWriteFileSync(file, ...args);
    });

    let outcome;
    try {
      outcome = await apply(run);
    } finally {
      writeSpy.mockRestore();
    }

    expect(outcome.ok).toBe(false);
    expect(roleFiles(run)).toEqual([]);
  });

  it("codex_no_declared_agents_is_noop", async () => {
    const run = codexLayout();
    const outcome = await apply(run);
    expect(outcome).toEqual({ ok: true, agents: [], resolutions: [], failures: [] });
    expect(run.logs).toEqual([]);
    expect(run.records()).toEqual([]);
    expect(fs.existsSync(run.codexHome)).toBe(false);
  });
});

describe("Codex sub-agent endpoint compatibility (purelock run 39601756922)", () => {
  const purelockAgent = () => fs.readFileSync(path.join(runs.purelockAgentsDir, "test-writer.md"), "utf8");
  const codexLayout = () => {
    const run = layout();
    return { ...run, codexHome: path.join(run.dir, "codex-home") };
  };
  // GH_AW_LLM_PROVIDER is `github` for purelock (engine.model-provider: github).
  const apply = (run, options = {}) => applyCodexSubagentModels({ reflectData: runs.purelockReflect(), provider: "github", codexHome: run.codexHome, agentsBaseDir: run.agentsBaseDir, env: run.env, logger: run.logger, ...options });
  const roleFiles = run =>
    fs.existsSync(path.join(run.codexHome, "agents"))
      ? fs
          .readdirSync(path.join(run.codexHome, "agents"))
          .filter(name => name.endsWith(".toml"))
          .sort()
      : [];
  const endpointsOf = model =>
    runs
      .purelockReflect()
      .endpoints.flatMap(endpoint => (endpoint.configured ? endpoint.routing_models || [] : []))
      .find(candidate => candidate.model_id === model)?.supported_endpoints;

  it("codex_alias_resolves_among_responses_capable_models", async () => {
    const run = codexLayout();
    stageInline(run, "codex", { "test-writer": purelockAgent() });

    const outcome = await apply(run);

    // The recorded run resolved `large` to claude-sonnet-5.5, which lists only /v1/messages and
    // /chat/completions; for Codex the alias resolves among /responses models instead.
    expect(endpointsOf("claude-sonnet-5.5")).not.toContain("/responses");
    expect(outcome.ok).toBe(true);
    expect(outcome.resolutions).toEqual([{ name: "test-writer", configuredModel: "large", model: "gpt-6.1-sol", status: "resolved" }]);
    expect(endpointsOf("gpt-6.1-sol")).toContain("/responses");
    expect(parseRoleToml(fs.readFileSync(path.join(run.codexHome, "agents", "test-writer.toml"), "utf8")).model).toBe("gpt-6.1-sol");
    expect(run.records()).toEqual([{ engine: "codex", role: "test-writer", configured_model: "large", resolved_model: "gpt-6.1-sol", provider: "copilot", status: "resolved", cause: null }]);
  });

  it("codex_exact_model_without_responses_fails_before_spawn", async () => {
    const run = codexLayout();
    // Fixture mutation: the recorded test-writer with the model the run resolved `large` to.
    stageInline(run, "codex", { "test-writer": purelockAgent().replace("model: large", "model: claude-sonnet-5.5") });

    const outcome = await apply(run);

    expect(outcome.ok).toBe(false);
    expect(outcome.resolutions).toEqual([]);
    expect(run.records()).toEqual([{ engine: "codex", role: "test-writer", configured_model: "claude-sonnet-5.5", resolved_model: null, provider: "copilot", status: "failed", cause: "unsupported_endpoint" }]);
    expect(run.logs).toContainEqual(expect.stringMatching(/codex sub-agent 'test-writer': .*does not list \/responses in its supported_endpoints.*Refusing to start Codex/));
    expect(roleFiles(run)).toEqual([]);
  });

  it("codex_alias_matching_only_models_without_responses_fails_before_spawn", async () => {
    const run = codexLayout();
    // Fixture mutation: `small` (-> mini -> haiku); the catalogue's only match is claude-haiku-5.5.
    stageInline(run, "codex", { "test-writer": purelockAgent().replace("model: large", "model: small") });
    let refreshes = 0;

    const outcome = await apply(run, { refetchReflectData: async () => (refreshes++, runs.purelockReflect()) });

    expect(outcome.ok).toBe(false);
    expect(refreshes).toBe(0);
    expect(run.records()).toEqual([{ engine: "codex", role: "test-writer", configured_model: "small", resolved_model: null, provider: "copilot", status: "failed", cause: "unsupported_endpoint" }]);
    expect(run.logs).toContainEqual(expect.stringContaining("(resolved to 'claude-haiku-5.5') does not list /responses"));
    expect(run.logs.join("\n")).not.toMatch(/The requested model is not supported/);
    expect(roleFiles(run)).toEqual([]);
  });

  it("checks a live exact id before an alias of the same name, also after a refresh", async () => {
    const run = codexLayout();
    // Fixture mutation: a workflow alias named like the live model the run resolved `large` to.
    run.env.GH_AW_MODEL_ALIASES = JSON.stringify({ "claude-sonnet-5.5": ["copilot/gpt-6.1-sol"] });
    stageInline(run, "codex", { "test-writer": purelockAgent().replace("model: large", "model: claude-sonnet-5.5") });
    // The first catalogue failed to list the Copilot models; the refresh lists them.
    const incomplete = runs.purelockReflect();
    incomplete.models_fetch_complete = false;
    incomplete.endpoints.forEach(endpoint => endpoint.configured && (endpoint.models = null));

    for (const options of [{}, { reflectData: incomplete, refetchReflectData: async () => runs.purelockReflect() }]) {
      const outcome = await apply(run, options);

      expect(outcome.ok).toBe(false);
      expect(outcome.resolutions).toEqual([]);
      expect(run.records()).toEqual([{ engine: "codex", role: "test-writer", configured_model: "claude-sonnet-5.5", resolved_model: null, provider: "copilot", status: "failed", cause: "unsupported_endpoint" }]);
      expect(roleFiles(run)).toEqual([]);
    }
  });

  it.each([
    ["incomplete metadata", reflect => reflect],
    ["no routing entry", reflect => (reflect.endpoints.forEach(endpoint => endpoint.routing_models && (endpoint.routing_models = endpoint.routing_models.filter(candidate => candidate.model_id !== "gpt-4o"))), reflect)],
  ])("fails an exact model with %s as unverifiable, not unsupported_endpoint", async (_label, mutate) => {
    const run = codexLayout();
    // Fixture mutation: the recorded test-writer pinned to gpt-4o, whose recorded entry is incomplete.
    stageInline(run, "codex", { "test-writer": purelockAgent().replace("model: large", "model: gpt-4o") });
    let refreshes = 0;

    const outcome = await apply(run, { reflectData: mutate(runs.purelockReflect()), refetchReflectData: async () => (refreshes++, mutate(runs.purelockReflect())) });

    expect(outcome.ok).toBe(false);
    expect(refreshes).toBe(1);
    expect(run.records()).toEqual([{ engine: "codex", role: "test-writer", configured_model: "gpt-4o", resolved_model: null, provider: "copilot", status: "failed", cause: "catalog_unavailable" }]);
    expect(run.logs).toContainEqual(expect.stringMatching(/has no complete endpoint metadata .*cannot be verified that it supports \/responses/));
    expect(run.logs.join("\n")).not.toMatch(/does not list \/responses/);
    expect(roleFiles(run)).toEqual([]);
  });

  it("keeps alias candidates with unknown endpoints and accepts them once a refresh verifies them", async () => {
    const run = codexLayout();
    // Fixture mutation: an alias whose first candidate has incomplete recorded metadata.
    run.env.GH_AW_MODEL_ALIASES = JSON.stringify({ fast: ["copilot/gpt-4o", "copilot/gpt-6.1-sol"] });
    stageInline(run, "codex", { "test-writer": purelockAgent().replace("model: large", "model: fast") });
    const verified = runs.purelockReflect();
    verified.endpoints
      .flatMap(endpoint => endpoint.routing_models || [])
      .forEach(candidate => candidate.model_id === "gpt-4o" && Object.assign(candidate, { candidate_metadata_complete: true, supported_endpoints: ["/chat/completions", "/responses"] }));

    const outcome = await apply(run, { refetchReflectData: async () => verified });

    expect(outcome.ok).toBe(true);
    expect(outcome.resolutions).toEqual([{ name: "test-writer", configuredModel: "fast", model: "gpt-4o", status: "resolved" }]);
    expect(roleFiles(run)).toEqual(["test-writer.toml"]);
  });

  it("keeps an exact /responses model", async () => {
    const run = codexLayout();
    stageInline(run, "codex", { "test-writer": purelockAgent().replace("model: large", "model: copilot/gpt-5.6-terra") });

    const outcome = await apply(run);

    expect(outcome.ok).toBe(true);
    expect(run.records()).toEqual([{ engine: "codex", role: "test-writer", configured_model: "copilot/gpt-5.6-terra", resolved_model: "gpt-5.6-terra", provider: "copilot", status: "exact", cause: null }]);
    expect(roleFiles(run)).toEqual(["test-writer.toml"]);
  });
});

describe("isDetectionPhase", () => {
  it.each([
    ["detection", true],
    [" Detection ", true],
    ["agent", false],
    ["", false],
    [undefined, false],
  ])("%s -> %s", (phase, expected) => {
    expect(isDetectionPhase(phase)).toBe(expected);
  });
});
