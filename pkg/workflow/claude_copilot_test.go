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
		{"copilot auto", &WorkflowData{Model: "copilot/auto"}, LLMProviderGitHub},
		{"native auto", &WorkflowData{Model: "auto"}, LLMProviderAnthropic},
		{"bare Copilot auto", &WorkflowData{Model: "auto", EngineConfig: &EngineConfig{LLMProvider: LLMProviderGitHub}}, LLMProviderGitHub},
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
		{"auto", "auto"},
		{"copilot/auto", "auto"},
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

func TestCopilotAutoModelCompilation(t *testing.T) {
	for _, engine := range []string{"claude", "codex"} {
		for _, model := range []string{"auto", "copilot/auto"} {
			t.Run(engine+"/"+model, func(t *testing.T) {
				compiler := NewCompiler()
				compiler.SetSkipValidation(true)
				workflowPath := filepath.Join(t.TempDir(), "copilot-auto.md")
				source := "---\non: workflow_dispatch\nengine:\n  id: " + engine + "\n  model-provider: github\nmodel: " + model + "\npermissions:\n  contents: read\n  copilot-requests: write\ncheckout: false\ntools:\n  github: false\nsafe-outputs:\n  noop:\n  threat-detection: false\n---\nCall noop once."
				require.NoError(t, os.WriteFile(workflowPath, []byte(source), 0600))
				require.NoError(t, compiler.CompileWorkflow(workflowPath))
				lock, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
				require.NoError(t, err)
				agent := extractJobSection(string(lock), "agent")
				assert.Contains(t, agent, "GH_AW_LLM_PROVIDER: github")
				assert.Contains(t, agent, "COPILOT_GITHUB_TOKEN: ${{ github.token }}")
				assert.NotContains(t, agent, "secrets.ANTHROPIC_API_KEY")
				assert.NotContains(t, agent, "secrets.OPENAI_API_KEY")
				if engine == "claude" {
					assert.Contains(t, agent, "ANTHROPIC_MODEL: auto")
				} else {
					assert.Contains(t, agent, "GH_AW_MODEL_AGENT_CODEX: auto")
					assert.Zero(t, compiler.GetWarningCount())
				}
			})
		}
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

func TestCopilotRequiresSandboxForLegacyEngine(t *testing.T) {
	for _, engine := range []string{"claude", "codex"} {
		t.Run(engine, func(t *testing.T) {
			data := &WorkflowData{
				AI:       engine,
				Model:    "copilot/claude-haiku-4.5",
				Features: map[string]any{"dangerously-disable-sandbox-agent": true},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{Disabled: true},
				},
			}
			require.ErrorContains(t, validateSandboxConfig(data), "requires the agent sandbox")
			data.EngineConfig = &EngineConfig{ID: engine, LLMProvider: LLMProviderAnthropic}
			require.NoError(t, validateSandboxConfig(data))
		})
	}
}

func TestClaudeCopilotSmokeCompilation(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "smoke-claude-copilot.md"))
	require.NoError(t, err)
	workflowPath := filepath.Join(t.TempDir(), "smoke-claude-copilot.md")
	require.NoError(t, os.WriteFile(workflowPath, source, 0600))
	compiler := NewCompiler()
	compiler.SetSkipValidation(true)
	require.NoError(t, compiler.CompileWorkflow(workflowPath))
	lock, err := os.ReadFile(stringutil.MarkdownToLockFile(workflowPath))
	require.NoError(t, err)
	agent := extractJobSection(string(lock), "agent")
	assert.Contains(t, agent, "awf --")
	assert.Contains(t, agent, `export ANTHROPIC_API_KEY="$COPILOT_DUMMY_BYOK"`)
	assert.Contains(t, agent, `\"modelFallback\":{\"enabled\":false}`)
	assert.Contains(t, agent, "crypto.randomBytes(32)")
	assert.Contains(t, agent, `flag: "wx"`)
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
