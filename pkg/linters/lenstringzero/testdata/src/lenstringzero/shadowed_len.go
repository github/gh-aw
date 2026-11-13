package lenstringzero

// A locally shadowed "len" is not the builtin, so comparisons against its
// result must not be flagged or rewritten to string comparisons.
func goodShadowedLen(s string) bool {
	len := func(x string) int { return 0 }
	return len(s) == 0
}

func goodShadowedLenAlias(s string) bool {
	len := func(x string) int { return 0 }
	n := len(s)
	return n == 0
}
