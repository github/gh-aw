package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

// handleArtifactDownloadError fills result based on the artifact download error.
// Failure-conclusion runs are kept (with empty metrics) so they appear in reports;
// all other runs without artifacts are marked as skipped.
func handleArtifactDownloadError(result *DownloadResult, err error, verbose bool) {
	run := result.Run
	if errors.Is(err, errLogsStorageLimitReached) {
		result.Skipped = true
		result.Error = err
	} else if errors.Is(err, ErrNoArtifacts) {
		logsOrchestratorLog.Printf("No artifacts available for run %d (conclusion=%s)", run.DatabaseID, run.Conclusion)
		if isFailureWithoutArtifacts(err, run.Conclusion) {
			result.Metrics = LogMetrics{}
			// ErrorCount will be populated by buildProcessedRun via fetchJobStatuses.
		} else {
			result.Skipped = true
			result.Error = err
		}
	} else {
		result.Error = err
	}
}

// logsRunPreflightAPIReserve conservatively estimates the number of core API
// requests a single run's full processing pipeline can make after the
// preflight rate-limit check: artifact listing, one or more artifact
// downloads, the workflow-run metadata fetch, an optional workflow-logs fetch,
// an optional evals fallback download, and the jobs fetch performed later by buildProcessedRun. A single
// check before downloadRunArtifacts cannot bound the ceiling on its own
// because usage is only sampled once; reserving this budget upfront prevents
// the remaining calls from silently pushing usage past a configured ceiling
// before the next check.
const logsRunPreflightAPIReserve = 9

// processSingleRunDownload executes the full download and analysis pipeline for one run.
// It is called concurrently from downloadRunArtifactsConcurrent for each run in the batch.
func processSingleRunDownload(
	ctx context.Context,
	run WorkflowRun,
	params concurrentRunDownloadParams,
	completedCount *atomic.Int64,
	progressBar *console.ProgressBar,
) (DownloadResult, error) {
	select {
	case <-ctx.Done():
		return DownloadResult{RunAnalysis: RunAnalysis{Run: run}, Skipped: true, Error: ctx.Err()}, nil
	default:
	}
	if params.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Processing run %d (%s)...", run.DatabaseID, run.Status)))
	}

	runOutputDir := filepath.Join(params.outputDir, fmt.Sprintf("run-%d", run.DatabaseID))
	perRunParams := resolveRunRepoContext(run, params)

	result, ok, err := prepareRunDownload(ctx, run, runOutputDir, perRunParams, params.storageLimit)
	applyKnownWorkflowPath(result, run)
	if err != nil {
		handleArtifactDownloadError(result, err, params.verbose)
		if !isFailureWithoutArtifacts(err, result.Run.Conclusion) {
			skipUnverifiedWorkflowRun(result, params.verbose)
		}
	} else if ok {
		inferMissingWorkflowPath(result, runOutputDir)
	} else {
		downloadAndTimeRunArtifacts(ctx, run, runOutputDir, perRunParams, params, result)
	}
	if err == nil && !result.Skipped && !isAgenticWorkflowPath(result.Run.WorkflowPath) {
		skipNonAgenticWorkflowRun(result, params.verbose)
	}

	completed := completedCount.Add(1)
	if progressBar != nil {
		fmt.Fprintf(os.Stderr, "Processing runs: %s\r", progressBar.Update(completed))
	}
	return *result, nil
}

