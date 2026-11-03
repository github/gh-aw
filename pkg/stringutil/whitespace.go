package stringutil

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var whitespaceLog = logger.New("stringutil:whitespace")

// NormalizeWhitespace normalizes trailing whitespace and newlines to reduce spurious conflicts.
// It trims trailing whitespace from each line and ensures exactly one trailing newline.
func NormalizeWhitespace(content string) string {
	// Split into lines and trim trailing whitespace from each line
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}

	// Join back and ensure exactly one trailing newline if content is not empty
	normalized := strings.Join(lines, "\n")
	normalized = strings.TrimRight(normalized, "\n")
	if normalized != "" {
		normalized += "\n"
	}

	return normalized
}

// NormalizeLeadingWhitespace removes consistent leading whitespace from all lines
// of a multi-line string. It finds the minimum indentation across all non-empty
// lines and strips that many leading whitespace characters (spaces or tabs) from
// every line.
//
// This is useful for cleaning up content generated with extra indentation,
// such as heredoc bodies.
func NormalizeLeadingWhitespace(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return content
	}

	// Find minimum leading whitespace (excluding empty lines)
	minLeading := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue // Skip empty lines
		}
		leading := len(line) - len(strings.TrimLeft(line, " \t"))
		if minLeading == -1 || leading < minLeading {
			minLeading = leading
		}
	}

	// If no content or no leading whitespace, return as-is
	if minLeading <= 0 {
		return content
	}

	whitespaceLog.Printf("NormalizeLeadingWhitespace: stripping %d leading whitespace chars from %d lines", minLeading, len(lines))

	// Remove the minimum leading whitespace from all lines
	var result strings.Builder
	for i, line := range lines {
		if i > 0 {
			result.WriteString("\n")
		}
		if strings.TrimSpace(line) == "" {
			// Keep empty lines as empty
			result.WriteString("")
		} else if len(line) >= minLeading {
			// Remove leading whitespace
			result.WriteString(line[minLeading:])
		} else {
			result.WriteString(line)
		}
	}

	return result.String()
}
