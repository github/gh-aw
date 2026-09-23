// Package stringsplitseq implements a Go analysis linter that flags
// strings.Split() calls used directly in range loops that should use
// strings.SplitSeq() for iteration without allocation (available in Go 1.22+).
package stringsplitseq

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the string-split-seq analysis pass.
var Analyzer = analyzerutil.New("stringsplitseq", "reports strings.Split() calls used directly in range loops that should use strings.SplitSeq() for iteration without allocation", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.RangeStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeRangeStmt(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeRangeStmt checks whether a range statement iterates directly over
// strings.Split() and reports a diagnostic if so.
func analyzeRangeStmt(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	rangeStmt, ok := n.(*ast.RangeStmt)
	if !ok {
		return
	}

	// The range operand must be a call to strings.Split
	call, ok := rangeStmt.X.(*ast.CallExpr)
	if !ok {
		return
	}

	if !isStringsSplit(pass, call) {
		return
	}

	// Check if it's already SplitSeq (shouldn't flag)
	if isStringsSplitSeq(pass, call) {
		return
	}

	pos := pass.Fset.PositionFor(rangeStmt.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}

	if nolint.HasDirectiveForLinter(pos, noLintIndex, "stringsplitseq") {
		return
	}

	pass.Report(analysis.Diagnostic{
		Pos:            call.Pos(),
		End:            call.End(),
		Message:        "strings.Split() allocates a slice for iteration; use strings.SplitSeq() instead (available in Go 1.22+)",
		SuggestedFixes: buildFix(pass, call),
	})
}

// isStringsSplit reports whether call is strings.Split from the standard
// library "strings" package.
func isStringsSplit(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Split" {
		return false
	}
	return astutil.IsPkgSelector(pass, sel, "strings")
}

// isStringsSplitSeq reports whether call is strings.SplitSeq from the standard
// library "strings" package.
func isStringsSplitSeq(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "SplitSeq" {
		return false
	}
	return astutil.IsPkgSelector(pass, sel, "strings")
}

// buildFix returns a SuggestedFix rewriting strings.Split to strings.SplitSeq.
func buildFix(pass *analysis.Pass, call *ast.CallExpr) []analysis.SuggestedFix {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	if len(call.Args) != 2 {
		return nil
	}

	if astutil.HasOverlappingComment(pass.Files, call.Pos(), call.End()) {
		return nil
	}

	pkgText := astutil.NodeText(pass.Fset, sel.X)
	if pkgText == "" {
		return nil
	}

	sText := astutil.NodeText(pass.Fset, call.Args[0]) //nolint:uncheckedsliceindex
	sepText := astutil.NodeText(pass.Fset, call.Args[1]) //nolint:uncheckedsliceindex
	if sText == "" || sepText == "" {
		return nil
	}

	return []analysis.SuggestedFix{{
		Message: "Replace strings.Split with strings.SplitSeq",
		TextEdits: []analysis.TextEdit{{
			Pos:     call.Pos(),
			End:     call.End(),
			NewText: fmt.Appendf(nil, "%s.SplitSeq(%s, %s)", pkgText, sText, sepText),
		}},
	}}
}
