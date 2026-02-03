package workflow

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var expressionsLog = logger.New("workflow:expressions")

// stripExpressionWrapper removes the ${{ }} wrapper from an expression if present
func stripExpressionWrapper(expression string) string {
	// Trim whitespace
	expr := strings.TrimSpace(expression)
	// Check if it starts with ${{ and ends with }}
	if strings.HasPrefix(expr, "${{") && strings.HasSuffix(expr, "}}") {
		// Remove the wrapper and trim inner whitespace
		return strings.TrimSpace(expr[3 : len(expr)-2])
	}
	return expr
}

// ExpressionParser handles parsing of expression strings into ConditionNode trees
type ExpressionParser struct {
	tokens []token
	pos    int
}

type token struct {
	kind  tokenKind
	value string
	pos   int
}

type tokenKind int

const (
	tokenLiteral tokenKind = iota
	tokenAnd
	tokenOr
	tokenNot
	tokenLeftParen
	tokenRightParen
	tokenEOF
)

// ParseExpression parses a string expression into a ConditionNode tree
// Supports && (AND), || (OR), ! (NOT), and parentheses for grouping
// Example: "condition1 && (condition2 || !condition3)"
func ParseExpression(expression string) (ConditionNode, error) {
	expressionsLog.Printf("Parsing expression: %s", expression)

	if strings.TrimSpace(expression) == "" {
		return nil, fmt.Errorf("empty expression")
	}

	parser := &ExpressionParser{}
	tokens, err := parser.tokenize(expression)
	if err != nil {
		expressionsLog.Printf("Failed to tokenize expression: %v", err)
		return nil, err
	}
	parser.tokens = tokens
	parser.pos = 0

	result, err := parser.parseOrExpression()
	if err != nil {
		expressionsLog.Printf("Failed to parse expression: %v", err)
		return nil, err
	}

	// Check that all tokens were consumed
	if parser.current().kind != tokenEOF {
		return nil, fmt.Errorf("unexpected token '%s' at position %d", parser.current().value, parser.current().pos)
	}

	expressionsLog.Printf("Successfully parsed expression with %d tokens", len(tokens))
	return result, nil
}

// tokenize breaks the expression string into tokens
func (p *ExpressionParser) tokenize(expression string) ([]token, error) {
	expressionsLog.Printf("Tokenizing expression of length %d", len(expression))
	var tokens []token
	i := 0

	for i < len(expression) {
		// Skip whitespace
		if unicode.IsSpace(rune(expression[i])) {
			i++
			continue
		}

		switch {
		case i+1 < len(expression) && expression[i:i+2] == "&&":
			tokens = append(tokens, token{tokenAnd, "&&", i})
			i += 2
		case i+1 < len(expression) && expression[i:i+2] == "||":
			tokens = append(tokens, token{tokenOr, "||", i})
			i += 2
		case expression[i] == '!' && (i+1 >= len(expression) || expression[i+1] != '='):
			// Only treat ! as NOT if not followed by = (to avoid conflicting with !=)
			tokens = append(tokens, token{tokenNot, "!", i})
			i++
		case expression[i] == '(':
			tokens = append(tokens, token{tokenLeftParen, "(", i})
			i++
		case expression[i] == ')':
			tokens = append(tokens, token{tokenRightParen, ")", i})
			i++
		default:
			// Parse literal expression - everything until we hit a logical operator or paren
			start := i
			parenCount := 0

			for i < len(expression) {
				ch := expression[i]

				// Handle quoted strings - skip everything inside quotes
				// Support single quotes ('), double quotes ("), and backticks (`)
				if ch == '\'' || ch == '"' || ch == '`' {
					quote := ch
					i++ // skip opening quote
					for i < len(expression) {
						if expression[i] == quote {
							i++ // skip closing quote
							break
						}
						if expression[i] == '\\' && i+1 < len(expression) {
							i += 2 // skip escaped character
						} else {
							i++
						}
					}
					continue
				}

				// Track parentheses that are part of the expression (e.g., function calls)
				if ch == '(' {
					parenCount++
					i++
					continue
				} else if ch == ')' {
					if parenCount > 0 {
						parenCount--
						i++
						continue
					} else {
						// This closes our group expression, stop here
						break
					}
				}

				// Check for logical operators when not inside parentheses
				if parenCount == 0 {
					// Check for && or ||
					if i+1 < len(expression) {
						next := expression[i : i+2]
						if next == "&&" || next == "||" {
							break
						}
					}

					// Check for logical NOT that's not part of !=
					if ch == '!' && (i+1 >= len(expression) || expression[i+1] != '=') {
						break
					}
				}

				i++
			}

			literal := strings.TrimSpace(expression[start:i])
			if literal == "" {
				return nil, fmt.Errorf("unexpected empty literal at position %d", start)
			}
			tokens = append(tokens, token{tokenLiteral, literal, start})
		}
	}

	tokens = append(tokens, token{tokenEOF, "", i})
	return tokens, nil
}

