// Package errorstringformat implements a Go analysis linter that flags
// err.Error() calls passed to string functions like strings.ToLower(),
// strings.ToUpper(), fmt.Sprintf(), or similar where the error value
// should be used directly.
package errorstringformat

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// stringFunctions is a set of packages and functions we want to detect
// when they receive an .Error() call argument.
var stringFunctions = map[string]map[string]bool{
	"strings": {
		"ToLower":         true,
		"ToUpper":         true,
		"Title":           true,
		"ToLowerSpecial":  true,
		"ToUpperSpecial":  true,
		"TrimSpace":       true,
		"TrimLeft":        true,
		"TrimRight":       true,
		"TrimPrefix":      true,
		"TrimSuffix":      true,
		"Fields":          true,
		"FieldsFunc":      true,
		"Split":           true,
		"SplitN":          true,
		"SplitAfter":      true,
		"SplitAfterN":     true,
		"Join":            true,
		"Repeat":          true,
		"Replace":         true,
		"ReplaceAll":      true,
		"EqualFold":       true,
		"HasPrefix":       true,
		"HasSuffix":       true,
		"Contains":        true,
		"ContainsAny":     true,
		"Index":           true,
		"IndexAny":        true,
		"IndexRune":       true,
		"LastIndex":       true,
		"LastIndexAny":    true,
		"Count":           true,
	},
	"fmt": {
		"Sprintf":  true,
		"Sprint":   true,
		"Sprintln": true,
		"Fprintf":  true,
		"Fprint":   true,
		"Fprintln": true,
		"Printf":   true,
		"Println":  true,
		"Print":    true,
	},
	"bytes": {
		"Join":       true,
		"Count":      true,
		"Contains":   true,
		"ContainsAny": true,
		"Equal":      true,
		"EqualFold":  true,
		"Fields":     true,
		"FieldsFunc": true,
		"HasPrefix":  true,
		"HasSuffix":  true,
		"Index":      true,
		"IndexAny":   true,
		"IndexRune":  true,
		"LastIndex":  true,
		"LastIndexAny": true,
		"Repeat":     true,
		"Replace":    true,
		"ReplaceAll": true,
		"Split":      true,
		"SplitN":     true,
		"SplitAfter": true,
		"SplitAfterN": true,
		"Title":      true,
		"ToLower":    true,
		"ToUpper":    true,
		"ToLowerSpecial": true,
		"ToUpperSpecial": true,
		"Trim":       true,
		"TrimSpace":  true,
		"TrimLeft":   true,
		"TrimRight":  true,
		"TrimPrefix": true,
		"TrimSuffix": true,
	},
}

// Analyzer is the errorstringformat analysis pass.
var Analyzer = analyzerutil.New("errorstringformat", "reports err.Error() calls passed to string functions where the error value could be passed directly", run)

func run(pass *analysis.Pass) (any, error) {
	insp, err := astutil.Inspector(pass)
	if err != nil {
		return nil, err
	}
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	insp.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		analyzeErrorStringCall(pass, n, generatedFiles, noLintIndex)
	})

	return nil, nil
}

// analyzeErrorStringCall checks whether a call passes an err.Error() to a
// string function and reports a diagnostic if so.
func analyzeErrorStringCall(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "errorstringformat") {
		return
	}

	// Check if this is a call to a package.function
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	funcName := sel.Sel.Name

	// Iterate through arguments and find any .Error() calls first
	var foundErrors bool
	for _, arg := range call.Args {
		if checkForErrorCall(pass, arg) {
			foundErrors = true
			break
		}
	}

	if !foundErrors {
		return
	}

	// Now check if this is a call to a function we care about
	// Check each package we care about
	for pkgPath := range stringFunctions {
		// Check if this is a selector on the expected package
		if !astutil.IsPkgSelector(pass, sel, pkgPath) {
			continue
		}

		// Now check if the function name is in our list
		funcs := stringFunctions[pkgPath]
		if !funcs[funcName] {
			continue
		}

		// Found a matching function call with error arguments
		for i, arg := range call.Args {
			if checkForErrorCall(pass, arg) {
				// Build the fix by removing the .Error() suffix
				errText := buildErrorFix(pass, arg)
				if errText == "" {
					// Can't generate fix, just report without one
					pass.Report(analysis.Diagnostic{
						Pos:     arg.Pos(),
						End:     arg.End(),
						Message: "err.Error() call should be replaced with err for direct use with %v or error type",
					})
				} else {
					pass.Report(analysis.Diagnostic{
						Pos:     arg.Pos(),
						End:     arg.End(),
						Message: "err.Error() call should be replaced with err for direct use with %v or error type",
						SuggestedFixes: []analysis.SuggestedFix{{
							Message: fmt.Sprintf("Replace argument %d with error value directly", i+1),
							TextEdits: []analysis.TextEdit{
								{
									Pos:     arg.Pos(),
									End:     arg.End(),
									NewText: []byte(errText),
								},
							},
						}},
					})
				}
			}
		}
		return // Exit after finding matching package
	}
}

// checkForErrorCall reports whether expr is a call to .Error() method.
func checkForErrorCall(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}

	// Check if this is a method call with no arguments
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	if sel.Sel.Name != "Error" {
		return false
	}

	if len(call.Args) != 0 {
		return false
	}

	// Check if the receiver type has an Error() method, which would mean it's
	// likely an error type
	if pass.TypesInfo == nil {
		return false
	}

	receiverType := pass.TypesInfo.TypeOf(sel.X)
	if receiverType == nil {
		return false
	}

	// Look for an Error() method on this type
	return hasErrorMethod(receiverType)
}

// hasErrorMethod checks if a type implements the error interface.
func hasErrorMethod(t types.Type) bool {
	if t == nil {
		return false
	}

	// Get the error interface type from the universe
	errorType := types.Universe.Lookup("error")
	if errorType == nil {
		return false
	}

	var errorInterface *types.Interface

	// The error type is a Named type, get its underlying interface
	if named, ok := errorType.Type().(*types.Named); ok {
		errorInterface, _ = named.Underlying().(*types.Interface)
	}

	if errorInterface == nil {
		return false
	}

	// Check if t implements the error interface
	return types.Implements(t, errorInterface)
}

// buildErrorFix generates the replacement text by removing .Error() from the call.
func buildErrorFix(pass *analysis.Pass, expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}

	// Get the text of the receiver (the error value)
	return astutil.NodeText(pass.Fset, sel.X)
}

