// Package ignoredlocalerror implements a Go analysis linter that flags calls to
// functions from the github.com/github/gh-aw module whose error result is discarded.
package ignoredlocalerror

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const modulePath = "github.com/github/gh-aw"

// Analyzer is the ignored-local-error analysis pass.
var Analyzer = analyzerutil.NewAtPath("ignoredlocalerror", "reports errors returned by functions in the github.com/github/gh-aw module when the caller discards them as a bare call or by assigning them to _", "ignored-local-error", run)

var errorType = types.Universe.Lookup("error").Type()

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.ExprStmt)(nil), (*ast.AssignStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		switch stmt := n.(type) {
		case *ast.ExprStmt:
			if call, ok := astutil.UnwrapParenExpr(stmt.X).(*ast.CallExpr); ok {
				if _, hasErr := errorResultIndexes(pass, call); hasErr {
					report(pass, call, generatedFiles, nolintIndex)
				}
			}
		case *ast.AssignStmt:
			checkAssign(pass, stmt, generatedFiles, nolintIndex)
		}
	})
}

func checkAssign(pass *analysis.Pass, stmt *ast.AssignStmt, generated filecheck.GeneratedIndex, idx nolint.DirectiveIndex) {
	if len(stmt.Rhs) == 1 && len(stmt.Lhs) > 1 {
		call, ok := astutil.UnwrapParenExpr(stmt.Rhs[0]).(*ast.CallExpr)
		if !ok {
			return
		}
		indexes, hasErr := errorResultIndexes(pass, call)
		if !hasErr {
			return
		}
		for _, i := range indexes {
			if i < len(stmt.Lhs) && isBlank(stmt.Lhs[i]) {
				report(pass, call, generated, idx)
				return
			}
		}
		return
	}
	if len(stmt.Lhs) != len(stmt.Rhs) {
		return
	}
	for i, rhs := range stmt.Rhs {
		call, ok := astutil.UnwrapParenExpr(rhs).(*ast.CallExpr)
		if !ok || !isBlank(stmt.Lhs[i]) {
			continue
		}
		// With one result per call, the single result must itself be an error.
		if sig := callSignature(pass, call); sig == nil || sig.Results().Len() != 1 {
			continue
		}
		if _, hasErr := errorResultIndexes(pass, call); hasErr {
			report(pass, call, generated, idx)
		}
	}
}

func isBlank(expr ast.Expr) bool {
	id, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	return ok && id.Name == "_"
}

func callSignature(pass *analysis.Pass, call *ast.CallExpr) *types.Signature {
	funType := pass.TypesInfo.TypeOf(call.Fun)
	if funType == nil {
		return nil
	}
	sig, _ := funType.Underlying().(*types.Signature)
	return sig
}

// errorResultIndexes returns the error result positions of a call to a
// function declared in this module.
func errorResultIndexes(pass *analysis.Pass, call *ast.CallExpr) ([]int, bool) {
	fn := calledFunc(pass, call)
	if fn == nil || fn.Pkg() == nil || !isLocalPackage(pass, fn.Pkg()) {
		return nil, false
	}
	sig, ok := fn.Type().(*types.Signature)
	if !ok {
		return nil, false
	}
	var out []int
	for i := range sig.Results().Len() {
		if types.Identical(sig.Results().At(i).Type(), errorType) {
			out = append(out, i)
		}
	}
	return out, len(out) > 0
}

func isLocalPackage(pass *analysis.Pass, pkg *types.Package) bool {
	if pkg == pass.Pkg {
		return true
	}
	path := pkg.Path()
	return path == modulePath || strings.HasPrefix(path, modulePath+"/")
}

func calledFunc(pass *analysis.Pass, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch fun := astutil.UnwrapParenExpr(call.Fun).(type) {
	case *ast.Ident:
		id = fun
	case *ast.SelectorExpr:
		id = fun.Sel
	case *ast.IndexExpr: // explicit generic instantiation
		return calledFuncFromExpr(pass, fun.X)
	case *ast.IndexListExpr:
		return calledFuncFromExpr(pass, fun.X)
	default:
		return nil
	}
	fn, _ := pass.TypesInfo.Uses[id].(*types.Func)
	return fn
}

func calledFuncFromExpr(pass *analysis.Pass, expr ast.Expr) *types.Func {
	return calledFunc(pass, &ast.CallExpr{Fun: expr})
}

func report(pass *analysis.Pass, call *ast.CallExpr, generated filecheck.GeneratedIndex, idx nolint.DirectiveIndex) {
	position := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(position.Filename, generated) {
		return
	}
	if nolint.HasDirectiveForLinter(position, idx, "ignoredlocalerror") {
		return
	}
	pass.ReportRangef(call, "error returned by local function %s is discarded", astutil.NodeText(pass.Fset, call.Fun))
}
