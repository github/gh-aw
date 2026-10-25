package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var observabilityInsightsLog = logger.New("cli:observability_insights")

type ObservabilityInsight struct {
	Category string `json:"category"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Evidence string `json:"evidence,omitempty"`
}

type workflowObservabilityStats struct {
	workflowName string
	runs         int
	failures     int
	timedOuts    int
	missingTools int
	mcpFailures  int
	missingData  int
	safeItems    int
	totalTurns   int
	minTurns     int
	maxTurns     int
	blocked      int
	totalNet     int
	blockedAtCap bool
}

func buildAuditObservabilityInsights(processedRun ProcessedRun, metrics MetricsData, toolUsage []ToolUsageInfo, createdItems []CreatedItemReport) []ObservabilityInsight {
	observabilityInsightsLog.Printf("Building audit observability insights: run_id=%d turns=%d tool_types=%d", processedRun.Run.DatabaseID, metrics.Turns, len(toolUsage))
	insights := make([]ObservabilityInsight, 0, 5)
	toolTypes := len(toolUsage)
	insights = appendAuditExecutionInsight(insights, metrics, toolTypes)
	insights = appendAuditActuationInsight(insights, processedRun, createdItems)
	insights = appendAuditCapabilityInsight(insights, processedRun)
	insights = appendAuditNetworkInsight(insights, processedRun)

	if processedRun.RedactedDomainsAnalysis != nil && processedRun.RedactedDomainsAnalysis.TotalDomains > 0 {
		insights = append(insights, ObservabilityInsight{
			Category: "privacy",
			Severity: "info",
			Title:    "Sensitive destinations were redacted",
			Summary:  fmt.Sprintf("Observability data preserved privacy boundaries by redacting %d domain(s) from emitted logs.", processedRun.RedactedDomainsAnalysis.TotalDomains),
			Evidence: fmt.Sprintf("redacted_domains=%d", processedRun.RedactedDomainsAnalysis.TotalDomains),
		})
	}
	observabilityInsightsLog.Printf("Audit observability insights built: count=%d", len(insights))
	return insights
}

func appendAuditExecutionInsight(insights []ObservabilityInsight, metrics MetricsData, toolTypes int) []ObservabilityInsight {
	switch {
	case metrics.Turns >= 12 || toolTypes >= 6:
		insights = append(insights, ObservabilityInsight{
			Category: "execution",
			Severity: "medium",
			Title:    "Exploratory execution path",
			Summary:  fmt.Sprintf("The agent used %d turns across %d tool types, which indicates adaptive planning instead of a strictly linear path.", metrics.Turns, toolTypes),
			Evidence: fmt.Sprintf("turns=%d tool_types=%d", metrics.Turns, toolTypes),
		})
	case metrics.Turns >= 6 || toolTypes >= 4:
		insights = append(insights, ObservabilityInsight{
			Category: "execution",
			Severity: "info",
			Title:    "Adaptive execution path",
			Summary:  fmt.Sprintf("The run stayed moderately dynamic with %d turns and %d tool types.", metrics.Turns, toolTypes),
			Evidence: fmt.Sprintf("turns=%d tool_types=%d", metrics.Turns, toolTypes),
		})
	default:
		insights = append(insights, ObservabilityInsight{
			Category: "execution",
			Severity: "info",
			Title:    "Directed execution path",
			Summary:  fmt.Sprintf("The run remained relatively linear with %d turns and %d tool types.", metrics.Turns, toolTypes),
			Evidence: fmt.Sprintf("turns=%d tool_types=%d", metrics.Turns, toolTypes),
		})
	}
	return insights
}

func appendAuditActuationInsight(insights []ObservabilityInsight, processedRun ProcessedRun, createdItems []CreatedItemReport) []ObservabilityInsight {
	createdCount := len(createdItems)
	safeItemsCount := processedRun.Run.SafeItemsCount
	if createdCount > 0 || safeItemsCount > 0 {
		insights = append(insights, ObservabilityInsight{
			Category: "actuation",
			Severity: "info",
			Title:    "Write path executed",
			Summary:  fmt.Sprintf("The workflow crossed from analysis into action, producing %d created item(s) and %d safe output action(s).", createdCount, safeItemsCount),
			Evidence: fmt.Sprintf("created_items=%d safe_items=%d", createdCount, safeItemsCount),
		})
	} else {
		insights = append(insights, ObservabilityInsight{
			Category: "actuation",
			Severity: "info",
			Title:    "Read-only posture observed",
			Summary:  "The workflow stayed in an analysis posture and did not emit any GitHub write actions.",
			Evidence: "created_items=0 safe_items=0",
		})
	}
	return insights
}

func appendAuditCapabilityInsight(insights []ObservabilityInsight, processedRun ProcessedRun) []ObservabilityInsight {
	frictionEvents := len(processedRun.MissingTools) + len(processedRun.MCPFailures) + len(processedRun.MissingData)
	if frictionEvents > 0 {
		severity := "medium"
		if len(processedRun.MCPFailures) > 0 || frictionEvents >= 3 {
			severity = "high"
		}
		insights = append(insights, ObservabilityInsight{
			Category: "tooling",
			Severity: severity,
			Title:    "Capability friction detected",
			Summary:  fmt.Sprintf("The run hit %d capability gap event(s): %d missing tool(s), %d MCP failure(s), and %d missing data signal(s).", frictionEvents, len(processedRun.MissingTools), len(processedRun.MCPFailures), len(processedRun.MissingData)),
			Evidence: fmt.Sprintf("missing_tools=%d mcp_failures=%d missing_data=%d", len(processedRun.MissingTools), len(processedRun.MCPFailures), len(processedRun.MissingData)),
		})
	}
	return insights
}

func appendAuditNetworkInsight(insights []ObservabilityInsight, processedRun ProcessedRun) []ObservabilityInsight {
	if processedRun.FirewallAnalysis != nil && processedRun.FirewallAnalysis.TotalRequests > 0 {
		blockedRate := float64(processedRun.FirewallAnalysis.BlockedRequests) / float64(processedRun.FirewallAnalysis.TotalRequests)
		severity := "info"
		title := "Network policy aligned"
		summary := fmt.Sprintf("The firewall observed %d request(s) with %d blocked, for a %.0f%% block rate.", processedRun.FirewallAnalysis.TotalRequests, processedRun.FirewallAnalysis.BlockedRequests, blockedRate*100)
		if processedRun.FirewallAnalysis.BlockedRequests > 0 {
			title = "Network friction detected"
			severity = "medium"
			blockedAtCap := processedRun.FirewallAnalysis.BlockedRequests >= firewallBlockedRequestCap
			if blockedRate >= 0.5 || (processedRun.FirewallAnalysis.BlockedRequests >= 10 && !blockedAtCap) {
				severity = "high"
			}
		}
		insights = append(insights, ObservabilityInsight{
			Category: "network",
			Severity: severity,
			Title:    title,
			Summary:  summary,
			Evidence: fmt.Sprintf("blocked=%d total=%d", processedRun.FirewallAnalysis.BlockedRequests, processedRun.FirewallAnalysis.TotalRequests),
		})
	}

	return insights
}

func buildLogsObservabilityInsights(processedRuns []ProcessedRun, toolUsage []ToolUsageSummary) []ObservabilityInsight {
	if len(processedRuns) == 0 {
		return nil
	}

	observabilityInsightsLog.Printf("Building logs observability insights: processed_runs=%d tool_usage_entries=%d", len(processedRuns), len(toolUsage))
	insights := make([]ObservabilityInsight, 0, 6)
	workflowStats, writeRuns, readOnlyRuns := collectWorkflowObservabilityStats(processedRuns)
	insights = appendWorkflowFailureInsight(insights, workflowStats)
	insights = appendWorkflowDriftInsight(insights, workflowStats)
	insights = appendWorkflowCapabilityInsight(insights, workflowStats)
	insights = appendWorkflowNetworkInsight(insights, workflowStats)
	if writeRuns > 0 || readOnlyRuns > 0 {
		insights = append(insights, ObservabilityInsight{
			Category: "actuation",
			Severity: "info",
			Title:    "Actuation mix summarized",
			Summary:  fmt.Sprintf("Across %d run(s), %d executed write-capable safe outputs and %d stayed read-only.", len(processedRuns), writeRuns, readOnlyRuns),
			Evidence: fmt.Sprintf("write_runs=%d read_only_runs=%d", writeRuns, readOnlyRuns),
		})
	}
	insights = appendToolConcentrationInsight(insights, toolUsage)
	observabilityInsightsLog.Printf("Logs observability insights built: count=%d write_runs=%d read_only_runs=%d", len(insights), writeRuns, readOnlyRuns)
	return insights
}

func collectWorkflowObservabilityStats(processedRuns []ProcessedRun) (map[string]*workflowObservabilityStats, int, int) {
	workflowStats := make(map[string]*workflowObservabilityStats)
	writeRuns := 0
	readOnlyRuns := 0

	for _, pr := range processedRuns {
		wfID := workflowIDFromRun(pr.Run.WorkflowPath, pr.Run.WorkflowName)
		stats, exists := workflowStats[wfID]
		if !exists {
			stats = &workflowObservabilityStats{
				workflowName: wfID,
				minTurns:     pr.Run.Turns,
				maxTurns:     pr.Run.Turns,
			}
			workflowStats[wfID] = stats
		}

		stats.runs++
		stats.totalTurns += pr.Run.Turns
		if stats.runs == 1 || pr.Run.Turns < stats.minTurns {
			stats.minTurns = pr.Run.Turns
		}
		if pr.Run.Turns > stats.maxTurns {
			stats.maxTurns = pr.Run.Turns
		}
		if pr.Run.Conclusion == "failure" {
			stats.failures++
		}
		if pr.Run.Conclusion == "timed_out" {
			stats.timedOuts++
		}
		stats.missingTools += len(pr.MissingTools)
		stats.mcpFailures += len(pr.MCPFailures)
		stats.missingData += len(pr.MissingData)
		stats.safeItems += pr.Run.SafeItemsCount
		if pr.Run.SafeItemsCount > 0 {
			writeRuns++
		} else {
			readOnlyRuns++
		}
		if pr.FirewallAnalysis != nil {
			stats.blocked += pr.FirewallAnalysis.BlockedRequests
			stats.totalNet += pr.FirewallAnalysis.TotalRequests
			if pr.FirewallAnalysis.BlockedRequests >= firewallBlockedRequestCap {
				stats.blockedAtCap = true
			}
		}
	}
	return workflowStats, writeRuns, readOnlyRuns
}

func appendWorkflowFailureInsight(insights []ObservabilityInsight, workflowStats map[string]*workflowObservabilityStats) []ObservabilityInsight {
	var failureHotspot *workflowObservabilityStats
	for _, stats := range workflowStats {
		if stats.failures == 0 {
			continue
		}
		if failureHotspot == nil || stats.failures > failureHotspot.failures || (stats.failures == failureHotspot.failures && stats.workflowName < failureHotspot.workflowName) {
			failureHotspot = stats
		}
	}
	if failureHotspot != nil {
		failureRate := float64(failureHotspot.failures) / float64(failureHotspot.runs)
		severity := "medium"
		if failureRate >= 0.5 {
			severity = "high"
		}
		observabilityInsightsLog.Printf("Failure hotspot detected: workflow=%s failures=%d runs=%d rate=%.2f", failureHotspot.workflowName, failureHotspot.failures, failureHotspot.runs, failureRate)
		insights = append(insights, ObservabilityInsight{
			Category: "reliability",
			Severity: severity,
			Title:    "Failure hotspot identified",
			Summary:  fmt.Sprintf("Workflow %s accounted for %d failure(s) across %d run(s), a %.0f%% failure rate.", failureHotspot.workflowName, failureHotspot.failures, failureHotspot.runs, failureRate*100),
			Evidence: fmt.Sprintf("workflow=%s failures=%d runs=%d", failureHotspot.workflowName, failureHotspot.failures, failureHotspot.runs),
		})
	}
	return insights
}

func appendWorkflowDriftInsight(insights []ObservabilityInsight, workflowStats map[string]*workflowObservabilityStats) []ObservabilityInsight {
	var driftHotspot *workflowObservabilityStats
	for _, stats := range workflowStats {
		if stats.runs < 2 {
			continue
		}
		if stats.maxTurns-stats.minTurns < 4 {
			continue
		}
		if driftHotspot == nil || (stats.maxTurns-stats.minTurns) > (driftHotspot.maxTurns-driftHotspot.minTurns) {
			driftHotspot = stats
		}
	}
	if driftHotspot != nil {
		avgTurns := float64(driftHotspot.totalTurns) / float64(driftHotspot.runs)
		observabilityInsightsLog.Printf("Execution drift detected: workflow=%s min_turns=%d max_turns=%d avg_turns=%.1f", driftHotspot.workflowName, driftHotspot.minTurns, driftHotspot.maxTurns, avgTurns)
		insights = append(insights, ObservabilityInsight{
			Category: "drift",
			Severity: "medium",
			Title:    "Execution drift observed",
			Summary:  fmt.Sprintf("Workflow %s varied from %d to %d turns across runs, which suggests changing task shape or unstable prompts (avg %.1f turns).", driftHotspot.workflowName, driftHotspot.minTurns, driftHotspot.maxTurns, avgTurns),
			Evidence: fmt.Sprintf("workflow=%s min_turns=%d max_turns=%d", driftHotspot.workflowName, driftHotspot.minTurns, driftHotspot.maxTurns),
		})
	}
	return insights
}

func appendWorkflowCapabilityInsight(insights []ObservabilityInsight, workflowStats map[string]*workflowObservabilityStats) []ObservabilityInsight {
	var toolingHotspot *workflowObservabilityStats
	for _, stats := range workflowStats {
		friction := stats.missingTools + stats.mcpFailures + stats.missingData
		if friction == 0 {
			continue
		}
		if toolingHotspot == nil || friction > (toolingHotspot.missingTools+toolingHotspot.mcpFailures+toolingHotspot.missingData) {
			toolingHotspot = stats
		}
	}
	if toolingHotspot != nil {
		friction := toolingHotspot.missingTools + toolingHotspot.mcpFailures + toolingHotspot.missingData
		severity := "medium"
		if toolingHotspot.mcpFailures > 0 || friction >= 4 {
			severity = "high"
		}
		insights = append(insights, ObservabilityInsight{
			Category: "tooling",
			Severity: severity,
			Title:    "Capability hotspot identified",
			Summary:  fmt.Sprintf("Workflow %s produced the most capability friction: %d missing tool(s), %d MCP failure(s), and %d missing data signal(s).", toolingHotspot.workflowName, toolingHotspot.missingTools, toolingHotspot.mcpFailures, toolingHotspot.missingData),
			Evidence: fmt.Sprintf("workflow=%s missing_tools=%d mcp_failures=%d missing_data=%d", toolingHotspot.workflowName, toolingHotspot.missingTools, toolingHotspot.mcpFailures, toolingHotspot.missingData),
		})
	}
	return insights
}

func appendWorkflowNetworkInsight(insights []ObservabilityInsight, workflowStats map[string]*workflowObservabilityStats) []ObservabilityInsight {
	var networkHotspot *workflowObservabilityStats
	var networkRate float64
	for _, stats := range workflowStats {
		if stats.totalNet == 0 || stats.blocked == 0 {
			continue
		}
		rate := float64(stats.blocked) / float64(stats.totalNet)
		if networkHotspot == nil || rate > networkRate {
			networkHotspot = stats
			networkRate = rate
		}
	}
	if networkHotspot != nil {
		severity := "medium"
		if networkRate >= 0.5 || (networkHotspot.blocked >= 10 && !networkHotspot.blockedAtCap) {
			severity = "high"
		}
		insights = append(insights, ObservabilityInsight{
			Category: "network",
			Severity: severity,
			Title:    "Network friction hotspot identified",
			Summary:  fmt.Sprintf("Workflow %s had the highest firewall block pressure with %d blocked request(s) out of %d total (%.0f%%).", networkHotspot.workflowName, networkHotspot.blocked, networkHotspot.totalNet, networkRate*100),
			Evidence: fmt.Sprintf("workflow=%s blocked=%d total=%d", networkHotspot.workflowName, networkHotspot.blocked, networkHotspot.totalNet),
		})
	}

	return insights
}

func appendToolConcentrationInsight(insights []ObservabilityInsight, toolUsage []ToolUsageSummary) []ObservabilityInsight {
	totalToolCalls := 0
	for _, tool := range toolUsage {
		totalToolCalls += tool.CallCount
	}
	if len(toolUsage) > 0 && totalToolCalls > 0 {
		topTool := toolUsage[0]
		share := float64(topTool.CallCount) / float64(totalToolCalls)
		if share >= 0.5 {
			severity := "info"
			if share >= 0.7 {
				severity = "medium"
			}
			insights = append(insights, ObservabilityInsight{
				Category: "tooling",
				Severity: severity,
				Title:    "Tool concentration observed",
				Summary:  fmt.Sprintf("Tool %q accounted for %.0f%% of observed tool calls, which suggests the workflow fleet depends heavily on a narrow capability path.", topTool.ToolName, share*100),
				Evidence: fmt.Sprintf("tool=%s calls=%d total_calls=%d", topTool.ToolName, topTool.CallCount, totalToolCalls),
			})
		}
	}

	return insights
}

func renderObservabilityInsights(insights []ObservabilityInsight) {
	renderObservabilityInsightsTo(os.Stderr, insights)
}

func renderObservabilityInsightsTo(w io.Writer, insights []ObservabilityInsight) {
	for _, insight := range insights {
		icon := "[info]"
		switch insight.Severity {
		case "critical":
			icon = "[critical]"
		case "high":
			icon = "[high]"
		case "medium":
			icon = "[medium]"
		case "low":
			icon = "[low]"
		}

		fmt.Fprintln(w, console.FormatSectionHeaderStderr(fmt.Sprintf("%s %s [%s]", icon, insight.Title, insight.Category)))
		fmt.Fprintln(w, console.FormatListItemStderr(insight.Summary))
		if strings.TrimSpace(insight.Evidence) != "" {
			fmt.Fprintln(w, console.FormatListItemStderr("Evidence: "+insight.Evidence))
		}
		fmt.Fprintln(w)
	}
}
