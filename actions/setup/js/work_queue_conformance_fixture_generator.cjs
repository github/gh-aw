"use strict";

// Deterministic v4 ledgers shared by the JS and Go replay engines. The committed
// fixture is regenerated from this script; Go must reproduce every expectation.
const h = require("./work_queue_test_helpers.cjs");
const { buildCommit, checkpointCommit, newRequest, replayTransactions, serializeProjection } = require("./work_queue_replay.cjs");
const { assignmentForDispatch, planDispatch } = require("./work_queue_scheduler.cjs");

const NEXT_PARAMETERS = { pool: "default", max_claims: 16, max_dispatches: 16, concurrency: 2, per_account_limit: 0, profiles: h.PROFILES };
const NEXT_REQUEST = "request:prefix-fixture";
const RESOURCE = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "9007199254740993", number: "7" };
const BACKING = { kind: "issue", host: "github.com", repository: "owner/repo", repository_id: "1", resource_id: "8", number: "8" };

const dispatcherRun = runId => ({ ...h.dispatcher, run_id: runId });

function summary(state) {
  const decision = planDispatch(state, NEXT_PARAMETERS, NEXT_REQUEST);
  return {
    tip: state.tip,
    commits: state.commits,
    stats: state.stats,
    works: Object.fromEntries([...state.works.values()].map(work => [work.node_key, { state: work.state, attempts: work.attempts }])),
    dispatches: Object.fromEntries([...state.dispatches.values()].map(dispatch => [dispatch.dispatch_id, dispatch.state])),
    next: { keys: decision.operations.map(operation => state.works.get(operation.work_id).node_key), reason: decision.reason },
  };
}

/** An unvalidated commit appended to a log prefix (for rejection fixtures). */
function raw(log, actor, kind, parameters, operations) {
  return buildCommit(replayTransactions(log), actor, newRequest(kind, actor, parameters), operations, 1);
}

function lifecycle() {
  const a = h.work("a");
  const b = h.work("b", { depends_on: [{ kind: "work", work_id: a.work_id }] });
  const c = h.work("c", { depends_on: [{ kind: "issue", resource: RESOURCE, condition: "completed" }] });
  const d = h.work("d", { priority: 1 });
  let log = h.submit([], [a, b, c, d]);
  const waiting = { kind: "Observation", observation_id: "o-waiting", resource: RESOURCE, condition: "completed", state: "waiting" };
  let step = h.dispatchNext(log, NEXT_PARAMETERS, dispatcherRun("101"), [waiting]);
  log = step.log;
  const [dispatchD, dispatchA] = step.decision.dispatches;
  log = h.launch(log, dispatchD, true, dispatcherRun("101"));
  log = h.launch(log, dispatchA, false, dispatcherRun("101"));
  const run7 = h.workerActor("7");
  log = h.bind(log, dispatchD, run7);
  log = h.finish(log, dispatchD, run7, {}, [{ kind: "create_issue", resource: { repository: "owner/repo", number: 42 } }]);
  const ready = { kind: "Observation", observation_id: "o-ready", resource: RESOURCE, condition: "completed", state: "ready", state_reason: "completed" };
  step = h.dispatchNext(log, NEXT_PARAMETERS, dispatcherRun("102"), [ready]);
  log = step.log;
  const [dispatchA2, dispatchC] = step.decision.dispatches;
  log = h.launch(log, dispatchA2, true, dispatcherRun("102"));
  const run8 = h.workerActor("8");
  log = h.bind(log, dispatchA2, run8);
  log = h.finish(log, dispatchA2, run8, { h1: "failed" });
  log = h.release(log, dispatchC, "run_missing");
  log = h.commit(log, h.producer, "cancel_work", { work_ids: [c.work_id], reason: "operator" }, [{ kind: "WorkCancellation", work_id: c.work_id, reason: "operator" }]);
  const claimD = replayTransactions(log).dispatches.get(dispatchD).claims[0];
  const links = [
    { kind: "IssueLink", work_id: d.work_id, resource: BACKING },
    { kind: "IssueComment", work_id: d.work_id, comment_id: "IC_1", claim_id: claimD },
  ];
  log = h.commit(log, h.projector, "issue_link", { operations: links }, links);
  step = h.dispatchNext(log, NEXT_PARAMETERS, dispatcherRun("103"));
  log = step.log;
  const [dispatchA3] = step.decision.dispatches;
  log = h.launch(log, dispatchA3, true, dispatcherRun("103"));
  log = h.bind(log, dispatchA3, h.workerActor("9", 1));
  log = h.bind(log, dispatchA3, h.workerActor("9", 2));
  log = h.finish(log, dispatchA3, h.workerActor("9", 2), {}, [{ kind: "create_pull_request", resource: { repository: "owner/repo", number: 43 } }]);
  step = h.dispatchNext(log, NEXT_PARAMETERS, dispatcherRun("104"));
  log = step.log;
  const [dispatchB] = step.decision.dispatches;
  return { log, dispatchB, dispatchA3, claimD };
}

