package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/scanfindings"
)

// renderJSON outputs the audit data as JSON
func renderJSON(data AuditData) error {
	auditReportLog.Print("Rendering audit report as JSON")
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// renderConsole outputs the audit data in a compact, high-density format optimized
// for agentic readability. Each line carries maximum information with minimal decoration.
func renderConsole(data AuditData, logsPath string) {
	auditReportLog.Print("Rendering compact audit report to console")
	renderConsoleOverview(data)
	renderConsoleComparison(data.Comparison)
	renderConsoleFingerprint(data.BehaviorFingerprint)
	renderConsoleMetrics(data.Metrics)
	renderConsoleSession(data.SessionAnalysis)
	renderConsoleTokenUsage(data.FirewallTokenUsage)
	renderConsoleGitHubAPIUsage(data.GitHubRateLimitUsage)
	renderConsoleJobs(data.Jobs)
	renderConsolePrompt(data.PromptAnalysis)
	renderConsoleActionableSections(data)
	renderConsoleOperationalSections(data)
	renderConsolePolicyAndExperiments(data)
	renderConsoleLogsPath(logsPath)
}

func renderConsoleOverview(data AuditData) {
	fmt.Fprintf(os.Stderr, "%s %s | %s | %s | %s\n",
		renderConsoleStatusIcon(data.Overview.Conclusion),
		data.Overview.WorkflowName,
		data.Overview.Conclusion,
		data.Overview.Duration,
		data.Overview.URL,
	)
	fmt.Fprintf(os.Stderr, "  run=%d branch=%s event=%s engine=%s\n",
		data.Overview.RunID,
		data.Overview.Branch,
		data.Overview.Event,
		renderConsoleEngineInfo(data.EngineConfig),
	)
}

func renderConsoleStatusIcon(conclusion string) string {
	switch conclusion {
	case "failure":
		return "❌"
	case "cancelled":
		return "⚠️"
	default:
		return "✅"
	}
}

func renderConsoleEngineInfo(engineConfig *AuditEngineConfig) string {
	if engineConfig == nil {
		return ""
	}
	parts := []string{engineConfig.EngineID}
	if engineConfig.Model != "" {
		parts = append(parts, engineConfig.Model)
	}
	if engineConfig.Version != "" {
		parts = append(parts, "v"+engineConfig.Version)
	}
	return strings.Join(parts, "/")
}

func renderConsoleComparison(comparison *AuditComparisonData) {
	if comparison == nil || !comparison.BaselineFound {
		return
	}
	compLine := "  comparison:"
	if comparison.Classification != nil {
		compLine += " " + comparison.Classification.Label
	}
	if comparison.Baseline != nil {
		compLine += fmt.Sprintf(" vs baseline %d", comparison.Baseline.RunID)
	}
	if comparison.Recommendation != nil && comparison.Recommendation.Action != "" {
		compLine += " | " + comparison.Recommendation.Action
	}
	fmt.Fprintln(os.Stderr, compLine)
}

func renderConsoleFingerprint(fingerprint *BehaviorFingerprint) {
	if fingerprint == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "  fingerprint: %s/%s/%s/%s/%s\n",
		fingerprint.ExecutionStyle,
		fingerprint.ToolBreadth,
		fingerprint.ActuationStyle,
		fingerprint.ResourceProfile,
		fingerprint.DispatchMode,
	)
}

func renderConsoleMetrics(metrics MetricsData) {
	line := fmt.Sprintf("  metrics: errors=%d warnings=%d", metrics.ErrorCount, metrics.WarningCount)
	if metrics.Turns > 0 {
		line += fmt.Sprintf(" turns=%d", metrics.Turns)
	}
	if metrics.TokenUsage > 0 {
		line += " tokens=" + console.FormatNumber(metrics.TokenUsage)
	}
	if metrics.AIC > 0 {
		line += fmt.Sprintf(" aic=%.2f", metrics.AIC)
	}
	if metrics.WorkingSet != nil && metrics.WorkingSet.RebuildFactor != nil {
		line += fmt.Sprintf(" working-set-rebuild=%.2f×", *metrics.WorkingSet.RebuildFactor)
	}
	if metrics.ActionMinutes > 0 {
		line += fmt.Sprintf(" action_min=%.0f", metrics.ActionMinutes)
	}
	fmt.Fprintln(os.Stderr, line)
}

