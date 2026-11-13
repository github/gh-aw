package cli

import (
	"context"
	"errors"
	"fmt"
	"github.com/github/gh-aw/pkg/console"
	"os"
	"time"
)

// logsFetchWorkflowRunBatch and logsProcessWorkflowRunBatch are indirection points so
// tests can exercise the batch collection loop without hitting the GitHub API.
var (
	logsFetchWorkflowRunBatch   = fetchWorkflowRunBatch
	logsProcessWorkflowRunBatch = processWorkflowRunBatch
	buildLogsProcessedRun       = buildProcessedRun
)

type logsCollectionState struct {
	processedRuns       []ProcessedRun
	beforeDate          string
	iteration           int
	timeoutReached      bool
	countLimitReached   bool
	storageLimitReached bool
	atifError           error
}

type logsGuardrailStatus struct {
	countRemaining   int
	countMaximum     int
	storageRemaining int64
	storageMaximum   int64
	timeoutRemaining time.Duration
}

func currentLogsGuardrailStatus(runtime logsDownloadRuntime, opts LogsDownloadOptions, state logsCollectionState) logsGuardrailStatus {
	countRemaining := opts.Count - len(state.processedRuns)
	if sharedRemaining := opts.countLimit.remaining(); sharedRemaining >= 0 {
		countRemaining = sharedRemaining
	}
	countRemaining = max(0, countRemaining)

	storageUsed, storageMaximum := runtime.storageLimit.usage()
	storageRemaining := int64(-1)
	if storageMaximum > 0 {
		storageRemaining = max(0, storageMaximum-storageUsed)
	}

	timeoutRemaining := time.Duration(-1)
	if deadline, ok := runtime.activeCtx.Deadline(); ok {
		timeoutRemaining = max(0, time.Until(deadline))
	}
	return logsGuardrailStatus{
		countRemaining:   countRemaining,
		countMaximum:     opts.Count,
		storageRemaining: storageRemaining,
		storageMaximum:   storageMaximum,
		timeoutRemaining: timeoutRemaining,
	}
}

func logLogsGuardrailStatus(runtime logsDownloadRuntime, opts LogsDownloadOptions, state logsCollectionState) {
	status := currentLogsGuardrailStatus(runtime, opts, state)
	logsOrchestratorLog.Printf(
		"Logs download guardrails: iteration=%d count_remaining=%d count_maximum=%d storage_remaining_bytes=%d storage_maximum_bytes=%d timeout_remaining=%s",
		state.iteration, status.countRemaining, status.countMaximum, status.storageRemaining, status.storageMaximum, status.timeoutRemaining,
	)
}

func logLogsCollectionResult(runtime logsDownloadRuntime, opts LogsDownloadOptions, state logsCollectionState) {
	logLogsIterationLimit(runtime.fetchAllInRange, state.iteration, len(state.processedRuns), opts.Count)
	logLogsTimeoutResult(state.timeoutReached, len(state.processedRuns))
	logLogsStorageLimitResult(state.storageLimitReached, len(state.processedRuns))
	logLogsGuardrailStatus(runtime, opts, state)
}

func collectProcessedWorkflowRuns(runtime logsDownloadRuntime, opts LogsDownloadOptions) ([]ProcessedRun, bool, bool, bool, string, error) {
	state := logsCollectionState{}
	for state.iteration < MaxIterations {
		logLogsGuardrailStatus(runtime, opts, state)
		stop, timedOut, err := shouldStopLogsIteration(runtime, opts)
		if err != nil {
			return state.processedRuns, state.timeoutReached || timedOut, state.countLimitReached, state.storageLimitReached, state.beforeDate, err
		}
		if stop {
			state.timeoutReached = state.timeoutReached || timedOut
			break
		}
		if markSharedLogsCountReached(&state, runtime.fetchAllInRange, opts.countLimit) {
			break
		}
		if len(state.processedRuns) >= opts.Count {
			state.countLimitReached = runtime.fetchAllInRange
			break
		}
		stop, err = runLogsBatchRound(&state, runtime, opts)
		if err != nil {
			return state.processedRuns, state.timeoutReached, state.countLimitReached, state.storageLimitReached, state.beforeDate, err
		}
		if stop {
			break
		}
	}
	logLogsCollectionResult(runtime, opts, state)
	if runtime.fetchAllInRange && !state.timeoutReached && state.iteration >= MaxIterations {
		state.countLimitReached = true
	}
	return state.processedRuns, state.timeoutReached, state.countLimitReached, state.storageLimitReached, state.beforeDate, nil
}

