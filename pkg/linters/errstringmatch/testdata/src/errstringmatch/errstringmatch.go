package errstringmatch

import (
	"errors"
	"strings"
)

var errNotFound = errors.New("not found")

// flagged: strings.Contains on err.Error() with a string literal
func checkError(err error) bool {
	return strings.Contains(err.Error(), "not found") // want `avoid strings\.Contains\(err\.Error\(\)`
}

func checkParenthesizedLiteral(err error) bool {
	return strings.Contains(err.Error(), ("not found")) // want `avoid strings\.Contains\(err\.Error\(\)`
}

// flagged: same pattern with a different variable name
func checkPermission(e error) bool {
	return strings.Contains(e.Error(), "403") // want `avoid strings\.Contains\(err\.Error\(\)`
}

// not flagged: using errors.Is
func checkErrorSafe(err error) bool {
	return errors.Is(err, errNotFound)
}

// not flagged: strings.Contains on a plain string, not err.Error()
func checkString(s string) bool {
	return strings.Contains(s, "prefix")
}

func checkIgnoredPreviousLine(err error) bool {
	//nolint:errstringmatch // gh CLI behavior is only available as text.
	return strings.Contains(err.Error(), "INSUFFICIENT_SCOPES")
}

func checkIgnoredSameLine(err error) bool {
	return strings.Contains(err.Error(), "already merged") //nolint:errstringmatch // gh CLI merge status is only available as text.
}

// flagged: strings.HasPrefix on err.Error() with a string literal
func checkHasPrefix(err error) bool {
	return strings.HasPrefix(err.Error(), "connection refused") // want `avoid strings\.HasPrefix\(err\.Error\(\)`
}

// flagged: strings.HasSuffix on err.Error() with a string literal
func checkHasSuffix(err error) bool {
	return strings.HasSuffix(err.Error(), "not found") // want `avoid strings\.HasSuffix\(err\.Error\(\)`
}

// flagged: strings.EqualFold on err.Error() with a string literal
func checkEqualFold(err error) bool {
	return strings.EqualFold(err.Error(), "timeout") // want `avoid strings\.EqualFold\(err\.Error\(\)`
}

// flagged: strings.Index on err.Error() with a string literal
func checkIndex(err error) bool {
	return strings.Index(err.Error(), "denied") >= 0 // want `avoid strings\.Index\(err\.Error\(\)`
}

// flagged: strings.LastIndex on err.Error() with a string literal
func checkLastIndex(err error) bool {
	return strings.LastIndex(err.Error(), "denied") >= 0 // want `avoid strings\.LastIndex\(err\.Error\(\)`
}

// flagged: strings.Compare on err.Error() with a string literal
func checkCompare(err error) bool {
	return strings.Compare(err.Error(), "timeout") == 0 // want `avoid strings\.Compare\(err\.Error\(\)`
}

// not flagged: strings.HasPrefix on a plain string, not err.Error()
func checkHasPrefixString(s string) bool {
	return strings.HasPrefix(s, "prefix")
}

// not flagged: strings.EqualFold on a plain string, not err.Error()
func checkEqualFoldString(s string) bool {
	return strings.EqualFold(s, "value")
}
