//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

// TestParseExpressionComprehensive provides extensive test coverage for the expression parser
// including edge cases, error conditions, and complex nested scenarios
func TestParseExpressionComprehensive(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		wantErr     bool
		errorString string
	}{
		// Basic operator tests
		{
			name:     "single literal",
			input:    "github.event_name",
			expected: "github.event_name",
			wantErr:  false,
		},
		{
			name:     "simple AND operation",
			input:    "a && b",
			expected: "(a) && (b)",
			wantErr:  false,
		},
		{
			name:     "simple OR operation",
			input:    "a || b",
			expected: "(a) || (b)",
			wantErr:  false,
		},
		{
			name:     "simple NOT operation",
			input:    "!a",
			expected: "!(a)",
			wantErr:  false,
		},

		// Operator precedence tests
		{
			name:     "AND has higher precedence than OR",
			input:    "a || b && c",
			expected: "(a) || ((b) && (c))",
			wantErr:  false,
		},
		{
			name:     "multiple AND with OR",
			input:    "a && b || c && d",
			expected: "((a) && (b)) || ((c) && (d))",
			wantErr:  false,
		},
		{
			name:     "NOT has highest precedence",
			input:    "!a && b",
			expected: "(!(a)) && (b)",
			wantErr:  false,
		},
		{
			name:     "NOT with OR",
			input:    "!a || b",
			expected: "(!(a)) || (b)",
			wantErr:  false,
		},
		{
			name:     "multiple NOT operations",
			input:    "!!a",
			expected: "!(!(a))",
			wantErr:  false,
		},
		{
			name:     "NOT with AND and OR",
			input:    "!a && b || c",
			expected: "((!(a)) && (b)) || (c)",
			wantErr:  false,
		},

		// Parentheses tests
		{
			name:     "simple parentheses",
			input:    "(a)",
			expected: "a",
			wantErr:  false,
		},
		{
			name:     "parentheses override precedence",
			input:    "(a || b) && c",
			expected: "((a) || (b)) && (c)",
			wantErr:  false,
		},
		{
			name:     "nested parentheses",
			input:    "((a && b) || (c && d))",
			expected: "((a) && (b)) || ((c) && (d))",
			wantErr:  false,
		},
		{
			name:     "deeply nested parentheses",
			input:    "(((((a)))))",
			expected: "a",
			wantErr:  false,
		},
		{
			name:     "complex nested expression",
			input:    "((a || b) && (c || d)) || ((e && f) || (g && h))",
			expected: "(((a) || (b)) && ((c) || (d))) || (((e) && (f)) || ((g) && (h)))",
			wantErr:  false,
		},

		// NOT with parentheses
		{
			name:     "NOT with parentheses",
			input:    "!(a && b)",
			expected: "!((a) && (b))",
			wantErr:  false,
		},
		{
			name:     "NOT in parentheses",
			input:    "(!a) && b",
			expected: "(!(a)) && (b)",
			wantErr:  false,
		},
		{
			name:     "complex NOT expression",
			input:    "!(a && (b || !c))",
			expected: "!((a) && ((b) || (!(c))))",
			wantErr:  false,
		},

		// Complex real-world expressions (using allowed expressions)
		{
			name:     "GitHub Actions condition - issue events",
			input:    "github.workflow == 'issues' && github.repository == 'test/repo'",
			expected: "(github.workflow == 'issues') && (github.repository == 'test/repo')",
			wantErr:  false,
		},
		{
			name:     "GitHub Actions condition - run check",
			input:    "github.run_id && !github.workflow",
			expected: "(github.run_id) && (!(github.workflow))",
			wantErr:  false,
		},
		{
			name:     "GitHub Actions condition - multiple checks",
			input:    "(github.workflow && github.repository) || (github.run_id && github.actor)",
			expected: "((github.workflow) && (github.repository)) || ((github.run_id) && (github.actor))",
			wantErr:  false,
		},

		// Function calls and complex literals
		{
			name:     "function call in expression",
			input:    "contains(github.workflow, 'test') && github.repository",
			expected: "(contains(github.workflow, 'test')) && (github.repository)",
			wantErr:  false,
		},
		{
			name:     "nested function calls",
			input:    "contains(labels, 'urgent') || (contains(labels, 'critical') && !contains(labels, 'wip'))",
			expected: "(contains(labels, 'urgent')) || ((contains(labels, 'critical')) && (!(contains(labels, 'wip'))))",
			wantErr:  false,
		},

		// String literals with special characters
		{
			name:     "string with operators inside",
			input:    "github.workflow == 'Fix && improve' || github.repository == 'done'",
			expected: "(github.workflow == 'Fix && improve') || (github.repository == 'done')",
			wantErr:  false,
		},
		{
			name:     "string with parentheses inside",
			input:    "github.workflow == 'test (draft)' && github.repository",
			expected: "(github.workflow == 'test (draft)') && (github.repository)",
			wantErr:  false,
		},
		{
			name:     "string with NOT operator inside",
			input:    "github.workflow == '!important note' || github.repository",
			expected: "(github.workflow == '!important note') || (github.repository)",
			wantErr:  false,
		},

		// OR with string literals (fallback patterns)
		{
			name:     "OR with single-quoted literal",
			input:    "inputs.repository || 'FStarLang/FStar'",
			expected: "(inputs.repository) || ('FStarLang/FStar')",
			wantErr:  false,
		},
		{
			name:     "OR with double-quoted literal",
			input:    `inputs.name || "default-name"`,
			expected: `(inputs.name) || ("default-name")`,
			wantErr:  false,
		},
		{
			name:     "OR with backtick literal",
			input:    "inputs.config || `default-config`",
			expected: "(inputs.config) || (`default-config`)",
			wantErr:  false,
		},
		{
			name:     "OR with number literal",
			input:    "inputs.count || 42",
			expected: "(inputs.count) || (42)",
			wantErr:  false,
		},
		{
			name:     "OR with boolean literal",
			input:    "inputs.flag || true",
			expected: "(inputs.flag) || (true)",
			wantErr:  false,
		},
		{
			name:     "complex OR with literal and parentheses",
			input:    "(inputs.value || 'default') && github.actor",
			expected: "((inputs.value) || ('default')) && (github.actor)",
			wantErr:  false,
		},
		{
			name:     "multiple OR with mixed literals",
			input:    "inputs.a || 'default-a' || inputs.b || 'default-b'",
			expected: "(((inputs.a) || ('default-a')) || (inputs.b)) || ('default-b')",
			wantErr:  false,
		},

		// Whitespace handling
		{
			name:     "expression with extra whitespace",
			input:    "  a  &&  b  ||  c  ",
			expected: "((a) && (b)) || (c)",
			wantErr:  false,
		},
		{
			name:     "expression with tabs and newlines",
			input:    "a\t&&\tb\n||\nc",
			expected: "((a) && (b)) || (c)",
			wantErr:  false,
		},
		{
			name:     "whitespace in parentheses",
			input:    "( a && b ) || ( c )",
			expected: "((a) && (b)) || (c)",
			wantErr:  false,
		},

		// Error cases - malformed expressions
		{
			name:        "empty expression",
			input:       "",
			wantErr:     true,
			errorString: "empty expression",
		},
		{
			name:        "only whitespace",
			input:       "   ",
			wantErr:     true,
			errorString: "empty expression",
		},
		{
			name:        "missing closing parenthesis",
			input:       "(a && b",
			wantErr:     true,
			errorString: "expected ')'",
		},
		{
			name:        "missing opening parenthesis",
			input:       "a && b)",
			wantErr:     true,
			errorString: "unexpected token ')'",
		},
		{
			name:        "empty parentheses",
			input:       "()",
			wantErr:     true,
			errorString: "unexpected token ')'",
		},
		{
			name:        "mismatched parentheses",
			input:       "((a && b) || c",
			wantErr:     true,
			errorString: "expected ')'",
		},

		// Error cases - invalid operators
		{
			name:        "consecutive AND operators",
			input:       "a && && b",
			wantErr:     true,
			errorString: "unexpected token '&&'",
		},
		{
			name:        "consecutive OR operators",
			input:       "a || || b",
			wantErr:     true,
			errorString: "unexpected token '||'",
		},
		{
			name:        "operator at end",
			input:       "a &&",
			wantErr:     true,
			errorString: "unexpected token",
		},
		{
			name:        "operator at start",
			input:       "&& a",
			wantErr:     true,
			errorString: "unexpected token '&&'",
		},
		{
			name:        "only operators",
			input:       "&&",
			wantErr:     true,
			errorString: "unexpected token '&&'",
		},
		{
			name:        "only OR operator",
			input:       "||",
			wantErr:     true,
			errorString: "unexpected token '||'",
		},

		// Error cases - NOT operator issues
		{
			name:        "NOT at end without operand",
			input:       "a && !",
			wantErr:     true,
			errorString: "unexpected token",
		},
		{
			name:        "multiple NOT without operand",
			input:       "!!",
			wantErr:     true,
			errorString: "unexpected token",
		},

		// Edge cases with quotes and escaping
		{
			name:     "escaped quotes in string",
			input:    "github.workflow == 'can\\'t reproduce' && github.repository == 'open'",
			expected: "(github.workflow == 'can\\'t reproduce') && (github.repository == 'open')",
			wantErr:  false,
		},
		{
			name:     "double quotes with operators",
			input:    "github.workflow == \"Fix && Improve\" || github.repository",
			expected: "(github.workflow == \"Fix && Improve\") || (github.repository)",
			wantErr:  false,
		},

		// Complex integration test cases
		{
			name:     "very complex expression",
			input:    "((github.workflow == 'issues' || github.repository == 'issue_comment') && (github.actor == 'opened' || github.run_id == 'created')) || (github.workflow == 'pull_request' && !github.repository && (github.actor == 'opened' || github.run_id == 'synchronize'))",
			expected: "(((github.workflow == 'issues') || (github.repository == 'issue_comment')) && ((github.actor == 'opened') || (github.run_id == 'created'))) || (((github.workflow == 'pull_request') && (!(github.repository))) && ((github.actor == 'opened') || (github.run_id == 'synchronize')))",
			wantErr:  false,
		},
		{
			name:     "expression with mixed function calls",
			input:    "(contains(github.workflow, 'bug') || contains(github.repository, 'enhancement')) && !contains(github.actor, 'wontfix')",
			expected: "((contains(github.workflow, 'bug')) || (contains(github.repository, 'enhancement'))) && (!(contains(github.actor, 'wontfix')))",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseExpression(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseExpression() expected error but got none")
					return
				}
				if tt.errorString != "" && !strings.Contains(err.Error(), tt.errorString) {
					t.Errorf("ParseExpression() error = %v, should contain %q", err, tt.errorString)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseExpression() unexpected error: %v", err)
				return
			}

			if result == nil {
				t.Errorf("ParseExpression() returned nil result")
				return
			}

			rendered := result.Render()
			if rendered != tt.expected {
				t.Errorf("ParseExpression() = %q, want %q", rendered, tt.expected)
			}
		})
	}
}

