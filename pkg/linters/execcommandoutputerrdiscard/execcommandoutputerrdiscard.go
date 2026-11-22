// Package execcommandoutputerrdiscard implements a Go analysis linter that
// reports exec.Cmd output calls whose failures discard stderr diagnostics.
package execcommandoutputerrdiscard

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
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
				(!hasObjectUse(pass, body, binding.output, call.End()) &&
					!returnsNamedResultOnFailurePath(pass, body, functionType, binding.output, binding.err, call.End())) {
				pass.ReportRangef(call, "exec.Cmd.CombinedOutput result should be retained to preserve stderr diagnostics")
			}
			continue
		}
		if binding.err == nil ||
			(!hasExitErrorStderrUse(pass, body, binding.err, call.End()) &&
				!returnsErrorOnFailurePath(pass, body, functionType, binding.err, call.End())) {
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
				lhs := make([]ast.Expr, len(stmt.Names))
				for i, name := range stmt.Names {
					lhs[i] = name
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

func returnsErrorOnFailurePath(pass *analysis.Pass, body *ast.BlockStmt, functionType *ast.FuncType, object types.Object, after token.Pos) bool {
	if body == nil || object == nil {
		return false
	}
	namedResult := hasNamedResult(pass, functionType, object)
	returned := false
	ast.Inspect(body, func(node ast.Node) bool {
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		ifStmt, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		branch := errorFailureBranch(pass, ifStmt, object)
		if branch == nil {
			return true
		}
		returned = failureBranchReturnsObject(pass, body, branch, object, namedResult, after)
		return !returned
	})
	return returned
}

func returnsNamedResultOnFailurePath(pass *analysis.Pass, body *ast.BlockStmt, functionType *ast.FuncType, resultObject, errObject types.Object, after token.Pos) bool {
	if body == nil || resultObject == nil || errObject == nil ||
		!hasNamedResult(pass, functionType, resultObject) {
		return false
	}
	returned := false
	ast.Inspect(body, func(node ast.Node) bool {
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		ifStmt, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		branch := errorFailureBranch(pass, ifStmt, errObject)
		if branch == nil {
			return true
		}
		returned = failureBranchReturnsObject(pass, body, branch, resultObject, true, after)
		return !returned
	})
	return returned
}

func failureBranchReturnsObject(pass *analysis.Pass, body *ast.BlockStmt, branch ast.Stmt, object types.Object, namedResult bool, after token.Pos) bool {
	block, ok := branch.(*ast.BlockStmt)
	if !ok || len(block.List) == 0 {
		return false
	}
	last, ok := block.List[len(block.List)-1].(*ast.ReturnStmt)
	if !ok || !returnPropagatesObject(pass, body, last, object, namedResult, after) {
		return false
	}
	allReturnsPropagate := true
	ast.Inspect(block, func(node ast.Node) bool {
		if !allReturnsPropagate {
			return false
		}
		if node != block {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		ret, ok := node.(*ast.ReturnStmt)
		if ok && !returnPropagatesObject(pass, body, ret, object, namedResult, after) {
			allReturnsPropagate = false
			return false
		}
		return true
	})
	return allReturnsPropagate
}

func returnPropagatesObject(pass *analysis.Pass, body *ast.BlockStmt, ret *ast.ReturnStmt, object types.Object, namedResult bool, after token.Pos) bool {
	if len(ret.Results) == 0 {
		return namedResult && !objectReassignedBetween(pass, body, object, after, ret.Pos())
	}
	for _, result := range ret.Results {
		ident, ok := astutil.UnwrapParenExpr(result).(*ast.Ident)
		if ok && ident.Pos() > after && pass.TypesInfo.ObjectOf(ident) == object &&
			!objectReassignedBetween(pass, body, object, after, ident.Pos()) {
			return true
		}
	}
	return false
}

func hasNamedResult(pass *analysis.Pass, functionType *ast.FuncType, object types.Object) bool {
	if functionType == nil || functionType.Results == nil || object == nil {
		return false
	}
	for _, field := range functionType.Results.List {
		for _, name := range field.Names {
			if pass.TypesInfo.Defs[name] == object {
				return true
			}
		}
	}
	return false
}

func errorFailureBranch(pass *analysis.Pass, ifStmt *ast.IfStmt, object types.Object) ast.Stmt {
	condition, ok := astutil.UnwrapParenExpr(ifStmt.Cond).(*ast.BinaryExpr)
	if !ok || (condition.Op != token.NEQ && condition.Op != token.EQL) {
		return nil
	}
	left, leftIsError := astutil.UnwrapParenExpr(condition.X).(*ast.Ident)
	right, rightIsError := astutil.UnwrapParenExpr(condition.Y).(*ast.Ident)
	errorOnLeft := leftIsError && pass.TypesInfo.ObjectOf(left) == object && rightIsError && right.Name == "nil"
	errorOnRight := rightIsError && pass.TypesInfo.ObjectOf(right) == object && leftIsError && left.Name == "nil"
	if !errorOnLeft && !errorOnRight {
		return nil
	}
	if condition.Op == token.NEQ {
		return ifStmt.Body
	}
	return ifStmt.Else
}

func hasObjectUse(pass *analysis.Pass, body *ast.BlockStmt, object types.Object, after token.Pos) bool {
	if object == nil || body == nil {
		return false
	}
	index := indexObjectUses(pass, body, object, after)
	used := false
	ast.Inspect(body, func(node ast.Node) bool {
		if used {
			return false
		}
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		ident, ok := node.(*ast.Ident)
		if ok && ident.Pos() > after && pass.TypesInfo.ObjectOf(ident) == object &&
			isRetainedObjectUse(ident, index) {
			used = true
			return false
		}
		return true
	})
	return used
}

type objectUseIndex struct {
	writes        map[*ast.Ident]bool
	discarded     map[*ast.Ident]bool
	selfAssigned  map[*ast.Ident]bool
	writePosition []token.Pos
}

func indexObjectUses(pass *analysis.Pass, body *ast.BlockStmt, object types.Object, after token.Pos) objectUseIndex {
	index := objectUseIndex{
		writes:       make(map[*ast.Ident]bool),
		discarded:    make(map[*ast.Ident]bool),
		selfAssigned: make(map[*ast.Ident]bool),
	}
	ast.Inspect(body, func(node ast.Node) bool {
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		switch stmt := node.(type) {
		case *ast.AssignStmt:
			writesOutput := false
			for _, lhs := range stmt.Lhs {
				if ident, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident); ok {
					index.writes[ident] = true
					if pass.TypesInfo.ObjectOf(ident) == object {
						writesOutput = true
					}
					if ident.Name == "_" && len(stmt.Lhs) == 1 && len(stmt.Rhs) == 1 {
						if rhs, ok := astutil.UnwrapParenExpr(stmt.Rhs[0]).(*ast.Ident); ok {
							index.discarded[rhs] = true
						}
					}
				}
			}
			if writesOutput && stmt.Pos() > after {
				index.writePosition = append(index.writePosition, stmt.Pos())
				for _, rhs := range stmt.Rhs {
					ast.Inspect(rhs, func(expr ast.Node) bool {
						ident, ok := expr.(*ast.Ident)
						if ok && pass.TypesInfo.ObjectOf(ident) == object {
							index.selfAssigned[ident] = true
						}
						return true
					})
				}
			}
		case *ast.ValueSpec:
			for _, name := range stmt.Names {
				index.writes[name] = true
				if pass.TypesInfo.ObjectOf(name) == object && stmt.Pos() > after {
					index.writePosition = append(index.writePosition, stmt.Pos())
				}
			}
		}
		return true
	})
	return index
}

func isRetainedObjectUse(ident *ast.Ident, index objectUseIndex) bool {
	if index.writes[ident] || index.discarded[ident] || index.selfAssigned[ident] {
		return false
	}
	for _, writePos := range index.writePosition {
		if writePos < ident.Pos() {
			return false
		}
	}
	return true
}

func hasExitErrorStderrUse(pass *analysis.Pass, body *ast.BlockStmt, errObject types.Object, after token.Pos) bool {
	if body == nil || errObject == nil {
		return false
	}
	exitErrorObjects := findExitErrorObjects(pass, body, errObject, after)
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		if found {
			return false
		}
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Stderr" || selector.Pos() <= after ||
			!isExecExitErrorType(pass.TypesInfo.TypeOf(selector.X)) || isDiscardedValue(body, selector) {
			return true
		}
		switch receiver := astutil.UnwrapParenExpr(selector.X).(type) {
		case *ast.TypeAssertExpr:
			found = !objectReassignedBetween(pass, body, errObject, after, receiver.Pos()) &&
				isExitErrorAssertion(pass, receiver, errObject)
		case *ast.Ident:
			object := pass.TypesInfo.ObjectOf(receiver)
			derivedAt, ok := exitErrorObjects[object]
			found = ok && derivedAt < selector.Pos() &&
				!objectReassignedBetween(pass, body, object, derivedAt, selector.Pos())
		}
		return !found
	})
	return found
}

