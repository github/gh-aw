// @ts-check
"use strict";
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("replay");

const { createHash } = require("node:crypto");
const { canonical, canonicalBytes, closed, digest, fingerprint, identity, integer, parseStrictJSON, queueError, utf8Compare, validateReason } = require("./work_queue_codec.cjs");
const { boundedBytes, checkLedgerBudget } = require("./work_queue_limits.cjs");
const { indexesFor, trackState, untrackState, updateIndexes } = require("./work_queue_indexes.cjs");
const { validateEffectResourceAuthority } = require("./work_queue_resource_scope.cjs");
const {
  actorFromContext,
  bindingAuthority,
  claimAuthority,
  decimal,
  defaultPolicy,
  freshClocks,
  normalizeTrustedContext,
  poolPolicy,
  validateActor,
  validatePolicy,
  validateRequestRole,
  validateSubmissionEntitlement,
  workerContinuationAuthority,
} = require("./work_queue_policy.cjs");
const { dependencyStatus, gateKey, observationSatisfies, validateGraphAdmission, validateResource, workDefinition } = require("./work_queue_graph.cjs");
const {
  applyScheduledClaim,
  assignmentForDispatch,
  assignmentOnly,
  assignmentsForRequest,
  diagnostics,
  eligibility,
  fifoCompare,
  planDispatch,
  planNext,
  reservationCounts,
  selectionOnly,
  validateDispatchParameters,
} = require("./work_queue_scheduler.cjs");

const OP_FIELDS = {
  Policy: [["kind", "epoch", "policy"], []],
  Control: [["kind", "control", "value", "reason"], []],
  Work: [
    ["kind", "work_id", "graph_id", "node_key", "pool", "priority", "fairness_key", "worker_profile", "batch_trust_domain", "payload", "depends_on", "enqueued"],
    ["subject", "replacement_of"],
  ],
  Claim: [["kind", "work_id", "claim_id", "dispatch_id", "handle", "observations"], []],
  Completion: [["kind", "work_id", "claim_id", "dispatch_id", "claim_handle", "run_id", "run_attempt"], []],
  Result: [["kind", "work_id", "claim_id", "completion_id", "descriptor", "evidence"], []],
  DeliveryFailure: [["kind", "work_id", "claim_id", "completion_id", "reason", "disposition", "evidence"], []],
  Observation: [
    ["kind", "observation_id", "resource", "condition", "state", "observed_at", "credential_generation", "read_status"],
    ["source_updated_at", "state_reason", "resource_state", "merged", "merge_commit"],
  ],
  ClaimCancellation: [["kind", "work_id", "claim_id", "reason", "retry_not_before"], []],
  WorkCancellation: [["kind", "work_id", "reason"], ["claim_id"]],
  WorkPriority: [["kind", "work_id", "priority", "expected_priority", "reason"], []],
  Dispatch: [
    ["kind", "dispatch_id", "state"],
    ["sender", "run", "evidence", "reason"],
  ],
  Release: [["kind", "dispatch_id", "evidence"], []],
  Checkpoint: [["kind", "prior_git_sha", "prior_tip", "history_sha256", "state_sha256", "state"], []],
};

const REQUEST_KINDS = {
  policy: ["Policy"],
  control: ["Control", "WorkPriority"],
  submit: ["Work"],
  dispatch_next: ["Observation", "Claim"],
  finish: ["Completion", "ClaimCancellation", "WorkCancellation"],
  observe: ["Observation"],
  dispatch: ["Dispatch"],
  release: ["ClaimCancellation", "WorkCancellation", "Release"],
  result: ["Result"],
  delivery_failure: ["DeliveryFailure"],
  cancel_work: ["WorkCancellation"],
  cancel_claim: ["ClaimCancellation", "WorkCancellation"],
  checkpoint: ["Checkpoint"],
};

const OP_ROLES = {
  Policy: ["administrator"],
  Control: ["administrator"],
  Work: ["producer", "dispatcher", "worker", "administrator"],
  Claim: ["dispatcher", "administrator", "worker"],
  Completion: ["worker"],
  Result: ["reconciler"],
  DeliveryFailure: ["reconciler"],
  Observation: ["reconciler", "dispatcher", "administrator", "worker"],
  ClaimCancellation: ["worker", "reconciler"],
  WorkCancellation: ["producer", "administrator", "reconciler", "worker"],
  WorkPriority: ["administrator"],
  Dispatch: ["dispatcher", "worker", "reconciler"],
  Release: ["reconciler"],
  Checkpoint: ["administrator"],
};

function validateEvidence(evidence) {
  closed(evidence, ["kind", "source", "repository", "workflow", "ref", "principal", "checked_at"], ["run_id", "run_attempt", "status", "conclusion", "receipt", "attempts", "effects"], "evidence");
  if (!["prelaunch", "nonlaunch", "terminal_run", "delivery", "reconciliation"].includes(evidence.kind) || !["trusted_publisher", "github_api", "trusted_activation", "verified_receipts"].includes(evidence.source))
    throw queueError("evidence_invalid", "unsupported evidence source/kind");
  for (const field of ["repository", "workflow", "ref", "principal", "conclusion", "receipt"]) if (Object.hasOwn(evidence, field)) identity(evidence[field], field);
  decimal(evidence.principal, "evidence principal", "evidence_invalid");
  if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(evidence.ref)) throw queueError("evidence_invalid", "evidence requires an immutable native revision");
  integer(evidence.checked_at, 0, Number.MAX_SAFE_INTEGER, "evidence timestamp");
  if (Object.hasOwn(evidence, "run_id")) decimal(evidence.run_id, "evidence run ID");
  if (Object.hasOwn(evidence, "run_attempt") && evidence.run_attempt !== 1) throw queueError("evidence_invalid", "evidence must name native attempt 1");
  if (Object.hasOwn(evidence, "attempts")) integer(evidence.attempts, 1, 4096, "reconciliation attempts");
  if (Object.hasOwn(evidence, "effects") && !["none", "partial", "unknown"].includes(evidence.effects)) throw queueError("evidence_invalid", "unsupported effect disposition");
  if (Object.hasOwn(evidence, "status") && evidence.status !== "completed") throw queueError("evidence_invalid", "only exact terminal status is evidence");
}

function validateRunBinding(run) {
  closed(run, ["run_id", "run_attempt", "repository", "workflow", "ref", "principal", "event"], [], "run binding");
  decimal(run.run_id, "native run ID");
  for (const field of ["repository", "workflow", "ref", "principal"]) identity(run[field], field);
  decimal(run.principal, "native run principal", "run_binding_conflict");
  if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(run.ref)) throw queueError("run_binding_conflict", "native run requires an immutable revision");
  if (run.run_attempt !== 1 || run.event !== "workflow_dispatch") throw queueError("run_binding_conflict", "only original workflow_dispatch attempts can bind");
}