func renderConsoleSession(session *SessionAnalysis) {
	if session == nil {
		return
	}
	line := "  session:"
	if session.WallTime != "" {
		line += " wall=" + session.WallTime
	}
	if session.TurnCount > 0 {
		line += fmt.Sprintf(" turns=%d", session.TurnCount)
	}
	if session.TokensPerMinute > 0 {
		line += fmt.Sprintf(" tok/min=%.0f", session.TokensPerMinute)
	}
	if session.TimeoutDetected {
		line += " TIMEOUT"
	}
	if session.NoopCount > 0 {
		line += fmt.Sprintf(" noops=%d", session.NoopCount)
	}
	fmt.Fprintln(os.Stderr, line)
}

func renderConsoleTokenUsage(tokenUsage *TokenUsageSummary) {
	if tokenUsage == nil || tokenUsage.TotalRequests == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "  tokens: in=%s out=%s cache_read=%s reqs=%d steering=%s\n",
		console.FormatNumber(tokenUsage.TotalInputTokens),
		console.FormatNumber(tokenUsage.TotalOutputTokens),
		console.FormatNumber(tokenUsage.TotalCacheReadTokens),
		tokenUsage.TotalRequests,
		console.FormatNumber(tokenUsage.TotalSteeringEvents),
	)
	if len(tokenUsage.Warnings) > 0 {
		fmt.Fprintln(os.Stderr, "  token_usage_warnings:")
		for _, warning := range tokenUsage.Warnings {
			fmt.Fprintf(os.Stderr, "    %s\n", warning)
		}
	}
}

func renderConsoleGitHubAPIUsage(rateLimit *GitHubRateLimitUsage) {
	if rateLimit == nil {
		return
	}
	fmt.Fprintf(os.Stderr, "  github_api: calls=%s quota=%s/%s\n",
		console.FormatNumber(rateLimit.TotalRequestsMade),
		console.FormatNumber(rateLimit.CoreConsumed),
		console.FormatNumber(rateLimit.CoreLimit),
	)
}

func renderConsoleJobs(jobs []JobData) {
	if len(jobs) == 0 {
		return
	}
	allPassed := true
	jobParts := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if job.Conclusion != "success" && job.Conclusion != "skipped" {
			allPassed = false
		}
		jobParts = append(jobParts, fmt.Sprintf("%s:%s", job.Name, job.Duration))
	}
	if allPassed {
		fmt.Fprintf(os.Stderr, "  jobs: %d/%d passed [%s]\n", len(jobs), len(jobs), strings.Join(jobParts, " "))
		return
	}
	fmt.Fprintln(os.Stderr, "  jobs:")
	for _, job := range jobs {
		fmt.Fprintf(os.Stderr, "    %s %s (%s) %s\n", renderConsoleJobIcon(job.Conclusion), job.Name, job.Duration, job.Conclusion)
	}
}

func renderConsoleJobIcon(conclusion string) string {
	switch conclusion {
	case "failure":
		return "✗"
	case "skipped":
		return "○"
	case "cancelled":
		return "⊘"
	default:
		return "✓"
	}
}

func renderConsolePrompt(promptAnalysis *PromptAnalysis) {
	if promptAnalysis == nil {
		return
	}
	line := fmt.Sprintf("  prompt: %s chars", console.FormatNumber(promptAnalysis.PromptSize))
	if promptAnalysis.PromptFile != "" {
		line += " file=" + promptAnalysis.PromptFile
	}
	fmt.Fprintln(os.Stderr, line)
}

func renderConsoleActionableSections(data AuditData) {
	renderConsoleFindings(filterActionableFindings(data.KeyFindings))
	renderConsoleAssessments(data.AgenticAssessments)
	renderConsoleRecommendations(filterActionableRecommendations(data.Recommendations))
	renderConsoleInsights(filterActionableInsights(data.ObservabilityInsights))
	renderConsoleErrors(data.Errors)
	renderConsoleWarnings(data.Warnings)
}

func renderConsoleFindings(findings []AuditFinding) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  findings:")
	for _, finding := range findings {
		fmt.Fprintf(os.Stderr, "    [%s] %s: %s\n", strings.ToUpper(finding.Severity.String()), finding.Title, finding.Description)
	}
}

func renderConsoleAssessments(assessments []AgenticAssessment) {
	if len(assessments) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  assessments:")
	for _, assessment := range assessments {
		line := fmt.Sprintf("    [%s] %s", strings.ToUpper(assessment.Severity), assessment.Summary)
		if assessment.Evidence != "" {
			line += " | " + assessment.Evidence
		}
		fmt.Fprintln(os.Stderr, line)
	}
}

