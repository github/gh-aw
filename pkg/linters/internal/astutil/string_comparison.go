package astutil

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// StringMethodComparison describes a comparison between a strings method call
// and an integer constant, including whether it checks for absence.
type StringMethodComparison struct {
	Op      token.Token
	Value   int64
	Negated bool
}

// ReportStringsMethodComparison reports a strings method comparison that can
// be replaced by another strings method according to comparisons.
func ReportStringsMethodComparison(pass *analysis.Pass, expr *ast.BinaryExpr, method, replacement string, comparisons []StringMethodComparison) {
	call, negated, matched := matchStringsMethodComparison(pass, expr, method, comparisons)
	if !matched || len(call.Args) != 2 {
		return
	}

	var sText, subText string
	for i, arg := range call.Args {
		if i == 0 {
			sText = NodeText(pass.Fset, arg)
		} else {
			subText = NodeText(pass.Fset, arg)
		}
	}
	pkgText := CallQualifierText(pass.Fset, call)
	if sText == "" || subText == "" || pkgText == "" {
		return
	}

	prefix := ""
	if negated {
		prefix = "!"
	}
	message := "use " + prefix + "strings." + replacement + "(" + sText + ", " + subText + ") instead of strings." + method + " comparison"
	fixMessage := "Replace strings." + method + " comparison with strings." + replacement
	var fixes []analysis.SuggestedFix
	if !HasOverlappingComment(pass.Files, expr.Pos(), expr.End()) {
		fixes = []analysis.SuggestedFix{{
			Message: fixMessage,
			TextEdits: []analysis.TextEdit{{
				Pos:     expr.Pos(),
				End:     expr.End(),
				NewText: []byte(prefix + pkgText + "." + replacement + "(" + sText + ", " + subText + ")"),
			}},
		}}
	}
	pass.Report(analysis.Diagnostic{
		Pos:            expr.Pos(),
		End:            expr.End(),
		Message:        message,
		SuggestedFixes: fixes,
	})
}

func matchStringsMethodComparison(pass *analysis.Pass, expr *ast.BinaryExpr, method string, comparisons []StringMethodComparison) (*ast.CallExpr, bool, bool) {
	left, right, flipped := NormalizeComparisonOperands(pass, expr, method)
	call, ok := AsStringsMethodCall(pass, left, method)
	if !ok {
		return nil, false, false
	}

	op := expr.Op
	if flipped {
		op = FlipComparisonOp(op)
	}
	value, ok := ConstIntValue(pass, right)
	if !ok {
		return nil, false, false
	}

	for _, comparison := range comparisons {
		if comparison.Op == op && comparison.Value == value {
			return call, comparison.Negated, true
		}
	}
	return nil, false, false
}
