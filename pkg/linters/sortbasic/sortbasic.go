// Package sortbasic implements a Go analysis linter that flags sort.Strings,
// sort.Ints, and sort.Float64s calls that should use slices.Sort from the
// standard library slices package.
package sortbasic

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:sortbasic")

// Analyzer is the sort-basic analysis pass.
var Analyzer = analyzerutil.New("sortbasic", "reports sort.Strings, sort.Ints, and sort.Float64s calls that should use slices.Sort", run)

// hotThreshold gates findings on coverage data; see coverage package docs.
var hotThreshold *int

func init() {
	hotThreshold = coverage.RegisterHotThresholdFlag(Analyzer)
}

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
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "sortbasic") {
			continue
		}

		sel, ok := matchSortCall(pass, call)
		if !ok {
			continue
		}
		if !coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
			continue
		}

		pkgLog.Printf("flagging sort.%s call at %s", sel.Sel.Name, pos)
		pass.ReportRangef(call, "sort.%s is a legacy helper; use slices.Sort instead", sel.Sel.Name)
	}

	return nil, nil
}

// matchSortCall reports whether call is a call to sort.Strings, sort.Ints, or
// sort.Float64s (even under an import alias) and returns the selector
// expression for the call site.
func matchSortCall(pass *analysis.Pass, call *ast.CallExpr) (*ast.SelectorExpr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil, false
	}
	if pass.TypesInfo == nil {
		return nil, false
	}
	obj := pass.TypesInfo.ObjectOf(pkgIdent)
	// ObjectOf can be nil when type information is incomplete.
	if obj == nil {
		return nil, false
	}
	pkgName, ok := obj.(*types.PkgName)
	if !ok || pkgName.Imported().Path() != "sort" {
		return nil, false
	}
	switch sel.Sel.Name {
	case "Strings", "Ints", "Float64s":
		return sel, true
	}
	return nil, false
}
