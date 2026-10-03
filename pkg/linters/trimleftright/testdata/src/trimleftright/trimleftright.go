// Package trimleftright is the test fixture for the trimleftright analyzer.
package trimleftright

import "strings"

// bad: TrimLeft with multi-char literal — likely meant TrimPrefix
func badTrimLeft(s string) string {
	return strings.TrimLeft(s, "foo") // want `strings\.TrimLeft with a multi-character cutset`
}

func badParenthesizedCutset(s string) string {
	return strings.TrimLeft(s, ("foo")) // want `strings\.TrimLeft with a multi-character cutset`
}

// bad: TrimRight with repeated alphanumeric cutset — likely meant TrimSuffix
func badTrimRight(s string) string {
	return strings.TrimRight(s, "barr") // want `strings\.TrimRight with a multi-character cutset`
}

// bad: distinct-char prefix — no repeated rune but still a prefix bug
func badHTTP(s string) string {
	return strings.TrimLeft(s, "http") // want `strings\.TrimLeft with a multi-character cutset`
}

// bad: distinct-char prefix with no repeated rune
func badABC(s string) string {
	return strings.TrimLeft(s, "abc") // want `strings\.TrimLeft with a multi-character cutset`
}

// bad: distinct-char suffix
func badTXT(s string) string {
	return strings.TrimRight(s, "txt") // want `strings\.TrimRight with a multi-character cutset`
}

// bad: mixed letter+digit prefix
func badV1(s string) string {
	return strings.TrimLeft(s, "v1") // want `strings\.TrimLeft with a multi-character cutset`
}

// good: TrimLeft with single character — valid cutset usage
func goodSingleChar(s string) string {
	return strings.TrimLeft(s, " ")
}

// good: TrimRight with empty string — valid (noop)
func goodEmpty(s string) string {
	return strings.TrimRight(s, "")
}

// good: TrimPrefix — correct function for prefix removal
func goodTrimPrefix(s string) string {
	return strings.TrimPrefix(s, "foo")
}

// good: TrimSuffix — correct function for suffix removal
func goodTrimSuffix(s string) string {
	return strings.TrimSuffix(s, "bar")
}

// good: TrimLeft with single Unicode rune
func goodUnicodeRune(s string) string {
	return strings.TrimLeft(s, "→")
}

// good: intentional vowel-class cutset
func goodVowels(s string) string {
	return strings.TrimLeft(s, "aeiou")
}

// good: intentional decimal-digit class
func goodDigits(s string) string {
	return strings.TrimLeft(s, "0123456789")
}

// good: complete hex-letter alphabet — intentional hex-class trimming
func goodHexLetters(s string) string {
	return strings.TrimLeft(s, "abcdef")
}

// good: full lowercase hex alphabet including digits
func goodFullHex(s string) string {
	return strings.TrimLeft(s, "0123456789abcdef")
}

// suppressed: nolint directive suppresses the diagnostic
func suppressed(s string) string {
	return strings.TrimLeft(s, "foo") //nolint:trimleftright
}
