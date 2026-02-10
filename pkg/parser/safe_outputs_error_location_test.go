//go:build !integration

package parser

import (
	"testing"
)

// TestSafeOutputsErrorLocationAtVariousDepths tests that syntax errors in safe-outputs
// are correctly reported at various nesting depths. This addresses the issue where
// nested properties under missing-tool were incorrectly pointing to parent keys.
func TestSafeOutputsErrorLocationAtVariousDepths(t *testing.T) {
	tests := []struct {
		name         string
		yamlContent  string
		jsonPath     string
		errorMessage string
		expectedLine int
		expectedCol  int
		description  string
	}{
		{
			name: "depth 1 - direct child of safe-outputs",
			yamlContent: `on: daily
safe-outputs:
  invalid-prop: true`,
			jsonPath:     "/safe-outputs",
			errorMessage: "at '/safe-outputs': additional properties 'invalid-prop' not allowed",
			expectedLine: 3,
			expectedCol:  3,
			description:  "Error at first nesting level under safe-outputs",
		},
		{
			name: "depth 2 - nested under valid handler (original bug)",
			yamlContent: `on: daily
safe-outputs:
  create-discussion:
  missing-tool:
    create-discussion: true`,
			jsonPath:     "/safe-outputs/missing-tool",
			errorMessage: "at '/safe-outputs/missing-tool': additional properties 'create-discussion' not allowed",
			expectedLine: 5,
			expectedCol:  5,
			description:  "The original bug - error should point to line 5, not line 3",
		},
		{
			name: "depth 2 - multiple invalid properties at same level",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
  invalid-handler:
    title: test
    invalid-nested: true`,
			jsonPath:     "/safe-outputs/invalid-handler",
			errorMessage: "at '/safe-outputs/invalid-handler': additional properties 'invalid-nested' not allowed",
			expectedLine: 6,
			expectedCol:  5,
			description:  "Multiple properties at depth 2, error on second one",
		},
		{
			name: "depth 3 - deeply nested invalid property",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
    labels:
      invalid-label-prop: value`,
			jsonPath:     "/safe-outputs/create-issue/labels",
			errorMessage: "at '/safe-outputs/create-issue/labels': additional properties 'invalid-label-prop' not allowed",
			expectedLine: 5,
			expectedCol:  7,
			description:  "Error at third nesting level",
		},
		{
			name: "depth 1 - error at root safe-outputs with valid children present",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
    title: Test
  unknown-prop: value`,
			jsonPath:     "/safe-outputs",
			errorMessage: "at '/safe-outputs': additional properties 'unknown-prop' not allowed",
			expectedLine: 5,
			expectedCol:  3,
			description:  "Error at root level but after valid nested structure",
		},
		{
			name: "depth 2 - error in first handler",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
    invalid-field: test
  create-discussion:
    title: Test`,
			jsonPath:     "/safe-outputs/create-issue",
			errorMessage: "at '/safe-outputs/create-issue': additional properties 'invalid-field' not allowed",
			expectedLine: 4,
			expectedCol:  5,
			description:  "Error in first handler at depth 2",
		},
		{
			name: "depth 2 - error in middle handler",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
    title: Test
  invalid-handler:
    some-prop: value
  create-discussion:
    title: Test`,
			jsonPath:     "/safe-outputs",
			errorMessage: "at '/safe-outputs': additional properties 'invalid-handler' not allowed",
			expectedLine: 5,
			expectedCol:  3,
			description:  "Error on invalid handler name between valid handlers",
		},
		{
			name: "depth 2 - error in last handler",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
    title: Test
  create-discussion:
    invalid-prop: value`,
			jsonPath:     "/safe-outputs/create-discussion",
			errorMessage: "at '/safe-outputs/create-discussion': additional properties 'invalid-prop' not allowed",
			expectedLine: 6,
			expectedCol:  5,
			description:  "Error in last handler",
		},
		{
			name: "depth 2 - multiple errors, should find first",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
  bad-handler-1:
    prop: value
  bad-handler-2:
    prop: value`,
			jsonPath:     "/safe-outputs",
			errorMessage: "at '/safe-outputs': additional properties 'bad-handler-1', 'bad-handler-2' not allowed",
			expectedLine: 4,
			expectedCol:  3,
			description:  "Multiple errors, should report first one found",
		},
		{
			name: "depth 3 - nested in github-token config",
			yamlContent: `on: daily
safe-outputs:
  github-token:
    permissions:
      invalid-perm: write`,
			jsonPath:     "/safe-outputs/github-token/permissions",
			errorMessage: "at '/safe-outputs/github-token/permissions': additional properties 'invalid-perm' not allowed",
			expectedLine: 5,
			expectedCol:  7,
			description:  "Error in deeply nested github-token permissions",
		},
		{
			name: "depth 2 - error with adjacent valid properties",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
    title: Valid Title
    invalid-field: bad
    labels: [bug]`,
			jsonPath:     "/safe-outputs/create-issue",
			errorMessage: "at '/safe-outputs/create-issue': additional properties 'invalid-field' not allowed",
			expectedLine: 5,
			expectedCol:  5,
			description:  "Error between valid properties",
		},
		{
			name: "depth 4 - very deeply nested error",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
    labels:
      - bug
      - invalid:
          nested-prop: value`,
			jsonPath:     "/safe-outputs/create-issue/labels/1",
			errorMessage: "at '/safe-outputs/create-issue/labels/1': additional properties 'nested-prop' not allowed",
			expectedLine: 7,
			expectedCol:  11,
			description:  "Very deep nesting level error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			location := LocateJSONPathInYAMLWithAdditionalProperties(tt.yamlContent, tt.jsonPath, tt.errorMessage)

			if !location.Found {
				t.Errorf("Expected to find error location, but Found=false")
				return
			}

			if location.Line != tt.expectedLine {
				t.Errorf("Expected Line=%d, got Line=%d. %s", tt.expectedLine, location.Line, tt.description)
			}

			if location.Column != tt.expectedCol {
				t.Errorf("Expected Column=%d, got Column=%d. %s", tt.expectedCol, location.Column, tt.description)
			}
		})
	}
}

// TestSafeOutputsErrorLocationWithComplexYAML tests error location in more realistic,
// complex workflow configurations
func TestSafeOutputsErrorLocationWithComplexYAML(t *testing.T) {
	tests := []struct {
		name         string
		yamlContent  string
		jsonPath     string
		errorMessage string
		expectedLine int
		expectedCol  int
		description  string
	}{
		{
			name: "complex workflow with multiple sections",
			yamlContent: `name: Complex Workflow
on:
  push:
    branches: [main]
  pull_request:
permissions:
  contents: read
  issues: write
safe-outputs:
  max: 10
  create-issue:
    title: Bug Report
  missing-tool:
    create-discussion: true
  create-discussion:
    title: Discussion`,
			jsonPath:     "/safe-outputs/missing-tool",
			errorMessage: "at '/safe-outputs/missing-tool': additional properties 'create-discussion' not allowed",
			expectedLine: 14,
			expectedCol:  5,
			description:  "Error in safe-outputs section of complex workflow",
		},
		{
			name: "safe-outputs with all valid handlers plus one invalid",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
    title: Issue
  create-discussion:
    title: Discussion
  invalid-tool:
    some-prop: value
  github-token:
    scopes: [repo]`,
			jsonPath:     "/safe-outputs",
			errorMessage: "at '/safe-outputs': additional properties 'invalid-tool' not allowed",
			expectedLine: 7,
			expectedCol:  3,
			description:  "Invalid handler among valid ones",
		},
		{
			name: "safe-outputs with comments and empty lines",
			yamlContent: `on: daily
safe-outputs:
  # Valid handler
  create-issue:
    title: Test
  
  # Invalid handler below
  bad-handler:
    prop: value`,
			jsonPath:     "/safe-outputs",
			errorMessage: "at '/safe-outputs': additional properties 'bad-handler' not allowed",
			expectedLine: 8,
			expectedCol:  3,
			description:  "Error location with comments and empty lines",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			location := LocateJSONPathInYAMLWithAdditionalProperties(tt.yamlContent, tt.jsonPath, tt.errorMessage)

			if !location.Found {
				t.Errorf("Expected to find error location, but Found=false. %s", tt.description)
				return
			}

			if location.Line != tt.expectedLine {
				t.Errorf("Expected Line=%d, got Line=%d. %s", tt.expectedLine, location.Line, tt.description)
			}

			if location.Column != tt.expectedCol {
				t.Errorf("Expected Column=%d, got Column=%d. %s", tt.expectedCol, location.Column, tt.description)
			}
		})
	}
}

