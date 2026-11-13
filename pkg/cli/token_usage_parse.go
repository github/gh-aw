package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// parseTokenUsageFile parses a token-usage.jsonl file and returns the aggregated summary.
func parseTokenUsageFile(filePath string) (*TokenUsageSummary, error) {
	tokenUsageLog.Printf("Parsing token usage file: %s", filePath)

	entries, duplicateRecordCount, _, warnings, err := scanTokenUsageEntriesWithSeen(filePath, nil)
	if err != nil {
		return nil, err
	}
	summary := buildTokenUsageSummary(entries, duplicateRecordCount)
	if summary != nil {
		summary.Warnings = append(summary.Warnings, warnings...)
	}
	return summary, nil
}

func buildTokenUsageSummary(entries []TokenUsageEntry, duplicateRecordCount int) *TokenUsageSummary {
	summary := &TokenUsageSummary{
		ByModel:     make(map[string]*ModelTokenUsage),
		agentModels: make(map[string]*ModelTokenUsage),
	}
	if len(entries) == 0 {
		tokenUsageLog.Print("No token usage entries found")
		return nil
	}

	for _, entry := range entries {
		if summary.endpoint == "" {
			summary.endpoint = entry.Path
		}
		// Aggregate totals
		summary.TotalInputTokens += entry.InputTokens
		summary.TotalOutputTokens += entry.OutputTokens
		summary.TotalCacheReadTokens += entry.CacheReadTokens
		summary.TotalCacheWriteTokens += entry.CacheWriteTokens
		summary.TotalRequests++
		summary.TotalDurationMs += entry.DurationMs
		summary.TotalResponseBytes += entry.ResponseBytes

		// Aggregate by model
		model := entry.Model
		if model == "" {
			model = "unknown"
		}
		if _, exists := summary.ByModel[model]; !exists {
			summary.ByModel[model] = &ModelTokenUsage{
				Provider: entry.Provider,
			}
		}
		m := summary.ByModel[model]
		m.InputTokens += entry.InputTokens
		m.OutputTokens += entry.OutputTokens
		m.CacheReadTokens += entry.CacheReadTokens
		m.CacheWriteTokens += entry.CacheWriteTokens
		m.ReasoningTokens += entry.ReasoningTokens
		m.Requests++
		m.DurationMs += entry.DurationMs
		m.ResponseBytes += entry.ResponseBytes
		if model != "unknown" && (entry.Purpose == "" || entry.Purpose == "agent" || entry.Purpose == "subagent") {
			if summary.agentModels[model] == nil {
				summary.agentModels[model] = &ModelTokenUsage{Provider: entry.Provider}
			}
			summary.agentModels[model].Requests++
		}
	}

	tokenUsageLog.Printf("Parsed %d entries: %d input, %d output, %d cache_read, %d cache_write, %d requests",
		len(entries), summary.TotalInputTokens, summary.TotalOutputTokens,
		summary.TotalCacheReadTokens, summary.TotalCacheWriteTokens, summary.TotalRequests)

	populateAICFromTokenUsageEntries(summary, entries)
	if duplicateRecordCount > 0 {
		addTokenUsageWarning(summary, fmt.Sprintf("%d duplicate token usage record(s) were ignored by event and request_id.", duplicateRecordCount))
	}
	summary.AmbientContext = extractAmbientContextMetrics(entries)

	return summary
}

func scanTokenUsageEntries(filePath string) ([]TokenUsageEntry, int, error) {
	entries, duplicateRecordCount, _, _, err := scanTokenUsageEntriesWithSeen(filePath, nil)
	return entries, duplicateRecordCount, err
}

