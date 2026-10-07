package cli

import (
	"context"
	"path"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/modelsdev"
	"github.com/github/gh-aw/pkg/workflow"
)

type activeModelInventory struct {
	models  []string
	aliases map[string]struct{}
}

func buildActiveModelInventory(report modelsReport) *activeModelInventory {
	if len(report.Observed) == 0 {
		return nil
	}

	models := make(map[string]struct{})
	for _, row := range report.Observed {
		model := modelsdev.NormalizeComparableModelID(row.Model)
		if model == "" {
			continue
		}
		models[model] = struct{}{}
		if row.Provider != "" {
			provider := modelsdev.NormalizeProvider(row.Provider)
			models[modelsdev.NormalizeComparableModelID(path.Join(provider, row.Model))] = struct{}{}
			if provider == "github-copilot" {
				for _, alias := range []string{"copilot", "github", "github_models"} {
					models[modelsdev.NormalizeComparableModelID(path.Join(alias, row.Model))] = struct{}{}
				}
			}
		}
	}

	aliases := make(map[string]struct{}, len(report.Aliases))
	for _, row := range report.Aliases {
		aliases[modelsdev.NormalizeComparableModelID(row.Alias)] = struct{}{}
	}

	activeModels := make([]string, 0, len(models))
	for model := range models {
		activeModels = append(activeModels, model)
	}
	slices.Sort(activeModels)
	return &activeModelInventory{models: activeModels, aliases: aliases}
}

func (i *activeModelInventory) contains(candidate string, workflowAliases map[string][]string) bool {
	base, _, _ := strings.Cut(strings.TrimSpace(candidate), "?")
	if base == "" || strings.Contains(base, "${{") {
		return true
	}

	normalized := modelsdev.NormalizeComparableModelID(base)
	if _, ok := i.aliases[normalized]; ok {
		return true
	}
	for alias := range workflowAliases {
		if modelsdev.NormalizeComparableModelID(alias) == normalized {
			return true
		}
	}

	for _, model := range i.models {
		if matched, err := path.Match(normalized, model); err == nil && matched {
			return true
		}
	}
	return false
}

func findUnknownConfiguredModels(data *workflow.WorkflowData, inventory *activeModelInventory) []ValidationIssue {
	if data == nil || inventory == nil {
		return nil
	}

	candidates := make(map[string][]string)
	add := func(field string, values []string) {
		for _, value := range values {
			if !inventory.contains(value, data.ModelMappings) {
				candidates[value] = append(candidates[value], field)
			}
		}
	}

	add("models.allowed", data.ModelPolicyAllowed)
	add("models.blocked", data.ModelPolicyBlocked)

	if engine, ok := data.RawFrontmatter["engine"].(map[string]any); ok {
		if models, ok := engine["models"].(map[string]any); ok {
			if value, ok := models["default"].(string); ok {
				add("engine.models.default", []string{value})
			}
			add("engine.models.supported", stringSlice(models["supported"]))
		}
	}

	values := make([]string, 0, len(candidates))
	for value := range candidates {
		values = append(values, value)
	}
	slices.Sort(values)

	warnings := make([]ValidationIssue, 0, len(values))
	for _, value := range values {
		fields := candidates[value]
		slices.Sort(fields)
		warnings = append(warnings, ValidationIssue{
			Type:    "unknown_model",
			Message: "Model " + value + " referenced by " + strings.Join(fields, ", ") + " was not found in the active model inventory",
		})
	}
	return warnings
}

// PrepareCompileModelValidation builds the active model inventory used by compile --models.
func PrepareCompileModelValidation(ctx context.Context, config *CompileConfig) {
	if config.DryRun {
		config.Models = true
	}
	if !config.Models {
		return
	}
	report := buildModelsReport(ctx, modelsReportOptions{
		logsDir:         defaultLogsOutputDir,
		refreshObserved: true,
		refreshCount:    defaultModelsRefreshCount,
	})
	config.activeModels = buildActiveModelInventory(report)
}

func unknownConfiguredModelMessages(data *workflow.WorkflowData, inventory *activeModelInventory) []string {
	issues := findUnknownConfiguredModels(data, inventory)
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	return messages
}

