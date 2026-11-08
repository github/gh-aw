// Package filepathwalkpreferwalkdir implements a Go analysis linter that flags
// calls to filepath.Walk and recommends filepath.WalkDir.
package filepathwalkpreferwalkdir

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the filepath-walk-prefer-walkdir analysis pass.
var Analyzer = analyzerutil.NewAtPath(
	"filepathwalkpreferwalkdir",
	"reports filepath.Walk calls because filepath.WalkDir avoids an os.Lstat per entry",
	"filepath-walk-prefer-walkdir",
	run,
)

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	return analyzerutil.Preorder(pass, []ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		pkgPath, name, ok := astutil.PackageCall(pass, call)
		if !ok || pkgPath != "path/filepath" || name != "Walk" {
			return
		}
		pos := pass.Fset.PositionFor(call.Pos(), false)
		if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
			return
		}
		if nolint.HasDirectiveForLinter(pos, nolintIndex, "filepathwalkpreferwalkdir") {
			return
		}
		pass.ReportRangef(call, "filepath.Walk performs an os.Lstat for every entry; use filepath.WalkDir instead")
	})
}
