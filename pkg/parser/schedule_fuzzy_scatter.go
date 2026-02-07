package parser

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var scheduleFuzzyScatterLog = logger.New("parser:schedule_fuzzy_scatter")

// This file contains fuzzy schedule scattering logic that deterministically
// distributes workflow execution times based on workflow identifiers.

// stableHash returns a deterministic hash value in the range [0, modulo)
// using FNV-1a hash algorithm, which is stable across platforms and Go versions.
func stableHash(s string, modulo int) int {
	h := fnv.New32a()
	// hash.Hash.Write never returns an error in practice, but check to satisfy gosec G104
	if _, err := h.Write([]byte(s)); err != nil {
		// Return 0 (safe fallback) if write somehow fails
		scheduleFuzzyScatterLog.Printf("Warning: hash write failed: %v", err)
		return 0
	}
	return int(h.Sum32() % uint32(modulo))
}

// ScatterSchedule takes a fuzzy cron expression and a workflow identifier
// and returns a deterministic scattered time for that workflow
func ScatterSchedule(fuzzyCron, workflowIdentifier string) (string, error) {
	scheduleFuzzyScatterLog.Printf("Scattering schedule: fuzzyCron=%s, workflowId=%s", fuzzyCron, workflowIdentifier)
	if !IsFuzzyCron(fuzzyCron) {
		scheduleFuzzyScatterLog.Printf("Invalid fuzzy cron expression: %s", fuzzyCron)
		return "", fmt.Errorf("not a fuzzy schedule: %s", fuzzyCron)
	}

	// For FUZZY:DAILY_AROUND:HH:MM * * *, scatter around the target time
	if strings.HasPrefix(fuzzyCron, "FUZZY:DAILY_AROUND:") {
		// Extract the target hour and minute from FUZZY:DAILY_AROUND:HH:MM
		parts := strings.Split(fuzzyCron, " ")
		if len(parts) < 1 {
			return "", fmt.Errorf("invalid fuzzy daily around pattern: %s", fuzzyCron)
		}

		// Parse the target time from FUZZY:DAILY_AROUND:HH:MM
		timePart := strings.TrimPrefix(parts[0], "FUZZY:DAILY_AROUND:")
		timeParts := strings.Split(timePart, ":")
		if len(timeParts) != 2 {
			return "", fmt.Errorf("invalid time format in fuzzy daily around pattern: %s", fuzzyCron)
		}

		targetHour, err := strconv.Atoi(timeParts[0])
		if err != nil || targetHour < 0 || targetHour > 23 {
			return "", fmt.Errorf("invalid target hour in fuzzy daily around pattern: %s", fuzzyCron)
		}

		targetMinute, err := strconv.Atoi(timeParts[1])
		if err != nil || targetMinute < 0 || targetMinute > 59 {
			return "", fmt.Errorf("invalid target minute in fuzzy daily around pattern: %s", fuzzyCron)
		}

		// Calculate target time in minutes since midnight
		targetMinutes := targetHour*60 + targetMinute

		// Define the scattering window: ±1 hour (120 minutes total range)
		windowSize := 120 // Total window is 2 hours (±1 hour)

		// Use a stable hash to get a deterministic offset within the window
		hash := stableHash(workflowIdentifier, windowSize)

		// Calculate offset from target time: range is [-60, +59] minutes
		offset := hash - (windowSize / 2)

		// Apply offset to target time
		scatteredMinutes := targetMinutes + offset

		// Handle wrap-around (keep within 0-1439 minutes, which is 0:00-23:59)
		for scatteredMinutes < 0 {
			scatteredMinutes += 24 * 60
		}
		for scatteredMinutes >= 24*60 {
			scatteredMinutes -= 24 * 60
		}

		hour := scatteredMinutes / 60
		minute := scatteredMinutes % 60

		result := fmt.Sprintf("%d %d * * *", minute, hour)
		scheduleFuzzyScatterLog.Printf("FUZZY:DAILY_AROUND scattered: original=%d:%d, scattered=%d:%d, result=%s", targetHour, targetMinute, hour, minute, result)
		// Return scattered daily cron: minute hour * * *
		return result, nil
	}

	// For FUZZY:DAILY_BETWEEN:START_H:START_M:END_H:END_M * * *, scatter within the time range
	if strings.HasPrefix(fuzzyCron, "FUZZY:DAILY_BETWEEN:") {
		// Extract the start and end times from FUZZY:DAILY_BETWEEN:START_H:START_M:END_H:END_M
		parts := strings.Split(fuzzyCron, " ")
		if len(parts) < 1 {
			return "", fmt.Errorf("invalid fuzzy daily between pattern: %s", fuzzyCron)
		}

		// Parse the times from FUZZY:DAILY_BETWEEN:START_H:START_M:END_H:END_M
		timePart := strings.TrimPrefix(parts[0], "FUZZY:DAILY_BETWEEN:")
		timeParts := strings.Split(timePart, ":")
		if len(timeParts) != 4 {
			return "", fmt.Errorf("invalid time format in fuzzy daily between pattern: %s", fuzzyCron)
		}

		startHour, err := strconv.Atoi(timeParts[0])
		if err != nil || startHour < 0 || startHour > 23 {
			return "", fmt.Errorf("invalid start hour in fuzzy daily between pattern: %s", fuzzyCron)
		}

		startMinute, err := strconv.Atoi(timeParts[1])
		if err != nil || startMinute < 0 || startMinute > 59 {
			return "", fmt.Errorf("invalid start minute in fuzzy daily between pattern: %s", fuzzyCron)
		}

		endHour, err := strconv.Atoi(timeParts[2])
		if err != nil || endHour < 0 || endHour > 23 {
			return "", fmt.Errorf("invalid end hour in fuzzy daily between pattern: %s", fuzzyCron)
		}

		endMinute, err := strconv.Atoi(timeParts[3])
		if err != nil || endMinute < 0 || endMinute > 59 {
			return "", fmt.Errorf("invalid end minute in fuzzy daily between pattern: %s", fuzzyCron)
		}

		// Calculate start and end times in minutes since midnight
		startMinutes := startHour*60 + startMinute
		endMinutes := endHour*60 + endMinute

		// Calculate the range size, handling ranges that cross midnight
		var rangeSize int
		if endMinutes > startMinutes {
			// Normal case: range within a single day (e.g., 9:00 to 17:00)
			rangeSize = endMinutes - startMinutes
		} else {
			// Range crosses midnight (e.g., 22:00 to 02:00)
			rangeSize = (24*60 - startMinutes) + endMinutes
		}

		// Use a stable hash to get a deterministic offset within the range
		hash := stableHash(workflowIdentifier, rangeSize)

		// Calculate the scattered time by adding hash offset to start time
		scatteredMinutes := startMinutes + hash

		// Handle wrap-around for ranges that cross midnight
		if scatteredMinutes >= 24*60 {
			scatteredMinutes -= 24 * 60
		}

		hour := scatteredMinutes / 60
		minute := scatteredMinutes % 60

		result := fmt.Sprintf("%d %d * * *", minute, hour)
		scheduleFuzzyScatterLog.Printf("FUZZY:DAILY_BETWEEN scattered: start=%d:%d, end=%d:%d, scattered=%d:%d, result=%s", startHour, startMinute, endHour, endMinute, hour, minute, result)
		// Return scattered daily cron: minute hour * * *
		return result, nil
	}

	// For FUZZY:DAILY * * *, we scatter across 24 hours
	if strings.HasPrefix(fuzzyCron, "FUZZY:DAILY") {
		// Use a stable hash of the workflow identifier to get a deterministic time
		hash := stableHash(workflowIdentifier, 1440) // Total minutes in a day

		hour := hash / 60
		minute := hash % 60

		result := fmt.Sprintf("%d %d * * *", minute, hour)
		scheduleFuzzyScatterLog.Printf("FUZZY:DAILY scattered: hash=%d, result=%s", hash, result)
		// Return scattered daily cron: minute hour * * *
		return result, nil
	}

	// For FUZZY:HOURLY/N * * *, we scatter the minute offset within the hour
	if strings.HasPrefix(fuzzyCron, "FUZZY:HOURLY/") {
		// Extract the interval from FUZZY:HOURLY/N
		parts := strings.Split(fuzzyCron, " ")
		if len(parts) < 1 {
			return "", fmt.Errorf("invalid fuzzy hourly pattern: %s", fuzzyCron)
		}

		hourlyPart := parts[0]
		intervalStr := strings.TrimPrefix(hourlyPart, "FUZZY:HOURLY/")
		interval, err := strconv.Atoi(intervalStr)
		if err != nil {
			return "", fmt.Errorf("invalid interval in fuzzy hourly pattern: %s", fuzzyCron)
		}

		// Use a stable hash to get a deterministic minute offset (0-59)
		minute := stableHash(workflowIdentifier, 60)

		result := fmt.Sprintf("%d */%d * * *", minute, interval)
		scheduleFuzzyScatterLog.Printf("FUZZY:HOURLY/%d scattered: minute=%d, result=%s", interval, minute, result)
		// Return scattered hourly cron: minute */N * * *
		return result, nil
	}

	// For FUZZY:WEEKLY_AROUND:DOW:HH:MM * * *, scatter around the target time on specific weekday
	if strings.HasPrefix(fuzzyCron, "FUZZY:WEEKLY_AROUND:") {
		// Extract the weekday and target time from FUZZY:WEEKLY_AROUND:DOW:HH:MM
		parts := strings.Split(fuzzyCron, " ")
		if len(parts) < 1 {
			return "", fmt.Errorf("invalid fuzzy weekly around pattern: %s", fuzzyCron)
		}

		// Parse the weekday and time from FUZZY:WEEKLY_AROUND:DOW:HH:MM
		timePart := strings.TrimPrefix(parts[0], "FUZZY:WEEKLY_AROUND:")
		timeParts := strings.Split(timePart, ":")
		if len(timeParts) != 3 {
			return "", fmt.Errorf("invalid format in fuzzy weekly around pattern: %s", fuzzyCron)
		}

		weekday := timeParts[0]
		targetHour, err := strconv.Atoi(timeParts[1])
		if err != nil || targetHour < 0 || targetHour > 23 {
			return "", fmt.Errorf("invalid target hour in fuzzy weekly around pattern: %s", fuzzyCron)
		}

		targetMinute, err := strconv.Atoi(timeParts[2])
		if err != nil || targetMinute < 0 || targetMinute > 59 {
			return "", fmt.Errorf("invalid target minute in fuzzy weekly around pattern: %s", fuzzyCron)
		}

		// Calculate target time in minutes since midnight
		targetMinutes := targetHour*60 + targetMinute

		// Define the scattering window: ±1 hour (120 minutes total range)
		windowSize := 120 // Total window is 2 hours (±1 hour)

		// Use a stable hash to get a deterministic offset within the window
		hash := stableHash(workflowIdentifier, windowSize)

		// Calculate offset from target time: range is [-60, +59] minutes
		offset := hash - (windowSize / 2)

		// Apply offset to target time
		scatteredMinutes := targetMinutes + offset

		// Handle wrap-around (keep within 0-1439 minutes, which is 0:00-23:59)
		for scatteredMinutes < 0 {
			scatteredMinutes += 24 * 60
		}
		for scatteredMinutes >= 24*60 {
			scatteredMinutes -= 24 * 60
		}

		hour := scatteredMinutes / 60
		minute := scatteredMinutes % 60

		result := fmt.Sprintf("%d %d * * %s", minute, hour, weekday)
		scheduleFuzzyScatterLog.Printf("FUZZY:WEEKLY_AROUND scattered: weekday=%s, target=%d:%d, scattered=%d:%d, result=%s", weekday, targetHour, targetMinute, hour, minute, result)
		// Return scattered weekly cron: minute hour * * DOW
		return result, nil
	}

	// For FUZZY:WEEKLY:DOW * * *, we scatter time on specific weekday
	if strings.HasPrefix(fuzzyCron, "FUZZY:WEEKLY:") {
		// Extract the weekday from FUZZY:WEEKLY:DOW
		parts := strings.Split(fuzzyCron, " ")
		if len(parts) < 1 {
			return "", fmt.Errorf("invalid fuzzy weekly pattern: %s", fuzzyCron)
		}

		weekdayPart := strings.TrimPrefix(parts[0], "FUZZY:WEEKLY:")
		weekday := weekdayPart

		// Use a stable hash of the workflow identifier to get a deterministic time
		hash := stableHash(workflowIdentifier, 1440) // Total minutes in a day

		hour := hash / 60
		minute := hash % 60

		result := fmt.Sprintf("%d %d * * %s", minute, hour, weekday)
		scheduleFuzzyScatterLog.Printf("FUZZY:WEEKLY:%s scattered: hash=%d, result=%s", weekday, hash, result)
		// Return scattered weekly cron: minute hour * * DOW
		return result, nil
	}

	// For FUZZY:WEEKLY * * *, we scatter across all weekdays and times
	if strings.HasPrefix(fuzzyCron, "FUZZY:WEEKLY") {
		// Use a stable hash of the workflow identifier to get a deterministic weekday and time
		// Total possibilities: 7 days * 1440 minutes = 10080 minutes in a week
		hash := stableHash(workflowIdentifier, 10080)

		// Extract weekday (0-6) and time within that day
		weekday := hash / 1440      // Which day of the week (0-6)
		minutesInDay := hash % 1440 // Which minute of that day (0-1439)
		hour := minutesInDay / 60
		minute := minutesInDay % 60

		result := fmt.Sprintf("%d %d * * %d", minute, hour, weekday)
		scheduleFuzzyScatterLog.Printf("FUZZY:WEEKLY scattered: weekday=%d, time=%d:%d, result=%s", weekday, hour, minute, result)
		// Return scattered weekly cron: minute hour * * DOW
		return result, nil
	}

	// For FUZZY:BI_WEEKLY * * *, we scatter across 2 weeks (14 days)
	if strings.HasPrefix(fuzzyCron, "FUZZY:BI_WEEKLY") {
		// Use a stable hash of the workflow identifier to get a deterministic day and time
		// Total possibilities: 14 days * 1440 minutes = 20160 minutes in 2 weeks
		hash := stableHash(workflowIdentifier, 20160)

		// Extract time within a day (scatter across 2 weeks)
		minutesInDay := hash % 1440 // Which minute of that day (0-1439)
		hour := minutesInDay / 60
		minute := minutesInDay % 60

		result := fmt.Sprintf("%d %d */%d * *", minute, hour, 14)
		scheduleFuzzyScatterLog.Printf("FUZZY:BI_WEEKLY scattered: time=%d:%d, result=%s", hour, minute, result)
		// Convert to cron: We use day-of-month pattern with 14-day interval
		// Schedule every 14 days at the scattered time
		return result, nil
	}

	// For FUZZY:TRI_WEEKLY * * *, we scatter across 3 weeks (21 days)
	if strings.HasPrefix(fuzzyCron, "FUZZY:TRI_WEEKLY") {
		// Use a stable hash of the workflow identifier to get a deterministic day and time
		// Total possibilities: 21 days * 1440 minutes = 30240 minutes in 3 weeks
		hash := stableHash(workflowIdentifier, 30240)

		// Extract time within a day (scatter across 3 weeks)
		minutesInDay := hash % 1440 // Which minute of that day (0-1439)
		hour := minutesInDay / 60
		minute := minutesInDay % 60

		result := fmt.Sprintf("%d %d */%d * *", minute, hour, 21)
		scheduleFuzzyScatterLog.Printf("FUZZY:TRI_WEEKLY scattered: time=%d:%d, result=%s", hour, minute, result)
		// Convert to cron: We use day-of-month pattern with 21-day interval
		// Schedule every 21 days at the scattered time
		return result, nil
	}

	scheduleFuzzyScatterLog.Printf("Unsupported fuzzy schedule type: %s", fuzzyCron)
	return "", fmt.Errorf("unsupported fuzzy schedule type: %s", fuzzyCron)
}
