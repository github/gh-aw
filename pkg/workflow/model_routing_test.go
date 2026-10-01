package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func TestBuildAWFConfigJSON_ModelRouting(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{
			ID: "copilot",
			ModelRouting: &CopilotModelRoutingConfig{
				Goal:          "cost",
				Mode:          "balanced",
				AllowedModels: []string{"claude-haiku-4.5", "gpt-5.4-mini"},
			},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
	}

	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData:   data,
	})
	require.NoError(t, err)
	require.NoError(t, validateAWFConfigJSON(configJSON))

	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	require.Equal(t, true, config["experimental"].(map[string]any)["modelRouting"])

	apiProxy := config["apiProxy"].(map[string]any)
	require.Equal(t, []any{"github-copilot/claude-haiku-4.5", "github-copilot/gpt-5.4-mini"}, apiProxy["allowedModels"])
	routing := apiProxy["routing"].(map[string]any)
	require.Equal(t, map[string]any{"goal": "cost", "mode": "balanced"}, routing["objective"])
	require.Equal(t, map[string]any{"conversationFile": modelRoutingConversationFile}, routing["task"])

	images := config["container"].(map[string]any)["images"].(map[string]any)
	for _, role := range []string{"agent", "apiProxy", "squid", "router"} {
		require.Regexp(t, `@sha256:[a-f0-9]{64}$`, images[role], role)
	}
}

func TestValidateModelRoutingRequiresMinimumAWFVersion(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{
			ID:           "copilot",
			ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced", AllowedModels: []string{"gpt-5.4-mini"}},
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true, Version: "v0.28.28"},
		},
	}

	err := validateModelRouting(data, "copilot")
	require.ErrorContains(t, err, "requires AWF v0.28.29 or newer")
}

func TestGenerateModelRoutingConversationStep(t *testing.T) {
	var generated strings.Builder
	generateModelRoutingConversationStep(&generated, &WorkflowData{
		EngineConfig: &EngineConfig{ModelRouting: &CopilotModelRoutingConfig{}},
	})

	var workflow struct {
		Steps []struct {
			Name string            `yaml:"name"`
			Env  map[string]string `yaml:"env"`
			Run  string            `yaml:"run"`
		} `yaml:"steps"`
	}
	require.NoError(t, yaml.Unmarshal([]byte("steps:\n"+generated.String()), &workflow))
	require.Len(t, workflow.Steps, 1)
	step := workflow.Steps[0]
	require.Equal(t, "Prepare model-routing conversation", step.Name)
	require.Contains(t, step.Env, "GH_AW_ROUTING_PROMPT")
	require.Contains(t, step.Env, "GH_AW_ROUTING_CONVERSATION_FILE")
	require.Contains(t, step.Env["GH_AW_ROUTING_CONVERSATION_FILE"], modelRoutingConversationFile)
	require.Equal(t, `node "${RUNNER_TEMP}/gh-aw/actions/prepare_model_routing_conversation.cjs"`, step.Run)
}

func TestExtractEngineConfig_ModelRouting(t *testing.T) {
	_, config, _ := (&Compiler{}).ExtractEngineConfig(map[string]any{
		"engine": map[string]any{
			"id": "copilot",
			"model-routing": map[string]any{
				"goal":           "cost-speed",
				"mode":           "auto",
				"allowed-models": []any{"gpt-5.4-mini"},
			},
		},
	})

	require.NotNil(t, config)
	require.Equal(t, &CopilotModelRoutingConfig{
		Goal:          "cost-speed",
		Mode:          "auto",
		AllowedModels: []string{"gpt-5.4-mini"},
	}, config.ModelRouting)
}

func TestCopilotModelRoutingDoesNotEmitCompileTimeModel(t *testing.T) {
	env := make(map[string]string)
	(&CopilotEngine{}).addCopilotModelEnv(env, &WorkflowData{
		Model: "small",
		EngineConfig: &EngineConfig{
			ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced"},
		},
	}, true, "GH_AW_MODEL")

	require.NotContains(t, env, "COPILOT_MODEL")
}
