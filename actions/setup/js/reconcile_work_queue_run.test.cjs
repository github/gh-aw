// @ts-check
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { applyCommit, buildCommit, checkpointCommit, newRequest, newState, replayTransactionLog, replayTransactions } = require("./work_queue_replay.cjs");
const { UNBOUND_GRACE_MS, main, maintenance } = require("./reconcile_work_queue_run.cjs");

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
  // Ledger commits built by the helpers are stamped at 1 ms.
  const RESERVED_AT = 1;
  beforeEach(() => {
    process.env.GITHUB_WORKFLOW_REF = "owner/repo/.github/workflows/agentics-maintenance.yml@refs/heads/main";
  });

  const unrelated = count => Array.from({ length: count }, (_, index) => ({ id: 1000 + index, display_title: "gh-aw work-queue unrelated", status: "in_progress" }));
  /** listWorkflowRuns over fixed pages of a created-window listing. */
  const listing = (pages, total = pages.flat().length) => vi.fn(async ({ page }) => ({ data: { total_count: total, workflow_runs: pages[page - 1] ?? [] } }));
  const run = (dispatchId, status) => ({ id: 77, display_title: `gh-aw work-queue ${dispatchId}`, status });

  async function runMaintenance(fake, actions, now = RESERVED_AT + UNBOUND_GRACE_MS) {
    fake.githubClient.rest.actions = actions;
    const core = fakeCore();
    const result = await maintenance({ githubClient: fake.githubClient, context, core, policy: POLICY, now: () => now });
    return { result, core, state: replayTransactionLog(fake.contents()) };
  }

  it("judges a launched or bound Dispatch by its exact run, without listing runs", async () => {
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
    const listWorkflowRuns = vi.fn();

    const { result, state } = await runMaintenance(fake, { getWorkflowRun, listWorkflowRuns });

    expect(result.released).toEqual([
      { dispatch_id: dispatches[0], reason: "run_terminal" },
      { dispatch_id: dispatches[1], reason: "run_missing" },
      { dispatch_id: dispatches[2], reason: "run_terminal" },
    ]);
    expect(result.checkpointed).toBe(false);
    expect(getWorkflowRun.mock.calls.map(([request]) => request.run_id)).toEqual([7, 8, 9, 10]);
    expect(listWorkflowRuns).not.toHaveBeenCalled();
    expect(nodes.map(node => state.works.get(node.work_id).state)).toEqual(["available", "available", "available", "claimed"]);
    expect(state.dispatches.get(dispatches[3]).state).toBe("started");
  });

  it("keeps a reservation open while its launch may still be in flight", async () => {
    const { log, nodes, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    const before = fake.contents();
    const listWorkflowRuns = listing([[]]);

    const { result, state } = await runMaintenance(fake, { listWorkflowRuns }, RESERVED_AT + UNBOUND_GRACE_MS - 1);

    expect(result.released).toEqual([]);
    expect(listWorkflowRuns).not.toHaveBeenCalled();
    expect(fake.contents()).toBe(before);
    expect(state.works.get(nodes[0].work_id)).toMatchObject({ state: "claimed", attempts: 0 });
    expect(state.dispatches.get(dispatches[0]).state).toBe("reserved");
  });

  it("finds a stale reservation's run beyond the second page of the listing", async () => {
    const { log, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    const before = fake.contents();
    const listWorkflowRuns = listing([unrelated(100), unrelated(100), [run(dispatches[0], "queued")]]);

    const { result } = await runMaintenance(fake, { listWorkflowRuns });

    expect(result.released).toEqual([]);
    expect(listWorkflowRuns).toHaveBeenCalledTimes(3);
    expect(fake.contents()).toBe(before);
  });

  it("keeps a stale reservation open when the run listing is incomplete", async () => {
    const { log, dispatches } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    const before = fake.contents();
    const listWorkflowRuns = listing(
      Array.from({ length: 10 }, () => unrelated(100)),
      1500
    );

    const { result, core, state } = await runMaintenance(fake, { listWorkflowRuns });

    expect(result.released).toEqual([]);
    expect(fake.contents()).toBe(before);
    expect(state.dispatches.get(dispatches[0]).state).toBe("reserved");
    expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("could not list every worker run"));
  });

  it("keeps a stale reservation open when 1,000 listed runs leave the listing unfinished", async () => {
    const { log } = dispatched(["a"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    const before = fake.contents();
    // No total_count: only the full tenth page shows that the listing goes on.
    const listWorkflowRuns = listing(
      Array.from({ length: 11 }, () => unrelated(100)),
      null
    );

    const { result, core } = await runMaintenance(fake, { listWorkflowRuns });

    expect(result.released).toEqual([]);
    expect(listWorkflowRuns).toHaveBeenCalledTimes(10);
    expect(fake.contents()).toBe(before);
    expect(core.warning).toHaveBeenCalledWith(expect.stringContaining("could not list every worker run"));
  });

  it("releases a stale reservation on a complete listing of runs created since it", async () => {
    const { log, nodes, dispatches } = dispatched(["missing", "finished"]);
    const fake = h.fakeQueueGitHub(h.ledgerText(log));
    const listWorkflowRuns = listing([[...unrelated(3), run(dispatches[1], "completed")]]);

    const { result, state } = await runMaintenance(fake, { listWorkflowRuns });

    expect(result.released).toEqual([
      { dispatch_id: dispatches[0], reason: "run_missing" },
      { dispatch_id: dispatches[1], reason: "run_terminal" },
    ]);
    for (const [request] of listWorkflowRuns.mock.calls) {
      expect(request).toMatchObject({ workflow_id: "worker.lock.yml", event: "workflow_dispatch", created: ">=1970-01-01T00:00:00Z", per_page: 100, page: 1 });
      expect(request).not.toHaveProperty("status");
    }
    expect(nodes.map(node => state.works.get(node.work_id))).toEqual([expect.objectContaining({ state: "available", attempts: 1 }), expect.objectContaining({ state: "available", attempts: 1 })]);
  });

  it("measures a checkpointed reservation from the Checkpoint and lists the run lifetime", async () => {
    const { log, dispatches } = dispatched(["a"]);
    const checkpointAt = Date.UTC(2026, 10, 1);
    const fake = h.fakeQueueGitHub(h.ledgerText([checkpointCommit(replayTransactions(log), h.reconciler, checkpointAt)]));
    const before = fake.contents();
    const young = listing([[]]);
    expect((await runMaintenance(fake, { listWorkflowRuns: young }, checkpointAt + UNBOUND_GRACE_MS - 1)).result.released).toEqual([]);
    expect(young).not.toHaveBeenCalled();
    expect(fake.contents()).toBe(before);

    const now = checkpointAt + UNBOUND_GRACE_MS;
    const stale = listing([[]]);
    expect((await runMaintenance(fake, { listWorkflowRuns: stale }, now)).result.released).toEqual([{ dispatch_id: dispatches[0], reason: "run_missing" }]);
    expect(stale).toHaveBeenCalledWith(expect.objectContaining({ created: `>=${new Date(now - 35 * 24 * 60 * 60 * 1000).toISOString().replace(/\.\d{3}Z$/, "Z")}` }));
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
