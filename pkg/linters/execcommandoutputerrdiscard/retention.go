package execcommandoutputerrdiscard

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/astutil"
)

func expressionRetainsObject(pass *analysis.Pass, body *ast.BlockStmt, expr ast.Expr, object types.Object, after, before token.Pos) bool {
	if object == nil || before <= after || !objectRetainedUntil(pass, body, object, after, before) {
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
	if !isBuiltinCall(pass, call, "append") {
		return false
	}
	for i, arg := range call.Args {
		if i == 0 && isObjectIdent(pass, arg, object) {
			return true
		}
		if call.Ellipsis.IsValid() && i == len(call.Args)-1 && isObjectIdent(pass, arg, object) {
			return true
		}
	}
	return false
}
func objectRetainedUntil(pass *analysis.Pass, body *ast.BlockStmt, object types.Object, after, before token.Pos) bool {
	if body == nil || object == nil {
		return false
	}
	aliases := findSliceAliases(pass, body, object, after, before)
	retained := true
	ast.Inspect(body, func(node ast.Node) bool {
		if !retained {
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
			writesObject := false
			for _, lhs := range node.Lhs {
				switch lhs := astutil.UnwrapParenExpr(lhs).(type) {
				case *ast.Ident:
					lhsObject := pass.TypesInfo.ObjectOf(lhs)
					writesObject = lhsObject == object
				case *ast.IndexExpr:
					writesObject = expressionUsesObjectOrAliasAt(pass, body, lhs.X, object, aliases, after, node.Pos())
				}
				if writesObject {
					break
				}
			}
			if writesObject && (len(node.Rhs) != 1 || !isRetainingAppend(pass, node.Rhs[0], object)) { //nolint:uncheckedsliceindex // len(node.Rhs) is checked above
				retained = false
				return false
			}
		case *ast.CallExpr:
			if node.Pos() <= after || node.Pos() >= before || len(node.Args) == 0 {
				return true
			}
			mutates := isBuiltinCall(pass, node, "clear") || isBuiltinCall(pass, node, "copy")
			mutatesAlias := isBuiltinCall(pass, node, "append") &&
				!isFullSliceAliasAt(pass, body, node.Args[0], object, aliases, after, node.Pos(), make(map[types.Object]bool)) && //nolint:uncheckedsliceindex // len(node.Args) is checked above
				expressionUsesObjectOrAliasAt(pass, body, node.Args[0], object, aliases, after, node.Pos()) //nolint:uncheckedsliceindex // len(node.Args) is checked above
			if (mutates || mutatesAlias) &&
				expressionUsesObjectOrAliasAt(pass, body, node.Args[0], object, aliases, after, node.Pos()) { //nolint:uncheckedsliceindex // len(node.Args) is checked above
				retained = false
				return false
			}
		}
		return true
	})
	return retained
}
func findSliceAliases(pass *analysis.Pass, body *ast.BlockStmt, object types.Object, after, before token.Pos) map[types.Object]bool {
	aliases := make(map[types.Object]bool)
	addAlias := func(lhs ast.Expr) {
		ident, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident)
		if !ok {
			return
		}
		alias := pass.TypesInfo.ObjectOf(ident)
		if alias != nil && alias != object &&
			types.Identical(types.Unalias(alias.Type()).Underlying(), types.Unalias(object.Type()).Underlying()) {
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
			if node.Pos() <= after || node.Pos() >= before {
				return true
			}
			for _, lhs := range node.Lhs {
				addAlias(lhs)
			}
		case *ast.ValueSpec:
			if node.Pos() <= after || node.Pos() >= before {
				return true
			}
			for _, name := range node.Names {
				addAlias(name)
			}
		}
		return true
	})
	return aliases
}
func expressionAliasesObjectAt(pass *analysis.Pass, body *ast.BlockStmt, expr ast.Expr, object types.Object, aliases map[types.Object]bool, after, before token.Pos) bool {
	switch expr := astutil.UnwrapParenExpr(expr).(type) {
	case *ast.Ident:
		identObject := pass.TypesInfo.ObjectOf(expr)
		return identObject == object ||
			aliases[identObject] && sliceAliasMayAliasAt(pass, body, identObject, object, aliases, after, before)
	case *ast.SliceExpr:
		return expressionAliasesObjectAt(pass, body, expr.X, object, aliases, after, before)
	case *ast.CallExpr:
		if isBuiltinCall(pass, expr, "append") && len(expr.Args) > 0 {
			return expressionAliasesObjectAt(pass, body, expr.Args[0], object, aliases, after, before) //nolint:uncheckedsliceindex // len(expr.Args) is checked above
		}
		return isSliceConversion(pass, expr) && len(expr.Args) == 1 &&
			expressionAliasesObjectAt(pass, body, expr.Args[0], object, aliases, after, before) //nolint:uncheckedsliceindex // len(expr.Args) is checked above
	default:
		return false
	}
}
func sliceAliasMayAliasAt(pass *analysis.Pass, body *ast.BlockStmt, alias, object types.Object, aliases map[types.Object]bool, after, before token.Pos) bool {
	mayAlias := false
	ast.Inspect(body, func(node ast.Node) bool {
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		var lhs []ast.Expr
		var rhs []ast.Expr
		switch node := node.(type) {
		case *ast.AssignStmt:
			lhs, rhs = node.Lhs, node.Rhs
		case *ast.ValueSpec:
			for _, name := range node.Names {
				lhs = append(lhs, name)
			}
			rhs = node.Values
		default:
			return true
		}
		if node.Pos() <= after || node.End() >= before {
			return true
		}
		for i, target := range lhs {
			ident, ok := astutil.UnwrapParenExpr(target).(*ast.Ident)
			if !ok || pass.TypesInfo.ObjectOf(ident) != alias {
				continue
			}
			assignedMayAlias := i < len(rhs) && len(lhs) == len(rhs) &&
				expressionAliasesObjectAt(pass, body, rhs[i], object, aliases, after, node.Pos()) //nolint:uncheckedsliceindex // index and slice lengths are checked above
			if isDirectFunctionBodyAssignment(body, node) {
				mayAlias = assignedMayAlias
			} else {
				mayAlias = mayAlias || assignedMayAlias
			}
		}
		return true
	})
	return mayAlias
}
func isFullSliceAliasAt(pass *analysis.Pass, body *ast.BlockStmt, expr ast.Expr, object types.Object, aliases map[types.Object]bool, after, before token.Pos, visiting map[types.Object]bool) bool {
	switch expr := astutil.UnwrapParenExpr(expr).(type) {
	case *ast.Ident:
		return isFullSliceAliasOfIdentAt(pass, body, expr, object, aliases, after, before, visiting)
	case *ast.SliceExpr:
		return expr.Low == nil && expr.High == nil && expr.Max == nil &&
			isFullSliceAliasAt(pass, body, expr.X, object, aliases, after, before, visiting)
	case *ast.CallExpr:
		if isSliceConversion(pass, expr) && len(expr.Args) == 1 {
			return isFullSliceAliasAt(pass, body, expr.Args[0], object, aliases, after, before, visiting) //nolint:uncheckedsliceindex // len(expr.Args) is checked above
		}
		if !isBuiltinCall(pass, expr, "append") || len(expr.Args) == 0 {
			return false
		}
		if isFullSliceAliasAt(pass, body, expr.Args[0], object, aliases, after, before, visiting) { //nolint:uncheckedsliceindex // append has at least one argument
			return true
		}
		return expr.Ellipsis.IsValid() &&
			isFullSliceAliasAt(pass, body, expr.Args[len(expr.Args)-1], object, aliases, after, before, visiting) //nolint:uncheckedsliceindex // append has at least one argument
	default:
		return false
	}
}

