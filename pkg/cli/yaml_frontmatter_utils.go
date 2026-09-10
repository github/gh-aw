package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var yamlUtilsLog = logger.New("cli:yaml_frontmatter_utils")

// isFrontmatterStrictFalse returns true when the frontmatter explicitly sets strict: false.
// These codemods only need to run in strict mode; if the workflow has opted out of strict
// mode, the deprecated keys are still valid and should not be touched.
func isFrontmatterStrictFalse(frontmatter map[string]any) bool {
	strictVal, ok := frontmatter["strict"]
	if !ok {
		return false
	}
	strictBool, ok := strictVal.(bool)
	return ok && !strictBool
}

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
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	indent := getIndentation(line)
	return indent == "" && strings.Contains(line, ":")
}

// isNestedUnder checks if currentLine is nested under (has more indentation than) parentIndent
func isNestedUnder(currentLine, parentIndent string) bool {
	currentIndent := getIndentation(currentLine)
	return len(currentIndent) > len(parentIndent)
}

// hasExitedBlock checks if we've left a YAML block (found a line with same or less indentation that's a key)
func hasExitedBlock(line, blockIndent string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
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

	yamlUtilsLog.Printf("Replacing frontmatter key %q with %q", oldKey, newKey)
	valueAndComment := parts[1]
	return fmt.Sprintf("%s%s:%s", leadingSpace, newKey, valueAndComment), true
}

// applyFrontmatterLineTransform parses frontmatter from content, applies a transform
// function to the frontmatter lines, and reconstructs the content if any changes were made.
// The transform function receives the frontmatter lines and returns the modified lines
// and a boolean indicating whether any changes were made.
func applyFrontmatterLineTransform(content string, transform func([]string) ([]string, bool)) (string, bool, error) {
	frontmatterLines, _, err := parseFrontmatterLines(content)
	if err != nil {
		return content, false, err
	}

	result, modified := transform(frontmatterLines)
	if !modified {
		return content, false, nil
	}

	yamlUtilsLog.Print("Frontmatter transformation applied successfully")
	originalFrontmatter := strings.Join(frontmatterLines, "\n")
	updatedFrontmatter := strings.Join(result, "\n")
	firstNewline := strings.IndexByte(content, '\n')
	if firstNewline < 0 {
		return content, false, errors.New("unable to locate frontmatter text in workflow content")
	}
	frontmatterStart := firstNewline + 1
	if !strings.HasPrefix(content[frontmatterStart:], originalFrontmatter) {
		return content, false, errors.New("unable to locate frontmatter text in workflow content")
	}
	return content[:frontmatterStart] + updatedFrontmatter + content[frontmatterStart+len(originalFrontmatter):], true, nil
}

// removeParentBlockIfTrulyEmpty removes a bare "parentBlock:" header line only
// when there are no nested lines at all underneath it — not even comments.
// This is intentionally more conservative than removeBlockIfEmpty: if a
// user-authored comment is the only thing left under the block, the header is
// kept so the comment is not silently deleted.
func removeParentBlockIfTrulyEmpty(lines []string, parentBlock string) []string {
	blockKeyLine := parentBlock + ":"
	var result []string

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Only match a bare block header with no inline value (e.g. "features:")
		if trimmed != blockKeyLine {
			result = append(result, line)
			continue
		}

		blockIndent := getIndentation(line)
		hasAnyNested := false
		for j := i + 1; j < len(lines); j++ {
			nextTrimmed := strings.TrimSpace(lines[j])
			if nextTrimmed == "" {
				continue // skip blank lines
			}
			if len(getIndentation(lines[j])) > len(blockIndent) {
				hasAnyNested = true
			}
			break
		}

		if !hasAnyNested {
			yamlUtilsLog.Printf("Removed empty parent block '%s'", parentBlock)
			continue // drop the header
		}
		result = append(result, line)
	}

	return result
}

// removeFieldFromBlock removes a field and its nested content from a YAML block.
// If removing the field leaves the parent block truly empty (no children, not
// even comments), the parent block line is also removed to avoid a dangling
// "parentBlock:" key (which YAML parses as null).
// Returns the modified lines and whether any changes were made.
//
//nolint:largefunc
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
		if inParentBlock && trimmedLine != "" && !strings.HasPrefix(trimmedLine, "#") {
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
			if trimmedLine == "" {
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

	if modified {
		result = removeParentBlockIfTrulyEmpty(result, parentBlock)
	}

	return result, modified
}

// isDescendant returns true if childIndent is deeper (more indented) than parentIndent.
// It is used as a "belongs to this block" check — any line more indented than the parent
// is treated as being within the parent's scope.
func isDescendant(childIndent, parentIndent string) bool {
	return len(childIndent) > len(parentIndent)
}

// isExpressionValue reports whether value is a GitHub Actions expression (i.e. contains ${{ ... }}).
func isExpressionValue(value string) bool {
	return strings.Contains(value, "${{") && strings.Contains(value, "}}")
}