function validateOperation(operation) {
  if (!operation || !Object.hasOwn(OP_FIELDS, operation.kind)) throw queueError("unsupported_protocol", "unknown queue operation");
  closed(operation, OP_FIELDS[operation.kind][0], OP_FIELDS[operation.kind][1], operation.kind, "unsupported_protocol");
  for (const field of ["work_id", "claim_id", "dispatch_id", "handle", "claim_handle", "completion_id", "epoch", "observation_id", "credential_generation", "read_status"])
    if (Object.hasOwn(operation, field)) identity(operation[field], field);
  if (Object.hasOwn(operation, "reason")) validateReason(operation.reason);
  if (Object.hasOwn(operation, "evidence")) validateEvidence(operation.evidence);
  if (Object.hasOwn(operation, "run")) validateRunBinding(operation.run);
  if (Object.hasOwn(operation, "sender")) validateActor(operation.sender);
  if (operation.kind === "Policy") validatePolicy(operation.policy);
  if (operation.kind === "Checkpoint") {
    identity(operation.prior_tip, "prior tip");
    if (
      !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(operation.prior_git_sha) ||
      !/^[a-f0-9]{64}$/.test(operation.history_sha256) ||
      !/^[a-f0-9]{64}$/.test(operation.state_sha256) ||
      !operation.state ||
      typeof operation.state !== "object" ||
      Array.isArray(operation.state)
    )
      throw queueError("checkpoint_invalid", "checkpoint requires bounded state, history digest and prior Git commit SHA");
  }
  if (operation.kind === "WorkPriority") {
    integer(operation.priority, 1, 5, "priority");
    integer(operation.expected_priority, 1, 5, "expected priority");
  }
  if (operation.kind === "Claim") {
    if (!Array.isArray(operation.observations) || operation.observations.length > 64 || new Set(operation.observations).size !== operation.observations.length)
      throw queueError("claim_invalid", "observations must be a bounded unique array");
    for (const id of operation.observations) identity(id, "observation reference");
  }
  if (operation.kind === "Completion") {
    decimal(operation.run_id, "completion run ID");
    if (operation.run_attempt !== 1) throw queueError("run_binding_conflict", "Completion requires original native attempt");
  }
  if (operation.kind === "ClaimCancellation") integer(operation.retry_not_before, 0, Number.MAX_SAFE_INTEGER, "retry-not-before");
  if (operation.kind === "Control") {
    if (!["admission_paused", "grants_paused", "credential_generation"].includes(operation.control)) throw queueError("control_invalid", "unknown Control");
    if (operation.control === "credential_generation") identity(operation.value, "credential generation");
    else if (typeof operation.value !== "boolean") throw queueError("control_invalid", "pause Controls require boolean values");
  }
  if (operation.kind === "Result" && (!operation.descriptor || typeof operation.descriptor !== "object" || Array.isArray(operation.descriptor))) throw queueError("result_invalid", "descriptor must be a JSON object");
  if (operation.kind === "DeliveryFailure" && !["none", "partial", "unknown"].includes(operation.disposition)) throw queueError("evidence_invalid", "unknown delivery disposition");
  if (operation.kind === "Observation") {
    validateResource(operation.resource);
    const conditions = operation.resource.kind === "issue" ? ["completed", "closed"] : ["merged"];
    if (!conditions.includes(operation.condition) || !["ready", "waiting", "failed", "unknown"].includes(operation.state)) throw queueError("observation_invalid", "typed condition/state is invalid");
    integer(operation.observed_at, 0, Number.MAX_SAFE_INTEGER, "observation timestamp");
    if (Object.hasOwn(operation, "source_updated_at")) integer(operation.source_updated_at, 0, Number.MAX_SAFE_INTEGER, "resource update timestamp");
    if (Object.hasOwn(operation, "state_reason") && !["completed", "not_planned", "reopened"].includes(operation.state_reason)) throw queueError("observation_invalid", "invalid Issue state_reason");
    if (Object.hasOwn(operation, "resource_state") && !["open", "closed"].includes(operation.resource_state)) throw queueError("observation_invalid", "invalid resource state");
    if (Object.hasOwn(operation, "merged") && typeof operation.merged !== "boolean") throw queueError("observation_invalid", "merged must be boolean");
    if (Object.hasOwn(operation, "merge_commit")) identity(operation.merge_commit, "merge provenance");
    if (operation.resource.kind === "issue" && (Object.hasOwn(operation, "merged") || Object.hasOwn(operation, "merge_commit"))) throw queueError("observation_invalid", "Issue cannot carry PR merge evidence");
    if (operation.state === "ready" && !observationSatisfies(operation)) throw queueError("observation_invalid", "ready observation does not prove its typed predicate");
  }
  if (operation.kind === "Dispatch" && !["started", "bound", "rejected", "uncertain", "unresolved"].includes(operation.state)) throw queueError("dispatch_invalid", "unknown dispatch state");
}

function validateRequest(request, actor) {
  closed(request, ["id", "kind", "parameters", "fingerprint"], [], "request");
  identity(request.id, "request ID");
  if (!Object.hasOwn(REQUEST_KINDS, request.kind)) throw queueError("request_invalid", "unsupported request kind");
  if (typeof request.fingerprint !== "string" || !/^[a-f0-9]{64}$/.test(request.fingerprint) || request.fingerprint !== fingerprint(actor, request.kind, request.parameters))
    throw queueError("request_invalid", "semantic fingerprint does not bind actor and validated parameters");
  if (request.kind === "submit") {
    closed(request.parameters, ["nodes"], [], "submit parameters");
    if (!Array.isArray(request.parameters.nodes) || !request.parameters.nodes.length || request.parameters.nodes.length > 256) throw queueError("request_invalid", "submit requires 1..256 immutable Work nodes");
    for (const node of request.parameters.nodes) validateOperation(node);
  } else if (request.kind === "dispatch_next") {
    closed(request.parameters, ["pool", "max_claims", "max_dispatches", "max_bytes"], [], "dispatch parameters");
    identity(request.parameters.pool, "pool");
    integer(request.parameters.max_claims, 1, 256, "max_claims");
    integer(request.parameters.max_dispatches, 1, 256, "max_dispatches");
    integer(request.parameters.max_bytes, 1, 48 * 1024, "max_bytes");
  } else if (request.kind === "finish") {
    closed(request.parameters, ["dispatch_id", "claim_handle", "outcome"], [], "finish parameters");
    identity(request.parameters.dispatch_id, "dispatch ID");
    identity(request.parameters.claim_handle, "Claim handle");
    if (!["completed", "cancelled"].includes(request.parameters.outcome)) throw queueError("request_invalid", "finish outcome must be completed or cancelled");
  } else if (request.kind === "checkpoint") {
    closed(request.parameters, ["prior_git_sha", "prior_tip", "history_sha256", "state_sha256"], [], "checkpoint parameters", "checkpoint_invalid");
    if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(request.parameters.prior_git_sha) || !/^[a-f0-9]{64}$/.test(request.parameters.history_sha256) || !/^[a-f0-9]{64}$/.test(request.parameters.state_sha256))
      throw queueError("checkpoint_invalid", "invalid prior Git SHA or state/history digest");
    identity(request.parameters.prior_tip, "prior tip");
  } else {
    closed(request.parameters, ["operations"], [], "operation parameters");
    if (!Array.isArray(request.parameters.operations) || !request.parameters.operations.length || request.parameters.operations.length > 256) throw queueError("request_invalid", "request requires bounded nonempty operations");
    for (const operation of request.parameters.operations) validateOperation(operation);
  }
  return request;
}

function newRequest(id, kind, actor, parameters) {
  validateActor(actor);
  return validateRequest({ id, kind, parameters: structuredClone(parameters), fingerprint: fingerprint(actor, kind, parameters) }, actor);
}

function proposedCommitId(state, request) {
  const input = state.tip ? `${state.tip}\n${request.id}` : request.id;
  return `q_${createHash("sha256").update(input, "utf8").digest("hex")}`;
}

function validateCommit(commit) {
  closed(commit, ["version", "id", "previous", "request", "actor", "policy_epoch", "at", "operations"], ["trace"], "QueueCommit");
  if (commit.version !== 3) throw queueError("unsupported_protocol", "only closed version-3 QueueCommit records are supported");
  identity(commit.id, "commit ID");
  if (commit.previous !== null) identity(commit.previous, "predecessor");
  identity(commit.policy_epoch, "policy epoch");
  integer(commit.at, 0, Number.MAX_SAFE_INTEGER, "decision timestamp");
  validateActor(commit.actor);
  validateRequest(commit.request, commit.actor);
  validateRequestRole(commit.actor, commit.request.kind);
  if (!Array.isArray(commit.operations) || !commit.operations.length || commit.operations.length > 256) throw queueError("resource_limit", "QueueCommit requires 1..256 operations");
  if (Object.hasOwn(commit, "trace")) {
    closed(commit.trace, [], ["trace_id", "span_id", "publisher_attempt"], "trace");
    for (const field of ["trace_id", "span_id"]) if (Object.hasOwn(commit.trace, field)) identity(commit.trace[field], field);
    if (Object.hasOwn(commit.trace, "publisher_attempt")) integer(commit.trace.publisher_attempt, 1, 4096, "publisher attempt");
    boundedBytes(commit.trace, 1024, "trace");
  }
  for (const operation of commit.operations) {
    validateOperation(operation);
    if (!REQUEST_KINDS[commit.request.kind].includes(operation.kind) || !OP_ROLES[operation.kind].includes(commit.actor.role))
      throw queueError("unauthorized_operation", `${commit.actor.role} cannot publish ${operation.kind} through ${commit.request.kind}`);
  }
  if (commit.request.kind === "submit" && canonical(commit.request.parameters.nodes) !== canonical(commit.operations)) throw queueError("request_invalid", "submission differs from validated stable semantics");
  if (
    commit.request.kind === "checkpoint" &&
    (commit.operations.length !== 1 ||
      commit.operations[0].kind !== "Checkpoint" ||
      canonical(commit.request.parameters) !==
        canonical({
          prior_git_sha: commit.operations[0].prior_git_sha,
          prior_tip: commit.operations[0].prior_tip,
          history_sha256: commit.operations[0].history_sha256,
          state_sha256: commit.operations[0].state_sha256,
        }))
  )
    throw queueError("checkpoint_invalid", "checkpoint archive differs from stable request");
  if (!["submit", "dispatch_next", "finish", "checkpoint"].includes(commit.request.kind) && canonical(commit.request.parameters.operations) !== canonical(commit.operations))
    throw queueError("request_invalid", "operations differ from validated stable semantics");
  if (canonicalBytes(commit) > 8 * 1024 * 1024) throw queueError("resource_limit", "QueueCommit exceeds parser bound");
  return commit;
}

