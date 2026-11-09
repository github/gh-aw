package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
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
	require.NotContains(t, apiProxy, "allowedModels")
	routing := apiProxy["routing"].(map[string]any)
	require.Equal(t, []any{"github-copilot/claude-haiku-4.5", "github-copilot/gpt-5.4-mini"}, routing["candidateModels"])
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
	routerImage := defaultAWFImageForRole(awfImageRoleRouter, expectedTag)
	routerPin, found := getEmbeddedContainerPin(routerImage)
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
	for _, implicit := range []bool{false, true} {
		for _, external := range []bool{false, true} {
			for _, override := range []bool{false, true} {
				name := "inline"
				if external {
					name = "external"
				}
				if override {
					name += " with override"
				}
				if implicit {
					name += " implicit default"
				}
				t.Run(name, func(t *testing.T) {
					routingBlock := `  model-routing:
    goal: cost
    mode: balanced
    allowed-models: [gpt-5.4-mini]
`
					if implicit {
						routingBlock = ""
					}
					source := `---
on: workflow_dispatch
strict: false
engine:
  id: copilot
` + routingBlock + `safe-outputs:
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
					require.Contains(t, detection, "COPILOT_MODEL: gpt-5.4-mini")
					for _, forbidden := range []string{"modelRouting", `"routing"`, "GH_AW_MODEL_ROUTING", "gh-aw-router"} {
						require.NotContains(t, detection, forbidden)
					}
					evals := extractJobSection(string(compiled), "evals")
					require.NotEmpty(t, evals)
					require.Contains(t, string(compiled), "Prepare model-routing conversation")
					for _, forbidden := range []string{"modelRouting", `"routing"`, "GH_AW_MODEL_ROUTING", "gh-aw-router"} {
						require.NotContains(t, evals, forbidden)
					}
					if implicit {
						require.Contains(t, string(compiled), "id: "+modelRoutingDecisionStepID)
						for _, forbidden := range []string{modelRoutingDecisionOutput + " ==", modelRoutingDecisionOutput + " !="} {
							require.NotContains(t, detection, forbidden, "implicit routing guards must not reach detection")
							require.NotContains(t, evals, forbidden, "implicit routing guards must not reach evals")
						}
					}
				})
			}
		}
	}
}

func TestCompiledRoutedWorkflowsKeepThreatDetectionJob(t *testing.T) {
	tests := []struct {
		name                string
		engine              string
		provider            string
		allowedModel        string
		detectionEngine     string
		detectionModel      string
		detectionModelEnv   string
		detectionModelValue string
		override            bool
	}{
		{
			name: "copilot", engine: "copilot", allowedModel: "gpt-5.4-mini",
			detectionEngine: "copilot", detectionModel: "gpt-4.1",
			detectionModelEnv: "COPILOT_MODEL", detectionModelValue: "gpt-4.1",
		},
		{
			name: "claude", engine: "claude", provider: "github", allowedModel: "claude-sonnet-4-6",
			detectionEngine: "copilot", detectionModel: "claude-haiku-4.5",
			detectionModelEnv: "COPILOT_MODEL", detectionModelValue: "claude-haiku-4.5",
		},
		{
			name: "pi", engine: "pi", provider: "github", allowedModel: "claude-sonnet-4-6",
			detectionEngine: "copilot", detectionModel: "pi/claude-haiku-4.5",
			detectionModelEnv: "COPILOT_MODEL", detectionModelValue: "claude-haiku-4.5",
		},
		{
			name: "claude with copilot override", engine: "claude", provider: "github",
			allowedModel: "claude-sonnet-4-6", detectionEngine: "copilot",
			detectionModel: "gpt-4.1", detectionModelEnv: "COPILOT_MODEL", detectionModelValue: "gpt-4.1",
			override: true,
		},
	}

	for _, tt := range tests {
		for _, external := range []bool{false, true} {
			path := "inline"
			if external {
				path = "external"
			}
			t.Run(tt.name+"/"+path, func(t *testing.T) {
				routed := compileRoutedDetectionTestWorkflow(t, tt, external, true)
				unrouted := compileRoutedDetectionTestWorkflow(t, tt, external, false)

				detection := routed.Jobs["detection"]
				require.NotNil(t, detection, "routed workflow must retain the detection job")
				require.Equal(t, "always() && needs.agent.result != 'skipped'", detection["if"])
				detectionYAML, err := yamlv3.Marshal(detection)
				require.NoError(t, err)
				for _, forbidden := range []string{
					"model_routing", "GH_AW_MODEL_ROUTING", "modelRouting", "model-routing",
					"gh-aw-router", "needs.agent.outputs.model", "needs.agent.outputs.routing",
				} {
					require.NotContains(t, string(detectionYAML), forbidden)
				}

				steps, ok := detection["steps"].([]any)
				require.True(t, ok, "detection job steps should be parsed from YAML")
				var executionStep map[string]any
				for _, rawStep := range steps {
					step, ok := rawStep.(map[string]any)
					if !ok {
						continue
					}
					if findCompiledEnvValue(t, step, tt.detectionModelEnv) != nil {
						executionStep = step
						break
					}
				}
				require.NotNil(t, executionStep, "expected %s execution step using fixed model %q", tt.detectionEngine, tt.detectionModel)
				require.Equal(t, tt.detectionModelValue, findCompiledEnvValue(t, executionStep, tt.detectionModelEnv), "engine execution step: %v", executionStep)
				if external {
					require.Contains(t, fmt.Sprint(executionStep["run"]), "--engine "+tt.detectionEngine)
				} else {
					require.Contains(t, fmt.Sprint(executionStep["name"]), "Copilot CLI")
				}
				if stepCondition, ok := executionStep["if"]; ok {
					for _, forbidden := range []string{"model_routing", "GH_AW_MODEL_ROUTING", "modelRouting", "needs.agent.outputs"} {
						require.NotContains(t, fmt.Sprint(stepCondition), forbidden)
					}
				}

				safeOutputs := routed.Jobs["safe_outputs"]
				require.NotNil(t, safeOutputs)
				require.Contains(t, safeOutputs["needs"], "detection")
				require.Contains(t, fmt.Sprint(safeOutputs["if"]), "needs.detection.result == 'success'")
				require.Equal(t, "${{ needs.detection.outputs.detection_conclusion }}", findCompiledEnvValue(t, safeOutputs, "GH_AW_DETECTION_CONCLUSION"))

				unroutedDetection := unrouted.Jobs["detection"]
				unroutedSafeOutputs := unrouted.Jobs["safe_outputs"]
				require.NotNil(t, unroutedDetection)
				require.NotNil(t, unroutedSafeOutputs)
				require.Equal(t, detection["if"], unroutedDetection["if"], "routing must not change detection gating")
				require.Equal(t, detection["needs"], unroutedDetection["needs"], "routing must not change detection dependencies")
				require.Equal(t, safeOutputs["if"], unroutedSafeOutputs["if"], "routing must not change safe_outputs gating")
				require.Equal(t, safeOutputs["needs"], unroutedSafeOutputs["needs"], "routing must not change safe_outputs dependencies")
			})
		}
	}
}

func compileRoutedDetectionTestWorkflow(t *testing.T, tt struct {
	name                string
	engine              string
	provider            string
	allowedModel        string
	detectionEngine     string
	detectionModel      string
	detectionModelEnv   string
	detectionModelValue string
	override            bool
}, external, routing bool) struct {
	Jobs map[string]map[string]any `yaml:"jobs"`
} {
	t.Helper()

	var source strings.Builder
	source.WriteString("---\non: workflow_dispatch\nstrict: false\nengine:\n  id: " + tt.engine + "\n")
	if tt.provider != "" {
		source.WriteString("  model-provider: " + tt.provider + "\n")
	}
	if routing {
		source.WriteString("  model-routing:\n    goal: cost\n    mode: balanced\n    allowed-models: [" + tt.allowedModel + "]\n")
	}
	source.WriteString("safe-outputs:\n  create-issue:\n  threat-detection:\n    model: " + tt.detectionModel + "\n")
	if tt.override {
		source.WriteString("    engine:\n      id: " + tt.detectionEngine + "\n")
		if routing {
			source.WriteString("      model-routing:\n        goal: cost\n        mode: balanced\n        allowed-models: [gpt-5.4-mini]\n")
		}
	}
	source.WriteString("features:\n  gh-aw-detection: ")
	if external {
		source.WriteString("true\n")
	} else {
		source.WriteString("false\n")
	}
	source.WriteString("---\nReport a finding.\n")

	dir := t.TempDir()
	workflowPath := filepath.Join(dir, "routed.md")
	require.NoError(t, os.WriteFile(workflowPath, []byte(source.String()), 0o600))
	require.NoError(t, NewCompiler().CompileWorkflow(workflowPath))
	compiled, err := os.ReadFile(filepath.Join(dir, "routed.lock.yml"))
	require.NoError(t, err)
	var workflow struct {
		Jobs map[string]map[string]any `yaml:"jobs"`
	}
	require.NoError(t, yamlv3.Unmarshal(compiled, &workflow))
	return workflow
}

func findCompiledEnvValue(t *testing.T, value any, key string) any {
	t.Helper()
	switch value := value.(type) {
	case map[string]any:
		if env, ok := value["env"].(map[string]any); ok {
			if envValue, found := env[key]; found {
				return envValue
			}
		}
		for _, nested := range value {
			if envValue := findCompiledEnvValue(t, nested, key); envValue != nil {
				return envValue
			}
		}
	case []any:
		for _, nested := range value {
			if envValue := findCompiledEnvValue(t, nested, key); envValue != nil {
				return envValue
			}
		}
	}
	return nil
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
	routerImage := constants.DefaultRouterRegistry + ":" + string(constants.DefaultRouterVersion)
	routerPin, found := getEmbeddedContainerPin(routerImage)
	require.True(t, found)
	require.Equal(t, routerPin.PinnedImage, images[awfImageRoleRouter])
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
		{name: "Claude accepts native Anthropic provider", engine: "claude", provider: LLMProviderAnthropic, model: "claude-sonnet-5"},
		{name: "Codex accepts native OpenAI provider", engine: "codex", provider: LLMProviderOpenAI, model: "gpt-5.4"},
		{name: "Claude rejects OpenAI provider", engine: "claude", provider: LLMProviderOpenAI, model: "claude-sonnet-5", wantError: "does not cross providers"},
		{name: "Codex rejects Anthropic provider", engine: "codex", provider: LLMProviderAnthropic, model: "gpt-5.4", wantError: "does not cross providers"},
		{name: "pi rejects non-Copilot provider", engine: "pi", provider: LLMProviderOpenAI, model: "gpt-5.6-sol", wantError: "requires GitHub Copilot inference"},
		{name: "Copilot rejects non-Copilot provider", engine: "copilot", provider: LLMProviderOpenAI, model: "gpt-5.6-sol", wantError: "requires GitHub Copilot inference"},
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

func TestCompileModelRoutingProviderSupport(t *testing.T) {
	for _, tc := range []struct {
		name         string
		engine       string
		provider     string
		fixedModel   string
		allowedModel string
		wantError    string
	}{
		{name: "codex without provider routes natively on openai", engine: "codex", allowedModel: "gpt-5.4"},
		{name: "codex with copilot-prefixed fixed model and no provider", engine: "codex", fixedModel: "copilot/gpt-5.6-luna", allowedModel: "gpt-5.6-luna", wantError: `resolves to the "openai" provider`},
		{name: "claude without provider routes natively on anthropic", engine: "claude", allowedModel: "claude-sonnet-5"},
		{name: "claude with anthropic", engine: "claude", provider: "anthropic", allowedModel: "claude-sonnet-5"},
		{name: "codex with openai", engine: "codex", provider: "openai", allowedModel: "gpt-5.4"},
		{name: "pi with anthropic", engine: "pi", provider: "anthropic", allowedModel: "claude-sonnet-5", wantError: `resolves to the "anthropic" provider`},
		{name: "copilot with openai", engine: "copilot", provider: "openai", allowedModel: "gpt-5.6-luna", wantError: `resolves to the "openai" provider`},
		{name: "codex with incompatible candidate", engine: "codex", provider: "github", allowedModel: "claude-sonnet-5", wantError: "incompatible with Codex's Responses API"},
		{name: "claude with github", engine: "claude", provider: "github", allowedModel: "claude-sonnet-5"},
		{name: "codex with github", engine: "codex", provider: "github", allowedModel: "gpt-5.6-luna"},
		{name: "copilot default provider", engine: "copilot", allowedModel: "gpt-5.6-luna"},
		{name: "pi default provider", engine: "pi", allowedModel: "gpt-5.6-luna"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var source strings.Builder
			source.WriteString("---\non: workflow_dispatch\n")
			if tc.fixedModel != "" {
				source.WriteString("model: " + tc.fixedModel + "\n")
			}
			source.WriteString("engine:\n  id: " + tc.engine + "\n")
			if tc.provider != "" {
				source.WriteString("  model-provider: " + tc.provider + "\n")
			}
			source.WriteString("  model-routing:\n    goal: cost\n    mode: economy\n    allowed-models: [" + tc.allowedModel + "]\n---\nReport a finding.\n")
			dir := t.TempDir()
			workflowPath := filepath.Join(dir, "routed.md")
			require.NoError(t, os.WriteFile(workflowPath, []byte(source.String()), 0o600))
			err := NewCompiler().CompileWorkflow(workflowPath)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				if strings.Contains(tc.wantError, "provider") {
					require.ErrorContains(t, err, "engine.model-provider: github")
				}
				_, statErr := os.Stat(filepath.Join(dir, "routed.lock.yml"))
				require.True(t, os.IsNotExist(statErr), "lock file must not be written when routing validation fails")
				return
			}
			require.NoError(t, err)
			compiled, err := os.ReadFile(filepath.Join(dir, "routed.lock.yml"))
			require.NoError(t, err)
			require.Contains(t, string(compiled), `awf --config "${RUNNER_TEMP}/gh-aw/awf-config.json"`)
			require.Contains(t, string(compiled), `modelRouting\":true`)
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
		implicit          bool
	}{
		{engineID: "copilot", detectionEngineID: "copilot", model: "gpt-5.6-sol"},
		{engineID: "claude", detectionEngineID: "claude", model: "claude-opus-5"},
		{engineID: "codex", detectionEngineID: "codex", model: "gpt-5.6-sol"},
		{engineID: "pi", detectionEngineID: "copilot", model: "gpt-5.6-sol"},
		{engineID: "copilot", detectionEngineID: "copilot", implicit: true},
		{engineID: "claude", detectionEngineID: "claude", implicit: true},
		{engineID: "codex", detectionEngineID: "codex", implicit: true},
		{engineID: "pi", detectionEngineID: "copilot", implicit: true},
	}
	for _, tc := range tests {
		name := tc.engineID
		if tc.implicit {
			name += " implicit default"
		}
		t.Run(name, func(t *testing.T) {
			routing := &CopilotModelRoutingConfig{Goal: "cost", Mode: "balanced", implicit: tc.implicit}
			if tc.model != "" {
				routing.AllowedModels = []string{tc.model}
			}
			data := &WorkflowData{
				AI: tc.engineID,
				EngineConfig: &EngineConfig{
					ID:           tc.engineID,
					LLMProvider:  LLMProviderGitHub,
					ModelRouting: routing,
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
					require.NotContains(t, steps, "needs.activation.outputs."+modelRoutingDecisionOutput)
				})
			}
		})
	}
}

// defaultRoutingCompilation is a compiled no-model workflow with its diagnostics.
type defaultRoutingCompilation struct {
	err      error
	lock     string
	stderr   string
	warnings int
	data     *WorkflowData
}

// compileDefaultRoutingFiles writes files (relative paths) into a temporary
// directory, compiles main.md, and returns the lock, stderr, and warning count.
func compileDefaultRoutingFiles(t *testing.T, files map[string]string) defaultRoutingCompilation {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}
	workflowPath := filepath.Join(dir, "main.md")
	compiler := NewCompiler()
	var compileErr error
	stderr := testutil.CaptureStderr(t, func() { compileErr = compiler.CompileWorkflow(workflowPath) })
	result := defaultRoutingCompilation{err: compileErr, stderr: stderr, warnings: compiler.GetWarningCount()}
	if compileErr != nil {
		return result
	}
	content, err := os.ReadFile(filepath.Join(dir, "main.lock.yml"))
	require.NoError(t, err)
	result.lock = string(content)
	data, err := NewCompiler().ParseWorkflowFile(workflowPath)
	require.NoError(t, err)
	result.data = data
	return result
}

func compileDefaultRoutingSource(t *testing.T, source string) defaultRoutingCompilation {
	t.Helper()
	return compileDefaultRoutingFiles(t, map[string]string{"main.md": source})
}

// awfConfigs decodes every AWF config branch the agent job may write: the routed
// branch first and, for runtime-guarded implicit routing, the non-routed branch.
func (c defaultRoutingCompilation) awfConfigs(t *testing.T) []map[string]any {
	t.Helper()
	agent := extractJobSection(c.lock, "agent")
	require.NotEmpty(t, agent)
	var configs []map[string]any
	for _, match := range awfConfigWritePattern.FindAllStringSubmatch(agent, -1) {
		config := strings.ReplaceAll(match[1], "${GH_AW_MAX_AI_CREDITS}", "0")
		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(shellEscapePattern.ReplaceAllString(config, "$1")), &decoded))
		configs = append(configs, decoded)
	}
	return configs
}

