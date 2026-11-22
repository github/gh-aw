// Package osstatignoredeerrorderef implements a Go analysis linter that flags
// dereferences of os.Stat and os.Lstat results when their errors are discarded.
package osstatignoredeerrorderef

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the os-stat-ignored-error-deref analysis pass.
var Analyzer = analyzerutil.New("osstatignoredeerrorderef", "reports dereferences of os.Stat and os.Lstat results whose errors are discarded without a nil check", run)

type flowState map[types.Object]string

type flowAnalyzer struct {
	pass           *analysis.Pass
	nolintIndex    nolint.DirectiveIndex
	generatedFiles filecheck.GeneratedIndex
}

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	analyzer := flowAnalyzer{
		pass:           pass,
		nolintIndex:    nolintIndex,
		generatedFiles: generatedFiles,
	}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			function, ok := node.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				return true
			}
			analyzer.block(function.Body, make(flowState))
			return false
		})
	}
	return nil, nil
}

func (a *flowAnalyzer) block(block *ast.BlockStmt, state flowState) (flowState, bool) {
	if block == nil {
		return state, false
	}
	for _, statement := range block.List {
		var terminated bool
		state, terminated = a.statement(statement, state)
		if terminated {
			return state, true
		}
	}
	return state, false
}

func (a *flowAnalyzer) statement(statement ast.Stmt, state flowState) (flowState, bool) {
	switch statement := statement.(type) {
	case *ast.BlockStmt:
		return a.block(statement, state)
	case *ast.AssignStmt:
		a.expressionList(statement.Rhs, state)
		a.assignment(statement, state)
	case *ast.DeclStmt:
		declaration, ok := statement.Decl.(*ast.GenDecl)
		if !ok {
			return state, false
		}
		for _, spec := range declaration.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			a.valueSpec(valueSpec, state)
		}
	case *ast.ExprStmt:
		a.expression(statement.X, state)
		if isBuiltinPanic(a.pass, statement.X) {
			return state, true
		}
	case *ast.ReturnStmt:
		a.expressionList(statement.Results, state)
		return state, true
	case *ast.IfStmt:
		return a.ifStatement(statement, state)
	case *ast.ForStmt:
		return a.forStatement(statement, state)
	case *ast.RangeStmt:
		a.expression(statement.X, state)
		bodyState, _ := a.block(statement.Body, cloneState(state))
		return joinStates(state, bodyState), false
	case *ast.IncDecStmt:
		a.clearObject(statement.X, state)
	case *ast.GoStmt:
		a.expression(statement.Call, state)
	case *ast.DeferStmt:
		a.expression(statement.Call, state)
	case *ast.SendStmt:
		a.expression(statement.Chan, state)
		a.expression(statement.Value, state)
	case *ast.SwitchStmt:
		return a.switchStatement(statement, state)
	case *ast.LabeledStmt:
		return a.statement(statement.Stmt, state)
	}
	return state, false
}

func (a *flowAnalyzer) ifStatement(statement *ast.IfStmt, state flowState) (flowState, bool) {
	if statement.Init != nil {
		var terminated bool
		state, terminated = a.statement(statement.Init, state)
		if terminated {
			return state, true
		}
	}
	trueState, falseState := a.condition(statement.Cond, state)
	bodyState, bodyTerminated := a.block(statement.Body, trueState)
	elseState, elseTerminated := falseState, false
	if statement.Else != nil {
		elseState, elseTerminated = a.statement(statement.Else, falseState)
	}
	if bodyTerminated && elseTerminated {
		return joinStates(bodyState, elseState), true
	}
	if bodyTerminated {
		return elseState, false
	}
	if elseTerminated {
		return bodyState, false
	}
	return joinStates(bodyState, elseState), false
}

