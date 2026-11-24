// @ts-check
"use strict";
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("replay");
const { iterateLines, iterateJsonlEvents } = require("./jsonl_helpers.cjs");

const { createHash } = require("node:crypto");
const { canonical, closed, digest, identity, integer, parseStrictJSON, queueError, utf8Compare } = require("./work_queue_codec.cjs");
const { EFFECTS_BYTES, LEDGER_BYTES, OPERATIONS_PER_COMMIT, boundedBytes } = require("./work_queue_limits.cjs");
const { PROFILE_WORKFLOW, decimal, validateActor } = require("./work_queue_policy.cjs");
const {
  ISSUE_CONDITIONS,
  WORK_OPTIONAL,
  WORK_REQUIRED,
  approvalGate,
  backingIssueOwner,
  continuationChild,
  gateKey,
  issueKey,
  trackAdmittedWork,
  trackBackingIssue,
  trackWorkState,
  validateGraphAdmission,
  validateResource,
  validateWork,
  workDefinition,
} = require("./work_queue_graph.cjs");

/** @type {4} */
const PROTOCOL_VERSION = 4;

const CLAIM_CANCELLATION_REASONS = ["worker_failed", "worker_terminated", "launch_failed", "run_terminal", "run_missing", "operator"];
const WORK_CANCELLATION_REASONS = ["attempts_exhausted", "operator", "related_issue_closed", "worker_contract_obsolete"];
const RELEASE_REASONS = ["launch_failed", "worker_terminated", "run_terminal", "run_missing", "operator"];
// The worker revision a dispatcher recorded for its launched run: a full Git object ID.
const LAUNCH_REVISION = /^(?:[0-9a-f]{40}|[0-9a-f]{64})$/;

const OP_FIELDS = {
  Work: [WORK_REQUIRED, WORK_OPTIONAL],
  Observation: [
    ["kind", "observation_id", "resource", "condition", "state"],
    ["state_reason", "merged", "merge_commit", "approved_by"],
  ],
  Claim: [["kind", "work_id", "claim_id", "dispatch_id", "handle"], []],
  Dispatch: [["kind", "dispatch_id", "state", "run_id", "revision"], []],
  Bound: [["kind", "dispatch_id", "run_id", "run_attempt"], []],
  Completion: [["kind", "work_id", "claim_id", "dispatch_id", "claim_handle", "effects"], []],
  ClaimCancellation: [["kind", "work_id", "claim_id", "reason"], []],
  WorkCancellation: [["kind", "work_id", "reason"], ["claim_id"]],
  Release: [["kind", "dispatch_id", "reason"], []],
  Checkpoint: [["kind", "prior_tip", "state"], []],
  IssueLink: [["kind", "work_id", "resource"], ["claim_id"]],
  IssueComment: [["kind", "work_id", "comment_id"], ["claim_id"]],
};

// Request kind -> permitted operation kinds and closed parameter fields.
const REQUEST_KINDS = {
  submit: { operations: ["Work"], parameters: ["work_ids"] },
  dispatch_next: { operations: ["Observation", "Claim"], parameters: ["pool", "max_claims", "max_dispatches", "concurrency", "per_account_limit", "profiles"] },
  launch: { operations: ["Dispatch", "Release", "WorkCancellation"], parameters: ["dispatch_id"] },
  bind: { operations: ["Bound"], parameters: ["dispatch_id"] },
  // A finish may admit one continuation Work right after each paired Completion.
  finish: { operations: ["Completion", "ClaimCancellation", "WorkCancellation", "Work"], parameters: ["dispatch_id"] },
  release: { operations: ["Release", "WorkCancellation"], parameters: ["dispatch_id", "reason"] },
  cancel_work: { operations: ["WorkCancellation"], parameters: ["work_ids", "reason"] },
  checkpoint: { operations: ["Checkpoint"], parameters: ["prior_tip"] },
  issue_link: { operations: ["IssueLink", "IssueComment"], parameters: ["operations"] },
  migrate: { operations: ["Work"], parameters: ["work_ids"] },
};

const OP_ROLES = {
  Work: ["producer", "dispatcher", "administrator", "worker"],
  Observation: ["dispatcher", "administrator"],
  Claim: ["dispatcher", "administrator"],
  Dispatch: ["dispatcher", "administrator"],
  Bound: ["worker"],
  Completion: ["worker"],
  ClaimCancellation: ["worker"],
  WorkCancellation: ["producer", "administrator", "reconciler", "worker", "dispatcher"],
  Release: ["dispatcher", "reconciler", "administrator"],
  Checkpoint: ["administrator", "reconciler"],
  IssueLink: ["projector"],
  IssueComment: ["projector"],
};

const WORK_STATES = ["available", "claimed", "completed", "cancelled"];
const CLAIM_STATES = ["open", "completed", "cancelled"];
const DISPATCH_STATES = ["reserved", "started", "bound", "closed"];
const CHECKPOINT_FIELDS = ["repository", "tip", "commits", "works", "claims", "dispatches", "observations", "requests", "work_creators"];
const WORK_RECORD_OPTIONAL = [...WORK_OPTIONAL, "claim_id", "completion_id", "effects", "cancellation_reason", "cancellation_commit_id", "issue_comment"];
const CLAIM_RECORD_REQUIRED = ["claim_id", "work_id", "dispatch_id", "handle", "state", "commit_id", "request_id"];
const CLAIM_RECORD_OPTIONAL = ["terminal_commit_id", "cancellation_reason", "effects", "issue_comment"];
const DISPATCH_RECORD_FIELDS = ["dispatch_id", "request_id", "commit_id", "pool", "worker_profile", "workflow", "state", "run", "launch", "claims"];

