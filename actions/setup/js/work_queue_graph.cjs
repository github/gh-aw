// @ts-check
"use strict";

const { canonical, canonicalBytes, closed, digest, identity, integer, queueError, utf8Compare } = require("./work_queue_codec.cjs");
const { ASSIGNMENT_BYTES, EFFECTS_BYTES, GRAPH_NODES, MAX_PENDING_WORK, PAYLOAD_BYTES, PREDECESSORS, boundedBytes } = require("./work_queue_limits.cjs");
const { decimal } = require("./work_queue_policy.cjs");
const { expectedGitHubHost } = require("./work_queue_host.cjs");

const WORK_REQUIRED = ["kind", "work_id", "graph_id", "node_key", "pool", "priority", "fairness_key", "worker_profile", "payload", "depends_on"];
const WORK_OPTIONAL = ["subject", "backing_issue", "replacement_of"];
const RESOURCE_FIELDS = ["kind", "host", "repository", "repository_id", "resource_id", "number"];

function nodeId(graph_id, node_key) {
  return digest({ graph_id, node_key });
}

function gateKey(resource, condition) {
  return canonical({ host: resource.host, repository_id: resource.repository_id, resource_id: resource.resource_id, kind: resource.kind, condition });
}

function issueKey(resource) {
  return canonical([resource.host, resource.repository_id, resource.resource_id]);
}

function validateResource(resource) {
  closed(resource, RESOURCE_FIELDS, [], "resource", "resource_invalid");
  if (!["issue", "pull_request"].includes(resource.kind)) throw queueError("resource_invalid", "resource kind must be Issue or Pull Request");
  for (const field of ["host", "repository"]) identity(resource[field], field);
  if (resource.host !== expectedGitHubHost() || !/^[^/\s]+\/[^/\s]+$/.test(resource.repository)) throw queueError("resource_unauthorized", "resource host/repository must be the configured GitHub server and owner/name");
  for (const field of ["repository_id", "resource_id", "number"]) decimal(resource[field], field, "resource_invalid");
  return resource;
}

function validateDependency(dependency) {
  if (dependency?.kind === "work") {
    closed(dependency, ["kind", "work_id"], [], "Work edge", "dependency_invalid");
    identity(dependency.work_id, "predecessor Work");
    return;
  }
  closed(dependency, ["kind", "resource", "condition"], [], "external edge", "dependency_invalid");
  validateResource(dependency.resource);
  if (dependency.kind !== dependency.resource.kind || !(dependency.kind === "issue" ? ["completed", "closed"] : ["merged"]).includes(dependency.condition)) {
    throw queueError("dependency_invalid", "typed external condition does not match resource");
  }
}

function edgeKey(dependency) {
  return dependency.kind === "work" ? `work:${dependency.work_id}` : `gate:${gateKey(dependency.resource, dependency.condition)}`;
}

/** Structural validation of one Work operation; admission against state is validateGraphAdmission. */
function validateWork(node) {
  closed(node, WORK_REQUIRED, WORK_OPTIONAL, "Work", "request_invalid");
  if (node.kind !== "Work") throw queueError("request_invalid", "graph admission requires Work operations");
  for (const field of ["work_id", "graph_id", "node_key", "pool", "worker_profile"]) identity(node[field], field);
  if (node.work_id !== nodeId(node.graph_id, node.node_key)) throw queueError("work_identity_invalid", "Work identity must bind graph_id/node_key");
  integer(node.priority, 1, 5, "priority");
  identity(node.fairness_key, "fairness key", true, 128);
  if (!node.payload || typeof node.payload !== "object" || Array.isArray(node.payload)) throw queueError("work_invalid", "payload must be a JSON object");
  boundedBytes(node.payload, PAYLOAD_BYTES, "Work payload");
  if (!Array.isArray(node.depends_on) || node.depends_on.length > PREDECESSORS) throw queueError("resource_limit", `Work may declare at most ${PREDECESSORS} predecessors`);
  const edges = new Set();
  let workEdges = 0;
  for (const dependency of node.depends_on) {
    validateDependency(dependency);
    const key = edgeKey(dependency);
    if (edges.has(key)) throw queueError("dependency_invalid", "duplicate predecessor");
    edges.add(key);
    if (dependency.kind === "work") workEdges++;
  }
  if (Object.hasOwn(node, "subject")) validateResource(node.subject);
  if (Object.hasOwn(node, "backing_issue") && validateResource(node.backing_issue).kind !== "issue") throw queueError("resource_invalid", "backing Issue must be an Issue");
  if (Object.hasOwn(node, "replacement_of")) {
    closed(node.replacement_of, ["work_id"], [], "replacement", "request_invalid");
    identity(node.replacement_of.work_id, "replaced Work");
    if (node.replacement_of.work_id === node.work_id) throw queueError("work_invalid", "Work cannot replace itself");
  }
  // A worker receives its payload plus each predecessor's recorded effects.
  if (canonicalBytes(node.payload) + workEdges * EFFECTS_BYTES > ASSIGNMENT_BYTES) throw queueError("assignment_limit", "Work payload and predecessor effects cannot fit one assignment");
  return node;
}

