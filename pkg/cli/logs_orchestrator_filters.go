// This file provides command-line interface functionality for gh-aw.
// This file (logs_orchestrator_filters.go) contains run-filter helpers for the
// logs orchestrator: deciding whether a downloaded run should be included in
// results and constructing a ProcessedRun from a DownloadResult.

package cli

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
)

// runFilterOpts bundles the filter flags passed to applyRunFilters.
type runFilterOpts struct {
	engine            string
	runtime           string
	noStaged          bool
	firewallOnly      bool
	noFirewall        bool
	safeOutputType    string
	filteredIntegrity bool
	evalsOnly         bool
	gradersOnly       bool
}

var fetchJobStatusesForProcessedRun = fetchJobStatuses

// matchEngineFilter checks whether the run recorded in awInfo matches the
// requested engine filter string.  It returns (matches, detectedEngineID).
// detectedEngineID is "" when awInfo is unavailable or carries no engine_id.
func matchEngineFilter(awInfo *AwInfo, awInfoErr error, filterEngine string) (bool, string) {
	if awInfoErr != nil || awInfo == nil || awInfo.EngineID == "" {
		return false, ""
	}
	return awInfo.EngineID == filterEngine, awInfo.EngineID
}

// matchRuntimeFilter checks whether the run recorded in awInfo matches the
// requested sandbox agent runtime filter string (e.g., "cloud-hypervisor").
// It returns (matches, detectedRuntime). detectedRuntime is "" when awInfo is
// unavailable or carries no agent_runtime.
func matchRuntimeFilter(awInfo *AwInfo, awInfoErr error, filterRuntime string) (bool, string) {
	if awInfoErr != nil || awInfo == nil || awInfo.AgentRuntime == "" {
		return false, ""
	}
	return awInfo.AgentRuntime == filterRuntime, awInfo.AgentRuntime
}

// applyRunFilters applies all configured run filters to a DownloadResult.
// It parses aw_info.json once (lazily) when any filter that needs it is active.
// Returns true when the run should be skipped / excluded from results.
func applyRunFilters(ctx context.Context, result DownloadResult, opts runFilterOpts, verbose bool) bool {
	var awInfo *AwInfo
	var awInfoErr error
	if opts.engine != "" || opts.runtime != "" || opts.noStaged || opts.firewallOnly || opts.noFirewall {
		awInfoPath := filepath.Join(result.LogsPath, "aw_info.json")
		awInfo, awInfoErr = parseAwInfo(awInfoPath, verbose)
	}

	return skipByEngineFilter(result, opts, awInfo, awInfoErr, verbose) ||
		skipByRuntimeFilter(result, opts, awInfo, awInfoErr, verbose) ||
		skipByStagedFilter(result, opts, awInfo, awInfoErr, verbose) ||
		skipByFirewallFilter(result, opts, awInfo, awInfoErr, verbose) ||
		skipBySafeOutputFilter(result, opts, verbose) ||
		skipByFilteredIntegrityFilter(result, opts, verbose) ||
		skipByEvalsFilter(ctx, result, opts, verbose) ||
		skipByGradersFilter(result, opts, verbose)
}

func skipByEngineFilter(result DownloadResult, opts runFilterOpts, awInfo *AwInfo, awInfoErr error, verbose bool) bool {
	if opts.engine == "" {
		return false
	}
	engineMatches, detectedEngineID := matchEngineFilter(awInfo, awInfoErr, opts.engine)
	if engineMatches {
		return false
	}
	if detectedEngineID == "" {
		detectedEngineID = "unknown"
	}
	logsOrchestratorLog.Printf("Skipping run %d: engine filter=%s, detected=%s", result.Run.DatabaseID, opts.engine, detectedEngineID)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: engine '%s' does not match filter '%s'", result.Run.DatabaseID, detectedEngineID, opts.engine)))
	}
	return true
}

func skipByRuntimeFilter(result DownloadResult, opts runFilterOpts, awInfo *AwInfo, awInfoErr error, verbose bool) bool {
	if opts.runtime == "" {
		return false
	}
	runtimeMatches, detectedRuntime := matchRuntimeFilter(awInfo, awInfoErr, opts.runtime)
	if runtimeMatches {
		return false
	}
	if detectedRuntime == "" {
		detectedRuntime = "unknown"
	}
	logsOrchestratorLog.Printf("Skipping run %d: runtime filter=%s, detected=%s", result.Run.DatabaseID, opts.runtime, detectedRuntime)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: runtime '%s' does not match filter '%s'", result.Run.DatabaseID, detectedRuntime, opts.runtime)))
	}
	return true
}

func skipByStagedFilter(result DownloadResult, opts runFilterOpts, awInfo *AwInfo, awInfoErr error, verbose bool) bool {
	if !opts.noStaged {
		return false
	}
	isStaged := awInfoErr == nil && awInfo != nil && awInfo.Staged
	if !isStaged {
		return false
	}
	logsOrchestratorLog.Printf("Skipping run %d: staged workflow filtered by --exclude-staged", result.Run.DatabaseID)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: workflow is staged (filtered out by --exclude-staged)", result.Run.DatabaseID)))
	}
	return true
}

