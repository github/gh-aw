// @ts-check
"use strict";

const log = require("./work_queue_logging.cjs").createWorkQueueLogger("run");
const { ERR_VALIDATION } = require("./error_codes.cjs");
const { canonical, digest } = require("./work_queue_codec.cjs");
const { actorFromContext } = require("./work_queue_policy.cjs");
const { API_VERSION, nativeId, authenticatePublisher, fetchNativeRunAttempt, validateNativeRun } = require("./work_queue_native.cjs");
const { loadQueue, validateStoredAssignment, expectedWorkerRun } = require("./work_queue_binding.cjs");
const { dispatchPrincipal } = require("./work_queue_policy.cjs");
const { assignmentForDispatch } = require("./work_queue_replay.cjs");
const { lifecycleEvidence, releaseAssignment, reconcileQueue } = require("./work_queue_reconciler.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");
const { readQueueMetadata } = require("./work_queue_read_retry.cjs");
const { ERR_WORK_QUEUE_RECONCILIATION } = require("./error_codes.cjs");

const reconciliationError = message => {
  const error = new Error(`${ERR_VALIDATION}: ${message}`);
  return Object.assign(error, { code: message.split(";", 1)[0], category: ERR_WORK_QUEUE_RECONCILIATION });
};

async function quiescenceReceipt(options, runId) {
  const names = options.jobNames ?? JSON.parse(process.env.GH_AW_JOB_DISPLAY_NAMES || "null");
  const needs = options.jobResults ?? JSON.parse(process.env.GH_AW_JOB_RESULTS || "null");
  if (!names || !needs || typeof names.conclusion !== "string" || !Object.keys(needs).length) throw reconciliationError("work_queue_worker_job_context_invalid");
  const finalizerName = names.conclusion;
  const jobs = [];
  for (let page = 1; page <= 4; page++) {
    const response = await readQueueMetadata(
      () =>
        options.githubClient.rest.actions.listJobsForWorkflowRunAttempt({
          ...options.context.repo,
          run_id: runId,
          attempt_number: 1,
          per_page: 100,
          page,
          headers: { "X-GitHub-Api-Version": API_VERSION },
          request: { retries: 0, retryCount: 0, timeout: 15000 },
        }),
      log,
      "run.jobs.read",
      options.sleepFn
    );
    if (response.status !== 200 || !Array.isArray(response.data?.jobs) || response.data.jobs.length > 100) throw reconciliationError("work_queue_worker_jobs_unavailable");
    jobs.push(...response.data.jobs);
    if (response.data.jobs.length < 100) {
      if (response.data.total_count !== jobs.length) throw reconciliationError("work_queue_worker_jobs_incomplete");
      break;
    }
    if (page === 4) throw reconciliationError("work_queue_worker_jobs_limit");
  }
  const finalizers = jobs.filter(job => job.name === finalizerName);
  if (finalizers.length !== 1 || finalizers[0].status !== "in_progress") throw reconciliationError("work_queue_worker_finalizer_invalid");
  for (const [id, dependency] of Object.entries(needs)) {
    const name = names[id];
    if (typeof name !== "string" || name === finalizerName || !["success", "failure", "cancelled", "skipped"].includes(dependency?.result)) throw reconciliationError("work_queue_worker_job_context_invalid");
    const matches = jobs.filter(job => job.name === name || job.name?.startsWith(`${name} (`) || job.name?.startsWith(`${name} / `));
    if (!matches.length && dependency.result !== "skipped") throw reconciliationError("work_queue_worker_jobs_incomplete");
  }
  const ids = new Set();
  for (const job of jobs) {
    const id = nativeId(job.id);
    if (ids.has(id) || nativeId(job.run_id) !== runId || (job.run_attempt !== undefined && nativeId(job.run_attempt) !== "1")) throw reconciliationError("work_queue_worker_job_identity_invalid");
    ids.add(id);
    if (job !== finalizers[0] && (job.status !== "completed" || typeof job.conclusion !== "string" || !job.conclusion)) throw reconciliationError("work_queue_worker_jobs_active");
  }
  log.debug("run.jobs.quiescent", { jobs: jobs.length });
  return digest(jobs.map(job => ({ id: nativeId(job.id), status: job.status, conclusion: job.conclusion })).sort((a, b) => a.id.localeCompare(b.id)));
}

