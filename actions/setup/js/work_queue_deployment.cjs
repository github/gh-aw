// @ts-check
"use strict";

const { canonical, closed, queueError } = require("./work_queue_codec.cjs");

function initializeDeployments(state) {
  state.deployments = new Map();
  if (state.policy.authorization !== "aw") return;
  for (const [pool, policy] of Object.entries(state.policy.pools)) {
    const workers = new Map();
    for (const [name, profile] of Object.entries(policy.profiles)) {
      if (!profile.logical_contract) continue;
      workers.set(name, { current_ref: profile.ref, current_contract: profile.logical_contract, revisions: { [profile.ref]: { profile: structuredClone(profile), available: true } } });
    }
    if (workers.size) state.deployments.set(pool, workers);
  }
}

function boundaryEqual(a, b) {
  return ["workflow", "principal", "trust_domain", "credential_scope", "effect_scope", "max_claims", "share_keys"].every(field => a[field] === b[field]);
}

function validateDeployment(operation) {
  closed(operation, ["kind", "pool", "worker_profile", "expected_ref", "expected_contract", "profile", "available", "reason"], ["activate"], "Deployment");
  require("./work_queue_policy.cjs").validateProfile(operation.profile, "aw");
  if (
    !/^[a-f0-9]{64}$/.test(operation.expected_contract) ||
    !/^[a-f0-9]{64}$/.test(operation.profile.logical_contract) ||
    !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(operation.expected_ref) ||
    typeof operation.available !== "boolean" ||
    (operation.activate !== undefined && typeof operation.activate !== "boolean")
  )
    throw queueError("deployment_invalid", "Deployment requires exact prior revision/contract and compiler-derived contract");
}

function applyDeployment(state, operation) {
  validateDeployment(operation);
  const worker = state.deployments?.get(operation.pool)?.get(operation.worker_profile);
  if (!worker || state.policy.authorization !== "aw") throw queueError("deployment_invalid", "deployment requires an installed contract-marked AW worker");
  if (worker.current_ref !== operation.expected_ref || worker.current_contract !== operation.expected_contract) throw queueError("deployment_conflict", "worker deployment changed; refresh expected revision and contract");
  const original = state.policy.pools[operation.pool].profiles[operation.worker_profile];
  if (!boundaryEqual(original, operation.profile)) throw queueError("deployment_invalid", "deployment cannot change worker scope, identity or scheduling economics");
  const prior = worker.revisions[operation.profile.ref];
  if (prior && canonical(prior.profile) !== canonical(operation.profile)) throw queueError("deployment_invalid", "immutable worker revision cannot change its logical contract or authority");
  if (!prior && operation.activate === false) throw queueError("deployment_invalid", "availability updates require an existing immutable revision");
  worker.revisions[operation.profile.ref] = { profile: structuredClone(operation.profile), available: operation.available };
  if (operation.activate !== false) {
    worker.current_ref = operation.profile.ref;
    worker.current_contract = operation.profile.logical_contract;
  }
}

function admissionProfile(state, node) {
  let profile = state.policy.pools[node.pool]?.profiles[node.worker_profile];
  if (!profile) throw queueError("work_invalid", "worker profile is not installed");
  const worker = state.deployments?.get(node.pool)?.get(node.worker_profile);
  if (worker) {
    profile = worker.revisions[node.execution_ref ?? worker.current_ref]?.profile;
    if (!profile) throw queueError("work_invalid", "explicit pin has no registered immutable worker revision");
  } else if (node.execution_ref !== undefined && node.execution_ref !== profile.ref) throw queueError("work_invalid", "historical worker only authorizes its installed exact revision");
  if (node.logical_contract !== undefined && node.logical_contract !== profile.logical_contract) throw queueError("work_invalid", "Work contract does not match its registered immutable revision");
  return profile;
}

function executionProfile(state, work) {
  const worker = state.deployments?.get(work.pool)?.get(work.worker_profile);
  if (!worker) return { profile: state.policy.pools[work.pool].profiles[work.worker_profile], reason: "ready" };
  const revision = worker.revisions[work.execution_ref ?? worker.current_ref];
  if (!revision?.available) return { reason: "worker_unavailable" };
  if (revision.profile.logical_contract !== work.admission_contract) return { reason: "worker_incompatible" };
  return { profile: revision.profile, reason: "ready" };
}

function futurePolicy(state) {
  const policy = structuredClone(state.policy);
  for (const [pool, workers] of state.deployments ?? []) for (const [name, worker] of workers) policy.pools[pool].profiles[name] = structuredClone(worker.revisions[worker.current_ref].profile);
  return policy;
}

function serializeDeployments(state) {
  return Object.fromEntries([...(state.deployments ?? [])].map(([pool, workers]) => [pool, Object.fromEntries(workers)]));
}

function restoreDeployments(state, value) {
  state.deployments = new Map(Object.entries(value ?? {}).map(([pool, workers]) => [pool, new Map(Object.entries(workers))]));
  for (const [pool, workers] of state.deployments)
    for (const [name, worker] of workers) {
      closed(worker, ["current_ref", "current_contract", "revisions"], [], "deployment checkpoint", "checkpoint_invalid");
      const original = state.policy.pools[pool]?.profiles[name];
      if (!original?.logical_contract || worker.revisions[worker.current_ref]?.profile.logical_contract !== worker.current_contract) throw queueError("checkpoint_invalid", "deployment checkpoint has no installed/current authority");
      for (const [ref, revision] of Object.entries(worker.revisions)) {
        closed(revision, ["profile", "available"], [], "revision checkpoint", "checkpoint_invalid");
        require("./work_queue_policy.cjs").validateProfile(revision.profile, "aw");
        if (ref !== revision.profile.ref || !boundaryEqual(original, revision.profile) || !/^[a-f0-9]{64}$/.test(revision.profile.logical_contract) || typeof revision.available !== "boolean")
          throw queueError("checkpoint_invalid", "deployment checkpoint changed immutable worker boundary");
      }
    }
}

module.exports = { initializeDeployments, validateDeployment, applyDeployment, admissionProfile, executionProfile, futurePolicy, serializeDeployments, restoreDeployments };
