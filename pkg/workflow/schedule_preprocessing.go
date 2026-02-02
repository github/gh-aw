package workflow

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var schedulePreprocessingLog = logger.New("workflow:schedule_preprocessing")

// normalizeScheduleString handles the common schedule string parsing, warning emission,
// fuzzy scattering, and validation logic. It returns the normalized cron expression
// and the original friendly format, or an error if validation fails.
func (c *Compiler) normalizeScheduleString(scheduleStr string, itemIndex int) (parsedCron string, friendlyFormat string, err error) {
	// Try to parse as a schedule expression
	parsedCron, original, err := parser.ParseSchedule(scheduleStr)
	if err != nil {
		// Return error for array items, but return nil error for top-level parsing
		// (caller will handle differently based on context)
		if itemIndex >= 0 {
			return "", "", fmt.Errorf("invalid schedule expression in item %d: %w", itemIndex, err)
		}
		return "", "", err
	}

	// Warn if using explicit daily cron pattern
	if parser.IsDailyCron(parsedCron) && !parser.IsFuzzyCron(parsedCron) {
		c.addDailyCronWarning(parsedCron)
	}

	// Warn if using hourly interval with fixed minute
	if parser.IsHourlyCron(parsedCron) && !parser.IsFuzzyCron(parsedCron) {
		c.addHourlyCronWarning(parsedCron)
	}

	// Warn if using explicit weekly cron pattern with fixed time
	if parser.IsWeeklyCron(parsedCron) && !parser.IsFuzzyCron(parsedCron) {
		c.addWeeklyCronWarning(parsedCron)
	}

	// Scatter fuzzy schedules if workflow identifier is set
	if parser.IsFuzzyCron(parsedCron) && c.workflowIdentifier != "" {
		// Combine repo slug and workflow identifier for scattering seed
		// This ensures workflows with the same name in different repositories
		// get different execution times, distributing load across an organization.
		// Format: "owner/repo/workflow-path" or just "workflow-path" if no repo slug
		seed := c.workflowIdentifier
		if c.repositorySlug != "" {
			seed = c.repositorySlug + "/" + c.workflowIdentifier
		} else {
			// Warn if repository slug is not available - scattering will not be org-aware
			schedulePreprocessingLog.Printf("Warning: repository slug not available for fuzzy schedule scattering")
			c.IncrementWarningCount()
			c.addScheduleWarning("Fuzzy schedule scattering without repository context. Workflows with the same name in different repositories may collide. Ensure you are in a git repository with a configured remote.")
		}
		scatteredCron, err := parser.ScatterSchedule(parsedCron, seed)
		if err != nil {
			schedulePreprocessingLog.Printf("Warning: failed to scatter fuzzy schedule: %v", err)
			// Keep the original fuzzy schedule as fallback
		} else {
			schedulePreprocessingLog.Printf("Scattered fuzzy schedule %s to %s for workflow %s", parsedCron, scatteredCron, c.workflowIdentifier)
			parsedCron = scatteredCron
			// Update the friendly format to show the scattering
			if original != "" {
				original = fmt.Sprintf("%s (scattered)", original)
			}
		}
	}

	// Validate final cron expression has correct syntax (5 fields)
	// FUZZY cron expressions are not supported by GitHub Actions
	if parser.IsFuzzyCron(parsedCron) {
		if itemIndex >= 0 {
			return "", "", fmt.Errorf("fuzzy cron expression '%s' in item %d must be scattered to proper cron format before compilation (ensure workflow identifier is set)", parsedCron, itemIndex)
		}
		return "", "", fmt.Errorf("fuzzy cron expression '%s' must be scattered to proper cron format before compilation (ensure workflow identifier is set)", parsedCron)
	}
	if !parser.IsCronExpression(parsedCron) {
		if itemIndex >= 0 {
			return "", "", fmt.Errorf("invalid cron expression '%s' in item %d: must have exactly 5 fields (minute hour day-of-month month day-of-week)", parsedCron, itemIndex)
		}
		return "", "", fmt.Errorf("invalid cron expression '%s': must have exactly 5 fields (minute hour day-of-month month day-of-week)", parsedCron)
	}

	return parsedCron, original, nil
}