// downloadAndTimeRunArtifacts performs the actual artifact download for a run
// that was not served from cache, recording wall-clock duration and resulting
// on-disk size onto result.Run so that end-of-run download stats can be
// aggregated across all runs processed in this invocation.
func downloadAndTimeRunArtifacts(
	ctx context.Context,
	run WorkflowRun,
	runOutputDir string,
	perRunParams concurrentRunDownloadParams,
	params concurrentRunDownloadParams,
	result *DownloadResult,
) {
	writeWorkflowRunFolderLocation(run.DatabaseID, runOutputDir)
	logsOrchestratorLog.Printf("Downloading artifacts for run %d: owner=%s, repo=%s", run.DatabaseID, perRunParams.dlOwner, perRunParams.dlRepo)
	// sizeBefore/downloadStart bracket only the actual GitHub download calls
	// (metadata fetch, artifact download, evals fallback) rather than the
	// preceding rate-limit wait or the trailing artifact analysis, so a long
	// quota wait or CPU-heavy analysis is never misreported as download
	// latency, and pre-existing bytes on disk (from an earlier incremental or
	// cache pass) are excluded from the reported size.
	var downloadStart time.Time
	var sizeBefore int64
	err := params.storageLimit.runDownloadDeferredReserved(ctx, runOutputDir, func() error {
		if err := waitForConfiguredRateLimit(ctx, params.verbose, params.maxGitHubAPIRateLimit, logsRunPreflightAPIReserve, params.rateLimitState); err != nil {
			return err
		}

		if err := os.MkdirAll(runOutputDir, constants.DirPermSensitive); err != nil {
			return fmt.Errorf("failed to create run output directory: %w", err)
		}
		if size, sizeErr := logsDirectorySize(runOutputDir); sizeErr == nil {
			sizeBefore = size
		} else {
			logsOrchestratorLog.Printf("failed to compute pre-download size for run %d: %v", run.DatabaseID, sizeErr)
		}
		downloadStart = time.Now()
		if !shouldDownloadAgenticWorkflowRun(ctx, run, runOutputDir, perRunParams, params.verbose, result) {
			return nil
		}
		if err := downloadRunArtifacts(ctx, downloadArtifactsOptions{runID: run.DatabaseID, outputDir: runOutputDir, verbose: params.verbose, owner: perRunParams.dlOwner, repo: perRunParams.dlRepo, hostname: perRunParams.dlHost, artifactFilter: params.artifactFilter, includeWorkQueue: params.includeWorkQueue}); err != nil {
			return err
		}
		// When evals are requested but not found in the usage artifact (older runs
		// that predate the conclusion-job copy), fall back to the dedicated evals
		// artifact so those runs are not silently skipped. This applies both when
		// --evals is set and when --artifacts evals was explicitly listed.
		if params.evalsArtifactRequested && !runHasEvals(runOutputDir, params.verbose) {
			tryDownloadEvalsArtifactFallback(ctx, run.DatabaseID, runOutputDir, perRunParams)
		}
		result.Run.DownloadDuration = time.Since(downloadStart)
		if size, sizeErr := logsDirectorySize(runOutputDir); sizeErr == nil {
			if size > sizeBefore {
				result.Run.DownloadSizeBytes = size - sizeBefore
			}
		} else {
			logsOrchestratorLog.Printf("failed to compute download size for run %d: %v", run.DatabaseID, sizeErr)
		}
		analyzeRunArtifacts(ctx, result, runOutputDir, params.verbose, params.artifactFilter)
		return nil
	})

	if err != nil {
		handleArtifactDownloadError(result, err, params.verbose)
		return
	}
}

func shouldDownloadAgenticWorkflowRun(
	ctx context.Context,
	run WorkflowRun,
	runOutputDir string,
	perRunParams concurrentRunDownloadParams,
	verbose bool,
	result *DownloadResult,
) bool {
	metadata, err := fetchAndCacheWorkflowRunMetadata(ctx, run, runOutputDir, perRunParams.dlOwner, perRunParams.dlRepo, perRunParams.dlHost, verbose)
	if err != nil {
		logsOrchestratorLog.Printf("Failed to fetch workflow run metadata for run %d: %v", run.DatabaseID, err)
	} else {
		applyWorkflowRunMetadata(&result.Run, metadata)
	}
	if result.Run.WorkflowPath == "" || isAgenticWorkflowPath(result.Run.WorkflowPath) {
		return true
	}
	skipNonAgenticWorkflowRun(result, verbose)
	return false
}

