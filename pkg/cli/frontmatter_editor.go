package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
)

var frontmatterEditorLog = logger.New("cli:frontmatter_editor")

// UpdateFieldInFrontmatter updates a field in the frontmatter while preserving the original formatting
// when possible. It tries to preserve whitespace, comments, and formatting by working with the raw
// frontmatter lines, similar to how addSourceToWorkflow works.
func UpdateFieldInFrontmatter(content, fieldName, fieldValue string) (string, error) {
	frontmatterEditorLog.Printf("Updating frontmatter field: %s = %s", fieldName, fieldValue)

	// Parse frontmatter using parser package
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		frontmatterEditorLog.Printf("Failed to parse frontmatter: %v", err)
		return "", fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	// Try to preserve original frontmatter formatting by manually updating the field
	if len(result.FrontmatterLines) > 0 {
		frontmatterEditorLog.Printf("Using raw frontmatter lines for field update (%d lines)", len(result.FrontmatterLines))
		newFrontmatterLines := updateFrontmatterFieldLines(result.FrontmatterLines, fieldName, fieldValue)
		return rebuildEditedFrontmatter(newFrontmatterLines, result.Markdown), nil
	}

	// Fallback to marshal-based approach if no raw lines are available
	return updateFieldInFrontmatterFallback(result, fieldName, fieldValue)
}

func updateFrontmatterFieldLines(frontmatterLines []string, fieldName, fieldValue string) []string {
	// Look for existing field in the raw lines
	fieldUpdated := false
	skipChildren := false
	fieldIndentLevel := 0
	newFrontmatterLines := make([]string, 0, len(frontmatterLines))

	for _, line := range frontmatterLines {
		// If we just updated the field, skip its child lines (block mapping values)
		if skipChildren {
			currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))
			if currentIndent > fieldIndentLevel {
				// This line is a child of the replaced field — drop it
				continue
			}
			// No longer in the child block
			skipChildren = false
		}

		// Check if this top-level line contains our field.
		if !fieldUpdated && isTopLevelFieldLine(line, fieldName) {
			// Preserve the original indentation and comments
			leadingSpace := line[:len(line)-len(strings.TrimLeft(line, " \t"))]

			updatedLine := formatTopLevelFieldReplacement(line, leadingSpace, fieldName, fieldValue)
			newFrontmatterLines = append(newFrontmatterLines, updatedLine)
			fieldUpdated = true
			// Track the indent level so we can skip any child lines that follow
			fieldIndentLevel = len(leadingSpace)
			skipChildren = true
			frontmatterEditorLog.Printf("Updated existing field %s", fieldName)
			continue
		}

		newFrontmatterLines = append(newFrontmatterLines, line)
	}

	// If field wasn't found in the raw lines, add it at the end
	if !fieldUpdated {
		newField := fmt.Sprintf("%s: %s", fieldName, fieldValue)
		newFrontmatterLines = append(newFrontmatterLines, newField)
		frontmatterEditorLog.Printf("Added new field %s at end of frontmatter", fieldName)
	}

	return newFrontmatterLines
}

func formatTopLevelFieldReplacement(line, leadingSpace, fieldName, fieldValue string) string {
	commentIndex := strings.Index(line, "#")
	if commentIndex > strings.Index(line, ":") && commentIndex != -1 {
		return fmt.Sprintf("%s%s: %s %s", leadingSpace, fieldName, fieldValue, line[commentIndex:])
	}
	return fmt.Sprintf("%s%s: %s", leadingSpace, fieldName, fieldValue)
}

func rebuildEditedFrontmatter(frontmatterLines []string, markdown string) string {
	var lines []string
	lines = append(lines, "---")
	lines = append(lines, frontmatterLines...)
	lines = append(lines, "---")
	if markdown != "" {
		lines = append(lines, "")
		lines = append(lines, markdown)
	}
	return strings.Join(lines, "\n")
}

