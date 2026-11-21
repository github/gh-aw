package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/github/gh-aw/pkg/console"
)

// finalizeAndSaveRunSummary fetches job details, derives the agentic analysis, builds the
// RunSummary struct, and writes it to disk.  It also sets the agentic-analysis fields on
// result directly so they are available to the caller.
func finalizeAndSaveRunSummary(ctx context.Context, result *DownloadResult, runOutputDir string, metrics LogMetrics, verbose bool) {
	jobDetails, jobErr := fetchJobDetails(ctx, result.Run.DatabaseID, runOutputDir, verbose, result.Run.Attempt)
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
		ModelRouting:            result.ModelRouting,
		GatewaySteeringEvents:   result.GatewaySteeringEvents,
		WorkingSet:              result.WorkingSet,
		Ledger:                  result.Ledger,
		WorkQueue:               result.WorkQueue,
		Friction:                result.Friction,
		GitHubRateLimitUsage:    result.GitHubRateLimitUsage,
		JobDetails:              jobDetails,
		SafeOutputs:             result.SafeOutputs,
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
			ModelRouting:            result.ModelRouting,
			GatewaySteeringEvents:   result.GatewaySteeringEvents,
			WorkingSet:              result.WorkingSet,
			Ledger:                  result.Ledger,
			WorkQueue:               result.WorkQueue,
			Friction:                result.Friction,
			GitHubRateLimitUsage:    result.GitHubRateLimitUsage,
			JobDetails:              jobDetails,
			SafeOutputs:             result.SafeOutputs,
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
		hadFriction := result.Friction != nil
		applyUsageActivitySummaryToResult(nil, result, true)
		return !hadFriction && result.Friction != nil
	}
	applyUsageActivitySummaryToResult(usageActivitySummary, result, true)
	return true
}

func backfillGatewaySteeringEventsIfNeeded(result *DownloadResult, runOutputDir string, verbose bool) bool {
	if len(result.GatewaySteeringEvents) > 0 {
		return false
	}
	events, err := extractGatewaySteeringEvents(runOutputDir)
	if err != nil {
		logsOrchestratorLog.Printf("Warning: failed to backfill gateway steering events for run %d: %v", result.Run.DatabaseID, err)
		if verbose {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to backfill gateway steering events for run %d: %v", result.Run.DatabaseID, err)))
		}
		return false
	}
	if len(events) == 0 {
		return false
	}
	result.GatewaySteeringEvents = events
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
