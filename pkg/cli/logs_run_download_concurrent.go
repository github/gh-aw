// This file provides command-line interface functionality for gh-aw.
// This file contains concurrent run artifact download orchestration.
//
// Key responsibilities:
//   - Concurrent downloading of artifacts from multiple runs

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/repoutil"
)

// concurrentRunDownloadParams holds parameters shared across all goroutines
// in the concurrent download pool, avoiding repetitive parameter passing.
type concurrentRunDownloadParams struct {
	outputDir             string
	verbose               bool
	dlHost                string
	dlOwner               string
	dlRepo                string
	artifactFilter        []string
	includeWorkQueue      bool
	evalsOnly             bool
	storageLimit          *logsStorageLimit
	maxGitHubAPIRateLimit int
	rateLimitState        *logsRateLimitState
	// evalsArtifactRequested is true when the caller wants evals results, either
	// because --evals was passed (evalsOnly) or because --artifacts evals was
	// explicitly listed. This drives the fallback download of the dedicated evals
	// artifact when evals.jsonl is absent from the usage artifact.
	evalsArtifactRequested bool
}

// runArtifactsConcurrentOptions bundles the per-invocation options for
// downloadRunArtifactsConcurrent, collapsing a long positional parameter list into a struct.
// maxRuns is a hint for the maximum number of runs to process concurrently (0 means unlimited).
// artifactFilter restricts which artifacts are downloaded; nil means download all.
// artifactSets is the original pre-resolution set list used to determine evalsOnly fallback behavior.
// evalsOnly skips non-evals artifacts to reduce download volume on evals-focused runs.
type runArtifactsConcurrentOptions struct {
	outputDir              string
	verbose                bool
	maxRuns                int
	repoOverride           string
	artifactFilter         []string
	evalsOnly              bool
	artifactSets           []string
	maxConcurrentDownloads int
	storageLimit           *logsStorageLimit
	maxGitHubAPIRateLimit  int
	rateLimitState         *logsRateLimitState
	cachedRuns             cachedLogsRuns
	filters                runFilterOpts
	onResult               func(int, DownloadResult)
}

var processConcurrentRunDownload = processSingleRunDownload

// buildConcurrentDownloadParams constructs download parameters by parsing the optional
// repoOverride ("owner/repo" or "HOST/owner/repo") once for the whole batch.
// artifactSets is the original (pre-resolution) set list; it is used to detect whether
// the caller explicitly requested the evals artifact set so the fallback download can
// be triggered even when --evals was not set.
func buildConcurrentDownloadParams(outputDir string, verbose bool, repoOverride string, artifactFilter []string, evalsOnly bool, artifactSets []string) concurrentRunDownloadParams {
	var dlHost, dlOwner, dlRepo string
	if repoOverride != "" {
		ownerRepo, host := repoutil.NormalizeRepoForAPI(repoOverride)
		if owner, repo, err := repoutil.SplitRepoSlug(ownerRepo); err == nil {
			dlHost, dlOwner, dlRepo = host, owner, repo
		}
	}
	evalsArtifactRequested := isEvalsArtifactRequested(evalsOnly, artifactSets)
	return concurrentRunDownloadParams{
		outputDir:              outputDir,
		verbose:                verbose,
		dlHost:                 dlHost,
		dlOwner:                dlOwner,
		dlRepo:                 dlRepo,
		artifactFilter:         artifactFilter,
		includeWorkQueue:       workQueueEvidenceRequested(artifactSets),
		evalsOnly:              evalsOnly,
		evalsArtifactRequested: evalsArtifactRequested,
	}
}

// initDownloadProgressBar creates and displays a progress bar when running interactively.
// Returns nil when verbose is true or when running in CI (where \r produces unwanted newlines).
func initDownloadProgressBar(verbose bool, total int) *console.ProgressBar {
	if verbose || IsRunningInCI() {
		return nil
	}
	pb := console.NewProgressBar(int64(total))
	fmt.Fprintf(os.Stderr, "Processing runs: %s\r", pb.Update(0))
	return pb
}

