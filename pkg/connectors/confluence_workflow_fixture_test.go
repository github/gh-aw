package connectors

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// This checks provider fixture intent, not compiler support for the new grammar.
func TestConfluenceConnectorGroupedWorkflowFixtures(t *testing.T) {
	for _, file := range []string{"workflow.yaml", "workflow-ingress-unsupported.yaml"} {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile("testdata/providers/confluence/" + file)
			require.NoError(t, err)
			var workflow map[string]any
			require.NoError(t, Decode(data, &workflow))
			require.Equal(t, "workflow_dispatch", workflow["on"])
			require.NotContains(t, workflow, "tools")
			require.NotContains(t, workflow, "safe-outputs")
			grouped, ok := workflow["connectors"].(map[string]any)
			require.True(t, ok)
			provider, ok := grouped["confluence"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, ".github/aw/connectors/confluence.yaml", provider["definition"])
			require.Equal(t, "pkg/connectors/testdata/providers/confluence/connection.yaml", provider["connection"])
			plan := confluenceFixturePlan(t, "")
			if file == "workflow-ingress-unsupported.yaml" {
				require.Equal(t, []any{"page-created"}, provider["on"])
				require.NotContains(t, provider, "tools")
				require.NotContains(t, provider, "context")
				require.NotContains(t, provider, "safe-outputs")
				require.ErrorIs(t, plan.ActivateTriggers(), ErrIngressUnavailable)
				return
			}
			require.NotContains(t, provider, "on")
			require.Equal(t, []any{"get-page"}, provider["tools"])
			require.Equal(t, []any{"add-review-comment"}, provider["safe-outputs"])
			recipes, ok := provider["context"].(map[string]any)
			require.True(t, ok)
			args, ok := recipes["runbook"].(map[string]any)
			require.True(t, ok)
			require.Equal(t, confluenceReadArgs(), args)
			require.NoError(t, validateArguments(plan.spec.Context["runbook"].Inputs,
				plan.spec.Context["runbook"].Resources, plan.connection.Resources, args))
		})
	}
}

func TestConfluenceConnectorIndependentSelections(t *testing.T) {
	data, err := os.ReadFile("testdata/providers/confluence/binding.yaml")
	require.NoError(t, err)
	for _, capability := range []string{"mcp", "context", "safe-output", "trigger"} {
		t.Run(capability, func(t *testing.T) {
			var all Binding
			require.NoError(t, Decode(data, &all))
			binding := Binding{APIVersion: all.APIVersion, Kind: all.Kind, Definition: all.Definition, Connection: all.Connection}
			switch capability {
			case "mcp":
				binding.MCP = all.MCP
			case "context":
				binding.Context = all.Context
			case "safe-output":
				binding.SafeOutputs = all.SafeOutputs
			case "trigger":
				binding.Triggers = all.Triggers
			}
			plan, err := Lower("../..", binding)
			require.NoError(t, err)
			require.Equal(t, binding, plan.binding)
			if capability == "trigger" {
				require.ErrorIs(t, plan.ActivateTriggers(), ErrIngressUnavailable)
			}
		})
	}
}
