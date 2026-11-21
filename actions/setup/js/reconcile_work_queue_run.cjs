// @ts-check
"use strict";

const log = require("./work_queue_logging.cjs").createWorkQueueLogger("run");
const { posix } = require("node:path");
const { canonical } = require("./work_queue_codec.cjs");
const { CHECKPOINT_AFTER_LINES } = require("./work_queue_limits.cjs");
const { actorFromContext, readWorkQueuePolicyEnv } = require("./work_queue_policy.cjs");
const { openClaims } = require("./work_queue_replay.cjs");
const { publishWorkQueueCheckpoint } = require("./work_queue_store.cjs");
const { loadQueue } = require("./work_queue_binding.cjs");
const { API_VERSION } = require("./work_queue_native.cjs");
const { releaseAssignment } = require("./work_queue_reconciler.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { resolveWorkQueueRuntime } = require("./aw_context.cjs");

// Run statuses of a launched worker that has not finished yet.
const ACTIVE_RUN_STATUSES = ["queued", "in_progress", "waiting", "pending", "requested"];
const RUN_PAGES = 2;

/**
 * @typedef {{githubClient: any, context: any, core?: any, branch?: string, policy?: import("./work_queue_policy.cjs").PolicyEnv | null}} RunOptions
 */

/** @param {Record<string, any>} options @returns {RunOptions & Record<string, any>} */
function configure(options) {
  return { ...options, context: options.context || global.context, githubClient: options.githubClient || global.github, core: options.core || global.core, policy: options.policy ?? readWorkQueuePolicyEnv() };
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
 * when it still holds an open Claim and is unbound or bound to this run.
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
    return !!dispatch && dispatch.state !== "closed" && openClaims(state, dispatch).length > 0 && (dispatch.state !== "bound" || dispatch.run.run_id === runId);
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
 * Titles of the worker workflow's not-yet-finished workflow_dispatch runs.
 * @param {RunOptions} options
 * @param {string} workflow
 */
async function activeRunTitles(options, workflow) {
  const titles = [];
  for (const status of ACTIVE_RUN_STATUSES) {
    for (let page = 1; page <= RUN_PAGES; page++) {
      const response = await options.githubClient.rest.actions.listWorkflowRuns({
        ...options.context.repo,
        workflow_id: posix.basename(workflow),
        event: "workflow_dispatch",
        status,
        per_page: 100,
        page,
        headers: { "X-GitHub-Api-Version": API_VERSION },
      });
      const runs = response?.data?.workflow_runs;
      if (!Array.isArray(runs)) throw new Error("work_queue_run_listing_invalid");
      for (const run of runs) if (typeof run?.display_title === "string") titles.push(run.display_title);
      if (runs.length < 100) break;
    }
  }
  return titles;
}

/**
 * Release reason for a Dispatch whose worker is gone, or null to leave it.
 * Bound: the bound run completed (`run_terminal`) or is missing (`run_missing`).
 * Unbound: no active worker run carries the dispatch ID in its title (`run_missing`);
 * the title is only a correlation hint, never proof of ownership.
 * @param {RunOptions} options
 * @param {any} dispatch
 * @param {Map<string, Promise<string[]>>} listings
 */
async function abandonedReason(options, dispatch, listings) {
  if (dispatch.state === "bound") {
    let response;
    try {
      response = await options.githubClient.rest.actions.getWorkflowRun({ ...options.context.repo, run_id: Number(dispatch.run.run_id), headers: { "X-GitHub-Api-Version": API_VERSION } });
    } catch (error) {
      if (error && typeof error === "object" && "status" in error && error.status === 404) return "run_missing";
      throw error;
    }
    return response?.data?.status === "completed" ? "run_terminal" : null;
  }
  if (!listings.has(dispatch.workflow)) listings.set(dispatch.workflow, activeRunTitles(options, dispatch.workflow));
  // prettier-ignore
  const titles = await /** @type {Promise<string[]>} */ (listings.get(dispatch.workflow));
  return titles.some(title => title.includes(dispatch.dispatch_id)) ? null : "run_missing";
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
  const listings = new Map();
  for (const dispatch of latest.state.dispatches.values()) {
    if (dispatch.state === "closed" || !openClaims(latest.state, dispatch).length) continue;
    let reason;
    try {
      reason = await abandonedReason(configured, dispatch, listings);
    } catch (error) {
      log.failure("maintenance.read.failed", error, true);
      configured.core?.warning?.(`Work queue maintenance could not read the worker run of dispatch ${dispatch.dispatch_id}; it stays open.`);
      continue;
    }
    if (!reason) continue;
    const unchanged = state => {
      const current = state.dispatches.get(dispatch.dispatch_id);
      return !!current && current.state === dispatch.state && canonical(current.run) === canonical(dispatch.run);
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

module.exports = { ACTIVE_RUN_STATUSES, main, maintenance };
