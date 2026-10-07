package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailedJobsDisplayNames(t *testing.T) {
	names := buildFailedJobsDisplayNames(&WorkflowData{
		Jobs: map[string]any{
			"agent":   map[string]any{"name": "Renamed agent"},
			"build":   map[string]any{"name": "Build project"},
			"dynamic": map[string]any{"name": "Build ${{ inputs.name }}"},
			"unnamed": map[string]any{},
		},
		SafeOutputs: &SafeOutputsConfig{
			Jobs: map[string]*SafeJobConfig{
				"send-report": {Name: "Send report"},
				"dynamic":     {Name: "${{ inputs.name }}"},
				"unnamed":     {},
			},
		},
	})
	require.Equal(t, "Renamed agent", names["agent"])
	require.Equal(t, "Build project", names["build"])
	require.Equal(t, "Send report", names["send_report"])
	require.Equal(t, "Push repository memory", names["push_repo_memory"])
	require.NotContains(t, names, "unnamed")
	require.NotContains(t, names, "dynamic", "runtime expressions must not be interpolated into serialized JSON")
	require.Equal(t, "Agent", generatedJobNames["agent"], "overrides must not mutate the shared compiler mapping")
}
