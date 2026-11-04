package splitindexzeroprefercut

import "strings"

const zero = 0
const nonEmptySeparator = ","

type splitter struct{}

func (splitter) Split(s, sep string) []string { return nil }

func direct(s string) string {
	return strings.Split(s, ",")[0] // want `strings\.Split\(\.\.\.\)\[0\] allocates`
}

func parenthesized(s string) string {
	return (strings.Split(s, ","))[0] // want `strings\.Split\(\.\.\.\)\[0\] allocates`
}

func constIndex(s string) string {
	return strings.Split(s, ",")[zero] // want `strings\.Split\(\.\.\.\)\[0\] allocates`
}

func constSeparator(s string) string {
	return strings.Split(s, nonEmptySeparator)[0] // want `strings\.Split\(\.\.\.\)\[0\] allocates`
}

func nonZero(s string) string {
	return strings.Split(s, ",")[1]
}

func variableIndex(s string, i int) string {
	return strings.Split(s, ",")[i]
}

func variableSeparator(s, sep string) string {
	return strings.Split(s, sep)[0]
}

func emptySeparator(s string) string {
	return strings.Split(s, "")[0]
}

func cutOK(s string) string {
	before, _, _ := strings.Cut(s, ",")
	return before
}

func unrelated(s string) string {
	return splitter{}.Split(s, ",")[0]
}

func splitN(s string) string {
	return strings.SplitN(s, ",", 2)[0]
}

func suppressed(s string) string {
	return strings.Split(s, ",")[0] //nolint:splitindexzeroprefercut
}