// preprocessScheduleFields converts human-friendly schedule expressions to cron expressions
// in the frontmatter's "on" section. It modifies the frontmatter map in place.
func (c *Compiler) preprocessScheduleFields(frontmatter map[string]any, markdownPath string, content string) error {
	schedulePreprocessingLog.Print("Preprocessing schedule fields in frontmatter")

	// Check if "on" field exists
	onValue, exists := frontmatter["on"]
	if !exists {
		return nil
	}

	// Check if "on" is a string - might be a schedule expression, slash command shorthand, label trigger shorthand, or other trigger shorthand
	if onStr, ok := onValue.(string); ok {
		schedulePreprocessingLog.Printf("Processing on field as string: %s", onStr)

		// Check if it's a slash command shorthand (starts with /)
		commandName, isSlashCommand, err := parseSlashCommandShorthand(onStr)
		if err != nil {
			return err
		}
		if isSlashCommand {
			schedulePreprocessingLog.Printf("Converting shorthand 'on: %s' to slash_command + workflow_dispatch", onStr)

			// Create the expanded format
			onMap := expandSlashCommandShorthand(commandName)
			frontmatter["on"] = onMap

			return nil
		}

		// Check if it's a label trigger shorthand (labeled label1 label2...)
		entityType, labelNames, isLabelTrigger, err := parseLabelTriggerShorthand(onStr)
		if err != nil {
			return err
		}
		if isLabelTrigger {
			schedulePreprocessingLog.Printf("Converting shorthand 'on: %s' to %s labeled + workflow_dispatch", onStr, entityType)

			// Create the expanded format
			onMap := expandLabelTriggerShorthand(entityType, labelNames)
			frontmatter["on"] = onMap

			return nil
		}

		// Try the new unified trigger parser for other trigger shorthands
		triggerIR, err := ParseTriggerShorthand(onStr)
		if err != nil {
			// Wrap the error with source location information
			return c.createTriggerParseError(markdownPath, content, onStr, err)
		}
		if triggerIR != nil {
			schedulePreprocessingLog.Printf("Converting shorthand 'on: %s' to structured trigger", onStr)

			// Convert IR to YAML map
			onMap := triggerIR.ToYAMLMap()
			frontmatter["on"] = onMap

			return nil
		}

		// Try to parse as a schedule expression (only if not already recognized as another trigger type)
		parsedCron, original, err := c.normalizeScheduleString(onStr, -1)
		if err != nil {
			// Check if this is an explicit rejection of unsupported syntax
			// vs. just not being a valid schedule at all
			if strings.Contains(err.Error(), "syntax is not supported") {
				// This is an explicit rejection - return the error
				return err
			}
			// Not a schedule expression either - leave as simple string trigger
			// (simple event names like "push", "fork", etc. are valid)
			schedulePreprocessingLog.Printf("Not a recognized shorthand or schedule: %s - leaving as-is", onStr)
			return nil
		}

		schedulePreprocessingLog.Printf("Converting shorthand 'on: %s' to schedule + workflow_dispatch", onStr)

		// Create schedule array format with workflow_dispatch
		scheduleArray := []any{
			map[string]any{
				"cron": parsedCron,
			},
		}

		// Replace the simple "on: schedule" with expanded format
		onMap := map[string]any{
			"schedule":          scheduleArray,
			"workflow_dispatch": nil,
		}
		frontmatter["on"] = onMap

		// Store friendly format if it was converted
		if original != "" {
			if c.scheduleFriendlyFormats == nil {
				c.scheduleFriendlyFormats = make(map[int]string)
			}
			c.scheduleFriendlyFormats[0] = original
		}

		return nil
	}

	// Only process if "on" is a map (object format)
	onMap, ok := onValue.(map[string]any)
	if !ok {
		// If "on" is neither string nor map, something is wrong
		return nil
	}

	// Check if schedule field exists in the "on" map
	scheduleValue, hasSchedule := onMap["schedule"]
	if !hasSchedule {
		return nil
	}

	// Handle shorthand string format: schedule: "daily at 02:00"
	if scheduleStr, ok := scheduleValue.(string); ok {
		schedulePreprocessingLog.Printf("Converting shorthand schedule string to array format: %s", scheduleStr)
		// Convert string to array format with single item
		parsedCron, original, err := c.normalizeScheduleString(scheduleStr, -1)
		if err != nil {
			return fmt.Errorf("invalid schedule expression: %w", err)
		}

		// Create array format
		scheduleArray := []any{
			map[string]any{
				"cron": parsedCron,
			},
		}
		onMap["schedule"] = scheduleArray

		// Store friendly format if it was converted
		if original != "" {
			if c.scheduleFriendlyFormats == nil {
				c.scheduleFriendlyFormats = make(map[int]string)
			}
			c.scheduleFriendlyFormats[0] = original
		}

		// Add workflow_dispatch if not already present
		if _, hasWorkflowDispatch := onMap["workflow_dispatch"]; !hasWorkflowDispatch {
			schedulePreprocessingLog.Printf("Adding workflow_dispatch to scheduled workflow")
			onMap["workflow_dispatch"] = nil
		}

		return nil
	}

	// Schedule should be an array of schedule items
	scheduleArray, ok := scheduleValue.([]any)
	if !ok {
		return fmt.Errorf("schedule field must be a string or an array")
	}

	// Initialize friendly formats map for this compilation
	if c.scheduleFriendlyFormats == nil {
		c.scheduleFriendlyFormats = make(map[int]string)
	}

	// Process each schedule item
	schedulePreprocessingLog.Printf("Processing %d schedule items", len(scheduleArray))
	for i, item := range scheduleArray {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("schedule item %d must be an object with a 'cron' field", i)
		}

		cronValue, hasCron := itemMap["cron"]
		if !hasCron {
			return fmt.Errorf("schedule item %d missing 'cron' field", i)
		}

		cronStr, ok := cronValue.(string)
		if !ok {
			return fmt.Errorf("schedule item %d 'cron' field must be a string", i)
		}

		// Try to parse as human-friendly schedule
		parsedCron, original, err := c.normalizeScheduleString(cronStr, i)
		if err != nil {
			// Error already includes item index from normalizeScheduleString
			return err
		}

		// Update the cron field with the parsed cron expression
		itemMap["cron"] = parsedCron

		// If there was an original friendly format, store it for later use
		if original != "" {
			c.scheduleFriendlyFormats[i] = original
		}
	}

	// Add workflow_dispatch if not already present
	if _, hasWorkflowDispatch := onMap["workflow_dispatch"]; !hasWorkflowDispatch {
		schedulePreprocessingLog.Printf("Adding workflow_dispatch to scheduled workflow")
		onMap["workflow_dispatch"] = nil
	}

	return nil
}

