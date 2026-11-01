//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvalsModelSchema(t *testing.T) {
	for _, tt := range []struct {
		name  string
		model any
		valid bool
	}{
		{name: "decisions model", model: "gpt-6-luna", valid: true},
		{name: "llm model", model: "small", valid: true},
		{name: "dynamic model", model: "${{ inputs.model }}", valid: true},
		{name: "non-string", model: 42},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": "workflow_dispatch",
				"evals": map[string]any{
					"model":     tt.model,
					"questions": []any{map[string]any{"id": "q", "question": "Correct?", "model": "gpt-6-luna"}},
				},
			}, "evals.md")
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestEvalsSchemaRejectsBackend(t *testing.T) {
	err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
		"on": "workflow_dispatch",
		"evals": map[string]any{
			"backend":   "auto",
			"questions": []any{map[string]any{"id": "q", "question": "Correct?"}},
		},
	}, "evals.md")
	require.ErrorContains(t, err, "backend")
}
