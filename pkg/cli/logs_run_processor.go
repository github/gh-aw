// This file provides command-line interface functionality for gh-aw.
// This file (logs_run_processor.go) contains the concurrent run artifact
// download and run filtering logic.
//
// Key responsibilities:
//   - Concurrent downloading of artifacts from multiple runs
//   - Filtering runs by safe output type, DIFC filtered items, etc.

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/sourcegraph/conc/pool"
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
	evalsOnly             bool
	storageLimit          *logsStorageLimit
	maxGitHubAPIRateLimit int
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
}

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

	// Configure concurrent download pool with bounded parallelism and context cancellation.
	// The conc pool automatically handles panic recovery and prevents goroutine leaks.
	// Results are written into a slot pre-allocated per run rather than collected via
	// pool.NewWithResults, whose completion order is not guaranteed to match submission
	// order. Preserving submission (API) order lets storage/rate-limit continuation
	// cursors rely on the position of the oldest run in the batch even though downloads
	// still run concurrently (the storage limiter itself remains the throughput gate).
	results := make([]DownloadResult, len(runs))
	p := pool.New().
		WithContext(ctx).
		WithMaxGoroutines(maxConcurrent)

	// Each download task runs concurrently with context awareness.
	for i, run := range runs {
		p.Go(func(ctx context.Context) error {
			result, _ := processSingleRunDownload(ctx, run, params, &completedCount, progressBar)
			results[i] = result
			return nil
		})
	}

	if err := p.Wait(); err != nil && opts.verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Download interrupted: %v", err)))
	}
	if progressBar != nil {
		console.ClearLine()
	}
	if opts.verbose {
		logConcurrentDownloadSummary(results)
	}
	logsOrchestratorLog.Printf("Concurrent download complete: total=%d, results=%d", totalRuns, len(results))
	return results
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
		if isFailureConclusion(run.Conclusion) {
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
	if err != nil {
		handleArtifactDownloadError(result, err, params.verbose)
	} else {
		if !ok {
			logsOrchestratorLog.Printf("Downloading artifacts for run %d: owner=%s, repo=%s", run.DatabaseID, perRunParams.dlOwner, perRunParams.dlRepo)
			err := params.storageLimit.runDownloadDeferredReserved(ctx, runOutputDir, func() error {
				if err := waitForConfiguredRateLimit(ctx, params.verbose, params.maxGitHubAPIRateLimit, logsRunPreflightAPIReserve); err != nil {
					return err
				}
				if err := os.MkdirAll(runOutputDir, constants.DirPermSensitive); err != nil {
					return fmt.Errorf("failed to create run output directory: %w", err)
				}
				if metadata, err := fetchAndCacheWorkflowRunMetadata(ctx, run, runOutputDir, perRunParams.dlOwner, perRunParams.dlRepo, perRunParams.dlHost, params.verbose); err != nil {
					logsOrchestratorLog.Printf("Failed to fetch workflow run metadata for run %d: %v", run.DatabaseID, err)
				} else {
					applyWorkflowRunMetadata(&result.Run, metadata)
				}
				if err := downloadRunArtifacts(ctx, downloadArtifactsOptions{runID: run.DatabaseID, outputDir: runOutputDir, verbose: params.verbose, owner: perRunParams.dlOwner, repo: perRunParams.dlRepo, hostname: perRunParams.dlHost, artifactFilter: params.artifactFilter}); err != nil {
					return err
				}
				// When evals are requested but not found in the usage artifact (older runs
				// that predate the conclusion-job copy), fall back to the dedicated evals
				// artifact so those runs are not silently skipped. This applies both when
				// --evals is set and when --artifacts evals was explicitly listed.
				if params.evalsArtifactRequested && !runHasEvals(runOutputDir, params.verbose) {
					tryDownloadEvalsArtifactFallback(ctx, run.DatabaseID, runOutputDir, perRunParams)
				}
				analyzeRunArtifacts(ctx, result, runOutputDir, params.verbose, params.artifactFilter)
				return nil
			})

			if err != nil {
				handleArtifactDownloadError(result, err, params.verbose)
			}
		} else {
			logsOrchestratorLog.Printf("Cache hit for run %d, using cached summary", run.DatabaseID)
		}
	}

	completed := completedCount.Add(1)
	if progressBar != nil {
		fmt.Fprintf(os.Stderr, "Processing runs: %s\r", progressBar.Update(completed))
	}
	return *result, nil
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
	err := waitForConfiguredRateLimit(ctx, params.verbose, params.maxGitHubAPIRateLimit, 1)
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

func waitForConfiguredRateLimit(ctx context.Context, verbose bool, configuredMax, reserve int) error {
	if configuredMax == 0 {
		return nil
	}
	return checkAndWaitForRateLimitShared(ctx, verbose, configuredMax, reserve)
}

// tryLoadCachedRunResult attempts to return a pre-built DownloadResult from the on-disk
// cache.  Returns (result, true) on a valid cache hit; (nil, false) otherwise.
func tryLoadCachedRunResult(
	ctx context.Context,
	run WorkflowRun,
	runOutputDir string,
	params concurrentRunDownloadParams,
) (*DownloadResult, bool) {
	summary, ok := loadRunSummary(runOutputDir, params.verbose)
	if !ok {
		return nil, false
	}
	if len(params.artifactFilter) == 0 {
		if missing := findMissingFilterEntries([]string{string(ArtifactSetAll)}, runOutputDir); len(missing) > 0 {
			logsOrchestratorLog.Printf("Cache bypass for run %d: complete artifact marker missing", run.DatabaseID)
			return nil, false
		}
	} else {
		if missing := findMissingFilterEntries(params.artifactFilter, runOutputDir); len(missing) > 0 {
			logsOrchestratorLog.Printf("Cache bypass for run %d: requested artifacts missing locally: %v", run.DatabaseID, missing)
			return nil, false
		}
	}

	// When --evals is requested but evals are not present in the cached run directory
	// (e.g., the run was cached before evals were included in the usage artifact),
	// bypass the cache so the fresh download can include the usage artifact with evals;
	// the post-download filter decides whether to skip.
	hasEvals := runHasEvals(runOutputDir, params.verbose) ||
		ensureEvalsResultsFromBranch(ctx, summary.Run, runOutputDir, params.dlOwner, params.dlRepo, params.dlHost, params.verbose)
	if params.evalsArtifactRequested && !hasEvals {
		logsOrchestratorLog.Printf("Cache bypass for run %d: evals requested (--evals or --artifacts evals) but not present locally", run.DatabaseID)
		return nil, false
	}

	result := DownloadResult{
		RunAnalysis: summary.RunAnalysis,
		LogsPath:    runOutputDir,
		Cached:      true,
	}
	metadataApplied := refreshCachedRunMetadata(ctx, &result.Run, runOutputDir, run, params)
	// Re-apply the usage activity backfill to heal stale cache entries.
	// Capture the SafeItemsCount before backfill to detect whether the field was healed.
	safeItemsBefore := result.Run.SafeItemsCount
	activitySummaryApplied := backfillCacheHitIfNeeded(&result, runOutputDir, params.verbose)
	// If the backfill populated SafeItemsCount (i.e. it was 0 before and is now non-zero),
	// persist the healed value back to run_summary.json so downstream readers (e.g.
	// the api-consumption-report) see the correct count without having to fall back to
	// usage/activity/summary.json.
	if result.Run.SafeItemsCount != safeItemsBefore || activitySummaryApplied || metadataApplied {
		healed := *summary
		healed.Run = result.Run
		healed.Metrics = result.Metrics
		healed.MCPToolUsage = result.MCPToolUsage
		healed.WorkingSet = result.WorkingSet
		if err := saveRunSummary(runOutputDir, &healed, params.verbose); err != nil {
			logsOrchestratorLog.Printf("Warning: failed to persist healed run summary for run %d: %v", result.Run.DatabaseID, err)
		}
	}
	return &result, true
}

func refreshCachedRunMetadata(ctx context.Context, cachedRun *WorkflowRun, runOutputDir string, run WorkflowRun, params concurrentRunDownloadParams) bool {
	needsRefresh, err := workflowRunMetadataCacheNeedsRefresh(runOutputDir, run, params.dlOwner, params.dlRepo)
	if err == nil && needsRefresh {
		err = waitForConfiguredRateLimit(ctx, params.verbose, params.maxGitHubAPIRateLimit, 1)
	}
	if err != nil {
		logsOrchestratorLog.Printf("Failed to refresh cached workflow run metadata for run %d: %v", run.DatabaseID, err)
		return false
	}
	metadata, err := fetchAndCacheWorkflowRunMetadata(ctx, run, runOutputDir, params.dlOwner, params.dlRepo, params.dlHost, params.verbose)
	if err != nil {
		logsOrchestratorLog.Printf("Failed to refresh cached workflow run metadata for run %d: %v", run.DatabaseID, err)
		return false
	}
	return applyWorkflowRunMetadata(cachedRun, metadata)
}

// analyzeRunArtifacts populates a DownloadResult with all analysis data derived from
// freshly-downloaded artifacts in runOutputDir.  Called only when the download succeeded
// and no valid cached summary was found.
func analyzeRunArtifacts(ctx context.Context, result *DownloadResult, runOutputDir string, verbose bool, artifactFilter []string) {
	metrics := extractRunMetricsAndMetadata(result, runOutputDir, verbose)

	usageActivitySummary, usageActivityErr := loadUsageActivitySummary(runOutputDir)
	if usageActivityErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to read usage activity summary for run %d: %v", result.Run.DatabaseID, usageActivityErr)))
	}

	// Firewall artifact gating: firewall/gateway logs live in the agent artifact.
	// Skip silently when the artifact was intentionally excluded from the filter.
	hasFirewallArtifact := artifactMatchesFilter(constants.AgentArtifactName.String(), artifactFilter)

	applyRunSecurityAnalysis(result, runOutputDir, verbose, hasFirewallArtifact)

	// Resolve experiment assignment once for all extraction functions below.
	expName, expVariant, _ := firstExperimentAssignment(extractExperimentData(runOutputDir))

	applyRunBehavioralSignals(result, runOutputDir, verbose, hasFirewallArtifact, expName, expVariant)

	applyRunUsageMetrics(result, &metrics, runOutputDir, verbose, usageActivitySummary, hasFirewallArtifact)

	finalizeAndSaveRunSummary(ctx, result, runOutputDir, metrics, verbose)
}