func (a *flowAnalyzer) forStatement(statement *ast.ForStmt, state flowState) (flowState, bool) {
	if statement.Init != nil {
		var terminated bool
		state, terminated = a.statement(statement.Init, state)
		if terminated {
			return state, true
		}
	}
	var bodyState, afterCondition flowState
	if statement.Cond != nil {
		bodyState, afterCondition = a.condition(statement.Cond, state)
	} else {
		bodyState, afterCondition = cloneState(state), cloneState(state)
	}
	bodyState, bodyTerminated := a.block(statement.Body, bodyState)
	if statement.Post != nil && !bodyTerminated {
		bodyState, _ = a.statement(statement.Post, bodyState)
	}
	return joinStates(afterCondition, bodyState), false
}

func (a *flowAnalyzer) switchStatement(statement *ast.SwitchStmt, state flowState) (flowState, bool) {
	if statement.Init != nil {
		var terminated bool
		state, terminated = a.statement(statement.Init, state)
		if terminated {
			return state, true
		}
	}
	if statement.Tag != nil {
		a.expression(statement.Tag, state)
	}
	result := make(flowState)
	hasDefault := false
	for _, clause := range statement.Body.List {
		caseClause, ok := clause.(*ast.CaseClause)
		if !ok {
			continue
		}
		if len(caseClause.List) == 0 {
			hasDefault = true
		}
		a.expressionList(caseClause.List, state)
		clauseState, terminated := a.statements(caseClause.Body, cloneState(state))
		if !terminated {
			result = joinStates(result, clauseState)
		}
	}
	if !hasDefault {
		result = joinStates(result, state)
	}
	return result, false
}

func (a *flowAnalyzer) statements(statements []ast.Stmt, state flowState) (flowState, bool) {
	for _, statement := range statements {
		var terminated bool
		state, terminated = a.statement(statement, state)
		if terminated {
			return state, true
		}
	}
	return state, false
}

func (a *flowAnalyzer) assignment(statement *ast.AssignStmt, state flowState) {
	statName := ""
	if _, pkgPath, funcName, ok := astutil.MatchDiscardedErrorCall(a.pass, statement); ok &&
		pkgPath == "os" && (funcName == "Stat" || funcName == "Lstat") {
		statName = funcName
	}
	for i, lhs := range statement.Lhs {
		object := objectOf(a.pass, lhs)
		if object == nil {
			continue
		}
		if i == 0 && statName != "" {
			state[object] = statName
		} else {
			delete(state, object)
		}
	}
}

func (a *flowAnalyzer) valueSpec(spec *ast.ValueSpec, state flowState) {
	a.expressionList(spec.Values, state)
	statName := discardedStatFromValueSpec(a.pass, spec)
	for i, name := range spec.Names {
		object := a.pass.TypesInfo.Defs[name]
		if object == nil {
			continue
		}
		if i == 0 && statName != "" {
			state[object] = statName
		} else {
			delete(state, object)
		}
	}
}

func discardedStatFromValueSpec(pass *analysis.Pass, spec *ast.ValueSpec) string {
	if len(spec.Names) != 2 || len(spec.Values) != 1 {
		return ""
	}
	for i, name := range spec.Names {
		if i == 1 && name.Name == "_" {
			for j, value := range spec.Values {
				if j == 0 {
					return discardedStatName(pass, value)
				}
			}
		}
	}
	return ""
}

func discardedStatName(pass *analysis.Pass, expr ast.Expr) string {
	call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	if !ok {
		return ""
	}
	pkgPath, funcName, ok := astutil.PackageCall(pass, call)
	if !ok || pkgPath != "os" || (funcName != "Stat" && funcName != "Lstat") {
		return ""
	}
	return funcName
}

func (a *flowAnalyzer) expressionList(expressions []ast.Expr, state flowState) {
	for _, expr := range expressions {
		a.expression(expr, state)
	}
}

