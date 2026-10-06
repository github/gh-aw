// @ts-check
"use strict";

const { canonical, canonicalBytes, closed, digest, identity, integer, queueError, utf8Compare } = require("./work_queue_codec.cjs");
const { boundedBytes } = require("./work_queue_limits.cjs");
const { decimal, poolPolicy, workerContinuationAuthority } = require("./work_queue_policy.cjs");

function nodeId(graph_id, node_key) {
  return digest({ graph_id, node_key });
}

function gateKey(resource, condition) {
  return canonical({ host: resource.host, repository_id: resource.repository_id, resource_id: resource.resource_id, kind: resource.kind, condition });
}

function validateResource(resource) {
  closed(resource, ["kind", "host", "repository", "repository_id", "resource_id", "number"], [], "resource");
  if (!["issue", "pull_request"].includes(resource.kind)) throw queueError("dependency_invalid", "resource kind must be Issue or Pull Request");
  for (const field of ["host", "repository"]) identity(resource[field], field);
  if (resource.host !== "github.com" || !/^[^/\s]+\/[^/\s]+$/.test(resource.repository)) throw queueError("dependency_invalid", "resource host/repository must be approved normalized GitHub coordinates");
  for (const field of ["repository_id", "resource_id", "number"]) decimal(resource[field], field);
  return resource;
}

function validateDependency(dependency) {
  if (dependency?.kind === "work") {
    closed(dependency, ["kind", "work_id"], [], "Work edge");
    identity(dependency.work_id, "predecessor Work");
  } else {
    closed(dependency, ["kind", "resource", "condition"], [], "external edge");
    validateResource(dependency.resource);
    if (dependency.kind !== dependency.resource.kind || !(dependency.kind === "issue" ? ["completed", "closed"] : ["merged"]).includes(dependency.condition)) {
      throw queueError("dependency_invalid", "typed external condition does not match resource");
    }
  }
}

function newWork(payload, graph_id, node_key, pool, policy, at) {
  const configured = policy.pools[pool];
  if (!configured) throw queueError("pool_invalid", "pool is not approved");
  return {
    kind: "Work",
    work_id: nodeId(graph_id, node_key),
    graph_id,
    node_key,
    pool,
    priority: 3,
    fairness_key: "",
    worker_profile: configured.default_profile,
    batch_trust_domain: configured.profiles[configured.default_profile].trust_domain,
    payload,
    depends_on: [],
    enqueued: at,
  };
}

