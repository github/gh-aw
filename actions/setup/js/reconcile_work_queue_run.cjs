// @ts-check
"use strict";

const log = require("./work_queue_logging.cjs").createWorkQueueLogger("run");
const { posix } = require("node:path");
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

const RUNS_PER_PAGE = 100;
// GitHub returns at most 1,000 runs for a filtered listing; one that needs more is incomplete.
const MAX_RUN_PAGES = 10;
// An unbound Dispatch is judged only this long after the newest ledger commit that
// touched it (reservation or launch). GitHub ends a hosted dispatcher job within six
// hours, and the launch loop itself is bounded (one 30 s POST per Dispatch, at most
// 256 per commit), so by then the POST was sent or never will be, and a dispatched run
// has long been listed.
const UNBOUND_GRACE_MS = 6 * 60 * 60 * 1000;
// GitHub cancels a workflow run after 35 days, so an active worker run was created
// within this window even when a Checkpoint has compacted the reservation commit.
const RUN_LIFETIME_MS = 35 * 24 * 60 * 60 * 1000;
// Runner clocks stamp ledger commits; GitHub stamps run creation.
const CLOCK_SKEW_MS = 10 * 60 * 1000;

/**
 * @typedef {{githubClient: any, context: any, core?: any, branch?: string, policy?: import("./work_queue_policy.cjs").PolicyEnv | null, now: () => number}} RunOptions
 */

/** @param {Record<string, any>} options @returns {RunOptions & Record<string, any>} */
function configure(options) {
  return {
    ...options,
    context: options.context || global.context,
    githubClient: options.githubClient || global.github,
    core: options.core || global.core,
    policy: options.policy ?? readWorkQueuePolicyEnv(),
    now: options.now || Date.now,
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
 * Status of one worker run: "completed", "missing" (HTTP 404) or "active".
 * @param {RunOptions} options
 * @param {string} runId
 */
async function runStatus(options, runId) {
  let response;
  try {
    response = await options.githubClient.rest.actions.getWorkflowRun({ ...options.context.repo, run_id: Number(runId), headers: { "X-GitHub-Api-Version": API_VERSION } });
  } catch (error) {
    if (error && typeof error === "object" && "status" in error && error.status === 404) return "missing";
    throw error;
  }
  return response?.data?.status === "completed" ? "completed" : "active";
}

/**
 * Ledger times of a Dispatch: its reservation commit (undefined once a Checkpoint
 * compacted it) and the newest commit that touched it, where a Checkpoint stands in
 * for the compacted commits.
 * @param {any} state
 * @param {any} dispatch
 */
function dispatchTimes(state, dispatch) {
  let reservedAt;
  let touchedAt;
  for (const commit of state.transactions) {
    if (commit.id === dispatch.commit_id) reservedAt = commit.at;
    if (commit.request.kind === "checkpoint" || commit.operations.some(operation => operation.dispatch_id === dispatch.dispatch_id)) touchedAt = commit.at;
  }
  return { reservedAt, touchedAt };
}

/**
 * Every workflow_dispatch run of the worker created since `since` (any status) whose
 * title carries the dispatch ID, or null when the listing is incomplete. Without a
 * status filter a run never leaves the listing, so paging (newest first) can repeat
 * a run but never skip one.
 * @param {RunOptions} options
 * @param {any} dispatch
 * @param {number} since
 */
async function correlatedRuns(options, dispatch, since) {
  const created = `>=${new Date(Math.max(0, since)).toISOString().replace(/\.\d{3}Z$/, "Z")}`;
  const matches = [];
  for (let page = 1; page <= MAX_RUN_PAGES; page++) {
    const response = await options.githubClient.rest.actions.listWorkflowRuns({
      ...options.context.repo,
      workflow_id: posix.basename(dispatch.workflow),
      event: "workflow_dispatch",
      created,
      per_page: RUNS_PER_PAGE,
      page,
      headers: { "X-GitHub-Api-Version": API_VERSION },
    });
    const runs = response?.data?.workflow_runs;
    if (!Array.isArray(runs)) throw new Error("work_queue_run_listing_invalid");
    const total = response.data.total_count;
    if (typeof total === "number" && total > MAX_RUN_PAGES * RUNS_PER_PAGE) return null;
    for (const run of runs) if (typeof run?.display_title === "string" && run.display_title.includes(dispatch.dispatch_id)) matches.push(run);
    if (runs.length < RUNS_PER_PAGE) return matches;
  }
  return null;
}

/**
 * Release reason for a Dispatch whose worker is gone, or null to leave it open.
 * Bound or started: the bound or launched run completed (`run_terminal`) or is
 * missing (`run_missing`). Reserved: the launch outcome is unrecorded, so the
 * dispatcher may still be about to POST; it is judged only after UNBOUND_GRACE_MS
 * and only on a complete listing: a not-yet-completed run carrying the dispatch ID
 * leaves it open, otherwise `run_terminal` (a completed run) or `run_missing`.
 * The title is only a correlation hint, never proof of ownership.
 * @param {RunOptions} options
 * @param {any} state
 * @param {any} dispatch
 */
async function abandonedReason(options, state, dispatch) {
  const runId = dispatch.state === "bound" ? dispatch.run.run_id : dispatch.launch?.run_id;
  if (runId !== undefined) {
    const status = await runStatus(options, runId);
    return status === "completed" ? "run_terminal" : status === "missing" ? "run_missing" : null;
  }
  const { reservedAt, touchedAt } = dispatchTimes(state, dispatch);
  const now = options.now();
  if (touchedAt === undefined || now - touchedAt < UNBOUND_GRACE_MS) {
    log.debug("maintenance.dispatch.in_flight");
    return null;
  }
  const runs = await correlatedRuns(options, dispatch, reservedAt === undefined ? now - RUN_LIFETIME_MS : reservedAt - CLOCK_SKEW_MS);
  if (!runs) {
    log.diagnostic("maintenance.listing.incomplete", { pages: MAX_RUN_PAGES });
    options.core?.warning?.(`Work queue maintenance could not list every worker run for dispatch ${dispatch.dispatch_id}; it stays open.`);
    return null;
  }
  if (runs.some(run => run.status !== "completed")) return null;
  return runs.length ? "run_terminal" : "run_missing";
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
      reason = await abandonedReason(configured, latest.state, dispatch);
    } catch (error) {
      log.failure("maintenance.read.failed", error, true);
      configured.core?.warning?.(`Work queue maintenance could not read the worker run of dispatch ${dispatch.dispatch_id}; it stays open.`);
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

module.exports = { UNBOUND_GRACE_MS, main, maintenance };
