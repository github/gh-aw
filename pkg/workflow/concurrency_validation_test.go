//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConcurrencyGroupExpression(t *testing.T) {
	tests := []struct {
		name        string
		group       string
		wantErr     bool
		errorInMsg  string // expected substring in error message
		description string
	}{
		// Valid expressions
		{
			name:        "simple static group",
			group:       "my-workflow",
			wantErr:     false,
			description: "Simple string without expressions should be valid",
		},
		{
			name:        "group with github.ref",
			group:       "workflow-${{ github.ref }}",
			wantErr:     false,
			description: "Valid GitHub expression should pass",
		},
		{
			name:        "group with github.workflow",
			group:       "gh-aw-${{ github.workflow }}",
			wantErr:     false,
			description: "Valid GitHub workflow expression should pass",
		},
		{
			name:        "group with event number",
			group:       "gh-aw-${{ github.workflow }}-${{ github.event.issue.number }}",
			wantErr:     false,
			description: "Multiple expressions should be valid",
		},
		{
			name:        "group with OR operator",
			group:       "gh-aw-${{ github.event.pull_request.number || github.ref }}",
			wantErr:     false,
			description: "Expression with OR operator should parse correctly",
		},
		{
			name:        "group with complex expression",
			group:       "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number }}",
			wantErr:     false,
			description: "Complex expression with multiple OR operators should be valid",
		},
		{
			name:        "group with AND operator",
			group:       "test-${{ github.workflow && github.repository }}",
			wantErr:     false,
			description: "Expression with AND operator should parse correctly",
		},
		{
			name:        "group with NOT operator",
			group:       "test-${{ !github.event.issue }}",
			wantErr:     false,
			description: "Expression with NOT operator should parse correctly",
		},
		{
			name:        "group with parentheses",
			group:       "test-${{ (github.workflow || github.ref) && github.repository }}",
			wantErr:     false,
			description: "Expression with parentheses for precedence should be valid",
		},

		// Invalid expressions - empty/whitespace
		{
			name:        "empty string",
			group:       "",
			wantErr:     true,
			errorInMsg:  "empty concurrency group expression",
			description: "Empty string should be rejected",
		},
		{
			name:        "only whitespace",
			group:       "   ",
			wantErr:     true,
			errorInMsg:  "empty concurrency group expression",
			description: "Whitespace-only string should be rejected",
		},

		// Invalid expressions - unbalanced braces
		{
			name:        "missing closing braces",
			group:       "workflow-${{ github.ref ",
			wantErr:     true,
			errorInMsg:  "unclosed expression braces",
			description: "Missing closing }} should be caught",
		},
		{
			name:        "missing opening braces",
			group:       "workflow-github.ref }}",
			wantErr:     true,
			errorInMsg:  "unbalanced closing braces",
			description: "Missing opening ${{ should be caught",
		},
		{
			name:        "unbalanced nested braces",
			group:       "workflow-${{ github.ref }} extra }}",
			wantErr:     true,
			errorInMsg:  "unbalanced closing braces",
			description: "Extra closing braces should be caught",
		},
		{
			name:        "multiple unclosed braces",
			group:       "workflow-${{ github.ref }}-${{ github.workflow ",
			wantErr:     true,
			errorInMsg:  "unclosed expression braces",
			description: "Multiple unclosed expressions should be caught",
		},

		// Invalid expressions - empty expression content
		{
			name:        "empty expression",
			group:       "workflow-${{ }}",
			wantErr:     true,
			errorInMsg:  "empty expression content",
			description: "Empty expression content should be rejected",
		},
		{
			name:        "whitespace expression",
			group:       "workflow-${{   }}",
			wantErr:     true,
			errorInMsg:  "empty expression content",
			description: "Whitespace-only expression should be rejected",
		},

		// Invalid expressions - unbalanced parentheses
		{
			name:        "unclosed opening parenthesis",
			group:       "test-${{ (github.workflow }}",
			wantErr:     true,
			errorInMsg:  "unclosed parentheses",
			description: "Unclosed parenthesis should be caught",
		},
		{
			name:        "extra closing parenthesis",
			group:       "test-${{ github.workflow) }}",
			wantErr:     true,
			errorInMsg:  "unbalanced parentheses",
			description: "Extra closing parenthesis should be caught",
		},
		{
			name:        "mismatched parentheses",
			group:       "test-${{ ((github.workflow) }}",
			wantErr:     true,
			errorInMsg:  "unclosed parentheses",
			description: "Mismatched parentheses count should be caught",
		},

		// Invalid expressions - unbalanced quotes
		{
			name:        "unclosed single quote",
			group:       "test-${{ 'unclosed }}",
			wantErr:     true,
			errorInMsg:  "unclosed single quote",
			description: "Unclosed single quote should be caught",
		},
		{
			name:        "unclosed double quote",
			group:       "test-${{ \"unclosed }}",
			wantErr:     true,
			errorInMsg:  "unclosed double quote",
			description: "Unclosed double quote should be caught",
		},
		{
			name:        "unclosed backtick",
			group:       "test-${{ `unclosed }}",
			wantErr:     true,
			errorInMsg:  "unclosed backtick",
			description: "Unclosed backtick should be caught",
		},

		// Invalid expressions - malformed logical operators
		{
			name:        "consecutive AND operators",
			group:       "test-${{ github.workflow && && github.ref }}",
			wantErr:     true,
			errorInMsg:  "invalid expression syntax",
			description: "Consecutive && operators should be caught",
		},
		{
			name:        "consecutive OR operators",
			group:       "test-${{ github.workflow || || github.ref }}",
			wantErr:     true,
			errorInMsg:  "invalid expression syntax",
			description: "Consecutive || operators should be caught",
		},
		{
			name:        "operator at end",
			group:       "test-${{ github.workflow && }}",
			wantErr:     true,
			errorInMsg:  "invalid expression syntax",
			description: "Operator at end should be caught",
		},
		{
			name:        "operator at start",
			group:       "test-${{ && github.workflow }}",
			wantErr:     true,
			errorInMsg:  "invalid expression syntax",
			description: "Operator at start should be caught",
		},

		// Edge cases
		{
			name:        "multiple valid expressions",
			group:       "prefix-${{ github.workflow }}-middle-${{ github.ref }}-suffix",
			wantErr:     false,
			description: "Multiple valid expressions with text between should pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConcurrencyGroupExpression(tt.group)

			if tt.wantErr {
				require.Error(t, err, "Test case: %s - Expected error but got nil", tt.description)
				if tt.errorInMsg != "" {
					assert.Contains(t, err.Error(), tt.errorInMsg,
						"Error message should contain expected substring for: %s", tt.description)
				}
			} else {
				assert.NoError(t, err, "Test case: %s - Expected no error but got: %v", tt.description, err)
			}
		})
	}
}

