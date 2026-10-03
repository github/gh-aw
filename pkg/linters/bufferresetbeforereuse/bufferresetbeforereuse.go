// Package bufferresetbeforereuse implements a Go analysis linter that flags
// reuse of bytes.Buffer or strings.Builder without calling Reset() between writes,
// which can accumulate previous data.
package bufferresetbeforereuse

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:bufferresetbeforereuse")

// Analyzer is the buffer-reset-before-reuse analysis pass.
var Analyzer = analyzerutil.New("bufferresetbeforereuse", "reports reuse of bytes.Buffer or strings.Builder without calling Reset() between writes, which can accumulate previous data", run)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	// Process function declarations and function literals
	nodeFilter := []ast.Node{(*ast.FuncDecl)(nil), (*ast.FuncLit)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		checkBufferReuse(pass, n, generatedFiles, noLintIndex)
	})
}

// checkBufferReuse analyzes a function body for buffer/builder reuse without Reset.
func checkBufferReuse(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	var body *ast.BlockStmt
	switch fn := n.(type) {
	case *ast.FuncDecl:
		body = fn.Body
	case *ast.FuncLit:
		body = fn.Body
	default:
		return
	}

	if body == nil {
		return
	}

	pos := pass.Fset.PositionFor(body.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}

	analyzeBlockForBufferReuse(pass, body, generatedFiles, noLintIndex)
}

// event represents a write, read, or reset operation on a buffer/builder
type event struct {
	varName string
	obj     types.Object
	typ     string // "write", "read", or "reset"
	pos     token.Pos
	node    ast.Node
}

func analyzeBlockForBufferReuse(pass *analysis.Pass, block *ast.BlockStmt, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	analyzeStraightLineBlock(pass, block, make(map[types.Object]bool), make(map[types.Object]bool), generatedFiles, noLintIndex)
}

func applyEvents(pass *analysis.Pass, events []*event, written, read map[types.Object]bool, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	for _, e := range events {
		switch e.typ {
		case "write":
			if read[e.obj] {
				// This is a write after the buffer has been read, without Reset
				pkgLog.Printf("flagging %s reuse at line %d", e.varName, pass.Fset.Position(e.pos).Line)

				// Check nolint directive
				filePos := pass.Fset.PositionFor(e.pos, false)
				if filecheck.ShouldSkipFilename(filePos.Filename, generatedFiles) {
					continue
				}
				if nolint.HasDirectiveForLinter(filePos, noLintIndex, "bufferresetbeforereuse") {
					pkgLog.Printf("suppressed diagnostic for %s at line %d", e.varName, filePos.Line)
					continue
				}

				pass.ReportRangef(
					e.node,
					"%s is reused without calling Reset() between writes, which can accumulate previous data",
					e.varName,
				)
			}
			written[e.obj] = true

		case "read":
			if written[e.obj] {
				read[e.obj] = true
			}

		case "reset":
			read[e.obj] = false
			written[e.obj] = false
		}
	}
}

func cloneBufferState(state map[types.Object]bool) map[types.Object]bool {
	return maps.Clone(state)
}

type bufferState struct {
	written map[types.Object]bool
	read    map[types.Object]bool
}

func mergeBufferStates(written, read map[types.Object]bool, paths []bufferState) {
	for key := range written {
		delete(written, key)
		delete(read, key)
	}
	for _, path := range paths {
		for key, value := range path.written {
			written[key] = written[key] || value
		}
		for key, value := range path.read {
			read[key] = read[key] || value
		}
	}
}

func endsWithReturn(body []ast.Stmt) bool {
	var last ast.Stmt
	for _, stmt := range body {
		last = stmt
	}
	_, ok := last.(*ast.ReturnStmt)
	return ok
}