// updateFieldInFrontmatterFallback implements the original behavior as a fallback
func updateFieldInFrontmatterFallback(result *parser.FrontmatterResult, fieldName, fieldValue string) (string, error) {
	// Initialize frontmatter if it doesn't exist
	if result.Frontmatter == nil {
		result.Frontmatter = make(map[string]any)
	}

	// Update the field
	result.Frontmatter[fieldName] = fieldValue

	// Convert back to YAML with proper field ordering
	updatedFrontmatter, err := workflow.MarshalWithFieldOrder(result.Frontmatter, constants.PriorityWorkflowFields)
	if err != nil {
		return "", fmt.Errorf("failed to marshal updated frontmatter: %w", err)
	}

	// Clean up quoted keys - replace "on": with on: at the start of a line
	frontmatterStr := strings.TrimSuffix(string(updatedFrontmatter), "\n")
	frontmatterStr = workflow.UnquoteYAMLKey(frontmatterStr, "on")

	// Reconstruct the file
	var lines []string
	lines = append(lines, "---")
	if frontmatterStr != "" {
		lines = append(lines, strings.Split(frontmatterStr, "\n")...)
	}
	lines = append(lines, "---")
	if result.Markdown != "" {
		lines = append(lines, result.Markdown)
	}

	return strings.Join(lines, "\n"), nil
}

// addFieldToFrontmatter adds a new field to the frontmatter while preserving formatting.
// This is used when we know the field doesn't exist yet.
// When trailingBlankLine is true, a blank line is appended after the new field to provide
// visual separation from fields added subsequently (e.g., separating engine from source).
func addFieldToFrontmatter(content, fieldName, fieldValue string, trailingBlankLine bool) (string, error) {
	// Parse frontmatter using parser package
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return "", fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	// Try to preserve original frontmatter formatting by manually inserting the field
	if len(result.FrontmatterLines) > 0 {
		// Check if field already exists
		if result.Frontmatter != nil {
			if _, exists := result.Frontmatter[fieldName]; exists {
				// Field exists, update it instead
				return UpdateFieldInFrontmatter(content, fieldName, fieldValue)
			}
		}

		// Field doesn't exist, add it manually to preserve formatting
		frontmatterLines := append([]string(nil), result.FrontmatterLines...)

		// Add field at the end of the frontmatter, preserving original formatting
		newField := fmt.Sprintf("%s: %s", fieldName, fieldValue)
		frontmatterLines = append(frontmatterLines, newField)
		if trailingBlankLine {
			frontmatterLines = append(frontmatterLines, "")
		}

		// Reconstruct the file with preserved formatting
		var lines []string
		lines = append(lines, "---")
		lines = append(lines, frontmatterLines...)
		lines = append(lines, "---")
		if result.Markdown != "" {
			// Add empty line before markdown content to match original format
			lines = append(lines, "")
			lines = append(lines, result.Markdown)
		}

		return strings.Join(lines, "\n"), nil
	}

	// Fallback to original behavior if no frontmatter lines are available
	return updateFieldInFrontmatterFallback(result, fieldName, fieldValue)
}

// RemoveTopLevelFieldFromFrontmatter removes a top-level field from the frontmatter.
// If the field is not found, the content is returned unchanged.
// It preserves the original formatting of all other frontmatter content.
func RemoveTopLevelFieldFromFrontmatter(content, fieldName string) (string, error) {
	frontmatterEditorLog.Printf("Removing top-level frontmatter field: %s", fieldName)

	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		frontmatterEditorLog.Printf("Failed to parse frontmatter: %v", err)
		return "", fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	if len(result.FrontmatterLines) == 0 {
		return content, nil
	}

	skipChildren := false
	fieldIndentLevel := 0
	newFrontmatterLines := make([]string, 0, len(result.FrontmatterLines))

	for _, line := range result.FrontmatterLines {
		if skipChildren {
			currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))
			if currentIndent > fieldIndentLevel {
				continue
			}
			skipChildren = false
		}

		if isTopLevelFieldLine(line, fieldName) {
			leadingSpace := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			fieldIndentLevel = len(leadingSpace)
			skipChildren = true
			frontmatterEditorLog.Printf("Removed top-level field %s", fieldName)
			continue
		}

		newFrontmatterLines = append(newFrontmatterLines, line)
	}

	var lines []string
	lines = append(lines, "---")
	lines = append(lines, newFrontmatterLines...)
	lines = append(lines, "---")
	if result.Markdown != "" {
		lines = append(lines, "")
		lines = append(lines, result.Markdown)
	}

	return strings.Join(lines, "\n"), nil
}

