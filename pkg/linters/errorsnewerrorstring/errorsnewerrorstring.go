// Package errorsnewerrorstring implements a Go analysis linter that flags
// errors.New(err.Error()) calls that discard the original error.
package errorsnewerrorstring

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the errorsnewerrorstring analysis pass.
var Analyzer = analyzerutil.New("errorsnewerrorstring", "reports errors.New(err.Error()) calls that discard the original error", run)

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

		newSelector, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
		if !ok || newSelector.Sel.Name != "New" || !astutil.IsPkgSelector(pass, newSelector, "errors") {
			return
		}
		if len(call.Args) != 1 {
			return
		}

		// The argument count is checked above.
		arg := call.Args[0] //nolint:uncheckedsliceindex
		errorCall, ok := astutil.UnwrapParenExpr(arg).(*ast.CallExpr)
		if !ok || len(errorCall.Args) != 0 {
			return
		}
		errorSelector, ok := astutil.UnwrapParenExpr(errorCall.Fun).(*ast.SelectorExpr)
		if !ok || errorSelector.Sel.Name != "Error" {
			return
		}
		receiverType := pass.TypesInfo.TypeOf(errorSelector.X)
		if receiverType == nil || !nolint.ImplementsError(receiverType) {
			return
		}

		if nolint.HasDirectiveForLinter(position, noLintIndex, "errorsnewerrorstring") {
			return
		}
		pass.Reportf(call.Pos(), "errors.New(err.Error()) discards the original error; return it directly or wrap it with fmt.Errorf using %%w")
	})
}