const defaultRoutingNoticeMarker = "Set engine.model-routing: false to opt out"

// requireNoDefaultRouting asserts the compiled lock keeps today's non-routed
// behavior: no routing config, router image, decision step, conversation step,
// or default notice.
func requireNoDefaultRouting(t *testing.T, c defaultRoutingCompilation) {
	t.Helper()
	require.NoError(t, c.err)
	require.NotContains(t, c.lock, "id: "+modelRoutingDecisionStepID)
	require.NotContains(t, c.lock, "Prepare model-routing conversation")
	require.NotContains(t, c.lock, "gh-aw-firewall/router")
	require.NotContains(t, c.lock, `\"routing\":{`)
	require.NotContains(t, c.lock, "GH_AW_MODEL_ROUTING: ")
	require.NotContains(t, c.stderr, defaultRoutingNoticeMarker)
	stderr := strings.ToLower(c.stderr)
	require.NotContains(t, stderr, "model routing", "no routing diagnostics for a workflow without the default")
	require.NotContains(t, stderr, "model-routing", "no routing diagnostics for a workflow without the default")
	if c.data != nil && c.data.EngineConfig != nil {
		require.Nil(t, c.data.EngineConfig.ModelRouting)
	}
}

func TestCompileDefaultModelRouting_Copilot(t *testing.T) {
	compiled := compileDefaultRoutingSource(t, `---
on: workflow_dispatch
permissions:
  contents: read
engine: copilot
---
Report a finding.
`)
	require.NoError(t, compiled.err)
	require.Zero(t, compiled.warnings, "the implicit default must not add warnings")
	require.Equal(t, 1, strings.Count(compiled.stderr, defaultRoutingNoticeMarker), "one informational notice per workflow")
	require.Contains(t, compiled.stderr, "goal cost and mode balanced")
	require.Contains(t, compiled.stderr, "vars.GH_AW_MODEL_AGENT_COPILOT")
	require.Contains(t, compiled.stderr, "vars.GH_AW_DEFAULT_MODEL_COPILOT")

	require.True(t, isModelRoutingImplicit(compiled.data))
	require.Equal(t, "cost", compiled.data.EngineConfig.ModelRouting.Goal)
	require.Equal(t, "balanced", compiled.data.EngineConfig.ModelRouting.Mode)
	require.Empty(t, compiled.data.EngineConfig.ModelRouting.AllowedModels)

	configs := compiled.awfConfigs(t)
	require.Len(t, configs, 2, "runtime-guarded routing writes a routed and a non-routed AWF config")
	routed := configs[0]
	require.Equal(t, true, routed["experimental"].(map[string]any)["modelRouting"])
	routing := routed["apiProxy"].(map[string]any)["routing"].(map[string]any)
	require.Equal(t, map[string]any{"goal": "cost", "mode": "balanced"}, routing["objective"])
	require.Equal(t, map[string]any{"conversationFile": modelRoutingConversationFile}, routing["task"])
	routerImage, ok := routed["container"].(map[string]any)["images"].(map[string]any)[awfImageRoleRouter].(string)
	require.True(t, ok, "routed config must pin the router image")
	require.Contains(t, routerImage, "@sha256:")

	require.Contains(t, compiled.lock, "id: "+modelRoutingDecisionStepID)
	require.Contains(t, compiled.lock, "Prepare model-routing conversation")
	require.Contains(t, compiled.lock, "if: "+modelRoutingActiveDownstream)
}