// extractRunMetricsAndMetadata extracts log metrics, infers missing workflow path, and
// computes duration.  It populates the matching fields on result.Run and returns the
// LogMetrics so callers can pass them to functions that need them (e.g. agentic analysis).
func extractRunMetricsAndMetadata(result *DownloadResult, runOutputDir string, verbose bool) LogMetrics {
	metrics, metricsErr := extractLogMetrics(runOutputDir, verbose)
	if metricsErr != nil {
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract metrics for run %d: %v", result.Run.DatabaseID, metricsErr)))
		}
		metrics = LogMetrics{}
	}
	result.Metrics = metrics
	// Update run with metrics so fingerprint computation uses the same data as the audit tool.
	result.Run.TokenUsage = metrics.TokenUsage
	result.Run.Turns = metrics.Turns
	result.Run.TurnsAvailable = (metricsErr == nil)
	result.Run.AvgTimeBetweenTurns = metrics.AvgTimeBetweenTurns
	result.Run.LogsPath = runOutputDir

	// If the GitHub API returned an empty workflow path (e.g. for scheduled or agentic
	// workflow runs), infer it from aw_info.json so the cached RunSummary and downstream
	// consumers have a usable identifier.
	if result.Run.WorkflowPath == "" {
		awInfoPath := filepath.Join(runOutputDir, "aw_info.json")
		if info, err := parseAwInfo(awInfoPath, false); err == nil && info != nil && info.WorkflowName != "" {
			result.Run.WorkflowPath = inferWorkflowPathFromDisplayName(info.WorkflowName)
		}
	}

	// Calculate duration and billable minutes from GitHub API timestamps.
	// This mirrors the identical computation in audit.go for consistency.
	if !result.Run.StartedAt.IsZero() && !result.Run.UpdatedAt.IsZero() {
		result.Run.Duration = result.Run.UpdatedAt.Sub(result.Run.StartedAt)
		result.Run.ActionMinutes = math.Ceil(result.Run.Duration.Minutes())
	}
	return metrics
}

