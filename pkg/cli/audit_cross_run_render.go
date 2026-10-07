package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/timeutil"
)

var crossRunRenderLog = logger.New("cli:audit_cross_run_render")

// renderCrossRunReportJSON outputs the cross-run report as JSON to stdout.
func renderCrossRunReportJSON(report *CrossRunAuditReport) error {
	crossRunRenderLog.Printf("Rendering cross-run report as JSON: runs_analyzed=%d, domains=%d", report.RunsAnalyzed, len(report.DomainInventory))
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

// renderCrossRunReportMarkdown outputs the cross-run report as Markdown to stdout.
func renderCrossRunReportMarkdown(report *CrossRunAuditReport) {
	renderCrossRunReportMarkdownToWriter(os.Stdout, report)
}

func renderCrossRunReportMarkdownToWriter(w io.Writer, report *CrossRunAuditReport) {
	crossRunRenderLog.Printf("Rendering cross-run report as markdown: runs_analyzed=%d, domains=%d", report.RunsAnalyzed, len(report.DomainInventory))
	fmt.Fprintln(w, "# Audit Report — Cross-Run Analysis")
	fmt.Fprintln(w)

	renderMarkdownExecutiveSummaryToWriter(w, report)
	renderMarkdownMetricsTrendToWriter(w, report.MetricsTrend)
	renderMarkdownModelRoutingToWriter(w, report.ModelRouting)
	renderMarkdownMCPHealthToWriter(w, report)
	renderMarkdownErrorTrendToWriter(w, report)
	renderMarkdownDomainInventoryToWriter(w, report)
	renderMarkdownDrain3InsightsToWriter(w, report.Drain3Insights)
	renderMarkdownClusterAnalysisToWriter(w, report.ClusterAnalysis)
	renderMarkdownPerRunBreakdownToWriter(w, report.PerRunBreakdown)
}

func renderMarkdownModelRoutingToWriter(w io.Writer, routing *ModelRoutingLogsSummary) {
	if routing == nil {
		return
	}
	writeModelRoutingReportLine(w, "## Model Routing\n\n")
	writeModelRoutingReportLine(w, "Classifier AIC: %.3f; deviated requests: %d/%d (%.1f%%)\n\n",
		routing.ClassifierAIC, routing.DeviatedRequests, routing.TotalRequests, routing.DeviatedTrafficShare*100)
	if len(routing.Routes) == 0 {
		return
	}
	writeModelRoutingReportLine(w, "| Labels (type / scope / complexity) | Mode | Selected | Router | Runs | Total AIC | Average AIC |\n")
	writeModelRoutingReportLine(w, "|---|---|---|---|---:|---:|---:|\n")
	for _, route := range routing.Routes {
		writeModelRoutingReportLine(w, "| %s / %s / %s | %s | %s:%s | %s | %d | %.3f | %.3f |\n",
			modelRoutingMarkdownCell(route.TaskType), modelRoutingMarkdownCell(route.Scope), modelRoutingMarkdownCell(route.Complexity),
			modelRoutingMarkdownCell(route.Mode), modelRoutingMarkdownCell(route.Model), modelRoutingMarkdownCell(route.Effort),
			modelRoutingMarkdownCell(route.RouterVersion), route.RunCount, route.TotalAIC, route.AverageAIC)
	}
	writeModelRoutingReportLine(w, "\n")
}

func writeModelRoutingReportLine(w io.Writer, format string, values ...any) {
	if _, err := fmt.Fprintf(w, format, values...); err != nil {
		crossRunRenderLog.Printf("Failed to write model routing report: %v", err)
	}
}

func modelRoutingMarkdownCell(value string) string {
	return strings.NewReplacer("|", `\|`, "`", "\\`").Replace(safeModelRoutingText(value))
}

func renderMarkdownExecutiveSummaryToWriter(w io.Writer, report *CrossRunAuditReport) {
	fmt.Fprintln(w, "## Executive Summary")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "| Metric | Value |\n")
	fmt.Fprintf(w, "|--------|-------|\n")
	fmt.Fprintf(w, "| Runs analyzed | %d |\n", report.RunsAnalyzed)
	fmt.Fprintf(w, "| Runs with firewall data | %d |\n", report.RunsWithData)
	fmt.Fprintf(w, "| Runs without firewall data | %d |\n", report.RunsWithoutData)
	fmt.Fprintf(w, "| Total requests | %d |\n", report.Summary.TotalRequests)
	fmt.Fprintf(w, "| Allowed requests | %d |\n", report.Summary.TotalAllowed)
	fmt.Fprintf(w, "| Blocked requests | %d |\n", report.Summary.TotalBlocked)
	fmt.Fprintf(w, "| Overall denial rate | %.1f%% |\n", report.Summary.OverallDenyRate*100)
	fmt.Fprintf(w, "| Unique domains | %d |\n", report.Summary.UniqueDomains)
	fmt.Fprintln(w)
}

