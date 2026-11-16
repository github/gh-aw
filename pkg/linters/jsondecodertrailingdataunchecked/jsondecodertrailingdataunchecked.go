// Package jsondecodertrailingdataunchecked reports in-memory JSON decoders
// whose first Decode is not followed by a Decode that rejects non-EOF results.
package jsondecodertrailingdataunchecked

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const linterName = "jsondecodertrailingdataunchecked"

// Analyzer is the json-decoder-trailing-data-unchecked analysis pass.
var Analyzer = analyzerutil.New(linterName, "reports in-memory JSON decoders whose first Decode is not followed by a Decode that rejects non-EOF results", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch fn := node.(type) {
			case *ast.FuncDecl:
				analyzeFunction(pass, fn.Body, generatedFiles, noLintIndex)
			case *ast.FuncLit:
				analyzeFunction(pass, fn.Body, generatedFiles, noLintIndex)
			}
			return true
		})
	}
	return nil, nil
}

func analyzeFunction(pass *analysis.Pass, body *ast.BlockStmt, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	if body == nil {
		return
	}

	parents := parentNodes(body)
	decodeCalls, tokenCalls := collectDecoderCalls(pass, body)
	var firstDecodes []*ast.CallExpr
	for decoder, calls := range decodeCalls {
		slices.SortFunc(calls, func(a, b *ast.CallExpr) int {
			return int(a.Pos() - b.Pos())
		})
		if tokenCalls[decoder] != nil && tokenCallsCheckEOF(pass, body, tokenCalls[decoder]) {
			continue
		}
		if len(calls) == 0 {
			continue
		}
		first := calls[0] //nolint:uncheckedsliceindex // empty call groups cannot be inserted into decodeCalls.
		if decodeIsEOFLoop(pass, first, parents) {
			continue
		}
		checked := false
		for _, followup := range calls {
			if followup == first {
				continue
			}
			if followsStraightLine(first, followup, parents) &&
				decodeChecksEOF(pass, body, followup, parents) {
				checked = true
				break
			}
		}
		if !checked {
			firstDecodes = append(firstDecodes, first)
		}
	}

	slices.SortFunc(firstDecodes, func(a, b *ast.CallExpr) int {
		return int(a.Pos() - b.Pos())
	})
	for _, firstDecode := range firstDecodes {
		pos := pass.Fset.PositionFor(firstDecode.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(pos, noLintIndex, linterName) {
			continue
		}
		pass.ReportRangef(firstDecode, "first Decode on an in-memory JSON reader must be followed by a Decode that rejects non-EOF results")
	}
}

func collectDecoderCalls(pass *analysis.Pass, body *ast.BlockStmt) (map[ast.Node][]*ast.CallExpr, map[ast.Node][]*ast.CallExpr) {
	decodeCalls := make(map[ast.Node][]*ast.CallExpr)
	tokenCalls := make(map[ast.Node][]*ast.CallExpr)
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
		if !ok || (selector.Sel.Name != "Decode" && selector.Sel.Name != "Token") ||
			!isJSONDecoder(pass, selector.X) {
			return true
		}
		construction := inMemoryDecoderConstruction(pass, body, selector.X, call.Pos())
		if construction == nil {
			return true
		}
		if selector.Sel.Name == "Decode" {
			decodeCalls[construction] = append(decodeCalls[construction], call)
		} else {
			tokenCalls[construction] = append(tokenCalls[construction], call)
		}
		return true
	})
	return decodeCalls, tokenCalls
}

func inMemoryDecoderConstruction(pass *analysis.Pass, body *ast.BlockStmt, receiver ast.Expr, before token.Pos) ast.Node {
	receiver = astutil.UnwrapParenExpr(receiver)
	if call, ok := receiver.(*ast.CallExpr); ok {
		if isInMemoryNewDecoder(pass, call) {
			return call
		}
		return nil
	}
	ident, ok := receiver.(*ast.Ident)
	if !ok {
		return nil
	}
	object := pass.TypesInfo.ObjectOf(ident)
	var latestExpr ast.Expr
	latestPos := token.NoPos
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		switch node := node.(type) {
		case *ast.AssignStmt:
			if node.Pos() >= before || node.Pos() < latestPos {
				return true
			}
			for i, lhs := range node.Lhs {
				if assignmentObject(pass, lhs) != object {
					continue
				}
				rhs, ok := astutil.RhsExprForIndex(node.Rhs, i)
				if ok {
					latestExpr, latestPos = rhs, node.Pos()
				}
			}
		case *ast.ValueSpec:
			if node.Pos() >= before || node.Pos() < latestPos {
				return true
			}
			for i, name := range node.Names {
				if pass.TypesInfo.ObjectOf(name) != object {
					continue
				}
				rhs, ok := astutil.RhsExprForIndex(node.Values, i)
				if ok {
					latestExpr, latestPos = rhs, node.Pos()
				}
			}
		}
		return true
	})
	if call, ok := astutil.UnwrapParenExpr(latestExpr).(*ast.CallExpr); ok && isInMemoryNewDecoder(pass, call) {
		return call
	}
	return nil
}

