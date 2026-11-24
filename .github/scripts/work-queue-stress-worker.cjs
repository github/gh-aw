"use strict";

const assert = require("node:assert/strict");
const { parentPort, workerData } = require("node:worker_threads");
const { performance } = require("node:perf_hooks");
const { AT, LAUNCH_REF, OWNER, PUBLISHER_WORKFLOW, REPO, clientFor, queueActor } = require("./work-queue-stress-git.cjs");
const { canonical } = require("../../actions/setup/js/work_queue_codec.cjs");
const { appendCommit, buildCommit, newRequest } = require("../../actions/setup/js/work_queue_replay.cjs");
const { publishWorkQueueRequest, readWorkQueueLog } = require("../../actions/setup/js/work_queue_store.cjs");
const { launchedRunEvidence, postQueueDispatch, readWorkflowRun, verifyLaunchedRun } = require("../../actions/setup/js/work_queue_native.cjs");
const { assignmentForDispatch, planDispatch } = require("../../actions/setup/js/work_queue_scheduler.cjs");
const { normalizeSubmitParameters } = require("../../actions/setup/js/work_queue_intents.cjs");
const { releaseOperations } = require("../../actions/setup/js/work_queue_binding.cjs");
const { finishOperations } = require("../../actions/setup/js/finish_work_queue_claim.cjs");
const { bindOperations } = require("../../actions/setup/js/write_work_queue_snapshot.cjs");

let sequence = 0;
let currentJob;
const pending = new Map();
const counters = { publication_calls: 0, recovered_publications: 0, cas_exhausted_retries: 0, binding_checks: 0, duplicate_launch_refusals: 0, history_checks: 0 };
// Harness-level retries of a whole publication after the store's own CAS budget is spent.
const PUBLICATION_ROUNDS = 8;

function rpc(method, args) {
  const id = ++sequence;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    parentPort.postMessage({ type: "api", id, method, args, meta: { worker: workerData.index, job: currentJob?.id } });
  });
}

const githubClient = clientFor(rpc);
const target = branch => ({ githubClient, owner: OWNER, repo: REPO, branch });
const read = branch => readWorkQueueLog(target(branch));

/**
 * Publish through the production store. Each store call re-plans `operationsFor` against
 * every re-read head; a lost ref response is recovered from the committed request ID.
 */
async function publish(branch, actor, kind, parameters, operationsFor) {
  counters.publication_calls++;
  for (let round = 1; ; round++) {
    try {
      const result = await publishWorkQueueRequest({ ...target(branch), actor, request: { kind, parameters }, operationsFor, now: () => AT });
      if (result.recovered) counters.recovered_publications++;
      return result;
    } catch (error) {
      if (error.code !== "cas_exhausted" || round === PUBLICATION_ROUNDS) throw error;
      counters.cas_exhausted_retries++;
      await new Promise(resolve => setTimeout(resolve, 5 * round + workerData.index));
    }
  }
}

/** Replay refuses `operations` from `actor` against `state` with `code`; nothing is written. */
function refused(state, actor, kind, parameters, operations, codes) {
  assert.throws(
    () => appendCommit(state, buildCommit(state, actor, newRequest(kind, actor, parameters), operations, AT)),
    error => [].concat(codes).includes(error.code)
  );
  counters.binding_checks++;
}

function stressNodes(job) {
  return Array.from({ length: job.count }, (_, offset) => {
    const index = job.start + offset;
    // Mixed priorities and a skewed account split exercise priority, FIFO and per-account limits.
    return { graph_id: job.graph, node_key: `item-${index}`, pool: "default", priority: 1 + ((index * 7) % 5), fairness_key: index % 3 === 2 ? "beta" : "alpha", payload: { item: index } };
  });
}

/** Admit Work like the producer's submit intent, including the compiled pending_limit. */
async function submit(job) {
  const works = normalizeSubmitParameters({ nodes: stressNodes(job) }, job.policy);
  const published = await publish(job.branch, queueActor("producer"), "submit", { work_ids: works.map(work => work.work_id) }, state => {
    const fresh = works.filter(work => !state.works.has(work.work_id));
    let count = 0;
    for (const work of state.works.values()) if (work.pool === "default" && (work.state === "available" || work.state === "claimed")) count++;
    if (count + fresh.length > job.policy.pending_limit) throw Object.assign(new Error(`work_queue_pending_limit: pool default would exceed ${job.policy.pending_limit}`), { code: "work_queue_pending_limit" });
    return fresh;
  });
  return { submitted: works.length, recovered: published.recovered };
}

/** One dispatcher run's dispatch_next: one Claim per Dispatch, planned against every re-read head. */
async function dispatch(job) {
  let reason = null;
  const published = await publish(job.branch, queueActor("dispatcher", job.run_id), "dispatch_next", job.parameters, (state, request) => {
    const decision = planDispatch(state, job.parameters, request.id);
    reason = decision.reason;
    return decision.operations;
  });
  const assignments = published.recovered ? [] : [...published.state.dispatches.values()].filter(entry => entry.request_id === published.request.id).map(entry => assignmentForDispatch(published.state, entry.dispatch_id));
  return { assignments, reason, recovered: published.recovered };
}

