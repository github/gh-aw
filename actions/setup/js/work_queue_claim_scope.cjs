// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { AsyncLocalStorage } = require("async_hooks");
const { identity, canonical, canonicalBytes, closed } = require("./work_queue_codec.cjs");
const { ASSIGNMENT_BYTES, PREDECESSORS } = require("./work_queue_limits.cjs");
const { ERR_WORK_QUEUE_CLAIM_SCOPE } = require("./error_codes.cjs");
const { validateContinuation } = require("./work_queue_graph.cjs");

// The snapshot assignment carries its Claim's payload plus predecessor effects.
const MAX_ASSIGNMENT_BYTES = ASSIGNMENT_BYTES + 16 * 1024;
const RUNTIME_ROLES = ["observer", "dispatcher", "worker"];
// Proof states that suppress a Claim's outputs instead of failing them.
const SUPPRESSED_STATES = new Set(["cancelled", "completed", "failed"]);
const claimExecution = new AsyncLocalStorage();
const claimEffectBatch = new AsyncLocalStorage();
const claimEffectBatches = new WeakMap();
/** @type {import("node:async_hooks").AsyncLocalStorage<string>} */
const claimOutputType = new AsyncLocalStorage();
/** @type {import("node:async_hooks").AsyncLocalStorage<{identity: ReturnType<typeof claimIdentity>, targets: readonly import("./work_queue_resource_scope.cjs").EffectResource[], operations: readonly string[]}>} */
const resourceExecution = new AsyncLocalStorage();
const own = (value, key) => Object.prototype.hasOwnProperty.call(value, key);
const object = value => value !== null && typeof value === "object" && !Array.isArray(value);

function scopeError(message, code = "claim_scope_invalid") {
  return Object.assign(new Error(`${ERR_WORK_QUEUE_CLAIM_SCOPE}: work_queue_claim_scope: ${message}`), { code });
}

function identifier(value, name) {
  return identity(value, name);
}

function freeze(value) {
  if (value && typeof value === "object") {
    for (const nested of Object.values(value)) freeze(nested);
    Object.freeze(value);
  }
  return value;
}

/**
 * Validate the snapshot assignment `{dispatch_id, claims:[{handle, claim_id, work_id, work, result_refs, continuation?}]}`
 * produced by `assignmentForDispatch`: a ledger Dispatch owns exactly one open Claim.
 */
function normalizeAssignment(value) {
  if (!object(value) || !Array.isArray(value.claims) || value.claims.length !== 1) throw scopeError("work queue assignment requires exactly one open Claim");
  if (canonicalBytes(value) > MAX_ASSIGNMENT_BYTES) throw scopeError("assignment exceeds its bounded host-input ceiling");
  if (Object.keys(value).some(field => !["dispatch_id", "claims"].includes(field)) || !own(value, "dispatch_id")) throw scopeError("assignment fields must be exactly dispatch_id and claims");
  identifier(value.dispatch_id, "dispatch_id");
  const [member] = value.claims;
  if (!object(member) || !object(member.work) || !Array.isArray(member.result_refs) || member.result_refs.length > PREDECESSORS) throw scopeError("the Claim must include its Work payload and bounded predecessor results");
  if (Object.keys(member).some(field => !["handle", "claim_id", "work_id", "work", "result_refs", "continuation"].includes(field))) throw scopeError("unknown assignment Claim field");
  if (own(member, "continuation")) {
    try {
      validateContinuation(member.continuation);
    } catch {
      throw scopeError("invalid assignment continuation");
    }
  }
  identifier(member.handle, "handle");
  identifier(member.claim_id, "claim_id");
  identifier(member.work_id, "work_id");
  const dependencies = new Set();
  for (const reference of member.result_refs) {
    if (!object(reference) || !Array.isArray(reference.descriptor) || Object.keys(reference).length !== 3 || !own(reference, "work_id") || !own(reference, "result_commit_id")) {
      throw scopeError("invalid predecessor result reference");
    }
    identifier(reference.work_id, "result work_id");
    identifier(reference.result_commit_id, "result_commit_id");
    if (dependencies.has(reference.work_id)) throw scopeError("duplicate predecessor result reference");
    dependencies.add(reference.work_id);
  }
  return freeze(JSON.parse(JSON.stringify(value)));
}

