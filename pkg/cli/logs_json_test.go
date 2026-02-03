//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestBuildLogsData tests the structured data creation for logs
func TestBuildLogsData(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	// Create sample processed runs
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:       12345,
				Number:           1,
				WorkflowName:     "Test Workflow",
				Status:           "completed",
				Conclusion:       "success",
				Duration:         5 * time.Minute,
				TokenUsage:       1000,
				EstimatedCost:    0.05,
				Turns:            3,
				ErrorCount:       0,
				WarningCount:     1,
				MissingToolCount: 0,
				CreatedAt:        time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				URL:              "https://github.com/test/repo/actions/runs/12345",
				LogsPath:         filepath.Join(tmpDir, "run-12345"),
				Event:            "push",
				HeadBranch:       "main",
			},
			MissingTools: []MissingToolReport{},
			MCPFailures:  []MCPFailureReport{},
		},
		{
			Run: WorkflowRun{
				DatabaseID:       12346,
				Number:           2,
				WorkflowName:     "Test Workflow",
				Status:           "completed",
				Conclusion:       "failure",
				Duration:         3 * time.Minute,
				TokenUsage:       500,
				EstimatedCost:    0.025,
				Turns:            2,
				ErrorCount:       1,
				WarningCount:     0,
				MissingToolCount: 1,
				CreatedAt:        time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
				URL:              "https://github.com/test/repo/actions/runs/12346",
				LogsPath:         filepath.Join(tmpDir, "run-12346"),
				Event:            "pull_request",
				HeadBranch:       "feature",
			},
			MissingTools: []MissingToolReport{
				{
					Tool:         "github_search",
					Reason:       "Not allowed",
					WorkflowName: "Test Workflow",
					RunID:        12346,
				},
			},
			MCPFailures: []MCPFailureReport{},
		},
	}

	// Build logs data
	logsData := buildLogsData(processedRuns, tmpDir, nil)

	// Verify summary
	if logsData.Summary.TotalRuns != 2 {
		t.Errorf("Expected TotalRuns to be 2, got %d", logsData.Summary.TotalRuns)
	}
	if logsData.Summary.TotalTokens != 1500 {
		t.Errorf("Expected TotalTokens to be 1500, got %d", logsData.Summary.TotalTokens)
	}
	// Use approximate comparison for float
	if logsData.Summary.TotalCost < 0.074 || logsData.Summary.TotalCost > 0.076 {
		t.Errorf("Expected TotalCost to be ~0.075, got %f", logsData.Summary.TotalCost)
	}
	if logsData.Summary.TotalTurns != 5 {
		t.Errorf("Expected TotalTurns to be 5, got %d", logsData.Summary.TotalTurns)
	}
	if logsData.Summary.TotalErrors != 1 {
		t.Errorf("Expected TotalErrors to be 1, got %d", logsData.Summary.TotalErrors)
	}
	if logsData.Summary.TotalWarnings != 1 {
		t.Errorf("Expected TotalWarnings to be 1, got %d", logsData.Summary.TotalWarnings)
	}
	if logsData.Summary.TotalMissingTools != 1 {
		t.Errorf("Expected TotalMissingTools to be 1, got %d", logsData.Summary.TotalMissingTools)
	}

	// Verify runs data
	if len(logsData.Runs) != 2 {
		t.Errorf("Expected 2 runs, got %d", len(logsData.Runs))
	}

	// Verify first run
	if logsData.Runs[0].DatabaseID != 12345 {
		t.Errorf("Expected DatabaseID 12345, got %d", logsData.Runs[0].DatabaseID)
	}
	// Duration format from formatDuration is "5.0m", not "5m0s"
	if logsData.Runs[0].Duration == "" {
		t.Errorf("Expected non-empty Duration, got empty string")
	}

	// Verify missing tools summary
	if len(logsData.MissingTools) != 1 {
		t.Errorf("Expected 1 missing tool, got %d", len(logsData.MissingTools))
	}
	if len(logsData.MissingTools) > 0 && logsData.MissingTools[0].Tool != "github_search" {
		t.Errorf("Expected missing tool 'github_search', got '%s'", logsData.MissingTools[0].Tool)
	}
}

