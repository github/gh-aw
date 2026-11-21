// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { applyCommit, buildCommit, newRequest, newState, replayTransactionLog } = require("./work_queue_replay.cjs");
const { main, maintenance } = require("./reconcile_work_queue_run.cjs");

const POLICY = {
  version: 4,
  pending_limit: 4096,
  issues: false,
  pools: { default: { concurrency: 16, per_account_limit: 0, max_attempts: 3, default_profile: "worker", profiles: { worker: { workflow: h.WORKER_WORKFLOW, effect_scope: "owner/repo" } } } },
};

function fakeCore() {
  return { info: vi.fn(), warning: vi.fn(), setOutput: vi.fn(), setFailed: vi.fn() };
}

const saved = {};
beforeEach(() => {
  for (const key of ["GITHUB_WORKFLOW_REF", "GH_AW_WORK_QUEUE_ROLE", "GH_AW_SAFE_OUTPUTS_STAGED"]) saved[key] = process.env[key];
  delete process.env.GH_AW_WORK_QUEUE_ROLE;
  delete process.env.GH_AW_SAFE_OUTPUTS_STAGED;
});
afterEach(() => {
  for (const [key, value] of Object.entries(saved)) {
    if (value === undefined) delete process.env[key];
    else process.env[key] = value;
  }
});

/** Ledger with one dispatched Work per key; returns the dispatch IDs in key order. */
function dispatched(keys) {
  const nodes = keys.map(key => h.work(key));
  const planned = h.dispatchNext(h.submit([], nodes));
  return { log: planned.log, nodes, dispatches: planned.decision.dispatches };
}

describe("worker termination", () => {
  const workerContext = dispatchId => ({
    repo: { owner: "owner", repo: "repo" },
    eventName: "workflow_dispatch",
    job: "conclusion",
    runId: 200,
    runAttempt: 1,
    payload: { inputs: { work_queue_assignment: JSON.stringify({ version: 4, dispatch_id: dispatchId }) } },
  });

  it("releases the run's own Dispatch while its Claim is open", async () => {
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/worker.lock.yml@refs/heads/main";
    const { log, nodes, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(h.bind(h.launch(log, dispatches[0]), dispatches[0], h.workerActor("200", 1))));
    const result = await main({ githubClient: fake.githubClient, context: workerContext(dispatches[0]), core: fakeCore(), policy: POLICY });

    expect(result).toEqual({ status: "released", dispatch_id: dispatches[0] });
    const state = replayTransactionLog(fake.contents());
    expect(state.dispatches.get(dispatches[0]).state).toBe("closed");
    expect(state.works.get(nodes[0].work_id)).toMatchObject({ state: "available", attempts: 1 });
    expect(state.transactions.at(-1)).toMatchObject({ actor: { role: "reconciler" }, request: { kind: "release", parameters: { reason: "worker_terminated" } } });
  });

  it("leaves a Dispatch bound to another run untouched", async () => {
    const { log, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(h.bind(h.launch(log, dispatches[0]), dispatches[0], h.workerActor("999", 1))));
    const before = fake.contents();
    expect(await main({ githubClient: fake.githubClient, context: workerContext(dispatches[0]), core: fakeCore(), policy: POLICY })).toEqual({ status: "settled", dispatch_id: dispatches[0] });
    expect(fake.contents()).toBe(before);
  });
});

describe("maintenance", () => {
  const context = { repo: { owner: "owner", repo: "repo" }, runId: 300, runAttempt: 1, payload: {} };

  it("releases a bound Dispatch whose run completed and an unbound one with no active run", async () => {
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/agentics-maintenance.yml@refs/heads/main";
    const { log, nodes, dispatches } = dispatched(["bound", "lost", "running"]);
    let ledger = h.launch(log, dispatches[0]);
    ledger = h.bind(ledger, dispatches[0], h.workerActor("7", 1));
    ledger = h.launch(ledger, dispatches[1]);
    ledger = h.launch(ledger, dispatches[2]);
    const fake = h.fakeQueueGitHub(h.ledgerText(ledger));
    const listWorkflowRuns = vi.fn(async ({ status, page }) => ({
      data: { workflow_runs: status === "in_progress" && page === 1 ? [{ display_title: `Worker ${dispatches[2]}` }, { display_title: "unrelated" }] : [] },
    }));
    fake.githubClient.rest.actions = { getWorkflowRun: vi.fn(async ({ run_id }) => ({ data: { id: run_id, status: "completed", conclusion: "failure" } })), listWorkflowRuns };

    const result = await maintenance({ githubClient: fake.githubClient, context, core: fakeCore(), policy: POLICY });

    expect(result.released).toEqual([
      { dispatch_id: dispatches[0], reason: "run_terminal" },
      { dispatch_id: dispatches[1], reason: "run_missing" },
    ]);
    expect(result.checkpointed).toBe(false);
    expect(fake.githubClient.rest.actions.getWorkflowRun).toHaveBeenCalledWith(expect.objectContaining({ run_id: 7 }));
    expect(new Set(listWorkflowRuns.mock.calls.map(([request]) => request.status))).toEqual(new Set(["queued", "in_progress", "waiting", "pending", "requested"]));
    expect(listWorkflowRuns.mock.calls.every(([request]) => request.workflow_id === "worker.lock.yml" && request.event === "workflow_dispatch")).toBe(true);
    const state = replayTransactionLog(fake.contents());
    expect(nodes.map(node => state.works.get(node.work_id).state)).toEqual(["available", "available", "claimed"]);
    expect(state.dispatches.get(dispatches[2]).state).toBe("started");
  });

  it("keeps a Dispatch open when its run cannot be read", async () => {
    const { log, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(h.bind(h.launch(log, dispatches[0]), dispatches[0], h.workerActor("7", 1))));
    fake.githubClient.rest.actions = {
      getWorkflowRun: async () => {
        throw Object.assign(new Error("Server Error"), { status: 502 });
      },
    };
    const core = fakeCore();
    expect((await maintenance({ githubClient: fake.githubClient, context, core, policy: POLICY })).released).toEqual([]);
    expect(core.warning).toHaveBeenCalled();
  });

  it("checkpoints a ledger longer than 512 lines into one Checkpoint line", async () => {
    const state = newState();
    const log = [];
    for (let index = 0; index < 513; index++) {
      const node = h.work(`n${index}`);
      const commit = buildCommit(state, h.producer, newRequest("submit", h.producer, { work_ids: [node.work_id] }), [node], 1);
      applyCommit(state, commit);
      log.push(commit);
    }
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    fake.githubClient.rest.actions = {};

    const result = await maintenance({ githubClient: fake.githubClient, context, core: fakeCore(), policy: POLICY });

    expect(result.checkpointed).toBe(true);
    const lines = fake.contents().trimEnd().split("\n");
    expect(lines).toHaveLength(1);
    expect(JSON.parse(lines[0])).toMatchObject({ version: 4, previous: null, request: { kind: "checkpoint" }, operations: [{ kind: "Checkpoint" }] });
    expect(replayTransactionLog(fake.contents()).works.size).toBe(513);
  });
});
