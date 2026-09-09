package cli

// audit_run_pipeline.go: end-to-end orchestration of fetching, caching, and
// preparing a single workflow run for analysis.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/fileutil"
)

// AuditWorkflowRun audits a single workflow run and generates a report
// If jobID is provided (>0), focuses audit on that specific job
// If stepNumber is provided (>0), extracts output for that specific step
// If experimentFilter is non-empty, the run is skipped when its experiment artifact does
// not contain an assignment for that experiment name. If variantFilter is also non-empty,
// the assigned variant must equal variantFilter.
func AuditWorkflowRun(ctx context.Context, runID int64, opts AuditOptions) error {
	cfg, err := newAuditRunConfig(runID, opts)
	if err != nil {
		return err
	}
	if err := ensureAuditNotCancelled(ctx); err != nil {
		return err
	}
	announceAuditRun(cfg)
	if cfg.jobID > 0 {
		return auditJobRun(cfg.jobOptions())
	}
	if done, err := renderCachedAuditIfAvailable(ctx, cfg); done {
		return err
	}
	run, err := prepareAuditWorkflowRun(ctx, cfg)
	if err != nil {
		return err
	}
	results, err := collectAuditAnalysisResults(ctx, run, cfg.outputDir, cfg.verbose, artifactMatchesFilter(constants.AgentArtifactName.String(), cfg.artifactFilter))
	if err != nil {
		return err
	}
	run = applyAuditMetrics(run, results)
	processedRun := buildProcessedAuditRun(run, results)
	saveAuditRunSummary(cfg.outputDir, run, processedRun, results, cfg.verbose)
	if shouldSkipAuditRun(cfg.runID, cfg.outputDir, cfg.experimentFilter, cfg.variantFilter, cfg.runtimeFilter) {
		return nil
	}
	if shouldSkipForEvals(ctx, cfg, run) {
		return nil
	}
	return renderAuditReport(ctx, processedRun, results.metrics, results.mcpToolUsage, cfg.auditOptions())
}

func newAuditRunConfig(runID int64, opts AuditOptions) (auditRunConfig, error) {
	if err := ValidateArtifactSets(opts.ArtifactSets); err != nil {
		return auditRunConfig{}, err
	}
	return auditRunConfig{
		runID:                  runID,
		owner:                  opts.Owner,
		repo:                   opts.Repo,
		hostname:               resolveAuditHostname(opts.Hostname),
		outputDir:              resolveAuditOutputDir(opts.OutputDir, runID),
		verbose:                opts.Verbose,
		parse:                  opts.Parse,
		jsonOutput:             opts.JSONOutput,
		jobID:                  opts.JobID,
		stepNumber:             opts.StepNumber,
		artifactFilter:         ResolveArtifactFilter(opts.ArtifactSets),
		experimentFilter:       opts.ExperimentFilter,
		variantFilter:          opts.VariantFilter,
		runtimeFilter:          opts.RuntimeFilter,
		evalsOnly:              opts.EvalsOnly,
		evalsArtifactRequested: isEvalsArtifactRequested(opts.EvalsOnly, opts.ArtifactSets),
	}, nil
}

func resolveAuditHostname(hostname string) string {
	if hostname == "" {
		hostname = getHostFromOriginRemote()
		if hostname != "github.com" {
			auditLog.Printf("Auto-detected GHES host from git remote: %s", hostname)
		}
	}
	return hostname
}

func resolveAuditOutputDir(outputDir string, runID int64) string {
	runOutputDir := filepath.Join(outputDir, fmt.Sprintf("run-%d", runID))
	if absDir, err := filepath.Abs(runOutputDir); err == nil {
		return absDir
	} else {
		auditLog.Printf("Failed to resolve absolute path for output directory %q: %v", runOutputDir, err)
	}
	return runOutputDir
}

func ensureAuditNotCancelled(ctx context.Context) error {
	select {
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Operation cancelled"))
		return ctx.Err()
	default:
		return nil
	}
}

