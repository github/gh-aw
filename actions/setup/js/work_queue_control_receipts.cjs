// @ts-check
"use strict";

const { canonical, closed, digest } = require("./work_queue_codec.cjs");
const { normalizeAssignment, normalizeClaimScope } = require("./work_queue_claim_scope.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");
const { normalizeDispatchParameters, requestForIntent, requestIdForIntent } = require("./work_queue_intents.cjs");
const { loadQueue, validateStoredAssignment, expectedWorkerRun, bindingForRun } = require("./work_queue_binding.cjs");
const { authenticatePublisher, fetchNativeRunAttempt, validateNativeRun } = require("./work_queue_native.cjs");

const inventories = new WeakMap();
const TYPES = { submit: "work_queue_submit", dispatch_next: "work_queue_dispatch_next" };

function freeze(value) {
  if (value && typeof value === "object") {
    for (const child of Object.values(value)) freeze(child);
    Object.freeze(value);
  }
  return value;
}

function controlReceiptForRequest(state, requestId) {
  const commit = state?.requests.get(requestId);
  if (!commit || commit.actor.role !== "worker" || !Object.hasOwn(TYPES, commit.request.kind)) return null;
  const dispatch = state.dispatches.get(commit.actor.dispatch_id);
  const member = dispatch?.claims.find(claim => claim.handle === commit.actor.claim_handle);
  const work = member && state.works.get(member.work_id);
  const claim = member && state.claims.get(member.claim_id);
  if (!member || claim?.state !== "completed" || work?.state !== "completed" || work.claim_id !== member.claim_id || !work.completion_id) throw new Error("work_queue_control_receipt_scope_invalid");
  return freeze({
    version: 3,
    type: TYPES[commit.request.kind],
    dispatch_id: dispatch.dispatch_id,
    claim_handle: member.handle,
    claim_id: member.claim_id,
    work_id: member.work_id,
    completion_id: work.completion_id,
    request_id: requestId,
    commit_id: commit.id,
    parameters_digest: digest(commit.request.parameters),
    writes: {
      works: commit.operations.filter(operation => operation.kind === "Work").length,
      claims: commit.operations.filter(operation => operation.kind === "Claim").length,
      dispatches: new Set(commit.operations.filter(operation => operation.kind === "Claim").map(operation => operation.dispatch_id)).size,
    },
  });
}

function claimControlReceipts(state, supplied, handle) {
  const { assignment, dispatch } = validateStoredAssignment(state, supplied, { allowReleased: true });
  const normalized = normalizeClaimScope({ claim_handle: handle }, assignment);
  if (!dispatch.run) throw new Error("work_queue_binding_not_durable");
  const original = { ...dispatch.run, role: "worker", dispatch_id: assignment.dispatch_id, claim_handle: normalized.claim_handle, authenticated: true, roles: ["worker"] };
  const actor = actorFromContext(original);
  return [...state.requests.values()].filter(commit => Object.hasOwn(TYPES, commit.request.kind) && canonical(commit.actor) === canonical(actor)).map(commit => controlReceiptForRequest(state, commit.request.id));
}

