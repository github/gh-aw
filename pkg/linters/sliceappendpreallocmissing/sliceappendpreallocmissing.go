// Package sliceappendpreallocmissing implements a Go analysis linter that flags
// slice append operations in loops where the final length can be determined
// but the slice is not pre-allocated with make(..., capacity).
package sliceappendpreallocmissing

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/ast/inspector"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:sliceappendpreallocmissing")

// Analyzer is the slice-append-prealloc-missing analysis pass.
var Analyzer = analyzerutil.New("sliceappendpreallocmissing", "reports slice append operations in loops where the final length can be determined but the slice is not pre-allocated with make(..., capacity)", run)

// hotThreshold gates findings on coverage data; see coverage package docs.
var hotThreshold *int

func init() {
	hotThreshold = coverage.RegisterHotThresholdFlag(Analyzer)
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

	// Track (variable, loop) pairs we've already flagged to avoid duplicate reports
	flagged := make(map[string]bool)

	for cur := range root.Preorder((*ast.AssignStmt)(nil)) {
		assign, ok := cur.Node().(*ast.AssignStmt)
		if !ok {
			continue
		}

		// Check if this is an append call
		appendInfo, ok := analyzeAppendCall(pass, assign)
		if !ok {
			continue
		}

		// Check if it's inside a loop
		loopPos, loopNode, inLoop := enclosingLoop(pass, cur)
		if !inLoop {
			continue
		}

		// Only flag appends that run on every iteration: a conditional append
		// does not grow the slice by the loop trip count.
		if !isDirectLoopBodyStmt(loopNode, assign) {
			continue
		}

		// Check if the slice was declared before the loop with no capacity
		declaredWithCapacity, ok := isSliceDeclaredBeforeLoop(pass, cur, appendInfo.target, loopNode)
		if !ok || declaredWithCapacity {
			continue
		}

		// Get the loop iteration count
		loopSize, ok := getLoopSize(pass, loopNode)
		if !ok {
			continue
		}

		pos := pass.Fset.PositionFor(assign.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			continue
		}

		if nolint.HasDirectiveForLinter(pos, noLintIndex, "sliceappendpreallocmissing") ||
			nolint.HasDirectiveForLinter(loopPos, noLintIndex, "sliceappendpreallocmissing") {
			continue
		}

		if !coverage.ShouldApply(pass, assign.Pos(), *hotThreshold) {
			continue
		}

		// Create a key for this variable in this loop to avoid duplicate reports
		varName := appendInfo.target.Name()
		loopKey := fmt.Sprintf("%s:%d", varName, loopNode.Pos())
		if flagged[loopKey] {
			// Already flagged this variable in this loop
			continue
		}
		flagged[loopKey] = true

		sliceName := appendInfo.target.Name()
		pkgLog.Printf("flagging append for slice %s in loop at %s", sliceName, pos)
		pass.ReportRangef(assign,
			"slice %s should be pre-allocated with capacity %s instead of dynamically growing via repeated append calls in a loop",
			sliceName, loopSize)
	}

	return nil, nil
}

// appendInfo holds information about an append operation.
type appendInfo struct {
	target types.Object // The object being appended to
}

// analyzeAppendCall checks if assign is an append call and returns info about the target.
func analyzeAppendCall(pass *analysis.Pass, assign *ast.AssignStmt) (*appendInfo, bool) {
	// Must be a simple assignment x = y
	if assign.Tok != token.ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return nil, false
	}

	// LHS must be an identifier
	lhsIdent, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return nil, false
	}

	// RHS must be an append call
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return nil, false
	}

	// Check if it's a call to built-in append
	fun, ok := call.Fun.(*ast.Ident)
	if !ok || fun.Name != "append" {
		return nil, false
	}

	// Verify it's the built-in append
	if pass.TypesInfo.ObjectOf(fun) != types.Universe.Lookup("append") {
		return nil, false
	}

	// Must have at least 2 args: append(slice, elem...)
	if len(call.Args) < 2 {
		return nil, false
	}

	// First arg to append must be the same as LHS
	firstArgIdent, ok := call.Args[0].(*ast.Ident)
	if !ok || firstArgIdent.Name != lhsIdent.Name {
		return nil, false
	}

	targetObject := pass.TypesInfo.ObjectOf(lhsIdent)
	if targetObject == nil {
		return nil, false
	}

	return &appendInfo{target: targetObject}, true
}

