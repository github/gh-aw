// Package sprintfbool is the test fixture for the sprintfbool analyzer.
package sprintfbool

import (
	"fmt"
	"strconv"
)

type myBool bool

// badSprintfBool demonstrates the flagged pattern: fmt.Sprintf with a single "%t" verb
// and a bool argument, which should be replaced by strconv.FormatBool.
func badSprintfBool(b bool) string {
	return fmt.Sprintf("%t", b) // want `use strconv\.FormatBool\(b\) instead of fmt\.Sprintf\("%t", b\)`
}

// badLiteral demonstrates the flagged pattern with a bool literal.
func badLiteral() string {
	return fmt.Sprintf("%t", true) // want `use strconv\.FormatBool\(true\) instead of fmt\.Sprintf\("%t", true\)`
}

// goodStrconvFormatBool is already using the preferred form — no diagnostic expected.
func goodStrconvFormatBool(b bool) string {
	return strconv.FormatBool(b)
}

// goodNamedBool uses a named bool type — not flagged because strconv.FormatBool
// requires the exact predeclared bool type.
func goodNamedBool(b myBool) string {
	return fmt.Sprintf("%t", b)
}

// goodMultipleVerbs uses more than one verb — not flagged.
func goodMultipleVerbs(a, b bool) string {
	return fmt.Sprintf("%t %t", a, b)
}

// goodOtherVerb uses a different verb — not flagged.
func goodOtherVerb(b bool) string {
	return fmt.Sprintf("%v", b)
}

// suppressedSprintfBoolDiagnostic suppresses the linter directive — no diagnostic expected.
func suppressedSprintfBoolDiagnostic(b bool) string {
	return fmt.Sprintf("%t", b) //nolint:sprintfbool
}