func assignmentObject(pass *analysis.Pass, expr ast.Expr) types.Object {
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	if !ok {
		return nil
	}
	return pass.TypesInfo.ObjectOf(ident)
}

func isInMemoryNewDecoder(pass *analysis.Pass, decoder *ast.CallExpr) bool {
	if !isPackageCall(pass, decoder, "encoding/json", "NewDecoder") || len(decoder.Args) != 1 {
		return false
	}
	reader, ok := astutil.UnwrapParenExpr(decoder.Args[0]).(*ast.CallExpr) //nolint:uncheckedsliceindex // len(decoder.Args) == 1 is checked above.
	if !ok || len(reader.Args) != 1 {
		return false
	}
	pkgPath, funcName, ok := astutil.PackageCall(pass, reader)
	return ok && funcName == "NewReader" && (pkgPath == "strings" || pkgPath == "bytes")
}

func isPackageCall(pass *analysis.Pass, call *ast.CallExpr, pkgPath, funcName string) bool {
	actualPkgPath, actualFuncName, ok := astutil.PackageCall(pass, call)
	return ok && actualPkgPath == pkgPath && actualFuncName == funcName
}

func isJSONDecoder(pass *analysis.Pass, expr ast.Expr) bool {
	t := pass.TypesInfo.TypeOf(expr)
	if t == nil {
		return false
	}
	if pointer, ok := t.(*types.Pointer); ok {
		t = pointer.Elem()
	}
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Name() == "Decoder" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "encoding/json"
}

func decodeChecksEOF(pass *analysis.Pass, body *ast.BlockStmt, call *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	var checked bool
	ast.Inspect(body, func(node ast.Node) bool {
		if checked {
			return false
		}
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		if ifStmt, ok := node.(*ast.IfStmt); ok {
			if guard, errObject := decodeErrorGuard(pass, ifStmt, call, parents); guard &&
				conditionRejectsNonEOF(pass, ifStmt.Cond, call, errObject) &&
				rejectsWithNonNilError(pass, ifStmt.Body) {
				checked = true
				return false
			}
		}
		return true
	})
	return checked
}

func decodeErrorGuard(pass *analysis.Pass, ifStmt *ast.IfStmt, call *ast.CallExpr, parents map[ast.Node]ast.Node) (bool, types.Object) {
	if object := decodeErrorObject(pass, ifStmt.Init, call); object != nil {
		return true, object
	}
	if containsNode(ifStmt.Cond, func(node ast.Node) bool { return node == call }) {
		return true, nil
	}
	assign, ok := parents[ifStmt].(*ast.BlockStmt)
	if !ok {
		return false, nil
	}
	index := statementIndex(assign, ifStmt)
	if index < 1 {
		return false, nil
	}
	prev, ok := assign.List[index-1].(*ast.AssignStmt) //nolint:uncheckedsliceindex // index > 0 is checked above.
	if !ok {
		return false, nil
	}
	object := decodeErrorObject(pass, prev, call)
	if object == nil {
		return false, nil
	}
	return conditionUsesObject(pass, ifStmt.Cond, object), object
}

func decodeErrorObject(pass *analysis.Pass, stmt ast.Stmt, call *ast.CallExpr) types.Object {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 {
		return nil
	}
	rhs, ok := astutil.RhsExprForIndex(assign.Rhs, 0)
	if !ok || astutil.UnwrapParenExpr(rhs) != call {
		return nil
	}

	resultIndex := 0
	if results, ok := pass.TypesInfo.TypeOf(call).(*types.Tuple); ok {
		if results.Len() != len(assign.Lhs) || results.Len() == 0 {
			return nil
		}
		resultIndex = results.Len() - 1
	} else if len(assign.Lhs) != 1 {
		return nil
	}
	ident, ok := astutil.UnwrapParenExpr(assign.Lhs[resultIndex]).(*ast.Ident) //nolint:uncheckedsliceindex // resultIndex is selected from a non-empty result list.
	if !ok || ident.Name == "_" {
		return nil
	}
	return pass.TypesInfo.ObjectOf(ident)
}

func conditionUsesObject(pass *analysis.Pass, condition ast.Expr, object types.Object) bool {
	found := false
	ast.Inspect(condition, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && pass.TypesInfo.ObjectOf(ident) == object {
			found = true
			return false
		}
		return !found
	})
	return found
}

