//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

func TestSaveAndLoadRunSummary(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")
	runDir := filepath.Join(tmpDir, "run-12345")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Set a test version
	originalVersion := GetVersion()
	SetVersionInfo("1.2.3-test")
	defer SetVersionInfo(originalVersion)

	// Create a test summary
	testSummary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       12345,
		ProcessedAt: time.Now(),
		Run: WorkflowRun{
			DatabaseID:   12345,
			Number:       42,
			WorkflowName: "Test Workflow",
			Status:       "completed",
			Conclusion:   "success",
		},
		Metrics: workflow.LogMetrics{
			TokenUsage:    1000,
			EstimatedCost: 0.05,
			Turns:         5,
		},
		MissingTools: []MissingToolReport{
			{
				Tool:   "test_tool",
				Reason: "Tool not available",
			},
		},
		ArtifactsList: []string{
			"aw_info.json",
			"agent-stdio.log",
		},
	}

	// Save the summary
	if err := saveRunSummary(runDir, testSummary, false); err != nil {
		t.Fatalf("Failed to save run summary: %v", err)
	}

	// Verify the file was created
	summaryPath := filepath.Join(runDir, runSummaryFileName)
	if _, err := os.Stat(summaryPath); os.IsNotExist(err) {
		t.Fatalf("Summary file was not created at %s", summaryPath)
	}

	// Load the summary
	loadedSummary, ok := loadRunSummary(runDir, false)
	if !ok {
		t.Fatal("Failed to load run summary")
	}

	// Verify the loaded data matches
	if loadedSummary.CLIVersion != testSummary.CLIVersion {
		t.Errorf("CLIVersion mismatch: got %s, want %s", loadedSummary.CLIVersion, testSummary.CLIVersion)
	}
	if loadedSummary.RunID != testSummary.RunID {
		t.Errorf("RunID mismatch: got %d, want %d", loadedSummary.RunID, testSummary.RunID)
	}
	if loadedSummary.Run.DatabaseID != testSummary.Run.DatabaseID {
		t.Errorf("Run.DatabaseID mismatch: got %d, want %d", loadedSummary.Run.DatabaseID, testSummary.Run.DatabaseID)
	}
	if loadedSummary.Metrics.TokenUsage != testSummary.Metrics.TokenUsage {
		t.Errorf("Metrics.TokenUsage mismatch: got %d, want %d", loadedSummary.Metrics.TokenUsage, testSummary.Metrics.TokenUsage)
	}
	if len(loadedSummary.MissingTools) != len(testSummary.MissingTools) {
		t.Errorf("MissingTools length mismatch: got %d, want %d", len(loadedSummary.MissingTools), len(testSummary.MissingTools))
	}
}

func TestLoadRunSummaryVersionMismatch(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")
	runDir := filepath.Join(tmpDir, "run-12345")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Set a test version and create a summary
	originalVersion := GetVersion()
	SetVersionInfo("1.2.3-test")
	defer SetVersionInfo(originalVersion)

	testSummary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       12345,
		ProcessedAt: time.Now(),
		Run: WorkflowRun{
			DatabaseID: 12345,
			Number:     42,
		},
	}

	// Save the summary
	if err := saveRunSummary(runDir, testSummary, false); err != nil {
		t.Fatalf("Failed to save run summary: %v", err)
	}

	// Change the version
	SetVersionInfo("2.0.0-different")

	// Try to load with different version
	loadedSummary, ok := loadRunSummary(runDir, false)
	if ok {
		t.Fatal("Expected loadRunSummary to return false due to version mismatch, but it returned true")
	}
	if loadedSummary != nil {
		t.Errorf("Expected nil summary due to version mismatch, but got: %+v", loadedSummary)
	}
}

func TestLoadRunSummaryMissingFile(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")
	runDir := filepath.Join(tmpDir, "run-12345")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Try to load from directory with no summary file
	loadedSummary, ok := loadRunSummary(runDir, false)
	if ok {
		t.Fatal("Expected loadRunSummary to return false for missing file, but it returned true")
	}
	if loadedSummary != nil {
		t.Errorf("Expected nil summary for missing file, but got: %+v", loadedSummary)
	}
}

func TestLoadRunSummaryInvalidJSON(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")
	runDir := filepath.Join(tmpDir, "run-12345")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Write invalid JSON to the summary file
	summaryPath := filepath.Join(runDir, runSummaryFileName)
	if err := os.WriteFile(summaryPath, []byte("invalid json {"), 0644); err != nil {
		t.Fatalf("Failed to write invalid JSON: %v", err)
	}

	// Try to load the invalid summary
	loadedSummary, ok := loadRunSummary(runDir, false)
	if ok {
		t.Fatal("Expected loadRunSummary to return false for invalid JSON, but it returned true")
	}
	if loadedSummary != nil {
		t.Errorf("Expected nil summary for invalid JSON, but got: %+v", loadedSummary)
	}
}