// logConcurrentDownloadSummary emits a verbose completion summary for concurrent downloads.
func logConcurrentDownloadSummary(results []DownloadResult) {
	successCount := 0
	for _, result := range results {
		if result.Error == nil && !result.Skipped {
			successCount++
		}
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(
		fmt.Sprintf("Completed parallel processing: %d successful, %d total", successCount, len(results))))
}

// downloadRunArtifactsConcurrent downloads artifacts for multiple workflow runs concurrently.
// artifactSets is the original (pre-resolution) set list passed by the caller; it is used
// alongside evalsOnly to determine whether the evals artifact fallback should run.
func downloadRunArtifactsConcurrent(ctx context.Context, runs []WorkflowRun, opts runArtifactsConcurrentOptions) []DownloadResult {
	logsOrchestratorLog.Printf("Starting concurrent artifact download: runs=%d, outputDir=%s, maxRuns=%d", len(runs), opts.outputDir, opts.maxRuns)
	if len(runs) == 0 {
		return []DownloadResult{}
	}

	// maxRuns is a hint only; all runs are processed so that cache hits and
	// filter passes are counted correctly.
	totalRuns := len(runs)
	if opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Processing %d runs in parallel...", totalRuns)))
	}

	progressBar := initDownloadProgressBar(opts.verbose, totalRuns)
	var completedCount atomic.Int64
	maxConcurrent := getMaxConcurrentDownloads()
	if opts.maxConcurrentDownloads > 0 {
		maxConcurrent = opts.maxConcurrentDownloads
	}
	params := buildConcurrentDownloadParams(opts.outputDir, opts.verbose, opts.repoOverride, opts.artifactFilter, opts.evalsOnly, opts.artifactSets)
	params.storageLimit = opts.storageLimit
	params.maxGitHubAPIRateLimit = opts.maxGitHubAPIRateLimit
	params.rateLimitState = opts.rateLimitState

	results := runConcurrentArtifactDownloads(ctx, runs, opts, params, &completedCount, progressBar, maxConcurrent)
	if progressBar != nil {
		console.ClearLine()
	}
	if opts.verbose {
		logConcurrentDownloadSummary(results)
	}
	logsOrchestratorLog.Printf("Concurrent download complete: total=%d, results=%d", totalRuns, len(results))
	return results
}

type runDownloadJob struct {
	index int
	run   WorkflowRun
}

func runConcurrentArtifactDownloads(
	ctx context.Context,
	runs []WorkflowRun,
	opts runArtifactsConcurrentOptions,
	params concurrentRunDownloadParams,
	completedCount *atomic.Int64,
	progressBar *console.ProgressBar,
	maxConcurrent int,
) []DownloadResult {
	results := make([]DownloadResult, len(runs))
	completed := make([]bool, len(runs))
	jobs := make(chan runDownloadJob)
	var workers sync.WaitGroup
	for range min(maxConcurrent, len(runs)) {
		workers.Go(func() {
			runArtifactDownloadWorker(ctx, jobs, results, completed, opts, params, completedCount, progressBar)
		})
	}
	for i, run := range runs {
		if cachedResult, ok := cachedJSONDownloadResult(run, opts.cachedRuns, opts.filters); ok {
			recordConcurrentDownloadResult(i, cachedResult, results, completed, opts.onResult)
			completedCount.Add(1)
			continue
		}
		submitted := false
		select {
		case <-ctx.Done():
		case jobs <- runDownloadJob{index: i, run: run}:
			submitted = true
		}
		if !submitted {
			break
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		fillCanceledDownloadResults(runs, results, completed, err)
		if opts.verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Download interrupted: %v", err)))
		}
	}
	return results
}

