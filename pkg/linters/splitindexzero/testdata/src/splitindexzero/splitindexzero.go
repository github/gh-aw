package splitindexzero

import "strings"

const sep = "/"

func flagged(s string) string {
	return strings.Split(s, "/")[0] // want `strings\.Split\(\.\.\.\)\[0\] allocates`
}

func flaggedConstIdent(s string) string {
	return strings.Split(s, sep)[0] // want `strings\.Split\(\.\.\.\)\[0\] allocates`
}

func flaggedZeroConst(s string) string {
	const zero = 0
	return strings.Split(s, ":")[zero] // want `strings\.Split\(\.\.\.\)\[0\] allocates`
}

func notFlaggedSecondElement(s string) string {
	return strings.Split(s, "/")[1]
}

func notFlaggedEmptySeparator(s string) string {
	return strings.Split(s, "")[0]
}

func notFlaggedDynamicSeparator(s, d string) string {
	return strings.Split(s, d)[0]
}

func notFlaggedVariable(parts []string) string {
	return parts[0]
}

func notFlaggedDynamicIndex(s string, i int) string {
	return strings.Split(s, "/")[i]
}

func suppressed(s string) string {
	return strings.Split(s, "/")[0] //nolint:splitindexzero
}
