// Package splitindexzero implements a Go analysis linter that flags
// strings.Split(s, sep)[0] and strings.SplitN(s, sep, 2)[0] expressions with a
// provably non-empty separator. They allocate a []string just to read the text
// before the first separator, which strings.Cut returns without allocating.
package splitindexzero

import (
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the splitindexzero analysis pass.
var Analyzer = analyzerutil.New("splitindexzero", "reports strings.Split(s, sep)[0] and strings.SplitN(s, sep, 2)[0] expressions with a non-empty separator that allocate a []string just to read the first segment; use strings.Cut instead", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.IndexExpr)(nil)}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		idx, ok := n.(*ast.IndexExpr)
		if !ok || !isConstInt(pass, idx.Index, 0) {
			return
		}
		call, ok := ast.Unparen(idx.X).(*ast.CallExpr)
		if !ok {
			return
		}
		name, ok := splitFuncName(pass, call)
		if !ok {
			return
		}
		if !hasNonEmptySeparator(pass, call) {
			return
		}
		if name == "SplitN" && !isConstInt(pass, call.Args[2], 2) {
			return
		}

		pos := pass.Fset.PositionFor(idx.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "splitindexzero") {
			return
		}

		pass.Report(analysis.Diagnostic{
			Pos:     idx.Pos(),
			End:     idx.End(),
			Message: "strings." + name + "(...)[0] allocates a []string just to read the first segment; use strings.Cut instead",
		})
	})
}

// splitFuncName returns "Split" or "SplitN" when call is a standard-library
// strings call with the argument count that function requires.
func splitFuncName(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !astutil.IsPkgSelector(pass, sel, "strings") {
		return "", false
	}
	switch {
	case sel.Sel.Name == "Split" && len(call.Args) == 2:
		return "Split", true
	case sel.Sel.Name == "SplitN" && len(call.Args) == 3:
		return "SplitN", true
	}
	return "", false
}

func hasNonEmptySeparator(pass *analysis.Pass, call *ast.CallExpr) bool {
	tv, ok := pass.TypesInfo.Types[call.Args[1]]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return false
	}
	return constant.StringVal(tv.Value) != ""
}

// isConstInt reports whether expr is a constant integer equal to want.
func isConstInt(pass *analysis.Pass, expr ast.Expr, want int64) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.Int {
		return false
	}
	if b, ok := tv.Type.Underlying().(*types.Basic); ok && b.Info()&types.IsInteger == 0 {
		return false
	}
	v, exact := constant.Int64Val(tv.Value)
	return exact && v == want
}
