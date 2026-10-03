// Package rawloginlib implements a Go analysis linter that flags
// standard log package calls in library (pkg/) packages.
package rawloginlib

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:rawloginlib")

var Analyzer = analyzerutil.New("rawloginlib", "reports use of the standard log package in library packages where pkg/logger should be used instead", run)

// rawLogFuncs is the set of standard log functions that should not be called in library code.
var rawLogFuncs = map[string]bool{
	"Print": true, "Printf": true, "Println": true,
	"Panic": true, "Panicf": true, "Panicln": true,
}

func run(pass *analysis.Pass) (any, error) {
	pkgPath := pass.Pkg.Path()
	if pass.Pkg.Name() == "main" || strings.HasSuffix(pkgPath, "/main") || strings.Contains(pkgPath, "/cmd/") {
		pkgLog.Printf("skipping cmd/main package %s", pkgPath)
		return nil, nil
	}
	pkgLog.Printf("analyzing package %s", pkgPath)

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		if filecheck.ShouldSkipFilename(pass.Fset.Position(call.Pos()).Filename, generatedFiles) {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if !astutil.IsPkgSelector(pass, sel, "log") {
			return
		}
		if !rawLogFuncs[sel.Sel.Name] {
			return
		}
		position := pass.Fset.PositionFor(call.Pos(), false)
		if nolint.HasDirectiveForLinter(position, noLintIndex, "rawloginlib") {
			return
		}
		pkgLog.Printf("flagging log.%s call at %s", sel.Sel.Name, position)
		pass.ReportRangef(call, "log.%s called in library package %s; use pkg/logger instead", sel.Sel.Name, pkgPath)
	})
}