func TestValidateBalancedBraces(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantErr    bool
		errorInMsg string
	}{
		{
			name:    "balanced single expression",
			input:   "test-${{ github.ref }}",
			wantErr: false,
		},
		{
			name:    "balanced multiple expressions",
			input:   "test-${{ github.ref }}-${{ github.workflow }}",
			wantErr: false,
		},
		{
			name:    "no expressions",
			input:   "simple-group-name",
			wantErr: false,
		},
		{
			name:       "missing closing braces",
			input:      "test-${{ github.ref",
			wantErr:    true,
			errorInMsg: "unclosed expression braces",
		},
		{
			name:       "extra closing braces",
			input:      "test-github.ref }}",
			wantErr:    true,
			errorInMsg: "unbalanced closing braces",
		},
		{
			name:       "nested incomplete expression",
			input:      "test-${{ github.ref }}-${{ incomplete",
			wantErr:    true,
			errorInMsg: "unclosed expression braces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBalancedBraces(tt.input)

			if tt.wantErr {
				require.Error(t, err, "Expected error for input: %s", tt.input)
				if tt.errorInMsg != "" {
					assert.Contains(t, err.Error(), tt.errorInMsg)
				}
			} else {
				assert.NoError(t, err, "Expected no error for input: %s", tt.input)
			}
		})
	}
}

