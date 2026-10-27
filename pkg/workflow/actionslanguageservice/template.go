package actionslanguageservice

import (
	"errors"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Interpolation retains the expression's content and byte range, including its
// delimiters. End is exclusive.
type Interpolation struct {
	Start   int
	End     int
	Content string
}

// ExtractExpressions finds interpolations without mistaking quoted braces for
// closing delimiters. It does not interpret ordinary text outside expressions.
func ExtractExpressions(source string) ([]Interpolation, error) {
	var result []Interpolation
	for offset := 0; offset < len(source); {
		start := strings.Index(source[offset:], "${{")
		if start < 0 {
			break
		}
		start += offset
		end, err := expressionEnd(source, start+3)
		if err != nil {
			return result, fmt.Errorf("opening '${{' at position %d without matching closing '}}': %w", start, err)
		}
		result = append(result, Interpolation{Start: start, End: end + 2, Content: source[start+3 : end]})
		offset = end + 2
	}
	return result, nil
}

// template follows the upstream quote-aware interpolation scanner. Braces and
// doubled single quotes inside string literals do not end an expression.
func (p *workflowParser) template(node *yaml.Node, context []string) (bool, *string, error) {
	if node.Tag != "!!str" {
		return false, nil, nil
	}
	source := node.Value
	var expressions []*Expression
	for offset := 0; offset < len(source); {
		start := strings.Index(source[offset:], "${{")
		if start < 0 {
			break
		}
		start += offset
		end, err := expressionEnd(source, start+3)
		if err != nil {
			return false, nil, p.error(node, err.Error())
		}
		text := strings.TrimSpace(source[start+3 : end])
		if text == "" {
			return false, nil, p.error(node, "an expression was expected")
		}
		if text == "insert" {
			if start != 0 || end+2 != len(source) {
				return false, nil, p.error(node, "the insert directive must occupy the entire value")
			}
			if len(context) == 0 {
				return false, nil, p.error(node, "a template expression is not allowed in this context")
			}
			return true, nil, nil
		}
		expr, err := ParseExpression(text, context)
		if err != nil {
			return false, nil, p.error(node, err.Error())
		}
		if start == 0 && end+2 == len(source) && expr.Kind == "literal" && expr.Token.Kind == "string" {
			text, ok := expr.Token.Value.(string)
			if !ok {
				return false, nil, p.error(node, "a string literal must contain a string value")
			}
			return false, &text, nil
		}
		if len(context) == 0 {
			return false, nil, p.error(node, "a template expression is not allowed in this context")
		}
		expressions = append(expressions, expr)
		offset = end + 2
	}
	if len(expressions) == 0 {
		return false, nil, nil
	}
	p.result.Expressions[node] = expressions
	return true, nil, nil
}

func expressionEnd(source string, start int) (int, error) {
	inString := false
	for i := start; i >= 0 && i < len(source); i++ {
		if source[i] == '\'' {
			inString = !inString
		} else if !inString && source[i] == '}' && i+1 < len(source) && source[i+1] == '}' {
			return i, nil
		}
	}
	if inString {
		return 0, errors.New("unclosed single quote in expression")
	}
	return 0, errors.New("the expression is not closed: found '${{' without matching '}}'")
}
