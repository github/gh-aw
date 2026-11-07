// Package sprintferrorsnew is the test fixture for the sprintferrorsnew analyzer.
package sprintferrorsnew

import (
	"errors"
	"fmt"
)

// bad demonstrates the flagged pattern: errors.New wrapping a fmt.Sprintf call.
func bad() error {
	return errors.New(fmt.Sprintf("invalid engine: %s", "claude")) // want `use fmt\.Errorf instead of errors\.New\(fmt\.Sprintf\(\.\.\.\)\)`
}

// badWithMultipleArgs demonstrates the flagged pattern with multiple format args.
func badWithMultipleArgs() error {
	return errors.New(fmt.Sprintf("invalid value %q for flag %s", "x", "-n")) // want `use fmt\.Errorf instead of errors\.New\(fmt\.Sprintf\(\.\.\.\)\)`
}

// badParenthesized demonstrates the flagged pattern with redundant parentheses
// around the errors.New callee, the argument, and the fmt.Sprintf callee.
func badParenthesized() []error {
	return []error{
		(errors.New)(fmt.Sprintf("invalid engine: %s", "claude")), // want `use fmt\.Errorf instead of errors\.New\(fmt\.Sprintf\(\.\.\.\)\)`
		errors.New((fmt.Sprintf("invalid engine: %s", "claude"))), // want `use fmt\.Errorf instead of errors\.New\(fmt\.Sprintf\(\.\.\.\)\)`
		errors.New((fmt.Sprintf)("invalid engine: %s", "claude")), // want `use fmt\.Errorf instead of errors\.New\(fmt\.Sprintf\(\.\.\.\)\)`
	}
}

// goodErrorf uses fmt.Errorf directly — no diagnostic expected.
func goodErrorf() error {
	return fmt.Errorf("invalid engine: %s", "claude")
}

// goodVariable uses a pre-built string variable — not flagged by this linter.
func goodVariable() error {
	msg := fmt.Sprintf("invalid engine: %s", "claude")
	return errors.New(msg)
}

// goodPlainString uses a string literal — no diagnostic expected.
func goodPlainString() error {
	return errors.New("something went wrong")
}

// suppressed keeps the shape intentionally and suppresses this style diagnostic.
func suppressed() error {
	return errors.New(fmt.Sprintf("invalid engine: %s", "claude")) //nolint:sprintferrorsnew
}