func TestValidateExpressionContent(t *testing.T) {
	tests := []struct {
		name       string
		expr       string
		fullGroup  string
		wantErr    bool
		errorInMsg string
	}{
		{
			name:      "simple property access",
			expr:      "github.ref",
			fullGroup: "test-${{ github.ref }}",
			wantErr:   false,
		},
		{
			name:      "OR expression",
			expr:      "github.event.issue.number || github.ref",
			fullGroup: "test-${{ github.event.issue.number || github.ref }}",
			wantErr:   false,
		},
		{
			name:      "expression with parentheses",
			expr:      "(github.workflow || github.ref) && github.repository",
			fullGroup: "test-${{ (github.workflow || github.ref) && github.repository }}",
			wantErr:   false,
		},
		{
			name:       "unclosed parenthesis",
			expr:       "(github.workflow",
			fullGroup:  "test-${{ (github.workflow }}",
			wantErr:    true,
			errorInMsg: "unclosed parentheses",
		},
		{
			name:       "extra closing parenthesis",
			expr:       "github.workflow)",
			fullGroup:  "test-${{ github.workflow) }}",
			wantErr:    true,
			errorInMsg: "unbalanced parentheses",
		},
		{
			name:       "unclosed single quote",
			expr:       "github.workflow == 'test",
			fullGroup:  "test-${{ github.workflow == 'test }}",
			wantErr:    true,
			errorInMsg: "unclosed single quote",
		},
		{
			name:       "consecutive operators",
			expr:       "github.workflow && && github.ref",
			fullGroup:  "test-${{ github.workflow && && github.ref }}",
			wantErr:    true,
			errorInMsg: "invalid expression syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionContent(tt.expr, tt.fullGroup)

			if tt.wantErr {
				require.Error(t, err, "Expected error for expression: %s", tt.expr)
				if tt.errorInMsg != "" {
					assert.Contains(t, err.Error(), tt.errorInMsg)
				}
			} else {
				assert.NoError(t, err, "Expected no error for expression: %s", tt.expr)
			}
		})
	}
}

func TestValidateBalancedQuotes(t *testing.T) {
	tests := []struct {
		name       string
		expr       string
		wantErr    bool
		errorInMsg string
	}{
		{
			name:    "no quotes",
			expr:    "github.workflow",
			wantErr: false,
		},
		{
			name:    "balanced single quotes",
			expr:    "github.workflow == 'test'",
			wantErr: false,
		},
		{
			name:    "balanced double quotes",
			expr:    "github.workflow == \"test\"",
			wantErr: false,
		},
		{
			name:    "balanced backticks",
			expr:    "github.workflow == `test`",
			wantErr: false,
		},
		{
			name:    "mixed balanced quotes",
			expr:    "github.workflow == 'test' || github.ref == \"value\"",
			wantErr: false,
		},
		{
			name:    "escaped quote inside string",
			expr:    "github.workflow == 'test\\'s'",
			wantErr: false,
		},
		{
			name:       "unclosed single quote",
			expr:       "github.workflow == 'test",
			wantErr:    true,
			errorInMsg: "unclosed single quote",
		},
		{
			name:       "unclosed double quote",
			expr:       "github.workflow == \"test",
			wantErr:    true,
			errorInMsg: "unclosed double quote",
		},
		{
			name:       "unclosed backtick",
			expr:       "github.workflow == `test",
			wantErr:    true,
			errorInMsg: "unclosed backtick",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBalancedQuotes(tt.expr)

			if tt.wantErr {
				require.Error(t, err, "Expected error for expression: %s", tt.expr)
				if tt.errorInMsg != "" {
					assert.Contains(t, err.Error(), tt.errorInMsg)
				}
			} else {
				assert.NoError(t, err, "Expected no error for expression: %s", tt.expr)
			}
		})
	}
}

func TestContainsLogicalOperators(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		expected bool
	}{
		{
			name:     "no operators",
			expr:     "github.workflow",
			expected: false,
		},
		{
			name:     "has AND operator",
			expr:     "github.workflow && github.ref",
			expected: true,
		},
		{
			name:     "has OR operator",
			expr:     "github.workflow || github.ref",
			expected: true,
		},
		{
			name:     "has NOT operator",
			expr:     "!github.workflow",
			expected: true,
		},
		{
			name:     "has multiple operators",
			expr:     "!github.workflow && github.ref || github.repository",
			expected: true,
		},
		{
			name:     "has != comparison (triggers due to ! character)",
			expr:     "github.workflow != 'test'",
			expected: true, // The function detects '!' even when part of '!='; parser will handle correctly
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsLogicalOperators(tt.expr)
			assert.Equal(t, tt.expected, result, "containsLogicalOperators(%s) = %v, want %v", tt.expr, result, tt.expected)
		})
	}
}

