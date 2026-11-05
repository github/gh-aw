// Package jsonunmarshalbytesconversion implements a Go analysis linter that
// flags json.Unmarshal([]byte(s), ...) calls where s is a string.
package jsonunmarshalbytesconversion

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the json-unmarshal-bytes-conversion analysis pass.
var Analyzer = analyzerutil.NewAtPath("jsonunmarshalbytesconversion", "reports json.Unmarshal([]byte(s), ...) calls where s is a string, which copy the string; use json.NewDecoder(strings.NewReader(s)) or retain the bytes", "json-unmarshal-bytes-conversion", run)

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
		analyze(pass, n, generatedFiles, noLintIndex)
	})
}

func analyze(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return
	}
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Unmarshal" {
		return
	}
	if !astutil.IsPkgSelector(pass, sel, "encoding/json") {
		return
	}

	conv, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.CallExpr)
	if !ok || len(conv.Args) != 1 || !astutil.IsByteSliceConversion(pass, conv) {
		return
	}
	if !astutil.IsStringType(pass, conv.Args[0]) {
		return
	}

	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "jsonunmarshalbytesconversion") {
		return
	}
	if !coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
		return
	}

	pass.Reportf(call.Pos(), "json.Unmarshal([]byte(%s), ...) copies the string; use json.NewDecoder(strings.NewReader(%s)) or retain the data as []byte",
		astutil.NodeText(pass.Fset, conv.Args[0]), astutil.NodeText(pass.Fset, conv.Args[0]))
}