// applyRunSecurityAnalysis runs access-log, firewall, and redacted-domain analyses and
// stores the results directly on result.
func applyRunSecurityAnalysis(result *DownloadResult, runOutputDir string, verbose bool, hasFirewallArtifact bool) {
	accessAnalysis, accessErr := analyzeAccessLogs(runOutputDir, verbose)
	if accessErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze access logs for run %d: %v", result.Run.DatabaseID, accessErr)))
	}
	result.AccessAnalysis = accessAnalysis

	var firewallAnalysis *FirewallAnalysis
	if hasFirewallArtifact {
		var firewallErr error
		firewallAnalysis, firewallErr = analyzeFirewallLogs(runOutputDir, verbose)
		if firewallErr != nil && verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze firewall logs for run %d: %v", result.Run.DatabaseID, firewallErr)))
		}
	}
	result.FirewallAnalysis = firewallAnalysis

	redactedDomainsAnalysis, redactedErr := analyzeRedactedDomains(runOutputDir, verbose)
	if redactedErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze redacted domains for run %d: %v", result.Run.DatabaseID, redactedErr)))
	}
	result.RedactedDomainsAnalysis = redactedDomainsAnalysis
}

// applyRunBehavioralSignals extracts missing-tool, missing-data, noop, MCP failure, and
// MCP tool-usage signals and stores them directly on result.
func applyRunBehavioralSignals(result *DownloadResult, runOutputDir string, verbose bool, hasFirewallArtifact bool, expName, expVariant string) {
	missingTools, missingErr := extractMissingToolsFromRun(runOutputDir, result.Run, verbose, expName, expVariant)
	if missingErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract missing tools for run %d: %v", result.Run.DatabaseID, missingErr)))
	}
	result.MissingTools = missingTools

	missingData, missingDataErr := extractMissingDataFromRun(runOutputDir, result.Run, verbose, expName, expVariant)
	if missingDataErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract missing data for run %d: %v", result.Run.DatabaseID, missingDataErr)))
	}
	result.MissingData = missingData

	noops, noopErr := extractNoopsFromRun(runOutputDir, result.Run, verbose, expName, expVariant)
	if noopErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract noops for run %d: %v", result.Run.DatabaseID, noopErr)))
	}
	result.Noops = noops

	mcpFailures, mcpErr := extractMCPFailuresFromRun(runOutputDir, result.Run, verbose, expName, expVariant)
	if mcpErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract MCP failures for run %d: %v", result.Run.DatabaseID, mcpErr)))
	}
	result.MCPFailures = mcpFailures

	skillActivations, skillErr := extractSkillActivationsFromRun(runOutputDir, result.Run, verbose, expName, expVariant)
	if skillErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract skill activations for run %d: %v", result.Run.DatabaseID, skillErr)))
	}
	result.SkillActivations = skillActivations

	// MCP tool usage data lives in gateway.jsonl which is part of the agent artifact.
	var mcpToolUsage *MCPToolUsageData
	if hasFirewallArtifact {
		var mcpToolErr error
		mcpToolUsage, mcpToolErr = extractMCPToolUsageData(runOutputDir, verbose)
		if mcpToolErr != nil && verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract MCP tool usage for run %d: %v", result.Run.DatabaseID, mcpToolErr)))
		}
	}
	result.MCPToolUsage = mcpToolUsage
}

