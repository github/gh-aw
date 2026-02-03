package cli

import (
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
		// Look for existing field in the raw lines
		fieldUpdated := false
		frontmatterLines := make([]string, len(result.FrontmatterLines))
		copy(frontmatterLines, result.FrontmatterLines)

		// Try to find and update the field in place
		for i, line := range frontmatterLines {
			trimmedLine := strings.TrimSpace(line)
			// Check if this line contains our field
			if strings.HasPrefix(trimmedLine, fieldName+":") {
				// Preserve the original indentation and comments
				leadingSpace := line[:len(line)-len(strings.TrimLeft(line, " \t"))]

				// Check if there's a comment on the same line
				commentIndex := strings.Index(line, "#")
				var comment string
				if commentIndex > strings.Index(line, ":") && commentIndex != -1 {
					comment = line[commentIndex:]
				}

				// Update the field value while preserving formatting
				if comment != "" {
					frontmatterLines[i] = fmt.Sprintf("%s%s: %s %s", leadingSpace, fieldName, fieldValue, comment)
				} else {
					frontmatterLines[i] = fmt.Sprintf("%s%s: %s", leadingSpace, fieldName, fieldValue)
				}
				fieldUpdated = true
				frontmatterEditorLog.Printf("Updated existing field %s in place (line %d)", fieldName, i+1)
				break
			}
		}

		// If field wasn't found in the raw lines, add it at the end
		if !fieldUpdated {
			newField := fmt.Sprintf("%s: %s", fieldName, fieldValue)
			frontmatterLines = append(frontmatterLines, newField)
			frontmatterEditorLog.Printf("Added new field %s at end of frontmatter", fieldName)
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

	// Fallback to marshal-based approach if no raw lines are available
	return updateFieldInFrontmatterFallback(result, fieldName, fieldValue)
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
func addFieldToFrontmatter(content, fieldName, fieldValue string) (string, error) {
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
		frontmatterLines := make([]string, len(result.FrontmatterLines))
		copy(frontmatterLines, result.FrontmatterLines)

		// Add field at the end of the frontmatter, preserving original formatting
		newField := fmt.Sprintf("%s: %s", fieldName, fieldValue)
		frontmatterLines = append(frontmatterLines, newField)

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

		frontmatterLines := make([]string, 0, len(result.FrontmatterLines))
		inOnBlock := false
		onIndentLevel := 0
		skipNextLine := false
		fieldIndentLevel := 0

		for i := 0; i < len(result.FrontmatterLines); i++ {
			line := result.FrontmatterLines[i]
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

		frontmatterEditorLog.Printf("Successfully removed field %s from 'on' trigger", fieldName)
		return strings.Join(lines, "\n"), nil
	}

	// This should rarely happen since we already checked for frontmatter existence
	frontmatterEditorLog.Printf("No raw frontmatter lines available, returning content unchanged")
	return content, nil
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
		return "", fmt.Errorf("no frontmatter found, cannot set field in 'on' trigger")
	}

	// Check if 'on' field exists
	onValue, exists := result.Frontmatter["on"]
	if !exists {
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
		return "", fmt.Errorf("no frontmatter found, cannot set field in 'on' trigger")
	}

	// Check if 'on' is an object (map)
	_, isMap := onValue.(map[string]any)
	if !isMap {
		// 'on' is not a map (might be a string), cannot set field
		return "", fmt.Errorf("'on' field is not an object, cannot set nested field")
	}

	// Work with raw frontmatter lines to preserve formatting
	if len(result.FrontmatterLines) > 0 {
		frontmatterEditorLog.Printf("Using raw frontmatter lines to set field (%d lines)", len(result.FrontmatterLines))

		frontmatterLines := make([]string, 0, len(result.FrontmatterLines))
		inOnBlock := false
		onIndentLevel := 0
		fieldUpdated := false

		for i := 0; i < len(result.FrontmatterLines); i++ {
			line := result.FrontmatterLines[i]
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
					// Preserve the original indentation and comments
					leadingSpace := line[:len(line)-len(strings.TrimLeft(line, " \t"))]

					// Check if there's a comment on the same line, after the field separator
					fieldSep := fieldName + ":"
					fieldSepIndex := strings.Index(line, fieldSep)
					commentIndex := strings.Index(line, "#")
					var comment string
					if fieldSepIndex != -1 && commentIndex > fieldSepIndex {
						comment = line[commentIndex:]
					}

					// Update the field value while preserving formatting
					if comment != "" {
						frontmatterLines = append(frontmatterLines, fmt.Sprintf("%s%s: %s %s", leadingSpace, fieldName, fieldValue, comment))
					} else {
						frontmatterLines = append(frontmatterLines, fmt.Sprintf("%s%s: %s", leadingSpace, fieldName, fieldValue))
					}
					fieldUpdated = true
					frontmatterEditorLog.Printf("Updated existing field %s in 'on' block", fieldName)
					continue
				}
			}

			// Keep this line
			frontmatterLines = append(frontmatterLines, line)
		}

		// If we were still in the 'on' block at the end of the frontmatter and didn't update the field
		if inOnBlock && !fieldUpdated {
			// Add the field at the end of the 'on' block
			indent := strings.Repeat(" ", onIndentLevel+4)
			newField := fmt.Sprintf("%s%s: %s", indent, fieldName, fieldValue)
			frontmatterLines = append(frontmatterLines, newField)
			fieldUpdated = true
			frontmatterEditorLog.Printf("Added new field %s at end of 'on' block", fieldName)
		}

		if !fieldUpdated {
			return "", fmt.Errorf("failed to set field %s in 'on' trigger", fieldName)
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

		frontmatterEditorLog.Printf("Successfully set field %s in 'on' trigger", fieldName)
		return strings.Join(lines, "\n"), nil
	}

	// This should rarely happen since we already checked for frontmatter existence
	frontmatterEditorLog.Printf("No raw frontmatter lines available")
	return "", fmt.Errorf("no frontmatter lines available to modify")
}
