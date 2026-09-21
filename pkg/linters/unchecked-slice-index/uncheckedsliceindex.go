// Package uncheckedsliceindex implements a Go analysis linter that flags
// direct slice or string indexing without bounds checking that can panic at runtime.
package uncheckedsliceindex

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the unchecked-slice-index analysis pass.
var Analyzer = analyzerutil.New("uncheckedsliceindex", "reports direct slice or string indexing without bounds checking that can panic at runtime", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	// Build parent maps for each file to enable control flow analysis.
	fileParents := make(map[*ast.File]map[ast.Node]ast.Node)
	for _, f := range pass.Files {
		fileParents[f] = astutil.BuildParentMap(f)
	}

	nodeFilter := []ast.Node{(*ast.IndexExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeIndexExpr(pass, n, generatedFiles, noLintIndex, fileParents, pass.Fset)
	})
}

// analyzeIndexExpr checks whether an index expression accesses a slice or string
// without an apparent bounds check in the control flow.
func analyzeIndexExpr(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex, fileParents map[*ast.File]map[ast.Node]ast.Node, fset *token.FileSet) {
	idxExpr, ok := n.(*ast.IndexExpr)
	if !ok {
		return
	}

	pos := pass.Fset.PositionFor(idxExpr.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "uncheckedsliceindex") {
		return
	}

	// Check if the indexed value is a slice or string type.
	indexedType := pass.TypesInfo.TypeOf(idxExpr.X)
	if indexedType == nil {
		return
	}

	isSlice, isString := isSliceOrStringType(indexedType)
	if !isSlice && !isString {
		return
	}

	// Skip arrays with constant indices (provably safe).
	if isArrayWithConstantIndex(pass, idxExpr) {
		return
	}

	// Get the parent chain to analyze control flow.
	f := astutil.FileForPos(pass.Files, idxExpr.Pos())
	var parents map[ast.Node]ast.Node
	if f != nil {
		parents = fileParents[f]
	}
	if parents == nil {
		return
	}

	// Check if this index is inside a range loop; if so, it's safe.
	if isInRangeLoop(idxExpr, parents) {
		return
	}

	// Check if there's a bounds check in the control flow.
	if hasBoundsCheck(pass, idxExpr, parents, fset) {
		return
	}

	// Report the finding.
	var typeDesc string
	if isString {
		typeDesc = "string"
	} else {
		typeDesc = "slice"
	}

	pass.ReportRangef(
		idxExpr,
		"direct %s indexing without bounds checking; use `if len(%s) > %s { ... }` or similar guard",
		typeDesc,
		astutil.NodeText(fset, idxExpr.X),
		astutil.NodeText(fset, idxExpr.Index),
	)
}

// isSliceOrStringType reports whether t is a slice or string type.
func isSliceOrStringType(t types.Type) (isSlice, isString bool) {
	if t == nil {
		return false, false
	}

	// Unwrap untyped types.
	if basic, ok := t.(*types.Basic); ok {
		if basic.Kind() == types.String || basic.Kind() == types.UntypedString {
			return false, true
		}
	}

	// Check for string type (including named string types) in underlying type.
	if basic, ok := t.Underlying().(*types.Basic); ok && basic.Kind() == types.String {
		return false, true
	}

	// Check for slice type.
	if _, ok := t.Underlying().(*types.Slice); ok {
		return true, false
	}

	// Check for array type.
	if _, ok := t.Underlying().(*types.Array); ok {
		return true, false
	}

	return false, false
}

// isArrayWithConstantIndex reports whether idxExpr accesses an array type with a constant index.
func isArrayWithConstantIndex(pass *analysis.Pass, idxExpr *ast.IndexExpr) bool {
	indexedType := pass.TypesInfo.TypeOf(idxExpr.X)
	if indexedType == nil {
		return false
	}

	// Check if it's an array type.
	_, ok := indexedType.Underlying().(*types.Array)
	if !ok {
		return false
	}

	// Check if the index is a constant integer.
	_, isConst := astutil.ConstIntValue(pass, idxExpr.Index)
	return isConst
}

// isInRangeLoop reports whether node is part of a range loop's body.
func isInRangeLoop(node ast.Node, parents map[ast.Node]ast.Node) bool {
	current := node
	for {
		parent, ok := parents[current]
		if !ok {
			return false
		}

		// If we encounter a range statement, check if the node is in its body.
		if rangeStmt, ok := parent.(*ast.RangeStmt); ok {
			// Check if we're in the body of this range.
			return isAncestorOf(rangeStmt.Body, current, parents)
		}

		// Stop traversing if we hit a function boundary.
		if _, ok := parent.(*ast.FuncDecl); ok {
			return false
		}
		if _, ok := parent.(*ast.FuncLit); ok {
			return false
		}

		current = parent
	}
}

// isAncestorOf reports whether ancestor is an ancestor of node in the parent chain.
func isAncestorOf(ancestor, node ast.Node, parents map[ast.Node]ast.Node) bool {
	current := node
	for {
		if current == ancestor {
			return true
		}
		parent, ok := parents[current]
		if !ok {
			return false
		}
		current = parent
	}
}

