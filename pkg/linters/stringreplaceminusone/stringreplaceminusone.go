// Package stringreplaceminusone implements a Go analysis linter that flags
// strings.Replace calls whose n argument is -1, which should instead use the
// more readable and idiomatic strings.ReplaceAll.
package stringreplaceminusone

import (
	"fmt"
	"go/ast"
	"go/constant"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the string-replace-minus-one analysis pass.
var Analyzer = analyzerutil.New("stringreplaceminusone", "reports strings.Replace calls with n=-1 that should use strings.ReplaceAll", run)

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

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "stringreplaceminusone") {
			return
		}

		if !isStringsReplaceCall(pass, call) {
			return
		}

		// strings.Replace takes exactly 4 arguments: s, old, new, n.
		if len(call.Args) != 4 {
			return
		}

		if !isConstantNegativeOne(pass, call.Args[3]) {
			return
		}

		pass.Report(analysis.Diagnostic{
			Pos:            call.Pos(),
			End:            call.End(),
			Message:        "use strings.ReplaceAll instead of strings.Replace with n=-1",
			SuggestedFixes: buildReplaceAllFix(pass, call),
		})
	})
}

// isStringsReplaceCall reports whether call is an invocation of strings.Replace.
func isStringsReplaceCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Replace" {
		return false
	}
	return astutil.IsPkgSelector(pass, sel, "strings")
}

// isConstantNegativeOne reports whether expr evaluates to the constant -1.
func isConstantNegativeOne(pass *analysis.Pass, expr ast.Expr) bool {
	tv, ok := pass.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.Int {
		return false
	}
	val, exact := constant.Int64Val(tv.Value)
	return exact && val == -1
}

// buildReplaceAllFix constructs a suggested fix that rewrites
// strings.Replace(s, old, new, -1) as strings.ReplaceAll(s, old, new).
func buildReplaceAllFix(pass *analysis.Pass, call *ast.CallExpr) []analysis.SuggestedFix {
	if len(call.Args) != 4 {
		return nil
	}
	if astutil.HasOverlappingComment(pass.Files, call.Pos(), call.End()) {
		return nil
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	pkgText := astutil.NodeText(pass.Fset, sel.X)
	sText := astutil.NodeText(pass.Fset, call.Args[0])
	oldText := astutil.NodeText(pass.Fset, call.Args[1])
	newText := astutil.NodeText(pass.Fset, call.Args[2])
	if pkgText == "" || sText == "" || oldText == "" || newText == "" {
		return nil
	}
	return []analysis.SuggestedFix{{
		Message: "Replace with strings.ReplaceAll",
		TextEdits: []analysis.TextEdit{{
			Pos:     call.Pos(),
			End:     call.End(),
			NewText: fmt.Appendf(nil, "%s.ReplaceAll(%s, %s, %s)", pkgText, sText, oldText, newText),
		}},
	}}
}