// runLogsBatchRound runs one scheduled batch round for this target. The shared
// rate-limit check and the batch request/processing both run inside a single
// scheduler turn, so a target can neither reserve API budget in one round and
// spend it in a later one nor run ahead of the other targets by checking its
// next round's quota before the current round completes. The turn is released
// on every exit path.
func runLogsBatchRound(state *logsCollectionState, runtime logsDownloadRuntime, opts LogsDownloadOptions) (bool, error) {
	if err := opts.batchScheduler.acquire(runtime.activeCtx, opts.batchTargetID); err != nil {
		return handleLogsBatchError(state, runtime.fetchAllInRange, opts.countLimit, opts.rateLimitState, err)
	}
	defer opts.batchScheduler.release(opts.batchTargetID)
	if err := waitForLogsRateLimit(runtime.activeCtx, opts.Verbose, state.iteration, opts.rateLimitFirstRequest, opts.MaxGitHubAPIRateLimit, opts.rateLimitState); err != nil {
		return handleLogsRateLimitWaitError(state, runtime.fetchAllInRange, opts, err)
	}
	state.iteration++
	return fetchAndProcessLogsBatch(state, runtime, opts)
}

// handleLogsRateLimitWaitError maps a failed pre-batch rate-limit check onto the
// collection loop's stop/continue decision. A transient check failure only skips
// this round; every other outcome stops collection.
func handleLogsRateLimitWaitError(state *logsCollectionState, fetchAllInRange bool, opts LogsDownloadOptions, err error) (bool, error) {
	if errors.Is(err, context.DeadlineExceeded) {
		state.timeoutReached = true
		return true, nil
	}
	if errors.Is(err, context.Canceled) {
		if opts.countLimit.isReached() {
			markSharedLogsCountReached(state, fetchAllInRange, opts.countLimit)
			return true, nil
		}
		if opts.rateLimitState.isReached() {
			return true, errLogsAPIRateLimitReached
		}
		// A genuine cancellation, so report it without partial results: the
		// caller returns the (now zeroed) state alongside the error.
		*state = logsCollectionState{}
		return true, err
	}
	if errors.Is(err, errLogsAPIRateLimitReached) ||
		errors.Is(err, errInvalidMaxGitHubAPIRateLimit) ||
		errors.Is(err, errLogsRateLimitEnforcementFailed) {
		return true, err
	}
	logsOrchestratorLog.Printf("Rate limit wait failed, retrying iteration: %v", err)
	state.iteration++
	return false, nil
}

