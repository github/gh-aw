package astutil

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/analysis"
)

func TestPackageFunctionAliasInitializer(t *testing.T) {
	t.Parallel()

	target := types.NewVar(token.NoPos, nil, "alias", types.Typ[types.Int])
	other := types.NewVar(token.NoPos, nil, "other", types.Typ[types.Int])
	first := ast.NewIdent("first")
	second := ast.NewIdent("別名")
	unmatched := ast.NewIdent("unmatched")
	firstValue := ast.NewIdent("firstFunction")
	secondValue := ast.NewIdent("secondFunction")
	multipleValues := &ast.CallExpr{Fun: ast.NewIdent("functions")}
	definitions := map[*ast.Ident]types.Object{
		first:  other,
		second: target,
	}
	assignmentUses := map[*ast.Ident]types.Object{
		first:  other,
		second: target,
	}

	tests := []struct {
		name string
		node ast.Node
		defs map[*ast.Ident]types.Object
		uses map[*ast.Ident]types.Object
		obj  types.Object
		want ast.Expr
	}{
		{name: "nil node has no initializer"},
		{name: "unrelated node has no initializer", node: firstValue},
		{name: "empty declaration has no initializer", node: &ast.ValueSpec{}},
		{
			name: "declaration returns first matching initializer",
			node: &ast.ValueSpec{Names: []*ast.Ident{first, second}, Values: []ast.Expr{firstValue, secondValue}},
			defs: definitions,
			obj:  other,
			want: firstValue,
		},
		{
			name: "declaration returns later Unicode named initializer",
			node: &ast.ValueSpec{Names: []*ast.Ident{first, second}, Values: []ast.Expr{firstValue, secondValue}},
			defs: definitions,
			obj:  target,
			want: secondValue,
		},
		{
			name: "declaration without matching object has no initializer",
			node: &ast.ValueSpec{Names: []*ast.Ident{first, unmatched}, Values: []ast.Expr{firstValue, secondValue}},
			defs: definitions,
			obj:  target,
		},
		{
			name: "declaration without values has no initializer",
			node: &ast.ValueSpec{Names: []*ast.Ident{second}},
			defs: definitions,
			obj:  target,
		},
		{
			name: "declaration cannot index a later tuple result",
			node: &ast.ValueSpec{Names: []*ast.Ident{first, second}, Values: []ast.Expr{multipleValues}},
			defs: definitions,
			obj:  target,
		},
		{
			name: "declaration returns tuple expression for first name",
			node: &ast.ValueSpec{Names: []*ast.Ident{first, second}, Values: []ast.Expr{multipleValues}},
			defs: definitions,
			obj:  other,
			want: multipleValues,
		},
		{name: "empty assignment has no initializer", node: &ast.AssignStmt{}},
		{
			name: "assignment returns first matching initializer",
			node: &ast.AssignStmt{Lhs: []ast.Expr{second, first}, Rhs: []ast.Expr{firstValue, secondValue}},
			uses: assignmentUses,
			obj:  target,
			want: firstValue,
		},
		{
			name: "short declaration returns defined initializer",
			node: &ast.AssignStmt{Lhs: []ast.Expr{second}, Rhs: []ast.Expr{secondValue}},
			defs: definitions,
			obj:  target,
			want: secondValue,
		},
		{
			name: "assignment skips unrelated identifiers",
			node: &ast.AssignStmt{Lhs: []ast.Expr{first, second}, Rhs: []ast.Expr{firstValue, secondValue}},
			uses: assignmentUses,
			obj:  target,
			want: secondValue,
		},
		{
			name: "assignment skips non identifier targets and preserves position",
			node: &ast.AssignStmt{
				Lhs: []ast.Expr{&ast.SelectorExpr{X: first, Sel: second}, second},
				Rhs: []ast.Expr{firstValue, secondValue},
			},
			uses: assignmentUses,
			obj:  target,
			want: secondValue,
		},
		{
			name: "assignment with only non identifier targets has no initializer",
			node: &ast.AssignStmt{Lhs: []ast.Expr{nil, &ast.ParenExpr{X: second}}, Rhs: []ast.Expr{firstValue, secondValue}},
			obj:  target,
		},
		{
			name: "assignment without matching object has no initializer",
			node: &ast.AssignStmt{Lhs: []ast.Expr{first, unmatched}, Rhs: []ast.Expr{firstValue, secondValue}},
			uses: assignmentUses,
			obj:  target,
		},
		{
			name: "assignment without values has no initializer",
			node: &ast.AssignStmt{Lhs: []ast.Expr{second}},
			uses: assignmentUses,
			obj:  target,
		},
		{
			name: "assignment cannot index a later tuple result",
			node: &ast.AssignStmt{Lhs: []ast.Expr{first, second}, Rhs: []ast.Expr{multipleValues}},
			uses: assignmentUses,
			obj:  target,
		},
		{
			name: "assignment returns tuple expression for first target",
			node: &ast.AssignStmt{Lhs: []ast.Expr{second, first}, Rhs: []ast.Expr{multipleValues}},
			uses: assignmentUses,
			obj:  target,
			want: multipleValues,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			defs := maps.Clone(tt.defs)
			uses := maps.Clone(tt.uses)
			pass := &analysis.Pass{TypesInfo: &types.Info{Defs: defs, Uses: uses}}

			got := packageFunctionAliasInitializer(pass, tt.node, tt.obj)

			assert.Equal(t, tt.want, got)
			if tt.want != nil {
				require.NotNil(t, got)
				assert.Same(t, tt.want, got)
			}
			assert.Equal(t, tt.defs, defs, "initializer lookup must not change definitions")
			assert.Equal(t, tt.uses, uses, "initializer lookup must not change uses")
		})
	}
}
