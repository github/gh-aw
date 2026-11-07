// Package sprintfint is the test fixture for the sprintfint analyzer.
package sprintfint

import (
	"fmt"
	"strconv"
)

type counter int

// bad demonstrates the flagged pattern: fmt.Sprintf with a single "%d" verb
// and an int argument, which should be replaced by strconv.Itoa.
func bad(n int) string {
	return fmt.Sprintf("%d", n) // want `use strconv\.Itoa\(x\) instead of fmt\.Sprintf\("%d", x\)`
}

// badLiteral demonstrates the flagged pattern with an integer literal.
func badLiteral() string {
	return fmt.Sprintf("%d", 42) // want `use strconv\.Itoa\(x\) instead of fmt\.Sprintf\("%d", x\)`
}

// badParenthesized demonstrates the flagged pattern with a parenthesized
// callee and format literal.
func badParenthesized(n int) string {
	return (fmt.Sprintf)(("%d"), n) // want `use strconv\.Itoa\(x\) instead of fmt\.Sprintf\("%d", x\)`
}

// goodStrconvItoa is already using the preferred form — no diagnostic expected.
func goodStrconvItoa(n int) string {
	return strconv.Itoa(n)
}

// goodInt64 uses int64 rather than int — not flagged; use strconv.FormatInt.
func goodInt64(n int64) string {
	return fmt.Sprintf("%d", n)
}

// goodUint uses uint — not flagged.
func goodUint(n uint) string {
	return fmt.Sprintf("%d", n)
}

// goodMultipleVerbs uses more than one verb — not flagged.
func goodMultipleVerbs(a, b int) string {
	return fmt.Sprintf("%d %d", a, b)
}

// goodOtherVerb uses a different verb — not flagged.
func goodOtherVerb(n int) string {
	return fmt.Sprintf("%v", n)
}

// goodNamedInt uses a named int type — not flagged because strconv.Itoa
// requires the exact predeclared int type.
func goodNamedInt(n counter) string {
	return fmt.Sprintf("%d", n)
}

// suppressed suppresses the linter directive — no diagnostic expected.
func suppressed(n int) string {
	return fmt.Sprintf("%d", n) //nolint:sprintfint
}
