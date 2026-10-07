// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { AsyncLocalStorage } = require("async_hooks");
const { identity, canonical, canonicalBytes } = require("./work_queue_codec.cjs");

const MAX_ASSIGNMENT_CLAIMS = 16;
const claimExecution = new AsyncLocalStorage();
/** @type {import("node:async_hooks").AsyncLocalStorage<{identity: ReturnType<typeof claimIdentity>, targets: readonly import("./work_queue_resource_scope.cjs").EffectResource[], operations: readonly string[], verification?: true}>} */
const resourceExecution = new AsyncLocalStorage();
/** @type {WeakMap<object, {identity: ReturnType<typeof claimIdentity>, targets: readonly import("./work_queue_resource_scope.cjs").EffectResource[]}>} */
const resourceVerifications = new WeakMap();
const own = (value, key) => Object.prototype.hasOwnProperty.call(value, key);
const object = value => value !== null && typeof value === "object" && !Array.isArray(value);

function scopeError(message, code = "claim_scope_invalid") {
  return Object.assign(new Error(`work_queue_claim_scope: ${message}`), { code });
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

function normalizeAssignment(value) {
  if (!object(value) || !Array.isArray(value.claims) || value.claims.length < 1 || value.claims.length > MAX_ASSIGNMENT_CLAIMS) {
    throw scopeError("work_queue_assignment requires an immutable array of 1 to 16 Claims; scalar assignments are unsupported");
  }
  if (canonicalBytes(value) > 48 * 1024) throw scopeError("assignment exceeds the 48 KiB host-input ceiling");
  identifier(value.dispatch_id, "dispatch_id");
  if (value.version !== 3) throw scopeError("unsupported assignment protocol; version 3 is required");
  for (const field of ["request_id", "commit_id", "policy_epoch", "pool", "worker_profile"]) identifier(value[field], field);
  const assignmentFields = new Set(["version", "dispatch_id", "request_id", "commit_id", "policy_epoch", "pool", "worker_profile", "claims"]);
  if ([...assignmentFields].some(field => !own(value, field))) throw scopeError("assignment fields must be explicit immutable JSON members");
  if (Object.keys(value).some(field => !assignmentFields.has(field))) throw scopeError("unknown assignment field");
  const handles = new Set();
  const claims = new Set();
  const works = new Set();
  for (const member of value.claims) {
    if (!object(member) || !object(member.work) || !Array.isArray(member.result_refs) || member.result_refs.length > 64) throw scopeError("each Claim must include its complete stored Work payload and bounded verified Result references");
    if (Object.keys(member).some(field => !["handle", "claim_id", "work_id", "work", "result_refs"].includes(field))) throw scopeError("unknown assignment Claim field");
    if (["handle", "claim_id", "work_id", "work", "result_refs"].some(field => !own(member, field))) throw scopeError("assignment Claim fields must be explicit JSON members");
    identifier(member.handle, "handle");
    identifier(member.claim_id, "claim_id");
    identifier(member.work_id, "work_id");
    const dependencies = new Set();
    for (const reference of member.result_refs) {
      if (!object(reference) || !object(reference.descriptor) || Object.keys(reference).some(field => !["work_id", "result_commit_id", "descriptor"].includes(field))) {
        throw scopeError("invalid verified Result reference");
      }
      identifier(reference.work_id, "Result work_id");
      identifier(reference.result_commit_id, "result_commit_id");
      if (["work_id", "result_commit_id", "descriptor"].some(field => !own(reference, field))) throw scopeError("Result reference fields must be explicit JSON members");
      if (dependencies.has(reference.work_id)) throw scopeError("duplicate verified Result reference");
      dependencies.add(reference.work_id);
    }
    if (handles.has(member.handle) || claims.has(member.claim_id) || works.has(member.work_id)) throw scopeError("duplicate assignment handle, Claim, or Work");
    handles.add(member.handle);
    claims.add(member.claim_id);
    works.add(member.work_id);
  }
  return freeze(JSON.parse(JSON.stringify(value)));
}

function normalizeClaimScope(message, assignment) {
  if (!object(message)) throw scopeError("safe-output message must be an object");
  const original = normalizeAssignment(assignment);
  let handle;
  if (own(message, "claim_handle")) {
    handle = identifier(message.claim_handle, "claim_handle");
  } else {
    if (original.claims.length !== 1) throw scopeError("claim_handle is required for the original multi-Claim assignment", "claim_scope_required");
    handle = original.claims[0].handle;
  }
  const member = original.claims.find(claim => claim.handle === handle);
  if (!member) throw scopeError("claim_handle is foreign to this immutable assignment");
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

function readClaimScopeContext() {
  const configuredRole = process.env.GH_AW_WORK_QUEUE_ROLE;
  const enabled = process.env.GH_AW_WORK_QUEUE_ENABLED === "true";
  if (!enabled) return null;
  if (configuredRole !== undefined && !["observer", "dispatcher", "worker"].includes(configuredRole)) throw scopeError("invalid protected compiler role");
  if (configuredRole === "observer") require("./aw_context.cjs").resolveWorkQueueRuntime(global.context?.payload, { role: configuredRole });
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
  const { closed } = require("./work_queue_codec.cjs");
  const snapshot = require("./work_queue_mcp_server.cjs").parseSnapshotEnvelope(new TextDecoder("utf-8", { fatal: true, ignoreBOM: true }).decode(fs.readFileSync(filename)));
  closed(snapshot, ["version", "sha", "transactionLog", "captured_at", "origin", "worker"], ["visible_work_ids", "role"], "trusted queue snapshot");
  if (
    snapshot.version !== 3 ||
    (snapshot.sha !== null && typeof snapshot.sha !== "string") ||
    typeof snapshot.transactionLog !== "string" ||
    !object(snapshot.origin) ||
    !Number.isSafeInteger(snapshot.captured_at) ||
    snapshot.captured_at < 0
  )
    throw scopeError("trusted activation snapshot requires the current closed version-3 contract");
  if (own(snapshot, "role") && !["observer", "dispatcher", "worker"].includes(snapshot.role)) throw scopeError("snapshot role is not a valid protected compiler role");
  if (own(snapshot, "visible_work_ids")) {
    if (!Array.isArray(snapshot.visible_work_ids)) throw scopeError("snapshot visibility requires a bounded array of Work identifiers");
    for (const id of snapshot.visible_work_ids) identifier(id, "visible Work ID");
  }
  const value = snapshot.worker;
  const role = snapshot.role ?? configuredRole ?? (value ? "worker" : "dispatcher");
  if (!["observer", "dispatcher", "worker"].includes(role) || (configuredRole !== undefined && configuredRole !== role)) throw scopeError("snapshot role conflicts with protected compiler configuration");
  if ((role === "worker") !== (value !== null)) throw scopeError("declared workers require their original assignment and nonworkers cannot acquire one");
  if (role === "observer" && configuredRole !== "observer") throw scopeError("ordinary observer outputs require an explicit protected compiler role");
  const runtime = require("./aw_context.cjs").resolveWorkQueueRuntime(value === null ? {} : { inputs: { work_queue_assignment: value } }, { role: configuredRole ?? snapshot.role });
  if (snapshot.transactionLog === "" && !(configuredRole === "observer" && role === "observer" && snapshot.sha === null))
    throw scopeError("work_queue_policy_missing: existing and participant queues require an installed Policy; only a protected observer may read a genuinely absent branch", "work_queue_policy_missing");
  if (role === "observer") {
    if (snapshot.transactionLog !== "") {
      const queue = require("./work_queue_replay.cjs");
      if (!queue.replayTransactions(queue.parseTransactionLog(snapshot.transactionLog)).policy) throw scopeError("work_queue_policy_missing: observer snapshot has no installed Policy", "work_queue_policy_missing");
    }
    return null;
  }
  if (!value) {
    if (enabled) return { assignment: null, snapshot };
    return null;
  }
  return { assignment: normalizeAssignment(runtime.assignment), snapshot };
}

function normalizeRuntimeMessage(message) {
  const execution = claimExecution.getStore();
  const scope = execution || readClaimScopeContext();
  if (!scope) {
    if (process.env.GH_AW_WORK_QUEUE_ENABLED === "true" && process.env.GH_AW_WORK_QUEUE_ROLE === "observer" && ["work_queue_submit", "work_queue_dispatch_next", "work_queue_claim_finish"].includes(message?.type))
      throw scopeError("protected observers cannot emit queue-control operations");
    return message;
  }
  if (!scope.assignment) throw scopeError("unassigned dispatcher cannot emit worker safe outputs");
  const normalized = normalizeClaimScope(message, scope.assignment);
  if (execution?.claim_handle && normalized.claim_handle !== execution.claim_handle) throw scopeError("message cannot escape its trusted per-Claim execution context");
  return normalized;
}

/**
 * @template {Record<string, unknown>} Message
 * @param {Message} message
 * @param {{authorize?: (request: Record<string, unknown>) => unknown, github?: unknown, context?: unknown, effect?: boolean, resource?: object, requireCompletion?: boolean}} [options]
 */
async function assertClaimAuthorized(message, options = {}) {
  const normalized = normalizeRuntimeMessage(message);
  const execution = claimExecution.getStore();
  const scope = execution || readClaimScopeContext();
  if (!scope) return normalized;
  if (options.effect === true) {
    if (execution?.closedEffectChannel) throw scopeError("Claim write channel is closed for independent delivery verification");
    if (resourceExecution.getStore()?.verification) throw scopeError("Claim resource verification is read-only");
    if (process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true" || execution?.staged === true) throw scopeError("read-only Claim preview cannot perform resource effects");
    const member = scope.assignment.claims.find(claim => claim.handle === normalized.claim_handle);
    if (member.work.effect_contract?.kind === "none" || member.work.effect_contract?.no_writes === true) throw scopeError("immutable Work effect_contract prohibits ordinary resource writes");
    if (options.resource === undefined) throw scopeError("resource effects require their independently resolved immutable Work target");
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
  if (proof?.suppressed === true && !["cancelled", "result"].includes(proof.state)) throw scopeError("trusted per-Claim proof cannot suppress a nonterminal authorization failure");
  if (proof?.authorized === true && (proof.suppressed === true || ["cancelled", "result"].includes(proof.state))) throw scopeError("trusted per-Claim proof cannot authorize a terminal Claim");
  if (proof?.claim_handle === normalized.claim_handle && proof.authorized === false) {
    const cancelled = proof.state === "cancelled" && proof.suppressed === true;
    const settled = proof.state === "result" && proof.suppressed === true;
    throw Object.assign(
      scopeError(
        cancelled ? "Claim was cancelled; scoped outputs are suppressed" : settled ? "Claim Result is already settled; scoped effects cannot be replayed" : "same-Claim Completion, run binding, or resource authority is not available"
      ),
      {
        code: cancelled ? "claim_cancelled" : settled ? "claim_already_settled" : "claim_not_authorized",
        state: proof.state,
        suppressed: proof.suppressed === true,
      }
    );
  }
  if (!proof || proof.authorized !== true || proof.claim_handle !== normalized.claim_handle) throw scopeError("same-Claim ownership, Completion, run binding, or resource scope is not authorized");
  return normalized;
}

function withClaimExecution(scope, callback) {
  const assignment = normalizeAssignment(scope.assignment);
  if (!assignment.claims.some(claim => claim.handle === scope.claim_handle)) throw scopeError("execution context has a foreign Claim");
  const existing = claimExecution.getStore();
  if (existing && (existing.claim_handle !== scope.claim_handle || canonical(existing.assignment) !== canonical(assignment))) {
    throw scopeError("nested execution cannot replace its original immutable assignment or Claim context");
  }
  return claimExecution.run({ ...scope, assignment }, callback);
}

function claimIdentity(handle, suppliedAssignment) {
  identifier(handle, "claim_handle");
  const assignment = suppliedAssignment === undefined ? currentClaimAssignment() || readClaimScopeContext()?.assignment : normalizeAssignment(suppliedAssignment);
  if (!assignment) throw scopeError("Claim identity requires its original immutable assignment");
  const member = assignment.claims.find(claim => claim.handle === handle);
  if (!member) throw scopeError("Claim identity is foreign to its original immutable assignment");
  return Object.freeze({ dispatch_id: assignment.dispatch_id, claim_id: member.claim_id, work_id: member.work_id, claim_handle: member.handle });
}

function assertClaimIdentity(identity) {
  if (canonical(claimIdentity(currentClaimHandle())) !== canonical(identity)) throw scopeError("factory cannot escape its original immutable Claim identity");
}

function receiptMatchesClaim(receipt, claim) {
  const assignment = currentClaimAssignment();
  if (!assignment) return false;
  const original = assignment.claims.find(member => member.handle === claim?.handle);
  return (
    !!receipt &&
    !!claim &&
    !!original &&
    claim.claim_id === original.claim_id &&
    claim.work_id === original.work_id &&
    receipt.claim_handle === claim.handle &&
    receipt.claim_id === claim.claim_id &&
    receipt.work_id === claim.work_id &&
    receipt.dispatch_id === assignment.dispatch_id
  );
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
    resources.length > 128 ||
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

/**
 * Register only independently read-back adapter receipts, never agent messages.
 * @template {Record<string, unknown>} T
 * @param {T} proof
 * @returns {T}
 */
function createClaimResourceVerification(proof) {
  const identity = claimIdentity(currentClaimHandle());
  if (proof.verified !== true || proof.claim_handle !== identity.claim_handle || !object(proof.authority_resource)) throw scopeError("adapter verification requires an independently resolved authority receipt");
  const resources = proof.authority_resources === undefined ? [proof.authority_resource] : proof.authority_resources;
  if (!Array.isArray(resources) || resources.length < 1 || resources.length > 128) throw scopeError("adapter verification requires bounded complete authority targets");
  const targets = resources.map(resource => require("./work_queue_resource_scope.cjs").validateEffectResource(require("./work_queue_effect_resource.cjs").canonicalResourceTarget(resource)));
  if (targets.some(target => target.host !== "github.com" || !target.repository_id)) throw scopeError("adapter verification requires independently resolved native repository identities");
  if (canonical(targets[0]) !== canonical(require("./work_queue_effect_resource.cjs").canonicalResourceTarget(proof.authority_resource))) throw scopeError("adapter verification has conflicting authority targets");
  resourceVerifications.set(proof, { identity, targets: freeze(targets) });
  return freeze(proof);
}

/**
 * @template T
 * @param {unknown} proof
 * @param {() => T | Promise<T>} callback
 * @param {{authorize?: (request: Record<string, unknown>) => unknown, context?: unknown, github?: unknown}} [options]
 * @returns {Promise<T>}
 */
async function withClaimResourceVerification(proof, callback, options = {}) {
  const facts = proof !== null && typeof proof === "object" ? resourceVerifications.get(proof) : undefined;
  if (!facts) throw scopeError("adapter verification requires its private in-process authority receipt");
  assertClaimIdentity(facts.identity);
  return resourceExecution.run({ ...facts, operations: [], verification: true }, async () => {
    for (const resource of facts.targets) {
      await assertClaimAuthorized({ type: "work_queue_resource_verification", claim_handle: facts.identity.claim_handle, repo: resource.repository }, { ...options, resource });
    }
    return callback();
  });
}

/** @param {unknown} resource */
function isProtectedClaimResourceTarget(resource) {
  const scope = resourceExecution.getStore();
  if (!scope) return false;
  assertClaimIdentity(scope.identity);
  return scope.targets.some(target => canonical(target) === canonical(resource));
}

/** @param {string} operation */
function currentClaimResourceEffects(operation) {
  const scope = resourceExecution.getStore();
  if (!scope || !scope.operations.includes(operation)) return null;
  assertClaimIdentity(scope.identity);
  return scope.targets;
}

const closedEffectChannels = new WeakMap();

function closeClaimEffectChannel() {
  const execution = claimExecution.getStore();
  if (!execution || !Array.isArray(execution.effects)) throw scopeError("closing a write channel requires trusted scoped effect accounting");
  if (execution.closedEffectChannel) return execution.closedEffectChannel;
  const channel = Object.freeze({});
  closedEffectChannels.set(channel, { identity: claimIdentity(execution.claim_handle), effects: canonical(execution.effects) });
  execution.closedEffectChannel = channel;
  return channel;
}

function claimEffectChannelMatches(channel, effects, assignment, handle) {
  const closed = channel && closedEffectChannels.get(channel);
  return !!closed && canonical(closed.identity) === canonical(claimIdentity(handle, assignment)) && closed.effects === canonical(effects);
}

function recordClaimEffect(effect) {
  const execution = claimExecution.getStore();
  if (!execution || !Array.isArray(execution.effects)) return null;
  if (execution.closedEffectChannel) throw scopeError("Claim write channel is closed for independent delivery verification");
  const attempt = { ...effect, ...claimIdentity(execution.claim_handle) };
  execution.effects.push(attempt);
  return attempt;
}

function scopedArtifactFilename(filename) {
  const handle = currentClaimHandle();
  return handle ? path.join(claimArtifactPath(path.dirname(filename), handle), path.basename(filename)) : filename;
}

module.exports = {
  MAX_ASSIGNMENT_CLAIMS,
  normalizeAssignment,
  normalizeClaimScope,
  normalizeRuntimeMessage,
  readClaimScopeContext,
  assertClaimAuthorized,
  withClaimExecution,
  claimArtifactPath,
  claimIdentity,
  assertClaimIdentity,
  receiptMatchesClaim,
  assertClaimArtifactDirectory,
  assertClaimArtifactFile,
  currentClaimHandle,
  currentClaimAssignment,
  withClaimResourceEffects,
  createClaimResourceVerification,
  withClaimResourceVerification,
  currentClaimResourceEffects,
  isProtectedClaimResourceTarget,
  recordClaimEffect,
  closeClaimEffectChannel,
  claimEffectChannelMatches,
  scopedArtifactFilename,
};
