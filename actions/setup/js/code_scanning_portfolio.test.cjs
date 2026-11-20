// @ts-check
import { describe, expect, it } from "vitest";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { BATCH, POOL, WORKER, alertFingerprint, buildCodeScanningPlan, buildCodeScanningPolicy } from "./code_scanning_portfolio.cjs";
import { normalizeDispatchParameters, normalizeSubmitParameters, stagePlanIntents } from "./work_queue_intents.cjs";
import { newState, newRequest, generateRequestOperations, replayTransactions } from "./work_queue_replay.cjs";
import { frozenResourceScope } from "./work_queue_resource_scope.cjs";
import { validateDeliveryContract } from "./work_queue_delivery.cjs";

const repository = "owner/repo";
const repositoryId = "7";
const settings = JSON.parse(fs.readFileSync(new URL("../../../.github/workflows/aw.json", import.meta.url), "utf8")).work_queue;
const administrator = { role: "administrator", repository, principal: "11" };
const producer = { role: "producer", repository, principal: "11" };
const dispatcher = { role: "dispatcher", repository, principal: "11", workflow: ".github/workflows/code-scanning-dispatcher.lock.yml", run_id: "15", run_attempt: 1 };

/**
 * @param {number} number
 * @param {string} level
 * @param {string} [message]
 */
function alert(number, level, message = `Untrusted finding ${number}`) {
  const severity = ["critical", "high", "medium", "low"].includes(level) ? { security_severity_level: level } : { severity: level };
  return { number, state: "open", rule: { id: `js/rule-${number}`, ...severity }, most_recent_instance: { location: { path: `src/file-${number}.js`, start_line: number * 10 }, message: { text: message } } };
}

const alerts = [alert(40, "medium"), alert(3, "low"), alert(12, "medium"), alert(7, "critical"), alert(21, "high")];
const moved = alerts.map(candidate => (candidate.number === 7 ? alert(7, "critical", "Finding moved") : candidate));
const ALERT_7 = "code-scanning-alert:7";

/**
 * @param {any[]} works
 * @param {any[]} [list]
 */
function plan(works, list = alerts) {
  return buildCodeScanningPlan({ alerts: list, works, repository, repositoryId });
}

/** @param {{nodes: Array<{graph_id: string}>}} result */
function graphs(result) {
  return result.nodes.map(node => node.graph_id);
}

function ledger() {
  let state = newState();
  /** @type {any[]} */
  const transactions = [];
  let at = 1000;
  /**
   * @param {string} kind
   * @param {any} parameters
   * @param {any} actor
   */
  const append = (kind, parameters, actor) => {
    const request = newRequest(`code-scanning:${transactions.length}:${++at}`, kind, actor, parameters);
    const id = `commit:${at}`;
    const decision = generateRequestOperations(state, request, actor, at, id);
    if (decision.operations.length) {
      transactions.push({ version: 3, id, previous: state.tip || null, request, actor, policy_epoch: "code-scanning-v1", at, operations: decision.operations });
      state = replayTransactions(transactions);
    }
    return state;
  };
  const policy = buildCodeScanningPolicy({ repository, ref: "a".repeat(40), settings });
  append("policy", { operations: [{ kind: "Policy", epoch: "code-scanning-v1", policy }] }, administrator);
  return {
    policy,
    state: () => state,
    works: () => [...state.works.values()],
    /** @param {any[]} nodes */
    submit: nodes => append("submit", normalizeSubmitParameters({ nodes }, policy, at, state), producer),
    /** @param {any} request */
    dispatch: request => append("dispatch_next", normalizeDispatchParameters(request, policy, BATCH), dispatcher),
    /** @param {string} work_id */
    cancel: work_id => append("cancel_work", { operations: [{ kind: "WorkCancellation", work_id, reason: "attempts_exhausted" }] }, administrator),
  };
}