function normalizeClaimScope(message, assignment) {
  if (!object(message)) throw scopeError("safe-output message must be an object");
  const [member] = normalizeAssignment(assignment).claims;
  if (own(message, "claim_handle") && identifier(message.claim_handle, "claim_handle") !== member.handle) throw scopeError("claim_handle is foreign to this assignment");
  for (const field of ["claim_id", "work_id"]) {
    if (own(message, field) && message[field] !== member[field]) throw scopeError(`explicit ${field} conflicts with claim_handle`);
  }
  for (const field of ["work_queue_claim", "work_queue_assignment", "assignment", "run_id", "run_attempt", "authorized"]) {
    // This handler's run_id identifies its target, not the worker's run binding.
    if (field === "run_id" && message.type === "approve_workflow_run") continue;
    if (own(message, field)) throw scopeError(`agent-supplied ${field} cannot replace trusted authority`);
  }
  return { ...message, claim_handle: member.handle };
}

function snapshotPath() {
  return process.env.GH_AW_WORK_QUEUE_SNAPSHOT || "/tmp/gh-aw/work-queue.snapshot.json";
}

/** The run's own event payload: the Actions context, else the runner's event file. */
function runEventPayload() {
  if (global.context?.payload) return global.context.payload;
  const eventPath = process.env.GITHUB_EVENT_PATH;
  if (!eventPath || !fs.existsSync(eventPath)) return undefined;
  return JSON.parse(fs.readFileSync(eventPath, "utf8"));
}

function readClaimScopeContext() {
  if (process.env.GH_AW_WORK_QUEUE_ENABLED !== "true") return null;
  const configuredRole = process.env.GH_AW_WORK_QUEUE_ROLE;
  if (configuredRole !== undefined && !RUNTIME_ROLES.includes(configuredRole)) throw scopeError("invalid protected compiler role");
  const awContext = require("./aw_context.cjs");
  if (configuredRole === "observer") awContext.resolveWorkQueueRuntime(global.context?.payload, { role: configuredRole });
  const filename = snapshotPath();
  let stat;
  try {
    stat = fs.lstatSync(filename);
  } catch (error) {
    if (!(error instanceof Error) || !("code" in error) || error.code !== "ENOENT") throw error;
    if (configuredRole === "observer") return null;
    throw scopeError("trusted activation snapshot is missing");
  }
  if (!stat.isFile() || stat.nlink !== 1 || stat.size > 162 * 1024 * 1024) throw scopeError("trusted activation snapshot exceeds its bounded regular-file transport");
  const snapshot = require("./work_queue_mcp_server.cjs").parseSnapshotEnvelope(new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(fs.readFileSync(filename)));
  closed(snapshot, ["version", "sha", "transactionLog", "origin", "worker"], ["visible_work_ids", "role"], "trusted queue snapshot");
  if (snapshot.version !== 4 || (snapshot.sha !== null && typeof snapshot.sha !== "string") || typeof snapshot.transactionLog !== "string" || !object(snapshot.origin))
    throw scopeError("trusted activation snapshot requires the closed version-4 contract");
  if (own(snapshot, "role") && !RUNTIME_ROLES.includes(snapshot.role)) throw scopeError("snapshot role is not a valid protected compiler role");
  if (own(snapshot, "visible_work_ids")) {
    if (!Array.isArray(snapshot.visible_work_ids)) throw scopeError("snapshot visibility requires an array of Work identifiers");
    for (const id of snapshot.visible_work_ids) identifier(id, "visible Work ID");
  }
  const value = snapshot.worker;
  const role = snapshot.role ?? configuredRole ?? (value ? "worker" : "dispatcher");
  if (!RUNTIME_ROLES.includes(role) || (configuredRole !== undefined && configuredRole !== role)) throw scopeError("snapshot role conflicts with protected compiler configuration");
  if (role !== "worker" && value !== null) throw scopeError("nonworkers cannot carry a Claim assignment");
  if (role === "observer" && configuredRole !== "observer") throw scopeError("ordinary observer outputs require an explicit protected compiler role");
  if (role === "observer") return null;
  // A settled worker (its Claim already closed) has no assignment and no outputs.
  if (!value) return { assignment: null, snapshot };
  const assignment = normalizeAssignment(value);
  const payload = runEventPayload();
  if (payload !== undefined) {
    const inbound = awContext.readInboundWorkQueueAssignment(payload);
    if (!inbound || inbound.dispatch_id !== assignment.dispatch_id) throw scopeError("snapshot assignment does not match this run's work_queue_assignment dispatch");
  }
  return { assignment, snapshot };
}

