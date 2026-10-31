// Package stringbytesroundtrip implements a Go analysis linter that flags two
// related but semantically distinct patterns:
//   - string([]byte(s)) when the result and s have the predeclared string type:
//     genuinely redundant — the result is value-identical to s and both
//     conversions can be removed. For named string types, the inner conversion
//     can still be removed, but an outer conversion may be necessary.
//   - []byte(string(b)) when b is already a []byte: not redundant but wasteful
//     — this is the defensive-copy idiom that produces a non-aliasing clone via
//     two copies; prefer slices.Clone(b) or bytes.Clone(b) for a single copy.
package stringbytesroundtrip

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the string-bytes-roundtrip analysis pass.
var Analyzer = analyzerutil.New("stringbytesroundtrip", "reports string([]byte(s)) as a redundant round-trip when s is already a string, and []byte(string(b)) as a wasteful two-copy clone when b is already a []byte (prefer slices.Clone or bytes.Clone)", run)

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
		analyzeRoundTrip(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeRoundTrip checks whether a conversion expression is a redundant
// string/[]byte round-trip (string([]byte(s))) or a wasteful two-copy clone
// ([]byte(string(b))) and reports a diagnostic if so.
func analyzeRoundTrip(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	outer, inner, ok := roundTripCalls(n)
	if !ok || shouldSkipRoundTrip(pass, outer, generatedFiles, noLintIndex) {
		return
	}
	outerUnderlying, innerUnderlying, innerArgUnderlying, ok := roundTripUnderlyingTypes(pass, outer, inner)
	if !ok {
		return
	}
	if !coverage.ShouldApply(pass, outer.Pos(), *hotThreshold) {
		return
	}
	if reportRedundantRoundTrip(pass, outer, inner, outerUnderlying, innerUnderlying, innerArgUnderlying) {
		return
	}
	reportWastefulCloneRoundTrip(pass, outer, inner, outerUnderlying, innerUnderlying, innerArgUnderlying)
}

func roundTripCalls(n ast.Node) (*ast.CallExpr, *ast.CallExpr, bool) {
	outer, ok := n.(*ast.CallExpr)
	if !ok || len(outer.Args) != 1 || outer.Ellipsis.IsValid() {
		return nil, nil, false
	}
	// astutil.UnwrapParenExpr handles a parenthesized inner conversion, e.g.
	// string(([]byte(s))).
	inner, ok := astutil.UnwrapParenExpr(outer.Args[0]).(*ast.CallExpr)
	if !ok || len(inner.Args) != 1 || inner.Ellipsis.IsValid() {
		return nil, nil, false
	}
	return outer, inner, true
}

func shouldSkipRoundTrip(pass *analysis.Pass, outer *ast.CallExpr, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) bool {
	pos := pass.Fset.PositionFor(outer.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return true
	}
	return nolint.HasDirectiveForLinter(pos, noLintIndex, "stringbytesroundtrip")
}

func roundTripUnderlyingTypes(pass *analysis.Pass, outer, inner *ast.CallExpr) (types.Type, types.Type, types.Type, bool) {
	outerFunInfo, ok := pass.TypesInfo.Types[outer.Fun]
	if !ok || !outerFunInfo.IsType() {
		return nil, nil, nil, false
	}
	innerFunInfo, ok := pass.TypesInfo.Types[inner.Fun]
	if !ok || !innerFunInfo.IsType() {
		return nil, nil, nil, false
	}
	outerType := pass.TypesInfo.TypeOf(outer)
	innerType := pass.TypesInfo.TypeOf(inner)
	innerArgType := pass.TypesInfo.TypeOf(inner.Args[0])
	if outerType == nil || innerType == nil || innerArgType == nil {
		return nil, nil, nil, false
	}
	return outerType.Underlying(), innerType.Underlying(), innerArgType.Underlying(), true
}

func reportRedundantRoundTrip(pass *analysis.Pass, outer, inner *ast.CallExpr, outerUnderlying, innerUnderlying, innerArgUnderlying types.Type) bool {
	if !isStringType(outerUnderlying) || !isByteSliceType(innerUnderlying) || !isStringType(innerArgUnderlying) {
		return false
	}
	argText := astutil.NodeText(pass.Fset, inner.Args[0])
	outerText := astutil.NodeText(pass.Fset, outer.Fun)
	innerText := astutil.NodeText(pass.Fset, inner.Fun)
	if isExactString(pass.TypesInfo.TypeOf(outer)) && isExactString(pass.TypesInfo.TypeOf(inner.Args[0])) {
		pass.ReportRangef(outer,
			"%s(%s(%s)) is a redundant round-trip; both conversions can be removed; the inner %s conversion copies the string unnecessarily",
			outerText, innerText, argText, innerText,
		)
		return true
	}
	pass.ReportRangef(outer,
		"%s(%s(%s)) is a redundant round-trip; replace it with %s(%s); the inner %s conversion copies the string unnecessarily",
		outerText, innerText, argText, outerText, argText, innerText,
	)
	return true
}

func reportWastefulCloneRoundTrip(pass *analysis.Pass, outer, inner *ast.CallExpr, outerUnderlying, innerUnderlying, innerArgUnderlying types.Type) {
	if !isByteSliceType(outerUnderlying) || !isStringType(innerUnderlying) || !isByteSliceType(innerArgUnderlying) {
		return
	}
	argText := astutil.NodeText(pass.Fset, inner.Args[0])
	pass.ReportRangef(outer,
		"[]byte(string(%s)) makes two copies to clone %s; use slices.Clone(%s) or bytes.Clone(%s) for a single-copy independent slice",
		argText, argText, argText, argText,
	)
}

// isStringType reports whether t is a string basic type. Callers pass an
// already-.Underlying()-resolved type, so this also matches named string types.
func isStringType(t types.Type) bool {
	basic, ok := t.(*types.Basic)
	return ok && basic.Kind() == types.String
}

// isExactString reports whether t denotes the predeclared string type, not a
// named type whose underlying type is string. Unlike isStringType, which
// expects an already-.Underlying()-resolved type, isExactString must be given
// the raw type so it can tell string from `type MyString string`. Aliases are
// resolved first, because an alias may denote either the predeclared string
// (`type A = string`) or a named string type (`type A = MyString`). That
// distinction matters because only the predeclared string can have both
// conversions removed; a named string type still needs an outer conversion.
func isExactString(t types.Type) bool {
	return isStringType(types.Unalias(t))
}

func isByteSliceType(t types.Type) bool {
	sl, ok := t.(*types.Slice)
	if !ok {
		return false
	}
	elem, ok := sl.Elem().Underlying().(*types.Basic)
	return ok && elem.Kind() == types.Byte
}
