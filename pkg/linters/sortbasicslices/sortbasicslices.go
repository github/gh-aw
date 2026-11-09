// Package sortbasicslices implements a Go analysis linter that flags
// sort.Strings, sort.Ints, and sort.Float64s calls that should use the
// type-safe slices.Sort from the standard library slices package.
package sortbasicslices

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the sort-basic-slices analysis pass.
var Analyzer = analyzerutil.New("sortbasicslices", "reports sort.Strings, sort.Ints, and sort.Float64s calls that should use the type-safe slices.Sort from the standard library slices package", run)

func run(pass *analysis.Pass) (any, error) {
	root, err := astutil.Root(pass)
	if err != nil {
		return nil, err
	}
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	for cur := range root.Preorder((*ast.CallExpr)(nil)) {
		call, ok := cur.Node().(*ast.CallExpr)
		if !ok {
			continue
		}

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "sortbasicslices") {
			continue
		}

		name, ok := basicSortName(pass, call)
		if !ok {
			continue
		}
		pass.ReportRangef(call, "sort.%s can be replaced with slices.Sort", name)
	}

	return nil, nil
}

// basicSortName returns the function name if call invokes sort.Strings,
// sort.Ints, or sort.Float64s, resolved through type information so import
// aliases are handled.
func basicSortName(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || pass.TypesInfo == nil {
		return "", false
	}
	fn, ok := pass.TypesInfo.ObjectOf(sel.Sel).(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "sort" {
		return "", false
	}
	switch fn.Name() {
	case "Strings", "Ints", "Float64s":
		return fn.Name(), true
	}
	return "", false
}