function validateWork(node, state, actor) {
  closed(node, ["kind", "work_id", "graph_id", "node_key", "pool", "priority", "fairness_key", "worker_profile", "batch_trust_domain", "payload", "depends_on", "enqueued"], ["subject", "replacement_of"], "Work");
  if (node.kind !== "Work") throw queueError("unsupported_protocol", "graph admission requires Work operations");
  for (const field of ["work_id", "graph_id", "node_key", "pool", "worker_profile", "batch_trust_domain"]) identity(node[field], field);
  if (node.work_id !== nodeId(node.graph_id, node.node_key)) throw queueError("work_identity_invalid", "Work identity must bind graph_id/node_key");
  integer(node.priority, 1, 5, "priority");
  identity(node.fairness_key, "fairness key", true, 128);
  integer(node.enqueued, 0, Number.MAX_SAFE_INTEGER, "enqueue timestamp");
  const pool = poolPolicy(state, node.pool);
  const profile = pool.profiles[node.worker_profile];
  if (!profile || profile.trust_domain !== node.batch_trust_domain || !Object.hasOwn(state.policy.accounting_weights, node.fairness_key)) throw queueError("work_unauthorized", "unapproved routing, trust domain, or accounting key");
  if (actor.role === "worker") {
    const { work: parent } = workerContinuationAuthority(state, actor);
    if (node.pool !== parent.pool || node.priority !== parent.priority || node.fairness_key !== parent.fairness_key) throw queueError("work_unauthorized", "children preserve trusted parent priority and accounting scope");
  } else {
    const rule = state.policy.producers[actor.principal];
    if (!rule || !rule.pools.includes(node.pool) || !rule.priorities.includes(node.priority) || !rule.fairness_keys.includes(node.fairness_key)) throw queueError("work_unauthorized", "producer lacks immutable submission entitlement");
  }
  if (!node.payload || typeof node.payload !== "object" || Array.isArray(node.payload)) throw queueError("work_invalid", "payload must be a JSON object");
  boundedBytes(node.payload, state.policy.limits.payload_bytes, "Work payload");
  if (!Array.isArray(node.depends_on) || node.depends_on.length > state.policy.limits.predecessors) throw queueError("resource_limit", "predecessor count exceeds policy");
  const edges = new Set();
  for (const dependency of node.depends_on) {
    validateDependency(dependency);
    const key = dependency.kind === "work" ? `work:${dependency.work_id}` : `gate:${gateKey(dependency.resource, dependency.condition)}`;
    if (edges.has(key)) throw queueError("dependency_invalid", "duplicate predecessor");
    edges.add(key);
    if (dependency.kind !== "work" && !pool.allowed_repositories.includes(dependency.resource.repository)) throw queueError("dependency_unauthorized", "external repository is not allowlisted");
  }
  if (Object.hasOwn(node, "subject")) {
    validateResource(node.subject);
    if (!pool.allowed_repositories.includes(node.subject.repository)) throw queueError("dependency_unauthorized", "subject repository is not allowlisted");
  }
  if (Object.hasOwn(node, "replacement_of")) {
    closed(node.replacement_of, ["work_id", "disposition", "evidence"], [], "replacement");
    identity(node.replacement_of.evidence, "remediation evidence");
    const failed = state.works.get(node.replacement_of.work_id);
    if (!failed || failed.barrier !== "failed" || !["idempotent", "compensated", "inspection"].includes(node.replacement_of.disposition))
      throw queueError("replacement_invalid", "replacement requires terminal DeliveryFailure and trusted safe remediation");
    if (failed.pool !== node.pool || failed.graph_id !== node.graph_id || failed.priority !== node.priority || failed.fairness_key !== node.fairness_key || failed.work_id === node.work_id)
      throw queueError("replacement_invalid", "replacement must preserve accounting scope and use a new identity");
  }
  // Provenance and declared Result inputs also consume the host input budget.
  const worstIdentity = "\\".repeat(256);
  const minimal = {
    version: 3,
    dispatch_id: "d".repeat(70),
    request_id: worstIdentity,
    commit_id: worstIdentity,
    policy_epoch: state.policy_epoch,
    pool: node.pool,
    worker_profile: node.worker_profile,
    claims: [{ handle: "h16", claim_id: "c".repeat(70), work_id: node.work_id, work: node.payload, result_refs: [] }],
  };
  let resultBytes = 0;
  for (const edge of node.depends_on)
    if (edge.kind === "work") {
      const parent = state.works.get(edge.work_id);
      const reference = { work_id: edge.work_id, result_commit_id: worstIdentity, descriptor: {} };
      if (parent?.barrier === "verified") {
        reference.result_commit_id = parent.result_commit_id;
        reference.descriptor = parent.result;
      } else resultBytes += Math.max(0, state.policy.limits.result_bytes - 2);
      minimal.claims[0].result_refs.push(reference);
    }
  if (canonicalBytes(minimal) + resultBytes > state.policy.limits.assignment_bytes) throw queueError("assignment_limit", "Work and bounded declared Result inputs cannot fit a single-Claim assignment");
}

function workDefinition(work) {
  const fields = ["kind", "work_id", "graph_id", "node_key", "pool", "priority", "fairness_key", "worker_profile", "batch_trust_domain", "payload", "depends_on", "enqueued", "subject", "replacement_of"];
  return Object.fromEntries(fields.filter(field => Object.hasOwn(work, field)).map(field => [field, work[field]]));
}