func renderMarkdownMetricsTrendToWriter(w io.Writer, mt MetricsTrendData) {
	if mt.TotalTokens == 0 && mt.TotalTurns == 0 && mt.AvgDurationNs == 0 {
		return
	}

	fmt.Fprintln(w, "## Metrics Trends")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "| Metric | Total | Avg/run | Min | Max | Spikes |\n")
	fmt.Fprintf(w, "|--------|-------|---------|-----|-----|--------|\n")
	if mt.TotalTokens > 0 {
		spikes := "—"
		if len(mt.TokenSpikes) > 0 {
			spikes = "⚠ " + formatRunIDs(mt.TokenSpikes)
		}
		fmt.Fprintf(w, "| Token Trend | %d | %d | %d | %d | %s |\n",
			mt.TotalTokens, mt.AvgTokens, mt.MinTokens, mt.MaxTokens, spikes)
	}
	if mt.TotalTurns > 0 {
		fmt.Fprintf(w, "| Turns | %d | %.1f | — | %d | — |\n",
			mt.TotalTurns, mt.AvgTurns, mt.MaxTurns)
	}
	if mt.AvgDurationNs > 0 {
		fmt.Fprintf(w, "| Duration | — | %s | %s | %s | — |\n",
			timeutil.FormatDurationNs(mt.AvgDurationNs),
			timeutil.FormatDurationNs(mt.MinDurationNs),
			timeutil.FormatDurationNs(mt.MaxDurationNs))
	}
	fmt.Fprintln(w)
}

func renderMarkdownMCPHealthToWriter(w io.Writer, report *CrossRunAuditReport) {
	if len(report.MCPHealth) == 0 {
		return
	}
	fmt.Fprintf(w, "## MCP Server Health (%d runs)\n\n", report.RunsAnalyzed)
	fmt.Fprintf(w, "| Server | Connected | Error Rate | Total Calls | Errors | Status |\n")
	fmt.Fprintf(w, "|--------|-----------|------------|-------------|--------|--------|\n")
	for _, h := range report.MCPHealth {
		status := "✅ ok"
		if h.Unreliable {
			status = "⚠ unreliable"
		}
		fmt.Fprintf(w, "| `%s` | %d/%d | %.1f%% | %d | %d | %s |\n",
			h.ServerName, h.RunsConnected, h.TotalRuns,
			h.ErrorRate*100, h.ToolCallCount, h.ErrorCount, status)
	}
	fmt.Fprintln(w)
}

func renderMarkdownErrorTrendToWriter(w io.Writer, report *CrossRunAuditReport) {
	et := report.ErrorTrend
	if et.TotalErrors == 0 && et.TotalWarnings == 0 {
		return
	}
	fmt.Fprintln(w, "## Error Trend")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "| Metric | Value |\n")
	fmt.Fprintf(w, "|--------|-------|\n")
	fmt.Fprintf(w, "| Runs with errors | %d/%d (%.0f%%) |\n",
		et.RunsWithErrors, report.RunsAnalyzed,
		safePercent(et.RunsWithErrors, report.RunsAnalyzed))
	fmt.Fprintf(w, "| Total errors | %d |\n", et.TotalErrors)
	fmt.Fprintf(w, "| Avg errors/run | %.2f |\n", et.AvgErrorsPerRun)
	if et.TotalWarnings > 0 {
		fmt.Fprintf(w, "| Runs with warnings | %d/%d |\n", et.RunsWithWarnings, report.RunsAnalyzed)
		fmt.Fprintf(w, "| Total warnings | %d |\n", et.TotalWarnings)
	}
	fmt.Fprintln(w)
}

func renderMarkdownDomainInventoryToWriter(w io.Writer, report *CrossRunAuditReport) {
	if len(report.DomainInventory) == 0 {
		return
	}
	fmt.Fprintln(w, "## Domain Inventory")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "| Domain | Status | Seen In | Allowed | Blocked |\n")
	fmt.Fprintf(w, "|--------|--------|---------|---------|--------|\n")
	for _, entry := range report.DomainInventory {
		fmt.Fprintf(w, "| `%s` | %s %s | %d/%d runs | %d | %d |\n",
			entry.Domain, firewallStatusEmoji(entry.OverallStatus), entry.OverallStatus,
			entry.SeenInRuns, report.RunsAnalyzed, entry.TotalAllowed, entry.TotalBlocked)
	}
	fmt.Fprintln(w)
}

