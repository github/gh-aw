// @ts-check
"use strict";
const fs = require("fs");
const path = require("path");
const { serializeTransactionLog, openClaim } = require("./work_queue_replay.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");
const { MAX_SNAPSHOT_PARSE_BYTES, canonical, queueError } = require("./work_queue_codec.cjs");
const { resolveWorkQueueRuntime } = require("./aw_context.cjs");
const { loadQueue, publishRequest } = require("./work_queue_binding.cjs");
const { assignmentForDispatch } = require("./work_queue_scheduler.cjs");
const { LAUNCH_UNVERIFIED, readWorkflowRun, verifyLaunchedRun } = require("./work_queue_native.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

const SNAPSHOT_PATH = "/tmp/gh-aw/work-queue.snapshot.json";

/**
 * Bound operations for this run: none when the Dispatch has no open Claim or is
 * already bound to this run attempt (a rerun of the same attempt is a no-op).
 * @param {any} state
 * @param {string} dispatchId
 * @param {{run_id: string, run_attempt: number}} run
 */
function bindOperations(state, dispatchId, run) {
  const dispatch = state.dispatches.get(dispatchId);
  if (!dispatch || !openClaim(state, dispatch)) return [];
  if (dispatch.state === "bound" && dispatch.run.run_id === run.run_id && dispatch.run.run_attempt === run.run_attempt) return [];
  return [{ kind: "Bound", dispatch_id: dispatchId, run_id: run.run_id, run_attempt: run.run_attempt }];
}

/**
 * Dispatcher-authenticated launch provenance for this run: the Dispatch's recorded launch
 * names this run, and the run read from the Actions API is that workflow_dispatch run of the
 * Dispatch's workflow at the recorded revision, which this job is checked out at.
 * @param {any} githubClient
 * @param {any} context
 * @param {any} dispatch
 */
async function verifyRunProvenance(githubClient, context, dispatch) {
  const runId = String(context.runId);
  if (dispatch.launch?.run_id !== runId) throw queueError(LAUNCH_UNVERIFIED, "this run is not the run the dispatcher recorded launching");
  const { owner, repo } = context.repo;
  verifyLaunchedRun(await readWorkflowRun(githubClient, { owner, repo, run_id: runId }), { owner, repo, run_id: runId, workflow: dispatch.workflow, revision: dispatch.launch.revision });
  const sha = context.sha ?? process.env.GITHUB_SHA;
  if (sha !== dispatch.launch.revision) throw queueError(LAUNCH_UNVERIFIED, "this job is not running the revision the dispatcher launched");
}

/**
 * Activation step `work_queue_snapshot`: a worker proves it is the run its dispatcher
 * launched, binds its Dispatch to this run and captures its open Claims; other roles capture
 * a read-only snapshot. A worker whose Claims are already closed writes `worker: null` and
 * `work_queue_assignment_settled=true`. Without launch provenance nothing is written.
 */
async function main(options = {}) {
  const githubClient = options.githubClient || global.github;
  const context = options.context || global.context;
  const coreApi = options.core || global.core;
  const outputPath = options.snapshotPath || process.env.GH_AW_WORK_QUEUE_SNAPSHOT || SNAPSHOT_PATH;
  const runtime = resolveWorkQueueRuntime(context.payload, { role: options.role, requireAssignment: options.requireAssignment });
  const target = { githubClient, context, core: coreApi, ...(options.branch === undefined ? {} : { branch: options.branch }) };
  let latest = await loadQueue({ ...target, role: runtime.role });
  /** @type {ReturnType<typeof assignmentForDispatch> | null} */
  let worker = null;
  let settled = false;
  if (runtime.role === "worker") {
    const dispatchId = runtime.assignment.dispatch_id;
    const dispatch = latest.state.dispatches.get(dispatchId);
    if (!dispatch) throw queueError("work_queue_assignment_unknown", `Dispatch ${dispatchId} is not in the work queue ledger`);
    // The dispatcher records the launch right after its POST; a run without that record fails at once.
    if (dispatch.state === "reserved") throw queueError(LAUNCH_UNVERIFIED, `Dispatch ${dispatchId} has no recorded launch; this run cannot bind it`);
    if (openClaim(latest.state, dispatch)) {
      await verifyRunProvenance(githubClient, context, dispatch);
      if (!isStagedMode(options.config)) {
        const run = { run_id: String(context.runId), run_attempt: Number(context.runAttempt ?? 1) };
        const published = await publishRequest({ ...target, role: "worker", kind: "bind", parameters: { dispatch_id: dispatchId }, operationsFor: state => bindOperations(state, dispatchId, run) });
        if (published.committed) latest = await loadQueue({ ...target, role: runtime.role });
      }
    }
    const assignment = assignmentForDispatch(latest.state, dispatchId);
    settled = assignment.claims.length === 0;
    worker = settled ? null : assignment;
    coreApi.info(settled ? `Work queue activation: assignment already settled for ${dispatchId}` : `Work queue activation: ${assignment.claims.length} Claim(s) of ${dispatchId} bound to run ${context.runId}`);
  } else {
    coreApi.info(`Work queue activation: ${runtime.role} snapshot at ${latest.sha ?? "an empty queue"}`);
  }
  const snapshot = {
    version: 4,
    sha: latest.sha,
    transactionLog: latest.transactions.length ? serializeTransactionLog(latest.transactions) : "",
    origin: actorFromContext(context, runtime.role),
    role: runtime.role,
    worker,
    ...(options.visibleWorkIds === undefined ? {} : { visible_work_ids: options.visibleWorkIds }),
  };
  const encoded = `${JSON.stringify(snapshot)}\n`;
  if (Buffer.byteLength(encoded, "utf8") > MAX_SNAPSHOT_PARSE_BYTES) throw new TypeError("work queue snapshot exceeds its bounded input limit");
  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  if (fs.existsSync(outputPath)) fs.chmodSync(outputPath, 0o644);
  fs.writeFileSync(outputPath, encoded, { mode: 0o444 });
  fs.chmodSync(outputPath, 0o444);
  coreApi.setOutput("work_queue_origin", canonical(snapshot.origin));
  coreApi.setOutput("work_queue_assignment_settled", settled ? "true" : "false");
  return snapshot;
}

module.exports = { SNAPSHOT_PATH, bindOperations, main };
