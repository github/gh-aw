// Package sortbasicslices implements a Go analysis linter that flags sort.Strings,
// sort.Ints, and sort.Float64s calls that should use the type-safe slices.Sort
// function from the standard library slices package.
package sortbasicslices

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

var pkgLog = logger.New("linters:sortbasicslices")

// Analyzer is the sort-basic-slices analysis pass.
var Analyzer = analyzerutil.New("sortbasicslices", "reports sort.Strings, sort.Ints, and sort.Float64s calls that should use slices.Sort", run)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())
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

		name, ok := matchSortCall(pass, call)
		if !ok {
			continue
		}

		pkgLog.Printf("flagging sort.%s call at %s", name, pos)
		pass.ReportRangef(call, "sort.%s is not type-safe; use slices.Sort instead", name)
	}

	return nil, nil
}

// matchSortCall reports whether call is a call to sort.Strings, sort.Ints, or
// sort.Float64s, including calls through an aliased import.
func matchSortCall(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok || pass.TypesInfo == nil {
		return "", false
	}
	obj := pass.TypesInfo.ObjectOf(pkgIdent)
	if obj == nil {
		return "", false
	}
	pkgName, ok := obj.(*types.PkgName)
	if !ok || pkgName.Imported().Path() != "sort" {
		return "", false
	}
	switch sel.Sel.Name {
	case "Strings", "Ints", "Float64s":
		return sel.Sel.Name, true
	default:
		return "", false
	}
}