func isTopLevelFieldLine(line, fieldName string) bool {
	return len(line) == len(strings.TrimLeft(line, " \t")) && strings.HasPrefix(strings.TrimSpace(line), fieldName+":")
}

func updateTopLevelFieldInFrontmatterRaw(content, fieldName, fieldValue string) (string, error) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content, nil
	}

	frontmatterEnd := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			frontmatterEnd = i
			break
		}
	}
	if frontmatterEnd == -1 {
		return "", errors.New("frontmatter closing delimiter not found")
	}

	fieldUpdated := false
	skipChildren := false
	fieldIndentLevel := 0
	updatedLines := make([]string, 0, len(lines)+1)
	updatedLines = append(updatedLines, lines[0])

	for _, line := range lines[1:frontmatterEnd] {
		if skipChildren {
			currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))
			if currentIndent > fieldIndentLevel {
				continue
			}
			skipChildren = false
		}

		if !fieldUpdated && isTopLevelFieldLine(line, fieldName) {
			updatedLines = append(updatedLines, fmt.Sprintf("%s: %s", fieldName, fieldValue))
			fieldUpdated = true
			fieldIndentLevel = 0
			skipChildren = true
			continue
		}

		updatedLines = append(updatedLines, line)
	}

	if !fieldUpdated {
		updatedLines = append(updatedLines, fmt.Sprintf("%s: %s", fieldName, fieldValue))
	}
	updatedLines = append(updatedLines, lines[frontmatterEnd:]...)

	return strings.Join(updatedLines, "\n"), nil
}

// RemoveFieldFromOnTrigger removes a field from the 'on' trigger object in the frontmatter.
// This handles nested fields like "stop-after" which are located under the "on" key.
// It preserves the original formatting of the frontmatter including comments and blank lines.
func RemoveFieldFromOnTrigger(content, fieldName string) (string, error) {
	frontmatterEditorLog.Printf("Removing field from 'on' trigger: %s", fieldName)

	// Parse frontmatter using parser package
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		frontmatterEditorLog.Printf("Failed to parse frontmatter: %v", err)
		return "", fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	// Check if frontmatter exists
	if result.Frontmatter == nil {
		// No frontmatter, return content unchanged
		return content, nil
	}

	// Check if 'on' field exists
	onValue, exists := result.Frontmatter["on"]
	if !exists {
		// No 'on' field, return content unchanged
		return content, nil
	}

	// Check if 'on' is an object (map)
	onMap, isMap := onValue.(map[string]any)
	if !isMap {
		// 'on' is not a map (might be a string), return content unchanged
		return content, nil
	}

	// Check if the field to remove exists in the 'on' map
	if _, fieldExists := onMap[fieldName]; !fieldExists {
		// Field doesn't exist, return content unchanged
		return content, nil
	}

	// Work with raw frontmatter lines to preserve formatting
	if len(result.FrontmatterLines) > 0 {
		frontmatterEditorLog.Printf("Using raw frontmatter lines to remove field (%d lines)", len(result.FrontmatterLines))
		frontmatterLines := removeOnTriggerFieldLines(result.FrontmatterLines, fieldName)
		updated := rebuildEditedFrontmatter(frontmatterLines, result.Markdown)
		frontmatterEditorLog.Printf("Successfully removed field %s from 'on' trigger", fieldName)
		return updated, nil
	}

	// This should rarely happen since we already checked for frontmatter existence
	frontmatterEditorLog.Printf("No raw frontmatter lines available, returning content unchanged")
	return content, nil
}

