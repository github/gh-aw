// Package strconvparseignorederror implements a Go analysis linter that flags
// strconv parsing calls (Atoi, ParseInt, ParseFloat, ParseBool, ParseUint)
// where the error return is discarded with _.
package strconvparseignorederror

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the strconv-parse-ignored-error analysis pass.
var Analyzer = analyzerutil.New("strconvparseignorederror", "reports strconv parsing calls where the error return is discarded with _", run)

// strconvParseFuncs is the set of strconv functions to check.
var strconvParseFuncs = map[string]bool{
	"Atoi":       true,
	"ParseInt":   true,
	"ParseFloat": true,
	"ParseBool":  true,
	"ParseUint":  true,
}

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.AssignStmt)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeStrconvAssign(pass, n, generatedFiles, nolintIndex)
	})
}

// analyzeStrconvAssign checks whether an assignment discards the error return
// from a strconv parsing function and reports a diagnostic if so.
func analyzeStrconvAssign(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, nolintIndex nolint.DirectiveIndex) {
	assign, ok := n.(*ast.AssignStmt)
	if !ok {
		return
	}
	call, pkgPath, funcName, ok := astutil.MatchDiscardedErrorCall(pass, assign)
	if !ok {
		return
	}
	if pkgPath != "strconv" || !strconvParseFuncs[funcName] {
		return
	}
	position := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(position, nolintIndex, "strconvparseignorederror") {
		return
	}
	pass.ReportRangef(call, "error return from strconv.%s is discarded; parse failures produce zero values silently", funcName)
}