func configuredModelValidationMessages(data *workflow.WorkflowData, inventory *activeModelInventory, requireInventory bool) []string {
	if requireInventory && inventory == nil && data != nil {
		hasConfiguredModels := len(data.ModelPolicyAllowed) > 0 || len(data.ModelPolicyBlocked) > 0
		if engine, ok := data.RawFrontmatter["engine"].(map[string]any); ok {
			if models, ok := engine["models"].(map[string]any); ok {
				defaultModel, hasDefaultModel := models["default"].(string)
				hasConfiguredModels = hasConfiguredModels || (hasDefaultModel && defaultModel != "") || len(stringSlice(models["supported"])) > 0
			}
		}
		if hasConfiguredModels {
			return []string{"development mode cannot check configured models: the observed active model inventory is unavailable"}
		}
	}
	return unknownConfiguredModelMessages(data, inventory)
}

func configuredModelPricingWarning(data *workflow.WorkflowData) string {
	if data == nil || data.Model == "" || data.DefaultAiCreditsPricing != nil {
		return ""
	}
	provider, model, ok := configuredModelPricingID(data, data.Model)
	if !ok || hasModelCostOverlay(data.ModelCosts, provider, model) {
		return ""
	}
	if configuredModelHasPricing(provider, model, data.ModelMappings, make(map[string]struct{})) {
		return ""
	}
	return "Model " + data.Model + " has no AI credits pricing. Add models.providers.<provider>.models.<model>.cost or set models.default-ai-credits-pricing, map it to a model with pricing, or select a priced model; otherwise the AWF API proxy may reject inference requests with HTTP 400."
}

func configuredModelPricingID(data *workflow.WorkflowData, model string) (string, string, bool) {
	model, _, _ = strings.Cut(strings.TrimSpace(model), "?")
	if model == "" || strings.Contains(model, "${{") {
		return "", "", false
	}
	var provider string
	if strings.Contains(model, "/") {
		var ok bool
		provider, model, ok = strings.Cut(model, "/")
		if !ok || provider == "" || model == "" {
			return "", "", false
		}
	} else if data.EngineConfig == nil {
		provider = "github-copilot"
	} else if data.EngineConfig.LLMProvider != "" {
		provider = string(data.EngineConfig.LLMProvider)
	} else if data.EngineConfig.InlineProviderID != "" {
		provider = data.EngineConfig.InlineProviderID
	} else {
		switch strings.ToLower(strings.TrimSpace(data.EngineConfig.ID)) {
		case "claude":
			provider = "anthropic"
		case "codex":
			provider = "openai"
		case "copilot", "":
			provider = "github-copilot"
		default:
			return "", "", false
		}
	}
	provider = modelsdev.NormalizeProvider(provider)
	model = strings.ToLower(strings.TrimSpace(model))
	if provider == "" || model == "" || model == "auto" {
		return "", "", false
	}
	return provider, model, true
}

func configuredModelHasPricing(provider, model string, aliases map[string][]string, visited map[string]struct{}) bool {
	for alias, targets := range aliases {
		if !strings.EqualFold(alias, model) {
			continue
		}
		if _, ok := visited[alias]; ok || len(targets) == 0 {
			return false
		}
		visited[alias] = struct{}{}
		for _, target := range targets {
			targetProvider, targetModel, ok := configuredModelPricingID(&workflow.WorkflowData{
				EngineConfig: &workflow.EngineConfig{LLMProvider: workflow.LLMProvider(provider)},
			}, target)
			if !ok || !configuredModelHasPricing(targetProvider, targetModel, aliases, visited) {
				delete(visited, alias)
				return false
			}
		}
		delete(visited, alias)
		return true
	}
	_, ok := findExactModelPricing(provider, model)
	return ok
}

func hasModelCostOverlay(costs map[string]any, provider, model string) bool {
	providers, ok := costs["providers"].(map[string]any)
	if !ok {
		return false
	}
	for name, rawProvider := range providers {
		if modelsdev.NormalizeProvider(name) != provider {
			continue
		}
		providerMap, ok := rawProvider.(map[string]any)
		if !ok {
			continue
		}
		models, ok := providerMap["models"].(map[string]any)
		if !ok {
			continue
		}
		for name := range models {
			if strings.EqualFold(name, model) {
				return true
			}
		}
	}
	return false
}

func stringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(raw))
	for _, entry := range raw {
		if text, ok := entry.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
