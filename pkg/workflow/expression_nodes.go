package workflow

import (
	"fmt"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var expressionNodesLog = logger.New("workflow:expression_nodes")

// ConditionNode represents a node in a condition expression tree
type ConditionNode interface {
	Render() string
}

// ExpressionNode represents a leaf expression
type ExpressionNode struct {
	Expression  string
	Description string // Optional comment/description for the expression
}

func (e *ExpressionNode) Render() string {
	return e.Expression
}

// AndNode represents an AND operation between two conditions
type AndNode struct {
	Left, Right ConditionNode
}

func (a *AndNode) Render() string {
	return fmt.Sprintf("(%s) && (%s)", a.Left.Render(), a.Right.Render())
}

// OrNode represents an OR operation between two conditions
type OrNode struct {
	Left, Right ConditionNode
}

func (o *OrNode) Render() string {
	return fmt.Sprintf("(%s) || (%s)", o.Left.Render(), o.Right.Render())
}

// NotNode represents a NOT operation on a condition
type NotNode struct {
	Child ConditionNode
}

func (n *NotNode) Render() string {
	// For simple function calls like cancelled(), render as !cancelled() instead of !(cancelled())
	// This prevents GitHub Actions from interpreting the extra parentheses as an object structure
	if _, isFunctionCall := n.Child.(*FunctionCallNode); isFunctionCall {
		return fmt.Sprintf("!%s", n.Child.Render())
	}
	return fmt.Sprintf("!(%s)", n.Child.Render())
}

// ParenthesesNode wraps a condition in parentheses for proper YAML interpretation
type ParenthesesNode struct {
	Child ConditionNode
}

func (p *ParenthesesNode) Render() string {
	return fmt.Sprintf("(%s)", p.Child.Render())
}

// DisjunctionNode represents an OR operation with multiple terms to avoid deep nesting
type DisjunctionNode struct {
	Terms     []ConditionNode
	Multiline bool // If true, render each term on separate line with comments
}

func (d *DisjunctionNode) Render() string {
	if len(d.Terms) == 0 {
		return ""
	}
	if len(d.Terms) == 1 {
		return d.Terms[0].Render()
	}

	// Use multiline rendering if enabled
	if d.Multiline {
		return d.RenderMultiline()
	}

	var parts []string
	for _, term := range d.Terms {
		parts = append(parts, term.Render())
	}
	return strings.Join(parts, " || ")
}

// RenderMultiline renders the disjunction with each term on a separate line,
// including comments for expressions that have descriptions
func (d *DisjunctionNode) RenderMultiline() string {
	if len(d.Terms) == 0 {
		return ""
	}
	if len(d.Terms) == 1 {
		return d.Terms[0].Render()
	}

	expressionNodesLog.Printf("Rendering multiline disjunction with %d terms", len(d.Terms))

	var lines []string
	for i, term := range d.Terms {
		var line string

		// Add comment if this is an ExpressionNode with a description
		if expr, ok := term.(*ExpressionNode); ok && expr.Description != "" {
			line = "# " + expr.Description + "\n"
		}

		// Add the expression with OR operator (except for the last term)
		if i < len(d.Terms)-1 {
			line += term.Render() + " ||"
		} else {
			line += term.Render()
		}

		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// FunctionCallNode represents a function call expression like contains(array, value)
type FunctionCallNode struct {
	FunctionName string
	Arguments    []ConditionNode
}

func (f *FunctionCallNode) Render() string {
	var args []string
	for _, arg := range f.Arguments {
		args = append(args, arg.Render())
	}
	return fmt.Sprintf("%s(%s)", f.FunctionName, strings.Join(args, ", "))
}

// PropertyAccessNode represents property access like github.event.action
type PropertyAccessNode struct {
	PropertyPath string
}

func (p *PropertyAccessNode) Render() string {
	return p.PropertyPath
}

// StringLiteralNode represents a string literal value
type StringLiteralNode struct {
	Value string
}

func (s *StringLiteralNode) Render() string {
	return fmt.Sprintf("'%s'", s.Value)
}

// BooleanLiteralNode represents a boolean literal value
type BooleanLiteralNode struct {
	Value bool
}

func (b *BooleanLiteralNode) Render() string {
	if b.Value {
		return "true"
	}
	return "false"
}

// NumberLiteralNode represents a numeric literal value
type NumberLiteralNode struct {
	Value string
}

func (n *NumberLiteralNode) Render() string {
	return n.Value
}

// ComparisonNode represents comparison operations like ==, !=, <, >, <=, >=
type ComparisonNode struct {
	Left     ConditionNode
	Operator string
	Right    ConditionNode
}

func (c *ComparisonNode) Render() string {
	return fmt.Sprintf("%s %s %s", c.Left.Render(), c.Operator, c.Right.Render())
}

// TernaryNode represents ternary conditional expressions like condition ? true_value : false_value
type TernaryNode struct {
	Condition  ConditionNode
	TrueValue  ConditionNode
	FalseValue ConditionNode
}

func (t *TernaryNode) Render() string {
	return fmt.Sprintf("%s ? %s : %s", t.Condition.Render(), t.TrueValue.Render(), t.FalseValue.Render())
}

// ContainsNode represents array membership checks using contains() function
type ContainsNode struct {
	Array ConditionNode
	Value ConditionNode
}

func (c *ContainsNode) Render() string {
	return fmt.Sprintf("contains(%s, %s)", c.Array.Render(), c.Value.Render())
}
