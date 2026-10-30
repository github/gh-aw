package workflow

import (
	"fmt"
	"slices"
)

type decisionProviderEndpoint struct {
	envKey       string
	overrideKeys []string
	defaultURL   string
}

func decisionEndpoint(provider LLMProvider) decisionProviderEndpoint {
	switch provider {
	case LLMProviderGitHub:
		return decisionProviderEndpoint{envKey: "GITHUB_COPILOT_BASE_URL", overrideKeys: []string{"GITHUB_COPILOT_BASE_URL"}, defaultURL: "https://api.githubcopilot.com"}
	case LLMProviderAnthropic:
		return decisionProviderEndpoint{envKey: "ANTHROPIC_BASE_URL", overrideKeys: []string{"ANTHROPIC_BASE_URL"}, defaultURL: "https://api.anthropic.com"}
	case LLMProviderOpenAI:
		return decisionProviderEndpoint{envKey: "OPENAI_BASE_URL", overrideKeys: []string{"OPENAI_BASE_URL"}, defaultURL: "https://api.openai.com/v1"}
	case "gemini":
		return decisionProviderEndpoint{envKey: "GOOGLE_GEMINI_BASE_URL", overrideKeys: []string{"GOOGLE_GEMINI_BASE_URL", "GEMINI_API_BASE_URL"}, defaultURL: "https://generativelanguage.googleapis.com/v1beta"}
	default:
		return decisionProviderEndpoint{}
	}
}

func validateDecisionProviderEndpoints(data *WorkflowData) error {
	if data == nil || data.Decision == nil {
		return nil
	}
	overrides := getEngineEnvOverrides(data)
	for _, provider := range decisionProviders(data) {
		endpoint := decisionEndpoint(provider)
		for _, key := range endpoint.overrideKeys {
			if value, ok := overrides[key]; ok && containsExpression(value) {
				return fmt.Errorf("on.decision provider endpoint engine.env.%s must be a literal URL; Actions expressions could route credentials to an event-controlled endpoint", key)
			}
		}
	}
	return nil
}

func decisionProviders(data *WorkflowData) []LLMProvider {
	aliases := data.ModelMappings
	if aliases == nil {
		aliases = BuiltinModelAliases()
	}
	patterns := expandModelPatterns(data.Decision.Model, aliases, string(LLMProviderGitHub))
	if len(patterns) == 0 {
		patterns = []string{data.Decision.Model}
	}
	var providers []LLMProvider
	for _, pattern := range patterns {
		provider := fallbackModelProvider(pattern, LLMProviderGitHub)
		if !slices.Contains(providers, provider) {
			providers = append(providers, provider)
		}
	}
	return providers
}

func decisionEnv(data *WorkflowData) map[string]string {
	env := map[string]string{
		"GH_AW_DECISION_ELIGIBLE":   "true",
		"GH_AW_DECISION_AUDIT_PATH": decisionAuditPath,
	}
	applyModelAliasesEnv(env, data)
	overrides := getEngineEnvOverrides(data)
	for _, provider := range decisionProviders(data) {
		names := llmProviderSecretNames(provider)
		name := decisionProviderTokenEnv(provider)
		env[name] = llmProviderSecretExpression(provider, data)
		if provider == "gemini" {
			names = append(names, "GOOGLE_API_KEY")
		}
		for _, candidate := range names {
			if override, ok := overrides[candidate]; ok {
				env[name] = override
				break
			}
		}
		endpoint := decisionEndpoint(provider)
		if endpoint.envKey != "" {
			env[endpoint.envKey] = endpoint.defaultURL
		}
		for _, key := range endpoint.overrideKeys {
			if value, ok := overrides[key]; ok {
				env[endpoint.envKey] = value
				break
			}
		}
	}
	return env
}

func decisionProviderTokenEnv(provider LLMProvider) string {
	switch provider {
	case LLMProviderGitHub:
		return "COPILOT_GITHUB_TOKEN"
	case LLMProviderOpenAI:
		return "OPENAI_API_KEY"
	case "gemini":
		return "GEMINI_API_KEY"
	default:
		return "ANTHROPIC_API_KEY"
	}
}
