// Package timesleepnocontext implements a Go analysis linter that flags
// bare time.Sleep calls inside functions that already receive a
// context.Context parameter, where a context-aware select should be used
// to propagate cancellation.
package timesleepnocontext

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:timesleepnocontext")

// Analyzer is the time-sleep-no-context analysis pass.
var Analyzer = analyzerutil.New("timesleepnocontext", "reports time.Sleep calls inside context-receiving functions where a context-aware select should be used to allow cancellation", run)

func run(pass *analysis.Pass) (any, error) {
	insp, err := astutil.Inspector(pass)
	if err != nil {
		return nil, err
	}

	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	for cur := range insp.Root().Preorder((*ast.CallExpr)(nil)) {
		call, ok := cur.Node().(*ast.CallExpr)
		if !ok {
			continue
		}
		if !isTimeSleepCall(pass, call) {
			continue
		}

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "timesleepnocontext") {
			continue
		}

		for encl := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
			funcNode := encl.Node()
			funcType := astutil.EnclosingFuncType(funcNode)
			if funcType == nil {
				continue
			}
			ctxParamName, hasCtx := astutil.ContextParamName(pass, funcType)
			if !hasCtx {
				if _, isFuncLit := funcNode.(*ast.FuncLit); isFuncLit && !astutil.IsGoOrDeferClosure(encl) {
					break
				}
				continue
			}
			pkgLog.Printf("flagging time.Sleep in context-aware function at %s", pos)
			pass.Report(analysis.Diagnostic{
				Pos:     call.Pos(),
				End:     call.End(),
				Message: fmt.Sprintf("use select with %s.Done() instead of time.Sleep to allow context cancellation", ctxParamName),
			})
			break
		}
	}

	return nil, nil
}

// isTimeSleepCall reports whether call is a call to time.Sleep. A
// parenthesized callee such as (time.Sleep)(d) is also recognized.
func isTimeSleepCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sleep" {
		return false
	}
	return astutil.IsPkgSelector(pass, sel, "time")
}
