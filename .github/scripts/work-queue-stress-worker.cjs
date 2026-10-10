"use strict";

const assert = require("node:assert/strict");
const { parentPort, workerData } = require("node:worker_threads");
const { clientFor, REPOSITORY, PRINCIPAL, WORKFLOW } = require("./work-queue-stress-git.cjs");
const { actorFromContext } = require("../../actions/setup/js/work_queue_policy.cjs");
const { canonical } = require("../../actions/setup/js/work_queue_codec.cjs");
const { newWork } = require("../../actions/setup/js/work_queue_graph.cjs");
const { compactTransactions, newRequest, replayTransactions, validateClaimAuthority } = require("../../actions/setup/js/work_queue_replay.cjs");
const { publishWorkQueueRequest, readWorkQueueLog } = require("../../actions/setup/js/work_queue_store.cjs");
const { authenticatePublisher, fetchNativeRun, fetchNativeRunAttempt, postQueueDispatch, validateNativeRun } = require("../../actions/setup/js/work_queue_native.cjs");
const { assignmentForDispatch, planDispatch } = require("../../actions/setup/js/work_queue_scheduler.cjs");
const { performance } = require("node:perf_hooks");

let sequence = 0;
let currentJob;
const pending = new Map();
const counters = { publication_calls: 0, recovered_publications: 0, retry_sleeps: 0, binding_checks: 0, history_checks: 0 };
const AT = 1800000000000;

function rpc(method, args) {
  const id = ++sequence;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    parentPort.postMessage({ type: "api", id, method, args, meta: { worker: workerData.index, job: currentJob?.id } });
  });
}

const githubClient = clientFor(rpc);
const read = branch => readWorkQueueLog({ githubClient, owner: "local", repo: "queue", branch });

async function trusted(role, runId = "100", extra = {}) {
  const context = await rpc("sim.nativeContext", { run_id: runId });
  return authenticatePublisher({ githubClient, context, role, workflowRef: `${REPOSITORY}/${runId === "100" ? ".github/workflows/publisher.lock.yml" : WORKFLOW}@${context.sha}`, ...extra });
}

async function publish(branch, id, kind, parameters, context, policyProposal = undefined) {
  counters.publication_calls++;
  const result = await publishWorkQueueRequest({
    githubClient,
    owner: "local",
    repo: "queue",
    branch,
    context,
    policyProposal,
    request: newRequest(id, kind, actorFromContext(context), parameters),
    maxRetries: 10,
    now: () => AT,
    sleepFn: async delay => {
      counters.retry_sleeps++;
      await new Promise(resolve => setTimeout(resolve, delay + (workerData.index % 3)));
    },
  });
  if (result.recovered) counters.recovered_publications++;
  return result;
}

function evidence(profile, runId, kind, source, extra = {}) {
  return { kind, source, repository: REPOSITORY, workflow: profile.workflow, ref: profile.ref, principal: profile.principal, checked_at: AT, run_id: runId, run_attempt: 1, ...extra };
}

async function lifecycle(job) {
  const initial = await read(job.branch);
  const assignment = job.assignment;
  const dispatch = initial.state.dispatches.get(assignment.dispatch_id);
  assert.equal(canonical(assignmentForDispatch(initial.state, dispatch.dispatch_id)), canonical(assignment), "immutable assignment membership changed");
  if (dispatch.released) return { skipped: true };
  const dispatcher = await trusted("dispatcher");
  const prefix = `${job.branch}:${assignment.dispatch_id}`;
  const start = await publish(job.branch, `${prefix}:start`, "dispatch", { operations: [{ kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "started", sender: actorFromContext(dispatcher) }] }, dispatcher);
  // Only the publisher that positively observed its own committed marker may
  // POST. A replay or ambiguous success is not permission to launch again.
  if (!start.publishedNow || start.reused || start.recovered || !start.persisted) return { skipped: true };
  const profile = dispatch.profile;
  const returned = await postQueueDispatch(githubClient, { repository: REPOSITORY, workflow: profile.workflow, ref: profile.ref }, { work_queue_assignment: canonical(assignment) });
  const runId = returned.run_id;
  const expected = { repository: REPOSITORY, repository_id: "7", workflow: profile.workflow, workflow_id: "10", ref: profile.ref, principal_id: PRINCIPAL, dispatch_id: assignment.dispatch_id, run_id: runId };
  const original = await fetchNativeRunAttempt(githubClient, REPOSITORY, runId);
  validateNativeRun(original, expected);
  validateNativeRun(await fetchNativeRun(githubClient, REPOSITORY, runId), expected);
  assert.throws(() => validateNativeRun({ ...original, run_attempt: 2 }, expected), /rerun_not_authorized/);
  assert.throws(() => validateNativeRun({ ...original, head_sha: "f".repeat(40) }, expected), /run_ref_mismatch/);
  assert.throws(() => validateNativeRun({ ...original, actor: { id: "9999" } }, expected), /run_principal_mismatch/);
  counters.binding_checks += 5;
  const binding = { run_id: runId, run_attempt: 1, repository: REPOSITORY, workflow: profile.workflow, ref: profile.ref, principal: profile.principal, event: "workflow_dispatch" };
  const bound = await publish(
    job.branch,
    `${prefix}:bind`,
    "dispatch",
    { operations: [{ kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "bound", run: binding, evidence: evidence(profile, runId, "reconciliation", "github_api") }] },
    dispatcher
  );
  const reconciler = await trusted("reconciler");
  const results = [];
  for (const member of assignment.claims) {
    const worker = await trusted("worker", runId, { dispatch_id: assignment.dispatch_id, claim_handle: member.handle });
    validateClaimAuthority(bound.state, member.claim_id, worker);
    assert.throws(() => validateClaimAuthority(bound.state, member.claim_id, { ...worker, run_id: "999999" }), /run_binding_conflict/);
    assert.throws(() => validateClaimAuthority(bound.state, member.claim_id, { ...worker, ref: "f".repeat(40) }), /run_binding_conflict/);
    assert.throws(() => validateClaimAuthority(bound.state, member.claim_id, { ...worker, claim_handle: "not-a-member" }), /run_binding_conflict/);
    const nativeContext = await rpc("sim.nativeContext", { run_id: runId });
    await assert.rejects(authenticatePublisher({ githubClient, context: { ...nativeContext, runAttempt: 2 }, role: "worker" }), /rerun_not_authorized/);
    counters.binding_checks += 5;
    const completed = await publish(job.branch, `${prefix}:finish:${member.handle}`, "finish", { dispatch_id: assignment.dispatch_id, claim_handle: member.handle, outcome: "completed" }, worker);
    const work = completed.state.works.get(member.work_id);
    validateClaimAuthority(completed.state, member.claim_id, worker, { requireCompletion: true });
    const receipt = await rpc("sim.receipt", { run_id: runId, dispatch_id: assignment.dispatch_id, handle: member.handle, work_id: member.work_id, completion_id: work.completion_id });
    results.push({
      kind: "Result",
      work_id: member.work_id,
      claim_id: member.claim_id,
      completion_id: work.completion_id,
      descriptor: receipt.descriptor,
      evidence: evidence(profile, runId, "delivery", "verified_receipts", { receipt: receipt.receipt }),
    });
  }
  await publish(job.branch, `${prefix}:result`, "result", { operations: results }, reconciler);
  await rpc("sim.completeRun", { run_id: runId });
  const terminal = validateNativeRun(await fetchNativeRunAttempt(githubClient, REPOSITORY, runId), expected);
  assert.equal(terminal.terminal, true);
  const released = await publish(
    job.branch,
    `${prefix}:release`,
    "release",
    { operations: [{ kind: "Release", dispatch_id: assignment.dispatch_id, evidence: evidence(profile, runId, "terminal_run", "github_api", { status: terminal.status, conclusion: terminal.conclusion }) }] },
    reconciler
  );
  assert.equal(canonical(assignmentForDispatch(released.state, dispatch.dispatch_id)), canonical(assignment), "lifecycle mutated assignment membership");
  return { completed: assignment.claims.length, released: true, run_id: runId };
}