func findExitErrorObjects(pass *analysis.Pass, body *ast.BlockStmt, errObject types.Object, after token.Pos) map[types.Object]token.Pos {
	exitErrorObjects := make(map[types.Object]token.Pos)
	ast.Inspect(body, func(node ast.Node) bool {
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		switch node := node.(type) {
		case *ast.AssignStmt:
			if node.Pos() > after && len(node.Lhs) > 0 && len(node.Rhs) == 1 &&
				!objectReassignedBetween(pass, body, errObject, after, node.Pos()) &&
				isExitErrorAssertion(pass, node.Rhs[0], errObject) {
				if object := bindingObject(pass, node.Lhs[0]); object != nil {
					exitErrorObjects[object] = node.Pos()
				}
			}
		case *ast.ValueSpec:
			if node.Pos() > after && len(node.Names) > 0 && len(node.Values) == 1 &&
				!objectReassignedBetween(pass, body, errObject, after, node.Pos()) &&
				isExitErrorAssertion(pass, node.Values[0], errObject) {
				if object := bindingObject(pass, node.Names[0]); object != nil {
					exitErrorObjects[object] = node.Pos()
				}
			}
		case *ast.CallExpr:
			if node.Pos() > after && !objectReassignedBetween(pass, body, errObject, after, node.Pos()) {
				isErrorsAsForExitError(pass, node, errObject, node.Pos(), exitErrorObjects)
			}
		}
		return true
	})
	return exitErrorObjects
}

