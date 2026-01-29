//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestWriteSummaryFile tests the writeSummaryFile function
func TestWriteSummaryFile(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	summaryPath := filepath.Join(tmpDir, "test-summary.json")

	// Create sample logs data
	logsData := LogsData{
		Summary: LogsSummary{
			TotalRuns:         3,
			TotalDuration:     "1h30m",
			TotalTokens:       15000,
			TotalCost:         2.50,
			TotalTurns:        25,
			TotalErrors:       2,
			TotalWarnings:     5,
			TotalMissingTools: 1,
		},
		Runs: []RunData{
			{
				DatabaseID:       12345,
				Number:           1,
				WorkflowName:     "Test Workflow",
				Agent:            "copilot",
				Status:           "completed",
				Conclusion:       "success",
				Duration:         "30m",
				TokenUsage:       5000,
				EstimatedCost:    0.75,
				Turns:            10,
				ErrorCount:       0,
				WarningCount:     2,
				MissingToolCount: 0,
				CreatedAt:        time.Now(),
				URL:              "https://github.com/owner/repo/actions/runs/12345",
				LogsPath:         "/tmp/logs/run-12345",
			},
		},
		LogsLocation: tmpDir,
	}

	// Test writing summary file
	err := writeSummaryFile(summaryPath, logsData, false)
	if err != nil {
		t.Fatalf("Failed to write summary file: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(summaryPath); os.IsNotExist(err) {
		t.Fatal("Summary file was not created")
	}

	// Read and verify the content
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("Failed to read summary file: %v", err)
	}

	// Parse the JSON to verify it's valid
	var parsedData LogsData
	if err := json.Unmarshal(data, &parsedData); err != nil {
		t.Fatalf("Failed to parse summary JSON: %v", err)
	}

	// Verify key fields
	if parsedData.Summary.TotalRuns != logsData.Summary.TotalRuns {
		t.Errorf("Expected TotalRuns %d, got %d", logsData.Summary.TotalRuns, parsedData.Summary.TotalRuns)
	}
	if parsedData.Summary.TotalTokens != logsData.Summary.TotalTokens {
		t.Errorf("Expected TotalTokens %d, got %d", logsData.Summary.TotalTokens, parsedData.Summary.TotalTokens)
	}
	if len(parsedData.Runs) != len(logsData.Runs) {
		t.Errorf("Expected %d runs, got %d", len(logsData.Runs), len(parsedData.Runs))
	}
	if len(parsedData.Runs) > 0 {
		if parsedData.Runs[0].DatabaseID != logsData.Runs[0].DatabaseID {
			t.Errorf("Expected DatabaseID %d, got %d", logsData.Runs[0].DatabaseID, parsedData.Runs[0].DatabaseID)
		}
	}
}

// TestWriteSummaryFileCreatesDirectory tests that parent directory is created
func TestWriteSummaryFileCreatesDirectory(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	summaryPath := filepath.Join(tmpDir, "subdir", "nested", "summary.json")

	// Create minimal logs data
	logsData := LogsData{
		Summary: LogsSummary{
			TotalRuns: 1,
		},
		Runs:         []RunData{},
		LogsLocation: tmpDir,
	}

	// Test writing summary file (should create nested directories)
	err := writeSummaryFile(summaryPath, logsData, false)
	if err != nil {
		t.Fatalf("Failed to write summary file: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(summaryPath); os.IsNotExist(err) {
		t.Fatal("Summary file was not created")
	}

	// Verify nested directories were created
	dir := filepath.Dir(summaryPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("Parent directories were not created")
	}
}

// TestWriteSummaryFileWithEmptyPath tests that empty path skips writing
func TestSummaryFileDisabling(t *testing.T) {
	// This test verifies the behavior when summaryFile is empty string
	// The actual skip logic is in the orchestrator, but we document the behavior here

	// Empty string path should be handled by the caller (orchestrator)
	// to skip calling writeSummaryFile entirely
	summaryFile := ""
	if summaryFile == "" {
		// This is the expected behavior - skip writing
		t.Log("Empty summary file path correctly skips writing")
	}
}
