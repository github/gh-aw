//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"strings"
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
			location := LocateJSONPathForPathInfo(tt.yamlContent, JSONPathInfo{Path: tt.jsonPath, Message: tt.errorMessage})

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
			location := LocateJSONPathForPathInfo(tt.yamlContent, JSONPathInfo{Path: tt.jsonPath, Message: tt.errorMessage})

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
			location := LocateJSONPathForPathInfo(tt.yamlContent, JSONPathInfo{Path: tt.jsonPath, Message: tt.errorMessage})

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

func TestValidateWithSchemaAndLocationReportsAllSafeOutputFailures(t *testing.T) {
	t.Parallel()

	yamlContent := `---
on: daily
safe-outputs:
  create-issue:
    invalid-issue-field: true
  create-discussion:
    invalid-discussion-field: true
---
# body`
	filePath := filepath.Join(t.TempDir(), "workflow.md")
	if err := os.WriteFile(filePath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	frontmatter := map[string]any{
		"on": "daily",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{
				"invalid-issue-field": true,
			},
			"create-discussion": map[string]any{
				"invalid-discussion-field": true,
			},
		},
	}

	err := validateWithSchemaAndLocation(frontmatter, mainWorkflowSchema, "main workflow file", filePath)
	if err == nil {
		t.Fatal("expected schema validation error, got nil")
	}

	errorText := err.Error()
	// The path is shown without a leading '/'; line:col info appears both in the
	// file:line:col prefix from console.FormatErrorStdout and in each detail line.
	wantSubstrings := []string{
		"safe-outputs/create-issue",
		"safe-outputs/create-discussion",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(errorText, want) {
			t.Fatalf("expected error to contain %q, got:\n%s", want, errorText)
		}
	}
}

// TestFormatSchemaFailureDetailEmptyPath verifies that an empty path returns the message
// without any path prefix (the root path "/" is stripped from the display).
func TestFormatSchemaFailureDetailEmptyPath(t *testing.T) {
	t.Parallel()

	pathInfo := JSONPathInfo{
		Path:    "",
		Message: "additional property 'x' not allowed",
	}
	result := formatSchemaFailureDetail(pathInfo, "", "on: daily\n", 1)
	// Empty/root paths return just the message without a path prefix
	if strings.HasPrefix(result, "at '/'") {
		t.Errorf("result should not start with \"at '/'\", got: %s", result)
	}
	if !strings.Contains(result, "Unknown property") {
		t.Errorf("expected result to contain error message, got: %s", result)
	}
}

// TestFormatSchemaFailureDetailLineColumn verifies that line/column numbers are
// included in the formatted detail when the path can be located in YAML, so that
// secondary failures in multi-failure output retain their location context.
func TestFormatSchemaFailureDetailLineColumn(t *testing.T) {
	t.Parallel()

	frontmatterContent := "on: daily\nsafe-outputs:\n  create-issue:\n    invalid-field: true\n"
	pathInfo := JSONPathInfo{
		Path:    "/safe-outputs/create-issue",
		Message: "additional property 'invalid-field' not allowed",
	}
	result := formatSchemaFailureDetail(pathInfo, "", frontmatterContent, 1)
	// Line/column info should be included in each detail line for secondary failures.
	if !strings.Contains(result, "line ") || !strings.Contains(result, "col ") {
		t.Errorf("expected result to contain line/col info, got: %s", result)
	}
	// The field path should still appear (without the leading '/').
	if !strings.Contains(result, "safe-outputs/create-issue") {
		t.Errorf("expected result to contain path 'safe-outputs/create-issue', got: %s", result)
	}
}

