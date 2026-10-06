// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const { canonical, closed, digest, identity, integer, parseStrictJSON } = require("./work_queue_codec.cjs");
const { actorFromContext, poolPolicy } = require("./work_queue_policy.cjs");
const { nodeId } = require("./work_queue_graph.cjs");
const { newRequest } = require("./work_queue_replay.cjs");

const MAX_INTENT_BYTES = 4 * 1024 * 1024;
const MAX_INTENTS = 256;
const INTENT_KINDS = ["submit", "dispatch_next", "finish"];

function readStagedIntentBatch(filename) {
  if (!fs.existsSync(filename)) return { intents: [], errors: [] };
  const stat = fs.statSync(filename);
  if (!stat.isFile() || stat.size > MAX_INTENT_BYTES) throw new Error("work_queue_intent_limit");
  const bytes = fs.readFileSync(filename);
  if (bytes.length > MAX_INTENT_BYTES) throw new Error("work_queue_intent_limit");
  let text;
  try {
    text = new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes);
  } catch {
    throw new Error("work_queue_intent_transport_invalid");
  }
  const lines = text.split("\n");
  if (lines.filter(line => line.trim()).length > MAX_INTENTS) throw new Error("work_queue_intent_limit");
  const ids = new Map();
  const invalid = new Set();
  const errors = [];
  for (const [index, line] of lines.entries()) {
    if (!line.trim()) continue;
    let intentId;
    try {
      const intent = parseStrictJSON(line);
      intentId = identity(intent?.intent_id, "intent ID");
      closed(intent, ["version", "intent_id", "kind", "parameters"], ["claim_handle"], "staged queue intent");
      if (intent.version !== 3 || !INTENT_KINDS.includes(intent.kind)) throw new Error("work_queue_intent_invalid");
      if (Object.hasOwn(intent, "claim_handle")) identity(intent.claim_handle, "Claim handle");
      const previous = ids.get(intentId);
      if (previous && canonical(previous.intent) !== canonical(intent)) {
        invalid.add(intentId);
        errors.push({ line: index + 1, intent_id: intentId, reason: "work_queue_intent_conflict" });
      } else if (!previous) {
        ids.set(intentId, { intent, line: index + 1 });
      }
    } catch {
      if (intentId !== undefined) invalid.add(intentId);
      errors.push({ line: index + 1, ...(intentId === undefined ? {} : { intent_id: intentId }), reason: "work_queue_intent_invalid" });
    }
  }
  for (const id of invalid) {
    const previous = ids.get(id);
    if (previous) errors.push({ line: previous.line, intent_id: id, reason: "work_queue_intent_conflict" });
  }
  return { intents: [...ids.values()].filter(entry => !invalid.has(entry.intent.intent_id)).map(entry => entry.intent), errors: errors.sort((left, right) => left.line - right.line) };
}

function readStagedIntents(filename) {
  const batch = readStagedIntentBatch(filename);
  if (batch.errors.length) throw new Error(batch.errors.some(error => error.reason === "work_queue_intent_conflict") ? "work_queue_intent_conflict" : "work_queue_intent_invalid");
  return batch.intents;
}

function stageIntent(filename, intent) {
  const previous = readStagedIntents(filename);
  if (previous.length >= MAX_INTENTS || Buffer.byteLength(canonical(intent)) + (fs.existsSync(filename) ? fs.statSync(filename).size : 0) > MAX_INTENT_BYTES) throw new Error("work_queue_intent_limit");
  fs.mkdirSync(path.dirname(filename), { recursive: true });
  fs.appendFileSync(filename, `${canonical(intent)}\n`, { mode: 0o644, encoding: "utf8" });
  fs.chmodSync(filename, 0o644);
}

function requestIdForIntent(trustedContext, intentId) {
  const actor = actorFromContext(trustedContext);
  identity(intentId, "intent ID");
  return `request:${digest({ origin: actor, intent_id: intentId })}`;
}

function requestForIntent(trustedContext, intentId, kind, parameters) {
  return newRequest(requestIdForIntent(trustedContext, intentId), kind, actorFromContext(trustedContext), parameters);
}

function normalizeDispatchParameters(parameters, policy, dispatchBudget) {
  closed(parameters, ["pool", "max_claims", "max_dispatches"], [], "dispatch_next intent");
  identity(parameters.pool, "pool");
  const pool = poolPolicy({ policy }, parameters.pool);
  integer(parameters.max_claims, 1, Math.min(policy.limits.operations, pool.logical_limit), "max_claims");
  integer(parameters.max_dispatches, 1, Math.min(4096, pool.native_limit), "max_dispatches");
  integer(dispatchBudget, 0, 4096, "remaining run dispatch budget");
  if (parameters.max_dispatches > dispatchBudget) throw new Error("work_queue_dispatch_budget_exceeded");
  return { ...parameters, max_bytes: policy.limits.assignment_bytes };
}

function normalizeSubmitParameters(parameters, policy, at, state) {
  closed(parameters, ["nodes"], [], "submit intent");
  if (!Array.isArray(parameters.nodes) || parameters.nodes.length < 1 || parameters.nodes.length > Math.min(256, policy.limits.graph_nodes)) throw new Error("work_queue_graph_limit");
  integer(at, 0, Number.MAX_SAFE_INTEGER, "submission timestamp");
  return {
    nodes: parameters.nodes.map(node => {
      closed(node, ["payload"], ["graph_id", "node_key", "work_id", "pool", "priority", "fairness_key", "worker_profile", "depends_on", "subject", "replacement_of"], "submitted Work");
      const graphId = Object.hasOwn(node, "graph_id") ? identity(node.graph_id, "graph ID") : digest(node.payload);
      const nodeKey = Object.hasOwn(node, "node_key") ? identity(node.node_key, "node key") : "root";
      const poolName = node.pool === undefined ? "default" : node.pool;
      const pool = poolPolicy({ policy }, poolName);
      const profileName = node.worker_profile === undefined ? pool.default_profile : node.worker_profile;
      const profile = pool.profiles[profileName];
      if (!profile) throw new Error("work_queue_profile_not_approved");
      const workId = nodeId(graphId, nodeKey);
      if (node.work_id !== undefined && node.work_id !== workId) throw new Error("work_queue_work_identity_invalid");
      return {
        kind: "Work",
        work_id: workId,
        graph_id: graphId,
        node_key: nodeKey,
        pool: poolName,
        priority: node.priority === undefined ? 3 : node.priority,
        fairness_key: node.fairness_key === undefined ? "" : node.fairness_key,
        worker_profile: profileName,
        batch_trust_domain: profile.trust_domain,
        payload: node.payload,
        depends_on: node.depends_on === undefined ? [] : node.depends_on,
        enqueued: state?.works.get(workId)?.enqueued ?? at,
        ...(node.subject === undefined ? {} : { subject: node.subject }),
        ...(node.replacement_of === undefined ? {} : { replacement_of: node.replacement_of }),
      };
    }),
  };
}

module.exports = { MAX_INTENT_BYTES, MAX_INTENTS, readStagedIntentBatch, readStagedIntents, stageIntent, requestIdForIntent, requestForIntent, normalizeDispatchParameters, normalizeSubmitParameters };
