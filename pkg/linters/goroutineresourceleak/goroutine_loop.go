package goroutineresourceleak

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/astutil"
)

func buildParentMap(files []*ast.File) map[ast.Node]ast.Node {
	type frame struct {
		node   ast.Node
		parent *frame
	}
	parents := make(map[ast.Node]ast.Node)
	for _, file := range files {
		var stack *frame
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				if stack != nil {
					stack = stack.parent
				}
				return true
			}
			if stack != nil {
				parents[node] = stack.node
			}
			stack = &frame{node: node, parent: stack}
			return true
		})
	}
	return parents
}

func checkLoopWaitGroupLeak(pass *analysis.Pass, goStmt *ast.GoStmt, parents map[ast.Node]ast.Node) bool {
	loop := enclosingLoop(goStmt, parents)
	if loop == nil {
		return false
	}
	goBlock, goIndex := containingBlock(goStmt, parents)
	if loopBodyStopsAfterGo(goBlock, goIndex) {
		return false
	}
	block, index := containingBlock(loop, parents)
	if block == nil || !hasWaitGroupSynchronization(pass, loop, goStmt, block, index) {
		return true
	}
	return false
}

func enclosingLoop(node ast.Node, parents map[ast.Node]ast.Node) ast.Node {
	for parent := parents[node]; parent != nil; parent = parents[parent] {
		switch parent.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return nil
		case *ast.ForStmt, *ast.RangeStmt:
			return parent
		}
	}
	return nil
}

func containingBlock(node ast.Node, parents map[ast.Node]ast.Node) (*ast.BlockStmt, int) {
	for parent := parents[node]; parent != nil; parent = parents[parent] {
		if block, ok := parent.(*ast.BlockStmt); ok {
			for i, stmt := range block.List {
				if stmt == node {
					return block, i
				}
			}
		}
		switch parent.(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return nil, 0
		}
	}
	return nil, 0
}

func loopBodyStopsAfterGo(block *ast.BlockStmt, index int) bool {
	if block == nil {
		return false
	}
	for i, stmt := range block.List {
		if i <= index {
			continue
		}
		switch stmt := stmt.(type) {
		case *ast.ReturnStmt:
			return true
		case *ast.BranchStmt:
			if stmt.Tok == token.BREAK && stmt.Label == nil {
				return true
			}
		}
	}
	return false
}

func hasWaitGroupSynchronization(
	pass *analysis.Pass,
	loop ast.Node,
	goStmt *ast.GoStmt,
	block *ast.BlockStmt,
	index int,
) bool {
	adds := make(map[receiverKey]bool)
	for _, stmt := range block.List[:index] {
		for _, key := range waitGroupMethodKeys(pass, stmt, "Add", token.NoPos) {
			adds[key] = true
		}
	}
	for _, key := range waitGroupMethodKeys(pass, loop, "Add", goStmt.Pos()) {
		adds[key] = true
	}
	for key := range adds {
		for _, stmt := range block.List[index+1:] {
			if hasWaitGroupMethod(pass, stmt, "Wait", key) {
				return true
			}
		}
	}
	return false
}

func hasWaitGroupMethod(pass *analysis.Pass, node ast.Node, method string, key receiverKey) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != method {
			return true
		}
		callKey, ok := waitGroupReceiverKey(pass, selector.X)
		if ok && callKey == key {
			found = true
			return false
		}
		return true
	})
	return found
}

func waitGroupMethodKeys(pass *analysis.Pass, node ast.Node, method string, before token.Pos) []receiverKey {
	keys := make(map[receiverKey]bool)
	ast.Inspect(node, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != method || (before.IsValid() && call.Pos() >= before) {
			return true
		}
		if receiver, ok := waitGroupReceiverKey(pass, selector.X); ok {
			keys[receiver] = true
		}
		return true
	})
	result := make([]receiverKey, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	return result
}

func waitGroupReceiverKey(pass *analysis.Pass, receiver ast.Expr) (receiverKey, bool) {
	t := pass.TypesInfo.TypeOf(receiver)
	if t == nil {
		return receiverKey{}, false
	}
	if pointer, ok := t.(*types.Pointer); ok {
		t = pointer.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "sync" || named.Obj().Name() != "WaitGroup" {
		return receiverKey{}, false
	}
	return getReceiverKey(pass, receiver)
}
