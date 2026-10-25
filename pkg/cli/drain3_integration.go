package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/agentdrain"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/setutil"
)

var drain3Log = logger.New("cli:drain3_integration")

// defaultAgentDrainStages lists the stage names recognised by the coordinator.
var defaultAgentDrainStages = []string{
	"plan", "tool_call", "tool_result", "retry", "error", "finish",
}

// buildDrain3Insights analyses a single ProcessedRun using Drain3-style template
// mining and returns additional ObservabilityInsights to be appended to the
// existing insight list.
func buildDrain3Insights(processedRun ProcessedRun, metrics MetricsData, toolUsage []ToolUsageInfo) []ObservabilityInsight {
	drain3Log.Printf("Building drain3 insights: run_id=%d turns=%d tools=%d mcpFailures=%d missingTools=%d",
		processedRun.Run.DatabaseID, metrics.Turns, len(toolUsage), len(processedRun.MCPFailures), len(processedRun.MissingTools))

	cfg := agentdrain.DefaultConfig()
	coordinator, err := agentdrain.NewCoordinator(cfg, defaultAgentDrainStages)
	if err != nil {
		drain3Log.Printf("Failed to create drain3 coordinator: %v", err)
		return nil
	}
	if err := coordinator.LoadDefaultWeights(); err != nil {
		drain3Log.Printf("Failed to load default drain3 weights: %v", err)
	}

	events := buildAgentEventsFromProcessedRun(processedRun, metrics, toolUsage)
	if len(events) == 0 {
		return nil
	}

	var anomalies []struct {
		evt    agentdrain.AgentEvent
		result *agentdrain.MatchResult
		report *agentdrain.AnomalyReport
	}

	for _, evt := range events {
		result, report, err := coordinator.AnalyzeEvent(evt)
		if err != nil {
			// Unknown stage – skip gracefully.
			drain3Log.Printf("AnalyzeEvent failed for stage=%s: %v", evt.Stage, err)
			continue
		}
		if report != nil && report.AnomalyScore > 0.5 {
			anomalies = append(anomalies, struct {
				evt    agentdrain.AgentEvent
				result *agentdrain.MatchResult
				report *agentdrain.AnomalyReport
			}{evt, result, report})
		}
	}

	return buildInsightsFromDrain3Analysis(coordinator, anomalies, events)
}

// buildDrain3InsightsMultiRun analyses multiple ProcessedRuns using a shared
// Drain3 coordinator, which allows cross-run pattern detection.  It returns
// additional ObservabilityInsights.
func buildDrain3InsightsMultiRun(processedRuns []ProcessedRun) []ObservabilityInsight {
	if len(processedRuns) == 0 {
		return nil
	}
	drain3Log.Printf("Building drain3 multi-run insights: runs=%d", len(processedRuns))

	cfg := agentdrain.DefaultConfig()
	coordinator, err := agentdrain.NewCoordinator(cfg, defaultAgentDrainStages)
	if err != nil {
		drain3Log.Printf("Failed to create drain3 coordinator: %v", err)
		return nil
	}
	if err := coordinator.LoadDefaultWeights(); err != nil {
		drain3Log.Printf("Failed to load default drain3 weights: %v", err)
	}

	totalEvents := 0
	var highAnomalies []struct {
		evt    agentdrain.AgentEvent
		result *agentdrain.MatchResult
		report *agentdrain.AnomalyReport
	}

	for _, pr := range processedRuns {
		events := buildAgentEventsFromProcessedRun(pr, MetricsData{
			Turns:        pr.Run.Turns,
			TokenUsage:   pr.Run.TokenUsage,
			ErrorCount:   pr.Run.ErrorCount,
			WarningCount: pr.Run.WarningCount,
		}, nil)
		totalEvents += len(events)

		for _, evt := range events {
			result, report, err := coordinator.AnalyzeEvent(evt)
			if err != nil {
				continue
			}
			if report != nil && report.AnomalyScore > 0.6 {
				highAnomalies = append(highAnomalies, struct {
					evt    agentdrain.AgentEvent
					result *agentdrain.MatchResult
					report *agentdrain.AnomalyReport
				}{evt, result, report})
			}
		}
	}

	if totalEvents == 0 {
		return nil
	}

	return buildMultiRunInsightsFromDrain3(coordinator, highAnomalies, len(processedRuns), totalEvents)
}

