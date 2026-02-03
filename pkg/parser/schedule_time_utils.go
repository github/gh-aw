package parser

import (
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var scheduleTimeUtilsLog = logger.New("parser:schedule_time_utils")

// This file contains time parsing and conversion utilities used by the schedule parser.
// These helper functions transform various time formats into minute/hour values for cron expressions.

// normalizeTimezoneAbbreviation converts common timezone abbreviations to UTC offsets
func normalizeTimezoneAbbreviation(token string) (string, bool) {
	switch strings.ToLower(token) {
	case "pt", "pst":
		scheduleTimeUtilsLog.Printf("Warning: PT timezone is ambiguous; treating as UTC-8 (PST)")
		return "utc-8", true
	case "pdt":
		return "utc-7", true
	case "est":
		return "utc-5", true
	case "edt":
		return "utc-4", true
	default:
		return "", false
	}
}

// isAMPMToken checks if a token is an AM/PM indicator
func isAMPMToken(token string) bool {
	switch strings.ToLower(token) {
	case "am", "pm":
		return true
	default:
		return false
	}
}

// normalizeTimeTokens combines time tokens into a normalized string
// Handles time, AM/PM, and timezone tokens
func normalizeTimeTokens(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}

	timeStr := tokens[0]
	nextIndex := 1
	if len(tokens) > 1 && isAMPMToken(tokens[1]) {
		timeStr += " " + tokens[1]
		nextIndex = 2
	}

	if len(tokens) > nextIndex {
		timezoneToken := strings.ToLower(tokens[nextIndex])
		if strings.HasPrefix(timezoneToken, "utc") {
			timeStr = timeStr + " " + timezoneToken
		} else if normalized, ok := normalizeTimezoneAbbreviation(timezoneToken); ok {
			timeStr = timeStr + " " + normalized
		}
	}

	return timeStr
}

// parseTimeToMinutes converts hour and minute strings to total minutes since midnight
func parseTimeToMinutes(hourStr, minuteStr string) int {
	hour, _ := strconv.Atoi(hourStr)
	minute, _ := strconv.Atoi(minuteStr)
	return hour*60 + minute
}

// parseTime converts a time string to minute and hour, with optional UTC offset
// Supports formats: HH:MM, midnight, noon, 3pm, 1am, HH:MM utc+N, HH:MM utc+HH:MM, HH:MM utc-N, 3pm utc+9
func parseTime(timeStr string) (minute string, hour string) {
	// Check for UTC offset
	parts := strings.Split(timeStr, " ")
	var utcOffset int
	var baseTime string

	if len(parts) == 2 && strings.HasPrefix(strings.ToLower(parts[1]), "utc") {
		baseTime = parts[0]
		offsetStr := strings.ToLower(parts[1])
		utcOffset = parseUTCOffset(offsetStr)
	} else if len(parts) == 3 && isAMPMToken(parts[1]) && strings.HasPrefix(strings.ToLower(parts[2]), "utc") {
		baseTime = parts[0] + " " + parts[1]
		offsetStr := strings.ToLower(parts[2])
		utcOffset = parseUTCOffset(offsetStr)
	} else {
		baseTime = timeStr
	}

	var baseMinute, baseHour int

	switch baseTime {
	case "midnight":
		baseMinute, baseHour = 0, 0
	case "noon":
		baseMinute, baseHour = 0, 12
	default:
		// Check for am/pm format (e.g., "3pm", "11am")
		lowerTime := strings.ToLower(baseTime)
		if strings.HasSuffix(lowerTime, "am") || strings.HasSuffix(lowerTime, "pm") {
			isPM := strings.HasSuffix(lowerTime, "pm")
			// Remove am/pm suffix
			timePart := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(lowerTime, "am"), "pm"))
			hourNum, minNum, ok := parseHourMinute(timePart)
			if !ok || hourNum < 1 || hourNum > 12 {
				return "0", "0"
			}
			if minNum < 0 || minNum > 59 {
				return "0", "0"
			}
			// Convert 12-hour to 24-hour format
			if isPM {
				if hourNum != 12 {
					hourNum += 12
				}
			} else { // AM
				if hourNum == 12 {
					hourNum = 0
				}
			}
			baseMinute, baseHour = minNum, hourNum
		} else {
			// Parse HH:MM format
			if !strings.Contains(baseTime, ":") {
				return "0", "0"
			}
			hourNum, minNum, ok := parseHourMinute(baseTime)
			if !ok || hourNum < 0 || hourNum > 23 {
				return "0", "0"
			}
			if minNum < 0 || minNum > 59 {
				return "0", "0"
			}
			baseMinute, baseHour = minNum, hourNum
		}
	}

	// Apply UTC offset (convert from local time to UTC)
	// If utc+9, we subtract 9 hours to get UTC time
	totalMinutes := baseHour*60 + baseMinute - utcOffset

	// Handle wrap-around (keep within 0-1439 minutes, which is 0:00-23:59)
	for totalMinutes < 0 {
		totalMinutes += 24 * 60
	}
	for totalMinutes >= 24*60 {
		totalMinutes -= 24 * 60
	}

	finalHour := totalMinutes / 60
	finalMinute := totalMinutes % 60

	return strconv.Itoa(finalMinute), strconv.Itoa(finalHour)
}

