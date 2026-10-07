//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStructuredOutputFrontmatterSchema(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
		valid bool
	}{
		{"inline", map[string]any{"schema": map[string]any{"type": "object"}}, true},
		{"file", map[string]any{"schema-file": ".github/schemas/output.json"}, true},
		{"both", map[string]any{"schema": map[string]any{}, "schema-file": "schema.json"}, false},
		{"neither", map[string]any{}, false},
		{"scalar", true, false},
		{"array schema", map[string]any{"schema": []any{}}, false},
		{"empty file", map[string]any{"schema-file": ""}, false},
		{"unknown option", map[string]any{"schema": map[string]any{}, "max-retries": 3}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": "workflow_dispatch", "structured-output": tt.value,
			}, "workflow.md")
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestStructuredOutputNotImportSafe(t *testing.T) {
	err := validateSharedWorkflowFields(map[string]any{
		"structured-output": map[string]any{"schema": map[string]any{"type": "object"}},
	})
	require.ErrorContains(t, err, "only allowed in main workflows")
}
