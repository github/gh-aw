package cli

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var yamlUtilsLog = logger.New("cli:codemod_yaml_utils")

// reconstructContent rebuilds the full markdown content from frontmatter lines and body
func reconstructContent(frontmatterLines []string, markdown string) string {
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

// parseFrontmatterLines extracts frontmatter lines from content
func parseFrontmatterLines(content string) ([]string, string, error) {
	result, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse frontmatter: %w", err)
	}
	return result.FrontmatterLines, result.Markdown, nil
}

// getIndentation extracts the leading whitespace from a line
func getIndentation(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// isTopLevelKey checks if a line is a top-level YAML key (no indentation, contains colon, not a comment)
func isTopLevelKey(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 || strings.HasPrefix(trimmed, "#") {
		return false
	}
	indent := getIndentation(line)
	return len(indent) == 0 && strings.Contains(line, ":")
}

// isNestedUnder checks if currentLine is nested under (has more indentation than) parentIndent
func isNestedUnder(currentLine, parentIndent string) bool {
	currentIndent := getIndentation(currentLine)
	return len(currentIndent) > len(parentIndent)
}

// hasExitedBlock checks if we've left a YAML block (found a line with same or less indentation that's a key)
func hasExitedBlock(line, blockIndent string) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return false
	}

	currentIndent := getIndentation(line)

	// If it's a comment, check indentation to see if we've exited
	if strings.HasPrefix(trimmed, "#") {
		return len(currentIndent) <= len(blockIndent)
	}

	// For regular lines, we've exited if indentation is same or less and it contains a colon
	return len(currentIndent) <= len(blockIndent) && strings.Contains(line, ":")
}

// findAndReplaceInLine replaces oldKey with newKey in a YAML line, preserving value and comments
func findAndReplaceInLine(line, oldKey, newKey string) (string, bool) {
	trimmedLine := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmedLine, oldKey+":") {
		return line, false
	}

	// Preserve indentation
	leadingSpace := getIndentation(line)

	// Extract the value and any trailing comment
	parts := strings.SplitN(line, ":", 2)
	if len(parts) < 2 {
		return line, false
	}

	valueAndComment := parts[1]
	return fmt.Sprintf("%s%s:%s", leadingSpace, newKey, valueAndComment), true
}

// removeFieldFromBlock removes a field and its nested content from a YAML block
// Returns the modified lines and whether any changes were made
func removeFieldFromBlock(lines []string, fieldName string, parentBlock string) ([]string, bool) {
	var result []string
	var modified bool
	var inParentBlock bool
	var parentIndent string
	var inFieldBlock bool
	var fieldIndent string

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Track if we're in the parent block
		if strings.HasPrefix(trimmedLine, parentBlock+":") {
			inParentBlock = true
			parentIndent = getIndentation(line)
			result = append(result, line)
			continue
		}

		// Check if we've left the parent block
		if inParentBlock && len(trimmedLine) > 0 && !strings.HasPrefix(trimmedLine, "#") {
			if hasExitedBlock(line, parentIndent) {
				inParentBlock = false
			}
		}

		// Remove field line if in parent block
		if inParentBlock && strings.HasPrefix(trimmedLine, fieldName+":") {
			modified = true
			inFieldBlock = true
			fieldIndent = getIndentation(line)
			yamlUtilsLog.Printf("Removed %s.%s on line %d", parentBlock, fieldName, i+1)
			continue
		}

		// Skip nested properties under the field (lines with greater indentation)
		if inFieldBlock {
			// Empty lines within the field block should be removed
			if len(trimmedLine) == 0 {
				continue
			}

			currentIndent := getIndentation(line)

			// Comments need to check indentation
			if strings.HasPrefix(trimmedLine, "#") {
				if len(currentIndent) > len(fieldIndent) {
					// Comment is nested under field, remove it
					yamlUtilsLog.Printf("Removed nested %s comment on line %d: %s", fieldName, i+1, trimmedLine)
					continue
				}
				// Comment is at same or less indentation, exit field block and keep it
				inFieldBlock = false
				result = append(result, line)
				continue
			}

			// If this line has more indentation than field, it's a nested property
			if len(currentIndent) > len(fieldIndent) {
				yamlUtilsLog.Printf("Removed nested %s property on line %d: %s", fieldName, i+1, trimmedLine)
				continue
			}
			// We've exited the field block (found a line at same or less indentation)
			inFieldBlock = false
		}

		result = append(result, line)
	}

	return result, modified
}
