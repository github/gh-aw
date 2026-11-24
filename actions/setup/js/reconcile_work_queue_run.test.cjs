// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { applyCommit, buildCommit, checkpointCommit, newRequest, newState, replayTransactionLog, replayTransactions } = require("./work_queue_replay.cjs");
const { main, maintenance } = require("./reconcile_work_queue_run.cjs");

const POLICY = {
  version: 4,
  pending_limit: 4096,
  issues: false,
  effect_scope: "owner/repo",
  workers: ["worker"],
  pools: { default: { concurrency: 16, per_account_limit: 0, max_attempts: 3 } },
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
    const fake = h.fakeQueueGitHub(h.ledgerText(h.bind(h.launch(log, dispatches[0], true, h.dispatcher, "999"), dispatches[0], h.workerActor("999", 1))));
    const before = fake.contents();
    expect(await main({ githubClient: fake.githubClient, context: workerContext(dispatches[0]), core: fakeCore(), policy: POLICY })).toEqual({ status: "settled", dispatch_id: dispatches[0] });
    expect(fake.contents()).toBe(before);
  });

  it("leaves a Dispatch launched as another run untouched", async () => {
    const { log, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(h.launch(log, dispatches[0], true, h.dispatcher, "999")));
    const before = fake.contents();
    expect(await main({ githubClient: fake.githubClient, context: workerContext(dispatches[0]), core: fakeCore(), policy: POLICY })).toEqual({ status: "settled", dispatch_id: dispatches[0] });
    expect(fake.contents()).toBe(before);
  });

  it("leaves a reserved Dispatch untouched: no run was launched for it yet", async () => {
    const { log, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    const before = fake.contents();
    expect(await main({ githubClient: fake.githubClient, context: workerContext(dispatches[0]), core: fakeCore(), policy: POLICY })).toEqual({ status: "settled", dispatch_id: dispatches[0] });
    expect(fake.contents()).toBe(before);
  });

  it("releases a Dispatch launched as this run before it bound", async () => {
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/worker.lock.yml@refs/heads/main";
    const { log, nodes, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(h.launch(log, dispatches[0])));
    expect(await main({ githubClient: fake.githubClient, context: workerContext(dispatches[0]), core: fakeCore(), policy: POLICY })).toEqual({ status: "released", dispatch_id: dispatches[0] });
    expect(replayTransactionLog(fake.contents()).works.get(nodes[0].work_id)).toMatchObject({ state: "available", attempts: 1 });
  });
});

