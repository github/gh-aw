// @ts-check
"use strict";

const { closed, identity, integer, parseStrictJSON, queueError } = require("./work_queue_codec.cjs");
const { MAX_PENDING_WORK } = require("./work_queue_limits.cjs");
const { readWorkQueuePolicyConfig } = require("./work_queue_policy_config.cjs");

const ROLES = ["administrator", "producer", "dispatcher", "worker", "reconciler", "projector", "observer"];
const REPOSITORY = /^[^/\s]+\/[^/\s]+$/;
const ACTOR_WORKFLOW = /^\.github\/workflows\/[^/]+\.ya?ml$/;
const PROFILE_WORKFLOW = /^\.github\/workflows\/[^/]+\.lock\.yml$/;
const WORKER_ID = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

/** @typedef {{role: string, repository: string, workflow?: string, run_id?: string, run_attempt?: number}} QueueActor */
/** @typedef {{concurrency: number, per_account_limit: number, max_attempts: number}} PolicyPool */
/** @typedef {{label: string, title_prefix: string, orphaned_after_days?: number}} PolicyIssues */
/** @typedef {{version: 4, pending_limit: number, issues: PolicyIssues | false, effect_scope: string, workers: string[], pools: Record<string, PolicyPool>}} PolicyEnv */

/**
 * @param {unknown} value
 * @param {string} name
 * @param {string} [code]
 * @returns {string}
 */
function decimal(value, name, code = "ledger_invalid") {
  if (typeof value !== "string" || !/^[1-9][0-9]*$/.test(value) || value.length > 256) throw queueError(code, `${name} must be a positive decimal identity string`);
  return value;
}

/** @returns {QueueActor} */
function validateActor(actor) {
  closed(actor, ["role", "repository"], ["workflow", "run_id", "run_attempt"], "actor", "request_invalid");
  if (!ROLES.includes(actor.role)) throw queueError("request_invalid", "unsupported actor role");
  identity(actor.repository, "actor repository");
  if (!REPOSITORY.test(actor.repository)) throw queueError("request_invalid", "actor repository must be owner/name");
  if (Object.hasOwn(actor, "workflow") && !ACTOR_WORKFLOW.test(identity(actor.workflow, "actor workflow"))) throw queueError("request_invalid", "actor workflow must be a repository workflow path");
  if (Object.hasOwn(actor, "run_id") !== Object.hasOwn(actor, "run_attempt")) throw queueError("request_invalid", "actor run requires run_id and run_attempt");
  if (Object.hasOwn(actor, "run_id")) {
    decimal(actor.run_id, "actor run ID", "request_invalid");
    integer(actor.run_attempt, 1, 4096, "actor run attempt");
  }
  if (actor.role === "worker" && (!Object.hasOwn(actor, "workflow") || !Object.hasOwn(actor, "run_id"))) throw queueError("request_invalid", "worker actor requires its workflow and run");
  return actor;
}

/**
 * Build the publishing actor from the job's own context. No API call proves it:
 * write access to the queue branch is the only authority v4 relies on.
 * @param {{repo: {owner: string, repo: string}, runId?: unknown, runAttempt?: unknown}} context
 * @param {string} role
 * @param {Record<string, string | undefined>} [environment]
 * @returns {QueueActor}
 */
function actorFromContext(context, role, environment = process.env) {
  if (!context?.repo?.owner || !context.repo.repo) throw queueError("request_invalid", "actor requires the repository context");
  const repository = `${context.repo.owner}/${context.repo.repo}`;
  /** @type {QueueActor} */
  const actor = { role, repository };
  const workflowRef = environment.GITHUB_WORKFLOW_REF;
  const prefix = `${repository}/`;
  if (typeof workflowRef === "string" && workflowRef.slice(0, prefix.length).toLowerCase() === prefix.toLowerCase()) actor.workflow = workflowRef.slice(prefix.length).split("@")[0];
  const runId = context.runId ?? environment.GITHUB_RUN_ID;
  if (runId !== undefined && runId !== null && runId !== "") {
    actor.run_id = String(runId);
    actor.run_attempt = Number(context.runAttempt ?? environment.GITHUB_RUN_ATTEMPT ?? 1);
  }
  return validateActor(actor);
}

function policyInteger(value, min, max, name) {
  if (!Number.isSafeInteger(value) || value < min || value > max) throw queueError("policy_invalid", `${name} must be an integer in ${min}..${max}`);
  return value;
}

