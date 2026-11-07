package cli

import (
	"context"
)

// tryLoadCachedRunResult attempts to return a pre-built DownloadResult from the on-disk
// cache.  Returns (result, true) on a valid cache hit; (nil, false) otherwise.
func tryLoadCachedRunResult(
	ctx context.Context,
	run WorkflowRun,
	runOutputDir string,
	params concurrentRunDownloadParams,
) (*DownloadResult, bool) {
	summary, ok := loadRunSummary(runOutputDir, params.verbose)
	if !ok || workQueueLogsMissing(params.includeWorkQueue, runOutputDir) {
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
	metadataRefresh := refreshCachedRunMetadata(ctx, &result.Run, runOutputDir, run, params)
	// Re-apply the usage activity backfill to heal stale cache entries.
	// Capture the SafeItemsCount before backfill to detect whether the field was healed.
	safeItemsBefore := result.Run.SafeItemsCount
	activitySummaryApplied := backfillCacheHitIfNeeded(&result, runOutputDir, params.verbose)
	steeringBackfillApplied := backfillGatewaySteeringEventsIfNeeded(&result, runOutputDir, params.verbose)
	workQueueBackfillApplied := backfillWorkQueueReport(&result.WorkQueue, runOutputDir)
	// If the backfill populated SafeItemsCount (i.e. it was 0 before and is now non-zero),
	// persist the healed value back to run_summary.json so downstream readers (e.g.
	// the api-consumption-report) see the correct count without having to fall back to
	// usage/activity/summary.json.
	if result.Run.SafeItemsCount != safeItemsBefore || activitySummaryApplied || steeringBackfillApplied || workQueueBackfillApplied || metadataRefresh.Applied {
		healed := *summary
		healed.Run = result.Run
		healed.Metrics = result.Metrics
		healed.MCPToolUsage = result.MCPToolUsage
		healed.WorkingSet = result.WorkingSet
		healed.Ledger = result.Ledger
		healed.Friction = result.Friction
		healed.SafeOutputs = result.SafeOutputs
		healed.GatewaySteeringEvents = result.GatewaySteeringEvents
		healed.WorkQueue = result.WorkQueue
		if err := saveRunSummary(runOutputDir, &healed, params.verbose); err != nil {
			logsOrchestratorLog.Printf("Warning: failed to persist healed run summary for run %d: %v", result.Run.DatabaseID, err)
		}
	}
	applyCachedRunWorkflowPathGate(&result, run, metadataRefresh.WorkflowPathVerified, params.verbose)
	return &result, true
}

// applyCachedRunWorkflowPathGate enforces current workflow-path trust for cache hits.
// An explicit current non-agentic path skips the cached result. When no current path
// is available and metadata did not verify one, the stale cached path is cleared only
// in memory so the result fails closed without rewriting the persisted summary.
func applyCachedRunWorkflowPathGate(result *DownloadResult, run WorkflowRun, workflowPathVerified bool, verbose bool) {
	if run.WorkflowPath != "" {
		result.Run.WorkflowPath = run.WorkflowPath
		if !isAgenticWorkflowPath(run.WorkflowPath) {
			skipNonAgenticWorkflowRun(result, verbose)
			result.Run.WorkflowPath = ""
		}
		return
	}
	if !workflowPathVerified {
		result.Run.WorkflowPath = ""
		skipUnverifiedWorkflowRun(result, verbose)
	}
}

type cachedRunMetadataRefresh struct {
	// Applied reports whether non-empty current metadata changed the cached run summary.
	Applied bool
	// WorkflowPathVerified reports whether current metadata contained an authoritative workflow path.
	WorkflowPathVerified bool
}

func refreshCachedRunMetadata(ctx context.Context, cachedRun *WorkflowRun, runOutputDir string, run WorkflowRun, params concurrentRunDownloadParams) cachedRunMetadataRefresh {
	needsRefresh, err := workflowRunMetadataCacheNeedsRefresh(runOutputDir, run, params.dlOwner, params.dlRepo, params.dlHost)
	if err == nil && needsRefresh {
		err = waitForConfiguredRateLimit(ctx, params.verbose, params.maxGitHubAPIRateLimit, 1, params.rateLimitState)
	}
	if err != nil {
		logsOrchestratorLog.Printf("Failed to refresh cached workflow run metadata for run %d: %v", run.DatabaseID, err)
		return cachedRunMetadataRefresh{}
	}
	metadata, err := fetchAndCacheWorkflowRunMetadata(ctx, run, runOutputDir, params.dlOwner, params.dlRepo, params.dlHost, params.verbose)
	if err != nil {
		logsOrchestratorLog.Printf("Failed to refresh cached workflow run metadata for run %d: %v", run.DatabaseID, err)
		return cachedRunMetadataRefresh{}
	}
	return cachedRunMetadataRefresh{
		Applied:              applyWorkflowRunMetadata(cachedRun, metadata),
		WorkflowPathVerified: metadata.WorkflowPath != "",
	}
}
