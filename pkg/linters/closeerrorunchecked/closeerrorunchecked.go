// Package closeerrorunchecked implements a Go analysis linter that flags
// Close() method calls on resources where the error return value is
// explicitly discarded, potentially hiding resource cleanup failures.
package closeerrorunchecked

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the close-error-unchecked analysis pass.
var Analyzer = analyzerutil.New("closeerrorunchecked", "reports Close() method calls where the error return value is explicitly discarded, potentially hiding resource cleanup failures", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{
		(*ast.AssignStmt)(nil),
		(*ast.ExprStmt)(nil),
	}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeCloseError(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeCloseError checks for Close() calls where the error is explicitly discarded.
func analyzeCloseError(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	switch stmt := n.(type) {
	case *ast.AssignStmt:
		analyzeAssignStmt(pass, stmt, generatedFiles, noLintIndex)
	case *ast.ExprStmt:
		analyzeExprStmt(pass, stmt, generatedFiles, noLintIndex)
	}
}

// analyzeAssignStmt checks for patterns like:
// - _ = file.Close()  (error assigned to blank identifier)
// - x, _ := obj.Close()  (error ignored in multi-return)
func analyzeAssignStmt(pass *analysis.Pass, assign *ast.AssignStmt, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	// Pattern 1: _ = file.Close()
	if len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
		for _, lhs := range assign.Lhs {
			blank, ok := lhs.(*ast.Ident)
			if !ok || blank.Name != "_" {
				return
			}
		}
		for _, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				return
			}
			if isCloseMethodCall(pass, call) {
				reportIfNotSkipped(pass, call, generatedFiles, noLintIndex)
			}
		}
		return
	}

	// Pattern 2: x, _ := obj.Close() or similar multi-return with ignored error
	// Only flag if the call itself returns exactly 2 values where the second is error
	if len(assign.Lhs) == 2 && len(assign.Rhs) == 1 {
		for i, lhs := range assign.Lhs {
			if i != len(assign.Lhs)-1 {
				continue
			}
			blank, ok := lhs.(*ast.Ident)
			if !ok || blank.Name != "_" {
				return
			}
		}
		for _, rhs := range assign.Rhs {
			call, ok := rhs.(*ast.CallExpr)
			if !ok {
				return
			}
			if isCloseMethodCall(pass, call) {
				reportIfNotSkipped(pass, call, generatedFiles, noLintIndex)
			}
		}
		return
	}
}

// analyzeExprStmt checks for patterns like:
// file.Close()  (bare call with error ignored)
func analyzeExprStmt(pass *analysis.Pass, exprStmt *ast.ExprStmt, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := exprStmt.X.(*ast.CallExpr)
	if !ok {
		return
	}

	// Don't flag defer statements (those are handled separately)
	// Check if this is inside a defer (this is a simple heuristic check)
	if isCloseMethodCall(pass, call) {
		reportIfNotSkipped(pass, call, generatedFiles, noLintIndex)
	}
}

// isCloseMethodCall returns true if call is of the form receiver.Close() where
// Close() returns error as its only return value or as its second return value.
func isCloseMethodCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" || len(call.Args) != 0 {
		return false
	}

	// Check if Close() returns an error
	callType := pass.TypesInfo.TypeOf(call)
	if callType == nil {
		return false
	}

	// Get the type information about the Close method
	// We need to check the signature of the Close method
	methodObj := pass.TypesInfo.Selections[sel]
	if methodObj == nil {
		// Try direct object lookup for simple identifiers
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return false
		}
		recvObj := pass.TypesInfo.ObjectOf(ident)
		if recvObj == nil {
			return false
		}

		// Look up Close method on this type
		if !hasCloseMethodReturningError(pass, recvObj.Type()) {
			return false
		}
		return true
	}

	// Check the function signature via the selection
	funcType, ok := methodObj.Type().(*types.Signature)
	if !ok || funcType.Params().Len() != 0 {
		return false
	}

	return hasCloseErrorResult(funcType)
}

// hasCloseMethodReturningError checks if type t has a Close() method that returns error.
func hasCloseMethodReturningError(pass *analysis.Pass, t types.Type) bool {
	// Handle pointer types
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}

	// Check for named types
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}

	// Look for Close method
	for method := range named.Methods() {
		if method.Name() != "Close" {
			continue
		}

		funcType, ok := method.Type().(*types.Signature)
		if !ok || funcType.Params().Len() != 0 {
			continue
		}

		if hasCloseErrorResult(funcType) {
			return true
		}
	}

	// Also check interface implementations
	return isCloserInterface(t)
}

func hasCloseErrorResult(funcType *types.Signature) bool {
	results := funcType.Results()
	return results.Len() == 1 && isErrorType(results.At(0).Type()) ||
		results.Len() == 2 && isErrorType(results.At(1).Type())
}

// isCloserInterface returns true if t implements io.Closer.
func isCloserInterface(t types.Type) bool {
	// Handle pointer types
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}

	// For interface types, check if they have a Close() method
	if iface, ok := t.(*types.Interface); ok {
		for method := range iface.Methods() {
			if method.Name() != "Close" {
				continue
			}
			funcType, ok := method.Type().(*types.Signature)
			if !ok || funcType.Params().Len() != 0 {
				continue
			}
			if hasCloseErrorResult(funcType) {
				return true
			}
		}
	}

	return false
}

// isErrorType returns true if t is the error interface type.
func isErrorType(t types.Type) bool {
	return t.String() == "error"
}

// reportIfNotSkipped reports a diagnostic if the linter is not suppressed.
func reportIfNotSkipped(pass *analysis.Pass, call *ast.CallExpr, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "closeerrorunchecked") {
		return
	}

	pass.Report(analysis.Diagnostic{
		Pos:     call.Pos(),
		Message: "Close() error is explicitly discarded; resource cleanup failures may be silently ignored",
	})
}