// applyRunUsageMetrics extracts token usage, GitHub rate-limit consumption, safe-output
// item counts, and backfills any missing activity summaries from the usage artifact.
func applyRunUsageMetrics(result *DownloadResult, metrics *LogMetrics, runOutputDir string, verbose bool, usageActivitySummary *usageActivitySummary, hasFirewallArtifact bool) {
	// token-usage.jsonl is also available in the compact usage artifact.
	tokenUsage, tokenErr := analyzeTokenUsage(runOutputDir, verbose)
	if tokenErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze token usage for run %d: %v", result.Run.DatabaseID, tokenErr)))
	}
	result.TokenUsage = tokenUsage
	backfillRunTokenUsageFromFirewall(metrics, result, tokenUsage)
	if tokenUsage != nil && tokenUsage.TotalEffectiveTokens > 0 {
		result.Run.EffectiveTokens = tokenUsage.TotalEffectiveTokens
	}

	rateLimitUsage, rlErr := analyzeGitHubRateLimits(runOutputDir, verbose)
	if rlErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze GitHub rate limit usage for run %d: %v", result.Run.DatabaseID, rlErr)))
	}
	result.GitHubRateLimitUsage = rateLimitUsage

	// Count safe output items created in GitHub (from manifest artifact).
	// This runs before applyUsageActivitySummaryToResult so that the summary
	// backfill only activates when the manifest returned zero items.
	result.Run.SafeItemsCount = len(extractCreatedItemsFromManifest(runOutputDir))

	// Fill missing activity summaries from usage artifact precomputes.
	// This call is unconditional but only backfills fields that are still empty.
	applyUsageActivitySummaryToResult(usageActivitySummary, result, !hasFirewallArtifact)
}

