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
	for lineNumber, line := range strings.Split(input.Document.Markdown, "\n") {
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
		for offset := 0; offset < len(line); {
			if err := ctx.Err(); err != nil {
				return diagnostics, err
			}
			if line[offset] == '`' {
				length := delimiterLength(line[offset:], '`')
				if end := matchingCodeDelimiter(line, offset+length, length); end >= 0 {
					offset = end + length
					continue
				}
				offset += length
				continue
			}
			if !strings.HasPrefix(line[offset:], "{{") {
				offset++
				continue
			}
			end := strings.Index(line[offset+2:], "}}")
			if end < 0 {
				offset += 2
				continue
			}
			end += offset + 2
			tag := strings.TrimSpace(line[offset+2 : end])
			if tag == "else" && conditionalDepth > 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Path: "prompt", Line: lineNumber + 1,
					Message: "AW prompt conditionals use {{#else}} for the fallback branch; replace the unprefixed {{else}} delimiter",
				})
			}
			switch {
			case len(tag) > 3 && tag[:3] == "#if" && (tag[3] == ' ' || tag[3] == '\t'):
				conditionalDepth++
			case tag == "/if" || tag == "#endif":
				if conditionalDepth > 0 {
					conditionalDepth--
				}
			}
			offset = end + 2
		}
	}
	return diagnostics, nil
}

func markdownFence(line string) (byte, int, string, bool) {
	indent := len(line) - len(strings.TrimLeft(line, " "))
	if indent > 3 || indent == len(line) {
		return 0, 0, "", false
	}
	marker := line[indent]
	if marker != '`' && marker != '~' {
		return 0, 0, "", false
	}
	length := delimiterLength(line[indent:], marker)
	if length < 3 {
		return 0, 0, "", false
	}
	return marker, length, line[indent+length:], true
}

func delimiterLength(text string, delimiter byte) int {
	length := 0
	for length < len(text) && text[length] == delimiter {
		length++
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
		if delimiterLength(text[offset:], '`') == length {
			return offset
		}
		offset += delimiterLength(text[offset:], '`')
	}
	return -1
}
