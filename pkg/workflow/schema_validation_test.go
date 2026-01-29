//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestExtractFieldPath(t *testing.T) {
	tests := []struct {
		name     string
		location []string
		expected string
	}{
		{
			name:     "single field",
			location: []string{"timeout-minutes"},
			expected: "timeout-minutes",
		},
		{
			name:     "nested field",
			location: []string{"jobs", "build", "runs-on"},
			expected: "runs-on",
		},
		{
			name:     "empty location",
			location: []string{},
			expected: "",
		},
		{
			name:     "permissions field",
			location: []string{"permissions"},
			expected: "permissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFieldPath(tt.location)
			if result != tt.expected {
				t.Errorf("extractFieldPath(%v) = %q, want %q", tt.location, result, tt.expected)
			}
		})
	}
}

func TestGetFieldExample(t *testing.T) {
	tests := []struct {
		name          string
		fieldPath     string
		errorMsg      string
		expectedParts []string // Parts that should be in the example
	}{
		{
			name:          "timeout-minutes",
			fieldPath:     "timeout-minutes",
			errorMsg:      "type error",
			expectedParts: []string{"Example:", "timeout-minutes:", "10"},
		},
		{
			name:          "engine",
			fieldPath:     "engine",
			errorMsg:      "invalid value",
			expectedParts: []string{"Valid engines are:", "copilot", "claude", "codex", "custom", "Example:"},
		},
		{
			name:          "permissions",
			fieldPath:     "permissions",
			errorMsg:      "type error",
			expectedParts: []string{"Example:", "permissions:", "contents:", "read"},
		},
		{
			name:          "runs-on",
			fieldPath:     "runs-on",
			errorMsg:      "type error",
			expectedParts: []string{"Example:", "runs-on:", "ubuntu-latest"},
		},
		{
			name:          "on trigger",
			fieldPath:     "on",
			errorMsg:      "type error",
			expectedParts: []string{"Example:", "on:"},
		},
		{
			name:          "generic integer field",
			fieldPath:     "unknown-int-field",
			errorMsg:      "expected type integer",
			expectedParts: []string{"Example:", "unknown-int-field:", "10"},
		},
		{
			name:          "generic string field",
			fieldPath:     "unknown-string-field",
			errorMsg:      "expected type string",
			expectedParts: []string{"Example:", "unknown-string-field:", "\"value\""},
		},
		{
			name:          "generic boolean field",
			fieldPath:     "unknown-bool-field",
			errorMsg:      "expected type boolean",
			expectedParts: []string{"Example:", "unknown-bool-field:", "true"},
		},
		{
			name:          "generic object field",
			fieldPath:     "unknown-object-field",
			errorMsg:      "expected type object",
			expectedParts: []string{"Example:", "unknown-object-field:", "key:", "value"},
		},
		{
			name:          "generic array field",
			fieldPath:     "unknown-array-field",
			errorMsg:      "expected type array",
			expectedParts: []string{"Example:", "unknown-array-field:", "["},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock validation error with a mock error struct
			ve := &mockValidationError{msg: tt.errorMsg}

			result := getFieldExample(tt.fieldPath, ve)

			if result == "" && len(tt.expectedParts) > 0 {
				t.Errorf("getFieldExample(%q, %q) returned empty string, want non-empty", tt.fieldPath, tt.errorMsg)
				return
			}

			for _, part := range tt.expectedParts {
				if !strings.Contains(result, part) {
					t.Errorf("getFieldExample(%q, %q) = %q, missing expected part %q",
						tt.fieldPath, tt.errorMsg, result, part)
				}
			}
		})
	}
}

func TestEnhanceSchemaValidationError(t *testing.T) {
	tests := []struct {
		name          string
		location      []string
		errorMsg      string
		expectExample bool
	}{
		{
			name:          "timeout-minutes error gets example",
			location:      []string{"timeout-minutes"},
			errorMsg:      "type mismatch",
			expectExample: true,
		},
		{
			name:          "engine error gets example",
			location:      []string{"engine"},
			errorMsg:      "invalid enum value",
			expectExample: true,
		},
		{
			name:          "permissions error gets example",
			location:      []string{"permissions"},
			errorMsg:      "type error",
			expectExample: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock validation error without using jsonschema.ValidationError
			// since it can't be properly instantiated in tests
			mockErr := &mockValidationError{msg: tt.errorMsg}

			// Instead of using enhanceSchemaValidationError (which requires a real ValidationError),
			// we test the helper functions directly
			fieldPath := extractFieldPath(tt.location)
			example := getFieldExample(fieldPath, mockErr)

			if tt.expectExample {
				if !strings.Contains(example, "Example:") {
					t.Errorf("getFieldExample() = %q, want to contain 'Example:'", example)
				}
			}
		})
	}
}

func TestValidateGitHubActionsSchemaWithExamples(t *testing.T) {
	// Note: This test requires the schema to be loaded, which happens in validateGitHubActionsSchema
	// We'll test the integration by using the actual compiler method

	compiler := NewCompiler()
	compiler.SetSkipValidation(false) // Enable validation for this test

	tests := []struct {
		name          string
		yamlContent   string
		expectError   bool
		expectedParts []string
	}{
		{
			name: "valid workflow passes",
			yamlContent: `
name: Test
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo hello
`,
			expectError: false,
		},
		// Note: We can't easily test invalid YAML here because the schema validation
		// happens after YAML parsing, and most errors would be caught by the parser first.
		// The enhancement is verified through the unit tests above.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := compiler.validateGitHubActionsSchema(tt.yamlContent)

			if tt.expectError && err == nil {
				t.Error("validateGitHubActionsSchema() expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("validateGitHubActionsSchema() unexpected error: %v", err)
			}

			if err != nil {
				errStr := err.Error()
				for _, part := range tt.expectedParts {
					if !strings.Contains(errStr, part) {
						t.Errorf("error %q missing expected part %q", errStr, part)
					}
				}
			}
		})
	}
}
