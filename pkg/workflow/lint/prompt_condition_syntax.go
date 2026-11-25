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
	text := strings.ReplaceAll(strings.ReplaceAll(input.Document.Markdown, "\r\n", "\n"), "\r", "\n")
	for lineNumber, line := range strings.SplitAfter(text, "\n") {
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		marker, length, rest, isFence := markdownFence(line)
		if fenceMarker != 0 {
			if isFence && marker == fenceMarker && length >= fenceLength && strings.TrimSpace(rest) == "" {
				fenceMarker = 0
				fenceLength = 0
			}
			continue
		}
		if isFence {
			fenceMarker = marker
			fenceLength = length
			continue
		}
		lineDiagnostics, depth, err := scanPromptConditionLine(ctx, line, lineNumber+1, conditionalDepth)
		diagnostics = append(diagnostics, lineDiagnostics...)
		if err != nil {
			return diagnostics, err
		}
		conditionalDepth = depth
	}
	return diagnostics, nil
}

func scanPromptConditionLine(ctx context.Context, line string, lineNumber int, conditionalDepth int) ([]Diagnostic, int, error) {
	var diagnostics []Diagnostic
	for offset := 0; offset < len(line); {
		if err := ctx.Err(); err != nil {
			return diagnostics, conditionalDepth, err
		}
		if strings.HasPrefix(line[offset:], "`") {
			length := delimiterLength(line[offset:], "`")
			if end := matchingCodeDelimiter(line, offset+length, length); end >= 0 {
				offset = end + length
				continue
			}
			offset += length
			continue
		}
		start := strings.Index(line[offset:], "{{")
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
		if tag == "else" && conditionalDepth > 0 {
			diagnostics = append(diagnostics, Diagnostic{
				Path: "prompt", Line: lineNumber,
				Message: "AW prompt conditionals use {{#else}} for the fallback branch; replace the unprefixed {{else}} delimiter",
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
	return diagnostics, conditionalDepth, nil
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
