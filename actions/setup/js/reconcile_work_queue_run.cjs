// @ts-check
"use strict";

const log = require("./work_queue_logging.cjs").createWorkQueueLogger("run");
const { nativeId } = require("./work_queue_native.cjs");
const { loadQueue } = require("./work_queue_binding.cjs");
const { assignmentForDispatch } = require("./work_queue_replay.cjs");
const { reconcileDispatch } = require("./work_queue_reconciler.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

async function main(options = {}) {
  log.debug("run.reconcile.start");
  try {
    const context = options.context || global.context;
    const githubClient = options.githubClient || global.github;
    const coreApi = options.core || global.core;
    const run = context.payload?.workflow_run;
    if (context.eventName !== "workflow_run" || context.payload?.action !== "completed") {
      log.debug("run.reconcile.ignored", { unsupported_event: true });
      return { status: "ignored" };
    }
    if (context.payload.repository?.fork || run?.event !== "workflow_dispatch" || run.status !== "completed" || nativeId(run.run_attempt) !== "1") {
      log.debug("run.reconcile.ignored", { unsupported_run: true });
      return { status: "ignored" };
    }
    if (isStagedMode(options) || isStagedMode(options.config)) {
      log.debug("run.reconcile.staged");
      return { status: "staged_preview" };
    }
    const runId = nativeId(run.id);
    const configured = { ...options, context, githubClient, core: coreApi };
    const latest = await loadQueue(configured);
    const candidates = [...latest.projection.dispatches.values()].filter(dispatch => dispatch.run?.run_id === runId && dispatch.run.run_attempt === 1);
    log.debug("run.binding.checked", { candidates: candidates.length });
    if (!candidates.length) {
      log.debug("run.reconcile.unbound");
      return { status: "unbound" };
    }
    if (candidates.length !== 1) {
      log.debug("run.binding.ambiguous");
      throw new Error("work_queue_run_binding_ambiguous");
    }
    const assignment = assignmentForDispatch(latest.projection, candidates[0].dispatch_id);
    // The event selects a stored binding; the reconciler independently authenticates
    // the publisher and original native attempt before any ledger mutation.
    const result = await reconcileDispatch({ ...configured, assignment, requestCancellation: false });
    log.debug("run.reconcile.complete", { released: result.released, claims: assignment.claims.length });
    coreApi?.info(`Work queue terminal reservation: ${result.state}; delivery remains independent`);
    return { dispatch_id: assignment.dispatch_id, ...result };
  } catch (error) {
    log.failure("run.reconcile.failed", error);
    throw error;
  }
}

module.exports = { main };
