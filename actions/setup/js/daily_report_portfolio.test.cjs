// @ts-check
import { describe, expect, it } from "vitest";
import fs from "node:fs";
import { DAILY_REPORTS, REPORTS_PER_DAY, REPORT_POOL, reportsForDay, buildDailyReportPlan, buildDailyReportPolicy } from "./daily_report_portfolio.cjs";
import { normalizeDispatchParameters, normalizeSubmitParameters } from "./work_queue_intents.cjs";
import { newState, newRequest, generateRequestOperations, replayTransactions } from "./work_queue_replay.cjs";
import { validatePolicy } from "./work_queue_policy.cjs";
import { frozenResourceScope } from "./work_queue_resource_scope.cjs";
import { validateDeliveryContract } from "./work_queue_delivery.cjs";

const repository = "owner/repo";
const policyOptions = { repository, ref: "a".repeat(40), producerPrincipal: "11", workerPrincipal: "12" };
const planOptions = { repository, repositoryId: "7", date: "2026-10-07" };
const source = name => fs.readFileSync(new URL(`../../../.github/workflows/${name}.md`, import.meta.url), "utf8");

function ledger() {
  let state = newState();
  const transactions = [];
  let at = 1000;
  const append = (kind, parameters, actor) => {
    const request = newRequest(`portfolio:${transactions.length}:${++at}`, kind, actor, parameters);
    const id = `commit:${at}`;
    const decision = generateRequestOperations(state, request, actor, at, id);
    if (decision.operations.length) {
      transactions.push({ version: 3, id, previous: state.tip || null, request, actor, policy_epoch: "portfolio-v1", at, operations: decision.operations });
      state = replayTransactions(transactions);
    }
    return state;
  };
  const policy = buildDailyReportPolicy(policyOptions);
  append("policy", { operations: [{ kind: "Policy", epoch: "portfolio-v1", policy }] }, { role: "administrator", repository, principal: "11" });
  return { append, policy, state: () => state, transactions };
}

