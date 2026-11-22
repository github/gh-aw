import { describe, expect, it, vi } from "vitest";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const h = require("./work_queue_test_helpers.cjs");
const { canonical } = require("./work_queue_codec.cjs");
const { LEDGER_BYTES } = require("./work_queue_limits.cjs");
const {
  applyCommit,
  appendCommit,
  buildCommit,
  checkpointCommit,
  newRequest,
  parseTransactionLog,
  parseTransactionRecords,
  replayTransactionLog,
  replayTransactions,
  requestId,
  restoreCheckpointState,
  serializeProjection,
  serializeTransactionLog,
  validateCommit,
} = require("./work_queue_replay.cjs");

const fails = (callback, code) => expect(callback).toThrow(expect.objectContaining({ code }));
const a = h.work("a");
const b = h.work("b");

/** Submit a and b, claim a with concurrency 1, start and bind it to run 7. */
function bound() {
  const planned = h.dispatchNext(h.submit([], [a, b]), { concurrency: 1 });
  const dispatch = planned.decision.dispatches[0];
  return { log: h.bind(h.launch(planned.log, dispatch), dispatch, h.workerActor("7")), dispatch };
}

function raw(log, actor, kind, parameters, operations) {
  return [...log, buildCommit(replayTransactions(log), actor, newRequest(kind, actor, parameters), operations, 1)];
}

describe("shared JSONL work-queue framing", () => {
  it("preserves strict decoding across LF and CRLF records", () => {
    expect(parseTransactionRecords('{"id":1}\r\n{"id":2}\n')).toEqual([{ id: 1 }, { id: 2 }]);
  });

  it("bounds the whole ledger before JSON decoding, counting UTF-8 bytes", () => {
    for (const text of ["x".repeat(LEDGER_BYTES), "é".repeat(LEDGER_BYTES / 2)]) {
      const parse = vi.spyOn(JSON, "parse");
      try {
        fails(() => parseTransactionRecords(`"${text}"\n`), "resource_limit");
        expect(parse).not.toHaveBeenCalled();
      } finally {
        parse.mockRestore();
      }
    }
  });

  it.each([
    ['{"id":1,"id":2}\n', "duplicate_key"],
    ['{"id":1,"\\u0069d":2}\n', "duplicate_key"],
    ['{"id":1.0}\n', "noncanonical_number"],
    ['{"id":1e0}\n', "noncanonical_number"],
    ['{"id":9007199254740992}\n', "noncanonical_number"],
    ['{"id":"\\ud800"}\n', "invalid_unicode"],
    ['\ufeff{"id":1}\n', "codec_invalid"],
    ['\u00a0{"id":1}\n', "codec_invalid"],
    ['{"id":}\n', "codec_invalid"],
    ["{}\n\n", "ledger_invalid"],
    ["{}\n \r\n", "ledger_invalid"],
    ["{}", "ledger_invalid"],
  ])("retains strict rejection of %j", (input, code) => {
    fails(() => parseTransactionRecords(input), code);
  });
});