function normalizeRuntimeMessage(message) {
  if (message && typeof message.type === "string") {
    const type = require("./safe_output_names.cjs").normalizeSafeOutputName(message.type);
    if (type !== message.type) message = { ...message, type };
  }
  const execution = claimExecution.getStore();
  const scope = execution || readClaimScopeContext();
  if (!scope) {
    if (process.env.GH_AW_WORK_QUEUE_ENABLED === "true" && process.env.GH_AW_WORK_QUEUE_ROLE === "observer" && ["work_queue_submit", "work_queue_dispatch_next", "work_queue_claim_finish"].includes(message?.type))
      throw scopeError("protected observers cannot emit queue-control operations");
    return message;
  }
  if (!scope.assignment) {
    const absentDispatcher = scope.snapshot?.sha === null && scope.snapshot.transactionLog === "" && scope.snapshot.worker === null;
    if (absentDispatcher && message?.type === "noop") return message;
    throw scopeError("unassigned dispatcher cannot emit worker safe outputs");
  }
  const normalized = normalizeClaimScope(message, scope.assignment);
  if (execution?.claim_handle && normalized.claim_handle !== execution.claim_handle) throw scopeError("message cannot escape its trusted per-Claim execution context");
  return normalized;
}

/** Whether a Work payload's effect_contract forbids resource writes. */
function writesProhibited(work) {
  const contract = work?.effect_contract;
  return object(contract) && (contract.kind === "none" || contract.no_writes === true);
}

/**
 * Authorize a Claim-scoped output or effect: the Claim is open, its Dispatch is bound
 * to this run attempt, its finish intent is `completed` (unless `requireCompletion:false`),
 * and any repository target equals the profile's effect scope.
 * @template {Record<string, unknown>} Message
 * @param {Message} message
 * @param {{authorize?: (request: Record<string, unknown>) => unknown, github?: unknown, context?: unknown, effect?: boolean, resource?: object, requireCompletion?: boolean}} [options]
 */
async function assertClaimAuthorized(message, options = {}) {
  const normalized = normalizeRuntimeMessage(message);
  const execution = claimExecution.getStore();
  const scope = execution || readClaimScopeContext();
  if (!scope?.assignment) return normalized;
  if (options.effect === true) {
    if (process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true" || execution?.staged === true) {
      console.log("🎭 Staged Mode Preview — blocking resource effects for read-only Claim preview");
      throw scopeError("read-only Claim preview cannot perform resource effects");
    }
    if (writesProhibited(scope.assignment.claims[0].work)) throw scopeError("Work effect_contract prohibits resource writes");
    if (options.resource === undefined) throw scopeError("resource effects require an independently resolved target");
  }
  const authorize = options.authorize || execution?.authorize || require("./finish_work_queue_claim.cjs").authorizeWorkerClaim;
  if (typeof authorize !== "function") throw scopeError("trusted per-Claim authorizer is unavailable");
  const proof = await authorize({
    github: options.github || global.github,
    context: options.context || global.context,
    assignment: scope.assignment,
    claim_handle: normalized.claim_handle,
    message: normalized,
    ...(options.resource === undefined ? {} : { resource: options.resource }),
    requireCompletion: options.requireCompletion !== false,
  });
  if (proof?.suppressed !== undefined && typeof proof.suppressed !== "boolean") throw scopeError("trusted per-Claim proof has an invalid suppression flag");
  if (proof?.suppressed === true && !SUPPRESSED_STATES.has(proof.state)) throw scopeError("trusted per-Claim proof cannot suppress an open Claim");
  if (proof?.authorized === true && proof.suppressed === true) throw scopeError("trusted per-Claim proof cannot authorize a suppressed Claim");
  if (proof?.claim_handle === normalized.claim_handle && proof.authorized === false) {
    const suppressed = proof.suppressed === true;
    const reason =
      proof.state === "cancelled"
        ? "Claim was cancelled; scoped outputs are suppressed"
        : proof.state === "completed"
          ? "Claim already completed; scoped effects cannot be replayed"
          : proof.state === "failed"
            ? "Claim finish intent is not completed; scoped effects are suppressed"
            : "same-Claim run binding is not authorized: the Claim is not open and bound to this run attempt";
    throw Object.assign(scopeError(reason), {
      code: proof.state === "cancelled" ? "claim_cancelled" : proof.state === "completed" ? "claim_already_settled" : proof.state === "failed" ? "claim_failed" : "claim_not_authorized",
      state: proof.state,
      suppressed,
    });
  }
  if (!proof || proof.authorized !== true || proof.claim_handle !== normalized.claim_handle) throw scopeError("same-Claim ownership, run binding, or repository scope is not authorized");
  return normalized;
}

