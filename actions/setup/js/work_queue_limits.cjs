// @ts-check
"use strict";

const { canonicalBytes, integer, queueError } = require("./work_queue_codec.cjs");

const DEFAULT_LIMITS = Object.freeze({
  ledger_bytes: 64 * 1024 * 1024,
  recovery_bytes: 16 * 1024 * 1024,
  payload_bytes: 16 * 1024,
  graph_nodes: 4096,
  predecessors: 64,
  pending_nodes: 4096,
  operations: 256,
  assignment_bytes: 48 * 1024,
  result_bytes: 4096,
  evidence_bytes: 1024,
  observation_writes: 4096,
});

function boundedBytes(value, limit, name) {
  if (canonicalBytes(value) > limit) throw queueError("resource_limit", `${name} exceeds ${limit} canonical bytes`);
}

function validateLimits(limits) {
  for (const [name, ceiling] of Object.entries(DEFAULT_LIMITS)) integer(limits[name], 1, ceiling, `limit ${name}`);
  if (Object.keys(limits).some(name => !Object.hasOwn(DEFAULT_LIMITS, name))) throw queueError("resource_limit", "unknown limit");
  return limits;
}

function recoveryHeadroom(state) {
  // Reserve every bounded remaining retry plus delivery and native closure.
  let bytes = 0;
  for (const node of state.works.values()) {
    if (node.state === "cancelled" || (node.state === "completed" && node.barrier !== "pending")) continue;
    const pool = state.policy.pools[node.pool];
    const limits = state.policy.limits;
    const remaining = node.state === "completed" ? 0 : Math.max(0, pool.retry.max_attempts - node.attempts);
    bytes += remaining * (4 * limits.evidence_bytes + 8192) + 2 * limits.result_bytes + 2 * limits.evidence_bytes + 8192;
    if (node.state === "claimed") bytes += 8192;
  }
  for (const dispatch of state.dispatches.values())
    if (!dispatch.released) {
      const remaining = Math.max(0, state.policy.pools[dispatch.pool].reconciliation.max_attempts + 4 - (state.lifecycleWrites.get(dispatch.dispatch_id) ?? 0));
      bytes += remaining * (2 * state.policy.limits.evidence_bytes + 12288) + 2 * state.policy.limits.evidence_bytes + 8192;
    }
  return bytes;
}

function checkLedgerBudget(state, candidateBytes, admission, observations = false) {
  if (!state.policy) return;
  const limits = state.policy.limits;
  const total = state.ledgerBytes + candidateBytes;
  const headroom = recoveryHeadroom(state);
  if (total > limits.ledger_bytes + limits.recovery_bytes) throw queueError("ledger_limit", "ledger recovery reserve exhausted");
  if (headroom > limits.ledger_bytes + limits.recovery_bytes - total) throw queueError("ledger_limit", "write would consume remaining bounded closure/recovery headroom");
  if (observations && total > limits.ledger_bytes) throw queueError("ledger_limit", "optional observations cannot consume closure/recovery headroom");
  if (admission && (total > limits.ledger_bytes || headroom > limits.recovery_bytes)) {
    throw queueError("ledger_limit", "new admission would consume closure/recovery headroom");
  }
}

module.exports = { DEFAULT_LIMITS, boundedBytes, checkLedgerBudget, recoveryHeadroom, validateLimits };