func runArtifactDownloadWorker(
	ctx context.Context,
	jobs <-chan runDownloadJob,
	results []DownloadResult,
	completed []bool,
	opts runArtifactsConcurrentOptions,
	params concurrentRunDownloadParams,
	completedCount *atomic.Int64,
	progressBar *console.ProgressBar,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case job, ok := <-jobs:
			if !ok {
				return
			}
			result := canceledDownloadResult(job.run, ctx.Err())
			if result.Error == nil {
				result = executeConcurrentRunDownload(ctx, job.run, params, completedCount, progressBar)
			}
			recordConcurrentDownloadResult(job.index, result, results, completed, opts.onResult)
			if result.Error != nil && errors.Is(result.Error, context.Canceled) {
				return
			}
		}
	}
}

func executeConcurrentRunDownload(
	ctx context.Context,
	run WorkflowRun,
	params concurrentRunDownloadParams,
	completedCount *atomic.Int64,
	progressBar *console.ProgressBar,
) (result DownloadResult) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = DownloadResult{
				RunAnalysis: RunAnalysis{Run: run},
				Skipped:     true,
				Error:       fmt.Errorf("run download panicked: %v", recovered),
			}
		}
	}()
	result, _ = processConcurrentRunDownload(ctx, run, params, completedCount, progressBar)
	return result
}

func canceledDownloadResult(run WorkflowRun, err error) DownloadResult {
	if err == nil {
		return DownloadResult{}
	}
	return DownloadResult{RunAnalysis: RunAnalysis{Run: run}, Skipped: true, Error: err}
}

func recordConcurrentDownloadResult(index int, result DownloadResult, results []DownloadResult, completed []bool, onResult func(int, DownloadResult)) {
	if index < 0 || index >= len(results) || index >= len(completed) {
		return
	}
	results[index] = result
	completed[index] = true
	if onResult != nil {
		onResult(index, result)
	}
}

func fillCanceledDownloadResults(runs []WorkflowRun, results []DownloadResult, completed []bool, err error) {
	for i, run := range runs {
		if i < 0 || i >= len(completed) || i >= len(results) {
			continue
		}
		if !completed[i] {
			results[i] = canceledDownloadResult(run, err)
		}
	}
}

func cachedJSONDownloadResult(run WorkflowRun, cachedRuns cachedLogsRuns, filters runFilterOpts) (DownloadResult, bool) {
	cachedRun, ok := cachedRuns.lookup(run, filters)
	if !ok || (run.WorkflowPath != "" && !isAgenticWorkflowPath(run.WorkflowPath)) || !isAgenticWorkflowPath(cachedRun.WorkflowPath) {
		return DownloadResult{}, false
	}
	cachedAudit := cachedRuns[run.DatabaseID].Audit
	if cachedAudit != nil && cachedAudit.SchemaVersion != auditSchemaVersion {
		return DownloadResult{}, false
	}
	logsOrchestratorLog.Printf("Cache hit for run %d from cached JSONL; skipping artifact download and processing", run.DatabaseID)
	return DownloadResult{
		RunAnalysis: RunAnalysis{Run: run},
		Cached:      true,
		CachedRun:   &cachedRun,
		LogsPath:    cachedRun.LogsPath,
		cachedAudit: cachedAudit,
	}, true
}

// resolveRunRepoContext returns a copy of params with dlOwner/dlRepo/dlHost resolved to
// the per-run repository.  The global override takes precedence; for stdin mode (no global
// override), the context is derived from run.URL.
func resolveRunRepoContext(run WorkflowRun, params concurrentRunDownloadParams) concurrentRunDownloadParams {
	if params.dlOwner != "" || run.URL == "" {
		return params
	}
	if c, err := parser.ParseRunURLExtended(run.URL); err == nil && c.Owner != "" {
		params.dlOwner, params.dlRepo, params.dlHost = c.Owner, c.Repo, c.Host
	}
	return params
}
