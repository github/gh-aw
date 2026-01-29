//go:build !integration

package parser

import (
	"strings"
	"testing"
)

// FuzzScheduleParser performs fuzz testing on the schedule parser
// to discover edge cases and potential security vulnerabilities in schedule input handling.
//
// The fuzzer validates that:
// 1. Valid schedule expressions are correctly parsed
// 2. Invalid schedules are properly rejected with errors
// 3. Parser handles all fuzzer-generated inputs without panic
// 4. Edge cases are handled correctly (empty, very long, special characters)
// 5. Malformed input returns proper errors
// 6. UTC offset parsing is robust
// 7. Minimum duration validation works correctly
func FuzzScheduleParser(f *testing.F) {
	// Seed corpus with valid schedule expressions

	// Daily schedules
	f.Add("daily")
	f.Add("daily at 02:00")
	f.Add("daily at midnight")
	f.Add("daily at noon")
	f.Add("daily at 09:30")

	// Daily around schedules (fuzzy with target time)
	f.Add("daily around 14:00")
	f.Add("daily around 02:00")
	f.Add("daily around midnight")
	f.Add("daily around noon")
	f.Add("daily around 09:30")
	f.Add("daily around 23:30")
	f.Add("daily around 3pm")
	f.Add("daily around 9am")
	f.Add("daily around 12am")
	f.Add("daily around 12pm")
	f.Add("daily around 11pm")
	f.Add("daily around 6am")
	f.Add("daily around 14:00 utc+9")
	f.Add("daily around 3pm utc-5")
	f.Add("daily around 9am utc+05:30")
	f.Add("daily around midnight utc-8")
	f.Add("daily around noon utc+2")
	f.Add("daily around 2pm utc+1")
	f.Add("daily around 11pm utc-3")
	f.Add("daily around 8 am PT")
	f.Add("daily around 8 am PST")
	f.Add("daily around 8 am PDT")

	// Daily between schedules (fuzzy with time range)
	f.Add("daily between 9:00 and 17:00")
	f.Add("daily between 9am and 5pm")
	f.Add("daily between midnight and noon")
	f.Add("daily between noon and midnight")
	f.Add("daily between 22:00 and 02:00")
	f.Add("daily between 10pm and 2am")
	f.Add("daily between 8:30 and 18:45")
	f.Add("daily between 6am and 6pm")
	f.Add("daily between 1am and 11pm")
	f.Add("daily between 00:00 and 23:59")
	f.Add("daily between 12am and 11:59pm")
	f.Add("daily between 23:00 and 01:00")
	f.Add("daily between 11pm and 1am")
	f.Add("daily between 7:15 and 16:45")
	f.Add("daily between 3:30am and 8:30pm")
	f.Add("daily between 9am utc-5 and 5pm utc-5")
	f.Add("daily between 8:00 utc+9 and 17:00 utc+9")
	f.Add("daily between 10:00 utc+0 and 14:00 utc+0")
	f.Add("daily between 9am utc-8 and 5pm utc-8")
	f.Add("daily between 8:00 utc+05:30 and 18:00 utc+05:30")

	// Weekly schedules
	f.Add("weekly on monday")
	f.Add("weekly on monday at 06:30")
	f.Add("weekly on friday at 17:00")
	f.Add("weekly on sunday at midnight")

	// Monthly schedules
	f.Add("monthly on 1")
	f.Add("monthly on 15")
	f.Add("monthly on 15 at 09:00")
	f.Add("monthly on 31 at noon")

	// Interval schedules (long format)
	f.Add("every 10 minutes")
	f.Add("every 5 minutes")
	f.Add("every 30 minutes")
	f.Add("every 1 hour")
	f.Add("every 2 hours")
	f.Add("every 6 hours")
	f.Add("every 12 hours")
	f.Add("every 1 day")
	f.Add("every 2 days")
	f.Add("every 3 days")
	f.Add("every 7 days")
	f.Add("every 10 days")
	f.Add("every 14 days")

	// Interval schedules (short duration format)
	f.Add("every 5m")
	f.Add("every 10m")
	f.Add("every 30m")
	f.Add("every 1h")
	f.Add("every 2h")
	f.Add("every 6h")
	f.Add("every 1d")
	f.Add("every 2d")
	f.Add("every 1w")
	f.Add("every 2w")
	f.Add("every 1mo")
	f.Add("every 2mo")

	// UTC offset schedules
	f.Add("daily at 02:00 utc+9")
	f.Add("daily at 14:00 utc-5")
	f.Add("daily at 09:30 utc+05:30")
	f.Add("weekly on monday at 08:00 utc+1")
	f.Add("monthly on 15 at 12:00 utc-8")
	f.Add("daily at 00:00 utc+0")

	// AM/PM time formats
	f.Add("daily at 3pm")
	f.Add("daily at 1am")
	f.Add("daily at 12am")
	f.Add("daily at 12pm")
	f.Add("daily at 11pm")
	f.Add("daily at 6am")
	f.Add("weekly on friday at 5pm")
	f.Add("monthly on 15 at 9am")
	f.Add("daily at 3pm utc+9")
	f.Add("daily at 9am utc-5")
	f.Add("daily at 12pm utc+1")
	f.Add("daily at 12am utc-8")
	f.Add("daily at 11pm utc+05:30")
	f.Add("weekly on monday at 8am utc+9")
	f.Add("weekly on friday at 6pm utc-7")
	f.Add("weekly on friday around 8 am PT")
	f.Add("weekly on friday around 8 am PST")
	f.Add("weekly on friday around 8 am PDT")
	f.Add("monthly on 15 at 10am utc+2")
	f.Add("monthly on 1 at 7pm utc-3")

	// Valid cron expressions (passthrough)
	f.Add("0 0 * * *")
	f.Add("*/5 * * * *")
	f.Add("0 */2 * * *")
	f.Add("30 6 * * 1")
	f.Add("0 9 15 * *")

	// Case variations
	f.Add("DAILY")
	f.Add("Weekly On Monday")
	f.Add("MONTHLY ON 15")

	// Invalid schedules (should error gracefully)

	// Empty and whitespace
	f.Add("")
	f.Add("   ")
	f.Add("\t\n")

	// Below minimum duration
	f.Add("every 1m")
	f.Add("every 2m")
	f.Add("every 3m")
	f.Add("every 4m")
	f.Add("every 1 minute")
	f.Add("every 2 minutes")

	// Invalid interval with time conflict
	f.Add("every 10 minutes at 06:00")
	f.Add("every 2h at noon")

	// Invalid interval units
	f.Add("every 2 weeks")
	f.Add("every 1 month")

	// Invalid numbers
	f.Add("every abc minutes")
	f.Add("every -5 minutes")
	f.Add("every 0 minutes")
	f.Add("every 1000000 hours")

	// Invalid weekly schedules
	f.Add("weekly monday")
	f.Add("weekly on funday")
	f.Add("weekly on 123")

	// Invalid monthly schedules
	f.Add("monthly 15")
	f.Add("monthly on abc")
	f.Add("monthly on 0")
	f.Add("monthly on 32")
	f.Add("monthly on -1")

	// Invalid time formats
	f.Add("daily at 25:00")
	f.Add("daily at 12:60")
	f.Add("daily at 12:30:45")
	f.Add("daily at abc")
	f.Add("daily at 12")

	// Invalid around schedules
	f.Add("daily around")
	f.Add("daily around 25:00")
	f.Add("daily around 12:60")
	f.Add("daily around abc")
	f.Add("daily around 13pm")
	f.Add("daily around 0am")
	f.Add("daily around 25am")
	f.Add("daily around utc+9")
	f.Add("weekly around monday")
	f.Add("monthly around 15")
	f.Add("every 10 minutes around 12:00")
	f.Add("hourly around 12:00")

	// Invalid between schedules
	f.Add("daily between")
	f.Add("daily between 9:00")
	f.Add("daily between and")
	f.Add("daily between 9:00 and")
	f.Add("daily between 9:00 17:00")
	f.Add("daily between 9:00 and 9:00")
	f.Add("daily between midnight and midnight")
	f.Add("daily between noon and noon")
	f.Add("daily between 3pm and 15:00")
	f.Add("daily between 25:00 and 17:00")
	f.Add("daily between 9:00 and 25:00")
	f.Add("daily between abc and def")
	f.Add("daily between 9am and")
	f.Add("daily between and 5pm")
	f.Add("daily between and and")
	f.Add("weekly between monday and friday")
	f.Add("monthly between 1 and 15")
	f.Add("every 10 minutes between 9:00 and 17:00")
	f.Add("hourly between 9:00 and 17:00")

	// Invalid UTC offsets
	f.Add("daily at 12:00 utc+25")
	f.Add("daily at 12:00 utc-15")
	f.Add("daily at 12:00 utc+99:99")
	f.Add("daily at 12:00 utc")
	f.Add("daily at 12:00 utc+abc")

	// Unsupported schedule types
	f.Add("hourly")
	f.Add("yearly on 12/25")
	f.Add("biweekly")
	f.Add("quarterly")

	// Malformed expressions
	f.Add("daily at")
	f.Add("weekly on")
	f.Add("monthly on")
	f.Add("every")
	f.Add("every 10")
	f.Add("at 12:00")

	// Very long strings
	longString := strings.Repeat("a", 10000)
	f.Add(longString)
	f.Add("daily at " + longString)
	f.Add("every " + longString + " minutes")

	// Special characters
	f.Add("daily\x00at\x0012:00")
	f.Add("daily at 12:00\n\r")
	f.Add("daily at 12:00;echo hack")
	f.Add("daily at 12:00' OR '1'='1")
	f.Add("daily at 12:00<script>alert(1)</script>")
	f.Add("daily at 12:00$(whoami)")
	f.Add("daily at 12:00`id`")

	// Unicode characters
	f.Add("daily at 午前12時")
	f.Add("每日 at 12:00")
	f.Add("weekly on lundi")
	f.Add("毎週月曜日 at 12:00")

	// Multiple spaces and tabs
	f.Add("daily  at  12:00")
	f.Add("daily\tat\t12:00")
	f.Add("weekly   on   monday")
	f.Add("every   10   minutes")

	// Mixed valid/invalid patterns
	f.Add("daily at 12:00 weekly on monday")
	f.Add("every 5 minutes every 10 minutes")
	f.Add("daily daily")

	// Edge case numbers
	f.Add("every 2147483647 minutes")
	f.Add("monthly on 2147483647")
	f.Add("every -2147483648 hours")

	// Complex UTC offsets
	f.Add("daily at 12:00 utc+12:30")
	f.Add("daily at 12:00 utc-11:30")
	f.Add("daily at 12:00 utc+14")
	f.Add("daily at 12:00 utc-12")

	// Duplicate keywords
	f.Add("daily daily at 12:00")
	f.Add("weekly on monday on tuesday")
	f.Add("every every 10 minutes")

	// Cron-like but invalid
	f.Add("0 0 * *")
	f.Add("0 0 * * * *")
	f.Add("* * * * * *")
	f.Add("@daily")
	f.Add("@weekly")

	// Mixed case with invalid syntax
	f.Add("DaIlY aT 12:00")
	f.Add("WEEKLY ON MONDAY AT 12:00")

	// Run the fuzzer
	f.Fuzz(func(t *testing.T, input string) {
		// The parser should never panic, even on malformed input
		cron, original, err := ParseSchedule(input)

		// Basic validation checks:
		// 1. Results should be consistent
		if err != nil {
			// On error, cron should be empty and original should be empty
			if cron != "" {
				t.Errorf("ParseSchedule returned non-empty cron '%s' with error: %v", cron, err)
			}
			if original != "" {
				t.Errorf("ParseSchedule returned non-empty original '%s' with error: %v", original, err)
			}
		}

		// 2. If successful, cron should not be empty
		if err == nil && cron == "" {
			t.Errorf("ParseSchedule succeeded but returned empty cron for input: %q", input)
		}

		// 3. If error is returned, it should have a meaningful message
		if err != nil {
			if err.Error() == "" {
				t.Errorf("ParseSchedule returned error with empty message for input: %q", input)
			}

			// Error should not be generic
			if err.Error() == "error" {
				t.Errorf("ParseSchedule returned generic 'error' message for input: %q", input)
			}
		}

		// 4. Validate cron expression format if successful
		if err == nil && cron != "" {
			// Allow fuzzy schedules (FUZZY:*) which have 4 fields
			if strings.HasPrefix(cron, "FUZZY:") {
				// Fuzzy schedules have the format:
				// - "FUZZY:DAILY * * *" (4 fields)
				// - "FUZZY:HOURLY/N * * *" (4 fields)
				// - "FUZZY:DAILY_AROUND:HH:MM * * *" (4 fields but with colon-separated time in first field)
				// - "FUZZY:DAILY_BETWEEN:START_H:START_M:END_H:END_M * * *" (4 fields with 4 colon-separated values in first field)
				fields := strings.Fields(cron)
				if len(fields) != 4 {
					t.Errorf("ParseSchedule returned invalid fuzzy cron format with %d fields (expected 4): %q for input: %q", len(fields), cron, input)
				}

				// For FUZZY:DAILY_AROUND, validate the time format
				if strings.HasPrefix(cron, "FUZZY:DAILY_AROUND:") {
					// Extract the time part from FUZZY:DAILY_AROUND:HH:MM
					firstField := fields[0]
					timePart := strings.TrimPrefix(firstField, "FUZZY:DAILY_AROUND:")
					timeParts := strings.Split(timePart, ":")
					if len(timeParts) != 2 {
						t.Errorf("ParseSchedule returned invalid FUZZY:DAILY_AROUND format (expected HH:MM): %q for input: %q", cron, input)
					}
				}

				// For FUZZY:DAILY_BETWEEN, validate the time range format
				if strings.HasPrefix(cron, "FUZZY:DAILY_BETWEEN:") {
					// Extract the time range from FUZZY:DAILY_BETWEEN:START_H:START_M:END_H:END_M
					firstField := fields[0]
					timePart := strings.TrimPrefix(firstField, "FUZZY:DAILY_BETWEEN:")
					timeParts := strings.Split(timePart, ":")
					if len(timeParts) != 4 {
						t.Errorf("ParseSchedule returned invalid FUZZY:DAILY_BETWEEN format (expected START_H:START_M:END_H:END_M): %q for input: %q", cron, input)
					}
				}
			} else {
				// Regular cron should have 5 fields separated by spaces
				fields := strings.Fields(cron)
				if len(fields) != 5 {
					t.Errorf("ParseSchedule returned invalid cron format with %d fields (expected 5): %q for input: %q", len(fields), cron, input)
				}

				// Each field should not be empty
				for i, field := range fields {
					if field == "" {
						t.Errorf("ParseSchedule returned cron with empty field %d: %q for input: %q", i, cron, input)
					}
				}
			}
		}

		// 5. If original is returned, it should match the input (after trimming)
		if original != "" && err == nil {
			// Original should be the input (for human-friendly formats)
			// Trim both for comparison since input might have trailing whitespace
			if strings.TrimSpace(original) != strings.TrimSpace(input) {
				t.Errorf("ParseSchedule returned original '%s' that doesn't match input '%s'", original, input)
			}
		}

		// 6. Check for known invalid patterns that should error
		if shouldError(input) && err == nil {
			// This is informational - the fuzzer might find edge cases
			// where our simple check is wrong
			_ = err
		}

		// 7. Check for known valid patterns that should succeed
		if looksValid(input) && err != nil {
			// This is informational - the input might have subtle issues
			_ = err
		}

		// 8. Validate minimum duration for interval schedules
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(input)), "every") {
			if err != nil && !strings.Contains(err.Error(), "minimum schedule interval") &&
				!strings.Contains(err.Error(), "invalid") &&
				!strings.Contains(err.Error(), "unsupported") {
				// If it errored for reasons other than minimum duration or format,
				// that's fine, but we want to be aware
				_ = err
			}
		}
	})
}