func TestExtractGroupExpression(t *testing.T) {
	tests := []struct {
		name        string
		concurrency any
		expected    string
		description string
	}{
		{
			name:        "string format",
			concurrency: "my-workflow-group",
			expected:    "my-workflow-group",
			description: "Simple string should be returned as-is",
		},
		{
			name: "object format with group",
			concurrency: map[string]any{
				"group": "my-workflow-group",
			},
			expected:    "my-workflow-group",
			description: "Group value should be extracted from object",
		},
		{
			name: "object format with group and cancel",
			concurrency: map[string]any{
				"group":              "my-workflow-group",
				"cancel-in-progress": true,
			},
			expected:    "my-workflow-group",
			description: "Group value should be extracted ignoring other fields",
		},
		{
			name: "object format without group",
			concurrency: map[string]any{
				"cancel-in-progress": true,
			},
			expected:    "",
			description: "Missing group should return empty string",
		},
		{
			name:        "nil input",
			concurrency: nil,
			expected:    "",
			description: "Nil should return empty string",
		},
		{
			name:        "invalid type",
			concurrency: 123,
			expected:    "",
			description: "Invalid type should return empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractGroupExpression(tt.concurrency)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}

// TestValidateConcurrencyGroupExpressionRealWorld tests real-world concurrency group patterns
func TestValidateConcurrencyGroupExpressionRealWorld(t *testing.T) {
	// These are actual patterns used in the codebase
	realWorldExpressions := []struct {
		name        string
		group       string
		description string
	}{
		{
			name:        "workflow-level PR concurrency",
			group:       "gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}",
			description: "Pattern used for PR workflows with fallback",
		},
		{
			name:        "workflow-level issue concurrency",
			group:       "gh-aw-${{ github.workflow }}-${{ github.event.issue.number }}",
			description: "Pattern used for issue workflows",
		},
		{
			name:        "workflow-level command concurrency",
			group:       "gh-aw-${{ github.workflow }}-${{ github.event.issue.number || github.event.pull_request.number }}",
			description: "Pattern used for command workflows",
		},
		{
			name:        "workflow-level discussion concurrency",
			group:       "gh-aw-${{ github.workflow }}-${{ github.event.discussion.number }}",
			description: "Pattern used for discussion workflows",
		},
		{
			name:        "workflow-level push concurrency",
			group:       "gh-aw-${{ github.workflow }}-${{ github.ref }}",
			description: "Pattern used for push workflows",
		},
		{
			name:        "engine-level concurrency",
			group:       "gh-aw-copilot-${{ github.workflow }}",
			description: "Pattern used for engine-level concurrency",
		},
		{
			name:        "simple static group",
			group:       "production",
			description: "Simple static concurrency group",
		},
	}

	for _, tt := range realWorldExpressions {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConcurrencyGroupExpression(tt.group)
			assert.NoError(t, err, "Real-world pattern should be valid: %s - %s", tt.group, tt.description)
		})
	}
}

// TestValidateConcurrencyGroupExpressionErrorMessages validates error message quality
func TestValidateConcurrencyGroupExpressionErrorMessages(t *testing.T) {
	tests := []struct {
		name               string
		group              string
		expectedErrorParts []string // Parts that should be in the error message
		description        string
	}{
		{
			name:  "missing closing brace error message",
			group: "workflow-${{ github.ref",
			expectedErrorParts: []string{
				"unclosed expression braces",
				"without matching closing",
				"Add the missing closing braces",
			},
			description: "Error should be actionable and clear",
		},
		{
			name:  "unbalanced parenthesis error message",
			group: "test-${{ (github.workflow }}",
			expectedErrorParts: []string{
				"unclosed parentheses",
				"opening '('",
				"Add the missing closing ')'",
			},
			description: "Error should indicate the specific problem",
		},
		{
			name:  "empty expression error message",
			group: "test-${{ }}",
			expectedErrorParts: []string{
				"empty expression content",
				"found empty expression",
				"Provide a valid GitHub Actions expression",
			},
			description: "Error should suggest a fix",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConcurrencyGroupExpression(tt.group)
			require.Error(t, err, tt.description)

			errorMsg := err.Error()
			for _, part := range tt.expectedErrorParts {
				assert.Contains(t, errorMsg, part,
					"Error message should contain '%s' for: %s", part, tt.description)
			}
		})
	}
}