function causalChain(transactions) {
  if (!Array.isArray(transactions) || transactions.length > 1000000) throw queueError("resource_limit", "ledger commit count exceeded");
  const byId = new Map();
  for (const transaction of transactions) {
    validateCommit(transaction);
    const existing = byId.get(transaction.id);
    if (existing && canonical(existing) !== canonical(transaction)) throw queueError("ledger_invalid", "conflicting duplicate commit ID");
    byId.set(transaction.id, transaction);
  }
  if (!byId.size) return [];
  const genesis = [...byId.values()].filter(commit => commit.previous === null);
  if (genesis.length !== 1) throw queueError("ledger_invalid", "ledger requires exactly one genesis");
  const next = new Map();
  for (const commit of byId.values()) {
    if (commit.previous === null) continue;
    if (!byId.has(commit.previous)) throw queueError("ledger_invalid", "missing causal predecessor");
    if (next.has(commit.previous)) throw queueError("ledger_invalid", "forked causal history");
    next.set(commit.previous, commit);
  }
  const ordered = [];
  const visited = new Set();
  let commit = genesis[0];
  while (commit) {
    if (visited.has(commit.id)) throw queueError("ledger_invalid", "cyclic causal history");
    visited.add(commit.id);
    ordered.push(commit);
    commit = next.get(commit.id);
  }
  if (visited.size !== byId.size) throw queueError("ledger_invalid", "disconnected or cyclic causal history");
  return ordered;
}

function newState() {
  /** @type {Array<ReturnType<typeof validateCommit>>} */
  const transactions = [];
  return {
    repository: "",
    tip: "",
    policy_epoch: "",
    /** @type {ReturnType<typeof defaultPolicy> | null} */
    policy: null,
    works: new Map(),
    claims: new Map(),
    dispatches: new Map(),
    observations: new Map(),
    observationsById: new Map(),
    requests: new Map(),
    clocks: new Map(),
    admission_paused: false,
    grants_paused: false,
    credential_generation: "initial",
    observation_writes: new Map(),
    lifecycleWrites: new Map(),
    terminalBarriers: new Map(),
    cancellations: new Map(),
    ledgerBytes: 0,
    transactions,
    stats: {},
  };
}

function quiescent(state) {
  return [...state.works.values()].every(work => ["completed", "cancelled"].includes(work.state) && work.barrier !== "pending") && [...state.dispatches.values()].every(dispatch => dispatch.released);
}

/**
 * @param state
 * @param claimId
 * @param context
 * @param {{requireCompletion?: boolean, resource?: Record<string, string>}} [options]
 */
function validateClaimAuthority(state, claimId, context, { requireCompletion = false, resource } = {}) {
  const normalized = normalizeTrustedContext(context);
  const authority = claimAuthority(state, claimId, actorFromContext(context), requireCompletion);
  if (requireCompletion && authority.work.barrier !== "pending") throw queueError("claim_effects_unauthorized", "a terminal delivery barrier cannot authorize effects again");
  const run = authority.dispatch.run;
  if (normalized.ref !== run.ref || normalized.event !== run.event) throw queueError("run_binding_conflict", "native event/revision is not the approved bound run");
  if (resource !== undefined) validateEffectResourceAuthority(state, authority, resource);
  return authority;
}

function validateWorkerContinuation(state, context) {
  const normalized = normalizeTrustedContext(context);
  const authority = workerContinuationAuthority(state, actorFromContext(context));
  if (normalized.ref !== authority.dispatch.run.ref || normalized.event !== authority.dispatch.run.event) throw queueError("run_binding_conflict", "native event/revision is not the approved bound run");
  return authority;
}

function assertEvidence(state, dispatch, evidence, at, kind) {
  validateEvidence(evidence);
  boundedBytes(evidence, state.policy.limits.evidence_bytes, "lifecycle evidence");
  const profile = poolPolicy(state, dispatch.pool).profiles[dispatch.worker_profile];
  if (evidence.kind !== kind || evidence.checked_at > at || evidence.repository !== dispatchRepository(state, dispatch) || evidence.workflow !== profile.workflow || evidence.ref !== profile.ref || evidence.principal !== profile.principal)
    throw queueError("evidence_invalid", "evidence scope or timestamp does not match reservation");
  if (kind === "terminal_run" || kind === "delivery") {
    if (!dispatch.run || evidence.run_id !== dispatch.run.run_id || evidence.run_attempt !== 1) throw queueError("evidence_invalid", "evidence does not name the exact bound native attempt");
    if (kind === "terminal_run" && (evidence.source !== "github_api" || evidence.status !== "completed" || !evidence.conclusion)) throw queueError("evidence_invalid", "release/failure requires exact trusted terminal evidence");
    if (kind === "delivery" && (evidence.source !== "verified_receipts" || !evidence.receipt)) throw queueError("evidence_invalid", "Result requires independently verified scoped receipts");
  }
  if (kind === "prelaunch" && (dispatch.state !== "reserved" || dispatch.sender || dispatch.run || evidence.source !== "trusted_publisher")) throw queueError("evidence_invalid", "prelaunch cancellation cannot race past a start marker");
  if (kind === "nonlaunch" && (dispatch.run || evidence.source !== "github_api" || !evidence.receipt)) throw queueError("evidence_invalid", "nonlaunch requires positive API rejection evidence, never silence");
}

function dispatchRepository(state, dispatch) {
  const grant = state.requests.get(dispatch.request_id);
  return grant.actor.repository;
}

function applyObservation(state, operation, commit) {
  if (commit.actor.role === "worker") {
    const authority = workerContinuationAuthority(state, commit.actor);
    if (!poolPolicy(state, authority.work.pool).allowed_repositories.includes(operation.resource.repository)) throw queueError("dependency_unauthorized", "worker observation is outside its original parent's pool");
  }
  if (operation.observed_at > commit.at || operation.credential_generation !== state.credential_generation) throw queueError("observation_invalid", "observation must bind current authorized generation and decision time");
  boundedBytes(operation, state.policy.limits.evidence_bytes, "Observation");
  const authorized = Object.values(state.policy.pools).some(pool => pool.allowed_repositories.includes(operation.resource.repository));
  if (!authorized) throw queueError("dependency_unauthorized", "observation resource is not allowlisted");
  const previous = state.observationsById.get(operation.observation_id);
  if (previous) {
    if (canonical(previous) !== canonical(operation)) throw queueError("observation_conflict", "observation identity is immutable");
    return;
  }
  const key = gateKey(operation.resource, operation.condition);
  const count = (state.observation_writes.get(key) ?? 0) + 1;
  if (count > state.policy.limits.observation_writes) throw queueError("resource_limit", "external gate observation write budget exhausted");
  state.observation_writes.set(key, count);
  state.observationsById.set(operation.observation_id, operation);
  state.observations.set(key, operation);
}