async function readClaimQueueControls(options) {
  const assignment = normalizeAssignment(options.assignment);
  const normalized = normalizeClaimScope({ ...(Object.hasOwn(options, "claim_handle") ? { claim_handle: options.claim_handle } : {}) }, assignment);
  const member = assignment.claims.find(claim => claim.handle === normalized.claim_handle);
  const configured = {
    ...options,
    githubClient: options.githubClient || options.github || global.github,
    context: options.context || global.context,
    core: options.core || global.core,
    policyProposal: undefined,
    initializationContext: undefined,
  };
  const latest = await loadQueue(configured);
  const { dispatch, profile } = validateStoredAssignment(latest.projection, assignment, { allowReleased: true });
  if (!dispatch.run) throw new Error("work_queue_binding_not_durable");
  const caller = await authenticatePublisher({ ...configured, role: "reconciler" });
  if (caller.run_id === dispatch.run.run_id && caller.run_attempt !== 1) throw new Error("rerun_not_authorized");
  const expected = { ...expectedWorkerRun(assignment, profile, configured.context), run_id: dispatch.run.run_id };
  const run = await fetchNativeRunAttempt(configured.githubClient, expected.repository, dispatch.run.run_id);
  const proof = validateNativeRun(run, expected);
  if (canonical(dispatch.run) !== canonical(bindingForRun(proof, expected))) throw new Error("run_binding_conflict");
  const claim = latest.projection.claims.get(member.claim_id);
  const work = latest.projection.works.get(member.work_id);
  if (claim?.state !== "completed" || work?.state !== "completed" || work.claim_id !== member.claim_id || !work.completion_id || work.barrier === "failed") throw new Error("work_queue_control_receipt_scope_invalid");
  const original = { ...dispatch.run, role: "worker", dispatch_id: assignment.dispatch_id, claim_handle: member.handle, authenticated: true, roles: ["worker"], created_at: Date.parse(run.created_at) };
  const controls = claimControlReceipts(latest.projection, assignment, member.handle);
  const inventory = freeze({
    version: 3,
    verified: true,
    dispatch_id: assignment.dispatch_id,
    claim_handle: member.handle,
    claim_id: member.claim_id,
    work_id: member.work_id,
    completion_id: work.completion_id,
    tip: latest.projection.tip,
    controls,
    controls_digest: digest(controls),
  });
  inventories.set(inventory, { state: latest.projection, assignment, context: original });
  return inventory;
}

async function verifyClaimQueueControl(options) {
  const assignment = normalizeAssignment(options.assignment);
  const message = normalizeClaimScope(options.message, assignment);
  closed(message, ["type", "intent_id", "parameters", "claim_handle"], [], "queue control readback message");
  const kind = Object.keys(TYPES).find(key => TYPES[key] === message.type);
  if (!kind || (options.claim_handle !== undefined && message.claim_handle !== options.claim_handle)) throw new Error("work_queue_control_receipt_scope_invalid");
  const inventory = options.inventory === undefined ? await readClaimQueueControls({ ...options, assignment, claim_handle: message.claim_handle }) : options.inventory;
  const facts = inventories.get(inventory);
  if (!facts || canonical(facts.assignment) !== canonical(assignment) || inventory.claim_handle !== message.claim_handle) throw new Error("work_queue_control_inventory_untrusted");
  const id = requestIdForIntent(facts.context, message.intent_id);
  const prior = facts.state.requests.get(id);
  const receipt = inventory.controls.find(control => control.request_id === id);
  if (!prior || !receipt || prior.request.kind !== kind) return { verified: false, effects: "unknown", reason: "queue_control_not_committed" };
  const policy = facts.state.transactions.flatMap(commit => commit.operations).find(operation => operation.kind === "Policy" && operation.epoch === prior.policy_epoch)?.policy;
  if (!policy) throw new Error("work_queue_policy_missing");
  const parameters = kind === "submit" ? require("./work_queue_dispatch.cjs").acceptedSubmissionParameters(facts.state, facts.context, message.parameters, prior) : normalizeDispatchParameters(message.parameters, policy, 4096);
  const request = requestForIntent(facts.context, message.intent_id, kind, parameters);
  if (prior.request.fingerprint !== request.fingerprint || canonical(prior.request.parameters) !== canonical(request.parameters) || canonical(prior.actor) !== canonical(actorFromContext(facts.context))) {
    return { verified: false, effects: "unknown", reason: "queue_control_request_mismatch" };
  }
  return { verified: true, claim_handle: message.claim_handle, resource: { kind: "queue_commit", repository: facts.context.repository, id: prior.id }, evidence: receipt };
}

module.exports = { controlReceiptForRequest, claimControlReceipts, readClaimQueueControls, verifyClaimQueueControl };
