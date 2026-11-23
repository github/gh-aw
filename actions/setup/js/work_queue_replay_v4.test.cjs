import { describe, expect, it } from "vitest";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");
const { publishWorkQueueRequest } = require("./work_queue_store.cjs");
const { releaseOperations } = require("./work_queue_binding.cjs");

const policyWithMaxAttempts = maxAttempts => ({
  version: 4,
  pending_limit: 30,
  issues: false,
  pools: { default: { concurrency: 1, per_account_limit: 0, max_attempts: maxAttempts, default_profile: "worker", profiles: { worker: { workflow: h.WORKER_WORKFLOW, effect_scope: "owner/repo" } } } },
});

describe("v4 lifecycle without clocks", () => {
  const a = h.work("a");
  const b = h.work("b");

  function completedA() {
    let log = h.submit([], [a, b]);
    const planned = h.dispatchNext(log, { concurrency: 1 });
    expect(planned.decision.operations.map(operation => operation.work_id)).toEqual([a.work_id]);
    log = h.launch(planned.log, planned.decision.dispatches[0], true, h.dispatcher, "7");
    log = h.bind(log, planned.decision.dispatches[0], h.workerActor("7", 1));
    return { log: h.finish(log, planned.decision.dispatches[0], h.workerActor("7", 1)), dispatch: planned.decision.dispatches[0] };
  }

  it("completes the bound Claim, frees capacity, then releases and cancels on exhausted attempts", async () => {
    const first = completedA();
    let state = replayTransactions(first.log);
    expect(state.works.get(a.work_id).state).toBe("completed");
    expect(state.works.get(b.work_id).state).toBe("available");
    expect(state.dispatches.get(first.dispatch).state).toBe("closed");

    const second = h.dispatchNext(first.log, { concurrency: 1 }, { ...h.dispatcher, run_id: "101" });
    expect(second.decision.operations.map(operation => operation.work_id)).toEqual([b.work_id]);
    const dispatchB = second.decision.dispatches[0];
    state = replayTransactions(h.release(second.log, dispatchB, "run_missing"));
    expect(state.works.get(b.work_id)).toMatchObject({ state: "available", attempts: 1 });

    // With max_attempts 1 the publisher appends the attempts rule to the same release commit.
    const fake = h.fakeQueueGitHub(h.ledgerText(second.log));
    const policy = policyWithMaxAttempts(1);
    const result = await publishWorkQueueRequest({
      githubClient: fake.githubClient,
      owner: "owner",
      repo: "repo",
      actor: h.reconciler,
      request: { kind: "release", parameters: { dispatch_id: dispatchB, reason: "run_missing" } },
      operationsFor: current => releaseOperations(current, dispatchB, "run_missing", policy),
    });
    expect(result.commit.operations.map(operation => [operation.kind, operation.reason])).toEqual([
      ["Release", "run_missing"],
      ["WorkCancellation", "attempts_exhausted"],
    ]);
    expect(result.state.works.get(b.work_id)).toMatchObject({ state: "cancelled", attempts: 1, cancellation_reason: "attempts_exhausted" });
  });

  it("rejects a Completion from another run and a Bound from another workflow", () => {
    let log = h.submit([], [a, b]);
    const planned = h.dispatchNext(log, { concurrency: 1 });
    const dispatch = planned.decision.dispatches[0];
    log = h.launch(planned.log, dispatch, true, h.dispatcher, "7");
    expect(() => h.bind(log, dispatch, h.workerActor("7", 1, ".github/workflows/other.lock.yml"))).toThrow(expect.objectContaining({ code: "run_binding_conflict" }));
    log = h.bind(log, dispatch, h.workerActor("7", 1));
    expect(() => h.finish(log, dispatch, h.workerActor("8", 1))).toThrow(expect.objectContaining({ code: "claim_scope_invalid" }));
  });

  it("replays identically whatever informational commit times say", () => {
    const { log } = completedA();
    const shifted = log.map((commit, index) => ({ ...commit, at: index % 2 ? Number.MAX_SAFE_INTEGER : 0 }));
    const original = replayTransactions(log);
    const replayed = replayTransactions(shifted);
    expect([...replayed.works.values()].map(work => [work.work_id, work.state, work.attempts])).toEqual([...original.works.values()].map(work => [work.work_id, work.state, work.attempts]));
  });

  it("keeps work-queue tests free of fake clocks", () => {
    const directory = path.dirname(new URL(import.meta.url).pathname);
    const offenders = fs
      .readdirSync(directory)
      .filter(file => /(work_queue|finish_work_queue|reconcile_work_queue|write_work_queue|cancel_work_queue).*\.(test\.cjs|_checks\.cjs)$/.test(file) && file !== path.basename(new URL(import.meta.url).pathname))
      .filter(file => /useFakeTimers|setSystemTime|advanceTimersByTime/.test(fs.readFileSync(path.join(directory, file), "utf8")));
    expect(offenders).toEqual([]);
  });
});