function applyLifecycle(state, operation, commit) {
  if (operation.evidence) boundedBytes(operation.evidence, state.policy.limits.evidence_bytes, "evidence");
  const work = operation.work_id && state.works.get(operation.work_id);
  const claim = operation.claim_id && state.claims.get(operation.claim_id);
  const dispatch = state.dispatches.get(operation.dispatch_id || claim?.dispatch_id);
  switch (operation.kind) {
    case "Completion": {
      const authority = claimAuthority(state, operation.claim_id, commit.actor, false);
      if (
        operation.work_id !== authority.work.work_id ||
        operation.dispatch_id !== authority.dispatch.dispatch_id ||
        operation.claim_handle !== authority.claim.handle ||
        operation.run_id !== authority.dispatch.run.run_id ||
        operation.run_attempt !== 1
      )
        throw queueError("claim_scope_invalid", "Completion is outside native Claim scope");
      work.state = "completed";
      work.completion_id = commit.id;
      work.completion_at = commit.at;
      work.barrier = "pending";
      claim.state = "completed";
      claim.terminal_commit_id = commit.id;
      break;
    }
    case "ClaimCancellation": {
      if (claim?.state === "cancelled") {
        const cancelled = state.cancellations.get(claim.claim_id);
        if (!cancelled || canonical(cancelled) !== canonical(operation)) throw queueError("cancellation_conflict", "cancellation/backoff metadata is immutable");
        break;
      }
      if (!claim || claim.work_id !== operation.work_id || claim.state !== "open" || work?.claim_id !== operation.claim_id) throw queueError("claim_ineffective", "only effective open Claims can be cancelled");
      if (commit.actor.role === "worker") claimAuthority(state, claim.claim_id, commit.actor, false);
      const retry = poolPolicy(state, work.pool).retry;
      let backoffOrigin = commit.at;
      if (commit.request.kind === "release") {
        const release = commit.operations.find(candidate => candidate.kind === "Release" && candidate.dispatch_id === claim.dispatch_id);
        if (release && ["prelaunch", "nonlaunch", "terminal_run"].includes(release.evidence.kind)) {
          assertEvidence(state, dispatch, release.evidence, commit.at, release.evidence.kind);
          backoffOrigin = release.evidence.checked_at;
        }
      }
      if (operation.retry_not_before < backoffOrigin + retry.backoff_ms || !Number.isSafeInteger(backoffOrigin + retry.backoff_ms)) throw queueError("retry_invalid", "retry must respect trusted bounded backoff");
      claim.state = "cancelled";
      claim.terminal_commit_id = commit.id;
      claim.cancellation_reason = operation.reason;
      claim.retry_not_before = operation.retry_not_before;
      work.state = "available";
      work.retry_not_before = operation.retry_not_before;
      delete work.claim_id;
      state.cancellations.set(claim.claim_id, operation);
      break;
    }
    case "WorkCancellation":
      if (!work || work.state === "completed") throw queueError("work_terminal", "completed ownership cannot be reopened or cancelled");
      if (operation.claim_id) {
        const selected = state.claims.get(operation.claim_id);
        if (
          !selected ||
          selected.work_id !== work.work_id ||
          work.claim_id !== selected.claim_id ||
          (!(work.state === "claimed" && selected.state === "open") && !(work.state === "cancelled" && selected.state === "cancelled" && selected.terminal_commit_id === work.cancellation_commit_id))
        )
          throw queueError("claim_scope_invalid", "selected Claim is no longer this Work's current owner; refresh state");
      }
      if (commit.actor.role === "producer") {
        const rule = state.policy.producers[commit.actor.principal];
        if (!rule || !rule.pools.includes(work.pool) || !rule.priorities.includes(work.priority) || !rule.fairness_keys.includes(work.fairness_key))
          throw queueError("admission_unauthorized", "producer cannot cancel another accounting scope");
      }
      if (commit.actor.role === "worker") {
        const scopedDispatch = state.dispatches.get(commit.actor.dispatch_id);
        const member = scopedDispatch?.claims.find(candidate => candidate.handle === commit.actor.claim_handle);
        const scoped = member && bindingAuthority(state, member.claim_id, commit.actor);
        if (
          !scoped ||
          scoped.work.work_id !== work.work_id ||
          scoped.claim.state !== "cancelled" ||
          !["available", "cancelled"].includes(work.state) ||
          work.attempts < poolPolicy(state, work.pool).retry.max_attempts ||
          commit.request.kind !== "finish" ||
          operation.reason !== "attempts_exhausted"
        )
          throw queueError("work_unauthorized", "worker can terminally cancel only its exhausted cancelled Claim scope");
      }
      if (work.state === "cancelled") {
        const cancelled = state.cancellations.get(work.work_id);
        if (!cancelled || canonical(cancelled) !== canonical(operation)) throw queueError("cancellation_conflict", "terminal cancellation metadata is immutable");
        break;
      }
      if (work.state === "claimed") {
        const cancelled = state.claims.get(work.claim_id);
        cancelled.state = "cancelled";
        cancelled.terminal_commit_id = commit.id;
        cancelled.cancellation_reason = operation.reason;
      }
      work.state = "cancelled";
      work.cancellation_reason = operation.reason;
      if (operation.claim_id) work.cancellation_claim_id = operation.claim_id;
      work.cancellation_commit_id = commit.id;
      state.cancellations.set(work.work_id, operation);
      break;
    case "Result":
    case "DeliveryFailure": {
      const previous = state.terminalBarriers.get(operation.work_id);
      if (previous) {
        if (canonical(previous) !== canonical(operation)) throw queueError("delivery_conflict", "terminal Result/failure descriptor and evidence are immutable");
        break;
      }
      if (!claim || !work || claim.work_id !== work.work_id || claim.state !== "completed" || work.claim_id !== claim.claim_id || work.completion_id !== operation.completion_id || work.barrier !== "pending")
        throw queueError("delivery_conflict", "Result/DeliveryFailure are exclusive terminal barriers for frozen Completion");
      if (operation.kind === "Result") {
        assertEvidence(state, dispatch, operation.evidence, commit.at, "delivery");
        boundedBytes(operation.descriptor, state.policy.limits.result_bytes, "Result descriptor");
        work.barrier = "verified";
        work.result = operation.descriptor;
        work.result_commit_id = commit.id;
      } else {
        assertEvidence(state, dispatch, operation.evidence, commit.at, "terminal_run");
        const recovery = poolPolicy(state, work.pool).reconciliation;
        if ((operation.evidence.attempts ?? 0) < recovery.max_attempts && commit.at - work.completion_at < recovery.deadline_ms) throw queueError("reconciliation_pending", "delivery failure requires exhausted bounded verification");
        if (operation.disposition !== (operation.evidence.effects ?? "unknown") || (operation.disposition === "none" && !operation.evidence.receipt))
          throw queueError("evidence_invalid", "none requires positive no-effects proof; missing receipts mean unknown");
        work.barrier = "failed";
        work.disposition = operation.disposition;
      }
      state.terminalBarriers.set(operation.work_id, operation);
      break;
    }
    case "Dispatch": {
      if (!dispatch || dispatch.released) throw queueError("dispatch_invalid", "dispatch reservation is absent or released");
      const profile = poolPolicy(state, dispatch.pool).profiles[dispatch.worker_profile];
      const writes = (state.lifecycleWrites.get(dispatch.dispatch_id) ?? 0) + 1;
      if (writes > poolPolicy(state, dispatch.pool).reconciliation.max_attempts + 4) throw queueError("resource_limit", "dispatch reconciliation write budget exhausted");
      if (operation.state === "started") {
        if (state.grants_paused) throw queueError("grants_paused", "new launch markers are paused");
        if (
          commit.actor.role !== "dispatcher" ||
          !commit.actor.workflow ||
          !commit.actor.run_id ||
          commit.actor.run_attempt < 1 ||
          dispatch.state !== "reserved" ||
          !operation.sender ||
          canonical(operation.sender) !== canonical(commit.actor) ||
          operation.run ||
          operation.evidence
        )
          throw queueError("dispatch_invalid", "start marker must select exactly one authenticated approved sender");
        dispatch.sender = operation.sender;
      } else if (operation.state === "bound") {
        if (!["started", "uncertain", "unresolved", "bound"].includes(dispatch.state) || !operation.run || operation.sender || !operation.evidence)
          throw queueError("dispatch_invalid", "binding requires a committed start marker and exact authenticated native run evidence");
        const run = operation.run;
        if (run.repository !== dispatchRepository(state, dispatch) || run.workflow !== profile.workflow || run.ref !== profile.ref || run.principal !== profile.principal)
          throw queueError("run_binding_conflict", "native run scope differs from approved profile");
        const evidence = operation.evidence;
        assertEvidence(state, dispatch, evidence, commit.at, evidence.kind);
        if (!["github_api", "trusted_activation"].includes(evidence.source) || evidence.run_id !== run.run_id || evidence.run_attempt !== 1) throw queueError("evidence_invalid", "binding needs authenticated exact native run provenance");
        if (
          commit.actor.role === "worker" &&
          (commit.actor.run_id !== run.run_id ||
            commit.actor.run_attempt !== 1 ||
            commit.actor.repository !== run.repository ||
            commit.actor.workflow !== run.workflow ||
            commit.actor.principal !== run.principal ||
            commit.actor.dispatch_id !== dispatch.dispatch_id)
        )
          throw queueError("run_binding_conflict", "activation may bind only its authenticated actual run");
        if (dispatch.run && canonical(dispatch.run) !== canonical(run)) throw queueError("run_binding_conflict", "assignment already belongs to a different native run");
        const existingBinding = indexesFor(state).runBindings.get(run.run_id);
        if (existingBinding && existingBinding !== dispatch.dispatch_id) throw queueError("run_binding_conflict", "native run is already bound to another assignment");
        dispatch.run = run;
      } else if (operation.state === "rejected") {
        if (!operation.evidence || operation.run || operation.sender) throw queueError("evidence_invalid", "rejection requires positive nonlaunch evidence");
        assertEvidence(state, dispatch, operation.evidence, commit.at, "nonlaunch");
      } else {
        if (!["started", "uncertain", "unresolved"].includes(dispatch.state) || operation.run || operation.sender || !operation.reason) throw queueError("dispatch_invalid", "uncertainty cannot revoke a binding or authorize a retry");
        if (operation.state === "unresolved") {
          if (!operation.evidence || operation.evidence.kind !== "reconciliation" || (operation.evidence.attempts ?? 0) < poolPolicy(state, dispatch.pool).reconciliation.max_attempts)
            throw queueError("reconciliation_pending", "unresolved requires bounded reconciliation evidence");
        }
      }
      dispatch.state = operation.state;
      if (operation.reason) dispatch.reason = operation.reason;
      state.lifecycleWrites.set(dispatch.dispatch_id, writes);
      dispatch.lifecycle_writes = writes;
      break;
    }
    case "Release":
      if (!dispatch || dispatch.released) throw queueError("release_invalid", "native reservation may release once only");
      assertEvidence(state, dispatch, operation.evidence, commit.at, operation.evidence.kind);
      if (!["prelaunch", "nonlaunch", "terminal_run"].includes(operation.evidence.kind)) throw queueError("release_invalid", "only positive nonlaunch or exact terminal evidence can release");
      if (dispatch.claims.some(member => state.claims.get(member.claim_id).state === "open")) throw queueError("release_invalid", "settle only still-open Claims before native release");
      dispatch.released = true;
      break;
    default:
      throw queueError("unsupported_protocol", "unsupported lifecycle operation");
  }
}

