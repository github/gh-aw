"use strict";

const { newRequest, replayTransactions } = require("./work_queue_replay.cjs");
const { defaultPolicy } = require("./work_queue_policy.cjs");
const { newWork } = require("./work_queue_graph.cjs");
const { planDispatch } = require("./work_queue_scheduler.cjs");

const administrator = { role: "administrator", principal: "1001", repository: "owner/repo" };
const producer = { ...administrator, role: "producer" };
const dispatcher = { ...administrator, role: "dispatcher", workflow: ".github/workflows/dispatcher.lock.yml", run_id: "100", run_attempt: 1 };
const reconciler = { ...administrator, role: "reconciler" };

function context(actor, extra = {}) {
  return { ...actor, authenticated: true, roles: [actor.role], ...extra };
}

function commit(previous, id, kind, actor, parameters, operations, at = 100, epoch = "initial") {
  return { version: 3, id, previous, request: newRequest(`request-${id}`, kind, actor, parameters), actor, policy_epoch: epoch, at, operations };
}

function genesis(policy = defaultPolicy({ repository: "owner/repo", principal: "1001" })) {
  const operations = [{ kind: "Policy", epoch: "initial", policy }];
  return commit(null, "genesis", "policy", administrator, { operations }, operations, 0);
}

function submission(log, names, { at = 1, graph = "graph", transform = node => node, id = "submit" } = {}) {
  const state = replayTransactions(log);
  const nodes = names.map((name, index) => transform(newWork({ task: name }, graph, name, "default", state.policy, at), index));
  return commit(state.tip, id, "submit", producer, { nodes }, nodes, at, state.policy_epoch);
}

function grant(log, { max_claims = 1, max_dispatches = 1, max_bytes = 49152, at = 10, id = "grant" } = {}) {
  const state = replayTransactions(log);
  const parameters = { pool: "default", max_claims, max_dispatches, max_bytes };
  const requestId = `request-${id}`;
  const decision = planDispatch(state, parameters, { requestId, commitId: id, at });
  return { ...decision, commit: commit(state.tip, id, "dispatch_next", dispatcher, parameters, decision.operations, at, state.policy_epoch) };
}

function operationCommit(log, id, kind, operations, actor = reconciler, at = 20) {
  const state = replayTransactions(log);
  return commit(state.tip, id, kind, actor, { operations }, operations, at, state.policy_epoch);
}

function evidence(state, dispatch, kind, at, extra = {}) {
  const profile = state.policy.pools[dispatch.pool].profiles[dispatch.worker_profile];
  return { kind, source: "github_api", repository: "owner/repo", workflow: profile.workflow, ref: profile.ref, principal: profile.principal, checked_at: at, ...extra };
}

function bind(log, dispatchId, { id = "binding", at = 30, runId = "200" } = {}) {
  let state = replayTransactions(log);
  const start = operationCommit(log, `start-${id}`, "dispatch", [{ kind: "Dispatch", dispatch_id: dispatchId, state: "started", sender: dispatcher }], dispatcher, at - 1);
  const started = [...log, start];
  state = replayTransactions(started);
  const dispatch = state.dispatches.get(dispatchId);
  const profile = state.policy.pools[dispatch.pool].profiles[dispatch.worker_profile];
  const run = { run_id: runId, run_attempt: 1, repository: "owner/repo", workflow: profile.workflow, ref: profile.ref, principal: profile.principal, event: "workflow_dispatch" };
  const binding = operationCommit(started, id, "dispatch", [{ kind: "Dispatch", dispatch_id: dispatchId, state: "bound", run, evidence: evidence(state, dispatch, "reconciliation", at, { run_id: runId, run_attempt: 1 }) }], reconciler, at);
  return [...started, binding];
}

function workerActor(state, dispatchId, handle = "h1") {
  const dispatch = state.dispatches.get(dispatchId);
  const run = dispatch.run;
  return { role: "worker", principal: run.principal, repository: run.repository, workflow: run.workflow, run_id: run.run_id, run_attempt: 1, dispatch_id: dispatchId, claim_handle: handle };
}

function finish(log, dispatchId, handle, outcome, { id = `finish-${handle}`, at = 40 } = {}) {
  const { finishOperations } = require("./work_queue_replay.cjs");
  const state = replayTransactions(log);
  const actor = workerActor(state, dispatchId, handle);
  const parameters = { dispatch_id: dispatchId, claim_handle: handle, outcome };
  const request = newRequest(`request-${id}`, "finish", actor, parameters);
  return commit(state.tip, id, "finish", actor, parameters, finishOperations(state, request, actor, at), at, state.policy_epoch);
}

module.exports = { administrator, bind, commit, context, dispatcher, evidence, finish, genesis, grant, operationCommit, producer, reconciler, submission, workerActor };
