package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/timeutil"
)

// generateFindings creates key findings from workflow run data
func generateFindings(processedRun ProcessedRun, metrics MetricsData, errors []ErrorInfo, warnings []ErrorInfo) []Finding {
	auditReportLog.Printf("Generating findings: errors=%d, warnings=%d, conclusion=%s", len(errors), len(warnings), processedRun.Run.Conclusion)
	var findings []Finding
	run := processedRun.Run

	// Failure findings
	if run.Conclusion == "failure" {
		findings = append(findings, Finding{
			Category:    "error",
			Severity:    "critical",
			Title:       "Workflow Failed",
			Description: fmt.Sprintf("Workflow '%s' failed with %d error(s)", run.WorkflowName, metrics.ErrorCount),
			Impact:      "Workflow did not complete successfully and may need intervention",
		})
	}

	if run.Conclusion == "timed_out" {
		findings = append(findings, Finding{
			Category:    "performance",
			Severity:    "high",
			Title:       "Workflow Timeout",
			Description: "Workflow exceeded time limit and was terminated",
			Impact:      "Tasks may be incomplete, consider optimizing workflow or increasing timeout",
		})
	}

	// Cost findings
	if metrics.EstimatedCost > 1.0 {
		findings = append(findings, Finding{
			Category:    "cost",
			Severity:    "high",
			Title:       "High Cost Detected",
			Description: fmt.Sprintf("Estimated cost of $%.2f exceeds typical threshold", metrics.EstimatedCost),
			Impact:      "Review token usage and consider optimization opportunities",
		})
	} else if metrics.EstimatedCost > 0.5 {
		findings = append(findings, Finding{
			Category:    "cost",
			Severity:    "medium",
			Title:       "Moderate Cost",
			Description: fmt.Sprintf("Estimated cost of $%.2f is moderate", metrics.EstimatedCost),
			Impact:      "Monitor costs if this workflow runs frequently",
		})
	}

	// Token usage findings
	if metrics.TokenUsage > 50000 {
		findings = append(findings, Finding{
			Category:    "performance",
			Severity:    "medium",
			Title:       "High Token Usage",
			Description: fmt.Sprintf("Used %s tokens", console.FormatNumber(metrics.TokenUsage)),
			Impact:      "High token usage may indicate verbose outputs or inefficient prompts",
		})
	}

	// Turn count findings
	if metrics.Turns > 10 {
		findings = append(findings, Finding{
			Category:    "performance",
			Severity:    "medium",
			Title:       "Many Iterations",
			Description: fmt.Sprintf("Workflow took %d turns to complete", metrics.Turns),
			Impact:      "Many turns may indicate task complexity or unclear instructions",
		})
	}

	// Error findings
	if len(errors) > 5 {
		findings = append(findings, Finding{
			Category:    "error",
			Severity:    "high",
			Title:       "Multiple Errors",
			Description: fmt.Sprintf("Encountered %d errors during execution", len(errors)),
			Impact:      "Multiple errors may indicate systemic issues requiring attention",
		})
	}

	// MCP failure findings
	if len(processedRun.MCPFailures) > 0 {
		serverNames := make([]string, len(processedRun.MCPFailures))
		for i, failure := range processedRun.MCPFailures {
			serverNames[i] = failure.ServerName
		}
		findings = append(findings, Finding{
			Category:    "tooling",
			Severity:    "high",
			Title:       "MCP Server Failures",
			Description: fmt.Sprintf("Failed MCP servers: %s", strings.Join(serverNames, ", ")),
			Impact:      "Missing tools may limit workflow capabilities",
		})
	}

	// Missing tool findings
	if len(processedRun.MissingTools) > 0 {
		toolNames := make([]string, 0, min(3, len(processedRun.MissingTools)))
		for i := 0; i < len(processedRun.MissingTools) && i < 3; i++ {
			toolNames = append(toolNames, processedRun.MissingTools[i].Tool)
		}
		desc := fmt.Sprintf("Missing tools: %s", strings.Join(toolNames, ", "))
		if len(processedRun.MissingTools) > 3 {
			desc += fmt.Sprintf(" (and %d more)", len(processedRun.MissingTools)-3)
		}
		findings = append(findings, Finding{
			Category:    "tooling",
			Severity:    "medium",
			Title:       "Tools Not Available",
			Description: desc,
			Impact:      "Agent requested tools that were not configured or available",
		})
	}

	// Firewall findings
	if processedRun.FirewallAnalysis != nil && processedRun.FirewallAnalysis.BlockedRequests > 0 {
		findings = append(findings, Finding{
			Category:    "network",
			Severity:    "medium",
			Title:       "Blocked Network Requests",
			Description: fmt.Sprintf("%d network requests were blocked by firewall", processedRun.FirewallAnalysis.BlockedRequests),
			Impact:      "Blocked requests may indicate missing network permissions or unexpected behavior",
		})
	}

	// Success findings
	if run.Conclusion == "success" && len(errors) == 0 {
		findings = append(findings, Finding{
			Category:    "success",
			Severity:    "info",
			Title:       "Workflow Completed Successfully",
			Description: fmt.Sprintf("Completed in %d turns with no errors", metrics.Turns),
			Impact:      "No action needed",
		})
	}

	return findings
}