describe("maintenance", () => {
  const context = { repo: { owner: "owner", repo: "repo" }, runId: 300, runAttempt: 1, payload: {} };
  beforeEach(() => {
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/agentics-maintenance.yml@refs/heads/main";
  });

  /** getWorkflowRunAttempt over fixed `run/attempt` statuses; any other attempt answers 404. */
  const attempts = statuses =>
    vi.fn(async ({ run_id, attempt_number }) => {
      const status = statuses[`${run_id}/${attempt_number}`];
      if (!status) throw Object.assign(new Error("Not Found"), { status: 404 });
      return { data: { id: run_id, run_attempt: attempt_number, status } };
    });

  async function runMaintenance(fake, actions) {
    fake.githubClient.rest.actions = actions;
    const core = fakeCore();
    const result = await maintenance({ githubClient: fake.githubClient, context, core, policy: POLICY });
    return { result, core, state: replayTransactionLog(fake.contents()) };
  }

  /** One reservation per dispatcher attempt of run 100, in attempt order. */
  function reservedByAttempts(count) {
    const nodes = Array.from({ length: count }, (_, index) => h.work(`n${index}`));
    let log = h.submit([], nodes);
    const dispatches = [];
    for (let attempt = 1; attempt <= count; attempt++) {
      const step = h.dispatchNext(log, { max_claims: 1, max_dispatches: 1 }, { ...h.dispatcher, run_attempt: attempt });
      log = step.log;
      dispatches.push(...step.decision.dispatches);
    }
    return { log, nodes, dispatches };
  }

  it("judges a launched or bound Dispatch by its exact run, never by the dispatcher attempt", async () => {
    const { log, nodes, dispatches } = dispatched(["bound", "lost", "done", "running"]);
    let ledger = h.bind(h.launch(log, dispatches[0], true, h.dispatcher, "7"), dispatches[0], h.workerActor("7", 1));
    ledger = h.launch(ledger, dispatches[1], true, h.dispatcher, "8");
    ledger = h.launch(ledger, dispatches[2], true, h.dispatcher, "9");
    ledger = h.launch(ledger, dispatches[3], true, h.dispatcher, "10");
    const fake = h.fakeQueueGitHub(h.ledgerText(ledger));
    const runs = { 7: "completed", 9: "completed", 10: "in_progress" };
    const getWorkflowRun = vi.fn(async ({ run_id }) => {
      if (!runs[run_id]) throw Object.assign(new Error("Not Found"), { status: 404 });
      return { data: { id: run_id, status: runs[run_id] } };
    });
    const getWorkflowRunAttempt = vi.fn();

    const { result, state } = await runMaintenance(fake, { getWorkflowRun, getWorkflowRunAttempt });

    expect(result.released).toEqual([
      { dispatch_id: dispatches[0], reason: "run_terminal" },
      { dispatch_id: dispatches[1], reason: "run_missing" },
      { dispatch_id: dispatches[2], reason: "run_terminal" },
    ]);
    expect(result.checkpointed).toBe(false);
    expect(getWorkflowRun.mock.calls.map(([request]) => request.run_id)).toEqual([7, 8, 9, 10]);
    expect(getWorkflowRunAttempt).not.toHaveBeenCalled();
    expect(nodes.map(node => state.works.get(node.work_id).state)).toEqual(["available", "available", "available", "claimed"]);
    expect(state.dispatches.get(dispatches[3]).state).toBe("started");
  });

  it("keeps a reservation open while the dispatcher attempt that reserved it still runs", async () => {
    const { log, nodes, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    const before = fake.contents();
    const getWorkflowRunAttempt = attempts({ "100/1": "in_progress" });
    const getWorkflowRun = vi.fn();

    const { result, state } = await runMaintenance(fake, { getWorkflowRun, getWorkflowRunAttempt });

    expect(result.released).toEqual([]);
    expect(getWorkflowRunAttempt).toHaveBeenCalledTimes(1);
    expect(getWorkflowRunAttempt).toHaveBeenCalledWith(expect.objectContaining({ owner: "owner", repo: "repo", run_id: 100, attempt_number: 1 }));
    expect(getWorkflowRun).not.toHaveBeenCalled();
    expect(fake.contents()).toBe(before);
    expect(state.works.get(nodes[0].work_id)).toMatchObject({ state: "claimed", attempts: 0 });
    expect(state.dispatches.get(dispatches[0])).toMatchObject({ state: "reserved", reserved_by: { run_id: "100", run_attempt: 1 } });
  });

  it("releases a reservation once the dispatcher attempt that reserved it completed or is gone", async () => {
    const { log, nodes, dispatches } = reservedByAttempts(3);
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    // Attempt 1 completed (its launch record never landed), attempt 2 is gone, attempt 3 still runs.
    const getWorkflowRunAttempt = attempts({ "100/1": "completed", "100/3": "queued" });

    const { result, state } = await runMaintenance(fake, { getWorkflowRunAttempt });

    expect(getWorkflowRunAttempt.mock.calls.map(([request]) => [request.run_id, request.attempt_number])).toEqual([
      [100, 1],
      [100, 2],
      [100, 3],
    ]);
    expect(result.released).toEqual([
      { dispatch_id: dispatches[0], reason: "run_terminal" },
      { dispatch_id: dispatches[1], reason: "run_missing" },
    ]);
    expect(nodes.map(node => state.works.get(node.work_id))).toEqual([
      expect.objectContaining({ state: "available", attempts: 1 }),
      expect.objectContaining({ state: "available", attempts: 1 }),
      expect.objectContaining({ state: "claimed", attempts: 0 }),
    ]);
    expect(state.dispatches.get(dispatches[2]).state).toBe("reserved");
  });

  it("judges a checkpointed reservation by the dispatcher attempt its Checkpoint records", async () => {
    const { log, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText([checkpointCommit(replayTransactions(log), h.reconciler, 1)]));
    const before = fake.contents();
    expect(JSON.parse(before).operations[0].state.dispatches[dispatches[0]].reserved_by).toEqual({ run_id: "100", run_attempt: 1 });

    expect((await runMaintenance(fake, { getWorkflowRunAttempt: attempts({ "100/1": "in_progress" }) })).result.released).toEqual([]);
    expect(fake.contents()).toBe(before);
    expect((await runMaintenance(fake, { getWorkflowRunAttempt: attempts({ "100/1": "completed" }) })).result.released).toEqual([{ dispatch_id: dispatches[0], reason: "run_terminal" }]);
  });

  it("does not release a reservation whose launch lands between the attempt lookup and the release commit", async () => {
    const { log, nodes, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    // The reserving attempt records its launch just before it completes.
    const launched = h.ledgerText(h.launch(log, dispatches[0]));
    fake.state.beforeUpdate = () => fake.install(launched);

    const { result, state } = await runMaintenance(fake, { getWorkflowRunAttempt: attempts({ "100/1": "completed" }) });

    expect(fake.state.updates).toBe(1);
    expect(result.released).toEqual([]);
    expect(fake.contents()).toBe(launched);
    expect(state.dispatches.get(dispatches[0]).state).toBe("started");
    expect(state.works.get(nodes[0].work_id)).toMatchObject({ state: "claimed", attempts: 0 });
  });

  it("does not release a Dispatch whose worker binds between the run lookup and the release commit", async () => {
    const { log, nodes, dispatches } = dispatched(["a"]);
    const launched = h.launch(log, dispatches[0]);
    const fake = h.fakeQueueGitHub(h.ledgerText(launched));
    // Attempt 1 of run 200 ended before binding; a re-run (attempt 2) binds while
    // maintenance publishes its release, so the first compare-and-swap loses.
    const rebound = h.ledgerText(h.bind(launched, dispatches[0], h.workerActor("200", 2)));
    fake.state.beforeUpdate = () => fake.install(rebound);
    const getWorkflowRun = vi.fn(async ({ run_id }) => ({ data: { id: run_id, status: "completed" } }));

    const { result, state } = await runMaintenance(fake, { getWorkflowRun });

    expect(getWorkflowRun).toHaveBeenCalledTimes(1);
    expect(fake.state.updates).toBe(1);
    expect(result.released).toEqual([]);
    expect(fake.contents()).toBe(rebound);
    expect(state.transactions.some(commit => commit.request.kind === "release")).toBe(false);
    expect(state.dispatches.get(dispatches[0])).toMatchObject({ state: "bound", run: { run_id: "200", run_attempt: 2 } });
    expect(state.claims.get(state.dispatches.get(dispatches[0]).claims[0]).state).toBe("open");
    expect(state.works.get(nodes[0].work_id)).toMatchObject({ state: "claimed", attempts: 0 });
  });

  it("keeps a Dispatch open when its run cannot be read", async () => {
    const { log, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(h.bind(h.launch(log, dispatches[0], true, h.dispatcher, "7"), dispatches[0], h.workerActor("7", 1))));
    const getWorkflowRun = async () => {
      throw Object.assign(new Error("Server Error"), { status: 502 });
    };
    const { result, core } = await runMaintenance(fake, { getWorkflowRun });
    expect(result.released).toEqual([]);
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
