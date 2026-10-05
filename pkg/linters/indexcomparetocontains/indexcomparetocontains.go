// Package indexcomparetocontains implements a Go analysis linter that flags
// strings.Index(), strings.LastIndex(), strings.IndexByte(),
// bytes.Index(), bytes.LastIndex(), bytes.IndexByte() calls compared
// with -1 or 0 that should use strings.Contains(), bytes.Contains(),
// or their negations instead.
package indexcomparetocontains

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the index-compare-to-contains analysis pass.
var Analyzer = analyzerutil.New(
	"indexcomparetocontains",
	"reports strings.Index() or bytes.Index() calls compared to 0 or -1 that should use strings.Contains() or bytes.Contains() instead",
	run,
)

// indexMethodComparisons defines comparisons that should use Contains/!Contains
var indexMethodComparisons = []astutil.StringMethodComparison{
	{Op: token.NEQ, Value: -1},                     // Index(...) != -1 -> Contains(...)
	{Op: token.GEQ, Value: 0},                      // Index(...) >= 0 -> Contains(...)
	{Op: token.GTR, Value: -1},                     // Index(...) > -1 -> Contains(...)
	{Op: token.EQL, Value: -1, Negated: true},      // Index(...) == -1 -> !Contains(...)
	{Op: token.LSS, Value: 0, Negated: true},       // Index(...) < 0 -> !Contains(...)
	{Op: token.LEQ, Value: -1, Negated: true},      // Index(...) <= -1 -> !Contains(...)
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.BinaryExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeIndexCompare(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeIndexCompare checks whether a binary expression is an Index method
// comparison with -1 or 0 that should use Contains.
func analyzeIndexCompare(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	expr, ok := n.(*ast.BinaryExpr)
	if !ok {
		return
	}
	pos := pass.Fset.PositionFor(expr.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "indexcomparetocontains") {
		return
	}

	// Try strings.Index variants
	if reportIndexCompare(pass, expr, "strings", "Index", "Contains") {
		return
	}
	if reportIndexCompare(pass, expr, "strings", "LastIndex", "Contains") {
		return
	}
	if reportIndexCompare(pass, expr, "strings", "IndexByte", "ContainsRune") {
		return
	}

	// Try bytes.Index variants
	if reportIndexCompare(pass, expr, "bytes", "Index", "Contains") {
		return
	}
	if reportIndexCompare(pass, expr, "bytes", "LastIndex", "Contains") {
		return
	}
	if reportIndexCompare(pass, expr, "bytes", "IndexByte", "Contains") {
		return
	}
}

// reportIndexCompare checks if a binary expression matches an index method comparison
// and reports a diagnostic if it does. Returns true if a diagnostic was reported.
func reportIndexCompare(pass *analysis.Pass, expr *ast.BinaryExpr, pkgPath, methodName, replacement string) bool {
	x := astutil.UnwrapParenExpr(expr.X)
	y := astutil.UnwrapParenExpr(expr.Y)

	// Check if left operand is the package method call
	if call, ok := asPackageMethodCall(pass, x, pkgPath, methodName); ok {
		if checkAndReport(pass, expr, call, expr.Op, y, pkgPath, methodName, replacement) {
			return true
		}
	}

	// Check if right operand is the package method call (yoda-order)
	if call, ok := asPackageMethodCall(pass, y, pkgPath, methodName); ok {
		if checkAndReport(pass, expr, call, astutil.FlipComparisonOp(expr.Op), x, pkgPath, methodName, replacement) {
			return true
		}
	}

	return false
}

// checkAndReport checks if a comparison matches a reportable pattern and reports it.
func checkAndReport(pass *analysis.Pass, expr *ast.BinaryExpr, call *ast.CallExpr, op token.Token, constExpr ast.Expr, pkgPath, methodName, replacement string) bool {
	value, ok := astutil.ConstIntValue(pass, constExpr)
	if !ok {
		return false
	}

	// Check if this matches a reportable pattern
	var negated bool
	matched := false
	for _, comparison := range indexMethodComparisons {
		if comparison.Op == op && comparison.Value == value {
			negated = comparison.Negated
			matched = true
			break
		}
	}
	if !matched {
		return false
	}

	if len(call.Args) < 1 {
		return false
	}

	var args []string
	for _, arg := range call.Args {
		text := astutil.NodeText(pass.Fset, arg)
		if text == "" {
			return false
		}
		args = append(args, text)
	}

	pkgText := astutil.CallQualifierText(pass.Fset, call)
	if pkgText == "" {
		return false
	}

	prefix := ""
	if negated {
		prefix = "!"
	}

	// Build argument list
	var argStr string
	if len(args) >= 2 {
		argStr = args[0] + ", " + args[1]
	} else if len(args) == 1 {
		argStr = args[0]
	} else {
		return false
	}

	message := "use " + prefix + pkgPath + "." + replacement + "(" + argStr + ") instead of " + pkgPath + "." + methodName + " comparison"
	fixMessage := "Replace " + pkgPath + "." + methodName + " comparison with " + pkgPath + "." + replacement

	var fixes []analysis.SuggestedFix
	if !astutil.HasOverlappingComment(pass.Files, expr.Pos(), expr.End()) {
		fixes = []analysis.SuggestedFix{{
			Message: fixMessage,
			TextEdits: []analysis.TextEdit{{
				Pos:     expr.Pos(),
				End:     expr.End(),
				NewText: []byte(prefix + pkgText + "." + replacement + "(" + argStr + ")"),
			}},
		}}
	}

	pass.Report(analysis.Diagnostic{
		Pos:            expr.Pos(),
		End:            expr.End(),
		Message:        message,
		SuggestedFixes: fixes,
	})
	return true
}

// asPackageMethodCall returns the *ast.CallExpr if expr is a call to the
// named method on the specified package (e.g. "Index" on "strings" or "bytes").
func asPackageMethodCall(pass *analysis.Pass, expr ast.Expr, pkgPath, methodName string) (*ast.CallExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != methodName {
		return nil, false
	}
	if !astutil.IsPkgSelector(pass, sel, pkgPath) {
		return nil, false
	}
	return call, true
}