// finalizeAndSaveRunSummary fetches job details, derives the agentic analysis, builds the
// RunSummary struct, and writes it to disk.  It also sets the agentic-analysis fields on
// result directly so they are available to the caller.
func finalizeAndSaveRunSummary(ctx context.Context, result *DownloadResult, runOutputDir string, metrics LogMetrics, verbose bool) {
	jobDetails, jobErr := fetchJobDetails(ctx, result.Run.DatabaseID, runOutputDir, verbose)
	if jobErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to fetch job details for run %d: %v", result.Run.DatabaseID, jobErr)))
	}
	if jobDetails != nil {
		result.JobDetails = jobDetails
	}

	artifacts, listErr := listArtifacts(runOutputDir)
	if listErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to list artifacts for run %d: %v", result.Run.DatabaseID, listErr)))
	}

	processedRun := ProcessedRun{
		Run:                     result.Run,
		AccessAnalysis:          result.AccessAnalysis,
		FirewallAnalysis:        result.FirewallAnalysis,
		RedactedDomainsAnalysis: result.RedactedDomainsAnalysis,
		MissingTools:            result.MissingTools,
		MissingData:             result.MissingData,
		Noops:                   result.Noops,
		MCPFailures:             result.MCPFailures,
		SkillActivations:        result.SkillActivations,
		MCPToolUsage:            result.MCPToolUsage,
		TokenUsage:              result.TokenUsage,
		WorkingSet:              result.WorkingSet,
		GitHubRateLimitUsage:    result.GitHubRateLimitUsage,
		JobDetails:              jobDetails,
	}
	awContext, _, _, taskDomain, behaviorFingerprint, agenticAssessments := deriveRunAgenticAnalysis(processedRun, metrics)
	result.AwContext = awContext
	result.TaskDomain = taskDomain
	result.BehaviorFingerprint = behaviorFingerprint
	result.AgenticAssessments = agenticAssessments

	summary := newRunSummary(result, metrics, jobDetails, artifacts)
	if saveErr := saveRunSummary(runOutputDir, summary, verbose); saveErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to save run summary for run %d: %v", result.Run.DatabaseID, saveErr)))
	}
}

func newRunSummary(result *DownloadResult, metrics LogMetrics, jobDetails []JobInfoWithDuration, artifacts []string) *RunSummary {
	return &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       result.Run.DatabaseID,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run:                     result.Run,
			Metrics:                 metrics,
			AwContext:               result.AwContext,
			TaskDomain:              result.TaskDomain,
			BehaviorFingerprint:     result.BehaviorFingerprint,
			AgenticAssessments:      result.AgenticAssessments,
			AccessAnalysis:          result.AccessAnalysis,
			FirewallAnalysis:        result.FirewallAnalysis,
			RedactedDomainsAnalysis: result.RedactedDomainsAnalysis,
			MissingTools:            result.MissingTools,
			MissingData:             result.MissingData,
			Noops:                   result.Noops,
			MCPFailures:             result.MCPFailures,
			SkillActivations:        result.SkillActivations,
			MCPToolUsage:            result.MCPToolUsage,
			TokenUsage:              result.TokenUsage,
			WorkingSet:              result.WorkingSet,
			GitHubRateLimitUsage:    result.GitHubRateLimitUsage,
			JobDetails:              jobDetails,
		},
		ArtifactsList: artifacts,
	}
}

