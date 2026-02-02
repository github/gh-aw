package cli

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var scheduleCodemodLog = logger.New("cli:codemod_schedule")

// getScheduleAtToAroundCodemod creates a codemod for converting "daily at TIME" to "daily around TIME"
func getScheduleAtToAroundCodemod() Codemod {
	return Codemod{
		ID:           "schedule-at-to-around-migration",
		Name:         "Migrate schedule 'at' syntax to 'around' syntax",
		Description:  "Converts deprecated 'daily at TIME', 'weekly on DAY at TIME', and 'monthly on N at TIME' to fuzzy schedules or standard cron",
		IntroducedIn: "0.5.0",
		Apply: func(content string, frontmatter map[string]any) (string, bool, error) {
			// Parse frontmatter to get raw lines
			frontmatterLines, markdown, err := parseFrontmatterLines(content)
			if err != nil {
				return content, false, err
			}

			var modified bool
			result := make([]string, len(frontmatterLines))

			for i, line := range frontmatterLines {
				trimmedLine := strings.TrimSpace(line)
				originalLine := line

				// Skip if not a cron or schedule line
				if !strings.Contains(trimmedLine, "cron:") && !strings.Contains(trimmedLine, "schedule:") {
					result[i] = originalLine
					continue
				}

				// Extract leading whitespace to preserve indentation
				leadingSpace := getIndentation(line)

				// Check if this is a list item (starts with - after whitespace)
				restAfterSpace := strings.TrimLeft(line, " \t")
				var listMarker string
				if strings.HasPrefix(restAfterSpace, "-") {
					// This is a list item, preserve the dash
					listMarker = "- "
				}

				// Extract the schedule value (after "cron:" or "schedule:")
				var scheduleValue string
				var fieldName string

				if strings.Contains(trimmedLine, "cron:") {
					parts := strings.SplitN(trimmedLine, "cron:", 2)
					if len(parts) == 2 {
						fieldName = "cron"
						scheduleValue = strings.TrimSpace(parts[1])
					}
				} else if strings.Contains(trimmedLine, "schedule:") {
					parts := strings.SplitN(trimmedLine, "schedule:", 2)
					if len(parts) == 2 {
						fieldName = "schedule"
						scheduleValue = strings.TrimSpace(parts[1])
					}
				}

				if scheduleValue == "" {
					result[i] = originalLine
					continue
				}

				// Remove quotes if present
				scheduleValue = strings.Trim(scheduleValue, "\"'")

				// Pattern 1: daily at TIME (not "daily around" or "daily between")
				if strings.HasPrefix(scheduleValue, "daily at") && !strings.Contains(scheduleValue, "around") && !strings.Contains(scheduleValue, "between") {
					newSchedule := strings.Replace(scheduleValue, "daily at", "daily around", 1)
					result[i] = fmt.Sprintf("%s%s%s: %s", leadingSpace, listMarker, fieldName, newSchedule)
					modified = true
					scheduleCodemodLog.Printf("Converted 'daily at' to 'daily around' on line %d: %s -> %s", i+1, scheduleValue, newSchedule)
					continue
				}

				// Pattern 2: weekly on DAY at TIME
				if strings.Contains(scheduleValue, "weekly on") && strings.Contains(scheduleValue, " at ") && !strings.Contains(scheduleValue, "around") {
					newSchedule := strings.Replace(scheduleValue, " at ", " around ", 1)
					result[i] = fmt.Sprintf("%s%s%s: %s", leadingSpace, listMarker, fieldName, newSchedule)
					modified = true
					scheduleCodemodLog.Printf("Converted 'weekly on DAY at' to 'weekly on DAY around' on line %d: %s -> %s", i+1, scheduleValue, newSchedule)
					continue
				}

				// Pattern 3: monthly on N [at TIME] - convert to cron
				if strings.HasPrefix(scheduleValue, "monthly on") {
					// Extract day number
					var day string
					var cronExpr string

					monthlyParts := strings.Fields(scheduleValue)
					for idx, part := range monthlyParts {
						if part == "on" && idx+1 < len(monthlyParts) {
							day = monthlyParts[idx+1]
							break
						}
					}

					if day != "" {
						// Check if there's a time specification
						if strings.Contains(scheduleValue, " at ") {
							// Has time - default to 09:00 as example since we can't parse arbitrary times in codemod
							// The user should manually adjust the hour/minute if needed
							cronExpr = fmt.Sprintf("0 9 %s * *", day)
						} else {
							// No time - suggest midnight
							cronExpr = fmt.Sprintf("0 0 %s * *", day)
						}

						// Replace with cron and add explanatory comment
						result[i] = fmt.Sprintf("%s%s%s: \"%s\"  # Converted from '%s' (adjust time as needed)", leadingSpace, listMarker, fieldName, cronExpr, scheduleValue)
						modified = true
						scheduleCodemodLog.Printf("Converted 'monthly on' to cron on line %d: %s -> %s", i+1, scheduleValue, cronExpr)
						continue
					}
				}

				result[i] = originalLine
			}

			if !modified {
				return content, false, nil
			}

			// Reconstruct the content
			newContent := reconstructContent(result, markdown)
			scheduleCodemodLog.Print("Applied schedule 'at' to 'around' migration")
			return newContent, true, nil
		},
	}
}
