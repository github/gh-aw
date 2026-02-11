//go:build !integration

package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestPrintCompilationSummaryWithFailedWorkflows tests that printCompilationSummary
// displays a clear list of failed workflow IDs before showing detailed error messages
func TestPrintCompilationSummaryWithFailedWorkflows(t *testing.T) {
	tests := []struct {
		name                string
		stats               *CompilationStats
		expectedInOutput    []string
		notExpectedInOutput []string
	}{
		{
			name: "multiple failed workflows with FailureDetails",
			stats: &CompilationStats{
				Total:    5,
				Errors:   3,
				Warnings: 1,
				FailureDetails: []WorkflowFailure{
					{
						Path:          ".github/workflows/test1.md",
						ErrorCount:    1,
						ErrorMessages: []string{"test1.md:5:1: error: Invalid field"},
					},
					{
						Path:          ".github/workflows/test2.md",
						ErrorCount:    2,
						ErrorMessages: []string{"test2.md:10:1: error: Missing required field"},
					},
					{
						Path:          ".github/workflows/test3.md",
						ErrorCount:    1,
						ErrorMessages: []string{"test3.md:3:1: error: Unknown property"},
					},
				},
			},
			expectedInOutput: []string{
				"Compiled 5 workflow(s): 3 error(s), 1 warning(s)",
				"Failed workflows:",
				"✗ test1.md",
				"✗ test2.md",
				"✗ test3.md",
				"test1.md:5:1: error: Invalid field",
				"test2.md:10:1: error: Missing required field",
				"test3.md:3:1: error: Unknown property",
			},
			notExpectedInOutput: []string{},
		},
		{
			name: "single failed workflow with FailureDetails",
			stats: &CompilationStats{
				Total:  1,
				Errors: 1,
				FailureDetails: []WorkflowFailure{
					{
						Path:          ".github/workflows/workflow-single.md",
						ErrorCount:    2,
						ErrorMessages: []string{"workflow-single.md:1:1: error: First error", "workflow-single.md:2:1: error: Second error"},
					},
				},
			},
			expectedInOutput: []string{
				"Compiled 1 workflow(s): 1 error(s), 0 warning(s)",
				"Failed workflows:",
				"✗ workflow-single.md",
				"workflow-single.md:1:1: error: First error",
				"workflow-single.md:2:1: error: Second error",
			},
			notExpectedInOutput: []string{},
		},
		{
			name: "backward compatibility with FailedWorkflows",
			stats: &CompilationStats{
				Total:           3,
				Errors:          2,
				FailedWorkflows: []string{"old-workflow1.md", "old-workflow2.md"},
			},
			expectedInOutput: []string{
				"Compiled 3 workflow(s): 2 error(s), 0 warning(s)",
				"Failed workflows:",
				"✗ old-workflow1.md",
				"✗ old-workflow2.md",
			},
			notExpectedInOutput: []string{},
		},
		{
			name: "successful compilation without failures",
			stats: &CompilationStats{
				Total:    5,
				Errors:   0,
				Warnings: 0,
			},
			expectedInOutput: []string{
				"Compiled 5 workflow(s): 0 error(s), 0 warning(s)",
			},
			notExpectedInOutput: []string{
				"Failed workflows:",
				"✗",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stderr output
			oldStderr := os.Stderr
			r, w, _ := os.Pipe()
			os.Stderr = w

			// Call the function
			printCompilationSummary(tt.stats)

			// Restore stderr and capture output
			w.Close()
			os.Stderr = oldStderr

			var buf bytes.Buffer
			buf.ReadFrom(r)
			output := buf.String()

			// Check for expected content
			for _, expected := range tt.expectedInOutput {
				if !strings.Contains(output, expected) {
					t.Errorf("Expected output to contain %q, but it didn't.\nFull output:\n%s", expected, output)
				}
			}

			// Check for content that should NOT be present
			for _, notExpected := range tt.notExpectedInOutput {
				if strings.Contains(output, notExpected) {
					t.Errorf("Expected output to NOT contain %q, but it did.\nFull output:\n%s", notExpected, output)
				}
			}
		})
	}
}
