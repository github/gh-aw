package stringssplitfirst

import "strings"

const sep = "."

func bad(s string) string {
	return strings.Split(s, ".")[0] // want `strings\.Split\(\.\.\.\)\[0\] allocates a slice`
}

func badConstSep(s string) string {
	return strings.Split(s, sep)[0] // want `strings\.Split\(\.\.\.\)\[0\] allocates a slice`
}

func goodCut(s string) string {
	before, _, _ := strings.Cut(s, ".")
	return before
}

func goodSecondField(s string) string {
	return strings.Split(s, ".")[1]
}

func goodDynamicIndex(s string, i int) string {
	return strings.Split(s, ".")[i]
}

func goodEmptySep(s string) string {
	return strings.Split(s, "")[0]
}

func goodDynamicSep(s, d string) string {
	return strings.Split(s, d)[0]
}

func goodSplitAssigned(s string) string {
	parts := strings.Split(s, ".")
	return parts[0]
}

func suppressed(s string) string {
	return strings.Split(s, ".")[0] //nolint:stringssplitfirst
}
