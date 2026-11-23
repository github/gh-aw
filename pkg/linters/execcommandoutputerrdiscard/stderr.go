package execcommandoutputerrdiscard

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/astutil"
)

func hasExitErrorStderrUse(pass *analysis.Pass, body *ast.BlockStmt, errObject types.Object, after token.Pos) bool {
	if body == nil || errObject == nil {
		return false
	}
	exitErrorObjects := findExitErrorObjects(pass, body, errObject, after)
	for _, stmt := range body.List {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok || ifStmt.Cond.Pos() <= after || hasReturnBefore(body, ifStmt, after) ||
			objectReassignedBetween(pass, body, errObject, after, ifStmt.Cond.Pos()) {
			continue
		}
		branch := errorFailureBranch(pass, ifStmt, errObject)
		if branch == nil {
			continue
		}
		found := false
		ast.Inspect(branch, func(node ast.Node) bool {
			if node != branch {
				if _, ok := node.(*ast.FuncLit); ok {
					return false
				}
			}
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Stderr" || selector.Pos() <= after ||
				!isExecExitErrorType(pass.TypesInfo.TypeOf(selector.X)) ||
				isDiscardedValue(body, selector) || isAssignmentDestination(body, selector) ||
				stderrFieldWrittenBetween(pass, body, selector.X, after, selector.Pos()) {
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
		if found {
			return true
		}
	}
	return false
}
func hasConfiguredStderrUse(pass *analysis.Pass, body *ast.BlockStmt, call *ast.CallExpr, errObject types.Object, after token.Pos) bool {
	streamed, capturedObject := configuredStderrWriter(pass, body, call, after)
	if streamed {
		return true
	}
	if capturedObject == nil || errObject == nil {
		return false
	}
	for _, stmt := range body.List {
		ifStmt, ok := stmt.(*ast.IfStmt)
		if !ok || ifStmt.Cond.Pos() <= after || hasReturnBefore(body, ifStmt, after) ||
			objectReassignedBetween(pass, body, errObject, after, ifStmt.Cond.Pos()) {
			continue
		}
		branch := errorFailureBranch(pass, ifStmt, errObject)
		if branch != nil && hasRetainedCapturedStderrUse(pass, body, branch, capturedObject, after) {
			return true
		}
	}
	return false
}

func configuredStderrWriter(pass *analysis.Pass, body *ast.BlockStmt, call *ast.CallExpr, after token.Pos) (bool, types.Object) {
	if body == nil {
		return false, nil
	}
	method, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false, nil
	}
	receiver, ok := astutil.UnwrapParenExpr(method.X).(*ast.Ident)
	if !ok {
		return false, nil
	}
	commandObject := pass.TypesInfo.ObjectOf(receiver)
	var capturedObject types.Object
	streamed := false
	var sinkPosition token.Pos
	for _, stmt := range body.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok || assign.Pos() >= after || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			continue
		}
		if commandIdent, ok := astutil.UnwrapParenExpr(assign.Lhs[0]).(*ast.Ident); ok && //nolint:uncheckedsliceindex // len(assign.Lhs) is checked above
			pass.TypesInfo.ObjectOf(commandIdent) == commandObject {
			streamed = false
			capturedObject = nil
			sinkPosition = token.NoPos
			continue
		}
		stderrSelector, ok := astutil.UnwrapParenExpr(assign.Lhs[0]).(*ast.SelectorExpr) //nolint:uncheckedsliceindex // len(assign.Lhs) is checked above
		if !ok || stderrSelector.Sel.Name != "Stderr" {
			continue
		}
		commandIdent, ok := astutil.UnwrapParenExpr(stderrSelector.X).(*ast.Ident)
		if !ok || pass.TypesInfo.ObjectOf(commandIdent) != commandObject {
			continue
		}
		streamed = isOSStderr(pass, assign.Rhs[0])               //nolint:uncheckedsliceindex // len(assign.Rhs) is checked above
		capturedObject = stderrWriterObject(pass, assign.Rhs[0]) //nolint:uncheckedsliceindex // len(assign.Rhs) is checked above
		sinkPosition = assign.Pos()
	}
	if sinkPosition.IsValid() {
		if objectReassignedBetween(pass, body, commandObject, sinkPosition, after) ||
			stderrSinkReassignedBetween(pass, body, commandObject, sinkPosition, after) {
			streamed = false
			capturedObject = nil
		} else if capturedObject != nil && objectReassignedBetween(pass, body, capturedObject, sinkPosition, after) {
			capturedObject = nil
		}
	}
	return streamed, capturedObject
}
func isOSStderr(pass *analysis.Pass, expr ast.Expr) bool {
	selector, ok := astutil.UnwrapParenExpr(expr).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Stderr" {
		return false
	}
	pkgIdent, ok := astutil.UnwrapParenExpr(selector.X).(*ast.Ident)
	if !ok {
		return false
	}
	pkgName, ok := pass.TypesInfo.Uses[pkgIdent].(*types.PkgName)
	return ok && pkgName.Imported().Path() == "os"
}
func stderrWriterObject(pass *analysis.Pass, expr ast.Expr) types.Object {
	if !isKnownCapturedStderrWriter(pass.TypesInfo.TypeOf(expr)) {
		return nil
	}
	switch expr := astutil.UnwrapParenExpr(expr).(type) {
	case *ast.UnaryExpr:
		if expr.Op == token.AND {
			return bindingObject(pass, expr.X)
		}
	case *ast.Ident:
		return pass.TypesInfo.ObjectOf(expr)
	}
	return nil
}
func isKnownCapturedStderrWriter(t types.Type) bool {
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	if pointer, ok := t.(*types.Pointer); ok {
		t = types.Unalias(pointer.Elem())
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "bytes" && named.Obj().Name() == "Buffer" ||
		named.Obj().Pkg().Path() == "strings" && named.Obj().Name() == "Builder"
}
func stderrSinkReassignedBetween(pass *analysis.Pass, body *ast.BlockStmt, commandObject types.Object, after, before token.Pos) bool {
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
			selector, ok := astutil.UnwrapParenExpr(lhs).(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Stderr" {
				continue
			}
			receiver, ok := astutil.UnwrapParenExpr(selector.X).(*ast.Ident)
			if ok && pass.TypesInfo.ObjectOf(receiver) == commandObject {
				reassigned = true
				return false
			}
		}
		return true
	})
	return reassigned
}
func capturedStderrConsumedBetween(pass *analysis.Pass, body *ast.BlockStmt, object types.Object, after, before token.Pos) bool {
	aliases := findCapturedBufferAliases(pass, body, object, after, before)
	consumed := false
	ast.Inspect(body, func(node ast.Node) bool {
		if consumed {
			return false
		}
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		switch node := node.(type) {
		case *ast.AssignStmt:
			consumed = assignmentConsumesCapturedStderr(pass, node, object, aliases, after, before)
		case *ast.CallExpr:
			consumed = callConsumesCapturedStderr(pass, node, object, aliases, after, before)
		}
		return !consumed
	})
	return consumed
}