// generateRecommendations creates actionable recommendations based on findings
func generateRecommendations(processedRun ProcessedRun, metrics MetricsData, findings []Finding) []Recommendation {
	auditReportLog.Printf("Generating recommendations: findings_count=%d, workflow_conclusion=%s", len(findings), processedRun.Run.Conclusion)
	var recommendations []Recommendation
	run := processedRun.Run

	// Check for high-severity findings
	hasCriticalFindings := false
	hasHighCostFindings := false
	hasManyTurns := false
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

	// Recommendations for failures
	if run.Conclusion == "failure" || hasCriticalFindings {
		recommendations = append(recommendations, Recommendation{
			Priority: "high",
			Action:   "Review error logs to identify root cause of failure",
			Reason:   "Understanding failure causes helps prevent recurrence",
			Example:  "Check the Errors section below for specific error messages and file locations",
		})
	}

	// Recommendations for cost optimization
	if hasHighCostFindings {
		recommendations = append(recommendations, Recommendation{
			Priority: "medium",
			Action:   "Optimize prompt size and reduce verbose outputs",
			Reason:   "High token usage increases costs and may slow execution",
			Example:  "Use concise prompts, limit output verbosity, and consider caching repeated data",
		})
	}

	// Recommendations for many turns
	if hasManyTurns {
		recommendations = append(recommendations, Recommendation{
			Priority: "medium",
			Action:   "Clarify workflow instructions or break into smaller tasks",
			Reason:   "Many iterations may indicate unclear objectives or overly complex tasks",
			Example:  "Split complex workflows into discrete steps with clear success criteria",
		})
	}

	// Recommendations for missing tools
	if len(processedRun.MissingTools) > 0 {
		recommendations = append(recommendations, Recommendation{
			Priority: "medium",
			Action:   "Add missing tools to workflow configuration",
			Reason:   "Missing tools limit agent capabilities and may cause failures",
			Example:  fmt.Sprintf("Add tools configuration for: %s", processedRun.MissingTools[0].Tool),
		})
	}

	// Recommendations for MCP failures
	if len(processedRun.MCPFailures) > 0 {
		recommendations = append(recommendations, Recommendation{
			Priority: "high",
			Action:   "Fix MCP server configuration or dependencies",
			Reason:   "MCP server failures prevent agent from accessing required tools",
			Example:  "Check server logs and verify MCP server is properly configured and accessible",
		})
	}

	// Recommendations for firewall blocks
	if processedRun.FirewallAnalysis != nil && processedRun.FirewallAnalysis.BlockedRequests > 10 {
		recommendations = append(recommendations, Recommendation{
			Priority: "medium",
			Action:   "Review network access configuration",
			Reason:   "Many blocked requests suggest missing network permissions",
			Example:  "Add allowed domains to network configuration or review firewall rules",
		})
	}

	// General best practices
	if len(recommendations) == 0 && run.Conclusion == "success" {
		recommendations = append(recommendations, Recommendation{
			Priority: "low",
			Action:   "Monitor workflow performance over time",
			Reason:   "Tracking metrics helps identify trends and optimization opportunities",
			Example:  "Run 'gh aw logs' periodically to review cost and performance trends",
		})
	}

	return recommendations
}

