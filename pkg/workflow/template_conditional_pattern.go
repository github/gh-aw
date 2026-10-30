package workflow

import (
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

// conditionalPattern retains gh-aw's conditional-tag grammar while the Actions
// scanner handles nested interpolations, including braces in quoted literals.
type conditionalPattern struct {
	pattern *regexp.Regexp
}

func (p conditionalPattern) indexes(source string, limit int) [][]int {
	masked := []byte(source)
	spans, _ := actionslanguageservice.ExtractExpressions(source)
	for _, span := range spans {
		for i := span.Start + 3; i < span.End-2; i++ {
			masked[i] = ' '
		}
	}
	return p.pattern.FindAllStringSubmatchIndex(string(masked), limit)
}

func (p conditionalPattern) FindAllStringSubmatch(source string, limit int) [][]string {
	var matches [][]string
	for _, indexes := range p.indexes(source, limit) {
		var match []string
		for i := 0; i < len(indexes); i += 2 {
			if indexes[i] < 0 {
				match = append(match, "")
			} else {
				match = append(match, source[indexes[i]:indexes[i+1]])
			}
		}
		matches = append(matches, match)
	}
	return matches
}

func (p conditionalPattern) FindStringSubmatch(source string) []string {
	matches := p.FindAllStringSubmatch(source, 1)
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

func (p conditionalPattern) FindAllString(source string, limit int) []string {
	var matches []string
	for _, indexes := range p.indexes(source, limit) {
		matches = append(matches, source[indexes[0]:indexes[1]])
	}
	return matches
}

func (p conditionalPattern) FindAllStringIndex(source string, limit int) [][]int {
	var matches [][]int
	for _, indexes := range p.indexes(source, limit) {
		matches = append(matches, indexes[:2])
	}
	return matches
}

func (p conditionalPattern) ReplaceAllStringFunc(source string, replace func(string) string) string {
	var result strings.Builder
	offset := 0
	for _, indexes := range p.indexes(source, -1) {
		result.WriteString(source[offset:indexes[0]])
		result.WriteString(replace(source[indexes[0]:indexes[1]]))
		offset = indexes[1]
	}
	result.WriteString(source[offset:])
	return result.String()
}
