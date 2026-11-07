package stringssplitfirst

import s "strings"

const (
	separator = ","
	first     = 0
	empty     = ""
)

func splitFirst(value string) string {
	return s.Split(value, separator)[first] // want `strings\.Split`
}

func parenthesizedSplit(value string) string {
	return (s.Split(value, ","))[0] // want `strings\.Split`
}

func emptySeparator(value string) string {
	return s.Split(value, empty)[0]
}

func dynamicSeparator(value, sep string) string {
	return s.Split(value, sep)[0]
}

func splitN(value string) string {
	return s.SplitN(value, ",", 2)[0]
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
	//nolint:stringssplitfirst
	return s.Split(value, ",")[0]
}