/**
 * @typedef {{role: string, repository: string, workflow?: string, run_id?: string, run_attempt?: number}} QueueActor
 * @typedef {{id: string, kind: string, parameters: Record<string, any>}} QueueRequest
 * @typedef {{version: 4, id: string, previous: string | null, request: QueueRequest, actor: QueueActor, at: number, operations: Array<Record<string, any>>}} QueueCommit
 */

function sha256(text) {
  return createHash("sha256").update(text, "utf8").digest("hex");
}

/** @returns {string} */
function requestId(actor, kind, parameters) {
  return `request:${digest({ actor, kind, parameters })}`;
}

/** @param {string | null} previous @param {string} request */
function commitId(previous, request) {
  return `q_${sha256(previous ? `${previous}\n${request}` : request)}`;
}

/** Commit ID a request would receive when appended to this projection. */
function proposedCommitId(state, request) {
  return commitId(state.tip || null, request.id);
}

function reason(value, allowed, name) {
  if (typeof value !== "string" || !allowed.includes(value)) throw queueError("reason_invalid", `${name} reason must be one of ${allowed.join(", ")}`);
  return value;
}

function workIds(value) {
  if (!Array.isArray(value) || value.length < 1 || value.length > OPERATIONS_PER_COMMIT || new Set(value).size !== value.length) throw queueError("request_invalid", "work_ids requires 1..256 unique Work identities");
  for (const id of value) identity(id, "Work ID");
}

function validateObservation(operation) {
  validateResource(operation.resource);
  const conditions = operation.resource.kind === "issue" ? ISSUE_CONDITIONS : ["merged"];
  if (!conditions.includes(operation.condition) || !["ready", "waiting", "failed", "unknown"].includes(operation.state)) throw queueError("observation_invalid", "typed condition/state is invalid");
  if (Object.hasOwn(operation, "state_reason") && !["completed", "not_planned", "reopened"].includes(operation.state_reason)) throw queueError("observation_invalid", "invalid Issue state_reason");
  if (Object.hasOwn(operation, "merged") && typeof operation.merged !== "boolean") throw queueError("observation_invalid", "merged must be boolean");
  if (Object.hasOwn(operation, "merge_commit")) identity(operation.merge_commit, "merge commit");
  if (operation.resource.kind === "issue" && (Object.hasOwn(operation, "merged") || Object.hasOwn(operation, "merge_commit"))) throw queueError("observation_invalid", "an Issue cannot carry pull request merge evidence");
  // A ready approval names the writer who closed the Issue as completed; nothing else carries a closer.
  const approval = operation.condition === "approved" && operation.state === "ready";
  if (approval !== Object.hasOwn(operation, "approved_by")) throw queueError("observation_invalid", "approved_by belongs exactly to a ready approved Issue gate");
  if (approval) decimal(operation.approved_by, "approved_by", "observation_invalid");
  if (operation.state === "ready") {
    if (operation.resource.kind === "pull_request" && (operation.merged !== true || !operation.merge_commit)) throw queueError("observation_invalid", "a ready pull request gate requires its merge commit");
    if ((operation.condition === "completed" || approval) && operation.state_reason !== "completed") throw queueError("observation_invalid", "a ready completed Issue gate requires state_reason completed");
  }
}

/** Structural validation of one closed v4 operation. */
function validateOperation(operation) {
  if (!operation || typeof operation !== "object" || Array.isArray(operation) || typeof operation.kind !== "string" || !Object.hasOwn(OP_FIELDS, operation.kind)) throw queueError("request_invalid", "unknown queue operation");
  closed(operation, OP_FIELDS[operation.kind][0], OP_FIELDS[operation.kind][1], operation.kind, "request_invalid");
  for (const field of ["work_id", "claim_id", "dispatch_id", "handle", "claim_handle", "observation_id", "comment_id", "prior_tip"]) if (Object.hasOwn(operation, field)) identity(operation[field], field);
  switch (operation.kind) {
    case "Work":
      validateWork(operation);
      break;
    case "Observation":
      validateObservation(operation);
      break;
    case "Dispatch":
      if (operation.state !== "started") throw queueError("request_invalid", "a Dispatch operation records only the started launch");
      decimal(operation.run_id, "launched run ID", "request_invalid");
      if (typeof operation.revision !== "string" || !LAUNCH_REVISION.test(operation.revision)) throw queueError("request_invalid", "a started Dispatch records the launched run's full commit revision");
      break;
    case "Bound":
      decimal(operation.run_id, "bound run ID", "request_invalid");
      integer(operation.run_attempt, 1, 4096, "bound run attempt");
      break;
    case "Completion":
      if (!Array.isArray(operation.effects) || operation.effects.some(effect => !effect || typeof effect !== "object" || Array.isArray(effect))) throw queueError("request_invalid", "Completion effects must be an array of objects");
      boundedBytes(operation.effects, EFFECTS_BYTES, "Completion effects");
      break;
    case "ClaimCancellation":
      reason(operation.reason, CLAIM_CANCELLATION_REASONS, "ClaimCancellation");
      break;
    case "WorkCancellation":
      reason(operation.reason, WORK_CANCELLATION_REASONS, "WorkCancellation");
      break;
    case "Release":
      reason(operation.reason, RELEASE_REASONS, "Release");
      break;
    case "Checkpoint":
      if (!operation.state || typeof operation.state !== "object" || Array.isArray(operation.state)) throw queueError("checkpoint_invalid", "checkpoint requires a projection state object");
      break;
    case "IssueLink":
      if (validateResource(operation.resource).kind !== "issue") throw queueError("resource_invalid", "IssueLink requires an Issue");
      break;
  }
  return operation;
}

