// @ts-check
"use strict";
const { SAFE_OUTPUT_E007 } = require("./error_codes.cjs");
const log = require("./work_queue_logging.cjs").createWorkQueueLogger("reconciler");
const { observeQueueOperation } = require("./work_queue_telemetry.cjs");

const { canonical, digest, integer } = require("./work_queue_codec.cjs");
const { dispatchPrincipal } = require("./work_queue_policy.cjs");
const { assignmentForDispatch } = require("./work_queue_replay.cjs");
const { loadQueue, publishOperations, validateStoredAssignment, expectedWorkerRun, bindingForRun } = require("./work_queue_binding.cjs");
const { API_VERSION, nativeId, nativeAttempt, hasDispatchToken, authenticatePublisher, fetchNativeRunAttempt, createNativeRunAttemptReader, validateNativeRun, correlationRetryDelay } = require("./work_queue_native.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

function lifecycleEvidence(assignment, profile, repository, kind, source, checkedAt, details = {}) {
  return { kind, source, repository, workflow: profile.workflow, ref: profile.ref, principal: profile.principal, checked_at: checkedAt, ...details };
}

function cancellationOperations(state, assignment, at, reason) {
  const retry = state.policy.pools[assignment.pool].retry;
  const operations = [];
  for (const member of assignment.claims) {
    const claim = state.claims.get(member.claim_id);
    const work = state.works.get(member.work_id);
    if (claim?.state !== "open") continue;
    operations.push({ kind: "ClaimCancellation", work_id: member.work_id, claim_id: member.claim_id, reason, retry_not_before: at + retry.backoff_ms });
    if (work.attempts >= retry.max_attempts) operations.push({ kind: "WorkCancellation", work_id: member.work_id, reason: "attempts_exhausted" });
  }
  return operations;
}

async function releaseAssignment(options, assignment, evidence) {
  log.debug("release.start");
  if (isStagedMode(options) || isStagedMode(options.config)) return { state: "staged_preview", released: false };
  const latest = await loadQueue(options);
  const validated = validateStoredAssignment(latest.projection, assignment, { allowReleased: true });
  if (validated.dispatch.released) return { state: "released", released: true };
  const trustedContext = await authenticatePublisher({ ...options, role: "reconciler" });
  const operations = [
    ...cancellationOperations(latest.projection, assignment, evidence.checked_at, evidence.kind === "terminal_run" ? "native_run_terminal" : evidence.kind === "worker_termination" ? "worker_termination" : "definitive_nonlaunch"),
    { kind: "Release", dispatch_id: assignment.dispatch_id, evidence },
  ];
  try {
    await publishOperations(options, trustedContext, ["release", assignment.dispatch_id, digest(evidence)], "release", operations);
  } catch (error) {
    log.failure("release.publish.failed", error);
    const refreshed = await loadQueue(options);
    if (!refreshed.projection.dispatches.get(assignment.dispatch_id)?.released) throw error;
  }
  const final = await loadQueue(options);
  if (!final.projection.dispatches.get(assignment.dispatch_id)?.released) throw new Error("work_queue_release_not_durable");
  log.debug("release.persisted", { operations: operations.length });
  return { state: "released", released: true };
}

async function discoverRuns(options, expected, readAttempt = createNativeRunAttemptReader(options.githubClient, expected.repository, { retryMetadata: false })) {
  log.debug("discovery.start");
  const [owner, repo] = expected.repository.split("/");
  const candidates = new Map();
  const correlationPending = new Set();
  let conflict = false;
  for (let page = 1; page <= 2; page++) {
    const response = await options.githubClient.rest.actions.listWorkflowRuns({
      owner,
      repo,
      workflow_id: expected.workflow,
      event: "workflow_dispatch",
      head_sha: expected.ref,
      per_page: 50,
      page,
      headers: { "X-GitHub-Api-Version": API_VERSION },
      request: { retries: 0, timeout: 15000 },
    });
    if (response.status !== undefined && response.status !== 200) throw new Error(`${SAFE_OUTPUT_E007}: native_discovery_unavailable`);
    const runs = response.data?.workflow_runs;
    if (!Array.isArray(runs)) throw new Error("native_discovery_invalid");
    log.debug("discovery.page", { page, runs: runs.length });
    for (const run of runs) {
      if (!hasDispatchToken(run?.display_title, expected.dispatch_id)) continue;
      let runId;
      try {
        runId = nativeId(run.id);
      } catch (error) {
        log.failure("discovery.identity_invalid", error);
        return { candidates: [...candidates.values()], correlationPending: [...correlationPending], conflict: true };
      }
      if (candidates.has(runId) || correlationPending.has(runId)) continue;
      const original = await readAttempt(runId, 1);
      try {
        const proof = validateNativeRun(original, { ...expected, run_id: runId });
        candidates.set(proof.run_id, proof);
      } catch (error) {
        log.failure("discovery.proof_invalid", error);
        if (error?.message === "run_correlation_mismatch") correlationPending.add(runId);
        else return { candidates: [...candidates.values()], correlationPending: [...correlationPending], conflict: true };
      }
      if (candidates.size + correlationPending.size > 1) {
        log.debug("discovery.multiple_candidates", { candidates: candidates.size, correlation_pending: correlationPending.size });
        return { candidates: [...candidates.values()], correlationPending: [...correlationPending], conflict: true };
      }
    }
    if (runs.length < 50) break;
    if (page === 2) conflict = true;
  }
  log.debug("discovery.complete", { candidates: candidates.size, conflict });
  return { candidates: [...candidates.values()], correlationPending: [...correlationPending], conflict };
}

function returnedRunHint(state, dispatchId) {
  for (const commit of [...(state.historicalTransactions || []), ...state.transactions].reverse()) {
    const operation = [...commit.operations].reverse().find(operation => operation.kind === "Dispatch" && operation.dispatch_id === dispatchId && operation.evidence?.run_id);
    if (operation) return operation.evidence.run_id;
  }
  return null;
}

async function reconcileDispatch(options) {
  return observeQueueOperation("reconcile_dispatch", options, () => reconcileDispatchImpl(options));
}

async function reconcileDispatchImpl(options) {
  log.debug("dispatch.reconcile.start");
  if (isStagedMode(options) || isStagedMode(options.config)) return { state: "staged_preview", released: false };
  const initial = await loadQueue(options);
  const { assignment, dispatch, profile } = validateStoredAssignment(initial.projection, options.assignment, { allowReleased: true });
  log.debug("dispatch.reconcile.checked", { released: dispatch.released, reserved: dispatch.state === "reserved", bound: !!dispatch.run });
  if (dispatch.released) return { state: "released", released: true };
  if (dispatch.state === "reserved") {
    const origin = initial.projection.requests.get(dispatch.request_id)?.actor;
    if (!origin?.workflow || !origin.run_id || !origin.run_attempt) return { state: "reserved", released: false };
    const repository = `${options.context.repo.owner}/${options.context.repo.repo}`;
    if (origin.repository.toLowerCase() !== repository.toLowerCase()) throw new Error("work_queue_dispatch_origin_mismatch");
    const run = await fetchNativeRunAttempt(options.githubClient, repository, origin.run_id, origin.run_attempt, { sleepFn: options.sleepFn });
    if (
      nativeId(run.id) !== origin.run_id ||
      nativeAttempt(run.run_attempt) !== origin.run_attempt ||
      run.path?.split("@")[0] !== origin.workflow ||
      nativeId(run.actor?.id) !== origin.principal ||
      (options.context.payload?.repository?.id !== undefined && nativeId(run.repository.id) !== nativeId(options.context.payload.repository.id))
    )
      throw new Error("work_queue_dispatch_origin_mismatch");
    if (run.status !== "completed" || typeof run.conclusion !== "string" || !run.conclusion) return { state: "reserved", released: false };
    // The original publisher has ended; a fresh ledger check must still prove
    // no start marker won the race before trusted prelaunch cancellation.
    return cancelBeforeLaunch(options);
  }
  const trustedContext = await authenticatePublisher({ ...options, role: "reconciler" });
  const repository = trustedContext.repository;
  const expected = expectedWorkerRun(assignment, profile, options.context, repository, dispatchPrincipal(dispatch));
  if (dispatch.run) {
    const proof = validateNativeRun(await fetchNativeRunAttempt(options.githubClient, repository, dispatch.run.run_id, 1, { sleepFn: options.sleepFn }), { ...expected, run_id: dispatch.run.run_id });
    log.debug("dispatch.reconcile.native_checked", { terminal: proof.terminal });
    if (!proof.terminal) {
      if (options.requestCancellation === true) {
        const [owner, repo] = repository.split("/");
        await options.githubClient.rest.actions.cancelWorkflowRun({ owner, repo, run_id: proof.run_id, headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0, timeout: 15000 } });
        log.debug("dispatch.cancellation.requested");
      }
      return { state: options.requestCancellation ? "cancellation_requested" : "bound", run_id: proof.run_id, released: false };
    }
    const evidence = lifecycleEvidence(assignment, profile, repository, "terminal_run", "github_api", options.now ?? Date.now(), {
      run_id: proof.run_id,
      run_attempt: 1,
      status: "completed",
      conclusion: proof.conclusion,
      principal: proof.principal,
    });
    return releaseAssignment(options, assignment, evidence);
  }
  const policy = initial.projection.policy.pools[assignment.pool].reconciliation;
  const started = Date.now();
  const readAttempt = createNativeRunAttemptReader(options.githubClient, repository, { retryMetadata: false });
  let attempts = 0;
  let hint = options.returnedRunId || returnedRunHint(initial.projection, assignment.dispatch_id);
  let conflict = false;
  for (; attempts < policy.max_attempts; attempts++) {
    if (attempts > 0 && Date.now() - started >= policy.deadline_ms) break;
    log.debug("dispatch.reconcile.attempt", { attempt: attempts + 1, run_hint: !!hint });
    let correlationPending = false;
    try {
      const current = await loadQueue(options);
      const observed = current.projection.dispatches.get(assignment.dispatch_id);
      if (observed?.released || observed?.run) return reconcileDispatch({ ...options, assignment });
      let candidates = [];
      if (hint) {
        const proof = validateNativeRun(await readAttempt(hint, 1), { ...expected, run_id: hint });
        candidates.push(proof);
      }
      const discovered = await discoverRuns(options, expected, readAttempt);
      candidates = [...new Map([...candidates, ...discovered.candidates].map(candidate => [candidate.run_id, candidate])).values()];
      conflict ||= discovered.conflict;
      log.debug("dispatch.reconcile.candidates", { candidates: candidates.length, conflict });
      const runIds = new Set([...candidates.map(candidate => candidate.run_id), ...discovered.correlationPending]);
      if (runIds.size > 1 || conflict) return { state: "run_binding_conflict", released: false };
      correlationPending = discovered.correlationPending.length > 0;
      if (correlationPending) hint = discovered.correlationPending[0];
      if (candidates.length === 1 && !correlationPending) {
        const proof = candidates[0];
        const binding = bindingForRun(proof, expected);
        const evidence = lifecycleEvidence(assignment, profile, repository, "reconciliation", "github_api", options.now ?? Date.now(), { run_id: proof.run_id, run_attempt: 1, principal: proof.principal });
        await publishOperations(options, trustedContext, ["bind", assignment.dispatch_id, proof.run_id], "dispatch", [{ kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "bound", run: binding, evidence }]);
        const latest = await loadQueue(options);
        const bound = latest.projection.dispatches.get(assignment.dispatch_id)?.run;
        if (!bound || canonical(bound) !== canonical(binding)) throw new Error("run_binding_conflict");
        log.debug("dispatch.reconcile.binding.persisted");
        return reconcileDispatch({ ...options, assignment, returnedRunId: undefined });
      }
    } catch (error) {
      log.failure("dispatch.reconcile.attempt_failed", error);
      if (error?.code === "run_binding_conflict" || error?.message === "run_binding_conflict") return { state: "run_binding_conflict", released: false };
      correlationPending = error?.message === "run_correlation_mismatch";
      // Missing discovery, deadlines and API failures do not establish nonlaunch
      // or invalidate the exact run hint; retain it throughout this retry budget.
    }
    if (Date.now() - started >= policy.deadline_ms) {
      log.debug("dispatch.reconcile.deadline_reached", { attempts: attempts + 1 });
      attempts++;
      break;
    }
    if (attempts + 1 < policy.max_attempts) {
      const remaining = policy.deadline_ms - (Date.now() - started);
      const delay = correlationPending ? correlationRetryDelay(attempts + 1, remaining) : Math.min(1000, 50 * 2 ** attempts, remaining);
      await (options.sleepFn || (delay => new Promise(resolve => setTimeout(resolve, delay))))(delay);
    }
  }
  if (attempts >= policy.max_attempts) {
    const evidence = lifecycleEvidence(assignment, profile, repository, "reconciliation", "github_api", options.now ?? Date.now(), { attempts, principal: dispatchPrincipal(dispatch) });
    try {
      await publishOperations(
        options,
        trustedContext,
        ["unresolved", assignment.dispatch_id, digest(evidence)],
        "dispatch",
        [{ kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "unresolved", reason: "launch_unresolved", evidence }],
        state => {
          const current = state.dispatches.get(assignment.dispatch_id);
          if (!current || !["started", "uncertain"].includes(current.state) || current.run || current.released) throw new Error("work_queue_resolution_already_recorded");
        }
      );
    } catch (error) {
      log.failure("dispatch.reconcile.unresolved_marker_failed", error);
      // Retain the previous start/uncertain marker when reconciliation cannot write.
    }
  }
  log.debug("dispatch.reconcile.unresolved", { attempts });
  return { state: "launch_unresolved", released: false, attempts, next_action: "Restore API access and reconcile authenticated worker or positive terminal/nonlaunch evidence." };
}

