// Package logfatallibrary implements a Go analysis linter that flags
// log.Fatal, log.Fatalf, and log.Fatalln calls in library (pkg/) packages.
// These functions call os.Exit(1) internally, which bypasses deferred cleanup
// and makes the package untestable in isolation.
package logfatallibrary

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

var pkgLog = logger.New("linters:logfatallibrary")

// fatalFuncs is the set of log functions that call os.Exit(1) internally.
var fatalFuncs = map[string]bool{
	"Fatal":   true,
	"Fatalf":  true,
	"Fatalln": true,
}

// Analyzer is the log-fatal-in-library analysis pass.
var Analyzer = analyzerutil.New("logfatallibrary", "reports log.Fatal, log.Fatalf, and log.Fatalln calls inside library packages where they implicitly call os.Exit and bypass deferred cleanup", run)

func run(pass *analysis.Pass) (any, error) {
	pkgPath := pass.Pkg.Path()
	// Skip packages under cmd/ entry-points — they are allowed to call log.Fatal.
	if pass.Pkg.Name() == "main" || strings.HasSuffix(pkgPath, "/main") || strings.Contains(pkgPath, "/cmd/") {
		pkgLog.Printf("skipping cmd/main package %s", pkgPath)
		return nil, nil
	}
	pkgLog.Printf("analyzing package %s", pkgPath)

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return
		}
		if strings.HasSuffix(pkgPath, ".test") || filecheck.ShouldSkipFilename(pass.Fset.Position(call.Pos()).Filename, generatedFiles) {
			return
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return
		}
		if !fatalFuncs[sel.Sel.Name] {
			return
		}
		if !astutil.IsPkgSelector(pass, sel, "log") {
			return
		}
		position := pass.Fset.PositionFor(call.Pos(), false)
		if nolint.HasDirectiveForLinter(position, noLintIndex, "logfatallibrary") {
			return
		}
		pkgLog.Printf("flagging log.%s call at %s", sel.Sel.Name, position)
		pass.ReportRangef(call, "log.%s called in library package %s; use error returns instead to avoid implicit os.Exit", sel.Sel.Name, pkgPath)
	})
}
