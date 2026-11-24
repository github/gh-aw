// @ts-check
"use strict";
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("mcp");

const fs = require("fs");
const path = require("path");
const { randomUUID } = require("crypto");
const { TextDecoder } = require("util");
const { createServer, registerTool, start } = require("./mcp_server_core.cjs");
const { newState, replayTransactionLog } = require("./work_queue_replay.cjs");
const { dependencyStatus } = require("./work_queue_graph.cjs");
const { eligibleWork, fifoCompare } = require("./work_queue_scheduler.cjs");
const { MAX_PARSE_BYTES, MAX_SNAPSHOT_PARSE_BYTES, closed, identity, integer, parseStrictJSON, utf8Compare } = require("./work_queue_codec.cjs");
const { INTENT_VERSION, normalizeDispatchParameters, stageFinishIntent, stageIntent, validateStagedNode } = require("./work_queue_intents.cjs");
const { normalizeAssignment } = require("./work_queue_claim_scope.cjs");

const DEFAULT_SNAPSHOT_PATH = "/tmp/gh-aw/work-queue.snapshot.json";
const DEFAULT_FINISH_INTENT_PATH = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "work-queue", "work-queue.finish.jsonl");
const DEFAULT_INTENT_PATH = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "work-queue", "work-queue.intents.jsonl");
const MAX_READ = 128;
const ROLES = ["observer", "dispatcher", "worker"];

/**
 * @typedef {{handle: string, claim_id: string, work_id: string, work: Record<string, unknown>, result_refs: Array<{work_id: string, result_commit_id: string, descriptor: unknown}>, continuation?: {route: string, message: string, remaining: number}}} AssignedClaim
 * @typedef {{dispatch_id: string, claims: AssignedClaim[]}} SnapshotAssignment
 * @typedef {{version: 4, sha: string | null, transactionLog: string, origin: Record<string, unknown>, role: "observer" | "dispatcher" | "worker", worker: SnapshotAssignment | null, visible_work_ids?: string[]}} SnapshotEnvelope
 */

/**
 * Parse and validate the v4 activation snapshot envelope.
 * @param {string} text
 * @returns {SnapshotEnvelope}
 */
function parseSnapshotEnvelope(text) {
  if (typeof text !== "string" || Buffer.byteLength(text, "utf8") > MAX_SNAPSHOT_PARSE_BYTES) throw new TypeError("work queue snapshot exceeds its bounded input limit");
  const snapshot = parseStrictJSON(text, { maxBytes: MAX_SNAPSHOT_PARSE_BYTES });
  closed(snapshot, ["version", "sha", "transactionLog", "origin", "role", "worker"], ["visible_work_ids"], "work queue snapshot", "work_queue_snapshot_invalid");
  if (snapshot.version !== 4) throw new TypeError("work queue snapshot must be protocol v4");
  if ((snapshot.sha !== null && typeof snapshot.sha !== "string") || typeof snapshot.transactionLog !== "string") throw new TypeError("work queue snapshot has an invalid ledger");
  if (Buffer.byteLength(snapshot.transactionLog, "utf8") > MAX_PARSE_BYTES) throw new TypeError("work queue ledger exceeds its bounded input limit");
  if (!snapshot.origin || typeof snapshot.origin !== "object" || Array.isArray(snapshot.origin)) throw new TypeError("work queue snapshot origin is invalid");
  if (!ROLES.includes(snapshot.role)) throw new TypeError("work queue snapshot role is invalid");
  if (snapshot.worker !== null) {
    if (snapshot.role !== "worker") throw new TypeError("only a worker snapshot carries an assignment");
    snapshot.worker = normalizeAssignment(snapshot.worker);
  }
  if (snapshot.visible_work_ids !== undefined && (!Array.isArray(snapshot.visible_work_ids) || snapshot.visible_work_ids.some(id => typeof id !== "string"))) throw new TypeError("work queue snapshot visibility is invalid");
  return snapshot;
}