func removeOnTriggerFieldLines(rawLines []string, fieldName string) []string {
	frontmatterLines := make([]string, 0, len(rawLines))
	inOnBlock := false
	onIndentLevel := 0
	skipNextLine := false
	fieldIndentLevel := 0

	for i := range len(rawLines) {
		line := rawLines[i]
		trimmedLine := strings.TrimSpace(line)

		// Skip if this is a continuation line that should be skipped
		if skipNextLine {
			// Check if this line is indented more than the field we're removing
			// If so, skip it (it's a continuation of the removed field)
			currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))
			if currentIndent > fieldIndentLevel {
				continue
			}
			skipNextLine = false
		}

		// Detect the start of the 'on:' block (must be just "on:" without inline value)
		if !inOnBlock && (trimmedLine == "on:" || trimmedLine == `"on":` ||
			strings.HasPrefix(trimmedLine, "on: #") || strings.HasPrefix(trimmedLine, `"on": #`)) {
			inOnBlock = true
			onIndentLevel = len(line) - len(strings.TrimLeft(line, " \t"))
			frontmatterLines = append(frontmatterLines, line)
			continue
		}

		// If we're in the 'on' block, check if this is the field to remove
		if inOnBlock {
			currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))

			// Check if we've exited the 'on' block
			if trimmedLine != "" && !strings.HasPrefix(trimmedLine, "#") && currentIndent <= onIndentLevel {
				inOnBlock = false
				frontmatterLines = append(frontmatterLines, line)
				continue
			}

			// Check if this is the field to remove (exact match)
			if trimmedLine == fieldName+":" ||
				strings.HasPrefix(trimmedLine, fieldName+": ") ||
				strings.HasPrefix(trimmedLine, fieldName+":\t") {
				frontmatterEditorLog.Printf("Found field %s to remove at line %d", fieldName, i+1)
				// Track the indentation of the field being removed
				fieldIndentLevel = currentIndent
				// Skip this line
				// Also mark that we should skip continuation lines if the value is multiline
				skipNextLine = true
				continue
			}
		}

		// Keep this line
		frontmatterLines = append(frontmatterLines, line)
	}

	return frontmatterLines
}

// SetFieldInOnTrigger sets a field value in the 'on' trigger object in the frontmatter.
// This handles nested fields like "stop-after" which are located under the "on" key.
// It preserves the original formatting of the frontmatter including comments and blank lines.
func SetFieldInOnTrigger(content, fieldName, fieldValue string) (string, error) {
	frontmatterEditorLog.Printf("Setting field in 'on' trigger: %s = %s", fieldName, fieldValue)

	// Parse frontmatter using parser package
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		frontmatterEditorLog.Printf("Failed to parse frontmatter: %v", err)
		return "", fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	// Check if frontmatter exists
	if result.Frontmatter == nil {
		// No frontmatter, cannot set nested field without 'on' block
		return "", errors.New("no frontmatter found, cannot set field in 'on' trigger")
	}

	// Check if 'on' field exists
	onValue, exists := result.Frontmatter["on"]
	if !exists {
		return createOnTriggerField(result, fieldName, fieldValue)
	}

	// Check if 'on' is an object (map)
	_, isMap := onValue.(map[string]any)
	if !isMap {
		return "", errors.New("'on' field is not an object, cannot set nested field")
	}
	if len(result.FrontmatterLines) > 0 {
		return setRawOnTriggerField(result, fieldName, fieldValue)
	}
	frontmatterEditorLog.Printf("No raw frontmatter lines available")
	return "", errors.New("no frontmatter lines available to modify")
}

func createOnTriggerField(result *parser.FrontmatterResult, fieldName, fieldValue string) (string, error) {
	// No 'on' field exists, need to create it
	// Add the 'on:' block with the field at the beginning of frontmatter
	if len(result.FrontmatterLines) > 0 {
		frontmatterEditorLog.Printf("Creating 'on' block with field %s", fieldName)

		// Create new frontmatter lines with 'on:' block at the start
		frontmatterLines := make([]string, 0, len(result.FrontmatterLines)+2)
		frontmatterLines = append(frontmatterLines, "on:")
		frontmatterLines = append(frontmatterLines, fmt.Sprintf("    %s: %s", fieldName, fieldValue))
		frontmatterLines = append(frontmatterLines, result.FrontmatterLines...)

		// Reconstruct the file
		var lines []string
		lines = append(lines, "---")
		lines = append(lines, frontmatterLines...)
		lines = append(lines, "---")
		if result.Markdown != "" {
			lines = append(lines, "")
			lines = append(lines, result.Markdown)
		}

		frontmatterEditorLog.Printf("Successfully created 'on' block with field %s", fieldName)
		return strings.Join(lines, "\n"), nil
	}

	// No frontmatter lines, cannot create 'on' block
	return "", errors.New("no frontmatter found, cannot set field in 'on' trigger")
}