function validateParameters(kind, parameters) {
  closed(parameters, REQUEST_KINDS[kind].parameters, [], `${kind} parameters`, "request_invalid");
  switch (kind) {
    case "submit":
    case "migrate":
      workIds(parameters.work_ids);
      break;
    case "dispatch_next": {
      identity(parameters.pool, "pool");
      integer(parameters.max_claims, 1, OPERATIONS_PER_COMMIT, "max_claims");
      integer(parameters.max_dispatches, 1, OPERATIONS_PER_COMMIT, "max_dispatches");
      integer(parameters.concurrency, 1, 4096, "concurrency");
      integer(parameters.per_account_limit, 0, 4096, "per_account_limit");
      const profiles = parameters.profiles;
      if (!profiles || typeof profiles !== "object" || Array.isArray(profiles) || !Object.keys(profiles).length || Object.keys(profiles).length > 256) throw queueError("request_invalid", "dispatch_next requires 1..256 worker profiles");
      for (const [name, profile] of Object.entries(profiles)) {
        identity(name, "worker profile");
        closed(profile, ["workflow"], [], `profile ${name}`, "request_invalid");
        if (typeof profile.workflow !== "string" || !PROFILE_WORKFLOW.test(profile.workflow)) throw queueError("request_invalid", "profile workflow must be .github/workflows/<name>.lock.yml");
      }
      break;
    }
    case "launch":
    case "bind":
    case "finish":
      identity(parameters.dispatch_id, "dispatch ID");
      break;
    case "release":
      identity(parameters.dispatch_id, "dispatch ID");
      reason(parameters.reason, RELEASE_REASONS, "release");
      break;
    case "cancel_work":
      workIds(parameters.work_ids);
      reason(parameters.reason, WORK_CANCELLATION_REASONS, "cancel_work");
      break;
    case "checkpoint":
      identity(parameters.prior_tip, "prior tip");
      break;
    case "issue_link":
      if (!Array.isArray(parameters.operations) || parameters.operations.length < 1 || parameters.operations.length > OPERATIONS_PER_COMMIT) throw queueError("request_invalid", "issue_link requires 1..256 operations");
      break;
  }
}

/** @returns {QueueRequest} */
function validateRequest(request, actor) {
  closed(request, ["id", "kind", "parameters"], [], "request", "request_invalid");
  if (typeof request.kind !== "string" || !Object.hasOwn(REQUEST_KINDS, request.kind)) throw queueError("request_invalid", "unsupported request kind");
  validateParameters(request.kind, request.parameters);
  if (request.id !== requestId(actor, request.kind, request.parameters)) throw queueError("request_invalid", "request ID does not bind actor, kind and parameters");
  return request;
}

/**
 * @param {string} kind
 * @param {QueueActor} actor
 * @param {Record<string, any>} parameters
 * @returns {QueueRequest}
 */
function newRequest(kind, actor, parameters) {
  validateActor(actor);
  const stable = parseStrictJSON(canonical(parameters));
  return validateRequest({ id: requestId(actor, kind, stable), kind, parameters: stable }, actor);
}

function authorizeOperation(commit, operation) {
  const role = commit.actor.role;
  const kind = commit.request.kind;
  const unauthorized = () => queueError("unauthorized_operation", `${role} cannot publish ${operation.kind} through ${kind}`);
  if (!REQUEST_KINDS[kind].operations.includes(operation.kind)) throw queueError("request_invalid", `${kind} cannot carry ${operation.kind}`);
  if (!OP_ROLES[operation.kind].includes(role)) throw unauthorized();
  if (operation.kind === "WorkCancellation" && ((role === "worker" && kind !== "finish") || (role === "dispatcher" && kind !== "launch"))) throw unauthorized();
  if (operation.kind === "Release" && role === "dispatcher" && operation.reason !== "launch_failed") throw unauthorized();
  if (kind === "migrate" && role !== "administrator") throw unauthorized();
  // Only a finish hands off: submit and migrate cannot mint a continuation (or, structurally, its approval gate).
  if (operation.kind === "Work" && kind !== "finish" && Object.hasOwn(operation, "continuation")) throw queueError("request_invalid", `${kind} cannot carry a continuation`);
}

/** @returns {QueueCommit} */
function validateCommit(commit) {
  if (!commit || typeof commit !== "object" || Array.isArray(commit)) throw queueError("ledger_invalid", "QueueCommit must be an object");
  if (commit.version !== PROTOCOL_VERSION) throw queueError("unsupported_protocol", "work queue protocol v4 required; run the v3 converter");
  closed(commit, ["version", "id", "previous", "request", "actor", "at", "operations"], [], "QueueCommit");
  identity(commit.id, "commit ID");
  if (commit.previous !== null) identity(commit.previous, "predecessor");
  integer(commit.at, 0, Number.MAX_SAFE_INTEGER, "commit time");
  validateActor(commit.actor);
  validateRequest(commit.request, commit.actor);
  if (!Array.isArray(commit.operations) || commit.operations.length < 1 || commit.operations.length > OPERATIONS_PER_COMMIT) throw queueError("request_invalid", `QueueCommit requires 1..${OPERATIONS_PER_COMMIT} operations`);
  for (const operation of commit.operations) {
    validateOperation(operation);
    authorizeOperation(commit, operation);
  }
  if (commit.request.kind === "issue_link" && canonical(commit.request.parameters.operations) !== canonical(commit.operations)) throw queueError("request_invalid", "issue_link operations differ from its request");
  if (commit.id !== commitId(commit.previous, commit.request.id)) throw queueError("ledger_invalid", "commit ID does not bind its predecessor and request");
  return commit;
}

function newState() {
  return {
    repository: "",
    tip: "",
    // Commit ordinal of the next commit, including history folded into a checkpoint.
    commits: 0,
    /** @type {Map<string, any>} */
    works: new Map(),
    /** @type {Map<string, any>} */
    claims: new Map(),
    /** @type {Map<string, any>} */
    dispatches: new Map(),
    /** @type {Map<string, any>} */
    observations: new Map(),
    /** @type {Set<string>} */
    requests: new Set(),
    /** @type {Map<string, QueueActor>} */
    workCreators: new Map(),
    /** @type {QueueCommit[]} */
    transactions: [],
    stats: { work: 0, available: 0, claimed: 0, completed: 0, cancelled: 0, claims: 0, dispatches: 0, transactions: 0, nodes: 0 },
  };
}