func conditionRejectsNonEOF(pass *analysis.Pass, condition ast.Expr, call *ast.CallExpr, errObject types.Object) bool {
	condition = astutil.UnwrapParenExpr(condition)
	if unary, ok := condition.(*ast.UnaryExpr); ok && unary.Op == token.NOT {
		return isErrorsIsEOF(pass, unary.X, call, errObject)
	}
	if binary, ok := condition.(*ast.BinaryExpr); ok {
		if binary.Op == token.LOR {
			return isNilError(pass, binary.X, call, errObject) &&
				conditionRejectsNonEOF(pass, binary.Y, call, errObject) ||
				isNilError(pass, binary.Y, call, errObject) &&
					conditionRejectsNonEOF(pass, binary.X, call, errObject)
		}
		if binary.Op == token.NEQ || binary.Op == token.EQL {
			if (sameDecodeResult(pass, binary.X, call, errObject) && isIOEOF(pass, binary.Y)) ||
				(sameDecodeResult(pass, binary.Y, call, errObject) && isIOEOF(pass, binary.X)) {
				return binary.Op == token.NEQ
			}
		}
	}
	return false
}

func isErrorsIsEOF(pass *analysis.Pass, expr ast.Expr, call *ast.CallExpr, errObject types.Object) bool {
	expr = astutil.UnwrapParenExpr(expr)
	callExpr, ok := expr.(*ast.CallExpr)
	if !ok || !isPackageCall(pass, callExpr, "errors", "Is") || len(callExpr.Args) != 2 {
		return false
	}
	firstArg := callExpr.Args[0]  //nolint:uncheckedsliceindex // len(callExpr.Args) == 2 is checked above.
	secondArg := callExpr.Args[1] //nolint:uncheckedsliceindex // len(callExpr.Args) == 2 is checked above.
	return sameDecodeResult(pass, firstArg, call, errObject) && isIOEOF(pass, secondArg)
}

func sameDecodeResult(pass *analysis.Pass, expr ast.Expr, call *ast.CallExpr, errObject types.Object) bool {
	if astutil.UnwrapParenExpr(expr) == call {
		return true
	}
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	return ok && errObject != nil && pass.TypesInfo.ObjectOf(ident) == errObject
}

func isNilError(pass *analysis.Pass, expr ast.Expr, call *ast.CallExpr, errObject types.Object) bool {
	binary, ok := astutil.UnwrapParenExpr(expr).(*ast.BinaryExpr)
	if !ok || binary.Op != token.EQL {
		return false
	}
	left, right := astutil.UnwrapParenExpr(binary.X), astutil.UnwrapParenExpr(binary.Y)
	return (isNil(left) && sameDecodeResult(pass, right, call, errObject)) ||
		(isNil(right) && sameDecodeResult(pass, left, call, errObject))
}

func isNil(expr ast.Expr) bool {
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	return ok && ident.Name == "nil"
}

func isIOEOF(pass *analysis.Pass, expr ast.Expr) bool {
	selector, ok := astutil.UnwrapParenExpr(expr).(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "EOF" {
		return false
	}
	object, ok := pass.TypesInfo.Uses[selector.Sel].(*types.Var)
	return ok && object.Pkg() != nil && object.Pkg().Path() == "io"
}

func rejectsWithNonNilError(pass *analysis.Pass, block *ast.BlockStmt) bool {
	if block == nil || len(block.List) == 0 {
		return false
	}
	var lastStmt ast.Stmt
	for _, stmt := range block.List {
		lastStmt = stmt
	}
	switch last := lastStmt.(type) {
	case *ast.ReturnStmt:
		for _, result := range last.Results {
			call, ok := astutil.UnwrapParenExpr(result).(*ast.CallExpr)
			if ok && (isPackageCall(pass, call, "errors", "New") || isPackageCall(pass, call, "fmt", "Errorf")) {
				return true
			}
		}
	case *ast.ExprStmt:
		call, ok := astutil.UnwrapParenExpr(last.X).(*ast.CallExpr)
		return ok && isBuiltinCall(pass, call, "panic")
	}
	return false
}

func isBuiltinCall(pass *analysis.Pass, call *ast.CallExpr, name string) bool {
	ident, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.Ident)
	return ok && ident.Name == name && pass.TypesInfo.Uses[ident] == types.Universe.Lookup(name)
}

func followsStraightLine(first, followup *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	firstStmt := enclosingStatement(first, parents)
	followStmt := enclosingStatement(followup, parents)
	if firstStmt == nil || followStmt == nil {
		return false
	}
	firstBlock, ok := parents[firstStmt].(*ast.BlockStmt)
	if !ok || parents[followStmt] != firstBlock {
		return false
	}
	firstIndex, followIndex := statementIndex(firstBlock, firstStmt), statementIndex(firstBlock, followStmt)
	return firstIndex >= 0 && followIndex == firstIndex+1
}