function workDefinition(work) {
  return Object.fromEntries([...WORK_REQUIRED, ...WORK_OPTIONAL].filter(field => Object.hasOwn(work, field)).map(field => [field, work[field]]));
}

function pendingWork(work) {
  return work.state === "available" || work.state === "claimed";
}

const admissionIndexes = new WeakMap();

// Derived admission bookkeeping; rebuilt from state.works whenever absent.
function admissionIndex(state) {
  let index = admissionIndexes.get(state);
  if (!index) {
    index = { graphs: new Map(), pending: new Map(), issues: new Map() };
    for (const work of state.works.values()) indexAdmittedWork(index, work);
    admissionIndexes.set(state, index);
  }
  return index;
}

function indexAdmittedWork(index, work) {
  let graph = index.graphs.get(work.graph_id);
  if (!graph) index.graphs.set(work.graph_id, (graph = { pool: work.pool, vertices: new Set() }));
  graph.vertices.add(`work:${work.work_id}`);
  for (const edge of work.depends_on) if (edge.kind !== "work") graph.vertices.add(edgeKey(edge));
  if (pendingWork(work)) index.pending.set(work.pool, (index.pending.get(work.pool) ?? 0) + 1);
  if (work.backing_issue) index.issues.set(issueKey(work.backing_issue), work.work_id);
}

/** Record a Work state transition in the admission index. */
function trackWorkState(state, work, previousState) {
  const index = admissionIndexes.get(state);
  if (!index) return;
  const before = previousState === "available" || previousState === "claimed";
  const after = pendingWork(work);
  if (before !== after) index.pending.set(work.pool, (index.pending.get(work.pool) ?? 0) + (after ? 1 : -1));
}

function trackBackingIssue(state, work) {
  const index = admissionIndexes.get(state);
  if (index && work.backing_issue) index.issues.set(issueKey(work.backing_issue), work.work_id);
}

function backingIssueOwner(state, resource) {
  return admissionIndex(state).issues.get(issueKey(resource));
}

/**
 * Validate new Work against the projection: DAG shape, graph bounds and the
 * per-pool pending ceiling. Existing Work is never re-validated (idempotent).
 * @param {{works: Map<string, any>}} state
 * @param {Array<any>} nodes new Work operations, none already admitted
 */