// backfillCacheHitIfNeeded re-applies the usage activity summary backfill to heal
// stale cache entries that were saved before compact activity metrics were
// introduced. Errors loading the summary are logged when verbose is true; a missing
// summary file is silent (no summary = nothing to backfill). It returns whether an
// activity summary was applied so the caller can persist healed cache data.
func backfillCacheHitIfNeeded(result *DownloadResult, runOutputDir string, verbose bool) bool {
	backfillRunTokenUsageFromFirewall(&result.Metrics, result, result.TokenUsage)
	usageActivitySummary, err := loadUsageActivitySummary(runOutputDir)
	if err != nil && verbose {
		logsOrchestratorLog.Printf("Warning: failed to load usage activity summary for cache-hit backfill (run %d): %v", result.Run.DatabaseID, err)
	}
	if usageActivitySummary == nil {
		return false
	}
	applyUsageActivitySummaryToResult(usageActivitySummary, result, true)
	return true
}

func backfillRunTokenUsageFromFirewall(metrics *LogMetrics, result *DownloadResult, tokenUsage *TokenUsageSummary) {
	// Backfill run-level token count from the firewall proxy summary when the
	// event-log parser returned 0. This is common for AWF-based engines (Claude,
	// Codex, Gemini) where token counts live in the proxy's token-usage.jsonl
	// rather than in events.jsonl, so the raw log metrics miss them.
	// Using input+output (not cache) keeps semantics consistent with what
	// extractLogMetrics measures from events.jsonl.
	if metrics == nil || result == nil || tokenUsage == nil || metrics.TokenUsage != 0 || result.Run.TokenUsage != 0 {
		return
	}
	if firewallTokens := tokenUsage.TotalInputTokens + tokenUsage.TotalOutputTokens; firewallTokens > 0 {
		metrics.TokenUsage = firewallTokens
		result.Metrics.TokenUsage = firewallTokens
		result.Run.TokenUsage = firewallTokens
	}
}

// runContainsSafeOutputType checks if a run's agent_output.json contains a specific safe output type
func runContainsSafeOutputType(runDir string, safeOutputType string, verbose bool) (bool, error) {
	logsOrchestratorLog.Printf("Checking run for safe output type: dir=%s, type=%s", runDir, safeOutputType)
	// Normalize the type for comparison (convert dashes to underscores)
	normalizedType := stringutil.NormalizeSafeOutputIdentifier(safeOutputType)

	// Look for agent_output.json in the run directory
	agentOutputPath := filepath.Join(runDir, constants.AgentOutputFilename.String())

	// Support both new flattened form and old directory form
	if stat, err := os.Stat(agentOutputPath); err != nil || stat.IsDir() {
		// Try old structure
		oldPath := filepath.Join(runDir, constants.AgentOutputArtifactName.String(), constants.AgentOutputArtifactName.String())
		if fileutil.FileExists(oldPath) {
			agentOutputPath = oldPath
		} else {
			// No agent_output.json found
			return false, nil
		}
	}

	// Read the file
	content, err := os.ReadFile(agentOutputPath)
	if err != nil {
		// File doesn't exist or can't be read
		return false, nil
	}

	// Parse the JSON
	var safeOutput struct {
		Items []json.RawMessage `json:"items"`
	}

	if err := json.Unmarshal(content, &safeOutput); err != nil {
		return false, fmt.Errorf("could not parse agent_output.json, expected valid JSON with an \"items\" array: %w", err)
	}

	// Check each item for the specified type
	for _, itemRaw := range safeOutput.Items {
		var item struct {
			Type string `json:"type"`
		}

		if err := json.Unmarshal(itemRaw, &item); err != nil {
			continue // Skip malformed items
		}

		// Normalize the item type for comparison
		normalizedItemType := stringutil.NormalizeSafeOutputIdentifier(item.Type)

		if normalizedItemType == normalizedType {
			return true, nil
		}
	}

	return false, nil
}

