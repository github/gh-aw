package taint

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rhysd/actionlint"
)

// TextExpression describes one interpolation without retaining its contents.
// Line is relative to the supplied text and Sources contains symbolic origins.
type TextExpression struct {
	Line    int
	Sources []Source
}

// ExpressionError identifies a parsing coverage gap without exposing values.
type ExpressionError struct {
	Path string
	Line int
}

func (a *analyzer) executableText(text, path string, bindings scope) error {
	site := Site{Kind: ExecutableInterpolation, Path: path}
	if !a.rule.IsSink(site) {
		return nil
	}
	return VisitInterpolations(context.Background(), text, path, func(_, _, line int, node actionlint.ExprNode) error {
		site.Line = line
		a.emit(site, a.expression(node, path, bindings))
		return nil
	})
}

func (e *ExpressionError) Error() string {
	return fmt.Sprintf("invalid Actions expression at %s:%d", e.Path, e.Line)
}

// AnalyzeText tracks sources in direct Actions interpolations, without treating
// shell expansions as executable interpolation. Bindings are symbolic Actions
// references, such as env.title, not shell variables. Results preceding a
// malformed expression are returned alongside the value-free parsing error.
func AnalyzeText(ctx context.Context, text, path string, rule Rule, bindings map[string][]Source) ([]TextExpression, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rule.IsSource == nil {
		return nil, errors.New("text taint analysis requires a source predicate")
	}
	bound := make(scope, len(bindings))
	for ref, sources := range bindings {
		bound[strings.ToLower(ref)] = union(bound[strings.ToLower(ref)], sources)
	}
	a := &analyzer{rule: rule}
	var result []TextExpression
	err := VisitInterpolations(ctx, text, path, func(_, _, line int, node actionlint.ExprNode) error {
		result = append(result, TextExpression{Line: line, Sources: a.expression(node, path, bound)})
		return ctx.Err()
	})
	return result, err
}

// VisitInterpolations visits parsed Actions expressions with byte offsets and
// text-relative lines. Quoted '}}' and doubled single quotes are expression
// data, not delimiters. Parse errors are value-free; earlier visits are retained.
func VisitInterpolations(ctx context.Context, text, path string, visit func(start, end, line int, node actionlint.ExprNode) error) error {
	offset, line := 0, 1
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := strings.Index(text[offset:], "${{")
		if start < 0 {
			return nil
		}
		start += offset
		line += strings.Count(text[offset:start], "\n")
		body := text[start+3:]
		lexer := actionlint.NewExprLexer(body)
		node, err := actionlint.NewExprParser().Parse(lexer)
		end := lexer.Offset()
		if err != nil || end < 2 || end > len(body) || body[end-2:end] != "}}" {
			return &ExpressionError{Path: path, Line: line}
		}
		end += start + 3
		if err := visit(start, end, line, node); err != nil {
			return err
		}
		line += strings.Count(text[start:end], "\n")
		offset = end
	}
}
