// @ts-check
"use strict";

const { queueError } = require("./work_queue_codec.cjs");
const { actorFromContext, readWorkQueuePolicyEnv } = require("./work_queue_policy.cjs");
const { openClaim } = require("./work_queue_replay.cjs");
const { publishWorkQueueRequest, readWorkQueueLog } = require("./work_queue_store.cjs");

// Roles whose jobs may write the ledger convert a v3 head before reading it.
const CONVERTING_ROLES = ["dispatcher", "producer", "administrator", "reconciler"];
const DEFAULT_MAX_ATTEMPTS = 3;

/**
 * @typedef {{githubClient: any, context: {repo: {owner: string, repo: string}, runId?: unknown, runAttempt?: unknown}, role: string, branch?: string, core?: any, policy?: import("./work_queue_policy.cjs").PolicyEnv | null}} QueueJobOptions
 */

/**
 * Load the replayed ledger for a job. A v3 head is converted first when the job's
 * role may write the ledger; workers, observers and projectors get unsupported_protocol.
 * @param {QueueJobOptions} options
 */
async function loadQueue({ githubClient, context, role, branch = undefined, core: coreApi = undefined, policy = undefined }) {
  const target = { githubClient, owner: context.repo.owner, repo: context.repo.repo, ...(branch === undefined ? {} : { branch }), core: coreApi };
  let current = await readWorkQueueLog({ ...target, allowLegacy: true });
  if (current.protocol === 3) {
    if (!CONVERTING_ROLES.includes(role)) throw queueError("unsupported_protocol", "work queue protocol v4 required; run the v3 converter");
    const resolved = policy ?? readWorkQueuePolicyEnv();
    if (!resolved) throw queueError("policy_missing", "v3 conversion requires the compiled work queue policy");
    const { convertV3Ledger } = require("./work_queue_convert_v3.cjs");
    await convertV3Ledger({ ...target, actor: actorFromContext(context, "administrator"), policy: resolved });
    current = await readWorkQueueLog(target);
  }
  return current;
}

/**
 * Publish one request as this job's actor. `operationsFor(state, request)` is
 * re-planned on every compare-and-swap retry; returning [] commits nothing.
 * @param {QueueJobOptions & {kind: string, parameters: Record<string, any>, operationsFor: (state: any, request: {id: string, kind: string, parameters: any}) => Array<any> | Promise<Array<any>>}} options
 */
function publishRequest({ githubClient, context, role, branch = undefined, core: coreApi = undefined, kind, parameters, operationsFor }) {
  return publishWorkQueueRequest({
    githubClient,
    owner: context.repo.owner,
    repo: context.repo.repo,
    ...(branch === undefined ? {} : { branch }),
    actor: actorFromContext(context, role),
    request: { kind, parameters },
    operationsFor,
    core: coreApi,
  });
}

/** @param {import("./work_queue_policy.cjs").PolicyEnv | null | undefined} policy @param {string} pool */
function maxAttempts(policy, pool) {
  return policy?.pools?.[pool]?.max_attempts ?? DEFAULT_MAX_ATTEMPTS;
}

/**
 * WorkCancellation{attempts_exhausted} for a cancelled Claim whose Work has used its
 * last attempt (the cancellation itself increments attempts).
 * @returns {Array<{kind: "WorkCancellation", work_id: string, reason: "attempts_exhausted", claim_id: string}>}
 */
function exhaustedCancellation(state, claim, policy) {
  const work = state.works.get(claim.work_id);
  return work.attempts + 1 >= maxAttempts(policy, work.pool) ? [{ kind: "WorkCancellation", work_id: work.work_id, reason: "attempts_exhausted", claim_id: claim.claim_id }] : [];
}

/**
 * Operations releasing a non-closed Dispatch: Release (replay cancels its open Claim)
 * plus the attempts rule for that Work. [] when there is nothing to release.
 * @param {any} state
 * @param {string} dispatchId
 * @param {string} reason
 * @param {import("./work_queue_policy.cjs").PolicyEnv | null | undefined} policy
 */
function releaseOperations(state, dispatchId, reason, policy) {
  const dispatch = state.dispatches.get(dispatchId);
  const claim = dispatch && openClaim(state, dispatch);
  if (!claim) return [];
  return [{ kind: "Release", dispatch_id: dispatchId, reason }, ...exhaustedCancellation(state, claim, policy)];
}

module.exports = { CONVERTING_ROLES, exhaustedCancellation, loadQueue, maxAttempts, publishRequest, releaseOperations };
