// @ts-check
"use strict";

const { canonical, closed, identity, integer, queueError } = require("./work_queue_codec.cjs");
const { DEFAULT_LIMITS, validateLimits } = require("./work_queue_limits.cjs");
const { tickScale } = require("./work_queue_scheduler.cjs");

const ROLES = new Set(["administrator", "producer", "dispatcher", "worker", "reconciler", "projector"]);
const ROLE_KINDS = {
  administrator: ["policy", "control", "submit", "dispatch_next", "observe", "cancel_work"],
  producer: ["submit", "cancel_work"],
  dispatcher: ["submit", "dispatch_next", "observe", "dispatch"],
  worker: ["submit", "dispatch_next", "observe", "finish", "dispatch"],
  reconciler: ["observe", "dispatch", "release", "result", "delivery_failure", "cancel_claim", "cancel_work"],
  projector: ["issue_link"],
};
const ACTOR_FIELDS = ["role", "principal", "repository"];
const ACTOR_OPTIONAL = ["workflow", "run_id", "run_attempt", "dispatch_id", "claim_handle"];

/** @typedef {{role: string, principal: string, repository: string, workflow?: string, run_id?: string, run_attempt?: number, dispatch_id?: string, claim_handle?: string}} QueueActor */

/**
 * @param {unknown} value
 * @param {string} name
 * @param {string} [code]
 * @returns {string}
 */
function decimal(value, name, code = "ledger_invalid") {
  const match = typeof value === "string" && value.match(/^[1-9][0-9]*$/);
  if (!match || match[0] !== value || value.length > 256) throw queueError(code, `${name} must be a positive decimal identity string`);
  return value;
}

function validateActor(actor) {
  closed(actor, ACTOR_FIELDS, ACTOR_OPTIONAL, "actor", "unsupported_protocol");
  if (!ROLES.has(actor.role)) throw queueError("actor_unauthorized", "unsupported operation role");
  decimal(actor.principal, "actor principal", "actor_unauthorized");
  for (const field of ["repository", "workflow", "dispatch_id", "claim_handle"]) if (Object.hasOwn(actor, field)) identity(actor[field], field);
  if (!/^[^/\s]+\/[^/\s]+$/.test(actor.repository)) throw queueError("actor_unauthorized", "repository must be owner/name");
  if (Object.hasOwn(actor, "run_id")) decimal(actor.run_id, "actor run_id");
  if (Object.hasOwn(actor, "run_attempt")) integer(actor.run_attempt, 1, 4096, "originating run attempt");
  if (Object.hasOwn(actor, "run_id") !== Object.hasOwn(actor, "run_attempt")) throw queueError("actor_unauthorized", "run binding requires run ID and attempt");
  if (actor.role === "worker" && (!actor.workflow || !actor.run_id || !actor.dispatch_id)) throw queueError("actor_unauthorized", "worker must have workflow, run, and assignment provenance");
  if (actor.role === "worker" && actor.run_attempt !== 1) throw queueError("actor_unauthorized", "only native attempt 1 can have worker Claim authority");
  return actor;
}

function validateRequestRole(actor, kind) {
  validateActor(actor);
  if (!ROLE_KINDS[actor.role].includes(kind)) throw queueError("actor_unauthorized", `${actor.role} cannot publish ${kind}`);
}

// Only trusted credential/job adapters construct this context, never intent data.
function normalizeTrustedContext(context) {
  if (!context || context.authenticated !== true || !Array.isArray(context.roles) || !context.roles.includes(context.role)) {
    throw queueError("actor_unauthorized", "authenticated caller credentials and approved role are required");
  }
  const actor = { role: context.role, principal: context.principal, repository: context.repository };
  for (const field of [...ACTOR_FIELDS, ...ACTOR_OPTIONAL]) if (Object.hasOwn(context, field)) actor[field] = context[field];
  validateActor(actor);
  return { ...actor, ...(context.ref ? { ref: identity(context.ref, "ref") } : {}), ...(context.event ? { event: identity(context.event, "event") } : {}) };
}

