package actionslanguageservice

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Expression is the syntactic Actions expression tree. Kind is literal,
// context, call, group, index, unary, binary, logical, or star.
type Expression struct {
	Kind     string
	Token    Token
	Children []*Expression
	Start    int
	End      int
}

// Child retrieves a required operand of a parser-created node. A missing operand
// is an AST construction error, not a recoverable expression syntax error.
func (e *Expression) Child(index int) *Expression {
	if index >= 0 && index < len(e.Children) {
		return e.Children[index]
	}
	panic("invalid Actions expression AST operand")
}

type function struct{ min, max int }

var syntaxContexts = []string{
	"github", "env", "vars", "job", "jobs", "steps", "runner", "secrets",
	"strategy", "matrix", "needs", "inputs",
	"always(0,0)", "cancelled(0,0)", "failure(0,MAX)", "success(0,MAX)", "hashFiles(1,255)",
}

// ValidateExpression validates unwrapped expression syntax outside a specific
// workflow field. ParseWorkflow applies the schema's narrower context rules.
func ValidateExpression(source string) error {
	result, err := ParseExpression(source, syntaxContexts)
	if err != nil {
		return err
	}
	if result == nil {
		return errors.New("an expression was expected")
	}
	return nil
}

var builtins = map[string]function{
	"contains": {2, 2}, "startswith": {2, 2}, "endswith": {2, 2},
	"format": {1, 255}, "join": {1, 2}, "tojson": {1, 1}, "fromjson": {1, 1},
	"case": {3, int(^uint(0) >> 1)},
}

type expressionParser struct {
	source     string
	tokens     []Token
	pos        int
	depth      int
	contexts   map[string]bool
	functions  map[string]function
	syntaxOnly bool
}

// ParseSyntax uses the official grammar without restricting named contexts.
// Field-specific parsing should use ParseExpression with the schema's context.
func ParseSyntax(source string) (*Expression, error) {
	return parseExpression(source, syntaxContexts, true)
}

// ParseExpression implements the upstream expression grammar. allowedContext
// uses the workflow schema's named values and function(min,max) declarations.
// Built-in functions are always available; status and hashFiles are extensions.
func ParseExpression(source string, allowedContext []string) (*Expression, error) {
	return parseExpression(source, allowedContext, false)
}

func parseExpression(source string, allowedContext []string, syntaxOnly bool) (*Expression, error) {
	tokens, err := lex(source)
	if err != nil {
		return nil, err
	}
	p := expressionParser{source: source, tokens: tokens, contexts: make(map[string]bool), functions: make(map[string]function), syntaxOnly: syntaxOnly}
	for _, item := range allowedContext {
		name, args, isFunction := strings.Cut(item, "(")
		if !isFunction {
			p.contexts[strings.ToLower(item)] = true
			continue
		}
		minText, maxText, ok := strings.Cut(strings.TrimSuffix(args, ")"), ",")
		minimum, minErr := strconv.Atoi(minText)
		maximum, maxErr := strconv.Atoi(maxText)
		if maxText == "MAX" {
			maximum, maxErr = int(^uint(0)>>1), nil
		}
		if !ok || minErr != nil || maxErr != nil || !strings.HasSuffix(args, ")") {
			return nil, fmt.Errorf("invalid function context %q", item)
		}
		p.functions[strings.ToLower(name)] = function{minimum, maximum}
	}
	if p.peek().Kind == "eof" {
		return nil, nil
	}
	result, err := p.expression()
	if err != nil {
		return nil, err
	}
	if p.peek().Kind != "eof" {
		return nil, p.error("unexpected symbol", p.peek())
	}
	return result, nil
}