function withClaimExecution(scope, callback) {
  const assignment = normalizeAssignment(scope.assignment);
  if (assignment.claims[0].handle !== scope.claim_handle) throw scopeError("execution context has a foreign Claim");
  const existing = claimExecution.getStore();
  if (existing && (existing.claim_handle !== scope.claim_handle || canonical(existing.assignment) !== canonical(assignment))) {
    throw scopeError("nested execution cannot replace its original assignment or Claim context");
  }
  return claimExecution.run({ ...scope, assignment }, callback);
}

function claimIdentity(handle, suppliedAssignment) {
  identifier(handle, "claim_handle");
  const assignment = suppliedAssignment === undefined ? currentClaimAssignment() || readClaimScopeContext()?.assignment : normalizeAssignment(suppliedAssignment);
  if (!assignment) throw scopeError("Claim identity requires its original assignment");
  const [member] = assignment.claims;
  if (member.handle !== handle) throw scopeError("Claim identity is foreign to its original assignment");
  return Object.freeze({ dispatch_id: assignment.dispatch_id, claim_id: member.claim_id, work_id: member.work_id, claim_handle: member.handle });
}

function assertClaimIdentity(identity) {
  if (canonical(claimIdentity(currentClaimHandle())) !== canonical(identity)) throw scopeError("factory cannot escape its original immutable Claim identity");
}

function claimArtifactPath(base, handle, assignment) {
  const identity = claimIdentity(handle, assignment);
  return path.join(base, "claims", crypto.createHash("sha256").update(canonical(identity)).digest("hex"));
}

function assertClaimArtifactDirectory(directory) {
  const root = path.resolve(directory);
  if (path.basename(path.dirname(root)) !== "claims" || path.basename(root) !== path.basename(claimArtifactPath("", currentClaimHandle()))) throw scopeError("artifact directory is outside its original Claim namespace");
  const base = fs.realpathSync(path.dirname(path.dirname(root)));
  if (fs.realpathSync(root) !== path.join(base, "claims", path.basename(root)) || !fs.lstatSync(root).isDirectory()) throw scopeError("artifact directory redirects its original Claim namespace");
  return root;
}

function assertClaimArtifactFile(filename, directory) {
  const root = assertClaimArtifactDirectory(directory);
  const relative = path.relative(root, path.resolve(filename));
  if (!relative || relative.startsWith("..") || path.isAbsolute(relative) || fs.realpathSync(filename) !== path.join(fs.realpathSync(root), relative)) throw scopeError("artifact file redirects or escapes its original Claim namespace");
}

function currentClaimHandle() {
  return claimExecution.getStore()?.claim_handle;
}

function currentClaimAssignment() {
  return claimExecution.getStore()?.assignment;
}

/**
 * Run one safe-output invocation with its type attached to the Claim effects it records.
 * @template T
 * @param {string} type
 * @param {() => T} callback
 * @returns {T}
 */
function withClaimOutputType(type, callback) {
  return claimOutputType.run(type, callback);
}

/**
 * @template T
 * @param {ReadonlyArray<Record<string, unknown>>} resources
 * @param {readonly string[]} operations
 * @param {() => T | Promise<T>} callback
 * @returns {Promise<T>}
 */
async function withClaimResourceEffects(resources, operations, callback) {
  if (
    !Array.isArray(resources) ||
    resources.length < 1 ||
    // Concrete file targets share the native commit/tree path bound.
    resources.length > 4096 ||
    !Array.isArray(operations) ||
    operations.length < 1 ||
    operations.length > 16 ||
    operations.some(operation => typeof operation !== "string" || !operation || operation.length > 256)
  ) {
    throw scopeError("adapter effects require bounded concrete targets and native operations");
  }
  const identity = claimIdentity(currentClaimHandle());
  const targets = resources.map(resource => require("./work_queue_effect_resource.cjs").canonicalResourceTarget(resource));
  const scope = freeze({ identity, targets, operations: [...operations] });
  return resourceExecution.run(scope, async () => {
    for (const resource of targets) {
      await assertClaimAuthorized({ type: "work_queue_resource_effect", claim_handle: identity.claim_handle, repo: resource.repository }, { effect: true, resource });
    }
    return callback();
  });
}