// createTriggerParseError creates a detailed error for trigger parsing issues with source location
func (c *Compiler) createTriggerParseError(filePath, content, triggerStr string, err error) error {
	schedulePreprocessingLog.Printf("Creating trigger parse error for: %s", triggerStr)

	lines := strings.Split(content, "\n")

	// Find the line where "on:" appears in the frontmatter
	var onLine int
	var onColumn int
	inFrontmatter := false

	for i, line := range lines {
		lineNum := i + 1

		// Check for frontmatter delimiter
		if strings.TrimSpace(line) == "---" {
			if !inFrontmatter {
				inFrontmatter = true
			} else {
				// End of frontmatter
				break
			}
			continue
		}

		if inFrontmatter {
			// Look for "on:" field
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "on:") {
				onLine = lineNum
				// Find the column where "on:" starts
				onColumn = strings.Index(line, "on:") + 1
				break
			}
		}
	}

	// If we found the line, create a formatted error
	if onLine > 0 {
		// Create context lines around the error
		var context []string
		startLine := max(1, onLine-2)
		endLine := min(len(lines), onLine+2)

		for i := startLine; i <= endLine; i++ {
			if i-1 < len(lines) {
				context = append(context, lines[i-1])
			}
		}

		compilerErr := console.CompilerError{
			Position: console.ErrorPosition{
				File:   filePath,
				Line:   onLine,
				Column: onColumn,
			},
			Type:    "error",
			Message: fmt.Sprintf("trigger syntax error: %s", err.Error()),
			Context: context,
		}

		// Format and return the error
		formattedErr := console.FormatError(compilerErr)
		return errors.New(formattedErr)
	}

	// Fallback to original error if we can't find the line
	schedulePreprocessingLog.Printf("Could not find 'on:' line in frontmatter, using fallback error")
	return fmt.Errorf("trigger syntax error: %w", err)
}