func (p *expressionParser) expression() (*Expression, error) {
	if err := p.pushDepth(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	return p.binary(0)
}

var precedence = [][]string{{"||"}, {"&&"}, {"==", "!="}, {">", ">=", "<", "<="}}

func (p *expressionParser) binary(level int) (*Expression, error) {
	if level >= len(precedence) {
		return p.unary()
	}
	operators := precedence[level] //nolint:uncheckedsliceindex // binary only recurses from level zero after the upper-bound check.
	left, err := p.binary(level + 1)
	if err != nil {
		return nil, err
	}
	logical := level < 2
	if !p.matches(operators...) {
		return left, nil
	}
	if logical {
		if err := p.pushDepth(); err != nil {
			return nil, err
		}
		defer func() { p.depth-- }()
	}
	for p.matches(operators...) {
		operator := p.next()
		right, err := p.binary(level + 1)
		if err != nil {
			return nil, err
		}
		kind := "binary"
		if logical {
			kind = "logical"
		}
		if logical && left.Kind == "logical" && left.Token.Kind == operator.Kind {
			left.Children = append(left.Children, right)
			left.End = right.End
		} else {
			left = &Expression{Kind: kind, Token: operator, Children: []*Expression{left, right}, Start: left.Start, End: right.End}
		}
	}
	return left, nil
}

func (p *expressionParser) unary() (*Expression, error) {
	if p.peek().Kind != "!" {
		return p.index()
	}
	operator := p.next()
	if err := p.pushDepth(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	value, err := p.unary()
	if err != nil {
		return nil, err
	}
	return &Expression{Kind: "unary", Token: operator, Children: []*Expression{value}, Start: operator.Offset, End: value.End}, nil
}

func (p *expressionParser) index() (*Expression, error) {
	value, err := p.primary()
	if err != nil {
		return nil, err
	}
	if value.Kind != "context" && value.Kind != "call" && value.Kind != "group" {
		return value, nil
	}
	depth := p.depth
	defer func() { p.depth = depth }()
	for p.matches("[", ".") {
		operator := p.next()
		var key *Expression
		if operator.Kind == "[" {
			if p.peek().Kind == "*" {
				key = tokenExpression("star", p.next())
			} else {
				key, err = p.expression()
				if err != nil {
					return nil, err
				}
			}
			if err := p.consume("]"); err != nil {
				return nil, err
			}
		} else {
			if !p.matches("identifier", "*") {
				return nil, p.error("unexpected symbol", p.peek())
			}
			token := p.next()
			kind := "literal"
			if token.Kind == "*" {
				kind = "star"
			} else {
				token.Value = token.Text
			}
			key = tokenExpression(kind, token)
		}
		if err := p.pushDepth(); err != nil {
			return nil, err
		}
		last := p.previous()
		value = &Expression{Kind: "index", Token: operator, Children: []*Expression{value, key}, Start: value.Start, End: last.Offset + len(last.Text)}
	}
	return value, nil
}

func (p *expressionParser) primary() (*Expression, error) {
	token := p.next()
	switch token.Kind {
	case "true", "false", "null", "number", "string":
		return tokenExpression("literal", token), nil
	case "(":
		value, err := p.expression()
		if err != nil {
			return nil, err
		}
		if err := p.consume(")"); err != nil {
			return nil, err
		}
		last := p.previous()
		return &Expression{Kind: "group", Token: token, Children: []*Expression{value}, Start: token.Offset, End: last.Offset + len(last.Text)}, nil
	case "identifier":
		name := strings.ToLower(token.Text)
		if p.peek().Kind != "(" {
			if !p.syntaxOnly && !p.contexts[name] {
				return nil, p.error("unrecognized named-value", token)
			}
			return tokenExpression("context", token), nil
		}
		return p.call(token)
	default:
		return nil, p.error("unexpected symbol", token)
	}
}

func (p *expressionParser) call(token Token) (*Expression, error) {
	p.next() // (
	var args []*Expression
	for p.peek().Kind != ")" {
		arg, err := p.expression()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.peek().Kind != ")" {
			if err := p.consume(","); err != nil {
				return nil, err
			}
		}
	}
	p.next()
	name := strings.ToLower(token.Text)
	f, ok := builtins[name]
	if !ok {
		f, ok = p.functions[name]
	}
	if !ok {
		return nil, p.error("unrecognized function", token)
	}
	if len(args) < f.min {
		return nil, p.error("too few parameters supplied", token)
	}
	if len(args) > f.max {
		return nil, p.error("too many parameters supplied", token)
	}
	if strings.EqualFold(token.Text, "case") && len(args)%2 == 0 {
		return nil, p.error("case requires an odd number of parameters", token)
	}
	last := p.previous()
	return &Expression{Kind: "call", Token: token, Children: args, Start: token.Offset, End: last.Offset + len(last.Text)}, nil
}

func tokenExpression(kind string, token Token) *Expression {
	return &Expression{Kind: kind, Token: token, Start: token.Offset, End: token.Offset + len(token.Text)}
}
func (p *expressionParser) peek() Token {
	if p.pos >= 0 && p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	panic("invalid Actions expression token cursor")
}

func (p *expressionParser) previous() Token {
	index := p.pos - 1
	if index >= 0 && index < len(p.tokens) {
		return p.tokens[index]
	}
	panic("invalid Actions expression previous token")
}

func (p *expressionParser) next() Token {
	token := p.peek()
	if token.Kind != "eof" {
		p.pos++
	}
	return token
}

func (p *expressionParser) matches(kinds ...string) bool {
	for _, kind := range kinds {
		if p.peek().Kind == kind {
			return true
		}
	}
	return false
}

func (p *expressionParser) consume(kind string) error {
	if p.peek().Kind != kind {
		return p.error("unexpected symbol", p.peek())
	}
	p.next()
	return nil
}

func (p *expressionParser) pushDepth() error {
	p.depth++
	if p.depth > MaxExpressionDepth {
		return p.error("exceeded max expression depth 50", p.peek())
	}
	return nil
}

func (p *expressionParser) error(message string, token Token) error {
	if token.Kind == "eof" {
		message = "unexpected end of expression"
	}
	return fmt.Errorf("%s: %q at position %d in expression: %s", message, token.Text, token.Offset+1, p.source)
}
