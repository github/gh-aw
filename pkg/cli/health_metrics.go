package cli

import (
	"fmt"
	"slices"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stats"
	"github.com/github/gh-aw/pkg/timeutil"
)

var healthMetricsLog = logger.New("cli:health_metrics")

// WorkflowHealth represents health metrics for a single workflow
type WorkflowHealth struct {
	WorkflowPath           string                   `json:"workflow_path,omitempty" console:"-"`
	Revisions              []HealthRevisionStatus   `json:"revisions,omitempty" console:"-"`
	Status                 string                   `json:"status,omitempty" console:"header:Status"`
	Collection             *HealthCollection        `json:"collection,omitempty" console:"-"`
	FailureClusters        []FailureCluster         `json:"failure_clusters,omitempty" console:"-"`
	FailureInvestigation   *FailureInvestigation    `json:"failure_investigation,omitempty" console:"-"`
	WorkflowName           string                   `json:"workflow_name" console:"header:Workflow"`
	TotalRuns              int                      `json:"total_runs" console:"-"`
	SuccessCount           int                      `json:"success_count" console:"-"`
	FailureCount           int                      `json:"failure_count" console:"-"`
	DriverExitCount        int                      `json:"driver_exit_count" console:"-"`
	AgentLogicFailureCount int                      `json:"agent_logic_failure_count" console:"-"`
	SubagentFailureCount   int                      `json:"subagent_failure_count" console:"header:Sub-agent Failures"`
	SubagentFailureRuns    int                      `json:"subagent_failure_runs" console:"-"`
	SubagentFailures       []HealthSubagentFailure  `json:"subagent_failures,omitempty" console:"-"`
	DetectionFailureRuns   int                      `json:"detection_failure_runs" console:"header:Detection Failure Runs"`
	DetectionFailures      []HealthDetectionFailure `json:"detection_failures,omitempty" console:"-"`
	DurableSessions        *HealthDurableSessions   `json:"durable_sessions,omitempty" console:"-"`
	SuccessRate            float64                  `json:"success_rate" console:"-"`
	DisplayRate            string                   `json:"-" console:"header:Success Rate"`
	Trend                  string                   `json:"trend" console:"header:Trend"`
	AvgDuration            time.Duration            `json:"avg_duration" console:"-"`
	DisplayDur             string                   `json:"-" console:"header:Avg Duration"`
	TotalTokens            int                      `json:"total_tokens" console:"-"`
	AvgTokens              int                      `json:"avg_tokens" console:"-"`
	DisplayTokens          string                   `json:"-" console:"header:Avg Tokens"`
	BelowThresh            bool                     `json:"below_threshold" console:"-"`
	// IntentionalFailure is true when the workflow is tagged with intentional-failure: true.
	// These workflows are excluded from fleet-health and prod-main success-rate rollups.
	IntentionalFailure bool `json:"intentional_failure,omitempty" console:"-"`
}

// HealthSummary represents aggregated health metrics across all workflows
type HealthSummary struct {
	StatusCounts             map[string]int        `json:"status_counts"`
	ObservedFailureWorkflows int                   `json:"observed_failure_workflows"`
	InsufficientWorkflows    int                   `json:"insufficient_evidence_workflows"`
	Collection               *HealthCollection     `json:"collection,omitempty"`
	FailureClusters          []FailureCluster      `json:"failure_clusters,omitempty"`
	FailureInvestigation     *FailureInvestigation `json:"failure_investigation,omitempty"`
	Period                   string                `json:"period"`
	TotalWorkflows           int                   `json:"total_workflows"`
	HealthyWorkflows         int                   `json:"healthy_workflows"`
	Workflows                []WorkflowHealth      `json:"workflows"`
	BelowThreshold           int                   `json:"below_threshold"`
	DegradedWorkflows        int                   `json:"degraded_workflows"`
	SubagentFailureCount     int                   `json:"subagent_failure_count"`
	SubagentFailureRuns      int                   `json:"subagent_failure_runs"`
	DetectionFailureRuns     int                   `json:"detection_failure_runs"`
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
	runs = slices.DeleteFunc(slices.Clone(runs), isIgnoredHealthRun)
	healthMetricsLog.Printf("Calculating health for workflow: %s, runs: %d", workflowName, len(runs))

	if len(runs) == 0 {
		return WorkflowHealth{
			WorkflowName:  workflowName,
			DisplayRate:   "N/A",
			Trend:         "→",
			DisplayDur:    "N/A",
			DisplayTokens: "-",
		}
	}

	health := accumulateWorkflowHealth(runs)
	health.WorkflowName = workflowName
	health.SuccessRate = safePercent(health.SuccessCount, health.TotalRuns)
	health.Trend = calculateTrend(runs).String()
	health.DisplayRate = fmt.Sprintf("%.0f%%  (%d/%d)", health.SuccessRate, health.SuccessCount, health.TotalRuns)
	health.DisplayDur = timeutil.FormatDuration(health.AvgDuration)
	health.DisplayTokens = console.FormatTokens(health.AvgTokens)
	health.BelowThresh = health.SuccessRate < threshold
	healthMetricsLog.Printf("Health calculated: workflow=%s, successRate=%.2f%%, trend=%s", workflowName, health.SuccessRate, health.Trend)
	return health
}

