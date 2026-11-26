// Read-only evidence for the pre-run decision. The writer reconciles only lower run numbers;
// a decision must also respect results that finished before it, regardless of run number.

/** @typedef {{run_number: number, event: string, head_branch: string | null, status: string, run_attempt?: number, conclusion?: string | null, first_attempt_conclusion?: string | null}} WorkflowRun */

/** @param {Pick<WorkflowRun, "event" | "head_branch">} run @param {string} defaultBranch */
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
 * Report both flags: a visible unsaved result takes precedence, but an incomplete listing
 * also invalidates the caller's turn and in-flight count.
 * @param {{ state: Pick<import("./routing_history.cjs").HistoryState, "last_run" | "reconciled_through" | "reset_after_run" | "unresolved_runs"> | null, runNumber: number, defaultBranch: string, runs: WorkflowRun[], reachesStart: boolean, activeRuns?: WorkflowRun[] }} input
 */
function checkRoutingSaveCoverage({ state, runNumber, defaultBranch, runs, reachesStart, activeRuns = [] }) {
  if (state !== null && (state.reconciled_through == null || !Number.isSafeInteger(state.reconciled_through) || state.reconciled_through < 0)) {
    return { hasUnsaved: false, listingIncomplete: true };
  }
  const reset = state?.reset_after_run ?? 0;
  const floor = Math.max(state?.reconciled_through ?? 0, reset);
  const pending = new Set((state?.unresolved_runs ?? []).filter(n => n > reset && n !== runNumber));
  /** @type {Map<number, WorkflowRun>} */
  const listed = new Map();
  for (const run of runs) {
    if (!listed.has(run.run_number)) listed.set(run.run_number, run);
  }
  const earliest = Math.min(floor, ...pending);
  const reachesFloor = reachesStart || (earliest === 0 && !state?.last_run) || [...listed.keys()].some(n => n <= earliest);
  const listingIncomplete = !reachesFloor || [...pending].some(n => !listed.has(n));
  const hasUnsaved = [...listed.values(), ...activeRuns].some(run => {
    const n = run.run_number;
    if (n === runNumber || (n <= floor && !pending.has(n)) || !isEligibleRun(run, defaultBranch)) return false;
    if ((run.run_attempt ?? 1) > 1) return !isSavedConclusion(run.first_attempt_conclusion);
    return run.status === "completed" && !isSavedConclusion(run.conclusion);
  });
  return { hasUnsaved, listingIncomplete };
}

module.exports = { checkRoutingSaveCoverage, isEligibleRun, isSavedConclusion };
