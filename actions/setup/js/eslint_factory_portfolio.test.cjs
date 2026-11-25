// @ts-check
import { describe, expect, it } from "vitest";
import fs from "node:fs";
import { createRequire } from "node:module";
import { ESLINT_WORKERS, RULE_WORKERS, RULES_PER_DAY, buildESLintFactoryPlan, parseESLintRuleIds, rulesForDay } from "./eslint_factory_portfolio.cjs";
import { normalizeSubmitParameters } from "./work_queue_intents.cjs";
import { replayTransactions } from "./work_queue_replay.cjs";
import { frozenResourceScope } from "./work_queue_resource_scope.cjs";
import { policyAllowsWorker, readWorkQueuePolicyEnv, workerWorkflow } from "./work_queue_policy.cjs";
import { checkFileProtection, checkFileProtectionPostApply } from "./manifest_file_helpers.cjs";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const repository = "owner/repo";
const indexSource = fs.readFileSync(new URL("../../../eslint-factory/src/index.ts", import.meta.url), "utf8");
const rules = parseESLintRuleIds(indexSource);
const planOptions = { date: "2026-10-08", repository, repositoryId: "7", rules };
const DAILY_NODES = 1 + 2 * RULES_PER_DAY;
const settings = JSON.parse(fs.readFileSync(new URL("../../../.github/workflows/aw.json", import.meta.url), "utf8")).work_queue;
const compiledPolicy = h.compiledDispatcherPolicy("eslint-factory-dispatcher", repository);

/** Admit the plan's new Work (existing Work IDs are idempotent no-ops). */
function admit(log, plan, policy) {
  const state = replayTransactions(log);
  const fresh = normalizeSubmitParameters({ nodes: plan.nodes }, policy).filter(work => !state.works.has(work.work_id));
  return fresh.length ? h.submit(log, fresh, { ...h.producer, run_id: String(10 + log.length) }) : log;
}

/** Commit the plan's dispatch_next with the compiled pool limits, launching only the factory workers. */
function dispatch(log, plan, policy, runId = "15") {
  const pool = policy.pools[plan.dispatch.pool];
  const profiles = Object.fromEntries(ESLINT_WORKERS.map(worker => [worker, { workflow: workerWorkflow(worker) }]));
  return h.dispatchNext(log, { ...plan.dispatch, concurrency: pool.concurrency, per_account_limit: pool.per_account_limit, profiles }, { ...h.dispatcher, run_id: runId }).log;
}

/** Decoded GH_AW_WORK_QUEUE_POLICY environments (chunked or not) of a compiled lock file. */
function compiledPolicyEnvironments(compiled) {
  return [...compiled.matchAll(/(?:^ +GH_AW_WORK_QUEUE_POLICY(?:_PARTS|_[0-9]+)?: "[^\n]+"\n)+/gm)].map(([block]) =>
    Object.fromEntries([...block.matchAll(/(GH_AW_WORK_QUEUE_POLICY(?:_PARTS|_[0-9]+)?): ("[^\n]+")/g)].map(([, name, value]) => [name, JSON.parse(value)]))
  );
}