// buildAgentEventsFromProcessedRun converts the structured data in a ProcessedRun
// into a slice of AgentEvents suitable for drain3 ingestion.
func buildAgentEventsFromProcessedRun(processedRun ProcessedRun, metrics MetricsData, toolUsage []ToolUsageInfo) []agentdrain.AgentEvent {
	var events []agentdrain.AgentEvent

	// Synthesise a planning event from overall metrics.
	if metrics.Turns > 0 {
		events = append(events, agentdrain.AgentEvent{
			Stage: "plan",
			Fields: map[string]string{
				"turns":  strconv.Itoa(metrics.Turns),
				"errors": strconv.Itoa(metrics.ErrorCount),
			},
		})
	}

	// Tool-call events from the per-tool usage summary.
	for _, tu := range toolUsage {
		events = append(events, agentdrain.AgentEvent{
			Stage: "tool_call",
			Fields: map[string]string{
				"tool":  tu.Name,
				"calls": strconv.Itoa(tu.CallCount),
			},
		})
	}

	events = appendDrain3ErrorEvents(events, processedRun)
	return appendDrain3CompletionEvents(events, processedRun, metrics)
}

func appendDrain3ErrorEvents(events []agentdrain.AgentEvent, processedRun ProcessedRun) []agentdrain.AgentEvent {
	// MCP failures become error-stage events.
	for _, f := range processedRun.MCPFailures {
		events = append(events, agentdrain.AgentEvent{
			Stage: "error",
			Fields: map[string]string{
				"type":   "mcp_failure",
				"server": f.ServerName,
				"status": f.Status,
			},
		})
	}

	// Missing tools are capability-friction errors.
	for _, mt := range processedRun.MissingTools {
		events = append(events, agentdrain.AgentEvent{
			Stage: "error",
			Fields: map[string]string{
				"type":   "missing_tool",
				"tool":   mt.Tool,
				"reason": mt.Reason,
			},
		})
	}

	// Missing data is a different error class.
	for _, md := range processedRun.MissingData {
		events = append(events, agentdrain.AgentEvent{
			Stage: "error",
			Fields: map[string]string{
				"type":      "missing_data",
				"data_type": md.DataType,
				"reason":    md.Reason,
			},
		})
	}

	return events
}

func appendDrain3CompletionEvents(events []agentdrain.AgentEvent, processedRun ProcessedRun, metrics MetricsData) []agentdrain.AgentEvent {
	// No-ops map to tool_result stage.
	for _, n := range processedRun.Noops {
		events = append(events, agentdrain.AgentEvent{
			Stage: "tool_result",
			Fields: map[string]string{
				"status":  "noop",
				"message": n.Message,
			},
		})
	}

	// Synthesise a finish event only when the run has a meaningful conclusion.
	conclusion := processedRun.Run.Conclusion
	if conclusion == "" {
		conclusion = processedRun.Run.Status
	}
	if conclusion != "" || metrics.TokenUsage > 0 {
		events = append(events, agentdrain.AgentEvent{
			Stage: "finish",
			Fields: map[string]string{
				"status": conclusion,
				"tokens": strconv.Itoa(metrics.TokenUsage),
			},
		})
	}

	return events
}