func TestCompileDefaultModelRouting_ExplicitModelUnchanged(t *testing.T) {
	base := "---\non: workflow_dispatch\npermissions:\n  contents: read\n"
	for name, files := range map[string]map[string]string{
		"top-level model":  {"main.md": base + "model: claude-sonnet-5.5\nengine: copilot\n---\nReport.\n"},
		"engine.model":     {"main.md": base + "engine:\n  id: copilot\n  model: gpt-5.6-luna\n---\nReport.\n"},
		"alias":            {"main.md": base + "engine:\n  id: copilot\n  model: small\n---\nReport.\n"},
		"auto":             {"main.md": base + "model: auto\nengine: copilot\n---\nReport.\n"},
		"expression":       {"main.md": base + "engine:\n  id: copilot\n  model: ${{ vars.MY_MODEL }}\n---\nReport.\n"},
		"engine env model": {"main.md": base + "engine:\n  id: copilot\n  env:\n    COPILOT_MODEL: gpt-5.6-luna\n---\nReport.\n"},
		"imported model": {
			"main.md":         base + "imports:\n  - shared/model.md\n---\nReport.\n",
			"shared/model.md": "---\nengine:\n  id: copilot\n  model: gpt-5.6-luna\n---\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			requireNoDefaultRouting(t, compileDefaultRoutingFiles(t, files))
		})
	}
}

