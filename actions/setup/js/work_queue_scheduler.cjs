// @ts-check
"use strict";

const { createHash } = require("node:crypto");
const { canonicalBytes, closed, integer, utf8Compare, queueError } = require("./work_queue_codec.cjs");

function gcd(a, b) {
  while (b !== 0n) [a, b] = [b, a % b];
  return a;
}

function tickScale(weights) {
  let q = 1n;
  for (const weight of weights) {
    if (!Number.isSafeInteger(weight) || weight < 1 || weight > 1000) throw queueError("policy_invalid", "weights must be integers between 1 and 1000");
    const w = BigInt(weight);
    q = (q / gcd(q, w)) * w;
  }
  return q;
}

function clock() {
  return { virtual: 0n, passes: new Map(), active: new Set() };
}

function copyClock(parent) {
  return { virtual: parent.virtual, passes: new Map(parent.passes), active: new Set(parent.active) };
}

function pick(parent, eligible, weights, scale, compare = utf8Compare) {
  const next = copyClock(parent);
  for (const key of eligible) {
    const weight = weights.get(key);
    if (weight === undefined) throw queueError("policy_invalid", "unregistered accounting key");
    const stride = scale / BigInt(weight);
    if (!parent.active.has(key)) {
      const old = next.passes.get(key) ?? 0n;
      const baseline = parent.virtual + stride;
      next.passes.set(key, old > baseline ? old : baseline);
    }
  }
  next.active = new Set(eligible);
  if (!eligible.length) return { key: null, clock: next };
  const key = [...eligible].sort((a, b) => {
    const delta = next.passes.get(a) - next.passes.get(b);
    return delta < 0n ? -1 : delta > 0n ? 1 : compare(a, b);
  })[0];
  const pass = next.passes.get(key);
  next.virtual = pass;
  next.passes.set(key, pass + scale / BigInt(weights.get(key)));
  return { key, clock: next };
}

function fifoCompare(a, b) {
  return a.position.commit - b.position.commit || a.position.operation - b.position.operation;
}

function copySchedule(schedule) {
  return { classes: copyClock(schedule.classes), keys: new Map([...schedule.keys].map(([priority, parent]) => [priority, copyClock(parent)])) };
}

function diagnostics(schedule) {
  const render = parent => ({
    v: parent.virtual.toString(),
    pass: Object.fromEntries([...parent.passes].sort(([a], [b]) => utf8Compare(String(a), String(b))).map(([key, pass]) => [key, pass.toString()])),
    active: Object.fromEntries([...parent.active].sort((a, b) => (typeof a === "number" ? a - b : utf8Compare(a, b))).map(key => [key, true])),
  });
  return { classes: render(schedule.classes), keys: Object.fromEntries([...schedule.keys].sort(([a], [b]) => a - b).map(([priority, parent]) => [priority, render(parent)])) };
}

function reservationSnapshot(state, pool) {
  let logical = 0;
  let native = 0;
  const accounts = new Map();
  for (const claim of state.claims.values()) {
    if (claim.state !== "open") continue;
    const work = state.works.get(claim.work_id);
    if (work.pool !== pool) continue;
    logical++;
    accounts.set(work.fairness_key, (accounts.get(work.fairness_key) || 0) + 1);
  }
  for (const dispatch of state.dispatches.values()) if (dispatch.pool === pool && !dispatch.released) native++;
  return { logical, accounts, native };
}

function reservationCounts(state, pool, key) {
  const { logical, accounts, native } = reservationSnapshot(state, pool);
  return { logical, account: accounts.get(key) || 0, native };
}

function eligibility(state, work, at, capacitySnapshot) {
  const { poolPolicy } = require("./work_queue_policy.cjs");
  const { dependencyStatus } = require("./work_queue_graph.cjs");
  if (work.state !== "available") return { ready: false, reason: `ownership_${work.state}`, observations: [] };
  const policy = poolPolicy(state, work.pool);
  if (state.grants_paused) return { ready: false, reason: "grants_paused", observations: [] };
  if (work.retry_not_before > at) return { ready: false, reason: "retry_delayed", observations: [] };
  if (work.attempts >= policy.retry.max_attempts) return { ready: false, reason: "retry_exhausted", observations: [] };
  const capacity = capacitySnapshot || reservationCounts(state, work.pool, work.fairness_key);
  if (capacity.logical >= policy.logical_limit) return { ready: false, reason: "capacity_blocked", observations: [] };
  if (policy.per_account_limit && capacity.account >= policy.per_account_limit) return { ready: false, reason: "account_capacity_blocked", observations: [] };
  return dependencyStatus(state, work, at);
}