func fetchAndProcessLogsBatch(state *logsCollectionState, runtime logsDownloadRuntime, opts LogsDownloadOptions) (bool, error) {
	// Bound this batch by what the shared multi-target budget still allows.
	// Without this, every concurrent target fetches and downloads artifacts as
	// if it alone had to satisfy the whole --count, and the surplus is only
	// discarded after the artifacts have already been downloaded.
	opts.Count = effectiveLogsBatchCount(opts.Count, len(state.processedRuns), opts.countLimit)
	logLogsIterationFetch(opts, runtime.fetchAllInRange, state.iteration, len(state.processedRuns))
	logsJSONLFromContext(runtime.activeCtx).emit(logsJSONLEvent{Type: "progress", Stage: "fetching", Workflow: opts.WorkflowName, Repository: opts.RepoOverride})
	opts.cachedJSONLCache = runtime.cachedJSONLCache
	batch, err := logsFetchWorkflowRunBatch(runtime.activeCtx, opts, state.beforeDate, len(state.processedRuns), runtime.fetchAllInRange)
	if err != nil {
		return handleLogsBatchError(state, runtime.fetchAllInRange, opts.countLimit, opts.rateLimitState, err)
	}
	opts.collectionStats.recordDiscovered(len(batch.runs))
	logsJSONLFromContext(runtime.activeCtx).emit(logsJSONLEvent{Type: "progress", Stage: "discovered", Workflow: opts.WorkflowName, Repository: opts.RepoOverride, Count: len(batch.runs)})
	if len(batch.runs) == 0 {
		cursor, shouldContinue, shouldStop := handleEmptyWorkflowRunBatch(batch, opts.Verbose)
		if shouldStop {
			return true, nil
		}
		if shouldContinue {
			state.beforeDate = cursor
			return false, nil
		}
	}
	logWorkflowRunBatchFound(batch, state.iteration, opts.Verbose)
	var batchProcessed int
	var allRunsConsumed, batchTimedOut, batchStorageLimitReached bool
	state.processedRuns, batchProcessed, allRunsConsumed, batchTimedOut, batchStorageLimitReached = logsProcessWorkflowRunBatch(runtime.activeCtx, batch, state.processedRuns, processWorkflowRunBatchOptions{
		count:                  opts.Count,
		outputDir:              opts.OutputDir,
		verbose:                opts.Verbose,
		repoOverride:           opts.RepoOverride,
		artifactFilter:         runtime.artifactFilter,
		evalsOnly:              opts.EvalsOnly,
		artifactSets:           opts.ArtifactSets,
		parse:                  opts.Parse,
		filters:                runtime.filters,
		maxConcurrentDownloads: opts.maxConcurrentDownloads,
		storageLimit:           runtime.storageLimit,
		maxGitHubAPIRateLimit:  opts.MaxGitHubAPIRateLimit,
		rateLimitState:         opts.rateLimitState,
		cachedRuns:             runtime.cachedRuns,
		cachedJSONLWriter:      opts.cachedJSONLWriter,
		countLimit:             opts.countLimit,
		collectionStats:        opts.collectionStats,
		atifError:              &state.atifError,
	})
	state.timeoutReached = state.timeoutReached || batchTimedOut
	if state.atifError != nil {
		return true, handleLogsATIFBatchError(state, runtime.fetchAllInRange, opts, batchStorageLimitReached)
	}
	logProcessedWorkflowRunBatch(opts, runtime.fetchAllInRange, state.iteration, batchProcessed, len(state.processedRuns), opts.Verbose)
	if opts.rateLimitState.isReached() {
		// Timeout state is copied above, but preserve the incoming cursor because
		// cancellation may leave runs in this batch unprocessed. Storage state
		// remains relevant because it independently constrains the continuation.
		state.storageLimitReached = batchStorageLimitReached
		return true, errLogsAPIRateLimitReached
	}
	return finishLogsBatch(state, runtime, opts, batch, allRunsConsumed, batchStorageLimitReached), nil
}

func handleLogsATIFBatchError(state *logsCollectionState, fetchAllInRange bool, opts LogsDownloadOptions, batchStorageLimitReached bool) error {
	stopErr, failure := splitLogsATIFError(state.atifError)
	var guardrailErr error
	if stopErr != nil {
		state.storageLimitReached = batchStorageLimitReached || errors.Is(stopErr, errLogsStorageLimitReached)
		_, guardrailErr = handleLogsBatchError(state, fetchAllInRange, opts.countLimit, opts.rateLimitState, stopErr)
		if errors.Is(stopErr, errLogsStorageLimitReached) {
			guardrailErr = nil
		}
	}
	if failure != nil {
		failure = &logsATIFExportError{err: failure}
	}
	return errors.Join(failure, guardrailErr)
}

// finishLogsBatch applies the outcome of one processed batch to the collection
// state and decides whether the iteration loop should stop.
func finishLogsBatch(
	state *logsCollectionState,
	runtime logsDownloadRuntime,
	opts LogsDownloadOptions,
	batch workflowRunBatch,
	allRunsConsumed, batchStorageLimitReached bool,
) bool {
	// Only mark this batch as storage-limit-truncated when one of its own
	// downloads was actually rejected with errLogsStorageLimitReached. Checking
	// the shared limiter's global isReached() state here would produce a false
	// continuation whenever the last successful download exactly filled the
	// budget, and (with the multi-target limiter) could mark an unrelated,
	// fully-completed target as partial just because another target tripped
	// the shared threshold.
	state.storageLimitReached = batchStorageLimitReached
	if state.storageLimitReached {
		// Keep the prior pagination boundary. The continuation's before_run_id
		// resumes within this batch after the oldest successfully processed run.
		return true
	}
	// Advance the pagination cursor before checking the shared count limit: a
	// fully consumed batch has already been scanned in its entirety, so the
	// continuation must resume after it even when the shared budget (possibly
	// spent by another concurrent target) stops collection right here. Doing
	// this after the count-limit return would leave the continuation pointing
	// at the original end_date/before_run_id and cause a resumed request to
	// re-fetch this same batch.
	if allRunsConsumed {
		if cursor, ok := selectPaginationCursorDate(batch.runs, batch.oldestFetchedCreatedAt); ok {
			state.beforeDate = cursor
		}
	}
	if markSharedLogsCountReached(state, runtime.fetchAllInRange, opts.countLimit) {
		return true
	}
	return shouldStopAfterWorkflowRunBatch(batch, opts.Verbose)
}

