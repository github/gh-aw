package cli

func computeModelInferenceAICWithCacheSemantics(provider, model string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, reasoningTokens int, inputTokensIncludeCache *bool) float64 {
	if inputTokensIncludeCache == nil {
		return computeModelInferenceAIC(provider, model, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens, reasoningTokens)
	}

	pricing, ok := findModelPricing(provider, model)
	if !ok {
		return 0
	}

	effectiveInput := inputTokens
	if *inputTokensIncludeCache {
		effectiveInput = max(inputTokens-cacheReadTokens-cacheWriteTokens, 0)
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

	costUSD := float64(effectiveInput)*promptPrice +
		float64(outputTokens)*completionPrice +
		float64(cacheReadTokens)*cacheReadPrice +
		float64(cacheWriteTokens)*cacheWritePrice +
		float64(reasoningTokens)*reasoningPrice
	return usdToAIC(costUSD)
}
