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

	for i := 0; i+1 < len(block.List); i++ {
		target, call, sliceType, ok := zeroLengthSliceAssignment(pass, block.List[i])
		if !ok {
			continue
		}
		rangeStmt, ok := block.List[i+1].(*ast.RangeStmt)
		if !ok || !hasKnownRangeSize(pass, rangeStmt.X) || containsReference(pass, rangeStmt.X, target) {
			continue
		}
		if !appendsOneElement(pass, rangeStmt, target) {
			continue
		}

		reportDiagnostic(pass, call, sliceType, rangeStmt.X, generatedFiles, noLintIndex)
	}
}

func zeroLengthSliceAssignment(pass *analysis.Pass, stmt ast.Stmt) (types.Object, *ast.CallExpr, *ast.ArrayType, bool) {
	var target *ast.Ident
	var value ast.Expr

	switch stmt := stmt.(type) {
	case *ast.AssignStmt:
		if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
			return nil, nil, nil, false
		}
		target, _ = stmt.Lhs[0].(*ast.Ident)
		value = stmt.Rhs[0]
	case *ast.DeclStmt:
		decl, ok := stmt.Decl.(*ast.GenDecl)
		if !ok || len(decl.Specs) != 1 {
			return nil, nil, nil, false
		}
		spec, ok := decl.Specs[0].(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || len(spec.Values) != 1 {
			return nil, nil, nil, false
		}
		target = spec.Names[0]
		value = spec.Values[0]
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

	call, ok := value.(*ast.CallExpr)
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

	sliceType, ok := call.Args[0].(*ast.ArrayType)
	if !ok || sliceType.Len != nil {
		return nil, nil, nil, false
	}
	if !isConstantZero(pass, call.Args[1]) {
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
	assign, ok := rangeStmt.Body.List[0].(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || !refersTo(pass, assign.Lhs[0], target) {
		return false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 2 || call.Ellipsis.IsValid() || !refersTo(pass, call.Args[0], target) {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(ident) == types.Universe.Lookup("append")
}

func refersTo(pass *analysis.Pass, expr ast.Expr, target types.Object) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && pass.TypesInfo.ObjectOf(ident) == target
}

func containsReference(pass *analysis.Pass, expr ast.Expr, target types.Object) bool {
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
	lenText := astutil.NodeText(pass.Fset, call.Args[1])
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