func TestCompileDefaultModelRouting_ExplicitFalse(t *testing.T) {
	base := "---\non: workflow_dispatch\npermissions:\n  contents: read\n"
	for name, files := range map[string]map[string]string{
		"main opt-out": {"main.md": base + "engine:\n  id: copilot\n  model-routing: false\n---\nReport.\n"},
		"imported opt-out": {
			"main.md":              base + "imports:\n  - shared/no-routing.md\n---\nReport.\n",
			"shared/no-routing.md": "---\nengine:\n  id: copilot\n  model-routing: false\n---\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			compiled := compileDefaultRoutingFiles(t, files)
			requireNoDefaultRouting(t, compiled)
			require.True(t, compiled.data.EngineConfig.ModelRoutingDisabled)
			require.Empty(t, compiled.data.Model, "opting out must not pin a model")
			require.Contains(t, compiled.lock, "COPILOT_MODEL: ${{ vars.GH_AW_MODEL_AGENT_COPILOT || vars.GH_AW_DEFAULT_MODEL_COPILOT || 'auto' }}")
		})
	}

	t.Run("extraction", func(t *testing.T) {
		_, config, _ := NewCompiler().ExtractEngineConfig(map[string]any{"engine": map[string]any{"id": "copilot", "model-routing": false}})
		require.NotNil(t, config)
		require.True(t, config.ModelRoutingDisabled)
		require.Nil(t, config.ModelRouting)
	})

	t.Run("import merge keeps false distinct from absent", func(t *testing.T) {
		compiler := NewCompiler()
		routed := `{"id":"copilot","model-routing":{"goal":"cost","mode":"economy"}}`
		disabled := `{"id":"copilot","model-routing":false}`

		merged := compiler.mergeImportedModelRoutingConfigs(&EngineConfig{ID: "copilot"}, []string{disabled}, false)
		require.True(t, merged.ModelRoutingDisabled)
		require.Nil(t, merged.ModelRouting)

		merged = compiler.mergeImportedModelRoutingConfigs(&EngineConfig{ID: "copilot"}, []string{disabled, routed}, false)
		require.False(t, merged.ModelRoutingDisabled, "an imported routing object wins over an imported opt-out")
		require.Equal(t, "economy", merged.ModelRouting.Mode)

		merged = compiler.mergeImportedModelRoutingConfigs(&EngineConfig{ID: "copilot", ModelRoutingDisabled: true}, []string{routed}, true)
		require.True(t, merged.ModelRoutingDisabled, "the main workflow's opt-out wins over imported routing")
		require.Nil(t, merged.ModelRouting)

		merged = compiler.mergeImportedModelRoutingConfigs(&EngineConfig{ID: "copilot"}, nil, false)
		require.False(t, merged.ModelRoutingDisabled)
	})

	t.Run("true is not a shorthand", func(t *testing.T) {
		compiled := compileDefaultRoutingSource(t, base+"engine:\n  id: copilot\n  model-routing: true\n---\nReport.\n")
		require.Error(t, compiled.err)
	})
}

