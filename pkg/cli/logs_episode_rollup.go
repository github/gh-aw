package cli

import (
	"cmp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/timeutil"
)

func (acc *episodeAccumulator) addRun(run RunData) {
	if !acc.runSet[run.RunID] {
		acc.runSet[run.RunID] = true
		acc.metadata.RunIDs = append(acc.metadata.RunIDs, run.RunID)
	}
	if run.WorkflowName != "" && !acc.nameSet[run.WorkflowName] {
		acc.nameSet[run.WorkflowName] = true
		acc.metadata.WorkflowNames = append(acc.metadata.WorkflowNames, run.WorkflowName)
	}
	acc.metadata.TotalRuns++
	acc.metadata.TotalTokens += run.TokenUsage
	acc.metadata.TotalAIC += run.AIC
	acc.metadata.ManifestEntryCount += run.ManifestEntryCount
	acc.metadata.TemporaryIDMappings += run.TemporaryIDMappings
	acc.metadata.ChainedTargetCount += run.ChainedTargetCount
	acc.metadata.ChainedFollowupActionCount += run.ChainedFollowupActionCount
	acc.metadata.DelegatedTempTargetCount += run.DelegatedTempTargetCount
	acc.metadata.ClosedTempTargetCount += run.ClosedTempTargetCount
	acc.metadata.MissingToolCount += run.MissingToolCount
}

func (acc *episodeAccumulator) addSignals(run RunData) {
	if run.Comparison != nil && run.Comparison.Classification != nil && run.Comparison.Classification.Label == "risky" {
		acc.metadata.RiskyNodeCount++
	}
	if run.Comparison != nil && run.Comparison.Classification != nil && run.Comparison.Classification.Label == "changed" {
		acc.metadata.ChangedNodeCount++
	}
	if run.BehaviorFingerprint != nil && run.BehaviorFingerprint.ActuationStyle != "read_only" {
		acc.metadata.WriteCapableNodeCount++
	}
	if run.Comparison != nil && run.Comparison.Baseline != nil && run.Comparison.Baseline.Selection == "latest_success" {
		acc.metadata.LatestSuccessFallbackCount++
	}
	if hasComparisonReasonCode(run.Comparison, "new_mcp_failure") {
		acc.metadata.NewMCPFailureRunCount++
	}
	if hasComparisonReasonCode(run.Comparison, "blocked_requests_increase") {
		acc.metadata.BlockedRequestIncreaseRunCount++
	}
	if hasAssessmentKindAtLeast(run.AgenticAssessments, "resource_heavy_for_domain", "medium") {
		acc.metadata.ResourceHeavyNodeCount++
	}
	if hasAssessmentKindAtLeast(run.AgenticAssessments, "poor_agentic_control", "medium") {
		acc.metadata.PoorControlNodeCount++
	}
}

func (acc *episodeAccumulator) addProcessedRun(run ProcessedRun) {
	acc.metadata.MCPFailureCount += len(run.MCPFailures)
	if run.FirewallAnalysis != nil {
		acc.metadata.BlockedRequestCount += run.FirewallAnalysis.BlockedRequests
		if run.FirewallAnalysis.BlockedRequests >= firewallBlockedRequestCap {
			acc.metadata.BlockedRequestAtCap = true
		}
	}
	if run.MCPToolUsage != nil {
		for _, call := range run.MCPToolUsage.ToolCalls {
			acc.toolCalls = append(acc.toolCalls, mcpToolCallToEpisodeToolCall(call))
		}
	}
}

func (acc *episodeAccumulator) addIdentity(run RunData) {
	if !run.CreatedAt.IsZero() && (acc.metadata.RootRunID == 0 || run.CreatedAt.Before(acc.rootTime)) {
		acc.rootTime = run.CreatedAt
		acc.metadata.RootRunID = run.RunID
		acc.metadata.PrimaryWorkflow = run.WorkflowName
	}
	if acc.metadata.PrimaryWorkflow == "" && run.WorkflowName != "" {
		acc.metadata.PrimaryWorkflow = run.WorkflowName
	}
	if acc.metadata.Repository == "" && run.Repository != "" {
		acc.metadata.Repository = run.Repository
		if organization, _, found := strings.Cut(run.Repository, "/"); found {
			acc.metadata.Organization = organization
		}
	}
}

func (acc *episodeAccumulator) addDuration(run RunData, processedRun ProcessedRun) {
	if run.StartedAt.IsZero() && run.UpdatedAt.IsZero() {
		return
	}
	if !run.StartedAt.IsZero() && !run.UpdatedAt.IsZero() && run.UpdatedAt.After(run.StartedAt) {
		acc.duration += run.UpdatedAt.Sub(run.StartedAt)
	} else if processedRun.Run.Duration > 0 {
		acc.duration += processedRun.Run.Duration
	}
}

func (acc *episodeAccumulator) finalize() EpisodeData {
	slices.Sort(acc.metadata.RunIDs)
	slices.Sort(acc.metadata.WorkflowNames)
	if acc.duration > 0 {
		acc.metadata.TotalDuration = timeutil.FormatDuration(acc.duration)
	}
	if acc.metadata.PrimaryWorkflow == "" && len(acc.metadata.WorkflowNames) > 0 {
		acc.metadata.PrimaryWorkflow = acc.metadata.WorkflowNames[0]
	}
	switch acc.metadata.RiskyNodeCount {
	case 0:
		acc.metadata.RiskDistribution = "none"
	case 1:
		acc.metadata.RiskDistribution = "concentrated"
	default:
		acc.metadata.RiskDistribution = "distributed"
	}
	acc.metadata.EscalationEligible, acc.metadata.EscalationReason = classifyEpisodeEscalation(acc.metadata)
	acc.metadata.SuggestedRoute = buildSuggestedRoute(acc.metadata)
	if len(acc.toolCalls) > 0 {
		slices.SortFunc(acc.toolCalls, func(a, b EpisodeToolCall) int {
			if a.Server != b.Server {
				return cmp.Compare(a.Server, b.Server)
			}
			if a.Tool != b.Tool {
				return cmp.Compare(a.Tool, b.Tool)
			}
			return cmp.Compare(a.Status, b.Status)
		})
		acc.metadata.ToolCalls = acc.toolCalls
	}
	return acc.metadata
}