func renderConsoleRecommendations(recommendations []Recommendation) {
	if len(recommendations) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  recommendations:")
	for _, recommendation := range recommendations {
		fmt.Fprintf(os.Stderr, "    [%s] %s — %s\n", strings.ToUpper(recommendation.Priority), recommendation.Action, recommendation.Reason)
	}
}

func renderConsoleInsights(insights []ObservabilityInsight) {
	if len(insights) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  insights:")
	for _, insight := range insights {
		line := fmt.Sprintf("    [%s] %s", strings.ToUpper(insight.Severity), insight.Title)
		if insight.Evidence != "" {
			line += " | " + insight.Evidence
		}
		fmt.Fprintln(os.Stderr, line)
	}
}

func renderConsoleErrors(errors []ValidationIssue) {
	if len(errors) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  errors:")
	for _, err := range errors {
		if err.File != "" && err.Line > 0 {
			fmt.Fprintf(os.Stderr, "    %s:%d: %s\n", filepath.Base(err.File), err.Line, err.Message)
			continue
		}
		fmt.Fprintf(os.Stderr, "    %s\n", err.Message)
	}
}

func renderConsoleWarnings(warnings []ValidationIssue) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  warnings:")
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "    %s\n", warning.Message)
	}
}

func renderConsoleOperationalSections(data AuditData) {
	renderConsoleSkillActivations(data.SkillActivations)
	renderConsoleMissingTools(data.MissingTools)
	renderConsoleMCPFailures(data.MCPFailures)
	renderCompactMCPHealth(data.MCPServerHealth)
	renderConsoleSafeOutputs(data.SafeOutputSummary)
	renderConsoleCreatedItems(data.CreatedItems)
	renderConsoleToolUsage(data.ToolUsage)
	renderConsoleMCPToolUsage(data.MCPToolUsage)
	if data.FirewallAnalysis != nil && data.FirewallAnalysis.TotalRequests > 0 {
		renderCompactFirewall(data.FirewallAnalysis)
	}
}

func renderConsoleSkillActivations(activations []SkillActivation) {
	if len(activations) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  skill_activations:")
	for _, act := range activations {
		line := "    " + act.Name + ": " + act.Status
		if act.Source != "" {
			line += " (source: " + act.Source + ")"
		}
		fmt.Fprintln(os.Stderr, line)
	}
}

func renderConsoleMissingTools(missingTools []MissingToolReport) {
	if len(missingTools) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  missing_tools:")
	for _, tool := range missingTools {
		line := "    " + tool.Tool + ": " + tool.Reason
		if tool.Alternatives != "" {
			line += " (alt: " + tool.Alternatives + ")"
		}
		fmt.Fprintln(os.Stderr, line)
	}
}

func renderConsoleMCPFailures(failures []MCPFailureReport) {
	if len(failures) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  mcp_failures:")
	for _, failure := range failures {
		fmt.Fprintf(os.Stderr, "    %s: %s\n", failure.ServerName, failure.Status)
	}
}

func renderConsoleSafeOutputs(summary *SafeOutputSummary) {
	if summary == nil || summary.TotalItems == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "  safe_outputs: %d items — %s\n", summary.TotalItems, summary.Summary)
}

func renderConsoleCreatedItems(items []CreatedItemReport) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  created:")
	for _, item := range items {
		line := "    " + item.Type
		if item.URL != "" {
			line += " " + item.URL
		} else if item.Repo != "" && item.Number > 0 {
			line += fmt.Sprintf(" %s#%d", item.Repo, item.Number)
		}
		fmt.Fprintln(os.Stderr, line)
	}
}

func renderConsoleToolUsage(toolUsage []ToolUsageInfo) {
	if len(toolUsage) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  tools:")
	for _, tool := range toolUsage {
		line := fmt.Sprintf("    %s ×%d", tool.Name, tool.CallCount)
		if tool.MaxDuration != "" {
			line += " max=" + tool.MaxDuration
		}
		fmt.Fprintln(os.Stderr, line)
	}
}

func renderConsoleMCPToolUsage(mcpToolUsage *MCPToolUsageData) {
	if mcpToolUsage == nil || len(mcpToolUsage.Summary) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, "  mcp_tools:")
	for _, summary := range mcpToolUsage.Summary {
		line := fmt.Sprintf("    %s/%s ×%d", summary.ServerName, summary.ToolName, summary.CallCount)
		if summary.ErrorCount > 0 {
			line += fmt.Sprintf(" errors=%d", summary.ErrorCount)
		}
		if summary.MaxDuration != "" {
			line += " max=" + summary.MaxDuration
		}
		fmt.Fprintln(os.Stderr, line)
	}
	if mcpToolUsage.GuardPolicySummary != nil && mcpToolUsage.GuardPolicySummary.TotalBlocked > 0 {
		fmt.Fprintf(os.Stderr, "    guard_blocked: %d\n", mcpToolUsage.GuardPolicySummary.TotalBlocked)
	}
}

