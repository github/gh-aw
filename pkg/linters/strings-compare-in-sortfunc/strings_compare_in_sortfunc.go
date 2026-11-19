// Package stringscompareinsortfunc reports strings.Compare in simple slices comparators.
package stringscompareinsortfunc

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const name = "stringscompareinsortfunc"

// Analyzer reports comparators that can use cmp.Compare.
var Analyzer = analyzerutil.New(name, "reports strings.Compare in simple slices comparators; use cmp.Compare instead", run)

func run(pass *analysis.Pass) (any, error) {
	directives, generated, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	return analyzerutil.Preorder(pass, []ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		funcName := calledFunc(pass, call.Fun, "slices")
		index := 1
		switch funcName {
		case "SortFunc", "SortStableFunc", "MinFunc", "MaxFunc":
		case "BinarySearchFunc":
			index = 2
		default:
			return
		}
		if len(call.Args) != index+1 {
			return
		}
		comparator, ok := astutil.UnwrapParenExpr(call.Args[index]).(*ast.FuncLit)
		if !ok || len(comparator.Body.List) != 1 {
			return
		}
		ret, ok := comparator.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return
		}
		compare, ok := astutil.UnwrapParenExpr(ret.Results[0]).(*ast.CallExpr)
		if !ok || calledFunc(pass, compare.Fun, "strings") != "Compare" {
			return
		}
		pos := pass.Fset.PositionFor(compare.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generated) || nolint.HasDirectiveForLinter(pos, directives, name) {
			return
		}
		pass.ReportRangef(compare, "strings.Compare in a slices comparator can use cmp.Compare instead")
	})
}

func calledFunc(pass *analysis.Pass, expr ast.Expr, pkgPath string) string {
	expr = astutil.UnwrapParenExpr(expr)
	switch x := expr.(type) {
	case *ast.IndexExpr:
		expr = astutil.UnwrapParenExpr(x.X)
	case *ast.IndexListExpr:
		expr = astutil.UnwrapParenExpr(x.X)
	}
	var obj types.Object
	switch x := expr.(type) {
	case *ast.SelectorExpr:
		if pass.TypesInfo.Selections[x] != nil {
			return ""
		}
		obj = pass.TypesInfo.Uses[x.Sel]
	case *ast.Ident:
		obj = pass.TypesInfo.Uses[x]
	}
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return ""
	}
	return fn.Name()
}