function rejections(log, { dispatchB, dispatchA3 }) {
  const state = replayTransactions(log);
  const claimB = state.dispatches.get(dispatchB).claims[0];
  const claimA = state.dispatches.get(dispatchA3).claims[0];
  const a = h.work("a");
  const b = h.work("b", { depends_on: [{ kind: "work", work_id: a.work_id }] });
  const started = h.launch(log, dispatchB, true, dispatcherRun("104"));
  const bound = h.bind(started, dispatchB, h.workerActor("11"));
  const completeB = [{ kind: "Completion", work_id: b.work_id, claim_id: claimB, dispatch_id: dispatchB, claim_handle: "h1", effects: [] }];
  const launchB = { dispatch_id: dispatchB };
  const startedB = [{ kind: "Dispatch", dispatch_id: dispatchB, state: "started" }];
  const cases = [
    { name: "Completion from another run", prefix: bound, commit: raw(bound, h.workerActor("12"), "finish", launchB, completeB), code: "claim_scope_invalid" },
    {
      name: "Bound from another workflow",
      prefix: started,
      commit: raw(started, h.workerActor("11", 1, ".github/workflows/other.lock.yml"), "bind", launchB, [{ kind: "Bound", dispatch_id: dispatchB, run_id: "11", run_attempt: 1 }]),
      code: "run_binding_conflict",
    },
    { name: "Bound to a second run", prefix: bound, commit: raw(bound, h.workerActor("12"), "bind", launchB, [{ kind: "Bound", dispatch_id: dispatchB, run_id: "12", run_attempt: 1 }]), code: "run_binding_conflict" },
    {
      name: "Completion of an already completed Claim",
      prefix: log,
      commit: raw(log, h.workerActor("9", 1), "finish", { dispatch_id: dispatchA3 }, [{ kind: "Completion", work_id: a.work_id, claim_id: claimA, dispatch_id: dispatchA3, claim_handle: "h1", effects: [] }]),
      code: "claim_ineffective",
    },
    {
      name: "worker WorkCancellation outside finish",
      prefix: bound,
      commit: raw(bound, h.workerActor("11"), "release", { dispatch_id: dispatchB, reason: "operator" }, [{ kind: "WorkCancellation", work_id: b.work_id, reason: "operator" }]),
      code: "unauthorized_operation",
    },
    {
      name: "dispatcher Release other than launch_failed",
      prefix: started,
      commit: raw(started, dispatcherRun("104"), "release", { dispatch_id: dispatchB, reason: "operator" }, [{ kind: "Release", dispatch_id: dispatchB, reason: "operator" }]),
      code: "unauthorized_operation",
    },
    {
      name: "cancel claimed Work before release",
      prefix: bound,
      commit: raw(bound, h.producer, "cancel_work", { work_ids: [b.work_id], reason: "operator" }, [{ kind: "WorkCancellation", work_id: b.work_id, reason: "operator" }]),
      code: "ownership_conflict",
    },
    {
      name: "reason outside the closed enum",
      prefix: bound,
      commit: raw(bound, h.reconciler, "release", { dispatch_id: dispatchB, reason: "operator" }, [{ kind: "Release", dispatch_id: dispatchB, reason: "work_expired" }]),
      code: "reason_invalid",
    },
    { name: "Dispatch started twice", prefix: started, commit: raw(started, dispatcherRun("105"), "launch", launchB, startedB), code: "dispatch_state_conflict" },
    {
      name: "operation not allowed for the request kind",
      prefix: log,
      commit: raw(log, h.reconciler, "release", { dispatch_id: dispatchB, reason: "operator" }, [{ kind: "Claim", work_id: b.work_id, claim_id: "c_x", dispatch_id: "d_x", handle: "h1" }]),
      code: "request_invalid",
    },
    { name: "request reused", prefix: started, commit: raw(started, dispatcherRun("104"), "launch", launchB, startedB), code: "request_reused" },
    { name: "commit that does not extend the tip", prefix: started, commit: raw(log, dispatcherRun("106"), "launch", launchB, startedB), code: "ledger_invalid" },
    { name: "foreign repository", prefix: log, commit: raw(log, { ...h.producer, repository: "other/repo" }, "submit", { work_ids: [h.work("e").work_id] }, [h.work("e")]), code: "actor_unauthorized" },
    {
      name: "dependency cycle",
      prefix: log,
      commit: raw(log, h.producer, "submit", { work_ids: [h.work("x").work_id, h.work("y").work_id] }, [
        h.work("x", { depends_on: [{ kind: "work", work_id: h.work("y").work_id }] }),
        h.work("y", { depends_on: [{ kind: "work", work_id: h.work("x").work_id }] }),
      ]),
      code: "dependency_cycle",
    },
  ];
  // Each case replays fixture.commits, then `extra`, then `commit`.
  return cases.map(({ name, prefix, commit, code }) => ({ name, extra: prefix.slice(log.length), commit, code }));
}

