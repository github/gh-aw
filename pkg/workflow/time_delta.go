package workflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/logger"
)

var timeDeltaLog = logger.New("workflow:time_delta")

// Pre-compiled regexes for time parsing (performance optimization)
var (
	timeDeltaPattern = regexp.MustCompile(`(\d+)(mo|w|d|h|m)`)
	ordinalPattern   = regexp.MustCompile(`\b(\d+)(st|nd|rd|th)\b`)
)

// TimeDelta represents a time duration that can be added to a base time
type TimeDelta struct {
	Hours   int
	Days    int
	Minutes int
	Weeks   int
	Months  int
}

// parseTimeDelta parses a relative time delta string like "+25h", "+3d", "+1w", "+1mo", "+1d12h30m", etc.
// Supported formats:
// - +25h (25 hours)
// - +3d (3 days)
// - +1w (1 week)
// - +1mo (1 month)
// - +30m (30 minutes)
// - +1d12h (1 day and 12 hours)
// - +2d5h30m (2 days, 5 hours, 30 minutes)
// - +1mo2w3d (1 month, 2 weeks, 3 days)
func parseTimeDelta(deltaStr string) (*TimeDelta, error) {
	return parseTimeDeltaWithMinutes(deltaStr, true)
}

// parseTimeDeltaForStopAfter parses a relative time delta string for stop-after configuration.
// Unlike parseTimeDelta, this function does NOT accept minute or second units.
// The minimum unit for stop-after is hours.
// Supported formats:
// - +25h (25 hours)
// - +3d (3 days)
// - +1w (1 week)
// - +1mo (1 month)
// - +1d12h (1 day and 12 hours)
// - +1mo2w3d (1 month, 2 weeks, 3 days)
func parseTimeDeltaForStopAfter(deltaStr string) (*TimeDelta, error) {
	return parseTimeDeltaWithMinutes(deltaStr, false)
}

// parseTimeDeltaWithMinutes parses a relative time delta string with optional minute support
func parseTimeDeltaWithMinutes(deltaStr string, allowMinutes bool) (*TimeDelta, error) {
	timeDeltaLog.Printf("Parsing time delta: input=%s, allowMinutes=%v", deltaStr, allowMinutes)

	if deltaStr == "" {
		return nil, fmt.Errorf("empty time delta")
	}

	// Must start with '+'
	if !strings.HasPrefix(deltaStr, "+") {
		timeDeltaLog.Printf("Time delta validation failed: missing '+' prefix")
		return nil, fmt.Errorf("time delta must start with '+', got: %s", deltaStr)
	}

	// Remove the '+' prefix
	deltaStr = deltaStr[1:]

	if deltaStr == "" {
		return nil, fmt.Errorf("empty time delta after '+'")
	}

	// Parse components using regex
	// Pattern matches: number followed by mo/w/d/h/m (months, weeks, days, hours, minutes)
	matches := timeDeltaPattern.FindAllStringSubmatch(deltaStr, -1)

	if len(matches) == 0 {
		return nil, fmt.Errorf("invalid time delta format: +%s. Expected format like +25h, +3d, +1w, +1mo, +1d12h30m", deltaStr)
	}

	// Check that all characters are consumed by matches
	consumed := 0
	for _, match := range matches {
		consumed += len(match[0])
	}
	if consumed != len(deltaStr) {
		return nil, fmt.Errorf("invalid time delta format: +%s. Extra characters detected", deltaStr)
	}

	delta := &TimeDelta{}
	seenUnits := make(map[string]bool)

	for _, match := range matches {
		if len(match) != 3 {
			continue
		}

		valueStr := match[1]
		unit := match[2]

		// Check for duplicate units
		if seenUnits[unit] {
			return nil, fmt.Errorf("duplicate unit '%s' in time delta: +%s", unit, deltaStr)
		}
		seenUnits[unit] = true

		value, err := strconv.Atoi(valueStr)
		if err != nil {
			return nil, fmt.Errorf("invalid number '%s' in time delta: +%s", valueStr, deltaStr)
		}

		if value < 0 {
			return nil, fmt.Errorf("negative values not allowed in time delta: +%s", deltaStr)
		}

		switch unit {
		case "mo":
			delta.Months = value
		case "w":
			delta.Weeks = value
		case "d":
			delta.Days = value
		case "h":
			delta.Hours = value
		case "m":
			if !allowMinutes {
				return nil, fmt.Errorf("minute unit 'm' is not allowed for stop-after. Minimum unit is hours 'h'. Use +%dh instead of +%dm", (value+59)/60, value)
			}
			delta.Minutes = value
		default:
			return nil, fmt.Errorf("unsupported time unit '%s' in time delta: +%s", unit, deltaStr)
		}
	}

	// Validate reasonable limits
	if delta.Months > MaxTimeDeltaMonths {
		return nil, fmt.Errorf("time delta too large: %d months exceeds maximum of %d months", delta.Months, MaxTimeDeltaMonths)
	}
	if delta.Weeks > MaxTimeDeltaWeeks {
		return nil, fmt.Errorf("time delta too large: %d weeks exceeds maximum of %d weeks", delta.Weeks, MaxTimeDeltaWeeks)
	}
	if delta.Days > MaxTimeDeltaDays {
		return nil, fmt.Errorf("time delta too large: %d days exceeds maximum of %d days", delta.Days, MaxTimeDeltaDays)
	}
	if delta.Hours > MaxTimeDeltaHours {
		return nil, fmt.Errorf("time delta too large: %d hours exceeds maximum of %d hours", delta.Hours, MaxTimeDeltaHours)
	}
	if delta.Minutes > MaxTimeDeltaMinutes {
		return nil, fmt.Errorf("time delta too large: %d minutes exceeds maximum of %d minutes", delta.Minutes, MaxTimeDeltaMinutes)
	}

	timeDeltaLog.Printf("Parsed time delta successfully: %s", delta.String())
	return delta, nil
}

