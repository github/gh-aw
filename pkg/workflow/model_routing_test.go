package workflow

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/testutil"
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

func TestThreatDetectionDoesNotUseModelRouting(t *testing.T) {
	routing := &CopilotModelRoutingConfig{
		Goal: "cost", Mode: "balanced", AllowedModels: []string{"gpt-5.4-mini"},
	}

	for _, override := range []bool{false, true} {
		name := "inherited"
		if override {
			name = "detection engine override"
		}
		t.Run(name, func(t *testing.T) {
			data := &WorkflowData{
				AI:           "copilot",
				EngineConfig: &EngineConfig{ID: "copilot", ModelRouting: routing},
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
				SafeOutputs: &SafeOutputsConfig{
					ThreatDetection: &ThreatDetectionConfig{Model: "gpt-5.4-mini"},
				},
			}
			if override {
				data.SafeOutputs.ThreatDetection.EngineConfig = &EngineConfig{
					ID: "copilot", ModelRouting: routing,
				}
			}

			detectionData := buildExternalDetectorWorkflowData(data, "copilot")
			require.Nil(t, detectionData.EngineConfig.ModelRouting)
			require.Same(t, routing, data.EngineConfig.ModelRouting)
			if override {
				require.Same(t, routing, data.SafeOutputs.ThreatDetection.EngineConfig.ModelRouting)
			}

			configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
				EngineName: "copilot", WorkflowData: detectionData,
			})
			require.NoError(t, err)
			var config map[string]any
			require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
			require.NotContains(t, config, "experimental")
			require.NotContains(t, config["apiProxy"].(map[string]any), "routing")
			require.NotContains(t, config["container"].(map[string]any)["images"].(map[string]any), awfImageRoleRouter)

			compiler := NewCompiler()
			for path, build := range map[string]func(*WorkflowData) []string{
				"external": compiler.buildExternalDetectorExecutionStep,
				"inline":   compiler.buildDetectionEngineExecutionStep,
			} {
				t.Run(path, func(t *testing.T) {
					steps := strings.Join(build(data), "")
					require.Contains(t, steps, "COPILOT_MODEL: gpt-5.4-mini")
					require.NotContains(t, steps, "GH_AW_MODEL_ROUTING")
					require.NotContains(t, steps, "modelRouting")
					require.NotContains(t, steps, `"routing"`)
				})
			}
		})
	}
}

func TestCompiledRoutedWorkflowDoesNotRouteDetectionOrEvals(t *testing.T) {
	for _, external := range []bool{false, true} {
		for _, override := range []bool{false, true} {
			name := "inline"
			if external {
				name = "external"
			}
			if override {
				name += " with override"
			}
			t.Run(name, func(t *testing.T) {
				source := `---
on: workflow_dispatch
strict: false
engine:
  id: copilot
  model-routing:
    goal: cost
    mode: balanced
    allowed-models: [gpt-5.4-mini]
safe-outputs:
  create-issue:
  threat-detection:
    model: gpt-5.4-mini
`
				if override {
					source += `    engine:
      id: copilot
      model-routing:
        goal: cost
        mode: balanced
        allowed-models: [gpt-5.4-mini]
`
				}
				source += `evals:
  - id: quality
    question: Did the workflow produce a useful result?
`
				source += "features:\n  gh-aw-detection: "
				if external {
					source += "true\n"
				} else {
					source += "false\n"
				}
				source += "---\nReport a finding.\n"
				dir := t.TempDir()
				workflowPath := filepath.Join(dir, "routed.md")
				require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0o600))
				require.NoError(t, NewCompiler().CompileWorkflow(workflowPath))
				compiled, err := os.ReadFile(filepath.Join(dir, "routed.lock.yml"))
				require.NoError(t, err)
				require.Contains(t, string(compiled), "Prepare model-routing conversation")
				detection := extractJobSection(string(compiled), "detection")
				require.NotEmpty(t, detection)
				require.Contains(t, detection, "COPILOT_MODEL: detection")
				for _, forbidden := range []string{"modelRouting", `"routing"`, "GH_AW_MODEL_ROUTING", "gh-aw-router"} {
					require.NotContains(t, detection, forbidden)
				}
				evals := extractJobSection(string(compiled), "evals")
				require.NotEmpty(t, evals)
				require.Contains(t, string(compiled), "Prepare model-routing conversation")
				for _, forbidden := range []string{"modelRouting", `"routing"`, "GH_AW_MODEL_ROUTING", "gh-aw-router"} {
					require.NotContains(t, evals, forbidden)
				}
			})
		}
	}
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

func TestValidateModelRoutingEngineCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name      string
		engine    string
		provider  LLMProvider
		model     string
		wantError string
	}{
		{name: "Copilot accepts any Copilot model", engine: "copilot", model: "gpt-5.4"},
		{name: "Claude accepts Claude models", engine: "claude", provider: LLMProviderGitHub, model: "claude-sonnet-5"},
		{name: "Codex accepts GPT models", engine: "codex", provider: LLMProviderGitHub, model: "gpt-5.6-sol"},
		{name: "pi accepts Claude models", engine: "pi", provider: LLMProviderGitHub, model: "claude-sonnet-5"},
		{name: "Claude rejects GPT models", engine: "claude", provider: LLMProviderGitHub, model: "gpt-5.6-sol", wantError: "native Messages API"},
		{name: "Codex rejects Claude models", engine: "codex", provider: LLMProviderGitHub, model: "claude-sonnet-5", wantError: "Responses API"},
		{name: "Claude rejects non-Copilot provider", engine: "claude", provider: LLMProviderAnthropic, model: "claude-sonnet-5", wantError: "requires GitHub Copilot inference"},
		{name: "Codex rejects non-Copilot provider", engine: "codex", provider: LLMProviderOpenAI, model: "gpt-5.6-sol", wantError: "requires GitHub Copilot inference"},
		{name: "pi rejects non-Copilot provider", engine: "pi", provider: LLMProviderOpenAI, model: "gpt-5.6-sol", wantError: "requires GitHub Copilot inference"},
		{name: "unknown engine rejected", engine: "gemini", model: "gpt-5.6-sol", wantError: "supported only by Copilot, Claude, Codex, and pi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:           tc.engine,
					LLMProvider:  tc.provider,
					ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced", AllowedModels: []string{tc.model}},
				},
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true, Version: "v0.28.37"}},
			}
			err := validateModelRouting(data, tc.engine)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWarnRoutedModelOverrides(t *testing.T) {
	compiler := NewCompiler()
	output := testutil.CaptureStderr(t, func() {
		compiler.warnRoutedModelOverrides(&WorkflowData{
			Model: "fixed-model",
			EngineConfig: &EngineConfig{
				ID:           "pi",
				ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced"},
				Env: map[string]string{
					"GH_AW_PI_MODEL":             "fixed-model",
					"GH_AW_CUSTOM_MODELING_FLAG": "value",
					"CUSTOM_SETTING":             "value",
				},
				Config: `{"settings":{"defaultThinkingLevel":"high"}}`,
			},
		})
	})
	require.Contains(t, output, "model, engine.env.GH_AW_PI_MODEL, engine.config.settings.defaultThinkingLevel")
	require.NotContains(t, output, "GH_AW_CUSTOM_MODELING_FLAG")
	require.Equal(t, 1, compiler.GetWarningCount())
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

func TestModelRoutingModelAttributionOutputs(t *testing.T) {
	for _, engineID := range []string{"copilot", "claude", "codex", "pi"} {
		t.Run(engineID, func(t *testing.T) {
			data := &WorkflowData{
				AI:           engineID,
				Model:        "must-not-be-baked-in",
				EngineConfig: &EngineConfig{ID: engineID, ModelRouting: &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced"}},
			}
			outputs := buildMainJobCoreOutputs(data)
			require.Contains(t, outputs["model"], "steps.parse-token-usage.outputs.model")
			require.Contains(t, outputs["model"], "needs.activation.outputs.model")
			require.Equal(t, "${{ steps.parse-token-usage.outputs.model_effort }}", outputs["model_effort"])
			require.Equal(t, "${{ steps.parse-token-usage.outputs.model_routing_status }}", outputs["model_routing_status"])

			jobEnv := make(map[string]string)
			addJobLevelEngineMetadata(jobEnv, data)
			require.Equal(t, "${{ needs.agent.outputs.model }}", jobEnv["GH_AW_ENGINE_MODEL"])
			require.Equal(t, "${{ needs.agent.outputs.model_effort }}", jobEnv["GH_AW_ENGINE_MODEL_EFFORT"])
			require.Equal(t, "${{ needs.agent.outputs.model_routing_status }}", jobEnv["GH_AW_MODEL_ROUTING_STATUS"])
			env := buildEngineMetadataEnvVars(data.EngineConfig, data.Model)
			require.NotContains(t, strings.Join(env, ""), "must-not-be-baked-in")
			require.Contains(t, strings.Join(env, ""), "GH_AW_ENGINE_MODEL: ${{ needs.agent.outputs.model }}")
			require.Contains(t, strings.Join(env, ""), "GH_AW_ENGINE_MODEL_EFFORT: ${{ needs.agent.outputs.model_effort }}")
			require.Contains(t, strings.Join(env, ""), "GH_AW_MODEL_ROUTING_STATUS: ${{ needs.agent.outputs.model_routing_status }}")
			require.Len(t, buildModelRoutingOutputEnvVars(data.EngineConfig, "agent"), 3)
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

func TestInheritedDetectionModelStaysUnsetForRoutedEnginesWithoutAModel(t *testing.T) {
	tests := []struct {
		engineID          string
		detectionEngineID string
		model             string
	}{
		{engineID: "copilot", detectionEngineID: "copilot", model: "gpt-5.6-sol"},
		{engineID: "claude", detectionEngineID: "claude", model: "claude-opus-5"},
		{engineID: "codex", detectionEngineID: "codex", model: "gpt-5.6-sol"},
		{engineID: "pi", detectionEngineID: "copilot", model: "gpt-5.6-sol"},
	}
	for _, tc := range tests {
		t.Run(tc.engineID, func(t *testing.T) {
			data := &WorkflowData{
				EngineConfig: &EngineConfig{
					ID:          tc.engineID,
					LLMProvider: LLMProviderGitHub,
					ModelRouting: &CopilotModelRoutingConfig{
						Goal: "cost", Mode: "balanced", AllowedModels: []string{tc.model},
					},
				},
			}

			require.Empty(t, data.Model)
			require.Empty(t, inheritedDetectionModel(data, tc.detectionEngineID))
			detectionData := buildExternalDetectorWorkflowData(data, tc.detectionEngineID)
			require.Empty(t, detectionData.Model)
			require.Nil(t, detectionData.EngineConfig.ModelRouting)
		})
	}
}

func TestRoutedEnginesDoNotRouteThreatDetection(t *testing.T) {
	tests := []struct {
		engineID          string
		detectionEngineID string
		model             string
	}{
		{engineID: "copilot", detectionEngineID: "copilot", model: "gpt-5.6-sol"},
		{engineID: "claude", detectionEngineID: "claude", model: "claude-opus-5"},
		{engineID: "codex", detectionEngineID: "codex", model: "gpt-5.6-sol"},
		{engineID: "pi", detectionEngineID: "copilot", model: "gpt-5.6-sol"},
	}
	for _, tc := range tests {
		t.Run(tc.engineID, func(t *testing.T) {
			data := &WorkflowData{
				AI: tc.engineID,
				EngineConfig: &EngineConfig{
					ID:          tc.engineID,
					LLMProvider: LLMProviderGitHub,
					ModelRouting: &CopilotModelRoutingConfig{
						Goal: "cost", Mode: "balanced", AllowedModels: []string{tc.model},
					},
				},
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
				SafeOutputs:        &SafeOutputsConfig{ThreatDetection: &ThreatDetectionConfig{}},
			}
			detectionData := buildExternalDetectorWorkflowData(data, tc.detectionEngineID)
			configJSON, err := BuildAWFConfigJSON(AWFCommandConfig{
				EngineName: tc.detectionEngineID, WorkflowData: detectionData,
			})
			require.NoError(t, err)
			var config map[string]any
			require.NoError(t, json.Unmarshal([]byte(configJSON), &config))
			require.NotContains(t, config["apiProxy"].(map[string]any), "routing")
			require.NotContains(t, config["container"].(map[string]any)["images"].(map[string]any), awfImageRoleRouter)

			compiler := NewCompiler()
			for name, build := range map[string]func(*WorkflowData) []string{
				"external": compiler.buildExternalDetectorExecutionStep,
				"inline":   compiler.buildDetectionEngineExecutionStep,
			} {
				t.Run(name, func(t *testing.T) {
					steps := strings.Join(build(data), "")
					require.NotContains(t, steps, "GH_AW_MODEL_ROUTING")
					require.NotContains(t, steps, "candidateModels")
					require.NotContains(t, steps, awfImageRoleRouter)
				})
			}
		})
	}
}
