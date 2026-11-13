package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
)

type workflowRunBatch struct {
	runs                   []WorkflowRun
	totalFetched           int
	batchSize              int
	oldestFetchedCreatedAt time.Time
}
type processWorkflowRunBatchOptions struct {
	count                  int
	outputDir              string
	verbose                bool
	repoOverride           string
	artifactFilter         []string
	evalsOnly              bool
	artifactSets           []string
	parse                  bool
	filters                runFilterOpts
	maxConcurrentDownloads int
	storageLimit           *logsStorageLimit
	maxGitHubAPIRateLimit  int
	rateLimitState         *logsRateLimitState
	cachedRuns             cachedLogsRuns
	cachedJSONLWriter      *cachedLogsJSONLWriter
	countLimit             *logsCountLimit
	collectionStats        *logsCollectionStats
	atifError              *error
}

func fetchWorkflowRunBatch(ctx context.Context, opts LogsDownloadOptions, beforeDate string, processedCount int, fetchAllInRange bool) (workflowRunBatch, error) {
	batchSize := computeLogsBatchSize(opts.WorkflowName, opts.Count, processedCount, fetchAllInRange)
	var oldestFetchedCreatedAt time.Time
	runs, totalFetched, err := listWorkflowRunsWithPagination(ListWorkflowRunsOptions{
		Context:                ctx,
		WorkflowName:           opts.WorkflowName,
		Limit:                  batchSize,
		StartDate:              opts.StartDate,
		EndDate:                opts.EndDate,
		BeforeDate:             beforeDate,
		Ref:                    opts.Ref,
		BeforeRunID:            opts.BeforeRunID,
		AfterRunID:             opts.AfterRunID,
		IgnoreWorkflowRuns:     opts.IgnoreWorkflowRuns,
		RepoOverride:           opts.RepoOverride,
		OldestFetchedCreatedAt: &oldestFetchedCreatedAt,
		ProcessedCount:         processedCount,
		TargetCount:            opts.Count,
		Verbose:                opts.Verbose,
		CachedJSONLCache:       opts.cachedJSONLCache,
		CachedJSONLWriter:      opts.cachedJSONLWriter,
	})
	return workflowRunBatch{runs: runs, totalFetched: totalFetched, batchSize: batchSize, oldestFetchedCreatedAt: oldestFetchedCreatedAt}, err
}

func computeLogsBatchSize(workflowName string, count, processedCount int, fetchAllInRange bool) int {
	batchSize := BatchSize
	if workflowName == "" {
		batchSize = BatchSizeForAllWorkflows
	}
	if fetchAllInRange || count-processedCount >= batchSize {
		return batchSize
	}
	needed := count - processedCount
	batchSize = needed * 3
	if workflowName == "" && batchSize < BatchSizeForAllWorkflows {
		batchSize = BatchSizeForAllWorkflows
	}
	if batchSize > BatchSizeForAllWorkflows {
		batchSize = BatchSizeForAllWorkflows
	}
	return batchSize
}

func handleEmptyWorkflowRunBatch(batch workflowRunBatch, verbose bool) (string, bool, bool) {
	if len(batch.runs) > 0 {
		return "", false, false
	}
	if shouldStopPagination(batch.totalFetched, batch.batchSize) {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("No more workflow runs found, stopping iteration"))
		}
		return "", false, true
	}
	cursor, ok := selectPaginationCursorDate(nil, batch.oldestFetchedCreatedAt)
	if !ok {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Workflow batch filtered to zero runs but no pagination cursor was found, stopping iteration"))
		}
		return "", false, true
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Batch filtered to zero runs; advancing pagination cursor and continuing"))
	}
	return cursor, true, false
}

func logWorkflowRunBatchFound(batch workflowRunBatch, iteration int, verbose bool) {
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Found %d workflow runs in batch %d", len(batch.runs), iteration)))
	}
}

func processWorkflowRunBatch(
	activeCtx context.Context,
	batch workflowRunBatch,
	processedRuns []ProcessedRun,
	opts processWorkflowRunBatchOptions,
) ([]ProcessedRun, int, bool, bool, bool) {
	runsRemaining := batch.runs
	batchProcessed := 0
	storageLimitReached := false
	for len(runsRemaining) > 0 && len(processedRuns) < opts.count {
		remainingNeeded := opts.count - len(processedRuns)
		if remainingNeeded <= 0 {
			break
		}
		if stop, timedOut := batchContextDone(activeCtx); stop {
			return processedRuns, batchProcessed, false, timedOut, storageLimitReached
		}
		chunk := nextWorkflowRunChunk(&runsRemaining, remainingNeeded)
		var chunkStorageLimitReached bool
		processedRuns, batchProcessed, chunkStorageLimitReached = appendProcessedWorkflowRuns(activeCtx, processedRuns, chunk, batchProcessed, opts)
		storageLimitReached = storageLimitReached || chunkStorageLimitReached
		if opts.atifError != nil && *opts.atifError != nil {
			return processedRuns, batchProcessed, false, false, storageLimitReached
		}
		if len(processedRuns) >= opts.count {
			break
		}
		if chunkStorageLimitReached {
			break
		}
	}
	return processedRuns, batchProcessed, len(runsRemaining) == 0, false, storageLimitReached
}