// String returns a human-readable representation of the TimeDelta
func (td *TimeDelta) String() string {
	var parts []string
	if td.Months > 0 {
		parts = append(parts, fmt.Sprintf("%dmo", td.Months))
	}
	if td.Weeks > 0 {
		parts = append(parts, fmt.Sprintf("%dw", td.Weeks))
	}
	if td.Days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", td.Days))
	}
	if td.Hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", td.Hours))
	}
	if td.Minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", td.Minutes))
	}
	if len(parts) == 0 {
		return "0m"
	}
	return "+" + strings.Join(parts, "")
}

// isRelativeStopTime checks if a stop-time value is a relative time delta
func isRelativeStopTime(stopTime string) bool {
	return strings.HasPrefix(stopTime, "+")
}

// parseAbsoluteDateTime parses various date-time formats and returns a standardized timestamp
func parseAbsoluteDateTime(dateTimeStr string) (string, error) {
	timeDeltaLog.Printf("Parsing absolute date-time: %s", dateTimeStr)

	// Try multiple date-time formats in order of preference
	formats := []string{
		// Standard formats
		"2006-01-02 15:04:05",  // YYYY-MM-DD HH:MM:SS
		"2006-01-02T15:04:05",  // ISO 8601 without timezone
		"2006-01-02T15:04:05Z", // ISO 8601 UTC
		"2006-01-02 15:04",     // YYYY-MM-DD HH:MM
		"2006-01-02",           // YYYY-MM-DD (defaults to start of day)

		// Alternative formats
		"01/02/2006 15:04:05", // MM/DD/YYYY HH:MM:SS
		"01/02/2006 15:04",    // MM/DD/YYYY HH:MM
		"01/02/2006",          // MM/DD/YYYY
		"02/01/2006 15:04:05", // DD/MM/YYYY HH:MM:SS
		"02/01/2006 15:04",    // DD/MM/YYYY HH:MM
		"02/01/2006",          // DD/MM/YYYY

		// Readable formats
		"January 2, 2006 15:04:05", // January 2, 2006 15:04:05
		"January 2, 2006 15:04",    // January 2, 2006 15:04
		"January 2, 2006",          // January 2, 2006
		"Jan 2, 2006 15:04:05",     // Jan 2, 2006 15:04:05
		"Jan 2, 2006 15:04",        // Jan 2, 2006 15:04
		"Jan 2, 2006",              // Jan 2, 2006
		"2 January 2006 15:04:05",  // 2 January 2006 15:04:05
		"2 January 2006 15:04",     // 2 January 2006 15:04
		"2 January 2006",           // 2 January 2006
		"2 Jan 2006 15:04:05",      // 2 Jan 2006 15:04:05
		"2 Jan 2006 15:04",         // 2 Jan 2006 15:04
		"2 Jan 2006",               // 2 Jan 2006
		"January 2 2006 15:04:05",  // January 2 2006 15:04:05 (no comma)
		"January 2 2006 15:04",     // January 2 2006 15:04 (no comma)
		"January 2 2006",           // January 2 2006 (no comma)
		"Jan 2 2006 15:04:05",      // Jan 2 2006 15:04:05 (no comma)
		"Jan 2 2006 15:04",         // Jan 2 2006 15:04 (no comma)
		"Jan 2 2006",               // Jan 2 2006 (no comma)

		// RFC formats
		time.RFC3339, // 2006-01-02T15:04:05Z07:00
		time.RFC822,  // 02 Jan 06 15:04 MST
		time.RFC850,  // Monday, 02-Jan-06 15:04:05 MST
		time.RFC1123, // Mon, 02 Jan 2006 15:04:05 MST
	}

	// Clean up the input string
	dateTimeStr = strings.TrimSpace(dateTimeStr)

	// Handle ordinal numbers (1st, 2nd, 3rd, 4th, etc.)
	dateTimeStr = ordinalPattern.ReplaceAllString(dateTimeStr, "$1")

	// Try to parse with each format
	for _, format := range formats {
		if parsed, err := time.Parse(format, dateTimeStr); err == nil {
			// Successfully parsed, convert to UTC and return in standard format
			result := parsed.UTC().Format("2006-01-02 15:04:05")
			timeDeltaLog.Printf("Successfully parsed date-time using format, result: %s", result)
			return result, nil
		}
	}

	// Try with more flexible ordinal handling - sometimes the ordinal removal creates double spaces
	normalizedStr := strings.ReplaceAll(dateTimeStr, "  ", " ")
	normalizedStr = strings.TrimSpace(normalizedStr)

	for _, format := range formats {
		if parsed, err := time.Parse(format, normalizedStr); err == nil {
			// Successfully parsed, convert to UTC and return in standard format
			result := parsed.UTC().Format("2006-01-02 15:04:05")
			timeDeltaLog.Printf("Successfully parsed date-time using ordinal normalization, result: %s", result)
			return result, nil
		}
	}

	// If none of the standard formats work, try some smart parsing
	// Handle formats like "June 1st 2025", "1st June 2025", etc.
	smartFormats := []string{
		"January 2nd 2006",
		"2nd January 2006",
		"Jan 2nd 2006",
		"2nd Jan 2006",
		"January 2nd 2006 15:04",
		"2nd January 2006 15:04",
		"Jan 2nd 2006 15:04",
		"2nd Jan 2006 15:04",
		"January 2nd 2006 15:04:05",
		"2nd January 2006 15:04:05",
		"Jan 2nd 2006 15:04:05",
		"2nd Jan 2006 15:04:05",
	}

	for _, format := range smartFormats {
		if parsed, err := time.Parse(format, dateTimeStr); err == nil {
			return parsed.UTC().Format("2006-01-02 15:04:05"), nil
		}
	}

	return "", fmt.Errorf("unable to parse date-time: %s. Supported formats include: YYYY-MM-DD HH:MM:SS, MM/DD/YYYY, January 2 2006, 1st June 2025, etc", dateTimeStr)
}

