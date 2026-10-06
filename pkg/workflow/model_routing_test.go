package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/require"
	yamlv3 "go.yaml.in/yaml/v3"
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
	for _, tc := range []struct {
		name     string
		topology RunnerTopology
		prompt   string
		fallback string
	}{
		{"hosted", "", constants.AwPromptsUserFile, constants.AwPromptsFile},
		{"arc-dind", RunnerTopologyArcDind, constants.AwPromptsUserFile, constants.AwPromptsFile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output strings.Builder
			generateModelRoutingConversationStep(&output, &WorkflowData{
				EngineConfig: &EngineConfig{ModelRouting: &CopilotModelRoutingConfig{}},
				RunnerConfig: &RunnerConfig{Topology: tc.topology},
			})

			require.Contains(t, output.String(), "Prepare model-routing conversation")
			require.Contains(t, output.String(), "GH_AW_ROUTING_PROMPT: "+tc.prompt+"\n")
			require.Contains(t, output.String(), "GH_AW_ROUTING_PROMPT_FALLBACK: "+tc.fallback+"\n")
			require.Contains(t, output.String(), modelRoutingConversationFile)
			require.Contains(t, output.String(), "role:'user',parts:[{text:prompt}]")
			require.NotContains(t, output.String(), "${{ runner.temp }}")
			require.NotContains(t, output.String(), "${RUNNER_TEMP}")

			source := `---
on: workflow_dispatch
strict: false
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: balanced
    allowed-models: [gpt-5.4-mini]
network:
  allowed: [defaults]
`
			if tc.topology != "" {
				source += "runner:\n  topology: " + string(tc.topology) + "\n"
				source += "sandbox:\n  agent:\n    images:\n      build-tools: registry.example.com/build-tools:v0.28.4@sha256:" + strings.Repeat("1", 64) + "\n"
			}
			source += "---\nSay hello.\n"
			dir := t.TempDir()
			workflowPath := filepath.Join(dir, "routed.md")
			require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0o600))
			require.NoError(t, NewCompiler().CompileWorkflow(workflowPath))
			compiled, err := os.ReadFile(filepath.Join(dir, "routed.lock.yml"))
			require.NoError(t, err)
			compiledYAML := string(compiled)
			require.Contains(t, compiledYAML, output.String())
			if tc.topology == RunnerTopologyArcDind {
				routingStepIndex := strings.Index(compiledYAML, "- name: Prepare model-routing conversation")
				promptStagingIndex := strings.Index(compiledYAML, "cp -a /tmp/gh-aw/aw-prompts")
				require.NotEqual(t, -1, routingStepIndex)
				require.NotEqual(t, -1, promptStagingIndex)
				require.Greater(t, promptStagingIndex, routingStepIndex)
			}
		})
	}
}

func TestModelRoutingConversationPromptFallback(t *testing.T) {
	var yaml strings.Builder
	generateModelRoutingConversationStep(&yaml, &WorkflowData{
		EngineConfig: &EngineConfig{ModelRouting: &CopilotModelRoutingConfig{}},
	})

	var steps []struct {
		Run string
	}
	require.NoError(t, yamlv3.Unmarshal([]byte(yaml.String()), &steps))
	require.Len(t, steps, 1)

	task := "  Task with \"quotes\", $variables, and a newline\n"
	fullPrompt := "System instructions\nFull workflow prompt"
	for _, tc := range []struct {
		name     string
		task     *string
		fallback *string
		want     string
	}{
		{"task only", &task, &fullPrompt, task},
		{"task without fallback", &task, nil, task},
		{"missing task", nil, &fullPrompt, fullPrompt},
		{"blank task", new(" \n\t"), &fullPrompt, fullPrompt},
		{"empty task", new(""), &fullPrompt, fullPrompt},
		{"both missing", nil, nil, ""},
		{"both blank", new("\n"), new(" \t"), ""},
		{"blank task missing fallback", new(" "), nil, ""},
		{"missing task blank fallback", nil, new("\n"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			taskPath := filepath.Join(dir, "user.txt")
			fallbackPath := filepath.Join(dir, "prompt.txt")
			conversationPath := filepath.Join(dir, "output", "routing-conversation.json")
			for file, text := range map[string]*string{taskPath: tc.task, fallbackPath: tc.fallback} {
				if text != nil {
					require.NoError(t, os.WriteFile(file, []byte(*text), 0o600))
				}
			}
			cmd := exec.Command("bash", "-c", steps[0].Run)
			cmd.Env = append(os.Environ(),
				"GH_AW_ROUTING_PROMPT="+taskPath,
				"GH_AW_ROUTING_PROMPT_FALLBACK="+fallbackPath,
				"GH_AW_ROUTING_CONVERSATION_FILE="+conversationPath,
			)
			output, err := cmd.CombinedOutput()
			if tc.want == "" {
				require.Error(t, err)
				require.Contains(t, string(output), "Rendered workflow prompt is empty; cannot route this task")
				require.NoFileExists(t, conversationPath)
				return
			}
			require.NoError(t, err, string(output))
			conversation, err := os.ReadFile(conversationPath)
			require.NoError(t, err)
			expected, err := json.Marshal([]any{map[string]any{
				"role": "user", "parts": []any{map[string]any{"text": tc.want}},
			}})
			require.NoError(t, err)
			require.JSONEq(t, string(expected), string(conversation))
			info, err := os.Stat(conversationPath)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	}
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
