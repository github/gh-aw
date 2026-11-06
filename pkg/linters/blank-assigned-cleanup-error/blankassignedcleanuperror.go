// Package blankassignedcleanuperror implements a Go analysis linter that flags
// `_ =` assignments discarding errors from filesystem operations such as
// os.RemoveAll, os.Remove, os.WriteFile, and os.MkdirAll.
//
// Receiver Close() calls are intentionally not reported here: the
// closeerrorunchecked analyzer already diagnoses `_ = x.Close()`.
// Assignments inside deferred closures are treated as deliberate best-effort
// cleanup and are skipped.
package blankassignedcleanuperror

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the blank-assigned-cleanup-error analysis pass.
var Analyzer = analyzerutil.NewAtPath(
	"blankassignedcleanuperror",
	"reports `_ =` assignments that discard errors from filesystem cleanup operations such as os.RemoveAll, os.Remove, os.WriteFile, and os.MkdirAll outside deferred functions",
	"blank-assigned-cleanup-error",
	run,
)

var checkedOSFuncs = []string{"RemoveAll", "Remove", "WriteFile", "MkdirAll"}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	root, err := astutil.Root(pass)
	if err != nil {
		return nil, err
	}

	for cur := range root.Preorder((*ast.AssignStmt)(nil)) {
		assign, ok := cur.Node().(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			continue
		}
		var lhs, rhs ast.Expr
		for _, expr := range assign.Lhs {
			lhs = expr
		}
		for _, expr := range assign.Rhs {
			rhs = expr
		}
		blank, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident)
		if !ok || blank.Name != "_" {
			continue
		}
		call, ok := astutil.UnwrapParenExpr(rhs).(*ast.CallExpr)
		if !ok {
			continue
		}
		fn, ok := astutil.CalledOSFunc(pass, call, checkedOSFuncs...)
		if !ok {
			continue
		}

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "blankassignedcleanuperror") {
			continue
		}
		if insideDeferredClosure(cur) {
			continue
		}
		pass.ReportRangef(call, "error from os.%s is discarded with `_ =`; handle or log the failure", fn.Name())
	}
	return nil, nil
}

// insideDeferredClosure reports whether cur is nested in a function literal
// that is directly invoked by a defer statement.
func insideDeferredClosure(cur inspector.Cursor) bool {
	for c := cur.Parent(); c.Node() != nil; c = c.Parent() {
		if _, ok := c.Node().(*ast.FuncLit); ok && astutil.IsGoOrDeferClosure(c) && isDeferred(c) {
			return true
		}
	}
	return false
}

// isDeferred reports whether the closure at c is called by a defer statement
// (as opposed to a go statement), looking through parentheses.
func isDeferred(c inspector.Cursor) bool {
	p := c.Parent()
	for {
		if _, ok := p.Node().(*ast.ParenExpr); !ok {
			break
		}
		p = p.Parent()
	}
	_, ok := p.Parent().Node().(*ast.DeferStmt)
	return ok
}
