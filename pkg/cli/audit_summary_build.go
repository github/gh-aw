package cli

// audit_summary_build.go: assembly of ProcessedRun and RunSummary values from
// collected audit analysis results.

import (
	"fmt"
	"os"
	"time"

	"github.com/github/gh-aw/pkg/console"
)

func applyAuditMetrics(run WorkflowRun, results auditAnalysisResults) WorkflowRun {
	run.TokenUsage = results.metrics.TokenUsage
	run.Turns = results.metrics.Turns
	run.ErrorCount = results.failedJobCount
	if run.Conclusion == "failure" && run.ErrorCount == 0 {
		run.ErrorCount = 1
	}
	run.WarningCount = 0
	run.SafeItemsCount = results.safeItemsCount
	return run
}

func buildProcessedAuditRun(run WorkflowRun, results auditAnalysisResults) ProcessedRun {
	processedRun := ProcessedRun{
		Run:                     run,
		FirewallAnalysis:        results.firewallAnalysis,
		PolicyAnalysis:          results.policyAnalysis,
		RedactedDomainsAnalysis: results.redactedDomainsAnalysis,
		MissingTools:            results.missingTools,
		MissingData:             results.missingData,
		Noops:                   results.noops,
		MCPFailures:             results.mcpFailures,
		SkillActivations:        results.skillActivations,
		GatewaySteeringEvents:   results.gatewaySteeringEvents,
		TokenUsage:              results.tokenUsageSummary,
		WorkingSet:              results.workingSet,
		GitHubRateLimitUsage:    results.rateLimitUsage,
		JobDetails:              results.jobDetails,
	}
	// Access analysis is persisted in RunSummary but has no live report rendering.
	awContext, _, _, taskDomain, behaviorFingerprint, agenticAssessments := deriveRunAgenticAnalysis(processedRun, results.metrics)
	processedRun.AwContext = awContext
	processedRun.TaskDomain = taskDomain
	processedRun.BehaviorFingerprint = behaviorFingerprint
	processedRun.AgenticAssessments = agenticAssessments
	return processedRun
}

func saveAuditRunSummary(runOutputDir string, run WorkflowRun, processedRun ProcessedRun, results auditAnalysisResults, verbose bool) {
	summary := buildAuditRunSummary(run, processedRun, results)
	if err := saveRunSummary(runOutputDir, summary, verbose); err != nil && verbose {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Failed to save run summary: %v", err)))
	}
}

func buildAuditRunSummary(run WorkflowRun, processedRun ProcessedRun, results auditAnalysisResults) *RunSummary {
	return &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       run.DatabaseID,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run:                     run,
			Metrics:                 results.metrics,
			AwContext:               processedRun.AwContext,
			TaskDomain:              processedRun.TaskDomain,
			BehaviorFingerprint:     processedRun.BehaviorFingerprint,
			AgenticAssessments:      processedRun.AgenticAssessments,
			AccessAnalysis:          results.accessAnalysis,
			FirewallAnalysis:        results.firewallAnalysis,
			RedactedDomainsAnalysis: results.redactedDomainsAnalysis,
			MissingTools:            results.missingTools,
			MissingData:             results.missingData,
			Noops:                   results.noops,
			MCPFailures:             results.mcpFailures,
			SkillActivations:        results.skillActivations,
			MCPToolUsage:            results.mcpToolUsage,
			TokenUsage:              results.tokenUsageSummary,
			GatewaySteeringEvents:   results.gatewaySteeringEvents,
			WorkingSet:              results.workingSet,
			GitHubRateLimitUsage:    results.rateLimitUsage,
			JobDetails:              results.jobDetails,
		},
		PolicyAnalysis: results.policyAnalysis,
		ArtifactsList:  results.artifacts,
	}
}