// TestExpressionParserEdgeCases tests specific edge cases that could cause issues
func TestExpressionParserEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		shouldError bool
		description string
	}{
		{
			name:        "very long expression",
			input:       strings.Repeat("a && ", 100) + "b",
			shouldError: false,
			description: "Parser should handle very long expressions",
		},
		{
			name:        "deeply nested parentheses",
			input:       strings.Repeat("(", 50) + "a" + strings.Repeat(")", 50),
			shouldError: false,
			description: "Parser should handle deep nesting",
		},
		{
			name:        "expression with many operators",
			input:       "a && b || c && d || e && f || g && h",
			shouldError: false,
			description: "Parser should handle many sequential operators",
		},
		{
			name:        "complex quoted strings",
			input:       "'string with (parentheses) && operators' || 'another || string'",
			shouldError: false,
			description: "Parser should correctly handle complex quoted strings",
		},
		{
			name:        "mixed single and double quotes",
			input:       "github.workflow == 'single quoted' && github.repository == \"double quoted\"",
			shouldError: false,
			description: "Parser should handle mixed quote types",
		},
		{
			name:        "single quotes inside double quotes",
			input:       "github.workflow == \"hello 'world'\" && github.repository",
			shouldError: false,
			description: "Single quotes inside double quotes should not terminate the string",
		},
		{
			name:        "double quotes inside single quotes",
			input:       "github.workflow == 'hello \"world\"' && github.repository",
			shouldError: false,
			description: "Double quotes inside single quotes should not terminate the string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseExpression(tt.input)

			if tt.shouldError && err == nil {
				t.Errorf("Expected error for %s but got none", tt.description)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.description, err)
			}
			if !tt.shouldError && result == nil {
				t.Errorf("Got nil result for %s", tt.description)
			}
		})
	}
}