function finishOperations(state, request, actor, at) {
  const { dispatch_id, claim_handle, outcome } = request.parameters;
  const dispatch = state.dispatches.get(dispatch_id);
  const member = dispatch?.claims.find(member => member.handle === claim_handle);
  if (!member) throw queueError("claim_scope_invalid", "finish selector is not in immutable assignment");
  bindingAuthority(state, member.claim_id, actor);
  const claim = state.claims.get(member.claim_id);
  if ((outcome === "completed" && claim.state === "completed") || (outcome === "cancelled" && claim.state === "cancelled")) return [];
  const authority = claimAuthority(state, member.claim_id, actor, false);
  if (outcome === "completed") return [{ kind: "Completion", work_id: member.work_id, claim_id: member.claim_id, dispatch_id, claim_handle, run_id: authority.dispatch.run.run_id, run_attempt: 1 }];
  const retry = poolPolicy(state, authority.work.pool).retry;
  /** @type {Array<{kind: string, work_id: string, claim_id: string, reason: string, retry_not_before: number} | {kind: string, work_id: string, reason: string}>} */
  const operations = [{ kind: "ClaimCancellation", work_id: member.work_id, claim_id: member.claim_id, reason: "worker_cancelled", retry_not_before: at + retry.backoff_ms }];
  if (authority.work.attempts >= retry.max_attempts) operations.push({ kind: "WorkCancellation", work_id: member.work_id, reason: "attempts_exhausted" });
  return operations;
}

function workerControlScope(state, commit) {
  if (commit.actor.role !== "worker" || !["submit", "dispatch_next", "observe"].includes(commit.request.kind)) return;
  const authority = workerContinuationAuthority(state, commit.actor);
  if (commit.request.kind === "submit") {
    for (const node of commit.request.parameters.nodes) validateSubmissionEntitlement(state, node, commit.actor);
  }
  if (commit.request.kind === "dispatch_next" && commit.request.parameters.pool !== authority.work.pool) throw queueError("claim_scope_invalid", "worker dispatch control is restricted to its parent's pool");
  if (commit.request.kind === "observe")
    for (const operation of commit.request.parameters.operations)
      if (!poolPolicy(state, authority.work.pool).allowed_repositories.includes(operation.resource.repository)) throw queueError("dependency_unauthorized", "worker observation is outside its original parent's pool");
}

function validateRequestContext(state, request, actor) {
  validateRequest(request, actor);
  validateRequestRole(actor, request.kind);
  workerControlScope(state, { actor, request });
  if (request.kind === "dispatch_next") validateDispatchParameters(request.parameters, state);
  if (request.kind === "submit") validateGraphAdmission(state, request.parameters.nodes, actor);
}

function replayTransactions(transactions) {
  return replayOrdered(causalChain(transactions));
}

function checkpointSnapshot(state, lastAt) {
  const projection = serializeProjection(state);
  projection.clocks = Object.fromEntries(
    Object.entries(projection.clocks).filter(([, clocks]) => clocks.classes.v !== "0" || Object.keys(clocks.classes.pass).length > 0 || Object.values(clocks.keys).some(clock => clock.v !== "0" || Object.keys(clock.pass).length > 0))
  );
  const object = map => Object.fromEntries(map);
  const receipts = [...state.requests.values()].map(commit => ({
    id: commit.id,
    request_id: commit.request.id,
    kind: commit.request.kind,
    fingerprint: commit.request.fingerprint,
    parameters_digest: commit.compacted_parameters_digest ?? digest(commit.request.parameters),
    ...(commit.request.kind === "dispatch_next" ? { dispatch_parameters: commit.request.parameters } : {}),
    actor: commit.actor,
    policy_epoch: commit.policy_epoch,
    at: commit.at,
    claims: commit.operations.filter(operation => operation.kind === "Claim"),
  }));
  const completionTimes = Object.fromEntries([...state.works].filter(([, work]) => work.state === "completed").map(([id, work]) => [id, work.completion_at]));
  return {
    ...projection,
    requests: receipts,
    seen_epochs: object([...state.seen_epochs].map(epoch => [epoch, true])),
    seen_generations: object([...state.seen_generations].map(generation => [generation, true])),
    observation_ids: object(state.observationsById),
    terminal_barriers: object(state.terminalBarriers),
    cancellations: object(state.cancellations),
    completion_times: completionTimes,
    last_at: lastAt,
  };
}

