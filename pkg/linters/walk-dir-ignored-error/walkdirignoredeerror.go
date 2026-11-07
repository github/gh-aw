// Package walkdirignoredeerror implements a Go analysis linter that flags
// filepath.Walk and filepath.WalkDir calls where the error return is discarded.
package walkdirignoredeerror

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the walk-dir-ignored-error analysis pass.
var Analyzer = analyzerutil.New("walkdirignoredeerror", "reports filepath.Walk or filepath.WalkDir calls where the error return is discarded", run)

// checkedFuncs maps package import path to the set of function names within
// that package whose discarded error return should be flagged.
var checkedFuncs = map[string]map[string]struct{}{
	"path/filepath": {"Walk": {}, "WalkDir": {}},
}

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.AssignStmt)(nil), (*ast.ExprStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeWalkDirStmt(pass, n, generatedFiles, nolintIndex)
	})
}

// analyzeWalkDirStmt checks whether a statement discards the error return from
// filepath.Walk or filepath.WalkDir and reports a diagnostic if so.
func analyzeWalkDirStmt(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, nolintIndex nolint.DirectiveIndex) {
	switch stmt := n.(type) {
	case *ast.AssignStmt:
		analyzeAssignStmt(pass, stmt, generatedFiles, nolintIndex)
	case *ast.ExprStmt:
		analyzeExprStmt(pass, stmt, generatedFiles, nolintIndex)
	}
}

// analyzeAssignStmt checks for patterns like:
// - _ = filepath.Walk(...)  (error assigned to blank identifier)
func analyzeAssignStmt(pass *analysis.Pass, assign *ast.AssignStmt, generatedFiles filecheck.GeneratedIndex, nolintIndex nolint.DirectiveIndex) {
	// Pattern: _ = filepath.Walk(...) or _ = filepath.WalkDir(...)
	if len(assign.Lhs) == 1 && len(assign.Rhs) == 1 {
		for _, lhs := range assign.Lhs {
			blank, ok := lhs.(*ast.Ident)
			if !ok || blank.Name != "_" {
				return
			}
		}
		for _, rhs := range assign.Rhs {
			call, ok := astutil.UnwrapParenExpr(rhs).(*ast.CallExpr)
			if !ok {
				return
			}
			if funcName, ok := walkDirCallName(pass, call); ok {
				reportIfNotSkipped(pass, call, funcName, generatedFiles, nolintIndex)
			}
		}
		return
	}
}

// analyzeExprStmt checks for patterns like:
// filepath.Walk(...) or filepath.WalkDir(...) (bare call with error ignored)
func analyzeExprStmt(pass *analysis.Pass, exprStmt *ast.ExprStmt, generatedFiles filecheck.GeneratedIndex, nolintIndex nolint.DirectiveIndex) {
	call, ok := astutil.UnwrapParenExpr(exprStmt.X).(*ast.CallExpr)
	if !ok {
		return
	}

	if funcName, ok := walkDirCallName(pass, call); ok {
		reportIfNotSkipped(pass, call, funcName, generatedFiles, nolintIndex)
	}
}

// walkDirCallName returns the resolved function name ("Walk" or "WalkDir")
// when call is filepath.Walk or filepath.WalkDir. The name comes from type
// information, so parenthesized callees such as (filepath.Walk)(...) and
// dot-imported calls such as Walk(...) are reported with the right name.
func walkDirCallName(pass *analysis.Pass, call *ast.CallExpr) (string, bool) {
	pkgPath, funcName, ok := astutil.PackageCall(pass, call)
	if !ok {
		return "", false
	}

	funcs, ok := checkedFuncs[pkgPath]
	if !ok {
		return "", false
	}

	_, checked := funcs[funcName]
	return funcName, checked
}

// reportIfNotSkipped reports a diagnostic if the linter is not suppressed.
func reportIfNotSkipped(pass *analysis.Pass, call *ast.CallExpr, funcName string, generatedFiles filecheck.GeneratedIndex, nolintIndex nolint.DirectiveIndex) {
	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, nolintIndex, "walkdirignoredeerror") {
		return
	}

	pass.ReportRangef(call, "error return from filepath.%s is discarded; directory traversal errors are silently ignored", funcName)
}
