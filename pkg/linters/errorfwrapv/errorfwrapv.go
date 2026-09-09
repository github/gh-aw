// Package errorfwrapv implements a Go analysis linter that flags calls to
// fmt.Errorf that either format error arguments with %v or otherwise pass
// error arguments without any %w, which breaks error-chain inspection via
// errors.Is and errors.As.
package errorfwrapv

import (
	"errors"
	"go/ast"
	"go/types"
	"strconv"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
)

var errorIface = astutil.UniverseErrorInterface()

type formatVerb struct {
	argIdx int
	verb   rune
}

// formatArgOffset is the index of the first format argument in fmt.Errorf calls.
// call.Args[0] is the format string; real arguments start at index 1.
const formatArgOffset = 1

// Analyzer is the errorfwrapv analysis pass.
var Analyzer = analyzerutil.New("errorfwrapv", "reports fmt.Errorf calls that pass error arguments without %w wrapping", run)

func run(pass *analysis.Pass) (any, error) {
	if errorIface == nil {
		return nil, errors.New("failed to resolve built-in error interface from types.Universe")
	}

	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeFmtErrorfCall(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeFmtErrorfCall checks whether a call expression is a fmt.Errorf that
// misuses error arguments (via %v or without %w) and reports a diagnostic.
func analyzeFmtErrorfCall(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	position := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(position.Filename, generatedFiles) {
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

	verbs := parseFormatVerbs(formatStr)
	errorArgVerbs, wrappedErrorArgs, hasVerbV := classifyErrorArgs(pass, call, verbs)

	if hasVerbV {
		if nolint.HasDirectiveForLinter(position, noLintIndex, "errorfwrapv") {
			return
		}
		pass.ReportRangef(call, "fmt.Errorf formats an error argument with %%v; use %%w to preserve the error chain")
		return
	}

	if len(call.Args) <= formatArgOffset {
		return
	}

	for i := formatArgOffset; i < len(call.Args); i++ {
		tv, ok := pass.TypesInfo.Types[call.Args[i]]
		if !ok || tv.Type == nil {
			continue
		}
		if !types.Implements(tv.Type, errorIface) {
			continue
		}
		if wrappedErrorArgs[i] {
			continue
		}
		if verbsForArg, ok := errorArgVerbs[i]; ok {
			if !needsWrapping(verbsForArg) {
				continue
			}
		}
		if nolint.HasDirectiveForLinter(position, noLintIndex, "errorfwrapv") {
			return
		}
		pass.ReportRangef(call, "fmt.Errorf passes an error argument without %%w; use %%w to preserve the error chain")
		// Keep diagnostics to one per call to avoid noisy duplicate reports.
		return
	}
}

// classifyErrorArgs iterates over format verbs and classifies error arguments.
// It returns errorArgVerbs (verb list per arg index), wrappedErrorArgs (args
// that already use %w), and hasVerbV (whether any error arg uses %v).
func classifyErrorArgs(pass *analysis.Pass, call *ast.CallExpr, verbs []formatVerb) (errorArgVerbs map[int][]rune, wrappedErrorArgs map[int]bool, hasVerbV bool) {
	errorArgVerbs = make(map[int][]rune)
	wrappedErrorArgs = make(map[int]bool)
	for _, fv := range verbs {
		callArgIdx := fv.argIdx + formatArgOffset
		if callArgIdx >= len(call.Args) {
			continue
		}
		tv, ok := pass.TypesInfo.Types[call.Args[callArgIdx]]
		if !ok || tv.Type == nil {
			continue
		}
		if !types.Implements(tv.Type, errorIface) {
			continue
		}
		errorArgVerbs[callArgIdx] = append(errorArgVerbs[callArgIdx], fv.verb)
		if fv.verb == 'w' {
			wrappedErrorArgs[callArgIdx] = true
		}
		if fv.verb == 'v' {
			hasVerbV = true
		}
	}
	return
}

// needsWrapping reports whether a slice of format verbs for an error argument
// requires a %w replacement. Verbs %T and %p are considered display-only and
// do not require wrapping.
func needsWrapping(verbs []rune) bool {
	for _, verb := range verbs {
		if verb != 'T' && verb != 'p' {
			return true
		}
	}
	return false
}

// parseFormatVerbs scans a format string produced by astutil.ResolveFormatString
// — the unquoted literal segments of a fmt.Errorf format-string argument,
// concatenated in order — for format verbs.
func parseFormatVerbs(s string) []formatVerb {
	var verbs []formatVerb

	nextArgIdx := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		if s[i] == '%' {
			continue
		}

		valueArgIdx := 0
		hasExplicitValueArg := false
		for i < len(s) {
			switch s[i] {
			case '-', '+', '#', '0', ' ':
				i++
			default:
				goto width
			}
		}

	width:
		i = consumeFormatWidthOrPrecision(s, i, &nextArgIdx)
		if i < len(s) && s[i] == '.' {
			i++
			i = consumeFormatWidthOrPrecision(s, i, &nextArgIdx)
		}
		if idx, nextPos, ok := parseFormatArgIndex(s, i); ok {
			valueArgIdx = idx
			nextArgIdx = idx + 1
			hasExplicitValueArg = true
			i = nextPos
		}
		if i >= len(s) {
			break
		}
		if !hasExplicitValueArg {
			valueArgIdx = nextArgIdx
			nextArgIdx++
		}
		verbs = append(verbs, formatVerb{argIdx: valueArgIdx, verb: rune(s[i])})
	}

	return verbs
}

func consumeFormatWidthOrPrecision(s string, i int, nextArgIdx *int) int {
	if idx, nextPos, ok := parseFormatArgIndex(s, i); ok && nextPos < len(s) && s[nextPos] == '*' {
		*nextArgIdx = idx + 1
		return nextPos + 1
	}
	if i < len(s) && s[i] == '*' {
		*nextArgIdx = *nextArgIdx + 1
		return i + 1
	}
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}

func parseFormatArgIndex(s string, i int) (int, int, bool) {
	if i >= len(s) || s[i] != '[' {
		return 0, i, false
	}

	j := i + 1
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i+1 || j >= len(s) || s[j] != ']' {
		return 0, i, false
	}

	n, err := strconv.Atoi(s[i+1 : j])
	if err != nil || n <= 0 {
		return 0, i, false
	}
	return n - 1, j + 1, true
}
