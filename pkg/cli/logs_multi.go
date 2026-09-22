package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/stringutil"
)

type logsWorkflowTarget struct {
	workflowName string
	repoOverride string
}

type logsTargetResult struct {
	target logsWorkflowTarget
	result workflowLogsResult
	err    error
}

type logsCountLimit struct {
	max       int64
	processed atomic.Int64
	// cancel, when set, is invoked exactly once as soon as the shared budget is
	// exhausted, so every other concurrent target's context is canceled instead
	// of running to the end of its current batch/iteration before next noticing
	// the shared limit was reached. See collectLogsTargets, which wires this to
	// a context derived from the multi-target download's own context.
	cancel     context.CancelFunc
	cancelOnce sync.Once
}

type logsBatchScheduler struct {
	mu            sync.Mutex
	cond          *sync.Cond
	active        map[int]struct{}
	lastCompleted map[int]int
	round         int
	inFlight      int
	maxInFlight   int
	waiting       int
}

func newLogsBatchScheduler(ctx context.Context, targetCount, maxInFlight int) *logsBatchScheduler {
	scheduler := &logsBatchScheduler{
		active:        make(map[int]struct{}, targetCount),
		lastCompleted: make(map[int]int, targetCount),
		maxInFlight:   maxInFlight,
	}
	scheduler.cond = sync.NewCond(&scheduler.mu)
	for targetID := range targetCount {
		scheduler.active[targetID] = struct{}{}
		scheduler.lastCompleted[targetID] = -1
	}
	context.AfterFunc(ctx, func() {
		scheduler.mu.Lock()
		scheduler.cond.Broadcast()
		scheduler.mu.Unlock()
	})
	return scheduler
}

func (s *logsBatchScheduler) acquire(ctx context.Context, targetID int) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, active := s.active[targetID]; !active {
			return context.Canceled
		}
		if s.lastCompleted[targetID] < s.round && s.inFlight < s.maxInFlight {
			s.inFlight++
			return nil
		}
		s.waiting++
		s.cond.Wait()
		s.waiting--
	}
}

func (s *logsBatchScheduler) release(targetID int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inFlight--
	s.lastCompleted[targetID] = s.round
	s.advanceRoundIfComplete()
	s.cond.Broadcast()
}

func (s *logsBatchScheduler) remove(targetID int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.active, targetID)
	delete(s.lastCompleted, targetID)
	s.advanceRoundIfComplete()
	s.cond.Broadcast()
}

func (s *logsBatchScheduler) advanceRoundIfComplete() {
	for targetID := range s.active {
		if s.lastCompleted[targetID] < s.round {
			return
		}
	}
	s.round++
}

func (l *logsCountLimit) tryAdd() bool {
	if l == nil {
		return true
	}
	for {
		processed := l.processed.Load()
		if processed >= l.max {
			return false
		}
		if l.processed.CompareAndSwap(processed, processed+1) {
			if processed+1 >= l.max {
				l.cancelRemainingTargets()
			}
			return true
		}
	}
}

// cancelRemainingTargets cancels the context shared by every concurrent
// target as soon as the budget is exhausted, so targets that are already
// running (mid-batch or mid-download) stop at their next cooperative
// cancellation checkpoint instead of finishing their current, possibly
// oversized, batch first.
func (l *logsCountLimit) cancelRemainingTargets() {
	if l == nil {
		return
	}
	l.cancelOnce.Do(func() {
		if l.cancel != nil {
			l.cancel()
		}
	})
}

func (l *logsCountLimit) isReached() bool {
	return l != nil && l.processed.Load() >= l.max
}

// remaining reports how many runs the shared budget still allows. It returns -1
// when no shared limit is configured (single-target downloads), so callers can
// distinguish "unlimited" from "exhausted".
func (l *logsCountLimit) remaining() int {
	if l == nil {
		return -1
	}
	remaining := l.max - l.processed.Load()
	if remaining < 0 {
		return 0
	}
	return int(remaining)
}

