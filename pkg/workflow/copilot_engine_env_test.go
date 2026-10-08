//go:build !integration

package workflow

import (
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestCopilotEngineEnvOverridesTokenExpression(t *testing.T) {
	engine := NewCopilotEngine()

	t.Run("engine env overrides default token expression", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Env: map[string]string{
					"COPILOT_GITHUB_TOKEN": "${{ secrets.MY_ORG_COPILOT_TOKEN }}",
				},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		// engine.env override should replace the default token expression
		if !strings.Contains(stepContent, "COPILOT_GITHUB_TOKEN: ${{ secrets.MY_ORG_COPILOT_TOKEN }}") {
			t.Errorf("Expected engine.env to override COPILOT_GITHUB_TOKEN, got:\n%s", stepContent)
		}
		if strings.Contains(stepContent, "COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}") {
			t.Errorf("Default COPILOT_GITHUB_TOKEN expression should be replaced by engine.env override, got:\n%s", stepContent)
		}
	})

	t.Run("engine env adds extra environment variables", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Env: map[string]string{
					"CUSTOM_VAR": "custom-value",
				},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		if !strings.Contains(stepContent, "CUSTOM_VAR: custom-value") {
			t.Errorf("Expected engine.env to add CUSTOM_VAR, got:\n%s", stepContent)
		}
	})
}

// TestCopilotEngineBYOKOmitsCopilotGitHubToken verifies that COPILOT_GITHUB_TOKEN is
// NOT injected into the execution step env when BYOK mode is active
// (i.e. COPILOT_PROVIDER_BASE_URL is set in engine.env). Forwarding the GitHub identity
// token to a third-party provider would be a credential leak.
func TestCopilotEngineBYOKOmitsCopilotGitHubToken(t *testing.T) {
	engine := NewCopilotEngine()

	t.Run("COPILOT_GITHUB_TOKEN absent when COPILOT_PROVIDER_BASE_URL is set (BYOK)", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Env: map[string]string{
					constants.CopilotProviderBaseURL: "https://api.openai.com/v1",
					constants.CopilotProviderAPIKey:  "${{ secrets.PROVIDER_API_KEY }}",
				},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		// COPILOT_GITHUB_TOKEN must not appear at all — not even its default expression.
		if strings.Contains(stepContent, "COPILOT_GITHUB_TOKEN:") {
			t.Errorf("COPILOT_GITHUB_TOKEN should be absent in BYOK mode, got:\n%s", stepContent)
		}
	})

	t.Run("COPILOT_GITHUB_TOKEN absent with COPILOT_PROVIDER_BASE_URL only (no API key)", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Env: map[string]string{
					constants.CopilotProviderBaseURL: "http://localhost:11434/v1",
				},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		if strings.Contains(stepContent, "COPILOT_GITHUB_TOKEN:") {
			t.Errorf("COPILOT_GITHUB_TOKEN should be absent in BYOK mode (local provider), got:\n%s", stepContent)
		}
	})

	t.Run("COPILOT_GITHUB_TOKEN present when COPILOT_PROVIDER_BASE_URL is not set (standard mode)", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test-workflow",
			EngineConfig: &EngineConfig{},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		if !strings.Contains(stepContent, "COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}") {
			t.Errorf("COPILOT_GITHUB_TOKEN should be present in standard (non-BYOK) mode, got:\n%s", stepContent)
		}
	})

	t.Run("AWF command omits --exclude-env COPILOT_GITHUB_TOKEN in BYOK mode", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name: "test-workflow",
			EngineConfig: &EngineConfig{
				Env: map[string]string{
					constants.CopilotProviderBaseURL: "http://localhost:11434/v1",
				},
			},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{Type: SandboxTypeAWF},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}

		stepContent := strings.Join([]string(steps[0]), "\n")
		if strings.Contains(stepContent, "--exclude-env COPILOT_GITHUB_TOKEN") {
			t.Errorf("AWF command should not exclude COPILOT_GITHUB_TOKEN in BYOK mode, got:\n%s", stepContent)
		}
	})
}

