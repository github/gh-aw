// Package trimleftright implements a Go analysis linter that flags calls to
// strings.TrimLeft or strings.TrimRight with a multi-character string literal
// cutset, where strings.TrimPrefix or strings.TrimSuffix is almost certainly
// the intended function.
//
// strings.TrimLeft(s, "foo") does NOT remove the prefix "foo"; it removes any
// leading rune that appears anywhere in the cutset characters 'f', 'o'.
// This is a well-known Go gotcha.
package trimleftright

import (
	"go/ast"
	"unicode"

	"golang.org/x/tools/go/analysis"

	"github.com/github/gh-aw/pkg/linters/internal/analyzerutil"
	"github.com/github/gh-aw/pkg/linters/internal/astutil"
	"github.com/github/gh-aw/pkg/linters/internal/filecheck"
	"github.com/github/gh-aw/pkg/linters/internal/nolint"
	"github.com/github/gh-aw/pkg/logger"
)

var pkgLog = logger.New("linters:trimleftright")

// Analyzer is the trimleftright analysis pass.
var Analyzer = analyzerutil.New("trimleftright", "reports likely mistaken strings.TrimLeft/TrimRight calls using multi-character alphanumeric literal cutsets", run)

func run(pass *analysis.Pass) (any, error) {
	pkgLog.Printf("analyzing package %s", pass.Pkg.Path())
	noLintIndex, generatedFiles, err := analyzerutil.Indexes(pass)
	if err != nil {
		return nil, err
	}

	nodeFilter := []ast.Node{(*ast.CallExpr)(nil)}
	return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
		analyzeTrimLeftRight(pass, n, generatedFiles, noLintIndex)
	})
}

// analyzeTrimLeftRight checks whether a call is a strings.TrimLeft or
// strings.TrimRight with a suspicious multi-character cutset and reports a
// diagnostic suggesting TrimPrefix or TrimSuffix.
func analyzeTrimLeftRight(pass *analysis.Pass, n ast.Node, generatedFiles filecheck.GeneratedIndex, noLintIndex nolint.DirectiveIndex) {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return
	}

	pos := pass.Fset.PositionFor(call.Pos(), false)
	if filecheck.ShouldSkipFilename(pos.Filename, generatedFiles) {
		return
	}
	if nolint.HasDirectiveForLinter(pos, noLintIndex, "trimleftright") {
		return
	}

	sel, ok := astutil.UnwrapParenExpr(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return
	}
	funcName := sel.Sel.Name
	if funcName != "TrimLeft" && funcName != "TrimRight" {
		return
	}
	if !astutil.IsPkgSelector(pass, sel, "strings") {
		return
	}
	if len(call.Args) != 2 {
		return
	}

	cutset, isCutset := astutil.StringLitValue(call.Args[1])
	if !isCutset || !looksSuspiciousCutset(cutset) {
		return
	}

	var suggested string
	switch funcName {
	case "TrimLeft":
		suggested = "TrimPrefix"
	case "TrimRight":
		suggested = "TrimSuffix"
	}

	pkgLog.Printf("flagging strings.%s with suspicious cutset at %s", funcName, pos)
	pass.Report(analysis.Diagnostic{
		Pos: call.Pos(),
		End: call.End(),
		Message: "strings." + funcName + " with a multi-character cutset treats each character independently; " +
			"use strings." + suggested + " if you intend to remove the exact string",
	})
}

// looksSuspiciousCutset reports likely TrimPrefix/TrimSuffix confusion.
// It returns true for any multi-rune all-alphanumeric cutset that does not
// look like an intentional character-class trimmer.
//
// Recognised character-class exceptions (returned false):
//   - Pure decimal-digit sets (e.g. "0123456789", "012")  — digit trimming.
//   - Pure ASCII-vowel sets (e.g. "aeiou", "aei")         — vowel trimming.
//   - Complete hex-letter alphabet in any case (all six of a–f present,
//     mixed with optional digits, e.g. "abcdef", "ABCDEF",
//     "0123456789abcdef")                                  — hex trimming.
//
// Single-rune cutsets and cutsets containing non-alphanumeric runes are
// returned false because they are either trivially correct or already
// covered by other idioms (whitespace trimming, punctuation sets).
func looksSuspiciousCutset(cutset string) bool {
	runes := []rune(cutset)
	if len(runes) <= 1 {
		return false
	}

	for _, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}

	// Exception: intentional decimal-digit set.
	if isAllASCIIDigits(runes) {
		return false
	}

	// Exception: intentional ASCII-vowel set.
	if isAllASCIIVowels(runes) {
		return false
	}

	// Exception: complete hex-letter alphabet (all six of a–f in any case,
	// optionally accompanied by decimal digits).
	if isCompleteHexAlphabet(runes) {
		return false
	}

	return true
}

// isAllASCIIDigits reports whether every rune is an ASCII decimal digit.
// An empty slice returns true (vacuous truth); callers must apply the length
// guard in looksSuspiciousCutset before invoking this helper.
func isAllASCIIDigits(runes []rune) bool {
	for _, r := range runes {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isAllASCIIVowels reports whether every rune is an ASCII vowel (a e i o u),
// case-insensitive.
// An empty slice returns true (vacuous truth); callers must apply the length
// guard in looksSuspiciousCutset before invoking this helper.
func isAllASCIIVowels(runes []rune) bool {
	for _, r := range runes {
		switch unicode.ToLower(r) {
		case 'a', 'e', 'i', 'o', 'u':
		default:
			return false
		}
	}
	return true
}

// isCompleteHexAlphabet reports whether runes consist solely of ASCII decimal
// digits and/or ASCII hex letters (a–f, A–F), and include all six hex-letter
// code points (a b c d e f, case-insensitively). This matches intentional
// hex-class trimming like "abcdef", "ABCDEF", or "0123456789abcdef".
// Partial hex-letter subsets such as "abc" are not matched and return false.
// Duplicate hex letters (e.g. "aabcdef") are also rejected and return false;
// a repeated hex letter is almost certainly a bug rather than a character class.
func isCompleteHexAlphabet(runes []rune) bool {
	if len(runes) > 16 { // 6 unique hex letters (a–f, case-folded to lowercase) + 10 digits
		return false
	}
	seen := make(map[rune]struct{})
	for _, r := range runes {
		lower := unicode.ToLower(r)
		switch {
		case lower >= 'a' && lower <= 'f':
			if _, ok := seen[lower]; ok {
				return false // repeated hex letter → suspicious
			}
			seen[lower] = struct{}{}
		case r >= '0' && r <= '9':
			// decimal digits are permissible alongside hex letters
		default:
			return false
		}
	}
	return len(seen) == 6
}