describe("v4 commit envelope", () => {
  it("binds request and commit identities and rejects other protocol versions first", () => {
    const [genesis] = h.submit([], [a]);
    expect(genesis.request.id).toBe(requestId(h.producer, "submit", { work_ids: [a.work_id] }));
    expect(validateCommit(genesis)).toBe(genesis);
    fails(() => validateCommit({ ...genesis, version: 3, policy_epoch: "x" }), "unsupported_protocol");
    fails(() => validateCommit({ ...genesis, id: "q_forged" }), "ledger_invalid");
    fails(() => validateCommit({ ...genesis, request: { ...genesis.request, id: "request:forged" } }), "request_invalid");
    fails(() => validateCommit({ ...genesis, extra: true }), "ledger_invalid");
    fails(() => validateCommit({ ...genesis, operations: [] }), "request_invalid");
    fails(() => validateCommit({ ...genesis, operations: [{ ...a, enqueued: 1 }] }), "request_invalid");
  });

  it("replays strictly in file order from one genesis", () => {
    const log = bound().log;
    expect(replayTransactions(log).tip).toBe(log.at(-1).id);
    fails(() => replayTransactions([log[1], log[0]]), "ledger_invalid");
    fails(() => replayTransactions([...log.slice(0, 2), ...log.slice(3)]), "ledger_invalid");
  });

  it("rejects worker genesis, foreign repositories, reused requests and migrate after other kinds", () => {
    fails(() => h.commit([], h.workerActor("7"), "submit", { work_ids: [a.work_id] }, [a]), "actor_unauthorized");
    const log = h.submit([], [a]);
    fails(() => h.submit(log, [b], { ...h.producer, repository: "other/repo" }), "actor_unauthorized");
    fails(() => replayTransactions(raw(log, h.producer, "submit", { work_ids: [a.work_id] }, [a])), "request_reused");
    fails(() => h.commit(log, h.administrator, "migrate", { work_ids: [b.work_id] }, [b]), "request_invalid");
    fails(() => h.commit([], h.producer, "migrate", { work_ids: [b.work_id] }, [b]), "unauthorized_operation");
  });

  it("enforces the per-request operation and role tables", () => {
    const { log, dispatch } = bound();
    fails(() => h.commit(log, h.reconciler, "release", { dispatch_id: dispatch, reason: "operator" }, [{ kind: "Dispatch", dispatch_id: dispatch, state: "started" }]), "request_invalid");
    fails(() => h.commit(log, h.producer, "submit", { work_ids: [h.work("c").work_id] }, [{ kind: "Release", dispatch_id: dispatch, reason: "operator" }]), "request_invalid");
    fails(() => h.commit(log, h.dispatcher, "release", { dispatch_id: dispatch, reason: "operator" }, [{ kind: "Release", dispatch_id: dispatch, reason: "operator" }]), "unauthorized_operation");
    fails(() => h.commit(log, h.workerActor("7"), "release", { dispatch_id: dispatch, reason: "operator" }, [{ kind: "Release", dispatch_id: dispatch, reason: "operator" }]), "unauthorized_operation");
    fails(() => h.commit(log, { ...h.dispatcher, run_id: "105" }, "release", { dispatch_id: dispatch, reason: "launch_failed" }, [{ kind: "WorkCancellation", work_id: b.work_id, reason: "operator" }]), "unauthorized_operation");
  });
});

