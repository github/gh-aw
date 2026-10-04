package cli

import (
	_ "embed"
	"encoding/json"
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

var (
	modelPriceRecords []modelPriceRecord
	modelPricesOnce   sync.Once
)

func initModelPrices() {
	modelPricesOnce.Do(func() {
		var data modelsCatalogData
		if err := json.Unmarshal(modelsJSON, &data); err != nil {
			return
		}

		modelPriceRecords = make([]modelPriceRecord, 0) //nolint:packagelevelmutableslicemap // sync.Once guards initialization.
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
				normalizedID := strings.Join([]string{normalizedProvider, normalizedModel}, "/")
				record := modelPriceRecord{
					id:       normalizedID,
					provider: normalizedProvider,
					model:    normalizedModel,
					pricing:  make(map[string]float64, len(entry.Cost)),
				}
				for key, value := range entry.Cost {
					if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
						record.pricing[key] = parsed
					}
				}
				modelPriceRecords = append(modelPriceRecords, record) //nolint:packagelevelmutableslicemap // sync.Once guards initialization.
			}
		}
		modelCostsLog.Printf("Initialized model price catalog: providers=%d, records=%d", len(data.Providers), len(modelPriceRecords))
	})
}

func findModelPricing(provider, model string) (map[string]float64, bool) {
	initModelPrices()

	normalizedProvider := modelsdev.NormalizeProvider(provider)
	normalizedModel := strings.ToLower(strings.TrimSpace(model))
	comparableModel := modelsdev.NormalizeComparableModelID(normalizedModel)
	if normalizedModel == "" { //nolint:tolowerequalfold
		return nil, false
	}

	fullID := normalizedModel
	if !strings.Contains(fullID, "/") && normalizedProvider != "" {
		fullID = strings.Join([]string{normalizedProvider, normalizedModel}, "/")
	}
	comparableFullID := modelsdev.NormalizeComparableModelID(fullID)

	for _, record := range modelPriceRecords {
		if (fullID != "" && record.id == fullID) || (comparableFullID != "" && modelsdev.NormalizeComparableModelID(record.id) == comparableFullID) {
			modelCostsLog.Printf("Exact pricing match: provider=%s, model=%s -> %s", provider, model, record.id)
			return record.pricing, true
		}
	}

	var bestProviderScoped map[string]float64
	bestProviderScopedLen := -1
	var bestGeneric map[string]float64
	bestGenericLen := -1

	for _, record := range modelPriceRecords {
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
