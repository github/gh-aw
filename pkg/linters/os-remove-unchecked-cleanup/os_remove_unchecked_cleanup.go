// Package osremoveuncheckedcleanup implements a Go analysis linter that flags
// bare statement calls to os.Remove or os.RemoveAll whose error is discarded.
package osremoveuncheckedcleanup

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const linterName = "osremoveuncheckedcleanup"

// Analyzer is the os-remove-unchecked-cleanup analysis pass.
var Analyzer = analyzerutil.NewAtPath(linterName,
	"reports bare statement calls to os.Remove or os.RemoveAll whose returned error is discarded",
	"os-remove-unchecked-cleanup", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.ExprStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		stmt, ok := n.(*ast.ExprStmt)
		if !ok {
			return
		}
		position := pass.Fset.PositionFor(stmt.Pos(), false)
		if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
			return
		}
		call, ok := astutil.UnwrapParenExpr(stmt.X).(*ast.CallExpr)
		if !ok {
			return
		}
		name, ok := osRemoveName(pass, call)
		if !ok {
			return
		}
		if nolint.HasDirectiveForLinter(position, noLintIndex, linterName) {
			return
		}
		pass.Reportf(call.Pos(), "os.%s error is discarded; handle it explicitly (e.g. log it or assign to _)", name)
	})
}

// osRemoveName returns "Remove" or "RemoveAll" when call invokes the os package function.
func osRemoveName(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || (sel.Sel.Name != "Remove" && sel.Sel.Name != "RemoveAll") {
		return "", false
	}
	fn, ok := pass.TypesInfo.Uses[sel.Sel].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "os" {
		return "", false
	}
	return fn.Name(), true
}