func renderConsolePolicyAndExperiments(data AuditData) {
	if data.PolicyAnalysis != nil && len(data.PolicyAnalysis.RuleHits) > 0 {
		fmt.Fprintf(os.Stderr, "  firewall_policy: %s\n", data.PolicyAnalysis.PolicySummary)
	}
	renderConsoleExperiments(data.Experiments)
	renderConsoleGraders(data.Graders)
}

func renderConsoleGraders(graders *GradersData) {
	if graders == nil || len(graders.Results) == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "  graders: pass=%d fail=%d error=%d unavailable=%d\n",
		graders.Passed, graders.Failed, graders.ErrorCount, graders.UnavailableCount)
	for _, result := range graders.Results {
		line := fmt.Sprintf("    %s %s=%s", result.Status, result.ID, formatGraderValue(result))
		if result.Threshold != nil {
			line += fmt.Sprintf(" threshold=%g", *result.Threshold)
		}
		if result.Direction != "" {
			line += " direction=" + result.Direction
		}
		if detail := graderDetailText(result); detail != "" {
			line += " (" + detail + ")"
		}
		fmt.Fprintln(os.Stderr, line)
	}
}

// graderDetailText returns the error text for failed graders, falling back to the
// informational message when no error was recorded.
func graderDetailText(result GraderResult) string {
	if result.Error != "" {
		return result.Error
	}
	return result.Message
}

func renderConsoleExperiments(experiments *ExperimentData) {
	if experiments == nil || len(experiments.Assignments) == 0 {
		return
	}
	parts := make([]string, 0, len(experiments.Assignments))
	for name, variant := range experiments.Assignments {
		parts = append(parts, name+"="+variant)
	}
	sort.Strings(parts)
	fmt.Fprintf(os.Stderr, "  experiments: %s\n", strings.Join(parts, " "))
}

func renderConsoleLogsPath(logsPath string) {
	absPath, _ := filepath.Abs(logsPath)
	fmt.Fprintf(os.Stderr, "  logs: %s\n", absPath)
}

// filterActionableFindings returns findings with severity > info/success
func filterActionableFindings(findings []AuditFinding) []AuditFinding {
	var result []AuditFinding
	for _, f := range findings {
		if f.Severity.AtLeast(scanfindings.SeverityLow) {
			result = append(result, f)
		}
	}
	return result
}

// filterActionableRecommendations returns high/medium priority recommendations
func filterActionableRecommendations(recs []Recommendation) []Recommendation {
	var result []Recommendation
	for _, r := range recs {
		if r.Priority == "high" || r.Priority == "medium" {
			result = append(result, r)
		}
	}
	return result
}

// filterActionableInsights returns insights with severity > info
func filterActionableInsights(insights []ObservabilityInsight) []ObservabilityInsight {
	var result []ObservabilityInsight
	for _, ins := range insights {
		if ins.Severity == "critical" || ins.Severity == "high" || ins.Severity == "medium" || ins.Severity == "low" {
			result = append(result, ins)
		}
	}
	return result
}

// renderCompactMCPHealth renders MCP health issues in compact form (only problems)
func renderCompactMCPHealth(health *MCPServerHealth) {
	if health == nil {
		return
	}
	// Only render if there are unhealthy servers
	hasIssues := false
	for _, server := range health.Servers {
		if server.Status != "healthy" {
			hasIssues = true
			break
		}
	}
	if !hasIssues {
		return
	}
	fmt.Fprintln(os.Stderr, "  mcp_health:")
	for _, server := range health.Servers {
		if server.Status != "healthy" {
			fmt.Fprintf(os.Stderr, "    %s: %s\n", server.ServerName, server.Status)
		}
	}
}

// renderCompactFirewall renders firewall analysis in compact form
func renderCompactFirewall(fa *FirewallAnalysis) {
	if fa == nil {
		return
	}
	line := fmt.Sprintf("  firewall: %d requests", fa.TotalRequests)
	if len(fa.BlockedDomains) > 0 {
		line += fmt.Sprintf(" blocked_domains=%d", len(fa.BlockedDomains))
	}
	fmt.Fprintln(os.Stderr, line)
}
