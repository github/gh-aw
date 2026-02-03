//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/cli/fileutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/testutil"
)

func TestCalculateDirectorySize(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) string
		expected int64
	}{
		{
			name: "empty directory",
			setup: func(t *testing.T) string {
				dir := testutil.TempDir(t, "test-*")
				return dir
			},
			expected: 0,
		},
		{
			name: "single file",
			setup: func(t *testing.T) string {
				dir := testutil.TempDir(t, "test-*")
				err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0644)
				if err != nil {
					t.Fatal(err)
				}
				return dir
			},
			expected: 5,
		},
		{
			name: "multiple files in nested directories",
			setup: func(t *testing.T) string {
				dir := testutil.TempDir(t, "test-*")
				// File 1: 10 bytes
				err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("0123456789"), 0644)
				if err != nil {
					t.Fatal(err)
				}
				// Create subdirectory
				subdir := filepath.Join(dir, "subdir")
				err = os.Mkdir(subdir, 0755)
				if err != nil {
					t.Fatal(err)
				}
				// File 2: 5 bytes
				err = os.WriteFile(filepath.Join(subdir, "file2.txt"), []byte("hello"), 0644)
				if err != nil {
					t.Fatal(err)
				}
				return dir
			},
			expected: 15,
		},
		{
			name: "nonexistent directory",
			setup: func(t *testing.T) string {
				return "/nonexistent/path/that/does/not/exist"
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := tt.setup(t)
			got := fileutil.CalculateDirectorySize(dir)
			if got != tt.expected {
				t.Errorf("fileutil.CalculateDirectorySize() = %d, want %d", got, tt.expected)
			}
		})
	}
}

