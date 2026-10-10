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

var stdoutFormatters = map[string]string{
	"FormatError":           "FormatErrorStdout",
	"FormatSuccessMessage":  "FormatSuccessMessageStdout",
	"FormatInfoMessage":     "FormatInfoMessageStdout",
	"FormatWarningMessage":  "FormatWarningMessageStdout",
	"FormatCommandMessage":  "FormatCommandMessageStdout",
	"FormatProgressMessage": "FormatProgressMessageStdout",
	"FormatPromptMessage":   "FormatPromptMessageStdout",
	"FormatVerboseMessage":  "FormatVerboseMessageStdout",
	"FormatListItem":        "FormatListItemStdout",
	"FormatSectionHeader":   "FormatSectionHeaderStdout",
	"RenderStruct":          "RenderStructStdout",
	"RenderTable":           "RenderTableStdout",
}

// Analyzer reports console formatters whose styling disagrees with the destination.
var Analyzer = analyzerutil.New("consolestderr", "requires destination-aware console formatting for direct standard-stream writes", run)

func run(pass *analysis.Pass) (any, error) {
	index, generated, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}
	return analyzerutil.Preorder(pass, []ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		destination := outputDestination(pass, call)
		if destination == "" {
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
					reportFormatter(pass, inner, destination)
				}
				return true
			})
		}
	})
}

func outputDestination(pass *analysis.Pass, call *ast.CallExpr) string {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || !astutil.IsPkgSelector(pass, sel, "fmt") || len(call.Args) < 2 {
		return ""
	}
	switch sel.Sel.Name {
	case "Fprint", "Fprintln", "Fprintf":
	default:
		return ""
	}
	writer, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.SelectorExpr)
	if ok && astutil.IsPkgSelector(pass, writer, "os") {
		switch writer.Sel.Name {
		case "Stderr", "Stdout":
			return writer.Sel.Name
		}
	}
	return ""
}

func reportFormatter(pass *analysis.Pass, call *ast.CallExpr, destination string) {
	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok || !astutil.IsPkgSelector(pass, sel, consolePath) {
		return
	}
	if sel.Sel.Name == "RenderStructWithOptions" {
		checkRenderOptions(pass, call, destination)
		return
	}
	replacement := formatterReplacement(sel.Sel.Name, destination)
	if replacement == "" {
		return
	}
	pass.Report(analysis.Diagnostic{
		Pos: sel.Sel.Pos(), End: sel.Sel.End(),
		Message: sel.Sel.Name + " uses the wrong output styling; use " + replacement + " for " + destination,
		SuggestedFixes: []analysis.SuggestedFix{{
			Message: "Use the destination-aware console formatter",
			TextEdits: []analysis.TextEdit{{
				Pos: sel.Sel.Pos(), End: sel.Sel.End(), NewText: []byte(replacement),
			}},
		}},
	})
}

func formatterReplacement(name, destination string) string {
	for diagnostic, stdout := range stdoutFormatters {
		if destination == "Stdout" && (name == diagnostic || name == diagnostic+"Stderr") {
			return stdout
		}
		if destination == "Stderr" && name == stdout {
			return diagnostic
		}
	}
	return ""
}
