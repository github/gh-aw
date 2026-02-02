//go:build !integration

package parser

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestFilterIgnoredFields(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		expected    map[string]any
	}{
		{
			name:        "nil frontmatter",
			frontmatter: nil,
			expected:    nil,
		},
		{
			name:        "empty frontmatter",
			frontmatter: map[string]any{},
			expected:    map[string]any{},
		},
		{
			name: "frontmatter with description - no longer filtered",
			frontmatter: map[string]any{
				"description": "This is a test workflow",
				"on":          "push",
			},
			expected: map[string]any{
				"description": "This is a test workflow",
				"on":          "push",
			},
		},
		{
			name: "frontmatter with applyTo - no longer filtered",
			frontmatter: map[string]any{
				"applyTo": "some-value",
				"on":      "push",
			},
			expected: map[string]any{
				"applyTo": "some-value",
				"on":      "push",
			},
		},
		{
			name: "frontmatter with both description and applyTo - no longer filtered",
			frontmatter: map[string]any{
				"description": "This is a test workflow",
				"applyTo":     "some-value",
				"on":          "push",
				"engine":      "claude",
			},
			expected: map[string]any{
				"description": "This is a test workflow",
				"applyTo":     "some-value",
				"on":          "push",
				"engine":      "claude",
			},
		},
		{
			name: "frontmatter with only valid fields",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "claude",
			},
			expected: map[string]any{
				"on":     "push",
				"engine": "claude",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterIgnoredFields(tt.frontmatter)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d fields, got %d fields", len(tt.expected), len(result))
			}

			for key, expectedValue := range tt.expected {
				if actualValue, ok := result[key]; !ok {
					t.Errorf("Expected field %q not found in result", key)
				} else {
					// For simple types, compare directly
					// For maps, we need to compare keys (simple check for this test)
					switch v := expectedValue.(type) {
					case map[string]any:
						if actualMap, ok := actualValue.(map[string]any); !ok {
							t.Errorf("Field %q: expected map, got %T", key, actualValue)
						} else if len(actualMap) != len(v) {
							t.Errorf("Field %q: expected map with %d keys, got %d keys", key, len(v), len(actualMap))
						}
					default:
						if actualValue != expectedValue {
							t.Errorf("Field %q: expected %v, got %v", key, expectedValue, actualValue)
						}
					}
				}
			}

			// Check that ignored fields are not present
			for _, ignoredField := range constants.IgnoredFrontmatterFields {
				if _, ok := result[ignoredField]; ok {
					t.Errorf("Ignored field %q should not be present in result", ignoredField)
				}
			}
		})
	}
}

func TestValidateMainWorkflowWithIgnoredFields(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		wantErr     bool
		errContains string
	}{
		{
			name: "valid frontmatter with description field - now properly validated",
			frontmatter: map[string]any{
				"on":          "push",
				"description": "This is a test workflow description",
				"engine":      "claude",
			},
			wantErr: false,
		},
		{
			name: "invalid frontmatter with applyTo field - not allowed in main workflow",
			frontmatter: map[string]any{
				"on":      "push",
				"applyTo": "some-target",
				"engine":  "claude",
			},
			wantErr:     true,
			errContains: "applyTo",
		},
		{
			name: "valid frontmatter with description - now properly validated",
			frontmatter: map[string]any{
				"on":          "push",
				"description": "Test workflow",
				"engine":      "claude",
			},
			wantErr: false,
		},
		{
			name: "invalid frontmatter with ignored fields - other validation should still work",
			frontmatter: map[string]any{
				"on":            "push",
				"description":   "Test workflow",
				"applyTo":       "some-target",
				"invalid_field": "should-fail",
			},
			wantErr:     true,
			errContains: "invalid_field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchema(tt.frontmatter)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMainWorkflowFrontmatterWithSchema() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error message should contain %q, got: %v", tt.errContains, err)
				}
			}
		})
	}
}

func TestValidateIncludedFileWithIgnoredFields(t *testing.T) {
	tests := []struct {
		name        string
		frontmatter map[string]any
		wantErr     bool
		errContains string
	}{
		{
			name: "valid included file with applyTo - should fail (not in schema yet)",
			frontmatter: map[string]any{
				"applyTo": "some-target",
				"engine":  "claude",
			},
			wantErr:     true,
			errContains: "applyTo",
		},
		{
			name: "valid included file with description - should pass",
			frontmatter: map[string]any{
				"description": "This is a test description",
				"engine":      "claude",
			},
			wantErr: false,
		},
		{
			name: "invalid included file with 'on' field - should fail",
			frontmatter: map[string]any{
				"on":     "push",
				"engine": "claude",
			},
			wantErr:     true,
			errContains: "on",
		},
		{
			name: "invalid included file with invalid field - should fail",
			frontmatter: map[string]any{
				"invalid_field": "should-fail",
				"engine":        "claude",
			},
			wantErr:     true,
			errContains: "invalid_field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIncludedFileFrontmatterWithSchema(tt.frontmatter)

			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIncludedFileFrontmatterWithSchema() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.errContains != "" {
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error message should contain %q, got: %v", tt.errContains, err)
				}
			}
		})
	}
}