func renderMarkdownDrain3InsightsToWriter(w io.Writer, insights []ObservabilityInsight) {
	if len(insights) == 0 {
		return
	}
	crossRunRenderLog.Printf("Rendering markdown drain3 insights: count=%d", len(insights))
	fmt.Fprintln(w, "## Agent Event Pattern Analysis")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "| Severity | Category | Title | Summary |\n")
	fmt.Fprintf(w, "|----------|----------|-------|--------|\n")
	for _, insight := range insights {
		summary := insight.Summary
		if insight.Evidence != "" {
			summary += " (" + insight.Evidence + ")"
		}
		fmt.Fprintf(w, "| %s %s | %s | %s | %s |\n",
			renderSeverityIcon(insight.Severity), insight.Severity, insight.Category, insight.Title, summary)
	}
	fmt.Fprintln(w)
}

func renderMarkdownPerRunBreakdownToWriter(w io.Writer, runs []PerRunFirewallBreakdown) {
	if len(runs) == 0 {
		return
	}
	fmt.Fprintln(w, "## Per-Run Breakdown")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "| Run ID | Workflow | Conclusion | Duration | Firewall | Tokens | Turns | MCP Err | Errors |\n")
	fmt.Fprintf(w, "|--------|----------|------------|----------|----------|--------|-------|---------|--------|\n")
	for _, run := range runs {
		firewallCol, tokenStr, turnsStr, durStr := markdownPerRunFields(run)
		fmt.Fprintf(w, "| %d | %s | %s | %s | %s | %s | %s | %d | %d |\n",
			run.RunID, run.WorkflowName, run.Conclusion, durStr,
			firewallCol, tokenStr, turnsStr,
			run.MCPErrors, run.ErrorCount)
	}
	fmt.Fprintln(w)
}

func markdownPerRunFields(run PerRunFirewallBreakdown) (string, string, string, string) {
	firewallCol := "—"
	if run.HasData {
		firewallCol = fmt.Sprintf("%d/%d", run.Allowed, run.Blocked)
	}
	tokenStr := "—"
	if run.Tokens > 0 {
		tokenStr = console.FormatTokens(run.Tokens)
		if run.TokenSpike {
			tokenStr += " ⚠"
		}
	}
	turnsStr := "—"
	if run.Turns > 0 {
		turnsStr = strconv.Itoa(run.Turns)
	}
	durStr := "—"
	if run.Duration > 0 {
		durStr = timeutil.FormatDurationNs(int64(run.Duration))
	}
	return firewallCol, tokenStr, turnsStr, durStr
}

// renderCrossRunReportPretty outputs the cross-run report as formatted console output to stderr.
func renderCrossRunReportPretty(report *CrossRunAuditReport) {
	crossRunRenderLog.Printf("Rendering cross-run report as pretty output: runs_analyzed=%d, runs_with_data=%d, deny_rate=%.1f%%",
		report.RunsAnalyzed, report.RunsWithData, report.Summary.OverallDenyRate*100)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Audit Report — Cross-Run Analysis"))
	fmt.Fprintln(os.Stderr)

	renderPrettyExecutiveSummary(report)
	renderPrettyMetricsTrend(report.MetricsTrend)
	renderPrettyModelRouting(report.ModelRouting)
	renderPrettyMCPHealth(report)
	renderPrettyErrorTrend(report)
	renderPrettyDomainInventory(report)
	renderPrettyDrain3Insights(report.Drain3Insights)
	renderPrettyClusterAnalysis(report.ClusterAnalysis)
	renderPrettyPerRunBreakdown(report.PerRunBreakdown)
	renderPrettyFinalStatus(report)
}

func renderPrettyModelRouting(routing *ModelRoutingLogsSummary) {
	if routing == nil {
		return
	}
	writeModelRoutingReportLine(os.Stderr, "%s\n", console.FormatInfoMessage("Model Routing"))
	writeModelRoutingReportLine(os.Stderr, "  classifier_aic=%.3f deviated_requests=%d/%d (%.1f%%)\n",
		routing.ClassifierAIC, routing.DeviatedRequests, routing.TotalRequests, routing.DeviatedTrafficShare*100)
	for _, route := range routing.Routes {
		writeModelRoutingReportLine(os.Stderr, "  %s/%s/%s mode=%s selected=%s:%s router=%s runs=%d aic_total=%.3f aic_avg=%.3f\n",
			safeModelRoutingText(route.TaskType), safeModelRoutingText(route.Scope), safeModelRoutingText(route.Complexity),
			safeModelRoutingText(route.Mode), safeModelRoutingText(route.Model), safeModelRoutingText(route.Effort),
			safeModelRoutingText(route.RouterVersion), route.RunCount, route.TotalAIC, route.AverageAIC)
	}
	writeModelRoutingReportLine(os.Stderr, "\n")
}

