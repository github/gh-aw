//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScriptModeUnlockCleanupCarriesGenAIAttribution(t *testing.T) {
	findCleanupStep := func(t *testing.T, engine *EngineConfig) string {
		t.Helper()
		compiler := NewCompiler(WithVersion("1.0.0"))
		compiler.SetActionMode(ActionModeScript)
		job, err := compiler.buildUnlockJob(&WorkflowData{Name: "Test", LockForAgent: true, EngineConfig: engine}, false)
		require.NoError(t, err)
		require.NotNil(t, job)
		for _, step := range job.Steps {
			if strings.Contains(step, "name: Clean Scripts") {
				return step
			}
		}
		require.FailNow(t, "unlock job has no Clean Scripts step")
		return ""
	}

	byok := findCleanupStep(t, &EngineConfig{ID: "copilot", Env: map[string]string{"COPILOT_PROVIDER_BASE_URL": "https://example.ai.gateway.azure.com/default/models/openai/v1"}})
	assert.Contains(t, byok, `GH_AW_INFO_ENGINE_ID: "copilot"`)
	assert.Contains(t, byok, `GH_AW_INFO_GENAI_PROVIDER: "azure.ai.openai"`)
	assert.Contains(t, byok, `GH_AW_INFO_GENAI_ENDPOINT: "https://example.ai.gateway.azure.com/default/models/openai/v1"`)

	github := findCleanupStep(t, &EngineConfig{ID: "copilot"})
	assert.Contains(t, github, `GH_AW_INFO_ENGINE_ID: "copilot"`)
	assert.NotContains(t, github, "GH_AW_INFO_GENAI_PROVIDER")
	assert.NotContains(t, github, "GH_AW_INFO_GENAI_ENDPOINT")
}
