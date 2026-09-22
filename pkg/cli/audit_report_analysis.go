package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/timeutil"
)

// filterActionableDomains removes placeholder values from a domain list.
// "-" and unknownDomain ("(unknown)") can appear when iptables drops traffic
// before Squid identifies the destination; they are not real domains and should
// not appear in allow-list recommendations.
func filterActionableDomains(domains []string) []string {
	result := make([]string, 0, len(domains))
	for _, d := range domains {
		if d != "-" && d != unknownDomain {
			result = append(result, d)
		}
	}
	return result
}

// generateFindings creates key findings from workflow run data
func generateFindings(processedRun ProcessedRun, metrics MetricsData, errors []ValidationIssue) []AuditFinding {
	auditReportLog.Printf("Generating findings: errors=%d, conclusion=%s", len(errors), processedRun.Run.Conclusion)
	findings := appendFailureAndTimeoutFindings(nil, processedRun, metrics, errors)
	findings = append(findings, generateThreatDetectionFindings(processedRun)...)
	findings = append(findings, generatePerformanceFindings(metrics)...)
	findings = append(findings, generateErrorVolumeFindings(errors)...)
	findings = append(findings, generateToolingFindings(processedRun)...)
	findings = append(findings, generateFirewallFindings(processedRun)...)
	findings = append(findings, generateSuccessFindings(processedRun.Run, metrics, errors)...)
	return findings
}

func findFailedAgentJob(jobDetails []JobInfoWithDuration) (JobInfoWithDuration, bool) {
	for _, job := range jobDetails {
		if strings.EqualFold(strings.TrimSpace(job.Name), "agent") && strings.EqualFold(job.Conclusion, "failure") {
			return job, true
		}
	}

	return JobInfoWithDuration{}, false
}

// generateRecommendations creates actionable recommendations based on findings
func generateRecommendations(processedRun ProcessedRun, metrics MetricsData, findings []AuditFinding) []Recommendation {
	auditReportLog.Printf("Generating recommendations: findings_count=%d, workflow_conclusion=%s", len(findings), processedRun.Run.Conclusion)
	var recommendations []Recommendation
	hasCriticalFindings, hasHighCostFindings, hasManyTurns := analyzeRecommendationInputs(findings)
	recommendations = appendFailureRecommendations(recommendations, processedRun.Run, hasCriticalFindings)
	recommendations = appendCostRecommendations(recommendations, hasHighCostFindings)
	recommendations = appendIterationRecommendations(recommendations, hasManyTurns)
	recommendations = appendToolingRecommendations(recommendations, processedRun)
	recommendations = appendSuccessRecommendations(recommendations, processedRun.Run)
	return recommendations
}

func appendFailureAndTimeoutFindings(findings []AuditFinding, processedRun ProcessedRun, metrics MetricsData, errors []ValidationIssue) []AuditFinding {
	run := processedRun.Run
	if run.Conclusion == "failure" {
		findings = append(findings, AuditFinding{
			Code:        AuditFindingWorkflowFailed,
			Category:    "error",
			Severity:    "critical",
			Title:       "Workflow Failed",
			Description: buildFailureFindingDescription(run, processedRun.JobDetails, metrics, errors),
			Impact:      "Workflow did not complete successfully and may need intervention",
		})
	}
	if run.Conclusion == "timed_out" {
		findings = append(findings, AuditFinding{
			Code:        AuditFindingWorkflowTimeout,
			Category:    "performance",
			Severity:    "high",
			Title:       "Workflow Timeout",
			Description: "Workflow exceeded time limit and was terminated",
			Impact:      "Tasks may be incomplete, consider optimizing workflow or increasing timeout",
		})
	}
	return findings
}

