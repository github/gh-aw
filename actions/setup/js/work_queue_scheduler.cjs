// @ts-check
"use strict";
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("scheduler");

const { createHash } = require("node:crypto");
const { closed, identity, integer, queueError } = require("./work_queue_codec.cjs");
const { OPERATIONS_PER_COMMIT } = require("./work_queue_limits.cjs");
const { dependencyStatus, resultReferences } = require("./work_queue_graph.cjs");

const DISPATCH_PARAMETERS = ["pool", "max_claims", "max_dispatches", "concurrency", "per_account_limit", "profiles"];

function fifoCompare(a, b) {
  return a.position.commit - b.position.commit || a.position.operation - b.position.operation;
}

function validateDispatchParameters(parameters) {
  closed(parameters, DISPATCH_PARAMETERS, [], "dispatch_next parameters", "request_invalid");
  identity(parameters.pool, "pool");
  integer(parameters.max_claims, 1, OPERATIONS_PER_COMMIT, "max_claims");
  integer(parameters.max_dispatches, 1, OPERATIONS_PER_COMMIT, "max_dispatches");
  integer(parameters.concurrency, 1, 4096, "concurrency");
  integer(parameters.per_account_limit, 0, 4096, "per_account_limit");
  if (!parameters.profiles || typeof parameters.profiles !== "object" || Array.isArray(parameters.profiles)) throw queueError("request_invalid", "dispatch_next requires worker profiles");
}

/** Deterministic Claim/Dispatch identities: one Claim per Dispatch. */
function claimIdentity(requestId, workId) {
  const seed = createHash("sha256").update(`${requestId}\n${workId}`, "utf8").digest("hex");
  return { claim_id: `c_${seed}_1`, dispatch_id: `d_${seed}_1`, handle: "h1" };
}

/**
 * Eligible Work of a pool in dispatch order: priority ascending, then causal FIFO.
 * @param {any} state
 * @param {string} pool
 * @param {Record<string, unknown>} [profiles] only Work routed to one of these profiles
 */
function eligibleWork(state, pool, profiles = undefined) {
  const eligible = [];
  for (const work of state.works.values()) {
    if (work.pool !== pool || work.state !== "available") continue;
    if (profiles && !Object.hasOwn(profiles, work.worker_profile)) continue;
    if (!dependencyStatus(state, work).ready) continue;
    eligible.push(work);
  }
  return eligible.sort((a, b) => a.priority - b.priority || fifoCompare(a, b));
}

/** Non-closed Dispatches and open Claims per fairness key of one pool. */
function poolOccupancy(state, pool) {
  let dispatches = 0;
  const accounts = new Map();
  for (const dispatch of state.dispatches.values()) if (dispatch.pool === pool && dispatch.state !== "closed") dispatches++;
  for (const claim of state.claims.values()) {
    if (claim.state !== "open") continue;
    const work = state.works.get(claim.work_id);
    if (work.pool === pool) accounts.set(work.fairness_key, (accounts.get(work.fairness_key) ?? 0) + 1);
  }
  return { dispatches, accounts };
}

/**
 * Plan the Claims of one dispatch_next request. Pure: the projection is never mutated.
 * @param {any} state
 * @param {{pool: string, max_claims: number, max_dispatches: number, concurrency: number, per_account_limit: number, profiles: Record<string, {workflow: string}>}} parameters
 * @param {string} requestId
 * @returns {{operations: Array<{kind: "Claim", work_id: string, claim_id: string, dispatch_id: string, handle: string}>, dispatches: string[], reason: string}}
 */
function planDispatch(state, parameters, requestId) {
  validateDispatchParameters(parameters);
  identity(requestId, "request ID");
  const { pool, concurrency, per_account_limit: perAccountLimit } = parameters;
  const limit = Math.min(parameters.max_claims, parameters.max_dispatches);
  const occupancy = poolOccupancy(state, pool);
  const eligible = eligibleWork(state, pool, parameters.profiles);
  /** @type {Array<{kind: "Claim", work_id: string, claim_id: string, dispatch_id: string, handle: string}>} */
  const operations = [];
  let reason = eligible.length ? "prefix_complete" : [...state.works.values()].some(work => work.pool === pool && work.state === "available") ? "no_eligible_work" : "no_work";
  for (const work of eligible) {
    if (occupancy.dispatches >= concurrency) {
      reason = "capacity_blocked";
      break;
    }
    if (operations.length >= limit) {
      reason = "claim_budget_reached";
      break;
    }
    const held = occupancy.accounts.get(work.fairness_key) ?? 0;
    if (perAccountLimit > 0 && held >= perAccountLimit) {
      reason = "account_capacity_blocked";
      continue;
    }
    operations.push({ kind: "Claim", work_id: work.work_id, ...claimIdentity(requestId, work.work_id) });
    occupancy.dispatches++;
    occupancy.accounts.set(work.fairness_key, held + 1);
    if (reason === "account_capacity_blocked") reason = "prefix_complete";
  }
  log.debug("dispatch.plan.complete", { claims: operations.length, eligible: eligible.length, reason });
  return { operations, dispatches: operations.map(operation => operation.dispatch_id), reason };
}

/**
 * The worker's view of a Dispatch: its open Claims with payloads and predecessor effects.
 * @returns {{dispatch_id: string, claims: Array<{handle: string, claim_id: string, work_id: string, work: object, result_refs: Array<{work_id: string, result_commit_id: string, descriptor: unknown}>, continuation?: {route: string, message: string, remaining: number}}>}}
 */
function assignmentForDispatch(state, dispatchId) {
  const dispatch = state.dispatches.get(dispatchId);
  if (!dispatch) throw queueError("dispatch_missing", "requested Dispatch does not exist");
  return assignmentOnly(state, dispatch);
}

function assignmentOnly(state, dispatch) {
  const claims = dispatch.claims
    .map(id => state.claims.get(id))
    .filter(claim => claim.state === "open")
    .map(claim => {
      const work = state.works.get(claim.work_id);
      const member = { handle: claim.handle, claim_id: claim.claim_id, work_id: work.work_id, work: structuredClone(work.payload), result_refs: structuredClone(resultReferences(state, work)) };
      // A continuation's remaining-work context travels beside, never inside, its payload.
      return work.continuation ? { ...member, continuation: structuredClone(work.continuation) } : member;
    });
  return { dispatch_id: dispatch.dispatch_id, claims };
}

function assignmentsForRequest(state, requestId) {
  return [...state.dispatches.values()].filter(dispatch => dispatch.request_id === requestId).map(dispatch => assignmentOnly(state, dispatch));
}

/** A read-only summary of what dispatch_next would select now. */
function selectionOnly(decision) {
  return { reason: decision.reason, work_ids: decision.operations.map(operation => operation.work_id) };
}

module.exports = { assignmentForDispatch, assignmentOnly, assignmentsForRequest, claimIdentity, eligibleWork, fifoCompare, planDispatch, poolOccupancy, selectionOnly, validateDispatchParameters };