/** @typedef {ReturnType<typeof newState>} QueueState */

/** Copy a projection for a speculative append; ledger commits are shared, records are not. */
function cloneState(state) {
  const copy = newState();
  copy.repository = state.repository;
  copy.tip = state.tip;
  copy.commits = state.commits;
  copy.works = new Map([...state.works].map(([id, work]) => [id, { ...work }]));
  copy.claims = new Map([...state.claims].map(([id, claim]) => [id, { ...claim }]));
  copy.dispatches = new Map([...state.dispatches].map(([id, dispatch]) => [id, { ...dispatch, claims: [dispatch.claims[0]] }]));
  copy.observations = new Map(state.observations);
  copy.requests = new Set(state.requests);
  copy.workCreators = new Map(state.workCreators);
  copy.transactions = [...state.transactions];
  copy.stats = { ...state.stats };
  return copy;
}

function setWorkState(state, work, next) {
  const previous = work.state;
  work.state = next;
  trackWorkState(state, work, previous);
}

function requireWork(state, workId) {
  const work = state.works.get(workId);
  if (!work) throw queueError("work_missing", "operation references missing Work");
  return work;
}

function requireClaim(state, claimId) {
  const claim = state.claims.get(claimId);
  if (!claim) throw queueError("claim_missing", "operation references a missing Claim");
  return claim;
}

function requireDispatch(state, dispatchId) {
  const dispatch = state.dispatches.get(dispatchId);
  if (!dispatch) throw queueError("dispatch_missing", "operation references a missing Dispatch");
  return dispatch;
}

/** A worker may settle only Claims of the dispatch bound to its own run attempt. */
function boundToActor(dispatch, actor) {
  return dispatch.state === "bound" && actor.workflow === dispatch.workflow && actor.run_id === dispatch.run.run_id && actor.run_attempt === dispatch.run.run_attempt;
}

function cancelClaim(state, claim, why, commit) {
  const work = state.works.get(claim.work_id);
  claim.state = "cancelled";
  claim.terminal_commit_id = commit.id;
  claim.cancellation_reason = why;
  work.attempts += 1;
  delete work.claim_id;
  setWorkState(state, work, "available");
}

function applyClaim(state, operation, commit) {
  const work = requireWork(state, operation.work_id);
  if (work.state !== "available" || work.claim_id !== undefined) throw queueError("ownership_conflict", "only available Work can be claimed");
  if (state.claims.has(operation.claim_id)) throw queueError("claim_conflict", "Claim identity already exists");
  const profiles = commit.request.parameters.profiles;
  if (!Object.hasOwn(profiles, work.worker_profile)) throw queueError("request_invalid", "dispatch_next parameters omit the Work's worker profile");
  const approval = approvalGate(work);
  if (approval && state.observations.get(gateKey(approval.resource, approval.condition))?.state !== "ready") throw queueError("approval_required", "an issue-routed continuation needs its ready approval");
  if (state.dispatches.has(operation.dispatch_id)) throw queueError("dispatch_state_conflict", "a Dispatch carries exactly one Claim");
  const dispatch = {
    dispatch_id: operation.dispatch_id,
    request_id: commit.request.id,
    commit_id: commit.id,
    pool: work.pool,
    worker_profile: work.worker_profile,
    workflow: profiles[work.worker_profile].workflow,
    state: "reserved",
    run: null,
    launch: null,
    claims: [operation.claim_id],
  };
  state.dispatches.set(dispatch.dispatch_id, dispatch);
  state.claims.set(operation.claim_id, { claim_id: operation.claim_id, work_id: work.work_id, dispatch_id: dispatch.dispatch_id, handle: operation.handle, state: "open", commit_id: commit.id, request_id: commit.request.id });
  work.claim_id = operation.claim_id;
  setWorkState(state, work, "claimed");
}

function applyBound(state, operation, commit) {
  const dispatch = requireDispatch(state, operation.dispatch_id);
  // A started or bound Dispatch still holds its open Claim: settling the Claim closes it.
  if (!["started", "bound"].includes(dispatch.state)) throw queueError("dispatch_state_conflict", "only a started Dispatch with an open Claim can bind a run");
  const actor = commit.actor;
  if (actor.workflow !== dispatch.workflow || actor.run_id !== operation.run_id || actor.run_attempt !== operation.run_attempt) throw queueError("run_binding_conflict", "a worker may bind only its own run of the dispatched workflow");
  // Only the run the dispatcher recorded launching may receive the Dispatch's Claims.
  if (operation.run_id !== dispatch.launch.run_id) throw queueError("run_binding_conflict", "a worker may bind only the run its dispatcher launched");
  if (dispatch.state === "bound") {
    if (dispatch.run.run_id !== operation.run_id || operation.run_attempt < dispatch.run.run_attempt) throw queueError("run_binding_conflict", "the Dispatch is bound to another run");
    if (operation.run_attempt === dispatch.run.run_attempt) return;
  }
  dispatch.state = "bound";
  dispatch.run = { run_id: operation.run_id, run_attempt: operation.run_attempt };
}

function applyCompletion(state, operation, commit) {
  const claim = requireClaim(state, operation.claim_id);
  if (claim.state !== "open") throw queueError("claim_ineffective", "only an open Claim can complete");
  const dispatch = state.dispatches.get(claim.dispatch_id);
  if (operation.work_id !== claim.work_id || operation.dispatch_id !== claim.dispatch_id || operation.claim_handle !== claim.handle || !boundToActor(dispatch, commit.actor))
    throw queueError("claim_scope_invalid", "Completion is outside the bound run's Claim scope");
  const work = state.works.get(claim.work_id);
  claim.state = "completed";
  claim.terminal_commit_id = commit.id;
  claim.effects = operation.effects;
  work.effects = operation.effects;
  work.completion_id = commit.id;
  setWorkState(state, work, "completed");
  dispatch.state = "closed";
}

