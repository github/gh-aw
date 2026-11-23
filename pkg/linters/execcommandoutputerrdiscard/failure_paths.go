package execcommandoutputerrdiscard

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/astutil"
)

func returnsErrorOnFailurePath(pass *analysis.Pass, body *ast.BlockStmt, functionType *ast.FuncType, object types.Object, after token.Pos) bool {
	if body == nil || object == nil {
		return false
	}
	namedResult := hasNamedResult(pass, functionType, object)
	for _, stmt := range body.List {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok {
			continue
		}
		if ifStmt.Cond.Pos() <= after || hasReturnBefore(body, ifStmt, after) {
			continue
		}
		branch := errorFailureBranch(pass, ifStmt, object)
		if branch != nil && failureBranchReturnsObject(pass, body, branch, object, namedResult, after) {
			return true
		}
	}
	return false
}

func returnsOutputOnAllReturns(pass *analysis.Pass, body *ast.BlockStmt, functionType *ast.FuncType, resultObject types.Object, after token.Pos) bool {
	if body == nil || resultObject == nil {
		return false
	}
	foundReturn := false
	allReturnsRetain := true
	ast.Inspect(body, func(node ast.Node) bool {
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		ret, ok := node.(*ast.ReturnStmt)
		if !ok || ret.Pos() <= after {
			return true
		}
		foundReturn = true
		if !returnPropagatesObject(pass, body, ret, resultObject, hasNamedResult(pass, functionType, resultObject), after) {
			allReturnsRetain = false
			return false
		}
		return true
	})
	return foundReturn && allReturnsRetain
}

func returnsObjectOnFailurePath(pass *analysis.Pass, body *ast.BlockStmt, object, errObject types.Object, after token.Pos) bool {
	if body == nil || object == nil || errObject == nil {
		return false
	}
	for _, stmt := range body.List {
		failureIf, ok := stmt.(*ast.IfStmt)
		if !ok || failureIf.Cond.Pos() <= after || hasReturnBefore(body, failureIf, after) ||
			objectReassignedBetween(pass, body, errObject, after, failureIf.Cond.Pos()) {
			continue
		}
		branch := errorFailureBranch(pass, failureIf, errObject)
		if branch != nil && failureBranchReturnsObject(pass, body, branch, object, false, after) {
			return true
		}
	}
	return false
}

func hasRetainedObjectUseOnFailurePath(pass *analysis.Pass, body *ast.BlockStmt, object, errObject types.Object, after token.Pos) bool {
	if body == nil || object == nil || errObject == nil {
		return false
	}
	for _, stmt := range body.List {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok || ifStmt.Cond.Pos() <= after || hasReturnBefore(body, ifStmt, after) ||
			objectReassignedBetween(pass, body, errObject, after, ifStmt.Cond.Pos()) {
			continue
		}
		branch, ok := errorFailureBranch(pass, ifStmt, errObject).(*ast.BlockStmt)
		if !ok {
			continue
		}
		for _, branchStmt := range branch.List {
			switch branchStmt.(type) {
			case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
				continue
			}
			retained := false
			ast.Inspect(branchStmt, func(node ast.Node) bool {
				if node != branchStmt {
					if _, ok := node.(*ast.FuncLit); ok {
						return false
					}
				}
				call, ok := node.(*ast.CallExpr)
				if !ok || !isDiagnosticSinkCall(pass, call) {
					return true
				}
				for _, arg := range call.Args {
					if expressionUsesObject(pass, arg, object) &&
						objectRetainedUntil(pass, body, object, after, call.Pos()) {
						retained = true
						return false
					}
				}
				return !retained
			})
			if retained {
				return true
			}
		}
	}
	return false
}

func isDiagnosticSinkCall(pass *analysis.Pass, call *ast.CallExpr) bool {
	pkgPath, name, ok := astutil.PackageCall(pass, call)
	if !ok {
		return false
	}
	switch pkgPath {
	case "log":
		return name == "Print" || name == "Printf" || name == "Println"
	case "log/slog":
		return name == "Error" || name == "Warn" || name == "Info" || name == "Debug"
	default:
		return false
	}
}