func setRawOnTriggerField(result *parser.FrontmatterResult, fieldName, fieldValue string) (string, error) {
	frontmatterEditorLog.Printf("Using raw frontmatter lines to set field (%d lines)", len(result.FrontmatterLines))
	frontmatterLines, fieldUpdated := setOnTriggerFieldLines(result.FrontmatterLines, fieldName, fieldValue)
	if !fieldUpdated {
		return "", fmt.Errorf("failed to set field %s in 'on' trigger", fieldName)
	}
	updated := rebuildEditedFrontmatter(frontmatterLines, result.Markdown)
	frontmatterEditorLog.Printf("Successfully set field %s in 'on' trigger", fieldName)
	return updated, nil
}

func setOnTriggerFieldLines(rawLines []string, fieldName, fieldValue string) ([]string, bool) {
	frontmatterLines := make([]string, 0, len(rawLines))
	inOnBlock := false
	onIndentLevel := 0
	fieldUpdated := false

	for i := range len(rawLines) {
		line := rawLines[i]
		trimmedLine := strings.TrimSpace(line)

		// Detect the start of the 'on:' block (must be just "on:" without inline value)
		if !inOnBlock && (trimmedLine == "on:" || trimmedLine == `"on":` ||
			strings.HasPrefix(trimmedLine, "on: #") || strings.HasPrefix(trimmedLine, `"on": #`)) {
			inOnBlock = true
			onIndentLevel = len(line) - len(strings.TrimLeft(line, " \t"))
			frontmatterLines = append(frontmatterLines, line)
			continue
		}

		// If we're in the 'on' block
		if inOnBlock {
			currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))

			// Check if we've exited the 'on' block
			if trimmedLine != "" && !strings.HasPrefix(trimmedLine, "#") && currentIndent <= onIndentLevel {
				inOnBlock = false

				// If we didn't update the field yet, add it before exiting the block
				if !fieldUpdated {
					// Calculate the appropriate indentation (one level deeper than 'on:')
					indent := strings.Repeat(" ", onIndentLevel+4)
					newField := fmt.Sprintf("%s%s: %s", indent, fieldName, fieldValue)
					frontmatterLines = append(frontmatterLines, newField)
					fieldUpdated = true
					frontmatterEditorLog.Printf("Added new field %s to 'on' block", fieldName)
				}

				frontmatterLines = append(frontmatterLines, line)
				continue
			}

			// Check if this is the field to update (exact match)
			if trimmedLine == fieldName+":" ||
				strings.HasPrefix(trimmedLine, fieldName+": ") ||
				strings.HasPrefix(trimmedLine, fieldName+":\t") {
				frontmatterLines = append(frontmatterLines, formatOnTriggerFieldReplacement(line, fieldName, fieldValue))
				fieldUpdated = true
				frontmatterEditorLog.Printf("Updated existing field %s in 'on' block", fieldName)
				continue
			}
		}

		// Keep this line
		frontmatterLines = append(frontmatterLines, line)
	}

	if inOnBlock && !fieldUpdated {
		return appendTrailingOnTriggerField(frontmatterLines, fieldName, fieldValue, onIndentLevel), true
	}

	return frontmatterLines, fieldUpdated
}

func appendTrailingOnTriggerField(frontmatterLines []string, fieldName, fieldValue string, onIndentLevel int) []string {
	indent := strings.Repeat(" ", onIndentLevel+4)
	newField := fmt.Sprintf("%s%s: %s", indent, fieldName, fieldValue)
	frontmatterLines = append(frontmatterLines, newField)
	frontmatterEditorLog.Printf("Added new field %s at end of 'on' block", fieldName)
	return frontmatterLines
}

