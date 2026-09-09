// Package stringsjoinone implements a Go analysis linter that flags
// strings.Join([]string{s}, sep) calls with a single-element slice literal,
// where the separator is never used and the call is equivalent to just s.
package stringsjoinone

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the strings-join-one analysis pass.
var Analyzer = analyzerutil.New("stringsjoinone", "reports strings.Join([]string{s}, sep) calls with a single-element slice literal where the separator is never used and the call is equivalent to just s", run)

// hotThreshold gates findings on coverage data; see coverage package docs.
var hotThreshold *int

func init() {
	hotThreshold = coverage.RegisterHotThresholdFlag(Analyzer)
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeJoinOne(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeJoinOne checks whether a call is strings.Join with a single-element
// []string literal and reports a diagnostic if so.
func analyzeJoinOne(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	joinCall, ok := astutil.AsStringsMethodCall(pass, call, "Join")
	if !ok {
		return
	}
	if len(joinCall.Args) != 2 {
		return
	}

	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "stringsjoinone") {
		return
	}

	elem, elemText, ok := matchSingleElementStringSlice(pass, joinCall.Args[0])
	if !ok {
		return
	}
	replacementText := formatReplacementText(elem, elemText)
	// Only flag when the separator is a compile-time constant so that the
	// suggested fix does not silently drop observable side effects.  For
	// example, strings.Join([]string{s}, <-ch) receives from a channel before
	// returning; replacing it with just s would remove that receive.
	if !isSafeToDiscardSeparator(pass, joinCall.Args[1]) {
		return
	}
	if !coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
		return
	}

	diag := analysis.Diagnostic{
		Pos:     call.Pos(),
		End:     call.End(),
		Message: fmt.Sprintf("strings.Join called with a single-element slice; use %s directly", replacementText),
	}
	if !astutil.HasOverlappingComment(pass.Files, call.Pos(), call.End()) {
		diag.SuggestedFixes = []analysis.SuggestedFix{{
			Message: "Replace strings.Join call with " + replacementText,
			TextEdits: []analysis.TextEdit{{
				Pos:     call.Pos(),
				End:     call.End(),
				NewText: []byte(replacementText),
			}},
		}}
	}
	pass.Report(diag)
}

// isSafeToDiscardSeparator reports whether sep is a compile-time constant and
// therefore safe to discard.  Expressions with run-time side effects (channel
// receives, function calls, non-constant variable reads, etc.) must not be
// silently dropped by a suggested fix.
func isSafeToDiscardSeparator(pass *analysis.Pass, sep ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[sep]
	return ok && tv.Value != nil
}

// matchSingleElementStringSlice reports whether expr is a []string{...} composite
// literal with exactly one element and returns the text of that element.
func matchSingleElementStringSlice(pass *analysis.Pass, expr ast.Expr) (elem ast.Expr, elemText string, ok bool) {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return nil, "", false
	}
	// The type must be []string (array type with no length, element type "string").
	arrayType, ok := lit.Type.(*ast.ArrayType)
	if !ok || arrayType.Len != nil {
		return nil, "", false
	}
	ident, ok := arrayType.Elt.(*ast.Ident)
	if !ok || ident.Name != "string" {
		return nil, "", false
	}
	if len(lit.Elts) != 1 {
		return nil, "", false
	}
	elem = lit.Elts[0]
	if _, isKV := elem.(*ast.KeyValueExpr); isKV {
		return nil, "", false
	}
	text := astutil.NodeText(pass.Fset, elem)
	if text == "" {
		return nil, "", false
	}
	return elem, text, true
}

func formatReplacementText(elem ast.Expr, elemText string) string {
	switch elem.(type) {
	case *ast.Ident, *ast.BasicLit, *ast.ParenExpr, *ast.SelectorExpr, *ast.IndexExpr, *ast.CallExpr:
		return elemText
	default:
		return "(" + elemText + ")"
	}
}
