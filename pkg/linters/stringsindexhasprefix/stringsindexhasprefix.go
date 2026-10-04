// Package stringsindexhasprefix implements a Go analysis linter that flags
// strings.Index(s, sub) comparisons with 0 (== 0 and != 0) and their yoda-order
// variants that should use the more readable strings.HasPrefix(s, sub) or
// !strings.HasPrefix(s, sub) instead.
package stringsindexhasprefix

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the strings-index-hasprefix analysis pass.
var Analyzer = analyzerutil.New("stringsindexhasprefix", "reports strings.Index(s, sub) comparisons with 0 (== 0 and != 0) and their yoda-order variants that should use strings.HasPrefix(s, sub) or !strings.HasPrefix(s, sub)", run)

var indexHasPrefixComparisons = []astutil.StringMethodComparison{
	{Op: token.EQL, Value: 0},
	{Op: token.NEQ, Value: 0, Negated: true},
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.BinaryExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeIndexHasPrefix(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeIndexHasPrefix checks whether a binary expression is a strings.Index
// comparison with 0 that should use strings.HasPrefix.
func analyzeIndexHasPrefix(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	expr, ok := n.(*ast.BinaryExpr)
	if !ok {
		return
	}
	pos := pass.Fset.PositionFor(expr.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "stringsindexhasprefix") {
		return
	}
	astutil.ReportStringsMethodComparison(pass, expr, "Index", "HasPrefix", indexHasPrefixComparisons)
}
