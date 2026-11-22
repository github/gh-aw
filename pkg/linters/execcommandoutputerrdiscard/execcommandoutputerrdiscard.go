// Package execcommandoutputerrdiscard implements a Go analysis linter that
// reports exec.Cmd output calls whose failures discard stderr diagnostics.
package execcommandoutputerrdiscard

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"

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

func isErrorObject(object types.Object) bool {
	if object == nil {
		return false
	}
	errorType := types.Universe.Lookup("error")
	return errorType != nil && types.Identical(types.Unalias(object.Type()), types.Unalias(errorType.Type()))
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
	format, ok := astutil.UnwrapParenExpr(call.Args[0]).(*ast.BasicLit)
	if !ok || format.Kind != token.STRING {
		return false
	}
	formatString, err := strconv.Unquote(format.Value)
	return err == nil && hasOnlyWrapVerb(formatString) &&
		expressionPropagatesError(pass, body, call.Args[1], object, after, before)
}

func hasOnlyWrapVerb(format string) bool {
	wrapCount := 0
	for i := 0; i < len(format)-1; i++ {
		if format[i] != '%' {
			continue
		}
		if format[i+1] == '%' {
			i++
			continue
		}
		if format[i+1] == 'w' {
			wrapCount++
			i++
			continue
		}
		if format[i+1] == '[' {
			closeBracket := i + 2
			for closeBracket < len(format) && format[closeBracket] >= '0' && format[closeBracket] <= '9' {
				closeBracket++
			}
			if closeBracket > i+2 && format[i+2:closeBracket] == "1" &&
				closeBracket+1 < len(format) && format[closeBracket] == ']' && format[closeBracket+1] == 'w' {
				wrapCount++
				i = closeBracket + 1
				continue
			}
		}
		return false
	}
	return wrapCount == 1
}

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
			if writesObject && (len(node.Rhs) != 1 || !isRetainingAppend(pass, node.Rhs[0], object)) {
				retained = false
				return false
			}
		case *ast.CallExpr:
			if node.Pos() <= after || node.Pos() >= before || len(node.Args) == 0 {
				return true
			}
			mutates := isBuiltinCall(pass, node, "clear") || isBuiltinCall(pass, node, "copy")
			mutatesAlias := isBuiltinCall(pass, node, "append") &&
				!isFullSliceAliasAt(pass, body, node.Args[0], object, aliases, after, node.Pos(), make(map[types.Object]bool)) &&
				expressionUsesObjectOrAliasAt(pass, body, node.Args[0], object, aliases, after, node.Pos())
			if (mutates || mutatesAlias) &&
				expressionUsesObjectOrAliasAt(pass, body, node.Args[0], object, aliases, after, node.Pos()) {
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
			return expressionAliasesObjectAt(pass, body, expr.Args[0], object, aliases, after, before)
		}
		return isSliceConversion(pass, expr) && len(expr.Args) == 1 &&
			expressionAliasesObjectAt(pass, body, expr.Args[0], object, aliases, after, before)
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
				expressionAliasesObjectAt(pass, body, rhs[i], object, aliases, after, node.Pos())
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
		identObject := pass.TypesInfo.ObjectOf(expr)
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
							rhs = node.Rhs[i]
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
							rhs = node.Values[i]
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
	case *ast.SliceExpr:
		return expr.Low == nil && expr.High == nil && expr.Max == nil &&
			isFullSliceAliasAt(pass, body, expr.X, object, aliases, after, before, visiting)
	case *ast.CallExpr:
		if isSliceConversion(pass, expr) && len(expr.Args) == 1 {
			return isFullSliceAliasAt(pass, body, expr.Args[0], object, aliases, after, before, visiting)
		}
		if !isBuiltinCall(pass, expr, "append") || len(expr.Args) == 0 {
			return false
		}
		if isFullSliceAliasAt(pass, body, expr.Args[0], object, aliases, after, before, visiting) {
			return true
		}
		return expr.Ellipsis.IsValid() &&
			isFullSliceAliasAt(pass, body, expr.Args[len(expr.Args)-1], object, aliases, after, before, visiting)
	default:
		return false
	}
}

