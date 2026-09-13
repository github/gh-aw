// Package execcommandwithoutcontext implements a Go analysis linter that flags
// calls to exec.Command inside functions that already receive a context.Context
// parameter, where exec.CommandContext should be used instead to propagate
// cancellation.
package execcommandwithoutcontext

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:execcommandwithoutcontext")

// Analyzer is the exec-command-without-context analysis pass.
var Analyzer = analyzerutil.New("execcommandwithoutcontext", "reports exec.Command calls inside context-receiving functions where exec.CommandContext should be used to propagate cancellation", run)

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
		checkExecCommandCall(pass, cur, generatedFiles, noLintIndex)
	}
	return nil, nil
}

// checkExecCommandCall inspects a single call expression and reports a diagnostic
// when exec.Command is used inside a context-receiving function.
func checkExecCommandCall(pass *analysis.Pass, cur inspector.Cursor, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := cur.Node().(*ast.CallExpr)
	if !ok {
		return
	}
	sel, ok := execCommandSelector(pass, call)
	if !ok {
		return
	}
	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "execcommandwithoutcontext") {
		return
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
		pkgLog.Printf("flagging exec.Command without context at %s", pos)
		var fixes []analysis.SuggestedFix
		if !astutil.HasOverlappingComment(pass.Files, call.Pos(), call.End()) {
			fixes = []analysis.SuggestedFix{
				{
					Message: fmt.Sprintf("Replace exec.Command with exec.CommandContext(%s, ...)", ctxParamName),
					TextEdits: []analysis.TextEdit{
						{
							Pos:     sel.Sel.Pos(),
							End:     sel.Sel.End(),
							NewText: []byte("CommandContext"),
						},
						{
							Pos:     call.Lparen + 1,
							End:     call.Lparen + 1,
							NewText: []byte(ctxParamName + ", "),
						},
					},
				},
			}
		}
		pass.Report(analysis.Diagnostic{
			Pos:            call.Pos(),
			End:            call.End(),
			Message:        fmt.Sprintf("use exec.CommandContext(%s, ...) instead of exec.Command to propagate context cancellation", ctxParamName),
			SuggestedFixes: fixes,
		})
		break
	}
}

// execCommandSelector reports the selector expression for calls to
// exec.Command from os/exec.
func execCommandSelector(pass *analysis.Pass, call *ast.CallExpr) (*ast.SelectorExpr, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Command" {
		return nil, false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return nil, false
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return nil, false
	}
	pkgName, ok := obj.(*types.PkgName)
	if !ok {
		return nil, false
	}
	if pkgName.Imported().Path() != "os/exec" {
		return nil, false
	}
	return sel, true
}
