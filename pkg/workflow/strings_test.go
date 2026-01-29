//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSortStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "already sorted",
			input:    []string{"a", "b", "c"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "reverse order",
			input:    []string{"c", "b", "a"},
			expected: []string{"a", "b", "c"},
		},
		{
			name:     "mixed order",
			input:    []string{"github.com", "api.github.com", "raw.githubusercontent.com"},
			expected: []string{"api.github.com", "github.com", "raw.githubusercontent.com"},
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "single element",
			input:    []string{"a"},
			expected: []string{"a"},
		},
		{
			name:     "duplicates",
			input:    []string{"b", "a", "b", "c", "a"},
			expected: []string{"a", "a", "b", "b", "c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid modifying the test case
			result := make([]string, len(tt.input))
			copy(result, tt.input)

			SortStrings(result)

			assert.Equal(t, tt.expected, result, "SortStrings failed for test case: %s", tt.name)
		})
	}
}

func TestSortStrings_NilSlice(t *testing.T) {
	var nilSlice []string

	// Should not panic with nil slice
	SortStrings(nilSlice)

	assert.Nil(t, nilSlice, "SortStrings should handle nil slice without panic")
}

func TestSortPermissionScopes(t *testing.T) {
	tests := []struct {
		name     string
		input    []PermissionScope
		expected []PermissionScope
	}{
		{
			name:     "already sorted",
			input:    []PermissionScope{"actions", "contents", "issues"},
			expected: []PermissionScope{"actions", "contents", "issues"},
		},
		{
			name:     "reverse order",
			input:    []PermissionScope{"pull-requests", "issues", "contents"},
			expected: []PermissionScope{"contents", "issues", "pull-requests"},
		},
		{
			name:     "mixed order",
			input:    []PermissionScope{"issues", "actions", "pull-requests", "contents"},
			expected: []PermissionScope{"actions", "contents", "issues", "pull-requests"},
		},
		{
			name:     "empty slice",
			input:    []PermissionScope{},
			expected: []PermissionScope{},
		},
		{
			name:     "single element",
			input:    []PermissionScope{"contents"},
			expected: []PermissionScope{"contents"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy to avoid modifying the test case
			result := make([]PermissionScope, len(tt.input))
			copy(result, tt.input)

			SortPermissionScopes(result)

			assert.Equal(t, tt.expected, result, "SortPermissionScopes failed for test case: %s", tt.name)
		})
	}
}

func TestSortPermissionScopes_NilSlice(t *testing.T) {
	var nilSlice []PermissionScope

	// Should not panic with nil slice
	SortPermissionScopes(nilSlice)

	assert.Nil(t, nilSlice, "SortPermissionScopes should handle nil slice without panic")
}

func TestSanitizeWorkflowName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase conversion",
			input:    "MyWorkflow",
			expected: "myworkflow",
		},
		{
			name:     "spaces to dashes",
			input:    "My Workflow Name",
			expected: "my-workflow-name",
		},
		{
			name:     "colons to dashes",
			input:    "workflow:test",
			expected: "workflow-test",
		},
		{
			name:     "slashes to dashes",
			input:    "workflow/test",
			expected: "workflow-test",
		},
		{
			name:     "backslashes to dashes",
			input:    "workflow\\test",
			expected: "workflow-test",
		},
		{
			name:     "special characters to dashes",
			input:    "workflow@#$test",
			expected: "workflow-test",
		},
		{
			name:     "preserve dots and underscores",
			input:    "workflow.test_name",
			expected: "workflow.test_name",
		},
		{
			name:     "complex name",
			input:    "My Workflow: Test/Build",
			expected: "my-workflow-test-build",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "@#$%^&*()",
			expected: "-",
		},
		{
			name:     "unicode characters",
			input:    "workflow-αβγ-test",
			expected: "workflow-test",
		},
		{
			name:     "mixed case with numbers",
			input:    "MyWorkflow123Test",
			expected: "myworkflow123test",
		},
		{
			name:     "multiple consecutive spaces",
			input:    "workflow   test",
			expected: "workflow-test",
		},
		{
			name:     "preserve hyphens",
			input:    "my-workflow-name",
			expected: "my-workflow-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeWorkflowName(tt.input)
			assert.Equal(t, tt.expected, result, "SanitizeWorkflowName failed for test case: %s", tt.name)
		})
	}
}

func TestShortenCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short command",
			input:    "ls -la",
			expected: "ls -la",
		},
		{
			name:     "exactly 20 characters",
			input:    "12345678901234567890",
			expected: "12345678901234567890",
		},
		{
			name:     "long command gets truncated",
			input:    "this is a very long command that exceeds the limit",
			expected: "this is a very long ...",
		},
		{
			name:     "newlines replaced with spaces",
			input:    "echo hello\nworld",
			expected: "echo hello world",
		},
		{
			name:     "multiple newlines",
			input:    "line1\nline2\nline3",
			expected: "line1 line2 line3",
		},
		{
			name:     "long command with newlines",
			input:    "echo this is\na very long\ncommand with newlines",
			expected: "echo this is a very ...",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only newlines",
			input:    "\n\n\n",
			expected: "   ",
		},
		{
			name:     "unicode characters",
			input:    "echo 你好世界 αβγ test",
			expected: "echo 你好世界 α...", // Truncates at 20 bytes, not 20 characters
		},
		{
			name:     "long unicode string",
			input:    "αβγδεζηθικλμνξοπρστυφχψω",
			expected: "αβγδεζηθικ...", // Truncates at 20 bytes, not 20 characters
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShortenCommand(tt.input)
			assert.Equal(t, tt.expected, result, "ShortenCommand failed for test case: %s", tt.name)
		})
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		opts     *SanitizeOptions
		expected string
	}{
		// Test basic functionality with nil options
		{
			name:     "nil options - simple name",
			input:    "MyWorkflow",
			opts:     nil,
			expected: "myworkflow",
		},
		{
			name:     "nil options - with spaces",
			input:    "My Workflow Name",
			opts:     nil,
			expected: "my-workflow-name",
		},

		// Test with PreserveSpecialChars (SanitizeWorkflowName-like behavior)
		{
			name:  "preserve dots and underscores",
			input: "workflow.test_name",
			opts: &SanitizeOptions{
				PreserveSpecialChars: []rune{'.', '_'},
			},
			expected: "workflow.test_name",
		},
		{
			name:  "preserve dots only",
			input: "workflow.test_name",
			opts: &SanitizeOptions{
				PreserveSpecialChars: []rune{'.'},
			},
			expected: "workflow.test-name",
		},
		{
			name:  "preserve underscores only",
			input: "workflow.test_name",
			opts: &SanitizeOptions{
				PreserveSpecialChars: []rune{'_'},
			},
			expected: "workflow-test_name",
		},
		{
			name:  "complex name with preservation",
			input: "My Workflow: Test/Build",
			opts: &SanitizeOptions{
				PreserveSpecialChars: []rune{'.', '_'},
			},
			expected: "my-workflow-test-build",
		},

		// Test TrimHyphens option
		{
			name:  "trim hyphens - leading and trailing",
			input: "---workflow---",
			opts: &SanitizeOptions{
				TrimHyphens: true,
			},
			expected: "workflow",
		},
		{
			name:  "no trim hyphens - leading and trailing consolidated",
			input: "---workflow---",
			opts: &SanitizeOptions{
				TrimHyphens: false,
			},
			expected: "-workflow-", // Multiple hyphens are always consolidated
		},
		{
			name:  "trim hyphens - with special chars at edges",
			input: "@@@workflow###",
			opts: &SanitizeOptions{
				TrimHyphens: true,
			},
			expected: "workflow",
		},

		// Test DefaultValue option
		{
			name:  "empty result with default",
			input: "@@@",
			opts: &SanitizeOptions{
				DefaultValue: "default-name",
			},
			expected: "default-name",
		},
		{
			name:  "empty result without default",
			input: "@@@",
			opts: &SanitizeOptions{
				DefaultValue: "",
			},
			expected: "",
		},
		{
			name:  "empty string with default",
			input: "",
			opts: &SanitizeOptions{
				DefaultValue: "github-agentic-workflow",
			},
			expected: "github-agentic-workflow",
		},

		// Test combined options (SanitizeIdentifier-like behavior)
		{
			name:  "identifier-like: simple name",
			input: "Test Workflow Name",
			opts: &SanitizeOptions{
				TrimHyphens:  true,
				DefaultValue: "github-agentic-workflow",
			},
			expected: "test-workflow-name",
		},
		{
			name:  "identifier-like: with underscores",
			input: "Test_Workflow_Name",
			opts: &SanitizeOptions{
				TrimHyphens:  true,
				DefaultValue: "github-agentic-workflow",
			},
			expected: "test-workflow-name",
		},
		{
			name:  "identifier-like: only special chars",
			input: "@#$%!",
			opts: &SanitizeOptions{
				TrimHyphens:  true,
				DefaultValue: "github-agentic-workflow",
			},
			expected: "github-agentic-workflow",
		},

		// Test edge cases
		{
			name:  "multiple consecutive hyphens",
			input: "test---multiple----hyphens",
			opts: &SanitizeOptions{
				PreserveSpecialChars: []rune{'.', '_'},
			},
			expected: "test-multiple-hyphens",
		},
		{
			name:  "unicode characters",
			input: "workflow-αβγ-test",
			opts: &SanitizeOptions{
				PreserveSpecialChars: []rune{'.', '_'},
			},
			expected: "workflow-test",
		},
		{
			name:  "common separators replacement",
			input: "path/to\\file:name",
			opts: &SanitizeOptions{
				PreserveSpecialChars: []rune{'.', '_'},
			},
			expected: "path-to-file-name",
		},
		{
			name:  "preserve hyphens in input",
			input: "my-workflow-name",
			opts: &SanitizeOptions{
				PreserveSpecialChars: []rune{'.', '_'},
			},
			expected: "my-workflow-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeName(tt.input, tt.opts)
			assert.Equal(t, tt.expected, result, "SanitizeName failed for test case: %s", tt.name)
		})
	}
}

func TestSanitizeName_NilOptions(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "nil options - empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "nil options - only hyphens",
			input:    "---",
			expected: "-", // Multiple hyphens consolidated to single hyphen
		},
		{
			name:     "nil options - leading/trailing hyphens",
			input:    "-workflow-",
			expected: "-workflow-", // Preserved with nil opts (TrimHyphens is false)
		},
		{
			name:     "nil options - underscores replaced",
			input:    "test_workflow_name",
			expected: "test-workflow-name", // Underscores replaced when not in PreserveSpecialChars
		},
		{
			name:     "nil options - dots removed",
			input:    "workflow.test.name",
			expected: "workflowtestname", // Dots removed when PreserveSpecialChars is empty
		},
		{
			name:     "nil options - complex name",
			input:    "Test_Workflow.Name@123",
			expected: "test-workflowname123", // Special chars removed when PreserveSpecialChars is empty
		},
		{
			name:     "nil options - multiple special characters",
			input:    "workflow@#$%test",
			expected: "workflowtest", // Special chars removed when PreserveSpecialChars is empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeName(tt.input, nil)
			assert.Equal(t, tt.expected, result, "SanitizeName with nil options failed for test case: %s", tt.name)
		})
	}
}