func buildFailureFindingDescription(run WorkflowRun, jobDetails []JobInfoWithDuration, metrics MetricsData, errors []ValidationIssue) string {
	if metrics.ErrorCount == 0 && len(errors) == 0 {
		if agentJob, ok := findFailedAgentJob(jobDetails); ok {
			return fmt.Sprintf(
				"Workflow '%s' failed after agent activation — agent job ran for %s before failing and no agent telemetry was available to analyze",
				run.WorkflowName,
				timeutil.FormatDuration(agentJob.Duration),
			)
		}
		return fmt.Sprintf(
			"Workflow '%s' failed before agent activation — no error logs were available to analyze",
			run.WorkflowName,
		)
	}

	errorCount := len(errors)
	if errorCount == 0 {
		errorCount = metrics.ErrorCount
	}

	desc := fmt.Sprintf("Workflow '%s' failed with %d error(s)", run.WorkflowName, errorCount)
	if len(errors) == 0 {
		return desc
	}

	const maxErrMsgLen = 200
	// Range avoids direct indexing while preserving the first error.
	for _, issue := range errors {
		return desc + ": " + stringutil.Truncate(issue.Message, maxErrMsgLen)
	}
	return desc
}

func generatePerformanceFindings(metrics MetricsData) []AuditFinding {
	var findings []AuditFinding
	if metrics.TokenUsage > 50000 {
		findings = append(findings, AuditFinding{
			Code:        AuditFindingHighTokenUsage,
			Category:    "performance",
			Severity:    "medium",
			Title:       "High Token Usage",
			Description: fmt.Sprintf("Used %s tokens", console.FormatNumber(metrics.TokenUsage)),
			Impact:      "High token usage may indicate verbose outputs or inefficient prompts",
		})
	}
	if metrics.Turns > 10 {
		findings = append(findings, AuditFinding{
			Code:        AuditFindingManyIterations,
			Category:    "performance",
			Severity:    "medium",
			Title:       "Many Iterations",
			Description: fmt.Sprintf("Workflow took %d turns to complete", metrics.Turns),
			Impact:      "Many turns may indicate task complexity or unclear instructions",
		})
	}
	return findings
}

func generateErrorVolumeFindings(errors []ValidationIssue) []AuditFinding {
	if len(errors) <= 5 {
		return nil
	}
	return []AuditFinding{{
		Code:        AuditFindingMultipleErrors,
		Category:    "error",
		Severity:    "high",
		Title:       "Multiple Errors",
		Description: fmt.Sprintf("Encountered %d errors during execution", len(errors)),
		Impact:      "Multiple errors may indicate systemic issues requiring attention",
	}}
}

func generateToolingFindings(processedRun ProcessedRun) []AuditFinding {
	var findings []AuditFinding
	if len(processedRun.MCPFailures) > 0 {
		serverNames := sliceutil.Map(processedRun.MCPFailures, func(failure MCPFailureReport) string {
			return failure.ServerName
		})
		findings = append(findings, AuditFinding{
			Code:        AuditFindingMCPServerFailures,
			Category:    "tooling",
			Severity:    "high",
			Title:       "MCP Server Failures",
			Description: "Failed MCP servers: " + strings.Join(serverNames, ", "),
			Impact:      "Missing tools may limit workflow capabilities",
		})
	}
	if len(processedRun.MissingTools) > 0 {
		findings = append(findings, AuditFinding{
			Code:        AuditFindingToolsNotAvailable,
			Category:    "tooling",
			Severity:    "medium",
			Title:       "Tools Not Available",
			Description: buildMissingToolsFindingDescription(processedRun.MissingTools),
			Impact:      "Agent requested tools that were not configured or available",
		})
	}
	return findings
}

func buildMissingToolsFindingDescription(missingTools []MissingToolReport) string {
	toolNames := sliceutil.Map(missingTools[:min(3, len(missingTools))], func(t MissingToolReport) string {
		return t.Tool
	})
	desc := "Missing tools: " + strings.Join(toolNames, ", ")
	if len(missingTools) > 3 {
		desc += fmt.Sprintf(" (and %d more)", len(missingTools)-3)
	}
	return desc
}

func generateFirewallFindings(processedRun ProcessedRun) []AuditFinding {
	if processedRun.FirewallAnalysis == nil || processedRun.FirewallAnalysis.BlockedRequests == 0 {
		return nil
	}
	blockedDomains := filterActionableDomains(processedRun.FirewallAnalysis.GetBlockedDomains())
	return []AuditFinding{{
		Code:        AuditFindingBlockedNetworkRequests,
		Category:    "network",
		Severity:    "medium",
		Title:       "Blocked Network Requests",
		Description: buildBlockedNetworkFindingDescription(processedRun.FirewallAnalysis.BlockedRequests, blockedDomains),
		Impact:      "Blocked requests may indicate missing network permissions or unexpected behavior",
	}}
}

