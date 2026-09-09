package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func parseAgentUsageFile(filePath string) (*TokenUsageSummary, error) {
	data, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, fmt.Errorf("failed to read agent usage file: %w", err)
	}
	var entry agentUsageEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("failed to parse agent usage file: %w", err)
	}
	summary := buildAgentUsageSummary(entry)
	tokenUsageLog.Printf("Parsed agent usage file: input=%d, output=%d, cache_read=%d, cache_write=%d",
		summary.TotalInputTokens, summary.TotalOutputTokens, summary.TotalCacheReadTokens, summary.TotalCacheWriteTokens)
	return summary, nil
}

func resolveAgentUsageModel(entry agentUsageEntry) string {
	model := strings.TrimSpace(entry.PrimaryModel)
	if model == "" {
		model = strings.TrimSpace(entry.Model)
	}
	if model == "" {
		return "unknown"
	}
	return model
}

func buildAgentUsageSummary(entry agentUsageEntry) *TokenUsageSummary {
	model := resolveAgentUsageModel(entry)
	provider := strings.TrimSpace(entry.Provider)
	summary := &TokenUsageSummary{
		TotalInputTokens:      entry.InputTokens,
		TotalOutputTokens:     entry.OutputTokens,
		TotalCacheReadTokens:  entry.CacheReadTokens,
		TotalCacheWriteTokens: entry.CacheWriteTokens,
		ByModel:               make(map[string]*ModelTokenUsage),
	}
	hasRawTokenData := entry.InputTokens > 0 ||
		entry.OutputTokens > 0 ||
		entry.CacheReadTokens > 0 ||
		entry.CacheWriteTokens > 0 ||
		entry.ReasoningTokens > 0
	if hasRawTokenData {
		summary.TotalRequests = 1
		summary.ByModel[model] = buildAgentModelTokenUsage(entry, provider)
	}
	ambientInputTokens := entry.InputTokens
	if entry.AmbientContextTokens != nil {
		ambientInputTokens = *entry.AmbientContextTokens
	}
	summary.AmbientContext = &AmbientContextMetrics{
		InputTokens:  ambientInputTokens,
		CachedTokens: entry.CacheReadTokens,
	}
	populateAgentUsageAIC(summary, entry, model, provider, hasRawTokenData)
	return summary
}

func buildAgentModelTokenUsage(entry agentUsageEntry, provider string) *ModelTokenUsage {
	return &ModelTokenUsage{
		Provider: provider,
		TokenCoreMetrics: TokenCoreMetrics{
			InputTokens:      entry.InputTokens,
			OutputTokens:     entry.OutputTokens,
			CacheReadTokens:  entry.CacheReadTokens,
			CacheWriteTokens: entry.CacheWriteTokens,
			ReasoningTokens:  entry.ReasoningTokens,
		},
		Requests: 1,
	}
}

func populateAgentUsageAIC(summary *TokenUsageSummary, entry agentUsageEntry, model, provider string, hasRawTokenData bool) {
	aic, present, valid := parseOptionalNonNegativeFloat(entry.AICredits)
	if !present || !valid {
		if hasRawTokenData {
			populateAIC(summary)
			summary.AICFound = summary.TotalAIC > 0
		}
		return
	}
	summary.TotalAIC = aic
	summary.AICFound = true
	if summary.ByModel[model] == nil {
		summary.ByModel[model] = &ModelTokenUsage{}
	}
	usage := summary.ByModel[model]
	usage.Provider = provider
	usage.InputTokens = entry.InputTokens
	usage.OutputTokens = entry.OutputTokens
	usage.CacheReadTokens = entry.CacheReadTokens
	usage.CacheWriteTokens = entry.CacheWriteTokens
	usage.ReasoningTokens = entry.ReasoningTokens
	usage.AIC = aic
}
