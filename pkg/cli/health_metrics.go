package cli

import (
	"fmt"
	"time"

	"github.com/githubnext/gh-aw/pkg/logger"
)

var healthMetricsLog = logger.New("cli:health_metrics")

// WorkflowHealth represents health metrics for a single workflow
type WorkflowHealth struct {
	WorkflowName  string        `json:"workflow_name" console:"header:Workflow"`
	TotalRuns     int           `json:"total_runs" console:"-"`
	SuccessCount  int           `json:"success_count" console:"-"`
	FailureCount  int           `json:"failure_count" console:"-"`
	SuccessRate   float64       `json:"success_rate" console:"-"`
	DisplayRate   string        `json:"-" console:"header:Success Rate"`
	Trend         string        `json:"trend" console:"header:Trend"`
	AvgDuration   time.Duration `json:"avg_duration" console:"-"`
	DisplayDur    string        `json:"-" console:"header:Avg Duration"`
	TotalTokens   int           `json:"total_tokens" console:"-"`
	AvgTokens     int           `json:"avg_tokens" console:"-"`
	DisplayTokens string        `json:"-" console:"header:Avg Tokens"`
	TotalCost     float64       `json:"total_cost" console:"-"`
	AvgCost       float64       `json:"avg_cost" console:"-"`
	DisplayCost   string        `json:"-" console:"header:Avg Cost ($)"`
	BelowThresh   bool          `json:"below_threshold" console:"-"`
}

// HealthSummary represents aggregated health metrics across all workflows
type HealthSummary struct {
	Period           string           `json:"period"`
	TotalWorkflows   int              `json:"total_workflows"`
	HealthyWorkflows int              `json:"healthy_workflows"`
	Workflows        []WorkflowHealth `json:"workflows"`
	BelowThreshold   int              `json:"below_threshold"`
}

// TrendDirection represents the trend of a workflow's health
type TrendDirection int

const (
	TrendImproving TrendDirection = iota
	TrendStable
	TrendDegrading
)

// String returns the visual indicator for the trend
func (t TrendDirection) String() string {
	switch t {
	case TrendImproving:
		return "↑"
	case TrendStable:
		return "→"
	case TrendDegrading:
		return "↓"
	default:
		return "?"
	}
}

// CalculateWorkflowHealth calculates health metrics for a workflow from its runs
func CalculateWorkflowHealth(workflowName string, runs []WorkflowRun, threshold float64) WorkflowHealth {
	healthMetricsLog.Printf("Calculating health for workflow: %s, runs: %d", workflowName, len(runs))

	if len(runs) == 0 {
		return WorkflowHealth{
			WorkflowName:  workflowName,
			DisplayRate:   "N/A",
			Trend:         "→",
			DisplayDur:    "N/A",
			DisplayTokens: "-",
			DisplayCost:   "-",
		}
	}

	// Calculate success and failure counts
	successCount := 0
	failureCount := 0
	var totalDuration time.Duration
	var totalTokens int
	var totalCost float64

	for _, run := range runs {
		if run.Conclusion == "success" {
			successCount++
		} else if isFailureConclusion(run.Conclusion) {
			failureCount++
		}
		totalDuration += run.Duration
		totalTokens += run.TokenUsage
		totalCost += run.EstimatedCost
	}

	totalRuns := len(runs)
	successRate := 0.0
	if totalRuns > 0 {
		successRate = float64(successCount) / float64(totalRuns) * 100
	}

	// Calculate average duration
	avgDuration := time.Duration(0)
	if totalRuns > 0 {
		avgDuration = totalDuration / time.Duration(totalRuns)
	}

	// Calculate average tokens and cost
	avgTokens := 0
	avgCost := 0.0
	if totalRuns > 0 {
		avgTokens = totalTokens / totalRuns
		avgCost = totalCost / float64(totalRuns)
	}

	// Calculate trend
	trend := calculateTrend(runs)

	// Format display values
	displayRate := fmt.Sprintf("%.0f%%  (%d/%d)", successRate, successCount, totalRuns)
	displayDur := formatDuration(avgDuration)
	displayTokens := formatTokens(avgTokens)
	displayCost := formatCost(avgCost)

	belowThreshold := successRate < threshold

	health := WorkflowHealth{
		WorkflowName:  workflowName,
		TotalRuns:     totalRuns,
		SuccessCount:  successCount,
		FailureCount:  failureCount,
		SuccessRate:   successRate,
		DisplayRate:   displayRate,
		Trend:         trend.String(),
		AvgDuration:   avgDuration,
		DisplayDur:    displayDur,
		TotalTokens:   totalTokens,
		AvgTokens:     avgTokens,
		DisplayTokens: displayTokens,
		TotalCost:     totalCost,
		AvgCost:       avgCost,
		DisplayCost:   displayCost,
		BelowThresh:   belowThreshold,
	}

	healthMetricsLog.Printf("Health calculated: workflow=%s, successRate=%.2f%%, trend=%s, avgCost=$%.3f", workflowName, successRate, trend.String(), avgCost)

	return health
}