// inferWorkflowPathFromDisplayName derives a best-effort workflow file path from a
// workflow display name when the GitHub API returns an empty path for the run.
//
// The inference converts the display name to a kebab-case slug using the same
// character-replacement rules as the JS sanitizeWorkflowName helper, then wraps it
// in the conventional lock-file path:
//
//	"Auto-Triage Issues"  →  ".github/workflows/auto-triage-issues.lock.yml"
//	"CI Failure Doctor"   →  ".github/workflows/ci-failure-doctor.lock.yml"
//
// The result is a best-effort guess and may not exactly match the actual file when
// the workflow filename was chosen independently of its display name. It is only used
// when no authoritative path is available.
func inferWorkflowPathFromDisplayName(displayName string) string {
	if displayName == "" {
		return ""
	}
	// Match sanitizeWorkflowName.cjs: lowercase, replace :[\/\s] with "-",
	// replace other non-identifier chars with "-", preserve "." and "_".
	slug := stringutil.SanitizeName(displayName, &stringutil.SanitizeOptions{
		PreserveSpecialChars: []rune{'.', '_'},
		TrimHyphens:          true,
	})
	if slug == "" {
		return ""
	}
	return constants.WorkflowsDirSlash + slug + ".lock.yml"
}

// runHasDifcFilteredItems checks if a run's gateway logs contain any DIFC_FILTERED events.
// It parses the gateway logs (falling back to rpc-messages.jsonl when gateway.jsonl is absent)
// and returns true when at least one DIFC integrity- or secrecy-filtered event is present.
func runHasDifcFilteredItems(runDir string, verbose bool) (bool, error) {
	logsOrchestratorLog.Printf("Checking run for DIFC filtered items: dir=%s", runDir)

	gatewayMetrics, err := parseGatewayLogs(runDir, verbose)
	if err != nil {
		// No gateway log file present — not an error for workflows without MCP
		return false, nil
	}

	if gatewayMetrics == nil {
		return false, nil
	}

	return gatewayMetrics.TotalFiltered > 0, nil
}

// runHasEvals checks whether a run's output directory contains an evals results file
// (evals.jsonl). It looks in five locations:
//  1. runDir/evals.jsonl — produced when flattenSingleFileArtifacts collapsed the
//     one-file evals artifact from its directory directly to the run root.
//  2. runDir/evals/evals.jsonl — un-flattened artifact directory.
//  3. runDir/{hash}-evals/evals.jsonl — workflow_call hash-prefixed variant.
//  4. runDir/usage/evals.jsonl — compact usage artifact captured by the conclusion job.
//  5. runDir/{hash}-usage/evals.jsonl — workflow_call hash-prefixed compact usage artifact.
func runHasEvals(runDir string, verbose bool) bool {
	logsOrchestratorLog.Printf("Checking run for evals results: dir=%s", runDir)

	// Case 1: flattenSingleFileArtifacts moved the file directly to the run root.
	rootEvalsFile := filepath.Join(runDir, constants.EvalsResultFilename.String())
	if fileutil.FileExists(rootEvalsFile) {
		logsOrchestratorLog.Printf("Found evals results at: %s", rootEvalsFile)
		return true
	}
	usageEvalsFile := filepath.Join(runDir, constants.UsageArtifactName.String(), constants.EvalsResultFilename.String())
	if fileutil.FileExists(usageEvalsFile) {
		logsOrchestratorLog.Printf("Found evals results in usage artifact at: %s", usageEvalsFile)
		return true
	}

	entries, err := os.ReadDir(runDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Match exact "evals" or workflow_call prefixed "{hash}-evals".
		if name == constants.EvalsArtifactName.String() || strings.HasSuffix(name, "-"+constants.EvalsArtifactName.String()) {
			evalsFile := filepath.Join(runDir, name, constants.EvalsResultFilename.String())
			if fileutil.FileExists(evalsFile) {
				logsOrchestratorLog.Printf("Found evals results at: %s", evalsFile)
				return true
			}
		}
		// Match workflow_call-prefixed "{hash}-usage".
		if strings.HasSuffix(name, "-"+constants.UsageArtifactName.String()) {
			evalsFile := filepath.Join(runDir, name, constants.EvalsResultFilename.String())
			if fileutil.FileExists(evalsFile) {
				logsOrchestratorLog.Printf("Found evals results in workflow_call usage artifact at: %s", evalsFile)
				return true
			}
		}
	}

	if verbose {
		logsOrchestratorLog.Printf("No evals results found in: %s", runDir)
	}
	return false
}
