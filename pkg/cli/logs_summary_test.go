//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/github/gh-aw/pkg/workflow"
)

func TestRunSummaryAndDownloadResultEmbedRunAnalysis(t *testing.T) {
	commonFields := map[string]struct{}{
		"Run":                     {},
		"Metrics":                 {},
		"AwContext":               {},
		"TaskDomain":              {},
		"BehaviorFingerprint":     {},
		"AgenticAssessments":      {},
		"AccessAnalysis":          {},
		"FirewallAnalysis":        {},
		"RedactedDomainsAnalysis": {},
		"MissingTools":            {},
		"MissingData":             {},
		"Noops":                   {},
		"MCPFailures":             {},
		"SkillActivations":        {},
		"MCPToolUsage":            {},
		"TokenUsage":              {},
		"GitHubRateLimitUsage":    {},
		"JobDetails":              {},
	}

	runAnalysisType := reflect.TypeFor[RunAnalysis]()
	for fieldName := range commonFields {
		if _, ok := runAnalysisType.FieldByName(fieldName); !ok {
			t.Fatalf("RunAnalysis missing shared field %s", fieldName)
		}
	}

	for _, typ := range []reflect.Type{
		reflect.TypeFor[RunSummary](),
		reflect.TypeFor[DownloadResult](),
	} {
		embedded, ok := typ.FieldByName("RunAnalysis")
		if !ok || !embedded.Anonymous {
			t.Fatalf("%s must embed RunAnalysis", typ.Name())
		}
		for field := range typ.Fields() {
			if _, duplicated := commonFields[field.Name]; duplicated {
				t.Fatalf("%s duplicates shared field %s outside RunAnalysis", typ.Name(), field.Name)
			}
		}
		for fieldName := range commonFields {
			if _, ok := typ.FieldByName(fieldName); !ok {
				t.Fatalf("%s does not promote shared field %s", typ.Name(), fieldName)
			}
		}
	}

	jsonData, err := json.Marshal(RunSummary{
		RunAnalysis: RunAnalysis{
			Run:     WorkflowRun{DatabaseID: 123},
			Metrics: workflow.LogMetrics{Turns: 2},
		},
	})
	if err != nil {
		t.Fatalf("Marshal RunSummary: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(jsonData, &fields); err != nil {
		t.Fatalf("Unmarshal RunSummary JSON: %v", err)
	}
	if _, ok := fields["RunAnalysis"]; ok {
		t.Fatal("RunSummary JSON should flatten embedded RunAnalysis")
	}
	for _, fieldName := range []string{"run", "metrics"} {
		if _, ok := fields[fieldName]; !ok {
			t.Fatalf("RunSummary JSON missing flattened field %q", fieldName)
		}
	}
}

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
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID:   12345,
				Number:       42,
				WorkflowName: "Test Workflow",
				Status:       "completed",
				Conclusion:   "success",
			},
			Metrics: workflow.LogMetrics{
				TokenUsage: 1000,
				Turns:      5,
			},
			TaskDomain: &TaskDomainInfo{
				Name:  "research",
				Label: "Research",
			},
			BehaviorFingerprint: &BehaviorFingerprint{
				ExecutionStyle:  "adaptive",
				ToolBreadth:     "moderate",
				ActuationStyle:  "selective_write",
				ResourceProfile: "moderate",
				DispatchMode:    "delegated",
			},
			AgenticAssessments: []AgenticAssessment{
				{
					Kind:     "delegated_context_present",
					Severity: "info",
					Summary:  "The run preserved upstream dispatch context.",
				},
			},
			MissingTools: []MissingToolReport{
				{
					Tool:   "test_tool",
					Reason: "Tool not available",
				},
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
	if loadedSummary.TaskDomain == nil || loadedSummary.TaskDomain.Name != testSummary.TaskDomain.Name {
		t.Fatalf("TaskDomain mismatch: got %+v, want %+v", loadedSummary.TaskDomain, testSummary.TaskDomain)
	}
	if loadedSummary.BehaviorFingerprint == nil || loadedSummary.BehaviorFingerprint.DispatchMode != testSummary.BehaviorFingerprint.DispatchMode {
		t.Fatalf("BehaviorFingerprint mismatch: got %+v, want %+v", loadedSummary.BehaviorFingerprint, testSummary.BehaviorFingerprint)
	}
	if len(loadedSummary.AgenticAssessments) != len(testSummary.AgenticAssessments) {
		t.Fatalf("AgenticAssessments length mismatch: got %d, want %d", len(loadedSummary.AgenticAssessments), len(testSummary.AgenticAssessments))
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
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID: 12345,
				Number:     42,
			},
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
		"nested/" + runAPIResponseFileName,
		jobsAPIResponseFileName,
		runAPIResponseFileName,
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

	// Verify real artifact files are in the list
	expectedFiles := []string{
		"aw_info.json",
		"agent-stdio.log",
		"safe_output.jsonl",
		"workflow-logs/job-1.txt",
		"workflow-logs/job-2.txt",
		"agent_output/output.json",
		"nested/" + runAPIResponseFileName,
	}
	for _, expectedFile := range expectedFiles {
		found := slices.Contains(artifacts, expectedFile)
		if !found {
			t.Errorf("Expected artifact %s not found in list: %v", expectedFile, artifacts)
		}
	}

	// Verify synthesized cache/summary files are not in the list
	for _, artifact := range artifacts {
		if artifact == runSummaryFileName || artifact == jobsAPIResponseFileName || artifact == runAPIResponseFileName {
			t.Errorf("Synthesized file %s should not be in artifacts list", artifact)
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
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID:   12345,
				Number:       42,
				URL:          "https://github.com/test/repo/actions/runs/12345",
				Status:       "completed",
				Conclusion:   "success",
				WorkflowName: "Test Workflow",
				CreatedAt:    time.Now().Add(-1 * time.Hour),
				StartedAt:    time.Now().Add(-50 * time.Minute),
				UpdatedAt:    time.Now().Add(-10 * time.Minute),
				Event:        "push",
				HeadBranch:   "main",
				HeadSha:      "abc123",
				DisplayTitle: "Test Run",
				Duration:     40 * time.Minute,
				TokenUsage:   1000,
				Turns:        5,
				ErrorCount:   0,
				WarningCount: 1,
				LogsPath:     "/tmp/run-12345",
			},
			Metrics: workflow.LogMetrics{
				TokenUsage: 1000,
				Turns:      5,
			},
			AccessAnalysis: &DomainAnalysis{
				AnalysisBase: AnalysisBase{
					DomainBuckets: DomainBuckets{
						AllowedDomains: []string{"github.com", "api.github.com"},
						BlockedDomains: []string{},
					},
					TotalRequests:   10,
					AllowedRequests: 10,
					BlockedRequests: 0,
				},
			},
			MissingTools: []MissingToolReport{
				{
					Tool:         "test_tool",
					Reason:       "Tool not available",
					Alternatives: "alternative_tool",
					ReportProvenance: ReportProvenance{
						Timestamp: time.Now().Format(time.RFC3339),
					},
				},
			},
			MCPFailures: []MCPFailureReport{
				{
					ServerName: "test-server",
					Status:     "failed",
					ReportProvenance: ReportProvenance{
						Timestamp: time.Now().Format(time.RFC3339),
					},
				},
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
		},
		ArtifactsList: []string{
			"aw_info.json",
			"agent-stdio.log",
			"safe_output.jsonl",
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

// TestSaveAndLoadRunSummary_SafeItemsCount verifies that SafeItemsCount is correctly
// persisted to and loaded from the RunSummary cache. This is a regression test for
// the bug where filtering logs by workflow_name returned safe_items_count=0 because
// the RunSummary was saved with Run: run (which had SafeItemsCount=0) instead of
// Run: result.Run (which had SafeItemsCount set after extractCreatedItemsFromManifest).
func TestSaveAndLoadRunSummary_SafeItemsCount(t *testing.T) {
	tmpDir := testutil.TempDir(t, "test-safe-items-*")
	runDir := filepath.Join(tmpDir, "run-99999")
	if err := os.MkdirAll(runDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	originalVersion := GetVersion()
	SetVersionInfo("1.0.0-test")
	defer SetVersionInfo(originalVersion)

	testSummary := &RunSummary{
		CLIVersion:  GetVersion(),
		RunID:       99999,
		ProcessedAt: time.Now(),
		RunAnalysis: RunAnalysis{
			Run: WorkflowRun{
				DatabaseID:     99999,
				WorkflowName:   "Plan Command",
				Status:         "completed",
				Conclusion:     "success",
				SafeItemsCount: 4,
			},
		},
	}

	if err := saveRunSummary(runDir, testSummary, false); err != nil {
		t.Fatalf("Failed to save run summary: %v", err)
	}

	loaded, ok := loadRunSummary(runDir, false)
	if !ok {
		t.Fatal("Failed to load run summary")
	}

	if loaded.Run.SafeItemsCount != 4 {
		t.Errorf("SafeItemsCount not persisted: got %d, want 4", loaded.Run.SafeItemsCount)
	}
}

// TestSafeItemsCountJSONKey verifies that WorkflowRun.SafeItemsCount serializes to
// "safe_items_count" (snake_case) in JSON so downstream audit tools (e.g. api-consumption-
// report) can read it directly from run_summary.json without a fallback.
func TestSafeItemsCountJSONKey(t *testing.T) {
	run := WorkflowRun{SafeItemsCount: 7}
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	// The JSON must contain the snake_case key, not the Go field name.
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	val, ok := m["safe_items_count"]
	if !ok {
		t.Errorf("expected JSON key 'safe_items_count' not found in %s", string(data))
	}
	if v, _ := val.(float64); int(v) != 7 {
		t.Errorf("safe_items_count = %v, want 7", val)
	}
	if _, hasPascal := m["SafeItemsCount"]; hasPascal {
		t.Errorf("unexpected PascalCase key 'SafeItemsCount' found in %s; must use snake_case", string(data))
	}
}