func announceAuditRun(cfg auditRunConfig) {
	auditLog.Printf("Starting audit for workflow run: runID=%d, owner=%s, repo=%s, hostname=%s, jobID=%d, stepNumber=%d", cfg.runID, cfg.owner, cfg.repo, cfg.hostname, cfg.jobID, cfg.stepNumber)
	if len(cfg.artifactFilter) > 0 {
		auditLog.Printf("Artifact filter active: %v", cfg.artifactFilter)
		if cfg.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Artifact filter: downloading only "+strings.Join(cfg.artifactFilter, ", ")))
		}
	}
	if !cfg.verbose {
		return
	}
	if cfg.jobID > 0 && cfg.stepNumber > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Auditing workflow run %d, job %d, step %d...", cfg.runID, cfg.jobID, cfg.stepNumber)))
		return
	}
	if cfg.jobID > 0 {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Auditing workflow run %d, job %d...", cfg.runID, cfg.jobID)))
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Auditing workflow run %d...", cfg.runID)))
}

func (cfg auditRunConfig) jobOptions() auditJobRunOptions {
	return auditJobRunOptions{
		runID:      cfg.runID,
		jobID:      cfg.jobID,
		stepNumber: cfg.stepNumber,
		owner:      cfg.owner,
		repo:       cfg.repo,
		hostname:   cfg.hostname,
		outputDir:  cfg.outputDir,
		verbose:    cfg.verbose,
		jsonOutput: cfg.jsonOutput,
	}
}

func (cfg auditRunConfig) auditOptions() AuditOptions {
	return AuditOptions{
		Owner:      cfg.owner,
		Repo:       cfg.repo,
		Hostname:   cfg.hostname,
		OutputDir:  cfg.outputDir,
		Verbose:    cfg.verbose,
		Parse:      cfg.parse,
		JSONOutput: cfg.jsonOutput,
		EvalsOnly:  cfg.evalsOnly,
	}
}

func renderCachedAuditIfAvailable(ctx context.Context, cfg auditRunConfig) (bool, error) {
	summary, ok := loadRunSummary(cfg.outputDir, cfg.verbose)
	if !ok {
		return false, nil
	}
	auditLog.Printf("Using cached run summary for run %d (processed at %s)", cfg.runID, summary.ProcessedAt.Format(time.RFC3339))
	if cfg.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Using cached run summary for run %d (processed at %s)", cfg.runID, summary.ProcessedAt.Format(time.RFC3339))))
	}
	if shouldSkipAuditRun(cfg.runID, cfg.outputDir, cfg.experimentFilter, cfg.variantFilter, cfg.runtimeFilter) {
		return true, nil
	}
	// When evals are requested but evals are not present locally (e.g., the run was
	// cached before evals were included in the usage artifact), bypass the cache
	// so prepareAuditWorkflowRun can fetch the usage artifact; the filter at
	// the post-download check will then correctly decide whether to skip the run.
	if cfg.evalsArtifactRequested && !runHasEvals(cfg.outputDir, cfg.verbose) &&
		!ensureEvalsResultsFromBranch(ctx, summary.Run, cfg.outputDir, cfg.owner, cfg.repo, cfg.hostname, cfg.verbose) {
		auditLog.Printf("Cache miss for run %d evals: evals not present locally, bypassing cache", cfg.runID)
		return false, nil
	}
	processedRun := processedRunFromSummary(summary, cfg.outputDir)
	return true, renderAuditReport(ctx, processedRun, summary.Metrics, summary.MCPToolUsage, cfg.auditOptions())
}

