package splitindexzero

import s "strings"

const (
	separator = ","
	limit     = 2
	first     = 0
	empty     = ""
)

func splitFirst(value string) string {
	return s.Split(value, separator)[0]
}

func splitNFirst(value string) string {
	return s.SplitN(value, separator, limit)[first] // want `strings\.Split`
}

func parenthesizedSplit(value string) string {
	return (s.Split(value, ","))[0]
}

func emptySeparator(value string) string {
	return s.Split(value, empty)[0]
}

func dynamicSeparator(value, sep string) string {
	return s.Split(value, sep)[0]
}

func splitNWithDifferentLimit(value string) string {
	return s.SplitN(value, ",", 3)[0]
}

func splitNWithOneLimit(value string) string {
	return s.SplitN(value, ",", 1)[0]
}

func splitNEmptySeparator(value string) string {
	return s.SplitN(value, empty, 2)[0]
}

func splitNDynamicSeparator(value, sep string) string {
	return s.SplitN(value, sep, 2)[0]
}

func laterElement(value string) string {
	return s.Split(value, ",")[1]
}

func dynamicIndex(value string, index int) string {
	return s.Split(value, ",")[index]
}

type fakeStrings struct{}

func (fakeStrings) Split(value, sep string) []string {
	return []string{value}
}

func shadowedPackageName(value string) string {
	strings := fakeStrings{}
	return strings.Split(value, ",")[0]
}

func suppressed(value string) string {
	//nolint:splitindexzero
	return s.SplitN(value, ",", 2)[0]
}
