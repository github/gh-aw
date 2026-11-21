// @ts-check
import { describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { BATCH, CLUSTER_SIZE, POOL, WORKER, alertFingerprint, buildCodeScanningPlan, buildCodeScanningPolicy, clusterKey, hasUncoveredAlert, listOpenPullFiles, selectAssignedCluster } from "./code_scanning_portfolio.cjs";
import { normalizeSubmitParameters, stagePlanIntents } from "./work_queue_intents.cjs";
import { replayTransactions } from "./work_queue_replay.cjs";
import { assignmentForDispatch } from "./work_queue_scheduler.cjs";
import { normalizeAssignment } from "./work_queue_claim_scope.cjs";
import { frozenResourceScope } from "./work_queue_resource_scope.cjs";
import { createRequire } from "node:module";
import { sanitizeContent } from "./sanitize_content.cjs";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const repository = "owner/repo";
const repositoryId = "7";
const settings = JSON.parse(fs.readFileSync(new URL("../../../.github/workflows/aw.json", import.meta.url), "utf8")).work_queue;

/**
 * @param {number} number
 * @param {{tool?: string, rule?: string, level?: string, path?: string, line?: number, message?: string}} [options]
 */
function alert(number, { tool = "CodeQL", rule = `js/rule-${number}`, level = "medium", path = `src/dir-${number}/file.js`, line = 10, message = `Untrusted finding ${number}` } = {}) {
  const severity = ["critical", "high", "medium", "low"].includes(level) ? { security_severity_level: level } : { severity: level };
  return { number, state: "open", tool: { name: tool }, rule: { id: rule, ...severity }, most_recent_instance: { location: { path, start_line: line }, message: { text: message } } };
}

/**
 * @param {any[]} alerts
 * @param {{pulls?: any[], works?: any[]}} [options]
 */
function plan(alerts, { pulls = [], works = [] } = {}) {
  return buildCodeScanningPlan({ alerts, pulls, works, repository, repositoryId });
}

/** @param {{nodes: Array<{payload: {alerts: Array<{number: number}>}}>}} result */
function clusters(result) {
  return result.nodes.map(node => node.payload.alerts.map(member => member.number));
}

/**
 * @param {number} number
 * @param {{title?: string, body?: string, files?: string[]}} fields
 */
function pull(number, { title = "Unrelated PR", body = "", files = [] }) {
  return { number, title, body, files };
}

function ledger() {
  /** @type {any[]} */
  let log = [];
  const policy = buildCodeScanningPolicy({ repository, settings });
  const state = () => replayTransactions(log);
  return {
    policy,
    state,
    works: () => [...state().works.values()],
    /** @param {any[]} nodes */
    submit: nodes => {
      const fresh = normalizeSubmitParameters({ nodes }, policy).filter(work => !state().works.has(work.work_id));
      if (fresh.length) log = h.submit(log, fresh, { ...h.producer, run_id: String(10 + log.length) });
    },
    /** @param {{pool: string, max_claims: number, max_dispatches: number}} request */
    dispatch: request => {
      const pool = policy.pools[request.pool];
      const profiles = Object.fromEntries(Object.entries(pool.profiles).map(([name, profile]) => [name, { workflow: profile.workflow }]));
      log = h.dispatchNext(log, { ...request, concurrency: pool.concurrency, per_account_limit: pool.per_account_limit, profiles }).log;
    },
    /** @param {string} work_id */
    cancel: work_id => {
      log = h.commit(log, h.administrator, "cancel_work", { work_ids: [work_id], reason: "attempts_exhausted" }, [{ kind: "WorkCancellation", work_id, reason: "attempts_exhausted" }]);
    },
  };
}

/**
 * Minimal Octokit double: paginate resolves list endpoints from fixtures.
 * @param {{pulls: any[], files: Record<number, any[] | Error>}} fixtures
 */
function fakeGithub(fixtures) {
  const github = {
    rest: { pulls: { list: () => {}, listFiles: () => {} } },
    /** @type {any[]} */
    calls: [],
    /**
     * @param {any} method
     * @param {any} params
     */
    paginate: async (method, params) => {
      github.calls.push(params);
      if (method === github.rest.pulls.list) return fixtures.pulls;
      const files = fixtures.files[params.pull_number];
      if (files instanceof Error) throw files;
      if (!files) throw new Error(`unexpected pull request ${params.pull_number}`);
      return files;
    },
  };
  return github;
}

const ai = Array.from({ length: 7 }, (_, index) => alert(50 + index, { tool: "AI Scan", rule: "ai-scan/cwe-79", level: "low", path: `pkg/web/file-${index}.go` }));
const aiRuleOnly = alert(9, { tool: "", rule: "ai-scan/cwe-22", level: "note" });
const backlog = [alert(4, { level: "high" }), ...ai, alert(3, { level: "critical" }), aiRuleOnly];

describe("code scanning work-queue portfolio", () => {
  it("stages AI Scan-first clusters of related alerts as three bounded fixer tasks", () => {
    const result = plan(backlog);
    expect(plan([...backlog].reverse())).toEqual(result);
    expect(plan([...backlog, ai[3]])).toEqual(result);
    expect(result).toMatchObject({ version: 1, open_alerts: 10, admitted: 3, covered: [], dispatch: { pool: "code-scanning", max_claims: 3, max_dispatches: 3 } });
    expect(clusters(result)).toEqual([[9], [50, 51, 52, 53, 54], [3]]);
    expect(result.nodes.map(node => node.priority)).toEqual([1, 1, 2]);
    expect(Buffer.byteLength(JSON.stringify(result))).toBeLessThan(16384);
    for (const node of result.nodes) {
      const lead = backlog.find(candidate => candidate.number === node.payload.alerts[0].number);
      expect(node).toMatchObject({ pool: POOL, worker_profile: WORKER, fairness_key: "", depends_on: [] });
      expect(node.graph_id).toMatch(/^code-scanning-cluster:[0-9a-f]{64}$/);
      expect(node.node_key).toMatch(/^[0-9a-f]{64}$/);
      expect(node.payload).toMatchObject({ cluster_key: clusterKey(lead), tool: lead?.tool.name, rule_id: lead?.rule.id });
      expect(node.payload.alerts.length).toBeLessThanOrEqual(CLUSTER_SIZE);
      for (const member of node.payload.alerts) {
        const source = backlog.find(candidate => candidate.number === member.number);
        expect(member).toEqual({ number: source?.number, severity: expect.any(String), path: source?.most_recent_instance.location.path, fingerprint: alertFingerprint(source) });
      }
      expect(frozenResourceScope(node.payload)).toEqual({ version: 1, resources: [{ host: "github.com", repository, repository_id: repositoryId }] });
      expect(node.payload.effect_contract).toEqual({ version: 1, outputs: expect.arrayContaining([{ type: "create_pull_request", min: 1, max: 1 }]) });
    }
    expect(result.nodes.map(node => node.payload.ai_scan)).toEqual([true, true, false]);
    const severity = plan([alert(9, { level: "note" }), alert(2, { level: "error" }), alert(5, { level: "warning" }), alert(6, { level: "low" }), { number: 1 }]);
    expect(clusters(severity)).toEqual([[2], [5], [9]]);
    expect(severity.nodes.map(node => node.priority)).toEqual([2, 3, 4]);
    expect(plan([{ number: 1 }]).nodes[0]).toMatchObject({ priority: 5, payload: { severity: "unknown", rule_id: "", tool: "", alerts: [{ number: 1, path: "", fingerprint: '["","",0,""]' }] } });
    const astral = plan([alert(8, { level: "high", message: `${"x".repeat(255)}😀😀` })]).nodes[0].payload.alerts[0].fingerprint;
    expect(JSON.parse(astral)[3]).toBe(`${"x".repeat(255)}😀`);
  });

  it("splits clusters by scanner, rule and parent directory and never stages one file twice", () => {
    const otherDirectory = alert(60, { tool: "AI Scan", rule: "ai-scan/cwe-79", level: "low", path: "pkg/other/file.go" });
    const sameFileOtherRule = alert(61, { tool: "AI Scan", rule: "ai-scan/other", level: "low", path: "pkg/web/file-0.go" });
    const otherScanner = alert(62, { tool: "CodeQL", rule: "ai-scan/cwe-79", level: "low", path: "pkg/web/scanner.go" });
    expect(clusters(plan([otherDirectory, sameFileOtherRule, otherScanner, ...ai]))).toEqual([[50, 51, 52, 53, 54], [60], [62]]);
    expect(clusters(plan([sameFileOtherRule]))).toEqual([[61]]);
    expect(clusterKey(ai[0])).toBe(Buffer.from('["AI Scan","ai-scan/cwe-79","pkg/web"]').toString("base64"));
  });

  it("skips every alert an open pull request covers and reports the blocking pull requests", () => {
    const target = alert(42, { level: "critical", path: "example.go" });
    const other = alert(77, { level: "low", path: "unrelated.go" });
    const fresh = (/** @type {any[]} */ pulls) => clusters(plan([target, other], { pulls }));
    expect(fresh([])).toEqual([[42], [77]]);
    const renamed = plan([target, other], { pulls: [pull(101, { title: "Refactor example", files: ["example.go", "renamed.go"] })] });
    expect(clusters(renamed)).toEqual([[77]]);
    expect(renamed.covered).toEqual([{ number: 42, pull_numbers: [101] }]);
    const url = `https://github.com/${repository}/security/code-scanning/42`;
    expect(fresh([pull(100, { body: `Fixes ${url}.` })])).toEqual([[77]]);
    for (const body of [`${url}0`, "https://github.com/other/repo/security/code-scanning/42", "Fix issue #42; unrelated work"]) expect(fresh([pull(100, { body })])).toEqual([[42], [77]]);
    for (const body of ["**Alert Number**: #42", "**Alerts**: #43, #42", "Fix AI Scan #42"]) expect(fresh([pull(100, { body })])).toEqual([[77]]);
    expect(fresh([pull(100, { title: "Fix code scanning alert #42" })])).toEqual([[77]]);
    expect(hasUncoveredAlert({ alerts: [target], pulls: [pull(100, { body: `Fixes ${url}` })], repository })).toBe(false);
    expect(hasUncoveredAlert({ alerts: [target, other], pulls: [pull(100, { body: `Fixes ${url}` })], repository })).toBe(true);
  });

  it("lets a fixer pull request's cluster marker block that cluster only, including unbatched alerts", () => {
    const marker = `Code scanning cluster: ${clusterKey(ai[0])}`;
    const body = sanitizeContent(`# Security Fix\n\n${marker}`);
    expect(body).toContain(marker);
    const fixer = pull(100, { title: "[code-scanning-fix] Escape rendered HTML", body });
    const result = plan(backlog, { pulls: [fixer] });
    expect(clusters(result)).toEqual([[9], [3], [4]]);
    expect(result.covered.map(entry => entry.number)).toEqual([50, 51, 52, 53, 54, 55, 56]);
    expect(clusters(plan(backlog, { pulls: [{ ...fixer, title: "Unrelated PR" }] }))).toEqual(clusters(plan(backlog)));
  });

  it("keeps queued Work's alerts, clusters and files out of new Work and re-stages stored Work when nothing is new", () => {
    const queue = ledger();
    const first = plan(backlog);
    queue.submit(first.nodes);
    expect(queue.works()).toHaveLength(3);
    const sameFile = alert(70, { rule: "js/other", path: "pkg/web/file-0.go", level: "critical" });
    const second = plan([...backlog, sameFile], { works: queue.works() });
    expect(clusters(second)).toEqual([[4]]);
    queue.submit(second.nodes);
    const admitted = queue.works();
    const third = plan(
      backlog.filter(candidate => candidate.number !== 55 && candidate.number !== 56),
      { works: admitted }
    );
    expect(third).toMatchObject({ admitted: 0, open_alerts: 8 });
    expect(third.nodes).toEqual([...first.nodes, ...second.nodes].slice(0, BATCH));
    queue.submit(third.nodes);
    expect(queue.works()).toEqual(admitted);
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "code-scanning-plan-"));
    try {
      expect(stagePlanIntents(path.join(directory, "work-queue.intents.jsonl"), third).map(intent => intent.kind)).toEqual(["submit", "dispatch_next"]);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("re-admits alerts after a closed remediation pull request but suppresses fingerprints whose retries ran out", () => {
    const queue = ledger();
    const target = alert(42, { level: "critical", path: "example.go" });
    const first = plan([target]);
    queue.submit(first.nodes);
    const [work] = queue.works();
    const completed = [{ ...work, state: "completed" }];
    const markerPull = pull(100, { title: "[code-scanning-fix] Fix example", body: `Code scanning cluster: ${clusterKey(target)}` });
    const covered = plan([target], { pulls: [markerPull], works: completed });
    expect(covered).toMatchObject({ admitted: 0, covered: [{ number: 42, pull_numbers: [100] }] });
    expect(covered.nodes).toEqual(first.nodes);
    const reopened = plan([target], { works: completed });
    expect(reopened.admitted).toBe(1);
    expect(reopened.nodes[0].graph_id).toBe(first.nodes[0].graph_id);
    expect(reopened.nodes[0].node_key).not.toBe(first.nodes[0].node_key);
    queue.submit(reopened.nodes);
    expect(queue.works()).toHaveLength(2);
    const exhausted = ledger();
    exhausted.submit(first.nodes);
    exhausted.cancel(exhausted.works()[0].work_id);
    expect(exhausted.works()[0].state).toBe("cancelled");
    const suppressed = plan([target], { works: exhausted.works() });
    expect(suppressed.admitted).toBe(0);
    expect(suppressed.nodes).toEqual(first.nodes);
    const moved = plan([alert(42, { level: "critical", path: "example.go", line: 99 })], { works: exhausted.works() });
    expect(moved.admitted).toBe(1);
    exhausted.submit(moved.nodes);
    expect(exhausted.works().map(entry => entry.state)).toEqual(["cancelled", "available"]);
  });

  it("grants three Claims from the dedicated pool and projects each worker's cluster without covered alerts", () => {
    const queue = ledger();
    expect(queue.policy.pools[POOL]).toMatchObject({ concurrency: 3, max_attempts: 3 });
    expect(queue.policy.pools[POOL].profiles[WORKER]).toEqual({ workflow: `.github/workflows/${WORKER}.lock.yml`, effect_scope: repository });
    const first = plan(backlog);
    queue.submit(first.nodes);
    queue.dispatch(first.dispatch);
    expect(queue.state().claims.size).toBe(3);
    const assignments = [...queue.state().dispatches.values()].map(entry => normalizeAssignment(JSON.parse(JSON.stringify(assignmentForDispatch(queue.state(), entry.dispatch_id)))));
    const assignment = assignments.find(entry => entry.claims[0].work.alerts.length === CLUSTER_SIZE);
    const selection = selectAssignedCluster({ assignment, pulls: [pull(200, { title: "Refactor", files: ["pkg/web/file-1.go"] })], repository });
    expect(selection).toMatchObject({ claim_handle: assignment.claims[0].handle, cluster_key: clusterKey(ai[0]), tool: "AI Scan", ai_scan: true, rule_id: "ai-scan/cwe-79" });
    expect(selection.alerts.map(member => member.number)).toEqual([50, 52, 53, 54]);
    expect(selection.alerts[0]).toEqual({ number: 50, severity: "low", fingerprint: alertFingerprint(ai[0]) });
    expect(selection.covered_alerts).toEqual([{ number: 51, pull_numbers: [200] }]);
    expect(() => selectAssignedCluster({ assignment: { ...assignment, claims: [...assignment.claims, ...assignment.claims] }, pulls: [], repository })).toThrow(/exactly one original Claim/);
    const malformed = { ...assignment, claims: [{ ...assignment.claims[0], work: { ...assignment.claims[0].work, alerts: [] } }] };
    expect(() => selectAssignedCluster({ assignment: malformed, pulls: [], repository })).toThrow(/malformed/);
  });

  it("lists every open pull request with renamed paths and fails closed on incomplete coverage", async () => {
    const pulls = [
      { number: 100, title: "Unrelated PR", body: null },
      { number: 101, title: "Rename", body: "Moves code" },
    ];
    const github = fakeGithub({ pulls, files: { 100: [{ filename: "unrelated.go" }], 101: [{ filename: "another.go" }, { filename: "renamed.go", previous_filename: "example.go" }] } });
    const inventory = await listOpenPullFiles(github, { owner: "owner", repo: "repo" });
    expect(inventory).toEqual([
      { number: 100, title: "Unrelated PR", body: "", files: ["unrelated.go"] },
      { number: 101, title: "Rename", body: "Moves code", files: ["another.go", "example.go", "renamed.go"] },
    ]);
    expect(github.calls).toEqual([
      { owner: "owner", repo: "repo", state: "open", per_page: 100 },
      { owner: "owner", repo: "repo", pull_number: 100, per_page: 100 },
      { owner: "owner", repo: "repo", pull_number: 101, per_page: 100 },
    ]);
    const covered = plan([alert(42, { path: "example.go" }), alert(43, { path: "unrelated.go" }), alert(44, { path: "other.go" })], { pulls: inventory });
    expect(covered.covered).toEqual([
      { number: 42, pull_numbers: [101] },
      { number: 43, pull_numbers: [100] },
    ]);
    expect(clusters(covered)).toEqual([[44]]);
    for (const files of [new Error("API unavailable"), [{ status: "modified" }], Array.from({ length: 3000 }, (_, index) => ({ filename: `file-${index}.go` }))]) {
      await expect(listOpenPullFiles(fakeGithub({ pulls: [pulls[1]], files: { 101: files } }), { owner: "owner", repo: "repo" })).rejects.toThrow(/API unavailable|pull request #101/);
    }
  });

  it("rejects malformed alerts, pull requests, queue Work, unverified repository identity, and an empty schedule", () => {
    expect(() => plan([{ ...alert(7), number: "7" }])).toThrow(/malformed/);
    expect(() => plan([alert(7)], { pulls: [{ number: 1, files: "a.go" }] })).toThrow(/pull request entry is malformed/);
    expect(() => plan([alert(7)], { works: [{ pool: POOL, graph_id: "code-scanning-alert:7", payload: {} }] })).toThrow(/Work is malformed/);
    expect(() => plan([])).toThrow(/No schedulable/);
    for (const id of ["0", "007", "github-actions[bot]"]) expect(() => buildCodeScanningPlan({ alerts: [alert(7)], pulls: [], works: [], repository, repositoryId: id })).toThrow(/identity/);
    expect(() => buildCodeScanningPlan({ alerts: [alert(7)], pulls: [], works: [], repository: "not-a-repo", repositoryId })).toThrow(/owner\/name/);
  });
});