func TestCompileDefaultModelRouting_ExplicitRoutingUnchanged(t *testing.T) {
	base := "---\non: workflow_dispatch\npermissions:\n  contents: read\n"
	for name, tc := range map[string]struct {
		files      map[string]string
		mode       string
		candidates []any
	}{
		"economy mode with shortlist": {
			files:      map[string]string{"main.md": base + "engine:\n  id: copilot\n  model-routing:\n    goal: cost\n    mode: economy\n    allowed-models: [gpt-5.6-luna]\n---\nReport.\n"},
			mode:       "economy",
			candidates: []any{"github-copilot/gpt-5.6-luna"},
		},
		"auto mode": {
			files: map[string]string{"main.md": base + "engine:\n  id: copilot\n  model-routing:\n    goal: cost\n    mode: auto\n---\nReport.\n"},
			mode:  "auto",
		},
		"imported routing": {
			files: map[string]string{
				"main.md":           base + "imports:\n  - shared/routing.md\n---\nReport.\n",
				"shared/routing.md": "---\nengine:\n  id: copilot\n  model-routing:\n    goal: cost\n    mode: robust\n---\n",
			},
			mode: "robust",
		},
	} {
		t.Run(name, func(t *testing.T) {
			compiled := compileDefaultRoutingFiles(t, tc.files)
			require.NoError(t, compiled.err)
			require.False(t, isModelRoutingImplicit(compiled.data))
			require.NotContains(t, compiled.stderr, defaultRoutingNoticeMarker)
			require.NotContains(t, compiled.lock, "id: "+modelRoutingDecisionStepID, "runtime model variables must not disable explicit routing")
			require.Contains(t, compiled.lock, "GH_AW_MODEL_ROUTING: 1")
			require.NotContains(t, compiled.lock, "COPILOT_MODEL: ${{")
			configs := compiled.awfConfigs(t)
			require.Len(t, configs, 1)
			routing := configs[0]["apiProxy"].(map[string]any)["routing"].(map[string]any)
			require.Equal(t, map[string]any{"goal": "cost", "mode": tc.mode}, routing["objective"])
			if tc.candidates != nil {
				require.Equal(t, tc.candidates, routing["candidateModels"])
			}
		})
	}
}