function loadWorkQueueSnapshot(snapshotPath = process.env.GH_AW_WORK_QUEUE_SNAPSHOT || DEFAULT_SNAPSHOT_PATH) {
  const stat = fs.statSync(snapshotPath);
  if (!stat.isFile() || stat.size > MAX_SNAPSHOT_PARSE_BYTES) throw new TypeError("work queue snapshot exceeds its bounded input limit");
  const snapshot = parseSnapshotEnvelope(new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(fs.readFileSync(snapshotPath)));
  const declared = process.env.GH_AW_WORK_QUEUE_ROLE;
  if (declared !== undefined && declared !== snapshot.role) throw new Error("work_queue_runtime_role_conflict");
  const projection = snapshot.transactionLog === "" ? newState() : replayTransactionLog(snapshot.transactionLog);
  log.debug("snapshot.load.complete", { bytes: stat.size, works: projection.works.size, worker: !!snapshot.worker });
  return Object.freeze({ sha: snapshot.sha, worker: snapshot.worker, role: snapshot.role, origin: snapshot.origin, visible_work_ids: snapshot.visible_work_ids, projection });
}

function validateSort(sort) {
  if (sort === undefined) return;
  if (!Array.isArray(sort) || sort.length < 1 || sort.length > 4) throw new TypeError("sort must contain one to four presentation operators");
  for (const term of sort) {
    closed(term, ["key", "direction"], [], "sort operator");
    if (!["id", "position", "priority"].includes(term.key) || !["asc", "desc"].includes(term.direction)) throw new TypeError("sort must use supported presentation keys and directions");
  }
}

function sortValue(work, key) {
  return key === "id" ? work.work_id : key === "priority" ? work.priority : work.position.commit * 1024 + work.position.operation;
}

/**
 * A worker's Claim lives only in its activation snapshot (the dispatch input
 * carries just the Dispatch ID), so the read view is how the agent learns it.
 */
function assignmentView(snapshot) {
  if (!snapshot.worker) return null;
  return {
    dispatch_id: snapshot.worker.dispatch_id,
    // A continuation is untrusted remaining-work context from the parent run, beside the unchanged payload.
    claims: snapshot.worker.claims.map(claim => ({
      claim_handle: claim.handle,
      claim_id: claim.claim_id,
      work_id: claim.work_id,
      work: claim.work,
      result_refs: claim.result_refs,
      ...(claim.continuation ? { continuation: claim.continuation } : {}),
    })),
  };
}

/** A bounded, read-only view of the activation snapshot for one pool; a worker's pool defaults to its assigned Work's. */
function readWorkQueueState(snapshot, args = {}) {
  closed(args, [], ["work", "pool", "limit", "offset", "sort"], "work_queue_read");
  if (args.work !== undefined) identity(args.work, "Work ID");
  const limit = args.limit ?? 32;
  const offset = args.offset ?? 0;
  integer(limit, 1, MAX_READ, "read limit");
  integer(offset, 0, 1000000, "read offset");
  validateSort(args.sort);
  const state = snapshot.projection;
  const assignment = assignmentView(snapshot);
  const pool = args.pool ?? (assignment ? state.works.get(assignment.claims[0].work_id)?.pool : undefined) ?? "default";
  identity(pool, "pool");
  const visibility = snapshot.visible_work_ids === undefined ? null : new Set(snapshot.visible_work_ids);
  const visible = work => !visibility || visibility.has(work.work_id);
  const works = [...state.works.values()].filter(work => work.pool === pool && visible(work) && (args.work === undefined || args.work === work.work_id));
  works.sort((left, right) => {
    for (const term of args.sort || []) {
      const a = sortValue(left, term.key);
      const b = sortValue(right, term.key);
      const difference = typeof a === "string" ? utf8Compare(a, /** @type {string} */ b) : a - /** @type {number} */ b;
      if (difference) return term.direction === "asc" ? difference : -difference;
    }
    return fifoCompare(left, right);
  });
  const next = eligibleWork(state, pool).find(visible);
  const counts = Object.create(null);
  for (const work of works) counts[work.state] = (counts[work.state] || 0) + 1;
  const selected = new Set(works.map(work => work.work_id));
  const claimCounts = Object.create(null);
  for (const claim of state.claims.values()) if (selected.has(claim.work_id)) claimCounts[claim.state] = (claimCounts[claim.state] || 0) + 1;
  const dispatches = [...state.dispatches.values()].filter(dispatch => dispatch.pool === pool && dispatch.state !== "closed");
  return {
    view: "activation_snapshot",
    snapshot_sha: snapshot.sha,
    queue_tip: state.tip,
    pool,
    ...(assignment ? { assignment } : {}),
    prediction: { work_id: next?.work_id ?? null, authoritative: false },
    counts,
    claim_counts: claimCounts,
    open_dispatches: { total: dispatches.length, unbound: dispatches.filter(dispatch => dispatch.state !== "bound").length },
    ...(args.work === undefined ? {} : { work: { id: args.work, state: works[0]?.state ?? "absent" } }),
    total: works.length,
    offset,
    next_offset: offset + limit < works.length ? offset + limit : null,
    works: works.slice(offset, offset + limit).map(work => ({
      id: work.work_id,
      graph_id: work.graph_id,
      node_key: work.node_key,
      state: work.state,
      pool: work.pool,
      priority: work.priority,
      worker_profile: work.worker_profile,
      attempts: work.attempts,
      position: work.position,
      claim_id: work.claim_id ?? null,
      ...(work.state === "available" ? { readiness: dependencyStatus(state, work).reason } : {}),
      ...(work.cancellation_reason === undefined ? {} : { cancellation_reason: work.cancellation_reason }),
    })),
  };
}

