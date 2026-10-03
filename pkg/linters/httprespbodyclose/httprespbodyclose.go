// Package httprespbodyclose implements a Go analysis linter that flags
// HTTP response Body.Close() calls that are made directly instead of deferred.
package httprespbodyclose

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:httprespbodyclose")

// Analyzer is the http-resp-body-close analysis pass.
var Analyzer = analyzerutil.New("httprespbodyclose", "reports HTTP response Body.Close() calls that are not deferred, which risks resource leaks on early return", run)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{
		(*ast.FuncDecl)(nil),
		(*ast.FuncLit)(nil),
	}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		inspectFunc(pass, n, noLintIndex, generatedFiles)
	})
}

func inspectFunc(pass *analysis.Pass, n ast.Node, noLintIndex nolint.DirectiveIndex, generatedFiles filecheck.GeneratedIndex) {
	var body *ast.BlockStmt
	switch fn := n.(type) {
	case *ast.FuncDecl:
		if fn.Body == nil {
			return
		}
		body = fn.Body
	case *ast.FuncLit:
		if fn.Body == nil {
			return
		}
		body = fn.Body
	}

	pos := pass.Fset.PositionFor(n.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}

	respVars := make(map[types.Object]map[token.Pos]*respVarState)
	reported := make(map[token.Pos]bool)

	walkResponses(pass, respVars, reported, body, noLintIndex)

	for _, variants := range respVars {
		for _, state := range variants {
			if state.hasManualClose && !state.hasDeferClose && !reported[state.assignPos] {
				if !nolint.HasDirectiveForLinter(pass.Fset.PositionFor(state.assignPos, false), noLintIndex, "httprespbodyclose") {
					reportMissingDefer(pass, state)
				}
			}
		}
	}
}

func cloneResponses(states map[types.Object]map[token.Pos]*respVarState) map[types.Object]map[token.Pos]*respVarState {
	copyOf := make(map[types.Object]map[token.Pos]*respVarState, len(states))
	for key, variants := range states {
		copyOf[key] = make(map[token.Pos]*respVarState, len(variants))
		for pos, st := range variants {
			value := *st
			copyOf[key][pos] = &value
		}
	}
	return copyOf
}

func walkResponses(pass *analysis.Pass, states map[types.Object]map[token.Pos]*respVarState, reported map[token.Pos]bool, node ast.Node, noLintIndex nolint.DirectiveIndex) {
	ast.Inspect(node, func(n ast.Node) bool {
		if branch, ok := n.(*ast.IfStmt); ok {
			if branch.Init != nil {
				walkResponses(pass, states, reported, branch.Init, noLintIndex)
			}
			walkResponses(pass, states, reported, branch.Cond, noLintIndex)
			thenStates := cloneResponses(states)
			elseStates := cloneResponses(states)
			walkResponses(pass, thenStates, reported, branch.Body, noLintIndex)
			if branch.Else != nil {
				walkResponses(pass, elseStates, reported, branch.Else, noLintIndex)
			}
			for key, variants := range thenStates {
				for pos, then := range variants {
					if other, ok := elseStates[key][pos]; ok {
						then.hasManualClose = then.hasManualClose || other.hasManualClose
						then.hasDeferClose = then.hasDeferClose && other.hasDeferClose
						if !then.manualClosePos.IsValid() {
							then.manualClosePos = other.manualClosePos
						}
					}
				}
				states[key] = variants
			}
			for key, variants := range elseStates {
				if states[key] == nil {
					states[key] = make(map[token.Pos]*respVarState)
				}
				for pos, st := range variants {
					if _, ok := states[key][pos]; !ok {
						states[key][pos] = st
					}
				}
			}
			return false
		}
		return inspectNode(pass, states, reported, n, noLintIndex)
	})
}

