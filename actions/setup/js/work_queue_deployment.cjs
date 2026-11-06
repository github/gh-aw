// @ts-check
"use strict";

const { canonical, closed, queueError } = require("./work_queue_codec.cjs");

/** @param {any} state @param {string[]} [selectedPools] */
function initializeDeployments(state, selectedPools = undefined) {
  if (selectedPools === undefined) state.deployments = new Map();
  else state.deployments ??= new Map();
  if (state.policy.authorization !== "aw") return;
  for (const [pool, policy] of Object.entries(state.policy.pools)) {
    if (selectedPools !== undefined && !selectedPools.includes(pool)) continue;
    const workers = state.deployments.get(pool) ?? new Map();
    for (const [name, profile] of Object.entries(policy.profiles)) {
      if (!profile.logical_contract || workers.has(name)) continue;
      workers.set(name, { current_ref: profile.ref, current_contract: profile.logical_contract, revisions: { [profile.ref]: { profile: structuredClone(profile), available: true } } });
    }
    if (workers.size) state.deployments.set(pool, workers);
  }
}

function boundaryEqual(a, b) {
  return ["workflow", "principal", "trust_domain", "credential_scope", "effect_scope", "max_claims", "share_keys"].every(field => a[field] === b[field]);
}

function validateDeployment(operation) {
  closed(operation, ["kind", "pool", "worker_profile", "expected_ref", "expected_contract", "profile", "available", "reason"], ["activate", "contract_routes", "register"], "Deployment");
  require("./work_queue_policy.cjs").validateProfile(operation.profile, "aw");
  if (
    !/^[a-f0-9]{64}$/.test(operation.expected_contract) ||
    !/^[a-f0-9]{64}$/.test(operation.profile.logical_contract) ||
    !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(operation.expected_ref) ||
    typeof operation.available !== "boolean" ||
    (operation.activate !== undefined && typeof operation.activate !== "boolean") ||
    (operation.register !== undefined && typeof operation.register !== "boolean")
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
  if (operation.register && (operation.activate !== false || operation.contract_routes !== undefined)) throw queueError("deployment_invalid", "bridge registration requires an availability-only operation");
  if (!prior && operation.activate === false && !operation.register) throw queueError("deployment_invalid", "availability updates require an existing immutable revision");
  if (operation.contract_routes !== undefined) {
    if (operation.activate === false) throw queueError("deployment_invalid", "contract routes require a default-revision activation");
    validateContractRoutes(operation.contract_routes, worker.revisions);
  }
  worker.revisions[operation.profile.ref] = { profile: structuredClone(operation.profile), available: operation.available };
  if (operation.activate !== false) {
    worker.current_ref = operation.profile.ref;
    worker.current_contract = operation.profile.logical_contract;
    if (operation.contract_routes !== undefined) {
      if (Object.keys(operation.contract_routes).length) worker.contract_routes = structuredClone(operation.contract_routes);
      else delete worker.contract_routes;
    }
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
  const ref = work.execution_ref ?? (work.admission_contract === worker.current_contract ? worker.current_ref : (worker.contract_routes?.[work.admission_contract] ?? worker.current_ref));
  const revision = worker.revisions[ref];
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
  // Checkpoint snapshots must not share nested deployment objects with mutable replay state.
  return structuredClone(Object.fromEntries([...(state.deployments ?? [])].map(([pool, workers]) => [pool, Object.fromEntries(workers)])));
}

function restoreDeployments(state, value) {
  // Replay mutates restored deployments, so detach them from immutable checkpoint data.
  state.deployments = new Map(Object.entries(structuredClone(value ?? {})).map(([pool, workers]) => [pool, new Map(Object.entries(workers))]));
  for (const [pool, workers] of state.deployments)
    for (const [name, worker] of workers) {
      closed(worker, ["current_ref", "current_contract", "revisions"], ["contract_routes"], "deployment checkpoint", "checkpoint_invalid");
      const original = state.policy.pools[pool]?.profiles[name];
      if (!original?.logical_contract || worker.revisions[worker.current_ref]?.profile.logical_contract !== worker.current_contract) throw queueError("checkpoint_invalid", "deployment checkpoint has no installed/current authority");
      for (const [ref, revision] of Object.entries(worker.revisions)) {
        closed(revision, ["profile", "available"], [], "revision checkpoint", "checkpoint_invalid");
        require("./work_queue_policy.cjs").validateProfile(revision.profile, "aw");
        if (ref !== revision.profile.ref || !boundaryEqual(original, revision.profile) || !/^[a-f0-9]{64}$/.test(revision.profile.logical_contract) || typeof revision.available !== "boolean")
          throw queueError("checkpoint_invalid", "deployment checkpoint changed immutable worker boundary");
      }
      if (worker.contract_routes !== undefined) validateContractRoutes(worker.contract_routes, worker.revisions);
    }
}

function validateContractRoutes(routes, revisions) {
  if (!routes || typeof routes !== "object" || Array.isArray(routes) || Object.keys(routes).length > 4096) throw queueError("deployment_invalid", "contract routes require an object with at most 4096 exact contract/revision bindings");
  for (const [contract, ref] of Object.entries(routes))
    if (!/^[a-f0-9]{64}$/.test(contract) || typeof ref !== "string" || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(ref) || revisions[ref]?.profile.logical_contract !== contract)
      throw queueError("deployment_invalid", "contract routes must bind exact admission contracts to registered immutable worker revisions");
}

function contractRoutesForDeployment(state, pool, name, next) {
  const installed = state.policy.pools[pool].profiles[name];
  const activated = new Map([[installed.logical_contract, installed.ref]]);
  for (const commit of state.requests.values()) {
    if (commit.policy_epoch !== state.policy_epoch) continue;
    for (const operation of commit.operations)
      if (operation.kind === "Deployment" && operation.pool === pool && operation.worker_profile === name && operation.activate !== false) activated.set(operation.profile.logical_contract, operation.profile.ref);
  }
  const routes = {};
  for (const work of state.works.values()) {
    if (work.pool !== pool || work.worker_profile !== name || work.execution_ref || !["available", "claimed"].includes(work.state) || work.admission_contract === next.logical_contract) continue;
    const ref = activated.get(work.admission_contract);
    if (!ref) throw queueError("deployment_invalid", "unfinished Work contract has no authenticated worker activation; inspect the installed deployment history");
    routes[work.admission_contract] = ref;
  }
  validateContractRoutes(routes, state.deployments.get(pool).get(name).revisions);
  return routes;
}

module.exports = { initializeDeployments, validateDeployment, applyDeployment, admissionProfile, executionProfile, futurePolicy, serializeDeployments, restoreDeployments, contractRoutesForDeployment };
