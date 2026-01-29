//go:build !integration

package workflow

import (
	"testing"
)

func TestExtractEngineConcurrencyField(t *testing.T) {
	compiler := NewCompiler()

	tests := []struct {
		name                string
		frontmatter         map[string]any
		expectedConcurrency string
		description         string
	}{
		{
			name: "Simple string concurrency (just group name)",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id":          "claude",
					"concurrency": "custom-group-${{ github.ref }}",
				},
			},
			expectedConcurrency: "concurrency:\n  group: \"custom-group-${{ github.ref }}\"",
			description:         "String concurrency should be converted to proper YAML format",
		},
		{
			name: "Object format concurrency with group only",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "claude",
					"concurrency": map[string]any{
						"group": "custom-group",
					},
				},
			},
			expectedConcurrency: "concurrency:\n  group: \"custom-group\"",
			description:         "Object with group only should generate proper YAML",
		},
		{
			name: "Object format concurrency with group and cancel-in-progress",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "claude",
					"concurrency": map[string]any{
						"group":              "custom-group",
						"cancel-in-progress": true,
					},
				},
			},
			expectedConcurrency: "concurrency:\n  group: \"custom-group\"\n  cancel-in-progress: true",
			description:         "Object with cancel-in-progress should include it in YAML",
		},
		{
			name: "Object format concurrency with cancel-in-progress false",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "claude",
					"concurrency": map[string]any{
						"group":              "custom-group",
						"cancel-in-progress": false,
					},
				},
			},
			expectedConcurrency: "concurrency:\n  group: \"custom-group\"",
			description:         "Object with cancel-in-progress false should not include it",
		},
		{
			name: "No concurrency field",
			frontmatter: map[string]any{
				"engine": map[string]any{
					"id": "claude",
				},
			},
			expectedConcurrency: "",
			description:         "Missing concurrency field should return empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, config := compiler.ExtractEngineConfig(tt.frontmatter)

			if config == nil {
				t.Fatalf("Expected config to be non-nil")
			}

			if config.Concurrency != tt.expectedConcurrency {
				t.Errorf("ExtractEngineConfig() failed for %s\nExpected:\n%s\nGot:\n%s",
					tt.description, tt.expectedConcurrency, config.Concurrency)
			}
		})
	}
}
