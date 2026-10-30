package astutil

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTwoValueTypeAssertionOKIdent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statement  string
		wantName   string
		wantOK     bool
		alterInput func(*ast.TypeAssertExpr, map[ast.Node]ast.Node) (*ast.TypeAssertExpr, map[ast.Node]ast.Node)
	}{
		{
			name:      "short declaration returns the status identifier",
			statement: "value, ok := input.(int)",
			wantName:  "ok",
			wantOK:    true,
		},
		{
			name:      "assignment returns the status identifier",
			statement: "value, ok = input.(int)",
			wantName:  "ok",
			wantOK:    true,
		},
		{
			name:      "discarded status remains identifiable",
			statement: "value, _ := input.(int)",
			wantName:  "_",
			wantOK:    true,
		},
		{
			name:      "unicode status identifiers are preserved",
			statement: "value, 成功 := input.(int)",
			wantName:  "成功",
			wantOK:    true,
		},
		{
			name:      "nested assignment parentheses are transparent",
			statement: "value, ok := (((input.(int))))",
			wantName:  "ok",
			wantOK:    true,
		},
		{
			name:      "variable declaration returns the status identifier",
			statement: "var value, ok = input.(int)",
			wantName:  "ok",
			wantOK:    true,
		},
		{
			name:      "variable declaration preserves discarded status",
			statement: "var value, _ = input.(int)",
			wantName:  "_",
			wantOK:    true,
		},
		{
			name:      "nested declaration parentheses are transparent",
			statement: "var value, 成功 = ((input.(int)))",
			wantName:  "成功",
			wantOK:    true,
		},
		{
			name:      "single target assignment is rejected",
			statement: "value := input.(int)",
		},
		{
			name:      "three target assignment is rejected",
			statement: "first, second, third := input.(int)",
		},
		{
			name:      "multiple assignment values are rejected",
			statement: "value, ok := input.(int), true",
		},
		{
			name:      "selector status target is rejected",
			statement: "value, result.ok = input.(int)",
		},
		{
			name:      "single name declaration is rejected",
			statement: "var value = input.(int)",
		},
		{
			name:      "three name declaration is rejected",
			statement: "var first, second, third = input.(int)",
		},
		{
			name:      "multiple declaration values are rejected",
			statement: "var value, ok = input.(int), true",
		},
		{
			name:      "assertions inside a call are not two value assignments",
			statement: "value, ok := consume(input.(int))",
		},
		{
			name:      "missing parent map is rejected",
			statement: "value, ok := input.(int)",
			alterInput: func(expr *ast.TypeAssertExpr, _ map[ast.Node]ast.Node) (*ast.TypeAssertExpr, map[ast.Node]ast.Node) {
				return expr, nil
			},
		},
		{
			name:      "empty parent map is rejected",
			statement: "value, ok := input.(int)",
			alterInput: func(expr *ast.TypeAssertExpr, _ map[ast.Node]ast.Node) (*ast.TypeAssertExpr, map[ast.Node]ast.Node) {
				return expr, map[ast.Node]ast.Node{}
			},
		},
		{
			name:      "incomplete parenthesis chain is rejected",
			statement: "value, ok := (input.(int))",
			alterInput: func(expr *ast.TypeAssertExpr, parents map[ast.Node]ast.Node) (*ast.TypeAssertExpr, map[ast.Node]ast.Node) {
				delete(parents, parents[expr])
				return expr, parents
			},
		},
		{
			name:      "assignment with no values is rejected",
			statement: "value, ok := input.(int)",
			alterInput: func(expr *ast.TypeAssertExpr, parents map[ast.Node]ast.Node) (*ast.TypeAssertExpr, map[ast.Node]ast.Node) {
				parents[expr] = &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("value"), ast.NewIdent("ok")}}
				return expr, parents
			},
		},
		{
			name:      "declaration with no values is rejected",
			statement: "var value, ok = input.(int)",
			alterInput: func(expr *ast.TypeAssertExpr, parents map[ast.Node]ast.Node) (*ast.TypeAssertExpr, map[ast.Node]ast.Node) {
				parents[expr] = &ast.ValueSpec{Names: []*ast.Ident{ast.NewIdent("value"), ast.NewIdent("ok")}}
				return expr, parents
			},
		},
		{
			name:      "nil assertion with no parents is rejected",
			statement: "value, ok := input.(int)",
			alterInput: func(_ *ast.TypeAssertExpr, _ map[ast.Node]ast.Node) (*ast.TypeAssertExpr, map[ast.Node]ast.Node) {
				return nil, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file, err := parser.ParseFile(token.NewFileSet(), "", "package example\nfunc example() {\n"+tt.statement+"\n}", 0)
			require.NoError(t, err)

			parents := make(map[ast.Node]ast.Node)
			var stack []ast.Node
			var typeAssert *ast.TypeAssertExpr
			var wantIdent *ast.Ident
			ast.Inspect(file, func(node ast.Node) bool {
				if node == nil {
					stack = stack[:len(stack)-1]
					return false
				}
				if len(stack) > 0 {
					parents[node] = stack[len(stack)-1]
				}
				stack = append(stack, node)
				if expr, ok := node.(*ast.TypeAssertExpr); ok {
					typeAssert = expr
				}
				if ident, ok := node.(*ast.Ident); ok && ident.Name == tt.wantName {
					wantIdent = ident
				}
				return true
			})
			require.NotNil(t, typeAssert)
			if tt.wantOK {
				require.NotNil(t, wantIdent)
			}
			if tt.alterInput != nil {
				typeAssert, parents = tt.alterInput(typeAssert, parents)
			}

			gotIdent, gotOK := TwoValueTypeAssertionOKIdent(typeAssert, parents)

			assert.Equal(t, tt.wantOK, gotOK)
			assert.Equal(t, wantIdent, gotIdent)
			if tt.wantOK {
				assert.Same(t, wantIdent, gotIdent)
			}
		})
	}
}
