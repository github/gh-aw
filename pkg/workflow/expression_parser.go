package workflow

import (
	"errors"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

var expressionsLog = logger.New("workflow:expressions")

func stripExpressionWrapper(expression string) string {
	expr := strings.TrimSpace(expression)
	if isExpression(expr) {
		return strings.TrimSpace(expr[3 : len(expr)-2])
	}
	return expr
}

// ParseExpression adapts the official grammar to the condition optimizer's
// existing nodes. Non-logical operands retain their original source spelling.
func ParseExpression(source string) (ConditionNode, error) {
	expression, err := actionslanguageservice.ParseSyntax(source)
	if err != nil {
		return nil, err
	}
	if expression == nil {
		return nil, errors.New("empty expression")
	}
	return conditionFromExpression(source, expression), nil
}

func conditionFromExpression(source string, expression *actionslanguageservice.Expression) ConditionNode {
	switch expression.Kind {
	case "group":
		return conditionFromExpression(source, expression.Child(0))
	case "unary":
		return &NotNode{Child: conditionFromExpression(source, expression.Child(0))}
	case "logical":
		var result ConditionNode
		for _, child := range expression.Children {
			operand := conditionFromExpression(source, child)
			if result == nil {
				result = operand
			} else if expression.Token.Kind == "&&" {
				result = &AndNode{Left: result, Right: operand}
			} else {
				result = &OrNode{Left: result, Right: operand}
			}
		}
		return result
	default:
		return &ExpressionNode{Expression: source[expression.Start:expression.End]}
	}
}

// VisitExpressionTree visits opaque operands of a condition tree. Typed builder
// nodes remain separate from the parser adapter, as required by optimizer callers.
func VisitExpressionTree(node ConditionNode, visitor func(*ExpressionNode) error) error {
	switch n := node.(type) {
	case *ExpressionNode:
		return visitor(n)
	case *AndNode:
		if err := VisitExpressionTree(n.Left, visitor); err != nil {
			return err
		}
		return VisitExpressionTree(n.Right, visitor)
	case *OrNode:
		if err := VisitExpressionTree(n.Left, visitor); err != nil {
			return err
		}
		return VisitExpressionTree(n.Right, visitor)
	case *NotNode:
		return VisitExpressionTree(n.Child, visitor)
	case *DisjunctionNode:
		for _, term := range n.Terms {
			if err := VisitExpressionTree(term, visitor); err != nil {
				return err
			}
		}
	}
	return nil
}

func BreakLongExpression(source string) []string {
	if len(source) <= int(constants.MaxExpressionLineLength) {
		return []string{source}
	}
	tokens, err := actionslanguageservice.Tokenize(source)
	if err != nil {
		expressionsLog.Printf("Cannot format invalid expression: %v", err)
		return []string{source}
	}
	var lines []string
	start := 0
	for _, token := range tokens {
		end := token.Offset + len(token.Text)
		if (token.Kind == "||" || token.Kind == "&&") &&
			len(strings.TrimSpace(source[start:end])) > int(constants.ExpressionBreakThreshold) {
			lines = append(lines, strings.TrimSpace(source[start:end]))
			start = end
		}
	}
	if last := strings.TrimSpace(source[start:]); last != "" {
		lines = append(lines, last)
	}
	var result []string
	for _, line := range lines {
		result = append(result, BreakAtParentheses(line)...)
	}
	return result
}

func BreakAtParentheses(source string) []string {
	if len(source) <= int(constants.MaxExpressionLineLength) {
		return []string{source}
	}
	tokens, err := actionslanguageservice.Tokenize(source)
	if err != nil {
		expressionsLog.Printf("Cannot format invalid expression: %v", err)
		return []string{source}
	}
	var lines []string
	start, depth := 0, 0
	for i, token := range tokens {
		switch token.Kind {
		case "(":
			depth++
		case ")":
			depth--
			if depth != 0 || token.Offset+1-start <= 80 || i+1 >= len(tokens) {
				continue
			}
			next := tokens[i+1]
			if next.Kind == "||" || next.Kind == "&&" {
				end := next.Offset + len(next.Text)
				lines = append(lines, strings.TrimSpace(source[start:end]))
				start = end
			}
		}
	}
	if last := strings.TrimSpace(source[start:]); last != "" {
		lines = append(lines, last)
	}
	return lines
}

func hasNewlineInStringLiteral(source string) bool {
	tokens, err := actionslanguageservice.Tokenize(source)
	if err != nil {
		expressionsLog.Printf("Cannot inspect invalid expression literal: %v", err)
		return false
	}
	for _, token := range tokens {
		if token.Kind == "string" && strings.Contains(token.Text, "\n") {
			return true
		}
	}
	return false
}

func escapeForYAMLDoubleQuoted(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(s)
}
