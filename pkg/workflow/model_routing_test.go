package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
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
	expectedTag := strings.TrimPrefix(string(constants.DefaultFirewallVersion), "v")
	for _, role := range []string{awfImageRoleAgent, awfImageRoleAPIProxy, awfImageRoleSquid, awfImageRoleCliProxy} {
		imageTag, found := imageReferenceTag(images[role].(string))
		require.True(t, found, role)
		require.Equal(t, expectedTag, imageTag, role)
		pin, found := getEmbeddedContainerPin(defaultAWFImageForRole(role, imageTag))
		require.True(t, found, role)
		require.Equal(t, pin.PinnedImage, images[role], role)
	}
	routerPin, found := getEmbeddedContainerPin("ghcr.io/githubnext/gh-aw-router:" + string(constants.DefaultRouterVersion))
	require.True(t, found)
	require.Equal(t, routerPin.PinnedImage, images[awfImageRoleRouter])
	assertModelRoutingImagesInManifest(t, data, images)
}

func TestBuildAWFConfigJSON_ModelRoutingUsesEffectiveAWFVersion(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{
			ID:           "copilot",
			ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced", AllowedModels: []string{"gpt-5.4-mini"}},
		},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{Type: SandboxTypeAWF, Version: "v0.28.31"},
		},
	}

	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData:   data,
	})
	require.NoError(t, err)

	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	images := config["container"].(map[string]any)["images"].(map[string]any)
	for _, role := range []string{awfImageRoleAgent, awfImageRoleAPIProxy, awfImageRoleSquid, awfImageRoleCliProxy} {
		imageTag, found := imageReferenceTag(images[role].(string))
		require.True(t, found, role)
		require.Equal(t, "0.28.31", imageTag, role)
		pin, found := getEmbeddedContainerPin(defaultAWFImageForRole(role, imageTag))
		require.True(t, found, role)
		require.Equal(t, pin.PinnedImage, images[role], role)
	}
	assertModelRoutingImagesInManifest(t, data, images)
}

func assertModelRoutingImagesInManifest(t *testing.T, data *WorkflowData, images map[string]any) {
	t.Helper()

	manifestImages := collectDockerImages(map[string]any{}, data, ActionModeRelease)
	for _, role := range []string{awfImageRoleAgent, awfImageRoleAPIProxy, awfImageRoleSquid, awfImageRoleRouter} {
		image := images[role].(string)
		require.Contains(t, manifestImages, image, role)
		require.Contains(t, data.DockerImagePins, GHAWManifestContainer{Image: image}, role)
	}
}

func TestBuildAWFConfigJSON_ModelRoutingRouterImageOverride(t *testing.T) {
	const routerOverride = "registry.example.com/approved/router:v0.1.3@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	data := &WorkflowData{
		EngineConfig: &EngineConfig{
			ID:           "copilot",
			ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced", AllowedModels: []string{"gpt-5.4-mini"}},
		},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{Type: SandboxTypeAWF, Images: map[string]string{awfImageRoleRouter: routerOverride}},
		},
	}
	require.NoError(t, validateSandboxAgentImages(data))

	configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
		EngineName:     "copilot",
		AllowedDomains: "github.com",
		WorkflowData:   data,
	})
	require.NoError(t, err)

	var config map[string]any
	require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
	images := config["container"].(map[string]any)["images"].(map[string]any)
	require.Equal(t, routerOverride, images[awfImageRoleRouter])
}

func TestValidateSandboxAgentImagesRejectsUnpinnedModelRoutingVersion(t *testing.T) {
	data := &WorkflowData{
		EngineConfig: &EngineConfig{
			ID:           "copilot",
			ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced", AllowedModels: []string{"gpt-5.4-mini"}},
		},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{Type: SandboxTypeAWF, Version: "v0.28.32"},
		},
	}
	err := validateSandboxAgentImages(data)
	require.ErrorContains(t, err, "no digest pins are available for model routing with AWF version 0.28.32")
	require.ErrorContains(t, err, "ghcr.io/github/gh-aw-firewall/agent:0.28.32")
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
	var yaml strings.Builder
	generateModelRoutingConversationStep(&yaml, &WorkflowData{
		EngineConfig: &EngineConfig{ModelRouting: &CopilotModelRoutingConfig{}},
	})

	require.Contains(t, yaml.String(), "Prepare model-routing conversation")
	require.Contains(t, yaml.String(), "GH_AW_ROUTING_PROMPT")
	require.Contains(t, yaml.String(), modelRoutingConversationFile)
	require.Contains(t, yaml.String(), "role:'user',parts:[{text:prompt}]")
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
