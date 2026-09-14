// Package blankassigncomma implements a Go analysis linter that flags
// assignment statements with multiple consecutive blank identifiers,
// which is often a code smell indicating the result should be checked,
// or the function should be called without assignment.
package blankassigncomma

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the blank-assign-comma analysis pass.
var Analyzer = analyzerutil.New("blankassigncomma", "reports assignments with multiple consecutive blank identifiers that may indicate unintended result ignoring", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.AssignStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		checkBlankAssignComma(pass, n, generatedFiles, noLintIndex)
	})
}

// checkBlankAssignComma reports a diagnostic when an assignment statement
// has multiple consecutive blank identifiers (e.g., _, _ = f()).
func checkBlankAssignComma(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	assign, ok := n.(*ast.AssignStmt)
	if !ok {
		return
	}

	// Only check when we have at least 2 blanks
	if len(assign.Lhs) < 2 {
		return
	}

	// Count consecutive blanks from the left
	consecutiveBlanks := 0
	for _, lhs := range assign.Lhs {
		if ident, ok := lhs.(*ast.Ident); ok && ident.Name == "_" {
			consecutiveBlanks++
		} else {
			break
		}
	}

	// Flag if we have 2 or more consecutive blanks
	if consecutiveBlanks < 2 {
		return
	}

	position := pass.Fset.PositionFor(assign.Pos(), false)
	if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
		return
	}

	if nolint.HasDirectiveForLinter(position, noLintIndex, "blankassigncomma") {
		return
	}

	pass.Report(analysis.Diagnostic{
		Pos: assign.Pos(),
		End: assign.End(),
		Message: fmt.Sprintf(
			"assignment with %d consecutive blank identifiers; consider removing the assignment or checking the results instead",
			consecutiveBlanks,
		),
	})
}
