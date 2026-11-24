// @ts-check
import { describe, expect, it } from "vitest";
import fs from "node:fs";
import { createRequire } from "node:module";
import { DAILY_REPORTS, FIXED_DAILY_REPORTS, WEEKLY_REPORTS, REPORT_PROFILES, REPORTS_PER_DAY, REPORT_POOL, reportsForDay, buildDailyReportPlan } from "./daily_report_portfolio.cjs";
import { normalizeSubmitParameters } from "./work_queue_intents.cjs";
import { replayTransactions } from "./work_queue_replay.cjs";
import { policyAllowsWorker, workerWorkflow } from "./work_queue_policy.cjs";
import { frozenResourceScope } from "./work_queue_resource_scope.cjs";
import { readDocumentMetadata } from "../../../scripts/markdown_metadata_test_adapter.cjs";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const repository = "owner/repo";
const policy = h.compiledDispatcherPolicy("daily-report-dispatcher", repository);
const planOptions = { repository, repositoryId: "7", date: "2026-10-07" };
const source = name => fs.readFileSync(new URL(`../../../.github/workflows/${name}.md`, import.meta.url), "utf8");

/** Admit the plan's new Work (existing Work IDs are idempotent no-ops). */
function admit(log, plan, policy) {
  const state = replayTransactions(log);
  const fresh = normalizeSubmitParameters({ nodes: plan.nodes }, policy).filter(work => !state.works.has(work.work_id));
  return fresh.length ? h.submit(log, fresh, { ...h.producer, run_id: String(10 + log.length) }) : log;
}

