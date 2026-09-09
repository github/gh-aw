// Package timenowsub implements a Go analysis linter that flags
// time.Now().Sub(t) calls that can be simplified to time.Since(t).
package timenowsub

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the time-now-sub analysis pass.
var Analyzer = analyzerutil.New("timenowsub", "reports time.Now().Sub(t) calls that should be simplified to time.Since(t)", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeTimeNowSub(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeTimeNowSub checks whether a call is a time.Now().Sub(t) that can be
// simplified to time.Since(t) and reports a diagnostic if so.
func analyzeTimeNowSub(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	outer, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	// Match <expr>.Sub(<arg>) where <expr> is time.Now().
	sel, ok := outer.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sub" {
		return
	}
	if len(outer.Args) != 1 {
		return
	}

	nowCall, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return
	}
	qualifier, ok := timeNowQualifier(pass, nowCall)
	if !ok {
		return
	}
	if !isSafeSinceArg(outer.Args[0]) {
		return
	}

	pos := pass.Fset.PositionFor(outer.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "timenowsub") {
		return
	}

	argText := astutil.NodeText(pass.Fset, outer.Args[0])
	if argText == "" {
		return
	}
	sinceText := qualifier + ".Since(" + argText + ")"

	diag := analysis.Diagnostic{
		Pos:     outer.Pos(),
		End:     outer.End(),
		Message: fmt.Sprintf("%s.Now().Sub(%s) can be simplified to %s", qualifier, argText, sinceText),
	}
	if !astutil.HasOverlappingComment(pass.Files, outer.Pos(), outer.End()) {
		diag.SuggestedFixes = []analysis.SuggestedFix{{
			Message: fmt.Sprintf("Replace %s.Now().Sub(%s) with %s", qualifier, argText, sinceText),
			TextEdits: []analysis.TextEdit{{
				Pos:     outer.Pos(),
				End:     outer.End(),
				NewText: []byte(sinceText),
			}},
		}}
	}
	pass.Report(diag)
}

// timeNowQualifier reports the imported identifier used for time.Now().
func timeNowQualifier(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	if len(call.Args) != 0 {
		return "", false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Now" {
		return "", false
	}
	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return "", false
	}
	pkgName, ok := obj.(*types.PkgName)
	if !ok {
		return "", false
	}
	return ident.Name, pkgName.Imported().Path() == "time"
}

// isSafeSinceArg reports whether expr can be evaluated before time.Now()
// without introducing calls or other potentially observable behavior changes.
func isSafeSinceArg(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return true
	case *ast.BasicLit:
		return true
	case *ast.ParenExpr:
		return isSafeSinceArg(e.X)
	case *ast.SelectorExpr:
		return isSafeSinceArg(e.X)
	case *ast.IndexExpr:
		return isSafeSinceArg(e.X) && isSafeSinceArg(e.Index)
	case *ast.StarExpr:
		return isSafeSinceArg(e.X)
	default:
		return false
	}
}
