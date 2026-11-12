package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func sumAICFromUsageJSONLFilesWithSeen(filePaths []string, seenAWFRequestIDs map[string]struct{}) (float64, bool, []string, error) {
	summary, _, err := summarizeUsageJSONLFilesWithSeen(filePaths, seenAWFRequestIDs)
	if err != nil {
		return 0, false, nil, err
	}
	return summary.TotalAIC, summary.AICFound, summary.Warnings, nil
}

func summarizeUsageJSONLFilesWithSeen(filePaths []string, seenAWFRequestIDs map[string]struct{}) (*TokenUsageSummary, bool, error) {
	combined := &TokenUsageSummary{ByModel: make(map[string]*ModelTokenUsage), agentModels: make(map[string]*ModelTokenUsage)}
	hadAWFRecords := false
	awfDuplicateRecordCount := 0
	for _, filePath := range filePaths {
		entries, duplicateRecordCount, awfSchemaRecordFound, fileWarnings, err := scanTokenUsageEntriesWithSeen(filePath, nil)
		if err != nil {
			return nil, false, err
		}
		if isKnownAWFTokenUsageJSONLFile(filePath) || awfSchemaRecordFound {
			hadAWFRecords = hadAWFRecords || len(entries) > 0
			// Once a file matches the AWF token-usage schema, parse the file as one
			// AWF stream. Older token-usage records may lack _schema/event but still
			// belong to the same stream and should share request deduplication.
			var mirroredRecordCount int
			entries, mirroredRecordCount, err = deduplicateTokenUsageAICStream(entries, seenAWFRequestIDs)
			if err != nil {
				return nil, false, err
			}
			duplicateRecordCount += mirroredRecordCount
			awfDuplicateRecordCount += duplicateRecordCount
			combined.Warnings = append(combined.Warnings, fileWarnings...)
			// Each proxy stream has its own cumulative counter. Never apply a
			// detection counter to an agent stream's running total.
			if summary := buildTokenUsageSummary(entries, 0); summary != nil {
				mergeTokenUsageSummary(combined, summary)
			}
			continue
		}

		fileAIC, fileFound, fileWarnings, err := processLegacyUsageJSONLFile(filePath)
		if err != nil {
			return nil, false, err
		}
		combined.TotalAIC += fileAIC
		combined.AICFound = combined.AICFound || fileFound
		combined.Warnings = append(combined.Warnings, fileWarnings...)
	}

	if awfDuplicateRecordCount > 0 {
		combined.Warnings = append(combined.Warnings, fmt.Sprintf("%d duplicate token usage record(s) were ignored by event and request_id.", awfDuplicateRecordCount))
	}
	return combined, hadAWFRecords, nil
}

func mergeTokenUsageSummary(combined, summary *TokenUsageSummary) {
	combined.TotalInputTokens += summary.TotalInputTokens
	combined.TotalOutputTokens += summary.TotalOutputTokens
	combined.TotalCacheReadTokens += summary.TotalCacheReadTokens
	combined.TotalCacheWriteTokens += summary.TotalCacheWriteTokens
	combined.TotalRequests += summary.TotalRequests
	combined.TotalDurationMs += summary.TotalDurationMs
	combined.TotalResponseBytes += summary.TotalResponseBytes
	combined.TotalSteeringEvents += summary.TotalSteeringEvents
	if combined.SteeringEventCounts == nil {
		combined.SteeringEventCounts = make(map[string]int)
	}
	for event, count := range summary.SteeringEventCounts {
		combined.SteeringEventCounts[event] += count
	}
	combined.SteeringNotices = append(combined.SteeringNotices, summary.SteeringNotices...)
	combined.SteeringDeliveries = append(combined.SteeringDeliveries, summary.SteeringDeliveries...)
	combined.SteeringNoticesUnavailable = combined.SteeringNoticesUnavailable || summary.SteeringNoticesUnavailable
	combined.SubagentModelRequests = append(combined.SubagentModelRequests, summary.SubagentModelRequests...)
	combined.DeclaredSubagentModels = append(combined.DeclaredSubagentModels, summary.DeclaredSubagentModels...)
	combined.SubagentModelActuals = append(combined.SubagentModelActuals, summary.SubagentModelActuals...)
	combined.AgentUsage = append(combined.AgentUsage, summary.AgentUsage...)
	combined.TotalAIC += summary.TotalAIC
	combined.AICFound = combined.AICFound || summary.AICFound
	combined.Warnings = append(combined.Warnings, summary.Warnings...)
	if combined.AmbientContext == nil {
		combined.AmbientContext = summary.AmbientContext
	}
	if combined.endpoint == "" {
		combined.endpoint = summary.endpoint
	}
	for model, usage := range summary.ByModel {
		if combined.ByModel[model] == nil {
			combined.ByModel[model] = &ModelTokenUsage{Provider: usage.Provider}
		}
		target := combined.ByModel[model]
		target.InputTokens += usage.InputTokens
		target.OutputTokens += usage.OutputTokens
		target.CacheReadTokens += usage.CacheReadTokens
		target.CacheWriteTokens += usage.CacheWriteTokens
		target.ReasoningTokens += usage.ReasoningTokens
		target.Requests += usage.Requests
		target.DurationMs += usage.DurationMs
		target.ResponseBytes += usage.ResponseBytes
		target.AIC += usage.AIC
	}
	for model, usage := range summary.agentModels {
		if combined.agentModels[model] == nil {
			combined.agentModels[model] = &ModelTokenUsage{Provider: usage.Provider}
		}
		combined.agentModels[model].Requests += usage.Requests
	}
}

