// @ts-check
"use strict";
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("reconciler");

const { publishRequest, releaseOperations } = require("./work_queue_binding.cjs");

/**
 * Release a non-closed Dispatch as the reconciler: replay cancels every open Claim
 * with `reason`; Work whose attempts are exhausted is cancelled in the same commit.
 * @param {{githubClient: any, context: any, core?: any, branch?: string, policy?: import("./work_queue_policy.cjs").PolicyEnv | null}} options
 * @param {string} dispatchId
 * @param {string} reason
 * @param {(state: any) => boolean} [stillApplies] re-checked on every compare-and-swap retry
 * @returns {Promise<{dispatch_id: string, released: boolean, commit_id?: string}>}
 */
async function releaseAssignment(options, dispatchId, reason, stillApplies = () => true) {
  const published = await publishRequest({
    githubClient: options.githubClient,
    context: options.context,
    core: options.core,
    ...(options.branch === undefined ? {} : { branch: options.branch }),
    role: "reconciler",
    kind: "release",
    parameters: { dispatch_id: dispatchId, reason },
    operationsFor: state => (stillApplies(state) ? releaseOperations(state, dispatchId, reason, options.policy) : []),
  });
  log.debug("release.complete", { released: published.committed });
  return { dispatch_id: dispatchId, released: published.committed, ...(published.commit ? { commit_id: published.commit.id } : {}) };
}

module.exports = { releaseAssignment };