// TestRenderLogsJSON tests JSON output rendering
func TestRenderLogsJSON(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	// Create sample logs data
	logsData := LogsData{
		Summary: LogsSummary{
			TotalRuns:         2,
			TotalDuration:     "8m0s",
			TotalTokens:       1500,
			TotalCost:         0.075,
			TotalTurns:        5,
			TotalErrors:       1,
			TotalWarnings:     1,
			TotalMissingTools: 1,
		},
		Runs: []RunData{
			{
				DatabaseID:    12345,
				Number:        1,
				WorkflowName:  "Test Workflow",
				Status:        "completed",
				Conclusion:    "success",
				Duration:      "5m0s",
				TokenUsage:    1000,
				EstimatedCost: 0.05,
				Turns:         3,
				ErrorCount:    0,
				WarningCount:  1,
				CreatedAt:     time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				URL:           "https://github.com/test/repo/actions/runs/12345",
				LogsPath:      filepath.Join(tmpDir, "run-12345"),
				Event:         "push",
				Branch:        "main",
			},
		},
		LogsLocation: tmpDir,
	}

	// Redirect stdout to capture JSON output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Render JSON
	err := renderLogsJSON(logsData)
	if err != nil {
		t.Fatalf("Failed to render JSON: %v", err)
	}

	// Restore stdout and read captured output
	w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// Verify it's valid JSON
	var parsedData LogsData
	if err := json.Unmarshal([]byte(output), &parsedData); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, output)
	}

	// Verify key fields
	if parsedData.Summary.TotalRuns != 2 {
		t.Errorf("Expected TotalRuns 2, got %d", parsedData.Summary.TotalRuns)
	}
	if parsedData.Summary.TotalTokens != 1500 {
		t.Errorf("Expected TotalTokens 1500, got %d", parsedData.Summary.TotalTokens)
	}
	if len(parsedData.Runs) != 1 {
		t.Errorf("Expected 1 run in JSON, got %d", len(parsedData.Runs))
	}
}

// TestBuildMissingToolsSummary tests missing tools aggregation
func TestBuildMissingToolsSummary(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "Workflow A",
				DatabaseID:   1,
			},
			MissingTools: []MissingToolReport{
				{
					Tool:         "github_search",
					Reason:       "Not allowed",
					WorkflowName: "Workflow A",
					RunID:        1,
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "Workflow B",
				DatabaseID:   2,
			},
			MissingTools: []MissingToolReport{
				{
					Tool:         "github_search",
					Reason:       "Permission denied",
					WorkflowName: "Workflow B",
					RunID:        2,
				},
				{
					Tool:         "web_fetch",
					Reason:       "Not configured",
					WorkflowName: "Workflow B",
					RunID:        2,
				},
			},
		},
	}

	summary := buildMissingToolsSummary(processedRuns)

	// Should have 2 unique tools
	if len(summary) != 2 {
		t.Errorf("Expected 2 unique tools, got %d", len(summary))
	}

	// github_search should have count 2 and be first (sorted by count desc)
	if summary[0].Tool != "github_search" {
		t.Errorf("Expected first tool to be 'github_search', got '%s'", summary[0].Tool)
	}
	if summary[0].Count != 2 {
		t.Errorf("Expected github_search count 2, got %d", summary[0].Count)
	}
	if len(summary[0].Workflows) != 2 {
		t.Errorf("Expected github_search in 2 workflows, got %d", len(summary[0].Workflows))
	}

	// web_fetch should have count 1
	if summary[1].Tool != "web_fetch" {
		t.Errorf("Expected second tool to be 'web_fetch', got '%s'", summary[1].Tool)
	}
	if summary[1].Count != 1 {
		t.Errorf("Expected web_fetch count 1, got %d", summary[1].Count)
	}
}