describe("ESLint factory producer and dispatcher", () => {
  it("prepares a date-keyed miner task plus a refiner and monster task per rotating rule with actual worker contracts", () => {
    const plan = buildESLintFactoryPlan(planOptions);
    expect(buildESLintFactoryPlan(planOptions)).toEqual(plan);
    expect(plan.nodes).toHaveLength(DAILY_NODES);
    expect(plan.rules).toEqual(rulesForDay(planOptions.date, rules));
    expect(plan.nodes[0]).toMatchObject({ node_key: "eslint-miner", worker_profile: "eslint-miner" });
    expect(plan.nodes.slice(1).map(node => [node.worker_profile, node.payload.rule.id])).toEqual(plan.rules.flatMap(rule => RULE_WORKERS.map(profile => [profile, rule])));
    for (const node of plan.nodes.slice(1)) {
      const rule = node.payload.rule.id;
      expect(node.node_key).toBe(`${node.worker_profile}:${rule}`);
      expect(node.payload.rule).toEqual({ id: rule, source: `eslint-factory/src/rules/${rule}.ts`, test: `eslint-factory/src/rules/${rule}.test.ts`, config_id: `gh-aw-custom/${rule}` });
      expect(node.payload.plan).toContain(node.worker_profile === "eslint-refiner" ? node.payload.rule.source : node.payload.rule.config_id);
    }
    expect(plan.dispatch).toEqual({ pool: "eslint-factory", max_claims: 7, max_dispatches: 7 });
    expect(Buffer.byteLength(JSON.stringify(plan))).toBeLessThan(16384);
    for (const node of plan.nodes) {
      expect(node).toMatchObject({ graph_id: "eslint-factory-cohort:2026-10-08", priority: 3, fairness_key: "", pool: "eslint-factory", depends_on: [] });
      expect(frozenResourceScope(node.payload)).toEqual({ version: 1, resources: [{ host: "github.com", repository, repository_id: "7" }] });
      expect(node.payload.effect_contract.version).toBe(1);
    }
    const contract = profile => plan.nodes.find(node => node.worker_profile === profile).payload.effect_contract.outputs;
    expect(contract("eslint-miner")).toContainEqual({ type: "create_pull_request", min: 0, max: 1 });
    expect(plan.nodes.every(node => node.payload.plan.includes("finish the Claim as failed instead of completing this write-capable Work with only noop"))).toBe(true);
    expect(contract("eslint-refiner")).toContainEqual({ type: "persist_eslint_memory", min: 1, max: 1 });
    expect(contract("eslint-monster")).toContainEqual({ type: "assign_to_agent", min: 0, max: 1 });
    expect(contract("eslint-monster")).toContainEqual({ type: "create_issue", min: 0, max: 1 });
  });

  it("parses the rule registry and rejects empty or repeated registrations", () => {
    expect(rules).toHaveLength(74);
    expect(rules).toEqual([...rules].sort());
    expect(rules).toContain("require-parseInt-radix");
    expect(() => parseESLintRuleIds("")).toThrow(/no rules/);
    const entry = '    "no-throw-plain-object": noThrowPlainObjectRule,\n';
    expect(() => parseESLintRuleIds(`rules: {\n${entry}${entry}}`)).toThrow(/repeats/);
  });

  it("rotates every rule through the daily cohort", () => {
    const days = Math.ceil(rules.length / RULES_PER_DAY);
    const seen = new Set();
    for (let day = 0; day < days; day++) {
      const date = new Date(Date.parse("2026-10-08T00:00:00.000Z") + day * 86400000).toISOString().slice(0, 10);
      const cohort = rulesForDay(date, rules);
      expect(new Set(cohort).size).toBe(RULES_PER_DAY);
      for (const rule of cohort) seen.add(rule);
    }
    expect([...seen].sort()).toEqual(rules);
    expect(rulesForDay("2026-10-08", ["a", "b"])).toEqual(["a", "b"]);
  });

  it("allows the miner's rule README without bypassing other file protections", () => {
    const compiled = fs.readFileSync(new URL("../../../.github/workflows/eslint-miner.lock.yml", import.meta.url), "utf8");
    const configs = [...compiled.matchAll(/GH_AW_SAFE_OUTPUTS_(?:HANDLER_)?CONFIG: ("[^\n]+")/g)].map(([, value]) => JSON.parse(JSON.parse(value)).create_pull_request);
    expect(configs).toHaveLength(2);
    for (const config of configs) {
      expect(config.allowed_files).toEqual(["eslint-factory/**"]);
      expect(config.protected_files_policy).toBe("fallback-to-issue");
      expect(config.protected_files_path_exclude).toEqual(["eslint-factory/README.md", ".changeset/*.md"]);
      expect(config.protected_files).toContain("README.md");
      for (const [file, action] of [
        ["eslint-factory/README.md", "allow"],
        ["eslint-factory/src/rules/new-rule.ts", "allow"],
        ["eslint-factory/docs/README.md", "fallback"],
        ["eslint-factory/package.json", "fallback"],
        ["README.md", "deny"],
        [".github/workflows/ci.yml", "deny"],
      ]) {
        const patch = `diff --git a/${file} b/${file}\n`;
        expect(checkFileProtection(patch, config).action, file).toBe(action);
        expect(checkFileProtectionPostApply([file], config).action, file).toBe(action);
      }
    }
  });

  it.each(["2026-02-29", "2026-02-30", "2026-13-01", "1969-12-31", "2026-1-01", "tomorrow"])("rejects invalid dates: %s", date => {
    expect(() => buildESLintFactoryPlan({ ...planOptions, date })).toThrow(/date/);
  });

  it("requires canonical repository identity and rules, and routes every worker through the eslint-factory pool", () => {
    for (const repositoryId of ["0", "007", "github-actions[bot]"]) expect(() => buildESLintFactoryPlan({ ...planOptions, repositoryId })).toThrow(/identity/);
    expect(() => buildESLintFactoryPlan({ ...planOptions, repository: "not-a-repo" })).toThrow();
    expect(() => buildESLintFactoryPlan({ ...planOptions, rules: [] })).toThrow(/rules/);
    expect(compiledPolicy.pools["eslint-factory"]).toMatchObject({ concurrency: settings.pools["eslint-factory"].concurrency, max_attempts: 3 });
    for (const worker of ESLINT_WORKERS) expect(policyAllowsWorker(compiledPolicy, "eslint-factory", worker), worker).toBe(true);
  });

  it("admits seven tasks once per UTC day, then claims seven singleton dispatches", () => {
    const policy = compiledPolicy;
    const plan = buildESLintFactoryPlan(planOptions);
    let log = admit([], plan, policy);
    const original = [...replayTransactions(log).works.values()];
    log = admit(log, plan, policy);
    expect([...replayTransactions(log).works.values()]).toEqual(original);
    expect(original).toHaveLength(DAILY_NODES);
    log = dispatch(log, plan, policy);
    let state = replayTransactions(log);
    expect(state.claims.size).toBe(DAILY_NODES);
    expect(state.dispatches.size).toBe(DAILY_NODES);
    for (const record of state.dispatches.values()) expect(record.claims).toHaveLength(1);
    log = dispatch(admit(log, plan, policy), plan, policy, "16");
    expect(replayTransactions(log).claims.size).toBe(DAILY_NODES);
    const tomorrow = buildESLintFactoryPlan({ ...planOptions, date: "2026-10-09" });
    log = dispatch(admit(log, tomorrow, policy), tomorrow, policy, "17");
    state = replayTransactions(log);
    expect(state.works.size).toBe(2 * DAILY_NODES);
    expect(state.claims.size).toBe(DAILY_NODES);
  });

  it("wires trusted plan preparation, submission before dispatch, and immutable run dates", () => {
    const source = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.md", import.meta.url), "utf8");
    expect(source).toContain("actions: read");
    expect(source).toContain("buildESLintFactoryPlan");
    expect(source).toContain("run.created_at");
    expect(source).toContain("cat /tmp/gh-aw/agent/eslint-factory-plan.json");
    expect(source).toContain("stagePlanIntents(");
    expect(source).toContain("shared/work-queue-staged-plan.md");
    expect(source).not.toContain("work-queue-policy:");
  });

  it("compiles the v4 policy environment and the built-in token into the dispatcher controls", () => {
    const source = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.md", import.meta.url), "utf8");
    const compiled = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.lock.yml", import.meta.url), "utf8");
    const environments = compiledPolicyEnvironments(compiled).map(environment => Object.fromEntries(Object.entries(environment).map(([name, value]) => [name, value.replaceAll("${{ github.repository }}", repository)])));
    expect(environments.length).toBeGreaterThanOrEqual(1);
    for (const environment of environments) {
      const policy = readWorkQueuePolicyEnv(environment);
      expect(policy).toMatchObject({ version: 4, pending_limit: settings.pending_limit });
      expect(policy.pools.default.concurrency).toBe(settings.concurrency);
      expect(policy.pools["eslint-factory"].concurrency).toBe(settings.pools["eslint-factory"].concurrency);
      for (const worker of ESLINT_WORKERS) expect(policyAllowsWorker(policy, "eslint-factory", worker), worker).toBe(true);
      expect(policy.effect_scope).toBe(repository);
    }
    expect(source).toContain("github-token: ${{ secrets.GITHUB_TOKEN }}");
    expect(source).not.toMatch(/secrets\.(?!GITHUB_TOKEN\b)/);
    const configs = [...compiled.matchAll(/(?:GH_AW_WORK_QUEUE_CONTROL_CONFIG|GH_AW_SAFE_OUTPUTS_HANDLER_CONFIG): ("[^\n]+")/g)].map(([, value]) => {
      const config = JSON.parse(JSON.parse(value));
      return config.dispatch_workflow || config;
    });
    expect(configs.length).toBeGreaterThan(1);
    for (const config of configs) {
      // Dispatch reuses the control step's built-in token; no separate launch credential is compiled.
      expect(config).not.toHaveProperty("github-token");
      expect(config).not.toHaveProperty("work_queue_dispatch_credential");
      expect([...config.work_queue_workflows].sort()).toEqual([...ESLINT_WORKERS].sort());
    }
    const controls = compiled.slice(compiled.indexOf("- name: Process trusted work queue controls"));
    expect(controls).toContain("GH_AW_WORK_QUEUE_TOKEN: ${{ secrets.GITHUB_TOKEN }}");
  });

  it("executes the actual preparation step with only three metadata reads and no ambient filesystem or credentials", async () => {
    const source = fs.readFileSync(new URL("../../../.github/workflows/eslint-factory-dispatcher.md", import.meta.url), "utf8");
    const block = source.match(/      script: \|\n((?:        .*\n)+)/);
    expect(block).not.toBeNull();
    const script = block[1].replace(/^        /gm, "");
    const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;
    const calls = [];
    const files = new Map();
    const intents = [];
    const fakeRequire = name => {
      if (name === "node:fs")
        return {
          mkdirSync: directory => expect(directory).toBe("/tmp/gh-aw/agent"),
          writeFileSync: (filename, content) => files.set(filename, content),
        };
      if (name === "node:path") return { join: (...parts) => parts.join("/") };
      if (name === "/fixture/gh-aw/actions/eslint_factory_portfolio.cjs") return { buildESLintFactoryPlan, parseESLintRuleIds };
      if (name === "/fixture/gh-aw/actions/work_queue_intents.cjs")
        return {
          stagePlanIntents: (filename, plan) => {
            expect(filename).toBe("/fixture/gh-aw/safeoutputs/work-queue/work-queue.intents.jsonl");
            intents.push(plan);
          },
        };
      throw new Error(`Unreviewed preparation dependency: ${name}`);
    };
    const github = {
      rest: {
        actions: {
          getWorkflowRun: async parameters => {
            expect(parameters).toEqual({ owner: "owner", repo: "repo", run_id: 15 });
            calls.push("run");
            return { data: { created_at: "2026-10-08T23:59:59Z" } };
          },
        },
        repos: {
          get: async parameters => {
            expect(parameters).toEqual({ owner: "owner", repo: "repo" });
            calls.push("repository");
            return { data: { full_name: repository, id: 7, default_branch: "main" } };
          },
          getContent: async parameters => {
            expect(parameters).toEqual({ owner: "owner", repo: "repo", path: "eslint-factory/src/index.ts", ref: "main" });
            calls.push("registry");
            return { data: { type: "file", encoding: "base64", content: Buffer.from(indexSource).toString("base64") } };
          },
        },
      },
    };
    await new AsyncFunction("require", "github", "context", "process", "core", script)(fakeRequire, github, { repo: { owner: "owner", repo: "repo" }, runId: 15 }, { env: { RUNNER_TEMP: "/fixture" } }, { info: () => {} });
    expect(calls).toEqual(["run", "repository", "registry"]);
    expect(files.size).toBe(1);
    expect(JSON.parse(files.get("/tmp/gh-aw/agent/eslint-factory-plan.json"))).toEqual(buildESLintFactoryPlan(planOptions));
    expect(intents).toEqual([buildESLintFactoryPlan(planOptions)]);
  });
});
