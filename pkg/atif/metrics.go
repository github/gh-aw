package atif

import (
	"encoding/json"
	"maps"
	"slices"
)

const maxSafeInteger int64 = 9007199254740991

var (
	cacheReadFields  = []string{"cache_read_input_tokens", "cacheReadInputTokens", "cache_read_tokens"}
	cacheWriteFields = []string{"cache_creation_input_tokens", "cacheCreationInputTokens", "cache_write_tokens"}
	usageAliases     = []struct {
		canonical string
		names     []string
	}{
		{"input_tokens", []string{"input_tokens", "inputTokens"}},
		{"output_tokens", []string{"output_tokens", "outputTokens"}},
		{"cache_read_input_tokens", cacheReadFields},
		{"cache_creation_input_tokens", cacheWriteFields},
		{"input_tokens_include_cache", []string{"input_tokens_include_cache", "inputTokensIncludeCache"}},
	}
)

func supplied(data map[string]json.RawMessage, names ...string) bool {
	for _, name := range names {
		if _, ok := data[name]; ok {
			return true
		}
	}
	return overflowed(data, names...)
}

func overflowed(data map[string]json.RawMessage, names ...string) bool {
	for _, field := range []string{"overflowed_tokens", "overflowedTokens"} {
		var fields []string
		if json.Unmarshal(data[field], &fields) != nil {
			continue
		}
		for _, field := range fields {
			if slices.Contains(names, field) {
				return true
			}
		}
	}
	return false
}

// Reconcile supplied metrics, not whole snapshots: turns and diagnostics often
// follow usage. Each caller keeps these snapshots within one source/session.
func reconcileMetrics(previous, supplied map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	merged := make(map[string]json.RawMessage, len(previous))
	maps.Copy(merged, previous)
	for key, value := range supplied {
		if key != "usage" {
			merged[key] = value
		}
	}
	for _, name := range []string{"totalCostUsd", "total_cost_usd"} {
		if raw, ok := supplied[name]; ok {
			merged["totalCostUsd"] = raw
			delete(merged, "total_cost_usd")
			break
		}
	}
	raw, present := supplied["usage"]
	if !present {
		return merged, nil
	}
	var incoming map[string]json.RawMessage
	if json.Unmarshal(raw, &incoming) != nil || incoming == nil {
		merged["usage"] = raw
		return merged, nil
	}
	usage := make(map[string]json.RawMessage)
	if json.Unmarshal(previous["usage"], &usage) != nil || usage == nil {
		usage = make(map[string]json.RawMessage)
	}
	for key, value := range incoming {
		if key != "overflowed_tokens" && key != "overflowedTokens" {
			usage[key] = value
		}
	}
	for _, aliases := range usageAliases {
		for _, name := range aliases.names {
			if value, ok := incoming[name]; ok {
				usage[aliases.canonical] = value
				break
			}
		}
		if overflowed(incoming, aliases.names...) {
			// Persist invalidation even when a later snapshot omits overflow metadata.
			usage[aliases.canonical] = json.RawMessage("null")
		}
		for _, alias := range aliases.names {
			if alias != aliases.canonical {
				delete(usage, alias)
			}
		}
	}
	var err error
	merged["usage"], err = json.Marshal(usage)
	return merged, err
}

func count(data map[string]json.RawMessage, names ...string) *int64 {
	if overflowed(data, names...) {
		return nil
	}
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
	metrics.CachedTokens = count(usage, cacheReadFields...)
	cacheWrite := count(usage, cacheWriteFields...)
	rawIncluded, ok := usage["input_tokens_include_cache"]
	if !ok {
		rawIncluded = usage["inputTokensIncludeCache"]
	}
	var included bool
	if string(rawIncluded) == "false" && metrics.PromptTokens != nil {
		for _, contribution := range []struct {
			tokens *int64
			names  []string
		}{{metrics.CachedTokens, cacheReadFields}, {cacheWrite, cacheWriteFields}} {
			cache := contribution.tokens
			if cache == nil && supplied(usage, contribution.names...) {
				metrics.PromptTokens = nil
				break
			}
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
		if supplied(usage, cacheReadFields...) || supplied(usage, cacheWriteFields...) {
			metrics.PromptTokens = nil
		}
	}
	if metrics.PromptTokens != nil && metrics.CachedTokens != nil && *metrics.CachedTokens > *metrics.PromptTokens {
		metrics.PromptTokens = nil
		metrics.CachedTokens = nil
	}
	return metrics
}
