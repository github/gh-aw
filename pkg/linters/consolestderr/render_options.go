package consolestderr

import (
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/astutil"
)

func checkRenderOptions(pass *analysis.Pass, call *ast.CallExpr, destination string) {
	if len(call.Args) > 1 {
		checkOptionsArgument(pass, call.Args[1], destination)
	}
}

func checkOptionsArgument(pass *analysis.Pass, options ast.Expr, destination string) {
	// Unknown option variables are reported without an automatic fix: changing
	// shared options could affect callers that intentionally target stdout.
	literal, ok := astutil.UnwrapParenExpr(options).(*ast.CompositeLit)
	if ok {
		stderr, known := literalDestination(pass, literal)
		if known && stderr == (destination == "Stderr") {
			return
		}
	}
	pass.Report(analysis.Diagnostic{
		Pos: options.Pos(), End: options.End(),
		Message: "RenderStructWithOptions requires RenderOptions matching " + destination,
	})
}

func literalDestination(pass *analysis.Pass, literal *ast.CompositeLit) (bool, bool) {
	optionType := pass.TypesInfo.TypeOf(literal)
	if optionType == nil {
		return false, false
	}
	fields, ok := optionType.Underlying().(*types.Struct)
	if !ok {
		return false, false
	}
	for i, element := range literal.Elts {
		if keyed, ok := element.(*ast.KeyValueExpr); ok {
			key, ok := keyed.Key.(*ast.Ident)
			if ok && key.Name == "Stderr" {
				return constantBool(pass, keyed.Value)
			}
		} else if i < fields.NumFields() && fields.Field(i).Name() == "Stderr" {
			return constantBool(pass, element)
		}
	}
	return false, true
}

func constantBool(pass *analysis.Pass, expr ast.Expr) (bool, bool) {
	value := pass.TypesInfo.Types[expr].Value
	if value != nil && value.Kind() == constant.Bool {
		return constant.BoolVal(value), true
	}
	return false, false
}