func markSharedLogsCountReached(state *logsCollectionState, fetchAllInRange bool, limit *logsCountLimit) bool {
	if !limit.isReached() {
		return false
	}
	state.countLimitReached = fetchAllInRange
	return true
}

// effectiveLogsBatchCount caps the per-target run count by the runs the shared
// multi-target budget still allows, so a target never fetches or downloads more
// artifacts than the combined report can include. It returns count unchanged
// when no shared limit is configured, and when the shared budget is already
// exhausted (the collection loop stops on its own in that case, after the
// pagination cursor has been advanced past the fully-consumed batch).
func effectiveLogsBatchCount(count, processedCount int, limit *logsCountLimit) int {
	remaining := limit.remaining()
	if remaining <= 0 {
		return count
	}
	return min(count, processedCount+remaining)
}

func handleLogsBatchError(state *logsCollectionState, fetchAllInRange bool, countLimit *logsCountLimit, rateLimitState *logsRateLimitState, err error) (bool, error) {
	if errors.Is(err, context.DeadlineExceeded) {
		state.timeoutReached = true
		return true, nil
	}
	if errors.Is(err, context.Canceled) {
		if countLimit.isReached() {
			// Another concurrent target already filled the shared budget;
			// stop cleanly instead of surfacing a spurious cancellation error.
			markSharedLogsCountReached(state, fetchAllInRange, countLimit)
			return true, nil
		}
		if rateLimitState.isReached() {
			return true, errLogsAPIRateLimitReached
		}
		return true, err
	}
	return false, err
}

func shouldStopLogsIteration(runtime logsDownloadRuntime, opts LogsDownloadOptions) (bool, bool, error) {
	select {
	case <-runtime.activeCtx.Done():
		if isDeadlineExceeded(runtime.activeCtx) {
			if opts.Verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Timeout reached, stopping download"))
			}
			return true, true, nil
		}
		if opts.countLimit.isReached() {
			// Another concurrent target already filled the shared budget and
			// canceled the context every target shares; this is an expected,
			// successful stop, not a real cancellation. Report "not stopped"
			// so the caller falls through to its ordinary shared-count-limit
			// check just below, which applies the correct fetchAllInRange
			// semantics and avoids surfacing a spurious cancellation error.
			return false, false, nil
		}
		if opts.rateLimitState.isReached() {
			return true, false, errLogsAPIRateLimitReached
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
		return true, false, runtime.activeCtx.Err()
	default:
	}
	if !runtime.startTime.IsZero() && runtime.timeoutDuration > 0 && time.Since(runtime.startTime) >= runtime.timeoutDuration {
		if opts.Verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Timeout reached after %.1f seconds, stopping download", time.Since(runtime.startTime).Seconds())))
		}
		return true, true, nil
	}
	return false, false, nil
}

// errLogsRateLimitEnforcementFailed indicates that a rate-limit check could not
// verify current API usage while an explicit --max-github-api-rate-limit
// ceiling was configured. Unlike the best-effort default mode, a configured
// budget must fail closed rather than proceed with unknown consumption.
var errLogsRateLimitEnforcementFailed = errors.New("failed to enforce configured GitHub API rate limit")

func waitForLogsRateLimit(ctx context.Context, verbose bool, iteration int, firstRequest bool, configuredMax int, state *logsRateLimitState) error {
	if iteration == 0 && !firstRequest && configuredMax == 0 {
		return nil
	}
	if err := state.check(ctx, verbose, configuredMax, 1); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if errors.Is(err, errLogsAPIRateLimitReached) {
			return err
		}
		if errors.Is(err, errInvalidMaxGitHubAPIRateLimit) {
			return err
		}
		if configuredMax != 0 {
			// An explicit budget was requested; usage can no longer be verified,
			// so fail closed instead of proceeding with unknown consumption.
			return fmt.Errorf("%w: %w", errLogsRateLimitEnforcementFailed, err)
		}
		logsOrchestratorLog.Printf("Rate limit check failed (using static cooldown): %v", err)
	}
	return nil
}

func logLogsIterationFetch(opts LogsDownloadOptions, fetchAllInRange bool, iteration, processedCount int) {
	if !opts.Verbose || iteration <= 1 {
		return
	}
	if fetchAllInRange {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Iteration %d: Fetching more runs in date range...", iteration)))
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Iteration %d: Need %d more runs with artifacts, fetching more...", iteration, opts.Count-processedCount)))
}