async function cancelBeforeLaunch(options) {
  log.debug("dispatch.cancel_before_launch.start");
  const latest = await loadQueue(options);
  const { assignment, dispatch, profile } = validateStoredAssignment(latest.projection, options.assignment, { allowReleased: true });
  if (dispatch.released) return { state: "released", released: true };
  if (dispatch.state !== "reserved" || dispatch.sender || dispatch.run) throw new Error("work_queue_launch_may_have_started");
  const trustedContext = await authenticatePublisher({ ...options, role: "reconciler" });
  const evidence = lifecycleEvidence(assignment, profile, trustedContext.repository, "prelaunch", "trusted_publisher", options.now ?? Date.now(), { principal: profile.principal ?? trustedContext.principal });
  return releaseAssignment(options, assignment, evidence);
}

function reconciliationCandidates(projection, pool, limit, runNumber) {
  const candidates = [...projection.dispatches.values()]
    .filter(
      dispatch =>
        (pool === undefined || dispatch.pool === pool) &&
        (!dispatch.released ||
          dispatch.claims?.some(member => {
            const claim = projection.claims.get(member.claim_id);
            const work = projection.works.get(member.work_id);
            const ownsPendingBarrier = claim?.keep_open === true || work?.claim_id === member.claim_id;
            const barrier = claim?.keep_open ? claim.barrier : work?.barrier;
            return claim?.state === "completed" && ownsPendingBarrier && barrier === "pending";
          }))
    )
    .sort((left, right) => {
      const runPriority = Number(!!right.run) - Number(!!left.run);
      if (runPriority !== 0) return runPriority;
      return left.dispatch_id < right.dispatch_id ? -1 : left.dispatch_id > right.dispatch_id ? 1 : 0;
    });
  if (candidates.length <= limit) return candidates;
  const offset = Number.isSafeInteger(runNumber) && runNumber > 0 ? (runNumber - 1) % candidates.length : 0;
  return Array.from({ length: limit }, (_, index) => candidates[(offset + index) % candidates.length]);
}

