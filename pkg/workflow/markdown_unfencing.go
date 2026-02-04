package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var markdownUnfencingLog = logger.New("workflow:markdown_unfencing")

// UnfenceMarkdown removes an outer code fence from markdown content if the entire
// content is wrapped in a markdown/md code fence. This handles cases where agents
// accidentally wrap the entire markdown body in a code fence.
//
// The function detects:
// - Content starting with ```markdown, ```md, ~~~markdown, or ~~~md (case insensitive)
// - Content ending with ``` or ~~~
// - The closing fence must match the opening fence type (backticks or tildes)
//
// Returns the unfenced content if a wrapping fence is detected, otherwise returns
// the original content unchanged.
func UnfenceMarkdown(content string) string {
	if content == "" {
		return content
	}

	// Trim leading/trailing whitespace for analysis
	trimmed := strings.TrimSpace(content)

	// Check for opening fence: ```markdown, ```md, ~~~markdown, or ~~~md
	// Must be at the start of the content (after trimming)
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 2 {
		// Need at least opening fence and closing fence
		return content
	}

	firstLine := strings.TrimSpace(lines[0])
	lastLine := strings.TrimSpace(lines[len(lines)-1])

	// Check if first line is a markdown code fence
	var fenceChar string
	var fenceLength int
	var isMarkdownFence bool

	// Check for backtick fences (3 or more backticks)
	if strings.HasPrefix(firstLine, "```") {
		fenceChar = "`"
		// Count the number of consecutive backticks
		fenceLength = 0
		for _, ch := range firstLine {
			if ch == '`' {
				fenceLength++
			} else {
				break
			}
		}
		remainder := strings.TrimSpace(firstLine[fenceLength:])
		// Check if it's markdown or md language tag or empty
		if remainder == "" || strings.EqualFold(remainder, "markdown") || strings.EqualFold(remainder, "md") {
			isMarkdownFence = true
		}
	} else if strings.HasPrefix(firstLine, "~~~") {
		// Check for tilde fences (3 or more tildes)
		fenceChar = "~"
		// Count the number of consecutive tildes
		fenceLength = 0
		for _, ch := range firstLine {
			if ch == '~' {
				fenceLength++
			} else {
				break
			}
		}
		remainder := strings.TrimSpace(firstLine[fenceLength:])
		// Check if it's markdown or md language tag or empty
		if remainder == "" || strings.EqualFold(remainder, "markdown") || strings.EqualFold(remainder, "md") {
			isMarkdownFence = true
		}
	}

	if !isMarkdownFence {
		// Not a markdown fence, return original content
		return content
	}

	// Check if last line is a matching closing fence
	// Must have at least as many fence characters as the opening fence
	var isClosingFence bool
	if fenceChar == "`" {
		// Count backticks in last line
		closingFenceLength := 0
		for _, ch := range lastLine {
			if ch == '`' {
				closingFenceLength++
			} else {
				break
			}
		}
		// Must have at least as many backticks as opening fence
		if closingFenceLength >= fenceLength && strings.TrimSpace(lastLine[closingFenceLength:]) == "" {
			isClosingFence = true
		}
	} else if fenceChar == "~" {
		// Count tildes in last line
		closingFenceLength := 0
		for _, ch := range lastLine {
			if ch == '~' {
				closingFenceLength++
			} else {
				break
			}
		}
		// Must have at least as many tildes as opening fence
		if closingFenceLength >= fenceLength && strings.TrimSpace(lastLine[closingFenceLength:]) == "" {
			isClosingFence = true
		}
	}

	if !isClosingFence {
		// No matching closing fence, return original content
		return content
	}

	// Extract the content between the fences
	// Remove first and last lines
	innerLines := lines[1 : len(lines)-1]
	innerContent := strings.Join(innerLines, "\n")

	markdownUnfencingLog.Printf("Unfenced markdown content: removed outer %s fence", fenceChar)

	// Return the inner content with original leading/trailing whitespace style preserved
	// We preserve the trimming behavior that was applied
	return strings.TrimSpace(innerContent)
}
