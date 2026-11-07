package cli

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
)

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

	inferMissingWorkflowPath(result, runOutputDir)

	// Calculate duration and billable minutes from GitHub API timestamps.
	// This mirrors the identical computation in audit.go for consistency.
	if !result.Run.StartedAt.IsZero() && !result.Run.UpdatedAt.IsZero() {
		result.Run.Duration = result.Run.UpdatedAt.Sub(result.Run.StartedAt)
		result.Run.ActionMinutes = math.Ceil(result.Run.Duration.Minutes())
	}
	return metrics
}

func inferMissingWorkflowPath(result *DownloadResult, runOutputDir string) {
	if result.Run.WorkflowPath != "" {
		return
	}
	awInfoPath := filepath.Join(runOutputDir, "aw_info.json")
	if info, err := parseAwInfo(awInfoPath, false); err == nil && info != nil && info.WorkflowName != "" {
		result.Run.WorkflowPath = inferWorkflowPathFromDisplayName(info.WorkflowName)
	}
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
	backfillWorkQueueReport(&result.WorkQueue, runOutputDir)
	// token-usage.jsonl is also available in the compact usage artifact.
	tokenUsage, tokenErr := analyzeTokenUsage(runOutputDir, verbose)
	if tokenErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze token usage for run %d: %v", result.Run.DatabaseID, tokenErr)))
	}
	result.TokenUsage = tokenUsage
	result.ModelRouting = analyzeModelRouting(runOutputDir)
	backfillRunTokenUsageFromFirewall(metrics, result, tokenUsage)
	steeringEvents, steeringErr := extractGatewaySteeringEvents(runOutputDir)
	if steeringErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to extract gateway steering events for run %d: %v", result.Run.DatabaseID, steeringErr)))
	}
	result.GatewaySteeringEvents = steeringEvents
	rateLimitUsage, rlErr := analyzeGitHubRateLimits(runOutputDir, verbose)
	if rlErr != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to analyze GitHub rate limit usage for run %d: %v", result.Run.DatabaseID, rlErr)))
	}
	result.GitHubRateLimitUsage = rateLimitUsage

	// Count safe output items created in GitHub (from manifest artifact).
	// This runs before applyUsageActivitySummaryToResult so that the summary
	// backfill only activates when the manifest returned zero items.
	result.SafeOutputs = extractCreatedItemsFromManifest(runOutputDir)
	result.Run.SafeItemsCount = len(result.SafeOutputs)

	// Fill missing activity summaries from usage artifact precomputes.
	// This call is unconditional but only backfills fields that are still empty.
	applyUsageActivitySummaryToResult(usageActivitySummary, result, !hasFirewallArtifact)
}
