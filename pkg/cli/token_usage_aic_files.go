package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func sumAICFromUsageJSONLFilesWithWarnings(filePaths []string) (float64, bool, []string, error) {
	var totalAIC float64
	found := false
	warnings := make([]string, 0)
	awfEntries := make([]TokenUsageEntry, 0)
	awfDuplicateRecordCount := 0
	seenAWFRequestIDs := make(map[string]struct{})
	for _, filePath := range filePaths {
		if isKnownAWFTokenUsageJSONLFile(filePath) {
			entries, duplicateRecordCount, err := scanKnownAWFTokenUsageJSONLFile(filePath, seenAWFRequestIDs)
			if err != nil {
				return 0, false, nil, err
			}
			awfEntries = append(awfEntries, entries...)
			awfDuplicateRecordCount += duplicateRecordCount
			continue
		}

		candidateSeenRequestIDs := maps.Clone(seenAWFRequestIDs)
		entries, duplicateRecordCount, awfSchemaRecordFound, err := scanTokenUsageEntriesWithSeen(filePath, candidateSeenRequestIDs)
		if err != nil {
			return 0, false, nil, err
		}
		if awfSchemaRecordFound {
			// Once a file matches the AWF token-usage schema, parse the file as one
			// AWF stream. Older token-usage records may lack _schema/event but still
			// belong to the same stream and should share request deduplication.
			seenAWFRequestIDs = candidateSeenRequestIDs
			awfEntries = append(awfEntries, entries...)
			awfDuplicateRecordCount += duplicateRecordCount
			continue
		}

		fileAIC, fileFound, err := processLegacyUsageJSONLFile(filePath)
		if err != nil {
			return 0, false, nil, err
		}
		totalAIC += fileAIC
		found = found || fileFound
	}

	if len(awfEntries) > 0 {
		summary := buildTokenUsageSummary(awfEntries, awfDuplicateRecordCount)
		if summary != nil {
			totalAIC += summary.TotalAIC
			found = found || summary.AICFound
			for _, warning := range summary.Warnings {
				if !slices.Contains(warnings, warning) {
					warnings = append(warnings, warning)
				}
			}
		}
	}
	return totalAIC, found, warnings, nil
}

func isKnownAWFTokenUsageJSONLFile(filePath string) bool {
	return strings.EqualFold(filepath.Base(filePath), "token_usage.jsonl") ||
		strings.EqualFold(filepath.Base(filePath), "token-usage.jsonl")
}

func scanKnownAWFTokenUsageJSONLFile(filePath string, seenRequestIDs map[string]struct{}) ([]TokenUsageEntry, int, error) {
	entries, duplicateRecordCount, _, err := scanTokenUsageEntriesWithSeen(filePath, seenRequestIDs)
	if err != nil {
		return nil, 0, err
	}
	return entries, duplicateRecordCount, nil
}

func processLegacyUsageJSONLFile(filePath string) (total float64, found bool, err error) {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return 0, false, fmt.Errorf("failed to open usage JSONL file %s: %w", filePath, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close usage JSONL file %s: %w", filePath, closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		recordAIC, recordFound := parseLegacyUsageJSONLAIC(line)
		total += recordAIC
		found = found || recordFound
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return 0, false, fmt.Errorf("error reading usage JSONL file %s: %w", filePath, scanErr)
	}
	return total, found, nil
}

func parseLegacyUsageJSONLAIC(line string) (float64, bool) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		return 0, false
	}
	usage := extractUsageRecord(parsed["usage"])
	for _, keys := range [][]string{{"ai_credits", "aiCredits"}, {"aic"}} {
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
