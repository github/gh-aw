// Package fprintferrorunchecked implements a Go analysis linter that flags
// fmt.Fprintf, fmt.Fprint, and fmt.Fprintln calls where the error return
// value is not checked, potentially silencing write failures.
package fprintferrorunchecked

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
			if call, funcName, ok := matchFprintCall(pass, stmt.X); ok {
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
	if len(assign.Rhs) != 1 {
		return
	}

	// Every left-hand side must be blank (true "discard all" pattern).
	for _, lhs := range assign.Lhs {
		blank, ok := lhs.(*ast.Ident)
		if !ok || blank.Name != "_" {
			return
		}
	}

	if call, funcName, ok := matchFprintCall(pass, assign.Rhs[0]); ok { //nolint:uncheckedsliceindex // len(assign.Rhs) is checked above
		reportUncheckedFprint(pass, call, funcName, noLintIndex)
	}
}

// matchFprintCall reports whether expr (ignoring redundant parentheses) is a
// call to fmt.Fprintf, fmt.Fprint, or fmt.Fprintln that returns an error, and
// returns the call and the function name.
func matchFprintCall(pass *analysis.Pass, expr ast.Expr) (*ast.CallExpr, string, bool) {
	call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	if !ok {
		return nil, "", false
	}
	// PackageCall resolves the callee through the type checker, so aliased
	// imports (import f "fmt") and parenthesized callees ((fmt.Fprintf)(...))
	// are recognised and a shadowing local named fmt is not.
	pkgPath, funcName, ok := astutil.PackageCall(pass, call)
	if !ok || pkgPath != "fmt" || !isFprintFunction(funcName) || !isFprintCallReturningError(pass, call) {
		return nil, "", false
	}
	return call, funcName, true
}

func reportUncheckedFprint(pass *analysis.Pass, call *ast.CallExpr, funcName string, noLintIndex nolint.DirectiveIndex) {
	position := pass.Fset.PositionFor(call.Pos(), false)
	if nolint.HasDirectiveForLinter(position, noLintIndex, "fprintferrorunchecked") || isConsoleOutput(pass, call) {
		return
	}
	pkgLog.Printf("flagging unchecked fmt.%s() error at %s:%d", funcName, position.Filename, position.Line)
	pass.ReportRangef(call, "error return from fmt.%s() is not checked; write failures may be silently ignored", funcName)
}

// Console writes are best-effort diagnostics; unlike file or network writes,
// their errors cannot usefully be recovered by the caller.
func isConsoleOutput(pass *analysis.Pass, call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	sel, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.SelectorExpr) //nolint:uncheckedsliceindex // len(call.Args) is checked above
	if !ok || (sel.Sel.Name != "Stdout" && sel.Sel.Name != "Stderr") {
		return false
	}
	obj := pass.TypesInfo.ObjectOf(sel.Sel)
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "os"
}

// isFprintFunction returns true if the function name is one of the fprintf functions we check.
func isFprintFunction(funcName string) bool {
	switch funcName {
	case "Fprintf", "Fprint", "Fprintln":
		return true
	}
	return false
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