// TestSafeOutputsEdgeCases tests edge cases in error location detection
func TestSafeOutputsEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		yamlContent  string
		jsonPath     string
		errorMessage string
		expectedLine int
		expectedCol  int
		shouldFind   bool
		description  string
	}{
		{
			name: "empty safe-outputs section",
			yamlContent: `on: daily
safe-outputs:`,
			jsonPath:     "/safe-outputs",
			errorMessage: "at '/safe-outputs': expected object, got null",
			expectedLine: 2,
			expectedCol:  14, // After "safe-outputs:"
			shouldFind:   true,
			description:  "Empty safe-outputs section",
		},
		{
			name: "safe-outputs with only whitespace",
			yamlContent: `on: daily
safe-outputs:
  `,
			jsonPath:     "/safe-outputs",
			errorMessage: "at '/safe-outputs': expected object, got null",
			expectedLine: 2,
			expectedCol:  14, // After "safe-outputs:"
			shouldFind:   true,
			description:  "Safe-outputs with only whitespace",
		},
		{
			name: "deeply nested with arrays",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
    labels:
      - bug
      - feature
    invalid: value`,
			jsonPath:     "/safe-outputs/create-issue",
			errorMessage: "at '/safe-outputs/create-issue': additional properties 'invalid' not allowed",
			expectedLine: 7,
			expectedCol:  5,
			shouldFind:   true,
			description:  "Error after array property",
		},
		{
			name: "non-existent path",
			yamlContent: `on: daily
safe-outputs:
  create-issue:
    title: Test`,
			jsonPath:     "/safe-outputs/nonexistent",
			errorMessage: "at '/safe-outputs/nonexistent': additional properties 'bad' not allowed",
			expectedLine: 1,
			expectedCol:  1,
			shouldFind:   false,
			description:  "Path doesn't exist in YAML",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			location := LocateJSONPathInYAMLWithAdditionalProperties(tt.yamlContent, tt.jsonPath, tt.errorMessage)

			if location.Found != tt.shouldFind {
				t.Errorf("Expected Found=%v, got Found=%v. %s", tt.shouldFind, location.Found, tt.description)
			}

			if tt.shouldFind {
				if location.Line != tt.expectedLine {
					t.Errorf("Expected Line=%d, got Line=%d. %s", tt.expectedLine, location.Line, tt.description)
				}

				if location.Column != tt.expectedCol {
					t.Errorf("Expected Column=%d, got Column=%d. %s", tt.expectedCol, location.Column, tt.description)
				}
			}
		})
	}
}
