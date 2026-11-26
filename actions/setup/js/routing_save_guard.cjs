// Read-only evidence for the pre-run decision. The writer reconciles only lower run numbers;
// a decision must also respect results that finished before it, regardless of run number.

/** @typedef {{run_number: number, event: string, head_branch: string | null, status: string, run_attempt?: number, conclusion?: string | null, first_attempt_conclusion?: string | null}} WorkflowRun */

/**
 * GitHub lists pull_request_target runs under the PR branch even though their definition
 * comes from the default branch. Other PR events do not qualify.
 * @param {Pick<WorkflowRun, "event" | "head_branch">} run @param {string} defaultBranch
 */
function isEligibleRun(run, defaultBranch) {
  if (/^pull_request(?!_target)/.test(run.event || "")) return false;
  return run.event === "pull_request_target" || run.head_branch === defaultBranch;
}

/** @param {string | null | undefined} conclusion */
function isSavedConclusion(conclusion) {
  return conclusion === "success" || conclusion === "skipped" || conclusion === "neutral";
}

/**
 * Pass null only for a known new history, not a failed state read. Pages are newest first;
 * active runs may supply first-attempt evidence but cannot prove a missing page was read.
 * Either flag withholds a test. An incomplete listing also invalidates the turn and in-flight
 * count. Without a verified floor in existing history, a failure cannot be classified as
 * unsaved: only listingIncomplete is set. Otherwise a visible unsaved result takes precedence.
 * @param {{ state: Pick<import("./routing_history.cjs").HistoryState, "last_run" | "reconciled_through" | "reset_after_run" | "unresolved_runs"> | null, runNumber: number, defaultBranch: string, runs: WorkflowRun[], reachesStart: boolean, activeRuns?: WorkflowRun[] }} input
 */
function checkRoutingSaveCoverage({ state, runNumber, defaultBranch, runs, reachesStart, activeRuns = [] }) {
  if (state !== null && (state.reconciled_through == null || !Number.isSafeInteger(state.reconciled_through) || state.reconciled_through < 0)) {
    return { hasUnsaved: false, listingIncomplete: true };
  }
  const reset = state?.reset_after_run ?? 0;
  const floor = Math.max(state?.reconciled_through ?? 0, reset);
  const pending = new Set((state?.unresolved_runs ?? []).filter(n => n > reset && n !== runNumber));
  const listed = new Set(runs.map(run => run.run_number));
  const reachesFloor = reachesStart || (floor === 0 && !state?.last_run) || runs.some(run => run.run_number <= floor);
  const listingIncomplete = !reachesFloor || [...pending].some(n => !listed.has(n));
  // An overlapping page can show that a run finished after the previous page was fetched.
  const hasUnsaved = [...runs, ...activeRuns].some(run => {
    const n = run.run_number;
    if (n === runNumber || (n <= floor && !pending.has(n)) || !isEligibleRun(run, defaultBranch)) return false;
    if ((run.run_attempt ?? 1) > 1) return !isSavedConclusion(run.first_attempt_conclusion);
    return run.status === "completed" && !isSavedConclusion(run.conclusion);
  });
  return { hasUnsaved, listingIncomplete };
}

module.exports = { checkRoutingSaveCoverage, isEligibleRun, isSavedConclusion };
