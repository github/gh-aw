//go:build !integration

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestExtractStopTimeFromLockFile tests the ExtractStopTimeFromLockFile function
func TestExtractStopTimeFromLockFile(t *testing.T) {
	tests := []struct {
		name         string
		lockContent  string
		expectedTime string
	}{
		{
			name: "valid stop-time in GH_AW_STOP_TIME format",
			lockContent: `name: Test Workflow
on:
  workflow_dispatch:
jobs:
  stop_time_check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/github-script@v8
        env:
          GH_AW_STOP_TIME: 2025-12-31 23:59:59
          GH_AW_WORKFLOW_NAME: "Test Workflow"`,
			expectedTime: "2025-12-31 23:59:59",
		},
		{
			name: "no stop-time in lock file",
			lockContent: `name: Test Workflow
on:
  workflow_dispatch:
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Test step
        run: echo "No stop time here"`,
			expectedTime: "",
		},
		{
			name: "GH_AW_STOP_TIME with extra whitespace",
			lockContent: `name: Test Workflow
on:
  workflow_dispatch:
jobs:
  stop_time_check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/github-script@v8
        env:
          GH_AW_STOP_TIME:   2025-06-01 12:00:00  
          GH_AW_WORKFLOW_NAME: "Test Workflow"`,
			expectedTime: "2025-06-01 12:00:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary file
			tmpDir, err := os.MkdirTemp("", "lock-file-test")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			lockFile := filepath.Join(tmpDir, "test.lock.yml")
			err = os.WriteFile(lockFile, []byte(tt.lockContent), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Test extraction
			result := ExtractStopTimeFromLockFile(lockFile)
			if result != tt.expectedTime {
				t.Errorf("ExtractStopTimeFromLockFile() = %q, want %q", result, tt.expectedTime)
			}
		})
	}

	// Test non-existent file
	t.Run("non-existent file", func(t *testing.T) {
		result := ExtractStopTimeFromLockFile("/non/existent/file.lock.yml")
		if result != "" {
			t.Errorf("ExtractStopTimeFromLockFile() for non-existent file = %q, want empty string", result)
		}
	})
}

// TestResolveStopTimeRejectsMinutes tests that resolveStopTime properly rejects minute units
func TestResolveStopTimeRejectsMinutes(t *testing.T) {
	baseTime := time.Date(2025, 8, 15, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		stopTime string
		errorMsg string
	}{
		{
			name:     "reject minutes only",
			stopTime: "+30m",
			errorMsg: "minute unit 'm' is not allowed for stop-after",
		},
		{
			name:     "reject days hours and minutes",
			stopTime: "+2d5h30m",
			errorMsg: "minute unit 'm' is not allowed for stop-after",
		},
		{
			name:     "reject complex with minutes",
			stopTime: "+1d12h30m",
			errorMsg: "minute unit 'm' is not allowed for stop-after",
		},
		{
			name:     "reject only minutes at end",
			stopTime: "+1w5m",
			errorMsg: "minute unit 'm' is not allowed for stop-after",
		},
		{
			name:     "reject 90 minutes",
			stopTime: "+90m",
			errorMsg: "minute unit 'm' is not allowed for stop-after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolveStopTime(tt.stopTime, baseTime)

			if err == nil {
				t.Errorf("resolveStopTime(%q, %v) expected error but got result: %s", tt.stopTime, baseTime, result)
				return
			}

			if !strings.Contains(err.Error(), tt.errorMsg) {
				t.Errorf("resolveStopTime(%q, %v) error = %v, want to contain %v", tt.stopTime, baseTime, err.Error(), tt.errorMsg)
			}
		})
	}
}

// TestRefreshStopTimeBehavior tests that the refreshStopTime flag controls stop time preservation
func TestRefreshStopTimeBehavior(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir, err := os.MkdirTemp("", "refresh-stop-time-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a markdown workflow file with stop-after
	mdFile := filepath.Join(tmpDir, "test.md")
	lockFile := filepath.Join(tmpDir, "test.lock.yml")

	// Create a lock file with existing stop time
	existingStopTime := "2025-12-31 23:59:59"
	lockContent := fmt.Sprintf(`name: Test Workflow
on:
  workflow_dispatch:
jobs:
  stop_time_check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/github-script`+"@v8\n"+`        env:
          GH_AW_STOP_TIME: %s
          GH_AW_WORKFLOW_NAME: "Test Workflow"
`, existingStopTime)
	err = os.WriteFile(lockFile, []byte(lockContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create lock file: %v", err)
	}

	// Test 1: Default behavior should preserve existing stop time
	t.Run("default behavior preserves stop time", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetRefreshStopTime(false)

		frontmatter := map[string]any{
			"on": map[string]any{
				"workflow_dispatch": nil,
				"stop-after":        "+48h",
			},
		}

		workflowData := &WorkflowData{}
		err := compiler.processStopAfterConfiguration(frontmatter, workflowData, mdFile)
		if err != nil {
			t.Fatalf("processStopAfterConfiguration failed: %v", err)
		}

		if workflowData.StopTime != existingStopTime {
			t.Errorf("Expected stop time to be preserved as %q, got %q", existingStopTime, workflowData.StopTime)
		}
	})

	// Test 2: With refresh flag, should generate new stop time
	t.Run("refresh flag generates new stop time", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetRefreshStopTime(true)

		frontmatter := map[string]any{
			"on": map[string]any{
				"workflow_dispatch": nil,
				"stop-after":        "+48h",
			},
		}

		workflowData := &WorkflowData{}
		err := compiler.processStopAfterConfiguration(frontmatter, workflowData, mdFile)
		if err != nil {
			t.Fatalf("processStopAfterConfiguration failed: %v", err)
		}

		if workflowData.StopTime == existingStopTime {
			t.Errorf("Expected stop time to be refreshed, but got the same value: %q", workflowData.StopTime)
		}

		// Verify the new stop time is a valid timestamp
		if workflowData.StopTime == "" {
			t.Error("Expected stop time to be set, got empty string")
		}
	})

	// Test 3: First compilation without existing lock file should generate new stop time
	t.Run("first compilation generates new stop time", func(t *testing.T) {
		// Remove the lock file for this test
		os.Remove(lockFile)

		compiler := NewCompiler()
		compiler.SetRefreshStopTime(false)

		frontmatter := map[string]any{
			"on": map[string]any{
				"workflow_dispatch": nil,
				"stop-after":        "+48h",
			},
		}

		workflowData := &WorkflowData{}
		err := compiler.processStopAfterConfiguration(frontmatter, workflowData, mdFile)
		if err != nil {
			t.Fatalf("processStopAfterConfiguration failed: %v", err)
		}

		// Verify a new stop time was generated
		if workflowData.StopTime == "" {
			t.Error("Expected stop time to be set, got empty string")
		}
	})
}