describe("three-of-ten daily discussion-report portfolio", () => {
  it("selects three distinct profiles per day and exactly three slots per profile over any ten-day rotation", () => {
    expect(DAILY_REPORTS).toHaveLength(10);
    expect(new Set(DAILY_REPORTS).size).toBe(10);
    for (const start of ["2026-01-01", "2026-02-25", "2026-12-27"]) {
      const counts = Object.fromEntries(DAILY_REPORTS.map(profile => [profile, 0]));
      for (let index = 0; index < 10; index++) {
        const date = new Date(Date.parse(start) + index * 86400000).toISOString().slice(0, 10);
        const selected = reportsForDay(date);
        expect(selected).toHaveLength(REPORTS_PER_DAY);
        expect(new Set(selected).size).toBe(REPORTS_PER_DAY);
        for (const profile of selected) counts[profile]++;
        expect(reportsForDay(date)).toEqual(selected);
      }
      expect(Object.values(counts)).toEqual(Array(10).fill(3));
    }
    expect(reportsForDay("2024-02-29")).toHaveLength(3);
  });

  it.each(["2026-02-29", "2026-02-30", "2026-13-01", "1969-12-31", "2026-1-01", "2026-10-07T00:00:00Z", "tomorrow"])("rejects invalid or noncanonical dates: %s", date => {
    expect(() => reportsForDay(date)).toThrow(/date|Date/);
  });

  it("produces immutable date-keyed definitions, native repository authority and exact discussion delivery contracts", () => {
    const plan = buildDailyReportPlan(planOptions);
    expect(buildDailyReportPlan(planOptions)).toEqual(plan);
    expect(plan.nodes).toHaveLength(3);
    expect(plan.dispatch).toEqual({ pool: REPORT_POOL, max_claims: 3, max_dispatches: 3 });
    expect(Buffer.byteLength(JSON.stringify(plan))).toBeLessThan(8192);
    for (const node of plan.nodes) {
      expect(node).toMatchObject({ graph_id: "daily-report-cohort:2026-10-07", pool: REPORT_POOL, priority: 3, fairness_key: node.worker_profile, node_key: node.worker_profile, depends_on: [] });
      expect(node.payload).toMatchObject({ report_date: plan.date, report_profile: node.worker_profile });
      expect(frozenResourceScope(node.payload)).toEqual({ version: 1, resources: [{ host: "github.com", repository, repository_id: "7" }] });
      expect(validateDeliveryContract(node.payload.effect_contract).outputs).toContainEqual({ type: "create_discussion", min: 1, max: 1 });
      expect(node.payload.effect_contract.outputs.some(output => output.type === "create_issue")).toBe(false);
    }
    expect(() => buildDailyReportPlan({ ...planOptions, repository: "not-a-repository" })).toThrow();
    expect(() => buildDailyReportPlan({ ...planOptions, repositoryId: "0" })).toThrow();
    expect(() => buildDailyReportPlan({ ...planOptions, repositoryId: "007" })).toThrow();
  });

  it("generates a validated singleton, one-attempt, equal-weight policy with only the ten immutable routes", () => {
    const policy = buildDailyReportPolicy(policyOptions);
    expect(validatePolicy(policy)).toEqual(policy);
    const pool = policy.pools[REPORT_POOL];
    expect(pool).toMatchObject({ logical_limit: 3, native_limit: 3, per_account_limit: 1, retry: { max_attempts: 1 } });
    expect(Object.keys(pool.profiles)).toEqual(DAILY_REPORTS);
    for (const profile of DAILY_REPORTS) {
      expect(policy.accounting_weights[profile]).toBe(1);
      expect(pool.profiles[profile]).toEqual({
        workflow: `.github/workflows/${profile}.lock.yml`,
        ref: policyOptions.ref,
        principal: "12",
        trust_domain: profile,
        credential_scope: "repository",
        effect_scope: repository,
        max_claims: 1,
        share_keys: false,
      });
    }
    expect(() => buildDailyReportPolicy({ ...policyOptions, ref: "main" })).toThrow();
    expect(() => buildDailyReportPolicy({ ...policyOptions, workerPrincipal: "github-actions[bot]" })).toThrow();
    expect(() => buildDailyReportPolicy({ ...policyOptions, producerPrincipal: "0" })).toThrow();
  });

  it("records idempotent daily Work and three singleton grants in the same QueueCommit history", () => {
    const queue = ledger();
    const plan = buildDailyReportPlan(planOptions);
    const producer = { role: "producer", repository, principal: "11" };
    const dispatcher = { role: "dispatcher", repository, principal: "11", workflow: ".github/workflows/daily-report-dispatcher.lock.yml", run_id: "15", run_attempt: 1 };
    queue.append("submit", normalizeSubmitParameters({ nodes: plan.nodes }, queue.policy, 2000, queue.state()), producer);
    const originalWork = [...queue.state().works.values()];
    queue.append("submit", normalizeSubmitParameters({ nodes: plan.nodes }, queue.policy, 3000, queue.state()), producer);
    expect([...queue.state().works.values()]).toEqual(originalWork);
    expect(queue.state().works.size).toBe(3);
    queue.append("dispatch_next", normalizeDispatchParameters(plan.dispatch, queue.policy, 3), dispatcher);
    expect(queue.state().claims.size).toBe(3);
    expect(queue.state().dispatches.size).toBe(3);
    for (const dispatch of queue.state().dispatches.values()) {
      expect(dispatch.claims).toHaveLength(1);
      expect(plan.selected).toContain(dispatch.profile.workflow.split("/").pop().replace(".lock.yml", ""));
    }
    queue.append("dispatch_next", normalizeDispatchParameters(plan.dispatch, queue.policy, 3), dispatcher);
    expect(queue.state().claims.size).toBe(3);
    const tomorrow = buildDailyReportPlan({ ...planOptions, date: "2026-10-08" });
    queue.append("submit", normalizeSubmitParameters({ nodes: tomorrow.nodes }, queue.policy, 4000, queue.state()), producer);
    expect(queue.state().works.size).toBe(6);
    queue.append("dispatch_next", normalizeDispatchParameters(tomorrow.dispatch, queue.policy, 3), { ...dispatcher, run_id: "16" });
    expect(queue.state().claims.size).toBe(3);
    expect(queue.state().dispatches.size).toBe(3);
    expect(replayTransactions(queue.transactions).claims.size).toBe(3);
  });

  it("keeps the dispatcher allowlist and ten dispatch-only Claim workers wired to actual compiled queue protocols", () => {
    const dispatcher = source("daily-report-dispatcher");
    const routes = [...dispatcher.matchAll(/^      - (daily-[a-z-]+)$/gm)].map(match => match[1]);
    expect(routes).toEqual(DAILY_REPORTS);
    expect(dispatcher).toContain("if: github.run_attempt == 1");
    expect(dispatcher).toContain("Date.parse(run.created_at) - 86400000");
    expect(dispatcher).not.toMatch(/^\s+workflow_dispatch:/m);
    expect(dispatcher).toContain("    max: 3");
    const lock = fs.readFileSync(new URL("../../../.github/workflows/daily-report-dispatcher.lock.yml", import.meta.url), "utf8");
    expect(lock).toContain("if: github.run_attempt == 1");
    expect(lock).toContain("daily_report_portfolio.cjs");
    for (const profile of DAILY_REPORTS) {
      const worker = source(profile);
      const frontmatter = worker.split("\n---")[0];
      expect(frontmatter).toMatch(/^  workflow_dispatch:/m);
      expect(frontmatter).not.toMatch(/^  schedule:/m);
      expect(frontmatter).toMatch(/^ *- shared\/daily-report-worker\.md$/m);
      expect(frontmatter).toMatch(/work-queue:\n    worker: true\n    require-assignment: true/);
      expect(frontmatter).not.toMatch(/storage:/);
      expect(frontmatter).not.toContain("source:");
      const compiled = fs.readFileSync(new URL(`../../../.github/workflows/${profile}.lock.yml`, import.meta.url), "utf8");
      expect(compiled).toContain("work_queue_assignment:");
      expect(compiled).toContain('GH_AW_WORK_QUEUE_ROLE: "worker"');
      expect(compiled).toContain("name: Reconcile work queue claim");
      expect(compiled).toContain("work_queue_claim_finish");
      expect(compiled).toContain("create_discussion");
    }
    expect(source("daily-evals-report")).not.toContain("create_issue");
    expect(source("daily-token-consumption-report")).not.toContain("create_issue");
  });
});