function restoreCheckpointState(checkpoint) {
  const op = checkpoint.operations[0];
  const invalid = () => queueError("checkpoint_invalid", "checkpoint state does not bind a valid prior projection");
  if (checkpoint.previous !== null || checkpoint.actor.role !== "administrator" || checkpoint.request.kind !== "checkpoint") throw invalid();
  const snapshot = op.state;
  closed(
    snapshot,
    [
      "repository",
      "tip",
      "policy_epoch",
      "policy",
      "works",
      "claims",
      "dispatches",
      "observations",
      "requests",
      "clocks",
      "admission_paused",
      "grants_paused",
      "credential_generation",
      "stats",
      "ledger_bytes",
      "observation_writes",
      "seen_epochs",
      "seen_generations",
      "observation_ids",
      "terminal_barriers",
      "cancellations",
      "completion_times",
      "last_at",
    ],
    [],
    "checkpoint state",
    "checkpoint_invalid"
  );
  if (digest(snapshot) !== op.state_sha256 || snapshot.tip !== op.prior_tip || snapshot.repository !== checkpoint.actor.repository || snapshot.policy_epoch !== checkpoint.policy_epoch || checkpoint.at < snapshot.last_at) throw invalid();
  validatePolicy(snapshot.policy);
  const object = value => value && typeof value === "object" && !Array.isArray(value);
  for (const key of ["works", "claims", "dispatches", "observations", "clocks", "observation_writes", "seen_epochs", "seen_generations", "observation_ids", "terminal_barriers", "cancellations", "completion_times"])
    if (!object(snapshot[key])) throw invalid();
  if (
    !Array.isArray(snapshot.requests) ||
    snapshot.requests.length !== snapshot.stats?.transactions ||
    Object.keys(snapshot.works).length !== snapshot.stats.work ||
    Object.keys(snapshot.claims).length !== snapshot.stats.claims ||
    snapshot.seen_epochs[snapshot.policy_epoch] !== true ||
    snapshot.seen_generations[snapshot.credential_generation] !== true
  )
    throw invalid();
  const state = newState();
  state.repository = snapshot.repository;
  state.policy_epoch = snapshot.policy_epoch;
  state.policy = snapshot.policy;
  state.tip = checkpoint.id;
  state.admission_paused = snapshot.admission_paused;
  state.grants_paused = snapshot.grants_paused;
  state.credential_generation = snapshot.credential_generation;
  state.works = new Map(Object.entries(snapshot.works).map(([id, work]) => [id, { ...work, ...(Object.hasOwn(snapshot.completion_times, id) ? { completion_at: snapshot.completion_times[id] } : {}) }]));
  state.claims = new Map(Object.entries(snapshot.claims));
  state.dispatches = new Map(Object.entries(snapshot.dispatches));
  state.observations = new Map(Object.entries(snapshot.observations));
  state.observationsById = new Map(Object.entries(snapshot.observation_ids));
  state.observation_writes = new Map(Object.entries(snapshot.observation_writes));
  state.terminalBarriers = new Map(Object.entries(snapshot.terminal_barriers));
  state.cancellations = new Map(Object.entries(snapshot.cancellations));
  state.lifecycleWrites = new Map([...state.dispatches].map(([id, dispatch]) => [id, dispatch.lifecycle_writes]));
  state.clocks = new Map(
    Object.entries(snapshot.clocks).map(([pool, clocks]) => {
      const decodeClock = clock => {
        if (!clock || !/^(?:0|[1-9][0-9]*)$/.test(clock.v) || !object(clock.pass) || !object(clock.active)) throw invalid();
        for (const pass of Object.values(clock.pass)) if (!/^(?:0|[1-9][0-9]*)$/.test(pass)) throw invalid();
        return { virtual: BigInt(clock.v), passes: new Map(Object.entries(clock.pass).map(([key, pass]) => [key, BigInt(pass)])), active: new Set(Object.keys(clock.active)) };
      };
      return [pool, { classes: decodeClock(clocks.classes), keys: new Map(Object.entries(clocks.keys).map(([key, clock]) => [Number(key), decodeClock(clock)])) }];
    })
  );
  state.requests = new Map();
  state.historicalTransactions = [];
  const commitIDs = new Set();
  for (const receipt of snapshot.requests) {
    closed(receipt, ["id", "request_id", "kind", "fingerprint", "parameters_digest", "actor", "policy_epoch", "at", "claims"], ["dispatch_parameters"], "checkpoint receipt", "checkpoint_invalid");
    identity(receipt.id, "checkpoint receipt commit");
    identity(receipt.request_id, "checkpoint receipt request");
    validateActor(receipt.actor);
    validateRequestRole(receipt.actor, receipt.kind);
    if (
      state.requests.has(receipt.request_id) ||
      commitIDs.has(receipt.id) ||
      receipt.actor.repository !== state.repository ||
      !/^[a-f0-9]{64}$/.test(receipt.fingerprint) ||
      !/^[a-f0-9]{64}$/.test(receipt.parameters_digest) ||
      (receipt.kind === "dispatch_next") !== Object.hasOwn(receipt, "dispatch_parameters") ||
      !Number.isSafeInteger(receipt.at) ||
      receipt.at < 0 ||
      !Array.isArray(receipt.claims) ||
      receipt.claims.some(operation => operation.kind !== "Claim")
    )
      throw invalid();
    commitIDs.add(receipt.id);
    const prior = {
      version: 3,
      id: receipt.id,
      request: { id: receipt.request_id, kind: receipt.kind, fingerprint: receipt.fingerprint, parameters: receipt.dispatch_parameters ?? null },
      compacted_parameters_digest: receipt.parameters_digest,
      actor: receipt.actor,
      policy_epoch: receipt.policy_epoch,
      at: receipt.at,
      operations: receipt.claims,
    };
    state.requests.set(receipt.request_id, prior);
    state.historicalTransactions.push(prior);
  }
  const nodeFields = ["kind", "work_id", "graph_id", "node_key", "pool", "priority", "fairness_key", "worker_profile", "batch_trust_domain", "payload", "depends_on", "enqueued", "subject", "replacement_of"];
  for (const [index, receipt] of snapshot.requests.entries()) {
    const prior = state.requests.get(receipt.request_id);
    if (receipt.kind === "submit") {
      const nodes = [...state.works.values()].filter(work => work.position?.commit === index).sort((a, b) => a.position.operation - b.position.operation);
      if (!nodes.length || nodes.some((node, offset) => node.position.operation !== offset)) throw invalid();
      prior.request.parameters = { nodes: nodes.map(work => Object.fromEntries(nodeFields.filter(field => Object.hasOwn(work, field)).map(field => [field, work[field]]))) };
    }
    if (["submit", "dispatch_next"].includes(receipt.kind) && (digest(prior.request.parameters) !== receipt.parameters_digest || fingerprint(receipt.actor, receipt.kind, prior.request.parameters) !== receipt.fingerprint)) throw invalid();
  }
  const installedPolicy = state.policy;
  if (!installedPolicy) throw invalid();
  for (const [id, work] of state.works) {
    if (
      work.work_id !== id ||
      !Number.isSafeInteger(work.position?.commit) ||
      work.position.commit < 0 ||
      work.position.commit >= snapshot.requests.length ||
      snapshot.requests[work.position.commit].kind !== "submit" ||
      !Number.isSafeInteger(work.attempts) ||
      work.attempts < 0 ||
      (work.state === "claimed" && (!work.claim_id || state.claims.get(work.claim_id)?.state !== "open")) ||
      (work.state === "completed" && !Object.hasOwn(snapshot.completion_times, id))
    )
      throw invalid();
    if (work.state !== "completed" && work.state !== "cancelled") {
      const pool = installedPolicy.pools[work.pool];
      if (!pool?.profiles[work.worker_profile] || work.attempts > pool.retry.max_attempts) throw invalid();
    }
  }
  for (const [id, claim] of state.claims) if (claim.claim_id !== id || !state.works.has(claim.work_id) || !state.dispatches.has(claim.dispatch_id)) throw invalid();
  for (const [id, dispatch] of state.dispatches) if (dispatch.dispatch_id !== id || state.requests.get(dispatch.request_id)?.id !== dispatch.commit_id) throw invalid();
  if (state.requests.has(checkpoint.request.id)) throw invalid();
  state.seen_epochs = new Set(Object.keys(snapshot.seen_epochs));
  state.seen_generations = new Set(Object.keys(snapshot.seen_generations));
  state.requests.set(checkpoint.request.id, checkpoint);
  state.transactions = [checkpoint];
  state.ledgerBytes = canonicalBytes(checkpoint) + 1;
  const policy = state.policy;
  if (!policy || state.ledgerBytes + require("./work_queue_limits.cjs").recoveryHeadroom(state) > policy.limits.ledger_bytes + policy.limits.recovery_bytes) throw invalid();
  return state;
}

