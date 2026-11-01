package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/github/gh-aw/pkg/constants"
)

func TestBuildEvalsJobKeepsRuntimeInferenceFallback(t *testing.T) {
	data := &WorkflowData{
		AI:                 "copilot",
		EngineConfig:       &EngineConfig{ID: "copilot", Auth: &EngineAuthConfig{Type: "github-oidc"}},
		TokenBrokerEnabled: true,
		Evals:              &EvalsConfig{Model: "gpt-6-luna", Questions: []EvalDefinition{{ID: "q", Question: "Correct?"}}},
	}
	job, err := NewCompiler().buildEvalsJob(data)
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.NotContains(t, job.Permissions, "copilot-requests")
	assert.Contains(t, job.Permissions, "id-token")
	assert.Contains(t, job.Permissions, "contents: read")
	steps := strings.Join(job.Steps, "")
	assert.Contains(t, steps, "evals_agentic_execution")
	assert.Contains(t, steps, "parse-mcp-gateway")
	assert.Contains(t, steps, "steps.evals_setup.outputs.run_llm == 'true'")
	assert.Contains(t, steps, "Parse BinEval results")
}

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
	assert.Equal(t, "${{ steps.parse-mcp-gateway.outputs.aic }}", job.Outputs["aic"])
	assert.Contains(t, strings.Join(job.Steps, ""), "id: parse-mcp-gateway\n")
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
	assert.Equal(t, "${{ steps.parse-mcp-gateway.outputs.aic }}", job.Outputs["aic"])
	assert.Contains(t, strings.Join(job.Steps, ""), "id: parse-mcp-gateway\n")
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
