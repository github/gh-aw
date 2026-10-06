//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCopilotProviderResolution(t *testing.T) {
	engine := NewClaudeEngine()
	for _, test := range []struct {
		name     string
		data     *WorkflowData
		provider LLMProvider
	}{
		{"nil", nil, LLMProviderAnthropic},
		{"default", &WorkflowData{Model: "claude-sonnet-4-6"}, LLMProviderAnthropic},
		{"copilot", &WorkflowData{Model: "copilot/claude-haiku-4.5"}, LLMProviderGitHub},
		{"expression", &WorkflowData{Model: "copilot/${{ inputs.model }}"}, LLMProviderGitHub},
		{"case and whitespace", &WorkflowData{Model: " COPILOT/claude-sonnet-4.6 "}, LLMProviderGitHub},
		{"explicit provider wins", &WorkflowData{
			Model: "copilot/claude-sonnet-4.6", EngineConfig: &EngineConfig{LLMProvider: LLMProviderAnthropic},
		}, LLMProviderAnthropic},
		{"explicit GitHub alias", &WorkflowData{
			Model: "claude-haiku-4.5", EngineConfig: &EngineConfig{LLMProvider: "copilot"},
		}, LLMProviderGitHub},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.provider, engine.ResolveLLMProvider(test.data))
		})
	}
}

func TestClaudeCopilotCredentialsAndModel(t *testing.T) {
	engine := NewClaudeEngine()
	for _, permission := range []string{"", "copilot-requests: write"} {
		t.Run(permission, func(t *testing.T) {
			data := &WorkflowData{
				Model: "copilot/claude-haiku-4.5", Permissions: permission,
				EngineConfig:       &EngineConfig{ID: "claude"},
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
			}
			env := engine.buildClaudeCommandEnv(data)
			assert.Equal(t, "github", env["GH_AW_LLM_PROVIDER"])
			assert.Equal(t, "claude-haiku-4.5", env["ANTHROPIC_MODEL"])
			assert.NotContains(t, env, "ANTHROPIC_API_KEY")
			if permission == "" {
				assert.Equal(t, "${{ secrets.COPILOT_GITHUB_TOKEN }}", env["COPILOT_GITHUB_TOKEN"])
				assert.Contains(t, engine.GetRequiredSecretNames(data), "COPILOT_GITHUB_TOKEN")
				assert.NotEmpty(t, engine.GetSecretValidationStep(data))
			} else {
				assert.Equal(t, "${{ github.token }}", env["COPILOT_GITHUB_TOKEN"])
				assert.NotContains(t, engine.GetRequiredSecretNames(data), "COPILOT_GITHUB_TOKEN")
				assert.Empty(t, engine.GetSecretValidationStep(data))
			}
			step := strings.Join(engine.GetExecutionSteps(data, "test.log")[0], "\n")
			assert.Contains(t, step, `export ANTHROPIC_API_KEY="$COPILOT_DUMMY_BYOK"`)
			assert.Contains(t, step, "--exclude-env COPILOT_GITHUB_TOKEN")
			assert.Contains(t, step, "--exclude-env ANTHROPIC_API_KEY")
			assert.NotContains(t, step, "secrets.ANTHROPIC_API_KEY")
			assert.Contains(t, getEngineAPIHosts(data, engine), "api.githubcopilot.com")
		})
	}
}

func TestClaudeCopilotNativeModelIDs(t *testing.T) {
	for _, test := range []struct{ input, expected string }{
		{"copilot/claude-sonnet-4.6", "claude-sonnet-4.6"},
		{"copilot/${{ inputs.model }}", "${{ inputs.model }}"},
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"anthropic/custom-model", "anthropic/custom-model"},
		{"copilot/claude-haiku-4.5?effort=low", "claude-haiku-4.5?effort=low"},
	} {
		t.Run(test.input, func(t *testing.T) {
			env := map[string]string{}
			applyClaudeModelEnvVars(env, &WorkflowData{Model: test.input})
			assert.Equal(t, test.expected, env["ANTHROPIC_MODEL"])
		})
	}
}

func TestClaudeCopilotRequiresSandbox(t *testing.T) {
	data := &WorkflowData{
		Model:        "copilot/claude-haiku-4.5",
		EngineConfig: &EngineConfig{ID: "claude"},
		Features:     map[string]any{"dangerously-disable-sandbox-agent": true},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{Disabled: true},
		},
	}
	require.ErrorContains(t, validateSandboxConfig(data), "requires the agent sandbox")
	data.EngineConfig.LLMProvider = LLMProviderAnthropic
	require.NoError(t, validateSandboxConfig(data))
}

func TestClaudeCopilotCompilationIncludesDetectionRouting(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(map[bool]string{false: "inline", true: "external"}[external], func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetSkipValidation(true)
			workflowPath := filepath.Join(t.TempDir(), "claude-copilot.md")
			source := "---\non: push\nengine: claude\nmodel: copilot/claude-haiku-4.5\npermissions:\n  copilot-requests: write\nsafe-outputs:\n  create-issue:\n"
			if external {
				source += "features:\n  gh-aw-detection: true\n"
			}
			source += "---\nSynthetic Claude Copilot fixture."
			require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0600))
			require.NoError(t, compiler.CompileWorkflow(workflowPath))
			lock, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
			require.NoError(t, err)
			for _, job := range []string{"agent", "detection"} {
				section := extractJobSection(string(lock), job)
				require.NotEmpty(t, section)
				assert.Contains(t, section, "GH_AW_LLM_PROVIDER: github")
				assert.Contains(t, section, "COPILOT_GITHUB_TOKEN: ${{ github.token }}")
				assert.Contains(t, section, "ANTHROPIC_MODEL: claude-haiku-4.5")
				assert.Contains(t, section, `export ANTHROPIC_API_KEY="$`+constants.CopilotBYOKDummyAPIKeyEnvVar+`"`)
				assert.Contains(t, section, "--exclude-env COPILOT_GITHUB_TOKEN")
				assert.NotContains(t, section, "secrets.ANTHROPIC_API_KEY")
			}
			assert.NotContains(t, string(lock), "Validate COPILOT_GITHUB_TOKEN")
			assert.NotContains(t, string(lock), "secrets.COPILOT_GITHUB_TOKEN")
		})
	}
}
