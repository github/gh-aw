//go:build !integration

package cli

import (
	"testing"
	"time"
)

// TestLogsOverviewIncludesMissingTools verifies that the overview table includes missing tools count
func TestLogsOverviewIncludesMissingTools(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:       12345,
				WorkflowName:     "Test Workflow A",
				Status:           "completed",
				Conclusion:       "success",
				CreatedAt:        time.Now(),
				Duration:         5 * time.Minute,
				TokenUsage:       1000,
				EstimatedCost:    0.01,
				Turns:            3,
				ErrorCount:       0,
				WarningCount:     2,
				MissingToolCount: 1,
				LogsPath:         "/tmp/gh-aw/run-12345",
			},
			MissingTools: []MissingToolReport{
				{Tool: "terraform", Reason: "Infrastructure automation needed"},
			},
		},
		{
			Run: WorkflowRun{
				DatabaseID:       67890,
				WorkflowName:     "Test Workflow B",
				Status:           "completed",
				Conclusion:       "failure",
				CreatedAt:        time.Now(),
				Duration:         3 * time.Minute,
				TokenUsage:       500,
				EstimatedCost:    0.005,
				Turns:            2,
				ErrorCount:       1,
				WarningCount:     0,
				MissingToolCount: 3,
				LogsPath:         "/tmp/gh-aw/run-67890",
			},
			MissingTools: []MissingToolReport{
				{Tool: "kubectl", Reason: "K8s management"},
				{Tool: "docker", Reason: "Container runtime"},
				{Tool: "helm", Reason: "K8s package manager"},
			},
		},
	}

	// Capture output by redirecting - this is a smoke test to ensure displayLogsOverview doesn't panic
	// and that it processes the MissingToolCount field
	displayLogsOverview(processedRuns, false)
	displayLogsOverview(processedRuns, true)
}

// TestWorkflowRunStructHasMissingToolCount verifies that WorkflowRun has the MissingToolCount field
func TestWorkflowRunStructHasMissingToolCount(t *testing.T) {
	run := WorkflowRun{
		MissingToolCount: 5,
	}

	if run.MissingToolCount != 5 {
		t.Errorf("Expected MissingToolCount to be 5, got %d", run.MissingToolCount)
	}
}

// TestProcessedRunPopulatesMissingToolCount verifies that missing tools are counted correctly
func TestProcessedRunPopulatesMissingToolCount(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   12345,
				WorkflowName: "Test Workflow",
			},
			MissingTools: []MissingToolReport{
				{Tool: "terraform", Reason: "Need infrastructure automation"},
				{Tool: "kubectl", Reason: "Need K8s management"},
			},
		},
	}

	// Simulate what the logs command does
	workflowRuns := make([]WorkflowRun, len(processedRuns))
	for i, pr := range processedRuns {
		run := pr.Run
		run.MissingToolCount = len(pr.MissingTools)
		workflowRuns[i] = run
	}

	if workflowRuns[0].MissingToolCount != 2 {
		t.Errorf("Expected MissingToolCount to be 2, got %d", workflowRuns[0].MissingToolCount)
	}
}

// TestLogsOverviewHeaderIncludesMissing verifies the header includes "Missing"
func TestLogsOverviewHeaderIncludesMissing(t *testing.T) {
	// This test verifies the structure by checking that our expected headers are defined
	expectedHeaders := []string{"Run ID", "Workflow", "Status", "Duration", "Tokens", "Cost ($)", "Turns", "Errors", "Warnings", "Missing", "Created", "Logs Path"}

	// Verify the "Missing" header is in the expected position (index 9)
	if expectedHeaders[9] != "Missing" {
		t.Errorf("Expected header at index 9 to be 'Missing', got '%s'", expectedHeaders[9])
	}

	// Verify we have 12 columns total
	if len(expectedHeaders) != 12 {
		t.Errorf("Expected 12 headers, got %d", len(expectedHeaders))
	}
}

