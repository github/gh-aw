// Package splitindexzeroprefercut implements a Go analysis linter that flags
// strings.Split(s, sep)[0] expressions with a provably non-empty separator.
// They allocate a slice of every substring when only the prefix before the
// first separator is needed; strings.Cut returns that prefix without the
// intermediate allocation.
package splitindexzeroprefercut

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

// Analyzer is the split-index-zero-prefer-cut analysis pass.
var Analyzer = analyzerutil.NewAtPath("splitindexzeroprefercut", "reports strings.Split(s, sep)[0] expressions with a provably non-empty separator that allocate all substrings when only the first is used; use strings.Cut instead", "split-index-zero-prefer-cut", run)

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
		idx, ok := n.(*ast.IndexExpr)
		if !ok {
			return
		}
		if !isConstZero(pass, idx.Index) {
			return
		}
		call, ok := astutil.UnwrapParenExpr(idx.X).(*ast.CallExpr)
		if !ok || !isStringsSplit(pass, call) {
			return
		}
		if !hasProvablyNonEmptySeparator(pass, call) {
			return
		}

		pos := pass.Fset.PositionFor(idx.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "splitindexzeroprefercut") {
			return
		}
		if !coverage.ShouldApply(pass, idx.Pos(), *hotThreshold) {
			return
		}

		pass.Reportf(idx.Pos(), "strings.Split(...)[0] allocates all substrings to use only the first; use strings.Cut instead")
	})
}

func isConstZero(pass *analysis.Pass, e ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.Int {
		return false
	}
	v, exact := constant.Int64Val(tv.Value)
	return exact && v == 0
}

func hasProvablyNonEmptySeparator(pass *analysis.Pass, call *ast.CallExpr) bool {
	if len(call.Args) != 2 {
		return false
	}
	tv, ok := pass.TypesInfo.Types[call.Args[1]]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return false
	}
	return constant.StringVal(tv.Value) != ""
}

func isStringsSplit(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Split" {
		return false
	}
	return astutil.IsPkgSelector(pass, sel, "strings")
}
