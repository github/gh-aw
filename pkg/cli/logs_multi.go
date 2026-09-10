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
			return true
		}
	}
}

func (l *logsCountLimit) isReached() bool {
	return l != nil && l.processed.Load() >= l.max
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
	continuation := buildContinuationIfNeeded(nil, timeoutReached, false, false, continuationOptions{
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
	})
	return workflowLogsResult{timeoutReached: true, continuation: continuation}
}

// DownloadWorkflowLogsForTargets downloads several workflow reports concurrently
// and renders one combined report. Each target gets an isolated output directory
// so run IDs from different repositories cannot collide in the local cache.
func DownloadWorkflowLogsForTargets(
	ctx context.Context,
	opts LogsDownloadOptions,
	targets []logsWorkflowTarget,
	initialErrors []error,
) error {
	if len(targets) == 0 {
		return errors.Join(initialErrors...)
	}
	activeCtx, timeoutCancel, _, _ := buildLogsDownloadContext(ctx, opts.TimeoutMinutes, opts.TimeoutSeconds, opts.Verbose)
	defer cancelLogsDownload(timeoutCancel)
	if err := ensureLogsGitignoreWithWarning(opts.Verbose); err != nil {
		return err
	}

	if err := prepareCachedLogsJSONL(&opts); err != nil {
		return err
	}
	allAPIRateLimits := startGitHubAPIRateLimitReports(activeCtx, logsTargetRateLimitHosts(targets))
	results := collectLogsTargets(activeCtx, opts, targets)
	processedRuns, continuations, timeoutReached, countLimitReached, storageLimitReached, allErrors := mergeLogsTargetResults(results, initialErrors)
	for _, err := range allErrors {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Skipping workflow target: "+err.Error()))
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	finishGitHubAPIRateLimitReports(activeCtx, allAPIRateLimits, opts.JSONOutput)
	cacheGitHubAPIRateLimitReports(opts.cachedJSONLWriter, allAPIRateLimits...)
	apiRateLimit, apiRateLimits := partitionGitHubAPIRateLimitReports(allAPIRateLimits)
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
	})
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
	shared := logsTargetSharedState{
		sem:                make(chan struct{}, workerCount),
		perTargetDownloads: max(1, getMaxConcurrentDownloads()/workerCount),
		cleanupErrors:      make(map[string]error, len(targets)),
		storageLimit:       newLogsStorageLimit(opts.OutputDir, opts.MaxStorageMB, opts.PruneOlderRuns),
		countLimit:         newLogsCountLimit(opts.Count),
	}
	for _, target := range targets {
		cleanupOpts := opts
		cleanupOpts.OutputDir = logsTargetOutputDir(opts.OutputDir, target)
		if err := cleanupLogsOutputDir(cleanupOpts); err != nil {
			shared.cleanupErrors[target.displayName()] = err
		}
	}
	for _, target := range targets {
		wg.Go(func() {
			resultChannel <- collectSingleLogsTarget(ctx, opts, target, shared)
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
// target worker: the semaphore bounding parallel downloads, the per-target
// download share, and the storage/count budgets shared across all targets.
type logsTargetSharedState struct {
	sem                chan struct{}
	perTargetDownloads int
	cleanupErrors      map[string]error
	storageLimit       *logsStorageLimit
	countLimit         *logsCountLimit
}

// collectSingleLogsTarget runs one workflow target's log collection, recovering
// from panics and building a resumable continuation when the target is still
// waiting for a worker slot when the shared deadline or cancellation fires.
func collectSingleLogsTarget(ctx context.Context, opts LogsDownloadOptions, target logsWorkflowTarget, shared logsTargetSharedState) (targetResult logsTargetResult) {
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
	targetOpts.TimeoutMinutes = 0
	targetOpts.TimeoutSeconds = 0

	select {
	case shared.sem <- struct{}{}:
		defer func() { <-shared.sem }()
	case <-ctx.Done():
		// The target never started (e.g. it was still queued behind the
		// semaphore when the shared deadline or cancellation fired). Build a
		// continuation from its own options so a resumable date-range target
		// is not silently dropped from the report.
		return logsTargetResult{
			target: target,
			result: queuedLogsTargetResult(targetOpts, ctx),
			err:    ctx.Err(),
		}
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
		if targetResult.err != nil {
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
