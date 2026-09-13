// Package ctxbackground implements a Go analysis linter that flags
// calls to context.Background() inside functions that already receive
// a context.Context parameter.
package ctxbackground

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the ctx-background analysis pass.
var Analyzer = analyzerutil.New("ctxbackground", "reports calls to context.Background() inside functions that already receive a context.Context parameter", run)

func run(pass *analysis.Pass) (any, error) {
	insp, err := astutil.Inspector(pass)
	if err != nil {
		return nil, err
	}
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	for cur := range insp.Root().Preorder((*ast.CallExpr)(nil)) {
		call, ok := cur.Node().(*ast.CallExpr)
		if !ok || !isContextBackgroundCall(pass, call) {
			continue
		}

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "ctxbackground") {
			continue
		}

		for encl := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
			ftype := astutil.EnclosingFuncType(encl.Node())
			if ftype == nil {
				continue
			}
			ctxParamName, ok := astutil.ContextParamName(pass, ftype)
			if !ok {
				break
			}

			var fixes []analysis.SuggestedFix
			if !astutil.HasOverlappingComment(pass.Files, call.Pos(), call.End()) {
				fixes = []analysis.SuggestedFix{
					{
						Message: "Replace context.Background() with context parameter",
						TextEdits: []analysis.TextEdit{
							{
								Pos:     call.Pos(),
								End:     call.End(),
								NewText: []byte(ctxParamName),
							},
						},
					},
				}
			}
			pass.Report(analysis.Diagnostic{
				Pos:            call.Pos(),
				End:            call.End(),
				Message:        "use the context.Context parameter instead of context.Background()",
				SuggestedFixes: fixes,
			})
			break
		}
	}

	return nil, nil
}

func isContextBackgroundCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Background" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok || pass.TypesInfo == nil {
		return false
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return false
	}
	pkgName, ok := obj.(*types.PkgName)
	if !ok {
		return false
	}
	return pkgName.Imported().Path() == "context"
}