func TestListArtifacts(t *testing.T) {
	// Create a temporary directory structure for testing
	tmpDir := testutil.TempDir(t, "test-*")
	runDir := filepath.Join(tmpDir, "run-12345")

	// Create some test files and directories
	testFiles := []string{
		"aw_info.json",
		"agent-stdio.log",
		"safe_output.jsonl",
		"workflow-logs/job-1.txt",
		"workflow-logs/job-2.txt",
		"agent_output/output.json",
	}

	for _, file := range testFiles {
		fullPath := filepath.Join(runDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", file, err)
		}
		if err := os.WriteFile(fullPath, []byte("test content"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", file, err)
		}
	}

	// List the artifacts
	artifacts, err := listArtifacts(runDir)
	if err != nil {
		t.Fatalf("Failed to list artifacts: %v", err)
	}

	// Verify all test files are in the list
	for _, expectedFile := range testFiles {
		found := false
		for _, artifact := range artifacts {
			if artifact == expectedFile {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected artifact %s not found in list: %v", expectedFile, artifacts)
		}
	}

	// Verify the summary file itself is not in the list
	for _, artifact := range artifacts {
		if artifact == runSummaryFileName {
			t.Errorf("Summary file %s should not be in artifacts list", runSummaryFileName)
		}
	}
}

func TestRunSummaryJSONStructure(t *testing.T) {
	// Verify the RunSummary struct can be properly marshaled and unmarshaled
	originalVersion := GetVersion()
	SetVersionInfo("1.2.3-test")
	defer SetVersionInfo(originalVersion)

	testSummary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       12345,
		ProcessedAt: time.Now(),
		Run: WorkflowRun{
			DatabaseID:    12345,
			Number:        42,
			URL:           "https://github.com/test/repo/actions/runs/12345",
			Status:        "completed",
			Conclusion:    "success",
			WorkflowName:  "Test Workflow",
			CreatedAt:     time.Now().Add(-1 * time.Hour),
			StartedAt:     time.Now().Add(-50 * time.Minute),
			UpdatedAt:     time.Now().Add(-10 * time.Minute),
			Event:         "push",
			HeadBranch:    "main",
			HeadSha:       "abc123",
			DisplayTitle:  "Test Run",
			Duration:      40 * time.Minute,
			TokenUsage:    1000,
			EstimatedCost: 0.05,
			Turns:         5,
			ErrorCount:    0,
			WarningCount:  1,
			LogsPath:      "/tmp/run-12345",
		},
		Metrics: workflow.LogMetrics{
			TokenUsage:    1000,
			EstimatedCost: 0.05,
			Turns:         5,
		},
		AccessAnalysis: &DomainAnalysis{
			DomainBuckets: DomainBuckets{
				AllowedDomains: []string{"github.com", "api.github.com"},
				BlockedDomains: []string{},
			},
			TotalRequests: 10,
			AllowedCount:  10,
			BlockedCount:  0,
		},
		MissingTools: []MissingToolReport{
			{
				Tool:         "test_tool",
				Reason:       "Tool not available",
				Alternatives: "alternative_tool",
				Timestamp:    time.Now().Format(time.RFC3339),
			},
		},
		MCPFailures: []MCPFailureReport{
			{
				ServerName: "test-server",
				Status:     "failed",
				Timestamp:  time.Now().Format(time.RFC3339),
			},
		},
		ArtifactsList: []string{
			"aw_info.json",
			"agent-stdio.log",
			"safe_output.jsonl",
		},
		JobDetails: []JobInfoWithDuration{
			{
				JobInfo: JobInfo{
					Name:       "test-job",
					Status:     "completed",
					Conclusion: "success",
				},
				Duration: 5 * time.Minute,
			},
		},
	}

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(testSummary, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal RunSummary to JSON: %v", err)
	}

	// Verify it's valid JSON
	var testUnmarshal RunSummary
	if err := json.Unmarshal(jsonData, &testUnmarshal); err != nil {
		t.Fatalf("Failed to unmarshal RunSummary JSON: %v", err)
	}

	// Verify key fields
	if testUnmarshal.CLIVersion != testSummary.CLIVersion {
		t.Errorf("CLIVersion mismatch after round-trip: got %s, want %s", testUnmarshal.CLIVersion, testSummary.CLIVersion)
	}
	if testUnmarshal.RunID != testSummary.RunID {
		t.Errorf("RunID mismatch after round-trip: got %d, want %d", testUnmarshal.RunID, testSummary.RunID)
	}
	if len(testUnmarshal.ArtifactsList) != len(testSummary.ArtifactsList) {
		t.Errorf("ArtifactsList length mismatch after round-trip: got %d, want %d", len(testUnmarshal.ArtifactsList), len(testSummary.ArtifactsList))
	}
}
