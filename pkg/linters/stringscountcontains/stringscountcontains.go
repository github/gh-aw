// Package stringscountcontains implements a Go analysis linter that flags
// strings.Count(s, sub) comparisons with 0 or 1 (e.g. > 0, >= 1, == 0,
// != 0, < 1, <= 0) and their yoda-order variants that should use the more
// readable strings.Contains(s, sub) or !strings.Contains(s, sub) instead.
package stringscountcontains

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the strings-count-contains analysis pass.
var Analyzer = analyzerutil.New("stringscountcontains", "reports strings.Count(s, sub) comparisons with 0 or 1 (e.g. > 0, >= 1, == 0, != 0, < 1, <= 0) and their yoda-order variants that should use strings.Contains(s, sub) or !strings.Contains(s, sub)", run)

var countContainsComparisons = []astutil.StringMethodComparison{
	{Op: token.GTR, Value: 0},
	{Op: token.GEQ, Value: 1},
	{Op: token.NEQ, Value: 0},
	{Op: token.EQL, Value: 0, Negated: true},
	{Op: token.LSS, Value: 1, Negated: true},
	{Op: token.LEQ, Value: 0, Negated: true},
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.BinaryExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeCountContains(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeCountContains checks whether a binary expression is a strings.Count
// comparison with 0 or 1 that should use strings.Contains.
func analyzeCountContains(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	expr, ok := n.(*ast.BinaryExpr)
	if !ok {
		return
	}
	pos := pass.Fset.PositionFor(expr.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "stringscountcontains") {
		return
	}
	astutil.ReportStringsMethodComparison(pass, expr, "Count", "Contains", countContainsComparisons)
}
