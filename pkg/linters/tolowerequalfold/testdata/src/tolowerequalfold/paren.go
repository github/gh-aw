package tolowerequalfold

import "strings"

// Redundant parentheses around the case-conversion call, its callee, or the
// alias identifier must not hide the comparison from the linter.
func parenthesizedExamples(name string) {
	_ = (strings.ToLower(name)) == "alice" // want `use strings\.EqualFold`

	_ = "alice" == (strings.ToLower(name)) // want `use strings\.EqualFold`

	_ = (strings.ToUpper)(name) == "ALICE" // want `use strings\.EqualFold`

	_ = ((strings.ToLower))(name) != "alice" // want `use strings\.EqualFold`

	lower := strings.ToLower(name)

	_ = (lower) == "alice" // want `use strings\.EqualFold`

	_ = "alice" == (lower) // want `use strings\.EqualFold`
}

func parenthesizedOK(name string) {
	// Self-comparison through parentheses is still a plain comparison.
	_ = (strings.ToLower(name)) == name

	// Case-mismatched literal stays excluded.
	_ = (strings.ToLower(name)) == "ALICE"
}
