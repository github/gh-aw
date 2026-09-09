// Package slicecontainsloop implements a Go analysis linter that reports
// explicit membership-search loops over slices that can be replaced with
// slices.Contains.
package slicecontainsloop

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the slice-contains-loop analysis pass.
var Analyzer = analyzerutil.New("slicecontainsloop", "reports explicit membership-search loops over slices that can be replaced with slices.Contains", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.RangeStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		checkRangeStmt(pass, n, generatedFiles, noLintIndex)
	})
}

func checkRangeStmt(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	rangeStmt, ok := n.(*ast.RangeStmt)
	if !ok || rangeStmt.Body == nil || rangeStmt.Value == nil {
		return
	}

	pos := pass.Fset.PositionFor(rangeStmt.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "slicecontainsloop") {
		return
	}

	targetExpr, ok := matchContainsLoop(pass, rangeStmt)
	if !ok {
		return
	}

	seqText := astutil.NodeText(pass.Fset, rangeStmt.X)
	if seqText == "" {
		return
	}
	pass.ReportRangef(rangeStmt, "use slices.Contains(%s, %s) instead of a manual contains loop", seqText, astutil.NodeText(pass.Fset, targetExpr))
}

func matchContainsLoop(pass *analysis.Pass, rangeStmt *ast.RangeStmt) (ast.Expr, bool) {
	if len(rangeStmt.Body.List) != 1 {
		return nil, false
	}
	valueIdent, ok := rangeStmt.Value.(*ast.Ident)
	if !ok {
		return nil, false
	}
	ifStmt, ok := rangeStmt.Body.List[0].(*ast.IfStmt)
	if !ok || ifStmt.Else != nil {
		return nil, false
	}
	if !isEqualityCondition(pass, ifStmt.Cond, valueIdent) {
		return nil, false
	}
	returnStmt, ok := singleReturnTrue(ifStmt.Body)
	if !ok {
		return nil, false
	}
	if len(returnStmt.Results) != 1 {
		return nil, false
	}
	if _, ok := returnStmt.Results[0].(*ast.Ident); !ok {
		return nil, false
	}
	if ident, ok := returnStmt.Results[0].(*ast.Ident); ok && ident.Name == "true" {
		return comparisonTarget(pass, ifStmt.Cond, valueIdent), true
	}
	return nil, false
}

func isEqualityCondition(pass *analysis.Pass, expr ast.Expr, value *ast.Ident) bool {
	binaryExpr, ok := expr.(*ast.BinaryExpr)
	if !ok || binaryExpr.Op != token.EQL {
		return false
	}
	return isValueIdentifier(pass, binaryExpr.X, value) || isValueIdentifier(pass, binaryExpr.Y, value)
}

func comparisonTarget(pass *analysis.Pass, expr ast.Expr, value *ast.Ident) ast.Expr {
	binaryExpr, _ := expr.(*ast.BinaryExpr)
	if binaryExpr == nil {
		return nil
	}
	if isValueIdentifier(pass, binaryExpr.X, value) {
		return binaryExpr.Y
	}
	return binaryExpr.X
}

func isValueIdentifier(pass *analysis.Pass, expr ast.Expr, value *ast.Ident) bool {
	if pass.TypesInfo == nil || value == nil {
		return false
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	return pass.TypesInfo.ObjectOf(ident) == pass.TypesInfo.ObjectOf(value)
}

func singleReturnTrue(body *ast.BlockStmt) (*ast.ReturnStmt, bool) {
	if body == nil || len(body.List) != 1 {
		return nil, false
	}
	returnStmt, ok := body.List[0].(*ast.ReturnStmt)
	if !ok || len(returnStmt.Results) != 1 {
		return nil, false
	}
	ident, ok := returnStmt.Results[0].(*ast.Ident)
	if !ok || ident.Name != "true" {
		return nil, false
	}
	return returnStmt, true
}