func TestParseDurationString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{
			name:     "valid duration - seconds",
			input:    "5s",
			expected: 5 * time.Second,
		},
		{
			name:     "valid duration - minutes",
			input:    "2m",
			expected: 2 * time.Minute,
		},
		{
			name:     "valid duration - hours",
			input:    "1h",
			expected: 1 * time.Hour,
		},
		{
			name:     "valid duration - complex",
			input:    "1h30m45s",
			expected: 1*time.Hour + 30*time.Minute + 45*time.Second,
		},
		{
			name:     "invalid duration",
			input:    "not a duration",
			expected: 0,
		},
		{
			name:     "empty string",
			input:    "",
			expected: 0,
		},
		{
			name:     "zero duration",
			input:    "0s",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDurationString(tt.input)
			if got != tt.expected {
				t.Errorf("parseDurationString(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "string shorter than max length",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "string equal to max length",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "string longer than max length",
			input:    "hello world",
			maxLen:   8,
			expected: "hello...",
		},
		{
			name:     "max length 3 or less",
			input:    "hello",
			maxLen:   3,
			expected: "hel",
		},
		{
			name:     "max length 2",
			input:    "hello",
			maxLen:   2,
			expected: "he",
		},
		{
			name:     "max length 1",
			input:    "hello",
			maxLen:   1,
			expected: "h",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
		{
			name:     "unicode string",
			input:    "Hello 世界",
			maxLen:   9,
			expected: "Hello ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stringutil.Truncate(tt.input, tt.maxLen)
			if got != tt.expected {
				t.Errorf("stringutil.Truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.expected)
			}
		})
	}
}

// TestDownloadedFilesInAuditData verifies that downloaded files are properly included in audit data
func TestDownloadedFilesInAuditData(t *testing.T) {
	// Create temporary directory with test files
	tmpDir := testutil.TempDir(t, "test-*")

	// Create various test files
	testFiles := map[string][]byte{
		"aw_info.json":      []byte(`{"engine":"copilot"}`),
		"safe_output.jsonl": []byte(`{"test":"data"}`),
		"agent-stdio.log":   []byte("Log content\n"),
		"aw.patch":          []byte("diff content\n"),
		"prompt.txt":        []byte("Prompt text\n"),
		"run_summary.json":  []byte(`{"version":"1.0"}`),
		"firewall.md":       []byte("# Firewall Analysis\n"),
		"log.md":            []byte("# Agent Log\n"),
		"custom.json":       []byte(`{}`),
		"notes.txt":         []byte("Some notes\n"),
	}

	for filename, content := range testFiles {
		if err := os.WriteFile(filepath.Join(tmpDir, filename), content, 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	// Create a subdirectory
	subdir := filepath.Join(tmpDir, "agent_output")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "result.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("Failed to create file in subdirectory: %v", err)
	}

	// Extract downloaded files
	files := extractDownloadedFiles(tmpDir)

	// Verify we got all files (directories are now excluded)
	expectedCount := len(testFiles)
	if len(files) != expectedCount {
		t.Errorf("Expected %d files, got %d", expectedCount, len(files))
	}

	// Verify specific files have correct attributes
	fileMap := make(map[string]FileInfo)
	for _, file := range files {
		// Use basename for lookup since paths are now relative to workspace root
		basename := filepath.Base(file.Path)
		fileMap[basename] = file
	}

	// Check aw_info.json
	if info, ok := fileMap["aw_info.json"]; ok {
		if info.Description != "Engine configuration and workflow metadata" {
			t.Errorf("Expected specific description for aw_info.json, got: %s", info.Description)
		}
		if info.Size == 0 {
			t.Error("aw_info.json should have non-zero size")
		}
	} else {
		t.Error("Expected to find aw_info.json in files list")
	}

	// Check firewall.md
	if info, ok := fileMap["firewall.md"]; ok {
		if info.Description != "Firewall log analysis report" {
			t.Errorf("Expected specific description for firewall.md, got: %s", info.Description)
		}
	} else {
		t.Error("Expected to find firewall.md in files list")
	}

	// Check custom.json (should have generic description)
	if info, ok := fileMap["custom.json"]; ok {
		if info.Description != "JSON data file" {
			t.Errorf("Expected generic JSON description for custom.json, got: %s", info.Description)
		}
	} else {
		t.Error("Expected to find custom.json in files list")
	}
}

// TestConsoleOutputIncludesFileInfo verifies console output displays file information
func TestConsoleOutputIncludesFileInfo(t *testing.T) {
	// Create temporary directory
	tmpDir := testutil.TempDir(t, "test-*")

	// Create test data
	run := WorkflowRun{
		DatabaseID:   123,
		WorkflowName: "Test",
		Status:       "completed",
		Conclusion:   "success",
		CreatedAt:    time.Now(),
		Event:        "push",
		HeadBranch:   "main",
		URL:          "https://github.com/test/repo/actions/runs/123",
		LogsPath:     tmpDir,
	}

	metrics := LogMetrics{}

	processedRun := ProcessedRun{
		Run: run,
	}

	downloadedFiles := []FileInfo{
		{
			Path:        "aw_info.json",
			Size:        256,
			Description: "Engine configuration and workflow metadata",
		},
	}

	// Build audit data
	auditData := buildAuditData(processedRun, metrics, nil)
	auditData.DownloadedFiles = downloadedFiles

	// Verify downloaded files are in audit data
	if len(auditData.DownloadedFiles) != 1 {
		t.Errorf("Expected 1 downloaded file in audit data, got %d", len(auditData.DownloadedFiles))
	}

	// Verify file details are preserved
	for _, file := range auditData.DownloadedFiles {
		if file.Path == "aw_info.json" {
			if file.Size != 256 {
				t.Errorf("Expected size 256, got %d", file.Size)
			}
			if file.Description == "" {
				t.Error("Expected description to be present")
			}
		}
	}
}

// TestAuditReportFileListingIntegration demonstrates the complete file listing flow
func TestAuditReportFileListingIntegration(t *testing.T) {
	// Create a realistic audit directory structure
	tmpDir := testutil.TempDir(t, "audit-integration-*")

	// Create all common artifact files
	artifacts := map[string]string{
		"aw_info.json":      `{"engine_id":"copilot","workflow_name":"test","model":"gpt-4"}`,
		"safe_output.jsonl": `{"type":"create_issue","data":{"title":"Test"}}`,
		"agent-stdio.log":   "Agent execution log\n" + strings.Repeat(".", 500),
		"aw.patch":          "diff --git a/test.txt b/test.txt\n+new line\n",
		"prompt.txt":        "Analyze this repository",
		"run_summary.json":  `{"cli_version":"1.0","run_id":12345}`,
		"log.md":            "# Agent Execution Log\n\n## Summary\nCompleted successfully.",
		"firewall.md":       "# Firewall Analysis\n\nNo blocked requests.",
		"custom.json":       `{"custom":"data"}`,
		"notes.txt":         "Some additional notes",
	}

	for filename, content := range artifacts {
		path := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create %s: %v", filename, err)
		}
	}

	// Create directories
	dirs := []string{"agent_output", "firewall-logs", "aw-prompts"}
	for _, dir := range dirs {
		dirPath := filepath.Join(tmpDir, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
		// Add a file in each directory
		filePath := filepath.Join(dirPath, "file.txt")
		if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
			t.Fatalf("Failed to create file in %s: %v", dir, err)
		}
	}

	// Extract downloaded files (simulates what audit command does)
	files := extractDownloadedFiles(tmpDir)

	// Verify we got all expected files (directories are now excluded)
	expectedCount := len(artifacts)
	if len(files) != expectedCount {
		t.Errorf("Expected %d items, got %d", expectedCount, len(files))
		t.Logf("Files found: %v", files)
	}

	// Build a map for easy lookup
	fileMap := make(map[string]FileInfo)
	for _, f := range files {
		// Use basename for lookup since paths are now relative to workspace root
		basename := filepath.Base(f.Path)
		fileMap[basename] = f
	}

	// Test specific file descriptions
	testCases := []struct {
		path        string
		expectDesc  bool
		descPattern string
	}{
		{"aw_info.json", true, "Engine configuration"},
		{"safe_output.jsonl", true, "Safe outputs"},
		{"firewall.md", true, "Firewall log analysis"},
		{"run_summary.json", true, "Cached summary"},
		{"custom.json", true, "JSON data file"},
		{"notes.txt", true, "Text file"},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			info, ok := fileMap[tc.path]
			if !ok {
				t.Fatalf("File %s not found in extracted files", tc.path)
			}

			if tc.expectDesc && info.Description == "" {
				t.Errorf("Expected description for %s, got empty string", tc.path)
			}

			if tc.expectDesc && !strings.Contains(info.Description, tc.descPattern) {
				t.Errorf("Expected description to contain '%s', got: %s", tc.descPattern, info.Description)
			}

			// Verify size is set
			if info.Size == 0 {
				t.Errorf("Expected non-zero size for file %s", tc.path)
			}
		})
	}

	// Create a complete audit data structure
	run := WorkflowRun{
		DatabaseID:   999888,
		WorkflowName: "Integration Test",
		Status:       "completed",
		Conclusion:   "success",
		CreatedAt:    time.Now(),
		Event:        "push",
		HeadBranch:   "main",
		URL:          "https://github.com/test/repo/actions/runs/999888",
		LogsPath:     tmpDir,
	}

	metrics := LogMetrics{}

	processedRun := ProcessedRun{
		Run: run,
	}

	// Build audit data with the extracted files
	auditData := buildAuditData(processedRun, metrics, nil)

	// The buildAuditData should have extracted files automatically
	if len(auditData.DownloadedFiles) == 0 {
		t.Error("Expected downloaded files to be populated in audit data")
	}

	// Verify the data can be rendered to JSON
	jsonData, err := json.Marshal(auditData)
	if err != nil {
		t.Fatalf("Failed to marshal audit data to JSON: %v", err)
	}

	// Verify JSON contains expected fields
	var parsed map[string]any
	if err := json.Unmarshal(jsonData, &parsed); err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	if _, ok := parsed["downloaded_files"]; !ok {
		t.Error("Expected 'downloaded_files' field in JSON output")
	}

	// Verify markdown report generation
	report := generateAuditReport(processedRun, metrics, files)

	// Check report contains expected sections and file listings
	expectedInReport := []string{
		"## Downloaded Files",
		"aw_info.json",
		"Engine configuration",
		"safe_output.jsonl",
		"Safe outputs",
	}

	for _, expected := range expectedInReport {
		if !strings.Contains(report, expected) {
			t.Errorf("Expected markdown report to contain: %s", expected)
		}
	}
}
