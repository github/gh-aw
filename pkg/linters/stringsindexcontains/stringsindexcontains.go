// Package stringsindexcontains implements a Go analysis linter that flags
// strings.Index(s, substr) comparisons with -1 or 0 (e.g. != -1, >= 0, > -1,
// == -1, < 0, <= -1) and their yoda-order variants that should use the more
// readable strings.Contains(s, substr) or !strings.Contains(s, substr) instead.
package stringsindexcontains

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the strings-index-contains analysis pass.
var Analyzer = analyzerutil.New("stringsindexcontains", "reports strings.Index(s, substr) comparisons with -1 or 0 (e.g. != -1, >= 0, > -1, == -1, < 0, <= -1) and their yoda-order variants that should use strings.Contains(s, substr) or !strings.Contains(s, substr)", run)

var indexContainsComparisons = []astutil.StringMethodComparison{
	{Op: token.NEQ, Value: -1},
	{Op: token.GEQ, Value: 0},
	{Op: token.GTR, Value: -1},
	{Op: token.EQL, Value: -1, Negated: true},
	{Op: token.LSS, Value: 0, Negated: true},
	{Op: token.LEQ, Value: -1, Negated: true},
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.BinaryExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeIndexContains(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeIndexContains checks whether a binary expression is a strings.Index
// comparison with -1 or 0 that should use strings.Contains.
func analyzeIndexContains(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	expr, ok := n.(*ast.BinaryExpr)
	if !ok {
		return
	}
	pos := pass.Fset.PositionFor(expr.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "stringsindexcontains") {
		return
	}
	astutil.ReportStringsMethodComparison(pass, expr, "Index", "Contains", indexContainsComparisons)
}