func processedRunFromSummary(summary *RunSummary, runOutputDir string) ProcessedRun {
	processedRun := ProcessedRun{
		Run:                     summary.Run,
		AwContext:               summary.AwContext,
		TaskDomain:              summary.TaskDomain,
		BehaviorFingerprint:     summary.BehaviorFingerprint,
		AgenticAssessments:      summary.AgenticAssessments,
		AccessAnalysis:          summary.AccessAnalysis,
		FirewallAnalysis:        summary.FirewallAnalysis,
		PolicyAnalysis:          summary.PolicyAnalysis,
		RedactedDomainsAnalysis: summary.RedactedDomainsAnalysis,
		MissingTools:            summary.MissingTools,
		MissingData:             summary.MissingData,
		Noops:                   summary.Noops,
		MCPFailures:             summary.MCPFailures,
		TokenUsage:              summary.TokenUsage,
		WorkingSet:              summary.WorkingSet,
		GitHubRateLimitUsage:    summary.GitHubRateLimitUsage,
		JobDetails:              summary.JobDetails,
	}
	// Run.Turns may be zero on cached-summary paths where the RunSummary was
	// serialised before the run completed. Metrics.Turns is populated from log
	// parsing and is authoritative; backfill here so that audit comparison deltas
	// are computed from an accurate value.
	if processedRun.Run.Turns == 0 && summary.Metrics.Turns > 0 {
		processedRun.Run.Turns = summary.Metrics.Turns
	}
	processedRun.Run.LogsPath = runOutputDir
	return processedRun
}

func shouldSkipAuditRun(runID int64, runOutputDir, experimentFilter, variantFilter, runtimeFilter string) bool {
	if experimentFilter != "" {
		expData := extractExperimentData(runOutputDir)
		if !experimentMatchesFilter(expData, experimentFilter, variantFilter) {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(formatExperimentSkipMessage(runID, experimentFilter, variantFilter)))
			return true
		}
	}
	if runtimeFilter != "" {
		awInfoPath := filepath.Join(runOutputDir, "aw_info.json")
		awInfo, awInfoErr := parseAwInfo(awInfoPath, false)
		runtimeMatches, detectedRuntime := matchRuntimeFilter(awInfo, awInfoErr, runtimeFilter)
		if !runtimeMatches {
			if detectedRuntime == "" {
				detectedRuntime = "unknown"
			}
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: runtime '%s' does not match filter '%s'", runID, detectedRuntime, runtimeFilter)))
			return true
		}
	}
	return false
}

func prepareAuditWorkflowRun(ctx context.Context, cfg auditRunConfig) (WorkflowRun, error) {
	run, hasLocalCache, useLocalCache, err := fetchAuditRunWithCache(ctx, cfg)
	if err != nil {
		return WorkflowRun{}, err
	}
	if !useLocalCache {
		useLocalCache, err = downloadAuditArtifactsIfNeeded(ctx, cfg, run, hasLocalCache)
		if err != nil {
			return WorkflowRun{}, err
		}
	}
	return prepareRunForAnalysis(run, cfg, useLocalCache), nil
}

func fetchAuditRunWithCache(ctx context.Context, cfg auditRunConfig) (WorkflowRun, bool, bool, error) {
	hasLocalCache := fileutil.DirExists(cfg.outputDir) && !fileutil.IsDirEmpty(cfg.outputDir)
	run, err := fetchWorkflowRunMetadata(ctx, cfg.runID, cfg.owner, cfg.repo, cfg.hostname, cfg.verbose)
	if err == nil {
		return run, hasLocalCache, false, nil
	}
	if !isPermissionError(err) {
		return WorkflowRun{}, false, false, err
	}
	if !hasLocalCache {
		return WorkflowRun{}, false, false, cacheRecoveryError(
			"GitHub API access denied and no local cache found.", cfg.runID, cfg.outputDir, err,
		)
	}
	fmt.Fprintln(os.Stderr, console.FormatWarningMessage("GitHub API access denied, but found locally cached artifacts. Processing cached data..."))
	return run, hasLocalCache, true, nil
}

func downloadAuditArtifactsIfNeeded(ctx context.Context, cfg auditRunConfig, run WorkflowRun, hasLocalCache bool) (bool, error) {
	if cfg.verbose {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Run: %s (Status: %s, Conclusion: %s)", run.WorkflowName, run.Status, run.Conclusion)))
	}
	auditLog.Printf("Downloading artifacts for run %d", cfg.runID)
	err := downloadRunArtifacts(ctx, downloadArtifactsOptions{runID: cfg.runID, outputDir: cfg.outputDir, verbose: cfg.verbose, owner: cfg.owner, repo: cfg.repo, hostname: cfg.hostname, artifactFilter: cfg.artifactFilter})
	if err == nil || errors.Is(err, ErrNoArtifacts) {
		downloadLegacyEvalsArtifactIfNeeded(ctx, cfg)
		if errors.Is(err, ErrNoArtifacts) {
			auditLog.Printf("No artifacts found for run %d", cfg.runID)
			if cfg.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No artifacts attached to this run. Proceeding with metadata-only audit."))
			}
		}
		return false, nil
	}
	if isPermissionError(err) && hasLocalCache {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("Artifact download failed due to permissions, but found locally cached artifacts. Processing cached data..."))
		return true, nil
	}
	if isPermissionError(err) {
		return false, cacheRecoveryError("could not download artifacts due to permissions and no local cache was found; expected a token with actions:read access or a local cache directory.", cfg.runID, cfg.outputDir, err)
	}
	return false, fmt.Errorf("could not download artifacts, expected the run to have completed and artifacts to still be available: %w", err)
}