func newLogsCountLimit(count int) *logsCountLimit {
	if count <= 0 {
		return nil
	}
	return &logsCountLimit{max: int64(count)}
}

var collectWorkflowLogsForTarget = collectWorkflowLogs

// queuedLogsTargetResult builds the workflowLogsResult for a target that never
// started downloading because the shared deadline or cancellation fired while
// it was still waiting for a worker slot. It preserves the target's own options
// (including any BeforeRunID cursor from a prior continuation) so a date-range
// target that made no progress still resumes from the correct position instead
// of silently disappearing from the report.
func queuedLogsTargetResult(opts LogsDownloadOptions, ctx context.Context) workflowLogsResult {
	timeoutReached := isDeadlineExceeded(ctx)
	if !timeoutReached {
		return workflowLogsResult{}
	}
	continuation := buildContinuationIfNeeded(nil, timeoutReached, false, false, false, logsTargetContinuationOptions(opts))
	return workflowLogsResult{timeoutReached: true, continuation: continuation}
}

func countLimitedLogsTargetResult(opts LogsDownloadOptions) workflowLogsResult {
	fetchAllInRange := opts.StartDate != "" || opts.EndDate != ""
	continuation := buildContinuationIfNeeded(nil, false, fetchAllInRange, false, false, logsTargetContinuationOptions(opts))
	return workflowLogsResult{countLimitReached: fetchAllInRange, continuation: continuation}
}

func rateLimitedLogsTargetResult(opts LogsDownloadOptions) workflowLogsResult {
	continuation := buildContinuationIfNeeded(nil, false, false, false, true, logsTargetContinuationOptions(opts))
	return workflowLogsResult{continuation: continuation}
}

func logsTargetContinuationOptions(opts LogsDownloadOptions) continuationOptions {
	return continuationOptions{
		workflowName:          opts.WorkflowName,
		startDate:             opts.StartDate,
		endDate:               opts.EndDate,
		engine:                opts.Engine,
		branch:                opts.Ref,
		afterRunID:            opts.AfterRunID,
		ignoreWorkflowRuns:    opts.IgnoreWorkflowRuns,
		count:                 opts.Count,
		timeoutMinutes:        opts.TimeoutMinutes,
		maxGitHubAPIRateLimit: opts.MaxGitHubAPIRateLimit,
		maxStorageMB:          opts.MaxStorageMB,
		pruneOlderRuns:        opts.PruneOlderRuns,
		previousBeforeRunID:   opts.BeforeRunID,
	}
}