// parseUTCOffset parses UTC offset strings (e.g., utc+9, utc-5, utc+09:00, utc-05:30)
// Returns the offset in minutes
func parseUTCOffset(offsetStr string) int {
	// Parse UTC offset (e.g., utc+9, utc-5, utc+09:00, utc-05:30)
	if len(offsetStr) <= 3 {
		return 0
	}

	offsetPart := offsetStr[3:] // Skip "utc"
	sign := 1
	if strings.HasPrefix(offsetPart, "+") {
		offsetPart = offsetPart[1:]
	} else if strings.HasPrefix(offsetPart, "-") {
		sign = -1
		offsetPart = offsetPart[1:]
	}

	// Check if it's HH:MM format
	if strings.Contains(offsetPart, ":") {
		offsetParts := strings.Split(offsetPart, ":")
		if len(offsetParts) == 2 {
			hours, err1 := strconv.Atoi(offsetParts[0])
			mins, err2 := strconv.Atoi(offsetParts[1])
			if err1 == nil && err2 == nil {
				return sign * (hours*60 + mins)
			}
		}
		return 0
	}

	// Just hours (e.g., utc+9)
	hours, err := strconv.Atoi(offsetPart)
	if err != nil {
		return 0
	}
	return sign * hours * 60
}

// parseHourMinute parses time parts in HH:MM or HH format
// Returns hour, minute, and success flag
func parseHourMinute(timePart string) (int, int, bool) {
	if strings.Contains(timePart, ":") {
		timeParts := strings.Split(timePart, ":")
		if len(timeParts) != 2 {
			return 0, 0, false
		}
		hourNum, err := strconv.Atoi(timeParts[0])
		if err != nil {
			return 0, 0, false
		}
		minNum, err := strconv.Atoi(timeParts[1])
		if err != nil {
			return 0, 0, false
		}
		return hourNum, minNum, true
	}

	hourNum, err := strconv.Atoi(timePart)
	if err != nil {
		return 0, 0, false
	}
	return hourNum, 0, true
}

// mapWeekday maps day names to cron day-of-week numbers (0=Sunday, 6=Saturday)
func mapWeekday(day string) string {
	day = strings.ToLower(day)
	weekdays := map[string]string{
		"sunday":    "0",
		"sun":       "0",
		"monday":    "1",
		"mon":       "1",
		"tuesday":   "2",
		"tue":       "2",
		"wednesday": "3",
		"wed":       "3",
		"thursday":  "4",
		"thu":       "4",
		"friday":    "5",
		"fri":       "5",
		"saturday":  "6",
		"sat":       "6",
	}
	return weekdays[day]
}
