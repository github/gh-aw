package actionslanguageservice

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func assertExpressionArity(t *testing.T, expression *Expression, kinds map[string]struct{}) {
	t.Helper()
	require.NotNil(t, expression)
	kinds[expression.Kind] = struct{}{}
	switch expression.Kind {
	case "literal", "context", "star":
		require.Empty(t, expression.Children)
	case "group", "unary":
		require.Len(t, expression.Children, 1)
	case "index", "binary":
		require.Len(t, expression.Children, 2)
	case "logical":
		require.GreaterOrEqual(t, len(expression.Children), 2)
	case "call":
		name := strings.ToLower(expression.Token.Text)
		arity, ok := builtins[name]
		if !ok {
			switch name {
			case "always", "cancelled":
				arity = function{0, 0}
			case "failure", "success":
				arity = function{0, int(^uint(0) >> 1)}
			case "hashfiles":
				arity = function{1, 255}
			default:
				t.Fatalf("unknown function %q", name)
			}
		}
		require.GreaterOrEqual(t, len(expression.Children), arity.min)
		require.LessOrEqual(t, len(expression.Children), arity.max)
		if name == "case" {
			require.Equal(t, 1, len(expression.Children)%2)
		}
	default:
		t.Fatalf("unknown expression kind %q", expression.Kind)
	}
	for i, child := range expression.Children {
		require.Same(t, child, expression.Child(i))
		assertExpressionArity(t, child, kinds)
	}
}

func TestExpressionArityInvariants(t *testing.T) {
	kinds := make(map[string]struct{})
	for _, test := range []struct {
		source string
		kind   string
		arity  int
	}{
		{"null", "literal", 0},
		{"true", "literal", 0},
		{"42", "literal", 0},
		{"'text'", "literal", 0},
		{"github", "context", 0},
		{"(github)", "group", 1},
		{"!!true", "unary", 1},
		{"github.ref", "index", 2},
		{"github['ref']", "index", 2},
		{"github[vars.key]", "index", 2},
		{"github.*.name", "index", 2},
		{"github[*]", "index", 2},
		{"(github).ref", "index", 2},
		{"fromJSON('{}').value", "index", 2},
		{"true == false", "binary", 2},
		{"true != false", "binary", 2},
		{"1 < 2", "binary", 2},
		{"1 <= 2", "binary", 2},
		{"2 > 1", "binary", 2},
		{"2 >= 1", "binary", 2},
		{"true && false", "logical", 2},
		{"true || false", "logical", 2},
		{"true && false && true", "logical", 3},
		{"true || false || true", "logical", 3},
		{"true && false || true", "logical", 2},
		{"always()", "call", 0},
		{"format('text')", "call", 1},
		{"contains(github.ref, 'main')", "call", 2},
		{"format('{0}{1}', github.ref, env.VALUE)", "call", 3},
		{"case(true, 1, false, 2, 0)", "call", 5},
	} {
		t.Run(test.source, func(t *testing.T) {
			expression, err := ParseExpression(test.source, syntaxContexts)
			require.NoError(t, err)
			require.NotNil(t, expression)
			require.Equal(t, test.kind, expression.Kind)
			require.Len(t, expression.Children, test.arity)
			assertExpressionArity(t, expression, kinds)
		})
	}
	for _, test := range conformanceCases(t) {
		if test.Kind != "expression" || !test.Accepted {
			continue
		}
		t.Run("conformance/"+test.Name, func(t *testing.T) {
			expression, err := ParseExpression(test.Source, test.Contexts)
			require.NoError(t, err)
			if expression != nil {
				assertExpressionArity(t, expression, kinds)
			}
		})
	}
	require.Equal(t, map[string]struct{}{
		"literal": {}, "context": {}, "star": {}, "group": {},
		"unary": {}, "index": {}, "binary": {}, "logical": {}, "call": {},
	}, kinds)
}

func TestExpressionChildPrecondition(t *testing.T) {
	expression, err := ParseSyntax("github.ref")
	require.NoError(t, err)
	for _, index := range []int{-1, len(expression.Children)} {
		require.PanicsWithValue(t, "invalid Actions expression AST operand", func() {
			expression.Child(index)
		})
	}
}

func TestExpressionParserCursorInvariants(t *testing.T) {
	tokens, err := lex("true")
	require.NoError(t, err)
	parser := expressionParser{tokens: tokens}
	require.Equal(t, "true", parser.next().Kind)
	require.Equal(t, "true", parser.previous().Kind)
	require.Equal(t, "eof", parser.next().Kind)
	require.Equal(t, "eof", parser.next().Kind)
	require.Equal(t, len(tokens)-1, parser.pos)

	for _, pos := range []int{-1, len(tokens)} {
		parser := expressionParser{tokens: tokens, pos: pos}
		expression, err := parser.parse()
		require.Nil(t, expression)
		require.ErrorContains(t, err, "invalid Actions expression token cursor")
	}
	for _, pos := range []int{0, len(tokens) + 1} {
		parser := expressionParser{tokens: tokens, pos: pos}
		require.Equal(t, "eof", parser.previous().Kind)
		expression, err := parser.parse()
		require.Nil(t, expression)
		require.ErrorContains(t, err, "invalid Actions expression previous token")
	}
	expression, err := (&expressionParser{}).parse()
	require.Nil(t, expression)
	require.ErrorContains(t, err, "invalid Actions expression token cursor")
}
