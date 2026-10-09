// @ts-check
"use strict";
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("mcp");

const fs = require("fs");
const path = require("path");
const { randomUUID } = require("crypto");
const { TextDecoder } = require("util");
const { createServer, registerTool, start } = require("./mcp_server_core.cjs");
const queue = require("./work_queue_replay.cjs");
const { MAX_PARSE_BYTES, MAX_SNAPSHOT_PARSE_BYTES, closed, identity, integer, parseStrictJSON, utf8Compare } = require("./work_queue_codec.cjs");
const { normalizeAssignment, normalizeClaimScope } = require("./work_queue_claim_scope.cjs");
const { readStagedIntents, stageIntent } = require("./work_queue_intents.cjs");
const { resolveWorkQueueRuntime } = require("./aw_context.cjs");

const DEFAULT_SNAPSHOT_PATH = "/tmp/gh-aw/work-queue.snapshot.json";
const DEFAULT_FINISH_INTENT_PATH = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "work-queue", "work-queue.finish.jsonl");
const DEFAULT_INTENT_PATH = path.join(process.env.RUNNER_TEMP || "/tmp", "gh-aw", "safeoutputs", "work-queue", "work-queue.intents.jsonl");
const MAX_READ = 128;
function parseSnapshotEnvelope(text) {
  if (typeof text !== "string" || Buffer.byteLength(text, "utf8") > MAX_SNAPSHOT_PARSE_BYTES) throw new TypeError("work queue snapshot exceeds its bounded input limit");
  const snapshot = parseStrictJSON(text, { maxBytes: MAX_SNAPSHOT_PARSE_BYTES });
  if (typeof snapshot?.transactionLog === "string" && Buffer.byteLength(snapshot.transactionLog, "utf8") > MAX_PARSE_BYTES) throw new TypeError("work queue ledger exceeds its bounded input limit");
  return snapshot;
}

function loadWorkQueueSnapshot(snapshotPath = process.env.GH_AW_WORK_QUEUE_SNAPSHOT || DEFAULT_SNAPSHOT_PATH) {
  log.debug("snapshot.load.start");
  const stat = fs.statSync(snapshotPath);
  if (!stat.isFile() || stat.size > MAX_SNAPSHOT_PARSE_BYTES) throw new TypeError("work queue snapshot exceeds its bounded input limit");
  const snapshot = parseSnapshotEnvelope(new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(fs.readFileSync(snapshotPath)));
  closed(snapshot, ["version", "sha", "transactionLog", "worker", "captured_at", "origin"], ["visible_work_ids", "role"], "work queue snapshot");
  if (snapshot.version !== 3 || (snapshot.sha !== null && typeof snapshot.sha !== "string") || typeof snapshot.transactionLog !== "string") throw new TypeError("work queue snapshot has an invalid current-only shape");
  integer(snapshot.captured_at, 0, Number.MAX_SAFE_INTEGER, "snapshot timestamp");
  const worker = snapshot.worker === null ? null : normalizeAssignment(snapshot.worker);
  const role = snapshotRuntimeRole({ ...snapshot, worker });
  const absent = role !== "worker" && snapshot.sha === null && snapshot.transactionLog === "";
  if (snapshot.transactionLog === "" && !absent) throw new Error("work_queue_policy_missing");
  const projection = absent ? queue.newState() : queue.replayTransactions(queue.parseTransactionLog(snapshot.transactionLog));
  if (!projection.policy && !absent) throw new Error("work_queue_policy_missing");
  if (snapshot.visible_work_ids !== undefined && (!Array.isArray(snapshot.visible_work_ids) || snapshot.visible_work_ids.some(id => typeof id !== "string"))) throw new TypeError("work queue snapshot visibility is invalid");
  log.debug("snapshot.load.complete", { bytes: stat.size, works: projection.works.size, worker: !!worker, absent });
  return Object.freeze({ sha: snapshot.sha, worker, role, captured_at: snapshot.captured_at, origin: snapshot.origin, visible_work_ids: snapshot.visible_work_ids, projection });
}

function snapshotRuntimeRole(snapshot) {
  const declared = process.env.GH_AW_WORK_QUEUE_ROLE;
  if (declared !== undefined && snapshot.role !== undefined && declared !== snapshot.role) throw new Error("work_queue_runtime_role_conflict");
  const payload = snapshot.worker ? { inputs: { work_queue_assignment: snapshot.worker } } : {};
  return resolveWorkQueueRuntime(payload, { role: declared ?? snapshot.role }).role;
}

