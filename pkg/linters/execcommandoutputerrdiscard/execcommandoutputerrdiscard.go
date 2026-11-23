// Package execcommandoutputerrdiscard implements a Go analysis linter that
// reports exec.Cmd output calls whose failures discard stderr diagnostics.
package execcommandoutputerrdiscard

import (
	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"go/ast"
	"go/types"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"
)

const linterName = "execcommandoutputerrdiscard"

// Analyzer is the exec-command-output-error-discard analysis pass.
var Analyzer = analyzerutil.New(linterName, "reports exec.Cmd output calls whose failures discard stderr diagnostics", run)

func run(pass *analysis.Pass) (any, error) {
	insp, err := astutil.Inspector(pass)
	if err != nil {
		return nil, err
	}

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	for cur := range insp.Root().Preorder((*ast.CallExpr)(nil)) {
		call, ok := cur.Node().(*ast.CallExpr)
		if !ok {
			continue
		}
		method := execOutputMethod(pass, call)
		if method == "" {
			continue
		}
		position := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(position, noLintIndex, linterName) {
			continue
		}

		functionType, body := enclosingFunction(cur)
		if callIsReturned(body, call) {
			continue
		}
		binding := findCallResults(pass, body, call)
		if method == "CombinedOutput" {
			if binding.output == nil ||
				(!returnsObjectOnFailurePath(pass, body, binding.output, binding.err, call.End()) &&
					!returnsOutputOnAllReturns(pass, body, functionType, binding.output, call.End()) &&
					!hasRetainedObjectUseOnFailurePath(pass, body, binding.output, binding.err, call.End())) {
				pass.ReportRangef(call, "exec.Cmd.CombinedOutput result should be retained to preserve stderr diagnostics")
			}
			continue
		}
		if !hasExitErrorStderrUse(pass, body, binding.err, call.End()) &&
			!hasConfiguredStderrUse(pass, body, call, binding.err, call.End()) &&
			!returnsErrorOnFailurePath(pass, body, functionType, binding.err, call.End()) {
			pass.ReportRangef(call, "exec.Cmd.Output error handling should retain stderr from *exec.ExitError.Stderr")
		}
	}
	return nil, nil
}
func execOutputMethod(pass *analysis.Pass, call *ast.CallExpr) string {
	selector, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	selection := pass.TypesInfo.Selections[selector]
	if selection == nil {
		return ""
	}
	method, ok := selection.Obj().(*types.Func)
	if !ok || method.Pkg() == nil || method.Pkg().Path() != "os/exec" {
		return ""
	}
	switch method.Name() {
	case "Output", "CombinedOutput":
		return method.Name()
	default:
		return ""
	}
}
func enclosingFunction(cur inspector.Cursor) (*ast.FuncType, *ast.BlockStmt) {
	for enclosing := range cur.Enclosing((*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)) {
		switch fn := enclosing.Node().(type) {
		case *ast.FuncDecl:
			return fn.Type, fn.Body
		case *ast.FuncLit:
			return fn.Type, fn.Body
		}
	}
	return nil, nil
}
func findCallResults(pass *analysis.Pass, body *ast.BlockStmt, call *ast.CallExpr) resultBinding {
	var binding resultBinding
	if body == nil {
		return binding
	}
	ast.Inspect(body, func(node ast.Node) bool {
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		switch stmt := node.(type) {
		case *ast.AssignStmt:
			if len(stmt.Rhs) == 1 && sameCall(stmt.Rhs[0], call) {
				binding = bindResults(pass, stmt.Lhs)
				return false
			}
		case *ast.ValueSpec:
			if len(stmt.Values) == 1 && sameCall(stmt.Values[0], call) {
				lhs := make([]ast.Expr, 0, len(stmt.Names))
				for _, name := range stmt.Names {
					lhs = append(lhs, name)
				}
				binding = bindResults(pass, lhs)
				return false
			}
		}
		return true
	})
	return binding
}

type resultBinding struct {
	output types.Object
	err    types.Object
}

func bindResults(pass *analysis.Pass, lhs []ast.Expr) resultBinding {
	if len(lhs) != 2 {
		return resultBinding{}
	}
	return resultBinding{
		output: bindingObject(pass, lhs[0]),
		err:    bindingObject(pass, lhs[1]),
	}
}
func bindingObject(pass *analysis.Pass, expr ast.Expr) types.Object {
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	if !ok || ident.Name == "_" {
		return nil
	}
	return pass.TypesInfo.ObjectOf(ident)
}
func sameCall(expr ast.Expr, call *ast.CallExpr) bool {
	exprCall, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	return ok && exprCall == call
}
func callIsReturned(body *ast.BlockStmt, call *ast.CallExpr) bool {
	if body == nil {
		return false
	}
	returned := false
	ast.Inspect(body, func(node ast.Node) bool {
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		ret, ok := node.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		for _, result := range ret.Results {
			if sameCall(result, call) {
				returned = true
				return false
			}
		}
		return !returned
	})
	return returned
}
