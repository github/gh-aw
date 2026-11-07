// Package osremoveuncheckedcleanup implements a Go analysis linter that flags
// os.Remove and os.RemoveAll calls whose errors are discarded.
package osremoveuncheckedcleanup

import (
	"go/ast"
	"path"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the os-remove-unchecked-cleanup analysis pass.
var Analyzer = analyzerutil.New("osremoveuncheckedcleanup", "reports os.Remove and os.RemoveAll calls whose errors are discarded", run)

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.AssignStmt)(nil), (*ast.ExprStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeDiscardedCleanup(pass, n, generatedFiles, nolintIndex)
	})
}

func analyzeDiscardedCleanup(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, nolintIndex nolint.DirectiveIndex) {
	stmt, ok := n.(ast.Stmt)
	if !ok {
		return
	}

	var call *ast.CallExpr
	var callOK bool
	switch stmt := stmt.(type) {
	case *ast.AssignStmt:
		if len(stmt.Lhs) != 1 || len(stmt.Rhs) != 1 {
			return
		}
		var lhs ast.Expr
		for _, expr := range stmt.Lhs {
			lhs = expr
			break
		}
		blank, ok := lhs.(*ast.Ident)
		if !ok || blank.Name != "_" {
			return
		}
		var rhs ast.Expr
		for _, expr := range stmt.Rhs {
			rhs = expr
			break
		}
		call, callOK = astutil.UnwrapParenExpr(rhs).(*ast.CallExpr)
	case *ast.ExprStmt:
		call, callOK = astutil.UnwrapParenExpr(stmt.X).(*ast.CallExpr)
	}
	if !callOK {
		return
	}

	pkgPath, funcName, ok := astutil.PackageCall(pass, call)
	if !ok || pkgPath != "os" || (funcName != "Remove" && funcName != "RemoveAll") {
		return
	}

	position := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(position, nolintIndex, "osremoveuncheckedcleanup") {
		return
	}

	pass.ReportRangef(call, "error return from %s.%s is discarded; cleanup failures may be silently ignored", path.Base(pkgPath), funcName)
}
