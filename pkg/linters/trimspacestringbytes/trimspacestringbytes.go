// Package trimspacestringbytes reports byte-to-string conversions before trimming.
package trimspacestringbytes

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const analyzerName = "trimspacestringbytes"

// Analyzer reports strings.TrimSpace calls that convert a byte slice to a string.
var Analyzer = analyzerutil.New(analyzerName, "reports strings.TrimSpace(string(b)) for byte slices", run)

var hotThreshold *int

func init() {
	hotThreshold = coverage.RegisterHotThresholdFlag(Analyzer)
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	return analyzerutil.Preorder(pass, []ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		if len(call.Args) != 1 || call.Ellipsis.IsValid() {
			return
		}
		pkgPath, funcName, ok := astutil.PackageCall(pass, call)
		if !ok || pkgPath != "strings" || funcName != "TrimSpace" {
			return
		}
		conv, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.CallExpr) //nolint:uncheckedsliceindex // len(call.Args) == 1 is checked above.
		if !ok || len(conv.Args) != 1 || conv.Ellipsis.IsValid() ||
			!astutil.IsStringConversion(pass, conv) || !astutil.IsByteSlice(pass, conv.Args[0]) { //nolint:uncheckedsliceindex // len(conv.Args) == 1 is checked above.
			return
		}

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(pos, noLintIndex, analyzerName) ||
			!coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
			return
		}
		pass.ReportRangef(call, "strings.TrimSpace(string(b)) copies the byte slice before trimming; consider bytes.TrimSpace(b), converting the result to string if needed")
	})
}