function migration() {
  const actor = { ...h.administrator, workflow: ".github/workflows/agentics-maintenance.yml", run_id: "500", run_attempt: 1 };
  const works = [h.work("m1", { backing_issue: BACKING }), h.work("m2", { priority: 2 }), h.work("m3")];
  let log = h.commit([], actor, "migrate", { work_ids: works.slice(0, 2).map(work => work.work_id) }, works.slice(0, 2));
  log = h.commit(log, actor, "migrate", { work_ids: [works[2].work_id] }, [works[2]]);
  log = h.submit(log, [h.work("n1")]);
  const late = raw(log, actor, "migrate", { work_ids: [h.work("m4").work_id] }, [h.work("m4")]);
  return { commits: log, canonical: h.ledgerText(log), projection: serializeProjection(replayTransactions(log)), late_migrate: { commit: late, code: "request_invalid" } };
}

function buildFixtures() {
  const scenario = lifecycle();
  const { log } = scenario;
  const state = replayTransactions(log);
  const checkpoint = checkpointCommit(state, h.reconciler, 1);
  return {
    version: 4,
    description: "Generated by work_queue_conformance_fixture_generator.cjs; Go and JS must reproduce every prefix summary, projection, rejection code and plan.",
    next_parameters: NEXT_PARAMETERS,
    next_request_id: NEXT_REQUEST,
    commits: log,
    canonical: h.ledgerText(log),
    prefixes: log.map((_, index) => summary(replayTransactions(log.slice(0, index + 1)))),
    projection: serializeProjection(state),
    assignment: assignmentForDispatch(state, scenario.dispatchB),
    checkpoint: { commit: checkpoint, projection: serializeProjection(replayTransactions([checkpoint])) },
    rejections: rejections(log, scenario),
    migration: migration(),
  };
}

if (require.main === module) process.stdout.write(JSON.stringify(buildFixtures(), null, 2) + "\n");

module.exports = { NEXT_PARAMETERS, NEXT_REQUEST, buildFixtures };