func TestCompileDefaultModelRouting_RuntimeOverrideGuard(t *testing.T) {
	compiled := compileDefaultRoutingSource(t, "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine: copilot\n---\nReport.\n")
	require.NoError(t, compiled.err)

	activation := extractJobSection(compiled.lock, "activation")
	require.Contains(t, activation, "GH_AW_MODEL_AGENT_VAR: ${{ vars.GH_AW_MODEL_AGENT_COPILOT }}")
	require.Contains(t, activation, "GH_AW_DEFAULT_MODEL_VAR: ${{ vars.GH_AW_DEFAULT_MODEL_COPILOT }}")
	require.Contains(t, activation, "model_routing: ${{ steps.resolve-model-routing.outputs.model_routing }}")

	env := agentExecutionEnv(t, compiled.lock)
	require.Equal(t, "${{ needs.activation.outputs.model_routing != 'true' && (vars.GH_AW_MODEL_AGENT_COPILOT || vars.GH_AW_DEFAULT_MODEL_COPILOT || 'auto') || '' }}", env["COPILOT_MODEL"])
	require.Equal(t, "${{ needs.activation.outputs.model_routing == 'true' && '1' || '0' }}", env["GH_AW_MODEL_ROUTING"])

	configs := compiled.awfConfigs(t)
	require.Len(t, configs, 2)
	plain := configs[1]
	require.NotContains(t, plain, "experimental")
	require.NotContains(t, plain["apiProxy"].(map[string]any), "routing")
	if images, ok := plain["container"].(map[string]any)["images"].(map[string]any); ok {
		require.NotContains(t, images, awfImageRoleRouter)
	}
	agent := extractJobSection(compiled.lock, "agent")
	require.Contains(t, agent, `if [ "${GH_AW_MODEL_ROUTING:-}" = "1" ]; then`)

	// The non-routed branch matches an explicit opt-out's AWF config.
	optOut := compileDefaultRoutingSource(t, "---\non: workflow_dispatch\npermissions:\n  contents: read\nengine:\n  id: copilot\n  model-routing: false\n---\nReport.\n")
	require.NoError(t, optOut.err)
	optOutConfigs := optOut.awfConfigs(t)
	require.Len(t, optOutConfigs, 1)
	require.Equal(t, optOutConfigs[0]["apiProxy"], plain["apiProxy"])
	require.Equal(t, optOutConfigs[0]["experimental"], plain["experimental"])
}