func isFullSliceAliasOfIdentAt(pass *analysis.Pass, body *ast.BlockStmt, ident *ast.Ident, object types.Object, aliases map[types.Object]bool, after, before token.Pos, visiting map[types.Object]bool) bool {
	identObject := pass.TypesInfo.ObjectOf(ident)
	if identObject == object {
		return true
	}
	if !aliases[identObject] || visiting[identObject] {
		return false
	}
	visiting[identObject] = true
	var rhs ast.Expr
	var latestPos token.Pos
	found := false
	latestDirect := false
	ast.Inspect(body, func(node ast.Node) bool {
		if node != body {
			if _, ok := node.(*ast.FuncLit); ok {
				return false
			}
		}
		switch node := node.(type) {
		case *ast.AssignStmt:
			if node.Pos() <= after || node.End() > before || node.Pos() < latestPos {
				return true
			}
			for i, lhs := range node.Lhs {
				lhsIdent, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident)
				if ok && pass.TypesInfo.ObjectOf(lhsIdent) == identObject {
					latestPos = node.Pos()
					found = true
					latestDirect = isDirectFunctionBodyAssignment(body, node)
					if i < len(node.Rhs) && len(node.Lhs) == len(node.Rhs) {
						rhs = node.Rhs[i] //nolint:uncheckedsliceindex // index and slice lengths are checked above
					} else {
						rhs = nil
					}
				}
			}
		case *ast.ValueSpec:
			if node.Pos() <= after || node.End() > before || node.Pos() < latestPos {
				return true
			}
			for i, name := range node.Names {
				if pass.TypesInfo.ObjectOf(name) == identObject {
					latestPos = node.Pos()
					found = true
					latestDirect = isDirectFunctionBodyAssignment(body, node)
					if i < len(node.Values) && len(node.Names) == len(node.Values) {
						rhs = node.Values[i] //nolint:uncheckedsliceindex // index and slice lengths are checked above
					} else {
						rhs = nil
					}
				}
			}
		}
		return true
	})
	if !found || rhs == nil || !latestDirect {
		return false
	}
	return isFullSliceAliasAt(pass, body, rhs, object, aliases, after, latestPos, visiting)
}
func isSliceConversion(pass *analysis.Pass, call *ast.CallExpr) bool {
	if !pass.TypesInfo.Types[call.Fun].IsType() || len(call.Args) != 1 {
		return false
	}
	target, targetOK := pass.TypesInfo.TypeOf(call).Underlying().(*types.Slice)
	source, sourceOK := pass.TypesInfo.TypeOf(call.Args[0]).Underlying().(*types.Slice) //nolint:uncheckedsliceindex // len(call.Args) is checked above
	return targetOK && sourceOK && types.Identical(target, source)
}
func isDirectFunctionBodyAssignment(body *ast.BlockStmt, node ast.Node) bool {
	for _, statement := range body.List {
		if statement == node {
			return true
		}
		declaration, ok := statement.(*ast.DeclStmt)
		if !ok {
			continue
		}
		general, ok := declaration.Decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range general.Specs {
			if spec == node {
				return true
			}
		}
	}
	return false
}
func expressionUsesObjectOrAliasAt(pass *analysis.Pass, body *ast.BlockStmt, expr ast.Expr, object types.Object, aliases map[types.Object]bool, after, before token.Pos) bool {
	usesObject := false
	ast.Inspect(expr, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok {
			identObject := pass.TypesInfo.ObjectOf(ident)
			if identObject == object ||
				aliases[identObject] && sliceAliasMayAliasAt(pass, body, identObject, object, aliases, after, before) {
				usesObject = true
				return false
			}
		}
		return !usesObject
	})
	return usesObject
}
func isBuiltinCall(pass *analysis.Pass, call *ast.CallExpr, name string) bool {
	ident, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.Ident)
	if !ok || ident.Name != name {
		return false
	}
	builtin, ok := pass.TypesInfo.Uses[ident].(*types.Builtin)
	return ok && builtin.Name() == name
}
func isRetainingAppend(pass *analysis.Pass, expr ast.Expr, object types.Object) bool {
	call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	if !ok {
		return false
	}
	if !isBuiltinCall(pass, call, "append") {
		return false
	}
	for i, arg := range call.Args {
		if i == 0 && isObjectIdent(pass, arg, object) {
			return true
		}
		if call.Ellipsis.IsValid() && i == len(call.Args)-1 && isObjectIdent(pass, arg, object) {
			return true
		}
	}
	return false
}
