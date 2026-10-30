// Package errorsnewerror implements a Go analysis linter that flags
// errors.New(err.Error()) calls, which rebuild an error from its own message
// and discard the original error chain.
package errorsnewerror

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the errorsnewerror analysis pass.
var Analyzer = analyzerutil.New("errorsnewerror", "reports errors.New(err.Error()) calls that copy an error's message into a new error and lose its chain; return err or wrap it with fmt.Errorf and %w instead", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeCall(pass, n, generatedFiles, noLintIndex)
	})
}

func analyzeCall(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return
	}
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" || !astutil.IsPkgSelector(pass, sel, "errors") {
		return
	}

	inner, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.CallExpr)
	if !ok || len(inner.Args) != 0 {
		return
	}
	method, ok := astutil.UnwrapParenExpr(inner.Fun).(*ast.SelectorExpr)
	if !ok || method.Sel.Name != "Error" {
		return
	}
	recvType := pass.TypesInfo.TypeOf(method.X)
	if recvType == nil || !implementsError(recvType) {
		return
	}

	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "errorsnewerror") {
		return
	}
	pass.Reportf(call.Pos(), "errors.New(err.Error()) discards the original error chain; return the error itself or wrap it with fmt.Errorf and %%w")
}

func implementsError(t types.Type) bool {
	errType := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	return types.Implements(t, errType)
}
