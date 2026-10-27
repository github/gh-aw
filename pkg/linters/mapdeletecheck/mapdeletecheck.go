// Package mapdeletecheck implements a Go analysis linter that flags
// redundant map membership checks before a delete call such as
// "if _, ok := m[k]; ok { delete(m, k) }" which can be simplified to
// "delete(m, k)" since delete is a no-op for missing keys.
package mapdeletecheck

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:mapdeletecheck")

// Analyzer is the map-delete-check analysis pass.
var Analyzer = analyzerutil.New("mapdeletecheck", "reports redundant map membership checks before delete(m, k) calls since delete is already a no-op for missing keys", run)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.IfStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeIfStmt(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeIfStmt checks whether an if statement is a redundant membership check
// before a delete call and reports a diagnostic if so.
func analyzeIfStmt(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	ifStmt, ok := n.(*ast.IfStmt)
	if !ok {
		return
	}

	pos := pass.Fset.PositionFor(ifStmt.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "mapdeletecheck") {
		return
	}
	if ifStmt.Else != nil {
		return
	}

	mapExpr, keyExpr, okIdent := matchMapIndexAssign(pass, ifStmt.Init)
	if okIdent == nil {
		return
	}

	condIdent, ok := ifStmt.Cond.(*ast.Ident)
	if !ok {
		return
	}
	condObj := pass.TypesInfo.Uses[condIdent]
	okObj := pass.TypesInfo.Defs[okIdent]
	if condObj == nil || okObj == nil || condObj != okObj {
		return
	}

	if !matchDeleteCallStmt(pass, ifStmt.Body, mapExpr, keyExpr) {
		return
	}

	mText := astutil.NodeText(pass.Fset, mapExpr)
	kText := astutil.NodeText(pass.Fset, keyExpr)

	diag := analysis.Diagnostic{
		Pos:     ifStmt.Pos(),
		End:     ifStmt.End(),
		Message: "redundant existence check before delete: delete(" + mText + ", " + kText + ") is already a no-op when the key is absent; remove the if statement",
	}
	if !astutil.HasOverlappingComment(pass.Files, ifStmt.Pos(), ifStmt.End()) {
		diag.SuggestedFixes = []analysis.SuggestedFix{{
			Message: "Replace if-check with plain delete",
			TextEdits: []analysis.TextEdit{{
				Pos:     ifStmt.Pos(),
				End:     ifStmt.End(),
				NewText: []byte("delete(" + mText + ", " + kText + ")"),
			}},
		}}
	}
	pkgLog.Printf("flagging redundant map delete check at %s", pos)
	pass.Report(diag)
}

// matchDeleteCallStmt reports whether body contains exactly one statement of
// the form delete(mapExpr, keyExpr) using the same map and key expressions.
func matchDeleteCallStmt(pass *analysis.Pass, body *ast.BlockStmt, mapExpr, keyExpr ast.Expr) bool {
	if len(body.List) != 1 {
		return false
	}
	exprStmt, ok := body.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	delCall, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	delIdent, ok := delCall.Fun.(*ast.Ident)
	if !ok || delIdent.Name != "delete" {
		return false
	}
	delBuiltin, ok := pass.TypesInfo.Uses[delIdent].(*types.Builtin)
	if !ok || delBuiltin.Name() != "delete" {
		return false
	}
	if len(delCall.Args) != 2 {
		return false
	}
	return sameExpr(pass, delCall.Args[0], mapExpr) && sameExpr(pass, delCall.Args[1], keyExpr)
}

// matchMapIndexAssign returns map expression, key expression, and ok identifier
// if stmt is of the form: _, ok := m[k].
func matchMapIndexAssign(pass *analysis.Pass, stmt ast.Stmt) (mapExpr, keyExpr ast.Expr, okIdent *ast.Ident) {
	if stmt == nil {
		return nil, nil, nil
	}
	a, ok := stmt.(*ast.AssignStmt)
	if !ok || a.Tok.String() != ":=" {
		return nil, nil, nil
	}
	if len(a.Lhs) != 2 || len(a.Rhs) != 1 {
		return nil, nil, nil
	}
	// First lhs must be blank identifier.
	blankIdent, ok := a.Lhs[0].(*ast.Ident)
	if !ok || blankIdent.Name != "_" {
		return nil, nil, nil
	}
	// Second lhs must be an identifier for the ok bool.
	okId, ok := a.Lhs[1].(*ast.Ident)
	if !ok {
		return nil, nil, nil
	}

	// Rhs must be a map index expression.
	idxExpr, ok := a.Rhs[0].(*ast.IndexExpr)
	if !ok {
		return nil, nil, nil
	}

	// Verify the indexed expression is a map type.
	mapType := pass.TypesInfo.TypeOf(idxExpr.X)
	if mapType == nil {
		return nil, nil, nil
	}
	if _, ok := mapType.Underlying().(*types.Map); !ok {
		return nil, nil, nil
	}

	return idxExpr.X, idxExpr.Index, okId
}

// sameExpr reports whether two expressions refer to the same entity.
func sameExpr(pass *analysis.Pass, a, b ast.Expr) bool {
	a = astutil.UnwrapParenExpr(a)
	b = astutil.UnwrapParenExpr(b)
	switch aExpr := a.(type) {
	case *ast.Ident:
		bExpr, ok := b.(*ast.Ident)
		if !ok {
			return false
		}
		aObj := pass.TypesInfo.Uses[aExpr]
		bObj := pass.TypesInfo.Uses[bExpr]
		return aObj != nil && aObj == bObj
	case *ast.BasicLit:
		bExpr, ok := b.(*ast.BasicLit)
		return ok && aExpr.Kind == bExpr.Kind && aExpr.Value == bExpr.Value
	case *ast.SelectorExpr:
		bExpr, ok := b.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		aSelObj := pass.TypesInfo.Uses[aExpr.Sel]
		bSelObj := pass.TypesInfo.Uses[bExpr.Sel]
		return aSelObj != nil && aSelObj == bSelObj && sameExpr(pass, aExpr.X, bExpr.X)
	case *ast.StarExpr:
		bExpr, ok := b.(*ast.StarExpr)
		return ok && sameExpr(pass, aExpr.X, bExpr.X)
	case *ast.IndexExpr:
		bExpr, ok := b.(*ast.IndexExpr)
		return ok && sameExpr(pass, aExpr.X, bExpr.X) && sameExpr(pass, aExpr.Index, bExpr.Index)
	default:
		return false
	}
}