func inspectNode(pass *analysis.Pass, respVars map[types.Object]map[token.Pos]*respVarState, reported map[token.Pos]bool, node ast.Node, noLintIndex nolint.DirectiveIndex) bool {
	if node == nil {
		return false
	}
	if _, ok := node.(*ast.FuncLit); ok {
		return false
	}

	if assign, ok := node.(*ast.AssignStmt); ok {
		trackHTTPAssignment(pass, respVars, reported, assign, noLintIndex)
		// Also check RHS for manual Body.Close() in assignments like: err := resp.Body.Close()
		for _, rhs := range assign.Rhs {
			if call, ok := rhs.(*ast.CallExpr); ok {
				markBodyClose(pass, respVars, call)
			}
		}
	}

	if deferStmt, ok := node.(*ast.DeferStmt); ok {
		if obj := bodyCloseReceiver(pass, deferStmt.Call); obj != nil {
			for _, state := range respVars[obj] {
				state.hasDeferClose = true
			}
		}
	}

	if exprStmt, ok := node.(*ast.ExprStmt); ok {
		if call, ok := exprStmt.X.(*ast.CallExpr); ok {
			markBodyClose(pass, respVars, call)
		}
	}

	return true
}

func markBodyClose(pass *analysis.Pass, respVars map[types.Object]map[token.Pos]*respVarState, call *ast.CallExpr) {
	obj := bodyCloseReceiver(pass, call)
	if obj == nil {
		return
	}
	for _, state := range respVars[obj] {
		state.hasManualClose = true
		if !state.manualClosePos.IsValid() {
			state.manualClosePos = call.Pos()
		}
	}
}

func trackHTTPAssignment(pass *analysis.Pass, respVars map[types.Object]map[token.Pos]*respVarState, reported map[token.Pos]bool, assign *ast.AssignStmt, noLintIndex nolint.DirectiveIndex) {
	for i, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if !ok || ident.Name == "_" {
			continue
		}
		obj := pass.TypesInfo.ObjectOf(ident)
		if obj == nil || !isHTTPResponsePtr(obj.Type()) {
			continue
		}

		var callPos token.Pos
		for rhsIndex, rhs := range assign.Rhs {
			// A single RHS may return multiple values; otherwise match LHS by index.
			if len(assign.Rhs) == 1 || rhsIndex == i {
				if call, ok := rhs.(*ast.CallExpr); ok {
					callPos = call.Pos()
				}
				break
			}
		}
		if !callPos.IsValid() {
			continue
		}

		// Report any prior unresolved violation before overwriting state.
		for _, prev := range respVars[obj] {
			if prev.hasManualClose && !prev.hasDeferClose && !reported[prev.assignPos] {
				if !nolint.HasDirectiveForLinter(pass.Fset.PositionFor(prev.assignPos, false), noLintIndex, "httprespbodyclose") {
					reportMissingDefer(pass, prev)
				}
				reported[prev.assignPos] = true
			}
		}
		respVars[obj] = map[token.Pos]*respVarState{callPos: {assignPos: callPos}}
	}
}

type respVarState struct {
	assignPos      token.Pos
	manualClosePos token.Pos
	hasDeferClose  bool
	hasManualClose bool
}

func reportMissingDefer(pass *analysis.Pass, state *respVarState) {
	pkgLog.Printf("flagging non-deferred Body.Close() at %s", pass.Fset.PositionFor(state.assignPos, false))

	diag := analysis.Diagnostic{
		Pos:     state.assignPos,
		Message: "HTTP response Body.Close() should be deferred immediately after receiving the response to prevent resource leaks",
	}
	if state.manualClosePos.IsValid() {
		diag.Related = []analysis.RelatedInformation{
			{
				Pos:     state.manualClosePos,
				Message: "manual Body.Close() call",
			},
		}
	}
	pass.Report(diag)
}

// isHTTPResponsePtr reports whether t is *net/http.Response.
func isHTTPResponsePtr(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := ptr.Elem().(*types.Named)
	if !ok {
		return false
	}
	obj := named.Obj()
	return obj.Pkg() != nil && obj.Pkg().Path() == "net/http" && obj.Name() == "Response"
}

// bodyCloseReceiver returns the types.Object for the *http.Response receiver
// of a resp.Body.Close() call, or nil if the call is not of that form.
func bodyCloseReceiver(pass *analysis.Pass, call *ast.CallExpr) types.Object {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Close" {
		return nil
	}
	bodySel, ok := sel.X.(*ast.SelectorExpr)
	if !ok || bodySel.Sel.Name != "Body" {
		return nil
	}
	ident, ok := bodySel.X.(*ast.Ident)
	if !ok {
		return nil
	}
	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil || !isHTTPResponsePtr(obj.Type()) {
		return nil
	}
	return obj
}
