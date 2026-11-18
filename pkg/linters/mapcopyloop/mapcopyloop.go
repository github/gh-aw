// Package mapcopyloop implements a Go analysis linter that flags
// range-over-map loops that copy each entry into another map.
package mapcopyloop

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the map-copy-loop analysis pass.
var Analyzer = analyzerutil.New("mapcopyloop", "reports range-over-map loops that copy entries into another map and can be replaced with maps.Copy", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.RangeStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeRangeStmt(pass, n, generatedFiles, noLintIndex)
	})
}

func analyzeRangeStmt(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	rangeStmt, ok := n.(*ast.RangeStmt)
	if !ok {
		return
	}

	pos := pass.Fset.PositionFor(rangeStmt.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) ||
		nolint.HasDirectiveForLinter(pos, noLintIndex, "mapcopyloop") {
		return
	}

	srcType := pass.TypesInfo.TypeOf(rangeStmt.X)
	if !isMap(srcType) {
		return
	}

	keyObj, valueObj, ok := rangeObjects(pass, rangeStmt)
	if !ok {
		return
	}
	indexExpr, ok := matchCopyBody(pass, rangeStmt, keyObj, valueObj)
	if !ok {
		return
	}
	if sameObject(pass, indexExpr.X, rangeStmt.X) {
		return
	}
	dstType := pass.TypesInfo.TypeOf(indexExpr.X)
	if !isMap(dstType) || !types.Identical(dstType, srcType) {
		return
	}
	srcText := astutil.NodeText(pass.Fset, rangeStmt.X)
	dstText := astutil.NodeText(pass.Fset, indexExpr.X)
	if srcText == "" || dstText == "" {
		return
	}

	diag := analysis.Diagnostic{
		Pos:     rangeStmt.Pos(),
		End:     rangeStmt.End(),
		Message: "range-copy loop can be replaced with maps.Copy(" + dstText + ", " + srcText + ")",
	}
	if mapsPackageVisible(pass.Pkg, rangeStmt.Pos()) &&
		!astutil.HasOverlappingComment(pass.Files, rangeStmt.Pos(), rangeStmt.End()) {
		diag.SuggestedFixes = []analysis.SuggestedFix{{
			Message: "Replace range-copy loop with maps.Copy",
			TextEdits: []analysis.TextEdit{{
				Pos:     rangeStmt.Pos(),
				End:     rangeStmt.End(),
				NewText: []byte("maps.Copy(" + dstText + ", " + srcText + ")"),
			}},
		}}
	}
	pass.Report(diag)
}

func isMap(typ types.Type) bool {
	if typ == nil {
		return false
	}
	_, ok := typ.Underlying().(*types.Map)
	return ok
}

func rangeObjects(pass *analysis.Pass, rangeStmt *ast.RangeStmt) (types.Object, types.Object, bool) {
	keyIdent, keyOK := rangeStmt.Key.(*ast.Ident)
	valueIdent, valueOK := rangeStmt.Value.(*ast.Ident)
	if !keyOK || !valueOK || keyIdent.Name == "_" || valueIdent.Name == "_" {
		return nil, nil, false
	}

	keyObj := pass.TypesInfo.ObjectOf(keyIdent)
	valueObj := pass.TypesInfo.ObjectOf(valueIdent)
	if keyObj == nil || valueObj == nil {
		return nil, nil, false
	}
	return keyObj, valueObj, true
}

func matchCopyBody(pass *analysis.Pass, rangeStmt *ast.RangeStmt, keyObj, valueObj types.Object) (*ast.IndexExpr, bool) {
	if len(rangeStmt.Body.List) != 1 {
		return nil, false
	}
	var bodyStmt ast.Stmt
	for _, stmt := range rangeStmt.Body.List {
		bodyStmt = stmt
	}
	assignStmt, ok := bodyStmt.(*ast.AssignStmt)
	if !ok || assignStmt.Tok != token.ASSIGN || len(assignStmt.Lhs) != 1 || len(assignStmt.Rhs) != 1 {
		return nil, false
	}
	var lhs, rhs ast.Expr
	for _, expr := range assignStmt.Lhs {
		lhs = expr
	}
	for _, expr := range assignStmt.Rhs {
		rhs = expr
	}
	indexExpr, ok := lhs.(*ast.IndexExpr)
	if !ok {
		return nil, false
	}
	keyIdent, ok := indexExpr.Index.(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(keyIdent) != keyObj {
		return nil, false
	}
	valueIdent, ok := rhs.(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(valueIdent) != valueObj {
		return nil, false
	}
	return indexExpr, true
}

func mapsPackageVisible(pkg *types.Package, pos token.Pos) bool {
	if pkg == nil {
		return false
	}
	scope := pkg.Scope().Innermost(pos)
	if scope == nil {
		return false
	}
	_, obj := scope.LookupParent("maps", pos)
	pkgName, ok := obj.(*types.PkgName)
	return ok && pkgName.Imported().Path() == "maps"
}

func sameObject(pass *analysis.Pass, expr, ref ast.Expr) bool {
	switch reference := ref.(type) {
	case *ast.Ident:
		ident, ok := expr.(*ast.Ident)
		return ok && pass.TypesInfo.ObjectOf(ident) == pass.TypesInfo.ObjectOf(reference)
	case *ast.SelectorExpr:
		selector, ok := expr.(*ast.SelectorExpr)
		return ok &&
			pass.TypesInfo.ObjectOf(selector.Sel) == pass.TypesInfo.ObjectOf(reference.Sel) &&
			sameObject(pass, selector.X, reference.X)
	default:
		return false
	}
}