// TestExpressionSafetyComprehensive tests comprehensive expression safety validation
func TestExpressionSafetyComprehensive(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantErr     bool
		errContains string
		description string
	}{
		// Complex allowed expressions
		{
			name:        "complex allowed logical expression",
			content:     "${{ (github.workflow && github.repository) || (github.run_id && github.actor) }}",
			wantErr:     false,
			description: "Complex logical expressions with allowed terms should pass",
		},
		{
			name:        "nested allowed expression with NOT",
			content:     "${{ !((github.workflow || github.repository) && github.run_id) }}",
			wantErr:     false,
			description: "Nested expressions with NOT and allowed terms should pass",
		},
		{
			name:        "mixed allowed expressions in markdown",
			content:     "Repository: ${{ github.repository }}, workflow: ${{ github.workflow && github.actor }}, run: ${{ github.run_id || github.run_number }}",
			wantErr:     false,
			description: "Multiple complex expressions in markdown should pass if all terms are allowed",
		},

		// Complex unauthorized expressions
		{
			name:        "unauthorized in complex expression",
			content:     "${{ (github.workflow && secrets.TOKEN) || github.repository }}",
			wantErr:     true,
			errContains: "secrets.TOKEN",
			description: "Unauthorized terms in complex expressions should be caught",
		},
		{
			name:        "unauthorized with NOT operator",
			content:     "${{ !secrets.PRIVATE_KEY && github.workflow }}",
			wantErr:     true,
			errContains: "secrets.PRIVATE_KEY",
			description: "Unauthorized terms with NOT should be caught",
		},
		{
			name:        "deeply nested unauthorized",
			content:     "${{ ((github.workflow || github.repository) && (github.actor || secrets.HIDDEN)) }}",
			wantErr:     true,
			errContains: "secrets.HIDDEN",
			description: "Unauthorized terms in deeply nested expressions should be caught",
		},
		{
			name:        "multiple unauthorized in same expression",
			content:     "${{ secrets.TOKEN && env.PRIVATE_VAR && github.workflow }}",
			wantErr:     true,
			errContains: "secrets.TOKEN",
			description: "Multiple unauthorized terms should be caught",
		},

		// Mixed scenarios
		{
			name:        "authorized and unauthorized in different expressions",
			content:     "Valid: ${{ github.workflow && github.repository }}, Invalid: ${{ secrets.TOKEN }}",
			wantErr:     true,
			errContains: "secrets.TOKEN",
			description: "Should catch unauthorized expressions even when other expressions are valid",
		},
		{
			name:        "complex function calls with authorization",
			content:     "${{ github.workflow && github.repository }}",
			wantErr:     false,
			description: "Complex expressions with only allowed simple terms should pass",
		},

		// Edge cases
		{
			name:        "expression with simple terms only",
			content:     "${{ github.workflow && github.repository }}",
			wantErr:     false,
			description: "Expressions with only simple allowed terms should be parsed correctly",
		},
		{
			name:        "complex real-world expression with only allowed terms",
			content:     "${{ (github.workflow && github.repository) || (github.actor && !github.run_id) }}",
			wantErr:     false,
			description: "Complex expressions with only simple allowed terms should work correctly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateExpressionSafety(tt.content)

			if tt.wantErr && err == nil {
				t.Errorf("Expected error for %s but got none", tt.description)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.description, err)
			}
			if tt.wantErr && err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error should contain %q for %s, got: %v", tt.errContains, tt.description, err)
				}
			}
		})
	}
}