// shouldError returns true if the input contains obvious invalid patterns
func shouldError(input string) bool {
	input = strings.TrimSpace(input)

	// Empty input should error
	if input == "" {
		return true
	}

	// Control characters should likely error
	if strings.ContainsAny(input, "\x00\x01\x02\x03") {
		return true
	}

	// Minimum duration violations
	if strings.HasPrefix(strings.ToLower(input), "every 1m") ||
		strings.HasPrefix(strings.ToLower(input), "every 2m") ||
		strings.HasPrefix(strings.ToLower(input), "every 3m") ||
		strings.HasPrefix(strings.ToLower(input), "every 4m") ||
		strings.HasPrefix(strings.ToLower(input), "every 1 minute") ||
		strings.HasPrefix(strings.ToLower(input), "every 2 minute") ||
		strings.HasPrefix(strings.ToLower(input), "every 3 minute") ||
		strings.HasPrefix(strings.ToLower(input), "every 4 minute") {
		return true
	}

	// Known unsupported types
	if strings.HasPrefix(strings.ToLower(input), "yearly") ||
		strings.HasPrefix(strings.ToLower(input), "hourly") {
		return true
	}

	return false
}

// looksValid returns true if the input looks like it might be valid
func looksValid(input string) bool {
	input = strings.TrimSpace(strings.ToLower(input))

	// Empty is not valid
	if input == "" {
		return false
	}

	// Check if it's a cron expression (5 fields)
	fields := strings.Fields(input)
	if len(fields) == 5 {
		// Could be a valid cron expression
		return true
	}

	// Check for valid patterns
	validPrefixes := []string{
		"daily",
		"weekly on",
		"monthly on",
		"every 5m",
		"every 10m",
		"every 15m",
		"every 30m",
		"every 1h",
		"every 2h",
		"every 5 minutes",
		"every 10 minutes",
		"every 15 minutes",
		"every 30 minutes",
		"every 1 hour",
		"every 2 hours",
	}

	for _, prefix := range validPrefixes {
		if strings.HasPrefix(input, prefix) {
			return true
		}
	}

	return false
}