async function execute(job) {
  if (job.kind === "history_check") {
    const { projectionDigest } = require("./work-queue-stress-retained.cjs");
    const started = performance.now();
    const cold = await read(job.branch);
    const coldReplayMs = performance.now() - started;
    assert.equal(cold.state.works.size, job.expected_work);
    assert.equal(cold.state.claims.size, job.expected_claims);
    assert.equal(cold.state.stats.cancelled, job.expected_claims);
    assert.equal(cold.state.stats.available, 8);
    const digest = projectionDigest(cold.state);
    const selectionStarted = performance.now();
    let selected;
    for (let index = 0; index < 10; index++) {
      const decision = planDispatch(cold.state, { pool: "probe", max_claims: 8, max_dispatches: 1, max_bytes: 49152 }, { requestId: "history-worker-probe", commitId: "history-worker-probe-commit", at: AT });
      assert.equal(decision.operations.length, 8);
      const prefix = canonical(decision.operations);
      if (selected) assert.equal(prefix, selected);
      selected = prefix;
    }
    const selectionMs = performance.now() - selectionStarted;
    const compactStarted = performance.now();
    const compacted = compactTransactions([...cold.transactions, cold.transactions[0], cold.transactions.at(-1)]);
    assert.equal(compacted.length, cold.transactions.length);
    assert.equal(projectionDigest(replayTransactions(compacted)), digest);
    counters.history_checks++;
    return {
      worker: workerData.index,
      work: cold.state.works.size,
      claims: cold.state.claims.size,
      completed: cold.state.stats.completed,
      ledger_bytes: cold.state.ledgerBytes,
      projection_sha256: digest,
      cold_replay_ms: coldReplayMs,
      ten_batch_plans_ms: selectionMs,
      canonicalization_ms: performance.now() - compactStarted,
      elapsed_ms: performance.now() - started,
    };
  }
  if (job.kind === "lifecycle") return lifecycle(job);
  const context = await trusted(job.kind === "submit" ? "producer" : "dispatcher");
  if (job.kind === "submit") {
    // Fixture construction does not replay per node.
    const nodes = Array.from({ length: job.count }, (_, offset) => {
      const index = job.start + offset;
      const node = newWork({ item: index, effect_contract: { kind: "none" } }, job.graph, `item-${index}`, "default", job.policy, AT);
      node.fairness_key = index % 2 ? "beta" : "alpha";
      return node;
    });
    const published = await publish(job.branch, job.id, "submit", { nodes }, context, job.policy);
    return { submitted: nodes.length, recovered: published.recovered === true };
  }
  if (job.kind === "dispatch") {
    const published = await publish(job.branch, job.id, "dispatch_next", { pool: "default", max_claims: 16, max_dispatches: 1, max_bytes: 48 * 1024 }, context);
    return { assignments: published.assignments, reason: published.reason, recovered: published.recovered === true };
  }
  throw new Error(`unknown worker job kind: ${job.kind}`);
}

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
      execute(message.job).then(
        result => parentPort.postMessage({ type: "result", id: message.job.id, result, counters: { ...counters }, rss: process.memoryUsage().rss }),
        error => parentPort.postMessage({ type: "result", id: message.job.id, error: { message: error.message, code: error.code, stack: error.stack }, counters: { ...counters }, rss: process.memoryUsage().rss })
      );
    }
  });
}
