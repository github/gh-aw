//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddLabelsItemSchemaValidation(t *testing.T) {
	valid := map[string]any{
		"on": "issues",
		"safe-outputs": map[string]any{
			"add-labels": map[string]any{
				"item-schema": map[string]any{
					"type":                 "object",
					"required":             []any{"name", "confidence"},
					"additionalProperties": false,
					"properties": map[string]any{
						"name":       map[string]any{"type": "string"},
						"confidence": map[string]any{"type": "string", "enum": []any{"HIGH", "MEDIUM"}},
						"suggest":    map[string]any{"type": "boolean"},
					},
				},
			},
		},
	}
	require.NoError(t, validateWithSchema(valid, mainWorkflowSchema, "main workflow file"))

	tests := []struct {
		name       string
		itemSchema map[string]any
	}{
		{
			name:       "string items broaden the built-in schema",
			itemSchema: map[string]any{"type": "string"},
		},
		{
			name: "unknown fields are incompatible",
			itemSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"color": map[string]any{"type": "string"}},
			},
		},
		{
			name: "confidence values must remain supported",
			itemSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"confidence": map[string]any{"type": "string", "enum": []any{"CERTAIN"}}},
			},
		},
		{
			name: "additional properties cannot be enabled",
			itemSchema: map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frontmatter := map[string]any{
				"on": "issues",
				"safe-outputs": map[string]any{
					"add-labels": map[string]any{"item-schema": test.itemSchema},
				},
			}
			require.Error(t, validateWithSchema(frontmatter, mainWorkflowSchema, "main workflow file"))
		})
	}
}
