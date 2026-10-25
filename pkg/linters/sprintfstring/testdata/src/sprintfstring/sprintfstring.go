// Package sprintfstring is the test fixture for the sprintfstring analyzer.
package sprintfstring

import (
	"errors"
	"fmt"
)

type name string

type stringer struct{}

func (stringer) String() string { return "s" }

func bad(s string) string {
	return fmt.Sprintf("%s", s) // want `use the string directly instead of fmt\.Sprintf\("%s", x\)`
}

func badExpr(a, b string) string {
	return fmt.Sprintf("%s", a+b) // want `use the string directly`
}

func goodNamed(n name) string {
	return fmt.Sprintf("%s", n)
}

func goodStringer(s stringer) string {
	return fmt.Sprintf("%s", s)
}

func goodError() string {
	return fmt.Sprintf("%s", errors.New("x"))
}

func goodBytes(b []byte) string {
	return fmt.Sprintf("%s", b)
}

func goodExtraText(s string) string {
	return fmt.Sprintf("%s!", s)
}

func suppressed(s string) string {
	return fmt.Sprintf("%s", s) //nolint:sprintfstring
}