// enclosingLoop returns the nearest enclosing for/range statement, its source
// position, and true if found, without crossing a function literal boundary.
func enclosingLoop(pass *analysis.Pass, cur inspector.Cursor) (token.Position, ast.Node, bool) {
	for encl := range cur.Enclosing(
		(*ast.ForStmt)(nil),
		(*ast.RangeStmt)(nil),
		(*ast.FuncLit)(nil),
	) {
		switch n := encl.Node().(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			return pass.Fset.PositionFor(n.Pos(), false), n, true
		case *ast.FuncLit:
			return token.Position{}, nil, false
		}
	}
	return token.Position{}, nil, false
}

// isDirectLoopBodyStmt reports whether stmt is a direct statement of the loop
// body, i.e. it is not nested inside a conditional or an inner block.
func isDirectLoopBodyStmt(loopNode ast.Node, stmt ast.Stmt) bool {
	var body *ast.BlockStmt
	switch loop := loopNode.(type) {
	case *ast.ForStmt:
		body = loop.Body
	case *ast.RangeStmt:
		body = loop.Body
	}
	if body == nil {
		return false
	}
	return slices.Contains(body.List, stmt)
}

// isSliceDeclaredBeforeLoop checks how the slice was last declared or assigned
// before the loop. Returns (shouldSkip, found), where shouldSkip is true when
// the state reaching the loop already has capacity or initial values.
func isSliceDeclaredBeforeLoop(
	pass *analysis.Pass,
	cur inspector.Cursor,
	target types.Object,
	loopNode ast.Node,
) (bool, bool) {
	// Find the declaration position of the target
	declPos := target.Pos()

	// The declaration must be before the loop
	loopPos := loopNode.Pos()
	if declPos >= loopPos {
		return false, false
	}

	// The fact that declPos < loopPos and the variable is used in the loop
	// means it was declared in an enclosing scope before the loop.
	// We just need to check how it was last declared or assigned before the loop.

	// Walk through all enclosing blocks looking for the declaration
	curCopy := cur
	for {
		parent := curCopy.Parent()
		// Parent will return a cursor even if we're at the root

		// Get the parent node and check its statements
		parentNode := parent.Node()
		if parentNode == nil {
			// We've reached the root, stop
			break
		}

		var stmts []ast.Stmt
		switch bn := parentNode.(type) {
		case *ast.BlockStmt:
			stmts = bn.List
		case *ast.FuncDecl:
			if bn.Body != nil {
				stmts = bn.Body.List
			}
		case *ast.FuncLit:
			if bn.Body != nil {
				stmts = bn.Body.List
			}
		default:
			curCopy = parent
			continue
		}

		// Look through the statements in this block for our declaration or
		// re-assignments. The last statement before the loop wins, since that
		// reflects the state of the variable when the loop starts.
		shouldSkip, found := false, false
		for _, stmt := range stmts {
			// Stop when we reach or pass the loop
			if stmt.Pos() >= loopPos {
				break
			}

			// Check if this is a declaration or assignment of our target
			if skip, ok := checkDeclareStmt(pass, stmt, target); ok {
				shouldSkip, found = skip, true
			}
		}
		if found {
			return shouldSkip, true
		}

		curCopy = parent
	}

	return false, false
}

