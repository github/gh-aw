// @ts-check
"use strict";

const { canonical, digest, integer } = require("./work_queue_codec.cjs");
const { assignmentForDispatch } = require("./work_queue_replay.cjs");
const { loadQueue, publishOperations, validateStoredAssignment, expectedWorkerRun, bindingForRun } = require("./work_queue_binding.cjs");
const { API_VERSION, nativeId, hasDispatchToken, authenticatePublisher, fetchNativeRunAttempt, validateNativeRun } = require("./work_queue_native.cjs");
const { isStagedMode } = require("./safe_output_helpers.cjs");

function lifecycleEvidence(assignment, profile, repository, kind, source, checkedAt, details = {}) {
  return { kind, source, repository, workflow: profile.workflow, ref: profile.ref, principal: profile.principal, checked_at: checkedAt, ...details };
}

function cancellationOperations(state, assignment, at, reason) {
  const retry = state.policy.pools[assignment.pool].retry;
  const publicationHeadroom = Math.max(1000, Math.min(300000, state.policy.pools[assignment.pool].reconciliation.deadline_ms));
  const operations = [];
  for (const member of assignment.claims) {
    const claim = state.claims.get(member.claim_id);
    const work = state.works.get(member.work_id);
    if (claim?.state !== "open") continue;
    operations.push({ kind: "ClaimCancellation", work_id: member.work_id, claim_id: member.claim_id, reason, retry_not_before: at + retry.backoff_ms + publicationHeadroom });
    if (work.attempts >= retry.max_attempts) operations.push({ kind: "WorkCancellation", work_id: member.work_id, reason: "attempts_exhausted" });
  }
  return operations;
}

async function releaseAssignment(options, assignment, evidence) {
  if (isStagedMode(options) || isStagedMode(options.config)) return { state: "staged_preview", released: false };
  const latest = await loadQueue(options);
  const validated = validateStoredAssignment(latest.projection, assignment, { allowReleased: true });
  if (validated.dispatch.released) return { state: "released", released: true };
  const trustedContext = await authenticatePublisher({ ...options, role: "reconciler" });
  const operations = [
    ...cancellationOperations(latest.projection, assignment, evidence.checked_at, evidence.kind === "terminal_run" ? "native_run_terminal" : "definitive_nonlaunch"),
    { kind: "Release", dispatch_id: assignment.dispatch_id, evidence },
  ];
  try {
    await publishOperations(options, trustedContext, ["release", assignment.dispatch_id, digest(evidence)], "release", operations);
  } catch (error) {
    const refreshed = await loadQueue(options);
    if (!refreshed.projection.dispatches.get(assignment.dispatch_id)?.released) throw error;
  }
  const final = await loadQueue(options);
  if (!final.projection.dispatches.get(assignment.dispatch_id)?.released) throw new Error("work_queue_release_not_durable");
  return { state: "released", released: true };
}

async function discoverRuns(options, expected) {
  const [owner, repo] = expected.repository.split("/");
  const candidates = new Map();
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
    if (response.status !== undefined && response.status !== 200) throw new Error("native_discovery_unavailable");
    const runs = response.data?.workflow_runs;
    if (!Array.isArray(runs)) throw new Error("native_discovery_invalid");
    for (const run of runs) {
      if (!hasDispatchToken(run?.display_title, expected.dispatch_id)) continue;
      let runId;
      try {
        runId = nativeId(run.id);
      } catch {
        return { candidates: [...candidates.values()], conflict: true };
      }
      if (candidates.has(runId)) continue;
      const original = run.run_attempt === 1 || run.run_attempt === "1" ? run : await fetchNativeRunAttempt(options.githubClient, expected.repository, runId);
      try {
        const proof = validateNativeRun(original, { ...expected, run_id: runId });
        candidates.set(proof.run_id, proof);
      } catch {
        return { candidates: [...candidates.values()], conflict: true };
      }
      if (candidates.size > 1) return { candidates: [...candidates.values()], conflict: true };
    }
    if (runs.length < 50) break;
    if (page === 2) conflict = true;
  }
  return { candidates: [...candidates.values()], conflict };
}

function returnedRunHint(state, dispatchId) {
  for (const commit of [...state.transactions].reverse()) {
    const operation = [...commit.operations].reverse().find(operation => operation.kind === "Dispatch" && operation.dispatch_id === dispatchId && operation.evidence?.run_id);
    if (operation) return operation.evidence.run_id;
  }
  return null;
}

