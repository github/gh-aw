// @ts-check
"use strict";
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("intents");
const { iterateLines, iterateJsonlEvents } = require("./jsonl_helpers.cjs");

const fs = require("fs");
const path = require("path");
const { canonical, closed, digest, identity, integer, parseStrictJSON, queueError } = require("./work_queue_codec.cjs");
const { poolEnv } = require("./work_queue_policy.cjs");
const { nodeId } = require("./work_queue_graph.cjs");
const { OPERATIONS_PER_COMMIT } = require("./work_queue_limits.cjs");

const INTENT_VERSION = 4;
const MAX_INTENT_BYTES = 4 * 1024 * 1024;
const MAX_INTENTS = 256;
const INTENT_KINDS = ["submit", "dispatch_next"];
const FINISH_OUTCOMES = ["completed", "failed"];
// Fields a staged Work node may carry; the trusted publisher fills the rest.
const NODE_OPTIONAL = ["graph_id", "node_key", "work_id", "pool", "priority", "fairness_key", "worker_profile", "depends_on", "subject", "backing_issue", "replacement_of"];

/** @param {string} filename */
function readIntentText(filename) {
  if (!fs.existsSync(filename)) return "";
  const stat = fs.statSync(filename);
  if (!stat.isFile() || stat.size > MAX_INTENT_BYTES) throw new Error("work_queue_intent_limit");
  const bytes = fs.readFileSync(filename);
  if (bytes.length > MAX_INTENT_BYTES) throw new Error("work_queue_intent_limit");
  try {
    return new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(bytes);
  } catch {
    throw new Error("work_queue_intent_transport_invalid");
  }
}

function* readIntentLines(filename) {
  const text = readIntentText(filename);
  let nonblank = 0;
  for (const line of iterateLines(text)) if (line.trim() && ++nonblank > MAX_INTENTS) throw new Error("work_queue_intent_limit");
  log.debug("intents.read.complete", { lines: nonblank });
  yield* iterateLines(text);
}

/**
 * Parse staged control intents. Duplicate identical lines collapse; conflicting
 * lines for one intent ID are reported and dropped.
 * @param {string} filename
 */
function readStagedIntentBatch(filename) {
  const ids = new Map();
  const invalid = new Set();
  const errors = [];
  for (const event of iterateJsonlEvents(readIntentLines(filename))) {
    if (event.type === "invalid" && event.reason === "blank") continue;
    let intentId;
    try {
      const intent = parseStrictJSON(event.rawLine);
      intentId = identity(intent?.intent_id, "intent ID");
      closed(intent, ["version", "intent_id", "kind", "parameters"], ["claim_handle"], "staged queue intent");
      if (intent.version !== INTENT_VERSION || !INTENT_KINDS.includes(intent.kind)) throw new Error("work_queue_intent_invalid");
      if (Object.hasOwn(intent, "claim_handle")) identity(intent.claim_handle, "Claim handle");
      const previous = ids.get(intentId);
      if (previous && canonical(previous.intent) !== canonical(intent)) {
        invalid.add(intentId);
        errors.push({ line: event.lineNumber, intent_id: intentId, reason: "work_queue_intent_conflict" });
      } else if (!previous) ids.set(intentId, { intent, line: event.lineNumber });
    } catch (error) {
      log.failure("intent.parse.failed", error);
      if (intentId !== undefined) invalid.add(intentId);
      errors.push({ line: event.lineNumber, ...(intentId === undefined ? {} : { intent_id: intentId }), reason: "work_queue_intent_invalid" });
    }
  }
  for (const id of invalid) {
    const previous = ids.get(id);
    if (previous) errors.push({ line: previous.line, intent_id: id, reason: "work_queue_intent_conflict" });
  }
  const intents = [...ids.values()].filter(entry => !invalid.has(entry.intent.intent_id)).map(entry => entry.intent);
  return { intents, errors: errors.sort((left, right) => left.line - right.line) };
}

function readStagedIntents(filename) {
  const batch = readStagedIntentBatch(filename);
  if (batch.errors.length) throw new Error(batch.errors.some(error => error.reason === "work_queue_intent_conflict") ? "work_queue_intent_conflict" : "work_queue_intent_invalid");
  return batch.intents;
}

