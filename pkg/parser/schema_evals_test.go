//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvalsBackendSchema(t *testing.T) {
	for _, tt := range []struct {
		name    string
		backend any
		valid   bool
	}{
		{name: "auto", backend: "auto", valid: true},
		{name: "llm", backend: "llm", valid: true},
		{name: "decisions", backend: "decisions", valid: true},
		{name: "unknown", backend: "other"},
		{name: "empty", backend: ""},
		{name: "non-string", backend: 42},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": "workflow_dispatch",
				"evals": map[string]any{
					"backend":   tt.backend,
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
