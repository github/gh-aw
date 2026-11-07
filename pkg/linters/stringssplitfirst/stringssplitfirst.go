// Package stringssplitfirst implements a Go analysis linter that reports
// strings.Split(s, sep)[0] calls with a constant non-empty separator, which
// allocate a slice to read only the first segment.
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
var Analyzer = analyzerutil.New("stringssplitfirst", "reports strings.Split(s, sep)[0] with a constant non-empty separator; use strings.Cut to read the first segment without allocating a slice", run)

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
		index, ok := n.(*ast.IndexExpr)
		if !ok || !isConstantInt(pass, index.Index, 0) {
			return
		}

		call, ok := astutil.UnwrapParenExpr(index.X).(*ast.CallExpr)
		if !ok || !isStringsSplit(pass, call) {
			return
		}

		pos := pass.Fset.PositionFor(index.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "stringssplitfirst") {
			return
		}
		if !coverage.ShouldApply(pass, index.Pos(), *hotThreshold) {
			return
		}

		pass.Report(analysis.Diagnostic{
			Pos:     index.Pos(),
			End:     index.End(),
			Message: "strings.Split(...)[0] allocates a []string just to read the first segment; use strings.Cut instead",
		})
	})
}

func isStringsSplit(pass *analysis.Pass, call *ast.CallExpr) bool {
	if _, ok := astutil.AsStringsMethodCall(pass, call, "Split"); !ok {
		return false
	}
	if len(call.Args) != 2 {
		return false
	}
	separator, ok := pass.TypesInfo.Types[call.Args[1]] //nolint:uncheckedsliceindex // len(call.Args) is checked above
	return ok && separator.Value != nil && separator.Value.Kind() == constant.String && constant.StringVal(separator.Value) != ""
}

func isConstantInt(pass *analysis.Pass, expr ast.Expr, want int64) bool {
	value, ok := astutil.ConstIntValue(pass, expr)
	return ok && value == want
}
