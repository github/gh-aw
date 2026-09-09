package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
)

type tokenUsageAICFieldState struct {
	hasReportedFields         bool
	hasValidReportedFields    bool
	hasExplicitCacheSemantics bool
}

func parseOptionalNonNegativeFloat(raw json.RawMessage) (value float64, present, valid bool) {
	if len(raw) == 0 {
		return 0, false, false
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, true, false
	}
	if err := json.Unmarshal(raw, &value); err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, true, false
	}
	return value, true, true
}

func parseOptionalBool(raw json.RawMessage) (*bool, bool, bool) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false, false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, true, true
	}
	return &value, true, false
}

func inspectTokenUsageAICFields(entries []TokenUsageEntry) tokenUsageAICFieldState {
	state := tokenUsageAICFieldState{}
	for _, entry := range entries {
		if len(entry.AICreditsThisResponse) > 0 || len(entry.AICreditsTotal) > 0 {
			state.hasReportedFields = true
		}
		if _, _, deltaValid := parseOptionalNonNegativeFloat(entry.AICreditsThisResponse); deltaValid {
			state.hasValidReportedFields = true
		}
		if _, _, totalValid := parseOptionalNonNegativeFloat(entry.AICreditsTotal); totalValid {
			state.hasValidReportedFields = true
		}
		if value, present, _ := parseOptionalBool(entry.InputTokensIncludeCache); present {
			if value != nil {
				state.hasExplicitCacheSemantics = true
			}
		}
	}
	return state
}

func orderTokenUsageEntriesForAIC(entries []TokenUsageEntry) []TokenUsageEntry {
	ordered := slices.Clone(entries)
	slices.SortStableFunc(ordered, func(left, right TokenUsageEntry) int {
		leftTimestamp, leftValid := parseTokenUsageTimestamp(left.Timestamp)
		rightTimestamp, rightValid := parseTokenUsageTimestamp(right.Timestamp)
		if leftValid && rightValid {
			return leftTimestamp.Compare(rightTimestamp)
		}
		if leftValid {
			return -1
		}
		if rightValid {
			return 1
		}
		return 0
	})
	return ordered
}

func applyTokenUsageAICEntries(summary *TokenUsageSummary, entries []TokenUsageEntry, hasReportedFields bool) (runningAIC float64, fallbackRecordCount int, invalidCacheSemanticsCount int) {
	for _, entry := range orderTokenUsageEntriesForAIC(entries) {
		model := entry.Model
		if model == "" {
			model = "unknown"
		}
		reportedDelta, deltaPresent, deltaValid := parseOptionalNonNegativeFloat(entry.AICreditsThisResponse)
		reportedTotal, totalPresent, totalValid := parseOptionalNonNegativeFloat(entry.AICreditsTotal)
		inputTokensIncludeCache, _, invalidCacheSemantics := parseOptionalBool(entry.InputTokensIncludeCache)
		if hasReportedFields && (!deltaPresent || !deltaValid || !totalPresent || !totalValid) {
			fallbackRecordCount++
		}
		deltaAIC := reportedDelta
		if !deltaValid {
			if invalidCacheSemantics {
				invalidCacheSemanticsCount++
			}
			deltaAIC = computeModelInferenceAICWithCacheSemantics(entry.Provider, model, entry.InputTokens, entry.OutputTokens, entry.CacheReadTokens, entry.CacheWriteTokens, entry.ReasoningTokens, inputTokensIncludeCache)
		}
		if usage := summary.ByModel[model]; usage != nil {
			usage.AIC += deltaAIC
		}
		if totalValid {
			runningAIC = reportedTotal
		} else {
			runningAIC += deltaAIC
		}
	}
	return runningAIC, fallbackRecordCount, invalidCacheSemanticsCount
}

func appendTokenUsageAICWarnings(summary *TokenUsageSummary, state tokenUsageAICFieldState, fallbackRecordCount int, invalidCacheSemanticsCount int) {
	if invalidCacheSemanticsCount > 0 {
		addTokenUsageWarning(summary, fmt.Sprintf("%d token usage record(s) had invalid input_tokens_include_cache values; legacy provider cache semantics were used.", invalidCacheSemanticsCount))
	}
	if fallbackRecordCount > 0 {
		addTokenUsageWarning(summary, fmt.Sprintf("%d token usage record(s) had missing or invalid AWF-reported AI Credits fields; fallback accounting was used for the missing values.", fallbackRecordCount))
	}
	summedDeltaAIC := 0.0
	for _, usage := range summary.ByModel {
		if usage != nil {
			summedDeltaAIC += usage.AIC
		}
	}
	if state.hasReportedFields && math.Abs(summedDeltaAIC-summary.TotalAIC) > 1e-6*max(1, math.Abs(summary.TotalAIC)) {
		addTokenUsageWarning(summary, "The AWF-reported cumulative AI Credits total differs from the sum of per-request credits; the cumulative total was preserved for reporting.")
	}
}

// populateAICFromTokenUsageEntries uses AWF-computed fields only for usage
// reporting. Budget enforcement and failure classification use separate paths.
func populateAICFromTokenUsageEntries(summary *TokenUsageSummary, entries []TokenUsageEntry) {
	if summary == nil {
		return
	}
	state := inspectTokenUsageAICFields(entries)
	if !state.hasReportedFields && !state.hasExplicitCacheSemantics {
		populateAIC(summary)
		invalidCacheSemanticsCount := 0
		for _, entry := range entries {
			if _, _, invalid := parseOptionalBool(entry.InputTokensIncludeCache); invalid {
				invalidCacheSemanticsCount++
			}
		}
		summary.AICFound = summary.TotalAIC > 0
		appendTokenUsageAICWarnings(summary, state, 0, invalidCacheSemanticsCount)
		return
	}
	for _, usage := range summary.ByModel {
		if usage != nil {
			usage.AIC = 0
		}
	}
	var fallbackRecordCount int
	var invalidCacheSemanticsCount int
	summary.TotalAIC, fallbackRecordCount, invalidCacheSemanticsCount = applyTokenUsageAICEntries(summary, entries, state.hasReportedFields)
	// Any valid reported AWF credit field, including an explicit zero, means AIC
	// data was found and callers must not fall back to repricing other artifacts.
	summary.AICFound = state.hasValidReportedFields || summary.TotalAIC > 0
	appendTokenUsageAICWarnings(summary, state, fallbackRecordCount, invalidCacheSemanticsCount)
}
