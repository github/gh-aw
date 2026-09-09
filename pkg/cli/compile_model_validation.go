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