function validateGraphAdmission(state, nodes) {
  const index = admissionIndex(state);
  const admitted = new Map();
  for (const node of nodes) {
    validateWork(node);
    if (admitted.has(node.work_id)) throw queueError("graph_invalid", "duplicate node key within one submission");
    admitted.set(node.work_id, node);
    if (node.backing_issue) {
      const owner = index.issues.get(issueKey(node.backing_issue));
      if (owner !== undefined && owner !== node.work_id) throw queueError("issue_binding_conflict", "one Work per backing Issue");
      for (const other of admitted.values()) if (other !== node && other.backing_issue && issueKey(other.backing_issue) === issueKey(node.backing_issue)) throw queueError("issue_binding_conflict", "one Work per backing Issue");
    }
  }
  const candidate = id => admitted.get(id) || state.works.get(id);
  const graphVertices = new Map();
  const graphPools = new Map();
  const pending = new Map();
  for (const node of admitted.values()) {
    const existing = index.graphs.get(node.graph_id);
    const vertices = graphVertices.get(node.graph_id) ?? new Set(existing?.vertices);
    vertices.add(`work:${node.work_id}`);
    for (const edge of node.depends_on) {
      if (edge.kind === "work") {
        const parent = candidate(edge.work_id);
        if (!parent || parent.graph_id !== node.graph_id || parent.pool !== node.pool) throw queueError("dependency_invalid", `missing, foreign, or self predecessor of ${node.node_key}`);
      } else vertices.add(edgeKey(edge));
    }
    graphVertices.set(node.graph_id, vertices);
    const pool = graphPools.get(node.graph_id) ?? existing?.pool;
    if (pool !== undefined && pool !== node.pool) throw queueError("graph_invalid", "graph must stay in one pool");
    graphPools.set(node.graph_id, node.pool);
    pending.set(node.pool, (pending.get(node.pool) ?? index.pending.get(node.pool) ?? 0) + 1);
  }
  for (const vertices of graphVertices.values()) if (vertices.size > GRAPH_NODES) throw queueError("resource_limit", `graph exceeds ${GRAPH_NODES} Work and gate vertices`);
  for (const count of pending.values()) if (count > MAX_PENDING_WORK) throw queueError("resource_limit", `pool exceeds ${MAX_PENDING_WORK} pending Work`);
  const colors = new Map();
  for (const id of [...admitted.keys()].sort(utf8Compare)) {
    if (colors.get(id) === 2) continue;
    const stack = [{ id, next: 0 }];
    colors.set(id, 1);
    while (stack.length) {
      const frame = stack[stack.length - 1];
      const edges = admitted.get(frame.id).depends_on;
      if (frame.next === edges.length) {
        colors.set(frame.id, 2);
        stack.pop();
        continue;
      }
      const edge = edges[frame.next++];
      // An admitted predecessor cannot point back into a new batch.
      if (edge.kind !== "work" || !admitted.has(edge.work_id) || colors.get(edge.work_id) === 2) continue;
      if (colors.get(edge.work_id) === 1) {
        const start = stack.findIndex(entry => entry.id === edge.work_id);
        const cycle = [...stack.slice(start).map(entry => entry.id), edge.work_id].map(member => candidate(member).node_key);
        throw queueError("dependency_cycle", cycle.join(" -> "));
      }
      colors.set(edge.work_id, 1);
      stack.push({ id: edge.work_id, next: 0 });
    }
  }
  return nodes;
}

/** Record newly admitted Work in the admission index (after validateGraphAdmission). */
function trackAdmittedWork(state, work) {
  const index = admissionIndexes.get(state);
  if (index) indexAdmittedWork(index, work);
}

/** An external edge is satisfied only by a ready Observation of its exact gate. */
function observationSatisfies(observation) {
  return observation?.state === "ready";
}

/**
 * @returns {{ready: boolean, reason: string, work_id?: string}}
 */
function dependencyStatus(state, work) {
  for (const edge of work.depends_on) {
    if (edge.kind === "work") {
      const parent = state.works.get(edge.work_id);
      if (!parent || parent.state === "cancelled") return { ready: false, reason: "dependency_failed", work_id: edge.work_id };
      if (parent.state !== "completed") return { ready: false, reason: "dependency_pending", work_id: edge.work_id };
    } else {
      const observation = state.observations.get(gateKey(edge.resource, edge.condition));
      if (!observation) return { ready: false, reason: "external_unobserved" };
      if (!observationSatisfies(observation)) return { ready: false, reason: "external_waiting" };
    }
  }
  return { ready: true, reason: "ready" };
}

function resultReferences(state, work) {
  return work.depends_on
    .filter(edge => edge.kind === "work")
    .map(edge => {
      const parent = state.works.get(edge.work_id);
      if (!parent || parent.state !== "completed") throw queueError("dependency_pending", "predecessor Work has not completed");
      return { work_id: parent.work_id, result_commit_id: parent.completion_id, descriptor: parent.effects };
    });
}

module.exports = {
  WORK_OPTIONAL,
  WORK_REQUIRED,
  backingIssueOwner,
  dependencyStatus,
  gateKey,
  issueKey,
  nodeId,
  observationSatisfies,
  resultReferences,
  trackAdmittedWork,
  trackBackingIssue,
  trackWorkState,
  validateDependency,
  validateGraphAdmission,
  validateResource,
  validateWork,
  workDefinition,
};