func formatOnTriggerFieldReplacement(line, fieldName, fieldValue string) string {
	leadingSpace := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	fieldSepIndex := strings.Index(line, fieldName+":")
	commentIndex := strings.Index(line, "#")
	if fieldSepIndex != -1 && commentIndex > fieldSepIndex {
		return fmt.Sprintf("%s%s: %s %s", leadingSpace, fieldName, fieldValue, line[commentIndex:])
	}
	return fmt.Sprintf("%s%s: %s", leadingSpace, fieldName, fieldValue)
}

// UpdateScheduleInOnBlock updates the "schedule" sub-key inside the "on:" block mapping in
// the workflow frontmatter. It replaces the existing schedule value—whether a scalar
// (schedule: daily) or a list (schedule:\n  - cron: "0 9 * * *")—with a new scalar
// expression, while preserving all sibling trigger keys (e.g., workflow_dispatch, push).
func UpdateScheduleInOnBlock(content, scheduleExpr string) (string, error) {
	frontmatterEditorLog.Printf("Updating schedule in on: block to %q", scheduleExpr)

	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return "", fmt.Errorf("failed to parse frontmatter: %w", err)
	}

	if len(result.FrontmatterLines) == 0 {
		return "", errors.New("no frontmatter lines available to modify")
	}

	frontmatterLines, scheduleFound := updateOnScheduleLines(result.FrontmatterLines, scheduleExpr)
	if !scheduleFound {
		return "", errors.New("schedule key not found inside on: block")
	}
	return rebuildEditedFrontmatter(frontmatterLines, result.Markdown), nil
}

func updateOnScheduleLines(rawLines []string, scheduleExpr string) ([]string, bool) {
	frontmatterLines := make([]string, 0, len(rawLines))
	inOnBlock := false
	onIndentLevel := 0
	scheduleFound := false
	skipScheduleChildren := false
	scheduleIndentLevel := 0

	for _, line := range rawLines {
		trimmedLine := strings.TrimSpace(line)
		currentIndent := len(line) - len(strings.TrimLeft(line, " \t"))

		// Detect the start of the 'on:' block (bare "on:" key, with optional trailing comment).
		if !inOnBlock &&
			(trimmedLine == "on:" || trimmedLine == `"on":` ||
				strings.HasPrefix(trimmedLine, "on: #") || strings.HasPrefix(trimmedLine, `"on": #`)) {
			inOnBlock = true
			onIndentLevel = currentIndent
			frontmatterLines = append(frontmatterLines, line)
			continue
		}

		if inOnBlock {
			// Drop child lines of the schedule: key (e.g., "- cron: daily").
			if skipScheduleChildren {
				if currentIndent > scheduleIndentLevel {
					continue
				}
				skipScheduleChildren = false
			}

			// Exit the on: block when a non-empty, non-comment line appears at or above on: indent.
			if trimmedLine != "" && !strings.HasPrefix(trimmedLine, "#") && currentIndent <= onIndentLevel {
				inOnBlock = false
				frontmatterLines = append(frontmatterLines, line)
				continue
			}

			// Replace the schedule: line (handles both scalar and bare-key forms).
			if !scheduleFound &&
				(trimmedLine == "schedule:" ||
					strings.HasPrefix(trimmedLine, "schedule: ") ||
					strings.HasPrefix(trimmedLine, "schedule:\t")) {
				leadingSpace := line[:currentIndent]
				frontmatterLines = append(frontmatterLines, fmt.Sprintf("%sschedule: %s", leadingSpace, scheduleExpr))
				scheduleFound = true
				scheduleIndentLevel = currentIndent
				skipScheduleChildren = true
				frontmatterEditorLog.Printf("Updated schedule in on: block to %q", scheduleExpr)
				continue
			}

			frontmatterLines = append(frontmatterLines, line)
			continue
		}

		frontmatterLines = append(frontmatterLines, line)
	}

	return frontmatterLines, scheduleFound
}