/** @returns {QueueActor} */
function actorFromContext(context) {
  const normalized = normalizeTrustedContext(context);
  /** @type {QueueActor} */
  const actor = { role: normalized.role, principal: normalized.principal, repository: normalized.repository };
  for (const field of ACTOR_OPTIONAL) if (Object.hasOwn(normalized, field)) actor[field] = normalized[field];
  return actor;
}

function validateTrustedContext(context, actor) {
  if (canonical(actorFromContext(context)) !== canonical(validateActor(actor))) throw queueError("actor_unauthorized", "request origin differs from authenticated caller");
  return normalizeTrustedContext(context);
}

function validateProfile(profile) {
  closed(profile, ["workflow", "ref", "principal", "trust_domain", "credential_scope", "effect_scope", "max_claims", "share_keys"], [], "worker profile");
  decimal(profile.principal, "worker principal", "policy_invalid");
  for (const field of ["workflow", "ref", "trust_domain", "credential_scope", "effect_scope"]) identity(profile[field], field);
  if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(profile.ref) || !profile.workflow.startsWith(".github/workflows/") || !profile.workflow.endsWith(".lock.yml") || profile.workflow.includes(".."))
    throw queueError("policy_invalid", "profiles require immutable revisions and approved workflow paths");
  integer(profile.max_claims, 1, 16, "profile max_claims");
  if (typeof profile.share_keys !== "boolean") throw queueError("policy_invalid", "share_keys must be boolean");
}

