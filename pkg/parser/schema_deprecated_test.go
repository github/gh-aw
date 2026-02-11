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

	// Check that timeout_minutes is NOT in the list (it has been removed from the schema)
	// This field was fully removed per https://github.com/github/gh-aw/issues/14736
	for _, field := range deprecatedFields {
		if field.Name == "timeout_minutes" {
			t.Errorf("timeout_minutes should not be in the deprecated fields list - it has been completely removed from the schema")
		}
	}

	// The list can be empty or contain other deprecated fields, but timeout_minutes should not be present
	t.Logf("Found %d deprecated fields in schema (timeout_minutes correctly removed)", len(deprecatedFields))
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