async function reconcileDispatch(options) {
  if (isStagedMode(options) || isStagedMode(options.config)) return { state: "staged_preview", released: false };
  const initial = await loadQueue(options);
  const { assignment, dispatch, profile } = validateStoredAssignment(initial.projection, options.assignment, { allowReleased: true });
  if (dispatch.released) return { state: "released", released: true };
  const repository = `${options.context.repo.owner}/${options.context.repo.repo}`;
  const expected = expectedWorkerRun(assignment, profile, options.context);
  if (dispatch.state === "reserved") return { state: "reserved", released: false };
  const trustedContext = await authenticatePublisher({ ...options, role: "reconciler" });
  if (dispatch.run) {
    const proof = validateNativeRun(await fetchNativeRunAttempt(options.githubClient, repository, dispatch.run.run_id), { ...expected, run_id: dispatch.run.run_id });
    if (!proof.terminal) {
      if (options.requestCancellation === true) {
        const [owner, repo] = repository.split("/");
        await options.githubClient.rest.actions.cancelWorkflowRun({ owner, repo, run_id: proof.run_id, headers: { "X-GitHub-Api-Version": API_VERSION }, request: { retries: 0, timeout: 15000 } });
      }
      return { state: options.requestCancellation ? "cancellation_requested" : "bound", run_id: proof.run_id, released: false };
    }
    const evidence = lifecycleEvidence(assignment, profile, repository, "terminal_run", "github_api", options.now ?? Date.now(), { run_id: proof.run_id, run_attempt: 1, status: "completed", conclusion: proof.conclusion });
    return releaseAssignment(options, assignment, evidence);
  }
  const policy = initial.projection.policy.pools[assignment.pool].reconciliation;
  const started = Date.now();
  let attempts = 0;
  let hint = options.returnedRunId || returnedRunHint(initial.projection, assignment.dispatch_id);
  let conflict = false;
  for (; attempts < policy.max_attempts; attempts++) {
    try {
      const current = await loadQueue(options);
      const observed = current.projection.dispatches.get(assignment.dispatch_id);
      if (observed?.released || observed?.run) return reconcileDispatch({ ...options, assignment });
      let candidates = [];
      if (hint) {
        const proof = validateNativeRun(await fetchNativeRunAttempt(options.githubClient, repository, hint), { ...expected, run_id: hint });
        candidates.push(proof);
      }
      const discovered = await discoverRuns(options, expected);
      candidates = [...new Map([...candidates, ...discovered.candidates].map(candidate => [candidate.run_id, candidate])).values()];
      conflict ||= discovered.conflict;
      if (candidates.length > 1 || conflict) return { state: "run_binding_conflict", released: false };
      if (candidates.length === 1) {
        const proof = candidates[0];
        const binding = bindingForRun(proof, expected);
        const evidence = lifecycleEvidence(assignment, profile, repository, "reconciliation", "github_api", options.now ?? Date.now(), { run_id: proof.run_id, run_attempt: 1 });
        await publishOperations(options, trustedContext, ["bind", assignment.dispatch_id, proof.run_id], "dispatch", [{ kind: "Dispatch", dispatch_id: assignment.dispatch_id, state: "bound", run: binding, evidence }]);
        const latest = await loadQueue(options);
        const bound = latest.projection.dispatches.get(assignment.dispatch_id)?.run;
        if (!bound || canonical(bound) !== canonical(binding)) throw new Error("run_binding_conflict");
        return reconcileDispatch({ ...options, assignment, returnedRunId: undefined });
      }
    } catch (error) {
      if (error?.code === "run_binding_conflict" || error?.message === "run_binding_conflict") return { state: "run_binding_conflict", released: false };
      // Missing discovery, deadlines and API failures do not establish nonlaunch.
    }
    hint = null;
    if (Date.now() - started >= policy.deadline_ms) {
      attempts++;
      break;
    }
    if (attempts + 1 < policy.max_attempts) await (options.sleepFn || (delay => new Promise(resolve => setTimeout(resolve, delay))))(Math.min(1000, 50 * 2 ** attempts));
  }
  if (attempts >= policy.max_attempts) {
    const evidence = lifecycleEvidence(assignment, profile, repository, "reconciliation", "github_api", options.now ?? Date.now(), { attempts });
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
    } catch {
      // Retain the previous start/uncertain marker when reconciliation cannot write.
    }
  }
  return { state: "launch_unresolved", released: false, attempts, next_action: "Restore API access and reconcile authenticated worker or positive terminal/nonlaunch evidence." };
}

async function cancelBeforeLaunch(options) {
  const latest = await loadQueue(options);
  const { assignment, dispatch, profile } = validateStoredAssignment(latest.projection, options.assignment, { allowReleased: true });
  if (dispatch.released) return { state: "released", released: true };
  if (dispatch.state !== "reserved" || dispatch.sender || dispatch.run) throw new Error("work_queue_launch_may_have_started");
  const evidence = lifecycleEvidence(assignment, profile, `${options.context.repo.owner}/${options.context.repo.repo}`, "prelaunch", "trusted_publisher", options.now ?? Date.now());
  return releaseAssignment(options, assignment, evidence);
}

async function reconcileQueue(options) {
  if (isStagedMode(options) || isStagedMode(options.config)) return { version: 3, status: "staged_preview", reconciled: 0, results: [] };
  const limit = options.maxReconciliations ?? 16;
  integer(limit, 1, 16, "native reconciliation batch limit");
  const latest = await loadQueue(options);
  const candidates = [...latest.projection.dispatches.values()]
    .filter(dispatch => !dispatch.released && dispatch.state !== "reserved" && (options.pool === undefined || dispatch.pool === options.pool))
    .sort((left, right) => Number(!!right.run) - Number(!!left.run))
    .slice(0, limit);
  const results = [];
  for (const dispatch of candidates) {
    const assignment = assignmentForDispatch(latest.projection, dispatch.dispatch_id);
    try {
      if (typeof options.verifyEffects === "function") await require("./finish_work_queue_claim.cjs").finalizeWorkerResults({ ...options, assignment });
      results.push({ dispatch_id: dispatch.dispatch_id, ...(await reconcileDispatch({ ...options, assignment })) });
    } catch {
      results.push({ dispatch_id: dispatch.dispatch_id, state: "launch_unresolved", released: false });
    }
  }
  return { version: 3, reconciled: results.length, results };
}

module.exports = { lifecycleEvidence, cancellationOperations, releaseAssignment, discoverRuns, returnedRunHint, reconcileDispatch, reconcileQueue, cancelBeforeLaunch };
