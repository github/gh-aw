// Package uncheckedsliceindex implements a Go analysis linter that flags
// direct slice or string indexing without bounds checking that can panic at runtime.
package uncheckedsliceindex

import (
	"go/ast"
	"go/constant"
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
	if isArrayWithConstantIndex(pass, idxExpr) || isConstantStringIndex(pass, idxExpr) {
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
	if isInRangeLoop(pass, idxExpr, parents) || isInBoundedForLoop(pass, idxExpr, parents) {
		return
	}

	// Check if there's a bounds check in the control flow.
	if hasBoundsCheck(pass, idxExpr, parents) {
		return
	}

	reportIndex(pass, idxExpr, isString, fset)
}

func reportIndex(pass *analysis.Pass, idxExpr *ast.IndexExpr, isString bool, fset *token.FileSet) {
	var typeDesc string
	if isString {
		typeDesc = "string"
	} else {
		typeDesc = "slice"
	}

	pass.ReportRangef(
		idxExpr,
		"direct %s indexing without bounds checking; ensure %s is nonnegative and less than len(%s) before indexing",
		typeDesc,
		astutil.NodeText(fset, idxExpr.Index),
		astutil.NodeText(fset, idxExpr.X),
	)
}

// isConstantStringIndex reports whether a constant string has an in-range constant index.
func isConstantStringIndex(pass *analysis.Pass, idxExpr *ast.IndexExpr) bool {
	index, isConst := astutil.ConstIntValue(pass, idxExpr.Index)
	value := pass.TypesInfo.Types[idxExpr.X].Value
	return isConst && index >= 0 && value != nil && value.Kind() == constant.String &&
		index < int64(len(constant.StringVal(value)))
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

// isInRangeLoop reports whether node indexes the value ranged over using that
// range statement's key.
func isInRangeLoop(pass *analysis.Pass, node *ast.IndexExpr, parents map[ast.Node]ast.Node) bool {
	current := ast.Node(node)
	for {
		parent, ok := parents[current]
		if !ok {
			return false
		}

		if rangeStmt, ok := parent.(*ast.RangeStmt); ok {
			return isAncestorOf(rangeStmt.Body, current, parents) &&
				sameExpr(pass, node.X, rangeStmt.X) &&
				sameExpr(pass, node.Index, rangeStmt.Key)
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

// isInBoundedForLoop reports whether a counted loop proves the index is in range.
func isInBoundedForLoop(pass *analysis.Pass, idxExpr *ast.IndexExpr, parents map[ast.Node]ast.Node) bool {
	current := ast.Node(idxExpr)
	for {
		parent, ok := parents[current]
		if !ok {
			return false
		}
		if forStmt, ok := parent.(*ast.ForStmt); ok {
			if !isAncestorOf(forStmt.Body, current, parents) {
				current = parent
				continue
			}
			index, ok := idxExpr.Index.(*ast.Ident)
			if ok && initializedToZero(pass, forStmt.Init, index) &&
				increments(pass, forStmt.Post, index) &&
				hasUpperBound(pass, forStmt.Cond, idxExpr) &&
				!writesObjects(pass, forStmt.Body.List, idxExpr.X, idxExpr.Index) {
				return true
			}
		}
		if _, ok := parent.(*ast.FuncDecl); ok {
			return false
		}
		if _, ok := parent.(*ast.FuncLit); ok {
			return false
		}
		current = parent
	}
}

// hasBoundsCheck reports whether an index expression is guarded by valid bounds.
func hasBoundsCheck(pass *analysis.Pass, idxExpr *ast.IndexExpr, parents map[ast.Node]ast.Node) bool {
	current := ast.Node(idxExpr)
	for {
		parent, ok := parents[current]
		if !ok {
			return hasTerminatingGuardBefore(pass, idxExpr, parents)
		}
		if ifStmt, ok := parent.(*ast.IfStmt); ok && isAncestorOf(ifStmt.Body, current, parents) &&
			hasBounds(pass, ifStmt.Cond, idxExpr) &&
			!changedBefore(pass, ifStmt.Body, idxExpr, parents, idxExpr.X, idxExpr.Index) {
			return true
		}
		if _, ok := parent.(*ast.FuncDecl); ok {
			return hasTerminatingGuardBefore(pass, idxExpr, parents)
		}
		if _, ok := parent.(*ast.FuncLit); ok {
			return hasTerminatingGuardBefore(pass, idxExpr, parents)
		}
		current = parent
	}
}

// hasBounds reports whether cond proves both bounds for idxExpr.
func hasBounds(pass *analysis.Pass, cond ast.Expr, idxExpr *ast.IndexExpr) bool {
	upper, lower := boundFacts(pass, cond, idxExpr)
	return upper && lower
}

// hasUpperBound reports whether cond proves a strict upper bound for idxExpr.
func hasUpperBound(pass *analysis.Pass, cond ast.Expr, idxExpr *ast.IndexExpr) bool {
	upper, _ := boundFacts(pass, cond, idxExpr)
	return upper
}

// boundFacts reports strict upper and nonnegative lower-bound facts from a conjunction.
// It deliberately ignores disjunctions because they do not prove facts on every path.
func boundFacts(pass *analysis.Pass, cond ast.Expr, idxExpr *ast.IndexExpr) (upper, lower bool) {
	cond = astutil.UnwrapParenExpr(cond)
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return false, false
	}
	if bin.Op == token.LOR {
		// A disjunction does not establish either bound on every path.
		return false, false
	}
	if bin.Op == token.LAND {
		upperX, lowerX := boundFacts(pass, bin.X, idxExpr)
		upperY, lowerY := boundFacts(pass, bin.Y, idxExpr)
		return upperX || upperY, lowerX || lowerY
	}
	if isLenOf(pass, bin.X, idxExpr.X) && sameExpr(pass, bin.Y, idxExpr.Index) && bin.Op == token.GTR {
		return true, indexNonnegative(pass, idxExpr.Index)
	}
	if sameExpr(pass, bin.X, idxExpr.Index) && isLenOf(pass, bin.Y, idxExpr.X) && bin.Op == token.LSS {
		return true, indexNonnegative(pass, idxExpr.Index)
	}
	if sameExpr(pass, bin.X, idxExpr.Index) && isZero(pass, bin.Y) && bin.Op == token.GEQ {
		return false, true
	}
	if isLenOf(pass, bin.X, idxExpr.X) && isZero(pass, bin.Y) &&
		(bin.Op == token.GTR || bin.Op == token.NEQ) && isZero(pass, idxExpr.Index) {
		return true, true
	}
	return false, false
}

// hasTerminatingGuardBefore reports whether a preceding invalid-bounds guard terminates.
func hasTerminatingGuardBefore(pass *analysis.Pass, idxExpr *ast.IndexExpr, parents map[ast.Node]ast.Node) bool {
	current := ast.Node(idxExpr)
	for {
		block, target := enclosingBlock(current, parents)
		if block == nil {
			return false
		}
		for i, stmt := range block.List {
			if stmt != target {
				continue
			}
			for j, prior := range block.List[:i] {
				ifStmt, ok := prior.(*ast.IfStmt)
				if !ok || !terminates(pass, ifStmt.Body) || writesObjects(pass, block.List[j+1:i], idxExpr.X, idxExpr.Index) {
					continue
				}
				upper, lower := invalidBounds(pass, ifStmt.Cond, idxExpr)
				if upper && lower {
					return true
				}
			}
			if _, ok := parents[block].(*ast.FuncDecl); ok {
				return false
			}
			if _, ok := parents[block].(*ast.FuncLit); ok {
				return false
			}
		}
		current = block
	}
}

// invalidBounds reports bounds violations that make a terminating guard safe to pass.
func invalidBounds(pass *analysis.Pass, cond ast.Expr, idxExpr *ast.IndexExpr) (upper, lower bool) {
	cond = astutil.UnwrapParenExpr(cond)
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok {
		return false, false
	}
	if bin.Op == token.LOR {
		upperX, lowerX := invalidBounds(pass, bin.X, idxExpr)
		upperY, lowerY := invalidBounds(pass, bin.Y, idxExpr)
		return upperX || upperY, lowerX || lowerY
	}
	if sameExpr(pass, bin.X, idxExpr.Index) && isLenOf(pass, bin.Y, idxExpr.X) && bin.Op == token.GEQ {
		return true, false
	}
	if sameExpr(pass, bin.X, idxExpr.Index) && isZero(pass, bin.Y) && bin.Op == token.LSS {
		return false, true
	}
	return false, false
}

// sameExpr reports whether x and y are identifiers for the same type-checker object.
func sameExpr(pass *analysis.Pass, x, y ast.Expr) bool {
	xID, xOK := astutil.UnwrapParenExpr(x).(*ast.Ident)
	yID, yOK := astutil.UnwrapParenExpr(y).(*ast.Ident)
	return xOK && yOK && pass.TypesInfo.ObjectOf(xID) != nil &&
		pass.TypesInfo.ObjectOf(xID) == pass.TypesInfo.ObjectOf(yID)
}

// isLenOf reports whether expr is the builtin len call for value.
func isLenOf(pass *analysis.Pass, expr, value ast.Expr) bool {
	call, ok := astutil.UnwrapParenExpr(expr).(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || !sameExpr(pass, call.Args[0], value) { //nolint:uncheckedsliceindex // len call.Args is checked above
		return false
	}
	fun, ok := call.Fun.(*ast.Ident)
	if !ok {
		return false
	}
	builtin, isBuiltin := pass.TypesInfo.ObjectOf(fun).(*types.Builtin)
	return isBuiltin && builtin.Name() == "len"
}

// isZero reports whether expr is the integer constant zero.
func isZero(pass *analysis.Pass, expr ast.Expr) bool {
	value, ok := astutil.ConstIntValue(pass, expr)
	return ok && value == 0
}

// indexNonnegative reports whether expr is a nonnegative constant or unsigned value.
func indexNonnegative(pass *analysis.Pass, expr ast.Expr) bool {
	if value, ok := astutil.ConstIntValue(pass, expr); ok {
		return value >= 0
	}
	basic, ok := pass.TypesInfo.TypeOf(expr).Underlying().(*types.Basic)
	return ok && basic.Info()&types.IsUnsigned != 0
}

// initializedToZero reports whether stmt initializes index to zero.
func initializedToZero(pass *analysis.Pass, stmt ast.Stmt, index *ast.Ident) bool {
	assign, ok := stmt.(*ast.AssignStmt)
	return ok && len(assign.Lhs) == 1 && len(assign.Rhs) == 1 &&
		sameExpr(pass, assign.Lhs[0], index) && isZero(pass, assign.Rhs[0]) //nolint:uncheckedsliceindex // lengths are checked above
}

// increments reports whether stmt increments index by one.
func increments(pass *analysis.Pass, stmt ast.Stmt, index *ast.Ident) bool {
	inc, ok := stmt.(*ast.IncDecStmt)
	return ok && inc.Tok == token.INC && sameExpr(pass, inc.X, index)
}

// enclosingBlock returns node's closest containing block and its statement in that block.
func enclosingBlock(node ast.Node, parents map[ast.Node]ast.Node) (*ast.BlockStmt, ast.Stmt) {
	current := node
	var target ast.Stmt
	for {
		parent, ok := parents[current]
		if !ok {
			return nil, nil
		}
		if stmt, ok := current.(ast.Stmt); ok {
			target = stmt
		}
		if block, ok := parent.(*ast.BlockStmt); ok {
			return block, target
		}
		current = parent
	}
}

// changedBefore reports whether values are written before node within block.
// It returns true if the parent chain cannot be classified, preserving fail-closed analysis.
// It intentionally considers only lexically preceding statements.
func changedBefore(pass *analysis.Pass, block *ast.BlockStmt, node ast.Node, parents map[ast.Node]ast.Node, values ...ast.Expr) bool {
	current := node
	for {
		parent, ok := parents[current]
		if !ok {
			return true
		}
		if parentBlock, ok := parent.(*ast.BlockStmt); ok {
			target, ok := current.(ast.Stmt)
			if !ok {
				return true
			}
			for i, stmt := range parentBlock.List {
				if stmt == target && writesObjects(pass, parentBlock.List[:i], values...) {
					return true
				}
			}
			if parentBlock == block {
				return false
			}
		}
		current = parent
	}
}

// writesObjects reports whether statements assign to any value expression's object.
func writesObjects(pass *analysis.Pass, statements []ast.Stmt, values ...ast.Expr) bool {
	objects := make(map[types.Object]struct{}, len(values))
	for _, value := range values {
		if ident, ok := astutil.UnwrapParenExpr(value).(*ast.Ident); ok {
			if object := pass.TypesInfo.ObjectOf(ident); object != nil {
				objects[object] = struct{}{}
			}
		}
	}
	for _, stmt := range statements {
		written := false
		ast.Inspect(stmt, func(node ast.Node) bool {
			if written {
				return false
			}
			switch n := node.(type) {
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok {
						if _, ok := objects[pass.TypesInfo.ObjectOf(ident)]; ok {
							written = true
							return false
						}
					}
				}
			case *ast.IncDecStmt:
				if ident, ok := n.X.(*ast.Ident); ok {
					if _, ok := objects[pass.TypesInfo.ObjectOf(ident)]; ok {
						written = true
						return false
					}
				}
			}
			return true
		})
		if written {
			return true
		}
	}
	return false
}

// terminates reports whether block ends in a statement that cannot fall through.
func terminates(pass *analysis.Pass, block *ast.BlockStmt) bool {
	if len(block.List) == 0 {
		return false
	}
	switch stmt := block.List[len(block.List)-1].(type) { //nolint:uncheckedsliceindex // len block.List is checked above
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	case *ast.ExprStmt:
		call, ok := stmt.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok {
			return false
		}
		builtin, isBuiltin := pass.TypesInfo.ObjectOf(ident).(*types.Builtin)
		return isBuiltin && builtin.Name() == "panic"
	}
	return false
}