// checkDeclareStmt checks if stmt declares target, and if so, whether it has a capacity,
// initial values, or is a nil slice. Returns (shouldSkip, found).
// shouldSkip is true if the slice should not be flagged (has capacity, has initial values, or is nil).
func checkDeclareStmt(pass *analysis.Pass, stmt ast.Stmt, target types.Object) (bool, bool) {
	switch stmt := stmt.(type) {
	case *ast.DeclStmt:
		decl, ok := stmt.Decl.(*ast.GenDecl)
		if !ok {
			return false, false
		}
		for _, spec := range decl.Specs {
			valSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range valSpec.Names {
				if pass.TypesInfo.ObjectOf(name) == target {
					// Found the declaration
					if i >= len(valSpec.Values) || len(valSpec.Values) == 0 {
						// No initializer - this is a nil slice, skip it
						return true, true
					}
					// Check if it's a make with capacity
					if hasMakeCapacity(pass, valSpec.Values[i]) {
						return true, true
					}
					// Check if it's a slice literal with elements
					if hasSliceLiteralElements(valSpec.Values[i]) {
						return true, true
					}
					return false, true
				}
			}
		}
	case *ast.AssignStmt:
		for i, lhs := range stmt.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			if pass.TypesInfo.ObjectOf(ident) == target {
				// Found the assignment
				if i < len(stmt.Rhs) {
					// Check if it's a make with capacity
					if hasMakeCapacity(pass, stmt.Rhs[i]) {
						return true, true
					}
					// Check if it's a slice literal with elements
					if hasSliceLiteralElements(stmt.Rhs[i]) {
						return true, true
					}
				}
				return false, true
			}
		}
	}
	return false, false
}

// hasMakeCapacity checks if expr is a make() call with a capacity argument.
func hasMakeCapacity(pass *analysis.Pass, expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}

	fun, ok := call.Fun.(*ast.Ident)
	if !ok || fun.Name != "make" {
		return false
	}

	if pass.TypesInfo.ObjectOf(fun) != types.Universe.Lookup("make") {
		return false
	}

	// make(T) - no capacity
	// make(T, len) - no capacity
	// make(T, len, cap) - has capacity
	return len(call.Args) >= 3
}

// hasSliceLiteralElements checks if expr is a slice literal with elements (not empty).
func hasSliceLiteralElements(expr ast.Expr) bool {
	lit, ok := expr.(*ast.CompositeLit)
	if !ok {
		return false
	}

	// Check if it's a slice type (has no Len field in the array type)
	arrayType, ok := lit.Type.(*ast.ArrayType)
	if !ok {
		return false
	}

	// Slice literal has no Len field
	if arrayType.Len != nil {
		return false
	}

	// Return true if there are any elements
	return len(lit.Elts) > 0
}

// getLoopSize returns the iteration count of the loop as a string.
// For for loops with constant init/condition, returns the count.
// For range loops over a constant-length expression (array, string literal, etc.), returns the length.
func getLoopSize(pass *analysis.Pass, loopNode ast.Node) (string, bool) {
	switch loop := loopNode.(type) {
	case *ast.ForStmt:
		// Handle simple for i := 0; i < n; i++ loops
		return getForLoopSize(pass, loop)
	case *ast.RangeStmt:
		// For range loops, only flag if we can statically determine the length
		rangeExpr := loop.X
		if hasKnownRangeLength(pass, rangeExpr) {
			return fmt.Sprintf("len(%s)", astutil.NodeText(pass.Fset, rangeExpr)), true
		}
		return "", false
	}
	return "", false
}