// generateFailureAnalysis creates structured analysis for failed workflows
func generateFailureAnalysis(processedRun ProcessedRun, errors []ErrorInfo) *FailureAnalysis {
	run := processedRun.Run
	auditReportLog.Printf("Generating failure analysis: conclusion=%s, error_count=%d", run.Conclusion, len(errors))

	// Determine primary failure reason
	primaryFailure := run.Conclusion
	if primaryFailure == "" {
		primaryFailure = "unknown"
	}

	// Collect failed job names
	var failedJobs []string
	for _, job := range processedRun.JobDetails {
		if job.Conclusion == "failure" || job.Conclusion == "timed_out" || job.Conclusion == "cancelled" {
			failedJobs = append(failedJobs, job.Name)
		}
	}

	// Generate error summary
	errorSummary := "No specific errors identified"
	if len(errors) > 0 {
		if len(errors) == 1 {
			errorSummary = errors[0].Message
		} else {
			errorSummary = fmt.Sprintf("%d errors: %s (and %d more)", len(errors), errors[0].Message, len(errors)-1)
		}
	}

	// Attempt to identify root cause
	rootCause := ""
	if len(processedRun.MCPFailures) > 0 {
		rootCause = fmt.Sprintf("MCP server failure: %s", processedRun.MCPFailures[0].ServerName)
	} else if len(errors) > 0 {
		// Look for common error patterns
		firstError := errors[0].Message
		if strings.Contains(strings.ToLower(firstError), "timeout") {
			rootCause = "Operation timeout"
		} else if strings.Contains(strings.ToLower(firstError), "permission") {
			rootCause = "Permission denied"
		} else if strings.Contains(strings.ToLower(firstError), "not found") {
			rootCause = "Resource not found"
		} else if strings.Contains(strings.ToLower(firstError), "authentication") {
			rootCause = "Authentication failure"
		}
	}

	return &FailureAnalysis{
		PrimaryFailure: primaryFailure,
		FailedJobs:     failedJobs,
		ErrorSummary:   errorSummary,
		RootCause:      rootCause,
	}
}

// generatePerformanceMetrics calculates aggregated performance statistics
func generatePerformanceMetrics(processedRun ProcessedRun, metrics MetricsData, toolUsage []ToolUsageInfo) *PerformanceMetrics {
	run := processedRun.Run
	auditReportLog.Printf("Generating performance metrics: token_usage=%d, tool_count=%d, duration=%v", metrics.TokenUsage, len(toolUsage), run.Duration)
	pm := &PerformanceMetrics{}

	auditReportLog.Printf("Calculating cost efficiency: estimated_cost=$%.2f", metrics.EstimatedCost)

	// Calculate tokens per minute
	if run.Duration > 0 && metrics.TokenUsage > 0 {
		minutes := run.Duration.Minutes()
		if minutes > 0 {
			pm.TokensPerMinute = float64(metrics.TokenUsage) / minutes
		}
	}

	// Determine cost efficiency
	if metrics.EstimatedCost > 0 && run.Duration > 0 {
		costPerMinute := metrics.EstimatedCost / run.Duration.Minutes()
		if costPerMinute < 0.01 {
			pm.CostEfficiency = "excellent"
		} else if costPerMinute < 0.05 {
			pm.CostEfficiency = "good"
		} else if costPerMinute < 0.10 {
			pm.CostEfficiency = "moderate"
		} else {
			pm.CostEfficiency = "poor"
		}
	}

	// Find most used tool
	if len(toolUsage) > 0 {
		mostUsed := toolUsage[0]
		for i := 1; i < len(toolUsage); i++ {
			if toolUsage[i].CallCount > mostUsed.CallCount {
				mostUsed = toolUsage[i]
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
