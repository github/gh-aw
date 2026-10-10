// Package unchecked deferredclose implements a Go analysis linter that flags
// defer close() calls without error checking, which can hide resource cleanup failures.
package unchecked_deferredclose

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the unchecked-deferred-close analysis pass.
var Analyzer = analyzerutil.New("unchecked_deferredclose", "reports defer close() calls without error checking that may hide resource cleanup failures", run)

var builtinErrorType = types.Universe.Lookup("error").Type()

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.DeferStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeDeferClose(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeDeferClose checks for defer close() calls that don't handle errors.
func analyzeDeferClose(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	deferStmt, ok := n.(*ast.DeferStmt)
	if !ok {
		return
	}

	// The defer statement's Call is already a *CallExpr
	callExpr := deferStmt.Call
	if callExpr == nil {
		return
	}

	// Check if the call is to a Close() method
	selector, ok := callExpr.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Close" {
		return
	}

	// Ensure it has no arguments
	if len(callExpr.Args) != 0 {
		return
	}

	position := pass.Fset.PositionFor(deferStmt.Pos(), false)
	if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
		return
	}

	if nolint.HasDirectiveForLinter(position, noLintIndex, "unchecked_deferredclose") {
		return
	}

	if pass.TypesInfo == nil {
		return
	}

	selection := pass.TypesInfo.Selections[selector]
	if selection == nil || selection.Kind() != types.MethodVal {
		return
	}

	sig, ok := selection.Obj().Type().(*types.Signature)
	if !ok || !closeReturnsError(sig) {
		return
	}

	// Report: defer close() ignores error return
	pass.Reportf(
		deferStmt.Pos(),
		"defer close() call ignores error return value; consider handling errors explicitly",
	)
}

func closeReturnsError(sig *types.Signature) bool {
	if sig.Results() == nil || sig.Results().Len() == 0 {
		return false
	}

	lastResult := sig.Results().At(sig.Results().Len() - 1)
	return lastResult.Type() == builtinErrorType
}