// isRelativeDate checks if a date string is a relative time delta (starts with + or -)
func isRelativeDate(dateStr string) bool {
	return strings.HasPrefix(dateStr, "+") || strings.HasPrefix(dateStr, "-")
}

// parseRelativeDate parses a relative date string like "-1d", "-1w", "-1mo", "+3d", etc.
// Supports both positive (+) and negative (-) deltas for log filtering use cases.
// Supported formats:
// - -1d (1 day ago)
// - -1w (1 week ago)
// - -1mo (1 month ago)
// - +3d (3 days from now)
// - -2w3d (2 weeks and 3 days ago)
func parseRelativeDate(dateStr string) (*TimeDelta, bool, error) {
	if dateStr == "" {
		return nil, false, fmt.Errorf("empty date string")
	}

	// Check if it's a relative date
	if !isRelativeDate(dateStr) {
		return nil, false, nil // Not a relative date, caller should handle as absolute
	}

	// Determine if it's negative (going backwards in time)
	isNegative := strings.HasPrefix(dateStr, "-")

	// Convert to positive format for parsing with existing parseTimeDelta
	var deltaStr string
	if isNegative {
		deltaStr = "+" + dateStr[1:] // Replace - with +
	} else {
		deltaStr = dateStr // Already has +
	}

	// Parse using existing function
	delta, err := parseTimeDelta(deltaStr)
	if err != nil {
		return nil, false, err
	}

	return delta, isNegative, nil
}

