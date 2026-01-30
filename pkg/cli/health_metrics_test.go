//go:build !integration

package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCalculateWorkflowHealth(t *testing.T) {
	tests := []struct {
		name          string
		workflowName  string
		runs          []WorkflowRun
		threshold     float64
		expectedRate  float64
		expectedTrend string
	}{
		{
			name:         "all successful runs",
			workflowName: "test-workflow",
			runs: []WorkflowRun{
				{Conclusion: "success", Duration: 2 * time.Minute},
				{Conclusion: "success", Duration: 3 * time.Minute},
				{Conclusion: "success", Duration: 2 * time.Minute},
			},
			threshold:     80.0,
			expectedRate:  100.0,
			expectedTrend: "→",
		},
		{
			name:         "mixed success and failure",
			workflowName: "test-workflow",
			runs: []WorkflowRun{
				{Conclusion: "success", Duration: 2 * time.Minute},
				{Conclusion: "failure", Duration: 1 * time.Minute},
				{Conclusion: "success", Duration: 3 * time.Minute},
				{Conclusion: "success", Duration: 2 * time.Minute},
			},
			threshold:    80.0,
			expectedRate: 75.0,
			// Don't check trend for small dataset
		},
		{
			name:         "all failed runs",
			workflowName: "test-workflow",
			runs: []WorkflowRun{
				{Conclusion: "failure", Duration: 1 * time.Minute},
				{Conclusion: "failure", Duration: 2 * time.Minute},
			},
			threshold:     80.0,
			expectedRate:  0.0,
			expectedTrend: "→",
		},
		{
			name:         "empty runs",
			workflowName: "test-workflow",
			runs:         []WorkflowRun{},
			threshold:    80.0,
			expectedRate: 0.0,
			// Empty runs should not be checked for below threshold
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			health := CalculateWorkflowHealth(tt.workflowName, tt.runs, tt.threshold)

			assert.Equal(t, tt.workflowName, health.WorkflowName, "Workflow name should match")

			// Use InDelta for all float comparisons to satisfy testifylint
			assert.InDelta(t, tt.expectedRate, health.SuccessRate, 0.01, "Success rate should match")

			if tt.expectedTrend != "" {
				assert.Equal(t, tt.expectedTrend, health.Trend, "Trend should match")
			}

			if len(tt.runs) > 0 {
				assert.Equal(t, len(tt.runs), health.TotalRuns, "Total runs should match")
			}

			// Check below threshold flag
			if len(tt.runs) > 0 && tt.expectedRate < tt.threshold {
				assert.True(t, health.BelowThresh, "Should be marked as below threshold")
			} else if len(tt.runs) > 0 {
				assert.False(t, health.BelowThresh, "Should not be marked as below threshold")
			}
		})
	}
}

func TestCalculateTrend(t *testing.T) {
	tests := []struct {
		name     string
		runs     []WorkflowRun
		expected TrendDirection
	}{
		{
			name: "improving trend",
			runs: []WorkflowRun{
				{Conclusion: "success"},
				{Conclusion: "success"},
				{Conclusion: "success"},
				{Conclusion: "success"},
				{Conclusion: "failure"},
				{Conclusion: "failure"},
				{Conclusion: "failure"},
				{Conclusion: "success"},
			},
			expected: TrendImproving,
		},
		{
			name: "degrading trend",
			runs: []WorkflowRun{
				{Conclusion: "failure"},
				{Conclusion: "failure"},
				{Conclusion: "failure"},
				{Conclusion: "failure"},
				{Conclusion: "success"},
				{Conclusion: "success"},
				{Conclusion: "success"},
				{Conclusion: "success"},
			},
			expected: TrendDegrading,
		},
		{
			name: "stable trend",
			runs: []WorkflowRun{
				{Conclusion: "success"},
				{Conclusion: "success"},
				{Conclusion: "failure"},
				{Conclusion: "failure"},
				{Conclusion: "success"},
				{Conclusion: "success"},
				{Conclusion: "failure"},
				{Conclusion: "failure"},
			},
			expected: TrendStable,
		},
		{
			name: "not enough data",
			runs: []WorkflowRun{
				{Conclusion: "success"},
				{Conclusion: "success"},
			},
			expected: TrendStable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			trend := calculateTrend(tt.runs)
			assert.Equal(t, tt.expected, trend, "Trend direction should match expected")
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "zero duration",
			duration: 0,
			expected: "0s",
		},
		{
			name:     "seconds only",
			duration: 45 * time.Second,
			expected: "45s",
		},
		{
			name:     "minutes only",
			duration: 5 * time.Minute,
			expected: "5m",
		},
		{
			name:     "minutes and seconds",
			duration: 2*time.Minute + 30*time.Second,
			expected: "2m 30s",
		},
		{
			name:     "hours only",
			duration: 2 * time.Hour,
			expected: "2h",
		},
		{
			name:     "hours and minutes",
			duration: 1*time.Hour + 15*time.Minute,
			expected: "1h 15m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatDuration(tt.duration)
			assert.Equal(t, tt.expected, result, "Formatted duration should match")
		})
	}
}