function validateSort(sort) {
  if (sort === undefined) return;
  if (!Array.isArray(sort) || sort.length < 1 || sort.length > 4) throw new TypeError("sort must contain one to four presentation operators");
  for (const term of sort) {
    closed(term, ["key", "direction"], [], "sort operator");
    if (!["id", "enqueued", "id_length"].includes(term.key) || !["asc", "desc"].includes(term.direction)) throw new TypeError("sort must use supported presentation keys and directions");
  }
}

function readWorkQueueState(snapshot, args = {}) {
  log.debug("snapshot.read.start");
  closed(args, [], ["work", "pool", "limit", "offset", "sort"], "work_queue_read");
  if (args.work !== undefined) identity(args.work, "Work ID");
  const limit = args.limit ?? 32;
  const offset = args.offset ?? 0;
  integer(limit, 1, MAX_READ, "read limit");
  integer(offset, 0, 1000000, "read offset");
  validateSort(args.sort);
  const state = snapshot.projection;
  const pool = args.pool ?? "default";
  identity(pool, "pool");
  const visibility = snapshot.visible_work_ids === undefined ? null : new Set(snapshot.visible_work_ids);
  const works = [...state.works.values()].filter(work => work.pool === pool && (!visibility || visibility.has(work.work_id)) && (args.work === undefined || args.work === work.work_id));
  works.sort((left, right) => {
    for (const term of args.sort || []) {
      const a = term.key === "id" ? left.work_id : term.key === "id_length" ? [...left.work_id].length : left.enqueued;
      const b = term.key === "id" ? right.work_id : term.key === "id_length" ? [...right.work_id].length : right.enqueued;
      const difference = typeof a === "string" ? utf8Compare(a, b) : a - b;
      if (difference) return term.direction === "asc" ? difference : -difference;
    }
    return left.position.commit - right.position.commit || left.position.operation - right.position.operation;
  });
  const next = state.policy ? queue.planNext(state, pool, snapshot.captured_at) : { work_id: null, reason: "queue_uninitialized" };
  const nextWork = next.work_id && (!visibility || visibility.has(next.work_id)) ? next.work_id : null;
  const counts = Object.create(null);
  for (const work of works) counts[work.state] = (counts[work.state] || 0) + 1;
  const selectedWorks = new Set(works.map(work => work.work_id));
  const claimCounts = Object.create(null);
  for (const claim of state.claims.values()) if (selectedWorks.has(claim.work_id)) claimCounts[claim.state] = (claimCounts[claim.state] || 0) + 1;
  const barriers = Object.create(null);
  for (const work of works) barriers[work.barrier] = (barriers[work.barrier] || 0) + 1;
  const reservations = [...state.dispatches.values()].filter(dispatch => dispatch.pool === pool && !dispatch.released && dispatch.claims.some(member => selectedWorks.has(member.work_id)));
  log.debug("snapshot.read.complete", { total: works.length, offset, limit, reservations: reservations.length });
  return {
    view: "activation_snapshot",
    snapshot_sha: snapshot.sha,
    queue_tip: state.tip,
    captured_at: snapshot.captured_at,
    prediction: { work_id: nextWork, reason: next.reason, authoritative: false },
    queue_state: state.policy ? "initialized" : "uninitialized",
    policy_mode: state.policy?.mode ?? null,
    fairness_units: "durable_claims",
    counts,
    claim_counts: claimCounts,
    barrier_counts: barriers,
    native_reservations: { outstanding: reservations.length, unbound: reservations.filter(dispatch => !dispatch.run).length },
    ...(args.work === undefined ? {} : { work: { id: args.work, state: works[0]?.state ?? "absent" } }),
    total: works.length,
    offset,
    next_offset: offset + limit < works.length ? offset + limit : null,
    works: works.slice(offset, offset + limit).map(work => ({
      id: work.work_id,
      state: work.state,
      barrier: work.barrier,
      pool: work.pool,
      worker_profile: work.worker_profile,
      enqueued: work.enqueued,
      claim_id: work.claim_id ?? null,
      retry_not_before: work.retry_not_before,
    })),
  };
}

