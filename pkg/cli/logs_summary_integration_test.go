//go:build integration

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

// TestRunSummaryCachingBehavior tests the complete caching behavior of run summaries
func TestRunSummaryCachingBehavior(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := testutil.TempDir(t, "test-*")
	runDir := filepath.Join(tmpDir, "run-99999")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Set a test version
	originalVersion := GetVersion()
	SetVersionInfo("1.0.0-test")
	defer SetVersionInfo(originalVersion)

	// Create some test artifact files
	testFiles := map[string]string{
		"aw_info.json": `{
			"engine_id": "claude",
			"engine_name": "Claude Code",
			"model": "claude-sonnet-4",
			"version": "1.0.0",
			"workflow_name": "Test Workflow"
		}`,
		"agent-stdio.log":   "Test log content\nSome agent output\n",
		"safe_output.jsonl": `{"type":"output","content":"test"}`,
	}

	for filename, content := range testFiles {
		filePath := filepath.Join(runDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	// Create a test run summary (simulating what would happen after first download)
	testRun := WorkflowRun{
		DatabaseID:   99999,
		Number:       1,
		WorkflowName: "Test Workflow",
		Status:       "completed",
		Conclusion:   "success",
		CreatedAt:    time.Now().Add(-1 * time.Hour),
		StartedAt:    time.Now().Add(-50 * time.Minute),
		UpdatedAt:    time.Now().Add(-10 * time.Minute),
	}

	testMetrics := workflow.LogMetrics{
		TokenUsage:    5000,
		EstimatedCost: 0.25,
		Turns:         3,
	}

	testSummary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       99999,
		ProcessedAt: time.Now(),
		Run:         testRun,
		Metrics:     testMetrics,
		ArtifactsList: []string{
			"aw_info.json",
			"agent-stdio.log",
			"safe_output.jsonl",
		},
		MissingTools: []MissingToolReport{},
		MCPFailures:  []MCPFailureReport{},
	}

	// Save the summary
	if err := saveRunSummary(runDir, testSummary, false); err != nil {
		t.Fatalf("Failed to save initial run summary: %v", err)
	}

	// Verify summary file exists
	summaryPath := filepath.Join(runDir, runSummaryFileName)
	if _, err := os.Stat(summaryPath); os.IsNotExist(err) {
		t.Fatal("Summary file was not created")
	}

	// Test 1: Load with same version should succeed
	loadedSummary, ok := loadRunSummary(runDir, false)
	if !ok {
		t.Fatal("Failed to load run summary with matching version")
	}

	if loadedSummary.RunID != testSummary.RunID {
		t.Errorf("Loaded RunID mismatch: got %d, want %d", loadedSummary.RunID, testSummary.RunID)
	}
	if loadedSummary.Metrics.TokenUsage != testMetrics.TokenUsage {
		t.Errorf("Loaded TokenUsage mismatch: got %d, want %d", loadedSummary.Metrics.TokenUsage, testMetrics.TokenUsage)
	}

	// Test 2: Change version and verify cache invalidation
	SetVersionInfo("2.0.0-different")
	loadedSummary, ok = loadRunSummary(runDir, false)
	if ok {
		t.Fatal("Expected cache invalidation due to version change, but load succeeded")
	}
	if loadedSummary != nil {
		t.Error("Expected nil summary after version mismatch")
	}

	// Reset version for next test
	SetVersionInfo("1.0.0-test")

	// Test 3: Verify the summary contains all expected data
	loadedSummary, ok = loadRunSummary(runDir, false)
	if !ok {
		t.Fatal("Failed to load run summary after resetting version")
	}

	// Verify artifacts list
	if len(loadedSummary.ArtifactsList) != 3 {
		t.Errorf("Expected 3 artifacts, got %d", len(loadedSummary.ArtifactsList))
	}

	// Verify run details
	if loadedSummary.Run.WorkflowName != "Test Workflow" {
		t.Errorf("WorkflowName mismatch: got %s, want %s", loadedSummary.Run.WorkflowName, "Test Workflow")
	}
	if loadedSummary.Run.Status != "completed" {
		t.Errorf("Status mismatch: got %s, want %s", loadedSummary.Run.Status, "completed")
	}

	// Test 4: Verify summary file is valid JSON and human-readable
	summaryData, err := os.ReadFile(summaryPath)
	if err != nil {
		t.Fatalf("Failed to read summary file: %v", err)
	}

	// Should be valid JSON
	var jsonCheck map[string]any
	if err := json.Unmarshal(summaryData, &jsonCheck); err != nil {
		t.Fatalf("Summary file is not valid JSON: %v", err)
	}

	// Should contain expected top-level keys
	expectedKeys := []string{"cli_version", "run_id", "processed_at", "run", "metrics", "artifacts_list"}
	for _, key := range expectedKeys {
		if _, exists := jsonCheck[key]; !exists {
			t.Errorf("Summary JSON missing expected key: %s", key)
		}
	}
}

// TestRunSummaryPreventsReprocessing tests that summary files prevent redundant processing
func TestRunSummaryPreventsReprocessing(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	runDir := filepath.Join(tmpDir, "run-88888")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Set a test version
	originalVersion := GetVersion()
	SetVersionInfo("1.5.0-test")
	defer SetVersionInfo(originalVersion)

	// Create minimal test artifacts
	if err := os.WriteFile(filepath.Join(runDir, "aw_info.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Simulate first processing: create summary
	firstProcessTime := time.Now()
	summary := &RunSummary{
		CLIVersion:    GetVersion(),
		RunID:         88888,
		ProcessedAt:   firstProcessTime,
		Run:           WorkflowRun{DatabaseID: 88888},
		Metrics:       workflow.LogMetrics{TokenUsage: 1000},
		ArtifactsList: []string{"aw_info.json"},
	}

	if err := saveRunSummary(runDir, summary, false); err != nil {
		t.Fatalf("Failed to save summary: %v", err)
	}

	// Simulate second attempt to process same run
	// Load should succeed and return cached data
	time.Sleep(10 * time.Millisecond) // Small delay to ensure different timestamp if recreated
	loaded, ok := loadRunSummary(runDir, false)
	if !ok {
		t.Fatal("Failed to load cached summary on second access")
	}

	// Verify we got the cached version (same ProcessedAt time)
	timeDiff := loaded.ProcessedAt.Sub(firstProcessTime)
	if timeDiff > time.Millisecond {
		t.Errorf("ProcessedAt time changed unexpectedly (cached: %v, loaded: %v), suggests reprocessing occurred",
			firstProcessTime, loaded.ProcessedAt)
	}

	// Verify data is unchanged
	if loaded.Metrics.TokenUsage != 1000 {
		t.Errorf("TokenUsage changed from cached value: got %d, want 1000", loaded.Metrics.TokenUsage)
	}
}

// TestListArtifactsExcludesSummary verifies that the summary file itself is not listed as an artifact
func TestListArtifactsExcludesSummary(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-*")
	runDir := filepath.Join(tmpDir, "run-77777")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create test files including the summary
	testFiles := []string{
		"aw_info.json",
		"agent-stdio.log",
		runSummaryFileName, // This should be excluded from the list
	}

	for _, filename := range testFiles {
		filePath := filepath.Join(runDir, filename)
		if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	// List artifacts
	artifacts, err := listArtifacts(runDir)
	if err != nil {
		t.Fatalf("Failed to list artifacts: %v", err)
	}

	// Should have 2 artifacts (excluding the summary)
	if len(artifacts) != 2 {
		t.Errorf("Expected 2 artifacts (excluding summary), got %d: %v", len(artifacts), artifacts)
	}

	// Verify summary is not in the list
	for _, artifact := range artifacts {
		if artifact == runSummaryFileName {
			t.Errorf("Summary file %s should not be in artifacts list", runSummaryFileName)
		}
	}

	// Verify expected files are in the list
	expectedFiles := []string{"aw_info.json", "agent-stdio.log"}
	for _, expected := range expectedFiles {
		found := false
		for _, artifact := range artifacts {
			if artifact == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected artifact %s not found in list: %v", expected, artifacts)
		}
	}
}