// DownloadWorkflowLogsForTargets downloads several workflow reports concurrently
// and renders one combined report. Each target gets an isolated output directory
// so run IDs from different repositories cannot collide in the local cache.
func DownloadWorkflowLogsForTargets( //nolint:largefunc // Keeps shared collection and final cache filtering in one lifecycle.
	ctx context.Context,
	opts LogsDownloadOptions,
	targets []logsWorkflowTarget,
	initialErrors []error,
) (err error) {
	if len(targets) == 0 {
		return errors.Join(initialErrors...)
	}
	logLogsMultiTargetDownloadStart(opts, len(targets))
	activeCtx, timeoutCancel, _, _ := buildLogsDownloadContext(ctx, opts.TimeoutMinutes, opts.TimeoutSeconds, opts.Verbose)
	defer cancelLogsDownload(timeoutCancel)
	if err := ensureLogsGitignoreWithWarning(opts.Verbose); err != nil {
		return err
	}

	if err := prepareCachedLogsJSONL(&opts); err != nil {
		return err
	}
	if opts.collectionStats == nil {
		opts.collectionStats = &logsCollectionStats{}
	}
	defer func() {
		err = errors.Join(err, finalizeCachedLogsJSONL(opts.cachedJSONLWriter, opts.cachedJSONLSourcePaths, opts.cachedJSONLWildcard, opts.StartDate, opts.EndDate))
	}()
	allAPIRateLimits := startGitHubAPIRateLimitReports(activeCtx, logsTargetRateLimitHosts(targets))
	results := collectLogsTargets(activeCtx, opts, targets)
	processedRuns, continuations, timeoutReached, countLimitReached, storageLimitReached, allErrors := mergeLogsTargetResults(results, initialErrors)
	renderLogsCollectionStats(opts.collectionStats)
	for _, err := range allErrors {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Skipping workflow target: "+err.Error()))
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	finishGitHubAPIRateLimitReports(activeCtx, allAPIRateLimits, opts.JSONOutput)
	cacheGitHubAPIRateLimitReports(opts.cachedJSONLWriter, allAPIRateLimits...)
	apiRateLimit, apiRateLimits := partitionGitHubAPIRateLimitReports(allAPIRateLimits)
	renderLogsDownloadStatsSummary(opts.collectionStats, allAPIRateLimits...)
	if len(processedRuns) == 0 {
		if len(allErrors) > 0 {
			return errors.Join(allErrors...)
		}
		_, err := handleEmptyProcessedRuns(nil, opts, timeoutReached, storageLimitReached, nil, continuations, apiRateLimit, apiRateLimits)
		return err
	}

	processedRuns = sortAndLimitLogsTargetRuns(processedRuns, opts.Count, opts.Verbose)
	artifactFilter, err := resolveLogsArtifactFilter(opts.ArtifactSets, opts.Verbose)
	if err != nil {
		return err
	}
	return renderLogsOutput(processedRuns, renderLogsOutputOptions{
		outputDir:         opts.OutputDir,
		summaryFile:       opts.SummaryFile,
		format:            opts.Format,
		reportFile:        opts.ReportFile,
		jsonOutput:        opts.JSONOutput,
		toolGraph:         opts.ToolGraph,
		train:             opts.Train,
		drain3Weights:     opts.Drain3Weights,
		audit:             opts.Audit,
		verbose:           opts.Verbose,
		artifactFilter:    artifactFilter,
		startDate:         opts.StartDate,
		endDate:           opts.EndDate,
		checkStaleness:    true,
		countLimitReached: countLimitReached,
		suppressRender:    opts.SuppressRender,
		continuations:     continuations,
		apiRateLimit:      apiRateLimit,
		apiRateLimits:     apiRateLimits,
		cachedJSONLWriter: opts.cachedJSONLWriter,
	})
}

func logLogsMultiTargetDownloadStart(opts LogsDownloadOptions, targetCount int) {
	logsOrchestratorLog.Printf("Starting multi-target workflow log download: targets=%d", targetCount)
	logLogsDownloadStart(opts)
}

func sortAndLimitLogsTargetRuns(processedRuns []ProcessedRun, count int, verbose bool) []ProcessedRun {
	slices.SortStableFunc(processedRuns, func(a, b ProcessedRun) int {
		return b.Run.CreatedAt.Compare(a.Run.CreatedAt)
	})
	if count > 0 {
		return limitProcessedRuns(processedRuns, count, verbose)
	}
	return processedRuns
}

func logsTargetRateLimitHosts(targets []logsWorkflowTarget) []string {
	hosts := make([]string, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		host := normalizedGitHubAPIHost(logsRateLimitHost(target.repoOverride))
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts
}

func collectLogsTargets(ctx context.Context, opts LogsDownloadOptions, targets []logsWorkflowTarget) []logsTargetResult {
	resultChannel := make(chan logsTargetResult, len(targets))
	var wg sync.WaitGroup
	workerCount := min(len(targets), getMaxConcurrentWorkflowDownloads())
	countLimit := newLogsCountLimit(opts.Count)
	targetsCtx, cancelTargets := context.WithCancel(ctx)
	defer cancelTargets()
	rateLimitState := newLogsRateLimitState(cancelTargets)
	// targetsCtx is canceled the instant the shared run-count budget is
	// exhausted (see logsCountLimit.cancelRemainingTargets), so every target
	// still running or queued observes it at its next cooperative
	// cancellation checkpoint instead of only checking countLimit.isReached()
	// between its own batches/iterations.
	if countLimit != nil {
		countLimit.cancel = cancelTargets
	}
	shared := logsTargetSharedState{
		batchScheduler:     newLogsBatchScheduler(targetsCtx, len(targets), workerCount),
		perTargetDownloads: max(1, getMaxConcurrentDownloads()/workerCount),
		cleanupErrors:      make(map[string]error, len(targets)),
		storageLimit:       newLogsStorageLimit(opts.OutputDir, opts.MaxStorageMB, opts.PruneOlderRuns),
		countLimit:         countLimit,
		rateLimitState:     rateLimitState,
	}
	for _, target := range targets {
		cleanupOpts := opts
		cleanupOpts.OutputDir = logsTargetOutputDir(opts.OutputDir, target)
		if err := cleanupLogsOutputDir(cleanupOpts); err != nil {
			shared.cleanupErrors[target.displayName()] = err
		}
	}
	for targetID, target := range targets {
		wg.Go(func() {
			resultChannel <- collectSingleLogsTarget(targetsCtx, opts, targetID, target, shared)
		})
	}
	wg.Wait()
	close(resultChannel)
	results := make([]logsTargetResult, 0, len(targets))
	for result := range resultChannel {
		results = append(results, result)
	}
	return results
}

// logsTargetSharedState bundles the resources shared by every concurrent
// target worker: the fair batch scheduler, the per-target download share, and
// the storage/count budgets shared across all targets.
type logsTargetSharedState struct {
	batchScheduler     *logsBatchScheduler
	perTargetDownloads int
	cleanupErrors      map[string]error
	storageLimit       *logsStorageLimit
	countLimit         *logsCountLimit
	rateLimitState     *logsRateLimitState
}

// collectSingleLogsTarget runs one workflow target's log collection and
// recovers from panics. Every target now starts immediately and fairness is
// enforced per batch by the shared scheduler, so the pre-start cancellation
// branch below is reached only in the rare race where the shared deadline or
// cancellation fires before this goroutine is scheduled. That branch is not
// dead code: it must still build a resumable continuation (or report the
// shared count/rate-limit outcome) so such a target is not silently dropped
// from the report.
func collectSingleLogsTarget(ctx context.Context, opts LogsDownloadOptions, targetID int, target logsWorkflowTarget, shared logsTargetSharedState) (targetResult logsTargetResult) { //nolint:largefunc // Existing target collection remains centralized.
	defer shared.batchScheduler.remove(targetID)
	defer func() {
		if recovered := recover(); recovered != nil {
			targetResult = logsTargetResult{target: target, err: fmt.Errorf("workflow collector panicked: %v", recovered)}
		}
	}()
	if err := shared.cleanupErrors[target.displayName()]; err != nil {
		return logsTargetResult{target: target, err: err}
	}

	targetOpts := opts
	targetOpts.WorkflowName = target.workflowName
	targetOpts.RepoOverride = target.repoOverride
	targetOpts.OutputDir = logsTargetOutputDir(opts.OutputDir, target)
	targetOpts.SummaryFile = ""
	targetOpts.Train = false
	targetOpts.SuppressRender = true
	targetOpts.skipEnsureGitignore = true
	targetOpts.rateLimitFirstRequest = true
	targetOpts.maxConcurrentDownloads = shared.perTargetDownloads
	targetOpts.storageLimit = shared.storageLimit
	targetOpts.countLimit = shared.countLimit
	targetOpts.rateLimitState = shared.rateLimitState
	targetOpts.batchScheduler = shared.batchScheduler
	targetOpts.batchTargetID = targetID
	// The shared deadline is already installed on ctx by the caller, so the
	// target must not build a second timeout context of its own. TimeoutMinutes
	// and TimeoutSeconds are deliberately preserved (rather than zeroed) so the
	// continuation this target emits still carries the caller's timeout and can
	// be replayed as-is.
	targetOpts.inheritTimeoutContext = true

	if shared.countLimit.isReached() {
		logsOrchestratorLog.Printf("Skipping workflow target %s: shared maximum run count reached", target.displayName())
		return logsTargetResult{target: target, result: countLimitedLogsTargetResult(targetOpts)}
	}
	if err := ctx.Err(); err != nil {
		if shared.countLimit.isReached() {
			logsOrchestratorLog.Printf("Skipping workflow target %s: shared maximum run count reached while queued", target.displayName())
			return logsTargetResult{target: target, result: countLimitedLogsTargetResult(targetOpts)}
		}
		if shared.rateLimitState.isReached() {
			return logsTargetResult{target: target, result: rateLimitedLogsTargetResult(targetOpts), err: errLogsAPIRateLimitReached}
		}
		return logsTargetResult{
			target: target,
			result: queuedLogsTargetResult(targetOpts, ctx),
			err:    err,
		}
	}
	if shared.countLimit.isReached() {
		logsOrchestratorLog.Printf("Skipping workflow target %s: shared maximum run count reached while queued", target.displayName())
		return logsTargetResult{target: target, result: countLimitedLogsTargetResult(targetOpts)}
	}

	result, err := collectWorkflowLogsForTarget(ctx, targetOpts)
	return logsTargetResult{target: target, result: result, err: err}
}

func mergeLogsTargetResults(
	results []logsTargetResult,
	initialErrors []error,
) ([]ProcessedRun, []WorkflowContinuation, bool, bool, bool, []error) {
	allErrors := append([]error(nil), initialErrors...)
	var processedRuns []ProcessedRun
	timeoutReached := false
	countLimitReached := false
	storageLimitReached := false
	var continuations []WorkflowContinuation
	for _, targetResult := range results {
		if targetResult.err != nil && !errors.Is(targetResult.err, errLogsAPIRateLimitReached) {
			allErrors = append(allErrors, fmt.Errorf("%s: %w", targetResult.target.displayName(), targetResult.err))
		}
		processedRuns = append(processedRuns, targetResult.result.processedRuns...)
		timeoutReached = timeoutReached || targetResult.result.timeoutReached
		countLimitReached = countLimitReached || targetResult.result.countLimitReached
		storageLimitReached = storageLimitReached || targetResult.result.storageLimitReached
		if targetResult.result.continuation != nil {
			continuations = append(continuations, WorkflowContinuation{
				Repository:       targetResult.target.repoOverride,
				ContinuationData: *targetResult.result.continuation,
			})
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(
				"Partial results for workflow target "+targetResult.target.displayName()+"; continuation parameters were written to the report",
			))
		}
	}
	return processedRuns, continuations, timeoutReached, countLimitReached, storageLimitReached, allErrors
}

func (t logsWorkflowTarget) displayName() string {
	if t.repoOverride == "" {
		return t.workflowName
	}
	return filepath.Join(t.repoOverride, t.workflowName)
}

func logsTargetOutputDir(root string, target logsWorkflowTarget) string {
	workflowDir := "workflow-" + stringutil.SanitizeForFilename(target.workflowName)
	if target.repoOverride == "" {
		return filepath.Join(root, workflowDir)
	}
	repoDir := "repo-" + stringutil.SanitizeForFilename(target.repoOverride)
	return filepath.Join(root, repoDir, workflowDir)
}

func getMaxConcurrentWorkflowDownloads() int {
	const maxConcurrentWorkflows = 4
	return min(maxConcurrentWorkflows, getMaxConcurrentDownloads())
}
