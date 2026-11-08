// Package blankassignedcleanuperror implements a Go analysis linter that flags
// cleanup errors assigned to the blank identifier.
package blankassignedcleanuperror

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the blank-assigned-cleanup-error analysis pass.
var Analyzer = analyzerutil.New("blankassignedcleanuperror", "reports discarded cleanup errors assigned to the blank identifier, including in deferred closures", run)

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.AssignStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return
		}
		blank, ok := astutil.UnwrapParenExpr(assign.Lhs[0]).(*ast.Ident) //nolint:uncheckedsliceindex // lengths are checked above
		if !ok || blank.Name != "_" {
			return
		}

		call, ok := astutil.UnwrapParenExpr(assign.Rhs[0]).(*ast.CallExpr) //nolint:uncheckedsliceindex // lengths are checked above
		if !ok {
			return
		}
		pkgPath, funcName, ok := astutil.PackageCall(pass, call)
		if !ok || pkgPath != "os" || !isCleanupFunction(funcName) {
			return
		}

		position := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(position, nolintIndex, "blankassignedcleanuperror") {
			return
		}
		if (funcName == "Remove" || funcName == "RemoveAll") &&
			nolint.HasDirectiveForLinter(position, nolintIndex, "osremoveuncheckedcleanup") {
			return
		}

		pass.ReportRangef(call, "cleanup error from os.%s is discarded by assignment to _", funcName)
	})
}

func isCleanupFunction(name string) bool {
	switch name {
	case "RemoveAll", "Remove", "WriteFile", "MkdirAll":
		return true
	default:
		return false
	}
}
