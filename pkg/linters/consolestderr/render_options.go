package consolestderr

import (
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/astutil"
)

func checkRenderOptions(pass *analysis.Pass, call *ast.CallExpr) {
	if len(call.Args) > 1 {
		checkOptionsArgument(pass, call.Args[1])
	}
}

func checkOptionsArgument(pass *analysis.Pass, options ast.Expr) {
	// Unknown option variables are reported without an automatic fix: changing
	// shared options could affect callers that intentionally target stdout.
	literal, ok := astutil.UnwrapParenExpr(options).(*ast.CompositeLit)
	if ok && literalTargetsStderr(pass, literal) {
		return
	}
	pass.Report(analysis.Diagnostic{
		Pos: options.Pos(), End: options.End(),
		Message: "RenderStructWithOptions written to stderr requires RenderOptions with explicit Stderr: true",
	})
}

func literalTargetsStderr(pass *analysis.Pass, literal *ast.CompositeLit) bool {
	optionType := pass.TypesInfo.TypeOf(literal)
	if optionType == nil {
		return false
	}
	fields, ok := optionType.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i, element := range literal.Elts {
		if keyed, ok := element.(*ast.KeyValueExpr); ok {
			key, ok := keyed.Key.(*ast.Ident)
			if ok && key.Name == "Stderr" {
				return constantTrue(pass, keyed.Value)
			}
		} else if i < fields.NumFields() && fields.Field(i).Name() == "Stderr" {
			return constantTrue(pass, element)
		}
	}
	return false
}

func constantTrue(pass *analysis.Pass, expr ast.Expr) bool {
	value := pass.TypesInfo.Types[expr].Value
	return value != nil && value.Kind() == constant.Bool && constant.BoolVal(value)
}
