// Package consolestderr checks that console strings written to stderr use
// destination-aware formatters.
package consolestderr

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const consolePath = "github.com/github/gh-aw/pkg/console"

var replacements = map[string]string{
	"FormatError":           "FormatErrorStderr",
	"FormatSuccessMessage":  "FormatSuccessMessageStderr",
	"FormatInfoMessage":     "FormatInfoMessageStderr",
	"FormatWarningMessage":  "FormatWarningMessageStderr",
	"FormatCommandMessage":  "FormatCommandMessageStderr",
	"FormatProgressMessage": "FormatProgressMessageStderr",
	"FormatPromptMessage":   "FormatPromptMessageStderr",
	"FormatVerboseMessage":  "FormatVerboseMessageStderr",
	"FormatListItem":        "FormatListItemStderr",
	"FormatSectionHeader":   "FormatSectionHeaderStderr",
	"RenderStruct":          "RenderStructStderr",
	"RenderTable":           "RenderTableStderr",
}

// Analyzer reports stdout-aware console formatters written directly to stderr.
var Analyzer = analyzerutil.New("consolestderr", "requires stderr-aware console formatting for direct stderr writes", run)

func run(pass *analysis.Pass) (any, error) {
	index, generated, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	return analyzerutil.Preorder(pass, []ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok || !writesStderr(pass, call) {
			return
		}
		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generated) || nolint.HasDirectiveForLinter(pos, index, "consolestderr") {
			return
		}
		for _, arg := range call.Args[1:] {
			ast.Inspect(arg, func(node ast.Node) bool {
				if _, ok := node.(*ast.FuncLit); ok {
					return false
				}
				if inner, ok := node.(*ast.CallExpr); ok {
					reportFormatter(pass, inner)
				}
				return true
			})
		}
	})
}

func writesStderr(pass *analysis.Pass, call *ast.CallExpr) bool {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || !astutil.IsPkgSelector(pass, sel, "fmt") || len(call.Args) < 2 {
		return false
	}
	switch sel.Sel.Name {
	case "Fprint", "Fprintln", "Fprintf":
	default:
		return false
	}
	writer, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.SelectorExpr)
	return ok && astutil.IsPkgSelector(pass, writer, "os") && writer.Sel.Name == "Stderr"
}

func reportFormatter(pass *analysis.Pass, call *ast.CallExpr) {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || !astutil.IsPkgSelector(pass, sel, consolePath) {
		return
	}
	if sel.Sel.Name == "RenderStructWithOptions" {
		checkRenderOptions(pass, call)
		return
	}
	replacement, ok := replacements[sel.Sel.Name]
	if !ok {
		return
	}
	pass.Report(analysis.Diagnostic{
		Pos: sel.Sel.Pos(), End: sel.Sel.End(),
		Message: sel.Sel.Name + " uses stdout styling; use " + replacement + " for stderr",
		SuggestedFixes: []analysis.SuggestedFix{{
			Message: "Use the stderr-aware console formatter",
			TextEdits: []analysis.TextEdit{{
				Pos: sel.Sel.Pos(), End: sel.Sel.End(), NewText: []byte(replacement),
			}},
		}},
	})
}
