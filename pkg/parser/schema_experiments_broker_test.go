package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExperimentsBrokerStorageSchema(t *testing.T) {
	for _, mode := range []string{"repo", "cache", "broker", "https://broker.example"} {
		t.Run(mode, func(t *testing.T) {
			err := ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
				"on": "workflow_dispatch",
				"experiments": map[string]any{
					"storage": mode,
					"prompt":  []string{"control", "candidate"},
				},
			}, "workflow.md")
			if mode == "https://broker.example" {
				require.Error(t, err, "an authored broker endpoint is not a storage mode")
			} else {
				require.NoError(t, err)
			}
		})
	}
	require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
		"on": "workflow_dispatch",
		"experiments": map[string]any{
			"storage":    "broker",
			"broker_url": "https://broker.example",
		},
	}, "workflow.md"), "broker endpoints must come from the managed secret, not frontmatter")
}
