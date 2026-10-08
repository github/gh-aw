//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestExperimentalSubagentModelPatterns(t *testing.T) {
	data := &WorkflowData{
		Experiments: map[string][]string{"model": {"small", "large", "small"}},
		ModelMappings: map[string][]string{
			"small":  {"copilot/*mini*", "nested"},
			"nested": {"copilot/*haiku*"},
			"large":  {"copilot/gpt-5.4"},
		},
	}
	patterns := []string{"github-copilot/*mini*", "github-copilot/*haiku*", "github-copilot/gpt-5.4"}
	for _, request := range []string{
		"${{ experiments.model }}",
		"${{ needs.activation.outputs.model }}",
		"${{ steps.pick-experiment.outputs.model }}",
		"${{ experiments.model }}?effort=high",
	} {
		t.Run(request, func(t *testing.T) {
			require.Equal(t, patterns, expandSubagentModelPatterns(request, data, "github-copilot"))
		})
	}
	require.Nil(t, expandSubagentModelPatterns("${{ experiments.unknown }}", data, "github-copilot"))
	require.Nil(t, expandSubagentModelPatterns("${{ inputs.model }}", data, "github-copilot"))
	require.Equal(t, []string{"openai/gpt-5.4"}, expandSubagentModelPatterns("gpt-5.4", data, "openai"))
}

func TestValidateExperimentalSubagentModels(t *testing.T) {
	for _, model := range []string{
		"small",
		"${{ experiments.model }}",
		"${{ experiments.model }}?effort=high",
		"${{ format('experiments.model-{0}', inputs.suffix) }}",
	} {
		t.Run(model, func(t *testing.T) {
			require.NoError(t, validateExperimentalSubagentModels(&WorkflowData{
				Experiments:    map[string][]string{"model": {"small", "large"}},
				SubAgentModels: []parser.SubAgentModel{{Name: "reader", Model: model}},
			}))
		})
	}
}

func TestExperimentalSubagentRoutingPolicy(t *testing.T) {
	data := &WorkflowData{
		Experiments: map[string][]string{"model": {"small", "large"}},
		ModelMappings: map[string][]string{
			"small": {"copilot/*mini*"},
			"large": {"copilot/gpt-5.4"},
		},
		SubAgentModels: []parser.SubAgentModel{{Name: "reader", Model: "${{ experiments.model }}"}},
	}
	candidates := []string{"github-copilot/gpt-6-sol"}
	allowed, warnings := subAgentRequestModels(data, candidates, nil, []string{"gpt-5.4"})
	require.Empty(t, warnings)
	require.Equal(t, []string{"github-copilot/gpt-6-sol", "github-copilot/*mini*"}, allowed)
	allowed, warnings = subAgentRequestModels(data, candidates, []string{"gpt-6-sol", "gpt-5-mini"}, nil)
	require.Empty(t, warnings)
	require.Equal(t, []string{"github-copilot/gpt-6-sol", "github-copilot/gpt-5-mini"}, allowed)
	require.Equal(t, []string{"github-copilot/gpt-6-sol"}, candidates, "sub-agents must not change routing candidates")
	allowed, warnings = subAgentRequestModels(data, candidates, []string{"gpt-6-sol"}, nil)
	require.Equal(t, candidates, allowed)
	require.Len(t, warnings, 1, "excluded experiment alternatives must retain the existing admission warning")
}