func TestGroupRunsByWorkflow(t *testing.T) {
	runs := []WorkflowRun{
		{WorkflowName: "workflow-a", Conclusion: "success"},
		{WorkflowName: "workflow-b", Conclusion: "success"},
		{WorkflowName: "workflow-a", Conclusion: "failure"},
		{WorkflowName: "workflow-c", Conclusion: "success"},
		{WorkflowName: "workflow-b", Conclusion: "success"},
	}

	grouped := GroupRunsByWorkflow(runs)

	assert.Len(t, grouped, 3, "Should have 3 different workflows")
	assert.Len(t, grouped["workflow-a"], 2, "workflow-a should have 2 runs")
	assert.Len(t, grouped["workflow-b"], 2, "workflow-b should have 2 runs")
	assert.Len(t, grouped["workflow-c"], 1, "workflow-c should have 1 run")
}

func TestFilterWorkflowsByName(t *testing.T) {
	runs := []WorkflowRun{
		{WorkflowName: "workflow-a", Conclusion: "success"},
		{WorkflowName: "workflow-b", Conclusion: "success"},
		{WorkflowName: "workflow-a", Conclusion: "failure"},
		{WorkflowName: "workflow-c", Conclusion: "success"},
	}

	filtered := FilterWorkflowsByName(runs, "workflow-a")

	assert.Len(t, filtered, 2, "Should filter to 2 runs for workflow-a")
	for _, run := range filtered {
		assert.Equal(t, "workflow-a", run.WorkflowName, "All filtered runs should be workflow-a")
	}
}

func TestCalculateHealthSummary(t *testing.T) {
	workflowHealths := []WorkflowHealth{
		{WorkflowName: "workflow-a", SuccessRate: 90.0, BelowThresh: false},
		{WorkflowName: "workflow-b", SuccessRate: 75.0, BelowThresh: true},
		{WorkflowName: "workflow-c", SuccessRate: 85.0, BelowThresh: false},
	}

	summary := CalculateHealthSummary(workflowHealths, "Last 7 Days", 80.0)

	assert.Equal(t, "Last 7 Days", summary.Period, "Period should match")
	assert.Equal(t, 3, summary.TotalWorkflows, "Total workflows should be 3")
	assert.Equal(t, 2, summary.HealthyWorkflows, "Healthy workflows should be 2")
	assert.Equal(t, 1, summary.BelowThreshold, "Below threshold count should be 1")
	assert.Len(t, summary.Workflows, 3, "Workflows array should have 3 entries")
}

func TestTrendDirectionString(t *testing.T) {
	tests := []struct {
		name     string
		trend    TrendDirection
		expected string
	}{
		{
			name:     "improving",
			trend:    TrendImproving,
			expected: "↑",
		},
		{
			name:     "stable",
			trend:    TrendStable,
			expected: "→",
		},
		{
			name:     "degrading",
			trend:    TrendDegrading,
			expected: "↓",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.trend.String()
			assert.Equal(t, tt.expected, result, "Trend string representation should match")
		})
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		name     string
		tokens   int
		expected string
	}{
		{
			name:     "zero tokens",
			tokens:   0,
			expected: "-",
		},
		{
			name:     "small tokens",
			tokens:   500,
			expected: "500",
		},
		{
			name:     "thousands",
			tokens:   5000,
			expected: "5.0K",
		},
		{
			name:     "millions",
			tokens:   2500000,
			expected: "2.5M",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatTokens(tt.tokens)
			assert.Equal(t, tt.expected, result, "Formatted tokens should match")
		})
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		name     string
		cost     float64
		expected string
	}{
		{
			name:     "zero cost",
			cost:     0.0,
			expected: "-",
		},
		{
			name:     "very small cost",
			cost:     0.0001,
			expected: "< 0.001",
		},
		{
			name:     "small cost",
			cost:     0.123,
			expected: "0.123",
		},
		{
			name:     "large cost",
			cost:     5.678,
			expected: "5.678",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatCost(tt.cost)
			assert.Equal(t, tt.expected, result, "Formatted cost should match")
		})
	}
}