func batchContextDone(ctx context.Context) (bool, bool) {
	select {
	case <-ctx.Done():
		return true, isDeadlineExceeded(ctx)
	default:
		return false, false
	}
}

func nextWorkflowRunChunk(runsRemaining *[]WorkflowRun, remainingNeeded int) []WorkflowRun {
	chunkSize := min(max(remainingNeeded*3, remainingNeeded), len(*runsRemaining))
	chunk := (*runsRemaining)[:chunkSize]
	*runsRemaining = (*runsRemaining)[chunkSize:]
	return chunk
}

type orderedLogsRunCollector struct {
	ctx                 context.Context
	opts                processWorkflowRunBatchOptions
	processedBase       int
	chunkSize           int
	candidates          []ProcessedRun
	accepted            []bool
	acceptedCount       int
	pendingResults      []DownloadResult
	resultReady         []bool
	resultResolved      []chan struct{}
	nextResult          int
	storageLimitReached bool
	mu                  sync.Mutex
}

func newOrderedLogsRunCollector(ctx context.Context, count int, chunkSize int, opts processWorkflowRunBatchOptions) *orderedLogsRunCollector {
	if opts.atifError == nil {
		opts.atifError = new(error)
	}
	collector := &orderedLogsRunCollector{
		ctx:            ctx,
		opts:           opts,
		processedBase:  count,
		chunkSize:      chunkSize,
		candidates:     make([]ProcessedRun, chunkSize),
		accepted:       make([]bool, chunkSize),
		pendingResults: make([]DownloadResult, chunkSize),
		resultReady:    make([]bool, chunkSize),
		resultResolved: make([]chan struct{}, chunkSize),
	}
	for i := range collector.resultResolved {
		collector.resultResolved[i] = make(chan struct{}) //nolint:uncheckedsliceindex // i comes from ranging over resultResolved.
	}
	return collector
}

func (c *orderedLogsRunCollector) onResult(index int, result DownloadResult) {
	resolved := c.recordResult(index, result)
	// Later results retain their worker slots until earlier API results resolve.
	// This bounds future work while preserving newest-first selection and cursors.
	<-resolved
}

func (c *orderedLogsRunCollector) recordResult(index int, result DownloadResult) <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if index < 0 || index >= c.chunkSize {
		message := fmt.Sprintf("Ordered run result index %d is out of range (chunk size %d); processing result without ordering guarantees.", index, c.chunkSize)
		logsOrchestratorLog.Print(message)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
		c.opts.collectionStats.recordResult(result)
		finalizeLogsRunDownload(c.opts.storageLimit, result)
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	resolved := c.resultResolved[index]                             //nolint:uncheckedsliceindex // index is checked against chunkSize, which matches resultResolved length.
	c.pendingResults[index] = result                                //nolint:uncheckedsliceindex // index is checked against chunkSize, which matches pendingResults length.
	c.resultReady[index] = true                                     //nolint:uncheckedsliceindex // index is checked against chunkSize, which matches resultReady length.
	for c.nextResult < c.chunkSize && c.resultReady[c.nextResult] { //nolint:uncheckedsliceindex // nextResult is bounded by chunkSize, which matches resultReady length.
		c.processReadyResult(c.nextResult)
		close(c.resultResolved[c.nextResult]) //nolint:uncheckedsliceindex // nextResult is bounded by chunkSize, which matches resultResolved length.
		c.nextResult++
	}
	return resolved
}

