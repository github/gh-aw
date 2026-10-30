package workflow

import "slices"

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
		"GH_AW_DECISION_AUDIT_PATH": "${{ runner.temp }}/gh-aw/decision-gate.json",
	}
	applyModelAliasesEnv(env, data)
	overrides := getEngineEnvOverrides(data)
	for _, provider := range decisionProviders(data) {
		names := llmProviderSecretNames(provider)
		name := names[0]
		if provider == LLMProviderOpenAI {
			name = "OPENAI_API_KEY"
		}
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
		var baseURLKeys []string
		switch provider {
		case LLMProviderGitHub:
			baseURLKeys = []string{"GITHUB_COPILOT_BASE_URL"}
		case LLMProviderAnthropic:
			baseURLKeys = []string{"ANTHROPIC_BASE_URL"}
		case LLMProviderOpenAI:
			baseURLKeys = []string{"OPENAI_BASE_URL"}
		case "gemini":
			baseURLKeys = []string{"GOOGLE_GEMINI_BASE_URL", "GEMINI_API_BASE_URL"}
		}
		for _, key := range baseURLKeys {
			if value, ok := overrides[key]; ok {
				env[baseURLKeys[0]] = value
				break
			}
		}
	}
	return env
}
