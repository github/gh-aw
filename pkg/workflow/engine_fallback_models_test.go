package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/stretchr/testify/require"
)

func TestEngineFallbackModelsExtractionAndSchema(t *testing.T) {
	engine := map[string]any{"id": "copilot", "model": "primary", "fallback-models": []any{"openai/secondary", "anthropic/last"}}
	_, config, model := NewCompiler().ExtractEngineConfig(map[string]any{"engine": engine})
	require.Equal(t, "primary", model)
	require.Equal(t, []string{"openai/secondary", "anthropic/last"}, config.FallbackModels)
	frontmatter := map[string]any{"on": "workflow_dispatch", "engine": engine}
	require.NoError(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "fallback.md"))
	for _, invalid := range []any{[]any{}, []any{""}, []any{"same", "same"}, []any{5}, []any{"${{ inputs.model }}"}, []any{"model\nINJECT=value"}} {
		engine["fallback-models"] = invalid
		require.Error(t, parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(frontmatter, "fallback.md"))
	}
}

func TestEngineFallbackProviderSecrets(t *testing.T) {
	for _, id := range []string{"copilot", "claude", "codex"} {
		t.Run(id, func(t *testing.T) {
			fallbacks := []string{"copilot/secondary", "copilot/last"}
			if id == "copilot" {
				fallbacks = []string{"anthropic/secondary", "openai/last", "openai/another"}
			}
			data := &WorkflowData{
				EngineConfig:       &EngineConfig{ID: id, FallbackModels: fallbacks},
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
			}
			require.ErrorContains(t, validateEngineFallbackModels(data), "same provider")
			env := map[string]string{}
			applyEngineHarnessRetryEnv(env, data)
			require.NotContains(t, env, "GH_AW_FALLBACK_MODELS")
			require.NotContains(t, env, "GH_AW_NATIVE_FALLBACK_MODELS")
			engine, err := GetGlobalEngineRegistry().GetEngine(id)
			require.NoError(t, err)
			for _, secret := range fallbackProviderSecretNames(data) {
				require.Contains(t, env, secret)
				require.Contains(t, engine.GetRequiredSecretNames(data), secret)
				require.Contains(t, ComputeAWFExcludeEnvVarNames(data, nil), secret)
				require.Contains(t, FilterEnvForSecrets(env, engine.GetRequiredSecretNames(data)), secret)
			}
			steps := buildFallbackProviderValidationSteps(data)
			require.Len(t, steps, len(fallbackModelProviders(data))-1)
			if id != "copilot" {
				require.NotContains(t, env, "MCP_GATEWAY_AGENT_ID")
			}
			data.Permissions = "copilot-requests: write"
			applyEngineHarnessRetryEnv(env, data)
			require.Equal(t, "${{ github.token }}", env["COPILOT_GITHUB_TOKEN"])
			for _, step := range buildFallbackProviderValidationSteps(data) {
				require.NotContains(t, strings.Join(step, "\n"), "validate-fallback-github")
			}
		})
	}
}

func TestEngineFallbackModelsValidation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		config   EngineConfig
		model    string
		want     string
		disabled bool
	}{
		{"custom harness", EngineConfig{ID: "copilot", HarnessScript: "custom.cjs", FallbackModels: []string{"secondary"}}, "", "", false},
		{"custom driver", EngineConfig{ID: "copilot", Driver: "custom.cjs", FallbackModels: []string{"secondary"}}, "", "", false},
		{"routing", EngineConfig{ID: "copilot", ModelRouting: &CopilotModelRoutingConfig{}, FallbackModels: []string{"secondary"}}, "", "model-routing", false},
		{"no harness", EngineConfig{ID: "gemini", FallbackModels: []string{"secondary"}}, "", "", false},
		{"expression", EngineConfig{ID: "copilot", FallbackModels: []string{"${{ inputs.model }}"}}, "", "literal", false},
		{"glob", EngineConfig{ID: "copilot", FallbackModels: []string{"gpt-*"}}, "", "literal", false},
		{"cross provider without AWF", EngineConfig{ID: "copilot", FallbackModels: []string{"openai/secondary"}}, "", "AWF", true},
		{"same provider without AWF", EngineConfig{ID: "codex", FallbackModels: []string{"openai/secondary"}}, "", "AWF", true},
		{"native unqualified codex", EngineConfig{ID: "codex", FallbackModels: []string{"secondary"}}, "", "", false},
		{"native unqualified claude", EngineConfig{ID: "claude", FallbackModels: []string{"secondary"}}, "", "", false},
		{"single model", EngineConfig{ID: "gemini"}, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateEngineFallbackModels(&WorkflowData{
				Model: tc.model, EngineConfig: &tc.config,
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
				SandboxConfig:      &SandboxConfig{Agent: &AgentSandboxConfig{Disabled: tc.disabled}},
			})
			if tc.want == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.want)
			}
		})
	}
}

