package stringreplaceminusone

import "strings"

// replaceAllConst is a named constant equivalent to -1 (replace all).
const replaceAllConst = -1

func badLiteral(s string) string {
	return strings.Replace(s, "o", "0", -1) // want `use strings\.ReplaceAll instead of strings\.Replace with n=-1`
}

func badNamedConst(s string) string {
	return strings.Replace(s, "o", "0", replaceAllConst) // want `use strings\.ReplaceAll instead of strings\.Replace with n=-1`
}

func badParenFunc(s string) string {
	return (strings.Replace)(s, "o", "0", -1) // want `use strings\.ReplaceAll instead of strings\.Replace with n=-1`
}

func goodFirstOnly(s string) string {
	return strings.Replace(s, "o", "0", 1) // replace only the first occurrence — n=1 is intentional
}

func goodReplaceAll(s string) string {
	return strings.ReplaceAll(s, "o", "0") // already uses the idiomatic form
}