describe("v4 lifecycle transitions", () => {
  it("claims only available Work and groups one Claim per Dispatch", () => {
    const planned = h.dispatchNext(h.submit([], [a, b]), { concurrency: 2 });
    const state = replayTransactions(planned.log);
    expect(planned.decision.operations).toHaveLength(2);
    for (const operation of planned.decision.operations) {
      expect(state.works.get(operation.work_id)).toMatchObject({ state: "claimed", claim_id: operation.claim_id });
      expect(state.dispatches.get(operation.dispatch_id)).toMatchObject({ state: "reserved", run: null, claims: [operation.claim_id], workflow: h.WORKER_WORKFLOW, pool: "default", worker_profile: "worker" });
    }
    const claim = planned.decision.operations[0];
    const again = { ...claim, claim_id: "c_again", dispatch_id: "d_again" };
    fails(() => replayTransactions(raw(planned.log, { ...h.dispatcher, run_id: "101" }, "dispatch_next", h.dispatchParameters(), [again])), "ownership_conflict");
    fails(() => replayTransactions(raw(h.submit([], [a]), h.dispatcher, "dispatch_next", h.dispatchParameters({ profiles: { other: { workflow: ".github/workflows/other.lock.yml" } } }), [{ ...claim }])), "request_invalid");
  });

  it("starts only reserved Dispatches and binds the worker's own run, rebinding a later attempt", () => {
    const planned = h.dispatchNext(h.submit([], [a]), { concurrency: 1 });
    const dispatch = planned.decision.dispatches[0];
    // A worker may bind before the dispatcher records the launch.
    let log = h.bind(planned.log, dispatch, h.workerActor("7"));
    fails(() => h.launch(log, dispatch), "dispatch_state_conflict");
    log = h.bind(log, dispatch, h.workerActor("7", 2));
    expect(replayTransactions(log).dispatches.get(dispatch)).toMatchObject({ state: "bound", run: { run_id: "7", run_attempt: 2 } });
    fails(() => h.bind(log, dispatch, h.workerActor("7", 1, h.WORKER_WORKFLOW)), "request_reused");
    fails(() => h.bind(log, dispatch, h.workerActor("8")), "run_binding_conflict");
    fails(() => h.commit(log, h.workerActor("9"), "bind", { dispatch_id: dispatch }, [{ kind: "Bound", dispatch_id: dispatch, run_id: "7", run_attempt: 3 }]), "run_binding_conflict");
    // An older attempt cannot take the binding back.
    const older = h.workerActor("7", 1);
    fails(
      () =>
        h.commit(log, { ...older, workflow: h.WORKER_WORKFLOW }, "finish", { dispatch_id: dispatch }, [
          { kind: "ClaimCancellation", work_id: a.work_id, claim_id: replayTransactions(log).dispatches.get(dispatch).claims[0], reason: "worker_failed" },
        ]),
      "claim_scope_invalid"
    );
  });

  it("records Completion effects from the bound run and closes the Dispatch", () => {
    const { log, dispatch } = bound();
    const effects = [{ kind: "create_issue", resource: { repository: "owner/repo", number: 1 } }];
    const state = replayTransactions(h.finish(log, dispatch, h.workerActor("7"), {}, effects));
    const work = state.works.get(a.work_id);
    expect(work).toMatchObject({ state: "completed", effects, completion_id: state.tip, claim_id: state.dispatches.get(dispatch).claims[0] });
    expect(state.claims.get(work.claim_id)).toMatchObject({ state: "completed", effects, terminal_commit_id: state.tip });
    expect(state.dispatches.get(dispatch).state).toBe("closed");
    fails(() => h.finish(log, dispatch, h.workerActor("7"), {}, [{ blob: "x".repeat(4096) }]), "resource_limit");
  });

  it("returns failed Claims to the pool and cancels exhausted Work only through its cancelled Claim", () => {
    const { log, dispatch } = bound();
    const claimId = replayTransactions(log).dispatches.get(dispatch).claims[0];
    const failed = h.commit(log, h.workerActor("7"), "finish", { dispatch_id: dispatch }, [
      { kind: "ClaimCancellation", work_id: a.work_id, claim_id: claimId, reason: "worker_failed" },
      { kind: "WorkCancellation", work_id: a.work_id, reason: "attempts_exhausted", claim_id: claimId },
    ]);
    const state = replayTransactions(failed);
    expect(state.works.get(a.work_id)).toMatchObject({ state: "cancelled", attempts: 1, cancellation_reason: "attempts_exhausted", cancellation_commit_id: state.tip });
    expect(state.works.get(a.work_id)).not.toHaveProperty("claim_id");
    expect(state.claims.get(claimId)).toMatchObject({ state: "cancelled", cancellation_reason: "worker_failed" });
    expect(state.dispatches.get(dispatch).state).toBe("closed");
    fails(() => h.commit(log, h.workerActor("7"), "finish", { dispatch_id: dispatch }, [{ kind: "WorkCancellation", work_id: a.work_id, reason: "attempts_exhausted", claim_id: claimId }]), "ownership_conflict");
  });

  it("releases every open Claim of a Dispatch with the Release reason", () => {
    const { log, dispatch } = bound();
    const state = replayTransactions(h.release(log, dispatch, "worker_terminated"));
    const claim = state.claims.get(state.dispatches.get(dispatch).claims[0]);
    expect(claim).toMatchObject({ state: "cancelled", cancellation_reason: "worker_terminated" });
    expect(state.works.get(a.work_id)).toMatchObject({ state: "available", attempts: 1 });
    expect(state.dispatches.get(dispatch).state).toBe("closed");
    fails(() => h.release(h.release(log, dispatch, "worker_terminated"), dispatch, "operator", { ...h.reconciler, run_id: "301" }), "dispatch_state_conflict");
    const claimId = state.dispatches.get(dispatch).claims[0];
    fails(
      () =>
        h.commit(h.release(log, dispatch, "worker_terminated"), h.workerActor("7"), "finish", { dispatch_id: dispatch }, [
          { kind: "Completion", work_id: a.work_id, claim_id: claimId, dispatch_id: dispatch, claim_handle: "h1", effects: [] },
        ]),
      "claim_ineffective"
    );
  });

  it("cancels available Work idempotently and refuses completed or claimed Work", () => {
    const cancel = (log, actor = h.producer, reason = "operator") => h.commit(log, actor, "cancel_work", { work_ids: [b.work_id], reason }, [{ kind: "WorkCancellation", work_id: b.work_id, reason }]);
    const { log, dispatch } = bound();
    const cancelled = cancel(log);
    expect(replayTransactions(cancel(cancelled, h.administrator)).works.get(b.work_id).state).toBe("cancelled");
    fails(() => cancel(cancelled, h.administrator, "related_issue_closed"), "work_terminal");
    const completed = h.finish(log, dispatch, h.workerActor("7"));
    fails(() => h.commit(completed, h.producer, "cancel_work", { work_ids: [a.work_id], reason: "operator" }, [{ kind: "WorkCancellation", work_id: a.work_id, reason: "operator" }]), "work_terminal");
    fails(() => h.commit(log, h.producer, "cancel_work", { work_ids: [a.work_id], reason: "operator" }, [{ kind: "WorkCancellation", work_id: a.work_id, reason: "operator" }]), "ownership_conflict");
  });

  it("upserts Observations by gate and satisfies external edges only when ready", () => {
    const resource = { kind: "pull_request", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "5", number: "5" };
    const gated = h.work("gated", { depends_on: [{ kind: "pull_request", resource, condition: "merged" }] });
    const observe = (state, extra) => ({ kind: "Observation", observation_id: `o-${state}`, resource, condition: "merged", state, ...extra });
    const log = h.submit([], [gated]);
    expect(h.dispatchNext(log, {}, h.dispatcher, [observe("waiting")]).decision.operations).toEqual([]);
    const ready = h.dispatchNext(log, {}, h.dispatcher, [observe("ready", { merged: true, merge_commit: "abc" })]);
    expect(ready.decision.operations.map(operation => operation.work_id)).toEqual([gated.work_id]);
    expect([...replayTransactions(ready.log).observations.values()]).toHaveLength(1);
    fails(() => h.dispatchNext(log, {}, h.dispatcher, [observe("ready")]), "observation_invalid");
  });

  it("binds Issue links and comments immutably", () => {
    const issue = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "9", number: "9" };
    const link = (log, operations) => h.commit(log, h.projector, "issue_link", { operations }, operations);
    let log = link(h.submit([], [a, b]), [{ kind: "IssueLink", work_id: a.work_id, resource: issue }]);
    expect(replayTransactions(log).works.get(a.work_id).backing_issue).toEqual(issue);
    fails(() => link(log, [{ kind: "IssueLink", work_id: b.work_id, resource: issue }]), "issue_binding_conflict");
    fails(() => link(log, [{ kind: "IssueLink", work_id: a.work_id, resource: { ...issue, resource_id: "10", number: "10" } }]), "issue_binding_conflict");
    log = link(log, [{ kind: "IssueComment", work_id: a.work_id, comment_id: "IC_1" }]);
    fails(() => link(log, [{ kind: "IssueComment", work_id: a.work_id, comment_id: "IC_2" }]), "issue_binding_conflict");
    fails(() => h.commit(log, h.producer, "issue_link", { operations: [{ kind: "IssueComment", work_id: b.work_id, comment_id: "IC_3" }] }, [{ kind: "IssueComment", work_id: b.work_id, comment_id: "IC_3" }]), "unauthorized_operation");
  });

  it("treats resubmission of an existing Work identity as a no-op", () => {
    const log = h.submit([], [a]);
    const resubmitted = h.submit(log, [{ ...a, payload: { changed: true } }, b], { ...h.producer, run_id: "11" });
    const state = replayTransactions(resubmitted);
    expect(state.works.get(a.work_id).payload).toEqual(a.payload);
    expect(state.works.get(b.work_id).position).toEqual({ commit: 1, operation: 1 });
  });
});

