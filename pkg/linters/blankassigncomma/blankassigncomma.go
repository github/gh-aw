// Package blankassigncomma implements a Go analysis linter that flags
// assignment statements where every result is discarded via blank
// identifiers (e.g., _, _ = f()), which is often a code smell indicating
// the function should be called without assignment, or a result should be
// checked instead of dropped.
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
var Analyzer = analyzerutil.New("blankassigncomma", "reports assignments where every result is discarded via blank identifiers, which may indicate unintended result ignoring", run)

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
// discards every result via blank identifiers (e.g., _, _ = f()). Assignments
// that keep at least one non-blank result (e.g., _, _, err := f()) are left
// alone since they retain and check a value that cannot be dropped.
func checkBlankAssignComma(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	assign, ok := n.(*ast.AssignStmt)
	if !ok {
		return
	}

	// Only check when we have at least 2 results.
	if len(assign.Lhs) < 2 {
		return
	}

	// Every entry on the left-hand side must be blank; a single non-blank
	// identifier anywhere (leading, middle, or trailing) means the result is
	// intentionally kept and the assignment is not a candidate for removal.
	for _, lhs := range assign.Lhs {
		ident, ok := lhs.(*ast.Ident)
		if !ok || ident.Name != "_" {
			return
		}
	}

	blankCount := len(assign.Lhs)

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
			"assignment with %d blank identifiers discards every result; consider removing the assignment or checking the results instead",
			blankCount,
		),
	})
}
