// @ts-check
import { describe, expect, it } from "vitest";
import fs from "node:fs";
import { createRequire } from "node:module";
import { ENGINES, SUPPORTED, POOL, changedEngines, buildEngineConformancePlan, buildEngineConformancePolicy } from "./engine_conformance_portfolio.cjs";
import { validatePolicyEnv } from "./work_queue_policy.cjs";
import { normalizeSubmitParameters } from "./work_queue_intents.cjs";
import { replayTransactions } from "./work_queue_replay.cjs";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const options = { repository: "owner/repo", repositoryId: "7", date: "2026-10-09" };
const settings = JSON.parse(fs.readFileSync(new URL("../../../.github/workflows/aw.json", import.meta.url), "utf8")).work_queue;
const policyOptions = { repository: options.repository, settings };

describe("engine conformance queue portfolio", () => {
  it("selects supported engines first, recent changes next and eventually covers every engine", () => {
    expect(ENGINES).toHaveLength(16);
    const seen = new Set();
    for (let day = 0; day < 42; day++) {
      const date = new Date(Date.parse("2026-01-01T00:00:00Z") + day * 86400000).toISOString().slice(0, 10);
      const plan = buildEngineConformancePlan({ ...options, date, changedPaths: [".github/workflows/engine-conformance-aider.md"] });
      expect(plan.selected).toHaveLength(3);
      expect(new Set(plan.selected).size).toBe(3);
      expect(SUPPORTED).toContain(plan.selected[0]);
      for (const engine of plan.selected) seen.add(engine);
      expect(buildEngineConformancePlan({ ...options, date, changedPaths: [".github/workflows/engine-conformance-aider.md"] })).toEqual(plan);
    }
    expect([...seen].sort()).toEqual([...ENGINES]);
    expect(changedEngines([".github/workflows/shared/engine-conformance.md"]).size).toBe(ENGINES.length);
    expect(changedEngines(["pkg/workflow/engine.go"]).size).toBe(ENGINES.length);
    expect([...changedEngines(["pkg/workflow/codex_config.go", ".github/workflows/shared/agy-conformance.md"])].sort()).toEqual(["agy", "codex"]);
    expect([...changedEngines(["pkg/workflow/copilot_engine.go"])]).toEqual(["copilot"]);
    expect([...changedEngines([".github/workflows/engine-conformance-mini-swe-agent.md"])]).toEqual(["mini-swe-agent"]);
  });

  it("rejects invalid dates, identities and changed-path inputs", () => {
    for (const date of ["2026-02-30", "2026-1-01", "1969-12-31"]) expect(() => buildEngineConformancePlan({ ...options, date })).toThrow();
    expect(() => buildEngineConformancePlan({ ...options, repositoryId: "0" })).toThrow();
    expect(() => buildEngineConformancePlan({ ...options, changedPaths: /** @type {any} */ [null] })).toThrow();
  });

  it("binds three idempotent, no-write tasks to the compiled pool with a bounded dispatch", () => {
    const plan = buildEngineConformancePlan(options);
    const policy = buildEngineConformancePolicy(policyOptions);
    expect(validatePolicyEnv(policy)).toEqual(policy);
    expect(Object.keys(policy.pools[POOL].profiles).sort()).toEqual(ENGINES.map(engine => `engine-conformance-${engine}`).sort());
    expect(policy.pools[POOL]).toMatchObject({ concurrency: 3, per_account_limit: 1 });
    expect(plan.dispatch).toEqual({ pool: POOL, max_claims: 3, max_dispatches: 3 });
    expect(plan.nodes.every(node => node.payload.effect_contract.kind === "none" && node.worker_profile === `engine-conformance-${node.payload.engine}`)).toBe(true);
    const works = normalizeSubmitParameters({ nodes: plan.nodes }, policy);
    expect(normalizeSubmitParameters({ nodes: plan.nodes }, policy)).toEqual(works);
    const pool = policy.pools[POOL];
    const profiles = Object.fromEntries(Object.entries(pool.profiles).map(([name, profile]) => [name, { workflow: profile.workflow }]));
    const planned = h.dispatchNext(h.submit([], works), { ...plan.dispatch, concurrency: pool.concurrency, per_account_limit: pool.per_account_limit, profiles });
    const state = replayTransactions(planned.log);
    expect(state.works.size).toBe(3);
    expect(state.claims.size).toBe(3);
    expect(state.dispatches.size).toBe(3);
  });

  it("keeps dispatcher, workers and their compiled assignment protocol in sync", () => {
    const source = name => fs.readFileSync(new URL(`../../../.github/workflows/${name}.md`, import.meta.url), "utf8");
    const compiled = name => fs.readFileSync(new URL(`../../../.github/workflows/${name}.lock.yml`, import.meta.url), "utf8");
    const dispatcher = source("engine-conformance-dispatcher");
    const shared = source("shared/engine-conformance");
    expect(shared).toContain("github: false");
    expect(shared).not.toMatch(/^\s+staged:\s*true$/m);
    expect(shared).toContain("noop:\n    report-as-issue: false");
    expect(shared).toContain("activation-comments: false");
    expect(shared).toContain("report-failure-as-issue: false");
    expect(shared).toContain("report-failed-jobs: false");
    expect(shared).toContain("missing-tool:\n    create-issue: false");
    expect(dispatcher.match(/^      - engine-conformance-[a-z-]+$/gm)).toHaveLength(ENGINES.length);
    expect(dispatcher).toContain("if: github.run_attempt == 1");
    expect(dispatcher).toContain("stagePlanIntents(");
    expect(dispatcher).toContain("shared/work-queue-staged-plan.md");
    expect(fs.readFileSync(new URL("../../../.github/workflows/shared/work-queue-staged-plan.md", import.meta.url), "utf8")).toContain("Do not submit, dispatch, edit intent files");
    expect(compiled("engine-conformance-dispatcher")).toContain("engine_conformance_portfolio.cjs");
    for (const engine of ENGINES) {
      const worker = source(`engine-conformance-${engine}`);
      expect(worker).toContain("shared/engine-conformance-worker.md");
      expect(worker).not.toMatch(/^  schedule:/m);
      expect(worker).not.toMatch(/^\s+staged:\s*true$/m);
      expect(compiled(`engine-conformance-${engine}`)).toContain("work_queue_assignment:");
      expect(compiled(`engine-conformance-${engine}`)).toContain('GH_AW_WORK_QUEUE_ROLE: "worker"');
      expect(compiled(`engine-conformance-${engine}`)).toContain("work_queue_claim_finish");
      if (["aider", "deepseek-harness", "mini-swe-agent"].includes(engine)) {
        expect(worker).toContain("mode: gh-proxy");
        expect(worker).toContain("toolsets: [repos]");
      } else {
        expect(compiled(`engine-conformance-${engine}`)).not.toMatch(/^\s+GH_AW_MCP_CLI_SERVERS:.*\bgithub\b/m);
      }
      expect(compiled(`engine-conformance-${engine}`)).not.toMatch(/GH_AW_(?:SAFE_OUTPUTS_STAGED|INFO_STAGED):\s*"?true"?/);
      expect(compiled(`engine-conformance-${engine}`)).toContain("name: Terminate work queue worker");
      expect(compiled(`engine-conformance-${engine}`)).toContain('\\"report-as-issue\\":\\"false\\"');
    }
    expect(() => buildEngineConformancePolicy({ ...policyOptions, repository: "not-a-repository" })).toThrow();
  });
});
