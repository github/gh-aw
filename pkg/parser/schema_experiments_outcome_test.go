package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExperimentOutcomeSchema(t *testing.T) {
	for _, tt := range []struct {
		name   string
		window any
		valid  bool
	}{
		{"default window", nil, true},
		{"automatic window", "auto", true},
		{"explicit window", "72h", true},
		{"compound duration", "1h30m", true},
		{"empty window", "", false},
		{"numeric window", 72, false},
		{"boolean window", true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			experiment := map[string]any{
				"variants":          []string{"control", "candidate"},
				"metric":            "outcome:accepted",
				"secondary_metrics": []string{"outcome:zero_touch"},
				"guardrail_metrics": []any{map[string]any{"name": "outcome:accepted", "threshold": ">=0.9"}},
			}
			if tt.window != nil {
				experiment["outcome_window"] = tt.window
			}
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on":          "workflow_dispatch",
				"experiments": map[string]any{"quality": experiment},
			}, "workflow.md")
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
