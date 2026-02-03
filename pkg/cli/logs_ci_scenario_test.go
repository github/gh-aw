//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
)

// TestLogsJSONOutputWithNoRuns verifies that the logs command outputs valid JSON
// even when no workflow runs match the criteria. This test simulates the CI test
// scenario where `gh aw logs -c 2 --engine copilot --json` might find no matching runs.
func TestLogsJSONOutputWithNoRuns(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-logs-*")

	// Create a context for the test
	ctx := context.Background()

	// Capture stdout to test JSON output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Call DownloadWorkflowLogs with parameters that will result in no matching runs
	// We use a non-existent workflow name to ensure no results
	err := DownloadWorkflowLogs(
		ctx,
		"nonexistent-workflow-12345", // Workflow that doesn't exist
		2,                            // count
		"",                           // startDate
		"",                           // endDate
		tmpDir,                       // outputDir
		"copilot",                    // engine
		"",                           // ref
		0,                            // beforeRunID
		0,                            // afterRunID
		"",                           // repoOverride
		false,                        // verbose
		false,                        // toolGraph
		false,                        // noStaged
		false,                        // firewallOnly
		false,                        // noFirewall
		false,                        // parse
		true,                         // jsonOutput - THIS IS KEY
		10,                           // timeout
		"summary.json",               // summaryFile
		"",                           // safeOutputType
	)

	// Restore stdout and read output
	w.Close()
	os.Stdout = oldStdout

	// The function should NOT return an error (it returns nil even with no runs)
	if err != nil {
		// Skip this test if GitHub API is not accessible (e.g., no GH_TOKEN)
		if err.Error() == "failed to list workflow runs: failed to query GitHub GraphQL API: failed to authenticate: no auth token found" ||
			err.Error() == "GitHub CLI authentication required. Run 'gh auth login' first" {
			t.Skip("Skipping test: GitHub authentication not available")
		}
		t.Fatalf("DownloadWorkflowLogs returned error: %v", err)
	}

	// Read the JSON output
	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	// If output is empty, it means the function returned before JSON output
	// This should not happen with our fix
	if len(output) == 0 {
		t.Fatalf("Expected JSON output, got empty string")
	}

	// Parse the JSON to verify it's valid
	var logsData LogsData
	if err := json.Unmarshal([]byte(output), &logsData); err != nil {
		t.Fatalf("Failed to parse JSON output: %v\nOutput: %s", err, output)
	}

	// Verify the summary structure exists
	if logsData.Summary.TotalRuns != 0 {
		t.Errorf("Expected TotalRuns to be 0, got %d", logsData.Summary.TotalRuns)
	}

	// Most importantly: verify total_tokens field exists
	// This is what the CI test checks with jq -e '.summary.total_tokens'
	var jsonMap map[string]any
	if err := json.Unmarshal([]byte(output), &jsonMap); err != nil {
		t.Fatalf("Failed to parse JSON as map: %v", err)
	}

	summary, ok := jsonMap["summary"].(map[string]any)
	if !ok {
		t.Fatalf("Expected summary to be a map, got %T", jsonMap["summary"])
	}

	// This is the exact check the CI does
	if _, exists := summary["total_tokens"]; !exists {
		t.Errorf("Expected total_tokens field to exist in summary (CI test would fail). Summary: %+v", summary)
	}

	// Verify all expected summary fields exist
	expectedFields := []string{
		"total_runs", "total_duration", "total_tokens", "total_cost",
		"total_turns", "total_errors", "total_warnings", "total_missing_tools",
	}
	for _, field := range expectedFields {
		if _, exists := summary[field]; !exists {
			t.Errorf("Expected field '%s' to exist in summary", field)
		}
	}
}

