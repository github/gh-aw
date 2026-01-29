//go:build !integration

// This file provides tests for expression pattern matching.

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExpressionPattern tests the basic expression matching pattern
func TestExpressionPattern(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMatch bool
		wantValue string
	}{
		{
			name:      "simple expression",
			input:     "${{ github.actor }}",
			wantMatch: true,
			wantValue: " github.actor ",
		},
		{
			name:      "expression with property",
			input:     "${{ github.event.inputs.branch }}",
			wantMatch: true,
			wantValue: " github.event.inputs.branch ",
		},
		{
			name:      "expression with comparison",
			input:     "${{ github.workflow == 'CI' }}",
			wantMatch: true,
			wantValue: " github.workflow == 'CI' ",
		},
		{
			name:      "no expression",
			input:     "plain text",
			wantMatch: false,
		},
		{
			name:      "multiple expressions (matches first)",
			input:     "${{ github.actor }} and ${{ github.repository }}",
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := ExpressionPattern.FindStringSubmatch(tt.input)
			if tt.wantMatch {
				assert.NotNil(t, matches, "Expected to find expression match")
				if len(matches) > 1 && tt.wantValue != "" {
					assert.Equal(t, tt.wantValue, matches[1], "Expression content mismatch")
				}
			} else {
				assert.Nil(t, matches, "Expected no expression match")
			}
		})
	}
}

// TestNeedsStepsPattern tests the needs/steps context pattern
func TestNeedsStepsPattern(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{
			name:      "needs with output",
			input:     "needs.build.outputs.version",
			wantMatch: true,
		},
		{
			name:      "steps with output",
			input:     "steps.setup.outputs.path",
			wantMatch: true,
		},
		{
			name:      "needs with result",
			input:     "needs.test.result",
			wantMatch: true,
		},
		{
			name:      "steps with conclusion",
			input:     "steps.deploy.conclusion",
			wantMatch: true,
		},
		{
			name:      "invalid - github prefix",
			input:     "github.event.inputs.branch",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := NeedsStepsPattern.MatchString(tt.input)
			assert.Equal(t, tt.wantMatch, matches, "NeedsStepsPattern match result mismatch")
		})
	}
}

// TestInputsPattern tests the github.event.inputs pattern
func TestInputsPattern(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{
			name:      "valid workflow_dispatch input",
			input:     "github.event.inputs.branch",
			wantMatch: true,
		},
		{
			name:      "valid input with hyphen",
			input:     "github.event.inputs.workflow-id",
			wantMatch: true,
		},
		{
			name:      "valid input with underscore",
			input:     "github.event.inputs.some_param",
			wantMatch: true,
		},
		{
			name:      "invalid - no input name",
			input:     "github.event.inputs",
			wantMatch: false,
		},
		{
			name:      "invalid - nested property",
			input:     "github.event.inputs.branch.name",
			wantMatch: false,
		},
		{
			name:      "invalid - wrong prefix",
			input:     "inputs.branch",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := InputsPattern.MatchString(tt.input)
			assert.Equal(t, tt.wantMatch, matches, "InputsPattern match result mismatch")
		})
	}
}

// TestWorkflowCallInputsPattern tests the workflow_call inputs pattern
func TestWorkflowCallInputsPattern(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{
			name:      "valid workflow_call input",
			input:     "inputs.branch",
			wantMatch: true,
		},
		{
			name:      "valid input with hyphen",
			input:     "inputs.workflow-id",
			wantMatch: true,
		},
		{
			name:      "valid input with underscore",
			input:     "inputs.some_param",
			wantMatch: true,
		},
		{
			name:      "invalid - no input name",
			input:     "inputs",
			wantMatch: false,
		},
		{
			name:      "invalid - nested property",
			input:     "inputs.branch.name",
			wantMatch: false,
		},
		{
			name:      "invalid - github prefix",
			input:     "github.event.inputs.branch",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := WorkflowCallInputsPattern.MatchString(tt.input)
			assert.Equal(t, tt.wantMatch, matches, "WorkflowCallInputsPattern match result mismatch")
		})
	}
}

