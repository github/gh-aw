// Package loopappendinefficency implements a Go analysis linter that flags
// append operations inside for/range loop bodies without pre-allocation,
// which can result in repeated slice growth and O(n) total allocations. The
// idiomatic fix is to pre-allocate the slice with make(slice, 0, capacity)
// before the loop.
package loopappendinefficency

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:loopappendinefficency")

// Analyzer is the loop-append-inefficiency analysis pass.
var Analyzer = analyzerutil.NewAtPath(
	"loopappendinefficency",
	"reports append operations inside loops that could be pre-allocated with make(slice, 0, capacity) for better performance",
	"github.com/github/gh-aw/pkg/linters/loopappendinefficency",
	run,
)

// hotThreshold gates findings on coverage data; see coverage package docs.
// Loop-append-inefficiency is a perf rule that only matters on hot paths: the
// repeated allocations are only worth paying attention to when the loop
// actually executes during tests.
var hotThreshold *int

func init() {
	hotThreshold = coverage.RegisterHotThresholdFlag(Analyzer)
}

// appendLoopMatch holds the components of an append-in-loop assignment
// identified by collectAppendLoopAssignment.
type appendLoopMatch struct {
	assign   *ast.AssignStmt
	lhsExpr  ast.Expr
	loopNode ast.Node
	pos      token.Position
}

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())
	root, err := astutil.Root(pass)
	if err != nil {
		return nil, err
	}
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	for cur := range root.Preorder((*ast.AssignStmt)(nil)) {
		m, ok := collectAppendLoopAssignment(pass, cur, noLintIndex, generatedFiles)
		if !ok {
			continue
		}
		if !shouldReportLoopAppend(pass, m.loopNode, m.lhsExpr) {
			continue
		}
		if !coverage.ShouldApply(pass, m.assign.Pos(), *hotThreshold) {
			continue
		}
		pkgLog.Printf("flagging append in loop at %s", m.pos)
		pass.ReportRangef(m.assign, "append inside a loop without pre-allocation causes repeated slice growth; use make(slice, 0, capacity) before the loop")
	}

	return nil, nil
}

func collectAppendLoopAssignment(
	pass *analysis.Pass,
	cur inspector.Cursor,
	noLintIndex nolint.DirectiveIndex,
	generatedFiles filecheck.GeneratedIndex,
) (*appendLoopMatch, bool) {
	assign, ok := cur.Node().(*ast.AssignStmt)
	if !ok {
		return nil, false
	}

	// Only handle simple assignments: x = append(x, ...)
	if assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil, false
	}

	// Check if RHS is an append call
	callExpr, ok := assign.Rhs[0].(*ast.CallExpr) //nolint:uncheckedsliceindex
	if !ok {
		return nil, false
	}

	if !isAppendCall(callExpr) {
		return nil, false
	}

	// Check if the first argument of append is the same as the LHS
	if len(callExpr.Args) < 1 {
		return nil, false
	}

	lhsExpr := assign.Lhs[0] //nolint:uncheckedsliceindex
	lhsIdent, ok := lhsExpr.(*ast.Ident)
	if !ok {
		return nil, false
	}

	rhsIdent, ok := callExpr.Args[0].(*ast.Ident) //nolint:uncheckedsliceindex
	if !ok {
		return nil, false
	}

	if lhsIdent.Name != rhsIdent.Name {
		return nil, false
	}

	pos := pass.Fset.PositionFor(assign.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return nil, false
	}

	loopPos, loopNode, inLoop := enclosingLoop(pass, cur)
	if !inLoop {
		return nil, false
	}

	if nolint.HasDirectiveForLinter(pos, noLintIndex, "loopappendinefficency") || nolint.HasDirectiveForLinter(loopPos, noLintIndex, "loopappendinefficency") {
		return nil, false
	}

	// Check if variable was pre-allocated before the loop
	if isPreAllocatedInEnclosingBlock(pass, cur, lhsIdent.Name, loopNode) {
		return nil, false
	}

	return &appendLoopMatch{assign: assign, lhsExpr: lhsExpr, loopNode: loopNode, pos: pos}, true
}

// isAppendCall checks if a CallExpr is a call to the builtin append function.
func isAppendCall(call *ast.CallExpr) bool {
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	return ident.Name == "append"
}

func shouldReportLoopAppend(pass *analysis.Pass, loopNode ast.Node, lhsExpr ast.Expr) bool {
	// Check if it's a slice type
	t := pass.TypesInfo.TypeOf(lhsExpr)
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Slice)
	if !ok {
		return false
	}

	lhsIdent, ok := lhsExpr.(*ast.Ident)
	if !ok {
		return true
	}

	// Don't flag if the variable is loop-scoped (range loop variable)
	if isLoopScopedIdent(loopNode, lhsIdent.Name) {
		return false
	}

	// Don't flag if the variable is declared inside the loop body
	if isLoopBodyLocal(pass, loopNode, lhsIdent) {
		return false
	}

	return true
}