function planNext(state, pool, at) {
  const { poolPolicy, policyScales } = require("./work_queue_policy.cjs");
  const policy = poolPolicy(state, pool);
  integer(at, 0, Number.MAX_SAFE_INTEGER, "decision timestamp");
  if (state.grants_paused) return { reason: "grants_paused", observations: [] };
  const capacity = reservationSnapshot(state, pool);
  if (capacity.logical >= policy.logical_limit) return { reason: "capacity_blocked", observations: [] };
  const buckets = new Map();
  let pending = 0;
  for (const work of state.works.values()) {
    if (work.pool !== pool || work.state !== "available") continue;
    pending++;
    const dependency = eligibility(state, work, at, { logical: capacity.logical, account: capacity.accounts.get(work.fairness_key) || 0 });
    if (!dependency.ready) continue;
    const keys = buckets.get(work.priority) ?? new Map();
    const oldest = keys.get(work.fairness_key);
    if (!oldest || fifoCompare(work, oldest.work) < 0) keys.set(work.fairness_key, { work, dependency });
    buckets.set(work.priority, keys);
  }
  if (!buckets.size) return { reason: pending ? "no_eligible_work" : "no_work", observations: [] };
  const schedule = copySchedule(state.clocks.get(pool) ?? { classes: clock(), keys: new Map() });
  const scales = policyScales(state.policy);
  const classWeights = new Map(state.policy.class_weights.map((weight, index) => [index + 1, weight]));
  let priority;
  if (state.policy.mode === "strict-priority") priority = Math.min(...buckets.keys());
  else {
    const selected = pick(schedule.classes, [...buckets.keys()], classWeights, scales.classes, (a, b) => a - b);
    priority = selected.key;
    schedule.classes = selected.clock;
  }
  const keys = buckets.get(priority);
  const selected = pick(schedule.keys.get(priority) ?? clock(), [...keys.keys()], new Map(Object.entries(state.policy.accounting_weights)), scales.keys);
  schedule.keys.set(priority, selected.clock);
  const winner = keys.get(selected.key);
  return {
    work_id: winner.work.work_id,
    reason: "selected",
    observations: [...winner.dependency.observations],
    class_pass: schedule.classes.virtual.toString(),
    key_pass: selected.clock.virtual.toString(),
    nextClock: schedule,
  };
}

function validateDispatchParameters(parameters, state) {
  closed(parameters, ["pool", "max_claims", "max_dispatches", "max_bytes"], [], "dispatch_next parameters");
  const { poolPolicy } = require("./work_queue_policy.cjs");
  poolPolicy(state, parameters.pool);
  integer(parameters.max_claims, 1, 256, "max_claims");
  integer(parameters.max_dispatches, 1, 256, "max_dispatches");
  integer(parameters.max_bytes, 1, 48 * 1024, "max_bytes");
}

function assignmentClaim(state, work, claim) {
  const { resultReferences } = require("./work_queue_graph.cjs");
  return { handle: claim.handle, claim_id: claim.claim_id, work_id: work.work_id, work: work.payload, result_refs: resultReferences(state, work) };
}

function groupCompatible(state, dispatch, work) {
  if (dispatch.worker_profile !== work.worker_profile || dispatch.pool !== work.pool) return false;
  const pool = state.policy.pools[work.pool];
  const profile = pool.profiles[work.worker_profile];
  if (profile.trust_domain !== work.batch_trust_domain) return false;
  const first = state.works.get(dispatch.claims[0].work_id);
  return first.batch_trust_domain === work.batch_trust_domain && (profile.share_keys || first.fairness_key === work.fairness_key);
}

function applyScheduledClaim(state, operation, selection, commit) {
  if (operation.work_id !== selection.work_id) throw queueError("selection_invalid", "Claim is not the next fair winner");
  const work = state.works.get(operation.work_id);
  if (state.claims.has(operation.claim_id)) throw queueError("claim_conflict", "Claim identity already exists");
  let dispatch = state.dispatches.get(operation.dispatch_id);
  if (dispatch && (dispatch.commit_id !== commit.id || dispatch.request_id !== commit.request.id || !groupCompatible(state, dispatch, work))) throw queueError("packing_invalid", "membership is immutable and group must be compatible");
  if (!dispatch) {
    dispatch = {
      version: 3,
      dispatch_id: operation.dispatch_id,
      request_id: commit.request.id,
      commit_id: commit.id,
      policy_epoch: state.policy_epoch,
      pool: work.pool,
      worker_profile: work.worker_profile,
      claims: [],
      profile: structuredClone(state.policy.pools[work.pool].profiles[work.worker_profile]),
      state: "reserved",
      released: false,
      lifecycle_writes: 0,
    };
    state.dispatches.set(dispatch.dispatch_id, dispatch);
  }
  dispatch.claims.push(assignmentClaim(state, work, operation));
  state.claims.set(operation.claim_id, { ...operation, state: "open", commit_id: commit.id, request_id: commit.request.id });
  work.state = "claimed";
  work.claim_id = operation.claim_id;
  work.attempts++;
  state.clocks.set(work.pool, selection.nextClock);
}

