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

	// Get the type of the receiver to check if Close() returns an error
	if pass.TypesInfo == nil {
		return
	}

	// Get type of the object being closed (the receiver)
	receiverType := pass.TypesInfo.TypeOf(selector.X)
	if receiverType == nil {
		return
	}

	// Look for a Close method on this type
	closeMethod, found := lookupClose(receiverType)
	if !found {
		return
	}

	// Check if Close() returns an error
	if sig, ok := closeMethod.Type().(*types.Signature); ok {
		if sig.Results() == nil || sig.Results().Len() == 0 {
			return
		}

		// Check if the return type is error
		lastResult := sig.Results().At(sig.Results().Len() - 1)
		if lastResult.Type() != builtinErrorType {
			return
		}

		// Report: defer close() ignores error return
		pass.Reportf(
			deferStmt.Pos(),
			"defer close() call ignores error return value; consider handling errors explicitly",
		)
	}
}

// lookupClose searches for a Close method on the given type.
func lookupClose(typ types.Type) (types.Object, bool) {
	// Unwrap pointer type if necessary
	ptr, ok := typ.(*types.Pointer)
	if ok {
		typ = ptr.Elem()
	}

	// If it's a named type, get its underlying type
	if named, ok := typ.(*types.Named); ok {
		for i := 0; i < named.NumMethods(); i++ {
			method := named.Method(i)
			if method.Name() == "Close" {
				return method, true
			}
		}
	}

	// Check interface types
	if iface, ok := typ.(*types.Interface); ok {
		for i := 0; i < iface.NumMethods(); i++ {
			method := iface.Method(i)
			if method.Name() == "Close" {
				return method, true
			}
		}
	}

	return nil, false
}