// addFriendlyScheduleComments adds comments showing the original friendly format for schedule cron expressions
// This function is called after the YAML has been generated from the frontmatter
func (c *Compiler) addFriendlyScheduleComments(yamlStr string, frontmatter map[string]any) string {
	// Retrieve the friendly formats for this compilation
	if len(c.scheduleFriendlyFormats) == 0 {
		return yamlStr
	}

	// Process the YAML string to add comments
	lines := strings.Split(yamlStr, "\n")
	var result []string
	scheduleItemIndex := -1
	inScheduleArray := false

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Check if we're entering the schedule array
		if strings.HasPrefix(trimmedLine, "schedule:") {
			inScheduleArray = true
			scheduleItemIndex = -1
			result = append(result, line)
			continue
		}

		// Check if we're leaving the schedule section (new top-level key)
		if inScheduleArray && strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "\t") {
			inScheduleArray = false
		}

		// If we're in the schedule array and find a cron line, add the friendly comment
		if inScheduleArray && strings.Contains(trimmedLine, "cron:") {
			scheduleItemIndex++
			result = append(result, line)

			// Add friendly format comment if available
			if friendly, exists := c.scheduleFriendlyFormats[scheduleItemIndex]; exists {
				// Get the indentation of the cron line
				indentation := ""
				if len(line) > len(trimmedLine) {
					indentation = line[:len(line)-len(trimmedLine)]
				}
				// Add comment with friendly format on the next line
				comment := indentation + "  # Friendly format: " + friendly
				result = append(result, comment)
			}
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// addDailyCronWarning emits a warning when a daily cron pattern with fixed time is detected
func (c *Compiler) addDailyCronWarning(cronExpr string) {
	// Extract hour and minute from the cron expression
	fields := strings.Fields(cronExpr)
	if len(fields) >= 2 {
		minute := fields[0]
		hour := fields[1]
		schedulePreprocessingLog.Printf("Warning: detected daily cron with fixed time: %s", cronExpr)

		// Construct the warning message
		warningMsg := fmt.Sprintf(
			"Schedule uses fixed daily time (%s:%s UTC). Consider using fuzzy schedule 'daily' instead to distribute workflow execution times and reduce load spikes.",
			hour, minute,
		)

		// This warning is added to the warning count
		// It will be collected and displayed by the compilation process
		c.IncrementWarningCount()

		// Store the warning for later display
		c.addScheduleWarning(warningMsg)
	}
}

// addHourlyCronWarning emits a warning when an hourly interval with fixed minute is detected
func (c *Compiler) addHourlyCronWarning(cronExpr string) {
	// Extract minute and interval from the cron expression
	fields := strings.Fields(cronExpr)
	if len(fields) >= 2 {
		minute := fields[0]
		hourField := fields[1]
		schedulePreprocessingLog.Printf("Warning: detected hourly cron with fixed minute: %s", cronExpr)

		// Extract the interval from */N pattern
		interval := strings.TrimPrefix(hourField, "*/")

		// Construct the warning message
		warningMsg := fmt.Sprintf(
			"Schedule uses hourly interval with fixed minute offset (%s). Consider using fuzzy schedule 'every %sh' instead to distribute workflow execution times and reduce load spikes.",
			minute, interval,
		)

		// This warning is added to the warning count
		c.IncrementWarningCount()

		// Store the warning for later display
		c.addScheduleWarning(warningMsg)
	}
}

// addWeeklyCronWarning emits a warning when a weekly cron pattern with fixed time is detected
func (c *Compiler) addWeeklyCronWarning(cronExpr string) {
	// Extract minute, hour, and weekday from the cron expression
	fields := strings.Fields(cronExpr)
	if len(fields) >= 5 {
		minute := fields[0]
		hour := fields[1]
		weekday := fields[4]
		schedulePreprocessingLog.Printf("Warning: detected weekly cron with fixed time: %s", cronExpr)

		// Map weekday number to name for better readability
		weekdayNames := map[string]string{
			"0": "Sunday",
			"1": "Monday",
			"2": "Tuesday",
			"3": "Wednesday",
			"4": "Thursday",
			"5": "Friday",
			"6": "Saturday",
		}
		weekdayName := weekdayNames[weekday]
		if weekdayName == "" {
			weekdayName = "day " + weekday
		}

		// Construct the warning message
		warningMsg := fmt.Sprintf(
			"Schedule uses fixed weekly time (%s %s:%s UTC). Consider using fuzzy schedule 'weekly on %s' instead to distribute workflow execution times and reduce load spikes.",
			weekdayName, hour, minute, strings.ToLower(weekdayName),
		)

		// This warning is added to the warning count
		c.IncrementWarningCount()

		// Store the warning for later display
		c.addScheduleWarning(warningMsg)
	}
}

// addScheduleWarning adds a warning to the compiler's schedule warnings list
func (c *Compiler) addScheduleWarning(warning string) {
	if c.scheduleWarnings == nil {
		c.scheduleWarnings = []string{}
	}
	c.scheduleWarnings = append(c.scheduleWarnings, warning)
}
