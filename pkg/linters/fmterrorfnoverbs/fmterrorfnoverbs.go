// Package fmterrorfnoverbs implements a Go analysis linter that flags calls to
// fmt.Errorf where the format string contains no format verbs, in which case
// errors.New is the idiomatic and cheaper alternative.
package fmterrorfnoverbs

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

// Analyzer is the fmterrorfnoverbs analysis pass.
var Analyzer = analyzerutil.New("fmterrorfnoverbs", "reports fmt.Errorf calls whose format string contains no verbs, preferring errors.New", run)

func run(pass *analysis.Pass) (any, error) {
	nolintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
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

		if !astutil.IsFmtErrorf(pass, call) {
			return
		}

		if len(call.Args) == 0 {
			return
		}

		formatStr, ok := astutil.ResolveFormatString(call.Args[0])
		if !ok {
			return
		}

		if !hasRealFormatVerb(formatStr) {
			position := pass.Fset.PositionFor(call.Pos(), false)
			if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
				return
			}
			if nolint.HasDirectiveForLinter(position, nolintIndex, "fmterrorfnoverbs") {
				return
			}
			if _, isPlainLit := call.Args[0].(*ast.BasicLit); isPlainLit {
				pass.ReportRangef(call, "fmt.Errorf called with no format verbs; use errors.New(%q) instead", formatStr)
				return
			}
			// The format string is built from concatenated pieces (e.g. a
			// caller-supplied prefix plus literal text), so formatStr doesn't
			// correspond to a single source literal; suggest errors.New
			// generically instead of proposing a synthetic replacement.
			pass.ReportRangef(call, "fmt.Errorf called with no format verbs; use errors.New instead")
		}
	})
}

// hasRealFormatVerb reports whether val (the raw content between the surrounding
// quotes of a Go string literal) contains at least one format verb that is not
// an escaped percent pair (%%). The sequence %% renders as a literal % at
// runtime and does not consume an argument, so it is not a real verb.
func hasRealFormatVerb(val string) bool {
	for i := 0; i < len(val); i++ {
		if val[i] != '%' {
			continue
		}
		i++
		if i >= len(val) {
			// Trailing lone % is a malformed verb directive; treat it as present
			// rather than suggesting errors.New for a broken format string.
			return true
		}
		if val[i] == '%' {
			// %% is an escaped percent, not a verb; skip the second % and continue.
			continue
		}
		return true
	}
	return false
}