// BenchmarkParseExpression benchmarks the expression parser performance
func BenchmarkParseExpression(b *testing.B) {
	expressions := []string{
		"github.workflow",
		"github.workflow && github.repository",
		"(github.workflow || github.repository) && github.run_id",
		"!(github.workflow && (github.repository || github.run_id))",
		"((github.workflow && github.repository) || github.run_id) && github.actor",
		"(github.event_name == 'issues' && github.event.action == 'opened') || (github.event_name == 'pull_request' && !github.event.pull_request.draft)",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		expr := expressions[i%len(expressions)]
		_, err := ParseExpression(expr)
		if err != nil {
			b.Fatalf("Parse error: %v", err)
		}
	}
}

// BenchmarkExpressionSafety benchmarks the expression safety validation performance
func BenchmarkExpressionSafety(b *testing.B) {
	content := `
# Complex Workflow

This workflow uses several expressions:
- Repository: ${{ github.repository }}
- Complex condition: ${{ (github.workflow && github.repository) || github.run_id }}
- Nested condition: ${{ !((github.workflow || github.repository) && github.run_id) }}
- Real-world example: ${{ github.actor && github.run_number }}
`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := validateExpressionSafety(content)
		if err != nil {
			b.Fatalf("Safety validation error: %v", err)
		}
	}
}