// hasKnownRangeLength checks if the range expression has a statically known length.
// This is true for array types, array/slice literals and string literals.
func hasKnownRangeLength(pass *analysis.Pass, expr ast.Expr) bool {
	typ := pass.TypesInfo.TypeOf(expr)
	if typ == nil {
		return false
	}

	switch underlying := typ.Underlying().(type) {
	case *types.Array:
		// Array type always has known, fixed length
		return true
	case *types.Slice:
		// Slice only has known length if it's a composite literal (e.g., []int{1, 2, 3})
		_, isLiteral := expr.(*ast.CompositeLit)
		return isLiteral
	case *types.Map:
		// Map length is never known at compile time
		return false
	case *types.Basic:
		// String type: only literals have known length at compile time
		if underlying.Info()&types.IsString != 0 {
			_, isLiteral := expr.(*ast.BasicLit)
			return isLiteral
		}
		return false
	default:
		return false
	}
}

// getForLoopSize extracts the loop count from a for loop, but only if the loop
// is a canonical counted loop: `for i := 0; i < const; i++` (or `i <= const`).
// Loops whose post statement does not advance the condition variable by exactly
// one are rejected, because their trip count is not the constant bound.
func getForLoopSize(pass *analysis.Pass, loop *ast.ForStmt) (string, bool) {
	// We need a Cond to determine the count
	if loop.Cond == nil {
		return "", false
	}

	binExpr, ok := loop.Cond.(*ast.BinaryExpr)
	if !ok {
		return "", false
	}

	// Only handle constant bounds
	if !isConstant(pass, binExpr.Y) {
		return "", false
	}

	// The condition must compare an induction variable that starts at zero and
	// is incremented by exactly one on every iteration.
	condIdent, ok := binExpr.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	induction := pass.TypesInfo.ObjectOf(condIdent)
	if induction == nil {
		return "", false
	}
	if !isZeroInitOf(pass, loop.Init, induction) || !isUnitIncrementOf(pass, loop.Post, induction) {
		return "", false
	}

	text := astutil.NodeText(pass.Fset, binExpr.Y)
	if text == "" {
		return "", false
	}

	switch binExpr.Op {
	case token.LSS:
		// i < n where n is constant
		return text, true
	case token.LEQ:
		// i <= n where n is constant: count is n+1
		return text + " + 1", true
	}

	return "", false
}

// isConstant checks if expr is a constant value (not a variable).
func isConstant(pass *analysis.Pass, expr ast.Expr) bool {
	value := pass.TypesInfo.Types[expr].Value
	return value != nil
}

// isZeroInitOf checks that init initializes the induction variable to 0.
func isZeroInitOf(pass *analysis.Pass, init ast.Stmt, induction types.Object) bool {
	assign, ok := init.(*ast.AssignStmt)
	if !ok {
		return false
	}

	if len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return false
	}

	if !isIdentOf(pass, assign.Lhs[0], induction) {
		return false
	}

	// RHS should be 0
	return isConstantZero(pass, assign.Rhs[0])
}

// isUnitIncrementOf checks that post advances the induction variable by exactly
// one, i.e. `i++` or `i += 1`.
func isUnitIncrementOf(pass *analysis.Pass, post ast.Stmt, induction types.Object) bool {
	switch stmt := post.(type) {
	case *ast.IncDecStmt:
		return stmt.Tok == token.INC && isIdentOf(pass, stmt.X, induction)
	case *ast.AssignStmt:
		if stmt.Tok != token.ADD_ASSIGN || len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
			return false
		}
		if !isIdentOf(pass, stmt.Lhs[0], induction) {
			return false
		}
		value := pass.TypesInfo.Types[stmt.Rhs[0]].Value
		return value != nil && constant.Compare(value, token.EQL, constant.MakeInt64(1))
	default:
		return false
	}
}

// isIdentOf reports whether expr is an identifier referring to obj.
func isIdentOf(pass *analysis.Pass, expr ast.Expr, obj types.Object) bool {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	return pass.TypesInfo.ObjectOf(ident) == obj
}

// isConstantZero checks if expr evaluates to the constant 0.
func isConstantZero(pass *analysis.Pass, expr ast.Expr) bool {
	value := pass.TypesInfo.Types[expr].Value
	return value != nil && constant.Sign(value) == 0
}
