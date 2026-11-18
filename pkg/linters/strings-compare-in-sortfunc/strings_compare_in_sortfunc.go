// Package stringscompareinsortfunc implements an analyzer for strings.Compare
// used as the sole return expression of a slices comparator function literal.
package stringscompareinsortfunc

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer reports strings.Compare used as a slices comparator result.
var Analyzer = analyzerutil.NewAtPath(
	"stringscompareinsortfunc",
	"reports strings.Compare(a, b) used as the comparator passed to slices.SortFunc, slices.SortStableFunc, slices.MinFunc, slices.MaxFunc, or slices.BinarySearchFunc, which should use the generic cmp.Compare(a, b) instead",
	"strings-compare-in-sortfunc",
	run,
)

// cmpArgIndex maps slices function names to the index of their comparator argument.
var cmpArgIndex = map[string]int{
	"SortFunc":         1,
	"SortStableFunc":   1,
	"MinFunc":          1,
	"MaxFunc":          1,
	"BinarySearchFunc": 2,
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		pkgPath, name, ok := astutil.PackageCall(pass, call)
		if !ok || pkgPath != "slices" {
			return
		}
		idx, ok := cmpArgIndex[name]
		if !ok {
			return
		}
		if idx >= len(call.Args) {
			return
		}
		lit, ok := astutil.UnwrapParenExpr(call.Args[idx]).(*ast.FuncLit)
		if !ok || lit.Body == nil || len(lit.Body.List) != 1 {
			return
		}
		ret, ok := lit.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return
		}
		inner, ok := astutil.UnwrapParenExpr(ret.Results[0]).(*ast.CallExpr)
		if !ok {
			return
		}
		innerPkg, innerName, ok := astutil.PackageCall(pass, inner)
		if !ok || innerPkg != "strings" || innerName != "Compare" {
			return
		}
		pos := pass.Fset.PositionFor(inner.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "stringscompareinsortfunc") {
			return
		}
		pass.Reportf(inner.Pos(), "use cmp.Compare instead of strings.Compare in sort comparator")
	})
}
