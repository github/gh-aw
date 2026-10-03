package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var jsLog = logger.New("workflow:js")

// All getter functions return empty strings since embedded scripts were removed

func getNotifyCommentErrorScript() string { return "" }
func getUploadAssetsScript() string       { return "" }

// Public Get* functions return empty strings since embedded scripts were removed

func GetJavaScriptSources() map[string]string {
	return map[string]string{}
}

func GetLogParserScript(name string) string {
	// Return non-empty placeholder to indicate parser exists
	// Actual script is loaded at runtime via require() from ${RUNNER_TEMP}/gh-aw/actions/
	return "EXTERNAL_SCRIPT"
}

func GetLogParserBootstrap() string {
	return ""
}

func GetReadBufferScript() string {
	return ""
}

func GetMCPServerCoreScript() string {
	return ""
}

func GetMCPHTTPTransportScript() string {
	return ""
}

func GetMCPLoggerScript() string {
	return ""
}

func GetMCPScriptsMCPServerHTTPScript() string {
	return ""
}

func GetMCPScriptsConfigLoaderScript() string {
	return ""
}

func GetMCPScriptsValidationScript() string {
	return ""
}

func GetMCPHandlerShellScript() string {
	return ""
}

func GetMCPHandlerPythonScript() string {
	return ""
}

// Helper functions for formatting JavaScript in YAML

func removeJavaScriptComments(code string) string {
	if jsLog.Enabled() {
		jsLog.Printf("Removing JavaScript comments from %d bytes of code", len(code))
	}
	var result strings.Builder
	lines := strings.Split(code, "\n")

	inBlockComment := false

	for _, line := range lines {
		processedLine := removeJavaScriptCommentsFromLine(line, &inBlockComment)
		result.WriteString(processedLine)
		result.WriteString("\n")
	}

	// Remove the trailing newline we added
	resultStr := result.String()
	if resultStr != "" && resultStr[len(resultStr)-1] == '\n' {
		resultStr = resultStr[:len(resultStr)-1]
	}

	if jsLog.Enabled() {
		jsLog.Printf("Removed comments, result: %d bytes", len(resultStr))
	}
	return resultStr
}

// removeJavaScriptCommentsFromLine removes JavaScript comments from a single line
// while preserving comments that appear within string literals and regex literals
func removeJavaScriptCommentsFromLine(line string, inBlockComment *bool) string {
	var result strings.Builder
	runes := []rune(line)
	i := 0

	for i < len(runes) {
		if *inBlockComment {
			// Look for end of block comment
			if i < len(runes)-1 && runes[i] == '*' && runes[i+1] == '/' {
				*inBlockComment = false
				i += 2 // Skip '*/'
			} else {
				i++
			}
			continue
		}

		// Check for start of comments
		if i < len(runes)-1 {
			// Block comment start
			if runes[i] == '/' && runes[i+1] == '*' {
				*inBlockComment = true
				i += 2 // Skip '/*'
				continue
			}
			// Line comment start
			if runes[i] == '/' && runes[i+1] == '/' {
				// Check if we're inside a string literal or regex literal
				beforeSlash := string(runes[:i])
				if !isInsideStringLiteral(beforeSlash) && !isInsideRegexLiteral(beforeSlash) {
					// Rest of line is a comment, stop processing
					break
				}
			}
		}

		// Check for regex literals
		if runes[i] == '/' {
			beforeSlash := string(runes[:i])
			if !isInsideStringLiteral(beforeSlash) && !isInsideRegexLiteral(beforeSlash) && canStartRegexLiteral(beforeSlash) {
				// This is likely a regex literal
				result.WriteRune(runes[i]) // Write the opening /
				i++

				// Process inside regex literal
				for i < len(runes) {
					if runes[i] == '/' {
						// Check if it's escaped
						escapeCount := 0
						j := i - 1
						for j >= 0 && runes[j] == '\\' {
							escapeCount++
							j--
						}
						if escapeCount%2 == 0 {
							// Not escaped, end of regex
							result.WriteRune(runes[i]) // Write the closing /
							i++
							// Skip regex flags (g, i, m, etc.)
							for i < len(runes) && (runes[i] >= 'a' && runes[i] <= 'z' || runes[i] >= 'A' && runes[i] <= 'Z') {
								result.WriteRune(runes[i])
								i++
							}
							break
						}
					}
					result.WriteRune(runes[i])
					i++
				}
				continue
			}
		}

		// Check for string literals
		if runes[i] == '"' || runes[i] == '\'' || runes[i] == '`' {
			quote := runes[i]
			result.WriteRune(runes[i])
			i++

			// Process inside string literal
			for i < len(runes) {
				result.WriteRune(runes[i])
				if runes[i] == quote {
					// Check if it's escaped
					escapeCount := 0
					j := i - 1
					for j >= 0 && runes[j] == '\\' {
						escapeCount++
						j--
					}
					if escapeCount%2 == 0 {
						// Not escaped, end of string
						i++
						break
					}
				}
				i++
			}
			continue
		}

		result.WriteRune(runes[i])
		i++
	}

	return result.String()
}

// isInsideStringLiteral checks if we're currently inside a string literal
// by counting unescaped quotes before the current position
func isInsideStringLiteral(text string) bool {
	runes := []rune(text)
	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false

	for i := range runes {
		switch runes[i] {
		case '\'':
			if !inDoubleQuote && !inBacktick {
				// Check if escaped
				escapeCount := 0
				j := i - 1
				for j >= 0 && runes[j] == '\\' {
					escapeCount++
					j--
				}
				if escapeCount%2 == 0 {
					inSingleQuote = !inSingleQuote
				}
			}
		case '"':
			if !inSingleQuote && !inBacktick {
				// Check if escaped
				escapeCount := 0
				j := i - 1
				for j >= 0 && runes[j] == '\\' {
					escapeCount++
					j--
				}
				if escapeCount%2 == 0 {
					inDoubleQuote = !inDoubleQuote
				}
			}
		case '`':
			if !inSingleQuote && !inDoubleQuote {
				inBacktick = !inBacktick
			}
		}
	}

	return inSingleQuote || inDoubleQuote || inBacktick
}