func TestCopilotEngineSetsDummyAPIKey(t *testing.T) {
	engine := NewCopilotEngine()

	t.Run("COPILOT_API_KEY is set when AWF sandbox is enabled", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test-workflow",
			EngineConfig: &EngineConfig{ID: "copilot"},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{Type: SandboxTypeAWF},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}

		stepContent := strings.Join([]string(steps[0]), "\n")

		// COPILOT_DUMMY_BYOK holds the literal sentinel in the env: block (not *_API_KEY shaped).
		expectedDummyVar := constants.CopilotBYOKDummyAPIKeyEnvVar + ": " + constants.CopilotBYOKDummyAPIKey
		if !strings.Contains(stepContent, expectedDummyVar) {
			t.Errorf("Expected %s to be set in env: block when AWF sandbox is enabled, got:\n%s", constants.CopilotBYOKDummyAPIKeyEnvVar, stepContent)
		}

		// COPILOT_API_KEY must be exported via shell expansion in the run: script,
		// NOT as a literal in the env: block (GitHub Actions env: values are not shell-expanded).
		expectedExport := `export COPILOT_API_KEY="$` + constants.CopilotBYOKDummyAPIKeyEnvVar + `"`
		if !strings.Contains(stepContent, expectedExport) {
			t.Errorf("Expected run: script to contain %q for correct shell expansion, got:\n%s", expectedExport, stepContent)
		}

		// Sanity-check: COPILOT_API_KEY must NOT appear in the env: block as a key.
		// That would put a token-shaped literal next to an *_API_KEY key.
		if strings.Contains(stepContent, "          COPILOT_API_KEY:") {
			t.Errorf("COPILOT_API_KEY must not appear as an env: key; got:\n%s", stepContent)
		}

		if !strings.Contains(stepContent, "AWF_REFLECT_ENABLED: 1") {
			t.Errorf("Expected AWF_REFLECT_ENABLED to be set when AWF sandbox is enabled, got:\n%s", stepContent)
		}
	})

	t.Run("COPILOT_API_KEY is NOT set when sandbox.agent: false", func(t *testing.T) {
		workflowData := &WorkflowData{
			Name:         "test-workflow",
			EngineConfig: &EngineConfig{ID: "copilot"},
			SandboxConfig: &SandboxConfig{
				Agent: &AgentSandboxConfig{Disabled: true},
			},
		}

		steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
		if len(steps) != 1 {
			t.Fatalf("Expected 1 step, got %d", len(steps))
		}

		stepContent := strings.Join([]string(steps[0]), "\n")
		if strings.Contains(stepContent, "COPILOT_API_KEY") {
			t.Errorf("Expected COPILOT_API_KEY to be absent when sandbox.agent: false, got:\n%s", stepContent)
		}
		if strings.Contains(stepContent, constants.CopilotBYOKDummyAPIKeyEnvVar) {
			t.Errorf("Expected %s to be absent when sandbox.agent: false, got:\n%s", constants.CopilotBYOKDummyAPIKeyEnvVar, stepContent)
		}
		if strings.Contains(stepContent, "AWF_REFLECT_ENABLED") {
			t.Errorf("Expected AWF_REFLECT_ENABLED to be absent when sandbox.agent: false, got:\n%s", stepContent)
		}
	})
}

