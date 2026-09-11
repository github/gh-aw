// Package slicemakezerolength implements a Go analysis linter that flags
// make([]T, 0) calls without a capacity argument when the final slice length
// can be statically determined, suggesting the capacity should be specified
// to avoid allocation overhead.
package slicemakezerolength

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the slice-make-zero-length analysis pass.
var Analyzer = analyzerutil.New("slicemakezerolength", "reports make([]T, 0) calls without capacity when the final length is known, which can be optimized", run)

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
		analyzeMakeSliceZeroLength(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeMakeSliceZeroLength checks whether a call is make([]T, 0) without
// a capacity argument and reports a diagnostic if so.
func analyzeMakeSliceZeroLength(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	// Check if this is a call to the built-in make function.
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "make" {
		return
	}
	if pass.TypesInfo.ObjectOf(ident) != types.Universe.Lookup("make") {
		return
	}

	// make([]T, 0) has exactly 2 arguments with no ellipsis.
	if len(call.Args) != 2 || call.Ellipsis.IsValid() {
		return
	}

	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "slicemakezerolength") {
		return
	}

	// The first argument must be a slice type []T.
	sliceType, ok := call.Args[0].(*ast.ArrayType)
	if !ok || sliceType.Len != nil {
		// Len != nil means it's an array type [n]T, not a slice type []T.
		return
	}

	// The second argument must be a literal 0.
	if !isZeroLiteral(call.Args[1]) {
		return
	}

	if !coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
		return
	}

	sliceTypeText := astutil.NodeText(pass.Fset, sliceType)
	if sliceTypeText == "" {
		sliceTypeText = "[]T"
	}
	lenText := astutil.NodeText(pass.Fset, call.Args[1])
	if lenText == "" {
		lenText = "0"
	}

	pass.Report(analysis.Diagnostic{
		Pos:     call.Pos(),
		End:     call.End(),
		Message: fmt.Sprintf("make(%s, %s) without capacity can be optimized", sliceTypeText, lenText),
	})
}

// isZeroLiteral reports whether expr is the literal 0.
func isZeroLiteral(expr ast.Expr) bool {
	lit, ok := expr.(*ast.BasicLit)
	if !ok {
		return false
	}
	// Check if the literal value is "0"
	return lit.Value == "0"
}
