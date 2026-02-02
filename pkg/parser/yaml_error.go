package parser

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var yamlErrorLog = logger.New("parser:yaml_error")

// ExtractYAMLError extracts line and column information from YAML parsing errors
// frontmatterLineOffset is the line number where the frontmatter content begins in the document (1-based)
// This allows proper line number reporting when frontmatter is not at the beginning of the document
func ExtractYAMLError(err error, frontmatterLineOffset int) (line int, column int, message string) {
	yamlErrorLog.Printf("Extracting YAML error information: offset=%d", frontmatterLineOffset)
	errStr := err.Error()

	// First try to extract from goccy/go-yaml's [line:column] format
	line, column, message = extractFromGoccyFormat(errStr, frontmatterLineOffset)
	if line > 0 || column > 0 {
		yamlErrorLog.Printf("Extracted error location from goccy format: line=%d, column=%d", line, column)
		return line, column, message
	}

	// Fallback to standard YAML error string parsing for other libraries
	yamlErrorLog.Print("Falling back to string parsing for error location")
	return extractFromStringParsing(errStr, frontmatterLineOffset)
}

// extractFromGoccyFormat extracts line/column from goccy/go-yaml's [line:column] message format
func extractFromGoccyFormat(errStr string, frontmatterLineOffset int) (line int, column int, message string) {
	// Look for goccy format like "[5:10] mapping value is not allowed in this context"
	if strings.Contains(errStr, "[") && strings.Contains(errStr, "]") {
		start := strings.Index(errStr, "[")
		end := strings.Index(errStr, "]")
		if start >= 0 && end > start {
			locationPart := errStr[start+1 : end]
			messagePart := strings.TrimSpace(errStr[end+1:])

			// Parse line:column format
			if strings.Contains(locationPart, ":") {
				parts := strings.Split(locationPart, ":")
				if len(parts) == 2 {
					lineStr := strings.TrimSpace(parts[0])
					columnStr := strings.TrimSpace(parts[1])

					// Parse line and column numbers
					if _, parseErr := fmt.Sscanf(lineStr, "%d", &line); parseErr == nil {
						if _, parseErr := fmt.Sscanf(columnStr, "%d", &column); parseErr == nil {
							// Adjust line number to account for frontmatter position in file
							if line > 0 {
								line += frontmatterLineOffset - 1 // -1 because line numbers in YAML errors are 1-based relative to YAML content
							}

							// Only return valid positions - avoid returning 1,1 when location is unknown
							if line <= frontmatterLineOffset && column <= 1 {
								return 0, 0, messagePart
							}

							return line, column, messagePart
						}
					}
				}
			}
		}
	}

	return 0, 0, ""
}

// extractFromStringParsing provides fallback string parsing for other YAML libraries
func extractFromStringParsing(errStr string, frontmatterLineOffset int) (line int, column int, message string) {
	// Parse "yaml: line X: column Y: message" format (enhanced parsers that provide column info)
	if strings.Contains(errStr, "yaml: line ") && strings.Contains(errStr, "column ") {
		parts := strings.SplitN(errStr, "yaml: line ", 2)
		if len(parts) > 1 {
			lineInfo := parts[1]

			// Look for column information
			colonIndex := strings.Index(lineInfo, ":")
			if colonIndex > 0 {
				lineStr := lineInfo[:colonIndex]

				// Parse line number
				if _, parseErr := fmt.Sscanf(lineStr, "%d", &line); parseErr == nil {
					// Look for column part
					remaining := lineInfo[colonIndex+1:]
					if strings.Contains(remaining, "column ") {
						columnParts := strings.SplitN(remaining, "column ", 2)
						if len(columnParts) > 1 {
							columnInfo := columnParts[1]
							colonIndex2 := strings.Index(columnInfo, ":")
							if colonIndex2 > 0 {
								columnStr := columnInfo[:colonIndex2]
								message = strings.TrimSpace(columnInfo[colonIndex2+1:])

								// Parse column number
								if _, parseErr := fmt.Sscanf(columnStr, "%d", &column); parseErr == nil {
									// Adjust line number to account for frontmatter position in file
									line += frontmatterLineOffset - 1 // -1 because line numbers in YAML errors are 1-based relative to YAML content
									return
								}
							}
						}
					}
				}
			}
		}
	}

	// Parse "yaml: line X: message" format (standard format without column info)
	if strings.Contains(errStr, "yaml: line ") {
		parts := strings.SplitN(errStr, "yaml: line ", 2)
		if len(parts) > 1 {
			lineInfo := parts[1]
			colonIndex := strings.Index(lineInfo, ":")
			if colonIndex > 0 {
				lineStr := lineInfo[:colonIndex]
				message = strings.TrimSpace(lineInfo[colonIndex+1:])

				// Parse line number
				if _, parseErr := fmt.Sscanf(lineStr, "%d", &line); parseErr == nil {
					// Adjust line number to account for frontmatter position in file
					line += frontmatterLineOffset - 1 // -1 because line numbers in YAML errors are 1-based relative to YAML content
					// Don't default to column 1 when not provided - return 0 instead
					column = 0
					return
				}
			}
		}
	}

	// Parse "yaml: unmarshal errors: line X: message" format (multiline errors)
	if strings.Contains(errStr, "yaml: unmarshal errors:") && strings.Contains(errStr, "line ") {
		lines := strings.Split(errStr, "\n")
		for _, errorLine := range lines {
			errorLine = strings.TrimSpace(errorLine)
			if strings.Contains(errorLine, "line ") && strings.Contains(errorLine, ":") {
				// Extract the first line number found in the error
				parts := strings.SplitN(errorLine, "line ", 2)
				if len(parts) > 1 {
					colonIndex := strings.Index(parts[1], ":")
					if colonIndex > 0 {
						lineStr := parts[1][:colonIndex]
						restOfMessage := strings.TrimSpace(parts[1][colonIndex+1:])

						// Parse line number
						if _, parseErr := fmt.Sscanf(lineStr, "%d", &line); parseErr == nil {
							// Adjust line number to account for frontmatter position in file
							line += frontmatterLineOffset - 1 // -1 because line numbers in YAML errors are 1-based relative to YAML content
							column = 0                        // Don't default to column 1
							message = restOfMessage
							return
						}
					}
				}
			}
		}
	}

	// Fallback: return original error message with no location
	return 0, 0, errStr
}
