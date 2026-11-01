// Package stringssplitfirst implements a Go analysis linter that flags
// strings.Split(s, sep)[0] expressions, which allocate a whole slice only to
// keep the text before the first separator.
package stringssplitfirst

import (
	"go/ast"
	"go/constant"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the strings-split-first analysis pass.
var Analyzer = analyzerutil.New("stringssplitfirst", "reports strings.Split(s, sep)[0] expressions that allocate a slice only to take the text before the first separator and should use strings.Cut", run)

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

	nodeFilter := []ast.Node{(*ast.IndexExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeIndex(pass, n, generatedFiles, noLintIndex)
	})
}

func analyzeIndex(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	idx, ok := n.(*ast.IndexExpr)
	if !ok || !isConstInt(pass, idx.Index, 0) {
		return
	}
	call, ok := idx.X.(*ast.CallExpr)
	if !ok {
		return
	}
	splitCall, ok := astutil.AsStringsMethodCall(pass, call, "Split")
	if !ok || len(splitCall.Args) != 2 {
		return
	}
	// With an empty separator Split divides into characters, which Cut cannot replicate.
	if !isNonEmptyConstString(pass, splitCall.Args[1]) {
		return
	}

	pos := pass.Fset.PositionFor(idx.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "stringssplitfirst") {
		return
	}
	if !coverage.ShouldApply(pass, idx.Pos(), *hotThreshold) {
		return
	}

	pass.ReportRangef(idx, "strings.Split(...)[0] allocates a slice to take the first field; use strings.Cut instead")
}

func isConstInt(pass *analysis.Pass, expr ast.Expr, want int64) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.Int {
		return false
	}
	v, exact := constant.Int64Val(tv.Value)
	return exact && v == want
}

func isNonEmptyConstString(pass *analysis.Pass, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return false
	}
	return constant.StringVal(tv.Value) != ""
}