/** @param {string} operation */
function currentClaimResourceEffects(operation) {
  const scope = resourceExecution.getStore();
  if (!scope || !scope.operations.includes(operation)) return null;
  assertClaimIdentity(scope.identity);
  return scope.targets;
}

/**
 * Record one attempted resource effect in the current Claim's inventory; the handler
 * manager appends the succeeded ones to the effects file after the Claim's pass.
 * @returns {Record<string, any> | null}
 */
function recordClaimEffect(effect) {
  const execution = claimExecution.getStore();
  if (!execution || !Array.isArray(execution.effects)) return null;
  const outputType = claimOutputType.getStore();
  const attempt = { ...effect, ...(outputType ? { output_type: outputType } : {}), ...claimIdentity(execution.claim_handle) };
  execution.effects.push(attempt);
  const batch = claimEffectBatch.getStore();
  if (batch) {
    if (canonical(batch.identity) !== canonical(claimIdentity(execution.claim_handle))) throw scopeError("Claim effect batch escaped its original Claim context");
    batch.effects.push(attempt);
  }
  return attempt;
}

/**
 * Annotate the current Claim's recorded effects of `kind` (e.g. `{status: "unverified"}`
 * when a single readback still reports processing). Returns the number annotated.
 * @param {string} kind
 * @param {Record<string, unknown>} fields
 */
function markClaimEffects(kind, fields) {
  const execution = claimExecution.getStore();
  if (!execution || !Array.isArray(execution.effects)) return 0;
  const matching = execution.effects.filter(effect => effect.kind === kind && effect.outcome === "succeeded");
  for (const effect of matching) Object.assign(effect, fields);
  return matching.length;
}

function createClaimEffectBatch() {
  const execution = claimExecution.getStore();
  if (!execution || !Array.isArray(execution.effects)) return null;
  const batch = Object.freeze({});
  claimEffectBatches.set(batch, { identity: claimIdentity(execution.claim_handle), effects: [], discarded: false });
  return batch;
}

function withClaimEffectBatch(batch, callback) {
  if (batch === null) return callback();
  const state = batch && typeof batch === "object" ? claimEffectBatches.get(batch) : undefined;
  const execution = claimExecution.getStore();
  if (!state || !execution || state.discarded || canonical(state.identity) !== canonical(claimIdentity(execution.claim_handle))) {
    throw scopeError("Claim effect batch is unavailable or belongs to another Claim");
  }
  return claimEffectBatch.run(state, callback);
}

function discardClaimEffectBatch(batch) {
  if (batch === null) return 0;
  const state = batch && typeof batch === "object" ? claimEffectBatches.get(batch) : undefined;
  const execution = claimExecution.getStore();
  if (!state || !execution || canonical(state.identity) !== canonical(claimIdentity(execution.claim_handle))) {
    throw scopeError("Claim effect batch cannot be discarded outside its original open Claim");
  }
  const discarded = new Set(state.effects);
  if (state.effects.some(effect => !execution.effects.includes(effect))) throw scopeError("Claim effect batch is not present in the original write inventory");
  for (let index = execution.effects.length - 1; index >= 0; index--) {
    if (discarded.has(execution.effects[index])) execution.effects.splice(index, 1);
  }
  state.discarded = true;
  return discarded.size;
}

function scopedArtifactFilename(filename) {
  const handle = currentClaimHandle();
  return handle ? path.join(claimArtifactPath(path.dirname(filename), handle), path.basename(filename)) : filename;
}

module.exports = {
  normalizeAssignment,
  normalizeClaimScope,
  normalizeRuntimeMessage,
  readClaimScopeContext,
  assertClaimAuthorized,
  withClaimExecution,
  withClaimOutputType,
  claimArtifactPath,
  claimIdentity,
  assertClaimIdentity,
  assertClaimArtifactDirectory,
  assertClaimArtifactFile,
  currentClaimHandle,
  currentClaimAssignment,
  withClaimResourceEffects,
  currentClaimResourceEffects,
  recordClaimEffect,
  createClaimEffectBatch,
  withClaimEffectBatch,
  discardClaimEffectBatch,
  markClaimEffects,
  scopedArtifactFilename,
};
