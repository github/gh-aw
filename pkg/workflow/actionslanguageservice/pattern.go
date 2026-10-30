package actionslanguageservice

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var patternLog = logger.New("workflow:actions_expression_matches")

// Pattern adapts existing matching call sites to the template scanner and AST.
// It is a detector, not a validator: strict entry points report malformed input.
// Filter returns captures after the full match, or nil to reject that match.
type Pattern struct {
	Filter    func(string, *Expression) []string
	Whole     bool
	Unwrapped bool
}

func (p Pattern) FindAllStringSubmatch(source string, limit int) [][]string {
	var result [][]string
	for _, match := range p.matches(source, limit) {
		result = append(result, append([]string{source[match.start:match.end]}, match.captures...))
	}
	return result
}

func (p Pattern) FindAllStringSubmatchIndex(source string, limit int) [][]int {
	var result [][]int
	for _, match := range p.matches(source, limit) {
		indexes := []int{match.start, match.end}
		for _, capture := range match.captures {
			position := strings.Index(source[match.contentStart:match.end], capture)
			if position < 0 {
				indexes = append(indexes, -1, -1)
			} else {
				indexes = append(indexes, match.contentStart+position, match.contentStart+position+len(capture))
			}
		}
		result = append(result, indexes)
	}
	return result
}

type patternMatch struct {
	start, end, contentStart int
	captures                 []string
}

func (p Pattern) matches(source string, limit int) []patternMatch {
	if limit == 0 {
		return nil
	}
	spans, err := ExtractExpressions(source)
	if err != nil {
		patternLog.Printf("Incomplete interpolation while detecting expressions: %v", err)
	}
	if p.Unwrapped {
		spans = []Interpolation{{Start: 0, End: len(source), Content: source}}
	}
	var result []patternMatch
	for _, span := range spans {
		if p.Whole && (span.Start != 0 || span.End != len(source)) {
			continue
		}
		var captures []string
		start := span.Start + 3
		if p.Unwrapped {
			start = 0
		}
		if p.Filter == nil {
			captures = []string{span.Content}
		} else {
			expr, err := ParseSyntax(span.Content)
			if err != nil || expr == nil {
				// Parser errors can include the source, so log only location metadata.
				patternLog.Printf("Cannot match invalid expression: start=%d end=%d length=%d", span.Start, span.End, len(span.Content))
				continue
			}
			captures = p.Filter(span.Content, expr)
			if captures == nil {
				continue
			}
		}
		result = append(result, patternMatch{span.Start, span.End, start, captures})
		if limit > 0 && len(result) == limit {
			break
		}
	}
	return result
}

func (p Pattern) FindStringSubmatch(source string) []string {
	for _, match := range p.FindAllStringSubmatch(source, 1) {
		return match
	}
	return nil
}

func (p Pattern) FindAllString(source string, limit int) []string {
	var result []string
	for _, match := range p.matches(source, limit) {
		result = append(result, source[match.start:match.end])
	}
	return result
}

func (p Pattern) MatchString(source string) bool {
	return len(p.FindAllStringSubmatchIndex(source, 1)) > 0
}

func (p Pattern) ReplaceAllStringFunc(source string, replace func(string) string) string {
	var result strings.Builder
	offset := 0
	for _, match := range p.matches(source, -1) {
		result.WriteString(source[offset:match.start])
		result.WriteString(replace(source[match.start:match.end]))
		offset = match.end
	}
	result.WriteString(source[offset:])
	return result.String()
}

func (p Pattern) ReplaceAllString(source, replacement string) string {
	return p.ReplaceAllStringFunc(source, func(string) string { return replacement })
}

// ReferencePattern matches static property paths selected by the caller's policy.
func ReferencePattern(whole, unwrapped bool, filter func([]string) []string) Pattern {
	return Pattern{Whole: whole, Unwrapped: unwrapped, Filter: func(_ string, expression *Expression) []string {
		path, ok := Reference(expression)
		if !ok {
			return nil
		}
		return filter(path)
	}}
}
