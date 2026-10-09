// @ts-check
import { describe, expect, it } from "vitest";
import fs from "node:fs";
import { DAILY_REPORTS, FIXED_DAILY_REPORTS, WEEKLY_REPORTS, REPORT_PROFILES, REPORTS_PER_DAY, REPORT_POOL, reportsForDay, buildDailyReportPlan, buildDailyReportPolicy } from "./daily_report_portfolio.cjs";
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

describe("bounded daily and weekly discussion-report portfolio", () => {
  it("reserves daily intelligence and weekly slots while rotating other reports fairly over any thirty-five-day cycle", () => {
    expect(DAILY_REPORTS).toHaveLength(10);
    expect(REPORT_PROFILES).toHaveLength(13);
    expect(new Set(REPORT_PROFILES).size).toBe(13);
    for (const start of ["1970-01-01", "2024-02-25", "2026-01-01", "2026-02-25", "2026-12-27"]) {
      const counts = Object.fromEntries(REPORT_PROFILES.map(profile => [profile, 0]));
      const dailySequence = [];
      for (let index = 0; index < 35; index++) {
        const date = new Date(Date.parse(start) + index * 86400000).toISOString().slice(0, 10);
        const selected = reportsForDay(date);
        expect(selected).toHaveLength(REPORTS_PER_DAY);
        expect(new Set(selected).size).toBe(REPORTS_PER_DAY);
        expect(selected).toContain("deep-report");
        for (const profile of selected) counts[profile]++;
        for (const report of WEEKLY_REPORTS) {
          expect(selected.includes(report.profile)).toBe(new Date(`${date}T00:00:00Z`).getUTCDay() === report.reportWeekday);
        }
        dailySequence.push(...selected.filter(profile => DAILY_REPORTS.includes(profile)));
        expect(reportsForDay(date)).toEqual(selected);
      }
      expect(DAILY_REPORTS.map(profile => counts[profile])).toEqual(Array(10).fill(6));
      expect(WEEKLY_REPORTS.map(report => counts[report.profile])).toEqual([5, 5]);
      expect(FIXED_DAILY_REPORTS.map(profile => counts[profile])).toEqual([35]);
      for (let index = 1; index < dailySequence.length; index++) {
        expect(DAILY_REPORTS.indexOf(dailySequence[index])).toBe((DAILY_REPORTS.indexOf(dailySequence[index - 1]) + 1) % DAILY_REPORTS.length);
      }
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
      expect(node.payload.effect_contract.outputs.some(output => output.type === "create_issue")).toBe(node.worker_profile === "deep-report");
    }
    expect(() => buildDailyReportPlan({ ...planOptions, repository: "not-a-repository" })).toThrow();
    expect(() => buildDailyReportPlan({ ...planOptions, repositoryId: "0" })).toThrow();
    expect(() => buildDailyReportPlan({ ...planOptions, repositoryId: "007" })).toThrow();
  });

  it("generates a validated singleton, one-attempt, equal-weight policy with all thirteen immutable routes", () => {
    const policy = buildDailyReportPolicy(policyOptions);
    expect(validatePolicy(policy)).toEqual(policy);
    const pool = policy.pools[REPORT_POOL];
    expect(pool).toMatchObject({ logical_limit: 3, native_limit: 3, per_account_limit: 1, retry: { max_attempts: 1 } });
    expect(Object.keys(pool.profiles)).toEqual(REPORT_PROFILES);
    for (const profile of REPORT_PROFILES) {
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

  it.each(["2026-10-10", "2026-10-11"])("admits weekly reports idempotently with exactly one required discussion on %s", date => {
    const queue = ledger();
    const plan = buildDailyReportPlan({ ...planOptions, date });
    const producer = { role: "producer", repository, principal: "11" };
    queue.append("submit", normalizeSubmitParameters({ nodes: plan.nodes }, queue.policy, 2000, queue.state()), producer);
    queue.append("submit", normalizeSubmitParameters({ nodes: plan.nodes }, queue.policy, 3000, queue.state()), producer);
    expect(queue.state().works.size).toBe(3);
    const weekly = plan.nodes.find(node => WEEKLY_REPORTS.some(report => report.profile === node.worker_profile));
    expect(weekly).toBeDefined();
    expect(weekly.payload.report_date).toBe(date);
    expect(weekly.payload.effect_contract.outputs).toContainEqual({ type: "create_discussion", min: 1, max: 1 });
    queue.append("dispatch_next", normalizeDispatchParameters(plan.dispatch, queue.policy, 3), {
      role: "dispatcher",
      repository,
      principal: "11",
      workflow: ".github/workflows/daily-report-dispatcher.lock.yml",
      run_id: "15",
      run_attempt: 1,
    });
    expect(queue.state().dispatches.size).toBe(3);
    expect([...queue.state().dispatches.values()].some(dispatch => dispatch.profile.workflow === `.github/workflows/${weekly.worker_profile}.lock.yml`)).toBe(true);
  });

  it("keeps the dispatcher allowlist and thirteen dispatch-only Claim workers wired to actual compiled queue protocols", () => {
    const dispatcher = source("daily-report-dispatcher");
    const routes = [...dispatcher.matchAll(/^      - ([a-z-]+)$/gm)].map(match => match[1]);
    expect(routes).toEqual(REPORT_PROFILES);
    expect(dispatcher).toContain("if: github.run_attempt == 1");
    expect(dispatcher).toContain("Date.parse(run.created_at) - 86400000");
    expect(dispatcher).not.toMatch(/^\s+workflow_dispatch:/m);
    expect(dispatcher).toContain("    max: 3");
    const lock = fs.readFileSync(new URL("../../../.github/workflows/daily-report-dispatcher.lock.yml", import.meta.url), "utf8");
    expect(lock).toContain("if: github.run_attempt == 1");
    expect(lock).toContain("daily_report_portfolio.cjs");
    for (const profile of REPORT_PROFILES) {
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
      if (["deep-report", "artifacts-summary", "repo-tree-map"].includes(profile)) expect(frontmatter).toContain("fallback-to-issue: false");
    }
    expect(source("daily-evals-report")).not.toContain("create_issue");
    expect(source("daily-token-consumption-report")).not.toContain("create_issue");
  });

  it("leaves timing-sensitive, stateful, remediation and other weekly workflows on their own schedules", () => {
    for (const profile of [
      "daily-news",
      "daily-arxiv-researcher",
      "daily-cache-strategy-analyzer",
      "daily-hippo-learn",
      "org-health-report",
      "portfolio-analyst",
      "archivx-agentic-workflows-analyzer",
      "workflow-skill-extractor",
      "dataflow-pr-discussion-dataset",
      "firewall-escape",
      "lint-monster",
      "issue-arborist",
      "smoke-copilot",
      "constraint-solving-potd",
    ]) {
      expect(REPORT_PROFILES).not.toContain(profile);
      expect(source(profile).split("\n---")[0]).toMatch(/^\s+schedule:/m);
    }
    expect(REPORT_PROFILES).not.toContain("agent-performance-analyzer");
    expect(source("agent-performance-analyzer")).toContain("on: daily");
  });

  it("preserves DeepReport's bounded follow-ups without authorizing unscoped repository-memory writes", () => {
    const plan = buildDailyReportPlan(planOptions);
    const deepReport = plan.nodes.find(node => node.worker_profile === "deep-report");
    expect(deepReport.depends_on).toEqual([]);
    expect(deepReport.payload.effect_contract.outputs).toEqual([
      { type: "create_discussion", min: 1, max: 1 },
      ...["noop", "report_incomplete", "missing_tool", "missing_data"].map(type => ({ type, min: 0, max: 1 })),
      { type: "create_issue", min: 0, max: 7 },
      { type: "add_comment", min: 0, max: 3 },
      { type: "upload_artifact", min: 0, max: 3 },
    ]);
    const worker = source("deep-report");
    expect(worker.split("\n---")[0]).not.toContain("repo-memory:");
    expect(worker).toContain("  cache-memory: true");
    expect(worker).toContain("/tmp/gh-aw/cache-memory/deep-report/");
    expect(worker).toContain("Do not wait for this activation's sibling workers");
    expect(worker).toContain("[report_date]");
    expect(worker).toContain("Do not invent tasks to reach a count");
    expect(worker).toContain("work_queue_claim_finish");
  });
});