const sortSchema = {
  type: "array",
  minItems: 1,
  maxItems: 4,
  description: "Presentation only; sorting never changes the scheduler's priority-then-FIFO order.",
  items: { type: "object", properties: { key: { enum: ["id", "position", "priority"] }, direction: { enum: ["asc", "desc"] } }, required: ["key", "direction"], additionalProperties: false },
};
const text = value => ({ content: [{ type: "text", text: JSON.stringify(value) }] });

function createWorkQueueStateTool(snapshot) {
  return {
    name: "work_queue_read",
    description: "Read a bounded immutable activation snapshot of one pool. The prediction is the next eligible Work by priority then FIFO; it may be stale and is never a grant.",
    inputSchema: {
      type: "object",
      properties: {
        work: { type: "string", minLength: 1, maxLength: 256 },
        pool: { type: "string", minLength: 1, maxLength: 256 },
        limit: { type: "integer", minimum: 1, maximum: MAX_READ },
        offset: { type: "integer", minimum: 0, maximum: 1000000 },
        sort: sortSchema,
      },
      additionalProperties: false,
    },
    handler: args => text(readWorkQueueState(snapshot, args)),
  };
}

/** A worker may only name the Claim handle of its own assignment; omission selects it. */
function assignedHandle(snapshot, handle) {
  if (!snapshot.worker) {
    if (handle !== undefined) throw new Error("work_queue_claim_scope_invalid");
    return undefined;
  }
  const assigned = snapshot.worker.claims[0].handle;
  if (handle !== undefined && handle !== assigned) throw new Error("work_queue_claim_scope_invalid");
  return assigned;
}

function stageControlIntent(snapshot, kind, parameters, claimHandle, options) {
  const intent = {
    version: INTENT_VERSION,
    intent_id: (options.createIntentId || (() => `intent:${randomUUID()}`))(),
    kind,
    parameters,
    ...(claimHandle === undefined ? {} : { claim_handle: claimHandle }),
  };
  stageIntent(options.intentPath || process.env.GH_AW_WORK_QUEUE_INTENTS || DEFAULT_INTENT_PATH, intent);
  log.debug("tool.intent.staged", { submit: kind === "submit" });
  return text({ intent_id: intent.intent_id, status: "staged", ...(claimHandle === undefined ? {} : { claim_handle: claimHandle }) });
}

function createWorkQueueFinishTool(snapshot, options = {}) {
  return {
    name: "work_queue_claim_finish",
    description:
      "Stage the outcome of the assigned Claim: completed (default) authorizes its safe-output effects; failed returns the Work to the queue. A missing outcome counts as failed. claim_handle may be omitted; if supplied it must be the assigned handle. Staging is not completion.",
    inputSchema: { type: "object", properties: { claim_handle: { type: "string", minLength: 1, maxLength: 256 }, outcome: { enum: ["completed", "failed"] } }, additionalProperties: false },
    handler: args => {
      closed(args, [], ["claim_handle", "outcome"], "work_queue_claim_finish");
      if (!snapshot.worker) throw new Error("work_queue_assignment_required");
      const handle = /** @type {string} */ assignedHandle(snapshot, args.claim_handle);
      const outcome = args.outcome ?? "completed";
      stageFinishIntent(options.finishIntentPath || process.env.GH_AW_WORK_QUEUE_FINISH_INTENT || DEFAULT_FINISH_INTENT_PATH, handle, outcome);
      return text({ claim_handle: handle, outcome, status: "staged" });
    },
  };
}

