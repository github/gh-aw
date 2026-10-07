package cli

import (
	_ "embed"
	"encoding/json"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/modelsdev"
)

var modelCostsLog = logger.New("cli:model_costs")

//go:embed data/models.json
var modelsJSON []byte

type modelsCatalogData struct {
	Providers map[string]modelsCatalogProvider `json:"providers"`
}

type modelsCatalogProvider struct {
	Models map[string]modelCostEntry `json:"models"`
}

type modelCostEntry struct {
	Cost map[string]string `json:"cost"`
}

type modelPriceRecord struct {
	id       string
	provider string
	model    string
	pricing  map[string]float64
}

var modelPriceRecords = sync.OnceValue(loadModelPriceRecords)

func loadModelPriceRecords() []modelPriceRecord {
	var data modelsCatalogData
	if err := json.Unmarshal(modelsJSON, &data); err != nil {
		return nil
	}

	records := make([]modelPriceRecord, 0)
	for providerName, providerData := range data.Providers {
		normalizedProvider := strings.ToLower(strings.TrimSpace(providerName))
		if normalizedProvider == "" { //nolint:tolowerequalfold
			continue
		}
		for modelName, entry := range providerData.Models {
			normalizedModel := strings.ToLower(strings.TrimSpace(modelName))
			if normalizedModel == "" { //nolint:tolowerequalfold
				continue
			}
			record := modelPriceRecord{
				id:       path.Join(normalizedProvider, normalizedModel),
				provider: normalizedProvider,
				model:    normalizedModel,
				pricing:  make(map[string]float64, len(entry.Cost)),
			}
			for key, value := range entry.Cost {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					record.pricing[key] = parsed
				}
			}
			records = append(records, record)
		}
	}
	modelCostsLog.Printf("Initialized model price catalog: providers=%d, records=%d", len(data.Providers), len(records))
	return records
}

func findModelPricing(provider, model string) (map[string]float64, bool) {
	normalizedProvider := modelsdev.NormalizeProvider(provider)
	normalizedModel := strings.ToLower(strings.TrimSpace(model))
	comparableModel := modelsdev.NormalizeComparableModelID(normalizedModel)
	if normalizedModel == "" { //nolint:tolowerequalfold
		return nil, false
	}

	fullID := normalizedModel
	if !strings.Contains(fullID, "/") && normalizedProvider != "" {
		fullID = path.Join(normalizedProvider, normalizedModel)
	}
	comparableFullID := modelsdev.NormalizeComparableModelID(fullID)

	if pricing, ok := findExactCatalogPricing(modelPriceRecords(), fullID, comparableFullID); ok {
		modelCostsLog.Printf("Exact pricing match: provider=%s, model=%s -> %s", provider, model, fullID)
		return pricing, true
	}

	var bestProviderScoped map[string]float64
	bestProviderScopedLen := -1
	var bestGeneric map[string]float64
	bestGenericLen := -1

	for _, record := range modelPriceRecords() {
		comparableRecordModel := modelsdev.NormalizeComparableModelID(record.model)
		if record.model == normalizedModel || comparableRecordModel == comparableModel {
			if normalizedProvider != "" && record.provider == normalizedProvider {
				return record.pricing, true
			}
			if bestGeneric == nil {
				bestGeneric = record.pricing
			}
			continue
		}

		if strings.HasPrefix(normalizedModel, record.model) || strings.HasPrefix(comparableModel, comparableRecordModel) {
			if normalizedProvider != "" && record.provider == normalizedProvider && len(record.model) > bestProviderScopedLen {
				bestProviderScoped = record.pricing
				bestProviderScopedLen = len(record.model)
			}
			if len(record.model) > bestGenericLen {
				bestGeneric = record.pricing
				bestGenericLen = len(record.model)
			}
		}
	}

	if bestProviderScoped != nil {
		modelCostsLog.Printf("Provider-scoped prefix pricing match: provider=%s, model=%s", provider, model)
		return bestProviderScoped, true
	}
	if bestGeneric != nil {
		modelCostsLog.Printf("Generic prefix pricing match: provider=%s, model=%s", provider, model)
		return bestGeneric, true
	}
	modelCostsLog.Printf("No pricing match: provider=%s, model=%s", provider, model)
	return nil, false
}

func findExactCatalogPricing(records []modelPriceRecord, fullID, comparableFullID string) (map[string]float64, bool) {
	for _, record := range records {
		if (fullID != "" && record.id == fullID) || (comparableFullID != "" && modelsdev.NormalizeComparableModelID(record.id) == comparableFullID) {
			return record.pricing, true
		}
	}
	return nil, false
}

func findExactModelPricing(provider, model string) (map[string]float64, bool) {
	normalizedProvider := modelsdev.NormalizeProvider(provider)
	model, _, _ = strings.Cut(strings.TrimSpace(model), "?")
	comparableModel := modelsdev.NormalizeComparableModelID(model)
	if normalizedProvider == "" || comparableModel == "" {
		return nil, false
	}
	for _, record := range modelPriceRecords() {
		if record.provider == normalizedProvider &&
			modelsdev.NormalizeComparableModelID(record.model) == comparableModel &&
			len(record.pricing) > 0 {
			return record.pricing, true
		}
	}
	return nil, false
}

func hasCatalogPricingMatching(provider, pattern string) bool {
	normalizedProvider := modelsdev.NormalizeProvider(provider)
	comparablePattern := modelsdev.NormalizeComparableModelID(pattern)
	if normalizedProvider == "" || comparablePattern == "" {
		return false
	}
	for _, record := range modelPriceRecords() {
		if record.provider != normalizedProvider || len(record.pricing) == 0 {
			continue
		}
		matched, err := path.Match(comparablePattern, modelsdev.NormalizeComparableModelID(record.model))
		if err == nil && matched {
			return true
		}
	}
	return false
}

func usdToAIC(usd float64) float64 {
	return usd / 0.01
}

func computeModelInferenceCostUSD(provider, model string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, reasoningTokens int) float64 {
	pricing, ok := findModelPricing(provider, model)
	if !ok {
		return 0
	}

	input := inputTokens
	cacheRead := cacheReadTokens
	if cacheRead > 0 && providerIncludesCacheReadsInInput(modelsdev.NormalizeProvider(provider)) {
		input = max(inputTokens-cacheReadTokens, 0)
	}

	promptPrice := pricing["input"]
	completionPrice := pricing["output"]
	cacheReadPrice := pricing["cache_read"]
	if cacheReadPrice == 0 {
		cacheReadPrice = promptPrice
	}
	cacheWritePrice := pricing["cache_write"]
	if cacheWritePrice == 0 {
		cacheWritePrice = promptPrice
	}
	reasoningPrice := pricing["reasoning"]
	if reasoningPrice == 0 {
		reasoningPrice = completionPrice
	}

	return float64(input)*promptPrice +
		float64(outputTokens)*completionPrice +
		float64(cacheRead)*cacheReadPrice +
		float64(cacheWriteTokens)*cacheWritePrice +
		float64(reasoningTokens)*reasoningPrice
}

func computeModelInferenceAIC(provider, model string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, reasoningTokens int) float64 {
	return usdToAIC(computeModelInferenceCostUSD(provider, model, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, reasoningTokens))
}
