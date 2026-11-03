// Package splitlinesonnewline implements a Go analysis linter that flags
// strings.Split(s, "\n") calls. Splitting on "\n" leaves a trailing empty
// element for newline-terminated input and preserves "\r" on CRLF input;
// strings.Lines, strings.SplitSeq, or explicit CR/trailing-newline handling
// is usually what the caller wants.
package splitlinesonnewline

import (
	"go/ast"
	"go/constant"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the splitlines-on-newline analysis pass.
var Analyzer = analyzerutil.NewAtPath("splitlinesonnewline", "reports strings.Split(s, \"\\n\") calls that leave a trailing empty element and keep \"\\r\" on CRLF input; use strings.Lines or explicit CR and trailing-newline handling instead", "splitlines-on-newline", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return
		}
		sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Split" || !astutil.IsPkgSelector(pass, sel, "strings") {
			return
		}
		tv, ok := pass.TypesInfo.Types[call.Args[1]] //nolint:uncheckedsliceindex // len(call.Args) is checked above
		if !ok || tv.Value == nil || tv.Value.Kind() != constant.String || constant.StringVal(tv.Value) != "\n" {
			return
		}

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "splitlinesonnewline") {
			return
		}

		pass.Reportf(call.Pos(), "strings.Split(s, \"\\n\") leaves a trailing empty element and keeps \"\\r\" on CRLF input; use strings.Lines or handle CR and the trailing newline explicitly")
	})
}