func analyzeChoice(pass *analysis.Pass, clauses []ast.Stmt, maySkip bool, written, read map[types.Object]bool, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	var paths []bufferState
	hasDefault := false
	for i, stmt := range clauses {
		switch clause := stmt.(type) {
		case *ast.CaseClause:
			hasDefault = hasDefault || clause.List == nil
		case *ast.CommClause:
			hasDefault = hasDefault || clause.Comm == nil
		}
		path := bufferState{cloneBufferState(written), cloneBufferState(read)}
		var body []ast.Stmt
		for _, next := range clauses[i:] {
			switch clause := next.(type) {
			case *ast.CaseClause:
				body = clause.Body
			case *ast.CommClause:
				body = clause.Body
			}
			analyzeStraightLineBlock(pass, &ast.BlockStmt{List: body}, path.written, path.read, generatedFiles, noLintIndex)
			if len(body) == 0 {
				break
			}
			var last ast.Stmt
			for _, stmt := range body {
				last = stmt
			}
			branch, ok := last.(*ast.BranchStmt)
			if !ok || branch.Tok != token.FALLTHROUGH {
				break
			}
		}
		if !endsWithReturn(body) {
			paths = append(paths, path)
		}
	}
	if maySkip && !hasDefault {
		paths = append(paths, bufferState{cloneBufferState(written), cloneBufferState(read)})
	}
	if len(paths) > 0 {
		mergeBufferStates(written, read, paths)
	}
}

func analyzeStraightLineBlock(pass *analysis.Pass, block *ast.BlockStmt, written, read map[types.Object]bool, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	if block == nil {
		return
	}
	for _, stmt := range block.List {
		if branch, ok := stmt.(*ast.IfStmt); ok {
			if branch.Init != nil {
				var events []*event
				collectEvents(pass, branch.Init, &events)
				applyEvents(pass, events, written, read, generatedFiles, noLintIndex)
			}
			var events []*event
			collectEvents(pass, branch.Cond, &events)
			applyEvents(pass, events, written, read, generatedFiles, noLintIndex)
			thenWritten, thenRead := cloneBufferState(written), cloneBufferState(read)
			elseWritten, elseRead := cloneBufferState(written), cloneBufferState(read)
			analyzeStraightLineBlock(pass, branch.Body, thenWritten, thenRead, generatedFiles, noLintIndex)
			switch other := branch.Else.(type) {
			case *ast.BlockStmt:
				analyzeStraightLineBlock(pass, other, elseWritten, elseRead, generatedFiles, noLintIndex)
			case *ast.IfStmt:
				analyzeStraightLineBlock(pass, &ast.BlockStmt{List: []ast.Stmt{other}}, elseWritten, elseRead, generatedFiles, noLintIndex)
			}
			paths := []bufferState{}
			if !endsWithReturn(branch.Body.List) {
				paths = append(paths, bufferState{thenWritten, thenRead})
			}
			if elseBlock, ok := branch.Else.(*ast.BlockStmt); !ok || !endsWithReturn(elseBlock.List) {
				paths = append(paths, bufferState{elseWritten, elseRead})
			}
			if len(paths) > 0 {
				mergeBufferStates(written, read, paths)
			}
			continue
		}
		if analyzeChoiceStatement(pass, stmt, written, read, generatedFiles, noLintIndex) {
			continue
		}
		if _, isBlock := stmt.(*ast.BlockStmt); !isBlock {
			var events []*event
			collectEvents(pass, stmt, &events)
			applyEvents(pass, events, written, read, generatedFiles, noLintIndex)
		}
		ast.Inspect(stmt, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			if nested, ok := n.(*ast.BlockStmt); ok {
				analyzeStraightLineBlock(pass, nested, written, read, generatedFiles, noLintIndex)
				return false
			}
			return true
		})
	}
}

