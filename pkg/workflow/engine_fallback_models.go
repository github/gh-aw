package workflow

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

func fallbackModelProvider(model string, primary LLMProvider) LLMProvider {
	if prefix, _, ok := strings.Cut(model, "/"); ok {
		if strings.EqualFold(prefix, "gemini") || strings.EqualFold(prefix, "google") {
			return "gemini"
		}
		if provider, known := llmProviderAliases[strings.ToLower(prefix)]; known {
			return provider
		}
	}
	return primary
}

func fallbackModelProviders(data *WorkflowData) []LLMProvider {
	if data == nil || data.EngineConfig == nil || len(data.EngineConfig.FallbackModels) == 0 {
		return nil
	}
	engine, err := GetGlobalEngineRegistry().GetEngine(data.EngineConfig.ID)
	if err != nil {
		return nil
	}
	resolver, ok := engine.(interface {
		ResolveLLMProvider(*WorkflowData) LLMProvider
	})
	var primary LLMProvider
	if data.EngineConfig.ID == "gemini" {
		primary = "gemini"
	} else if ok {
		primary = resolver.ResolveLLMProvider(data)
		if primary == "google" {
			primary = "gemini"
		}
	} else {
		return nil
	}
	providers := []LLMProvider{primary}
	for _, model := range append([]string{data.Model}, data.EngineConfig.FallbackModels...) {
		entries := expandModelPatterns(model, data.ModelMappings, string(primary))
		if len(entries) == 0 {
			entries = []string{model}
		}

		for _, entry := range entries {
			provider := fallbackModelProvider(entry, primary)
			if !slices.Contains(providers, provider) {
				providers = append(providers, provider)
			}
		}
	}
	return providers
}

func (c *Compiler) resolveFallbackModelPricing(modelCosts map[string]any, data *WorkflowData) map[string]any {
	if data.EngineConfig == nil {
		return modelCosts
	}
	primary := fallbackModelProviders(data)
	if len(primary) > 0 {
		for _, model := range data.EngineConfig.FallbackModels {
			for _, entry := range expandModelPatterns(model, data.ModelMappings, string(primary[0])) {
				if strings.Contains(entry, "*") {
					continue
				}
				modelData := *data
				modelData.Model = entry
				modelCosts = c.resolveModelPricingIfMissing(modelCosts, &modelData)
			}
		}
	}
	return modelCosts
}

func fallbackProviderSecretNames(data *WorkflowData) []string {
	var names []string
	for _, provider := range fallbackModelProviders(data) {
		if provider == LLMProviderAnthropic && isAnthropicWIF(data) {
			continue
		}
		names = append(names, llmProviderSecretNames(provider)...)
	}
	return names
}

func applyFallbackProviderEnv(env map[string]string, data *WorkflowData) {
	for _, provider := range fallbackModelProviders(data) {
		if provider == LLMProviderAnthropic && isAnthropicWIF(data) {
			continue
		}
		for _, name := range llmProviderSecretNames(provider) {
			env[name] = llmProviderSecretExpression(provider, data)
		}
	}
	if len(fallbackModelProviders(data)) > 1 {
		env[constants.CopilotBYOKDummyAPIKeyEnvVar] = constants.CopilotBYOKDummyAPIKey
	}
}

func fallbackProviderValidationStepID(provider LLMProvider) string {
	return "validate-fallback-" + string(provider)
}

func buildFallbackProviderValidationSteps(data *WorkflowData) []GitHubActionStep {
	providers := fallbackModelProviders(data)
	if len(providers) < 2 || data.EngineConfig.Command != "" || strings.TrimSpace(data.Environment) != "" {
		return nil
	}
	var steps []GitHubActionStep
	for _, provider := range providers[1:] {
		if provider == LLMProviderGitHub && hasCopilotRequestsWritePermission(data) ||
			provider == LLMProviderAnthropic && isAnthropicWIF(data) {
			continue
		}
		steps = append(steps, GenerateMultiSecretValidationStepWithID(
			llmProviderSecretNames(provider), "fallback "+string(provider), llmProviderDocsURL(provider),
			getEngineEnvOverrides(data), fallbackProviderValidationStepID(provider)))
	}
	return steps
}

func validateEngineFallbackModels(data *WorkflowData) error {
	if data == nil || data.EngineConfig == nil || len(data.EngineConfig.FallbackModels) == 0 {
		return nil
	}
	cfg := data.EngineConfig
	engine, err := GetGlobalEngineRegistry().GetEngine(cfg.ID)
	if err != nil {
		return err
	}
	if len(nativeAWFFallbackModels(data)) == 0 && (!engineRequiresNodeHarness(engine) || cfg.IsInlineDefinition || cfg.HarnessScript != "" || cfg.Driver != "") {
		return errors.New("engine.fallback-models requires an engine with a built-in retry harness and no custom driver or harness")
	}
	if cfg.ModelRouting != nil {
		return errors.New("engine.fallback-models cannot be combined with engine.model-routing; use fixed models for fallback")
	}
	for _, model := range cfg.FallbackModels {
		if containsExpression(model) || strings.TrimSpace(model) == "" || strings.Contains(model, "*") {
			return fmt.Errorf("engine.fallback-models must contain non-empty literal model identifiers, got %q", model)
		}
		if errs := validateModelIdentifierStrings([]string{model}, "engine.fallback-models"); len(errs) > 0 {
			return fmt.Errorf("%s", errs[0])
		}
	}
	providers := fallbackModelProviders(data)
	if len(providers) > 1 && !isFirewallEnabled(data) {
		return errors.New("cross-provider engine.fallback-models requires the AWF agent sandbox; enable sandbox.agent")
	}
	for _, provider := range providers {
		if cfg.ID == "codex" && provider != LLMProviderOpenAI && provider != LLMProviderGitHub ||
			cfg.ID == "claude" && provider != LLMProviderAnthropic && provider != LLMProviderGitHub ||
			cfg.ID == "copilot" && provider != LLMProviderGitHub && provider != LLMProviderOpenAI && provider != LLMProviderAnthropic ||
			cfg.ID == "gemini" && provider != "gemini" {
			return fmt.Errorf("engine.fallback-models: %s does not support the %s provider protocol", cfg.ID, provider)
		}
	}
	return nil
}