func TestCompileDefaultModelRouting_ProviderMatrix(t *testing.T) {
	routedSource := func(engine, provider string) string {
		source := "---\non: workflow_dispatch\npermissions:\n  contents: read\n  copilot-requests: write\nengine:\n  id: " + engine + "\n"
		if provider != "" {
			source += "  model-provider: " + provider + "\n"
		}
		return source + "---\nReport a finding.\n"
	}
	for _, tc := range []struct {
		engine, provider string
		candidates       []any
		modelEnv         string
		guarded          bool
	}{
		{engine: "claude", provider: "github", candidates: []any{"github-copilot/claude-*"}, modelEnv: "GH_AW_MODEL_AGENT_CLAUDE", guarded: true},
		{engine: "codex", provider: "github", candidates: []any{"github-copilot/gpt-*"}, modelEnv: "GH_AW_MODEL_AGENT_CODEX", guarded: true},
		{engine: "pi", provider: "github"},
		{engine: "pi"},
	} {
		t.Run(tc.engine+"/"+tc.provider+"/routed", func(t *testing.T) {
			compiled := compileDefaultRoutingSource(t, routedSource(tc.engine, tc.provider))
			require.NoError(t, compiled.err)
			require.Zero(t, compiled.warnings)
			require.Equal(t, 1, strings.Count(compiled.stderr, defaultRoutingNoticeMarker))
			require.True(t, isModelRoutingImplicit(compiled.data))
			routing := compiled.awfConfigs(t)[0]["apiProxy"].(map[string]any)["routing"].(map[string]any)
			require.Equal(t, map[string]any{"goal": "cost", "mode": "balanced"}, routing["objective"])
			if tc.candidates != nil {
				require.Equal(t, tc.candidates, routing["candidateModels"], "model-family restriction must be kept")
			}
			require.Equal(t, tc.guarded, strings.Contains(compiled.lock, "id: "+modelRoutingDecisionStepID))
			if tc.guarded {
				env := agentExecutionEnv(t, compiled.lock)
				require.True(t, strings.HasPrefix(env[tc.modelEnv], "${{ needs.activation.outputs.model_routing != 'true' && (vars."+tc.modelEnv), env[tc.modelEnv])
			} else {
				require.Contains(t, compiled.lock, "GH_AW_MODEL_ROUTING: 1")
			}
		})
	}
	for _, tc := range []struct{ engine, provider string }{
		{engine: "claude"},
		{engine: "claude", provider: "anthropic"},
		{engine: "codex"},
		{engine: "codex", provider: "openai"},
		{engine: "pi", provider: "anthropic"},
	} {
		t.Run(tc.engine+"/"+tc.provider+"/native unchanged", func(t *testing.T) {
			compiled := compileDefaultRoutingSource(t, routedSource(tc.engine, tc.provider))
			if tc.engine == "pi" && compiled.err != nil {
				return
			}
			requireNoDefaultRouting(t, compiled)
		})
	}
}