/** Commit the plan's dispatch_next with the compiled pool limits, launching only report workers. */
function dispatch(log, plan, policy, runId = "15") {
  const pool = policy.pools[plan.dispatch.pool];
  const profiles = Object.fromEntries(REPORT_PROFILES.map(profile => [profile, { workflow: workerWorkflow(profile) }]));
  return h.dispatchNext(log, { ...plan.dispatch, concurrency: pool.concurrency, per_account_limit: pool.per_account_limit, profiles }, { ...h.dispatcher, run_id: runId }).log;
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
      expect(node.payload.effect_contract.outputs).toContainEqual({ type: "create_discussion", min: 1, max: 1 });
      expect(node.payload.effect_contract.outputs.some(output => output.type === "create_issue")).toBe(node.worker_profile === "deep-report");
    }
    expect(() => buildDailyReportPlan({ ...planOptions, repository: "not-a-repository" })).toThrow();
    expect(() => buildDailyReportPlan({ ...planOptions, repositoryId: "0" })).toThrow();
    expect(() => buildDailyReportPlan({ ...planOptions, repositoryId: "007" })).toThrow();
  });

  it("matches each report worker's compiled upload-asset limit", () => {
    const assetLimits = { "daily-firewall-report": 3, "daily-repo-chronicle": 3, "daily-issues-report": 5 };
    const coveredProfiles = new Set();
    for (let day = 1; day <= 14; day++) {
      const date = `2026-11-${String(day).padStart(2, "0")}`;
      for (const node of buildDailyReportPlan({ ...planOptions, date }).nodes) {
        coveredProfiles.add(node.worker_profile);
        expect(node.payload.effect_contract.outputs.find(output => output.type === "upload_asset")?.max).toBe(assetLimits[node.worker_profile]);
      }
    }
    expect(coveredProfiles).toEqual(new Set(REPORT_PROFILES));
  });

  it("uses shared scheduling and all thirteen AW worker routes without enrollment", () => {
    expect(policy.pools[REPORT_POOL]).toEqual({ concurrency: 3, per_account_limit: 1, max_attempts: 3 });
    for (const profile of REPORT_PROFILES) expect(policyAllowsWorker(policy, REPORT_POOL, profile), profile).toBe(true);
    expect(policy.effect_scope).toBe(repository);
    expect(() => buildDailyReportPlan({ ...planOptions, repository: "not-a-repository" })).toThrow();
  });

  it("records idempotent daily Work and three singleton Claims in the ledger", () => {
    const plan = buildDailyReportPlan(planOptions);
    let log = admit([], plan, policy);
    const originalWork = [...replayTransactions(log).works.values()];
    log = admit(log, plan, policy);
    expect([...replayTransactions(log).works.values()]).toEqual(originalWork);
    expect(originalWork).toHaveLength(3);
    log = dispatch(log, plan, policy);
    let state = replayTransactions(log);
    expect(state.claims.size).toBe(3);
    expect(state.dispatches.size).toBe(3);
    for (const record of state.dispatches.values()) {
      expect(record.claims).toHaveLength(1);
      expect(plan.selected).toContain(record.workflow.split("/").pop().replace(".lock.yml", ""));
    }
    log = dispatch(log, plan, policy, "16");
    expect(replayTransactions(log).claims.size).toBe(3);
    const tomorrow = buildDailyReportPlan({ ...planOptions, date: "2026-10-08" });
    log = admit(log, tomorrow, policy);
    log = dispatch(log, tomorrow, policy, "17");
    state = replayTransactions(log);
    expect(state.works.size).toBe(6);
    expect(state.claims.size).toBe(3);
    expect(state.dispatches.size).toBe(3);
  });

  it.each(["2026-10-10", "2026-10-11"])("admits weekly reports idempotently with exactly one required discussion on %s", date => {
    const plan = buildDailyReportPlan({ ...planOptions, date });
    const log = admit(admit([], plan, policy), plan, policy);
    expect(replayTransactions(log).works.size).toBe(3);
    const weekly = plan.nodes.find(node => WEEKLY_REPORTS.some(report => report.profile === node.worker_profile));
    expect(weekly).toBeDefined();
    expect(weekly.payload.report_date).toBe(date);
    expect(weekly.payload.effect_contract.outputs).toContainEqual({ type: "create_discussion", min: 1, max: 1 });
    const state = replayTransactions(dispatch(log, plan, policy));
    expect(state.dispatches.size).toBe(3);
    expect([...state.dispatches.values()].some(record => record.workflow === `.github/workflows/${weekly.worker_profile}.lock.yml`)).toBe(true);
  });

  it("keeps the dispatcher allowlist and thirteen dispatch-only Claim workers wired to actual compiled queue protocols", () => {
    const dispatcher = source("daily-report-dispatcher");
    const routes = [...dispatcher.matchAll(/^      - ([a-z-]+)$/gm)].map(match => match[1]);
    expect(routes).toEqual(REPORT_PROFILES);
    expect(dispatcher).toContain("if: github.run_attempt == 1");
    expect(dispatcher).toContain("schedule: daily around 12:00");
    expect(dispatcher).toContain("Date.parse(run.created_at) - 86400000");
    expect(dispatcher).toContain("stagePlanIntents(");
    expect(dispatcher).toContain("shared/work-queue-staged-plan.md");
    expect(fs.readFileSync(new URL("../../../.github/workflows/shared/work-queue-staged-plan.md", import.meta.url), "utf8")).toContain("Do not submit, dispatch, edit intent files");
    expect(dispatcher).not.toMatch(/^\s+workflow_dispatch:/m);
    expect(dispatcher).toContain("    max: 3");
    const lock = fs.readFileSync(new URL("../../../.github/workflows/daily-report-dispatcher.lock.yml", import.meta.url), "utf8");
    const document = readDocumentMetadata(lock, { version: "1.2", uniqueKeys: true });
    const activation = document.value.jobs.activation;
    expect(activation.if).toContain("github.run_attempt == 1");
    expect(activation.if).not.toContain("needs.admission");
    expect(activation.needs).toBeUndefined();
    expect(document.value.jobs).not.toHaveProperty("admission");
    const admissionIndex = activation.steps.findIndex(step => step.id === "admission");
    expect(admissionIndex).toBeGreaterThan(-1);
    expect(activation.steps[admissionIndex]).toMatchObject({
      name: "Check workflow admission",
      if: "vars.GH_AW_DEFAULT_ADMISSION_URL != ''",
    });
    expect(activation.outputs["admission-result"]).toBe("${{ steps.admission.outputs.result }}");
    expect(admissionIndex).toBeLessThan(activation.steps.findIndex(step => step.name === "Restore daily AIC scan observations"));
    expect(admissionIndex).toBeLessThan(activation.steps.findIndex(step => step.name === "Check daily workflow token guardrail"));
    expect(lock).toContain("daily_report_portfolio.cjs");
    expect(lock).toContain("name: Restore daily AIC scan observations");
    expect(lock).toContain("name: Check daily workflow token guardrail");
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
      expect(compiled).toContain("name: Finish work queue claim");
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
    const hippoFrontmatter = source("daily-hippo-learn").split("\n---")[0];
    expect(hippoFrontmatter).toMatch(/^\s+workflow_dispatch:/m);
    expect(hippoFrontmatter).not.toMatch(/^\s+schedule:/m);
    expect(REPORT_PROFILES).not.toContain("agent-performance-analyzer");
    expect(source("agent-performance-analyzer")).toContain("on: daily");
  });

  it("keeps the manual-only hippo workflow outside the report portfolio", () => {
    expect(REPORT_PROFILES).not.toContain("daily-hippo-learn");
    const frontmatter = source("daily-hippo-learn").split("\n---")[0];
    expect(frontmatter).toMatch(/^\s+workflow_dispatch:/m);
    expect(frontmatter).not.toMatch(/^\s+schedule:/m);
    const compiled = readDocumentMetadata(fs.readFileSync(new URL("../../../.github/workflows/daily-hippo-learn.lock.yml", import.meta.url), "utf8"), { version: "1.2", uniqueKeys: true });
    expect(compiled.value.on).toHaveProperty("workflow_dispatch");
    expect(compiled.value.on).not.toHaveProperty("schedule");
  });

  it("preserves DeepReport's bounded follow-ups without authorizing unscoped repository-memory writes", () => {
    const plan = buildDailyReportPlan(planOptions);
    const deepReport = plan.nodes.find(node => node.worker_profile === "deep-report");
    expect(deepReport.depends_on).toEqual([]);
    expect(deepReport.payload.effect_contract.outputs).toEqual([
      { type: "create_discussion", min: 1, max: 1 },
      ...["noop", "report_incomplete", "report_missing_tool", "report_missing_data"].map(type => ({ type, min: 0, max: 1 })),
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