// parseOrExpression parses OR expressions (lowest precedence)
func (p *ExpressionParser) parseOrExpression() (ConditionNode, error) {
	left, err := p.parseAndExpression()
	if err != nil {
		return nil, err
	}

	for p.current().kind == tokenOr {
		p.advance() // consume ||
		right, err := p.parseAndExpression()
		if err != nil {
			return nil, err
		}
		left = &OrNode{Left: left, Right: right}
	}

	return left, nil
}

// parseAndExpression parses AND expressions (higher precedence than OR)
func (p *ExpressionParser) parseAndExpression() (ConditionNode, error) {
	left, err := p.parseUnaryExpression()
	if err != nil {
		return nil, err
	}

	for p.current().kind == tokenAnd {
		p.advance() // consume &&
		right, err := p.parseUnaryExpression()
		if err != nil {
			return nil, err
		}
		left = &AndNode{Left: left, Right: right}
	}

	return left, nil
}

// parseUnaryExpression parses NOT expressions and primary expressions
func (p *ExpressionParser) parseUnaryExpression() (ConditionNode, error) {
	if p.current().kind == tokenNot {
		p.advance() // consume !
		operand, err := p.parseUnaryExpression()
		if err != nil {
			return nil, err
		}
		return &NotNode{Child: operand}, nil
	}

	return p.parsePrimaryExpression()
}

// parsePrimaryExpression parses literals and parenthesized expressions
func (p *ExpressionParser) parsePrimaryExpression() (ConditionNode, error) {
	if expressionsLog.Enabled() {
		expressionsLog.Printf("Parsing primary expression at token: %s", p.current().value)
	}
	switch p.current().kind {
	case tokenLeftParen:
		p.advance() // consume (
		expr, err := p.parseOrExpression()
		if err != nil {
			return nil, err
		}
		if p.current().kind != tokenRightParen {
			return nil, fmt.Errorf("expected ')' at position %d", p.current().pos)
		}
		p.advance() // consume )
		return expr, nil

	case tokenLiteral:
		literal := p.current().value
		p.advance()
		return &ExpressionNode{Expression: literal}, nil

	default:
		return nil, fmt.Errorf("unexpected token '%s' at position %d", p.current().value, p.current().pos)
	}
}

// current returns the current token
func (p *ExpressionParser) current() token {
	if p.pos >= len(p.tokens) {
		return token{tokenEOF, "", -1}
	}
	return p.tokens[p.pos]
}

// advance moves to the next token
func (p *ExpressionParser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

// VisitExpressionTree walks through an expression tree and calls the visitor function
// for each ExpressionNode (literal expression) found in the tree
func VisitExpressionTree(node ConditionNode, visitor func(expr *ExpressionNode) error) error {
	if node == nil {
		return nil
	}

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
	default:
		// For other node types (ComparisonNode, PropertyAccessNode, etc.)
		// we don't recurse since they represent complete literal expressions
		return nil
	}

	return nil
}

