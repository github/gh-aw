// Package readerstringconv reports string/[]byte conversions used only to
// construct a reader of the opposite kind.
package readerstringconv

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the reader-string-conversion analysis pass.
var Analyzer = analyzerutil.New("readerstringconv", "reports strings.NewReader(string(b)) and bytes.NewReader([]byte(s)) calls that copy data only to change reader type", run)

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

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return
		}
		pkgPath, funcName, ok := astutil.PackageCall(pass, call)
		if !ok || funcName != "NewReader" || (pkgPath != "strings" && pkgPath != "bytes") {
			return
		}
		conv, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.CallExpr) //nolint:uncheckedsliceindex // len(call.Args) == 1 is checked above.
		if !ok || len(conv.Args) != 1 {
			return
		}
		if !pass.TypesInfo.Types[astutil.UnwrapParenExpr(conv.Fun)].IsType() {
			return
		}
		source := conv.Args[0] //nolint:uncheckedsliceindex // len(conv.Args) == 1 is checked above.

		var message string
		switch {
		case pkgPath == "strings" && astutil.IsStringType(pass, conv) && astutil.IsByteSlice(pass, source):
			message = "use bytes.NewReader instead of converting []byte to string for strings.NewReader"
		case pkgPath == "bytes" && astutil.IsByteSlice(pass, conv) && astutil.IsStringType(pass, source):
			message = "use strings.NewReader instead of converting string to []byte for bytes.NewReader"
		default:
			return
		}

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(pos, noLintIndex, "readerstringconv") ||
			!coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
			return
		}
		pass.ReportRangef(call, "%s", message)
	})
}
