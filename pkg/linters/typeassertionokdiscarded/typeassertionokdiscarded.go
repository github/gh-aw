// Package typeassertionokdiscarded implements a Go analysis linter that flags
// type assertions in the two-value form where the ok return is explicitly
// discarded via blank identifier, which can silently accept a zero value.
package typeassertionokdiscarded

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the type-assertion-ok-discarded analysis pass.
var Analyzer = analyzerutil.New("typeassertionokdiscarded", "reports type assertions using the two-value form where the ok return is explicitly discarded via blank identifier, which can silently accept a zero value", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	// Build a parent map for each file so we can detect two-value assignments.
	fileParents := make(map[*ast.File]map[ast.Node]ast.Node)
	for _, f := range pass.Files {
		fileParents[f] = astutil.BuildParentMap(f)
	}

	nodeFilter := []ast.Node{
		(*ast.TypeAssertExpr)(nil),
	}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		inspectTypeAssertExpr(pass, noLintIndex, generatedFiles, fileParents, n)
	})
}

func inspectTypeAssertExpr(pass *analysis.Pass, noLintIndex nolint.DirectiveIndex, generatedFiles filecheck.GeneratedIndex, fileParents map[*ast.File]map[ast.Node]ast.Node, n ast.Node) {
	typeAssert, ok := n.(*ast.TypeAssertExpr)
	if !ok {
		return
	}

	// Type-switch guards have nil Type; skip them.
	if typeAssert.Type == nil {
		return
	}

	pos := pass.Fset.PositionFor(typeAssert.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}

	// Find the parent map for the file containing this node.
	f := astutil.FileForPos(pass.Files, typeAssert.Pos())
	var parents map[ast.Node]ast.Node
	if f != nil {
		parents = fileParents[f]
	}

	// Check if this is a two-value assignment where the ok is blank.
	if parents != nil {
		if isTwoValueBlankOkAssertion(typeAssert, parents) {
			if nolint.HasDirectiveForLinter(pos, noLintIndex, "typeassertionokdiscarded") {
				return
			}

			t := pass.TypesInfo.TypeOf(typeAssert.Type)
			if t == nil {
				return
			}

			pass.ReportRangef(
				typeAssert,
				"type assertion ok value is explicitly discarded with blank identifier; use single-value form x.(%s) or check the ok value instead",
				t,
			)
		}
	}
}

func isTwoValueBlankOkAssertion(typeAssert *ast.TypeAssertExpr, parents map[ast.Node]ast.Node) bool {
	okIdent, isTwoValue := astutil.TwoValueTypeAssertionOKIdent(typeAssert, parents)
	return isTwoValue && okIdent.Name == "_"
}
