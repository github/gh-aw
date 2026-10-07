//go:build !integration

package workflow

import (
	"os"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiCopilotProvider(t *testing.T) {
	engine := NewGeminiEngine()
	for _, permissions := range []string{"", "permissions:\n  copilot-requests: write"} {
		t.Run(permissions, func(t *testing.T) {
			data := &WorkflowData{
				Name:         "test",
				Model:        "copilot/gemini-3.8-flash",
				EngineConfig: &EngineConfig{ID: "gemini"},
				Permissions:  permissions,
				NetworkPermissions: &NetworkPermissions{
					Firewall: &FirewallConfig{Enabled: true},
				},
			}
			require.NoError(t, validateGeminiProvider(data))
			assert.Equal(t, LLMProviderGitHub, engine.ResolveLLMProvider(data))
			steps := engine.GetExecutionSteps(data, "test.log")
			require.Len(t, steps, 2)
			step := strings.Join(steps[1], "\n")
			assert.Contains(t, step, "gemini_copilot.cjs gemini --yolo --skip-trust")
			assert.Contains(t, step, "GH_AW_GEMINI_COPILOT_MODEL: gemini-3.8-flash")
			assert.Contains(t, step, "GEMINI_MODEL: gemini-3.8-flash")
			assert.Contains(t, step, "--exclude-env COPILOT_GITHUB_TOKEN")
			assert.NotContains(t, step, "GEMINI_API_KEY:")
			assert.NotContains(t, step, `\"gemini\":`)
			assert.Equal(t, getEngineAPIHosts(data, NewCopilotEngine()), getEngineAPIHosts(data, engine))
			if permissions == "" {
				assert.Contains(t, step, "COPILOT_GITHUB_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN }}")
				assert.Equal(t, []string{"COPILOT_GITHUB_TOKEN"}, engine.GetRequiredSecretNames(data))
				assert.Contains(t, strings.Join(engine.GetSecretValidationStep(data), "\n"), "COPILOT_GITHUB_TOKEN")
			} else {
				assert.Contains(t, step, "COPILOT_GITHUB_TOKEN: ${{ github.token }}")
				assert.Empty(t, engine.GetRequiredSecretNames(data))
				assert.Empty(t, engine.GetSecretValidationStep(data))
			}
		})
	}
}

func TestGeminiProviderResolution(t *testing.T) {
	for _, test := range []struct {
		model    string
		explicit LLMProvider
		expected LLMProvider
		id       string
	}{
		{"gemini-3.8-flash", "", LLMProviderGoogle, "gemini-3.8-flash"},
		{"google/gemini-3.8-flash", "", LLMProviderGoogle, "gemini-3.8-flash"},
		{" COPILOT/gemini-3.8-flash ", "", LLMProviderGitHub, "gemini-3.8-flash"},
		{"copilot/gemini-3.8-flash", LLMProviderGoogle, LLMProviderGoogle, "gemini-3.8-flash"},
		{"gemini-3.8-flash", LLMProviderGitHub, LLMProviderGitHub, "gemini-3.8-flash"},
	} {
		t.Run(test.model+string(test.explicit), func(t *testing.T) {
			data := &WorkflowData{Model: test.model, EngineConfig: &EngineConfig{ID: "gemini", LLMProvider: test.explicit}}
			assert.Equal(t, test.expected, NewGeminiEngine().ResolveLLMProvider(data))
			assert.Equal(t, test.id, geminiModelID(test.model))
		})
	}
}

func TestGeminiProviderValidation(t *testing.T) {
	for _, test := range []struct {
		name     string
		model    string
		provider LLMProvider
		firewall bool
		wif      bool
		error    string
	}{
		{"google without sandbox", "gemini-3.8-flash", "", false, false, ""},
		{"copilot requires sandbox", "copilot/gemini-3.8-flash", "", false, false, "requires the AWF sandbox"},
		{"wrong Copilot model", "copilot/gpt-5", "", true, false, "requires a Gemini model"},
		{"unsupported provider", "gemini-3.8-flash", LLMProviderOpenAI, true, false, "Google or GitHub Copilot"},
		{"conflicting WIF", "copilot/gemini-3.8-flash", "", true, true, "cannot authenticate GitHub Copilot"},
		{"expression with explicit provider", "${{ inputs.model }}", LLMProviderGitHub, true, false, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := &WorkflowData{
				Model: test.model, EngineConfig: &EngineConfig{ID: "gemini", LLMProvider: test.provider},
				NetworkPermissions: &NetworkPermissions{Firewall: &FirewallConfig{Enabled: test.firewall}},
			}
			if test.wif {
				data.EngineConfig.Auth = &EngineAuthConfig{
					Type: "github-oidc", Provider: "gcp",
					GoogleWorkloadIdentityProvider: "provider", GoogleServiceAccount: "service", GoogleProject: "project",
				}
			}
			err := validateGeminiProvider(data)
			if test.error == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, test.error)
			}
		})
	}
}

func TestGeminiSmokeWorkflowUsesSupportedTools(t *testing.T) {
	source, err := os.ReadFile("../../.github/workflows/smoke-gemini.md")
	require.NoError(t, err)
	content := string(source)
	assert.Contains(t, content, "copilot-requests: write")
	assert.Contains(t, content, "model: copilot/gemini-3.8-flash")
	assert.Contains(t, content, "    - go\n")
	assert.Contains(t, content, "Execute all 5 tests sequentially")
	assert.Contains(t, content, "native `web_fetch` tool")
	assert.NotContains(t, content, "`task`")
	assert.NotContains(t, content, "`read_agent`")
	assert.NotContains(t, content, "experiments:")
	assert.NotContains(t, content, "{{#if")
	assert.Equal(t, "0.63.0", constants.DefaultGeminiVersion.String())
}
