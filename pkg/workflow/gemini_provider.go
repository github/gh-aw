package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func validateGeminiProvider(data *WorkflowData) error {
	if data == nil || ResolveEngineID(data) != string(constants.GeminiEngine) {
		return nil
	}
	provider := NewGeminiEngine().ResolveLLMProvider(data)
	if provider != LLMProviderGoogle && provider != LLMProviderGitHub {
		return NewValidationError("engine.model-provider", string(provider),
			"Gemini supports Google or GitHub Copilot inference",
			"Use google or github, or select a copilot/gemini* model.")
	}
	if provider != LLMProviderGitHub {
		return nil
	}
	if !isFirewallEnabled(data) {
		return NewValidationError("sandbox.agent", "false",
			"Gemini with GitHub Copilot inference requires the AWF sandbox",
			"Enable the agent sandbox to preserve credential-isolated Copilot routing.")
	}
	if isGeminiVertexWIF(data) {
		return NewValidationError("engine.auth", "github-oidc",
			"Google Workload Identity Federation cannot authenticate GitHub Copilot inference",
			"Remove engine.auth and use permissions: { copilot-requests: write } or COPILOT_GITHUB_TOKEN.")
	}
	model := geminiModelID(data.Model)
	if !containsExpression(model) && !strings.HasPrefix(strings.ToLower(model), "gemini") {
		return NewValidationError("engine.model", data.Model,
			"Gemini with GitHub Copilot inference requires a Gemini model",
			"Select a Copilot Gemini model such as copilot/gemini-3.8-flash.")
	}
	return nil
}