func scanTokenUsageEntriesWithSeen(filePath string, seenRequestIDs map[string]struct{}) (entries []TokenUsageEntry, duplicateRecordCount int, awfSchemaRecordFound bool, warnings []string, err error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, 0, false, nil, fmt.Errorf("failed to open token usage file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close token usage file: %w", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	entries = make([]TokenUsageEntry, 0)
	if seenRequestIDs == nil {
		seenRequestIDs = make(map[string]struct{})
	}
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		entry, warning := decodeTokenUsageEntryWithWarning(line, filePath, lineNum)
		if warning != "" {
			warnings = append(warnings, warning)
			continue
		}
		if strings.HasPrefix(entry.Schema, "token-usage/") || entry.Event == "token_usage" {
			awfSchemaRecordFound = true
		}
		if entry.RequestID != "" {
			// AWF defines request_id as unique per API request. Include the event
			// discriminator so additive future record types cannot collide.
			eventName := entry.Event
			if eventName == "" {
				eventName = "token_usage"
			}
			dedupeKey := eventName + ":" + entry.RequestID
			if _, exists := seenRequestIDs[dedupeKey]; exists {
				tokenUsageLog.Printf("Skipping duplicate request_id at line %d: %s", lineNum, entry.RequestID)
				duplicateRecordCount++
				continue
			}
			seenRequestIDs[dedupeKey] = struct{}{}
		}
		if !tokenUsageEntryHasAIC(entry) {
			warnings = append(warnings, usageRecordWarning(filePath, lineNum, "unpriced usage"))
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, duplicateRecordCount, awfSchemaRecordFound, warnings, fmt.Errorf("error reading token usage file: %w", err)
	}
	return entries, duplicateRecordCount, awfSchemaRecordFound, warnings, nil
}

func decodeTokenUsageEntryWithWarning(line, filePath string, lineNumber int) (TokenUsageEntry, string) {
	var entry TokenUsageEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		tokenUsageLog.Printf("Skipping invalid JSON at line %d: %v", lineNumber, err)
		reason := "malformed usage record"
		if json.Valid([]byte(line)) {
			reason = "invalid usage fields"
		}
		return entry, usageRecordWarning(filePath, lineNumber, reason)
	}
	return entry, ""
}

func usageRecordWarning(filePath string, lineNumber int, reason string) string {
	return fmt.Sprintf("%s line %d: %s; observed cost may be incomplete", filepath.Base(filePath), lineNumber, reason)
}

func extractUsageRecord(value any) map[string]any {
	record, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return record
}

func usageNumericValue(parsed map[string]any, usage map[string]any, keys ...string) float64 {
	for _, key := range keys {
		for _, candidate := range []any{usage[key], parsed[key]} {
			switch v := candidate.(type) {
			case float64:
				if !isFinite(v) {
					continue
				}
				return v
			case json.Number:
				if num, err := v.Float64(); err == nil && isFinite(num) {
					return num
				}
			case int:
				return float64(v)
			case int64:
				return float64(v)
			case string:
				if strings.TrimSpace(v) == "" {
					continue
				}
				num := json.Number(v)
				if parsedNum, err := num.Float64(); err == nil && isFinite(parsedNum) {
					return parsedNum
				}
			}
		}
	}
	return 0
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func extractAmbientContextMetrics(entries []TokenUsageEntry) *AmbientContextMetrics {
	if len(entries) == 0 {
		return nil
	}

	type orderedTokenEntry struct {
		entry        TokenUsageEntry
		timestamp    time.Time
		hasTimestamp bool
		order        int
	}

	ordered := make([]orderedTokenEntry, 0, len(entries))
	for i, entry := range entries {
		ts, hasTimestamp := parseTokenUsageTimestamp(entry.Timestamp)
		ordered = append(ordered, orderedTokenEntry{
			entry:        entry,
			timestamp:    ts,
			hasTimestamp: hasTimestamp,
			order:        i,
		})
	}

	slices.SortStableFunc(ordered, func(left, right orderedTokenEntry) int {
		if left.hasTimestamp && right.hasTimestamp {
			switch {
			case left.timestamp.Before(right.timestamp):
				return -1
			case right.timestamp.Before(left.timestamp):
				return 1
			default:
				return 0
			}
		}
		if left.hasTimestamp != right.hasTimestamp {
			if left.hasTimestamp {
				return -1
			}
			return 1
		}
		if left.order < right.order {
			return -1
		}
		if left.order > right.order {
			return 1
		}
		return 0
	})

	var firstCall TokenUsageEntry
	for _, entry := range ordered {
		firstCall = entry.entry
		break
	}
	return &AmbientContextMetrics{
		InputTokens:  firstCall.InputTokens,
		CachedTokens: firstCall.CacheReadTokens,
	}
}

func parseTokenUsageTimestamp(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return ts, true
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts, true
	}
	return time.Time{}, false
}