func buildBlockedNetworkFindingDescription(blockedRequests int, blockedDomains []string) string {
	switch {
	case len(blockedDomains) == 1:
		// Range avoids direct indexing while preserving the only domain.
		for _, domain := range blockedDomains {
			return "Agent attempted to access blocked domain: " + domain
		}
	case len(blockedDomains) > 1 && len(blockedDomains) <= 3:
		return "Agent attempted to access blocked domains: " + strings.Join(blockedDomains, ", ")
	case len(blockedDomains) > 3:
		return fmt.Sprintf(
			"Agent attempted to access %d blocked domains, including: %s",
			len(blockedDomains),
			strings.Join(blockedDomains[:3], ", "),
		)
	default:
		return fmt.Sprintf("%d network request(s) were blocked by firewall", blockedRequests)
	}
	return fmt.Sprintf("%d network request(s) were blocked by firewall", blockedRequests)
}

func generateSuccessFindings(run WorkflowRun, metrics MetricsData, errors []ValidationIssue) []AuditFinding {
	if run.Conclusion != "success" || len(errors) > 0 {
		return nil
	}
	return []AuditFinding{{
		Code:        AuditFindingWorkflowSucceeded,
		Category:    "success",
		Severity:    "info",
		Title:       "Workflow Completed Successfully",
		Description: fmt.Sprintf("Completed in %d turns with no errors", metrics.Turns),
		Impact:      "No action needed",
	}}
}

func analyzeRecommendationInputs(findings []AuditFinding) (hasCriticalFindings, hasHighCostFindings, hasManyTurns bool) {
	for _, finding := range findings {
		if finding.Severity == "critical" {
			hasCriticalFindings = true
		}
		if finding.Category == "cost" && (finding.Severity == "high" || finding.Severity == "medium") {
			hasHighCostFindings = true
		}
		if finding.Category == "performance" && strings.Contains(finding.Title, "Iterations") {
			hasManyTurns = true
		}
	}
	return hasCriticalFindings, hasHighCostFindings, hasManyTurns
}

func appendFailureRecommendations(recommendations []Recommendation, run WorkflowRun, hasCriticalFindings bool) []Recommendation {
	if run.Conclusion != "failure" && !hasCriticalFindings {
		return recommendations
	}
	return append(recommendations, Recommendation{
		Priority: "high",
		Action:   "Review error logs to identify root cause of failure",
		Reason:   "Understanding failure causes helps prevent recurrence",
		Example:  "Check the errors field for specific error messages, or inspect the log files in logs_path",
	})
}

func appendCostRecommendations(recommendations []Recommendation, hasHighCostFindings bool) []Recommendation {
	if !hasHighCostFindings {
		return recommendations
	}
	return append(recommendations, Recommendation{
		Priority: "medium",
		Action:   "Optimize prompt size and reduce verbose outputs",
		Reason:   "High token usage increases costs and may slow execution",
		Example:  "Use concise prompts, limit output verbosity, and consider caching repeated data",
	})
}

func appendIterationRecommendations(recommendations []Recommendation, hasManyTurns bool) []Recommendation {
	if !hasManyTurns {
		return recommendations
	}
	return append(recommendations, Recommendation{
		Priority: "medium",
		Action:   "Clarify workflow instructions or break into smaller tasks",
		Reason:   "Many iterations may indicate unclear objectives or overly complex tasks",
		Example:  "Split complex workflows into discrete steps with clear success criteria",
	})
}