const sortSchema = {
  type: "array",
  minItems: 1,
  maxItems: 4,
  description: "Presentation only; sorting never changes the scheduler's prediction or selection.",
  items: { type: "object", properties: { key: { enum: ["id", "enqueued", "id_length"] }, direction: { enum: ["asc", "desc"] } }, required: ["key", "direction"], additionalProperties: false },
};
const text = value => ({ content: [{ type: "text", text: JSON.stringify(value) }] });

function createWorkQueueStateTool(snapshot) {
  return {
    name: "work_queue_read",
    description: "Read a bounded immutable activation snapshot. Predictions may be stale and are never grants. Presentation sorting cannot influence trusted selection.",
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

function createWorkQueueExplainTool(snapshot) {
  return {
    name: "work_queue_explain",
    description: "Explain readiness, dependencies, capacity and fairness using the same activation-snapshot engine. This does not reserve or dispatch Work.",
    inputSchema: { type: "object", properties: { work: { type: "string", minLength: 1, maxLength: 256 }, pool: { type: "string", minLength: 1, maxLength: 256 } }, required: ["work"], additionalProperties: false },
    handler: args => {
      log.debug("snapshot.explain.start");
      closed(args, ["work"], ["pool"], "work_queue_explain");
      identity(args.work, "Work ID");
      if (snapshot.visible_work_ids && !snapshot.visible_work_ids.includes(args.work)) throw new Error("work_queue_read_scope_denied");
      const work = snapshot.projection.works.get(args.work);
      if (work && args.pool !== undefined && work.pool !== args.pool) throw new Error("work_queue_read_scope_denied");
      const explanation = work
        ? queue.explainWork(snapshot.projection, args.work, snapshot.captured_at)
        : { work_id: args.work, state: "absent", ready: false, reason: snapshot.projection.policy ? "work_absent" : "queue_uninitialized", authoritative: false };
      log.debug("snapshot.explain.complete", { present: !!work });
      return text({ view: "activation_snapshot", snapshot_sha: snapshot.sha, captured_at: snapshot.captured_at, explanation });
    },
  };
}

function stageToolIntent(snapshot, kind, parameters, options) {
  log.debug("tool.intent.stage.start", { finish: kind === "finish", submit: kind === "submit", dispatch: kind === "dispatch_next" });
  if (snapshotRuntimeRole(snapshot) === "observer") throw new Error("work_queue_observer_read_only");
  const scope = snapshot.worker ? normalizeClaimScope(parameters, snapshot.worker) : parameters;
  const { claim_handle, ...body } = scope;
  const outputPath = kind === "finish" ? options.finishIntentPath || process.env.GH_AW_WORK_QUEUE_FINISH_INTENT || DEFAULT_FINISH_INTENT_PATH : options.intentPath || process.env.GH_AW_WORK_QUEUE_INTENTS || DEFAULT_INTENT_PATH;
  if (kind === "finish") {
    const previous = readStagedIntents(outputPath).find(intent => intent.kind === "finish" && intent.claim_handle === claim_handle);
    if (previous) {
      if (previous.parameters.outcome !== body.outcome) throw new Error("work_queue_finish_conflict");
      log.debug("tool.intent.stage.reused");
      return text({ intent_id: previous.intent_id, status: "staged", claim_handle });
    }
  }
  const intent = { version: 3, intent_id: (options.createIntentId || (() => `intent:${randomUUID()}`))(), kind, parameters: body, ...(claim_handle === undefined ? {} : { claim_handle }) };
  stageIntent(outputPath, intent);
  log.debug("tool.intent.stage.complete");
  return text({ intent_id: intent.intent_id, status: "staged", ...(claim_handle === undefined ? {} : { claim_handle }) });
}

function createWorkQueueFinishTool(options = {}) {
  const snapshot = options.snapshot;
  return {
    name: "work_queue_claim_finish",
    description: "Stage an independent completed/cancelled intent for one immutable assigned Claim. Only an originally one-Claim assignment may omit claim_handle. Staging is not durable completion.",
    inputSchema: { type: "object", properties: { claim_handle: { type: "string", minLength: 1, maxLength: 256 }, outcome: { enum: ["completed", "cancelled"] } }, additionalProperties: false },
    handler: args => {
      closed(args, [], ["claim_handle", "outcome"], "work_queue_claim_finish");
      if (!snapshot?.worker) throw new Error("work_queue_assignment_required");
      const outcome = args.outcome === undefined ? "completed" : args.outcome;
      if (!["completed", "cancelled"].includes(outcome)) throw new TypeError("outcome must be completed or cancelled");
      return stageToolIntent(snapshot, "finish", { ...args, outcome }, options);
    },
  };
}

function createWorkQueueDispatchTool(snapshot, options = {}) {
  return {
    name: "work_queue_dispatch_next",
    description: "Stage a bounded request for the trusted scheduler's fair prefix. Accepts no Work selector, worker, ref, trace or actor override; returns staged intent identity, not Claims.",
    inputSchema: {
      type: "object",
      properties: {
        pool: { type: "string", minLength: 1, maxLength: 256 },
        max_claims: { type: "integer", minimum: 1, maximum: 256 },
        max_dispatches: { type: "integer", minimum: 1, maximum: 256 },
        claim_handle: { type: "string", minLength: 1, maxLength: 256 },
      },
      required: ["pool", "max_claims", "max_dispatches"],
      additionalProperties: false,
    },
    handler: args => {
      closed(args, ["pool", "max_claims", "max_dispatches"], ["claim_handle"], "work_queue_dispatch_next");
      identity(args.pool, "pool");
      integer(args.max_claims, 1, 256, "max_claims");
      integer(args.max_dispatches, 1, 256, "max_dispatches");
      if (!snapshot.worker && Object.hasOwn(args, "claim_handle")) throw new Error("work_queue_claim_scope_invalid");
      return stageToolIntent(snapshot, "dispatch_next", args, options);
    },
  };
}

function createWorkQueueSubmitTool(snapshot, options = {}) {
  return {
    name: "work_queue_submit",
    description:
      "Stage a bounded graph of immutable Work payloads and approved worker-profile requests. Omitted graph/node IDs default to the canonical payload hash and root; use explicit IDs for distinct nodes. Trusted ingestion resolves metadata, dependencies and policy; staging grants no authority.",
    inputSchema: {
      type: "object",
      properties: { nodes: { type: "array", minItems: 1, maxItems: 256, items: { type: "object" } }, claim_handle: { type: "string", minLength: 1, maxLength: 256 } },
      required: ["nodes"],
      additionalProperties: false,
    },
    handler: args => {
      closed(args, ["nodes"], ["claim_handle"], "work_queue_submit");
      if (!Array.isArray(args.nodes) || args.nodes.length < 1 || args.nodes.length > 256) throw new TypeError("submission must contain 1 to 256 nodes");
      for (const node of args.nodes) {
        closed(node, ["payload"], ["graph_id", "node_key", "work_id", "pool", "priority", "fairness_key", "worker_profile", "depends_on", "subject", "backing_issue", "replacement_of"], "submitted Work");
        if (Object.hasOwn(node, "graph_id")) identity(node.graph_id, "graph ID");
        if (Object.hasOwn(node, "node_key")) identity(node.node_key, "node key");
      }
      if (!snapshot.worker && Object.hasOwn(args, "claim_handle")) throw new Error("work_queue_claim_scope_invalid");
      return stageToolIntent(snapshot, "submit", args, options);
    },
  };
}

function createWorkQueueTools(snapshot, options = {}) {
  /** @type {import("./mcp_server_core.cjs").Tool[]} */
  const tools = [createWorkQueueStateTool(snapshot), createWorkQueueExplainTool(snapshot)];
  if (snapshotRuntimeRole(snapshot) !== "observer") tools.push(createWorkQueueSubmitTool(snapshot, options), createWorkQueueDispatchTool(snapshot, options), createWorkQueueFinishTool({ ...options, snapshot }));
  return tools;
}

function startWorkQueueServer(options = {}) {
  log.debug("server.start");
  const snapshot = loadWorkQueueSnapshot(options.snapshotPath);
  const server = createServer({ name: "work-queue", version: "3.0.0" }, { logDir: options.logDir || process.env.GH_AW_MCP_LOG_DIR });
  const tools = createWorkQueueTools(snapshot, options);
  for (const tool of tools) registerTool(server, tool);
  log.debug("server.tools.registered", { tools: tools.length });
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
  DEFAULT_SNAPSHOT_PATH,
  DEFAULT_FINISH_INTENT_PATH,
  DEFAULT_INTENT_PATH,
  createWorkQueueStateTool,
  createWorkQueueExplainTool,
  createWorkQueueSubmitTool,
  createWorkQueueDispatchTool,
  createWorkQueueFinishTool,
  createWorkQueueTools,
  parseSnapshotEnvelope,
  loadWorkQueueSnapshot,
  readWorkQueueState,
  startWorkQueueServer,
};
