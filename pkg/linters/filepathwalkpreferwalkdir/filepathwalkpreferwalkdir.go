// Package filepathwalkpreferwalkdir reports filepath.Walk calls.
package filepathwalkpreferwalkdir

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

const linterName = "filepathwalkpreferwalkdir"

// Analyzer reports filepath.Walk calls and recommends filepath.WalkDir.
var Analyzer = analyzerutil.New(linterName, "reports filepath.Walk calls and recommends filepath.WalkDir", run)

func run(pass *analysis.Pass) (any, error) {
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	return analyzerutil.Preorder(pass, []ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		pkgPath, funcName, ok := astutil.PackageCall(pass, call)
		if !ok || pkgPath != "path/filepath" || funcName != "Walk" {
			return
		}

		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) ||
			nolint.HasDirectiveForLinter(pos, noLintIndex, linterName) {
			return
		}
		pass.ReportRangef(call, "filepath.Walk is less efficient than filepath.WalkDir; prefer filepath.WalkDir (its callback receives fs.DirEntry; use d.Info() for metadata)")
	})
}