// TestValidateConcurrencyGroupExpressionWithComplexExpressions tests complex valid expressions
func TestValidateConcurrencyGroupExpressionWithComplexExpressions(t *testing.T) {
	complexExpressions := []struct {
		name        string
		group       string
		description string
	}{
		{
			name:        "nested parentheses",
			group:       "test-${{ ((github.workflow || github.ref) && github.repository) }}",
			description: "Deeply nested parentheses should be valid",
		},
		{
			name:        "multiple NOT operators",
			group:       "test-${{ !!github.workflow }}",
			description: "Double negation should parse correctly",
		},
		{
			name:        "complex boolean expression",
			group:       "test-${{ (github.workflow && github.ref) || (!github.repository && github.actor) }}",
			description: "Complex boolean logic should be valid",
		},
		{
			name:        "comparison expressions",
			group:       "test-${{ github.workflow == 'test' && github.repository != 'other' }}",
			description: "Comparison operators should be valid",
		},
	}

	for _, tt := range complexExpressions {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConcurrencyGroupExpression(tt.group)
			assert.NoError(t, err, "Complex expression should be valid: %s - %s", tt.group, tt.description)
		})
	}
}

// Benchmark tests
func BenchmarkValidateConcurrencyGroupExpression(b *testing.B) {
	testCases := []string{
		"simple-group",
		"gh-aw-${{ github.workflow }}",
		"gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}",
		"test-${{ (github.workflow || github.ref) && github.repository }}",
	}

	for _, tc := range testCases {
		b.Run(tc, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = validateConcurrencyGroupExpression(tc)
			}
		})
	}
}

func BenchmarkValidateBalancedBraces(b *testing.B) {
	input := "gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateBalancedBraces(input)
	}
}

func BenchmarkValidateExpressionContent(b *testing.B) {
	expr := "(github.workflow || github.ref) && github.repository"
	fullGroup := "test-${{ (github.workflow || github.ref) && github.repository }}"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateExpressionContent(expr, fullGroup)
	}
}

func TestExtractConcurrencyGroupFromYAML(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		expected    string
		description string
	}{
		{
			name: "simple group with double quotes",
			yaml: `concurrency:
  group: "my-workflow-group"`,
			expected:    "my-workflow-group",
			description: "Should extract group value from YAML with double quotes",
		},
		{
			name: "simple group with single quotes",
			yaml: `concurrency:
  group: 'my-workflow-group'`,
			expected:    "my-workflow-group",
			description: "Should extract group value from YAML with single quotes",
		},
		{
			name: "group with expression",
			yaml: `concurrency:
  group: "gh-aw-${{ github.workflow }}"`,
			expected:    "gh-aw-${{ github.workflow }}",
			description: "Should extract group with GitHub Actions expression",
		},
		{
			name: "group with cancel-in-progress",
			yaml: `concurrency:
  group: "my-workflow-group"
  cancel-in-progress: true`,
			expected:    "my-workflow-group",
			description: "Should extract group ignoring cancel-in-progress",
		},
		{
			name: "complex group expression",
			yaml: `concurrency:
  group: "gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}"
  cancel-in-progress: true`,
			expected:    "gh-aw-${{ github.workflow }}-${{ github.event.pull_request.number || github.ref }}",
			description: "Should extract complex group expression",
		},
		{
			name: "group without quotes",
			yaml: `concurrency:
  group: simple-group`,
			expected:    "simple-group",
			description: "Should extract group without quotes",
		},
		{
			name:        "string format with double quotes",
			yaml:        `concurrency: "my-workflow-group"`,
			expected:    "my-workflow-group",
			description: "Should extract string format concurrency",
		},
		{
			name:        "string format with expression",
			yaml:        `concurrency: workflow-${{ github.ref }}`,
			expected:    "workflow-${{ github.ref }}",
			description: "Should extract string format with expression",
		},
		{
			name:        "string format with unclosed expression",
			yaml:        `concurrency: workflow-${{ github.ref`,
			expected:    "workflow-${{ github.ref",
			description: "Should extract string even with malformed expression (validation will catch it)",
		},
		{
			name: "no group field",
			yaml: `concurrency:
  cancel-in-progress: true`,
			expected:    "",
			description: "Should return empty string when no group field",
		},
		{
			name:        "empty yaml",
			yaml:        "",
			expected:    "",
			description: "Should return empty string for empty input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractConcurrencyGroupFromYAML(tt.yaml)
			assert.Equal(t, tt.expected, result, tt.description)
		})
	}
}
