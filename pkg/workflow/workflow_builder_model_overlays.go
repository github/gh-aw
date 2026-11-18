package workflow

import (
	"maps"
	"regexp"
	"sort"
	"strings"
)

// Model costs and policy overlay helpers for workflow builder.

func rawModelsSection(frontmatter map[string]any) map[string]any {
	modelsMap, ok := frontmatter["models"].(map[string]any)
	if !ok {
		return nil
	}
	return modelsMap
}

func extractMainModelCostsOverlay(toolsResult *toolsProcessingResult, frontmatter map[string]any) map[string]any {
	if toolsResult.parsedFrontmatter != nil {
		providersMap := modelOverlayMap(toolsResult.parsedFrontmatter.ModelCosts["providers"])
		if len(providersMap) > 0 {
			return map[string]any{"providers": providersMap}
		}
	}

	modelsMap := rawModelsSection(frontmatter)
	if modelsMap == nil {
		return nil
	}
	providers, hasProviders := modelsMap["providers"]
	if !hasProviders {
		return nil
	}
	providersMap, ok := providers.(map[string]any)
	if !ok || len(providersMap) == 0 {
		return nil
	}
	return map[string]any{"providers": providersMap}
}

func mergeModelCostOverlays(importedOverlays []map[string]any, mainOverlay map[string]any) map[string]any {
	capacity := len(importedOverlays)
	if len(mainOverlay) > 0 {
		capacity++
	}
	overlays := make([]map[string]any, 0, capacity)
	overlays = append(overlays, importedOverlays...)
	if len(mainOverlay) > 0 {
		overlays = append(overlays, mainOverlay)
	}
	if len(overlays) == 0 {
		return nil
	}

	var merged map[string]any
	for i, overlay := range overlays {
		if i == 0 {
			merged = maps.Clone(overlay)
			continue
		}
		merged = mergeModelCostOverlayPair(merged, overlay)
	}
	return merged
}

func modelOverlayMap(value any) map[string]any {
	result, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return result
}

func mergeModelCostOverlayPair(base, overlay map[string]any) map[string]any {
	result := maps.Clone(base)
	baseProviders := modelOverlayMap(base["providers"])
	overlayProviders := modelOverlayMap(overlay["providers"])

	if len(overlayProviders) == 0 {
		return result
	}

	var mergedProviders map[string]any
	if baseProviders == nil {
		mergedProviders = make(map[string]any)
	} else {
		mergedProviders = maps.Clone(baseProviders)
	}
	for providerName, overlayProviderAny := range overlayProviders {
		overlayProvider, ok := overlayProviderAny.(map[string]any)
		if !ok {
			mergedProviders[providerName] = overlayProviderAny
			continue
		}

		baseProvider := modelOverlayMap(baseProviders[providerName])
		baseModels := modelOverlayMap(baseProvider["models"])
		overlayModels := modelOverlayMap(overlayProvider["models"])

		var mergedProvider map[string]any
		if baseProvider == nil {
			mergedProvider = make(map[string]any)
		} else {
			mergedProvider = maps.Clone(baseProvider)
		}
		overlayProviderNonModels := maps.Clone(overlayProvider)
		delete(overlayProviderNonModels, "models")
		maps.Copy(mergedProvider, overlayProviderNonModels)
		var mergedModels map[string]any
		if baseModels == nil {
			mergedModels = make(map[string]any)
		} else {
			mergedModels = maps.Clone(baseModels)
		}
		maps.Copy(mergedModels, overlayModels)
		mergedProvider["models"] = mergedModels
		mergedProviders[providerName] = mergedProvider
	}

	result["providers"] = mergedProviders
	return result
}

// extractMainModelPolicyOverlay returns only models.allowed/blocked policy
// entries and never treats providers data as policy.
func extractMainModelPolicyOverlay(toolsResult *toolsProcessingResult, frontmatter map[string]any) map[string][]string {
	if toolsResult.parsedFrontmatter != nil {
		mainPolicy := map[string][]string{
			"allowed": toolsResult.parsedFrontmatter.ModelPolicyAllowed,
			"blocked": toolsResult.parsedFrontmatter.ModelPolicyBlocked,
		}
		if len(mainPolicy["allowed"]) > 0 || len(mainPolicy["blocked"]) > 0 {
			return mainPolicy
		}
	}
	modelsMap := rawModelsSection(frontmatter)
	if modelsMap == nil {
		return nil
	}
	mainPolicy := map[string][]string{
		"allowed": parseModelPolicyList(modelsMap["allowed"]),
		"blocked": parseModelPolicyList(modelsMap["blocked"]),
	}
	if len(mainPolicy["allowed"]) == 0 && len(mainPolicy["blocked"]) == 0 {
		return nil
	}
	return mainPolicy
}

