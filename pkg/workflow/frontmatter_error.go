package workflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var frontmatterErrorLog = logger.New("workflow:frontmatter_error")

// frontmatterParseErrPrefix is the string prefix that ExtractFrontmatterFromContent
// prepends to the formatted yaml.FormatError() output when a YAML syntax error occurs.
// It is used as a sentinel to detect whether a frontmatter error already carries
// formatted YAML position information.
const frontmatterParseErrPrefix = "failed to parse frontmatter:\n"

// Package-level compiled regex patterns for better performance
var (
	lineColPattern       = regexp.MustCompile(`\[(\d+):(\d+)\]\s*(.+)`)
	sourceContextPattern = regexp.MustCompile(`\n(>?\s*\d+\s*\|)`)
	yamlKeyLinePattern   = regexp.MustCompile(`^(\s*)([A-Za-z0-9._-]+)\s*:\s*(.+?)\s*$`)
	yamlAnyKeyLine       = regexp.MustCompile(`^(\s*)([A-Za-z0-9._-]+)\s*:\s*(.*)$`)
)

// readSourceContextLines extracts source lines around a target line (±3 lines)
// from the given file content for Rust-style error rendering.
// The returned slice is suitable for console.CompilerError.Context.
func readSourceContextLines(content []byte, targetLine int) []string {
	allLines := strings.Split(string(content), "\n")
	contextSize := 7 // ±3 lines around the error

	// Calculate the expected first line of the context window
	expectedFirstLine := targetLine - contextSize/2
	fileStart := max(0, expectedFirstLine-1) // 0-indexed, clamped to file start

	var contextLines []string

	// Pad with empty strings for lines that are before the file
	for lineNum := expectedFirstLine; lineNum < 1; lineNum++ {
		contextLines = append(contextLines, "")
	}

	// Add real lines from the file
	fileEnd := min(len(allLines), fileStart+contextSize-len(contextLines))
	for i := fileStart; i < fileEnd; i++ {
		contextLines = append(contextLines, allLines[i])
	}

	return contextLines
}

// findFrontmatterFieldLine searches frontmatterLines for a line whose first
// non-space key matches fieldName (e.g., "engine") and returns the 1-based
// document line number.  frontmatterStart is the 1-based line number of the
// first frontmatter line (i.e., the line immediately after the opening "---").
// Returns 0 if the field is not found.
//
// Only top-level (non-indented) keys are matched.  Nested values that happen
// to contain the field name are ignored.
func findFrontmatterFieldLine(frontmatterLines []string, frontmatterStart int, fieldName string) int {
	prefix := fieldName + ":"
	for i, line := range frontmatterLines {
		// Match only non-indented lines so nested YAML values are not confused
		// with top-level keys (e.g. "  engine: ..." inside a mapping is ignored).
		if strings.HasPrefix(line, prefix) {
			return frontmatterStart + i
		}
	}
	return 0
}

func findEngineIDPosition(result *parser.FrontmatterResult) (int, int) {
	if result == nil {
		return 0, 0
	}

	engineIndent := -1
	engineChildIndent := -1
	for i, line := range result.FrontmatterLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if engineIndent < 0 {
			if indent == 0 && strings.HasPrefix(trimmed, "engine:") {
				remainder := strings.TrimSpace(strings.TrimPrefix(trimmed, "engine:"))
				if remainder == "" || strings.HasPrefix(remainder, "#") {
					engineIndent = indent
					continue
				}
				return result.FrontmatterStart + i, 1
			}
			continue
		}

		if indent <= engineIndent {
			break
		}
		if engineChildIndent < 0 {
			engineChildIndent = indent
		}
		if indent == engineChildIndent && strings.HasPrefix(trimmed, "id:") {
			return result.FrontmatterStart + i, indent + 1
		}
	}

	line := result.FieldLines["engine"]
	if line == 0 {
		line = findFrontmatterFieldLine(result.FrontmatterLines, result.FrontmatterStart, "engine")
	}
	if line == 0 {
		return 0, 0
	}
	return line, 1
}

