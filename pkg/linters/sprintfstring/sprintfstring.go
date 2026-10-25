// Package sprintfstring implements a Go analysis linter that flags
// fmt.Sprintf("%s", x) calls where x is a plain string, which is a no-op
// formatting call that should just use x.
package sprintfstring

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the sprintfstring analysis pass.
var Analyzer = analyzerutil.New("sprintfstring", `reports fmt.Sprintf("%s", x) calls where x is already a string; use x directly`, run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeSprintfString(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeSprintfString checks whether a call is fmt.Sprintf("%s", x) with x of
// type string and reports a diagnostic suggesting x be used directly.
func analyzeSprintfString(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}
	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "sprintfstring") {
		return
	}
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Sprintf" || !astutil.IsPkgSelector(pass, sel, "fmt") {
		return
	}
	if len(call.Args) != 2 || call.Ellipsis.IsValid() {
		return
	}
	formatLit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || formatLit.Kind != token.STRING || formatLit.Value != `"%s"` {
		return
	}
	arg := call.Args[1]
	// Only exact string: named string types may implement Stringer/error or change type when unwrapped.
	if pass.TypesInfo.TypeOf(arg) != types.Typ[types.String] {
		return
	}
	diag := analysis.Diagnostic{
		Pos:     call.Pos(),
		End:     call.End(),
		Message: `use the string directly instead of fmt.Sprintf("%s", x)`,
	}
	if argText := astutil.NodeText(pass.Fset, arg); argText != "" && !astutil.HasOverlappingComment(pass.Files, call.Pos(), call.End()) {
		diag.SuggestedFixes = []analysis.SuggestedFix{{
			Message:   "Replace fmt.Sprintf(\"%s\", x) with x",
			TextEdits: []analysis.TextEdit{{Pos: call.Pos(), End: call.End(), NewText: []byte(argText)}},
		}}
	}
	pass.Report(diag)
}