func isSliceConversion(pass *analysis.Pass, call *ast.CallExpr) bool {
	if !pass.TypesInfo.Types[call.Fun].IsType() || len(call.Args) != 1 {
		return false
	}
	target, targetOK := pass.TypesInfo.TypeOf(call).Underlying().(*types.Slice)
	source, sourceOK := pass.TypesInfo.TypeOf(call.Args[0]).Underlying().(*types.Slice)
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
	if body == nil {
		return false
	}
	method, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return false
	}
	receiver, ok := astutil.UnwrapParenExpr(method.X).(*ast.Ident)
	if !ok {
		return false
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
		if commandIdent, ok := astutil.UnwrapParenExpr(assign.Lhs[0]).(*ast.Ident); ok &&
			pass.TypesInfo.ObjectOf(commandIdent) == commandObject {
			streamed = false
			capturedObject = nil
			sinkPosition = token.NoPos
			continue
		}
		stderrSelector, ok := astutil.UnwrapParenExpr(assign.Lhs[0]).(*ast.SelectorExpr)
		if !ok || stderrSelector.Sel.Name != "Stderr" {
			continue
		}
		commandIdent, ok := astutil.UnwrapParenExpr(stderrSelector.X).(*ast.Ident)
		if !ok || pass.TypesInfo.ObjectOf(commandIdent) != commandObject {
			continue
		}
		streamed = isOSStderr(pass, assign.Rhs[0])
		capturedObject = stderrWriterObject(pass, assign.Rhs[0])
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
	if streamed {
		return true
	}
	if capturedObject == nil {
		return false
	}
	if errObject == nil {
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
			if node.Pos() <= after || node.Pos() >= before {
				return true
			}
			if len(node.Lhs) == len(node.Rhs) {
				for i, lhs := range node.Lhs {
					if _, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident); ok &&
						expressionUsesBufferBytes(pass, node.Rhs[i], object, aliases) {
						consumed = true
						return false
					}
				}
			}
			for _, lhs := range node.Lhs {
				index, ok := astutil.UnwrapParenExpr(lhs).(*ast.IndexExpr)
				if ok && expressionUsesBufferBytes(pass, index.X, object, aliases) {
					consumed = true
					return false
				}
			}
		case *ast.CallExpr:
			if node.Pos() <= after || node.Pos() >= before {
				return true
			}
			if (isBuiltinCall(pass, node, "clear") || isBuiltinCall(pass, node, "copy") ||
				isBuiltinCall(pass, node, "append")) &&
				len(node.Args) > 0 && expressionUsesBufferBytes(pass, node.Args[0], object, aliases) {
				consumed = true
				return false
			}
			selector, ok := astutil.UnwrapParenExpr(node.Fun).(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, ok := astutil.UnwrapParenExpr(selector.X).(*ast.Ident)
			if !ok {
				return true
			}
			receiverObject := pass.TypesInfo.ObjectOf(receiver)
			if receiverObject != object && !aliases[receiverObject] {
				return true
			}
			switch selector.Sel.Name {
			case "Reset", "Truncate", "Read", "ReadByte", "ReadBytes", "ReadRune", "Next", "WriteTo":
				consumed = true
				return false
			}
		}
		return true
	})
	return consumed
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
				addAlias(lhs, node.Rhs[i])
			}
		case *ast.ValueSpec:
			if node.Pos() <= after || node.Pos() >= before || len(node.Names) != len(node.Values) {
				return true
			}
			for i, name := range node.Names {
				addAlias(name, node.Values[i])
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
				if isExitErrorAssertion(pass, node.Rhs[0], errObject) {
					if object := bindingObject(pass, node.Lhs[0]); object != nil {
						exitErrorObjects[object] = node.Pos()
					}
				}
			}
		case *ast.ValueSpec:
			if node.Pos() > after && len(node.Names) > 0 && len(node.Values) == 1 &&
				!objectReassignedBetween(pass, body, errObject, after, node.Pos()) {
				if isExitErrorAssertion(pass, node.Values[0], errObject) {
					if object := bindingObject(pass, node.Names[0]); object != nil {
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
	errArg, targetArg := call.Args[0], call.Args[1]
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