// BreakLongExpression breaks a long expression into multiple lines at logical points
// such as after || and && operators for better readability
func BreakLongExpression(expression string) []string {
	// If the expression is not too long, return as-is
	if len(expression) <= int(constants.MaxExpressionLineLength) {
		return []string{expression}
	}

	expressionsLog.Printf("Breaking long expression: length=%d", len(expression))

	var lines []string
	current := ""
	i := 0

	for i < len(expression) {
		char := expression[i]

		// Handle quoted strings - don't break inside quotes
		// Support single quotes ('), double quotes ("), and backticks (`)
		if char == '\'' || char == '"' || char == '`' {
			quote := char
			current += string(char)
			i++

			// Continue until closing quote
			for i < len(expression) {
				current += string(expression[i])
				if expression[i] == quote {
					i++
					break
				}
				if expression[i] == '\\' && i+1 < len(expression) {
					i++ // Skip escaped character
					if i < len(expression) {
						current += string(expression[i])
					}
				}
				i++
			}
			continue
		}

		// Look for logical operators as break points
		if i+2 <= len(expression) {
			next2 := expression[i : i+2]
			if next2 == "||" || next2 == "&&" {
				current += next2
				i += 2

				// If the current line is getting long (>ExpressionBreakThreshold chars), break here
				if len(strings.TrimSpace(current)) > int(constants.ExpressionBreakThreshold) {
					lines = append(lines, strings.TrimSpace(current))
					current = ""
					// Skip whitespace after operator
					for i < len(expression) && (expression[i] == ' ' || expression[i] == '\t') {
						i++
					}
					continue
				}
				continue
			}
		}

		current += string(char)
		i++
	}

	// Add the remaining part
	if strings.TrimSpace(current) != "" {
		lines = append(lines, strings.TrimSpace(current))
	}

	// If we still have very long lines, try to break at parentheses
	var finalLines []string
	for _, line := range lines {
		if len(line) > int(constants.MaxExpressionLineLength) {
			subLines := BreakAtParentheses(line)
			finalLines = append(finalLines, subLines...)
		} else {
			finalLines = append(finalLines, line)
		}
	}

	return finalLines
}

// BreakAtParentheses attempts to break long lines at parentheses for function calls
func BreakAtParentheses(expression string) []string {
	if len(expression) <= int(constants.MaxExpressionLineLength) {
		return []string{expression}
	}

	var lines []string
	current := ""
	parenDepth := 0

	for i := 0; i < len(expression); i++ {
		char := expression[i]
		current += string(char)

		switch char {
		case '(':
			parenDepth++
		case ')':
			parenDepth--

			// If we're back to zero depth and the line is getting long, consider a break
			if parenDepth == 0 && len(current) > 80 && i < len(expression)-1 {
				// Look ahead to see if there's a logical operator
				j := i + 1
				for j < len(expression) && (expression[j] == ' ' || expression[j] == '\t') {
					j++
				}

				if j+1 < len(expression) && (expression[j:j+2] == "||" || expression[j:j+2] == "&&") {
					// Add the operator to current line and break
					for k := i + 1; k < j+2; k++ {
						current += string(expression[k])
					}
					lines = append(lines, strings.TrimSpace(current))
					current = ""
					i = j + 2 - 1 // Set to j+2-1 so the loop increment makes i = j+2

					// Skip whitespace after operator
					for i+1 < len(expression) && (expression[i+1] == ' ' || expression[i+1] == '\t') {
						i++
					}
				}
			}
		}
	}

	// Add remaining part
	if strings.TrimSpace(current) != "" {
		lines = append(lines, strings.TrimSpace(current))
	}

	return lines
}

// NormalizeExpressionForComparison normalizes an expression by removing extra spaces and newlines
// This is used for comparing multiline expressions with their single-line equivalents
func NormalizeExpressionForComparison(expression string) string {
	// Replace newlines and tabs with spaces
	normalized := strings.ReplaceAll(expression, "\n", " ")
	normalized = strings.ReplaceAll(normalized, "\t", " ")

	// Replace multiple spaces with single spaces
	for strings.Contains(normalized, "  ") {
		normalized = strings.ReplaceAll(normalized, "  ", " ")
	}

	// Trim leading and trailing spaces
	return strings.TrimSpace(normalized)
}
