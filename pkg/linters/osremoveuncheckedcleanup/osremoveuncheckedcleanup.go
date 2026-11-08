// Package osremoveuncheckedcleanup implements a Go analysis linter that flags
// os.Remove and os.RemoveAll calls whose errors are discarded.
package osremoveuncheckedcleanup

import (
	"go/ast"

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

	nodeFilter := []ast.Node{(*ast.AssignStmt)(nil), (*ast.ExprStmt)(nil), (*ast.GoStmt)(nil)}
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
		blank, ok := stmt.Lhs[0].(*ast.Ident) //nolint:uncheckedsliceindex // len(stmt.Lhs) == 1 was checked above.
		if !ok || blank.Name != "_" {
			return
		}
		call, callOK = astutil.UnwrapParenExpr(stmt.Rhs[0]).(*ast.CallExpr) //nolint:uncheckedsliceindex // len(stmt.Rhs) == 1 was checked above.
	case *ast.ExprStmt:
		call, callOK = astutil.UnwrapParenExpr(stmt.X).(*ast.CallExpr)
	case *ast.GoStmt:
		call, callOK = astutil.UnwrapParenExpr(stmt.Call).(*ast.CallExpr)
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

	pass.ReportRangef(call, "error return from os.%s is discarded; cleanup failures may be silently ignored", funcName)
}
