// Package reflectdeepequalusage implements a Go analysis linter that flags
// reflect.DeepEqual() usage in conditionals or comparisons that should use
// typed equality operators or type-specific comparison functions for performance
// and type safety.
package reflectdeepequalusage

import (
	"fmt"
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:reflectdeepequalusage")

// Analyzer is the reflect-deepequal-usage analysis pass.
var Analyzer = analyzerutil.New(
	"reflectdeepequalusage",
	"reports reflect.DeepEqual() usage in conditionals or comparisons that should use typed equality operators or type-specific comparison functions for performance and type safety",
	run,
)

// URL points to documentation for this linter.
var _ = func() struct{} {
	Analyzer.URL = "https://github.com/github/gh-aw/tree/main/pkg/linters/reflect-deepequal-usage"
	return struct{}{}
}()

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeCall(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeCall checks whether a call is reflect.DeepEqual() and reports a diagnostic if so.
func analyzeCall(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	// Get the file position for skipping generated files
	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "reflectdeepequalusage") {
		return
	}

	// Check if this is a call to reflect.DeepEqual
	pkgPath, funcName, ok := astutil.PackageCall(pass, call)
	if !ok || pkgPath != "reflect" || funcName != "DeepEqual" {
		return
	}

	pkgLog.Printf("flagging reflect.DeepEqual() call at %s:%d", pos.Filename, pos.Line)
	pass.Report(analysis.Diagnostic{
		Pos: call.Pos(),
		End: call.End(),
		Message: fmt.Sprintf(
			"reflect.DeepEqual() is inefficient and should not be used in conditionals; prefer typed equality operators or type-specific comparison functions",
		),
	})
}
