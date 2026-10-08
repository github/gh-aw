// @ts-check
"use strict";

const { canonical } = require("./work_queue_codec.cjs");

// Indexes live only during checked replay and private scheduling simulations.
// Public projections remain freely mutable; their indexes are rebuilt on demand.
const tracked = new WeakMap();

function externalVertex(edge) {
  const resource = edge.resource;
  return `gate:${canonical({ host: resource.host, repository_id: resource.repository_id, resource_id: resource.resource_id, kind: resource.kind, condition: edge.condition })}`;
}

function createIndexes(state) {
  const indexes = {
    available: new Map(),
    unfinished: new Set(),
    openClaims: new Set(),
    reservations: new Set(),
    runBindings: new Map(),
    graphs: new Map(),
    pending: new Map(),
    workStatus: new Map(),
    maxGraphVertices: 0,
  };
  for (const work of state.works.values()) indexWork(indexes, work);
  for (const claim of state.claims.values()) if (claim.state === "open") indexes.openClaims.add(claim.claim_id);
  for (const dispatch of state.dispatches.values()) indexDispatch(indexes, dispatch);
  return indexes;
}

function indexesFor(state) {
  return tracked.get(state) || createIndexes(state);
}

function trackState(state) {
  tracked.set(state, createIndexes(state));
}

function untrackState(state) {
  tracked.delete(state);
}

function indexWork(indexes, work) {
  const previous = indexes.workStatus.get(work.work_id);
  if (!previous) {
    let graph = indexes.graphs.get(work.graph_id);
    if (!graph) {
      graph = { pool: work.pool, vertices: new Set() };
      indexes.graphs.set(work.graph_id, graph);
    }
    graph.vertices.add(`work:${work.work_id}`);
    for (const edge of work.depends_on) if (edge.kind !== "work") graph.vertices.add(externalVertex(edge));
    indexes.maxGraphVertices = Math.max(indexes.maxGraphVertices, graph.vertices.size);
  }
  const pending = !["completed", "cancelled"].includes(work.state) || work.barrier === "pending";
  if (pending !== (previous?.pending ?? false)) indexes.pending.set(work.pool, (indexes.pending.get(work.pool) || 0) + (pending ? 1 : -1));
  if (work.state === "available") {
    let available = indexes.available.get(work.pool);
    if (!available) indexes.available.set(work.pool, (available = new Set()));
    available.add(work.work_id);
  } else indexes.available.get(work.pool)?.delete(work.work_id);
  if (work.state !== "cancelled" && (work.state !== "completed" || work.barrier === "pending")) indexes.unfinished.add(work.work_id);
  else indexes.unfinished.delete(work.work_id);
  indexes.workStatus.set(work.work_id, { pending });
}

function indexDispatch(indexes, dispatch) {
  if (dispatch.released) indexes.reservations.delete(dispatch.dispatch_id);
  else indexes.reservations.add(dispatch.dispatch_id);
  if (dispatch.run) indexes.runBindings.set(dispatch.run.run_id, dispatch.dispatch_id);
}

function updateIndexes(state, operation) {
  const indexes = tracked.get(state);
  if (!indexes) return;
  const work = state.works.get(operation.work_id);
  if (work) indexWork(indexes, work);
  const claim = state.claims.get(operation.claim_id);
  if (claim) {
    if (claim.state === "open") indexes.openClaims.add(claim.claim_id);
    else indexes.openClaims.delete(claim.claim_id);
  }
  // Work cancellation can close its current Claim without a Claim selector.
  if (work?.claim_id) {
    const current = state.claims.get(work.claim_id);
    if (current && current.state !== "open") indexes.openClaims.delete(current.claim_id);
  }
  const dispatch = state.dispatches.get(operation.dispatch_id || claim?.dispatch_id);
  if (dispatch) indexDispatch(indexes, dispatch);
}

function schedulingState(state) {
  const indexes = indexesFor(state);
  const works = new Map();
  for (const id of indexes.unfinished) {
    const work = state.works.get(id);
    works.set(id, { ...work });
    for (const edge of work.depends_on) if (edge.kind === "work" && !works.has(edge.work_id)) works.set(edge.work_id, state.works.get(edge.work_id));
  }
  const working = {
    ...state,
    works,
    claims: new Map([...indexes.openClaims].map(id => [id, state.claims.get(id)])),
    dispatches: new Map(
      [...indexes.reservations].map(id => {
        const dispatch = state.dispatches.get(id);
        return [id, { ...dispatch, claims: [...dispatch.claims] }];
      })
    ),
    clocks: new Map(state.clocks),
    transactions: [],
  };
  trackState(working);
  return working;
}

module.exports = { externalVertex, indexesFor, schedulingState, trackState, untrackState, updateIndexes };
