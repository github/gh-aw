// @ts-check
import { describe, expect, it } from "vitest";
import fs from "node:fs";
import { ENGINES, SUPPORTED, POOL, changedEngines, buildEngineConformancePlan, buildEngineConformancePolicy } from "./engine_conformance_portfolio.cjs";
import { validatePolicy } from "./work_queue_policy.cjs";
import { normalizeSubmitParameters, normalizeDispatchParameters } from "./work_queue_intents.cjs";
import { newState, newRequest, generateRequestOperations, replayTransactions } from "./work_queue_replay.cjs";

const options = { repository: "owner/repo", repositoryId: "7", date: "2026-10-09" };
const settings = JSON.parse(fs.readFileSync(new URL("../../../.github/workflows/aw.json", import.meta.url), "utf8")).work_queue;
const policyOptions = { repository: options.repository, ref: "a".repeat(40), settings };

describe("engine conformance queue portfolio", () => {
  it("selects supported engines first, recent changes next and eventually covers every engine", () => {
    expect(ENGINES).toHaveLength(14);
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
  });

  it("rejects invalid dates, identities and changed-path inputs", () => {
    for (const date of ["2026-02-30", "2026-1-01", "1969-12-31"]) expect(() => buildEngineConformancePlan({ ...options, date })).toThrow();
    expect(() => buildEngineConformancePlan({ ...options, repositoryId: "0" })).toThrow();
    expect(() => buildEngineConformancePlan({ ...options, changedPaths: /** @type {any} */ [null] })).toThrow();
  });

  it("binds three idempotent, no-write tasks to immutable routes with a bounded dispatch", () => {
    const plan = buildEngineConformancePlan(options);
    const policy = buildEngineConformancePolicy(policyOptions);
    expect(validatePolicy(policy)).toEqual(policy);
    expect(Object.keys(policy.pools[POOL].profiles).sort()).toEqual(ENGINES.map(engine => `engine-conformance-${engine}`).sort());
    expect(policy.authorization).toBe("aw");
    expect(policy.producers).toEqual({});
    expect(policy.pools[POOL]).toMatchObject({ logical_limit: 3, native_limit: 3, per_account_limit: 1 });
    expect(plan.dispatch).toEqual({ pool: POOL, max_claims: 3, max_dispatches: 3 });
    expect(plan.nodes.every(node => node.payload.effect_contract.kind === "none" && node.worker_profile === `engine-conformance-${node.payload.engine}`)).toBe(true);
    let state = newState();
    const transactions = [];
    let at = 1000;
    function append(kind, parameters, actor) {
      const request = newRequest(`conformance:${++at}`, kind, actor, parameters);
      const id = `commit:${at}`;
      const decision = generateRequestOperations(state, request, actor, at, id);
      if (decision.operations.length) {
        transactions.push({ version: 3, id, previous: state.tip || null, request, actor, policy_epoch: "conformance-v1", at, operations: decision.operations });
        state = replayTransactions(transactions);
      }
    }
    append("policy", { operations: [{ kind: "Policy", epoch: "conformance-v1", policy }] }, { role: "administrator", repository: options.repository, principal: "11" });
    const actor = { role: "producer", repository: options.repository, principal: "11" };
    append("submit", normalizeSubmitParameters({ nodes: plan.nodes }, policy, 2000, state), actor);
    append("submit", normalizeSubmitParameters({ nodes: plan.nodes }, policy, 3000, state), actor);
    expect(state.works.size).toBe(3);
    append("dispatch_next", normalizeDispatchParameters(plan.dispatch, policy, 3), {
      role: "dispatcher",
      repository: options.repository,
      principal: "11",
      workflow: ".github/workflows/engine-conformance-dispatcher.lock.yml",
      run_id: "15",
      run_attempt: 1,
    });
    expect(state.claims.size).toBe(3);
    expect(state.dispatches.size).toBe(3);
  });

  it("keeps dispatcher, workers and their compiled assignment protocol in sync", () => {
    const source = name => fs.readFileSync(new URL(`../../../.github/workflows/${name}.md`, import.meta.url), "utf8");
    const compiled = name => fs.readFileSync(new URL(`../../../.github/workflows/${name}.lock.yml`, import.meta.url), "utf8");
    const dispatcher = source("engine-conformance-dispatcher");
    expect(dispatcher.match(/^      - engine-conformance-[a-z-]+$/gm)).toHaveLength(ENGINES.length);
    expect(dispatcher).toContain("if: github.run_attempt == 1");
    expect(compiled("engine-conformance-dispatcher")).toContain("engine_conformance_portfolio.cjs");
    for (const engine of ENGINES) {
      const worker = source(`engine-conformance-${engine}`);
      expect(worker).toContain("shared/engine-conformance-worker.md");
      expect(worker).not.toMatch(/^  schedule:/m);
      expect(compiled(`engine-conformance-${engine}`)).toContain("work_queue_assignment:");
      expect(compiled(`engine-conformance-${engine}`)).toContain('GH_AW_WORK_QUEUE_ROLE: "worker"');
      expect(compiled(`engine-conformance-${engine}`)).toContain("work_queue_claim_finish");
    }
    expect(() => buildEngineConformancePolicy({ ...policyOptions, ref: "main" })).toThrow();
  });
});
