// Package uncheckedtypeassertion implements a Go analysis linter that flags
// single-value type assertions x.(T) that may panic at runtime if the dynamic
// type does not match, and where the two-value safe form x.(T) is not used.
package uncheckedtypeassertion

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:uncheckedtypeassertion")

// Analyzer is the unchecked-type-assertion analysis pass.
var Analyzer = analyzerutil.New("uncheckedtypeassertion", "reports single-value type assertions that may panic if the dynamic type does not match", run)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	// Build a parent map for each file so we can detect the two-value form.
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

	// Skip the safe two-value form:  v, ok := x.(T)  or  v, ok = x.(T)
	if parents != nil {
		if isSafeTwoValueAssertion(typeAssert, parents) {
			return
		}
	}

	t := pass.TypesInfo.TypeOf(typeAssert.Type)
	if t == nil {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "uncheckedtypeassertion") {
		return
	}

	pkgLog.Printf("flagging unchecked type assertion to %s at %s", t, pos)
	pass.ReportRangef(
		typeAssert,
		"type assertion x.(%s) is unchecked and may panic; use the two-value form v, ok := x.(%s) instead",
		t, t,
	)
}

func isSafeTwoValueAssertion(typeAssert *ast.TypeAssertExpr, parents map[ast.Node]ast.Node) bool {
	_, isTwoValue := astutil.TwoValueTypeAssertionOKIdent(typeAssert, parents)
	return isTwoValue
}
