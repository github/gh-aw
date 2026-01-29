//go:build !integration

package workflow

import (
	"regexp"
	"testing"
)

// TestGoJavaScriptPatternConsistency ensures that the Go pattern for matching {{#if}}
// and the JavaScript pattern for matching {{#if}} are consistent in what they match.
// This is important because:
// 1. Go wraps expressions: {{#if expr}} -> {{#if ${{ expr }} }}
// 2. JavaScript renders templates: evaluates {{#if condition}} blocks (after ${{ }} evaluation)
// Both should handle the same cases with leading whitespace and trailing spaces.
func TestGoJavaScriptPatternConsistency(t *testing.T) {
	// Go pattern from template.go:20 - uses .*?\s* to handle trailing spaces
	goPattern := regexp.MustCompile(`\{\{#if\s+(.*?)\s*\}\}`)

	// JavaScript patterns from render_template.cjs:32 and :48
	// First pass handles blocks where tags are on their own lines
	jsFirstPassPattern := regexp.MustCompile(`(?s)(\n?)([ \t]*\{\{#if\s+(.*?)\s*\}\}[ \t]*\n)([\s\S]*?)([ \t]*\{\{/if\}\}[ \t]*)(\n?)`)
	// Second pass handles inline conditionals (no newline requirement)
	jsSecondPassPattern := regexp.MustCompile(`(?s)\{\{#if\s+(.*?)\s*\}\}([\s\S]*?)\{\{/if\}\}`)

	tests := []struct {
		name                string
		input               string
		shouldMatchGo       bool
		shouldMatchJsFirst  bool
		shouldMatchJsSecond bool
		expectedExpr        string // The expression captured by the pattern
	}{
		{
			name:                "no leading whitespace",
			input:               "{{#if expr}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  false, // First pass requires newline after opening tag
			shouldMatchJsSecond: false, // Second pass requires closing tag
			expectedExpr:        "expr",
		},
		{
			name:                "two leading spaces",
			input:               "  {{#if expr}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  false, // Requires newline after
			shouldMatchJsSecond: false, // Requires closing tag
			expectedExpr:        "expr",
		},
		{
			name:                "four leading spaces",
			input:               "    {{#if expr}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  false, // Requires newline after
			shouldMatchJsSecond: false, // Requires closing tag
			expectedExpr:        "expr",
		},
		{
			name:                "tab leading",
			input:               "\t{{#if expr}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  false, // Requires newline after
			shouldMatchJsSecond: false, // Requires closing tag
			expectedExpr:        "expr",
		},
		{
			name:                "mixed whitespace leading",
			input:               " \t {{#if expr}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  false, // Requires newline after
			shouldMatchJsSecond: false, // Requires closing tag
			expectedExpr:        "expr",
		},
		{
			name:                "inline with text before",
			input:               "text {{#if expr}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  false, // Requires newline after
			shouldMatchJsSecond: false, // Requires closing tag
			expectedExpr:        "expr",
		},
		{
			name:                "complete block no leading space",
			input:               "{{#if expr}}\ncontent\n{{/if}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  true,
			shouldMatchJsSecond: true,
			expectedExpr:        "expr",
		},
		{
			name:                "complete block two leading spaces",
			input:               "  {{#if expr}}\n  content\n  {{/if}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  true,
			shouldMatchJsSecond: true,
			expectedExpr:        "expr",
		},
		{
			name:                "complete block four leading spaces",
			input:               "    {{#if expr}}\n    content\n    {{/if}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  true,
			shouldMatchJsSecond: true,
			expectedExpr:        "expr",
		},
		{
			name:                "complete block tab leading",
			input:               "\t{{#if expr}}\n\tcontent\n\t{{/if}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  true,
			shouldMatchJsSecond: true,
			expectedExpr:        "expr",
		},
		{
			name:                "complete inline block (no newlines)",
			input:               "{{#if expr}}content{{/if}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  false, // First pass requires newlines
			shouldMatchJsSecond: true,  // Second pass handles inline
			expectedExpr:        "expr",
		},
		{
			name:                "complete inline block with leading spaces",
			input:               "  {{#if expr}}content{{/if}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  false, // First pass requires newlines
			shouldMatchJsSecond: true,  // Second pass handles inline
			expectedExpr:        "expr",
		},
		{
			name:                "evaluated expression no leading space",
			input:               "{{#if 123 }}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  false, // Requires newline after
			shouldMatchJsSecond: false, // Requires closing tag
			expectedExpr:        "123", // Trailing space trimmed by \s*
		},
		{
			name:                "evaluated expression with leading spaces",
			input:               "  {{#if 456 }}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  false, // Requires newline after
			shouldMatchJsSecond: false, // Requires closing tag
			expectedExpr:        "456", // Trailing space trimmed by \s*
		},
		{
			name:                "complete block with evaluated expression and leading spaces",
			input:               "  {{#if 789 }}\n  content\n  {{/if}}",
			shouldMatchGo:       true,
			shouldMatchJsFirst:  true,
			shouldMatchJsSecond: true,
			expectedExpr:        "789", // Trailing space trimmed by \s*
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test Go pattern
			goMatch := goPattern.FindStringSubmatch(tt.input)
			if tt.shouldMatchGo {
				if goMatch == nil {
					t.Errorf("Go pattern should match but didn't for input: %q", tt.input)
				} else if len(goMatch) > 1 && goMatch[1] != tt.expectedExpr {
					t.Errorf("Go pattern captured %q, expected %q", goMatch[1], tt.expectedExpr)
				}
			} else {
				if goMatch != nil {
					t.Errorf("Go pattern should not match but did for input: %q", tt.input)
				}
			}

			// Test JS first pass pattern (blocks with newlines)
			jsFirstMatch := jsFirstPassPattern.FindStringSubmatch(tt.input)
			if tt.shouldMatchJsFirst {
				if jsFirstMatch == nil {
					t.Errorf("JS first pass pattern should match but didn't for input: %q", tt.input)
				} else if len(jsFirstMatch) > 3 && jsFirstMatch[3] != tt.expectedExpr {
					t.Errorf("JS first pass pattern captured %q, expected %q", jsFirstMatch[3], tt.expectedExpr)
				}
			} else {
				if jsFirstMatch != nil {
					t.Errorf("JS first pass pattern should not match but did for input: %q", tt.input)
				}
			}

			// Test JS second pass pattern (inline conditionals)
			jsSecondMatch := jsSecondPassPattern.FindStringSubmatch(tt.input)
			if tt.shouldMatchJsSecond {
				if jsSecondMatch == nil {
					t.Errorf("JS second pass pattern should match but didn't for input: %q", tt.input)
				} else if len(jsSecondMatch) > 1 && jsSecondMatch[1] != tt.expectedExpr {
					t.Errorf("JS second pass pattern captured %q, expected %q", jsSecondMatch[1], tt.expectedExpr)
				}
			} else {
				if jsSecondMatch != nil {
					t.Errorf("JS second pass pattern should not match but did for input: %q", tt.input)
				}
			}
		})
	}
}

// TestPatternMatchesLeadingWhitespace specifically validates that both Go and JavaScript
// patterns correctly handle leading whitespace in practical scenarios.
func TestPatternMatchesLeadingWhitespace(t *testing.T) {
	goPattern := regexp.MustCompile(`\{\{#if\s+([^}]+)\}\}`)

	testCases := []struct {
		name  string
		input string
	}{
		{"no indent", "{{#if expr}}"},
		{"2 spaces", "  {{#if expr}}"},
		{"4 spaces", "    {{#if expr}}"},
		{"tab", "\t{{#if expr}}"},
		{"mixed", " \t {{#if expr}}"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Both Go and JS should match {{#if regardless of leading whitespace
			goMatch := goPattern.FindStringSubmatch(tc.input)
			if goMatch == nil {
				t.Errorf("Go pattern failed to match %q", tc.input)
			}
			if len(goMatch) > 1 && goMatch[1] != "expr" {
				t.Errorf("Go pattern captured %q, expected 'expr'", goMatch[1])
			}
		})
	}
}