func (c *orderedLogsRunCollector) processReadyResult(index int) {
	result := c.pendingResults[index] //nolint:uncheckedsliceindex // index is checked against chunkSize, which matches pendingResults length.
	c.opts.collectionStats.recordResult(result)
	if c.processedBase+c.acceptedCount >= c.opts.count || c.opts.countLimit.isReached() {
		finalizeLogsRunDownload(c.opts.storageLimit, result)
		return
	}
	if result.CachedRun != nil {
		processedRun := processedRunFromCachedData(*result.CachedRun, result.cachedAudit, c.opts.outputDir)
		if !c.exportATIF(processedRun.Run, false) {
			return
		}
		if c.opts.countLimit.tryAdd() {
			c.candidates[index] = processedRun //nolint:uncheckedsliceindex // index is checked against chunkSize, which matches candidates length.
			c.accepted[index] = true           //nolint:uncheckedsliceindex // index is checked against chunkSize, which matches accepted length.
			c.acceptedCount++
			logsJSONLFromContext(c.ctx).run(processedRun)
		}
		return
	}
	if errors.Is(result.Error, errLogsStorageLimitReached) {
		c.storageLimitReached = true
	}
	if shouldSkipProcessedWorkflowRun(result, c.opts.verbose) || applyRunFilters(c.ctx, result, c.opts.filters, c.opts.verbose) {
		finalizeLogsRunDownload(c.opts.storageLimit, result)
		return
	}
	processedRun := buildLogsProcessedRun(c.ctx, result, c.opts.verbose, true)
	parseWorkflowRunArtifacts(result, processedRun, c.opts.parse, c.opts.verbose)
	exportCompleted := c.exportATIF(processedRun.Run, result.storageReserved)
	finalizeLogsRunDownload(c.opts.storageLimit, result)
	if !exportCompleted {
		return
	}
	if c.opts.countLimit.tryAdd() {
		c.candidates[index] = processedRun //nolint:uncheckedsliceindex // index is checked against chunkSize, which matches candidates length.
		c.accepted[index] = true           //nolint:uncheckedsliceindex // index is checked against chunkSize, which matches accepted length.
		c.acceptedCount++
		logsJSONLFromContext(c.ctx).run(processedRun)
	}
}

func (c *orderedLogsRunCollector) exportATIF(run WorkflowRun, reserved bool) bool {
	if err := exportLogsATIF(c.ctx, run, c.opts.outputDir, c.opts.storageLimit, reserved); err != nil {
		*c.opts.atifError = errors.Join(*c.opts.atifError, fmt.Errorf("ATIF export for run %d: %w", run.DatabaseID, err))
		stop, _ := splitLogsATIFError(err) //nolint:ignoredlocalerror // The complete error is retained above; only its stop classification is needed here.
		return stop == nil
	}
	return true
}

func (c *orderedLogsRunCollector) appendAccepted(processedRuns []ProcessedRun, batchProcessed int, writer *cachedLogsJSONLWriter) ([]ProcessedRun, int) {
	for i, accepted := range c.accepted {
		if !accepted {
			continue
		}
		candidate := c.candidates[i] //nolint:uncheckedsliceindex // i comes from accepted; candidates is allocated with the same chunkSize.
		processedRuns = append(processedRuns, candidate)
		batchProcessed++
		if err := writer.Append(candidate); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(err.Error()))
		}
	}
	return processedRuns, batchProcessed
}

func appendProcessedWorkflowRuns(
	activeCtx context.Context,
	processedRuns []ProcessedRun,
	chunk []WorkflowRun,
	batchProcessed int,
	opts processWorkflowRunBatchOptions,
) ([]ProcessedRun, int, bool) {
	collector := newOrderedLogsRunCollector(activeCtx, len(processedRuns), len(chunk), opts)
	downloadRunArtifactsConcurrent(activeCtx, chunk, runArtifactsConcurrentOptions{
		outputDir:              opts.outputDir,
		verbose:                opts.verbose,
		maxRuns:                opts.count - len(processedRuns),
		repoOverride:           opts.repoOverride,
		artifactFilter:         opts.artifactFilter,
		evalsOnly:              opts.evalsOnly,
		artifactSets:           opts.artifactSets,
		maxConcurrentDownloads: opts.maxConcurrentDownloads,
		storageLimit:           opts.storageLimit,
		maxGitHubAPIRateLimit:  opts.maxGitHubAPIRateLimit,
		rateLimitState:         opts.rateLimitState,
		cachedRuns:             opts.cachedRuns,
		filters:                opts.filters,
		onResult:               collector.onResult,
	})
	processedRuns, batchProcessed = collector.appendAccepted(processedRuns, batchProcessed, opts.cachedJSONLWriter)
	return processedRuns, batchProcessed, collector.storageLimitReached
}

func finalizeLogsRunDownload(storageLimit *logsStorageLimit, result DownloadResult) {
	if storageLimit == nil || !result.storageReserved {
		return
	}
	if err := storageLimit.finalizeDownload(result.LogsPath); err != nil {
		logsOrchestratorLog.Printf("Failed to finalize cache pruning for run %d: %v", result.Run.DatabaseID, err)
	}
}

func shouldSkipProcessedWorkflowRun(result DownloadResult, verbose bool) bool {
	if result.Skipped {
		if verbose && result.Error != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Skipping run %d: %v", result.Run.DatabaseID, result.Error)))
		}
		return true
	}
	if result.Error != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to download artifacts for run %d: %v", result.Run.DatabaseID, result.Error)))
		return true
	}
	return false
}

