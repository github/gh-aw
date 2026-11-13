package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"slices"
)

type tokenUsageAICFieldState struct {
	hasReportedFields      bool
	hasValidReportedFields bool
}

func tokenUsageEntryHasAIC(entry TokenUsageEntry) bool {
	if _, _, valid := parseOptionalNonNegativeFloat(entry.AICreditsThisResponse); valid {
		return true
	}
	if _, _, valid := parseOptionalNonNegativeFloat(entry.AICreditsTotal); valid {
		return true
	}
	return false
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

// tokenUsageEntriesWithCreditDeltas derives missing per-request credits from the
// same ordered AWF totals used by summary accounting, before filtering by caller.
func tokenUsageEntriesWithCreditDeltas(entries []TokenUsageEntry) []TokenUsageEntry {
	ordered := orderTokenUsageEntriesForAIC(entries)
	var running float64
	for index := range ordered {
		entry := &ordered[index]
		delta, _, deltaValid := parseOptionalNonNegativeFloat(entry.AICreditsThisResponse)
		total, _, totalValid := parseOptionalNonNegativeFloat(entry.AICreditsTotal)
		if !deltaValid && totalValid {
			delta = max(0, total-running)
		}
		entry.reportedCreditDelta = delta
		if totalValid {
			running = total
		} else {
			running += delta
		}
	}
	return ordered
}

func applyTokenUsageAICEntries(summary *TokenUsageSummary, entries []TokenUsageEntry) (runningAIC float64, missingRecordCount int) {
	for _, entry := range tokenUsageEntriesWithCreditDeltas(entries) {
		model := entry.Model
		if model == "" {
			model = "unknown"
		}
		_, deltaPresent, deltaValid := parseOptionalNonNegativeFloat(entry.AICreditsThisResponse)
		reportedTotal, totalPresent, totalValid := parseOptionalNonNegativeFloat(entry.AICreditsTotal)
		if !deltaPresent || !deltaValid || !totalPresent || !totalValid {
			missingRecordCount++
		}
		deltaAIC := tokenUsageEntryCredits(entry)
		if usage := summary.ByModel[model]; usage != nil {
			usage.AIC += deltaAIC
		}
		if totalValid {
			runningAIC = reportedTotal
		} else {
			runningAIC += deltaAIC
		}
	}
	return runningAIC, missingRecordCount
}

func appendTokenUsageAICWarnings(summary *TokenUsageSummary, state tokenUsageAICFieldState, missingRecordCount int) {
	if missingRecordCount > 0 && state.hasValidReportedFields {
		addTokenUsageWarning(summary, fmt.Sprintf("%d token usage record(s) had missing or invalid AWF-reported AI Credits fields; only reported costs are available and observed cost may be incomplete.", missingRecordCount))
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
	for _, usage := range summary.ByModel {
		if usage != nil {
			usage.AIC = 0
		}
	}
	var missingRecordCount int
	summary.TotalAIC, missingRecordCount = applyTokenUsageAICEntries(summary, entries)
	// Any valid reported AWF credit field, including an explicit zero, means AIC
	// data was found and callers must not fall back to repricing other artifacts.
	summary.AICFound = state.hasValidReportedFields || summary.TotalAIC > 0
	appendTokenUsageAICWarnings(summary, state, missingRecordCount)
}