function planDispatch(state, parameters, { requestId, commitId, at, precedingOperations = 0 }) {
  validateDispatchParameters(parameters, state);
  integer(precedingOperations, 0, state.policy.limits.operations, "preceding operation count");
  const working = structuredClone(state);
  const operations = [];
  const groups = [];
  const policy = state.policy.pools[parameters.pool];
  const commit = { id: commitId, request: { id: requestId } };
  const prefix = createHash("sha256").update(requestId, "utf8").digest("hex");
  const limit = Math.min(parameters.max_claims, state.policy.limits.operations - precedingOperations);
  let next = planNext(working, parameters.pool, at);
  let lastSelection = next;
  let reason = next.reason;
  while (operations.length < limit && next.work_id) {
    const work = working.works.get(next.work_id);
    const profile = policy.profiles[work.worker_profile];
    let group;
    let claim;
    const claimId = `c_${prefix}_${operations.length + 1}`;
    for (const candidate of groups) {
      if (!groupCompatible(working, candidate, work) || candidate.claims.length >= profile.max_claims) continue;
      const proposed = { kind: "Claim", work_id: work.work_id, claim_id: claimId, dispatch_id: candidate.dispatch_id, handle: `h${candidate.claims.length + 1}`, observations: next.observations };
      const claims = [...candidate.claims, assignmentClaim(working, work, proposed)];
      if (canonicalBytes({ ...assignmentOnly(candidate), claims }) <= Math.min(parameters.max_bytes, state.policy.limits.assignment_bytes)) {
        group = candidate;
        claim = proposed;
        break;
      }
    }
    if (!group) {
      if (groups.length >= parameters.max_dispatches) {
        reason = "dispatch_budget_blocked";
        break;
      }
      if (reservationCounts(working, parameters.pool, "").native >= policy.native_limit) {
        reason = "native_capacity_blocked";
        break;
      }
      const dispatchId = `d_${prefix}_${groups.length + 1}`;
      claim = { kind: "Claim", work_id: work.work_id, claim_id: claimId, dispatch_id: dispatchId, handle: "h1", observations: next.observations };
      group = { version: 3, dispatch_id: dispatchId, request_id: requestId, commit_id: commitId, policy_epoch: state.policy_epoch, pool: work.pool, worker_profile: work.worker_profile, claims: [assignmentClaim(working, work, claim)] };
      if (canonicalBytes(group) > Math.min(parameters.max_bytes, state.policy.limits.assignment_bytes)) {
        reason = "assignment_bytes_blocked";
        break;
      }
      applyScheduledClaim(working, claim, next, commit);
      groups.push(working.dispatches.get(dispatchId));
    } else applyScheduledClaim(working, claim, next, commit);
    operations.push(claim);
    lastSelection = next;
    next = planNext(working, parameters.pool, at);
    reason = next.reason;
  }
  if (operations.length === limit) {
    reason = limit ? (limit === parameters.max_claims ? "claim_budget_reached" : "operation_budget_reached") : "operation_budget_blocked";
    next = lastSelection;
  } else if (operations.length && reason === "no_work") reason = "prefix_complete";
  return { tip: state.tip, operations, assignments: groups.map(assignmentOnly), reason, next: selectionOnly(next) };
}

function assignmentOnly(dispatch) {
  const fields = ["version", "dispatch_id", "request_id", "commit_id", "policy_epoch", "pool", "worker_profile", "claims"];
  return Object.fromEntries(fields.map(field => [field, dispatch[field]]));
}

function assignmentForDispatch(state, dispatchId) {
  const dispatch = state.dispatches.get(dispatchId);
  if (!dispatch) throw queueError("dispatch_missing", "requested Dispatch does not exist");
  const assignment = structuredClone(assignmentOnly(dispatch));
  function freeze(value) {
    if (value && typeof value === "object") {
      for (const child of Object.values(value)) freeze(child);
      Object.freeze(value);
    }
    return value;
  }
  return freeze(assignment);
}

function assignmentsForRequest(state, requestId) {
  return [...state.dispatches.values()].filter(dispatch => dispatch.request_id === requestId).map(dispatch => structuredClone(assignmentOnly(dispatch)));
}

function selectionOnly(selection) {
  const { nextClock, ...view } = selection;
  return view;
}

function dispatchNext(state, parameters, context) {
  return planDispatch(state, parameters, context);
}

module.exports = {
  applyScheduledClaim,
  assignmentForDispatch,
  assignmentOnly,
  assignmentsForRequest,
  clock,
  copySchedule,
  diagnostics,
  dispatchNext,
  eligibility,
  fifoCompare,
  groupCompatible,
  pick,
  planDispatch,
  planNext,
  reservationCounts,
  selectionOnly,
  tickScale,
  validateDispatchParameters,
};
