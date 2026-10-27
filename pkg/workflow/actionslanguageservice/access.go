package actionslanguageservice

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var referenceLog = logger.New("workflow:actions_expression_references")

// Walk visits syntactic nodes, including references inside function arguments
// and computed indexes. Returning false skips the current node's children.
func Walk(expression *Expression, visit func(*Expression) bool) {
	if expression == nil || !visit(expression) {
		return
	}
	for _, child := range expression.Children {
		Walk(child, visit)
	}
}

// Reference returns the static property path, preserving spelling. Computed
// indexes are not static references; quoted text is never treated as a context.
func Reference(expression *Expression) ([]string, bool) {
	if expression == nil {
		return nil, false
	}
	switch expression.Kind {
	case "group":
		return Reference(expression.Child(0))
	case "context":
		return []string{expression.Token.Text}, true
	case "index":
		path, ok := Reference(expression.Child(0))
		if !ok {
			return nil, false
		}
		index := expression.Child(1)
		if index.Kind == "star" {
			return append(path, "*"), true
		}
		if index.Kind != "literal" {
			return nil, false
		}
		switch index.Token.Kind {
		case "identifier", "string":
			if value, ok := index.Token.Value.(string); ok {
				return append(path, value), true
			}
		case "number":
			return append(path, index.Token.Text), true
		}
	}
	return nil, false
}

// References returns terminal static references in source order. The AST walk
// still finds named values used in computed indexes.
func References(expression *Expression) [][]string {
	var paths [][]string
	Walk(expression, func(node *Expression) bool {
		if path, ok := Reference(node); ok {
			paths = append(paths, path)
			return false
		}
		return true
	})
	return paths
}

// RewriteReferences rewrites complete AST access nodes, never string contents.
// Invalid source is preserved; callers requiring validation must parse first.
func RewriteReferences(source string, replace func([]string) (string, bool)) string {
	expression, err := ParseSyntax(source)
	if err != nil {
		referenceLog.Print("Cannot rewrite invalid expression")
		return source
	}
	var result strings.Builder
	offset := 0
	Walk(expression, func(node *Expression) bool {
		path, ok := Reference(node)
		if !ok {
			return true
		}
		replacement, changed := replace(path)
		if !changed {
			return true
		}
		result.WriteString(source[offset:node.Start])
		result.WriteString(replacement)
		offset = node.End
		return false
	})
	if offset == 0 {
		return source
	}
	result.WriteString(source[offset:])
	return result.String()
}

// WrappedExpression parses a value consisting of exactly one interpolation.
func WrappedExpression(source string) (*Expression, bool) {
	source = strings.TrimSpace(source)
	spans, err := ExtractExpressions(source)
	if err != nil || len(spans) != 1 {
		return nil, false
	}
	span := spans[0] //nolint:uncheckedsliceindex // The preceding guard requires exactly one interpolation.
	if span.Start != 0 || span.End != len(source) {
		return nil, false
	}
	expression, err := ParseSyntax(span.Content)
	return expression, err == nil && expression != nil
}