func parseWorkflowRunArtifacts(result DownloadResult, processedRun ProcessedRun, parse, verbose bool) {
	if !parse {
		return
	}
	awInfoPath := filepath.Join(result.LogsPath, "aw_info.json")
	detectedEngine := extractEngineFromAwInfo(awInfoPath, verbose)
	if err := parseAgentLog(result.LogsPath, detectedEngine, verbose); err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse log for run %d: %v", processedRun.Run.DatabaseID, err)))
	} else if logMdPath := filepath.Join(result.LogsPath, "log.md"); fileutil.FileExists(logMdPath) {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Parsed log for run %d → %s", processedRun.Run.DatabaseID, logMdPath)))
	}
	if err := parseFirewallLogs(result.LogsPath, verbose); err != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to parse firewall logs for run %d: %v", processedRun.Run.DatabaseID, err)))
	} else if firewallMdPath := filepath.Join(result.LogsPath, "firewall.md"); fileutil.FileExists(firewallMdPath) {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Parsed firewall logs for run %d → %s", processedRun.Run.DatabaseID, firewallMdPath)))
	}
}

func logProcessedWorkflowRunBatch(opts LogsDownloadOptions, fetchAllInRange bool, iteration, batchProcessed, processedCount int, verbose bool) {
	if !verbose {
		return
	}
	if fetchAllInRange {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Processed %d runs with artifacts in batch %d (total: %d)", batchProcessed, iteration, processedCount)))
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Processed %d runs with artifacts in batch %d (total: %d/%d)", batchProcessed, iteration, processedCount, opts.Count)))
}

func shouldStopAfterWorkflowRunBatch(batch workflowRunBatch, verbose bool) bool {
	if !shouldStopPagination(batch.totalFetched, batch.batchSize) {
		return false
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Received fewer runs than requested, likely reached end of available runs"))
	}
	return true
}

func logLogsIterationLimit(fetchAllInRange bool, iteration, processedCount, count int) {
	if iteration < MaxIterations {
		return
	}
	if fetchAllInRange {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Reached maximum iterations (%d), collected %d runs with artifacts", MaxIterations, processedCount)))
		return
	}
	if processedCount < count {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Reached maximum iterations (%d), collected %d runs with artifacts out of %d requested", MaxIterations, processedCount, count)))
	}
}

func logLogsTimeoutResult(timeoutReached bool, processedCount int) {
	if timeoutReached && processedCount > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Timeout reached, returning %d processed runs", processedCount)))
	}
}

func logLogsStorageLimitResult(storageLimitReached bool, processedCount int) {
	if storageLimitReached && processedCount > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Storage limit reached, returning %d processed runs", processedCount)))
	}
}

func handleEmptyProcessedRuns(
	processedRuns []ProcessedRun,
	opts LogsDownloadOptions,
	timeoutReached, storageLimitReached bool,
	continuation *ContinuationData,
	continuations []WorkflowContinuation,
	apiRateLimit *GitHubAPIRateLimitReport,
	apiRateLimits []*GitHubAPIRateLimitReport,
) (bool, error) {
	if len(processedRuns) > 0 {
		return false, nil
	}
	if opts.JSONOutput || opts.Format == "jsonl" {
		logsData := buildLogsData([]ProcessedRun{}, opts.OutputDir, continuation)
		logsData.Continuations = continuations
		logsData.GitHubAPIRateLimit = populatedGitHubAPIRateLimitReport(apiRateLimit)
		logsData.GitHubAPIRateLimits = populatedGitHubAPIRateLimitReports(apiRateLimits)
		logsData.Message = noRunsMessage(opts.StartDate, timeoutReached, storageLimitReached)
		if opts.Format == "jsonl" {
			if err := opts.jsonlStream.summary(logsData); err != nil {
				return true, err
			}
		} else if opts.JSONOutput {
			if err := renderLogsJSON(logsData, opts.Verbose); err != nil {
				return true, fmt.Errorf("failed to render JSON output: %w", err)
			}
		}
	}
	if timeoutReached {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Timeout reached before any runs could be downloaded"))
	} else if storageLimitReached {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Storage limit reached before any new runs could be downloaded"))
	} else {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No workflow runs with artifacts found matching the specified criteria"))
	}
	return true, nil
}

func limitProcessedRuns(processedRuns []ProcessedRun, count int, verbose bool) []ProcessedRun {
	if len(processedRuns) <= count {
		return processedRuns
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Limiting output to %d most recent runs (fetched %d total)", count, len(processedRuns))))
	}
	return processedRuns[:count]
}