func downloadLegacyEvalsArtifactIfNeeded(ctx context.Context, cfg auditRunConfig) {
	if !cfg.evalsArtifactRequested || runHasEvals(cfg.outputDir, cfg.verbose) {
		return
	}
	auditLog.Printf("Evals not found in usage artifact for run %d, attempting fallback download of dedicated evals artifact", cfg.runID)
	evalsArtifactFilter := []string{constants.EvalsArtifactName.String()}
	if err := downloadRunArtifacts(ctx, downloadArtifactsOptions{runID: cfg.runID, outputDir: cfg.outputDir, verbose: cfg.verbose, owner: cfg.owner, repo: cfg.repo, hostname: cfg.hostname, artifactFilter: evalsArtifactFilter}); err != nil {
		auditLog.Printf("Fallback evals artifact download failed for run %d: %v", cfg.runID, err)
		if cfg.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Evals not found in usage artifact for run %d and fallback download failed: %v", cfg.runID, err)))
		}
		return
	}
	auditLog.Printf("Fallback evals artifact downloaded for run %d", cfg.runID)
}

func cacheRecoveryError(message string, runID int64, runOutputDir string, err error) error {
	return fmt.Errorf("%s\n\n"+
		"To download artifacts, use the GitHub MCP server:\n\n"+
		"1. Use the github-mcp-server tool 'download_workflow_run_artifacts' with:\n"+
		"   - run_id: %d\n"+
		"   - output_directory: %s\n\n"+
		"2. After downloading, run this audit command again to analyze the cached artifacts.\n\n"+
		"Original error: %w", message, runID, runOutputDir, err)
}

func prepareRunForAnalysis(run WorkflowRun, cfg auditRunConfig, useLocalCache bool) WorkflowRun {
	if useLocalCache && run.DatabaseID == 0 {
		run = WorkflowRun{
			DatabaseID:   cfg.runID,
			WorkflowName: fmt.Sprintf("Workflow Run %d", cfg.runID),
			Status:       "unknown",
			LogsPath:     cfg.outputDir,
		}
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Using locally cached artifacts without metadata. Some report details may be unavailable."))
	}
	run.LogsPath = cfg.outputDir
	if !run.StartedAt.IsZero() && !run.UpdatedAt.IsZero() {
		run.Duration = run.UpdatedAt.Sub(run.StartedAt)
	}
	return run
}

// shouldSkipForEvals returns true when evals filtering is active but no evals results
// are found locally after download. It logs the skip decision and, when verbose, prints
// an info message to stderr. Call this only after artifact download has completed.
func shouldSkipForEvals(ctx context.Context, cfg auditRunConfig, run WorkflowRun) bool {
	if !cfg.evalsOnly {
		return false
	}
	if runHasEvals(cfg.outputDir, cfg.verbose) ||
		ensureEvalsResultsFromBranch(ctx, run, cfg.outputDir, cfg.owner, cfg.repo, cfg.hostname, cfg.verbose) {
		return false
	}
	auditLog.Printf("Skipping run %d: no evals results found (filtered by --evals)", cfg.runID)
	if cfg.verbose {
		fmt.Fprintf(os.Stderr, "%s\n", console.FormatInfoMessage(fmt.Sprintf("Skipping run %d: workflow does not have evals results (filtered by --evals)", cfg.runID)))
	}
	return true
}