function validateGraphAdmission(state, nodes, actor) {
  const all = new Map(state.works);
  const seen = new Set();
  for (const node of nodes) {
    validateWork(node, state, actor);
    if (seen.has(node.work_id)) throw queueError("graph_invalid", "duplicate node key within one submission");
    seen.add(node.work_id);
    const existing = all.get(node.work_id);
    if (existing && canonical(workDefinition(existing)) !== canonical(node)) throw queueError("work_conflict", "immutable node definition differs");
    if (!existing && state.admission_paused) throw queueError("admission_paused", "new Work admission is paused");
    if (!existing) all.set(node.work_id, node);
  }
  const graphVertices = new Map();
  const graphPools = new Map();
  const pending = new Map();
  for (const node of all.values()) {
    const vertices = graphVertices.get(node.graph_id) ?? new Set();
    vertices.add(`work:${node.work_id}`);
    for (const edge of node.depends_on) {
      if (edge.kind === "work") {
        const parent = all.get(edge.work_id);
        if (!parent || parent.graph_id !== node.graph_id || parent.pool !== node.pool || parent.work_id === node.work_id) throw queueError("dependency_invalid", `missing, foreign, or self predecessor of ${node.node_key}`);
      } else vertices.add(`gate:${gateKey(edge.resource, edge.condition)}`);
    }
    graphVertices.set(node.graph_id, vertices);
    if (graphPools.has(node.graph_id) && graphPools.get(node.graph_id) !== node.pool) throw queueError("graph_invalid", "graph must stay in one pool");
    graphPools.set(node.graph_id, node.pool);
    if (!["completed", "cancelled"].includes(node.state) || node.barrier === "pending") pending.set(node.pool, (pending.get(node.pool) ?? 0) + 1);
  }
  for (const vertices of graphVertices.values()) if (vertices.size > state.policy.limits.graph_nodes) throw queueError("resource_limit", "graph counts Work and deduplicated Issue/PR gates");
  for (const count of pending.values()) if (count > state.policy.limits.pending_nodes) throw queueError("resource_limit", "pool pending-node bound exceeded");
  const colors = new Map();
  const path = [];
  function visit(id) {
    if (colors.get(id) === 2) return;
    if (colors.get(id) === 1) {
      const cycle = [...path.slice(path.indexOf(id)), id].map(id => all.get(id).node_key);
      throw queueError("dependency_cycle", cycle.join(" -> "));
    }
    colors.set(id, 1);
    path.push(id);
    for (const edge of all.get(id).depends_on) if (edge.kind === "work") visit(edge.work_id);
    path.pop();
    colors.set(id, 2);
  }
  for (const id of [...all.keys()].sort(utf8Compare)) visit(id);
  return nodes;
}

function observationSatisfies(observation) {
  if (observation.state !== "ready" || observation.read_status !== "ok") return false;
  if (observation.resource.kind === "issue") return observation.resource_state === "closed" && (observation.condition === "closed" || observation.state_reason === "completed");
  return observation.condition === "merged" && observation.merged === true && typeof observation.merge_commit === "string" && observation.merge_commit.length > 0;
}

function dependencyStatus(state, work, at) {
  const observations = [];
  for (const edge of work.depends_on) {
    if (edge.kind === "work") {
      const parent = state.works.get(edge.work_id);
      if (parent.state === "cancelled") return { ready: false, reason: "dependency_failed", work_id: parent.work_id, observations };
      if (parent.barrier === "failed") return { ready: false, reason: "dependency_delivery_failed", work_id: parent.work_id, observations };
      if (parent.barrier !== "verified") return { ready: false, reason: "dependency_result_unavailable", work_id: parent.work_id, observations };
    } else {
      const observation = state.observations.get(gateKey(edge.resource, edge.condition));
      if (!observation || observation.state === "unknown") return { ready: false, reason: "external_unavailable", observations };
      if (observation.credential_generation !== state.credential_generation || observation.observed_at > at || at - observation.observed_at > poolPolicy(state, work.pool).max_observation_age_ms)
        return { ready: false, reason: "observation_stale", observations };
      if (!observationSatisfies(observation)) return { ready: false, reason: "dependency_external_waiting", observations };
      observations.push(observation.observation_id);
    }
  }
  return { ready: true, reason: "ready", observations };
}

function resultReferences(state, work) {
  return work.depends_on
    .filter(edge => edge.kind === "work")
    .map(edge => {
      const parent = state.works.get(edge.work_id);
      if (parent.barrier !== "verified") throw queueError("dependency_result_unavailable", "declared Result is not verified");
      return { work_id: parent.work_id, result_commit_id: parent.result_commit_id, descriptor: parent.result };
    });
}

module.exports = { dependencyStatus, gateKey, newWork, nodeId, observationSatisfies, resultReferences, validateDependency, validateGraphAdmission, validateResource, validateWork, workDefinition };