// hasBoundsCheck reports whether there's a bounds check for the given index
// expression in the immediate control flow.
func hasBoundsCheck(pass *analysis.Pass, idxExpr *ast.IndexExpr, parents map[ast.Node]ast.Node, fset *token.FileSet) bool {
	// Walk up the parent chain to find the enclosing if/else statement.
	current := ast.Node(idxExpr)
	for {
		parent, ok := parents[current]
		if !ok {
			return false
		}

		// Stop at function boundaries.
		if _, ok := parent.(*ast.FuncDecl); ok {
			return false
		}
		if _, ok := parent.(*ast.FuncLit); ok {
			return false
		}

		// If we encounter an if statement, check if it guards this index.
		if ifStmt, ok := parent.(*ast.IfStmt); ok {
			if checkIfStatementGuards(pass, ifStmt, idxExpr, parents, fset) {
				return true
			}
		}

		current = parent
	}
}

// checkIfStatementGuards reports whether an if statement contains a bounds check
// that guards the given index expression.
func checkIfStatementGuards(pass *analysis.Pass, ifStmt *ast.IfStmt, idxExpr *ast.IndexExpr, parents map[ast.Node]ast.Node, fset *token.FileSet) bool {
	if ifStmt == nil || idxExpr == nil {
		return false
	}

	// Check if the condition contains a bounds check for the indexed value and index.
	if checkConditionForBounds(pass, ifStmt.Cond, idxExpr, fset) {
		// Now verify that idxExpr is actually inside the then-body of the if.
		return isAncestorOf(ifStmt.Body, idxExpr, parents)
	}

	return false
}

// checkConditionForBounds reports whether a condition checks bounds for an indexed value.
func checkConditionForBounds(pass *analysis.Pass, cond ast.Expr, idxExpr *ast.IndexExpr, fset *token.FileSet) bool {
	if cond == nil {
		return false
	}

	// Extract the variable name being indexed.
	indexedVar := astutil.NodeText(fset, idxExpr.X)
	indexValue := astutil.NodeText(fset, idxExpr.Index)

	// Unwrap parentheses.
	cond = astutil.UnwrapParenExpr(cond)

	// Check for binary expressions (e.g., len(s) > i, i < len(s), i < len(s) && ...)
	if binOp, ok := cond.(*ast.BinaryExpr); ok {
		// Handle AND operations: check if left side is a bounds check.
		if binOp.Op.String() == "&&" {
			if checkConditionForBounds(pass, binOp.X, idxExpr, fset) {
				return true
			}
		}

		// Check both sides for bounds check patterns.
		if isBoundsCheckExpr(pass, binOp.X, indexedVar, indexValue, fset) || isBoundsCheckExpr(pass, binOp.Y, indexedVar, indexValue, fset) {
			return true
		}

		// Also check the binary operation itself for patterns like:
		// len(s) > i, i < len(s), len(s) > 0, etc.
		if matchesBoundsCheckPattern(binOp, indexedVar, indexValue, fset) {
			return true
		}
	}

	return false
}

// isBoundsCheckExpr reports whether expr is a bounds check for the indexed variable.
func isBoundsCheckExpr(pass *analysis.Pass, expr ast.Expr, indexedVar, indexValue string, fset *token.FileSet) bool {
	if expr == nil {
		return false
	}

	expr = astutil.UnwrapParenExpr(expr)

	binOp, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return false
	}

	return matchesBoundsCheckPattern(binOp, indexedVar, indexValue, fset)
}

// matchesBoundsCheckPattern reports whether a binary operation is a bounds check.
func matchesBoundsCheckPattern(binOp *ast.BinaryExpr, indexedVar, indexValue string, fset *token.FileSet) bool {
	if binOp == nil {
		return false
	}

	lhs := astutil.NodeText(fset, binOp.X)
	rhs := astutil.NodeText(fset, binOp.Y)
	op := binOp.Op.String()

	// Pattern: len(s) > i
	if lhs == fmt.Sprintf("len(%s)", indexedVar) && rhs == indexValue && (op == ">" || op == ">=") {
		return true
	}

	// Pattern: i < len(s)
	if lhs == indexValue && rhs == fmt.Sprintf("len(%s)", indexedVar) && (op == "<" || op == "<=") {
		return true
	}

	// Pattern: len(s) > 0 (simple check for non-empty)
	if lhs == fmt.Sprintf("len(%s)", indexedVar) && rhs == "0" && (op == ">" || op == "!=") {
		// This is safe for indexing like s[0].
		return indexValue == "0"
	}

	// Pattern: i >= 0 && i < len(s) is handled by the AND case in checkConditionForBounds.
	// Here we just check: i >= 0
	if lhs == indexValue && rhs == "0" && op == ">=" {
		return false // Not a complete bounds check by itself.
	}

	return false
}
