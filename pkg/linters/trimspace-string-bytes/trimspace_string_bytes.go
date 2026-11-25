// Package trimspacestringbytes implements an analyzer for strings.TrimSpace
// calls that wrap a []byte-to-string conversion.
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

// Analyzer reports strings.TrimSpace(string(b)) calls where b is a []byte.
var Analyzer = analyzerutil.NewAtPath(
	"trimspacestringbytes",
	"reports strings.TrimSpace(string(b)) on a []byte b, which allocates an intermediate string that bytes.TrimSpace avoids",
	"trimspace-string-bytes",
	run,
)

var hotThreshold *int

func init() {
	hotThreshold = coverage.RegisterHotThresholdFlag(Analyzer)
}

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
		trimCall, ok := astutil.AsStringsMethodCall(pass, call, "TrimSpace")
		if !ok || len(trimCall.Args) != 1 {
			return
		}
		conv, ok := astutil.UnwrapParenExpr(trimCall.Args[0]).(*ast.CallExpr) //nolint:uncheckedsliceindex // length checked above
		if !ok || len(conv.Args) != 1 || !astutil.IsStringConversion(pass, conv) {
			return
		}
		if !astutil.IsByteSlice(pass, conv.Args[0]) { //nolint:uncheckedsliceindex // length checked above
			return
		}

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "trimspacestringbytes") {
			return
		}
		if !coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
			return
		}

		pass.Report(analysis.Diagnostic{
			Pos:     call.Pos(),
			End:     call.End(),
			Message: "strings.TrimSpace(string(b)) converts a []byte to an intermediate string; use bytes.TrimSpace(b) and convert the result only if a string is needed",
		})
	})
}