// TestValidateWithSchemaAndLocationSingleFailureNoBulletPrefix verifies that when
// only one schema failure occurs the error message does not include the
// "Multiple schema validation failures" prefix.
func TestValidateWithSchemaAndLocationSingleFailureNoBulletPrefix(t *testing.T) {
	t.Parallel()

	yamlContent := `---
on: daily
safe-outputs:
  create-issue:
    invalid-single-field: true
---
# body`
	filePath := filepath.Join(t.TempDir(), "workflow.md")
	if err := os.WriteFile(filePath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	frontmatter := map[string]any{
		"on": "daily",
		"safe-outputs": map[string]any{
			"create-issue": map[string]any{
				"invalid-single-field": true,
			},
		},
	}

	err := validateWithSchemaAndLocation(frontmatter, mainWorkflowSchema, "main workflow file", filePath)
	if err == nil {
		t.Fatal("expected schema validation error, got nil")
	}

	errorText := err.Error()
	if strings.Contains(errorText, "Multiple schema validation failures") {
		t.Errorf("single failure should not use 'Multiple schema validation failures' prefix; got:\n%s", errorText)
	}
}

// TestStripDetailLinePrefix verifies that stripDetailLinePrefix removes the
// "'path' (line N, col M): " prefix added by formatSchemaFailureDetail.
func TestStripDetailLinePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips path-position prefix",
			input:    "'engine' (line 2, col 1): value must be one of 'claude', 'codex'",
			expected: "value must be one of 'claude', 'codex'",
		},
		{
			name:     "strips nested path prefix",
			input:    "'safe-outputs/create-issue' (line 5, col 3): Unknown property: bad-field",
			expected: "Unknown property: bad-field",
		},
		{
			name:     "leaves plain message unchanged (no prefix)",
			input:    "value must be one of 'read', 'write', 'none'",
			expected: "value must be one of 'read', 'write', 'none'",
		},
		{
			name:     "leaves multi-failure message unchanged",
			input:    "Multiple schema validation failures:\n- 'engine' (line 2, col 1): bad value",
			expected: "Multiple schema validation failures:\n- 'engine' (line 2, col 1): bad value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := stripDetailLinePrefix(tt.input)
			if result != tt.expected {
				t.Errorf("stripDetailLinePrefix(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestFormatSchemaFailureDetailNoDuplicateSuggestions verifies that when
// appendKnownFieldValidValuesHint already adds a "Valid …" list (e.g. for
// permissions paths), generateSchemaBasedSuggestions does not append another
// duplicate list to the same message.
func TestFormatSchemaFailureDetailNoDuplicateSuggestions(t *testing.T) {
	t.Parallel()

	// A permissions path triggers appendKnownFieldValidValuesHint to add
	// "Valid permission scopes: …".  Without the guard introduced by the fix,
	// generateSchemaBasedSuggestions would also append "Valid fields are: …",
	// resulting in the permission list being shown twice.
	pathInfo := JSONPathInfo{
		Path:    "/permissions/completely-unknown-xyz",
		Message: "additional properties not allowed",
	}
	result := formatSchemaFailureDetail(pathInfo, mainWorkflowSchema, "on: daily\npermissions:\n  completely-unknown-xyz: read\n", 2)

	// Count occurrences of "Valid " — there must be at most one.
	count := strings.Count(result, "Valid ")
	if count > 1 {
		t.Errorf("formatSchemaFailureDetail should not repeat 'Valid ' more than once; got %d occurrences in:\n%s", count, result)
	}
}

// TestValidateWithSchemaAndLocationSingleFailureStripsPathPrefix verifies that
// for a single schema validation failure the "'path' (line N, col M):" prefix is
// stripped from the error message (it is redundant because the IDE-format header
// already encodes the position).
func TestValidateWithSchemaAndLocationSingleFailureStripsPathPrefix(t *testing.T) {
	t.Parallel()

	// Use an unknown top-level field that is guaranteed to fail with a single
	// additional-properties error.
	yamlContent := `---
on: daily
completely-unknown-field-xyz: true
---
# body`
	filePath := filepath.Join(t.TempDir(), "workflow.md")
	if err := os.WriteFile(filePath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	frontmatter := map[string]any{
		"on":                           "daily",
		"completely-unknown-field-xyz": true,
	}

	err := validateWithSchemaAndLocation(frontmatter, mainWorkflowSchema, "main workflow file", filePath)
	if err == nil {
		t.Fatal("expected schema validation error, got nil")
	}

	errorText := err.Error()
	// The "'completely-unknown-field-xyz' (line N, col M):" prefix must NOT appear
	// for a single failure — the IDE header already encodes position.
	if strings.Contains(errorText, "(line ") && strings.Contains(errorText, ", col ") {
		// Only fail if the path+position prefix appears BEFORE the message content,
		// which indicates it hasn't been stripped.
		if strings.Contains(errorText, "'completely-unknown-field-xyz' (line") {
			t.Errorf("single failure message should not contain the path+position prefix; got:\n%s", errorText)
		}
	}
	// The error must still describe the problem.
	if !strings.Contains(errorText, "completely-unknown-field-xyz") {
		t.Errorf("error message should identify the invalid field; got:\n%s", errorText)
	}
}