function createWorkQueueDispatchTool(snapshot, options = {}) {
  return {
    name: "work_queue_dispatch_next",
    description: "Stage a bounded request for the trusted scheduler to claim the pool's next eligible Work (priority, then FIFO) and launch workers. Accepts no Work selector; returns the staged intent identity, not Claims.",
    inputSchema: {
      type: "object",
      properties: {
        pool: { type: "string", minLength: 1, maxLength: 256 },
        max_claims: { type: "integer", minimum: 1, maximum: 256 },
        max_dispatches: { type: "integer", minimum: 1, maximum: 256 },
      },
      required: ["pool", "max_claims", "max_dispatches"],
      additionalProperties: false,
    },
    handler: args => stageControlIntent(snapshot, "dispatch_next", normalizeDispatchParameters(args), undefined, options),
  };
}

function createWorkQueueSubmitTool(snapshot, options = {}) {
  return {
    name: "work_queue_submit",
    description:
      "Stage a bounded graph of immutable Work payloads. Omitted graph/node IDs default to the canonical payload hash and root; use explicit IDs for distinct nodes. Workers' children default to their own Work's graph, pool, priority and fairness key. Trusted ingestion resolves dependencies and pool limits; staging grants no authority.",
    inputSchema: {
      type: "object",
      properties: { nodes: { type: "array", minItems: 1, maxItems: 256, items: { type: "object" } }, claim_handle: { type: "string", minLength: 1, maxLength: 256 } },
      required: ["nodes"],
      additionalProperties: false,
    },
    handler: args => {
      closed(args, ["nodes"], ["claim_handle"], "work_queue_submit");
      if (!Array.isArray(args.nodes) || args.nodes.length < 1 || args.nodes.length > 256) throw new TypeError("submission must contain 1 to 256 nodes");
      for (const node of args.nodes) validateStagedNode(node);
      return stageControlIntent(snapshot, "submit", { nodes: args.nodes }, assignedHandle(snapshot, args.claim_handle), options);
    },
  };
}

/** Observers read; dispatchers submit and dispatch; workers submit children and finish Claims. */
function createWorkQueueTools(snapshot, options = {}) {
  /** @type {import("./mcp_server_core.cjs").Tool[]} */
  const tools = [createWorkQueueStateTool(snapshot)];
  if (snapshot.role === "dispatcher") tools.push(createWorkQueueSubmitTool(snapshot, options), createWorkQueueDispatchTool(snapshot, options));
  if (snapshot.role === "worker") tools.push(createWorkQueueSubmitTool(snapshot, options), createWorkQueueFinishTool(snapshot, options));
  return tools;
}

function startWorkQueueServer(options = {}) {
  const snapshot = loadWorkQueueSnapshot(options.snapshotPath);
  const server = createServer({ name: "work-queue", version: "4.0.0" }, { logDir: options.logDir || process.env.GH_AW_MCP_LOG_DIR });
  for (const tool of createWorkQueueTools(snapshot, options)) registerTool(server, tool);
  start(server);
}

if (require.main === module) {
  try {
    startWorkQueueServer();
  } catch (error) {
    log.failure("server.start.failed", error);
    console.error("Error starting work queue MCP server: current activation snapshot is unavailable or invalid");
    process.exit(1);
  }
}

module.exports = {
  DEFAULT_FINISH_INTENT_PATH,
  DEFAULT_INTENT_PATH,
  DEFAULT_SNAPSHOT_PATH,
  createWorkQueueDispatchTool,
  createWorkQueueFinishTool,
  createWorkQueueStateTool,
  createWorkQueueSubmitTool,
  createWorkQueueTools,
  loadWorkQueueSnapshot,
  parseSnapshotEnvelope,
  readWorkQueueState,
  startWorkQueueServer,
};
