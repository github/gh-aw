package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validModelRoutingData() *WorkflowData {
	return &WorkflowData{
		EngineConfig: &EngineConfig{
			ID: "copilot",
			ModelRouting: &ModelRoutingConfig{
				Goal:          "cost",
				Mode:          "balanced",
				AllowedModels: []string{"gpt-5.4-mini"},
			},
		},
		SandboxConfig: &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf"}},
	}
}

func TestValidateEngineModelRouting(t *testing.T) {
	t.Run("accepts supported Copilot routing configuration", func(t *testing.T) {
		require.NoError(t, validateEngineModelRouting(validModelRoutingData()))
	})

	t.Run("rejects firewall versions below the routing minimum", func(t *testing.T) {
		data := validModelRoutingData()
		data.SandboxConfig.Agent.Version = "v0.28.28"
		err := validateEngineModelRouting(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires AWF v0.28.29 or newer")
	})

	t.Run("rejects non-Copilot models", func(t *testing.T) {
		data := validModelRoutingData()
		data.EngineConfig.ModelRouting.AllowedModels = []string{"openai/gpt-5.4-mini"}
		err := validateEngineModelRouting(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only supports Copilot models")
	})

	t.Run("rejects Docker socket exposure", func(t *testing.T) {
		data := validModelRoutingData()
		data.SandboxConfig.Agent.Mounts = []string{"/var/run/docker.sock:/var/run/docker.sock"}
		err := validateEngineModelRouting(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "incompatible with this AWF runtime configuration")
	})
}

func TestApplyEngineModelRouting(t *testing.T) {
	config := &EngineConfig{}
	applyEngineModelRouting(config, map[string]any{
		"model-routing": map[string]any{
			"goal":           "cost-speed",
			"mode":           "robust",
			"allowed-models": []any{"claude-haiku-4.5", "gpt-5.4-mini"},
		},
	})
	require.NotNil(t, config.ModelRouting)
	assert.Equal(t, "cost-speed", config.ModelRouting.Goal)
	assert.Equal(t, "robust", config.ModelRouting.Mode)
	assert.Equal(t, []string{"claude-haiku-4.5", "gpt-5.4-mini"}, config.ModelRouting.AllowedModels)
}