describe("code scanning work-queue portfolio", () => {
  it("ranks open alerts by severity into three bounded fixer tasks", () => {
    const result = plan([]);
    expect(plan([], [...alerts].reverse())).toEqual(result);
    expect(plan([], [...alerts, alert(7, "critical")])).toEqual(result);
    expect(result).toMatchObject({ version: 1, open_alerts: 5, admitted: 3, dispatch: { pool: "code-scanning", max_claims: 3, max_dispatches: 3 } });
    expect(graphs(result)).toEqual([ALERT_7, "code-scanning-alert:21", "code-scanning-alert:12"]);
    expect(result.nodes.map(node => node.priority)).toEqual([1, 2, 3]);
    expect(Buffer.byteLength(JSON.stringify(result))).toBeLessThan(8192);
    for (const node of result.nodes) {
      const source = alerts.find(candidate => node.graph_id === `code-scanning-alert:${candidate.number}`);
      expect(node).toMatchObject({ pool: POOL, worker_profile: WORKER, fairness_key: "", depends_on: [] });
      expect(node.node_key).toMatch(/^[0-9a-f]{64}$/);
      expect(node.payload).toMatchObject({ alert_number: source?.number, rule_id: source?.rule.id, fingerprint: alertFingerprint(source) });
      expect(frozenResourceScope(node.payload)).toEqual({ version: 1, resources: [{ host: "github.com", repository, repository_id: repositoryId }] });
      expect(validateDeliveryContract(node.payload.effect_contract)).toEqual(node.payload.effect_contract);
      expect(node.payload.effect_contract.outputs).toContainEqual({ type: "create_pull_request", min: 1, max: 1 });
    }
    const ruleSeverity = plan([], [alert(9, "note"), alert(2, "error"), alert(5, "warning"), { number: 1 }]);
    expect(graphs(ruleSeverity)).toEqual(["code-scanning-alert:2", "code-scanning-alert:5", "code-scanning-alert:9"]);
    expect(ruleSeverity.nodes.map(node => node.priority)).toEqual([1, 2, 3]);
    expect(plan([], [{ number: 1 }]).nodes[0]).toMatchObject({ priority: 5, payload: { severity: "unknown", rule_id: "", fingerprint: '["","",0,""]' } });
    const astral = plan([], [alert(8, "high", `${"x".repeat(255)}😀😀`)]).nodes[0].payload.fingerprint;
    expect(JSON.parse(astral)[3]).toBe(`${"x".repeat(255)}😀`);
  });

  it("admits each alert once, then re-stages identical stored Work so dispatch can still grant retries", () => {
    const queue = ledger();
    const first = plan([]);
    queue.submit(first.nodes);
    const second = plan(queue.works());
    expect(second.admitted).toBe(2);
    expect(graphs(second)).toEqual(["code-scanning-alert:40", "code-scanning-alert:3"]);
    queue.submit(second.nodes);
    const admitted = queue.works();
    expect(admitted).toHaveLength(5);
    const third = plan(admitted);
    expect(third).toMatchObject({ open_alerts: 5, admitted: 0 });
    expect(third.nodes).toEqual(first.nodes);
    queue.submit(third.nodes);
    expect(queue.works()).toEqual(admitted);
    const directory = fs.mkdtempSync(path.join(os.tmpdir(), "code-scanning-plan-"));
    try {
      expect(stagePlanIntents(path.join(directory, "work-queue.intents.jsonl"), third).map(intent => intent.kind)).toEqual(["submit", "dispatch_next"]);
    } finally {
      fs.rmSync(directory, { recursive: true, force: true });
    }
  });

  it("reopens an alert with a changed fingerprint only after its earlier Work was cancelled", () => {
    const queue = ledger();
    const first = plan([]);
    queue.submit(first.nodes);
    const completed = queue.works().map(work => (work.graph_id === ALERT_7 ? { ...work, state: "completed" } : work));
    for (const works of [queue.works(), completed]) expect(graphs(plan(works, moved))).toEqual(["code-scanning-alert:40", "code-scanning-alert:3"]);
    queue.cancel(queue.works().find(work => work.graph_id === ALERT_7)?.work_id);
    expect(graphs(plan(queue.works()))).toEqual(["code-scanning-alert:40", "code-scanning-alert:3"]);
    const retried = plan(queue.works(), moved);
    expect(retried.admitted).toBe(3);
    expect(graphs(retried)).toEqual([ALERT_7, "code-scanning-alert:40", "code-scanning-alert:3"]);
    expect(retried.nodes[0].node_key).not.toBe(first.nodes[0].node_key);
    queue.submit(retried.nodes);
    expect(queue.works()).toHaveLength(6);
  });

  it("grants three Claims from the dedicated pool and keeps claimed alerts from parallel attempts", () => {
    const queue = ledger();
    expect(queue.policy.pools[POOL]).toMatchObject({ logical_limit: 3, native_limit: 3, retry: { max_attempts: 3 } });
    expect(queue.policy.pools[POOL].profiles[WORKER]).toMatchObject({ workflow: `.github/workflows/${WORKER}.lock.yml`, max_claims: 1 });
    const first = plan([]);
    queue.submit(first.nodes);
    queue.dispatch(first.dispatch);
    expect(queue.state().claims.size).toBe(3);
    expect(queue.works().filter(work => work.state === "claimed")).toHaveLength(3);
    expect(graphs(plan(queue.works(), moved))).toEqual(["code-scanning-alert:40", "code-scanning-alert:3"]);
  });

  it("rejects malformed alerts, unverified repository identity, and an empty schedule", () => {
    expect(() => plan([], [{ ...alert(7, "high"), number: "7" }])).toThrow(/malformed/);
    expect(() => plan([], [])).toThrow(/No open code scanning alerts/);
    for (const id of ["0", "007", "github-actions[bot]"]) expect(() => buildCodeScanningPlan({ alerts, works: [], repository, repositoryId: id })).toThrow(/identity/);
    expect(() => buildCodeScanningPlan({ alerts, works: [], repository: "not-a-repo", repositoryId })).toThrow(/owner\/name/);
  });
});