// createFrontmatterError creates a detailed error for frontmatter parsing issues
// frontmatterLineOffset is the line number where the frontmatter content begins (1-based)
// Returns error in VSCode-compatible format: filename:line:column: error message
func (c *Compiler) createFrontmatterError(filePath, content string, err error, frontmatterLineOffset int) error {
	frontmatterErrorLog.Printf("Creating frontmatter error for file: %s, offset: %d", filePath, frontmatterLineOffset)

	errorStr := err.Error()

	// Check if error already contains formatted yaml.FormatError() output with source context
	// yaml.FormatError() produces output like "failed to parse frontmatter:\n[line:col] message\n>  line | content..."
	if strings.Contains(errorStr, frontmatterParseErrPrefix+"[") && (strings.Contains(errorStr, "\n>") || strings.Contains(errorStr, "|")) {
		// Extract line and column from the formatted error for VSCode compatibility
		// Pattern: [line:col] message
		if matches := lineColPattern.FindStringSubmatch(errorStr); len(matches) >= 4 {
			line := matches[1]
			col := matches[2]
			originalLine := line
			originalCol := col
			message := matches[3]
			// Extract just the first line of the message (before newline)
			if idx := strings.Index(message, "\n"); idx != -1 {
				message = message[:idx]
			}
			// Translate raw YAML parser messages to user-friendly plain English.
			// Uses the shared translation table from pkg/parser to keep both code paths in sync.
			message = parser.TranslateYAMLMessage(message)
			line, col, message = improveFrontmatterDiagnostic(content, line, col, message)

			// Format as: filename:line:column: error: message
			// This is compatible with VSCode's problem matcher
			vscodeFormat := fmt.Sprintf("%s:%s:%s: error: %s", filePath, line, col, message)

			// Extract just the source context lines (skip the [line:col] message line to avoid duplication)
			// Find the first line that starts with whitespace + digit + | (source context line)
			if loc := sourceContextPattern.FindStringIndex(errorStr); loc != nil {
				// Extract from the first source context line to the end
				context := errorStr[loc[0]+1:] // +1 to skip the leading newline
				if line != originalLine || col != originalCol {
					if custom := renderSourceContextForPosition(content, parsePositiveInt(line), parsePositiveInt(col)); custom != "" {
						context = custom
					}
				}
				// Return VSCode-compatible format on first line, followed by source context only
				frontmatterErrorLog.Print("Formatting error for VSCode compatibility")
				return parser.NewFormattedParserError(fmt.Sprintf("%s\n%s", vscodeFormat, context))
			}

			// If we can't extract source context, return just the VSCode format
			return parser.NewFormattedParserError(vscodeFormat)
		}

		// Fallback if we can't parse the line/col: emit an IDE-compatible error
		// pointing to the frontmatter start so the developer is at least brought to
		// the right section rather than the useless line 1, col 1.
		return frontmatterStartFallback(filePath, errorStr, frontmatterLineOffset)
	}

	// Fallback: if not already formatted, point to the frontmatter start.
	frontmatterErrorLog.Printf("Using fallback error message: %v", err)
	fallbackFmt := fmt.Sprintf("%s:%d:1: error: %s", filePath, frontmatterLineOffset, err)
	return parser.NewFormattedParserError(fallbackFmt)
}

func frontmatterStartFallback(filePath, errorStr string, frontmatterLineOffset int) error {
	frontmatterErrorLog.Print("Could not extract line/col from formatted error, falling back to frontmatter start")
	fallbackMsg := "failed to parse YAML frontmatter"
	// Try to surface a single-line description from the raw error text.
	if _, rest, found := strings.Cut(errorStr, frontmatterParseErrPrefix); found {
		firstLine, _, _ := strings.Cut(rest, "\n")
		if translated := parser.TranslateYAMLMessage(strings.TrimSpace(firstLine)); translated != "" {
			fallbackMsg = "failed to parse YAML frontmatter: " + translated
		}
	}
	fallbackFmt := fmt.Sprintf("%s:%d:1: error: %s", filePath, frontmatterLineOffset, fallbackMsg)
	return parser.NewFormattedParserError(fallbackFmt)
}