func TestEngineFallbackAliasesAndPricing(t *testing.T) {
	data := &WorkflowData{
		EngineConfig:  &EngineConfig{ID: "copilot", FallbackModels: []string{"recovery", "last"}},
		ModelMappings: map[string][]string{"recovery": {"openai/secondary", "anthropic/backup"}},
	}
	require.Equal(t, []LLMProvider{LLMProviderGitHub, LLMProviderOpenAI, LLMProviderAnthropic}, fallbackModelProviders(data))
	compiler := NewCompiler()
	var models []string
	compiler.modelPricingResolver = func(_ context.Context, provider, model string) (map[string]float64, bool) {
		models = append(models, provider+"/"+model)
		return map[string]float64{"input": 1, "output": 2}, true
	}
	costs := compiler.resolveFallbackModelPricing(nil, data)
	require.Equal(t, []string{"openai/secondary", "anthropic/backup", "github-copilot/last"}, models)
	require.True(t, modelCostsHasPricingFor(costs, "anthropic", "backup"))
	require.True(t, modelCostsHasPricingFor(costs, "openai", "secondary"))
	require.True(t, modelCostsHasPricingFor(costs, "github-copilot", "last"))
}

func TestEngineFallbackAliasEnvOverrides(t *testing.T) {
	compiler := NewCompiler()
	compiler.strictMode = true
	frontmatter := map[string]any{
		"engine": map[string]any{
			"id": "copilot", "fallback-models": []any{"recovery"},
			"env": map[string]any{"ANTHROPIC_API_KEY": "${{ secrets.CUSTOM_KEY }}"},
		},
	}
	require.NoError(t, compiler.validateEarlyEnvSecrets(frontmatter))
	require.NoError(t, compiler.validateEnvSecretsWithModels(frontmatter, map[string][]string{"recovery": {"anthropic/secondary"}}))
	require.Error(t, compiler.validateEnvSecretsWithModels(frontmatter, nil))
	data := &WorkflowData{
		EngineConfig:       &EngineConfig{ID: "copilot", FallbackModels: []string{"gemini/secondary"}},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
	}
	require.ErrorContains(t, validateEngineFallbackModels(data), "same provider")
}

func TestCompileEngineFallbackProviders(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fallback.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
on: workflow_dispatch
permissions:
  contents: read
  copilot-requests: write
engine:
  id: copilot
  model: primary
  fallback-models: [openai/secondary, anthropic/last]
  env:
    ANTHROPIC_API_KEY: ${{ secrets.CUSTOM_ANTHROPIC_KEY }}
---
Say hello.
`), 0o600))
	require.ErrorContains(t, NewCompiler().CompileWorkflow(path), "same provider")
	_, err := os.Stat(filepath.Join(dir, "fallback.lock.yml"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestCompileEngineFallbackNative(t *testing.T) {
	for _, id := range []string{"copilot", "claude", "codex", "gemini", "pi"} {
		t.Run(id, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "fallback.md")
			require.NoError(t, os.WriteFile(path, []byte(`---
on: workflow_dispatch
permissions:
  contents: read
engine:
  id: `+id+`
  model: primary
  fallback-models: [secondary, last]
---
Say hello.
`), 0o600))
			require.NoError(t, NewCompiler().CompileWorkflow(path))
			lock, err := os.ReadFile(filepath.Join(dir, "fallback.lock.yml"))
			require.NoError(t, err)
			require.Contains(t, string(lock), `\"fallbackModels\":[\"secondary\",\"last\"]`)
			require.NotContains(t, string(lock), "GH_AW_FALLBACK_MODELS:")
			require.NotContains(t, string(lock), "GH_AW_NATIVE_FALLBACK_MODELS:")
		})
	}
}

func TestCodexHasNoImplicitFallbackModel(t *testing.T) {
	require.Empty(t, constants.CodexDefaultModel)
	for _, phase := range []struct {
		modelVar         string
		detection, evals bool
	}{
		{modelVar: constants.EnvVarModelAgentCodex},
		{modelVar: constants.EnvVarModelDetectionCodex, detection: true},
		{modelVar: constants.EnvVarModelEvalsCodex, evals: true},
	} {
		data := &WorkflowData{
			EngineConfig:   &EngineConfig{ID: "codex"},
			Model:          "${{ vars.POISON_PRIMARY }}",
			IsDetectionRun: phase.detection,
			IsEvalsRun:     phase.evals,
		}
		env := NewCodexEngine().buildCodexExecutionEnv(data, false, true, phase.modelVar)
		require.Equal(t, data.Model, env[phase.modelVar])
		require.Equal(t, "${{ vars."+phase.modelVar+" || vars.GH_AW_DEFAULT_MODEL_CODEX || '' }}", env[constants.EnvVarModelFallback])
	}
	require.Equal(t, "${{ vars.GH_AW_MODEL_EVALS_CODEX || vars.GH_AW_DEFAULT_MODEL_CODEX || '' }}", buildEvalsModelFallbackExpression("codex"))
}