func failureBranchReturnsObject(pass *analysis.Pass, body *ast.BlockStmt, branch ast.Stmt, object types.Object, namedResult bool, after token.Pos) bool {
	block, ok := branch.(*ast.BlockStmt)
	if !ok || len(block.List) == 0 {
		return false
	}
	var lastStatement ast.Stmt
	for _, statement := range block.List {
		lastStatement = statement
	}
	last, ok := lastStatement.(*ast.ReturnStmt)
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
		if !namedResult {
			return false
		}
		if isErrorObject(object) {
			return !objectReassignedBetween(pass, body, object, after, ret.Pos())
		}
		return objectRetainedUntil(pass, body, object, after, ret.Pos())
	}
	for _, result := range ret.Results {
		if isErrorObject(object) {
			if expressionPropagatesError(pass, body, result, object, after, result.Pos()) {
				return true
			}
			continue
		}
		if expressionRetainsObject(pass, body, result, object, after, result.Pos()) {
			return true
		}
	}
	return false
}

func expressionPropagatesError(pass *analysis.Pass, body *ast.BlockStmt, expr ast.Expr, object types.Object, after, before token.Pos) bool {
	if object == nil || before <= after || objectReassignedBetween(pass, body, object, after, before) {
		return false
	}
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	if ok {
		return pass.TypesInfo.ObjectOf(ident) == object
	}
	call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	if !ok {
		return false
	}
	pkgPath, name, ok := astutil.PackageCall(pass, call)
	if !ok || len(call.Args) == 0 {
		return false
	}
	if pkgPath == "errors" && name == "Join" {
		for _, arg := range call.Args {
			if expressionPropagatesError(pass, body, arg, object, after, before) {
				return true
			}
		}
	}
	if pkgPath != "fmt" || name != "Errorf" || len(call.Args) != 2 {
		return false
	}
	format, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.BasicLit) //nolint:uncheckedsliceindex // len(call.Args) is checked above
	if !ok || format.Kind != token.STRING {
		return false
	}
	formatString, err := strconv.Unquote(format.Value)
	return err == nil && hasOnlyWrapVerb(formatString) &&
		expressionPropagatesError(pass, body, call.Args[1], object, after, before) //nolint:uncheckedsliceindex // len(call.Args) is checked above
}

func hasOnlyWrapVerb(format string) bool {
	wrapCount := 0
	for i := 0; i < len(format)-1; i++ {
		if format[i] != '%' { //nolint:uncheckedsliceindex // loop stops before the end of format
			continue
		}
		if format[i+1] == '%' { //nolint:uncheckedsliceindex // loop stops before the end of format
			i++
			continue
		}
		if format[i+1] == 'w' { //nolint:uncheckedsliceindex // loop stops before the end of format
			wrapCount++
			i++
			continue
		}
		if format[i+1] == '[' { //nolint:uncheckedsliceindex // loop stops before the end of format
			closeBracket := i + 2
			for closeBracket < len(format) && format[closeBracket] >= '0' && format[closeBracket] <= '9' { //nolint:uncheckedsliceindex // closeBracket is checked against len(format)
				closeBracket++
			}
			if closeBracket > i+2 && format[i+2:closeBracket] == "1" &&
				closeBracket+1 < len(format) && format[closeBracket] == ']' && format[closeBracket+1] == 'w' { //nolint:uncheckedsliceindex // closeBracket+1 is checked against len(format)
				wrapCount++
				i = closeBracket + 1
				continue
			}
		}
		return false
	}
	return wrapCount == 1
}

func hasReturnBefore(body *ast.BlockStmt, target *ast.IfStmt, after token.Pos) bool {
	found := false
	for _, stmt := range body.List {
		if stmt.Pos() <= after {
			continue
		}
		if stmt == target || stmt.Pos() >= target.Pos() {
			break
		}
		ast.Inspect(stmt, func(node ast.Node) bool {
			if node != stmt {
				if _, ok := node.(*ast.FuncLit); ok {
					return false
				}
			}
			if _, ok := node.(*ast.ReturnStmt); ok {
				found = true
				return false
			}
			return true
		})
		if found {
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
