// @ts-check
"use strict";

const log = require("./work_queue_logging.cjs").createWorkQueueLogger("run");
const { canonical } = require("./work_queue_codec.cjs");
const { CHECKPOINT_AFTER_LINES } = require("./work_queue_limits.cjs");
const { actorFromContext, readWorkQueuePolicyEnv } = require("./work_queue_policy.cjs");
const { openClaim } = require("./work_queue_replay.cjs");
const { publishWorkQueueCheckpoint } = require("./work_queue_store.cjs");
const { loadQueue } = require("./work_queue_binding.cjs");
const { API_VERSION } = require("./work_queue_native.cjs");
const { releaseAssignment } = require("./work_queue_reconciler.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { resolveWorkQueueRuntime } = require("./aw_context.cjs");

/**
 * @typedef {{githubClient: any, context: any, core?: any, branch?: string, policy?: import("./work_queue_policy.cjs").PolicyEnv | null}} RunOptions
 */

/** @param {Record<string, any>} options @returns {RunOptions & Record<string, any>} */
function configure(options) {
  return {
    ...options,
    context: options.context || global.context,
    githubClient: options.githubClient || global.github,
    core: options.core || global.core,
    policy: options.policy ?? readWorkQueuePolicyEnv(),
  };
}

async function projectIssues(options) {
  try {
    if (options.policy)
      await require("./work_queue_issues.cjs").projectIssues({ githubClient: options.githubClient, context: options.context, core: options.core, policy: options.policy, ...(options.branch === undefined ? {} : { branch: options.branch }) });
  } catch (error) {
    log.failure("issues.project.failed", error, true);
    options.core?.warning?.("Work queue Issue projection failed; maintenance retries it.");
  }
}

/**
 * Worker conclusion job ("Terminate work queue worker"): release this run's Dispatch
 * when it still holds an open Claim and was launched as this run or is bound to it.
 * @param {Record<string, any>} [options]
 */
async function main(options = {}) {
  const configured = configure(options);
  const { context } = configured;
  if (context.eventName !== "workflow_dispatch" || context.payload?.inputs?.work_queue_assignment === undefined) return { status: "ignored" };
  const dispatchId = resolveWorkQueueRuntime(context.payload, { role: "worker" }).assignment?.dispatch_id;
  if (typeof dispatchId !== "string") return { status: "ignored" };
  if (isStagedMode(options) || isStagedMode(options.config)) return { status: "staged_preview", dispatch_id: dispatchId };
  const runId = String(context.runId);
  const owned = state => {
    const dispatch = state.dispatches.get(dispatchId);
    if (!dispatch || !openClaim(state, dispatch)) return false;
    // A reserved Dispatch names no launched run yet, so no worker run can own it.
    const ownRun = dispatch.state === "bound" ? dispatch.run : dispatch.launch;
    return !!ownRun && ownRun.run_id === runId;
  };
  const latest = await loadQueue({ ...configured, role: "reconciler" });
  if (!owned(latest.state)) {
    log.debug("run.terminate.settled");
    return { status: "settled", dispatch_id: dispatchId };
  }
  const result = await releaseAssignment(configured, dispatchId, "worker_terminated", owned);
  if (result.released) configured.core?.info?.(`Work queue released dispatch ${dispatchId}: worker_terminated`);
  await projectIssues(configured);
  return { status: result.released ? "released" : "settled", dispatch_id: dispatchId };
}

/**
 * The response of a GitHub read, or null when it answers HTTP 404.
 * @param {() => Promise<any>} read
 */
async function readOrMissing(read) {
  try {
    return await read();
  } catch (error) {
    if (error && typeof error === "object" && "status" in error && error.status === 404) return null;
    throw error;
  }
}

/**
 * Release reason for a Dispatch whose worker is gone, or null to leave it open. No clock is
 * read. Bound or started: the bound or launched run completed (`run_terminal`) or is missing
 * (`run_missing`). Reserved: the launch outcome is unrecorded, and only the dispatcher attempt
 * that reserved it can record one, so the Dispatch is judged by that attempt: still running
 * leaves it open; completed (`run_terminal`) or missing (`run_missing`) means no launch will
 * ever be recorded, and any run it POSTed can never bind the Dispatch.
 * @param {RunOptions} options
 * @param {any} dispatch
 */
async function abandonedReason(options, dispatch) {
  const headers = { "X-GitHub-Api-Version": API_VERSION };
  const runId = dispatch.state === "bound" ? dispatch.run.run_id : dispatch.launch?.run_id;
  const response =
    runId !== undefined
      ? await readOrMissing(() => options.githubClient.rest.actions.getWorkflowRun({ ...options.context.repo, run_id: Number(runId), headers }))
      : await readOrMissing(() => options.githubClient.rest.actions.getWorkflowRunAttempt({ ...options.context.repo, run_id: Number(dispatch.reserved_by.run_id), attempt_number: dispatch.reserved_by.run_attempt, headers }));
  if (!response) return "run_missing";
  return response.data?.status === "completed" ? "run_terminal" : null;
}

/**
 * agentics-maintenance: convert a v3 head, release Dispatches whose worker is gone,
 * checkpoint a long ledger and project Issues. Only conversion and checkpoint
 * failures fail the job; everything else warns.
 * @param {Record<string, any> & {projectIssues?: boolean}} [options]
 */
async function maintenance(options = {}) {
  const configured = configure(options);
  const latest = await loadQueue({ ...configured, role: "reconciler" });
  const released = [];
  for (const dispatch of latest.state.dispatches.values()) {
    if (!openClaim(latest.state, dispatch)) continue;
    // The judged run evidence; every compare-and-swap attempt re-checks it against the latest head.
    const observed = canonical({ state: dispatch.state, run: dispatch.run, launch: dispatch.launch ?? null });
    let reason;
    try {
      reason = await abandonedReason(configured, dispatch);
    } catch (error) {
      log.failure("maintenance.read.failed", error, true);
      configured.core?.warning?.(`Work queue maintenance could not read the judged run of dispatch ${dispatch.dispatch_id}; it stays open.`);
      continue;
    }
    if (!reason) continue;
    const unchanged = state => {
      const current = state.dispatches.get(dispatch.dispatch_id);
      if (!current || !openClaim(state, current)) return false;
      const stale = canonical({ state: current.state, run: current.run, launch: current.launch ?? null }) !== observed;
      if (stale) log.debug("maintenance.release.stale");
      return !stale;
    };
    try {
      const result = await releaseAssignment(configured, dispatch.dispatch_id, reason, unchanged);
      if (result.released) released.push({ dispatch_id: dispatch.dispatch_id, reason });
    } catch (error) {
      log.failure("maintenance.release.failed", error, true);
      configured.core?.warning?.(`Work queue maintenance could not release dispatch ${dispatch.dispatch_id}.`);
    }
  }
  const checkpoint = await publishWorkQueueCheckpoint({
    githubClient: configured.githubClient,
    owner: configured.context.repo.owner,
    repo: configured.context.repo.repo,
    ...(configured.branch === undefined ? {} : { branch: configured.branch }),
    actor: actorFromContext(configured.context, "reconciler"),
    minimumLines: CHECKPOINT_AFTER_LINES,
    core: configured.core,
  });
  if (options.projectIssues === true) await projectIssues(configured);
  log.diagnostic("maintenance.complete", { released: released.length, checkpointed: checkpoint.checkpointed });
  return { released, checkpointed: checkpoint.checkpointed };
}

module.exports = { main, maintenance };