func TestCopilotEngineLLMProviderAnthropicAutoBYOK(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			LLMProvider: LLMProviderAnthropic,
		},
		NetworkPermissions: &NetworkPermissions{
			Firewall: &FirewallConfig{Enabled: true},
		},
		SandboxConfig: &SandboxConfig{
			Agent: &AgentSandboxConfig{ID: "awf"},
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}
	stepContent := strings.Join([]string(steps[0]), "\n")

	if !strings.Contains(stepContent, "GH_AW_LLM_PROVIDER: anthropic") {
		t.Errorf("Expected GH_AW_LLM_PROVIDER override, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "COPILOT_PROVIDER_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}") {
		t.Errorf("Expected COPILOT_PROVIDER_API_KEY derived from Anthropic secret, got:\n%s", stepContent)
	}
	expectedBaseURL := "COPILOT_PROVIDER_BASE_URL: http://api-proxy:" + strconv.Itoa(constants.ClaudeLLMGatewayPort)
	if !strings.Contains(stepContent, expectedBaseURL) {
		t.Errorf("Expected COPILOT_PROVIDER_BASE_URL for anthropic gateway, got:\n%s", stepContent)
	}
	if strings.Contains(stepContent, "COPILOT_GITHUB_TOKEN:") {
		t.Errorf("COPILOT_GITHUB_TOKEN should be omitted in auto-BYOK mode, got:\n%s", stepContent)
	}
}

func TestCopilotEngineLLMProviderOpenAIAutoBYOKUsesIsolatedGateway(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		EngineConfig: &EngineConfig{
			LLMProvider: LLMProviderOpenAI,
		},
		NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: true}},
		SandboxConfig:      &SandboxConfig{Agent: &AgentSandboxConfig{ID: "awf"}},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}
	stepContent := strings.Join([]string(steps[0]), "\n")
	if !strings.Contains(stepContent, "COPILOT_PROVIDER_BASE_URL: http://api-proxy:"+strconv.Itoa(constants.CodexLLMGatewayPort)) {
		t.Errorf("Expected isolated OpenAI gateway URL, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "COPILOT_PROVIDER_API_KEY: ${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}") {
		t.Errorf("Expected OpenAI provider credentials to be preserved, got:\n%s", stepContent)
	}
}

// TestCopilotEngineForwardsSafeOutputsInputEnvVars verifies that GH_AW_INPUT_* variables
// extracted from the safe-outputs config are included in the Copilot execution step env.
// These vars must reach the agent step so that the TOML env_vars forwarding chain
// (runner step → AWF sandbox → Copilot CLI → safe-outputs container) can resolve
// ${GH_AW_INPUT_…} placeholders in config.json. Without this, a dynamic target-repo
// like "${{ inputs.owner }}/${{ inputs.repo }}" stays unexpanded in the patch filename,
// causing a mismatch with the consumer's lookup and silently dropping the PR.
func TestCopilotEngineForwardsSafeOutputsInputEnvVars(t *testing.T) {
	engine := NewCopilotEngine()
	workflowData := &WorkflowData{
		Name: "test-workflow",
		SafeOutputs: &SafeOutputsConfig{
			CreatePullRequests: &CreatePullRequestsConfig{
				BaseSafeOutputConfig: BaseSafeOutputConfig{Max: strPtr("1")},
				TargetRepoSlug:       "${{ inputs.owner }}/${{ inputs.repo }}",
			},
		},
		SafeOutputsInputEnvVars: map[string]string{
			"GH_AW_INPUT_OWNER": "${{ inputs.owner }}",
			"GH_AW_INPUT_REPO":  "${{ inputs.repo }}",
		},
	}

	steps := engine.GetExecutionSteps(workflowData, "/tmp/gh-aw/test.log")
	if len(steps) != 1 {
		t.Fatalf("Expected 1 execution step, got %d", len(steps))
	}
	stepContent := strings.Join([]string(steps[0]), "\n")

	if !strings.Contains(stepContent, "GH_AW_INPUT_OWNER: ${{ inputs.owner }}") {
		t.Errorf("Expected GH_AW_INPUT_OWNER in step env for TOML env_vars forwarding, got:\n%s", stepContent)
	}
	if !strings.Contains(stepContent, "GH_AW_INPUT_REPO: ${{ inputs.repo }}") {
		t.Errorf("Expected GH_AW_INPUT_REPO in step env for TOML env_vars forwarding, got:\n%s", stepContent)
	}
}
