//go:build !integration

package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateYAML_ErrorWrapping verifies that errors from buildJobsAndValidate
// are properly wrapped when returned from generateYAML
func TestGenerateYAML_ErrorWrapping(t *testing.T) {
	tests := []struct {
		name            string
		workflowContent string
		wantErrContains []string
	}{
		{
			name: "circular job dependency wraps error",
			workflowContent: `---
engine: copilot
on:
  issues:
    types: [opened]
permissions:
  issues: read
  pull-requests: read
jobs:
  job1:
    runs-on: ubuntu-latest
    needs: [job2]
    steps:
      - run: echo "test"
  job2:
    runs-on: ubuntu-latest
    needs: [job1]
    steps:
      - run: echo "test"
---
# Test Workflow`,
			wantErrContains: []string{
				"failed to generate YAML",
				"failed to build and validate jobs",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary test file
			tmpDir := t.TempDir()
			testFile := filepath.Join(tmpDir, "test.md")
			err := os.WriteFile(testFile, []byte(tt.workflowContent), 0644)
			require.NoError(t, err, "Failed to create test file")

			// Create compiler and try to compile
			compiler := NewCompilerWithVersion("1.0.0")
			err = compiler.CompileWorkflow(testFile)

			// Should return an error
			require.Error(t, err, "Expected error from invalid workflow")

			// Error should contain all wrapping messages
			for _, wantStr := range tt.wantErrContains {
				assert.Contains(t, err.Error(), wantStr,
					"Error should contain: %s", wantStr)
			}

			// Verify error can be unwrapped to find the root cause
			// The error chain should be preserved
			var unwrappedErr error
			for unwrappedErr = err; errors.Unwrap(unwrappedErr) != nil; unwrappedErr = errors.Unwrap(unwrappedErr) {
				// Walk the error chain
			}
			assert.Error(t, unwrappedErr, "Should have an unwrapped error at the end of the chain")
		})
	}
}

// TestGenerateYAML_ErrorChainPreservation validates that the error chain
// is preserved through multiple layers of error wrapping
func TestGenerateYAML_ErrorChainPreservation(t *testing.T) {
	// Create a workflow with a validation error that will propagate through
	// buildJobsAndValidate -> generateYAML -> generateAndValidateYAML
	workflowContent := `---
engine: copilot
on:
  issues:
    types: [opened]
permissions:
  issues: read
  pull-requests: read
jobs:
  test_job:
    runs-on: ubuntu-latest
    needs: [nonexistent_job]
    steps:
      - run: echo "test"
---
# Test Workflow`

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.md")
	err := os.WriteFile(testFile, []byte(workflowContent), 0644)
	require.NoError(t, err)

	compiler := NewCompilerWithVersion("1.0.0")
	err = compiler.CompileWorkflow(testFile)

	require.Error(t, err, "Expected error from invalid job dependency")

	// The error should be wrapped multiple times:
	// 1. Original error from job validation
	// 2. Wrapped by buildJobsAndValidate
	// 3. Wrapped by generateYAML
	// 4. Wrapped by generateAndValidateYAML with formatCompilerError

	// Count the layers by unwrapping
	layers := 0
	for currentErr := err; currentErr != nil; currentErr = errors.Unwrap(currentErr) {
		layers++
		if layers > 10 {
			t.Fatal("Too many error layers - possible infinite loop")
		}
	}

	// Should have at least 2 layers of wrapping (generateYAML + formatCompilerError)
	assert.GreaterOrEqual(t, layers, 2, "Error should be wrapped multiple times")

	// The error message should contain file path (from formatCompilerError)
	assert.Contains(t, err.Error(), "test.md", "Error should contain file path")

	// The error message should contain context from wrapping
	errMsg := err.Error()
	assert.NotEmpty(t, errMsg, "Error message should not be empty")
	assert.Greater(t, len(errMsg), 50, "Error message should be descriptive")
}

// TestBuildJobsAndValidate_ErrorWrapping verifies that buildJobsAndValidate
// properly wraps errors with context messages
func TestBuildJobsAndValidate_ErrorWrapping(t *testing.T) {
	tests := []struct {
		name            string
		workflowContent string
		wantErrContains []string // Multiple strings that should all be present
	}{
		{
			name: "dependency validation error is wrapped",
			workflowContent: `---
engine: copilot
on:
  issues:
    types: [opened]
permissions:
  issues: read
  pull-requests: read
jobs:
  job1:
    runs-on: ubuntu-latest
    needs: [missing_job]
    steps:
      - run: echo "test"
---
# Test`,
			wantErrContains: []string{
				"failed to generate YAML",
				"failed to build and validate jobs",
				"job dependency validation failed",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			testFile := filepath.Join(tmpDir, "test.md")
			err := os.WriteFile(testFile, []byte(tt.workflowContent), 0644)
			require.NoError(t, err)

			compiler := NewCompilerWithVersion("1.0.0")
			err = compiler.CompileWorkflow(testFile)

			require.Error(t, err, "Expected error")

			// All expected strings should be present in the error
			for _, wantStr := range tt.wantErrContains {
				assert.Contains(t, err.Error(), wantStr,
					"Error should contain: %s", wantStr)
			}

			// Error should be wrapped (errors.Unwrap should return non-nil)
			unwrapped := errors.Unwrap(err)
			assert.Error(t, unwrapped, "Error should be wrapped")
		})
	}
}