// isInsideRegexLiteral checks if we're currently inside a regex literal
// by tracking unescaped forward slashes
func isInsideRegexLiteral(text string) bool {
	runes := []rune(text)
	inSingleQuote := false
	inDoubleQuote := false
	inBacktick := false
	inRegex := false

	for i := range runes {
		switch runes[i] {
		case '\'':
			if !inDoubleQuote && !inBacktick && !inRegex {
				// Check if escaped
				escapeCount := 0
				j := i - 1
				for j >= 0 && runes[j] == '\\' {
					escapeCount++
					j--
				}
				if escapeCount%2 == 0 {
					inSingleQuote = !inSingleQuote
				}
			}
		case '"':
			if !inSingleQuote && !inBacktick && !inRegex {
				// Check if escaped
				escapeCount := 0
				j := i - 1
				for j >= 0 && runes[j] == '\\' {
					escapeCount++
					j--
				}
				if escapeCount%2 == 0 {
					inDoubleQuote = !inDoubleQuote
				}
			}
		case '`':
			if !inSingleQuote && !inDoubleQuote && !inRegex {
				inBacktick = !inBacktick
			}
		case '/':
			if !inSingleQuote && !inDoubleQuote && !inBacktick {
				// Check if escaped
				escapeCount := 0
				j := i - 1
				for j >= 0 && runes[j] == '\\' {
					escapeCount++
					j--
				}
				if escapeCount%2 == 0 {
					if inRegex {
						// End of regex
						inRegex = false
					} else if canStartRegexLiteralAt(text, i) {
						// Start of regex
						inRegex = true
					}
				}
			}
		}
	}

	return inRegex
}

// canStartRegexLiteral checks if a regex literal can start at the current position
// based on what comes before
func canStartRegexLiteral(beforeText string) bool {
	return canStartRegexLiteralAt(beforeText, len([]rune(beforeText)))
}

// canStartRegexLiteralAt checks if a regex literal can start at the given position
func canStartRegexLiteralAt(text string, pos int) bool {
	if pos == 0 {
		return true // Beginning of line
	}

	runes := []rune(text)
	if pos > len(runes) {
		return false
	}

	// Skip backward over whitespace
	i := pos - 1
	for i >= 0 && (runes[i] == ' ' || runes[i] == '\t') {
		i--
	}

	if i < 0 {
		return true // Only whitespace before
	}

	lastChar := runes[i]

	// Regex can start after these characters/operators
	switch lastChar {
	case '=', '(', '[', ',', ':', ';', '!', '&', '|', '?', '+', '-', '*', '/', '%', '{', '}', '~', '^':
		return true
	case ')':
		// Check if it's after keywords like "return", "throw"
		word := extractWordBefore(runes, i)
		return word == "return" || word == "throw" || word == "typeof" || word == "new" || word == "in" || word == "of"
	default:
		// Check if it's after certain keywords
		word := extractWordBefore(runes, i+1)
		return word == "return" || word == "throw" || word == "typeof" || word == "new" || word == "in" || word == "of" ||
			word == "if" || word == "while" || word == "for" || word == "case"
	}
}

// extractWordBefore extracts the word that ends at the given position
func extractWordBefore(runes []rune, endPos int) string {
	if endPos < 0 || endPos >= len(runes) {
		return ""
	}

	// Find the start of the word
	start := endPos
	for start >= 0 && (isLetter(runes[start]) || isDigit(runes[start]) || runes[start] == '_' || runes[start] == '$') {
		start--
	}
	start++ // Move to the first character of the word

	if start > endPos {
		return ""
	}

	return string(runes[start : endPos+1])
}

// isLetter checks if a rune is a letter
func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isDigit checks if a rune is a digit
func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// FormatJavaScriptForYAML formats a JavaScript script with proper indentation for embedding in YAML
func FormatJavaScriptForYAML(script string) []string {
	if jsLog.Enabled() {
		jsLog.Printf("Formatting JavaScript for YAML: %d bytes", len(script))
	}
	var formattedLines []string

	// Remove JavaScript comments first
	cleanScript := removeJavaScriptComments(script)

	scriptLines := strings.SplitSeq(cleanScript, "\n")
	for line := range scriptLines {
		// Skip empty lines when inlining to YAML
		if strings.TrimSpace(line) != "" {
			formattedLines = append(formattedLines, fmt.Sprintf("            %s\n", line))
		}
	}
	if jsLog.Enabled() {
		jsLog.Printf("Formatted %d lines for YAML", len(formattedLines))
	}
	return formattedLines
}

// WriteJavaScriptToYAML writes a JavaScript script with proper indentation to a strings.Builder
func WriteJavaScriptToYAML(yaml *strings.Builder, script string) {
	// Validate that script is not empty - this helps catch errors where getter functions
	// return empty strings after embedded scripts were removed
	if strings.TrimSpace(script) == "" {
		jsLog.Print("WARNING: Attempted to write empty JavaScript script to YAML")
		return
	}

	// Remove JavaScript comments first
	cleanScript := removeJavaScriptComments(script)

	scriptLines := strings.SplitSeq(cleanScript, "\n")
	for line := range scriptLines {
		// Skip empty lines when inlining to YAML
		if strings.TrimSpace(line) != "" {
			fmt.Fprintf(yaml, "            %s\n", line)
		}
	}
}

// GetLogParserScript returns the JavaScript content for a log parser by name
