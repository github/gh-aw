//go:build !integration

package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLogsNonExistentWorkflow tests that querying a non-existent workflow returns empty results
// instead of crashing with an error
func TestLogsNonExistentWorkflow(t *testing.T) {
	// Create a temporary directory for logs output
	tmpDir := testutil.TempDir(t, "logs-nonexistent-*")
	outputDir := filepath.Join(tmpDir, "output")

	// Set up environment to prevent actual API calls
	// Note: This test focuses on the workflow name validation logic
	// The actual API call behavior would be tested in integration tests

	// Test calling DownloadWorkflowLogs with a clearly non-existent workflow name
	// that doesn't match any existing workflow file
	err := DownloadWorkflowLogs(
		context.Background(),
		"nonexistent-workflow-xyz-12345", // Non-existent workflow
		5,                                // count
		"",                               // startDate
		"",                               // endDate
		outputDir,                        // outputDir
		"",                               // engine
		"",                               // ref
		0,                                // beforeRunID
		0,                                // afterRunID
		"",                               // repoOverride
		false,                            // verbose
		false,                            // toolGraph
		false,                            // noStaged
		false,                            // firewallOnly
		false,                            // noFirewall
		false,                            // parse
		true,                             // jsonOutput - enable JSON mode
		0,                                // timeout
		"summary.json",                   // summaryFile
		"",                               // safeOutputType
	)

	// The command may fail due to GitHub CLI auth issues in test environment
	// In a real environment with proper auth, it should succeed with empty results
	if err != nil {
		// If error is due to GitHub auth, skip the test
		if assert.Contains(t, err.Error(), "GitHub CLI authentication required") {
			t.Skip("Skipping test - requires GitHub CLI authentication")
		}
		// If error is NOT about authentication, fail the test
		t.Fatalf("Expected command to succeed with empty results, got error: %v", err)
	}

	// After the fix, the command should succeed and create a summary file with zero runs
	summaryPath := filepath.Join(outputDir, "summary.json")
	require.FileExists(t, summaryPath, "Summary file should be created even with no runs")

	// Read and verify the summary
	summaryData, err := os.ReadFile(summaryPath)
	require.NoError(t, err, "Should be able to read summary file")

	var logsData LogsData
	err = json.Unmarshal(summaryData, &logsData)
	require.NoError(t, err, "Summary should contain valid JSON")

	// Verify empty results
	assert.Equal(t, 0, logsData.Summary.TotalRuns, "Should report 0 total runs")
	assert.Equal(t, 0, len(logsData.Runs), "Should have empty runs array")
	assert.Equal(t, 0, logsData.Summary.TotalTokens, "Should report 0 total tokens")
	assert.Equal(t, float64(0), logsData.Summary.TotalCost, "Should report 0.0 total cost")
}

// TestLogsNonExistentWorkflowJSON tests JSON output for non-existent workflow
func TestLogsNonExistentWorkflowJSON(t *testing.T) {
	tmpDir := testutil.TempDir(t, "logs-nonexistent-json-*")
	outputDir := filepath.Join(tmpDir, "output")

	// This test verifies that the JSON output format is correct
	// when there are zero runs (whether due to non-existent workflow or filters)

	// Build logs data with no runs (simulating non-existent workflow result)
	logsData := buildLogsData([]ProcessedRun{}, outputDir, nil)

	// Verify the structure matches expected format
	assert.Equal(t, 0, logsData.Summary.TotalRuns, "TotalRuns should be 0")
	assert.NotNil(t, logsData.Runs, "Runs array should not be nil")
	assert.Equal(t, 0, len(logsData.Runs), "Runs array should be empty")

	// Marshal to JSON to verify serialization
	jsonData, err := json.Marshal(logsData)
	require.NoError(t, err, "Should marshal to JSON without error")

	// Verify the JSON structure
	var parsed map[string]any
	err = json.Unmarshal(jsonData, &parsed)
	require.NoError(t, err, "Should parse JSON without error")

	// Verify summary exists and has expected fields
	summary, ok := parsed["summary"].(map[string]any)
	require.True(t, ok, "Summary should be present in JSON")

	totalRuns, ok := summary["total_runs"].(float64)
	require.True(t, ok, "total_runs should be present in summary")
	assert.Equal(t, float64(0), totalRuns, "total_runs should be 0")

	// Verify runs array exists and is empty
	runs, ok := parsed["runs"].([]any)
	require.True(t, ok, "Runs should be present as array in JSON")
	assert.Equal(t, 0, len(runs), "Runs array should be empty")
}

// TestBuildLogsDataNonExistentWorkflow tests buildLogsData with empty runs
// This simulates the result of querying a non-existent workflow
func TestBuildLogsDataNonExistentWorkflow(t *testing.T) {
	tmpDir := testutil.TempDir(t, "logs-build-nonexistent-*")

	// Build logs data with no runs (as would happen for non-existent workflow)
	logsData := buildLogsData([]ProcessedRun{}, tmpDir, nil)

	// Verify all summary fields have zero values
	assert.Equal(t, 0, logsData.Summary.TotalRuns, "TotalRuns should be 0")
	assert.Equal(t, 0, logsData.Summary.TotalTokens, "TotalTokens should be 0")
	assert.Equal(t, float64(0), logsData.Summary.TotalCost, "TotalCost should be 0.0")
	assert.Equal(t, 0, logsData.Summary.TotalTurns, "TotalTurns should be 0")
	assert.Equal(t, 0, logsData.Summary.TotalErrors, "TotalErrors should be 0")
	assert.Equal(t, 0, logsData.Summary.TotalWarnings, "TotalWarnings should be 0")
	assert.Equal(t, 0, logsData.Summary.TotalMissingTools, "TotalMissingTools should be 0")

	// Verify arrays are empty but not nil
	assert.NotNil(t, logsData.Runs, "Runs should not be nil")
	assert.Equal(t, 0, len(logsData.Runs), "Runs should be empty array")
	assert.NotNil(t, logsData.ToolUsage, "ToolUsage should not be nil")
	assert.Equal(t, 0, len(logsData.ToolUsage), "ToolUsage should be empty array")
}
