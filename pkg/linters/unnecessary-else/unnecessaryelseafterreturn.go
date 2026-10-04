// Package unnecessaryelseafterreturn implements a Go analysis linter that
// flags else blocks following if statements that always return, which are
// unnecessary since the return statement exits the function.
package unnecessaryelseafterreturn

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:unnecessaryelseafterreturn")

// Analyzer is the unnecessary-else-after-return analysis pass.
var Analyzer = analyzerutil.New("unnecessaryelseafterreturn", "reports else blocks that follow if statements guaranteed to return; the else is unnecessary since the return exits the function", run)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.IfStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		checkUnnecessaryElse(pass, n, generatedFiles, noLintIndex)
	})
}

// checkUnnecessaryElse reports if statements with else blocks where the if
// body is guaranteed to return, making the else unnecessary.
func checkUnnecessaryElse(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	ifStmt, ok := n.(*ast.IfStmt)
	if !ok {
		return
	}

	// Only check if statements that have an else clause
	if ifStmt.Else == nil {
		return
	}

	pos := pass.Fset.PositionFor(ifStmt.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}

	if nolint.HasDirectiveForLinter(pos, noLintIndex, "unnecessaryelseafterreturn") {
		return
	}

	// Check if the if body always returns
	if !alwaysReturns(ifStmt.Body) {
		return
	}

	pkgLog.Printf("flagging unnecessary else after return at %s", pos)
	pass.ReportRangef(ifStmt.Else,
		"else block is unnecessary; remove it since the preceding if always returns",
	)
}

// alwaysReturns reports whether a block statement is guaranteed to return.
// It checks if the last statement in the block is a return, or if all paths
// through the block return (e.g., in switch or if statements).
func alwaysReturns(block *ast.BlockStmt) bool {
	if block == nil || len(block.List) == 0 {
		return false
	}

	// Check the last statement in the block
	lastStmt := block.List[len(block.List)-1]

	switch stmt := lastStmt.(type) {
	case *ast.ReturnStmt:
		return true

	case *ast.IfStmt:
		// If the last statement is an if, all branches must return
		if !alwaysReturns(stmt.Body) {
			return false
		}
		// Check the else clause
		if stmt.Else == nil {
			return false
		}
		switch elseStmt := stmt.Else.(type) {
		case *ast.BlockStmt:
			return alwaysReturns(elseStmt)
		case *ast.IfStmt:
			// Else if case: recursively check
			return alwaysReturns(&ast.BlockStmt{
				List: []ast.Stmt{elseStmt},
			})
		default:
			return false
		}

	case *ast.SwitchStmt:
		// A switch always returns if all cases return
		if stmt.Body == nil {
			return false
		}
		hasDefault := false
		for _, switchCase := range stmt.Body.List {
			caseStmt, ok := switchCase.(*ast.CaseClause)
			if !ok {
				continue
			}
			// Check if this is a default case
			if len(caseStmt.List) == 0 {
				hasDefault = true
			}
			if len(caseStmt.Body) == 0 {
				return false
			}
			if !alwaysReturns(&ast.BlockStmt{List: caseStmt.Body}) {
				return false
			}
		}
		return hasDefault

	case *ast.TypeSwitchStmt:
		// A type switch always returns if all cases return
		if stmt.Body == nil {
			return false
		}
		hasDefault := false
		for _, switchCase := range stmt.Body.List {
			caseStmt, ok := switchCase.(*ast.CaseClause)
			if !ok {
				continue
			}
			// Check if this is a default case
			if len(caseStmt.List) == 0 {
				hasDefault = true
			}
			if len(caseStmt.Body) == 0 {
				return false
			}
			if !alwaysReturns(&ast.BlockStmt{List: caseStmt.Body}) {
				return false
			}
		}
		return hasDefault

	default:
		return false
	}
}
