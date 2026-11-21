// @ts-check
"use strict";

const { WORKER_WORKFLOW, bind, dispatchNext, fakeQueueGitHub, launch, ledgerText, repository, submit, work, workerActor } = require("./work_queue_test_helpers.cjs");
const { replayTransactions } = require("./work_queue_replay.cjs");
const { assignmentForDispatch } = require("./work_queue_scheduler.cjs");

/** Compiled v4 policy env for pool "default" / profile "worker". */
function workerPolicy({ maxAttempts = 3, effectScope = repository } = {}) {
  return {
    version: 4,
    pending_limit: 30,
    issues: false,
    pools: { default: { concurrency: 16, per_account_limit: 0, max_attempts: maxAttempts, default_profile: "worker", profiles: { worker: { workflow: WORKER_WORKFLOW, effect_scope: effectScope } } } },
  };
}

/** The worker job's Actions context for one run attempt of a Dispatch. */
function workerContext(dispatchId, runId = "200", runAttempt = 1) {
  return { repo: { owner: "owner", repo: "repo" }, runId: Number(runId), runAttempt, payload: { inputs: { work_queue_assignment: JSON.stringify({ version: 4, dispatch_id: dispatchId }) } } };
}

/**
 * A ledger with one launched Dispatch holding `count` Claims (one Dispatch per Claim is
 * the scheduler rule, so `count > 1` uses separate Dispatches and returns the first),
 * optionally bound to run `runId`/`runAttempt`, served by an in-memory queue branch.
 * @param {{count?: number, bound?: boolean, runId?: string, runAttempt?: number, works?: any[]}} [options]
 */
function workerQueue({ count = 1, bound = true, runId = "200", runAttempt = 1, works = undefined } = {}) {
  let log = submit([], works ?? Array.from({ length: count }, (_, index) => work(`n${index + 1}`)));
  const planned = dispatchNext(log, { max_claims: 1, max_dispatches: 1 });
  log = planned.log;
  const dispatchId = planned.decision.dispatches[0];
  log = launch(log, dispatchId);
  if (bound) log = bind(log, dispatchId, workerActor(runId, runAttempt));
  const fake = fakeQueueGitHub(ledgerText(log));
  const state = replayTransactions(log);
  return {
    log,
    dispatchId,
    fake,
    githubClient: fake.githubClient,
    context: workerContext(dispatchId, runId, runAttempt),
    assignment: assignmentForDispatch(state, dispatchId),
    policy: workerPolicy(),
    workflowRef: `${repository}/${WORKER_WORKFLOW}@refs/heads/main`,
  };
}

module.exports = { workerContext, workerPolicy, workerQueue };
