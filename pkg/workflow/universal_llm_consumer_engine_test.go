//go:build !integration

package workflow

import (
	"strconv"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestUniversalLLMConsumerEngine_GetUniversalRequiredSecretNames_NilWorkflowData(t *testing.T) {
	engine := &UniversalLLMConsumerEngine{}

	assert.NotPanics(t, func() {
		secrets := engine.GetUniversalRequiredSecretNames(nil)
		assert.ElementsMatch(t, []string{"COPILOT_GITHUB_TOKEN"}, secrets, "Nil workflow data should safely fall back to only the copilot backend secret profile")
	}, "GetUniversalRequiredSecretNames should handle nil workflowData safely")
}

func TestUniversalLLMConsumerEngine_ApplyUniversalProviderEnv_SetsProvider(t *testing.T) {
	engine := &UniversalLLMConsumerEngine{}
	tests := []struct {
		model    string
		provider string
	}{
		{model: "copilot/gpt-5", provider: "github"},
		{model: "anthropic/claude-sonnet-4.6", provider: "anthropic"},
		{model: "openai/gpt-5", provider: "openai"},
		{model: "codex/gpt-5", provider: "openai"},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			env := map[string]string{}
			engine.ApplyUniversalProviderEnv(env, &WorkflowData{
				Model:        tt.model,
				EngineConfig: &EngineConfig{},
			}, true)
			assert.Equal(t, tt.provider, env["GH_AW_LLM_PROVIDER"])
		})
	}
}

func TestUniversalLLMConsumerEngine_ProviderURLsFollowNetworkTopology(t *testing.T) {
	engine := &UniversalLLMConsumerEngine{}
	for _, tc := range []struct {
		name        string
		model       string
		runtime     AgentRuntime
		expectedURL string
	}{
		{
			name:        "isolated Copilot",
			model:       "copilot/gpt-5",
			expectedURL: "http://api-proxy:" + strconv.Itoa(constants.CopilotLLMGatewayPort),
		},
		{
			name:        "isolated Anthropic",
			model:       "anthropic/claude-sonnet-4.6",
			expectedURL: "http://api-proxy:" + strconv.Itoa(constants.ClaudeLLMGatewayPort),
		},
		{
			name:        "isolated OpenAI",
			model:       "openai/gpt-5",
			expectedURL: "http://api-proxy:" + strconv.Itoa(constants.CodexLLMGatewayPort),
		},
		{
			name:        "host-access runtime",
			model:       "anthropic/claude-sonnet-4.6",
			runtime:     AgentRuntimeDockerSudoIptables,
			expectedURL: "http://host.docker.internal:" + strconv.Itoa(constants.ClaudeLLMGatewayPort),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{}
			engine.ApplyUniversalProviderEnv(env, &WorkflowData{
				Model:        tc.model,
				EngineConfig: &EngineConfig{},
				SandboxConfig: &SandboxConfig{
					Agent: &AgentSandboxConfig{ID: "awf", Runtime: tc.runtime},
				},
			}, true)

			assert.Contains(t, env, "GH_AW_LLM_PROVIDER")
			for _, key := range []string{"ANTHROPIC_BASE_URL", "OPENAI_BASE_URL", "GITHUB_COPILOT_BASE_URL"} {
				if value, ok := env[key]; ok {
					assert.Equal(t, tc.expectedURL, value)
				}
			}
			assert.Contains(t, []string{env["ANTHROPIC_BASE_URL"], env["OPENAI_BASE_URL"], env["GITHUB_COPILOT_BASE_URL"]}, tc.expectedURL)
		})
	}
}