// buildInsightsFromDrain3Analysis converts drain3 coordinator analysis into
// ObservabilityInsights for a single run.
func buildInsightsFromDrain3Analysis(
	coordinator *agentdrain.Coordinator,
	anomalies []struct {
		evt    agentdrain.AgentEvent
		result *agentdrain.MatchResult
		report *agentdrain.AnomalyReport
	},
	events []agentdrain.AgentEvent,
) []ObservabilityInsight {
	var insights []ObservabilityInsight

	// Cluster summary insight.
	allClusters := coordinator.AllClusters()
	totalClusters := 0
	for _, cs := range allClusters {
		totalClusters += len(cs)
	}
	if totalClusters > 0 {
		stageBreakdown := buildStageBreakdown(allClusters)
		insights = append(insights, ObservabilityInsight{
			Category: "execution",
			Severity: "info",
			Title:    "Log template patterns mined",
			Summary: fmt.Sprintf(
				"Analysis identified %d distinct event templates across %d pipeline stages from %d events.",
				totalClusters, len(allClusters), len(events),
			),
			Evidence: stageBreakdown,
		})
	}

	// Anomaly insight.
	if len(anomalies) > 0 {
		severity := "low"
		if len(anomalies) >= 3 {
			severity = "high"
		} else if len(anomalies) >= 2 {
			severity = "medium"
		}
		reasons := buildAnomalyReasons(anomalies)
		insights = append(insights, ObservabilityInsight{
			Category: "reliability",
			Severity: severity,
			Title:    fmt.Sprintf("%d anomalous event pattern(s) detected", len(anomalies)),
			Summary: fmt.Sprintf(
				"Anomaly detection flagged %d event(s) as unusual based on template similarity and cluster rarity.",
				len(anomalies),
			),
			Evidence: reasons,
		})
	}

	// Stage sequence insight.
	sequence := agentdrain.StageSequence(events)
	if sequence != "" {
		insights = append(insights, ObservabilityInsight{
			Category: "execution",
			Severity: "info",
			Title:    "Agent stage sequence",
			Summary:  "The observed pipeline stage sequence for this run.",
			Evidence: sequence,
		})
	}

	return insights
}

// buildMultiRunInsightsFromDrain3 converts cross-run drain3 analysis into insights.
func buildMultiRunInsightsFromDrain3(
	coordinator *agentdrain.Coordinator,
	highAnomalies []struct {
		evt    agentdrain.AgentEvent
		result *agentdrain.MatchResult
		report *agentdrain.AnomalyReport
	},
	runCount, totalEvents int,
) []ObservabilityInsight {
	var insights []ObservabilityInsight

	allClusters := coordinator.AllClusters()
	totalClusters := 0
	for _, cs := range allClusters {
		totalClusters += len(cs)
	}

	if totalClusters > 0 {
		stageBreakdown := buildStageBreakdown(allClusters)
		insights = append(insights, ObservabilityInsight{
			Category: "execution",
			Severity: "info",
			Title:    "Cross-run log template patterns",
			Summary: fmt.Sprintf(
				"Mined %d distinct event templates across %d pipeline stages from %d events in %d runs.",
				totalClusters, len(allClusters), totalEvents, runCount,
			),
			Evidence: stageBreakdown,
		})
	}

	if len(highAnomalies) > 0 {
		severity := "medium"
		if len(highAnomalies) >= 5 {
			severity = "high"
		}
		reasons := buildAnomalyReasons(highAnomalies)
		insights = append(insights, ObservabilityInsight{
			Category: "reliability",
			Severity: severity,
			Title:    fmt.Sprintf("%d high-anomaly events across %d runs", len(highAnomalies), runCount),
			Summary: fmt.Sprintf(
				"Cross-run analysis flagged %d events with anomaly score > 0.6, indicating unusual patterns relative to the learned templates.",
				len(highAnomalies),
			),
			Evidence: reasons,
		})
	}

	return insights
}

// buildStageBreakdown builds a human-readable stage → cluster-count string.
func buildStageBreakdown(allClusters map[string][]agentdrain.Cluster) string {
	if len(allClusters) == 0 {
		return ""
	}
	parts := make([]string, 0, len(allClusters))
	for stage, clusters := range allClusters {
		if len(clusters) > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", stage, len(clusters)))
		}
	}
	return strings.Join(parts, " ")
}

// buildAnomalyReasons summarises anomaly reasons into a compact evidence string.
func buildAnomalyReasons(anomalies []struct {
	evt    agentdrain.AgentEvent
	result *agentdrain.MatchResult
	report *agentdrain.AnomalyReport
}) string {
	reasons := make([]string, 0, len(anomalies))
	seen := make(map[string]struct {
	})
	for _, a := range anomalies {
		r := fmt.Sprintf("stage=%s score=%.2f: %s", a.evt.Stage, a.report.AnomalyScore, a.report.Reason)
		if !setutil.Contains(seen, r) {
			reasons = append(reasons, r)
			seen[r] = struct {
			}{}
		}
		if len(reasons) >= 5 {
			break
		}
	}
	return strings.Join(reasons, "; ")
}