function applyClaimCancellation(state, operation, commit) {
  const claim = requireClaim(state, operation.claim_id);
  if (claim.work_id !== operation.work_id) throw queueError("claim_scope_invalid", "ClaimCancellation names another Work");
  if (claim.state === "cancelled") return;
  if (claim.state !== "open") throw queueError("claim_ineffective", "only an open Claim can be cancelled");
  const work = state.works.get(claim.work_id);
  if (work.claim_id !== claim.claim_id) throw queueError("ownership_conflict", "Claim is not its Work's current owner");
  const dispatch = state.dispatches.get(claim.dispatch_id);
  if (commit.actor.role === "worker" && !boundToActor(dispatch, commit.actor)) throw queueError("claim_scope_invalid", "ClaimCancellation is outside the bound run's Claim scope");
  cancelClaim(state, claim, operation.reason, commit);
  dispatch.state = "closed";
}

function applyWorkCancellation(state, operation, commit) {
  const work = requireWork(state, operation.work_id);
  if (work.state === "completed") throw queueError("work_terminal", "completed Work cannot be cancelled");
  if (work.state === "cancelled") {
    if (work.cancellation_reason === operation.reason) return;
    throw queueError("work_terminal", "cancelled Work keeps its original reason");
  }
  if (work.state === "claimed") throw queueError("ownership_conflict", "release the Dispatch before cancelling claimed Work");
  if (operation.claim_id !== undefined) {
    const claim = state.claims.get(operation.claim_id);
    if (!claim || claim.work_id !== work.work_id || claim.state !== "cancelled") throw queueError("claim_scope_invalid", "WorkCancellation must name this Work's cancelled Claim");
  }
  work.cancellation_reason = operation.reason;
  work.cancellation_commit_id = commit.id;
  setWorkState(state, work, "cancelled");
}

function applyRelease(state, operation, commit) {
  const dispatch = requireDispatch(state, operation.dispatch_id);
  if (dispatch.state === "closed") throw queueError("dispatch_state_conflict", "Dispatch is already closed");
  cancelClaim(state, state.claims.get(dispatch.claims[0]), operation.reason, commit);
  dispatch.state = "closed";
}

function applyIssueLink(state, operation) {
  const work = requireWork(state, operation.work_id);
  if (operation.claim_id !== undefined && state.claims.get(operation.claim_id)?.work_id !== work.work_id) throw queueError("claim_scope_invalid", "IssueLink Claim belongs to another Work");
  if (work.backing_issue) {
    if (issueKey(work.backing_issue) === issueKey(operation.resource)) return;
    throw queueError("issue_binding_conflict", "Work already has a backing Issue");
  }
  const owner = backingIssueOwner(state, operation.resource);
  if (owner !== undefined && owner !== work.work_id) throw queueError("issue_binding_conflict", "one Work per backing Issue");
  work.backing_issue = operation.resource;
  trackBackingIssue(state, work);
}

function applyIssueComment(state, operation) {
  const work = requireWork(state, operation.work_id);
  let target = work;
  if (operation.claim_id !== undefined) {
    target = requireClaim(state, operation.claim_id);
    if (target.work_id !== work.work_id) throw queueError("claim_scope_invalid", "IssueComment Claim belongs to another Work");
  }
  if (target.issue_comment !== undefined) {
    if (target.issue_comment === operation.comment_id) return;
    throw queueError("issue_binding_conflict", "Issue comment binding is immutable");
  }
  target.issue_comment = operation.comment_id;
}

/**
 * A finish-carried Work is the continuation of the Completion immediately before
 * it: fresh, exactly `continuationChild` of that parent, and admitted against the
 * projection that already counts the parent as completed.
 */
function admitContinuation(state, commit, index) {
  const operation = commit.operations[index];
  const invalid = message => queueError("request_invalid", message);
  const paired = commit.operations[index - 1];
  if (paired?.kind !== "Completion" || paired.dispatch_id !== commit.request.parameters.dispatch_id) throw invalid("a finish-carried Work must immediately follow its parent's Completion of the request's Dispatch");
  if (state.works.has(operation.work_id)) throw invalid("a continuation must be fresh Work");
  const parent = state.works.get(paired.work_id);
  const continuation = operation.continuation;
  if (!continuation || parent?.completion_id !== commit.id) throw invalid("a finish-carried Work must be the continuation of a Completion in this commit");
  const approval = operation.depends_on.find(edge => edge.kind === "issue" && edge.condition === "approved")?.resource;
  if (canonical(operation) !== canonical(continuationChild(parent, continuation, approval))) throw invalid("a continuation must inherit exactly from its parent");
  // An automatic hop spends one unit of its parent's budget; a zero balance cannot hand off.
  if (continuation.route === "workqueue" && parent.continuation && continuation.remaining >= parent.continuation.remaining) throw invalid("an automatic continuation must decrease its remaining budget");
  validateGraphAdmission(state, [operation]);
}