function appendLine(filename, line) {
  if (Buffer.byteLength(line) + 1 + (fs.existsSync(filename) ? fs.statSync(filename).size : 0) > MAX_INTENT_BYTES) throw new Error("work_queue_intent_limit");
  fs.mkdirSync(path.dirname(filename), { recursive: true });
  fs.appendFileSync(filename, `${line}\n`, { mode: 0o644, encoding: "utf8" });
  fs.chmodSync(filename, 0o644);
}

function stageIntent(filename, intent) {
  if (readStagedIntents(filename).length >= MAX_INTENTS) throw new Error("work_queue_intent_limit");
  appendLine(filename, canonical(intent));
}

/** @param {Record<string, any>} node */
function validateStagedNode(node) {
  closed(node, ["payload"], NODE_OPTIONAL, "submitted Work", "work_queue_intent_invalid");
  if (!node.payload || typeof node.payload !== "object" || Array.isArray(node.payload)) throw queueError("work_queue_intent_invalid", "Work payload must be a JSON object");
  canonical(node.payload);
  for (const field of ["graph_id", "node_key", "work_id", "pool", "worker_profile"]) if (Object.hasOwn(node, field)) identity(node[field], field);
  return node;
}

/** Stage the deterministic submit + dispatch_next pair of a trusted plan. */
function stagePlanIntents(filename, plan) {
  if (plan?.version !== 1 || !Array.isArray(plan.nodes) || plan.nodes.length < 1 || plan.nodes.length > OPERATIONS_PER_COMMIT) throw new Error("work_queue_plan_invalid");
  normalizeDispatchParameters(plan.dispatch);
  for (const node of plan.nodes) {
    validateStagedNode(node);
    if (identity(node.pool, "pool") !== plan.dispatch.pool) throw new Error("work_queue_plan_pool_mismatch");
  }
  const parameters = [
    { kind: "submit", parameters: { nodes: plan.nodes } },
    { kind: "dispatch_next", parameters: plan.dispatch },
  ];
  const id = digest(parameters);
  const intents = parameters.map(intent => ({ version: INTENT_VERSION, intent_id: `plan:${id}:${intent.kind}`, ...intent }));
  const previous = readStagedIntents(filename);
  if (previous.length > intents.length || previous.some((intent, index) => canonical(intent) !== canonical(intents[index]))) throw new Error("work_queue_plan_intents_conflict");
  for (const intent of intents) if (!previous.some(existing => existing.intent_id === intent.intent_id)) stageIntent(filename, intent);
  return intents;
}

/** A batch holding plan intents must be exactly one intact staged plan. */
function validatePreparedPlanIntentBatch(intents, errors) {
  const hasPlanIntent = intents.some(intent => intent.intent_id.startsWith("plan:")) || errors.some(error => error.intent_id?.startsWith("plan:"));
  if (!hasPlanIntent) return null;
  if (errors.length || intents.length !== 2) return "work_queue_plan_intent_batch_invalid";
  const [submit, dispatch] = intents;
  if (submit.kind !== "submit" || dispatch.kind !== "dispatch_next") return "work_queue_plan_intent_batch_invalid";
  const batchId = digest([
    { kind: "submit", parameters: submit.parameters },
    { kind: "dispatch_next", parameters: dispatch.parameters },
  ]);
  if (submit.intent_id !== `plan:${batchId}:submit` || dispatch.intent_id !== `plan:${batchId}:dispatch_next`) return "work_queue_plan_intent_batch_invalid";
  return null;
}

/**
 * The staged dispatch request; the publisher adds pool limits from the policy env.
 * @returns {{pool: string, max_claims: number, max_dispatches: number}}
 */
function normalizeDispatchParameters(parameters) {
  closed(parameters, ["pool", "max_claims", "max_dispatches"], [], "dispatch_next intent", "work_queue_intent_invalid");
  identity(parameters.pool, "pool");
  integer(parameters.max_claims, 1, OPERATIONS_PER_COMMIT, "max_claims");
  integer(parameters.max_dispatches, 1, OPERATIONS_PER_COMMIT, "max_dispatches");
  return { pool: parameters.pool, max_claims: parameters.max_claims, max_dispatches: parameters.max_dispatches };
}

