// Package sprintferrorsnew implements a Go analysis linter that flags
// errors.New(fmt.Sprintf(...)) calls that should use fmt.Errorf instead.
package sprintferrorsnew

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the sprintferrorsnew analysis pass.
var Analyzer = analyzerutil.New("sprintferrorsnew", "reports errors.New(fmt.Sprintf(...)) calls that should use fmt.Errorf instead", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}

		if filecheck.ShouldSkipFilename(pass.Fset.Position(call.Pos()).Filename, generatedFiles) {
			return
		}

		// Match errors.New(...), including a parenthesized callee such as
		// (errors.New)(...).
		sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "New" {
			return
		}
		if !astutil.IsPkgSelector(pass, sel, "errors") {
			return
		}
		if len(call.Args) != 1 {
			return
		}

		// Check if the sole argument is a fmt.Sprintf(...) call, ignoring
		// redundant parentheses around the argument or its callee.
		argCall, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.CallExpr)
		if !ok {
			return
		}
		argSel, ok := astutil.UnwrapParenExpr(argCall.Fun).(*ast.SelectorExpr)
		if !ok || argSel.Sel.Name != "Sprintf" {
			return
		}
		if !astutil.IsPkgSelector(pass, argSel, "fmt") {
			return
		}

		position := pass.Fset.PositionFor(call.Pos(), false)
		if nolint.HasDirectiveForLinter(position, noLintIndex, "sprintferrorsnew") {
			return
		}
		pass.Reportf(call.Pos(), "use fmt.Errorf instead of errors.New(fmt.Sprintf(...))")
	})
}