// ResolveRelativeDate resolves a relative date string to an absolute timestamp
// suitable for use with GitHub CLI.
// If the date string is not relative, it returns the original string.
//
// Returns a full ISO 8601 timestamp (YYYY-MM-DDTHH:MM:SSZ) for precise filtering.
func ResolveRelativeDate(dateStr string, baseTime time.Time) (string, error) {
	if dateStr == "" {
		return "", nil
	}

	// Check if it's a relative date
	delta, isNegative, err := parseRelativeDate(dateStr)
	if err != nil {
		return "", err
	}
	if delta == nil {
		// Not a relative date, return as-is
		return dateStr, nil
	}

	// Calculate the absolute time using precise calculation
	// Always use AddDate for months, weeks, and days for maximum precision
	absoluteTime := baseTime.UTC()
	if isNegative {
		absoluteTime = absoluteTime.AddDate(0, -delta.Months, -delta.Weeks*7-delta.Days)
		absoluteTime = absoluteTime.Add(-time.Duration(delta.Hours)*time.Hour - time.Duration(delta.Minutes)*time.Minute)
	} else {
		absoluteTime = absoluteTime.AddDate(0, delta.Months, delta.Weeks*7+delta.Days)
		absoluteTime = absoluteTime.Add(time.Duration(delta.Hours)*time.Hour + time.Duration(delta.Minutes)*time.Minute)
	}

	// Return full ISO 8601 timestamp for precise filtering
	return absoluteTime.Format(time.RFC3339), nil
}

// parseExpiresFromConfig parses expires value from config map.
// Supports both integer (hours or days) and string formats like "2h", "7d", "2w", "1m", "1y"
// Also supports boolean false to explicitly disable expiration (returns -1)
// Returns the number of hours, -1 if explicitly disabled (false), or 0 if invalid or not present
// Note: For uint64 values, returns 0 if the value would overflow int.
// Note: Integer values without units are treated as days and converted to hours (for backward compatibility)
func parseExpiresFromConfig(configMap map[string]any) int {
	timeDeltaLog.Printf("DEBUG: parseExpiresFromConfig called with configMap: %+v", configMap)
	if expires, exists := configMap["expires"]; exists {
		// Try numeric types first
		switch v := expires.(type) {
		case bool:
			// false explicitly disables expiration
			if !v {
				timeDeltaLog.Print("expires set to false, expiration disabled")
				return -1
			}
			// true is not a valid expires value
			return 0
		case int:
			// Integer values without units are treated as days for backward compatibility
			return v * 24
		case int64:
			return int(v) * 24
		case float64:
			return int(v) * 24
		case uint64:
			// Check for overflow before converting uint64 to int
			const maxInt = int(^uint(0) >> 1)
			if v > uint64(maxInt/24) {
				timeDeltaLog.Printf("uint64 value %d for expires exceeds max int value, returning 0", v)
				return 0
			}
			return int(v) * 24
		case string:
			// Parse relative time specification like "2h", "7d", "2w", "1m", "1y"
			return parseRelativeTimeSpec(v)
		}
	}
	return 0
}

// parseRelativeTimeSpec parses a relative time specification string.
// Supports: h (hours), d (days), w (weeks), m (months ~30 days), y (years ~365 days)
// Examples: "2h" = 2 hours, "7d" = 168 hours, "2w" = 336 hours, "1m" = 720 hours, "1y" = 8760 hours
// Returns 0 if the format is invalid or if the duration is less than 2 hours
func parseRelativeTimeSpec(spec string) int {
	timeDeltaLog.Printf("DEBUG: parseRelativeTimeSpec called with spec: %s", spec)
	if spec == "" {
		return 0
	}

	// Get the last character (unit)
	unit := spec[len(spec)-1:]
	// Get the number part
	numStr := spec[:len(spec)-1]

	// Parse the number
	var num int
	_, err := fmt.Sscanf(numStr, "%d", &num)
	if err != nil || num <= 0 {
		timeDeltaLog.Printf("Invalid expires time spec number: %s", spec)
		return 0
	}

	// Convert to hours based on unit
	switch unit {
	case "h", "H":
		// Reject durations less than 2 hours
		if num < 2 {
			timeDeltaLog.Printf("Invalid expires duration: %d hours is less than the minimum 2 hours", num)
			return 0
		}
		timeDeltaLog.Printf("Parsed %d hours from spec: %s", num, spec)
		return num
	case "d", "D":
		hours := num * 24
		timeDeltaLog.Printf("Converted %d days to %d hours", num, hours)
		return hours
	case "w", "W":
		hours := num * 7 * 24
		timeDeltaLog.Printf("Converted %d weeks to %d hours", num, hours)
		return hours
	case "m", "M":
		hours := num * 30 * 24 // months to hours (approximate)
		timeDeltaLog.Printf("Converted %d months to %d hours", num, hours)
		return hours
	case "y", "Y":
		hours := num * 365 * 24 // years to hours (approximate)
		timeDeltaLog.Printf("Converted %d years to %d hours", num, hours)
		return hours
	default:
		timeDeltaLog.Printf("Invalid expires time spec unit: %s", spec)
		return 0
	}
}