func enclosingStatement(node ast.Node, parents map[ast.Node]ast.Node) ast.Stmt {
	for node != nil {
		if stmt, ok := node.(ast.Stmt); ok {
			if _, isBlockChild := parents[node].(*ast.BlockStmt); isBlockChild {
				return stmt
			}
		}
		node = parents[node]
	}
	return nil
}

func statementIndex(block *ast.BlockStmt, stmt ast.Stmt) int {
	for i, candidate := range block.List {
		if candidate == stmt {
			return i
		}
	}
	return -1
}

func parentNodes(root ast.Node) map[ast.Node]ast.Node {
	parents := make(map[ast.Node]ast.Node)
	var stack []ast.Node
	ast.Inspect(root, func(node ast.Node) bool {
		if node == nil {
			stack = stack[:len(stack)-1] //nolint:uncheckedsliceindex // ast.Inspect closes a node after it was pushed.
			return false
		}
		if len(stack) != 0 {
			parents[node] = stack[len(stack)-1] //nolint:uncheckedsliceindex // len(stack) != 0 is checked above.
		}
		stack = append(stack, node)
		return true
	})
	return parents
}

func containsNode(root ast.Node, match func(ast.Node) bool) bool {
	if root == nil {
		return false
	}
	found := false
	ast.Inspect(root, func(candidate ast.Node) bool {
		if match(candidate) {
			found = true
			return false
		}
		return !found
	})
	return found
}

func tokenCallsCheckEOF(pass *analysis.Pass, body *ast.BlockStmt, calls []*ast.CallExpr) bool {
	if len(calls) == 0 {
		return false
	}
	var checked bool
	ast.Inspect(body, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		if ifStmt, ok := node.(*ast.IfStmt); ok {
			for _, call := range calls {
				if ifChecksCallEOF(pass, ifStmt, call) {
					checked = true
					return false
				}
			}
		}
		return !checked
	})
	return checked
}

func ifChecksCallEOF(pass *analysis.Pass, ifStmt *ast.IfStmt, call *ast.CallExpr) bool {
	errObject := decodeErrorObject(pass, ifStmt.Init, call)
	if errObject != nil && (conditionChecksDecodeEOF(pass, ifStmt.Cond, call, errObject) ||
		blockChecksDecodeEOF(pass, ifStmt.Body, call, errObject)) {
		return true
	}
	return containsNode(ifStmt.Cond, func(node ast.Node) bool { return node == call }) &&
		conditionChecksDecodeEOF(pass, ifStmt.Cond, call, nil)
}

func decodeIsEOFLoop(pass *analysis.Pass, call *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	for node := parents[call]; node != nil; node = parents[node] {
		var loop ast.Node
		switch node.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			loop = node
		}
		if loop == nil {
			continue
		}
		checked := false
		ast.Inspect(loop, func(candidate ast.Node) bool {
			if _, ok := candidate.(*ast.FuncLit); ok {
				return false
			}
			if ifStmt, ok := candidate.(*ast.IfStmt); ok && ifChecksCallEOF(pass, ifStmt, call) {
				checked = true
				return false
			}
			return !checked
		})
		if checked {
			return true
		}
	}
	return false
}

func blockChecksDecodeEOF(pass *analysis.Pass, block *ast.BlockStmt, call *ast.CallExpr, errObject types.Object) bool {
	found := false
	ast.Inspect(block, func(node ast.Node) bool {
		if _, ok := node.(*ast.FuncLit); ok {
			return false
		}
		if ifStmt, ok := node.(*ast.IfStmt); ok && conditionChecksDecodeEOF(pass, ifStmt.Cond, call, errObject) {
			found = true
			return false
		}
		return !found
	})
	return found
}

func conditionChecksDecodeEOF(pass *analysis.Pass, condition ast.Expr, call *ast.CallExpr, errObject types.Object) bool {
	found := false
	ast.Inspect(condition, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.BinaryExpr:
			if (node.Op == token.EQL || node.Op == token.NEQ) &&
				((sameDecodeResult(pass, node.X, call, errObject) && isIOEOF(pass, node.Y)) ||
					(sameDecodeResult(pass, node.Y, call, errObject) && isIOEOF(pass, node.X))) {
				found = true
			}
		case *ast.CallExpr:
			if isPackageCall(pass, node, "errors", "Is") && len(node.Args) == 2 {
				firstArg := node.Args[0]  //nolint:uncheckedsliceindex // len(node.Args) == 2 is checked above.
				secondArg := node.Args[1] //nolint:uncheckedsliceindex // len(node.Args) == 2 is checked above.
				if sameDecodeResult(pass, firstArg, call, errObject) && isIOEOF(pass, secondArg) {
					found = true
				}
			}
		}
		return !found
	})
	return found
}
