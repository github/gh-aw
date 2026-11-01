//go:build !integration

package workflow

import (
	"strconv"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestUniversalLLMConsumerEngine_CopilotCredentialIsolation(t *testing.T) {
	engine := &UniversalLLMConsumerEngine{}
	for _, useCopilotRequests := range []bool{false, true} {
		for _, firewallEnabled := range []bool{false, true} {
			t.Run(strconv.FormatBool(useCopilotRequests)+"/"+strconv.FormatBool(firewallEnabled), func(t *testing.T) {
				data := &WorkflowData{Model: "copilot/gpt-5", EngineConfig: &EngineConfig{}}
				expectedToken := "${{ secrets.COPILOT_GITHUB_TOKEN }}"
				expectedSecrets := []string{"COPILOT_GITHUB_TOKEN"}
				if useCopilotRequests {
					data.Permissions = "copilot-requests: write"
					expectedToken = "${{ github.token }}"
					expectedSecrets = []string{}
				}
				env := map[string]string{}
				engine.ApplyUniversalProviderEnv(env, data, firewallEnabled)
				assert.Equal(t, expectedToken, env["COPILOT_GITHUB_TOKEN"])
				assert.NotContains(t, env, "OPENAI_API_KEY")
				assert.NotContains(t, env, "CODEX_API_KEY")
				assert.ElementsMatch(t, expectedSecrets, engine.GetUniversalRequiredSecretNames(data))
				if firewallEnabled {
					assert.Equal(t, env["GITHUB_COPILOT_BASE_URL"], env["OPENAI_BASE_URL"])
				} else {
					assert.NotContains(t, env, "OPENAI_BASE_URL")
				}
			})
		}
	}
}

func TestUniversalLLMConsumerEngine_NonCopilotCredentials(t *testing.T) {
	engine := &UniversalLLMConsumerEngine{}
	for _, model := range []string{"anthropic/claude-sonnet-4.6", "openai/gpt-5", "codex/gpt-5"} {
		t.Run(model, func(t *testing.T) {
			env := map[string]string{}
			engine.ApplyUniversalProviderEnv(env, &WorkflowData{Model: model, EngineConfig: &EngineConfig{}}, true)
			assert.NotContains(t, env, "COPILOT_GITHUB_TOKEN")
			if model == "anthropic/claude-sonnet-4.6" {
				assert.Equal(t, "${{ secrets.ANTHROPIC_API_KEY }}", env["ANTHROPIC_API_KEY"])
				assert.NotContains(t, env, "OPENAI_API_KEY")
			} else {
				assert.Equal(t, "${{ secrets.CODEX_API_KEY || secrets.OPENAI_API_KEY }}", env["OPENAI_API_KEY"])
				assert.Equal(t, env["OPENAI_API_KEY"], env["CODEX_API_KEY"])
			}
		})
	}
}

func TestUniversalLLMConsumerEngine_BackendResolution(t *testing.T) {
	for _, tc := range []struct {
		model   string
		backend UniversalLLMBackend
	}{
		{" copilot / org/model ", UniversalLLMBackendCopilot},
		{"ANTHROPIC/model", UniversalLLMBackendAnthropic},
		{"openai/org/model", UniversalLLMBackendCodex},
		{"codex/model", UniversalLLMBackendCodex},
		{"", ""},
		{"copilot", ""},
		{"/model", ""},
		{"copilot/ ", ""},
		{"unknown/model", ""},
	} {
		t.Run(tc.model, func(t *testing.T) {
			backend, err := resolveUniversalLLMBackendFromModel(tc.model)
			assert.Equal(t, tc.backend, backend)
			if tc.backend == "" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
	aliases := map[string]UniversalLLMBackend{"github-copilot": UniversalLLMBackendCopilot}
	backend, err := resolveBackendWithAliases(" GITHUB-COPILOT / org/model ", aliases)
	require.NoError(t, err)
	assert.Equal(t, UniversalLLMBackendCopilot, backend)
	backend, err = resolveBackendWithAliases("anthropic/model", aliases)
	require.NoError(t, err)
	assert.Equal(t, UniversalLLMBackendAnthropic, backend)
	_, err = resolveBackendWithAliases("github-copilot", aliases)
	require.Error(t, err)
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
