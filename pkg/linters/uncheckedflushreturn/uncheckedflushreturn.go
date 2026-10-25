// Package uncheckedflushreturn implements a Go analysis linter that flags
// Flush() method calls where the error return is discarded.
package uncheckedflushreturn

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

var pkgLog = logger.New("linters:uncheckedflushreturn")

// Analyzer is the unchecked-flush-return analysis pass.
var Analyzer = analyzerutil.New("uncheckedflushreturn", "reports Flush() method calls where the error return is discarded", run)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.ExprStmt)(nil), (*ast.AssignStmt)(nil), (*ast.DeferStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		switch stmt := n.(type) {
		case *ast.ExprStmt:
			position := pass.Fset.PositionFor(stmt.Pos(), false)
			if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
				return
			}
			checkDiscardedFlushExpr(pass, stmt, noLintIndex)
		case *ast.AssignStmt:
			position := pass.Fset.PositionFor(stmt.Pos(), false)
			if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
				return
			}
			checkDiscardedFlushAssign(pass, stmt, noLintIndex)
		case *ast.DeferStmt:
			position := pass.Fset.PositionFor(stmt.Pos(), false)
			if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
				return
			}
			checkDiscardedFlushDefer(pass, stmt, noLintIndex)
		}
	})
}

// checkDiscardedFlushExpr flags Flush() used as a bare expression statement,
// where the error return is silently discarded.
func checkDiscardedFlushExpr(pass *analysis.Pass, stmt *ast.ExprStmt, noLintIndex nolint.DirectiveIndex) {
	call, ok := astutil.UnwrapParenExpr(stmt.X).(*ast.CallExpr)
	if !ok {
		return
	}
	if !isFlushCallReturningError(pass, call) {
		return
	}
	reportUncheckedFlush(pass, call, noLintIndex)
}

// checkDiscardedFlushAssign flags _ = x.Flush() assignments where the error
// is explicitly thrown away with a blank identifier.
func checkDiscardedFlushAssign(pass *analysis.Pass, assign *ast.AssignStmt, noLintIndex nolint.DirectiveIndex) {
	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return
	}
	blank, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || blank.Name != "_" {
		return
	}
	call, ok := astutil.UnwrapParenExpr(assign.Rhs[0]).(*ast.CallExpr)
	if !ok {
		return
	}
	if !isFlushCallReturningError(pass, call) {
		return
	}
	reportUncheckedFlush(pass, call, noLintIndex)
}

// checkDiscardedFlushDefer flags defer x.Flush() statements where the error
// return is dropped when the deferred call executes.
func checkDiscardedFlushDefer(pass *analysis.Pass, stmt *ast.DeferStmt, noLintIndex nolint.DirectiveIndex) {
	if stmt.Call == nil {
		return
	}
	if !isFlushCallReturningError(pass, stmt.Call) {
		return
	}
	reportUncheckedFlush(pass, stmt.Call, noLintIndex)
}

func reportUncheckedFlush(pass *analysis.Pass, call *ast.CallExpr, noLintIndex nolint.DirectiveIndex) {
	position := pass.Fset.PositionFor(call.Pos(), false)
	if nolint.HasDirectiveForLinter(position, noLintIndex, "uncheckedflushreturn") {
		return
	}
	pkgLog.Printf("flagging unchecked Flush() error at %s:%d", position.Filename, position.Line)
	pass.ReportRangef(call, "error return from Flush() is discarded; flush failures silently drop buffered data")
}

// isFlushCallReturningError returns true when call is a method named Flush
// that takes no arguments and returns a single error value.
func isFlushCallReturningError(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "Flush" {
		return false
	}
	if len(call.Args) != 0 {
		return false
	}
	sig, ok := pass.TypesInfo.TypeOf(call.Fun).(*types.Signature)
	if !ok {
		return false
	}
	res := sig.Results()
	if res.Len() != 1 {
		return false
	}
	_, isErr := res.At(0).Type().Underlying().(*types.Interface)
	return isErr && types.Implements(res.At(0).Type(), errorInterface)
}

// errorInterface is the built-in error interface type.
var errorInterface = func() *types.Interface {
	errorType := types.Universe.Lookup("error").Type()
	iface, ok := errorType.Underlying().(*types.Interface)
	if !ok {
		return types.NewInterfaceType(nil, nil).Complete()
	}
	return iface
}()
