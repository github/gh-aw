// @ts-check
"use strict";

const fs = require("fs");
const path = require("path");
const crypto = require("crypto");
const { AsyncLocalStorage } = require("async_hooks");
const { identity, canonical, canonicalBytes } = require("./work_queue_codec.cjs");

const MAX_ASSIGNMENT_CLAIMS = 16;
const claimExecution = new AsyncLocalStorage();
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
  const enabled = process.env.GH_AW_WORK_QUEUE_ENABLED === "true";
  if (!enabled) return null;
  const filename = snapshotPath();
  if (!fs.existsSync(filename)) {
    if (enabled) throw scopeError("trusted activation snapshot is missing");
    return null;
  }
  const { parseStrictJSON, closed } = require("./work_queue_codec.cjs");
  const snapshot = parseStrictJSON(fs.readFileSync(filename, "utf8"));
  closed(snapshot, ["version", "sha", "transactionLog", "captured_at", "origin", "worker"], ["visible_work_ids"], "trusted queue snapshot");
  if (snapshot.version !== 3 || !object(snapshot.origin) || !Number.isSafeInteger(snapshot.captured_at) || snapshot.captured_at < 0) throw scopeError("trusted activation snapshot requires the current closed version-3 contract");
  const value = snapshot.worker;
  if (!value) {
    if (enabled) return { assignment: null, snapshot };
    return null;
  }
  return { assignment: normalizeAssignment(value), snapshot };
}

function normalizeRuntimeMessage(message) {
  const execution = claimExecution.getStore();
  const scope = execution || readClaimScopeContext();
  if (!scope) return message;
  if (!scope.assignment) throw scopeError("unassigned dispatcher cannot emit worker safe outputs");
  const normalized = normalizeClaimScope(message, scope.assignment);
  if (execution?.claim_handle && normalized.claim_handle !== execution.claim_handle) throw scopeError("message cannot escape its trusted per-Claim execution context");
  return normalized;
}

/** @param {Record<string, any>} message @param {Record<string, any>} [options] */
async function assertClaimAuthorized(message, options = {}) {
  const normalized = normalizeRuntimeMessage(message);
  const execution = claimExecution.getStore();
  const scope = execution || readClaimScopeContext();
  if (!scope) return normalized;
  if (options.effect === true) {
    if (process.env.GH_AW_SAFE_OUTPUTS_STAGED === "true" || execution?.staged === true) throw scopeError("read-only Claim preview cannot perform resource effects");
    const member = scope.assignment.claims.find(claim => claim.handle === normalized.claim_handle);
    if (member.work.effect_contract?.kind === "none" || member.work.effect_contract?.no_writes === true) throw scopeError("immutable Work effect_contract prohibits ordinary resource writes");
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

function recordClaimEffect(effect) {
  const execution = claimExecution.getStore();
  if (!execution || !Array.isArray(execution.effects)) return null;
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
  recordClaimEffect,
  scopedArtifactFilename,
};
