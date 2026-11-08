package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/constants"
)

func TestBuildEvalsJobNeedsWithoutDetection(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "copilot",
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "q1", Question: "Does it build?"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{},
	}

	job, err := compiler.buildEvalsJob(data)
	require.NoError(t, err)
	require.NotNil(t, job)

	assert.ElementsMatch(t, []string{
		string(constants.AgentJobName),
		string(constants.ActivationJobName),
	}, job.Needs)
	assert.NotContains(t, job.Needs, string(constants.SafeOutputsJobName))
	assert.NotContains(t, job.Needs, string(constants.DetectionJobName))
	assert.Contains(t, job.If, "needs.agent.result == 'success'")
	assert.NotContains(t, job.If, "needs.safe_outputs.result")
	assert.Equal(t, "${{ steps.parse_evals_token_usage.outputs.aic }}", job.Outputs["aic"])
	assert.Contains(t, strings.Join(job.Steps, ""), "id: parse_evals_token_usage\n")
}

func TestBuildEvalsJobAuthWarning(t *testing.T) {
	for _, tc := range []struct {
		name        string
		model       string
		permissions string
		hasEvals    bool
		broker      bool
		warnings    int
	}{
		{name: "PAT with default judge", hasEvals: true, warnings: 1},
		{name: "PAT with explicit Copilot judge", model: "copilot/gpt-6-luna-decisions", hasEvals: true, warnings: 1},
		{name: "Actions token", permissions: "permissions:\n  copilot-requests: write", hasEvals: true},
		{name: "token broker", hasEvals: true, broker: true},
		{name: "OpenAI judge", model: "openai/gpt-6-luna", hasEvals: true},
		{name: "no evals"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetStrictMode(true)
			data := &WorkflowData{AI: "copilot", Permissions: tc.permissions, TokenBrokerEnabled: tc.broker, TokenBrokerCopilotRequest: tc.broker}
			if tc.hasEvals {
				data.Evals = &EvalsConfig{Model: tc.model, Questions: []EvalDefinition{{ID: "q1", Question: "Does it build?"}}}
			}
			_, err := compiler.buildEvalsJob(data)
			require.NoError(t, err)
			assert.Equal(t, tc.warnings, compiler.GetWarningCount())
		})
	}
}

func TestBuildEvalsJobNeedsWithDetection(t *testing.T) {
	compiler := NewCompiler()

	data := &WorkflowData{
		AI: "copilot",
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "q1", Question: "Does it build?"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{
			ThreatDetection: &ThreatDetectionConfig{},
		},
	}

	job, err := compiler.buildEvalsJob(data)
	require.NoError(t, err)
	require.NotNil(t, job)

	assert.ElementsMatch(t, []string{
		string(constants.AgentJobName),
		string(constants.ActivationJobName),
		string(constants.DetectionJobName),
	}, job.Needs)
	assert.NotContains(t, job.Needs, string(constants.SafeOutputsJobName))
	assert.Contains(t, job.If, "needs.agent.result == 'success'")
	assert.NotContains(t, job.If, "needs.safe_outputs.result")
	assert.Equal(t, "${{ steps.parse_evals_token_usage.outputs.aic }}", job.Outputs["aic"])
	assert.Contains(t, strings.Join(job.Steps, ""), "id: parse_evals_token_usage\n")
}

func TestBuildEvalsJobWrapperAddsJob(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "copilot",
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "q1", Question: "Does it build?"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{},
	}

	err := compiler.buildEvalsJobWrapper(data)
	require.NoError(t, err)

	job, exists := compiler.jobManager.GetJob(string(constants.EvalsJobName))
	require.True(t, exists)
	assert.Equal(t, string(constants.EvalsJobName), job.Name)
	assert.Contains(t, job.Needs, string(constants.AgentJobName))
}

func TestBuildEvalsJobRestoresSetupActionInDevMode(t *testing.T) {
	compiler := NewCompiler()
	data := &WorkflowData{
		AI: "copilot",
		Evals: &EvalsConfig{
			Questions: []EvalDefinition{
				{ID: "q1", Question: "Does it build?"},
			},
		},
		SafeOutputs: &SafeOutputsConfig{},
	}

	job, err := compiler.buildEvalsJob(data)
	require.NoError(t, err)

	steps := strings.Join(job.Steps, "")
	restoreIndex := strings.Index(steps, "- name: Restore actions folder")
	uploadIndex := strings.Index(steps, "- name: Upload evals results")
	assert.NotEqual(t, -1, restoreIndex)
	assert.Greater(t, restoreIndex, uploadIndex)
	assert.Contains(t, steps, "if: always()")
	assert.Contains(t, steps, "sparse-checkout: |\n            actions/setup")
}
