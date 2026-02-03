//go:build !integration

package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatValidationError verifies that validation errors are formatted with console styling
func TestFormatValidationError(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		expectEmpty   bool
		mustContain   []string
		mustNotChange string // Content that must be preserved
	}{
		{
			name:        "nil error returns empty string",
			err:         nil,
			expectEmpty: true,
		},
		{
			name:        "simple single-line error",
			err:         errors.New("missing required field 'engine'"),
			expectEmpty: false,
			mustContain: []string{
				"missing required field 'engine'",
			},
			mustNotChange: "missing required field 'engine'",
		},
		{
			name:        "error with example",
			err:         errors.New("invalid engine: unknown. Valid engines are: copilot, claude, codex, custom. Example: engine: copilot"),
			expectEmpty: false,
			mustContain: []string{
				"invalid engine",
				"Valid engines are",
				"Example:",
			},
			mustNotChange: "invalid engine: unknown. Valid engines are: copilot, claude, codex, custom. Example: engine: copilot",
		},
		{
			name: "multi-line error",
			err: errors.New(`invalid configuration:
  field 'engine' is required
  field 'on' is missing`),
			expectEmpty: false,
			mustContain: []string{
				"invalid configuration",
				"field 'engine' is required",
				"field 'on' is missing",
			},
		},
		{
			name: "structured validation error (GitHubToolsetValidationError)",
			err: workflow.NewGitHubToolsetValidationError(map[string][]string{
				"issues": {"list_issues", "create_issue"},
			}),
			expectEmpty: false,
			mustContain: []string{
				"ERROR",
				"issues",
				"list_issues",
				"create_issue",
				"Suggested fix",
			},
		},
		{
			name: "error with formatting characters",
			err:  fmt.Errorf("path must be relative, got: /absolute/path"),
			mustContain: []string{
				"path must be relative",
				"/absolute/path",
			},
			mustNotChange: "path must be relative, got: /absolute/path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatValidationError(tt.err)

			if tt.expectEmpty {
				assert.Empty(t, result, "Expected empty string for nil error")
				return
			}

			// Verify content is preserved
			if tt.mustNotChange != "" {
				assert.Contains(t, result, tt.mustNotChange,
					"Formatted error must contain original error message")
			}

			// Verify all required content is present
			for _, expected := range tt.mustContain {
				assert.Contains(t, result, expected,
					"Formatted error must contain: %s", expected)
			}

			// Verify formatting is applied (should not be identical to plain error)
			if tt.err != nil && !tt.expectEmpty {
				plainMsg := tt.err.Error()
				// The formatted message should be longer (due to ANSI codes or prefix)
				// or at minimum have the error symbol prefix
				if result == plainMsg {
					t.Errorf("Expected formatting to be applied, but result matches plain error.\nPlain: %s\nFormatted: %s",
						plainMsg, result)
				}
			}
		})
	}
}

// TestPrintValidationError verifies that PrintValidationError outputs to stderr
// Note: This is a smoke test to ensure the function doesn't panic
func TestPrintValidationError(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "nil error does not panic",
			err:  nil,
		},
		{
			name: "simple error does not panic",
			err:  errors.New("test error"),
		},
		{
			name: "complex structured error does not panic",
			err: workflow.NewGitHubToolsetValidationError(map[string][]string{
				"repos": {"get_repository"},
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test ensures PrintValidationError doesn't panic
			// Actual output testing would require capturing stderr
			require.NotPanics(t, func() {
				PrintValidationError(tt.err)
			}, "PrintValidationError should not panic")
		})
	}
}

// TestFormatValidationErrorPreservesStructure verifies that multi-line errors maintain their structure
func TestFormatValidationErrorPreservesStructure(t *testing.T) {
	// Create a structured error with multiple lines and sections
	structuredErr := workflow.NewGitHubToolsetValidationError(map[string][]string{
		"issues":  {"list_issues", "create_issue"},
		"actions": {"list_workflows"},
	})

	result := FormatValidationError(structuredErr)

	// Verify structure is preserved
	require.NotEmpty(t, result, "Result should not be empty")

	// Verify line breaks are maintained (multi-line error)
	assert.Contains(t, result, "\n", "Multi-line structure should be preserved")

	// Verify all sections are present
	sections := []string{
		"ERROR",
		"actions",
		"issues",
		"list_workflows",
		"list_issues",
		"create_issue",
		"Suggested fix",
		"toolsets:",
	}

	for _, section := range sections {
		assert.Contains(t, result, section,
			"Structured error should contain section: %s", section)
	}

	// Verify the error message contains the original structured content
	originalMsg := structuredErr.Error()
	lines := strings.Split(originalMsg, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			assert.Contains(t, result, strings.TrimSpace(line),
				"Structured error should preserve line: %s", line)
		}
	}
}

// TestFormatValidationErrorContentIntegrity verifies that formatting doesn't alter error content
func TestFormatValidationErrorContentIntegrity(t *testing.T) {
	errorMessages := []string{
		"simple error",
		"error with special chars: @#$%^&*()",
		"error with path: /home/user/file.txt",
		"error with URL: https://example.com",
		"error with code snippet: engine: copilot",
		"multi\nline\nerror\nwith\nbreaks",
		"error with numbers: 123 456 789",
		"error with quotes: 'single' and \"double\"",
	}

	for _, msg := range errorMessages {
		t.Run(fmt.Sprintf("content_integrity_%s", strings.ReplaceAll(msg, "\n", "_")), func(t *testing.T) {
			err := errors.New(msg)
			result := FormatValidationError(err)

			// Verify the original message content is present in the result
			assert.Contains(t, result, msg,
				"Formatted error must preserve original content")

			// Verify no content is lost or corrupted
			// The formatted version should contain at least as many meaningful characters
			originalLength := len(strings.TrimSpace(msg))
			// Remove common ANSI codes to get actual content length
			cleanResult := strings.ReplaceAll(result, "\033[", "")
			cleanResult = strings.ReplaceAll(cleanResult, "\x1b[", "")

			if len(cleanResult) < originalLength {
				t.Errorf("Formatting appears to have removed content. Original: %d chars, Result: %d chars",
					originalLength, len(cleanResult))
			}
		})
	}
}
