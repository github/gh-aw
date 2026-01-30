//go:build !integration

package parser

import (
	"testing"
)

func TestGetMainWorkflowDeprecatedFields(t *testing.T) {
	deprecatedFields, err := GetMainWorkflowDeprecatedFields()
	if err != nil {
		t.Fatalf("GetMainWorkflowDeprecatedFields() error = %v", err)
	}

	// Check that timeout_minutes IS in the list as a deprecated field
	// This allows strict mode to properly detect and reject it
	found := false
	var timeoutMinutesField *DeprecatedField
	for _, field := range deprecatedFields {
		if field.Name == "timeout_minutes" {
			found = true
			timeoutMinutesField = &field
			break
		}
	}

	if !found {
		t.Error("timeout_minutes should be in the deprecated fields list to support strict mode validation")
	} else {
		// Verify it has the correct replacement
		if timeoutMinutesField.Replacement != "timeout-minutes" {
			t.Errorf("timeout_minutes replacement = %v, want 'timeout-minutes'", timeoutMinutesField.Replacement)
		}
	}
}

func TestFindDeprecatedFieldsInFrontmatter(t *testing.T) {
	deprecatedFields := []DeprecatedField{
		{
			Name:        "timeout_minutes",
			Replacement: "timeout-minutes",
			Description: "Deprecated: Use 'timeout-minutes' instead",
		},
		{
			Name:        "old_field",
			Replacement: "new_field",
			Description: "Deprecated: Use 'new_field' instead",
		},
	}

	tests := []struct {
		name        string
		frontmatter map[string]any
		want        []string // field names that should be found
	}{
		{
			name: "no deprecated fields",
			frontmatter: map[string]any{
				"timeout-minutes": 10,
				"engine":          "copilot",
			},
			want: []string{},
		},
		{
			name: "one deprecated field",
			frontmatter: map[string]any{
				"timeout_minutes": 10,
				"engine":          "copilot",
			},
			want: []string{"timeout_minutes"},
		},
		{
			name: "multiple deprecated fields",
			frontmatter: map[string]any{
				"timeout_minutes": 10,
				"old_field":       "value",
				"engine":          "copilot",
			},
			want: []string{"timeout_minutes", "old_field"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := FindDeprecatedFieldsInFrontmatter(tt.frontmatter, deprecatedFields)

			if len(found) != len(tt.want) {
				t.Errorf("FindDeprecatedFieldsInFrontmatter() found %d fields, want %d", len(found), len(tt.want))
			}

			// Check that all expected fields were found
			foundMap := make(map[string]bool)
			for _, field := range found {
				foundMap[field.Name] = true
			}

			for _, wantField := range tt.want {
				if !foundMap[wantField] {
					t.Errorf("Expected to find deprecated field '%s', but it was not found", wantField)
				}
			}
		})
	}
}

func TestExtractReplacementFromDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        string
	}{
		{
			name:        "single quote pattern",
			description: "Deprecated: Use 'timeout-minutes' instead.",
			want:        "timeout-minutes",
		},
		{
			name:        "double quote pattern",
			description: "Deprecated: Use \"timeout-minutes\" instead.",
			want:        "timeout-minutes",
		},
		{
			name:        "backtick pattern",
			description: "Deprecated: Use `timeout-minutes` instead.",
			want:        "timeout-minutes",
		},
		{
			name:        "replaced with pattern",
			description: "This field is replaced with 'new-field'.",
			want:        "new-field",
		},
		{
			name:        "no replacement pattern",
			description: "This field is deprecated.",
			want:        "",
		},
		{
			name:        "complex description with replacement",
			description: "This is a long description explaining why this field is deprecated. Use 'new-field' instead for better compatibility.",
			want:        "new-field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractReplacementFromDescription(tt.description)
			if got != tt.want {
				t.Errorf("extractReplacementFromDescription() = %v, want %v", got, tt.want)
			}
		})
	}
}