// TestSecretExpressionPattern tests the secrets expression pattern
func TestSecretExpressionPattern(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantMatch      bool
		wantSecretName string
	}{
		{
			name:           "simple secret",
			input:          "${{ secrets.GITHUB_TOKEN }}",
			wantMatch:      true,
			wantSecretName: "GITHUB_TOKEN",
		},
		{
			name:           "secret with fallback",
			input:          "${{ secrets.MY_TOKEN || 'default' }}",
			wantMatch:      true,
			wantSecretName: "MY_TOKEN",
		},
		{
			name:           "secret with underscore prefix",
			input:          "${{ secrets._INTERNAL_SECRET }}",
			wantMatch:      true,
			wantSecretName: "_INTERNAL_SECRET",
		},
		{
			name:      "invalid - lowercase secret",
			input:     "${{ secrets.my_token }}",
			wantMatch: false,
		},
		{
			name:      "invalid - starts with number",
			input:     "${{ secrets.123_TOKEN }}",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := SecretExpressionPattern.FindStringSubmatch(tt.input)
			if tt.wantMatch {
				assert.NotNil(t, matches, "Expected to find secret match")
				if len(matches) > 1 && tt.wantSecretName != "" {
					assert.Equal(t, tt.wantSecretName, matches[1], "Secret name mismatch")
				}
			} else {
				assert.Nil(t, matches, "Expected no secret match")
			}
		})
	}
}

// TestComparisonExtractionPattern tests the comparison extraction pattern
func TestComparisonExtractionPattern(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantMatch    bool
		wantProperty string
	}{
		{
			name:         "equality comparison",
			input:        "github.workflow == 'CI'",
			wantMatch:    true,
			wantProperty: "github.workflow",
		},
		{
			name:         "inequality comparison",
			input:        "github.actor != 'bot'",
			wantMatch:    true,
			wantProperty: "github.actor",
		},
		{
			name:         "less than comparison",
			input:        "count < 10",
			wantMatch:    true,
			wantProperty: "count",
		},
		{
			name:         "greater than or equal",
			input:        "value >= 5",
			wantMatch:    true,
			wantProperty: "value",
		},
		{
			name:      "no comparison operator",
			input:     "github.workflow",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := ComparisonExtractionPattern.FindStringSubmatch(tt.input)
			if tt.wantMatch {
				assert.NotNil(t, matches, "Expected to find comparison match")
				if len(matches) > 1 && tt.wantProperty != "" {
					assert.Equal(t, tt.wantProperty, matches[1], "Property name mismatch")
				}
			} else {
				assert.Nil(t, matches, "Expected no comparison match")
			}
		})
	}
}

// TestStringLiteralPattern tests string literal matching
func TestStringLiteralPattern(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{
			name:      "single quoted string",
			input:     "'hello world'",
			wantMatch: true,
		},
		{
			name:      "double quoted string",
			input:     `"hello world"`,
			wantMatch: true,
		},
		{
			name:      "backtick quoted string",
			input:     "`hello world`",
			wantMatch: true,
		},
		{
			name:      "empty single quotes",
			input:     "''",
			wantMatch: true,
		},
		{
			name:      "not a string literal",
			input:     "hello world",
			wantMatch: false,
		},
		{
			name:      "mismatched quotes",
			input:     "'hello\"",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := StringLiteralPattern.MatchString(tt.input)
			assert.Equal(t, tt.wantMatch, matches, "StringLiteralPattern match result mismatch")
		})
	}
}

// TestNumberLiteralPattern tests numeric literal matching
func TestNumberLiteralPattern(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{
			name:      "positive integer",
			input:     "42",
			wantMatch: true,
		},
		{
			name:      "negative integer",
			input:     "-42",
			wantMatch: true,
		},
		{
			name:      "positive decimal",
			input:     "3.14",
			wantMatch: true,
		},
		{
			name:      "negative decimal",
			input:     "-3.14",
			wantMatch: true,
		},
		{
			name:      "zero",
			input:     "0",
			wantMatch: true,
		},
		{
			name:      "decimal with leading zero",
			input:     "0.5",
			wantMatch: true,
		},
		{
			name:      "not a number",
			input:     "abc",
			wantMatch: false,
		},
		{
			name:      "number with text",
			input:     "42abc",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := NumberLiteralPattern.MatchString(tt.input)
			assert.Equal(t, tt.wantMatch, matches, "NumberLiteralPattern match result mismatch")
		})
	}
}

// TestRangePattern tests numeric range pattern matching
func TestRangePattern(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMatch bool
	}{
		{
			name:      "valid range",
			input:     "1-10",
			wantMatch: true,
		},
		{
			name:      "large range",
			input:     "100-200",
			wantMatch: true,
		},
		{
			name:      "single digit range",
			input:     "0-9",
			wantMatch: true,
		},
		{
			name:      "invalid - no hyphen",
			input:     "10",
			wantMatch: false,
		},
		{
			name:      "invalid - negative numbers",
			input:     "-1-10",
			wantMatch: false,
		},
		{
			name:      "invalid - text",
			input:     "one-ten",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := RangePattern.MatchString(tt.input)
			assert.Equal(t, tt.wantMatch, matches, "RangePattern match result mismatch")
		})
	}
}