func objectReassignedBetween(pass *analysis.Pass, body *ast.BlockStmt, object types.Object, after, before token.Pos) bool {
	if body == nil || object == nil {
		return false
	}
	reassigned := false
	ast.Inspect(body, func(node ast.Node) bool {
		if reassigned {
			return false
		}
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		assign, ok := node.(*ast.AssignStmt)
		if !ok || assign.Pos() <= after || assign.Pos() >= before {
			return true
		}
		for _, lhs := range assign.Lhs {
			ident, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident)
			if ok && pass.TypesInfo.ObjectOf(ident) == object {
				reassigned = true
				return false
			}
		}
		return true
	})
	return reassigned
}

func isDiscardedValue(body *ast.BlockStmt, value ast.Node) bool {
	discarded := false
	ast.Inspect(body, func(node ast.Node) bool {
		if discarded {
			return false
		}
		assign, ok := node.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		lhs, ok := astutil.UnwrapParenExpr(assign.Lhs[0]).(*ast.Ident)
		if !ok || lhs.Name != "_" {
			return true
		}
		ast.Inspect(assign.Rhs[0], func(rhs ast.Node) bool {
			if rhs == value {
				discarded = true
				return false
			}
			return true
		})
		return !discarded
	})
	return discarded
}

func isExitErrorAssertion(pass *analysis.Pass, expr ast.Expr, errObject types.Object) bool {
	assertion, ok := astutil.UnwrapParenExpr(expr).(*ast.TypeAssertExpr)
	if !ok || !isExecExitErrorType(pass.TypesInfo.TypeOf(assertion.Type)) {
		return false
	}
	ident, ok := astutil.UnwrapParenExpr(assertion.X).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(ident) == errObject
}

func isErrorsAsForExitError(pass *analysis.Pass, call *ast.CallExpr, errObject types.Object, position token.Pos, objects map[types.Object]token.Pos) bool {
	pkgPath, name, ok := astutil.PackageCall(pass, call)
	if !ok || pkgPath != "errors" || name != "As" || len(call.Args) != 2 {
		return false
	}
	errIdent, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(errIdent) != errObject {
		return false
	}
	target, ok := astutil.UnwrapParenExpr(call.Args[1]).(*ast.UnaryExpr)
	if !ok || target.Op != token.AND {
		return false
	}
	exitErr, ok := astutil.UnwrapParenExpr(target.X).(*ast.Ident)
	if !ok || !isExecExitErrorType(pass.TypesInfo.TypeOf(exitErr)) {
		return false
	}
	object := pass.TypesInfo.ObjectOf(exitErr)
	if object != nil {
		objects[object] = position
	}
	return true
}

func isExecExitErrorType(t types.Type) bool {
	if t == nil {
		return false
	}
	if pointer, ok := types.Unalias(t).(*types.Pointer); ok {
		t = pointer.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Name() == "ExitError" && named.Obj().Pkg() != nil &&
		named.Obj().Pkg().Path() == "os/exec"
}
