// Package fprintferrorunchecked implements a Go analysis linter that flags
// fmt.Fprintf, fmt.Fprint, and fmt.Fprintln calls where the error return
// value is not checked, potentially silencing write failures.
package fprintferrorunchecked

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:fprintferrorunchecked")

// Analyzer is the fprintf-error-unchecked analysis pass.
var Analyzer = analyzerutil.New("fprintferrorunchecked", "reports unchecked fmt.Fprintf/Fprint/Fprintln calls that may silently fail", run)

var errorInterface = func() *types.Interface {
	errorType := types.Universe.Lookup("error").Type()
	iface, ok := errorType.Underlying().(*types.Interface)
	if !ok {
		return types.NewInterfaceType(nil, nil).Complete()
	}
	return iface
}()

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.AssignStmt)(nil), (*ast.ExprStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		switch stmt := n.(type) {
		case *ast.AssignStmt:
			position := pass.Fset.PositionFor(stmt.Pos(), false)
			if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
				return
			}
			checkUncheckedFprintAssign(pass, stmt, noLintIndex)
		case *ast.ExprStmt:
			position := pass.Fset.PositionFor(stmt.Pos(), false)
			if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
				return
			}
			call, ok := stmt.X.(*ast.CallExpr)
			if !ok {
				return
			}
			funcName := extractFunctionName(call)
			if isFprintFunction(funcName) && isFprintCallReturningError(pass, call) {
				reportUncheckedFprint(pass, call, funcName, noLintIndex)
			}
		}
	})
}

// checkUncheckedFprintAssign flags _ = fmt.Fprintf(...) assignments where the error
// is explicitly thrown away with a blank identifier.
func checkUncheckedFprintAssign(pass *analysis.Pass, assign *ast.AssignStmt, noLintIndex nolint.DirectiveIndex) {
	// Only flag patterns like: _ = fmt.Fprintf(...) or _, _ = fmt.Fprintf(...)
	// Do not flag: n, err := fmt.Fprintf(...) or similar patterns where result is used

	// Pattern 1: _ = fmt.Fprintf(...)
	if len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
		blank, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || blank.Name != "_" {
			return
		}
		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return
		}
		funcName := extractFunctionName(call)
		if !isFprintFunction(funcName) {
			return
		}
		if !isFprintCallReturningError(pass, call) {
			return
		}
		reportUncheckedFprint(pass, call, funcName, noLintIndex)
		return
	}

	// Pattern 2: _, _ = fmt.Fprintf(...) or similar multi-return with all blanks
	// We only flag if ALL left-hand sides are blanks (true "discard all" pattern)
	if len(assign.Rhs) == 1 {
		allBlanks := true
		for _, lhs := range assign.Lhs {
			blank, ok := lhs.(*ast.Ident)
			if !ok || blank.Name != "_" {
				allBlanks = false
				break
			}
		}
		if !allBlanks {
			return
		}

		call, ok := assign.Rhs[0].(*ast.CallExpr)
		if !ok {
			return
		}
		funcName := extractFunctionName(call)
		if !isFprintFunction(funcName) {
			return
		}
		if !isFprintCallReturningError(pass, call) {
			return
		}
		reportUncheckedFprint(pass, call, funcName, noLintIndex)
	}
}

func reportUncheckedFprint(pass *analysis.Pass, call *ast.CallExpr, funcName string, noLintIndex nolint.DirectiveIndex) {
	position := pass.Fset.PositionFor(call.Pos(), false)
	if nolint.HasDirectiveForLinter(position, noLintIndex, "fprintferrorunchecked") {
		return
	}
	pkgLog.Printf("flagging unchecked fmt.%s() error at %s:%d", funcName, position.Filename, position.Line)
	pass.ReportRangef(call, "error return from fmt.%s() is not checked; write failures may be silently ignored", funcName)
}

// isFprintFunction returns true if the function name is one of the fprintf functions we check.
func isFprintFunction(funcName string) bool {
	switch funcName {
	case "Fprintf", "Fprint", "Fprintln":
		return true
	}
	return false
}

// extractFunctionName extracts the function name from a call expression.
// Returns empty string if not a simple function or method call.
func extractFunctionName(call *ast.CallExpr) string {
	if call == nil || call.Fun == nil {
		return ""
	}

	// Handle selector expressions (fmt.Fprintf)
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "fmt" {
			return sel.Sel.Name
		}
	}

	return ""
}

// isFprintCallReturningError returns true when call is a fmt.Fprintf/Fprint/Fprintln
// that returns (int, error) or similar (int/int64, error).
func isFprintCallReturningError(pass *analysis.Pass, call *ast.CallExpr) bool {
	sig, ok := pass.TypesInfo.TypeOf(call.Fun).(*types.Signature)
	if !ok {
		return false
	}

	res := sig.Results()
	// fmt.Fprintf, fmt.Fprint, fmt.Fprintln all return (int, error)
	if res.Len() != 2 {
		return false
	}

	// Check if second return value is error
	secondReturnType := res.At(1).Type()
	return types.Implements(secondReturnType, errorInterface)
}
