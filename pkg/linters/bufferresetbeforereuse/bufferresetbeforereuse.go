// Package bufferresetbeforereuse implements a Go analysis linter that flags
// reuse of bytes.Buffer or strings.Builder without calling Reset() between writes,
// which can accumulate previous data.
package bufferresetbeforereuse

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

	// Analyze the function body for buffer write patterns
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

// analyzeBlockForBufferReuse examines a block statement for improper buffer reuse
func analyzeBlockForBufferReuse(pass *analysis.Pass, block *ast.BlockStmt, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	ast.Inspect(block, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.BlockStmt:
			analyzeStraightLineBlock(pass, node, generatedFiles, noLintIndex)
		}
		return true
	})
}

func analyzeStraightLineBlock(pass *analysis.Pass, block *ast.BlockStmt, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	// Collect all events (writes, reads, resets) in order
	var events []*event
	collectEvents(pass, block, &events)

	// For each variable, check if writes follow reads without Reset
	written := make(map[types.Object]bool) // has this variable been written?
	read := make(map[types.Object]bool)    // has this variable been read?

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

// collectEvents collects all write, read, and reset operations on buffers in order
func collectEvents(pass *analysis.Pass, block *ast.BlockStmt, events *[]*event) {
	ast.Inspect(block, func(n ast.Node) bool {
		if n != block {
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
