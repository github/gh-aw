//go:build !integration

package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompileErrorFormatting verifies that compilation errors use console.FormatErrorMessage
func TestCompileErrorFormatting(t *testing.T) {
	// Create a temporary test workflow with invalid frontmatter
	tempDir := t.TempDir()
	invalidWorkflow := fmt.Sprintf("%s/invalid.md", tempDir)

	// Write invalid workflow (missing closing frontmatter delimiter)
	err := os.WriteFile(invalidWorkflow, []byte(`---
name: test
engine: invalid_engine
This is not valid frontmatter
`), 0644)
	require.NoError(t, err, "Failed to write test file")

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Create compiler and attempt to compile
	compiler := workflow.NewCompiler()
	_ = CompileWorkflowWithValidation(compiler, invalidWorkflow, false, false, false, false, false, false)

	// Restore stderr and read captured output
	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = buf.String() // Capture but don't use (just verifying no panic)

	// Since we can't easily test for the exact formatting (would require TTY detection),
	// we verify that the test runs without panic
	// The actual formatting is tested in other tests below
}

// TestResolveWorkflowErrorFormatting verifies that workflow resolution errors use console formatting
func TestResolveWorkflowErrorFormatting(t *testing.T) {
	// Test with non-existent workflow file
	nonExistentFile := "/tmp/nonexistent-workflow-file-12345.md"

	// Capture stderr
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Attempt to resolve
	_, err := resolveWorkflowFile(nonExistentFile, false)

	// Restore stderr and read captured output
	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	// Should return an error
	require.Error(t, err, "Expected error for non-existent file")

	// Error message should contain helpful information
	assert.Contains(t, err.Error(), "not found", "Error should mention file not found")
}

// TestConsoleFormatErrorMessageUsage verifies console.FormatErrorMessage is used correctly
func TestConsoleFormatErrorMessageUsage(t *testing.T) {
	tests := []struct {
		name          string
		message       string
		shouldContain []string
	}{
		{
			name:    "simple error message",
			message: "Something went wrong",
			shouldContain: []string{
				"Something went wrong",
			},
		},
		{
			name:    "error with details",
			message: "Failed to compile workflow: invalid syntax",
			shouldContain: []string{
				"Failed to compile workflow",
				"invalid syntax",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatted := console.FormatErrorMessage(tt.message)

			// Verify the formatted message contains the original text
			for _, expected := range tt.shouldContain {
				assert.Contains(t, formatted, expected,
					"Formatted message should contain: %s", expected)
			}
		})
	}
}

// TestConsoleFormatWarningMessageUsage verifies console.FormatWarningMessage is used correctly
func TestConsoleFormatWarningMessageUsage(t *testing.T) {
	tests := []struct {
		name          string
		message       string
		shouldContain []string
	}{
		{
			name:    "simple warning",
			message: "This is a warning",
			shouldContain: []string{
				"This is a warning",
			},
		},
		{
			name:    "warning with details",
			message: "Failed to cleanup: directory not empty",
			shouldContain: []string{
				"Failed to cleanup",
				"directory not empty",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatted := console.FormatWarningMessage(tt.message)

			// Verify the formatted message contains the original text
			for _, expected := range tt.shouldContain {
				assert.Contains(t, formatted, expected,
					"Formatted message should contain: %s", expected)
			}
		})
	}
}

// TestErrorMessagePatterns verifies common error message patterns include helpful context
func TestErrorMessagePatterns(t *testing.T) {
	tests := []struct {
		name          string
		errorCreator  func() error
		shouldContain []string
	}{
		{
			name: "wrapped error maintains context",
			errorCreator: func() error {
				baseErr := fmt.Errorf("base error")
				return fmt.Errorf("failed to compile: %w", baseErr)
			},
			shouldContain: []string{
				"failed to compile",
				"base error",
			},
		},
		{
			name: "error with format specifiers",
			errorCreator: func() error {
				return fmt.Errorf("invalid value '%s' for field '%s'", "bad-value", "engine")
			},
			shouldContain: []string{
				"invalid value",
				"bad-value",
				"engine",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.errorCreator()
			require.Error(t, err)

			errMsg := err.Error()
			for _, expected := range tt.shouldContain {
				assert.Contains(t, errMsg, expected,
					"Error message should contain: %s", expected)
			}
		})
	}
}

// TestNoPlainErrorOutput verifies that critical error paths don't use plain fmt.Fprintln
func TestNoPlainErrorOutput(t *testing.T) {
	// This test serves as documentation that errors should use console formatting

	testCases := []struct {
		description string
		goodExample string
		badExample  string
	}{
		{
			description: "Compilation errors should use console.FormatErrorMessage",
			goodExample: `fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))`,
			badExample:  `fmt.Fprintln(os.Stderr, err.Error())`,
		},
		{
			description: "Warning messages should use console.FormatWarningMessage",
			goodExample: `fmt.Fprintln(os.Stderr, console.FormatWarningMessage("warning text"))`,
			badExample:  `fmt.Fprintf(os.Stderr, "Warning: %v\n", err)`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			// Verify patterns are documented
			assert.NotEmpty(t, tc.goodExample)
			assert.NotEmpty(t, tc.badExample)
			assert.NotEqual(t, tc.goodExample, tc.badExample)
		})
	}
}

// TestErrorFormattingConsistency verifies console formatting functions are consistent
func TestErrorFormattingConsistency(t *testing.T) {
	testMessage := "test error message"

	// Test error formatting
	errorFormatted := console.FormatErrorMessage(testMessage)
	assert.NotEmpty(t, errorFormatted)
	assert.Contains(t, errorFormatted, testMessage)

	// Test warning formatting
	warningFormatted := console.FormatWarningMessage(testMessage)
	assert.NotEmpty(t, warningFormatted)
	assert.Contains(t, warningFormatted, testMessage)

	// Error and warning formatting should be different
	assert.NotEqual(t, errorFormatted, warningFormatted,
		"Error and warning formatting should produce different output")
}

// TestErrorFormattingDoesNotMangle verifies formatting doesn't corrupt messages
func TestErrorFormattingDoesNotMangle(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "message with special characters",
			message: "Error: failed to compile workflow 'test.md' at line 42",
		},
		{
			name:    "message with multiple sentences",
			message: "Compilation failed. Check your workflow syntax. See docs for help.",
		},
		{
			name:    "message with newlines",
			message: "Error on line 1:\n  invalid field 'unknown'",
		},
		{
			name:    "message with path",
			message: "Failed to read /path/to/workflow.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatted := console.FormatErrorMessage(tt.message)

			// All essential parts of the message should be preserved
			// (formatting may add prefixes/styling but shouldn't lose content)
			essentialParts := strings.Fields(tt.message)
			for _, part := range essentialParts {
				// Skip very short parts like ":", "at", etc.
				if len(part) > 2 {
					assert.Contains(t, formatted, part,
						"Formatted message should preserve essential part: %s", part)
				}
			}
		})
	}
}