// TestLogsJSONRunDataFields verifies that run data includes key fields like
// agent (engine_id), workflow_path, and workflow_name that should be resolved
func TestLogsJSONRunDataFields(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-logs-run-fields-*")

	// Create a mock processed run with all fields populated
	mockRun := ProcessedRun{
		Run: WorkflowRun{
			DatabaseID:       12345,
			Number:           1,
			WorkflowName:     "Test Workflow",
			WorkflowPath:     ".github/workflows/test.yml",
			Status:           "completed",
			Conclusion:       "success",
			TokenUsage:       1000,
			EstimatedCost:    0.01,
			Turns:            5,
			ErrorCount:       0,
			WarningCount:     0,
			MissingToolCount: 0,
			LogsPath:         tmpDir,
		},
	}

	// Create a mock aw_info.json with engine_id
	awInfoPath := filepath.Join(tmpDir, "aw_info.json")
	awInfo := map[string]any{
		"engine_id":     "copilot-claude-3.5-sonnet",
		"engine_name":   "copilot",
		"workflow_name": "Test Workflow",
	}
	awInfoBytes, _ := json.Marshal(awInfo)
	if err := os.WriteFile(awInfoPath, awInfoBytes, 0644); err != nil {
		t.Fatalf("Failed to write mock aw_info.json: %v", err)
	}

	// Build logs data
	logsData := buildLogsData([]ProcessedRun{mockRun}, tmpDir, nil)

	// Marshal to JSON
	jsonBytes, err := json.MarshalIndent(logsData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	// Parse as map to check field existence
	var parsed map[string]any
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// Verify runs array exists
	runs, ok := parsed["runs"].([]any)
	if !ok {
		t.Fatalf("Expected 'runs' to be an array, got %T", parsed["runs"])
	}
	if len(runs) != 1 {
		t.Fatalf("Expected 1 run, got %d", len(runs))
	}

	// Check first run has required fields
	run := runs[0].(map[string]any)

	// This is what the updated CI test checks
	requiredFields := []string{
		"database_id",
		"workflow_name",
		"workflow_path", // New field - workflow ID
		"agent",         // Engine ID
		"status",
	}

	for _, field := range requiredFields {
		if _, exists := run[field]; !exists {
			t.Errorf("Expected field '%s' to exist in run data (CI test would fail). Run: %+v", field, run)
		}
	}

	// Verify specific values
	if agent, ok := run["agent"].(string); ok {
		if agent != "copilot-claude-3.5-sonnet" {
			t.Errorf("Expected agent to be 'copilot-claude-3.5-sonnet', got '%s'", agent)
		}
	} else {
		t.Error("Agent field should be a string")
	}

	if workflowPath, ok := run["workflow_path"].(string); ok {
		if workflowPath != ".github/workflows/test.yml" {
			t.Errorf("Expected workflow_path to be '.github/workflows/test.yml', got '%s'", workflowPath)
		}
	} else {
		t.Error("Workflow path field should be a string")
	}

	if workflowName, ok := run["workflow_name"].(string); ok {
		if workflowName != "Test Workflow" {
			t.Errorf("Expected workflow_name to be 'Test Workflow', got '%s'", workflowName)
		}
	} else {
		t.Error("Workflow name field should be a string")
	}
}

// TestLogsJSONOutputStructure verifies the complete JSON structure when there are no runs
func TestLogsJSONOutputStructure(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-logs-structure-*")

	// Build logs data with empty runs
	logsData := buildLogsData([]ProcessedRun{}, tmpDir, nil)

	// Marshal to JSON
	jsonBytes, err := json.MarshalIndent(logsData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	// Parse back to verify structure
	var parsed map[string]any
	if err := json.Unmarshal(jsonBytes, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	// Verify top-level structure
	if _, exists := parsed["summary"]; !exists {
		t.Error("Missing 'summary' field in JSON output")
	}
	if _, exists := parsed["runs"]; !exists {
		t.Error("Missing 'runs' field in JSON output")
	}

	// Verify summary has all required fields
	summary := parsed["summary"].(map[string]any)
	requiredFields := []string{
		"total_runs", "total_duration", "total_tokens", "total_cost",
		"total_turns", "total_errors", "total_warnings", "total_missing_tools",
	}

	for _, field := range requiredFields {
		if _, exists := summary[field]; !exists {
			t.Errorf("Missing required field '%s' in summary", field)
		}
	}

	// Verify runs is an empty array (not null)
	runs, ok := parsed["runs"].([]any)
	if !ok {
		t.Errorf("Expected 'runs' to be an array, got %T", parsed["runs"])
	}
	if len(runs) != 0 {
		t.Errorf("Expected empty runs array, got %d runs", len(runs))
	}
}

// TestSummaryFileWrittenWithNoRuns verifies that the summary.json file is created
// even when there are no runs (important for campaign orchestrators)
func TestSummaryFileWrittenWithNoRuns(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-summary-*")

	// Build logs data with empty runs
	logsData := buildLogsData([]ProcessedRun{}, tmpDir, nil)

	// Write summary file
	summaryPath := filepath.Join(tmpDir, "summary.json")
	err := writeSummaryFile(summaryPath, logsData, false)
	if err != nil {
		t.Fatalf("Failed to write summary file: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(summaryPath); os.IsNotExist(err) {
		t.Fatal("Expected summary.json to be created, but it doesn't exist")
	}

	// Read and verify content
	content, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("Failed to read summary file: %v", err)
	}

	var parsed LogsData
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("Failed to parse summary JSON: %v", err)
	}

	// Verify the structure is valid
	if parsed.Summary.TotalRuns != 0 {
		t.Errorf("Expected TotalRuns to be 0, got %d", parsed.Summary.TotalRuns)
	}
	if parsed.Summary.TotalTokens != 0 {
		t.Errorf("Expected TotalTokens to be 0, got %d", parsed.Summary.TotalTokens)
	}
}