func isAgenticWorkflowPath(workflowPath string) bool {
	return strings.HasSuffix(workflowPath, ".lock.yml")
}

func skipNonAgenticWorkflowRun(result *DownloadResult, verbose bool) {
	result.Skipped = true
	logsOrchestratorLog.Printf("Skipping non-agentic workflow run: run=%d, workflow_path=%s", result.Run.DatabaseID, result.Run.WorkflowPath)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d because workflow path %q is not an agentic .lock.yml workflow", result.Run.DatabaseID, result.Run.WorkflowPath)))
	}
}

func skipUnverifiedWorkflowRun(result *DownloadResult, verbose bool) {
	if !result.Skipped && !isAgenticWorkflowPath(result.Run.WorkflowPath) {
		skipNonAgenticWorkflowRun(result, verbose)
	}
}

func isFailureWithoutArtifacts(err error, conclusion string) bool {
	return errors.Is(err, ErrNoArtifacts) && isFailureConclusion(conclusion)
}

func applyKnownWorkflowPath(result *DownloadResult, run WorkflowRun) {
	if run.WorkflowPath != "" {
		result.Run.WorkflowPath = run.WorkflowPath
	}
}

func writeWorkflowRunFolderLocation(runID int64, runOutputDir string) {
	runFolder, err := filepath.Abs(runOutputDir)
	if err != nil {
		runFolder = runOutputDir
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Workflow run %d folder: %s", runID, runFolder)))
}

func prepareRunDownload(
	ctx context.Context,
	run WorkflowRun,
	runOutputDir string,
	params concurrentRunDownloadParams,
	storageLimit *logsStorageLimit,
) (*DownloadResult, bool, error) {
	result := &DownloadResult{RunAnalysis: RunAnalysis{Run: run}, LogsPath: runOutputDir}
	if storageLimit != nil {
		if err := storageLimit.reserve(runOutputDir); err != nil {
			return result, false, err
		}
		result.storageReserved = true
	}
	cachedResult, ok := tryLoadCachedRunResult(ctx, run, runOutputDir, params)
	if ok {
		cachedResult.storageReserved = result.storageReserved
		return cachedResult, true, nil
	}
	return result, false, nil
}

// tryDownloadEvalsArtifactFallback attempts to download the dedicated evals artifact for
// a run that already has its primary artifacts on disk but does not contain evals.jsonl.
// This handles older runs where evals.jsonl was uploaded as a standalone artifact instead
// of being included in the usage artifact by the conclusion job.
// Errors are logged but not propagated — the caller proceeds with whatever was downloaded.
func tryDownloadEvalsArtifactFallback(ctx context.Context, runID int64, runOutputDir string, params concurrentRunDownloadParams) {
	logsOrchestratorLog.Printf("evals not found in usage artifact for run %d, attempting fallback download of dedicated evals artifact", runID)
	evalsFilter := []string{constants.EvalsArtifactName.String()}
	err := waitForConfiguredRateLimit(ctx, params.verbose, params.maxGitHubAPIRateLimit, 1, params.rateLimitState)
	if err == nil {
		err = downloadRunArtifacts(ctx, downloadArtifactsOptions{runID: runID, outputDir: runOutputDir, verbose: params.verbose, owner: params.dlOwner, repo: params.dlRepo, hostname: params.dlHost, artifactFilter: evalsFilter})
	}
	if err != nil {
		logsOrchestratorLog.Printf("Fallback evals artifact download failed for run %d: %v", runID, err)
		if params.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Evals not found in usage artifact for run %d and fallback download failed: %v", runID, err)))
		}

	} else {
		logsOrchestratorLog.Printf("Fallback evals artifact downloaded for run %d", runID)
	}
}

func waitForConfiguredRateLimit(ctx context.Context, verbose bool, configuredMax, reserve int, state *logsRateLimitState) error {
	if configuredMax == 0 {
		return nil
	}
	return state.check(ctx, verbose, configuredMax, reserve)
}