function validatePolicy(policy) {
  closed(policy, ["mode", "class_weights", "accounting_weights", "producers", "pools", "limits"], ["projectors"], "policy");
  if (Object.hasOwn(policy, "projectors")) {
    if (!Array.isArray(policy.projectors) || !policy.projectors.length || policy.projectors.length > 256) throw queueError("policy_invalid", "projectors require 1..256 installed rules");
    for (const rule of policy.projectors) {
      closed(rule, ["principal", "workflow", "ref", "pools", "repositories"], [], "projector rule");
      decimal(rule.principal, "projector principal", "policy_invalid");
      identity(rule.workflow, "projector workflow");
      if (!rule.workflow.startsWith(".github/workflows/") || !rule.workflow.endsWith(".lock.yml") || rule.workflow.includes("..") || !/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(rule.ref))
        throw queueError("policy_invalid", "projectors require approved immutable workflow revisions");
      for (const field of ["pools", "repositories"]) {
        if (!Array.isArray(rule[field]) || !rule[field].length || rule[field].length > 256) throw queueError("policy_invalid", "projectors require bounded explicit targets");
        for (const value of rule[field]) identity(value, field);
      }
      if (rule.pools.some(pool => !Object.hasOwn(policy.pools, pool)) || rule.repositories.some(repository => !/^[^/\s]+\/[^/\s]+$/.test(repository))) throw queueError("policy_invalid", "invalid projector pool/repository");
    }
  }
  if (!["weighted-priority", "strict-priority"].includes(policy.mode)) throw queueError("policy_invalid", "mandatory scheduler mode is unsupported");
  if (!Array.isArray(policy.class_weights) || policy.class_weights.length !== 5) throw queueError("policy_invalid", "five priority class weights are required");
  for (const weight of policy.class_weights) integer(weight, 1, 1000, "class weight");
  if (!policy.accounting_weights || typeof policy.accounting_weights !== "object" || Array.isArray(policy.accounting_weights)) throw queueError("policy_invalid", "accounting weights must be an object");
  if (policy.accounting_weights[""] !== 1 || Object.keys(policy.accounting_weights).length > 1024) throw queueError("policy_invalid", "default key weight 1 and at most 1024 registered keys are required");
  for (const [key, weight] of Object.entries(policy.accounting_weights)) {
    identity(key, "fairness key", true, 128);
    integer(weight, 1, 1000, "accounting weight");
  }
  for (const field of ["pools", "producers"]) {
    if (!policy[field] || typeof policy[field] !== "object" || Array.isArray(policy[field]) || Object.keys(policy[field]).length > 1024) throw queueError("policy_invalid", `invalid ${field}`);
  }
  if (!Object.keys(policy.pools).length || Object.keys(policy.pools).length > 64) throw queueError("policy_invalid", "policy must approve 1..64 pools");
  for (const [name, pool] of Object.entries(policy.pools)) {
    identity(name, "pool");
    closed(pool, ["default_profile", "profiles", "logical_limit", "native_limit", "allowed_repositories", "max_observation_age_ms", "retry", "reconciliation"], ["per_account_limit"], "pool");
    identity(pool.default_profile, "default profile");
    if (!pool.profiles || typeof pool.profiles !== "object" || Array.isArray(pool.profiles) || !Object.hasOwn(pool.profiles, pool.default_profile) || Object.keys(pool.profiles).length > 256)
      throw queueError("policy_invalid", "default profile must be approved");
    for (const [profileName, profile] of Object.entries(pool.profiles)) {
      identity(profileName, "profile");
      validateProfile(profile);
      if (policy.limits?.operations < 2 * profile.max_claims + 1) throw queueError("policy_invalid", "operation budget cannot hold bounded worst-case assignment closure");
    }
    for (const field of ["logical_limit", "native_limit", "per_account_limit"]) if (Object.hasOwn(pool, field)) integer(pool[field], 1, 4096, field);
    integer(pool.max_observation_age_ms, 1, 3600000, "max observation age");
    if (!Array.isArray(pool.allowed_repositories) || pool.allowed_repositories.length > 256) throw queueError("policy_invalid", "invalid dependency allowlist");
    for (const repository of pool.allowed_repositories) identity(repository, "allowed repository");
    closed(pool.retry, ["max_attempts", "backoff_ms"], [], "retry policy");
    closed(pool.reconciliation, ["max_attempts", "deadline_ms"], [], "reconciliation policy");
    integer(pool.retry.max_attempts, 1, 16, "retry attempts");
    integer(pool.retry.backoff_ms, 1, 3600000, "retry backoff");
    integer(pool.reconciliation.max_attempts, 1, 64, "reconciliation attempts");
    integer(pool.reconciliation.deadline_ms, 1, 3600000, "reconciliation deadline");
  }
  for (const [principal, rule] of Object.entries(policy.producers)) {
    decimal(principal, "producer principal", "policy_invalid");
    closed(rule, ["pools", "priorities", "fairness_keys"], [], "producer rule");
    for (const name of ["pools", "priorities", "fairness_keys"]) if (!Array.isArray(rule[name]) || !rule[name].length || rule[name].length > 1024) throw queueError("policy_invalid", `invalid producer ${name}`);
    for (const pool of rule.pools) if (!Object.hasOwn(policy.pools, pool)) throw queueError("policy_invalid", "producer pool is not installed");
    for (const priority of rule.priorities) integer(priority, 1, 5, "producer priority");
    for (const key of rule.fairness_keys) if (!Object.hasOwn(policy.accounting_weights, key)) throw queueError("policy_invalid", "producer key is unregistered");
  }
  validateLimits(policy.limits);
  return policy;
}

function poolPolicy(state, pool) {
  if (!state.policy || !Object.hasOwn(state.policy.pools, pool)) throw queueError("policy_invalid", "pool is not installed");
  return state.policy.pools[pool];
}

function bindingAuthority(state, claimId, actor) {
  if (actor.role !== "worker") throw queueError("actor_unauthorized", "only an authenticated worker role has Claim authority");
  const claim = state.claims.get(claimId);
  const work = claim && state.works.get(claim.work_id);
  const dispatch = claim && state.dispatches.get(claim.dispatch_id);
  if (!claim || !work || !dispatch || !dispatch.run || dispatch.state !== "bound") throw queueError("claim_ineffective", "Claim has no authenticated native assignment binding");
  if (dispatch.policy_epoch !== state.policy_epoch) throw queueError("claim_ineffective", "Claim belongs to a retired Policy epoch");
  if (dispatch.released) throw queueError("claim_ineffective", "Claim native reservation has been released");
  const run = dispatch.run;
  if (
    actor.repository !== run.repository ||
    actor.workflow !== run.workflow ||
    actor.run_id !== run.run_id ||
    actor.run_attempt !== 1 ||
    actor.principal !== run.principal ||
    actor.dispatch_id !== dispatch.dispatch_id ||
    actor.claim_handle !== claim.handle
  )
    throw queueError("run_binding_conflict", "native worker context does not match immutable assignment");
  return { claim, work, dispatch };
}