// improveFrontmatterDiagnostic adjusts known low-quality parser diagnostics to
// point at the true source line and provide user-facing wording.
func improveFrontmatterDiagnostic(content, line, col, message string) (string, string, string) {
	lower := strings.ToLower(strings.TrimSpace(message))
	// Only the translated phrase is checked here because parser.TranslateYAMLMessage
	// runs before improveFrontmatterDiagnostic (see createFrontmatterError); the raw
	// parser wording is never seen by the time this function is called.
	isScalarWithNestedKey := strings.Contains(lower, "value cannot have child keys here")
	if !isScalarWithNestedKey {
		return line, col, message
	}

	lineNum, colNum := parsePositiveInt(line), parsePositiveInt(col)
	if lineNum <= 1 || colNum <= 0 {
		return line, col, message
	}

	lines := strings.Split(content, "\n")
	if lineNum-1 >= len(lines) {
		return line, col, message
	}

	childLine := lines[lineNum-1]
	parentLineNum := lineNum - 1
	if parentLineNum-1 < 0 || parentLineNum-1 >= len(lines) {
		return line, col, message
	}
	parentLine := lines[parentLineNum-1]

	parentMatch := yamlKeyLinePattern.FindStringSubmatch(parentLine)
	if len(parentMatch) < 4 {
		return line, col, message
	}

	parentIndent := len(parentMatch[1])
	parentKey := parentMatch[2]
	parentValue := strings.TrimSpace(parentMatch[3])
	if parentValue == "" || isLikelyYAMLContainer(parentValue) {
		return line, col, message
	}

	childIndent := len(childLine) - len(strings.TrimLeft(childLine, " "))
	if childIndent <= parentIndent {
		return line, col, message
	}

	ancestorKey := nearestAncestorKey(lines, parentLineNum-1, parentIndent)
	if ancestorKey != "tools" {
		return line, col, message
	}

	// Find the value's column offset by searching after the colon, not the full line,
	// to correctly handle keys and values that share a substring (e.g. "foo: foo").
	keyEnd := len(parentMatch[1]) + len(parentMatch[2])
	valueCol := colNum - 1 // fallback: original column
	if colonPos := strings.Index(parentLine[keyEnd:], ":"); colonPos >= 0 {
		afterColon := parentLine[keyEnd+colonPos+1:]
		trimmed := strings.TrimLeft(afterColon, " ")
		valueCol = keyEnd + colonPos + 1 + (len(afterColon) - len(trimmed))
	}

	return strconv.Itoa(parentLineNum), strconv.Itoa(valueCol + 1),
		fmt.Sprintf("tools.%s tool config must be a mapping (object), not a scalar value (for example: toolsets: [default])", parentKey)
}

func parsePositiveInt(v string) int {
	n := 0
	for _, ch := range v {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

func isLikelyYAMLContainer(value string) bool {
	switch strings.TrimSpace(value) {
	case "|", ">", "{}", "[]":
		return true
	}
	return strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[")
}

func nearestAncestorKey(lines []string, startIdx, childIndent int) string {
	for i := startIdx; i >= 0; i-- {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		match := yamlAnyKeyLine.FindStringSubmatch(line)
		if len(match) < 3 {
			continue
		}
		if len(match[1]) < childIndent {
			return match[2]
		}
	}
	return ""
}

func renderSourceContextForPosition(content string, targetLine, targetCol int) string {
	if targetLine <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if targetLine > len(lines) {
		return ""
	}
	if targetCol <= 0 {
		targetCol = 1
	}

	startLine := max(1, targetLine-2)
	endLine := min(len(lines), targetLine+2)

	var b strings.Builder
	for lineNum := startLine; lineNum <= endLine; lineNum++ {
		prefix := " "
		if lineNum == targetLine {
			prefix = ">"
		}
		fmt.Fprintf(&b, "%s %3d | %s\n", prefix, lineNum, lines[lineNum-1])
		if lineNum == targetLine {
			// The source-context prefix is: one prefix char ("%s") + one space + three-digit
			// line number ("%3d") + " | " = 7 fixed chars plus the variable prefix char = 8
			// total. Column N therefore needs 7+N spaces before the caret character.
			fmt.Fprintf(&b, "%s^\n", strings.Repeat(" ", 7+targetCol))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
