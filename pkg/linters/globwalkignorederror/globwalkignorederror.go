// Package globwalkignorederror implements a Go analysis linter that flags
// filepath.Glob and os.ReadDir calls where the error return is discarded
// with _.
package globwalkignorederror

import (
	"go/ast"
	"path"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the glob-walk-ignored-error analysis pass.
var Analyzer = analyzerutil.New("globwalkignorederror", "reports filepath.Glob and os.ReadDir calls where the error return is discarded with _", run)

// checkedFuncs maps package import path to the set of function names within
// that package whose discarded error return should be flagged.
var checkedFuncs = map[string]map[string]struct{}{
	"path/filepath": {"Glob": {}},
	"os":            {"ReadDir": {}},
}

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.AssignStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeGlobWalkAssign(pass, n, generatedFiles, nolintIndex)
	})
}

// analyzeGlobWalkAssign checks whether an assignment discards the error
// return from filepath.Glob or os.ReadDir and reports a diagnostic if so.
func analyzeGlobWalkAssign(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, nolintIndex nolint.DirectiveIndex) {
	assign, ok := n.(*ast.AssignStmt)
	if !ok {
		return
	}
	call, pkgPath, funcName, ok := astutil.MatchDiscardedErrorCall(pass, assign)
	if !ok {
		return
	}
	funcs, ok := checkedFuncs[pkgPath]
	if !ok {
		return
	}
	if _, checked := funcs[funcName]; !checked {
		return
	}
	position := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(position, nolintIndex, "globwalkignorederror") {
		return
	}
	pass.ReportRangef(call, "error return from %s.%s is discarded; malformed patterns or unreadable directories silently produce an empty result", path.Base(pkgPath), funcName)
}