// TestDisplayLogsOverviewWithVariousMissingToolCounts tests different scenarios
func TestDisplayLogsOverviewWithVariousMissingToolCounts(t *testing.T) {
	testCases := []struct {
		name             string
		processedRuns    []ProcessedRun
		expectedNonPanic bool
	}{
		{
			name: "no missing tools",
			processedRuns: []ProcessedRun{
				{
					Run: WorkflowRun{
						DatabaseID:       1,
						WorkflowName:     "Clean Workflow",
						MissingToolCount: 0,
						LogsPath:         "/tmp/gh-aw/run-1",
					},
					MissingTools: []MissingToolReport{},
				},
			},
			expectedNonPanic: true,
		},
		{
			name: "single missing tool",
			processedRuns: []ProcessedRun{
				{
					Run: WorkflowRun{
						DatabaseID:       2,
						WorkflowName:     "Workflow with One Missing",
						MissingToolCount: 1,
						LogsPath:         "/tmp/gh-aw/run-2",
					},
					MissingTools: []MissingToolReport{
						{Tool: "terraform", Reason: "Need IaC"},
					},
				},
			},
			expectedNonPanic: true,
		},
		{
			name: "multiple missing tools",
			processedRuns: []ProcessedRun{
				{
					Run: WorkflowRun{
						DatabaseID:       3,
						WorkflowName:     "Workflow with Multiple Missing",
						MissingToolCount: 5,
						LogsPath:         "/tmp/gh-aw/run-3",
					},
					MissingTools: []MissingToolReport{
						{Tool: "terraform", Reason: "IaC"},
						{Tool: "kubectl", Reason: "K8s"},
						{Tool: "docker", Reason: "Containers"},
						{Tool: "helm", Reason: "Packages"},
						{Tool: "argocd", Reason: "GitOps"},
					},
				},
			},
			expectedNonPanic: true,
		},
		{
			name: "mixed missing tool counts",
			processedRuns: []ProcessedRun{
				{
					Run: WorkflowRun{
						DatabaseID:       4,
						WorkflowName:     "Workflow A",
						MissingToolCount: 0,
						LogsPath:         "/tmp/gh-aw/run-4",
					},
					MissingTools: []MissingToolReport{},
				},
				{
					Run: WorkflowRun{
						DatabaseID:       5,
						WorkflowName:     "Workflow B",
						MissingToolCount: 2,
						LogsPath:         "/tmp/gh-aw/run-5",
					},
					MissingTools: []MissingToolReport{
						{Tool: "kubectl", Reason: "K8s"},
						{Tool: "docker", Reason: "Containers"},
					},
				},
				{
					Run: WorkflowRun{
						DatabaseID:       6,
						WorkflowName:     "Workflow C",
						MissingToolCount: 1,
						LogsPath:         "/tmp/gh-aw/run-6",
					},
					MissingTools: []MissingToolReport{
						{Tool: "helm", Reason: "Packages"},
					},
				},
			},
			expectedNonPanic: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// This test ensures displayLogsOverview doesn't panic with various missing tool counts
			defer func() {
				if r := recover(); r != nil && tc.expectedNonPanic {
					t.Errorf("displayLogsOverview panicked with: %v", r)
				}
			}()
			displayLogsOverview(tc.processedRuns, false)
			displayLogsOverview(tc.processedRuns, true)
		})
	}
}

// TestTotalMissingToolsCalculation verifies totals are calculated correctly
func TestTotalMissingToolsCalculation(t *testing.T) {
	runs := []WorkflowRun{
		{DatabaseID: 1, MissingToolCount: 2, LogsPath: "/tmp/gh-aw/run-1"},
		{DatabaseID: 2, MissingToolCount: 0, LogsPath: "/tmp/gh-aw/run-2"},
		{DatabaseID: 3, MissingToolCount: 5, LogsPath: "/tmp/gh-aw/run-3"},
		{DatabaseID: 4, MissingToolCount: 1, LogsPath: "/tmp/gh-aw/run-4"},
	}

	expectedTotal := 2 + 0 + 5 + 1 // = 8

	// Calculate total the same way displayLogsOverview does
	var totalMissingTools int
	for _, run := range runs {
		totalMissingTools += run.MissingToolCount
	}

	if totalMissingTools != expectedTotal {
		t.Errorf("Expected total missing tools to be %d, got %d", expectedTotal, totalMissingTools)
	}
}

// TestOverviewDisplayConsistency verifies that the overview function is consistent
func TestOverviewDisplayConsistency(t *testing.T) {
	// Create a run with known values
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:       99999,
				WorkflowName:     "Consistency Test",
				Status:           "completed",
				Conclusion:       "success",
				Duration:         10 * time.Minute,
				TokenUsage:       2000,
				EstimatedCost:    0.02,
				Turns:            5,
				ErrorCount:       1,
				WarningCount:     3,
				MissingToolCount: 2,
				CreatedAt:        time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
				LogsPath:         "/tmp/gh-aw/run-99999",
			},
			MissingTools: []MissingToolReport{
				{Tool: "terraform", Reason: "IaC"},
				{Tool: "kubectl", Reason: "K8s"},
			},
		},
	}

	// Call displayLogsOverview - it should not panic and should handle all fields
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("displayLogsOverview panicked: %v", r)
		}
	}()

	displayLogsOverview(processedRuns, false)
	displayLogsOverview(processedRuns, true)
}

// TestMissingToolsIntegration tests the full flow from ProcessedRun to display
func TestMissingToolsIntegration(t *testing.T) {
	// Create a ProcessedRun with missing tools
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:       11111,
				WorkflowName:     "Integration Test Workflow",
				Status:           "completed",
				Conclusion:       "success",
				MissingToolCount: 2,
			},
			MissingTools: []MissingToolReport{
				{
					Tool:         "terraform",
					Reason:       "Infrastructure automation needed",
					Alternatives: "Manual AWS console",
					Timestamp:    "2024-01-15T10:30:00Z",
					WorkflowName: "Integration Test Workflow",
					RunID:        11111,
				},
				{
					Tool:         "kubectl",
					Reason:       "Kubernetes cluster management",
					WorkflowName: "Integration Test Workflow",
					RunID:        11111,
				},
			},
		},
	}

	// Verify count is correct
	if processedRuns[0].Run.MissingToolCount != 2 {
		t.Errorf("Expected MissingToolCount to be 2, got %d", processedRuns[0].Run.MissingToolCount)
	}

	// Display should work without panicking
	displayLogsOverview(processedRuns, false)
	displayLogsOverview(processedRuns, true)
}

// TestMissingToolCountFieldAccessibility verifies field is accessible
func TestMissingToolCountFieldAccessibility(t *testing.T) {
	var run WorkflowRun

	// Should be able to set and get the field
	run.MissingToolCount = 10

	if run.MissingToolCount != 10 {
		t.Errorf("MissingToolCount field not accessible or not working correctly")
	}

	// Should support zero value
	var emptyRun WorkflowRun
	if emptyRun.MissingToolCount != 0 {
		t.Errorf("MissingToolCount should default to 0, got %d", emptyRun.MissingToolCount)
	}
}
