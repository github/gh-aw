// Package appendbytestring implements a Go analysis linter that flags
// append(b, []byte(s)...) calls where b is []byte and s is a string,
// which can be simplified to append(b, s...) without the redundant conversion.
package appendbytestring

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

// Analyzer is the append-byte-string analysis pass.
var Analyzer = analyzerutil.New("appendbytestring", "reports append(b, []byte(s)...) calls where s is a string that can be simplified to append(b, s...)", run)

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
		analyzeAppendByteString(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeAppendByteString checks whether a call is an append(b, []byte(s)...)
// that can be simplified to append(b, s...) and reports a diagnostic if so.
func analyzeAppendByteString(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	// Match append(b, x...) with exactly 2 arguments and an ellipsis.
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "append" {
		return
	}
	if len(call.Args) != 2 || !call.Ellipsis.IsValid() {
		return
	}

	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "appendbytestring") {
		return
	}

	// The first argument must be []byte.
	if !astutil.IsByteSlice(pass, call.Args[0]) {
		return
	}

	// The second argument must be a []byte(s) conversion where s is a string.
	conv, ok := call.Args[1].(*ast.CallExpr)
	if !ok {
		return
	}
	if !astutil.IsByteSliceConversion(pass, conv) {
		return
	}
	if len(conv.Args) != 1 {
		return
	}
	strArg := conv.Args[0]
	if !astutil.IsStringType(pass, strArg) {
		return
	}

	sText := astutil.NodeText(pass.Fset, strArg)
	if sText == "" {
		return
	}
	if !coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
		return
	}

	pass.Report(analysis.Diagnostic{
		Pos:            call.Pos(),
		End:            call.End(),
		Message:        fmt.Sprintf("append(b, []byte(%s)...) can be simplified to append(b, %s...); the []byte conversion is unnecessary", sText, sText),
		SuggestedFixes: buildFix(pass, conv, strArg),
	})
}

// buildFix returns a SuggestedFix rewriting append(b, []byte(s)...) to append(b, s...).
func buildFix(pass *analysis.Pass, conv *ast.CallExpr, strArg ast.Expr) []analysis.SuggestedFix {
	sText := astutil.NodeText(pass.Fset, strArg)
	if sText == "" {
		return nil
	}
	if astutil.HasOverlappingComment(pass.Files, conv.Pos(), conv.End()) {
		return nil
	}
	// Replace the entire second argument []byte(s) with just s.
	// The ellipsis token follows the closing paren of the outer append call,
	// so we only need to rewrite conv.Pos()..conv.End() to sText.
	return []analysis.SuggestedFix{{
		Message: "Replace []byte(s) with s in append",
		TextEdits: []analysis.TextEdit{
			{
				Pos:     conv.Pos(),
				End:     conv.End(),
				NewText: []byte(sText),
			},
		},
	}}
}