func analyzeChoiceStatement(pass *analysis.Pass, stmt ast.Stmt, written, read map[types.Object]bool, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) bool {
	switch choice := stmt.(type) {
	case *ast.SwitchStmt:
		var events []*event
		collectEvents(pass, choice.Init, &events)
		collectEvents(pass, choice.Tag, &events)
		applyEvents(pass, events, written, read, generatedFiles, noLintIndex)
		analyzeChoice(pass, choice.Body.List, true, written, read, generatedFiles, noLintIndex)
		return true
	case *ast.TypeSwitchStmt:
		var events []*event
		collectEvents(pass, choice.Init, &events)
		collectEvents(pass, choice.Assign, &events)
		applyEvents(pass, events, written, read, generatedFiles, noLintIndex)
		analyzeChoice(pass, choice.Body.List, true, written, read, generatedFiles, noLintIndex)
		return true
	case *ast.SelectStmt:
		analyzeChoice(pass, choice.Body.List, false, written, read, generatedFiles, noLintIndex)
		return true
	}
	return false
}

// collectEvents collects all write, read, and reset operations on buffers in order
func collectEvents(pass *analysis.Pass, root ast.Node, events *[]*event) {
	if root == nil {
		return
	}
	ast.Inspect(root, func(n ast.Node) bool {
		if n != root {
			switch n.(type) {
			case *ast.BlockStmt, *ast.FuncLit, *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
				return false
			}
		}

		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		appendCallEvent(pass, call, events)

		return true
	})
}

func appendCallEvent(pass *analysis.Pass, call *ast.CallExpr, events *[]*event) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	ident, ok := sel.X.(*ast.Ident)
	if !ok {
		return
	}

	obj := bufferOrBuilderObject(pass, ident)
	if obj == nil {
		return
	}

	appendMethodEvent(pass, call, ident.Name, obj, sel.Sel.Name, events)
}

func appendMethodEvent(pass *analysis.Pass, call *ast.CallExpr, varName string, obj types.Object, methodName string, events *[]*event) {
	switch {
	case methodName == "Reset":
		*events = append(*events, newEvent(varName, obj, "reset", call))
		pkgLog.Printf("found reset on %s at line %d", varName, pass.Fset.Position(call.Pos()).Line)
	case isWriteMethod(methodName):
		*events = append(*events, newEvent(varName, obj, "write", call))
		pkgLog.Printf("found write on %s at line %d", varName, pass.Fset.Position(call.Pos()).Line)
	case isReadMethod(methodName):
		*events = append(*events, newEvent(varName, obj, "read", call))
		pkgLog.Printf("found read on %s at line %d", varName, pass.Fset.Position(call.Pos()).Line)
	}
}

func newEvent(varName string, obj types.Object, typ string, call *ast.CallExpr) *event {
	return &event{
		varName: varName,
		obj:     obj,
		typ:     typ,
		pos:     call.Pos(),
		node:    call,
	}
}

// bufferOrBuilderObject returns the object for identifiers that refer to a bytes.Buffer or strings.Builder variable.
func bufferOrBuilderObject(pass *analysis.Pass, ident *ast.Ident) types.Object {
	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return nil
	}

	t := obj.Type()
	if t == nil {
		return nil
	}

	// Handle pointer types
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}

	// Check if it's bytes.Buffer or strings.Builder
	if isNamedType(t, "bytes", "Buffer") || isNamedType(t, "strings", "Builder") {
		return obj
	}
	return nil
}

// isNamedType checks if a type is named type from specified package and name
func isNamedType(t types.Type, pkgPath, name string) bool {
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}

	obj := named.Obj()
	if obj == nil {
		return false
	}

	pkg := obj.Pkg()
	if pkg == nil {
		return false
	}

	return pkg.Path() == pkgPath && obj.Name() == name
}

// isWriteMethod checks if a method is a write operation
func isWriteMethod(name string) bool {
	switch name {
	case "Write", "WriteString", "WriteRune", "WriteByte":
		return true
	}
	return false
}

// isReadMethod checks if a method is a read operation
func isReadMethod(name string) bool {
	switch name {
	case "String", "Bytes", "Len":
		return true
	}
	return false
}