// TestBuildLogsDataWithContinuation tests continuation field in logs data
func TestBuildLogsDataWithContinuation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	// Create sample processed runs
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   12345,
				Number:       1,
				WorkflowName: "Test Workflow",
				Status:       "completed",
				Conclusion:   "success",
				CreatedAt:    time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				URL:          "https://github.com/test/repo/actions/runs/12345",
				LogsPath:     filepath.Join(tmpDir, "run-12345"),
			},
		},
		{
			Run: WorkflowRun{
				DatabaseID:   12344,
				Number:       2,
				WorkflowName: "Test Workflow",
				Status:       "completed",
				Conclusion:   "success",
				CreatedAt:    time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC),
				URL:          "https://github.com/test/repo/actions/runs/12344",
				LogsPath:     filepath.Join(tmpDir, "run-12344"),
			},
		},
	}

	// Create continuation data (simulating timeout scenario)
	continuation := &ContinuationData{
		Message:      "Timeout reached. Use these parameters to continue fetching more logs.",
		WorkflowName: "Test Workflow",
		Count:        100,
		StartDate:    "2024-01-01",
		EndDate:      "2024-12-31",
		Engine:       "copilot",
		Branch:       "main",
		AfterRunID:   0,
		BeforeRunID:  12344, // Continue from the oldest run
		Timeout:      50,
	}

	// Build logs data with continuation
	logsData := buildLogsData(processedRuns, tmpDir, continuation)

	// Verify continuation field is present
	if logsData.Continuation == nil {
		t.Fatal("Expected continuation field to be present, got nil")
	}

	// Verify continuation data
	if logsData.Continuation.Message != "Timeout reached. Use these parameters to continue fetching more logs." {
		t.Errorf("Expected continuation message, got '%s'", logsData.Continuation.Message)
	}
	if logsData.Continuation.WorkflowName != "Test Workflow" {
		t.Errorf("Expected WorkflowName 'Test Workflow', got '%s'", logsData.Continuation.WorkflowName)
	}
	if logsData.Continuation.BeforeRunID != 12344 {
		t.Errorf("Expected BeforeRunID 12344, got %d", logsData.Continuation.BeforeRunID)
	}
	if logsData.Continuation.Count != 100 {
		t.Errorf("Expected Count 100, got %d", logsData.Continuation.Count)
	}
	if logsData.Continuation.Engine != "copilot" {
		t.Errorf("Expected Engine 'copilot', got '%s'", logsData.Continuation.Engine)
	}

	// Test JSON serialization of continuation
	jsonOutput, err := json.MarshalIndent(logsData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal logs data to JSON: %v", err)
	}

	// Verify continuation is in JSON
	var parsedData LogsData
	if err := json.Unmarshal(jsonOutput, &parsedData); err != nil {
		t.Fatalf("Failed to unmarshal logs data from JSON: %v", err)
	}

	if parsedData.Continuation == nil {
		t.Fatal("Expected continuation field in unmarshaled JSON, got nil")
	}
	if parsedData.Continuation.BeforeRunID != 12344 {
		t.Errorf("Expected BeforeRunID 12344 in unmarshaled JSON, got %d", parsedData.Continuation.BeforeRunID)
	}
}

// TestBuildLogsDataWithoutContinuation tests that continuation is omitted when nil
func TestBuildLogsDataWithoutContinuation(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")

	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				DatabaseID:   12345,
				WorkflowName: "Test Workflow",
				LogsPath:     filepath.Join(tmpDir, "run-12345"),
			},
		},
	}

	// Build logs data without continuation
	logsData := buildLogsData(processedRuns, tmpDir, nil)

	// Verify continuation field is nil
	if logsData.Continuation != nil {
		t.Errorf("Expected continuation field to be nil, got %+v", logsData.Continuation)
	}

	// Test JSON serialization
	jsonOutput, err := json.Marshal(logsData)
	if err != nil {
		t.Fatalf("Failed to marshal logs data to JSON: %v", err)
	}

	// Verify continuation is omitted from JSON (due to omitempty tag)
	var parsedMap map[string]any
	if err := json.Unmarshal(jsonOutput, &parsedMap); err != nil {
		t.Fatalf("Failed to unmarshal logs data to map: %v", err)
	}

	if _, exists := parsedMap["continuation"]; exists {
		t.Error("Expected continuation field to be omitted from JSON when nil")
	}
}

// TestBuildMCPFailuresSummary tests MCP failures aggregation
func TestBuildMCPFailuresSummary(t *testing.T) {
	processedRuns := []ProcessedRun{
		{
			Run: WorkflowRun{
				WorkflowName: "Workflow A",
				DatabaseID:   1,
			},
			MCPFailures: []MCPFailureReport{
				{
					ServerName:   "playwright",
					Status:       "failed",
					WorkflowName: "Workflow A",
					RunID:        1,
				},
			},
		},
		{
			Run: WorkflowRun{
				WorkflowName: "Workflow B",
				DatabaseID:   2,
			},
			MCPFailures: []MCPFailureReport{
				{
					ServerName:   "playwright",
					Status:       "failed",
					WorkflowName: "Workflow B",
					RunID:        2,
				},
			},
		},
	}

	summary := buildMCPFailuresSummary(processedRuns)

	// Should have 1 unique server
	if len(summary) != 1 {
		t.Errorf("Expected 1 unique server, got %d", len(summary))
	}

	// playwright should have count 2
	if summary[0].ServerName != "playwright" {
		t.Errorf("Expected server 'playwright', got '%s'", summary[0].ServerName)
	}
	if summary[0].Count != 2 {
		t.Errorf("Expected playwright count 2, got %d", summary[0].Count)
	}
	if len(summary[0].Workflows) != 2 {
		t.Errorf("Expected playwright in 2 workflows, got %d", len(summary[0].Workflows))
	}
}
