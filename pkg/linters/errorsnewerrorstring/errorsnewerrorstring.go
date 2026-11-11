// Package errorsnewerrorstring implements a Go analysis linter that flags
// errors.New(err.Error()) calls that discard the wrapped error chain.
package errorsnewerrorstring

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

var errorIface = astutil.UniverseErrorInterface()

// Analyzer is the errorsnewerrorstring analysis pass.
var Analyzer = analyzerutil.New("errorsnewerrorstring", "reports errors.New(err.Error()) calls that discard the wrapped error chain; return the original error or wrap it with fmt.Errorf and %w instead", run)

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

		position := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
			return
		}

		sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "New" || !astutil.IsPkgSelector(pass, sel, "errors") {
			return
		}
		if len(call.Args) != 1 {
			return
		}
		if !isErrorDotCall(pass, call.Args[0]) { //nolint:uncheckedsliceindex // len call.Args is checked above
			return
		}

		if nolint.HasDirectiveForLinter(position, noLintIndex, "errorsnewerrorstring") {
			return
		}
		pass.Reportf(call.Pos(), `errors.New(err.Error()) discards the wrapped error chain; return err or use fmt.Errorf("...: %%w", err)`)
	})
}

// isErrorDotCall reports whether expr is a call of the form X.Error() where
// X implements the error interface.
func isErrorDotCall(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Error" {
		return false
	}
	if errorIface == nil {
		return false
	}
	t := pass.TypesInfo.TypeOf(sel.X)
	return t != nil && types.Implements(t, errorIface)
}