// toFloat64 converts any numeric value from a parsed YAML/JSON frontmatter map to float64.
// Returns (value, true) on success, or (0, false) if the value is nil or not a numeric type.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// resolveDefaultAiCreditsPricing returns models.default-ai-credits-pricing from the main
// workflow frontmatter when present, otherwise falls back to the first imported value.
func resolveDefaultAiCreditsPricing(frontmatter map[string]any, imported map[string]any) *AiCreditsPricingConfig {
	if pricing := extractDefaultAiCreditsPricingFromModels(frontmatter); pricing != nil {
		return pricing
	}
	return extractDefaultAiCreditsPricingFromObject(imported)
}

// extractDefaultAiCreditsPricingFromModels returns the fallback AI credits pricing configured
// under models.default-ai-credits-pricing in the workflow frontmatter, or nil if absent.
func extractDefaultAiCreditsPricingFromModels(frontmatter map[string]any) *AiCreditsPricingConfig {
	modelsMap := rawModelsSection(frontmatter)
	if modelsMap == nil {
		return nil
	}
	return extractDefaultAiCreditsPricingFromModelsMap(modelsMap)
}

func extractDefaultAiCreditsPricingFromModelsMap(modelsMap map[string]any) *AiCreditsPricingConfig {
	if modelsMap == nil {
		return nil
	}
	pricingVal, hasPricing := modelsMap["default-ai-credits-pricing"]
	if !hasPricing {
		return nil
	}
	pricingObj, ok := pricingVal.(map[string]any)
	if !ok {
		return nil
	}
	return extractDefaultAiCreditsPricingFromObject(pricingObj)
}

func extractDefaultAiCreditsPricingFromObject(pricingObj map[string]any) *AiCreditsPricingConfig {
	if pricingObj == nil {
		return nil
	}
	var input, output float64
	if v, ok := toFloat64(pricingObj["input"]); ok {
		input = v
	}
	if v, ok := toFloat64(pricingObj["output"]); ok {
		output = v
	}
	var cachedInput *float64
	if v, ok := toFloat64(pricingObj["cache_read"]); ok {
		cachedInput = &v
	}
	var cacheWrite *float64
	if v, ok := toFloat64(pricingObj["cache_write"]); ok {
		cacheWrite = &v
	}
	var reasoning *float64
	if v, ok := toFloat64(pricingObj["reasoning"]); ok {
		reasoning = &v
	}
	return &AiCreditsPricingConfig{
		Input:       input,
		Output:      output,
		CachedInput: cachedInput,
		CacheWrite:  cacheWrite,
		Reasoning:   reasoning,
	}
}

func mergeModelPolicyOverlays(importedPolicies []map[string][]string, mainPolicy map[string][]string) ([]string, []string) {
	overlays := make([]map[string][]string, 0, len(importedPolicies)+1)
	overlays = append(overlays, importedPolicies...)
	if len(mainPolicy) > 0 {
		overlays = append(overlays, mainPolicy)
	}
	if len(overlays) == 0 {
		return nil, nil
	}

	allowedSet := map[string]struct{}{}
	disallowedSet := map[string]struct{}{}
	for _, overlay := range overlays {
		for _, model := range overlay["allowed"] {
			if model != "" {
				allowedSet[model] = struct{}{}
			}
		}
		for _, model := range overlay["blocked"] {
			if model != "" {
				disallowedSet[model] = struct{}{}
			}
		}
	}

	allowedModels := make([]string, 0, len(allowedSet))
	for model := range allowedSet {
		allowedModels = append(allowedModels, model)
	}
	disallowedModels := make([]string, 0, len(disallowedSet))
	for model := range disallowedSet {
		disallowedModels = append(disallowedModels, model)
	}
	allowedModels = filterAllowedModelConflictsWithSet(allowedModels, disallowedSet)
	sort.Strings(allowedModels)
	sort.Strings(disallowedModels)
	return allowedModels, disallowedModels
}

func filterAllowedModelConflictsWithSet(allowed []string, disallowedSet map[string]struct{}) []string {
	if len(allowed) == 0 || len(disallowedSet) == 0 {
		return allowed
	}
	filtered := make([]string, 0, len(allowed))
	for _, model := range allowed {
		if modelConflictsWithDisallowedPolicy(model, disallowedSet) {
			continue
		}
		filtered = append(filtered, model)
	}
	return filtered
}

func modelConflictsWithDisallowedPolicy(model string, disallowedSet map[string]struct{}) bool {
	for disallowed := range disallowedSet {
		if disallowed == model {
			return true
		}
		if modelPolicyPatternMatches(disallowed, model) {
			return true
		}
		// Also check the inverse direction so an allowed wildcard pattern (for example
		// "*opus*") conflicts with a disallowed exact entry ("claude-opus").
		if modelPolicyPatternMatches(model, disallowed) {
			return true
		}
	}
	return false
}

func modelPolicyPatternMatches(pattern, value string) bool {
	if pattern == value {
		return true
	}
	if !strings.ContainsAny(pattern, "*?") {
		return false
	}
	re := "^" + regexp.QuoteMeta(pattern) + "$"
	re = strings.ReplaceAll(re, `\*`, ".*")
	re = strings.ReplaceAll(re, `\?`, ".")
	matched, err := regexp.MatchString(re, value)
	return err == nil && matched
}
