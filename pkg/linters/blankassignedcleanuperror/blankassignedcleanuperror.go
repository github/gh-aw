// Package blankassignedcleanuperror implements a Go analysis linter that flags
// cleanup errors assigned to the blank identifier.
package blankassignedcleanuperror

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the blank-assigned-cleanup-error analysis pass.
var Analyzer = analyzerutil.New("blankassignedcleanuperror", "reports discarded cleanup errors assigned to the blank identifier", run)

type sourceRange struct {
	start token.Pos
	end   token.Pos
}

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	deferredClosures := deferredCleanupClosures(pass.Files)
	nodeFilter := []ast.Node{(*ast.AssignStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return
		}
		var lhs, rhs ast.Expr
		for _, expr := range assign.Lhs {
			lhs = expr
		}
		for _, expr := range assign.Rhs {
			rhs = expr
		}
		blank, ok := astutil.UnwrapParenExpr(lhs).(*ast.Ident)
		if !ok || blank.Name != "_" {
			return
		}
		for _, deferred := range deferredClosures {
			if assign.Pos() >= deferred.start && assign.End() <= deferred.end {
				return
			}
		}

		call, ok := astutil.UnwrapParenExpr(rhs).(*ast.CallExpr)
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

		pass.ReportRangef(call, "cleanup error from os.%s is discarded by assignment to _", funcName)
	})
}

func deferredCleanupClosures(files []*ast.File) []sourceRange {
	var ranges []sourceRange
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			deferStmt, ok := n.(*ast.DeferStmt)
			if !ok {
				return true
			}
			call, ok := deferStmt.Call.Fun.(*ast.FuncLit)
			if !ok {
				call, ok = astutil.UnwrapParenExpr(deferStmt.Call.Fun).(*ast.FuncLit)
			}
			if ok {
				ranges = append(ranges, sourceRange{start: call.Body.Pos(), end: call.Body.End()})
			}
			return true
		})
	}
	return ranges
}

func isCleanupFunction(name string) bool {
	switch name {
	case "RemoveAll", "Remove", "WriteFile", "MkdirAll":
		return true
	default:
		return false
	}
}
