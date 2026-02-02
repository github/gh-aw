package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var xmlCommentsLog = logger.New("workflow:xml_comments")

// removeXMLComments removes XML comments (<!-- -->) from markdown content
// while preserving comments that appear within code blocks
func removeXMLComments(content string) string {
	xmlCommentsLog.Printf("Removing XML comments from content: %d lines", len(strings.Split(content, "\n")))

	// Track if we're inside a code block to avoid removing comments in code
	lines := strings.Split(content, "\n")
	var result []string
	inCodeBlock := false
	var openMarker string
	inXMLComment := false
	removedComments := 0

	for _, line := range lines {
		// If we're in a code block, preserve the line as-is (ignore XML comment processing)
		// Code blocks that started BEFORE any XML comment take precedence
		if inCodeBlock {
			trimmedLine := strings.TrimSpace(line)
			// Check if this line closes the code block
			if isMatchingCodeBlockMarker(trimmedLine, openMarker) {
				inCodeBlock = false
				openMarker = ""
			}
			result = append(result, line)
			continue
		}

		// Process the line for XML comments (not in a code block)
		processedLine, wasInComment, isInComment := removeXMLCommentsFromLine(line, inXMLComment)
		inXMLComment = isInComment

		// If we're in an XML comment, skip this line entirely (including code block markers)
		if wasInComment && isInComment {
			// In the middle of a comment, skip the line completely
			removedComments++
			continue
		}

		// Check for code block markers (3 or more ` or ~) - but only if not in XML comment
		trimmedLine := strings.TrimSpace(processedLine)

		if !inCodeBlock && isValidCodeBlockMarker(trimmedLine) {
			// Opening a code block
			openMarker, _ = extractCodeBlockMarker(trimmedLine)
			inCodeBlock = true
			xmlCommentsLog.Printf("Detected code block opening with marker: %s", openMarker)
			result = append(result, processedLine)
			continue
		}

		// Handle XML comment boundaries
		if !wasInComment && !isInComment {
			// Line had no comment involvement, keep as-is
			result = append(result, processedLine)
		} else if !wasInComment && isInComment {
			// Line started a multiline comment, keep the processed part and add empty line
			if strings.TrimSpace(processedLine) != "" {
				result = append(result, processedLine)
			}
			result = append(result, "")
		} else if wasInComment && !isInComment {
			// Line ended a multiline comment, keep the processed part
			if strings.TrimSpace(processedLine) != "" {
				result = append(result, processedLine)
			}
		}
	}

	xmlCommentsLog.Printf("XML comment removal completed: removed %d comment lines, output %d lines", removedComments, len(result))
	return strings.Join(result, "\n")
}

// removeXMLCommentsFromLine removes XML comments from a single line
// Returns: processed line, was initially in comment, is now in comment
func removeXMLCommentsFromLine(line string, inXMLComment bool) (string, bool, bool) {
	result := line
	wasInComment := inXMLComment

	for {
		if inXMLComment {
			// We're in a multiline comment, look for closing tag
			if closeIndex := strings.Index(result, "-->"); closeIndex != -1 {
				// Found closing tag, remove everything up to and including it
				result = result[closeIndex+3:]
				inXMLComment = false
				// Continue processing in case there are more comments on this line
			} else {
				// No closing tag found, entire line is part of the comment
				return "", wasInComment, inXMLComment
			}
		} else {
			// Not in a comment, look for opening tag
			if openIndex := strings.Index(result, "<!--"); openIndex != -1 {
				// Found opening tag
				if closeIndex := strings.Index(result[openIndex:], "-->"); closeIndex != -1 {
					// Complete comment on same line
					actualCloseIndex := openIndex + closeIndex + 3
					result = result[:openIndex] + result[actualCloseIndex:]
					// Continue processing in case there are more comments on this line
				} else {
					// Start of multiline comment
					result = result[:openIndex]
					inXMLComment = true
					break
				}
			} else {
				// No opening tag found, done processing this line
				break
			}
		}
	}

	return result, wasInComment, inXMLComment
}

// extractCodeBlockMarker extracts the marker string and language from a code block line
// Returns marker string (e.g., "```", "~~~~") and language specifier
func extractCodeBlockMarker(trimmedLine string) (string, string) {
	if len(trimmedLine) < 3 {
		return "", ""
	}

	var count int

	// Check for backticks
	if strings.HasPrefix(trimmedLine, "```") {
		for i, r := range trimmedLine {
			if r == '`' {
				count++
			} else {
				// Found language specifier or other content
				return strings.Repeat("`", count), strings.TrimSpace(trimmedLine[i:])
			}
		}
		// All characters are backticks
		return strings.Repeat("`", count), ""
	}

	// Check for tildes
	if strings.HasPrefix(trimmedLine, "~~~") {
		for i, r := range trimmedLine {
			if r == '~' {
				count++
			} else {
				// Found language specifier or other content
				return strings.Repeat("~", count), strings.TrimSpace(trimmedLine[i:])
			}
		}
		// All characters are tildes
		return strings.Repeat("~", count), ""
	}

	return "", ""
}

// isValidCodeBlockMarker checks if a trimmed line is a valid code block marker (3 or more ` or ~)
func isValidCodeBlockMarker(trimmedLine string) bool {
	marker, _ := extractCodeBlockMarker(trimmedLine)
	return len(marker) >= 3
}

// isMatchingCodeBlockMarker checks if the trimmed line matches the opening marker
func isMatchingCodeBlockMarker(trimmedLine string, openMarker string) bool {
	marker, _ := extractCodeBlockMarker(trimmedLine)
	if len(marker) == 0 || len(openMarker) == 0 {
		return false
	}

	// Markers must be the same type (both backticks or both tildes)
	if marker[0] != openMarker[0] {
		return false
	}

	// Closing marker must have at least as many characters as opening marker
	return len(marker) >= len(openMarker)
}