func isIgnoredHealthRun(run WorkflowRun) bool {
	return isNonDispatchedConclusion(run.Conclusion)
}

func accumulateWorkflowHealth(runs []WorkflowRun) WorkflowHealth {
	health := WorkflowHealth{TotalRuns: len(runs)}
	var durationStats, tokenStats stats.StatVar

	for _, run := range runs {
		if run.Conclusion == "success" {
			health.SuccessCount++
		} else if isFailureConclusion(run.Conclusion) {
			health.FailureCount++
			if isDriverExitFailure(run) {
				health.DriverExitCount++
			} else if run.TurnsAvailable || run.Turns > 0 {
				// Only count as agent-logic when we have reliable turn data.
				// Runs without artifact logs (TurnsAvailable=false, Turns=0) are left unclassified.
				health.AgentLogicFailureCount++
			}
		}
		health.TotalTokens += run.TokenUsage
		durationStats.Add(float64(run.Duration))
		tokenStats.Add(float64(run.TokenUsage))
	}

	health.AvgDuration = time.Duration(durationStats.Mean())
	health.AvgTokens = int(tokenStats.Mean())
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

	return safePercent(successCount, len(runs))
}

// CalculateHealthSummary calculates aggregated health metrics across all workflows.
// Workflows tagged with IntentionalFailure: true are excluded from the healthy/below-threshold
// counts so they do not depress fleet-health and prod-main success-rate baselines.
func CalculateHealthSummary(workflowHealths []WorkflowHealth, period string, threshold float64) HealthSummary {
	healthMetricsLog.Printf("Calculating health summary: workflows=%d, period=%s", len(workflowHealths), period)

	healthyCount, belowThresholdCount := 0, 0
	degradedCount, subagentFailures, subagentFailureRuns, detectionFailureRuns := 0, 0, 0, 0
	statusCounts := make(map[string]int)
	observedFailures := 0

	for _, wh := range workflowHealths {
		status := wh.Status
		if status == "" {
			status = "unknown"
		}
		statusCounts[status]++
		if wh.IntentionalFailure {
			// Exclude from rollup counts — these workflows are expected to fail.
			continue
		}
		if wh.FailureCount > 0 {
			observedFailures++
		}
		subagentFailures += wh.SubagentFailureCount
		subagentFailureRuns += wh.SubagentFailureRuns
		detectionFailureRuns += wh.DetectionFailureRuns
		if wh.SubagentFailureCount > 0 || wh.DetectionFailureRuns > 0 {
			degradedCount++
		}
		if isHealthEvidenceInsufficient(wh.Status) {
			continue
		}
		if wh.SuccessRate >= threshold && wh.SubagentFailureCount == 0 && wh.DetectionFailureRuns == 0 &&
			(wh.Status == "healthy" || wh.Status == "recovery_observed" || wh.Status == "") {
			healthyCount++
		}
		if wh.BelowThresh {
			belowThresholdCount++
		}
	}

	summary := HealthSummary{
		StatusCounts:             statusCounts,
		ObservedFailureWorkflows: observedFailures,
		Period:                   period,
		TotalWorkflows:           len(workflowHealths),
		HealthyWorkflows:         healthyCount,
		Workflows:                workflowHealths,
		BelowThreshold:           belowThresholdCount,
		DegradedWorkflows:        degradedCount,
		SubagentFailureCount:     subagentFailures,
		SubagentFailureRuns:      subagentFailureRuns,
		DetectionFailureRuns:     detectionFailureRuns,
	}
	for _, wh := range workflowHealths {
		if isHealthEvidenceInsufficient(wh.Status) {
			summary.InsufficientWorkflows++
		}
	}

	healthMetricsLog.Printf("Health summary: total=%d, healthy=%d, below_threshold=%d", len(workflowHealths), healthyCount, belowThresholdCount)

	return summary
}
