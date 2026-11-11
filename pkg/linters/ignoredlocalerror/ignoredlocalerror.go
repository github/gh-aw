// Package ignoredlocalerror implements a Go analysis linter that flags discarded
// errors returned by functions in the github.com/github/gh-aw module.
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
var Analyzer = analyzerutil.New("ignoredlocalerror", "reports discarded errors from functions in the github.com/github/gh-aw module", run)

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{
		(*ast.AssignStmt)(nil),
		(*ast.ValueSpec)(nil),
		(*ast.ExprStmt)(nil),
		(*ast.DeferStmt)(nil),
		(*ast.GoStmt)(nil),
	}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeStmt(pass, n, generatedFiles, nolintIndex)
	})
}

func analyzeStmt(pass *analysis.Pass, node ast.Node, generatedFiles filecheck.GeneratedIndex, nolintIndex nolint.DirectiveIndex) {
	switch stmt := node.(type) {
	case *ast.AssignStmt:
		for i, rhs := range stmt.Rhs {
			call, ok := astutil.UnwrapParenExpr(rhs).(*ast.CallExpr)
			if !ok {
				continue
			}
			if discardedErrorResult(pass, call, stmt.Lhs, stmt.Rhs, i) {
				reportIfNotSkipped(pass, call, generatedFiles, nolintIndex)
			}
		}
	case *ast.ValueSpec:
		lhs := make([]ast.Expr, 0, len(stmt.Names))
		for _, name := range stmt.Names {
			lhs = append(lhs, name)
		}
		for i, rhs := range stmt.Values {
			call, ok := astutil.UnwrapParenExpr(rhs).(*ast.CallExpr)
			if !ok {
				continue
			}
			if discardedErrorResult(pass, call, lhs, stmt.Values, i) {
				reportIfNotSkipped(pass, call, generatedFiles, nolintIndex)
			}
		}
	case *ast.ExprStmt:
		call, ok := astutil.UnwrapParenExpr(stmt.X).(*ast.CallExpr)
		if ok && hasErrorResult(pass, call) {
			reportIfNotSkipped(pass, call, generatedFiles, nolintIndex)
		}
	case *ast.DeferStmt:
		if hasErrorResult(pass, stmt.Call) {
			reportIfNotSkipped(pass, stmt.Call, generatedFiles, nolintIndex)
		}
	case *ast.GoStmt:
		if hasErrorResult(pass, stmt.Call) {
			reportIfNotSkipped(pass, stmt.Call, generatedFiles, nolintIndex)
		}
	}
}

func discardedErrorResult(pass *analysis.Pass, call *ast.CallExpr, lhs, rhs []ast.Expr, rhsIndex int) bool {
	signature, ok := callSignature(pass, call)
	if !ok {
		return false
	}

	results := signature.Results()
	if len(rhs) == 1 {
		if len(lhs) != results.Len() {
			return false
		}
		for i, lhsExpr := range lhs {
			if isErrorType(results.At(i).Type()) && isBlank(lhsExpr) {
				return true
			}
		}
		return false
	}

	if results.Len() != 1 {
		return false
	}
	for i, lhsExpr := range lhs {
		if i == rhsIndex {
			return isErrorType(results.At(0).Type()) && isBlank(lhsExpr)
		}
	}
	return false
}

func hasErrorResult(pass *analysis.Pass, call *ast.CallExpr) bool {
	signature, ok := callSignature(pass, call)
	if !ok {
		return false
	}
	for result := range signature.Results().Variables() {
		if isErrorType(result.Type()) {
			return true
		}
	}
	return false
}

func callSignature(pass *analysis.Pass, call *ast.CallExpr) (*types.Signature, bool) {
	packageCall := *call
	packageCall.Fun = unwrapTypeInstantiation(call.Fun)
	pkgPath, _, ok := astutil.PackageCall(pass, &packageCall)
	if !ok {
		if selector, isSelector := astutil.UnwrapParenExpr(packageCall.Fun).(*ast.SelectorExpr); isSelector {
			if fn, isFunc := pass.TypesInfo.Uses[selector.Sel].(*types.Func); isFunc && fn.Pkg() != nil {
				pkgPath = fn.Pkg().Path()
				ok = true
			}
		}
	}
	if !ok || (pkgPath != modulePath && !strings.HasPrefix(pkgPath, modulePath+"/")) {
		return nil, false
	}

	callType := pass.TypesInfo.TypeOf(astutil.UnwrapParenExpr(call.Fun))
	signature, ok := callType.(*types.Signature)
	return signature, ok
}

func unwrapTypeInstantiation(expr ast.Expr) ast.Expr {
	for {
		expr = astutil.UnwrapParenExpr(expr)
		switch instantiated := expr.(type) {
		case *ast.IndexExpr:
			expr = instantiated.X
		case *ast.IndexListExpr:
			expr = instantiated.X
		default:
			return expr
		}
	}
}

func isErrorType(t types.Type) bool {
	return nolint.ImplementsError(t)
}

func isBlank(expr ast.Expr) bool {
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	return ok && ident.Name == "_"
}

func reportIfNotSkipped(pass *analysis.Pass, call *ast.CallExpr, generatedFiles filecheck.GeneratedIndex, nolintIndex nolint.DirectiveIndex) {
	position := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(position, nolintIndex, "ignoredlocalerror") {
		return
	}
	pass.ReportRangef(call, "error return from a github.com/github/gh-aw function is discarded")
}