func skipByFirewallFilter(result DownloadResult, opts runFilterOpts, awInfo *AwInfo, awInfoErr error, verbose bool) bool {
	if !opts.firewallOnly && !opts.noFirewall {
		return false
	}
	hasFirewall := awInfoErr == nil && awInfo != nil && awInfo.Steps.Firewall != ""
	if opts.firewallOnly && !hasFirewall {
		logAndMaybeExplainSkip(result.Run.DatabaseID, "no firewall detected, filtered by --firewall", "workflow does not use firewall (filtered by --firewall)", verbose)
		return true
	}
	if opts.noFirewall && hasFirewall {
		logAndMaybeExplainSkip(result.Run.DatabaseID, "firewall detected, filtered by --no-firewall", "workflow uses firewall (filtered by --no-firewall)", verbose)
		return true
	}
	return false
}

func skipBySafeOutputFilter(result DownloadResult, opts runFilterOpts, verbose bool) bool {
	if opts.safeOutputType == "" {
		return false
	}
	hasSafeOutputType, checkErr := runContainsSafeOutputType(result.LogsPath, opts.safeOutputType, verbose)
	if checkErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to check safe output type for run %d: %v", result.Run.DatabaseID, checkErr)))
	}
	if hasSafeOutputType {
		return false
	}
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: no '%s' safe output messages found", result.Run.DatabaseID, opts.safeOutputType)))
	}
	return true
}

func skipByFilteredIntegrityFilter(result DownloadResult, opts runFilterOpts, verbose bool) bool {
	if !opts.filteredIntegrity {
		return false
	}
	hasFiltered, checkErr := runHasDifcFilteredItems(result.LogsPath, verbose)
	if checkErr != nil {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to check DIFC filtered items for run %d: %v", result.Run.DatabaseID, checkErr)))
		return true
	}
	if hasFiltered {
		return false
	}
	logAndMaybeExplainSkip(result.Run.DatabaseID, "no DIFC filtered items found", "no DIFC integrity-filtered items found in gateway logs", verbose)
	return true
}

func skipByEvalsFilter(ctx context.Context, result DownloadResult, opts runFilterOpts, verbose bool) bool {
	if !opts.evalsOnly {
		return false
	}
	if runHasEvals(result.LogsPath, verbose) || ensureEvalsResultsFromBranch(ctx, result.Run, result.LogsPath, "", "", "", verbose) {
		return false
	}
	logAndMaybeExplainSkip(result.Run.DatabaseID, "no evals results found, filtered by --evals", "workflow does not have evals results (filtered by --evals)", verbose)
	return true
}

func skipByGradersFilter(result DownloadResult, opts runFilterOpts, verbose bool) bool {
	if !opts.gradersOnly || runHasGraders(result.LogsPath) {
		return false
	}
	logAndMaybeExplainSkip(result.Run.DatabaseID, "no grader results found, filtered by --graders", "workflow does not have grader results (filtered by --graders)", verbose)
	return true
}

func logAndMaybeExplainSkip(runID int64, logReason, message string, verbose bool) {
	logsOrchestratorLog.Printf("Skipping run %d: %s", runID, logReason)
	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: %s", runID, message)))
	}
}

// buildProcessedRun constructs a ProcessedRun from a DownloadResult, computing
// duration, action minutes, and job-failure counts.
func buildProcessedRun(ctx context.Context, result DownloadResult, verbose, logFailedJobs bool) ProcessedRun {
	run := result.Run
	run.TokenUsage = result.Metrics.TokenUsage
	applyMetricsTurnsToRun(&run, result.Metrics)
	run.AvgTimeBetweenTurns = result.Metrics.AvgTimeBetweenTurns
	run.ErrorCount = 0
	run.WarningCount = 0
	run.LogsPath = result.LogsPath

	// Add failed jobs to error count.
	if failedJobCount, err := fetchJobStatusesForProcessedRun(ctx, run.DatabaseID, verbose); err == nil {
		run.ErrorCount += failedJobCount
		if verbose && logFailedJobs && failedJobCount > 0 {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Added %d failed jobs to error count for run %d", failedJobCount, run.DatabaseID)))
		}
	}

	// Always use GitHub API timestamps for duration calculation.
	// GitHub Actions bills per minute, rounded up per job.
	if !run.StartedAt.IsZero() && !run.UpdatedAt.IsZero() {
		run.Duration = run.UpdatedAt.Sub(run.StartedAt)
		run.ActionMinutes = math.Ceil(run.Duration.Minutes())
	}

	return ProcessedRun{
		Run:                     run,
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
		MCPToolUsage:            result.MCPToolUsage,
		TokenUsage:              result.TokenUsage,
		WorkingSet:              result.WorkingSet,
		GitHubRateLimitUsage:    result.GitHubRateLimitUsage,
		JobDetails:              result.JobDetails,
		SafeOutputs:             result.SafeOutputs,
	}
}
