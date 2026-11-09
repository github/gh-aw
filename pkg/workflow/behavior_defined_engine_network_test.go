//go:build !integration

package workflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBehaviorDefinedEngineProxyProviderHostsAreNotAgentEgress(t *testing.T) {
	for _, path := range []string{
		"../../.github/workflows/shared/atomic.md",
		"../../.github/workflows/shared/factory.md",
		"../../.github/workflows/shared/mini-swe-agent.md",
		"../../.github/workflows/shared/aider.md",
		"../../.github/workflows/shared/crush.md",
		"../../.github/workflows/shared/deepseek-harness.md",
		"../../.github/workflows/shared/goose.md",
		"../../.github/workflows/shared/opencode.md",
	} {
		var definition struct {
			Engine EngineDefinition `yaml:"engine"`
		}
		readConformanceFrontmatter(t, path, &definition)
		engine, err := NewBehaviorDefinedEngine(&definition.Engine)
		require.NoError(t, err, path)
		require.NotNil(t, definition.Engine.Behaviors.Network, path)

		for provider, host := range definition.Engine.Behaviors.Network.ProviderDomains {
			t.Run(definition.Engine.ID+"/"+provider, func(t *testing.T) {
				data := &WorkflowData{
					Name:               definition.Engine.ID,
					Model:              provider + "/fixture-model",
					EngineConfig:       &EngineConfig{ID: definition.Engine.ID, Version: definition.Engine.Version},
					NetworkPermissions: &NetworkPermissions{Allowed: []string{}},
				}
				assert.NotContains(t, emittedAWFAllowedDomains(t, engine, data), host)

				data.NetworkPermissions.Allowed = []string{host}
				assert.Contains(t, emittedAWFAllowedDomains(t, engine, data), host)
			})
		}
	}

	t.Run("vendor bundle opt-in remains available", func(t *testing.T) {
		var definition struct {
			Engine EngineDefinition `yaml:"engine"`
		}
		readConformanceFrontmatter(t, "../../.github/workflows/shared/aider.md", &definition)
		engine, err := NewBehaviorDefinedEngine(&definition.Engine)
		require.NoError(t, err)
		data := &WorkflowData{
			Name:               definition.Engine.ID,
			Model:              "copilot/fixture-model",
			EngineConfig:       &EngineConfig{ID: definition.Engine.ID, Version: definition.Engine.Version},
			NetworkPermissions: &NetworkPermissions{Allowed: []string{"copilot-vendor"}},
		}
		assert.Contains(t, emittedAWFAllowedDomains(t, engine, data), "api.enterprise.githubcopilot.com")
	})
}

func TestBehaviorDefinedEngineDirectProviderDomainsRemainAvailable(t *testing.T) {
	engine, err := NewBehaviorDefinedEngine(&EngineDefinition{
		ID: "direct-provider",
		Behaviors: &EngineBehaviorDefinition{
			Network: &EngineNetworkDefinition{
				ProviderDomains: map[string]string{"openai": "api.openai.com"},
			},
			Execution: &EngineExecutionDefinition{},
		},
	})
	require.NoError(t, err)

	data := &WorkflowData{
		Model:              "openai/fixture-model",
		EngineConfig:       &EngineConfig{ID: "direct-provider"},
		NetworkPermissions: &NetworkPermissions{Allowed: []string{}},
	}
	assert.Contains(t, domainSet(t, engine.allowedDomains(data)), "api.openai.com")
}

func emittedAWFAllowedDomains(t *testing.T, engine *BehaviorDefinedEngine, data *WorkflowData) []string {
	t.Helper()
	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName:     engine.GetID(),
		WorkflowData:   data,
		AllowedDomains: engine.allowedDomains(data),
	})
	require.NoError(t, err)

	var config struct {
		Network *AWFNetworkConfig `json:"network"`
	}
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	if config.Network == nil {
		return nil
	}
	return config.Network.AllowDomains
}
