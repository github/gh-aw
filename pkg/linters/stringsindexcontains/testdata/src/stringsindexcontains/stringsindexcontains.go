package stringsindexcontains

import "strings"

func badContains(s, sub string) bool {
	return strings.Index(s, sub) != -1 // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badContainsGEQ(s, sub string) bool {
	return strings.Index(s, sub) >= 0 // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badContainsGTR(s, sub string) bool {
	return strings.Index(s, sub) > -1 // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badNotContains(s, sub string) bool {
	return strings.Index(s, sub) == -1 // want `use !strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badNotContainsLT(s, sub string) bool {
	return strings.Index(s, sub) < 0 // want `use !strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badNotContainsLEQ(s, sub string) bool {
	return strings.Index(s, sub) <= -1 // want `use !strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badYodaContains(s, sub string) bool {
	return -1 != strings.Index(s, sub) // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badYodaContainsLEQ(s, sub string) bool {
	return 0 <= strings.Index(s, sub) // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badYodaNotContains(s, sub string) bool {
	return -1 == strings.Index(s, sub) // want `use !strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badYodaNotContainsGTR(s, sub string) bool {
	return 0 > strings.Index(s, sub) // want `use !strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func goodContains(s, sub string) bool {
	return strings.Contains(s, sub)
}

func goodNotContains(s, sub string) bool {
	return !strings.Contains(s, sub)
}

func goodIndexUsedForPosition(s, sub string) int {
	// Using the index value itself (not just for containment check) is fine.
	return strings.Index(s, sub)
}

func goodIndexComparesNonMinusOne(s, sub string) bool {
	// Comparing against a value other than -1/0 (as a containment sentinel) is fine.
	return strings.Index(s, sub) > 3
}

func goodIndexEqualZero(s, sub string) bool {
	// == 0 is a prefix check (not a containment check) and is intentionally not flagged.
	return strings.Index(s, sub) == 0
}

func badParenContains(s, sub string) bool {
	return (strings.Index(s, sub)) != -1 // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badParenYodaNotContains(s, sub string) bool {
	return -1 == (strings.Index(s, sub)) // want `use !strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}

func badIndexWithComments(s, sub string) bool {
	return strings.Index(s, sub /* substr */) != -1 // want `use strings\.Contains\(s, sub\) instead of strings\.Index comparison`
}