// calculateTrend determines the trend direction based on recent vs older runs
func calculateTrend(runs []WorkflowRun) TrendDirection {
	if len(runs) < 4 {
		// Not enough data to determine trend
		return TrendStable
	}

	// Split runs into two halves: recent and older
	midpoint := len(runs) / 2
	recentRuns := runs[:midpoint]
	olderRuns := runs[midpoint:]

	// Calculate success rates for each half
	recentSuccess := calculateSuccessRate(recentRuns)
	olderSuccess := calculateSuccessRate(olderRuns)

	// Determine trend based on difference
	diff := recentSuccess - olderSuccess

	const improvementThreshold = 5.0  // 5% improvement
	const degradationThreshold = -5.0 // 5% degradation

	if diff >= improvementThreshold {
		return TrendImproving
	} else if diff <= degradationThreshold {
		return TrendDegrading
	}
	return TrendStable
}

// calculateSuccessRate calculates the success rate for a set of runs
func calculateSuccessRate(runs []WorkflowRun) float64 {
	if len(runs) == 0 {
		return 0.0
	}

	successCount := 0
	for _, run := range runs {
		if run.Conclusion == "success" {
			successCount++
		}
	}

	return float64(successCount) / float64(len(runs)) * 100
}

// formatDuration formats a duration in a human-readable format
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	// Round to seconds
	seconds := int(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}

	minutes := seconds / 60
	remainingSeconds := seconds % 60

	if minutes < 60 {
		if remainingSeconds > 0 {
			return fmt.Sprintf("%dm %ds", minutes, remainingSeconds)
		}
		return fmt.Sprintf("%dm", minutes)
	}

	hours := minutes / 60
	remainingMinutes := minutes % 60

	if remainingMinutes > 0 {
		return fmt.Sprintf("%dh %dm", hours, remainingMinutes)
	}
	return fmt.Sprintf("%dh", hours)
}

// CalculateHealthSummary calculates aggregated health metrics across all workflows
func CalculateHealthSummary(workflowHealths []WorkflowHealth, period string, threshold float64) HealthSummary {
	healthMetricsLog.Printf("Calculating health summary: workflows=%d, period=%s", len(workflowHealths), period)

	healthyCount := 0
	belowThresholdCount := 0

	for _, wh := range workflowHealths {
		if wh.SuccessRate >= threshold {
			healthyCount++
		}
		if wh.BelowThresh {
			belowThresholdCount++
		}
	}

	summary := HealthSummary{
		Period:           period,
		TotalWorkflows:   len(workflowHealths),
		HealthyWorkflows: healthyCount,
		Workflows:        workflowHealths,
		BelowThreshold:   belowThresholdCount,
	}

	healthMetricsLog.Printf("Health summary: total=%d, healthy=%d, below_threshold=%d", len(workflowHealths), healthyCount, belowThresholdCount)

	return summary
}

// FilterWorkflowsByName filters workflow runs by workflow name
func FilterWorkflowsByName(runs []WorkflowRun, workflowName string) []WorkflowRun {
	filtered := make([]WorkflowRun, 0)
	for _, run := range runs {
		if run.WorkflowName == workflowName {
			filtered = append(filtered, run)
		}
	}
	return filtered
}

// GroupRunsByWorkflow groups workflow runs by workflow name
func GroupRunsByWorkflow(runs []WorkflowRun) map[string][]WorkflowRun {
	grouped := make(map[string][]WorkflowRun)
	for _, run := range runs {
		grouped[run.WorkflowName] = append(grouped[run.WorkflowName], run)
	}
	return grouped
}

// formatTokens formats token count in a human-readable format
func formatTokens(tokens int) string {
	if tokens == 0 {
		return "-"
	}
	if tokens < 1000 {
		return fmt.Sprintf("%d", tokens)
	}
	if tokens < 1000000 {
		return fmt.Sprintf("%.1fK", float64(tokens)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(tokens)/1000000)
}

// formatCost formats cost in a human-readable format
func formatCost(cost float64) string {
	if cost == 0 {
		return "-"
	}
	if cost < 0.001 {
		return "< 0.001"
	}
	return fmt.Sprintf("%.3f", cost)
}
