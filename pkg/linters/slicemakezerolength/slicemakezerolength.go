// Package slicemakezerolength implements a Go analysis linter that flags
// make([]T, 0) calls without a capacity argument when the final slice length
// can be statically determined, suggesting the capacity should be specified
// to avoid allocation overhead.
package slicemakezerolength

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/coverage"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the slice-make-zero-length analysis pass.
var Analyzer = analyzerutil.New("slicemakezerolength", "reports make([]T, 0) calls without capacity when the final length is known, which can be optimized", run)

// hotThreshold gates findings on coverage data; see coverage package docs.
var hotThreshold *int

func init() {
	hotThreshold = coverage.RegisterHotThresholdFlag(Analyzer)
}

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.BlockStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeBlock(pass, n, generatedFiles, noLintIndex)
	})
}

func analyzeBlock(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	block, ok := n.(*ast.BlockStmt)
	if !ok {
		return
	}

	for i, stmt := range block.List {
		target, call, sliceType, ok := zeroLengthSliceAssignment(pass, stmt)
		if !ok {
			continue
		}
		for offset, stmt := range block.List[i+1:] {
			j := i + 1 + offset
			if rangeStmt, ok := stmt.(*ast.RangeStmt); ok {
				if hasKnownRangeSize(pass, rangeStmt.X) && !containsReference(pass, rangeStmt.X, target) &&
					appendsOneElement(pass, rangeStmt, target) && rangeAvailableBefore(pass, rangeStmt.X, block.List[i+1:j]) {
					reportDiagnostic(pass, call, sliceType, rangeStmt.X, generatedFiles, noLintIndex)
					break
				}
			}
			if containsReference(pass, stmt, target) || containsEarlyExit(stmt) {
				break
			}
		}
	}
}

// Moving the range expression before an early exit may evaluate it on a path
// that previously never reached the loop.
func containsEarlyExit(stmt ast.Stmt) bool {
	found := false
	ast.Inspect(stmt, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt, *ast.BranchStmt:
			found = true
		}
		return !found
	})
	return found
}

// Moving len(rangeExpr) to the allocation must not cross a definition or use of
// its inputs. Conservatively stop even on reads, which may escape mutable inputs.
func rangeAvailableBefore(pass *analysis.Pass, rangeExpr ast.Expr, intervening []ast.Stmt) bool {
	available := true
	ast.Inspect(rangeExpr, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		object := pass.TypesInfo.ObjectOf(ident)
		if object == nil {
			return true
		}
		for _, stmt := range intervening {
			if containsReference(pass, stmt, object) {
				available = false
				return false
			}
		}
		return available
	})
	return available
}

func zeroLengthSliceAssignment(pass *analysis.Pass, stmt ast.Stmt) (types.Object, *ast.CallExpr, *ast.ArrayType, bool) {
	var target *ast.Ident
	var value ast.Expr

	switch stmt := stmt.(type) {
	case *ast.AssignStmt:
		if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
			return nil, nil, nil, false
		}
		if ident, ok := stmt.Lhs[0].(*ast.Ident); ok { //nolint:uncheckedsliceindex // len(stmt.Lhs) is checked above
			target = ident
		}
		value = stmt.Rhs[0] //nolint:uncheckedsliceindex // len(stmt.Rhs) is checked above
	case *ast.DeclStmt:
		decl, ok := stmt.Decl.(*ast.GenDecl)
		if !ok || len(decl.Specs) != 1 {
			return nil, nil, nil, false
		}
		spec, ok := decl.Specs[0].(*ast.ValueSpec) //nolint:uncheckedsliceindex // len(decl.Specs) is checked above
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 1 {
			return nil, nil, nil, false
		}
		target = spec.Names[0] //nolint:uncheckedsliceindex // len(spec.Names) is checked above
		value = spec.Values[0] //nolint:uncheckedsliceindex // len(spec.Values) is checked above
	default:
		return nil, nil, nil, false
	}

	if target == nil {
		return nil, nil, nil, false
	}
	targetObject := pass.TypesInfo.ObjectOf(target)
	if targetObject == nil {
		return nil, nil, nil, false
	}

	call, ok := astutil.UnwrapParenExpr(value).(*ast.CallExpr)
	if !ok || len(call.Args) != 2 || call.Ellipsis.IsValid() {
		return nil, nil, nil, false
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok || ident.Name != "make" {
		return nil, nil, nil, false
	}
	if pass.TypesInfo.ObjectOf(ident) != types.Universe.Lookup("make") {
		return nil, nil, nil, false
	}

	sliceType, ok := call.Args[0].(*ast.ArrayType) //nolint:uncheckedsliceindex // len(call.Args) is checked above
	if !ok || sliceType.Len != nil {
		return nil, nil, nil, false
	}
	if !isConstantZero(pass, call.Args[1]) { //nolint:uncheckedsliceindex // len(call.Args) is checked above
		return nil, nil, nil, false
	}

	return targetObject, call, sliceType, true
}