describe("v4 checkpoints", () => {
  it("restores an equivalent projection and continues numbering positions", () => {
    const { log, dispatch } = bound();
    const state = replayTransactions(h.finish(log, dispatch, h.workerActor("7")));
    const checkpoint = checkpointCommit(state, h.reconciler, 5);
    expect(checkpoint).toMatchObject({ previous: null, request: { kind: "checkpoint", parameters: { prior_tip: state.tip } } });
    const restored = replayTransactions([checkpoint]);
    const before = serializeProjection(state);
    const after = serializeProjection(restored);
    for (const field of ["repository", "works", "claims", "dispatches", "observations", "work_creators"]) expect(canonical(after[field])).toBe(canonical(before[field]));
    expect(restored).toMatchObject({ tip: checkpoint.id, commits: state.commits + 1 });
    expect([...restored.requests]).toEqual(expect.arrayContaining([...state.requests, checkpoint.request.id]));
    const next = h.submit([checkpoint], [h.work("c")], { ...h.producer, run_id: "11" });
    expect(replayTransactions(next).works.get(h.work("c").work_id).position).toEqual({ commit: state.commits + 1, operation: 0 });
    fails(() => replayTransactions(next.slice(1)), "ledger_invalid");
    fails(() => replayTransactions([...log, checkpoint]), "checkpoint_invalid");
    expect(replayTransactionLog(h.ledgerText(next)).tip).toBe(next.at(-1).id);
  });

  it("rejects checkpoints whose projection is inconsistent", () => {
    const { log } = bound();
    const checkpoint = checkpointCommit(replayTransactions(log), h.reconciler, 5);
    const tamper = mutate => {
      const copy = structuredClone(checkpoint);
      mutate(copy.operations[0].state);
      const request = newRequest("checkpoint", h.reconciler, { prior_tip: copy.operations[0].prior_tip });
      return { ...copy, request };
    };
    fails(() => restoreCheckpointState(tamper(state => (Object.values(state.works)[0].state = "completed"))), "checkpoint_invalid");
    fails(() => restoreCheckpointState(tamper(state => (state.repository = "other/repo"))), "checkpoint_invalid");
    fails(() => restoreCheckpointState(tamper(state => delete Object.values(state.dispatches)[0].workflow)), "checkpoint_invalid");
    fails(() => restoreCheckpointState(tamper(state => (state.extra = true))), "checkpoint_invalid");
    fails(() => restoreCheckpointState({ ...checkpoint, actor: h.producer }), "request_invalid");
  });

  it("replays the same checkpoint objects repeatedly without changing the checkpoint", () => {
    const { log, dispatch } = bound();
    const source = replayTransactions(log);
    const claimId = source.dispatches.get(dispatch).claims[0];
    const checkpoint = checkpointCommit(source, h.reconciler, 5);
    const checkpointBytes = h.ledgerText([checkpoint]);
    expect(checkpoint.operations[0].state.claims[claimId].state).toBe("open");

    // Advancing the source projection in place must not reach into the checkpoint it produced.
    applyCommit(source, h.finish(log, dispatch, h.workerActor("7")).at(-1));
    expect(source.claims.get(claimId).state).toBe("completed");
    expect(h.ledgerText([checkpoint])).toBe(checkpointBytes);

    const finished = h.finish([checkpoint], dispatch, h.workerActor("7"));
    const text = h.ledgerText(finished);
    for (const state of [replayTransactions(finished), replayTransactions(finished)]) {
      expect(state.claims.get(claimId)).toMatchObject({ state: "completed", dispatch_id: dispatch });
      expect(state.dispatches.get(dispatch)).toMatchObject({ state: "closed", run: { run_id: "7", run_attempt: 1 } });
      expect(state.tip).toBe(finished.at(-1).id);
      expect(state.transactions[0]).toBe(checkpoint);
    }
    expect(serializeTransactionLog(finished)).toBe(text);
    expect(finished[0]).toBe(checkpoint);
    expect(h.ledgerText([checkpoint])).toBe(checkpointBytes);
    expect(checkpoint.operations[0].state.claims[claimId].state).toBe("open");
    expect(checkpoint.operations[0].state.dispatches[dispatch].state).toBe("bound");
  });

  it("appendCommit validates against a copy and leaves the source projection unchanged", () => {
    const { log, dispatch } = bound();
    const state = replayTransactions(log);
    const before = canonical(serializeProjection(state));
    const bad = buildCommit(
      state,
      h.workerActor("8"),
      newRequest("finish", h.workerActor("8"), { dispatch_id: dispatch }),
      [{ kind: "Completion", work_id: a.work_id, claim_id: state.dispatches.get(dispatch).claims[0], dispatch_id: dispatch, claim_handle: "h1", effects: [] }],
      1
    );
    fails(() => appendCommit(state, bad), "claim_scope_invalid");
    expect(canonical(serializeProjection(state))).toBe(before);
    expect(parseTransactionLog(h.ledgerText(log))).toHaveLength(log.length);
  });
});
