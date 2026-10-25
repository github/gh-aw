// Package wgdonenotdeferred implements a Go analysis linter that flags
// sync.WaitGroup Done() calls that are not deferred, which can lead to
// deadlocks if the function panics or returns early before Done() is reached.
package wgdonenotdeferred

import (
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:wgdonenotdeferred")

// Analyzer is the wgdonenotdeferred analysis pass.
var Analyzer = analyzerutil.New("wgdonenotdeferred", "reports sync.WaitGroup Done() calls that are not deferred, which can cause deadlock if the function panics", run)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{
		(*ast.FuncDecl)(nil),
	}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return
		}
		pos := pass.Fset.PositionFor(fn.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		inspectBody(pass, noLintIndex, fn.Body)
	})
}

func inspectBody(pass *analysis.Pass, noLintIndex nolint.DirectiveIndex, body *ast.BlockStmt) {
	var stack []ast.Node
	deferredFuncLits := make(map[*ast.FuncLit]struct{})

	ast.Inspect(body, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1]
			return false
		}

		if deferStmt, ok := node.(*ast.DeferStmt); ok {
			if deferStmt.Call != nil {
				if deferredFuncLit, ok := astutil.UnwrapParenExpr(deferStmt.Call.Fun).(*ast.FuncLit); ok {
					deferredFuncLits[deferredFuncLit] = struct{}{}
				}
			}
		}

		if funcLit, ok := node.(*ast.FuncLit); ok {
			if _, deferred := deferredFuncLits[funcLit]; deferred {
				stack = append(stack, node)
				return false
			}
		}

		inLoop := false
	loopSearch:
		for _, ancestor := range slices.Backward(stack) {
			switch ancestor.(type) {
			case *ast.FuncLit:
				break loopSearch
			case *ast.ForStmt, *ast.RangeStmt:
				inLoop = true
				break loopSearch
			}
		}

		// Skip diagnostics for loop-body calls where defer would be semantically wrong.
		if !inLoop {
			// Flag any statement-level wg.Done() call that is not wrapped in defer.
			// A deferred call appears as a DeferStmt, not an ExprStmt, so checking for
			// ExprStmt naturally excludes deferred calls.
			if exprStmt, ok := node.(*ast.ExprStmt); ok {
				if call, ok := astutil.UnwrapParenExpr(exprStmt.X).(*ast.CallExpr); ok {
					if isWaitGroupDone(pass, call) {
						pos := pass.Fset.PositionFor(call.Pos(), false)
						if !nolint.HasDirectiveForLinter(pos, noLintIndex, "wgdonenotdeferred") {
							pkgLog.Printf("flagging non-deferred WaitGroup Done() at %s", pos)
							pass.ReportRangef(call,
								"sync.WaitGroup Done() should be deferred to prevent deadlock if the function panics")
						}
					}
				}
			}
		}

		stack = append(stack, node)
		return true
	})
}

func isWaitGroupDone(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Done" {
		return false
	}
	return isWaitGroupType(pass.TypesInfo.TypeOf(sel.X))
}

func isWaitGroupType(t types.Type) bool {
	return isWaitGroupTypeRecursive(t, map[types.Type]struct{}{})
}

func isWaitGroupTypeRecursive(t types.Type, seen map[types.Type]struct{}) bool {
	if t == nil {
		return false
	}

	if _, ok := seen[t]; ok {
		return false
	}
	seen[t] = struct{}{}

	if ptr, ok := t.(*types.Pointer); ok {
		return isWaitGroupTypeRecursive(ptr.Elem(), seen)
	}

	named, ok := t.(*types.Named)
	if !ok {
		return false
	}

	obj := named.Obj()
	if obj == nil || obj.Pkg() == nil {
		return false
	}

	if obj.Pkg().Path() == "sync" && obj.Name() == "WaitGroup" {
		return true
	}

	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for field := range st.Fields() {
		if !field.Anonymous() {
			continue
		}
		if isWaitGroupTypeRecursive(field.Type(), seen) {
			return true
		}
	}
	return false
}