func hasKnownRangeSize(pass *analysis.Pass, expr ast.Expr) bool {
	typ := pass.TypesInfo.TypeOf(expr)
	if typ == nil {
		return false
	}
	switch underlying := typ.Underlying().(type) {
	case *types.Array, *types.Slice, *types.Map:
		return true
	case *types.Basic:
		return underlying.Info()&types.IsString != 0
	default:
		return false
	}
}

func appendsOneElement(pass *analysis.Pass, rangeStmt *ast.RangeStmt, target types.Object) bool {
	if len(rangeStmt.Body.List) != 1 {
		return false
	}
	assign, ok := rangeStmt.Body.List[0].(*ast.AssignStmt)                                             //nolint:uncheckedsliceindex // len(rangeStmt.Body.List) is checked above
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || !refersTo(pass, assign.Lhs[0], target) { //nolint:uncheckedsliceindex // len(assign.Lhs) is checked before indexing
		return false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)                                                           //nolint:uncheckedsliceindex // len(assign.Rhs) is checked above
	if !ok || len(call.Args) != 2 || call.Ellipsis.IsValid() || !refersTo(pass, call.Args[0], target) { //nolint:uncheckedsliceindex // len(call.Args) is checked before indexing
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(ident) == types.Universe.Lookup("append")
}

func refersTo(pass *analysis.Pass, expr ast.Expr, target types.Object) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(ident) == target
}

func containsReference(pass *analysis.Pass, expr ast.Node, target types.Object) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if ok && pass.TypesInfo.ObjectOf(ident) == target {
			found = true
			return false
		}
		return !found
	})
	return found
}

func reportDiagnostic(pass *analysis.Pass, call *ast.CallExpr, sliceType *ast.ArrayType, rangeExpr ast.Expr, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "slicemakezerolength") {
		return
	}
	if !coverage.ShouldApply(pass, call.Pos(), *hotThreshold) {
		return
	}

	sliceTypeText := astutil.NodeText(pass.Fset, sliceType)
	lenText := astutil.NodeText(pass.Fset, call.Args[1]) //nolint:uncheckedsliceindex // zeroLengthSliceAssignment requires exactly two arguments
	rangeText := astutil.NodeText(pass.Fset, rangeExpr)
	if sliceTypeText == "" || lenText == "" || rangeText == "" {
		return
	}

	pass.Report(analysis.Diagnostic{
		Pos:     call.Pos(),
		End:     call.End(),
		Message: fmt.Sprintf("make(%s, %s) before this range loop can use capacity len(%s)", sliceTypeText, lenText, rangeText),
	})
}

func isConstantZero(pass *analysis.Pass, expr ast.Expr) bool {
	value := pass.TypesInfo.Types[expr].Value
	return value != nil && constant.Sign(value) == 0
}
