// Package splitindexzero implements a Go analysis linter that reports
// strings.Split(s, sep)[0] and strings.SplitN(s, sep, 2)[0] calls, which
// allocate a slice to read only the first segment.
package splitindexzero

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

// Analyzer is the split-index-zero analysis pass.
var Analyzer = analyzerutil.New("splitindexzero", "reports strings.Split(s, sep)[0] and strings.SplitN(s, sep, 2)[0] calls that allocate a slice to read only the first segment; use strings.Cut instead", run)

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

		if !isSplitCall(pass, index.X) {
			return
		}

		pos := pass.Fset.PositionFor(index.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "splitindexzero") {
			return
		}
		if !coverage.ShouldApply(pass, index.Pos(), *hotThreshold) {
			return
		}

		pass.Report(analysis.Diagnostic{
			Pos:     index.Pos(),
			End:     index.End(),
			Message: "strings.Split/SplitN(...)[0] allocates a []string just to read the first segment; use strings.Cut instead",
		})
	})
}

func isSplitCall(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || !astutil.IsPkgSelector(pass, sel, "strings") {
		return false
	}

	switch sel.Sel.Name {
	case "Split":
		if len(call.Args) != 2 {
			return false
		}
		return hasNonEmptyStringArgument(pass, call.Args[1]) //nolint:uncheckedsliceindex // len(call.Args) is checked above
	case "SplitN":
		if len(call.Args) != 3 {
			return false
		}
		if !hasNonEmptyStringArgument(pass, call.Args[1]) { //nolint:uncheckedsliceindex // len(call.Args) is checked above
			return false
		}
		return isConstantInt(pass, call.Args[2], 2) //nolint:uncheckedsliceindex // len(call.Args) is checked above
	default:
		return false
	}
}

func hasNonEmptyStringArgument(pass *analysis.Pass, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	return ok && tv.Value != nil && tv.Value.Kind() == constant.String && constant.StringVal(tv.Value) != ""
}

func isConstantInt(pass *analysis.Pass, expr ast.Expr, want int64) bool {
	value, ok := astutil.ConstIntValue(pass, expr)
	return ok && value == want
}