function replayOrdered(ordered) {
  if (!ordered.length) throw queueError("policy_missing", "existing queue has no policy genesis");
  let state = newState();
  trackState(state);
  let epochs = new Set();
  let generations = new Set(["initial"]);
  let offset = 0;
  if (ordered[0].operations[0]?.kind === "Checkpoint") {
    untrackState(state);
    state = restoreCheckpointState(ordered[0]);
    trackState(state);
    offset = state.historicalTransactions.length + 1;
    epochs = new Set(state.seen_epochs);
    generations = new Set(state.seen_generations);
    ordered = ordered.slice(1);
  }
  for (let ordinal = 0; ordinal < ordered.length; ordinal++) {
    const commit = ordered[ordinal];
    if (ordinal === 0 && !offset) state.repository = commit.actor.repository;
    const priorRequest = state.requests.get(commit.request.id);
    if (priorRequest) throw queueError("request_reused", priorRequest.request.fingerprint === commit.request.fingerprint ? "committed request cannot appear in another commit" : "request identity has different semantics");
    if (ordinal === 0 && !offset && (commit.operations.length !== 1 || commit.operations[0].kind !== "Policy" || commit.request.kind !== "policy" || commit.actor.role !== "administrator"))
      throw queueError("policy_missing", "genesis must install exactly one mandatory Policy");
    if (state.policy && commit.actor.repository !== state.repository) throw queueError("actor_unauthorized", "foreign queue repository origin");
    const policies = commit.operations.filter(operation => operation.kind === "Policy");
    if (policies.length) {
      if (policies.length !== 1 || commit.operations.length !== 1 || commit.policy_epoch !== policies[0].epoch || epochs.has(policies[0].epoch) || (state.policy && !quiescent(state)))
        throw queueError("policy_not_quiescent", "Policy epoch changes require a drained queue and new immutable identity");
      epochs.add(policies[0].epoch);
      state.policy = policies[0].policy;
      state.policy_epoch = policies[0].epoch;
      state.clocks = freshClocks(state.policy);
      state.observation_writes = new Map();
    } else if (!state.policy || commit.policy_epoch !== state.policy_epoch) throw queueError("policy_invalid", "commit does not bind authoritative installed epoch");
    if (!state.policy) throw queueError("policy_missing", "existing queue has no policy genesis");
    if (commit.operations.length > state.policy.limits.operations) throw queueError("resource_limit", "operation count exceeds installed policy");
    workerControlScope(state, commit);
    if (commit.request.kind === "finish" && canonical(finishOperations(state, commit.request, commit.actor, commit.at)) !== canonical(commit.operations)) throw queueError("request_invalid", "finish differs from exact scoped outcome");
    const newWork = commit.operations.filter(operation => operation.kind === "Work");
    if (newWork.length) validateGraphAdmission(state, newWork, commit.actor);
    let expected;
    let claimIndex = 0;
    for (let index = 0; index < commit.operations.length; index++) {
      const operation = commit.operations[index];
      switch (operation.kind) {
        case "Checkpoint":
          throw queueError("checkpoint_invalid", "checkpoint must be the sole root transaction");
        case "Policy":
          break;
        case "Control":
          if (operation.control === "admission_paused") state.admission_paused = operation.value;
          if (operation.control === "grants_paused") state.grants_paused = operation.value;
          if (operation.control === "credential_generation") {
            if (operation.value !== state.credential_generation && generations.has(operation.value)) throw queueError("credential_generation_reused", "credential cutover cannot revive a stale observation generation");
            generations.add(operation.value);
            state.credential_generation = operation.value;
          }
          boundedBytes(operation, state.policy.limits.evidence_bytes, "Control");
          break;
        case "Work":
          if (!state.works.has(operation.work_id)) state.works.set(operation.work_id, { ...operation, state: "available", position: { commit: ordinal + offset, operation: index }, attempts: 0, retry_not_before: 0, barrier: "none" });
          break;
        case "WorkPriority": {
          const work = state.works.get(operation.work_id);
          if (!work) throw queueError("work_missing", "priority change references missing Work");
          if (work.state !== "available" || work.claim_id) throw queueError("ownership_terminal", "reprioritization requires available Work; immutable assignments cannot change");
          if ((work.effective_priority ?? work.priority) !== operation.expected_priority) throw queueError("priority_conflict", "priority changed since selection; refresh state");
          work.effective_priority = operation.priority;
          break;
        }
        case "Observation":
          if (expected) throw queueError("packing_invalid", "dispatch observations must precede its deterministic Claim prefix");
          applyObservation(state, operation, commit);
          break;
        case "Claim": {
          if (!expected) {
            validateDispatchParameters(commit.request.parameters, state);
            expected = planDispatch(state, commit.request.parameters, { requestId: commit.request.id, commitId: commit.id, at: commit.at, precedingOperations: index });
          }
          if (canonical(operation) !== canonical(expected.operations[claimIndex])) throw queueError("selection_invalid", "Claim/order/group/handle does not match deterministic fair packing");
          const selection = planNext(state, commit.request.parameters.pool, commit.at);
          applyScheduledClaim(state, operation, selection, commit);
          claimIndex++;
          break;
        }
        default:
          applyLifecycle(state, operation, commit);
      }
      updateIndexes(state, operation);
    }
    if (expected && claimIndex !== expected.operations.length) throw queueError("packing_invalid", "Claim commit omitted an admissible member of the fair prefix");
    if (!expected && commit.request.kind === "dispatch_next") throw queueError("request_invalid", "no-grant evaluation cannot consume a stable request identity");
    // Retry exhaustion cannot leave an immediately retryable poison head.
    for (const operation of commit.operations)
      if (operation.kind === "ClaimCancellation") {
        const work = state.works.get(operation.work_id);
        if (work.attempts >= poolPolicy(state, work.pool).retry.max_attempts && work.state !== "cancelled") throw queueError("retry_exhausted", "exhausted Claim must be followed by explicit WorkCancellation");
      }
    state.requests.set(commit.request.id, commit);
    state.tip = commit.id;
    state.transactions.push(commit);
    const bytes = canonicalBytes(commit) + 1;
    checkLedgerBudget(
      state,
      bytes,
      commit.operations.some(operation => operation.kind === "Work" || operation.kind === "Claim"),
      commit.operations.some(operation => operation.kind === "Observation")
    );
    state.ledgerBytes += bytes;
  }
  if (!state.policy) throw queueError("policy_missing", "existing queue has no policy genesis");
  projectViews(state);
  state.seen_epochs = epochs;
  state.seen_generations = generations;
  untrackState(state);
  return Object.assign(state, { policy: state.policy });
}

function projectViews(state) {
  const ordered = [...state.works.values()].sort(fifoCompare);
  state.work = Object.fromEntries(ordered.map(work => [work.work_id, work.state]));
  state.winner = Object.fromEntries(ordered.map(work => [work.work_id, work.claim_id ?? null]));
  state.claim = Object.fromEntries([...state.claims].map(([id, claim]) => [id, claim.state]));
  state.available = ordered.filter(work => work.state === "available").map(work => work.work_id);
  const vertices = new Set(ordered.map(work => `work:${work.work_id}`));
  for (const work of ordered) for (const edge of work.depends_on) if (edge.kind !== "work") vertices.add(`${work.graph_id}:${gateKey(edge.resource, edge.condition)}`);
  state.stats = {
    work: ordered.length,
    available: ordered.filter(work => work.state === "available").length,
    claimed: ordered.filter(work => work.state === "claimed").length,
    completed: ordered.filter(work => work.state === "completed").length,
    cancelled: ordered.filter(work => work.state === "cancelled").length,
    claims: state.claims.size,
    dispatches: [...state.dispatches.values()].filter(dispatch => !dispatch.released).length,
    transactions: state.transactions.length + (state.historicalTransactions?.length ?? 0),
    nodes: vertices.size,
  };
}

function serializeProjection(state) {
  const mapObject = map => Object.fromEntries([...map].sort(([a], [b]) => utf8Compare(String(a), String(b))));
  return {
    repository: state.repository,
    tip: state.tip,
    policy_epoch: state.policy_epoch,
    policy: state.policy,
    works: Object.fromEntries(
      [...state.works].map(([id, work]) => {
        const { completion_at, ...wire } = work;
        return [id, wire];
      })
    ),
    claims: mapObject(state.claims),
    dispatches: mapObject(state.dispatches),
    observations: mapObject(state.observations),
    requests: mapObject(state.requests),
    clocks: Object.fromEntries([...state.clocks].map(([pool, clock]) => [pool, diagnostics(clock)])),
    admission_paused: state.admission_paused,
    grants_paused: state.grants_paused,
    credential_generation: state.credential_generation,
    stats: state.stats,
    ledger_bytes: state.ledgerBytes,
    observation_writes: mapObject(state.observation_writes),
  };
}

