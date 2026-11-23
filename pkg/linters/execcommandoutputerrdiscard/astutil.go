package execcommandoutputerrdiscard

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/astutil"
)

func isErrorObject(object types.Object) bool {
	if object == nil {
		return false
	}
	errorType := types.Universe.Lookup("error")
	return errorType != nil && types.Identical(types.Unalias(object.Type()), types.Unalias(errorType.Type()))
}

func isObjectIdent(pass *analysis.Pass, expr ast.Expr, object types.Object) bool {
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(ident) == object
}

func expressionUsesObject(pass *analysis.Pass, expr ast.Expr, object types.Object) bool {
	usesObject := false
	ast.Inspect(expr, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok && pass.TypesInfo.ObjectOf(ident) == object {
			usesObject = true
			return false
		}
		return !usesObject
	})
	return usesObject
}

func nodeContains(root, target ast.Node) bool {
	contains := false
	ast.Inspect(root, func(node ast.Node) bool {
		if node == target {
			contains = true
			return false
		}
		return !contains
	})
	return contains
}

func isAssignmentDestination(body *ast.BlockStmt, target *ast.SelectorExpr) bool {
	destination := false
	ast.Inspect(body, func(node ast.Node) bool {
		if destination {
			return false
		}
		switch stmt := node.(type) {
		case *ast.AssignStmt:
			for _, lhs := range stmt.Lhs {
				if lhs == target {
					destination = true
					return false
				}
			}
		case *ast.IncDecStmt:
			destination = stmt.X == target
		}
		return !destination
	})
	return destination
}

func sameExitErrorReceiver(pass *analysis.Pass, left, right ast.Expr) bool {
	left, right = astutil.UnwrapParenExpr(left), astutil.UnwrapParenExpr(right)
	leftAssert, leftIsAssert := left.(*ast.TypeAssertExpr)
	rightAssert, rightIsAssert := right.(*ast.TypeAssertExpr)
	if leftIsAssert || rightIsAssert {
		if !leftIsAssert || !rightIsAssert {
			return false
		}
		leftIdent, leftOK := astutil.UnwrapParenExpr(leftAssert.X).(*ast.Ident)
		rightIdent, rightOK := astutil.UnwrapParenExpr(rightAssert.X).(*ast.Ident)
		return leftOK && rightOK && pass.TypesInfo.ObjectOf(leftIdent) == pass.TypesInfo.ObjectOf(rightIdent)
	}
	leftIdent, leftOK := left.(*ast.Ident)
	rightIdent, rightOK := right.(*ast.Ident)
	return leftOK && rightOK && pass.TypesInfo.ObjectOf(leftIdent) == pass.TypesInfo.ObjectOf(rightIdent)
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
				!objectReassignedBetween(pass, body, errObject, after, node.Pos()) {
				if isExitErrorAssertion(pass, node.Rhs[0], errObject) { //nolint:uncheckedsliceindex // len(node.Rhs) is checked above
					if object := bindingObject(pass, node.Lhs[0]); object != nil { //nolint:uncheckedsliceindex // len(node.Lhs) is checked above
						exitErrorObjects[object] = node.Pos()
					}
				}
			}
		case *ast.ValueSpec:
			if node.Pos() > after && len(node.Names) > 0 && len(node.Values) == 1 &&
				!objectReassignedBetween(pass, body, errObject, after, node.Pos()) {
				if isExitErrorAssertion(pass, node.Values[0], errObject) { //nolint:uncheckedsliceindex // len(node.Values) is checked above
					if object := bindingObject(pass, node.Names[0]); object != nil { //nolint:uncheckedsliceindex // len(node.Names) is checked above
						exitErrorObjects[object] = node.Pos()
					}
				}
			}
		case *ast.CallExpr:
			if node.Pos() > after && !objectReassignedBetween(pass, body, errObject, after, node.Pos()) {
				isErrorsAsForExitError(pass, node, errObject, node.End(), exitErrorObjects)
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
		switch node := node.(type) {
		case *ast.AssignStmt:
			if node.Pos() <= after || node.Pos() >= before {
				return true
			}
			for _, lhs := range node.Lhs {
				ident, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident)
				if ok && pass.TypesInfo.ObjectOf(ident) == object {
					reassigned = true
					return false
				}
			}
		case *ast.IncDecStmt:
			ident, ok := astutil.UnwrapParenExpr(node.X).(*ast.Ident)
			if ok && node.Pos() > after && node.Pos() < before && pass.TypesInfo.ObjectOf(ident) == object {
				reassigned = true
				return false
			}
		case *ast.CallExpr:
			if node.Pos() <= after || node.Pos() >= before {
				return true
			}
			for _, arg := range node.Args {
				address, ok := astutil.UnwrapParenExpr(arg).(*ast.UnaryExpr)
				if !ok || address.Op != token.AND {
					continue
				}
				ident, ok := astutil.UnwrapParenExpr(address.X).(*ast.Ident)
				if ok && pass.TypesInfo.ObjectOf(ident) == object {
					reassigned = true
					return false
				}
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
		lhs, ok := astutil.UnwrapParenExpr(assign.Lhs[0]).(*ast.Ident) //nolint:uncheckedsliceindex // len(assign.Lhs) is checked above
		if !ok || lhs.Name != "_" {
			return true
		}
		ast.Inspect(assign.Rhs[0], func(rhs ast.Node) bool { //nolint:uncheckedsliceindex // len(assign.Rhs) is checked above
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
	errArg, targetArg := call.Args[0], call.Args[1] //nolint:uncheckedsliceindex // len(call.Args) is checked above
	errIdent, ok := astutil.UnwrapParenExpr(errArg).(*ast.Ident)
	if !ok || pass.TypesInfo.ObjectOf(errIdent) != errObject {
		return false
	}
	target, ok := astutil.UnwrapParenExpr(targetArg).(*ast.UnaryExpr)
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
