//go:build !integration

package cli

import (
	"testing"

	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildActiveModelInventoryDoesNothingWithoutObservedData(t *testing.T) {
	t.Parallel()

	assert.Nil(t, buildActiveModelInventory(modelsReport{
		Catalog: []modelCatalogRow{{Provider: "openai", Model: "gpt-5.4"}},
		Aliases: []modelAliasRow{{Alias: "large"}},
	}))
}

func TestFindUnknownConfiguredModels(t *testing.T) {
	t.Parallel()

	inventory := buildActiveModelInventory(modelsReport{
		Aliases: []modelAliasRow{{Alias: "large"}},
		Observed: []observedModelRow{
			{Provider: "github-copilot", Model: "gpt-5.4"},
			{Provider: "anthropic", Model: "claude-sonnet-4.6"},
		},
	})
	require.NotNil(t, inventory)

	data := &workflow.WorkflowData{
		ModelMappings: map[string][]string{"custom-alias": {"gpt-5.4"}},
		ModelPolicyAllowed: []string{
			"gpt-5.4",
			"copilot/gpt-5.*",
			"large",
			"custom-alias",
			"missing-policy-model",
		},
		ModelPolicyBlocked: []string{"anthropic/claude-sonnet-4.6"},
		RawFrontmatter: map[string]any{
			"engine": map[string]any{
				"models": map[string]any{
					"default":   "large",
					"supported": []any{"gpt-5.4?effort=high", "${{ inputs.model }}", "missing-engine-model"},
				},
			},
		},
	}

	warnings := findUnknownConfiguredModels(data, inventory)
	require.Len(t, warnings, 2)
	assert.Equal(t, "unknown_model", warnings[0].Type)
	assert.Contains(t, warnings[0].Message, "missing-engine-model")
	assert.Contains(t, warnings[0].Message, "engine.models.supported")
	assert.Contains(t, warnings[1].Message, "missing-policy-model")
	assert.Contains(t, warnings[1].Message, "models.allowed")
}

func TestFindUnknownConfiguredModelsSkipsMissingInventory(t *testing.T) {
	t.Parallel()

	data := &workflow.WorkflowData{
		ModelPolicyAllowed: []string{"unknown"},
	}
	assert.Empty(t, findUnknownConfiguredModels(data, nil))
}
