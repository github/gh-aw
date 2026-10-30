package atif

import "encoding/json"

const maxSafeInteger int64 = 9007199254740991

func count(data map[string]json.RawMessage, names ...string) *int64 {
	for _, name := range names {
		if raw, ok := data[name]; ok {
			var value int64
			if string(raw) != "null" && json.Unmarshal(raw, &value) == nil && value >= 0 && value <= maxSafeInteger {
				return &value
			}
			return nil
		}
	}
	return nil
}

func finalMetrics(data map[string]json.RawMessage, steps int) *FinalMetrics {
	metrics := &FinalMetrics{TotalSteps: steps}
	if data == nil {
		return metrics
	}
	metrics.Extra = map[string]any{"unified_session_result": data}
	var cost float64
	if raw, ok := data["totalCostUsd"]; ok && json.Unmarshal(raw, &cost) == nil && string(raw) != "null" && cost >= 0 {
		metrics.CostUSD = &cost
	}
	var usage map[string]json.RawMessage
	if json.Unmarshal(data["usage"], &usage) != nil {
		return metrics
	}
	metrics.PromptTokens = count(usage, "input_tokens", "inputTokens")
	metrics.CompletionTokens = count(usage, "output_tokens", "outputTokens")
	metrics.CachedTokens = count(usage, "cache_read_input_tokens", "cacheReadInputTokens")
	cacheWrite := count(usage, "cache_creation_input_tokens", "cacheCreationInputTokens")
	rawIncluded, ok := usage["input_tokens_include_cache"]
	if !ok {
		rawIncluded = usage["inputTokensIncludeCache"]
	}
	var included bool
	if string(rawIncluded) == "false" && metrics.PromptTokens != nil {
		for _, cache := range []*int64{metrics.CachedTokens, cacheWrite} {
			if cache != nil {
				if *metrics.PromptTokens > maxSafeInteger-*cache {
					metrics.PromptTokens = nil
					break
				}
				total := *metrics.PromptTokens + *cache
				metrics.PromptTokens = &total
			}
		}
	} else if json.Unmarshal(rawIncluded, &included) != nil || !included {
		// Unknown cache semantics cannot establish ATIF's cache-inclusive total.
		if metrics.CachedTokens != nil || cacheWrite != nil {
			metrics.PromptTokens = nil
		}
	}
	if metrics.PromptTokens != nil && metrics.CachedTokens != nil && *metrics.CachedTokens > *metrics.PromptTokens {
		metrics.PromptTokens = nil
		metrics.CachedTokens = nil
	}
	return metrics
}
