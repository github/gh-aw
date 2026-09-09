// Package appendoneelement implements a Go analysis linter that flags
// append(s, []T{x}...) calls where a single-element slice literal is
// spread, which can be simplified to append(s, x).
package appendoneelement

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the append-one-element analysis pass.
var Analyzer = analyzerutil.New("appendoneelement", "reports append(s, []T{x}...) calls where a single-element slice literal is spread and can be simplified to append(s, x)", run)

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
		analyzeAppendOneElement(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeAppendOneElement checks whether a call is an append(s, []T{x}...) that
// can be simplified to append(s, x) and reports a diagnostic if so.
func analyzeAppendOneElement(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "append" {
		return
	}
	if pass.TypesInfo.ObjectOf(ident) != types.Universe.Lookup("append") {
		return
	}
	if len(call.Args) != 2 || !call.Ellipsis.IsValid() {
		return
	}

	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "appendoneelement") {
		return
	}

	sliceText, elemText, litText, ok := matchSingleElementSpread(pass, call)
	if !ok {
		return
	}
	if !coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
		return
	}

	diag := analysis.Diagnostic{
		Pos:     call.Pos(),
		End:     call.End(),
		Message: fmt.Sprintf("append(s, %s...) can be simplified to append(s, %s)", litText, elemText),
	}
	if !astutil.HasOverlappingComment(pass.Files, call.Pos(), call.End()) {
		diag.SuggestedFixes = []analysis.SuggestedFix{{
			Message: fmt.Sprintf("Replace %s... with %s", litText, elemText),
			TextEdits: []analysis.TextEdit{{
				Pos:     call.Pos(),
				End:     call.End(),
				NewText: fmt.Appendf(nil, "append(%s, %s)", sliceText, elemText),
			}},
		}}
	}
	pass.Report(diag)
}

// matchSingleElementSpread validates that call.Args[1] is a single-element slice
// literal spread and returns the text representations of slice, element, and literal.
func matchSingleElementSpread(pass *analysis.Pass, call *ast.CallExpr) (sliceText, elemText, litText string, ok bool) {
	lit, litOK := call.Args[1].(*ast.CompositeLit)
	if !litOK {
		return "", "", "", false
	}
	arrayType, atOK := lit.Type.(*ast.ArrayType)
	if !atOK || arrayType.Len != nil {
		return "", "", "", false
	}
	if len(lit.Elts) != 1 {
		return "", "", "", false
	}

	elem := lit.Elts[0]
	if _, ok := elem.(*ast.KeyValueExpr); ok {
		return "", "", "", false
	}
	if nestedLit, ok := elem.(*ast.CompositeLit); ok && nestedLit.Type == nil {
		return "", "", "", false
	}

	elemText = astutil.NodeText(pass.Fset, elem)
	if elemText == "" {
		return "", "", "", false
	}
	litText = astutil.NodeText(pass.Fset, lit)
	if litText == "" {
		return "", "", "", false
	}
	sliceText = astutil.NodeText(pass.Fset, call.Args[0])
	if sliceText == "" {
		return "", "", "", false
	}
	return sliceText, elemText, litText, true
}
