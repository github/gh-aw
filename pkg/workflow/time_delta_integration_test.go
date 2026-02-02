//go:build integration

package workflow

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"time"

	"github.com/github/gh-aw/pkg/testutil"
)

func TestStopTimeResolutionIntegration(t *testing.T) {
	tests := []struct {
		name           string
		frontmatter    string
		markdown       string
		expectStopTime bool
		shouldContain  string
	}{
		{
			name: "absolute stop-after unchanged",
			frontmatter: `---
engine: claude
on:
  schedule:
    - cron: "0 9 * * 1"
  stop-after: "2025-12-31 23:59:59"
---`,
			markdown:       "# Test Workflow\n\nThis is a test workflow.",
			expectStopTime: true,
			shouldContain:  "2025-12-31 23:59:59",
		},
		{
			name: "readable date format",
			frontmatter: `---
engine: claude
on:
  schedule:
    - cron: "0 9 * * 1"
  stop-after: "June 1, 2025"
---`,
			markdown:       "# Test Workflow\n\nThis is a test workflow.",
			expectStopTime: true,
			shouldContain:  "2025-06-01 00:00:00",
		},
		{
			name: "ordinal date format",
			frontmatter: `---
engine: claude
on:
  schedule:
    - cron: "0 9 * * 1"
  stop-after: "1st June 2025"
---`,
			markdown:       "# Test Workflow\n\nThis is a test workflow.",
			expectStopTime: true,
			shouldContain:  "2025-06-01 00:00:00",
		},
		{
			name: "US date format",
			frontmatter: `---
engine: claude
on:
  schedule:
    - cron: "0 9 * * 1"
  stop-after: "06/01/2025 15:30"
---`,
			markdown:       "# Test Workflow\n\nThis is a test workflow.",
			expectStopTime: true,
			shouldContain:  "2025-06-01 15:30:00",
		},
		{
			name: "relative stop-after gets resolved",
			frontmatter: `---
engine: claude
on:
  schedule:
    - cron: "0 9 * * 1"
  stop-after: "+25h"
---`,
			markdown:       "# Test Workflow\n\nThis is a test workflow.",
			expectStopTime: true,
			shouldContain:  "", // We'll check the format but not exact time
		},
		{
			name: "complex relative stop-after gets resolved",
			frontmatter: `---
engine: claude
on:
  schedule:
    - cron: "0 9 * * 1"  
  stop-after: "+1d12h"
---`,
			markdown:       "# Test Workflow\n\nThis is a test workflow.",
			expectStopTime: true,
			shouldContain:  "", // We'll check the format but not exact time
		},
		{
			name: "no stop-after specified",
			frontmatter: `---
engine: claude
on:
  schedule:
    - cron: "0 9 * * 1"
---`,
			markdown:       "# Test Workflow\n\nThis is a test workflow.",
			expectStopTime: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary files
			tmpDir := testutil.TempDir(t, "test-*")
			mdFile := tmpDir + "/test-workflow.md"
			lockFile := tmpDir + "/test-workflow.lock.yml"

			// Write the test workflow
			content := tt.frontmatter + "\n\n" + tt.markdown
			err := os.WriteFile(mdFile, []byte(content), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Compile the workflow
			compiler := NewCompiler()
			if err := compiler.CompileWorkflow(mdFile); err != nil {
				t.Fatalf("Failed to compile workflow: %v", err)
			}

			// Check that the lock file was created
			if _, err := os.Stat(lockFile); os.IsNotExist(err) {
				t.Fatalf("Lock file was not created: %s", lockFile)
			}

			// Read the compiled workflow
			compiledContent, err := os.ReadFile(lockFile)
			if err != nil {
				t.Fatalf("Failed to read compiled workflow: %v", err)
			}

			compiledStr := string(compiledContent)

			if tt.expectStopTime {
				// Should contain stop-time check
				if !strings.Contains(compiledStr, "GH_AW_STOP_TIME:") {
					t.Error("Compiled workflow should contain stop-time check but doesn't")
				}

				if tt.shouldContain != "" {
					// Check for specific absolute time
					if !strings.Contains(compiledStr, tt.shouldContain) {
						t.Errorf("Compiled workflow should contain %q but doesn't", tt.shouldContain)
					}
				} else {
					// For relative times, check that the format looks like a resolved timestamp
					// Extract the GH_AW_STOP_TIME value
					lines := strings.Split(compiledStr, "\n")
					var stopTimeLine string
					for _, line := range lines {
						if strings.Contains(line, "GH_AW_STOP_TIME:") {
							stopTimeLine = line
							break
						}
					}

					if stopTimeLine == "" {
						t.Error("Could not find GH_AW_STOP_TIME line in compiled workflow")
						return
					}

					// Extract the timestamp value (after colon and space)
					parts := strings.SplitN(stopTimeLine, "GH_AW_STOP_TIME:", 2)
					if len(parts) < 2 {
						t.Error("Could not extract GH_AW_STOP_TIME value from line: " + stopTimeLine)
						return
					}

					timestamp := strings.TrimSpace(parts[1])

					// Parse as timestamp to verify it's valid
					_, err := time.Parse("2006-01-02 15:04:05", timestamp)
					if err != nil {
						t.Errorf("GH_AW_STOP_TIME value %q is not a valid timestamp: %v", timestamp, err)
					}

					// Verify it's in the future (relative to now)
					parsedTime, _ := time.Parse("2006-01-02 15:04:05", timestamp)
					if parsedTime.Before(time.Now()) {
						t.Errorf("Resolved stop-time %q is in the past, expected future time", timestamp)
					}
				}
			} else {
				// Should not contain stop-time check
				if strings.Contains(compiledStr, "GH_AW_STOP_TIME:") {
					t.Error("Compiled workflow should not contain stop-time check but does")
				}
			}
		})
	}
}

func TestStopTimeResolutionError(t *testing.T) {
	tests := []struct {
		name        string
		stopTime    string
		expectedErr string
	}{
		{
			name:        "invalid relative format",
			stopTime:    "+invalid",
			expectedErr: "invalid stop-after format",
		},
		{
			name:        "invalid absolute format",
			stopTime:    "not-a-date",
			expectedErr: "invalid stop-after format",
		},
		{
			name:        "invalid month name",
			stopTime:    "Foo 1, 2025",
			expectedErr: "invalid stop-after format",
		},
		{
			name:        "minutes not allowed",
			stopTime:    "+30m",
			expectedErr: "minute unit 'm' is not allowed for stop-after",
		},
		{
			name:        "complex with minutes not allowed",
			stopTime:    "+1d12h30m",
			expectedErr: "minute unit 'm' is not allowed for stop-after",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := testutil.TempDir(t, "test-*")
			mdFile := tmpDir + "/test-workflow.md"

			content := fmt.Sprintf(`---
engine: claude
on:
  schedule:
    - cron: "0 9 * * 1"
  stop-after: "%s"
---

# Test Workflow

This is a test workflow with invalid stop-after.`, tt.stopTime)

			err := os.WriteFile(mdFile, []byte(content), 0644)
			if err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// Compile the workflow - should fail
			compiler := NewCompiler()
			err = compiler.CompileWorkflow(mdFile)
			if err == nil {
				t.Errorf("Expected compilation to fail with invalid stop-after format %q but it succeeded", tt.stopTime)
				return
			}

			if !strings.Contains(err.Error(), tt.expectedErr) {
				t.Errorf("Expected error to mention %q but got: %v", tt.expectedErr, err)
			}
		})
	}
}
