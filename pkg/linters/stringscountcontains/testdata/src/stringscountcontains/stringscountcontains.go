package stringscountcontains

import "strings"

func badCountGTR(s, sub string) bool {
	return strings.Count(s, sub) > 0 // want `use strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badCountGEQ(s, sub string) bool {
	return strings.Count(s, sub) >= 1 // want `use strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badCountNEQ(s, sub string) bool {
	return strings.Count(s, sub) != 0 // want `use strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badCountEQL(s, sub string) bool {
	return strings.Count(s, sub) == 0 // want `use !strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badCountLSS(s, sub string) bool {
	return strings.Count(s, sub) < 1 // want `use !strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badCountLEQ(s, sub string) bool {
	return strings.Count(s, sub) <= 0 // want `use !strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badYodaGTR(s, sub string) bool {
	return 0 < strings.Count(s, sub) // want `use strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badYodaGEQ(s, sub string) bool {
	return 1 <= strings.Count(s, sub) // want `use strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badYodaNEQ(s, sub string) bool {
	return 0 != strings.Count(s, sub) // want `use strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badYodaEQL(s, sub string) bool {
	return 0 == strings.Count(s, sub) // want `use !strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badYodaLSS(s, sub string) bool {
	return 1 > strings.Count(s, sub) // want `use !strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func goodCountActualCount(s, sub string) bool {
	// Comparing against a value other than 0/1 (as a containment sentinel) is fine.
	return strings.Count(s, sub) > 2
}

func goodContainsAlready(s, sub string) bool {
	return strings.Contains(s, sub)
}

func badParenCountGTR(s, sub string) bool {
	return (strings.Count(s, sub)) > 0 // want `use strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badParenYodaCountEQL(s, sub string) bool {
	return 0 == (strings.Count(s, sub)) // want `use !strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}

func badCountWithComments(s, sub string) bool {
	return strings.Count(s, sub /* substr */) > 0 // want `use strings\.Contains\(s, sub\) instead of strings\.Count comparison`
}
