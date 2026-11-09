package osexitinlibrary

import "os"

// flagged: a parenthesized callee is still os.Exit.
func stopProcessParenthesized() {
	(os.Exit)(1) // want `os.Exit called in library package`
}

// flagged: multiple layers of parentheses.
func stopProcessNestedParens() {
	((os.Exit))(1) // want `os.Exit called in library package`
}

// suppressed: nolint still applies to a parenthesized callee.
func suppressedParenthesized() {
	(os.Exit)(1) //nolint:osexitinlibrary
}
