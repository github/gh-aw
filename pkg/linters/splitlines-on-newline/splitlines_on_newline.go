// Package splitlinesonnewline implements an analyzer for strings.Split calls
// that use a newline separator.
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

// Analyzer reports strings.Split calls that use a newline separator.
var Analyzer = analyzerutil.NewAtPath(
	"splitlinesonnewline",
	"reports strings.Split calls with a newline separator that can retain carriage returns for CRLF input and add a trailing empty element",
	"splitlines-on-newline",
	run,
)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		stringsCall, ok := astutil.AsStringsMethodCall(pass, call, "Split")
		if !ok || len(stringsCall.Args) != 2 || !hasNewlineSeparator(pass, stringsCall) {
			return
		}

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "splitlinesonnewline") {
			return
		}

		pass.Report(analysis.Diagnostic{
			Pos:     call.Pos(),
			End:     call.End(),
			Message: "strings.Split with a newline separator can retain carriage returns on CRLF input and produce a trailing empty element; normalize line endings or trim carriage returns from each line",
		})
	})
}

func hasNewlineSeparator(pass *analysis.Pass, call *ast.CallExpr) bool {
	tv, ok := pass.TypesInfo.Types[call.Args[1]] //nolint:uncheckedsliceindex // call is checked for exactly two arguments above
	return ok && tv.Value != nil && tv.Value.Kind() == constant.String && constant.StringVal(tv.Value) == "\n"
}