describe("finish-carried continuations", () => {
  const { MAX_PENDING_WORK, OPERATIONS_PER_COMMIT } = require("./work_queue_limits.cjs");
  const { continuationChild } = require("./work_queue_graph.cjs");
  const { appendCommit, buildCommit, newRequest } = require("./work_queue_replay.cjs");
  const fails = (callback, code) => expect(callback).toThrow(expect.objectContaining({ code }));

  function boundParent(log, parent) {
    const planned = h.dispatchNext(log, { max_claims: 1, max_dispatches: 1 });
    const dispatchId = planned.decision.dispatches[0];
    expect(replayTransactions(planned.log).claims.get(replayTransactions(planned.log).dispatches.get(dispatchId).claims[0]).work_id).toBe(parent.work_id);
    return { log: h.bind(h.launch(planned.log, dispatchId, true, h.dispatcher, "7"), dispatchId, h.workerActor("7")), dispatchId };
  }

  function finishCommit(log, dispatchId, extra) {
    const state = replayTransactions(log);
    const claim = state.claims.get(state.dispatches.get(dispatchId).claims[0]);
    const completion = { kind: "Completion", work_id: claim.work_id, claim_id: claim.claim_id, dispatch_id: dispatchId, claim_handle: claim.handle, effects: [] };
    const actor = h.workerActor("7");
    return buildCommit(state, actor, newRequest("finish", actor, { dispatch_id: dispatchId }), [completion, ...extra], 1);
  }

  it("lets a pool at its pending ceiling replace the completed parent with its successor", () => {
    const parent = h.work("p", { priority: 1 });
    const log = h.submit([], [parent]);
    let state = replayTransactions(log);
    const fillers = Array.from({ length: MAX_PENDING_WORK - 1 }, (_, index) => h.work(`filler-${index}`, { graph_id: "fillers", priority: 5 }));
    const submit = works => buildCommit(state, h.producer, newRequest("submit", h.producer, { work_ids: works.map(work => work.work_id) }), works, 1);
    for (let start = 0; start < fillers.length; start += OPERATIONS_PER_COMMIT) {
      const next = submit(fillers.slice(start, start + OPERATIONS_PER_COMMIT));
      state = appendCommit(state, next);
      log.push(next);
    }
    fails(() => appendCommit(state, submit([h.work("overflow")])), "resource_limit");
    const bound = boundParent(log, parent);
    const child = continuationChild(parent, { route: "workqueue", message: "next", remaining: 0 });
    const commit = finishCommit(bound.log, bound.dispatchId, [child]);
    const finished = appendCommit(replayTransactions(bound.log), commit);
    expect(finished.works.get(child.work_id).state).toBe("available");
    // A lost acknowledgement replays the same request, never a second child.
    fails(() => replayTransactions([...bound.log, commit, commit]), "ledger_invalid");
    fails(() => appendCommit(finished, buildCommit(finished, commit.actor, commit.request, commit.operations, 1)), "request_reused");
  });

  it("keeps ordinary finish commits unchanged", () => {
    const parent = h.work("p");
    const bound = boundParent(h.submit([], [parent]), parent);
    const state = appendCommit(replayTransactions(bound.log), finishCommit(bound.log, bound.dispatchId, []));
    expect(state.works.get(parent.work_id).state).toBe("completed");
    expect(state.works.size).toBe(1);
  });
});