function unresolvedDeliveryResults(assignment, projection) {
  return Object.fromEntries(
    assignment.claims.map(member => {
      const claim = projection.claims.get(member.claim_id);
      const work = projection.works.get(member.work_id);
      if (claim?.state === "cancelled") return [member.handle, { state: "cancelled", effects: "unknown" }];
      if (claim?.state === "completed") {
        const barrier = claim.keep_open ? claim.barrier : work?.barrier;
        const terminal = projection.terminalBarriers.get(claim.keep_open ? member.claim_id : member.work_id);
        if (barrier === "verified") return [member.handle, { state: "result", effects: terminal?.evidence.effects ?? "unknown" }];
        if (barrier === "failed") return [member.handle, { state: "delivery_failed", effects: terminal?.evidence.effects ?? work?.disposition ?? "unknown" }];
      }
      return [member.handle, { state: "pending", effects: "unknown", reason: "verification_unresolved" }];
    })
  );
}

async function reconcileQueue(options) {
  return observeQueueOperation("reconcile", options, () => reconcileQueueImpl(options));
}

async function reconcileQueueImpl(options) {
  log.debug("queue.reconcile.start");
  if (isStagedMode(options) || isStagedMode(options.config)) return { version: 3, status: "staged_preview", reconciled: 0, results: [] };
  const limit = options.maxReconciliations ?? 16;
  integer(limit, 1, 16, "native reconciliation batch limit");
  const latest = await loadQueue(options);
  const candidates = reconciliationCandidates(latest.projection, options.pool, limit, options.context?.runNumber);
  log.debug("queue.reconcile.selected", { candidates: candidates.length, limit });
  const results = [];
  for (const dispatch of candidates) {
    const assignment = assignmentForDispatch(latest.projection, dispatch.dispatch_id);
    let reservation;
    try {
      reservation = await reconcileDispatch({ ...options, assignment });
    } catch (error) {
      log.failure("queue.reconcile.dispatch_failed", error);
      results.push({ dispatch_id: dispatch.dispatch_id, state: "launch_unresolved", released: false });
      continue;
    }
    let delivery;
    if (dispatch.state !== "reserved" && dispatch.run && (typeof options.verifyEffects === "function" || options.expireUnverified === true)) {
      try {
        delivery = await require("./finish_work_queue_claim.cjs").finalizeWorkerResults({ ...options, assignment });
      } catch (error) {
        log.failure("queue.reconcile.delivery_failed", error);
        let projection = latest.projection;
        try {
          projection = (await loadQueue(options)).projection;
        } catch (refreshError) {
          log.failure("queue.reconcile.delivery_refresh_failed", refreshError);
        }
        delivery = {
          version: 3,
          dispatch_id: assignment.dispatch_id,
          claims: unresolvedDeliveryResults(assignment, projection),
        };
      }
    }
    results.push({ dispatch_id: dispatch.dispatch_id, ...reservation, ...(delivery ? { delivery } : {}) });
  }
  log.debug("queue.reconcile.complete", { reconciled: results.length });
  return { version: 3, reconciled: results.length, results };
}

module.exports = { lifecycleEvidence, cancellationOperations, releaseAssignment, discoverRuns, returnedRunHint, reconcileDispatch, reconcileQueue, reconciliationCandidates, cancelBeforeLaunch };
