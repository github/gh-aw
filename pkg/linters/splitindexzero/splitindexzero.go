// Package splitindexzero implements a Go analysis linter that flags
// strings.Split(s, sep)[0] expressions with a provably non-empty separator.
// The call allocates a []string only to read the first element;
// strings.Cut(s, sep) returns the same prefix without the allocation.
package splitindexzero

import (
	"go/ast"
	"go/constant"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the split-index-zero analysis pass.
var Analyzer = analyzerutil.New("splitindexzero", "reports strings.Split(s, sep)[0] expressions with a provably non-empty separator that allocate a []string just to read the first element; use strings.Cut(s, sep) instead", run)

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
		index, ok := n.(*ast.IndexExpr)
		if !ok {
			return
		}
		if !isConstZero(pass, index.Index) {
			return
		}
		split, ok := astutil.AsStringsMethodCall(pass, index.X, "Split")
		if !ok || !hasNonEmptySeparator(pass, split) {
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
			Message: "strings.Split(...)[0] allocates a []string just to read the first element; use strings.Cut(...) and take the first result instead",
		})
	})
}

// isConstZero reports whether expr is the constant integer 0.
func isConstZero(pass *analysis.Pass, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.Int {
		return false
	}
	return constant.Compare(tv.Value, token.EQL, constant.MakeInt64(0))
}

// hasNonEmptySeparator reports whether the separator argument is a non-empty
// constant string. An empty separator splits into characters, which Cut does not replicate.
func hasNonEmptySeparator(pass *analysis.Pass, call *ast.CallExpr) bool {
	if len(call.Args) != 2 {
		return false
	}
	tv, ok := pass.TypesInfo.Types[call.Args[1]]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return false
	}
	return constant.StringVal(tv.Value) != ""
}