/**
 * A launched worker run that is not the Dispatch's recorded launch: production provenance
 * refuses it before binding, and replay refuses its Bound even if published.
 */
async function refuseDuplicateRun(job, state, runId) {
  const recorded = state.dispatches.get(job.assignment.dispatch_id);
  assert.notEqual(recorded.launch.run_id, runId, "duplicate run must not be the recorded launch");
  const worker = queueActor("worker", runId);
  refused(state, worker, "bind", { dispatch_id: recorded.dispatch_id }, [{ kind: "Bound", dispatch_id: recorded.dispatch_id, run_id: runId, run_attempt: 1 }], ["run_binding_conflict", "dispatch_state_conflict"]);
  await rpc("sim.cancelRun", { run_id: runId });
  counters.duplicate_launch_refusals++;
  return { skipped: true, duplicate_run: runId };
}

async function lifecycle(job) {
  const dispatchId = job.assignment.dispatch_id;
  const initial = await read(job.branch);
  const reserved = initial.state.dispatches.get(dispatchId);
  if (reserved.state !== "reserved") return { skipped: true };
  // Dispatcher: POST the worker launch, read back the created run, then record it as the
  // dispatcher attempt that reserved the Dispatch, the only actor replay lets record it.
  const destination = { owner: OWNER, repo: REPO, workflow: reserved.workflow };
  const response = await postQueueDispatch(githubClient, { ...destination, ref: LAUNCH_REF }, { work_queue_assignment: JSON.stringify({ version: 4, dispatch_id: dispatchId }) });
  const evidence = await launchedRunEvidence(githubClient, destination, response);
  const launcher = queueActor("dispatcher", reserved.reserved_by.run_id, reserved.reserved_by.run_attempt);
  const launched = await publish(job.branch, launcher, "launch", { dispatch_id: dispatchId }, state =>
    state.dispatches.get(dispatchId)?.state === "reserved" ? [{ kind: "Dispatch", dispatch_id: dispatchId, state: "started", run_id: evidence.run_id, revision: evidence.revision }] : []
  );
  if (launched.state.dispatches.get(dispatchId).launch?.run_id !== evidence.run_id) return refuseDuplicateRun(job, launched.state, evidence.run_id);

  // Worker: prove this run is the recorded launch, then bind it.
  const runId = evidence.run_id;
  const context = await rpc("sim.nativeContext", { run_id: runId });
  const worker = queueActor("worker", runId);
  const started = (await read(job.branch)).state;
  const dispatch = started.dispatches.get(dispatchId);
  assert.equal(dispatch.launch.run_id, runId);
  assert.equal(context.sha, dispatch.launch.revision, "the worker runs the revision its dispatcher launched");
  const run = await readWorkflowRun(githubClient, { owner: OWNER, repo: REPO, run_id: runId });
  const expected = { owner: OWNER, repo: REPO, run_id: runId, workflow: dispatch.workflow, revision: dispatch.launch.revision };
  verifyLaunchedRun(run, expected);
  for (const forged of [
    { ...run, head_sha: "f".repeat(40) },
    { ...run, path: PUBLISHER_WORKFLOW },
    { ...run, event: "push" },
    { ...run, id: Number(runId) + 1 },
  ]) {
    assert.throws(
      () => verifyLaunchedRun(forged, expected),
      error => error.code === "work_queue_launch_unverified"
    );
    counters.binding_checks++;
  }
  const bound = await publish(job.branch, worker, "bind", { dispatch_id: dispatchId }, state => bindOperations(state, dispatchId, { run_id: runId, run_attempt: 1 }));
  const state = bound.state;
  assert.equal(state.dispatches.get(dispatchId).state, "bound");
  const assignment = assignmentForDispatch(state, dispatchId);
  assert.equal(canonical(assignment), canonical(job.assignment), "bound assignment differs from the granted Claim");
  const [member] = assignment.claims;
  const completion = handle => [{ kind: "Completion", work_id: member.work_id, claim_id: member.claim_id, dispatch_id: dispatchId, claim_handle: handle, effects: [] }];

  // Only this run attempt of the dispatched workflow may bind or settle the Claim.
  refused(state, queueActor("worker", "999999"), "bind", { dispatch_id: dispatchId }, [{ kind: "Bound", dispatch_id: dispatchId, run_id: "999999", run_attempt: 1 }], "run_binding_conflict");
  refused(state, queueActor("worker", runId, 1, PUBLISHER_WORKFLOW), "bind", { dispatch_id: dispatchId }, [{ kind: "Bound", dispatch_id: dispatchId, run_id: runId, run_attempt: 1 }], "run_binding_conflict");
  refused(state, queueActor("worker", "999999"), "finish", { dispatch_id: dispatchId }, completion(member.handle), "claim_scope_invalid");
  refused(state, worker, "finish", { dispatch_id: dispatchId }, completion(`${member.handle}x`), "claim_scope_invalid");
  refused(state, queueActor("dispatcher"), "finish", { dispatch_id: dispatchId }, completion(member.handle), "unauthorized_operation");
  // A monotone rerun may rebind; the superseded attempt then cannot complete.
  const rerun = queueActor("worker", runId, 2);
  const rebound = appendCommit(state, buildCommit(state, rerun, newRequest("bind", rerun, { dispatch_id: dispatchId }), bindOperations(state, dispatchId, { run_id: runId, run_attempt: 2 }), AT));
  refused(rebound, worker, "finish", { dispatch_id: dispatchId }, completion(member.handle), "claim_scope_invalid");

  // The run delivers its (no-op) effect and finishes its sole Claim through the production planner.
  const { receipt } = await rpc("sim.receipt", { run_id: runId, dispatch_id: dispatchId, claim_id: member.claim_id, work_id: member.work_id, handle: member.handle });
  const effects = new Map([[member.handle, [{ kind: "local_noop", receipt, work_id: member.work_id }]]]);
  const finished = await publish(job.branch, worker, "finish", { dispatch_id: dispatchId }, current => finishOperations(current, dispatchId, new Map([[member.handle, "completed"]]), effects, job.policy));
  assert.equal(finished.state.claims.get(member.claim_id).state, "completed");
  assert.equal(finished.state.dispatches.get(dispatchId).state, "closed");
  // A settled Dispatch has no assignment and nothing for maintenance to release.
  assert.deepEqual(assignmentForDispatch(finished.state, dispatchId).claims, []);
  assert.deepEqual(releaseOperations(finished.state, dispatchId, "run_terminal", job.policy), []);
  refused(finished.state, queueActor("reconciler"), "release", { dispatch_id: dispatchId, reason: "run_terminal" }, [{ kind: "Release", dispatch_id: dispatchId, reason: "run_terminal" }], "dispatch_state_conflict");
  await rpc("sim.completeRun", { run_id: runId });
  const terminal = await readWorkflowRun(githubClient, { owner: OWNER, repo: REPO, run_id: runId });
  verifyLaunchedRun(terminal, expected);
  assert.equal(terminal.status, "completed");
  return { completed: 1, run_id: runId };
}