func renderPrettyExecutiveSummary(report *CrossRunAuditReport) {
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Executive Summary"))
	fmt.Fprintf(os.Stderr, "  Runs analyzed:              %d\n", report.RunsAnalyzed)
	fmt.Fprintf(os.Stderr, "  Runs with firewall data:    %d\n", report.RunsWithData)
	fmt.Fprintf(os.Stderr, "  Runs without firewall data: %d\n", report.RunsWithoutData)
	fmt.Fprintf(os.Stderr, "  Total requests:             %d\n", report.Summary.TotalRequests)
	fmt.Fprintf(os.Stderr, "  Allowed / Blocked:          %d / %d\n", report.Summary.TotalAllowed, report.Summary.TotalBlocked)
	fmt.Fprintf(os.Stderr, "  Overall denial rate:        %.1f%%\n", report.Summary.OverallDenyRate*100)
	fmt.Fprintf(os.Stderr, "  Unique domains:             %d\n", report.Summary.UniqueDomains)
	fmt.Fprintln(os.Stderr)
}

func renderPrettyMetricsTrend(mt MetricsTrendData) {
	if mt.TotalTokens == 0 && mt.TotalTurns == 0 && mt.AvgDurationNs == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Metrics Trends"))
	renderPrettyTokenTrend(mt)
	renderPrettyTurnTrend(mt)
	renderPrettyDurationTrend(mt)
	fmt.Fprintln(os.Stderr)
}

func renderPrettyTokenTrend(mt MetricsTrendData) {
	if mt.TotalTokens == 0 {
		return
	}
	fmt.Fprintf(os.Stderr, "  Tokens:   total=%s  avg=%s/run  min=%s  max=%s\n%s",
		console.FormatTokens(mt.TotalTokens), console.FormatTokens(mt.AvgTokens),
		console.FormatTokens(mt.MinTokens), console.FormatTokens(mt.MaxTokens), prettySpikeNote("Token", mt.TokenSpikes))
}

func renderPrettyTurnTrend(mt MetricsTrendData) {
	if mt.TotalTurns > 0 {
		fmt.Fprintf(os.Stderr, "  Turns:    total=%d  avg=%.1f/run  max=%d\n", mt.TotalTurns, mt.AvgTurns, mt.MaxTurns)
	}
}

func renderPrettyDurationTrend(mt MetricsTrendData) {
	if mt.AvgDurationNs > 0 {
		fmt.Fprintf(os.Stderr, "  Duration: avg=%s  min=%s  max=%s\n",
			timeutil.FormatDurationNs(mt.AvgDurationNs),
			timeutil.FormatDurationNs(mt.MinDurationNs),
			timeutil.FormatDurationNs(mt.MaxDurationNs))
	}
}

func prettySpikeNote(label string, runIDs []int64) string {
	if len(runIDs) == 0 {
		return ""
	}
	return fmt.Sprintf("  ⚠ %s spikes in runs: %s\n", label, formatRunIDs(runIDs))
}

func renderPrettyMCPHealth(report *CrossRunAuditReport) {
	if len(report.MCPHealth) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("MCP Server Health (%d runs)", report.RunsAnalyzed)))
	for _, h := range report.MCPHealth {
		statusIcon := "✅"
		if h.Unreliable {
			statusIcon = "⚠"
		}
		fmt.Fprintf(os.Stderr, "  %s %-30s  connected=%d/%d  calls=%d  errors=%d  error_rate=%.1f%%\n",
			statusIcon, h.ServerName, h.RunsConnected, h.TotalRuns,
			h.ToolCallCount, h.ErrorCount, h.ErrorRate*100)
	}
	fmt.Fprintln(os.Stderr)
}

func renderPrettyErrorTrend(report *CrossRunAuditReport) {
	et := report.ErrorTrend
	if et.TotalErrors == 0 && et.TotalWarnings == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Error Trend"))
	fmt.Fprintf(os.Stderr, "  Runs with errors:  %d/%d (%.0f%%)\n",
		et.RunsWithErrors, report.RunsAnalyzed,
		safePercent(et.RunsWithErrors, report.RunsAnalyzed))
	fmt.Fprintf(os.Stderr, "  Total errors:      %d (avg=%.2f/run)\n", et.TotalErrors, et.AvgErrorsPerRun)
	if et.TotalWarnings > 0 {
		fmt.Fprintf(os.Stderr, "  Total warnings:    %d (%d runs)\n", et.TotalWarnings, et.RunsWithWarnings)
	}
	fmt.Fprintln(os.Stderr)
}

