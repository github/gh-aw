package parser

import (
	"fmt"
	"hash/fnv"
	"io"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var scheduleFuzzyScatterLog = logger.New("parser:schedule_fuzzy_scatter")

// This file contains fuzzy schedule scattering logic that deterministically
// distributes workflow execution times based on workflow identifiers.

// buildWeightedHourPool constructs the weighted pool of hours used for full-day scatter
// patterns. The pool reflects the following distribution:
//
//   - BEST  (weight 3): 02:00–05:59 UTC — low-traffic hours, preferred for maintenance
//   - BROAD (weight 1): 06:00–23:59 UTC — full daytime/evening window
//
// Pool size: 4×3 (BEST) + 18×1 (BROAD) = 12 + 18 = 30 slots.
// BEST represents 12/30 = 40% and BROAD represents 18/30 = 60% of the hour pool.
func buildWeightedHourPool() []int {
	var pool []int

	// BEST: hours 02–05, weight 3 (appear 3 times each)
	for h := 2; h <= 5; h++ {
		pool = append(pool, h, h, h)
	}

	// BROAD: hours 06–23, weight 1
	for h := 6; h <= 23; h++ {
		pool = append(pool, h)
	}

	return pool
}

// buildAvailableMinutes constructs the valid minute values used for the independent
// minute selection in daily scatter patterns. The pool pre-excludes:
//
//   - Hour-boundary windows [0–4] and [55–59] — high-traffic around each hour boundary
//   - EU morning peak [27–33] — ±3 minutes around :30 in hours 06–09
//   - US business-hours peaks [12–18] and [42–48] — ±3 minutes around :15 and :45
//
// Pre-excluding these ranges means avoidPeakMinutes does not need to remap pool
// values, which previously caused clustering: several raw minutes all collapsing to
// the same post-remap value (e.g. 27–33 → 34) and creating artificial collisions.
//
// Remaining valid minutes: [5–11, 19–26, 34–41, 49–54] = 29 values.
func buildAvailableMinutes() []int {
	var pool []int
	for m := 5; m <= 54; m++ {
		// Exclude EU morning peak zone (±3 of :30, affecting hours 06–09)
		if m >= 27 && m <= 33 {
			continue
		}
		// Exclude US business-hours peak zones (±3 of :15 and :45, hours 14–18)
		if m >= 12 && m <= 18 {
			continue
		}
		if m >= 42 && m <= 48 {
			continue
		}
		pool = append(pool, m)
	}
	return pool
}

// weightedHourPool is the pre-computed weighted pool of hours (BEST + BROAD tiers).
var weightedHourPool = buildWeightedHourPool()

// availableMinutes is the pre-computed curated set of valid minutes for scatter
// selection: 29 values spanning [5–11, 19–26, 34–41, 49–54] with hour-boundary
// and peak-traffic ranges pre-excluded (see buildAvailableMinutes).
var availableMinutes = buildAvailableMinutes()

// weightedDailyTimeSlot returns a deterministic (hour, minute) pair for the given
// workflow identifier using two hash operations — one for hour selection and one for
// minute selection — where the minute hash incorporates the hour-pool index as a
// disambiguation component.
//
// The original single-hash approach (972-slot flat pool) produced exact cron-time
// collisions for ~5 workflow pairs per 99 workflows (birthday paradox). Three-way
// collisions caused concurrent token-API bursts that exhausted the 60 req/min quota,
// silently losing safe-output writes.
//
// This implementation reduces collision probability by requiring two independent
// conditions to hold simultaneously for a full (hour, minute) collision:
//
//  1. Both workflows must resolve to the same hour value (not necessarily the same
//     pool index — different indices can yield the same hour via BEST-tier weight-3
//     duplication, e.g. indices 0 and 1 both resolve to hour 2).
//  2. The minute hash of a composite seed (identifier + ":" + hHash index string)
//     must produce the same minute value for both workflows.
//
// The composite seed in condition 2 means that even when two workflows share the same
// resolved hour, they typically receive different minute seeds as long as their hHash
// values differ. Only when both the resolved hour AND the composite-seed minute hash
// collide does a duplicate cron expression occur.
func weightedDailyTimeSlot(identifier string) (int, int) {
	// Hash 1: select hour from the weighted hour pool (preserves BEST/BROAD preference).
	hHash := stableHash(identifier, len(weightedHourPool))
	hour := weightedHourPool[hHash]

	// Hash 2: select minute using a composite seed that encodes the hour-pool index.
	// Incorporating hHash into the seed ensures two workflows that share the same
	// hour via different pool indices (a common outcome of the BEST-tier weight-3
	// duplication) still get different minute hashes as long as their hHash values
	// differ.  When hHash also coincides, the full identifier strings diverge, making
	// collisions on this second hash unlikely for distinct real-world workflow names.
	// avoidPeakMinutes is intentionally NOT called here because availableMinutes
	// already pre-excludes all peak ranges; calling it on pool values would remap
	// multiple distinct raw minutes to the same output, artificially increasing
	// collision counts.
	minuteSeed := fmt.Sprintf("%s:%d", identifier, hHash)
	minute := availableMinutes[stableHash(minuteSeed, len(availableMinutes))]

	return hour, minute
}

// avoidHourBoundary remaps a minute value to avoid the 5-minute window before
// and after each hour (minutes 0–4 and 55–59). These windows are subject to
// usage peaks on GitHub Actions, especially at 00:00 UTC.
// Minutes [0, 4] are shifted to [5, 9] and minutes [55, 59] are shifted to [50, 54],
// keeping all results within [5, 54].
//
// The input is expected to be in the range [0, 59] (a valid minute value).
// Values outside this range are not remapped.
func avoidHourBoundary(minute int) int {
	if minute < 5 {
		return minute + 5
	}
	if minute > 54 {
		return minute - 5
	}
	return minute
}

// avoidPeakMinutes shifts minute values that fall within 3 minutes of known high-traffic
// peak minutes during busy UTC hours:
//
//   - EU morning peak (06:00–09:59 UTC): avoids minutes [27, 33] (±3 around :30),
//     shifting any value in that window to 34 (first minute clearly outside the window)
//   - US business hours (14:00–18:59 UTC): avoids minutes [12, 18] (±3 around :15)
//     and [42, 48] (±3 around :45), shifting to 19 and 49 respectively
//
// All replacement values stay within [5, 54]. This is applied after avoidHourBoundary
// for targeted-scatter patterns where the hour is determined by a user-specified target.
func avoidPeakMinutes(hour, minute int) int {
	// EU morning peak: stay 3 minutes away from :30 in hours 06–09
	if hour >= 6 && hour <= 9 && minute >= 27 && minute <= 33 {
		return 34
	}
	// US business hours (moderate): stay 3 minutes away from :15 and :45 in hours 14–18
	if hour >= 14 && hour <= 18 {
		if minute >= 12 && minute <= 18 {
			return 19
		}
		if minute >= 42 && minute <= 48 {
			return 49
		}
	}
	return minute
}

// stableHash returns a deterministic hash value in the range [0, modulo)
// using FNV-1a hash algorithm, which is stable across platforms and Go versions.
// If modulo is <= 0, stableHash returns 0 (the range is empty/invalid).
func stableHash(s string, modulo int) int {
	h := fnv.New32a()
	// hash.Hash.Write never returns an error in practice, but check to satisfy gosec G104
	if _, err := io.WriteString(h, s); err != nil {
		// Return 0 (safe fallback) if write somehow fails
		scheduleFuzzyScatterLog.Printf("Warning: hash write failed: %v", err)
		return 0
	}
	if modulo <= 0 {
		scheduleFuzzyScatterLog.Printf("Warning: stableHash called with non-positive modulo %d, returning 0", modulo)
		return 0
	}
	// Use int64 arithmetic to avoid truncation when modulo exceeds math.MaxUint32
	// on 64-bit platforms where int is 64 bits wide.
	return int(int64(h.Sum32()) % int64(modulo))
}

// ScatterSchedule takes a fuzzy cron expression and a workflow identifier
// and returns a deterministic scattered time for that workflow
func ScatterSchedule(fuzzyCron, workflowIdentifier string) (string, error) {
	scheduleFuzzyScatterLog.Printf("Scattering schedule: fuzzyCron=%s, workflowId=%s", fuzzyCron, workflowIdentifier)
	if !IsFuzzyCron(fuzzyCron) {
		scheduleFuzzyScatterLog.Printf("Invalid fuzzy cron expression: %s", fuzzyCron)
		return "", fmt.Errorf("not a fuzzy schedule: %s", fuzzyCron)
	}
	handlers := []func(string, string) (string, bool, error){
		handleDailyAroundWeekdays,
		handleDailyBetweenWeekdays,
		handleDailyAround,
		handleDailyBetween,
		handleDailyWeekdays,
		handleDaily,
		handleHourlyWeekdays,
		handleHourly,
		handleEveryMinute,
		handleWeeklyAround,
		handleWeeklySpecific,
		handleWeekly,
		handleBiWeekly,
		handleTriWeekly,
	}
	for _, handler := range handlers {
		result, matched, err := handler(fuzzyCron, workflowIdentifier)
		if err != nil {
			return "", err
		}
		if matched {
			return result, nil
		}
	}
	scheduleFuzzyScatterLog.Printf("Unsupported fuzzy schedule type: %s", fuzzyCron)
	return "", fmt.Errorf("unsupported fuzzy schedule type: %s", fuzzyCron)
}

func handleDailyAroundWeekdays(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	const prefix = "FUZZY:DAILY_AROUND_WEEKDAYS:"
	if !strings.HasPrefix(fuzzyCron, prefix) {
		return "", false, nil
	}
	targetHour, targetMinute, err := parseAroundTarget(fuzzyCron, prefix, "invalid fuzzy daily around weekdays pattern", "invalid time format in fuzzy daily around weekdays pattern", "invalid target hour in fuzzy daily around weekdays pattern", "invalid target minute in fuzzy daily around weekdays pattern")
	if err != nil {
		return "", true, err
	}
	hour, minute := scatterAroundTime(targetHour, targetMinute, workflowIdentifier)
	result := fmt.Sprintf("%d %d * * 1-5", minute, hour)
	scheduleFuzzyScatterLog.Printf("FUZZY:DAILY_AROUND_WEEKDAYS scattered: original=%d:%d, scattered=%d:%d, result=%s", targetHour, targetMinute, hour, minute, result)
	return result, true, nil
}

func handleDailyBetweenWeekdays(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	const prefix = "FUZZY:DAILY_BETWEEN_WEEKDAYS:"
	if !strings.HasPrefix(fuzzyCron, prefix) {
		return "", false, nil
	}
	startHour, startMinute, endHour, endMinute, err := parseBetweenRange(fuzzyCron, prefix, "invalid fuzzy daily between weekdays pattern", "invalid time format in fuzzy daily between weekdays pattern", "invalid start hour in fuzzy daily between weekdays pattern", "invalid start minute in fuzzy daily between weekdays pattern", "invalid end hour in fuzzy daily between weekdays pattern", "invalid end minute in fuzzy daily between weekdays pattern")
	if err != nil {
		return "", true, err
	}
	hour, minute := scatterBetweenTime(startHour, startMinute, endHour, endMinute, workflowIdentifier)
	result := fmt.Sprintf("%d %d * * 1-5", minute, hour)
	scheduleFuzzyScatterLog.Printf("FUZZY:DAILY_BETWEEN_WEEKDAYS scattered: start=%d:%d, end=%d:%d, scattered=%d:%d, result=%s", startHour, startMinute, endHour, endMinute, hour, minute, result)
	return result, true, nil
}

func handleDailyAround(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	const prefix = "FUZZY:DAILY_AROUND:"
	if !strings.HasPrefix(fuzzyCron, prefix) {
		return "", false, nil
	}
	targetHour, targetMinute, err := parseAroundTarget(fuzzyCron, prefix, "invalid fuzzy daily around pattern", "invalid time format in fuzzy daily around pattern", "invalid target hour in fuzzy daily around pattern", "invalid target minute in fuzzy daily around pattern")
	if err != nil {
		return "", true, err
	}
	hour, minute := scatterAroundTime(targetHour, targetMinute, workflowIdentifier)
	result := fmt.Sprintf("%d %d * * *", minute, hour)
	scheduleFuzzyScatterLog.Printf("FUZZY:DAILY_AROUND scattered: original=%d:%d, scattered=%d:%d, result=%s", targetHour, targetMinute, hour, minute, result)
	return result, true, nil
}

func handleDailyBetween(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	const prefix = "FUZZY:DAILY_BETWEEN:"
	if !strings.HasPrefix(fuzzyCron, prefix) {
		return "", false, nil
	}
	startHour, startMinute, endHour, endMinute, err := parseBetweenRange(fuzzyCron, prefix, "invalid fuzzy daily between pattern", "invalid time format in fuzzy daily between pattern", "invalid start hour in fuzzy daily between pattern", "invalid start minute in fuzzy daily between pattern", "invalid end hour in fuzzy daily between pattern", "invalid end minute in fuzzy daily between pattern")
	if err != nil {
		return "", true, err
	}
	hour, minute := scatterBetweenTime(startHour, startMinute, endHour, endMinute, workflowIdentifier)
	result := fmt.Sprintf("%d %d * * *", minute, hour)
	scheduleFuzzyScatterLog.Printf("FUZZY:DAILY_BETWEEN scattered: start=%d:%d, end=%d:%d, scattered=%d:%d, result=%s", startHour, startMinute, endHour, endMinute, hour, minute, result)
	return result, true, nil
}

func handleDailyWeekdays(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	if !strings.HasPrefix(fuzzyCron, "FUZZY:DAILY_WEEKDAYS") {
		return "", false, nil
	}
	hour, minute := weightedDailyTimeSlot(workflowIdentifier)
	result := fmt.Sprintf("%d %d * * 1-5", minute, hour)
	scheduleFuzzyScatterLog.Printf("FUZZY:DAILY_WEEKDAYS scattered: result=%s", result)
	return result, true, nil
}

func handleDaily(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	if !strings.HasPrefix(fuzzyCron, "FUZZY:DAILY") {
		return "", false, nil
	}
	hour, minute := weightedDailyTimeSlot(workflowIdentifier)
	result := fmt.Sprintf("%d %d * * *", minute, hour)
	scheduleFuzzyScatterLog.Printf("FUZZY:DAILY scattered: result=%s", result)
	return result, true, nil
}

func handleHourlyWeekdays(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	const prefix = "FUZZY:HOURLY_WEEKDAYS/"
	if !strings.HasPrefix(fuzzyCron, prefix) {
		return "", false, nil
	}
	interval, err := parseHourlyInterval(fuzzyCron, prefix, "invalid fuzzy hourly weekdays pattern", "invalid interval in fuzzy hourly weekdays pattern")
	if err != nil {
		return "", true, err
	}
	minute := stableHash(workflowIdentifier, 50) + 5
	result := fmt.Sprintf("%d */%d * * 1-5", minute, interval)
	scheduleFuzzyScatterLog.Printf("FUZZY:HOURLY_WEEKDAYS/%d scattered: minute=%d, result=%s", interval, minute, result)
	return result, true, nil
}

func handleHourly(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	const prefix = "FUZZY:HOURLY/"
	if !strings.HasPrefix(fuzzyCron, prefix) {
		return "", false, nil
	}
	interval, err := parseHourlyInterval(fuzzyCron, prefix, "invalid fuzzy hourly pattern", "invalid interval in fuzzy hourly pattern")
	if err != nil {
		return "", true, err
	}
	minute := stableHash(workflowIdentifier, 50) + 5
	result := fmt.Sprintf("%d */%d * * *", minute, interval)
	scheduleFuzzyScatterLog.Printf("FUZZY:HOURLY/%d scattered: minute=%d, result=%s", interval, minute, result)
	return result, true, nil
}

// handleEveryMinute scatters "FUZZY:EVERY_MINUTE/N * * * *" to "M/N * * * *",
// where M is a deterministic offset in [0, N-1] derived from the workflow identifier.
// This ensures that concurrent "every N minutes" workflows start on different minutes,
// distributing execution across the clock face and reducing simultaneous load spikes.
func handleEveryMinute(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	const prefix = "FUZZY:EVERY_MINUTE/"
	if !strings.HasPrefix(fuzzyCron, prefix) {
		return "", false, nil
	}
	// parseHourlyInterval extracts the integer N from a "PREFIX/N ..." pattern;
	// it is reused here because the parsing logic is identical.
	interval, err := parseHourlyInterval(fuzzyCron, prefix, "invalid fuzzy every-minute pattern", "invalid interval in fuzzy every-minute pattern")
	if err != nil {
		return "", true, err
	}
	if interval < 1 {
		return "", true, fmt.Errorf("invalid interval in fuzzy every-minute pattern: interval must be >= 1, got %d", interval)
	}
	// stableHash returns a value in [0, interval-1], so offset is always a valid
	// starting minute that preserves the N-minute period.
	offset := stableHash(workflowIdentifier, interval)
	result := fmt.Sprintf("%d/%d * * * *", offset, interval)
	scheduleFuzzyScatterLog.Printf("FUZZY:EVERY_MINUTE/%d scattered: offset=%d, result=%s", interval, offset, result)
	return result, true, nil
}

func handleWeeklyAround(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	const prefix = "FUZZY:WEEKLY_AROUND:"
	if !strings.HasPrefix(fuzzyCron, prefix) {
		return "", false, nil
	}
	parts, err := parsePrefixedTokenParts(fuzzyCron, prefix, 3, "invalid fuzzy weekly around pattern", "invalid format in fuzzy weekly around pattern")
	if err != nil {
		return "", true, err
	}
	weekday := parts[0]
	targetHour, err := parseBoundedInt(parts[1], 0, 23, "invalid target hour in fuzzy weekly around pattern", fuzzyCron)
	if err != nil {
		return "", true, err
	}
	targetMinute, err := parseBoundedInt(parts[2], 0, 59, "invalid target minute in fuzzy weekly around pattern", fuzzyCron)
	if err != nil {
		return "", true, err
	}
	hour, minute := scatterAroundTime(targetHour, targetMinute, workflowIdentifier)
	result := fmt.Sprintf("%d %d * * %s", minute, hour, weekday)
	scheduleFuzzyScatterLog.Printf("FUZZY:WEEKLY_AROUND scattered: weekday=%s, target=%d:%d, scattered=%d:%d, result=%s", weekday, targetHour, targetMinute, hour, minute, result)
	return result, true, nil
}

func handleWeeklySpecific(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	const prefix = "FUZZY:WEEKLY:"
	if !strings.HasPrefix(fuzzyCron, prefix) {
		return "", false, nil
	}
	parts := strings.Split(fuzzyCron, " ")
	if len(parts) < 1 {
		return "", true, fmt.Errorf("invalid fuzzy weekly pattern: %s", fuzzyCron)
	}
	weekday := strings.TrimPrefix(parts[0], prefix)
	hour, minute := weightedDailyTimeSlot(workflowIdentifier)
	result := fmt.Sprintf("%d %d * * %s", minute, hour, weekday)
	scheduleFuzzyScatterLog.Printf("FUZZY:WEEKLY:%s scattered: result=%s", weekday, result)
	return result, true, nil
}

func handleWeekly(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	if !strings.HasPrefix(fuzzyCron, "FUZZY:WEEKLY") {
		return "", false, nil
	}
	weekday := stableHash(workflowIdentifier, 7)
	hour, minute := weightedDailyTimeSlot(workflowIdentifier)
	result := fmt.Sprintf("%d %d * * %d", minute, hour, weekday)
	scheduleFuzzyScatterLog.Printf("FUZZY:WEEKLY scattered: weekday=%d, time=%d:%d, result=%s", weekday, hour, minute, result)
	return result, true, nil
}

func handleBiWeekly(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	if !strings.HasPrefix(fuzzyCron, "FUZZY:BI_WEEKLY") {
		return "", false, nil
	}
	hour, minute := weightedDailyTimeSlot(workflowIdentifier)
	result := fmt.Sprintf("%d %d */%d * *", minute, hour, 14)
	scheduleFuzzyScatterLog.Printf("FUZZY:BI_WEEKLY scattered: time=%d:%d, result=%s", hour, minute, result)
	return result, true, nil
}

func handleTriWeekly(fuzzyCron, workflowIdentifier string) (string, bool, error) {
	if !strings.HasPrefix(fuzzyCron, "FUZZY:TRI_WEEKLY") {
		return "", false, nil
	}
	hour, minute := weightedDailyTimeSlot(workflowIdentifier)
	result := fmt.Sprintf("%d %d */%d * *", minute, hour, 21)
	scheduleFuzzyScatterLog.Printf("FUZZY:TRI_WEEKLY scattered: time=%d:%d, result=%s", hour, minute, result)
	return result, true, nil
}

func parseAroundTarget(fuzzyCron, prefix, invalidPatternMsg, invalidFormatMsg, invalidHourMsg, invalidMinuteMsg string) (int, int, error) {
	parts, err := parsePrefixedTokenParts(fuzzyCron, prefix, 2, invalidPatternMsg, invalidFormatMsg)
	if err != nil {
		return 0, 0, err
	}
	hour, err := parseBoundedInt(parts[0], 0, 23, invalidHourMsg, fuzzyCron)
	if err != nil {
		return 0, 0, err
	}
	minute, err := parseBoundedInt(parts[1], 0, 59, invalidMinuteMsg, fuzzyCron)
	if err != nil {
		return 0, 0, err
	}
	return hour, minute, nil
}

func parseBetweenRange(
	fuzzyCron, prefix, invalidPatternMsg, invalidFormatMsg, invalidStartHourMsg, invalidStartMinuteMsg, invalidEndHourMsg, invalidEndMinuteMsg string,
) (int, int, int, int, error) {
	parts, err := parsePrefixedTokenParts(fuzzyCron, prefix, 4, invalidPatternMsg, invalidFormatMsg)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	startHour, err := parseBoundedInt(parts[0], 0, 23, invalidStartHourMsg, fuzzyCron)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	startMinute, err := parseBoundedInt(parts[1], 0, 59, invalidStartMinuteMsg, fuzzyCron)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	endHour, err := parseBoundedInt(parts[2], 0, 23, invalidEndHourMsg, fuzzyCron)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	endMinute, err := parseBoundedInt(parts[3], 0, 59, invalidEndMinuteMsg, fuzzyCron)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return startHour, startMinute, endHour, endMinute, nil
}

func parseHourlyInterval(fuzzyCron, prefix, invalidPatternMsg, invalidIntervalMsg string) (int, error) {
	parts := strings.Split(fuzzyCron, " ")
	if len(parts) < 1 {
		return 0, fmt.Errorf("%s: %s", invalidPatternMsg, fuzzyCron)
	}
	intervalStr := strings.TrimPrefix(parts[0], prefix)
	interval, err := strconv.Atoi(intervalStr)
	if err != nil {
		return 0, fmt.Errorf("%s: %s", invalidIntervalMsg, fuzzyCron)
	}
	return interval, nil
}

func parsePrefixedTokenParts(fuzzyCron, prefix string, expectedParts int, invalidPatternMsg, invalidFormatMsg string) ([]string, error) {
	parts := strings.Split(fuzzyCron, " ")
	if len(parts) < 1 {
		return nil, fmt.Errorf("%s: %s", invalidPatternMsg, fuzzyCron)
	}
	timePart := strings.TrimPrefix(parts[0], prefix)
	timeParts := strings.Split(timePart, ":")
	if len(timeParts) != expectedParts {
		return nil, fmt.Errorf("%s: %s", invalidFormatMsg, fuzzyCron)
	}
	return timeParts, nil
}

func parseBoundedInt(value string, minVal, maxVal int, errorPrefix, fuzzyCron string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minVal || parsed > maxVal {
		return 0, fmt.Errorf("%s: %s", errorPrefix, fuzzyCron)
	}
	return parsed, nil
}

func scatterAroundTime(targetHour, targetMinute int, workflowIdentifier string) (int, int) {
	targetMinutes := targetHour*60 + targetMinute
	windowSize := 120
	hash := stableHash(workflowIdentifier, windowSize)
	offset := hash - (windowSize / 2)
	scatteredMinutes := wrapMinutes(targetMinutes + offset)
	return finalizeScatteredMinutes(scatteredMinutes)
}

func scatterBetweenTime(startHour, startMinute, endHour, endMinute int, workflowIdentifier string) (int, int) {
	startMinutes := startHour*60 + startMinute
	endMinutes := endHour*60 + endMinute
	rangeSize := computeRangeSize(startMinutes, endMinutes)
	hash := stableHash(workflowIdentifier, rangeSize)
	scatteredMinutes := startMinutes + hash
	if scatteredMinutes >= 24*60 {
		scatteredMinutes -= 24 * 60
	}
	return finalizeScatteredMinutes(scatteredMinutes)
}

func finalizeScatteredMinutes(scatteredMinutes int) (int, int) {
	hour := scatteredMinutes / 60
	minute := avoidPeakMinutes(hour, avoidHourBoundary(scatteredMinutes%60))
	return hour, minute
}

func computeRangeSize(startMinutes, endMinutes int) int {
	if endMinutes > startMinutes {
		return endMinutes - startMinutes
	}
	return (24*60 - startMinutes) + endMinutes
}

func wrapMinutes(minutes int) int {
	for minutes < 0 {
		minutes += 24 * 60
	}
	for minutes >= 24*60 {
		minutes -= 24 * 60
	}
	return minutes
}