async function main(options = {}) {
  log.debug("run.reconcile.start");
  try {
    const context = options.context || global.context;
    const configured = { ...options, context, githubClient: options.githubClient || global.github, core: options.core || global.core };
    if (context.eventName !== "workflow_dispatch" || context.payload?.repository?.fork) {
      log.debug("run.reconcile.ignored", { unsupported_event: true });
      return { status: "ignored" };
    }
    if (context.job !== "conclusion" || nativeId(context.runAttempt ?? process.env.GITHUB_RUN_ATTEMPT ?? "1") !== "1") throw reconciliationError("work_queue_worker_finalizer_context_invalid");
    if (isStagedMode(options) || isStagedMode(options.config)) {
      log.debug("run.reconcile.staged");
      return { status: "staged_preview" };
    }
    const runId = nativeId(context.runId);
    const latest = await loadQueue(configured);
    const candidates = [...latest.projection.dispatches.values()].filter(dispatch => dispatch.run?.run_id === runId && dispatch.run.run_attempt === 1);
    log.debug("run.binding.checked", { candidates: candidates.length });
    if (!candidates.length) {
      log.debug("run.reconcile.unbound");
      return { status: "unbound" };
    }
    if (candidates.length !== 1) {
      log.debug("run.binding.ambiguous");
      throw reconciliationError("work_queue_run_binding_ambiguous");
    }
    const assignment = assignmentForDispatch(latest.projection, candidates[0].dispatch_id);
    const { dispatch, profile } = validateStoredAssignment(latest.projection, assignment, { allowReleased: true });
    if (dispatch.released) return { dispatch_id: assignment.dispatch_id, state: "released", released: true };
    const publisher = await authenticatePublisher({ ...configured, role: "reconciler" });
    const expected = expectedWorkerRun(assignment, profile, context, publisher.repository, dispatchPrincipal(dispatch));
    const proof = validateNativeRun(await fetchNativeRunAttempt(configured.githubClient, publisher.repository, runId, 1, { sleepFn: options.sleepFn }), { ...expected, run_id: runId });
    const receipt = await quiescenceReceipt(configured, runId);
    const evidence = lifecycleEvidence(assignment, profile, publisher.repository, "worker_termination", "github_api", options.now ?? Date.now(), { run_id: runId, run_attempt: 1, receipt, principal: proof.principal });
    const result = await releaseAssignment(configured, assignment, evidence);
    log.debug("run.reconcile.complete", { released: result.released, claims: assignment.claims.length });
    configured.core?.info("Work queue worker reservation released; delivery remains independent");
    return { dispatch_id: assignment.dispatch_id, ...result };
  } catch (error) {
    log.failure("run.reconcile.failed", error);
    throw error;
  }
}

async function maintenance(options = {}) {
  const configured = { ...options, context: options.context || global.context, githubClient: options.githubClient || global.github, core: options.core || global.core };
  const result = await reconcileQueue({ ...configured, requestCancellation: false, expireUnverified: true });
  const released = result.results.filter(entry => entry.released).length;
  const unresolved = result.results.filter(entry => ["launch_unresolved", "run_binding_conflict"].includes(entry.state) || Object.values(entry.delivery?.claims || {}).some(claim => claim.reason === "verification_unresolved")).length;
  log.diagnostic("maintenance.reconcile.complete", { reconciled: result.reconciled, released, unresolved });
  const pending = [];
  if (result.reconciled && options.projectIssues === true) {
    try {
      const { readIssuesConfig } = require("./cancel_work_queue_closed_issue.cjs");
      const issues = await readIssuesConfig(configured.githubClient, configured.context);
      if (issues) {
        const origin = await authenticatePublisher({ ...configured, role: "reconciler" });
        const latest = await loadQueue(configured);
        const requests = [...latest.projection.requests.entries()]
          .filter(([, receipt]) => canonical(receipt.actor) === canonical(actorFromContext(origin)) && receipt.operations.some(operation => ["WorkCancellation", "Result", "DeliveryFailure"].includes(operation.kind)))
          .map(([id]) => id);
        for (let offset = 0; offset < requests.length; offset += 16) {
          try {
            const projected = await require("./work_queue_issues.cjs").main({ ...configured, issues, trustedContext: origin, transitionRequestIds: requests.slice(offset, offset + 16) });
            pending.push(...(projected.pending || []));
          } catch (error) {
            log.failure("maintenance.projection.batch.failed", error.originalError || error, true);
            pending.push({ reason: "Issue projection batch failed; inspect safe API diagnostics and the exact terminal requests" });
          }
        }
      }
    } catch (error) {
      log.failure("maintenance.projection.failed", error.originalError || error, true);
      pending.push({ reason: "Issue projection setup failed; inspect safe API diagnostics and the exact terminal requests" });
    }
  }
  log.diagnostic("maintenance.projection.complete", { pending: pending.length });
  if (unresolved) throw reconciliationError("work_queue_maintenance_reconciliation_unresolved");
  if (pending.length && !options.deferIssueFailure) throw reconciliationError("work_queue_issue_synchronization_pending; queue transitions are committed; recover exact requests with work-queue sync-issues --request-id");
  return { ...result, issue_sync_pending: pending };
}

module.exports = { main, maintenance };
