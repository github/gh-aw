package workflow

import (
	"regexp"
	"strings"
)

var inlineCodeDelimiterPattern = regexp.MustCompile("`+")

// Mask code only for identifying literal conditional examples. Explicit Actions
// expressions and valid bindings inside code regions still undergo validation.
func maskTemplateCodeRegions(markdown string) string {
	masked := []byte(markdown)
	mask := func(start, end int) {
		for i := start; i < end; i++ {
			if i >= 0 && i < len(masked) {
				if masked[i] != '\n' && masked[i] != '\r' {
					masked[i] = ' '
				}
			}
		}
	}
	offset := 0
	openMarker := ""
	for line := range strings.SplitAfterSeq(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if openMarker != "" {
			mask(offset, offset+len(line))
			if isMatchingCodeBlockMarker(trimmed, openMarker) {
				openMarker = ""
			}
		} else if isValidCodeBlockMarker(trimmed) {
			openMarker, _ = extractCodeBlockMarker(trimmed)
			mask(offset, offset+len(line))
		}
		offset += len(line)
	}
	delimiters := inlineCodeDelimiterPattern.FindAllIndex(masked, -1)
	covered := 0
	for i, location := range delimiters {
		open := [2]int(location)
		if open[0] < covered {
			continue
		}
		for _, candidate := range delimiters[i+1:] {
			close := [2]int(candidate)
			if close[1]-close[0] == open[1]-open[0] {
				mask(open[0], close[1])
				covered = close[1]
				break
			}
		}
	}
	return string(masked)
}

func replaceConditionalTags(markdown string, pattern conditionalPattern, replace func(string, bool) string) string {
	masked := maskTemplateCodeRegions(markdown)
	var result strings.Builder
	offset := 0
	for _, location := range pattern.FindAllStringIndex(markdown, -1) {
		bounds := [2]int(location)
		start, end := bounds[0], bounds[1]
		result.WriteString(markdown[offset:start])
		codeExample := false
		if start >= 0 && start < len(masked) {
			codeExample = masked[start] == ' '
		}
		result.WriteString(replace(markdown[start:end], codeExample))
		offset = end
	}
	result.WriteString(markdown[offset:])
	return result.String()
}