async function historyCheck(job) {
  const { projectionDigest, verifiedCheckpoint } = require("./work-queue-stress-retained.cjs");
  const started = performance.now();
  const cold = await read(job.branch);
  const coldReplayMs = performance.now() - started;
  assert.equal(cold.state.works.size, job.expected_work);
  assert.equal(cold.state.claims.size, job.expected_claims);
  assert.equal(cold.state.stats.cancelled, job.expected_claims);
  assert.equal(cold.state.stats.available, job.expected_work - job.expected_claims);
  const digest = projectionDigest(cold.state);
  const selectionStarted = performance.now();
  let selected;
  for (let index = 0; index < 10; index++) {
    const decision = planDispatch(cold.state, job.probe_parameters, job.probe_request_id);
    assert.equal(decision.operations.length, job.expected_work - job.expected_claims);
    const prefix = canonical(decision.operations);
    if (selected) assert.equal(prefix, selected);
    selected = prefix;
  }
  const selectionMs = performance.now() - selectionStarted;
  const checkpointStarted = performance.now();
  // Serialize and strictly re-read the checkpoint line as persisted; it must be bounded.
  const checkpoint = verifiedCheckpoint(cold.state, queueActor("administrator"));
  counters.history_checks++;
  return {
    worker: workerData.index,
    work: cold.state.works.size,
    claims: cold.state.claims.size,
    completed: cold.state.stats.completed,
    ledger_bytes: Buffer.byteLength(cold.contents),
    projection_sha256: digest,
    cold_replay_ms: coldReplayMs,
    ten_batch_plans_ms: selectionMs,
    checkpoint_bytes: checkpoint.bytes,
    checkpoint_requests: checkpoint.requests,
    checkpoint_ms: performance.now() - checkpointStarted,
    elapsed_ms: performance.now() - started,
  };
}

const JOBS = { submit, dispatch, lifecycle, history_check: historyCheck };

if (parentPort) {
  parentPort.on("message", message => {
    if (message.type === "api-result") {
      const waiter = pending.get(message.id);
      if (!waiter) return;
      pending.delete(message.id);
      if (message.error) waiter.reject(Object.assign(new Error(message.error.message), message.error));
      else waiter.resolve(message.result);
    } else if (message.type === "job") {
      currentJob = message.job;
      const run = Object.hasOwn(JOBS, message.job.kind) ? JOBS[message.job.kind] : () => Promise.reject(new Error(`unknown worker job kind: ${message.job.kind}`));
      run(message.job).then(
        result => parentPort.postMessage({ type: "result", id: message.job.id, result, counters: { ...counters }, rss: process.memoryUsage().rss }),
        error => parentPort.postMessage({ type: "result", id: message.job.id, error: { message: error.message, code: error.code, stack: error.stack }, counters: { ...counters }, rss: process.memoryUsage().rss })
      );
    }
  });
}
