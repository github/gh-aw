// Package hasprefixdotdotpathescape reports overbroad parent-path prefix checks.
package hasprefixdotdotpathescape

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const linterName = "hasprefixdotdotpathescape"

// Analyzer reports overbroad checks for paths escaping a directory.
var Analyzer = analyzerutil.New(linterName, `reports strings.HasPrefix on path-like values with a ".." prefix`, run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	return analyzerutil.Preorder(pass, []ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return
		}
		var ident *ast.Ident
		switch fun := astutil.UnwrapParenExpr(call.Fun).(type) {
		case *ast.SelectorExpr:
			ident = fun.Sel
		case *ast.Ident:
			ident = fun
		}
		if ident == nil {
			return
		}
		fn, ok := pass.TypesInfo.Uses[ident].(*types.Func)
		if !ok || fn.Name() != "HasPrefix" || fn.Pkg() == nil || fn.Pkg().Path() != "strings" {
			return
		}
		prefix := call.Args[1] //nolint:uncheckedsliceindex // len(call.Args) is checked above
		value := pass.TypesInfo.Types[prefix].Value
		if value == nil || value.Kind() != constant.String || constant.StringVal(value) != ".." {
			return
		}
		if !isPathLikeExpression(pass, call.Args[0]) { //nolint:uncheckedsliceindex // len(call.Args) is checked above
			return
		}
		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) || nolint.HasDirectiveForLinter(pos, noLintIndex, linterName) {
			return
		}
		pass.Reportf(call.Pos(), `strings.HasPrefix(value, "..") also matches names like "..foo"; check exact ".." and a separator, or use filepath.IsLocal`)
	})
}

// isPathLikeExpression uses identifier names and path-manipulation calls as a conservative path-context heuristic.
func isPathLikeExpression(pass *analysis.Pass, expr ast.Expr) bool {
	switch expr := astutil.UnwrapParenExpr(expr).(type) {
	case *ast.Ident:
		return isPathLikeName(expr.Name)
	case *ast.SelectorExpr:
		return isPathLikeName(expr.Sel.Name)
	case *ast.CallExpr:
		var ident *ast.Ident
		switch fun := astutil.UnwrapParenExpr(expr.Fun).(type) {
		case *ast.SelectorExpr:
			ident = fun.Sel
		case *ast.Ident:
			ident = fun
		}
		if ident == nil {
			return false
		}
		fn, ok := pass.TypesInfo.Uses[ident].(*types.Func)
		if !ok || fn.Pkg() == nil || (fn.Pkg().Path() != "path" && fn.Pkg().Path() != "path/filepath") {
			return false
		}
		switch fn.Name() {
		case "Abs", "Base", "Clean", "Dir", "EvalSymlinks", "FromSlash", "Join", "Rel", "ToSlash":
			return true
		}
	}
	return false
}

func isPathLikeName(name string) bool {
	runes := []rune(name)
	start := 0
	for i := 1; i < len(runes); i++ {
		current, previous := runes[i], runes[i-1]
		if !unicode.IsLetter(current) && !unicode.IsDigit(current) {
			if isPathNameComponent(runes[start:i]) {
				return true
			}
			start = i + 1
		} else if unicode.IsUpper(current) && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
			if isPathNameComponent(runes[start:i]) {
				return true
			}
			start = i
		} else if unicode.IsUpper(previous) && unicode.IsUpper(current) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
			if isPathNameComponent(runes[start:i]) {
				return true
			}
			start = i
		}
	}
	return isPathNameComponent(runes[start:])
}

func isPathNameComponent(component []rune) bool {
	switch strings.ToLower(string(component)) {
	case "dir", "directory", "directories", "dirs", "file", "filename", "filenames", "files", "filepath", "filepaths", "path", "paths", "root":
		return true
	default:
		return false
	}
}
