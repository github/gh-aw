// @ts-check
"use strict";

const { canonicalBytes, integer, queueError } = require("./work_queue_codec.cjs");

// Protocol v4 bounds. They are constants of the wire format, never Policy: the
// compiled policy environment carries only pool scheduling preferences.
const PAYLOAD_BYTES = 16 * 1024;
const OPERATIONS_PER_COMMIT = 256;
const GRAPH_NODES = 4096;
const PREDECESSORS = 64;
const ASSIGNMENT_BYTES = 48 * 1024;
const EFFECTS_BYTES = 4096;
const LEDGER_BYTES = 64 * 1024 * 1024;
const CHECKPOINT_AFTER_LINES = 512;
// Upper bound of the configurable pending_limit; replay enforces it per pool.
const MAX_PENDING_WORK = 4096;

function boundedBytes(value, limit, name) {
  if (canonicalBytes(value) > limit) throw queueError("resource_limit", `${name} exceeds ${limit} canonical bytes`);
}

// External dependency refreshes share one dispatch_next commit with its Claims.
function observationRefreshBudget(maxClaims, maximum = 128) {
  integer(maxClaims, 1, OPERATIONS_PER_COMMIT, "max_claims");
  integer(maximum, 0, OPERATIONS_PER_COMMIT - 1, "observation refresh budget");
  return Math.min(maximum, OPERATIONS_PER_COMMIT - maxClaims);
}

module.exports = {
  ASSIGNMENT_BYTES,
  CHECKPOINT_AFTER_LINES,
  EFFECTS_BYTES,
  GRAPH_NODES,
  LEDGER_BYTES,
  MAX_PENDING_WORK,
  OPERATIONS_PER_COMMIT,
  PAYLOAD_BYTES,
  PREDECESSORS,
  boundedBytes,
  observationRefreshBudget,
};
