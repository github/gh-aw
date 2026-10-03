//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
)

func TestPiProviderOverridesSelectOneConsistentProfile(t *testing.T) {
	tests := []struct {
		model            string
		override         LLMProvider
		backend          UniversalLLMBackend
		secret, provider string
		port             int
	}{
		{"", LLMProviderOpenAI, UniversalLLMBackendCodex, "CODEX_API_KEY", "openai", constants.CodexLLMGatewayPort},
		{"anthropic/claude-sonnet-4", LLMProviderGitHub, UniversalLLMBackendCopilot, "COPILOT_GITHUB_TOKEN", "github-copilot", constants.CopilotLLMGatewayPort},
		{"google/gemini-2.5-pro", LLMProviderOpenAI, UniversalLLMBackendCodex, "CODEX_API_KEY", "openai", constants.CodexLLMGatewayPort},
		{"openai/gpt-5.4", "google", piBackendGoogle, "GEMINI_API_KEY", "google", constants.GeminiLLMGatewayPort},
	}
	for _, test := range tests {
		t.Run(test.model+"/"+test.override.String(), func(t *testing.T) {
			for _, firewall := range []bool{false, true} {
				data := &WorkflowData{Model: test.model, EngineConfig: &EngineConfig{ID: "pi", LLMProvider: test.override}, NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: firewall}}}
				assert.Equal(t, test.backend, resolvePiBackend(data))
				profile := piProviderProfile(data)
				assert.Contains(t, profile.coreSecretNames, test.secret)
				assert.Equal(t, test.port, profile.gatewayPort)
				assert.Equal(t, test.provider, piExecutionProvider(data))
			}
		})
	}
}
