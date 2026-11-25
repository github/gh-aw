package lint

import (
	"context"
	"strings"
)

func PromptConditionSyntaxRule() Rule {
	return Rule{
		ID: "prompt-condition-syntax", Description: "Reject unsupported unprefixed else delimiters in authored AW prompts",
		Severity: Error, Targets: AWAST, Check: checkPromptConditionSyntax,
	}
}

func checkPromptConditionSyntax(ctx context.Context, input Input) ([]Diagnostic, error) {
	if err := integrityInput(ctx, input); err != nil || input.Document == nil {
		return nil, err
	}
	var diagnostics []Diagnostic
	var fenceMarker byte
	var fenceLength int
	conditionalDepth := 0
	codeSpanLength := 0
	text := strings.ReplaceAll(strings.ReplaceAll(input.Document.Markdown, "\r\n", "\n"), "\r", "\n")
	lines := strings.SplitAfter(text, "\n")
	for lineNumber, line := range lines {
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		marker, length, rest, isFence := markdownFence(line)
		if fenceMarker != 0 {
			codeSpanLength = 0
			if isFence && marker == fenceMarker && length >= fenceLength && strings.TrimSpace(rest) == "" {
				fenceMarker = 0
				fenceLength = 0
			}
			continue
		}
		if isFence {
			codeSpanLength = 0
			fenceMarker = marker
			fenceLength = length
			continue
		}
		if strings.TrimSpace(line) == "" {
			codeSpanLength = 0
		}
		lineDiagnostics, depth, nextCodeSpanLength, err := scanPromptConditionLine(ctx, line, lines[lineNumber+1:], lineNumber+1, conditionalDepth, codeSpanLength)
		diagnostics = append(diagnostics, lineDiagnostics...)
		if err != nil {
			return diagnostics, err
		}
		conditionalDepth = depth
		codeSpanLength = nextCodeSpanLength
	}
	return diagnostics, nil
}

func scanPromptConditionLine(ctx context.Context, line string, remainingLines []string, lineNumber int, conditionalDepth int, codeSpanLength int) ([]Diagnostic, int, int, error) {
	var diagnostics []Diagnostic
	offset := 0
	if codeSpanLength > 0 {
		end := matchingCodeDelimiter(line, 0, codeSpanLength)
		if end < 0 {
			return diagnostics, conditionalDepth, codeSpanLength, nil
		}
		offset = end + codeSpanLength
		codeSpanLength = 0
	}
	for offset < len(line) {
		if err := ctx.Err(); err != nil {
			return diagnostics, conditionalDepth, codeSpanLength, err
		}
		backtick := strings.IndexByte(line[offset:], '`')
		start := strings.Index(line[offset:], "{{")
		if backtick >= 0 && (start < 0 || backtick < start) {
			backtick += offset
			length := delimiterLength(line[backtick:], "`")
			if end := matchingCodeDelimiter(line, backtick+length, length); end >= 0 {
				offset = end + length
				continue
			}
			if hasMatchingCodeDelimiterAcrossLines(remainingLines, length) {
				codeSpanLength = length
				break
			}
			offset = backtick + length
			continue
		}
		if start < 0 {
			break
		}
		start += offset
		end := strings.Index(line[start+2:], "}}")
		if end < 0 {
			offset = start + 2
			continue
		}
		end += start + 2
		tag := strings.TrimSpace(line[start+2 : end])
		if isUnsupportedElseTag(tag) && conditionalDepth > 0 {
			diagnostics = append(diagnostics, Diagnostic{
				Path: "prompt", Line: lineNumber,
				Message: "AW prompt conditionals require a # prefix on fallback delimiters; use {{#else}} or {{#elseif}}",
			})
		}
		if strings.HasPrefix(tag, "#if ") || strings.HasPrefix(tag, "#if\t") {
			conditionalDepth++
		} else if tag == "/if" || tag == "#endif" {
			if conditionalDepth > 0 {
				conditionalDepth--
			}
		}
		offset = end + 2
	}
	return diagnostics, conditionalDepth, codeSpanLength, nil
}

func isUnsupportedElseTag(tag string) bool {
	fields := strings.Fields(tag)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "else":
		return true
	case "elseif", "else-if", "else_if":
		return len(fields) > 1
	default:
		return false
	}
}

func hasMatchingCodeDelimiterAcrossLines(lines []string, length int) bool {
	for _, line := range lines {
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if strings.TrimSpace(line) == "" {
			return false
		}
		if _, _, _, isFence := markdownFence(line); isFence {
			return false
		}
		if matchingCodeDelimiter(line, 0, length) >= 0 {
			return true
		}
	}
	return false
}

func markdownFence(line string) (byte, int, string, bool) {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent > 3 || indent == len(line) {
		return 0, 0, "", false
	}
	remainder := line[indent:]
	marker := byte('`')
	if strings.HasPrefix(remainder, "~") {
		marker = '~'
	} else if !strings.HasPrefix(remainder, "`") {
		return 0, 0, "", false
	}
	length := delimiterLength(remainder, string(marker))
	if length < 3 {
		return 0, 0, "", false
	}
	return marker, length, remainder[length:], true
}

func delimiterLength(text string, delimiter string) int {
	length := 0
	for length < len(text) && strings.HasPrefix(text[length:], delimiter) {
		length += len(delimiter)
	}
	return length
}

func matchingCodeDelimiter(text string, offset, length int) int {
	for offset < len(text) {
		next := strings.IndexByte(text[offset:], '`')
		if next < 0 {
			return -1
		}
		offset += next
		if delimiterLength(text[offset:], "`") == length {
			return offset
		}
		offset += delimiterLength(text[offset:], "`")
	}
	return -1
}