func TestCompileDefaultModelRouting_IneligibleUnchanged(t *testing.T) {
	base := "---\non: workflow_dispatch\npermissions:\n  contents: read\n"
	for name, source := range map[string]string{
		"gemini":               base + "engine: gemini\n---\nReport.\n",
		"no AWF":               "---\non: workflow_dispatch\nstrict: false\npermissions:\n  contents: read\nfeatures:\n  dangerously-disable-sandbox-agent: true\nsandbox:\n  agent: false\nengine: copilot\n---\nReport.\n",
		"engine env override":  base + "engine:\n  id: copilot\n  env:\n    GH_AW_DEFAULT_MODEL_COPILOT: gpt-5.6-luna\n---\nReport.\n",
		"engine env base url":  base + "engine:\n  id: copilot\n  env:\n    COPILOT_PROVIDER_BASE_URL: https://example.com\n---\nReport.\n",
		"model-bearing args":   base + "engine:\n  id: copilot\n  args: [\"--model\", \"gpt-5.6-luna\"]\n---\nReport.\n",
		"custom command":       base + "engine:\n  id: copilot\n  command: /usr/local/bin/copilot\n---\nReport.\n",
		"old pinned AWF":       base + "engine: copilot\nsandbox:\n  agent:\n    version: v0.28.28\n---\nReport.\n",
		"custom sandbox image": base + "engine: copilot\nsandbox:\n  agent:\n    images:\n      agent: ghcr.io/example/agent:1\n---\nReport.\n",
	} {
		t.Run(name, func(t *testing.T) {
			compiled := compileDefaultRoutingSource(t, source)
			if compiled.err != nil {
				// A configuration that was already invalid must not become valid or
				// produce a routing-specific error.
				require.NotContains(t, compiled.err.Error(), "model-routing")
				require.NotContains(t, compiled.stderr, defaultRoutingNoticeMarker)
				return
			}
			requireNoDefaultRouting(t, compiled)
		})
	}

	t.Run("fallback models", func(t *testing.T) {
		data := &WorkflowData{
			EngineConfig:       &EngineConfig{ID: "copilot", LLMProvider: LLMProviderGitHub, FallbackModels: []string{"gpt-5.6-luna"}},
			NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
		}
		require.NotEmpty(t, defaultModelRoutingIneligibility(data))
	})
}

// TestRecompileDefaultModelRouting_EligibleLocksOnly checks the committed
// repository locks: only workflows whose source chooses neither a model nor
// routing carry the implicit default, and every workflow with an explicit model,
// explicit routing, or the opt-out keeps its non-defaulted lock.
func TestRecompileDefaultModelRouting_EligibleLocksOnly(t *testing.T) {
	workflowsDir := filepath.Join("..", "..", ".github", "workflows")
	sources, err := filepath.Glob(filepath.Join(workflowsDir, "*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, sources)
	implicit := 0
	for _, sourcePath := range sources {
		lockContent, err := os.ReadFile(strings.TrimSuffix(sourcePath, ".md") + ".lock.yml")
		if err != nil {
			continue
		}
		content, err := os.ReadFile(sourcePath)
		require.NoError(t, err)
		result, err := parser.ExtractFrontmatterFromContent(string(content))
		require.NoError(t, err)
		if _, ok := result.Frontmatter["on"]; !ok {
			continue
		}
		lock := string(lockContent)
		name := filepath.Base(sourcePath)
		explicitChoice := declaresModelOrRoutingChoice(result.Frontmatter, "", nil, "")
		guarded := strings.Contains(lock, "id: "+modelRoutingDecisionStepID)
		if guarded {
			implicit++
			require.False(t, explicitChoice, "%s declares a model or routing choice but its lock carries the implicit routing default", name)
		}
		engineObj, _ := result.Frontmatter["engine"].(map[string]any)
		if engineObj == nil {
			continue
		}
		if routing, ok := engineObj["model-routing"]; ok {
			require.False(t, guarded, "%s sets engine.model-routing; its lock must not carry the runtime-guarded default", name)
			if routing == false {
				require.NotContains(t, lock, "Prepare model-routing conversation", "%s opts out of routing", name)
			}
		}
	}
	require.Positive(t, implicit, "expected eligible repository workflows to use the implicit default")
}