function replayTransactionLog(contents) {
  log.debug("ledger.replay.start");
  if (typeof contents !== "string" || Buffer.byteLength(contents, "utf8") > 80 * 1024 * 1024) throw queueError("resource_limit", "ledger exceeds cold parser bound");
  if (!contents || !contents.endsWith("\n")) throw queueError("ledger_invalid", "queue log must be nonempty and newline terminated");
  const lines = contents.split("\n");
  if (lines.at(-1) === "") lines.pop();
  if (lines.some(line => !line.trim())) throw queueError("ledger_invalid", "empty or truncated log record");
  const commits = lines.map((line, index) => {
    if (Buffer.byteLength(line, "utf8") > 8 * 1024 * 1024) throw queueError("resource_limit", `commit line ${index + 1} exceeds parser bound`);
    return parseStrictJSON(line);
  });
  const state = replayTransactions(commits);
  log.debug("ledger.replay.complete", { transactions: commits.length, works: state.works.size, claims: state.claims.size });
  return state;
}

function parseTransactionLog(contents) {
  return replayTransactionLog(contents).transactions;
}

function serializeTransactionLog(transactions) {
  const ordered = replayTransactions(transactions).transactions;
  return ordered.length ? ordered.map(commit => canonical(commit)).join("\n") + "\n" : "";
}

function compactTransactions(transactions, priorGitSHA, actor, at = Date.now()) {
  const validated = replayTransactions(transactions);
  if (priorGitSHA === undefined) return validated.transactions;
  if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(priorGitSHA) || actor?.role !== "administrator" || actor.repository !== validated.repository || !Number.isSafeInteger(at) || at < 0 || at > Number.MAX_SAFE_INTEGER)
    throw queueError("checkpoint_invalid", "checkpoint requires an administrator, valid time and prior Git commit SHA");
  const history = serializeTransactionLog(validated.transactions);
  const history_sha256 = createHash("sha256").update(history, "utf8").digest("hex");
  const state = checkpointSnapshot(validated, validated.transactions.at(-1).at);
  const parameters = { prior_git_sha: priorGitSHA, prior_tip: validated.tip, history_sha256, state_sha256: digest(state) };
  const operation = { kind: "Checkpoint", ...parameters, state };
  const requestId = `checkpoint_${createHash("sha256").update(`${validated.tip}\n${priorGitSHA}`, "utf8").digest("hex")}`;
  const request = newRequest(requestId, "checkpoint", actor, parameters);
  const checkpoint = {
    version: 3,
    id: `q_${createHash("sha256").update(requestId, "utf8").digest("hex")}`,
    previous: null,
    request,
    actor,
    policy_epoch: validated.policy_epoch,
    at,
    operations: [operation],
  };
  replayTransactions([checkpoint]);
  return [checkpoint];
}

function prepareCheckpoint(source, priorGitSHA, actor, at) {
  const transactions = Array.isArray(source) ? source : source?.transactions;
  if (!Array.isArray(transactions) || !transactions.length) throw queueError("checkpoint_invalid", "checkpoint requires a validated nonempty queue");
  const ordered = causalChain(transactions);
  const root = ordered[0];
  const administrator = actor ?? { role: "administrator", principal: root.actor.principal, repository: root.actor.repository };
  return compactTransactions(transactions, priorGitSHA, administrator, at ?? ordered.at(-1).at);
}

function appendCommit(transactions, commit) {
  log.debug("commit.append.start", { transactions: transactions.length });
  validateCommit(commit);
  const ordered = causalChain(transactions);
  if (ordered.some(transaction => transaction.request.id === commit.request.id)) {
    const before = replayOrdered(ordered);
    const prior = before.requests.get(commit.request.id);
    if (prior.request.fingerprint !== commit.request.fingerprint || canonical(prior.actor) !== canonical(commit.actor)) throw queueError("request_reused", "stable request identity has different validated meaning");
    log.debug("commit.append.reused");
    return { transactions: before.transactions, state: before, commit: prior, idempotent: true };
  }
  if (commit.previous !== (ordered.at(-1)?.id ?? null)) throw queueError("ledger_invalid", "append does not extend checked causal tip");
  if (ordered.some(transaction => transaction.id === commit.id)) throw queueError("ledger_invalid", "conflicting duplicate commit ID");
  if (ordered.length >= 1000000) throw queueError("resource_limit", "ledger commit count exceeded");
  const state = replayOrdered([...ordered, commit]);
  log.debug("commit.append.complete", { transactions: state.transactions.length, operations: commit.operations.length });
  return { transactions: state.transactions, state, commit, idempotent: false };
}

function generateRequestOperations(state, request, actor, at, commitId, observations = []) {
  validateRequestContext(state, request, actor);
  if (request.kind === "dispatch_next") return observations.length ? planDispatchWithObservations(state, request, actor, at, commitId, observations) : planDispatch(state, request.parameters, { requestId: request.id, commitId, at });
  if (request.kind === "submit") {
    const nodes = request.parameters.nodes;
    if (nodes.every(node => state.works.has(node.work_id))) {
      const index = state.works.get(nodes[0].work_id).position.commit;
      return {
        operations: [],
        idempotent: true,
        reason: "already_submitted",
        commit: index < (state.historicalTransactions?.length ?? 0) ? state.historicalTransactions[index] : state.transactions[index - (state.historicalTransactions?.length ?? 0)],
      };
    }
    return { operations: nodes };
  }
  if (request.kind === "finish") {
    const operations = finishOperations(state, request, actor, at);
    return { operations, idempotent: !operations.length, ...(!operations.length ? { reason: "already_finished" } : {}) };
  }
  return { operations: request.parameters.operations };
}

function planDispatchWithObservations(state, request, actor, at, commitId, observations) {
  validateRequestContext(state, request, actor);
  if (request.kind !== "dispatch_next" || !Array.isArray(observations)) throw queueError("request_invalid", "atomic observed dispatch requires dispatch_next and an observation array");
  const working = { ...state, observations: new Map(state.observations), observationsById: new Map(state.observationsById), observation_writes: new Map(state.observation_writes) };
  const commit = { at, actor };
  for (const observation of observations) {
    validateOperation(observation);
    if (observation.kind !== "Observation" || !OP_ROLES.Observation.includes(actor.role)) throw queueError("unauthorized_operation", "only trusted dependency evidence may precede a grant");
    applyObservation(working, observation, commit);
  }
  const decision = planDispatch(working, request.parameters, { requestId: request.id, commitId, at, precedingOperations: observations.length });
  return { ...decision, tip: state.tip, operations: decision.operations.length ? [...observations, ...decision.operations] : [] };
}

function explainWork(state, workId, at) {
  const work = state.works.get(workId);
  if (!work) throw queueError("work_missing", "requested Work does not exist");
  const next = Object.hasOwn(state.policy.pools, work.pool) ? selectionOnly(planNext(state, work.pool, at)) : { reason: "policy_epoch_retired", observations: [] };
  const dependency = dependencyStatus(state, work, at);
  return {
    work_id: workId,
    state: work.state,
    barrier: work.barrier,
    position: work.position,
    readiness: dependency,
    scheduling: eligibility(state, work, at),
    retry_not_before: work.retry_not_before,
    attempts: work.attempts,
    next,
    capacity: reservationCounts(state, work.pool, work.fairness_key),
  };
}

function unsupportedDirectClaim() {
  throw queueError("unsupported_protocol", "direct Work selectors and standalone queue facts are unsupported; use a checked dispatch_next request");
}

module.exports = {
  appendCommit,
  assignmentForDispatch,
  assignmentsForRequest,
  applyTransactions: appendCommit,
  causalChain,
  claimOldestAvailableWork: unsupportedDirectClaim,
  compactTransactions,
  createWorkTransaction: unsupportedDirectClaim,
  explainWork,
  finishOperations,
  generateRequestOperations,
  newRequest,
  newState,
  oldestAvailableWork: unsupportedDirectClaim,
  parseTransactionLog,
  planDispatchWithObservations,
  prepareCheckpoint,
  proposedCommitId,
  planNext,
  quiescent,
  replayTransactions,
  replayTransactionLog,
  serializeProjection,
  serializeTransactionLog,
  validateClaimAuthority,
  validateCommit,
  validateEvidence,
  validateOperation,
  validateRequest,
  validateRequestContext,
  validateRunBinding,
  validateWorkerContinuation,
  validateTransaction: validateCommit,
  workDefinition,
};