function claimAuthority(state, claimId, actor, requireCompletion) {
  const authority = bindingAuthority(state, claimId, actor);
  const { claim, work } = authority;
  const expected = requireCompletion ? "completed" : "open";
  if (work.claim_id !== claimId || claim.state !== expected || work.state !== (requireCompletion ? "completed" : "claimed")) throw queueError("claim_ineffective", `frozen Claim must be ${expected}`);
  if (requireCompletion && work.barrier === "failed") throw queueError("claim_effects_unauthorized", "terminal DeliveryFailure cannot authorize effects again");
  return authority;
}

function workerContinuationAuthority(state, actor) {
  if (!actor.claim_handle) throw queueError("claim_scope_required", "worker queue-control intent requires immutable Claim scope");
  const dispatch = state.dispatches.get(actor.dispatch_id);
  const member = dispatch?.claims.find(member => member.handle === actor.claim_handle);
  if (!member) throw queueError("claim_scope_invalid", "worker control intent is outside original assignment");
  const authority = bindingAuthority(state, member.claim_id, actor);
  const { claim, work } = authority;
  if (work.claim_id !== claim.claim_id || claim.state !== "completed" || work.state !== "completed" || work.barrier !== "pending")
    throw queueError("claim_effects_unauthorized", "Claim-scoped queue continuation requires its original completed Work with pending delivery");
  return authority;
}

function validateSubmissionEntitlement(state, node, actor) {
  validateRequestRole(actor, "submit");
  if (actor.role === "worker") {
    const { work: parent } = workerContinuationAuthority(state, actor);
    if (node.pool !== parent.pool || node.priority !== parent.priority || node.fairness_key !== parent.fairness_key) throw queueError("child_entitlement", "children preserve trusted parent priority and accounting scope");
    return;
  }
  const rule = state.policy.producers[actor.principal];
  if (!rule || !rule.pools.includes(node.pool) || !rule.priorities.includes(node.priority) || !rule.fairness_keys.includes(node.fairness_key)) throw queueError("admission_unauthorized", "producer lacks immutable submission entitlement");
}

function freshClocks(policy) {
  return new Map();
}

function policyScales(policy) {
  return { classes: tickScale(policy.class_weights), keys: tickScale(Object.values(policy.accounting_weights)) };
}

function defaultPolicy({ repository, principal, workflow = ".github/workflows/worker.lock.yml", ref = "0".repeat(40) }) {
  return {
    mode: "weighted-priority",
    class_weights: [8, 4, 2, 1, 1],
    accounting_weights: { "": 1 },
    producers: { [principal]: { pools: ["default"], priorities: [1, 2, 3, 4, 5], fairness_keys: [""] } },
    pools: {
      default: {
        default_profile: "default",
        profiles: { default: { workflow, ref, principal, trust_domain: "default", credential_scope: "repository", effect_scope: repository, max_claims: 1, share_keys: false } },
        logical_limit: 16,
        native_limit: 16,
        allowed_repositories: [repository],
        max_observation_age_ms: 60000,
        retry: { max_attempts: 3, backoff_ms: 30000 },
        reconciliation: { max_attempts: 5, deadline_ms: 300000 },
      },
    },
    limits: { ...DEFAULT_LIMITS },
  };
}

module.exports = {
  actorFromContext,
  bindingAuthority,
  claimAuthority,
  decimal,
  defaultPolicy,
  freshClocks,
  normalizeTrustedContext,
  policyScales,
  poolPolicy,
  validateActor,
  validatePolicy,
  validateProfile,
  validateRequestRole,
  validateSubmissionEntitlement,
  validateTrustedContext,
  workerContinuationAuthority,
};
