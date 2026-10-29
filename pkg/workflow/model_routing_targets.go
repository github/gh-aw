// This file validates the provider and effective inference target of
// engine.model-routing. Routing runs on GitHub Copilot, or on the native provider of
// an engine that has one (Claude on Anthropic, Codex on OpenAI). AWF routes natively
// only over the matching provider's native HTTPS endpoint: it does not switch
// credentials, translate between providers, or route through custom gateways.

package workflow

import (
	"fmt"
	"strings"
)

// modelRoutingNativeTarget describes the native endpoint used by native routing and
// the frontmatter that can move inference away from it.
type modelRoutingNativeTarget struct {
	host       string
	baseURLEnv string
	keyName    string
}

var modelRoutingNativeTargets = map[LLMProvider]modelRoutingNativeTarget{
	LLMProviderAnthropic: {host: "api.anthropic.com", baseURLEnv: "ANTHROPIC_BASE_URL", keyName: "ANTHROPIC_API_KEY"},
	LLMProviderOpenAI:    {host: "api.openai.com", baseURLEnv: "OPENAI_BASE_URL", keyName: "CODEX_API_KEY or OPENAI_API_KEY"},
}

// validateModelRoutingProviderTarget checks that routing runs on GitHub Copilot or on
// the engine's native provider, that the engine's credentials resolve to the same
// provider, and that native routing is not redirected to a custom target.
func validateModelRoutingProviderTarget(data *WorkflowData, engineName string) error {
	engineName = strings.ToLower(engineName)
	provider := modelRoutingProvider(engineName, data)
	native, hasNative := modelRoutingNativeLLMProviders[engineName]
	if provider != LLMProviderGitHub && provider != native {
		if !hasNative {
			return NewValidationError("engine.model-provider", string(provider),
				fmt.Sprintf("task-level model routing requires GitHub Copilot inference, but the %s engine resolves to the %q provider", engineName, provider),
				"Set engine.model-provider: github, or remove engine.model-routing.")
		}
		return NewValidationError("engine.model-provider", string(provider),
			fmt.Sprintf("task-level model routing for the %s engine runs only on GitHub Copilot or the engine's native %q provider, but the %s engine resolves to the %q provider; AWF native routing does not cross providers", engineName, native, engineName, provider),
			fmt.Sprintf("Set engine.model-provider: %s (native, with %s) or github, or remove engine.model-routing.", native, modelRoutingNativeTargets[native].keyName))
	}
	if !hasNative {
		return nil
	}
	if engineProvider := modelRoutingEngineCredentialProvider(engineName, data); engineProvider != provider {
		return NewValidationError("engine.model-provider", string(provider),
			fmt.Sprintf("task-level model routing for the %s engine resolves to the %q provider, but the engine's credentials resolve to the %q provider from model %q, which routing ignores", engineName, provider, engineProvider, data.Model),
			fmt.Sprintf("Set engine.model-provider: %s or engine.model-provider: %s explicitly, or remove engine.model-routing.", engineProvider, provider))
	}
	if provider != native {
		return nil
	}
	if source, value := modelRoutingCustomNativeTarget(data, native); source != "" {
		target := modelRoutingNativeTargets[native]
		return NewValidationError(source, value,
			fmt.Sprintf("task-level model routing on the native %q provider requires its native HTTPS endpoint (https://%s), but %s sets a custom target; AWF native routing does not support custom targets, BYOK gateways, or Azure OpenAI", native, target.host, source),
			fmt.Sprintf("Remove %s so the %s engine uses https://%s, use GitHub Copilot inference (engine.model-provider: github), or remove engine.model-routing.", source, engineName, target.host))
	}
	return nil
}

// modelRoutingEngineCredentialProvider returns the provider whose credentials the
// engine's execution step uses; a copilot/ fixed model can select GitHub Copilot.
func modelRoutingEngineCredentialProvider(engineName string, data *WorkflowData) LLMProvider {
	switch engineName {
	case "claude":
		return NewClaudeEngine().ResolveLLMProvider(data)
	case "codex":
		return NewCodexEngine().ResolveLLMProvider(data)
	default:
		return modelRoutingProvider(engineName, data)
	}
}

// modelRoutingCustomNativeTarget returns the frontmatter field (and its value) that
// moves native inference away from the provider's native HTTPS endpoint, or "".
func modelRoutingCustomNativeTarget(data *WorkflowData, provider LLMProvider) (string, string) {
	if data == nil || data.EngineConfig == nil {
		return "", ""
	}
	target := modelRoutingNativeTargets[provider]
	if apiTarget := strings.TrimSpace(data.EngineConfig.APITarget); apiTarget != "" && !strings.EqualFold(apiTarget, target.host) {
		return "engine.api-target", apiTarget
	}
	if engineEnvHasNonEmptyValue(data, target.baseURLEnv) {
		baseURL := strings.TrimSpace(data.EngineConfig.Env[target.baseURLEnv])
		if !isModelRoutingNativeBaseURL(baseURL, target.host) {
			return "engine.env." + target.baseURLEnv, baseURL
		}
	}
	if data.SandboxConfig != nil && data.SandboxConfig.Agent != nil && data.SandboxConfig.Agent.Targets[string(provider)] != nil {
		return "sandbox.agent.targets." + string(provider), ""
	}
	// engine.auth for another cloud (Azure OpenAI or GCP) routes inference to that
	// cloud's endpoint rather than the native provider's.
	if auth := data.EngineConfig.Auth; auth != nil && auth.Provider != "" && !strings.EqualFold(auth.Provider, string(provider)) {
		return "engine.auth", auth.Provider
	}
	return "", ""
}

// isModelRoutingNativeBaseURL reports whether baseURL is the provider's native HTTPS
// endpoint (optionally with its /v1 path).
func isModelRoutingNativeBaseURL(baseURL, host string) bool {
	rest, ok := strings.CutPrefix(strings.ToLower(strings.TrimRight(baseURL, "/")), "https://")
	if !ok {
		return false
	}
	pathPart, hasVersion := strings.CutPrefix(rest, host)
	return hasVersion && (pathPart == "" || pathPart == "/v1")
}