func appendToolingRecommendations(recommendations []Recommendation, processedRun ProcessedRun) []Recommendation {
	if len(processedRun.MissingTools) > 0 {
		var firstMissingTool string
		// Range avoids direct indexing while preserving the first missing tool.
		for _, missingTool := range processedRun.MissingTools {
			firstMissingTool = missingTool.Tool
			break
		}
		recommendations = append(recommendations, Recommendation{
			Priority: "medium",
			Action:   "Add missing tools to workflow configuration",
			Reason:   "Missing tools limit agent capabilities and may cause failures",
			Example:  "Add tools configuration for: " + firstMissingTool,
		})
	}
	if len(processedRun.MCPFailures) > 0 {
		recommendations = append(recommendations, Recommendation{
			Priority: "high",
			Action:   "Fix MCP server configuration or dependencies",
			Reason:   "MCP server failures prevent agent from accessing required tools",
			Example:  "Check server logs and verify MCP server is properly configured and accessible",
		})
	}
	if processedRun.FirewallAnalysis == nil || processedRun.FirewallAnalysis.BlockedRequests == 0 {
		return recommendations
	}
	return append(recommendations, Recommendation{
		Priority: "medium",
		Action:   "Add blocked domains to the workflow network allow-list",
		Reason:   "Firewall-blocked domains prevent the agent from completing its tasks",
		Example:  buildFirewallRecommendationExample(processedRun.FirewallAnalysis),
	})
}

func buildFirewallRecommendationExample(firewallAnalysis *FirewallAnalysis) string {
	blockedDomains := filterActionableDomains(firewallAnalysis.GetBlockedDomains())
	if len(blockedDomains) == 0 {
		return "Add allowed domains to network configuration or review firewall rules"
	}
	return fmt.Sprintf(
		"Add the blocked domain(s) to your workflow frontmatter:\n\n```yaml\nnetwork:\n  allowed:\n    - %s\n```",
		strings.Join(blockedDomains, "\n    - "),
	)
}

func appendSuccessRecommendations(recommendations []Recommendation, run WorkflowRun) []Recommendation {
	if len(recommendations) > 0 || run.Conclusion != "success" {
		return recommendations
	}
	return append(recommendations, Recommendation{
		Priority: "low",
		Action:   "Monitor workflow performance over time",
		Reason:   "Tracking metrics helps identify trends and optimization opportunities",
		Example:  "Run 'gh aw logs' periodically to review cost and performance trends",
	})
}

// generatePerformanceMetrics calculates aggregated performance statistics
func generatePerformanceMetrics(processedRun ProcessedRun, metrics MetricsData, toolUsage []ToolUsageInfo) *PerformanceMetrics {
	run := processedRun.Run
	auditReportLog.Printf("Generating performance metrics: token_usage=%d, tool_count=%d, duration=%v", metrics.TokenUsage, len(toolUsage), run.Duration)
	pm := &PerformanceMetrics{}

	// Calculate tokens per minute
	if run.Duration > 0 && metrics.TokenUsage > 0 {
		minutes := run.Duration.Minutes()
		if minutes > 0 {
			pm.TokensPerMinute = float64(metrics.TokenUsage) / minutes
		}
	}

	// Find most used tool
	if len(toolUsage) > 0 {
		var mostUsed ToolUsageInfo
		for _, tool := range toolUsage {
			if tool.CallCount > mostUsed.CallCount {
				mostUsed = tool
			}
		}
		pm.MostUsedTool = fmt.Sprintf("%s (%d calls)", mostUsed.Name, mostUsed.CallCount)
		auditReportLog.Printf("Most used tool: %s with %d calls", mostUsed.Name, mostUsed.CallCount)
	}

	// Calculate average tool duration
	if len(toolUsage) > 0 {
		totalDuration := time.Duration(0)
		count := 0
		for _, tool := range toolUsage {
			if tool.MaxDuration != "" {
				// Try to parse duration string using time.ParseDuration
				if d, err := time.ParseDuration(tool.MaxDuration); err == nil {
					totalDuration += d
					count++
				}
			}
		}
		if count > 0 {
			avgDuration := totalDuration / time.Duration(count)
			pm.AvgToolDuration = timeutil.FormatDuration(avgDuration)
		}
	}

	// Network request count from firewall
	if processedRun.FirewallAnalysis != nil {
		pm.NetworkRequests = processedRun.FirewallAnalysis.TotalRequests
	}

	return pm
}