/**
 * Build v4 Work operations from a staged submission. Defaults: graph_id = payload
 * digest, node_key "root", pool "default", priority 3, fairness_key "", profile =
 * the pool's default_profile. Replay validates the result structurally.
 * @param {{nodes: Array<Record<string, any>>}} parameters
 * @param {import("./work_queue_policy.cjs").PolicyEnv} policy
 * @returns {Array<Record<string, any>>}
 */
function normalizeSubmitParameters(parameters, policy) {
  closed(parameters, ["nodes"], [], "submit intent", "work_queue_intent_invalid");
  if (!Array.isArray(parameters.nodes) || parameters.nodes.length < 1 || parameters.nodes.length > OPERATIONS_PER_COMMIT) throw queueError("work_queue_graph_limit", `submission requires 1..${OPERATIONS_PER_COMMIT} nodes`);
  return parameters.nodes.map(node => {
    validateStagedNode(node);
    const graphId = node.graph_id ?? digest(node.payload);
    const nodeKey = node.node_key ?? "root";
    const poolName = node.pool ?? "default";
    const pool = poolEnv(policy, poolName);
    const profile = node.worker_profile ?? pool.default_profile;
    if (!Object.hasOwn(pool.profiles, profile)) throw queueError("work_queue_profile_not_approved", `worker profile ${profile} is not configured for pool ${poolName}`);
    const workId = nodeId(graphId, nodeKey);
    if (node.work_id !== undefined && node.work_id !== workId) throw queueError("work_identity_invalid", "work_id must bind graph_id/node_key");
    return {
      kind: "Work",
      work_id: workId,
      graph_id: graphId,
      node_key: nodeKey,
      pool: poolName,
      priority: node.priority ?? 3,
      fairness_key: node.fairness_key ?? "",
      worker_profile: profile,
      payload: node.payload,
      depends_on: node.depends_on ?? [],
      ...(node.subject === undefined ? {} : { subject: node.subject }),
      ...(node.backing_issue === undefined ? {} : { backing_issue: node.backing_issue }),
      ...(node.replacement_of === undefined ? {} : { replacement_of: node.replacement_of }),
    };
  });
}

/**
 * Finish intents: `{"version":4,"claim_handle":"h1","outcome":"completed"|"failed"}`.
 * @param {string} filename
 * @returns {Map<string, "completed" | "failed">}
 */
function readFinishIntents(filename) {
  const outcomes = new Map();
  for (const event of iterateJsonlEvents(readIntentLines(filename))) {
    if (event.type === "invalid" && event.reason === "blank") continue;
    let intent;
    try {
      intent = parseStrictJSON(event.rawLine);
      closed(intent, ["version", "claim_handle", "outcome"], [], "finish intent");
      identity(intent.claim_handle, "Claim handle");
    } catch {
      throw new Error("work_queue_finish_intent_invalid");
    }
    if (intent.version !== INTENT_VERSION || !FINISH_OUTCOMES.includes(intent.outcome)) throw new Error("work_queue_finish_intent_invalid");
    const previous = outcomes.get(intent.claim_handle);
    if (previous !== undefined && previous !== intent.outcome) throw new Error("work_queue_finish_intent_invalid");
    outcomes.set(intent.claim_handle, intent.outcome);
  }
  return outcomes;
}

/**
 * Stage one Claim's outcome; restaging the same outcome is a no-op.
 * @param {string} filename
 * @param {string} claimHandle
 * @param {string} outcome
 */
function stageFinishIntent(filename, claimHandle, outcome) {
  identity(claimHandle, "Claim handle");
  if (!FINISH_OUTCOMES.includes(outcome)) throw new TypeError("outcome must be completed or failed");
  const previous = readFinishIntents(filename).get(claimHandle);
  if (previous === outcome) return false;
  if (previous !== undefined) throw new Error("work_queue_finish_conflict");
  appendLine(filename, canonical({ version: INTENT_VERSION, claim_handle: claimHandle, outcome }));
  return true;
}

module.exports = {
  INTENT_VERSION,
  MAX_INTENT_BYTES,
  MAX_INTENTS,
  normalizeDispatchParameters,
  validateStagedNode,
  normalizeSubmitParameters,
  readFinishIntents,
  readIntentLines,
  readStagedIntentBatch,
  readStagedIntents,
  stageFinishIntent,
  stageIntent,
  stagePlanIntents,
  validatePreparedPlanIntentBatch,
};