func assignmentConsumesCapturedStderr(pass *analysis.Pass, assign *ast.AssignStmt, object types.Object, aliases map[types.Object]bool, after, before token.Pos) bool {
	if assign.Pos() <= after || assign.Pos() >= before {
		return false
	}
	if len(assign.Lhs) == len(assign.Rhs) {
		for i, lhs := range assign.Lhs {
			if _, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident); ok &&
				expressionUsesBufferBytes(pass, assign.Rhs[i], object, aliases) { //nolint:uncheckedsliceindex // index and slice lengths are checked above
				return true
			}
		}
	}
	for _, lhs := range assign.Lhs {
		index, ok := astutil.UnwrapParenExpr(lhs).(*ast.IndexExpr)
		if ok && expressionUsesBufferBytes(pass, index.X, object, aliases) {
			return true
		}
	}
	return false
}

func callConsumesCapturedStderr(pass *analysis.Pass, call *ast.CallExpr, object types.Object, aliases map[types.Object]bool, after, before token.Pos) bool {
	if call.Pos() <= after || call.Pos() >= before {
		return false
	}
	if (isBuiltinCall(pass, call, "clear") || isBuiltinCall(pass, call, "copy") ||
		isBuiltinCall(pass, call, "append")) &&
		len(call.Args) > 0 && expressionUsesBufferBytes(pass, call.Args[0], object, aliases) { //nolint:uncheckedsliceindex // len(call.Args) is checked above
		return true
	}
	selector, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	receiver, ok := astutil.UnwrapParenExpr(selector.X).(*ast.Ident)
	if !ok {
		return false
	}
	receiverObject := pass.TypesInfo.ObjectOf(receiver)
	if receiverObject != object && !aliases[receiverObject] {
		return false
	}
	switch selector.Sel.Name {
	case "Reset", "Truncate", "Read", "ReadByte", "ReadBytes", "ReadRune", "Next", "WriteTo":
		return true
	}
	return false
}
func findCapturedBufferAliases(pass *analysis.Pass, body *ast.BlockStmt, object types.Object, after, before token.Pos) map[types.Object]bool {
	aliases := make(map[types.Object]bool)
	isAlias := func(expr ast.Expr) bool {
		switch expr := astutil.UnwrapParenExpr(expr).(type) {
		case *ast.Ident:
			identObject := pass.TypesInfo.ObjectOf(expr)
			return identObject == object || aliases[identObject]
		case *ast.UnaryExpr:
			if expr.Op != token.AND {
				return false
			}
			ident, ok := astutil.UnwrapParenExpr(expr.X).(*ast.Ident)
			if !ok {
				return false
			}
			identObject := pass.TypesInfo.ObjectOf(ident)
			return identObject == object || aliases[identObject]
		default:
			return false
		}
	}
	addAlias := func(lhs, rhs ast.Expr) {
		ident, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident)
		if !ok {
			return
		}
		alias := pass.TypesInfo.ObjectOf(ident)
		if alias != nil && alias != object && isAlias(rhs) {
			aliases[alias] = true
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		switch node := node.(type) {
		case *ast.AssignStmt:
			if node.Pos() <= after || node.Pos() >= before || len(node.Lhs) != len(node.Rhs) {
				return true
			}
			for i, lhs := range node.Lhs {
				addAlias(lhs, node.Rhs[i]) //nolint:uncheckedsliceindex // index and slice lengths are checked above
			}
		case *ast.ValueSpec:
			if node.Pos() <= after || node.Pos() >= before || len(node.Names) != len(node.Values) {
				return true
			}
			for i, name := range node.Names {
				addAlias(name, node.Values[i]) //nolint:uncheckedsliceindex // index and slice lengths are checked above
			}
		}
		return true
	})
	return aliases
}
func expressionUsesBufferBytes(pass *analysis.Pass, expr ast.Expr, object types.Object, aliases map[types.Object]bool) bool {
	usesBytes := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if usesBytes {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Bytes" {
			return true
		}
		receiver, ok := astutil.UnwrapParenExpr(selector.X).(*ast.Ident)
		if ok {
			receiverObject := pass.TypesInfo.ObjectOf(receiver)
			if receiverObject == object || aliases[receiverObject] {
				usesBytes = true
				return false
			}
		}
		return true
	})
	return usesBytes
}
func hasRetainedCapturedStderrUse(pass *analysis.Pass, body *ast.BlockStmt, branch ast.Stmt, object types.Object, after token.Pos) bool {
	found := false
	ast.Inspect(branch, func(node ast.Node) bool {
		if found {
			return false
		}
		if node != branch {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		call, ok := node.(*ast.CallExpr)
		if !ok || isDiscardedValue(body, call) {
			return true
		}
		selector, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "String" && selector.Sel.Name != "Bytes") {
			return true
		}
		receiver, ok := astutil.UnwrapParenExpr(selector.X).(*ast.Ident)
		if !ok || pass.TypesInfo.ObjectOf(receiver) != object {
			return true
		}
		if objectReassignedBetween(pass, body, object, after, call.Pos()) ||
			capturedStderrConsumedBetween(pass, body, object, after, call.Pos()) {
			return true
		}
		ast.Inspect(branch, func(parent ast.Node) bool {
			switch parent := parent.(type) {
			case *ast.ReturnStmt:
				for _, result := range parent.Results {
					if nodeContains(result, call) {
						found = true
						return false
					}
				}
			case *ast.CallExpr:
				if parent == call {
					return true
				}
				for _, arg := range parent.Args {
					if !nodeContains(arg, call) {
						continue
					}
					pkgPath, name, ok := astutil.PackageCall(pass, parent)
					if ok && (pkgPath == "log" && (name == "Print" || name == "Printf" || name == "Println") ||
						pkgPath == "log/slog" && (name == "Error" || name == "Warn" || name == "Info" || name == "Debug")) {
						found = true
						return false
					}
					if ok && pkgPath == "fmt" && name == "Errorf" && returnedCall(branch, parent) {
						found = true
						return false
					}
				}
			}
			return !found
		})
		return !found
	})
	return found
}
func returnedCall(branch ast.Node, call *ast.CallExpr) bool {
	returned := false
	ast.Inspect(branch, func(node ast.Node) bool {
		if ret, ok := node.(*ast.ReturnStmt); ok {
			for _, result := range ret.Results {
				if nodeContains(result, call) {
					returned = true
					return false
				}
			}
		}
		return !returned
	})
	return returned
}
func stderrFieldWrittenBetween(pass *analysis.Pass, body *ast.BlockStmt, receiver ast.Expr, after, before token.Pos) bool {
	written := false
	ast.Inspect(body, func(node ast.Node) bool {
		if written {
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
			selector, ok := astutil.UnwrapParenExpr(lhs).(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "Stderr" && sameExitErrorReceiver(pass, selector.X, receiver) {
				written = true
				return false
			}
		}
		return true
	})
	return written
}
