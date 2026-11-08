// Package jsonunmarshalbytesconversion reports string-to-byte conversions passed
// to json.Unmarshal.
package jsonunmarshalbytesconversion

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the json-unmarshal-bytes-conversion analysis pass.
var Analyzer = analyzerutil.New("jsonunmarshalbytesconversion", "reports json.Unmarshal calls that convert a string to []byte", run)

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
		if !ok || len(call.Args) != 2 {
			return
		}
		pkgPath, funcName, ok := astutil.PackageCall(pass, call)
		if !ok || pkgPath != "encoding/json" || funcName != "Unmarshal" {
			return
		}
		conversion, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.CallExpr) //nolint:uncheckedsliceindex // len(call.Args) == 2 is checked above.
		if !ok || len(conversion.Args) != 1 {
			return
		}
		source := conversion.Args[0] //nolint:uncheckedsliceindex // len(conversion.Args) == 1 is checked above.
		if !astutil.IsByteSliceConversion(pass, conversion) || !astutil.IsStringType(pass, source) {
			return
		}

		position := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(position, noLintIndex, "jsonunmarshalbytesconversion") ||
			!coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
			return
		}
		pass.ReportRangef(call, "json.Unmarshal([]byte(s), ...) copies the string; use json.NewDecoder(strings.NewReader(s)) for streams, or follow Decode with a second Decode that returns io.EOF to preserve Unmarshal's whole-document validation")
	})
}