function applyOperation(state, operation, commit, ordinal, index) {
  switch (operation.kind) {
    case "Work":
      if (commit.request.kind === "finish") admitContinuation(state, commit, index);
      if (!state.works.has(operation.work_id)) {
        const work = { ...operation, state: "available", position: { commit: ordinal, operation: index }, attempts: 0 };
        state.works.set(work.work_id, work);
        state.workCreators.set(work.work_id, commit.actor);
        trackAdmittedWork(state, work);
      }
      return;
    case "Observation":
      state.observations.set(gateKey(operation.resource, operation.condition), operation);
      return;
    case "Claim":
      return applyClaim(state, operation, commit);
    case "Dispatch": {
      const dispatch = requireDispatch(state, operation.dispatch_id);
      if (dispatch.state !== "reserved") throw queueError("dispatch_state_conflict", "only a reserved Dispatch can start");
      dispatch.state = "started";
      dispatch.launch = { run_id: operation.run_id, revision: operation.revision };
      return;
    }
    case "Bound":
      return applyBound(state, operation, commit);
    case "Completion":
      return applyCompletion(state, operation, commit);
    case "ClaimCancellation":
      return applyClaimCancellation(state, operation, commit);
    case "WorkCancellation":
      return applyWorkCancellation(state, operation, commit);
    case "Release":
      return applyRelease(state, operation, commit);
    case "IssueLink":
      return applyIssueLink(state, operation);
    case "IssueComment":
      return applyIssueComment(state, operation);
    case "Checkpoint":
      throw queueError("checkpoint_invalid", "a Checkpoint must be the sole first line of the log");
  }
}

/**
 * Apply one validated commit to the projection in place. Replay never reads `at`.
 * @param {QueueState} state
 * @param {QueueCommit} commit
 */
function applyCommit(state, commit) {
  validateCommit(commit);
  if (commit.operations.some(operation => operation.kind === "Checkpoint")) throw queueError("checkpoint_invalid", "a Checkpoint must be the sole first line of the log");
  if (commit.previous !== (state.tip || null)) throw queueError("ledger_invalid", "commit does not extend the causal tip");
  if (!state.tip) {
    if (commit.actor.role === "worker") throw queueError("actor_unauthorized", "a worker cannot create the queue genesis");
    state.repository = commit.actor.repository;
  } else if (commit.actor.repository !== state.repository) throw queueError("actor_unauthorized", "foreign queue repository");
  if (state.requests.has(commit.request.id)) throw queueError("request_reused", "request is already committed");
  if (commit.request.kind === "migrate" && !state.transactions.every(prior => prior.request.kind === "migrate")) throw queueError("request_invalid", "migrate commits must form the log genesis");
  // submit/migrate admit their batch up front (forward references); a finish admits
  // each continuation right after its paired Completion.
  if (commit.request.kind !== "finish") {
    const fresh = new Map();
    for (const operation of commit.operations) if (operation.kind === "Work" && !state.works.has(operation.work_id)) fresh.set(operation.work_id, [...(fresh.get(operation.work_id) ?? []), operation]);
    if (fresh.size) validateGraphAdmission(state, [...fresh.values()].flat());
  }
  const ordinal = state.commits;
  commit.operations.forEach((operation, index) => applyOperation(state, operation, commit, ordinal, index));
  state.requests.add(commit.request.id);
  state.tip = commit.id;
  state.commits += 1;
  state.transactions.push(commit);
  return state;
}

function projectStats(state) {
  const stats = { work: state.works.size, available: 0, claimed: 0, completed: 0, cancelled: 0, claims: state.claims.size, dispatches: 0, transactions: state.commits, nodes: state.works.size };
  const gates = new Set();
  for (const work of state.works.values()) {
    stats[work.state]++;
    for (const edge of work.depends_on) if (edge.kind !== "work") gates.add(`${work.graph_id}\n${gateKey(edge.resource, edge.condition)}`);
  }
  for (const dispatch of state.dispatches.values()) if (dispatch.state !== "closed") stats.dispatches++;
  stats.nodes += gates.size;
  state.stats = stats;
  return state;
}

function isCheckpointCommit(commit) {
  return Array.isArray(commit?.operations) && commit.operations[0]?.kind === "Checkpoint";
}

/** Serialize the projection as the canonical checkpoint `state` object. */
function serializeProjection(state) {
  return {
    repository: state.repository,
    tip: state.tip,
    commits: state.commits,
    works: Object.fromEntries(state.works),
    claims: Object.fromEntries(state.claims),
    dispatches: Object.fromEntries(state.dispatches),
    observations: Object.fromEntries(state.observations),
    requests: [...state.requests].sort(utf8Compare),
    work_creators: Object.fromEntries(state.workCreators),
  };
}