func (a *flowAnalyzer) expression(expr ast.Expr, state flowState) {
	if expr == nil {
		return
	}
	if unary, ok := astutil.UnwrapParenExpr(expr).(*ast.UnaryExpr); ok && unary.Op == token.NOT {
		a.condition(unary, state)
		return
	}
	if binary, ok := astutil.UnwrapParenExpr(expr).(*ast.BinaryExpr); ok &&
		(binary.Op == token.LAND || binary.Op == token.LOR) {
		a.condition(binary, state)
		return
	}
	ast.Inspect(expr, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		switch node := node.(type) {
		case *ast.FuncLit:
			a.block(node.Body, cloneState(state))
			return false
		case *ast.SelectorExpr:
			a.reportIfTainted(node, node.X, state)
		case *ast.UnaryExpr:
			if node.Op == token.MUL {
				a.reportIfTainted(node, node.X, state)
			}
		}
		return true
	})
}

func (a *flowAnalyzer) reportIfTainted(node ast.Node, expr ast.Expr, state flowState) {
	object := objectOf(a.pass, expr)
	funcName, ok := state[object]
	if object == nil || !ok {
		return
	}
	position := a.pass.Fset.PositionFor(node.Pos(), false)
	if filecheck.ShouldSkipFilename(position.Filename, a.generatedFiles) ||
		nolint.HasDirectiveForLinter(position, a.nolintIndex, "osstatignoredeerrorderef") {
		return
	}
	a.pass.ReportRangef(node, "result of os.%s is dereferenced after its error is discarded without a nil check", funcName)
}

func (a *flowAnalyzer) condition(expr ast.Expr, state flowState) (flowState, flowState) {
	expr = astutil.UnwrapParenExpr(expr)
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.NOT {
		falseState, trueState := a.condition(unary.X, state)
		return trueState, falseState
	}
	if binary, ok := expr.(*ast.BinaryExpr); ok {
		switch binary.Op {
		case token.LAND:
			leftTrue, leftFalse := a.condition(binary.X, state)
			rightTrue, rightFalse := a.condition(binary.Y, leftTrue)
			return rightTrue, joinStates(leftFalse, rightFalse)
		case token.LOR:
			leftTrue, leftFalse := a.condition(binary.X, state)
			rightTrue, rightFalse := a.condition(binary.Y, leftFalse)
			return joinStates(leftTrue, rightTrue), rightFalse
		}
		if object, op, ok := nilComparison(a.pass, binary); ok {
			if _, tainted := state[object]; tainted {
				trueState, falseState := cloneState(state), cloneState(state)
				if op == token.NEQ {
					delete(trueState, object)
				} else {
					delete(falseState, object)
				}
				return trueState, falseState
			}
		}
	}
	a.expression(expr, state)
	return cloneState(state), cloneState(state)
}

func nilComparison(pass *analysis.Pass, expr *ast.BinaryExpr) (types.Object, token.Token, bool) {
	if expr.Op != token.EQL && expr.Op != token.NEQ {
		return nil, token.ILLEGAL, false
	}
	left, leftIsIdent := astutil.UnwrapParenExpr(expr.X).(*ast.Ident)
	right, rightIsIdent := astutil.UnwrapParenExpr(expr.Y).(*ast.Ident)
	if leftIsIdent && rightIsIdent && right.Name == "nil" {
		return objectOf(pass, left), expr.Op, true
	}
	if leftIsIdent && rightIsIdent && left.Name == "nil" {
		return objectOf(pass, right), expr.Op, true
	}
	return nil, token.ILLEGAL, false
}

func objectOf(pass *analysis.Pass, expr ast.Expr) types.Object {
	ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident)
	if !ok {
		return nil
	}
	return pass.TypesInfo.ObjectOf(ident)
}

func isBuiltinPanic(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	builtin, ok := pass.TypesInfo.ObjectOf(ident).(*types.Builtin)
	return ok && builtin.Name() == "panic"
}

func (a *flowAnalyzer) clearObject(expr ast.Expr, state flowState) {
	if object := objectOf(a.pass, expr); object != nil {
		delete(state, object)
	}
}

func cloneState(state flowState) flowState {
	clone := make(flowState, len(state))
	maps.Copy(clone, state)
	return clone
}

func joinStates(left, right flowState) flowState {
	joined := cloneState(left)
	maps.Copy(joined, right)
	return joined
}
