package parser

import (
	"fmt"
	"maps"

	"github.com/github/gh-aw/pkg/jsonutil"
)

const (
	modelPolicyAllowedKey = "allowed"
	modelPolicyBlockedKey = "blocked"
)

func (acc *importAccumulator) appendModelsField(fm map[string]any, importPath string) {
	modelsContent, err := extractFieldJSONFromMap(fm, "models", "{}")
	if err != nil || modelsContent == "" || modelsContent == "{}" {
		return
	}
	var rawModels map[string]any
	if jsonErr := jsonutil.UnmarshalString(modelsContent, &rawModels); jsonErr != nil {
		acc.warnings = append(acc.warnings, fmt.Sprintf("import %q: models field is not a valid object; skipping invalid value", importPath))
		return
	}
	if modelPolicy := normalizeModelPolicies(rawModels, importPath, &acc.warnings); len(modelPolicy) > 0 {
		acc.modelPolicies = append(acc.modelPolicies, modelPolicy)
		parserLog.Printf("Extracted model policy from import: allowed=%d, blocked=%d", len(modelPolicy["allowed"]), len(modelPolicy["blocked"]))
	}
	if providers, hasProviders := rawModels["providers"]; hasProviders {
		if providerMap, ok := sanitizeModelProvidersForCosts(providers, importPath, &acc.warnings); ok {
			acc.modelCosts = append(acc.modelCosts, map[string]any{"providers": providerMap})
			parserLog.Printf("Extracted model costs from import: providers=%d", len(providerMap))
		}
	}
	if acc.defaultAiCreditsPricing == nil {
		if defaultPricing, hasDefaultPricing := rawModels["default-ai-credits-pricing"]; hasDefaultPricing {
			if pricingMap, ok := defaultPricing.(map[string]any); ok {
				acc.defaultAiCreditsPricing = maps.Clone(pricingMap)
				parserLog.Printf("Extracted default-ai-credits-pricing from import: %s", importPath)
			} else {
				acc.warnings = append(acc.warnings, fmt.Sprintf("import %q: models.default-ai-credits-pricing must be an object; skipping invalid value", importPath))
			}
		}
	}

	aliasModels := make(map[string]any, len(rawModels))
	for key, value := range rawModels {
		// providers is reserved for model-cost overlays and should not be treated
		// as an alias key, even when aliases and providers coexist.
		if key == "providers" || key == "default-ai-credits-pricing" || isModelPolicyKey(key) {
			continue
		}
		aliasModels[key] = value
	}
	if len(aliasModels) == 0 {
		return
	}
	modelsMap := normalizeModelAliases(aliasModels)
	if len(modelsMap) > 0 {
		acc.models = append(acc.models, modelsMap)
		parserLog.Printf("Extracted model aliases from import: %d entries", len(modelsMap))
	}
}
func normalizeModelPolicies(rawModels map[string]any, importPath string, warnings *[]string) map[string][]string {
	parse := func(key string) []string {
		value, exists := rawModels[key]
		if !exists {
			return nil
		}
		return parseModelPolicyField(value, key, importPath, warnings)
	}
	allowed := parse(modelPolicyAllowedKey)
	blocked := parse(modelPolicyBlockedKey)
	if len(allowed) == 0 && len(blocked) == 0 {
		return nil
	}
	return map[string][]string{
		modelPolicyAllowedKey: allowed,
		modelPolicyBlockedKey: blocked,
	}
}
func normalizeModelAliases(rawModels map[string]any) map[string][]string {
	modelsMap := make(map[string][]string, len(rawModels))
	for k, v := range rawModels {
		strs := parseStringSliceField(v, true)
		if len(strs) == 0 {
			continue
		}
		modelsMap[k] = strs
	}
	return modelsMap
}

// parseModelPolicyField parses one imported models policy field as a string list.
// Invalid field shapes or entries are ignored and appended to warnings.
func parseModelPolicyField(value any, fieldName, importPath string, warnings *[]string) []string {
	values, ok := value.([]any)
	if !ok {
		*warnings = append(*warnings, fmt.Sprintf("import %q: models.%s must be an array; skipping invalid value", importPath, fieldName))
		return nil
	}
	result := make([]string, 0, len(values))
	for _, v := range values {
		s, ok := v.(string)
		if !ok {
			*warnings = append(*warnings, fmt.Sprintf("import %q: models.%s contains a non-string entry; skipping invalid entry", importPath, fieldName))
			continue
		}
		if s == "" {
			*warnings = append(*warnings, fmt.Sprintf("import %q: models.%s contains an empty string entry; skipping invalid entry", importPath, fieldName))
			continue
		}
		result = append(result, s)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// sanitizeModelProvidersForCosts validates models.providers from an import.
// It returns the provider map and true when the input is a non-empty object; otherwise false.
func sanitizeModelProvidersForCosts(providers any, importPath string, warnings *[]string) (map[string]any, bool) {
	providerMap, ok := providers.(map[string]any)
	if !ok || len(providerMap) == 0 {
		*warnings = append(*warnings, fmt.Sprintf("import %q: models.providers must be a non-empty object; skipping invalid value", importPath))
		return nil, false
	}
	sanitizedProviders := make(map[string]any, len(providerMap))
	for providerName, providerValue := range providerMap {
		if isModelPolicyKey(providerName) || providerName == "blocked" {
			*warnings = append(*warnings, fmt.Sprintf("import %q: models.providers.%s is reserved for policy and ignored in cost data", importPath, providerName))
			continue
		}
		sanitizedProviders[providerName] = providerValue
	}
	if len(sanitizedProviders) == 0 {
		*warnings = append(*warnings, fmt.Sprintf("import %q: models.providers must contain at least one non-policy provider key", importPath))
		return nil, false
	}
	return sanitizedProviders, true
}
func parseStringSliceField(value any, keepEmpty bool) []string {
	values, ok := value.([]any)
	if !ok {
		return nil
	}

	result := make([]string, 0, len(values))
	for _, v := range values {
		if s, ok := v.(string); ok {
			if s == "" && !keepEmpty {
				continue
			}
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
func isModelPolicyKey(key string) bool {
	return key == modelPolicyAllowedKey || key == modelPolicyBlockedKey
}