function plainObject(value) {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function recordEffects(effects) {
  if (!Array.isArray(effects) || effects.some(effect => !plainObject(effect))) throw queueError("checkpoint_invalid", "checkpoint effects must be an array of objects");
}

/**
 * Restore a checkpoint into fresh mutable records (like `cloneState`), so replaying the same
 * commit objects again, or serializing them afterwards, still sees the checkpoint as written.
 * @param {QueueCommit} commit @returns {QueueState}
 */
function restoreCheckpointState(commit) {
  validateCommit(commit);
  const invalid = message => queueError("checkpoint_invalid", message);
  if (commit.previous !== null || commit.request.kind !== "checkpoint" || commit.operations.length !== 1) throw invalid("a checkpoint must be the sole operation of a root commit");
  const operation = commit.operations[0];
  const snapshot = operation.state;
  closed(snapshot, CHECKPOINT_FIELDS, [], "checkpoint state", "checkpoint_invalid");
  if (snapshot.repository !== commit.actor.repository || snapshot.tip !== operation.prior_tip || commit.request.parameters.prior_tip !== operation.prior_tip) throw invalid("checkpoint does not bind its repository and prior tip");
  if (!Number.isSafeInteger(snapshot.commits) || snapshot.commits < 1) throw invalid("checkpoint requires its commit ordinal");
  for (const field of ["works", "claims", "dispatches", "observations", "work_creators"]) if (!plainObject(snapshot[field])) throw invalid(`checkpoint ${field} must be an object`);
  if (!Array.isArray(snapshot.requests) || new Set(snapshot.requests).size !== snapshot.requests.length) throw invalid("checkpoint requests must be unique");
  for (const id of snapshot.requests) identity(id, "checkpoint request");
  const state = newState();
  state.repository = snapshot.repository;
  for (const [id, record] of Object.entries(snapshot.works)) {
    closed(record, [...WORK_REQUIRED, "state", "position", "attempts"], WORK_RECORD_OPTIONAL, "checkpoint Work", "checkpoint_invalid");
    for (const field of ["claim_id", "completion_id", "cancellation_commit_id", "issue_comment"]) if (Object.hasOwn(record, field)) identity(record[field], field);
    if (Object.hasOwn(record, "cancellation_reason")) reason(record.cancellation_reason, WORK_CANCELLATION_REASONS, "Work");
    if (Object.hasOwn(record, "effects")) recordEffects(record.effects);
    validateWork(workDefinition(record));
    closed(record.position, ["commit", "operation"], [], "checkpoint Work position", "checkpoint_invalid");
    if (record.work_id !== id || !WORK_STATES.includes(record.state) || !Number.isSafeInteger(record.attempts) || record.attempts < 0) throw invalid("checkpoint Work record is inconsistent");
    integer(record.position.commit, 0, snapshot.commits - 1, "Work position commit");
    integer(record.position.operation, 0, OPERATIONS_PER_COMMIT - 1, "Work position operation");
    if ((record.state === "claimed") !== Object.hasOwn(record, "claim_id") && record.state !== "completed") throw invalid("only claimed or completed Work names a Claim");
    if ((record.state === "completed") !== Object.hasOwn(record, "completion_id") || (record.state === "completed") !== Object.hasOwn(record, "effects")) throw invalid("completed Work carries its completion");
    if ((record.state === "cancelled") !== Object.hasOwn(record, "cancellation_reason")) throw invalid("cancelled Work carries its reason");
    state.works.set(id, { ...record });
  }
  for (const [id, record] of Object.entries(snapshot.claims)) {
    closed(record, CLAIM_RECORD_REQUIRED, CLAIM_RECORD_OPTIONAL, "checkpoint Claim", "checkpoint_invalid");
    for (const field of ["handle", "commit_id", "request_id", "terminal_commit_id", "issue_comment"]) if (Object.hasOwn(record, field)) identity(record[field], field);
    if (Object.hasOwn(record, "cancellation_reason")) reason(record.cancellation_reason, CLAIM_CANCELLATION_REASONS, "Claim");
    if (Object.hasOwn(record, "effects")) recordEffects(record.effects);
    const work = state.works.get(record.work_id);
    if (record.claim_id !== id || !CLAIM_STATES.includes(record.state) || !work || !plainObject(snapshot.dispatches[record.dispatch_id])) throw invalid("checkpoint Claim record is inconsistent");
    if ((record.state === "open") !== (work.state === "claimed" && work.claim_id === id)) throw invalid("open Claims and claimed Work must agree");
    state.claims.set(id, { ...record });
  }
  for (const [id, record] of Object.entries(snapshot.dispatches)) {
    closed(record, DISPATCH_RECORD_FIELDS, [], "checkpoint Dispatch", "checkpoint_invalid");
    for (const field of ["request_id", "commit_id", "pool", "worker_profile"]) identity(record[field], field);
    if (record.dispatch_id !== id || !DISPATCH_STATES.includes(record.state) || !Array.isArray(record.claims) || !PROFILE_WORKFLOW.test(record.workflow)) throw invalid("checkpoint Dispatch record is inconsistent");
    if (record.claims.length !== 1) throw invalid("a checkpoint Dispatch carries exactly one Claim");
    const claim = state.claims.get(record.claims[0]);
    if (typeof record.claims[0] !== "string" || claim?.dispatch_id !== id) throw invalid("checkpoint Dispatch members are inconsistent");
    const work = state.works.get(claim.work_id);
    if (record.pool !== work.pool || record.worker_profile !== work.worker_profile) throw invalid("a checkpoint Dispatch must match its Work's pool and profile");
    if (record.run !== null) {
      closed(record.run, ["run_id", "run_attempt"], [], "checkpoint run", "checkpoint_invalid");
      decimal(record.run.run_id, "checkpoint run ID", "checkpoint_invalid");
      integer(record.run.run_attempt, 1, 4096, "checkpoint run attempt");
    }
    if ((record.state === "bound") !== (record.run !== null) && record.state !== "closed") throw invalid("only bound Dispatches carry a run");
    if (record.launch !== null) {
      closed(record.launch, ["run_id", "revision"], [], "checkpoint launch", "checkpoint_invalid");
      decimal(record.launch.run_id, "checkpoint launched run ID", "checkpoint_invalid");
      if (typeof record.launch.revision !== "string" || !LAUNCH_REVISION.test(record.launch.revision)) throw invalid("checkpoint launch revision is malformed");
    }
    if ((record.state === "reserved") !== (record.launch === null) && record.state !== "closed") throw invalid("only started or bound Dispatches carry their launch");
    if (record.run !== null && record.launch?.run_id !== record.run.run_id) throw invalid("a bound run must be the launched run");
    if ((record.state !== "closed") !== (claim.state === "open")) throw invalid("a checkpoint Dispatch is closed exactly when its Claim is settled");
    state.dispatches.set(id, { ...record, claims: [claim.claim_id] });
  }
  // Every Claim, settled or open, must be its Dispatch's sole member, so no two Claims share one.
  for (const claim of state.claims.values()) if (state.dispatches.get(claim.dispatch_id)?.claims[0] !== claim.claim_id) throw invalid("checkpoint Claims must each own their Dispatch");
  for (const [key, observation] of Object.entries(snapshot.observations)) {
    validateOperation(observation);
    if (observation.kind !== "Observation" || gateKey(observation.resource, observation.condition) !== key) throw invalid("checkpoint Observation key is inconsistent");
    state.observations.set(key, observation);
  }
  for (const [id, actor] of Object.entries(snapshot.work_creators)) {
    if (!state.works.has(id)) throw invalid("checkpoint Work creator names missing Work");
    state.workCreators.set(id, validateActor(actor));
  }
  if (state.workCreators.size !== state.works.size) throw invalid("every checkpoint Work requires its creator");
  state.requests = new Set(snapshot.requests);
  state.requests.add(commit.request.id);
  state.tip = commit.id;
  state.commits = snapshot.commits + 1;
  state.transactions = [commit];
  return state;
}

/**
 * Replay a v4 log in causal (file) order. A Checkpoint may only be line 1.
 * @param {unknown[]} transactions
 * @returns {QueueState}
 */
function replayTransactions(transactions) {
  if (!Array.isArray(transactions)) throw queueError("ledger_invalid", "transactions must be an array");
  let state = newState();
  for (const [index, commit] of transactions.entries()) {
    // prettier-ignore
    if (index === 0 && isCheckpointCommit(commit)) state = restoreCheckpointState(/** @type {QueueCommit} */ (commit));
    else applyCommit(state, /** @type {QueueCommit} */ (commit));
  }
  return projectStats(state);
}

/** Validate and append one commit to a copy of the projection. */
function appendCommit(state, commit) {
  const next = cloneState(state);
  applyCommit(next, commit);
  return projectStats(next);
}

function* boundedTransactionLines(contents) {
  let lineNumber = 0;
  for (const rawLine of iterateLines(contents, true)) {
    const line = rawLine.slice(0, -1);
    lineNumber++;
    if (!line.trim()) throw queueError("ledger_invalid", "empty or truncated log record");
    if (Buffer.byteLength(line, "utf8") > LEDGER_BYTES) throw queueError("resource_limit", `commit line ${lineNumber} exceeds the ledger bound`);
    yield rawLine;
  }
}

function parseTransactionRecords(contents) {
  if (typeof contents !== "string" || Buffer.byteLength(contents, "utf8") > LEDGER_BYTES) throw queueError("resource_limit", "ledger exceeds its byte bound");
  if (!contents || !contents.endsWith("\n")) throw queueError("ledger_invalid", "queue log must be nonempty and newline terminated");
  const records = [];
  for (const event of iterateJsonlEvents(boundedTransactionLines(contents))) {
    // Preserve duplicate-key, numeric, Unicode and nesting checks on original bytes.
    records.push(parseStrictJSON(event.rawLine, { maxBytes: LEDGER_BYTES }));
  }
  return records;
}

function replayTransactionLog(contents) {
  log.debug("ledger.replay.start");
  const state = replayTransactions(parseTransactionRecords(contents));
  log.debug("ledger.replay.complete", { transactions: state.transactions.length, works: state.works.size, claims: state.claims.size });
  return state;
}

function parseTransactionLog(contents) {
  return replayTransactionLog(contents).transactions;
}

function serializeTransactionLog(transactions) {
  const ordered = replayTransactions(transactions).transactions;
  return ordered.length ? ordered.map(commit => canonical(commit)).join("\n") + "\n" : "";
}

/**
 * Build the next commit for a request. `at` is informational only.
 * @param {QueueState} state
 * @param {QueueActor} actor
 * @param {QueueRequest} request
 * @param {Array<Record<string, any>>} operations
 * @param {number} [at]
 * @returns {QueueCommit}
 */
function buildCommit(state, actor, request, operations, at = Date.now()) {
  const previous = state.tip || null;
  return { version: PROTOCOL_VERSION, id: commitId(previous, request.id), previous, request, actor, at, operations };
}

/**
 * A checkpoint commit replaces the whole log: it is a root carrying the projection.
 * @param {QueueState} state
 * @param {QueueActor} actor
 * @param {number} [at]
 * @returns {QueueCommit}
 */
function checkpointCommit(state, actor, at = Date.now()) {
  if (!state.tip) throw queueError("checkpoint_invalid", "an empty queue has nothing to checkpoint");
  const request = newRequest("checkpoint", actor, { prior_tip: state.tip });
  // The snapshot must not alias live records that later appends to `state` mutate in place.
  const commit = { version: PROTOCOL_VERSION, id: commitId(null, request.id), previous: null, request, actor, at, operations: [{ kind: "Checkpoint", prior_tip: state.tip, state: structuredClone(serializeProjection(state)) }] };
  restoreCheckpointState(/** @type {QueueCommit} */ commit);
  return /** @type {QueueCommit} */ commit;
}

/** A Dispatch's sole Claim while it is open, otherwise `undefined`. */
function openClaim(state, dispatch) {
  const claim = state.claims.get(dispatch.claims[0]);
  return claim?.state === "open" ? claim : undefined;
}

/** The committed transaction that carried a request, when still in the log. */
function committedRequest(state, id) {
  if (!state.requests.has(id)) return undefined;
  return state.transactions.find(commit => commit.request.id === id) ?? null;
}

module.exports = {
  CLAIM_CANCELLATION_REASONS,
  LAUNCH_REVISION,
  OP_FIELDS,
  OP_ROLES,
  PROTOCOL_VERSION,
  RELEASE_REASONS,
  REQUEST_KINDS,
  WORK_CANCELLATION_REASONS,
  appendCommit,
  applyCommit,
  buildCommit,
  checkpointCommit,
  cloneState,
  commitId,
  committedRequest,
  newRequest,
  newState,
  openClaim,
  parseTransactionLog,
  parseTransactionRecords,
  projectStats,
  proposedCommitId,
  replayTransactionLog,
  replayTransactions,
  requestId,
  restoreCheckpointState,
  serializeProjection,
  serializeTransactionLog,
  validateCommit,
  validateOperation,
  validateRequest,
  workDefinition,
};