// enclosingLoop returns the nearest enclosing for/range statement, its source
// position, and true for cur (an AssignStmt), without crossing a function
// literal boundary. Assignments inside func literals are intentionally exempt.
func enclosingLoop(pass *analysis.Pass, cur inspector.Cursor) (token.Position, ast.Node, bool) {
	for encl := range cur.Enclosing(
		(*ast.ForStmt)(nil),
		(*ast.RangeStmt)(nil),
		(*ast.FuncLit)(nil),
	) {
		switch encl.Node().(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			return pass.Fset.PositionFor(encl.Node().Pos(), false), encl.Node(), true
		case *ast.FuncLit:
			return token.Position{}, nil, false
		}
	}
	return token.Position{}, nil, false
}

// isLoopScopedIdent reports whether name is declared by loopNode as a loop
// variable: the Key or Value identifier of a RangeStmt. Such variables are
// per-iteration rebinds, not cross-iteration accumulators.
func isLoopScopedIdent(loopNode ast.Node, name string) bool {
	n, ok := loopNode.(*ast.RangeStmt)
	if !ok {
		return false
	}
	if id, ok := n.Key.(*ast.Ident); ok && id.Name == name {
		return true
	}
	if id, ok := n.Value.(*ast.Ident); ok && id.Name == name {
		return true
	}
	return false
}

// isLoopBodyLocal reports whether ident is declared inside the loop body
// (rather than before the loop). Such variables are freshly created on every
// iteration and are therefore not cross-iteration accumulators.
func isLoopBodyLocal(pass *analysis.Pass, loopNode ast.Node, ident *ast.Ident) bool {
	obj := pass.TypesInfo.ObjectOf(ident)
	if obj == nil {
		return false
	}
	var body *ast.BlockStmt
	switch n := loopNode.(type) {
	case *ast.ForStmt:
		body = n.Body
	case *ast.RangeStmt:
		body = n.Body
	}
	if body == nil {
		return false
	}
	pos := obj.Pos()
	return pos >= body.Lbrace && pos < body.Rbrace
}

// isPreAllocatedInEnclosingBlock checks if a variable was pre-allocated with make()
// before the loop by checking the enclosing block statements.
func isPreAllocatedInEnclosingBlock(pass *analysis.Pass, cur inspector.Cursor, varName string, loopNode ast.Node) bool {
	// Get the loop's start position
	var loopStart token.Pos
	switch n := loopNode.(type) {
	case *ast.ForStmt:
		loopStart = n.Pos()
	case *ast.RangeStmt:
		loopStart = n.Pos()
	default:
		return false
	}

	// Walk up through parent nodes to find a block that contains this loop
	for parent := range cur.Enclosing((*ast.BlockStmt)(nil)) {
		block, ok := parent.Node().(*ast.BlockStmt)
		if !ok {
			continue
		}

		// Check all statements in this block before the loop
		for _, stmt := range block.List {
			if stmt.Pos() >= loopStart {
				break
			}

			// Check if this statement initializes our variable with make()
			if isVarMakeInit(stmt, varName) {
				return true
			}
		}

		// After checking one block, stop (don't check parent blocks)
		return false
	}

	return false
}

// isVarMakeInit checks if a statement initializes varName with a make() call
func isVarMakeInit(stmt ast.Stmt, varName string) bool {
	// Handle: var x = make(...)
	if declStmt, ok := stmt.(*ast.DeclStmt); ok {
		if genDecl, ok := declStmt.Decl.(*ast.GenDecl); ok && genDecl.Tok == token.VAR {
			for _, spec := range genDecl.Specs {
				if valSpec, ok := spec.(*ast.ValueSpec); ok {
					for _, name := range valSpec.Names {
						if name.Name == varName && len(valSpec.Values) > 0 && isMakeCall(valSpec.Values[0]) { //nolint:uncheckedsliceindex
							return true
						}
					}
				}
			}
		}
		return false
	}

	// Handle: x = make(...) and x := make(...)
	if assign, ok := stmt.(*ast.AssignStmt); ok {
		if len(assign.Lhs) >= 1 && len(assign.Rhs) >= 1 {
			if lhs, ok := assign.Lhs[0].(*ast.Ident); ok && lhs.Name == varName { //nolint:uncheckedsliceindex
				if isMakeCall(assign.Rhs[0]) { //nolint:uncheckedsliceindex
					return true
				}
			}
		}
		return false
	}

	return false
}

// isMakeCall checks if an expression is a call to make() with a capacity argument.
// make() with 3 arguments like make([]int, 0, 10) indicates pre-allocation.
func isMakeCall(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	if ident.Name != "make" {
		return false
	}
	// Only consider it pre-allocated if make() has 3 arguments (type, length, capacity)
	// make([]int, 0, len) has 3 args
	// make([]int, 0) has 2 args
	return len(call.Args) == 3
}