func deduplicateTokenUsageAICStream(entries []TokenUsageEntry, seenRequestIDs map[string]struct{}) ([]TokenUsageEntry, int, error) {
	unique := make([]TokenUsageEntry, 0, len(entries))
	var runningAIC, mirroredAIC float64
	duplicates := 0
	for _, entry := range orderTokenUsageEntriesForAIC(entries) {
		reportedTotal, _, totalValid := parseOptionalNonNegativeFloat(entry.AICreditsTotal)
		delta, _, deltaValid := parseOptionalNonNegativeFloat(entry.AICreditsThisResponse)
		if !deltaValid {
			includeCache, _, _ := parseOptionalBool(entry.InputTokensIncludeCache)
			delta = computeModelInferenceAICWithCacheSemantics(entry.Provider, entry.Model, entry.InputTokens,
				entry.OutputTokens, entry.CacheReadTokens, entry.CacheWriteTokens, entry.ReasoningTokens, includeCache)
		}
		nextAIC := runningAIC + delta
		if totalValid {
			nextAIC = reportedTotal
		}
		event := entry.Event
		if event == "" {
			event = "token_usage"
		}
		key := event + ":" + entry.RequestID
		_, duplicate := seenRequestIDs[key]
		if entry.RequestID != "" && duplicate {
			// A mirrored prefix may include cumulative cost before its first
			// recorded request. Subtract that prefix, not just its last delta.
			mirroredAIC += nextAIC - runningAIC
			duplicates++
		} else {
			if entry.RequestID != "" {
				seenRequestIDs[key] = struct{}{}
			}
			if totalValid && mirroredAIC != 0 {
				credits, err := json.Marshal(max(0, reportedTotal-mirroredAIC))
				if err != nil {
					return nil, 0, fmt.Errorf("failed to encode deduplicated token usage credits: %w", err)
				}
				entry.AICreditsTotal = credits
			}
			unique = append(unique, entry)
		}
		runningAIC = nextAIC
	}
	return unique, duplicates, nil
}

func isKnownAWFTokenUsageJSONLFile(filePath string) bool {
	return strings.EqualFold(filepath.Base(filePath), "token_usage.jsonl") ||
		strings.EqualFold(filepath.Base(filePath), "token-usage.jsonl")
}

func processLegacyUsageJSONLFile(filePath string) (total float64, found bool, warnings []string, err error) {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return 0, false, nil, fmt.Errorf("failed to open usage JSONL file %s: %w", filePath, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close usage JSONL file %s: %w", filePath, closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNumber := 0
	inspectUsage := strings.Contains(strings.ToLower(filepath.Base(filePath)), "usage")
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			if inspectUsage {
				warnings = append(warnings, usageRecordWarning(filePath, lineNumber, "malformed usage record"))
			}
			continue
		}
		recordAIC, recordFound := parseLegacyUsageAIC(parsed)
		total += recordAIC
		found = found || recordFound
		if inspectUsage || legacyUsageRecordHasAccountingFields(parsed) {
			var entry TokenUsageEntry
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				warnings = append(warnings, usageRecordWarning(filePath, lineNumber, "invalid usage fields"))
			} else if !recordFound {
				warnings = append(warnings, usageRecordWarning(filePath, lineNumber, "unpriced usage"))
			}
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return 0, false, warnings, fmt.Errorf("error reading usage JSONL file %s: %w", filePath, scanErr)
	}
	return total, found, warnings, nil
}

func legacyUsageRecordHasAccountingFields(parsed map[string]any) bool {
	for _, record := range []map[string]any{parsed, extractUsageRecord(parsed["usage"])} {
		for _, key := range []string{
			"ai_credits", "aiCredits", "aic", "input_tokens", "inputTokens", "output_tokens", "outputTokens",
			"cache_read_tokens", "cacheReadTokens", "cache_write_tokens", "cacheWriteTokens", "reasoning_tokens", "reasoningTokens",
		} {
			if _, present := record[key]; present {
				return true
			}
		}
	}
	return false
}

func parseLegacyUsageAIC(parsed map[string]any) (float64, bool) {
	usage := extractUsageRecord(parsed["usage"])
	for _, keys := range [][]string{{"ai_credits", "aiCredits"}, {"aic"}} {
		for _, key := range keys {
			for _, candidate := range []any{usage[key], parsed[key]} {
				if value, ok := candidate.(float64); ok && isFinite(value) && value >= 0 {
					return value, true
				}
				if text, ok := candidate.(string); ok {
					if value, err := strconv.ParseFloat(strings.TrimSpace(text), 64); err == nil && isFinite(value) && value >= 0 {
						return value, true
					}
				}
			}
		}
		if value := usageNumericValue(parsed, usage, keys...); value > 0 {
			return value, true
		}
	}
	computedAIC := computeModelInferenceAIC(
		usageStringValue(parsed, usage, "provider"),
		usageStringValue(parsed, usage, "model"),
		int(usageNumericValue(parsed, usage, "input_tokens", "inputTokens")),
		int(usageNumericValue(parsed, usage, "output_tokens", "outputTokens")),
		int(usageNumericValue(parsed, usage, "cache_read_tokens", "cacheReadTokens")),
		int(usageNumericValue(parsed, usage, "cache_write_tokens", "cacheWriteTokens")),
		int(usageNumericValue(parsed, usage, "reasoning_tokens", "reasoningTokens")),
	)
	return computedAIC, computedAIC > 0
}
