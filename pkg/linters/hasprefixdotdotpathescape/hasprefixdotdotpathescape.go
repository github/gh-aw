// Package hasprefixdotdotpathescape reports overbroad parent-path prefix checks.
package hasprefixdotdotpathescape

import (
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const linterName = "hasprefixdotdotpathescape"

// Analyzer reports overbroad checks for paths escaping a directory.
var Analyzer = analyzerutil.New(linterName, `reports strings.HasPrefix(value, "..") checks that also match names such as "..foo"`, run)

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
		var prefix ast.Expr
		for i, arg := range call.Args {
			if i == 1 {
				prefix = arg
			}
		}
		value := pass.TypesInfo.Types[prefix].Value
		if value == nil || value.Kind() != constant.String || constant.StringVal(value) != ".." {
			return
		}
		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) || nolint.HasDirectiveForLinter(pos, noLintIndex, linterName) {
			return
		}
		pass.Reportf(call.Pos(), `strings.HasPrefix(value, "..") also matches names like "..foo"; check exact ".." and a separator, or use filepath.IsLocal`)
	})
}