/**
 * The lock file of an approved worker ID.
 * @param {string} id
 * @returns {string}
 */
function workerWorkflow(id) {
  return ".github/workflows/" + id + ".lock.yml";
}

/**
 * Validate the compiled v4 policy environment (GH_AW_WORK_QUEUE_POLICY). Every pool
 * routes to every approved worker: worker `<id>` runs `.github/workflows/<id>.lock.yml`
 * and its effects stay within `effect_scope`.
 * @param {unknown} value
 * @returns {PolicyEnv}
 */
function validatePolicyEnv(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw queueError("policy_invalid", "work queue policy must be an object");
  // prettier-ignore
  if (/** @type {{version?: unknown}} */ (value).version !== 4) throw queueError("unsupported_protocol", "work queue policy environment v4 required");
  closed(value, ["version", "pending_limit", "issues", "effect_scope", "workers", "pools"], [], "work queue policy", "policy_invalid");
  // prettier-ignore
  const policy = /** @type {PolicyEnv} */ (value);
  policyInteger(policy.pending_limit, 1, MAX_PENDING_WORK, "pending_limit");
  if (policy.issues !== false) {
    closed(policy.issues, ["label", "title_prefix"], ["orphaned_after_days"], "work queue issues", "policy_invalid");
    identity(policy.issues.label, "Issue label", false, 64);
    identity(policy.issues.title_prefix, "Issue title prefix", false, 128);
    if (Object.hasOwn(policy.issues, "orphaned_after_days")) policyInteger(policy.issues.orphaned_after_days, 1, 3650, "orphaned_after_days");
  }
  if (!REPOSITORY.test(identity(policy.effect_scope, "effect scope"))) throw queueError("policy_invalid", "effect_scope must be owner/name");
  const workers = policy.workers;
  if (!Array.isArray(workers) || !workers.length || workers.length > 256) throw queueError("policy_invalid", "policy must approve 1..256 workers");
  workers.forEach((worker, index) => {
    if (!WORKER_ID.test(identity(worker, "worker")) || worker.includes("..")) throw queueError("policy_invalid", `invalid worker ${worker}`);
    identity(workerWorkflow(worker), "worker workflow");
    if (index > 0 && workers[index - 1] >= worker) throw queueError("policy_invalid", "workers must be sorted and unique");
  });
  if (!policy.pools || typeof policy.pools !== "object" || Array.isArray(policy.pools)) throw queueError("policy_invalid", "pools must be an object");
  const pools = Object.keys(policy.pools);
  if (!pools.length || pools.length > 64) throw queueError("policy_invalid", "policy must define 1..64 pools");
  for (const name of pools) {
    identity(name, "pool");
    const pool = policy.pools[name];
    closed(pool, ["concurrency", "per_account_limit", "max_attempts"], [], `pool ${name}`, "policy_invalid");
    policyInteger(pool.concurrency, 1, 4096, `${name}.concurrency`);
    policyInteger(pool.per_account_limit, 0, 4096, `${name}.per_account_limit`);
    policyInteger(pool.max_attempts, 1, 16, `${name}.max_attempts`);
  }
  return policy;
}

/**
 * Read and validate GH_AW_WORK_QUEUE_POLICY (including chunked transport).
 * @param {Record<string, unknown>} [environment]
 * @returns {PolicyEnv | null}
 */
function readWorkQueuePolicyEnv(environment = process.env) {
  const raw = readWorkQueuePolicyConfig(environment);
  if (!raw) return null;
  return validatePolicyEnv(parseStrictJSON(raw));
}

/**
 * @param {PolicyEnv} policy
 * @param {string} name
 * @returns {PolicyPool}
 */
function poolEnv(policy, name) {
  if (!policy || !Object.hasOwn(policy.pools, name)) throw queueError("pool_invalid", `pool ${name} is not configured`);
  return policy.pools[name];
}

/**
 * Whether the compiled policy still routes pool `pool` to worker `id`.
 * @param {PolicyEnv | null | undefined} policy
 * @param {string} pool
 * @param {string} id
 * @returns {boolean}
 */
function policyAllowsWorker(policy, pool, id) {
  return !!policy && Object.hasOwn(policy.pools, pool) && policy.workers.includes(id);
}

module.exports = { PROFILE_WORKFLOW, ROLES, actorFromContext, decimal, policyAllowsWorker, poolEnv, readWorkQueuePolicyEnv, validateActor, validatePolicyEnv, workerWorkflow };
