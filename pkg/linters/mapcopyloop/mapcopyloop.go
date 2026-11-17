// Package mapcopyloop implements a Go analysis linter that flags
// range-over-map loops whose only body statement copies each entry into
// another map (dst[k] = v), which can be replaced with maps.Copy(dst, src).
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
var Analyzer = analyzerutil.New("mapcopyloop", "reports range-over-map loops whose body only copies each entry into another map and can be replaced with maps.Copy(dst, src)", run)

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
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "mapcopyloop") {
		return
	}

	if !isMap(pass.TypesInfo.TypeOf(rangeStmt.X)) {
		return
	}

	keyObj := rangeVarObj(pass, rangeStmt.Key)
	valObj := rangeVarObj(pass, rangeStmt.Value)
	if keyObj == nil || valObj == nil {
		return
	}

	dst, ok := matchCopyBody(pass, rangeStmt, keyObj, valObj)
	if !ok {
		return
	}

	srcText := astutil.NodeText(pass.Fset, rangeStmt.X)
	dstText := astutil.NodeText(pass.Fset, dst)
	if srcText == "" || dstText == "" || srcText == dstText {
		return
	}

	diag := analysis.Diagnostic{
		Pos:     rangeStmt.Pos(),
		End:     rangeStmt.End(),
		Message: "range-copy loop over map can be replaced with maps.Copy(" + dstText + ", " + srcText + ")",
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

func isMap(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Map)
	return ok
}

// rangeVarObj returns the object for a non-blank identifier range variable.
func rangeVarObj(pass *analysis.Pass, e ast.Expr) types.Object {
	id, ok := e.(*ast.Ident)
	if !ok || id.Name == "_" {
		return nil
	}
	if obj := pass.TypesInfo.Defs[id]; obj != nil {
		return obj
	}
	return pass.TypesInfo.Uses[id]
}

// matchCopyBody returns the destination map expression when the loop body is
// exactly `dst[k] = v` with k and v being the range key and value.
func matchCopyBody(pass *analysis.Pass, rangeStmt *ast.RangeStmt, keyObj, valObj types.Object) (ast.Expr, bool) {
	if len(rangeStmt.Body.List) != 1 {
		return nil, false
	}
	assign, ok := rangeStmt.Body.List[0].(*ast.AssignStmt)
	if !ok || assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil, false
	}
	idx, ok := assign.Lhs[0].(*ast.IndexExpr)
	if !ok || !isMap(pass.TypesInfo.TypeOf(idx.X)) {
		return nil, false
	}
	idxIdent, ok := idx.Index.(*ast.Ident)
	if !ok || pass.TypesInfo.Uses[idxIdent] != keyObj {
		return nil, false
	}
	rhs, ok := assign.Rhs[0].(*ast.Ident)
	if !ok || pass.TypesInfo.Uses[rhs] != valObj {
		return nil, false
	}
	return idx.X, true
}

// mapsPackageVisible reports whether `maps` resolves to the standard maps
// package at pos (imported and not shadowed).
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
