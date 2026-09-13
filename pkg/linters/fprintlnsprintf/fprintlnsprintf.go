// Package fprintlnsprintf implements a Go analysis linter that flags
// fmt.Fprintln(w, fmt.Sprintf(...)) calls that should be rewritten as fmt.Fprintf(w, ...).
package fprintlnsprintf

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the fprintlnsprintf analysis pass.
var Analyzer = analyzerutil.New("fprintlnsprintf", "reports fmt.Fprintln(w, fmt.Sprintf(...)) calls that should be rewritten as fmt.Fprintf(w, ...)", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}

		// Check if this is exactly fmt.Fprintln(w, fmt.Sprintf(...)).
		if !isFmtFunc(pass, call, "Fprintln") {
			return
		}
		if len(call.Args) != 2 {
			return
		}

		// Skip test files.
		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}

		// Check if the printed argument is fmt.Sprintf(...).
		printedArg, ok := call.Args[1].(*ast.CallExpr)
		if !ok {
			return
		}
		if !isFmtFunc(pass, printedArg, "Sprintf") {
			return
		}
		if nolint.HasDirectiveForLinter(pos, noLintIndex, "fprintlnsprintf") {
			return
		}

		pass.Report(analysis.Diagnostic{
			Pos:            call.Pos(),
			End:            call.End(),
			Message:        "use fmt.Fprintf instead of fmt.Fprintln(w, fmt.Sprintf(...))",
			SuggestedFixes: buildFprintfFix(pass, call, printedArg),
		})
	})
}

// buildFprintfFix returns a SuggestedFix rewriting
// fmt.Fprintln(w, fmt.Sprintf("format", args...)) to
// fmt.Fprintf(w, "format\n", args...).
// A fix is only emitted when the format argument is a plain double-quoted
// string literal; other forms (raw strings, variables) are left unfixed.
func buildFprintfFix(pass *analysis.Pass, call *ast.CallExpr, sprintfCall *ast.CallExpr) []analysis.SuggestedFix {
	if len(sprintfCall.Args) == 0 {
		return nil
	}
	if astutil.HasOverlappingComment(pass.Files, call.Pos(), call.End()) {
		return nil
	}
	formatLit, ok := sprintfCall.Args[0].(*ast.BasicLit)
	if !ok || formatLit.Kind != token.STRING {
		return nil
	}
	raw := formatLit.Value
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return nil // not a plain double-quoted literal
	}

	// Build the replacement format literal. The Fprintln call we are replacing
	// always writes one trailing newline, so the replacement format must always
	// gain one as well.
	newFormatLit := []byte(raw[:len(raw)-1] + `\n"`)

	outerSel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	return []analysis.SuggestedFix{{
		Message: `Replace fmt.Fprintln with fmt.Fprintf`,
		TextEdits: []analysis.TextEdit{
			// 1. "Fprintln" → "Fprintf"
			{Pos: outerSel.Sel.Pos(), End: outerSel.Sel.End(), NewText: []byte("Fprintf")},
			// 2. Delete "fmt.Sprintf(" — from the start of sprintfCall to after its "("
			{Pos: sprintfCall.Pos(), End: sprintfCall.Lparen + 1, NewText: nil},
			// 3. Replace the format literal (always adding one trailing \n).
			{Pos: formatLit.Pos(), End: formatLit.End(), NewText: newFormatLit},
			// 4. Delete the closing ")" of fmt.Sprintf(...)
			{Pos: sprintfCall.Rparen, End: sprintfCall.Rparen + 1, NewText: nil},
		},
	}}
}

// isFmtFunc returns true if call is a call to fmt.<name>.
func isFmtFunc(pass *analysis.Pass, call *ast.CallExpr, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return astutil.IsPkgSelector(pass, sel, "fmt") && sel.Sel.Name == name
}
