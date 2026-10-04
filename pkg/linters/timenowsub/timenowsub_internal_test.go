//go:build !integration

package timenowsub

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func TestTimeNowQualifierParentheses(t *testing.T) {
	// Go rejects parenthesized package qualifiers, so exercise these AST shapes directly.
	for _, name := range []string{"time", "clock"} {
		for depth := range 3 {
			ident := &ast.Ident{Name: name}
			var receiver ast.Expr = ident
			for range depth {
				receiver = &ast.ParenExpr{X: receiver}
			}
			call := &ast.CallExpr{Fun: &ast.SelectorExpr{
				X:   receiver,
				Sel: &ast.Ident{Name: "Now"},
			}}
			pass := &analysis.Pass{TypesInfo: &types.Info{
				Uses: map[*ast.Ident]types.Object{
					ident: types.NewPkgName(token.NoPos, nil, name, types.NewPackage("time", "time")),
				},
			}}
			qualifier, ok := timeNowQualifier(pass, call)
			if !ok || qualifier != name {
				t.Errorf("name=%q depth=%d: got (%q, %v), want (%q, true)", name, depth, qualifier, ok, name)
			}
		}
	}
}