func renderPrettyDomainInventory(report *CrossRunAuditReport) {
	if len(report.DomainInventory) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Domain Inventory (%d domains)", len(report.DomainInventory))))
	for _, entry := range report.DomainInventory {
		fmt.Fprintf(os.Stderr, "  %s %-45s  %s  seen=%d/%d  allowed=%d  blocked=%d\n",
			firewallStatusEmoji(entry.OverallStatus), entry.Domain, entry.OverallStatus,
			entry.SeenInRuns, report.RunsAnalyzed, entry.TotalAllowed, entry.TotalBlocked)
	}
	fmt.Fprintln(os.Stderr)
}

func renderPrettyDrain3Insights(insights []ObservabilityInsight) {
	if len(insights) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("Agent Event Pattern Analysis (%d insights)", len(insights))))
	for _, insight := range insights {
		fmt.Fprintf(os.Stderr, "  %s [%s/%s] %s\n", renderSeverityIcon(insight.Severity), insight.Category, insight.Severity, insight.Title)
		fmt.Fprintf(os.Stderr, "     %s\n", insight.Summary)
		if insight.Evidence != "" {
			fmt.Fprintf(os.Stderr, "     evidence: %s\n", insight.Evidence)
		}
	}
	fmt.Fprintln(os.Stderr)
}

func renderSeverityIcon(severity string) string {
	switch severity {
	case "high":
		return "🔴"
	case "medium":
		return "🟠"
	case "low":
		return "🟡"
	default:
		return "ℹ"
	}
}

func renderPrettyPerRunBreakdown(runs []PerRunFirewallBreakdown) {
	if len(runs) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Per-Run Breakdown"))
	for _, run := range runs {
		fmt.Fprintln(os.Stderr, prettyPerRunLine(run))
	}
	fmt.Fprintln(os.Stderr)
}

func prettyPerRunLine(run PerRunFirewallBreakdown) string {
	prefix := fmt.Sprintf("  Run #%-12d  %-30s  %-10s", run.RunID, stringutil.Truncate(run.WorkflowName, 30), run.Conclusion)
	optional := prettyPerRunOptionalFields(run)
	if !run.HasData {
		return fmt.Sprintf("%s  (no firewall data)%s  mcp_errors=%d  errors=%d", prefix, optional, run.MCPErrors, run.ErrorCount)
	}
	return fmt.Sprintf("%s  requests=%d  allowed=%d  blocked=%d  deny=%.1f%%  domains=%d%s  turns=%d  mcp_errors=%d  errors=%d",
		prefix, run.TotalRequests, run.Allowed, run.Blocked, run.DenyRate*100,
		run.UniqueDomains, optional, run.Turns, run.MCPErrors, run.ErrorCount)
}

func prettyPerRunOptionalFields(run PerRunFirewallBreakdown) string {
	parts := strings.Builder{}
	if run.Duration > 0 {
		parts.WriteString("  dur=")
		parts.WriteString(timeutil.FormatDurationNs(int64(run.Duration)))
	}
	if run.Tokens > 0 {
		parts.WriteString("  tokens=")
		parts.WriteString(console.FormatTokens(run.Tokens))
		if run.TokenSpike {
			parts.WriteString("⚠")
		}
	}
	return parts.String()
}

func renderPrettyFinalStatus(report *CrossRunAuditReport) {
	if report.RunsWithData == 0 && len(report.MCPHealth) == 0 && report.MetricsTrend.TotalTokens == 0 {
		crossRunRenderLog.Printf("No data found in any analyzed runs: runs_analyzed=%d", report.RunsAnalyzed)
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage("No data found in any of the analyzed runs."))
		return
	}

	parts := []string{fmt.Sprintf("%d runs analyzed", report.RunsAnalyzed)}
	if report.Summary.UniqueDomains > 0 {
		parts = append(parts, fmt.Sprintf("%d unique domains", report.Summary.UniqueDomains))
		parts = append(parts, fmt.Sprintf("%.1f%% overall denial rate", report.Summary.OverallDenyRate*100))
	}
	fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Report complete: "+strings.Join(parts, ", ")))
}

// formatRunIDs formats a slice of run IDs as a comma-separated string.
func formatRunIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = fmt.Sprintf("#%d", id)
	}
	return strings.Join(parts, ", ")
}
